import { Agent, MaxTurnsExceededError, Runner, RunState, type AgentInputItem, type RunToolApprovalItem } from "@openai/agents";
import { NonRetriableError } from "inngest";
import { z } from "zod";
import { AetherHttpError, type AetherClient, type Approval, type MarkerKind, type Thread } from "./client";
import {
  Events,
  type ApprovalAnsweredPayload,
  type MessageSentPayload,
  type ScheduleFiredPayload,
  type ToolCall,
} from "./contract";
import { DocLimit, instructionsFor } from "./instructions";
import type { Model } from "./model";
import type { ToolDeps, ToolFactory } from "./tools";

export interface StepLike {
  run<T>(id: string, fn: () => Promise<T>): Promise<T>;
}

export interface TurnEvent {
  name: string;
  data: MessageSentPayload | ScheduleFiredPayload | ApprovalAnsweredPayload;
}

export interface TurnContext {
  daemon: string;
  runId: string;
  client: AetherClient;
  model: Model;
  tools: ToolFactory[];
  step: StepLike;
  maxTurns: number;
  now?: () => Date;
}

export interface TurnInput {
  thread: string;
  text: string;
}

export type TurnResult =
  | { status: "replied"; reply: string }
  | { status: "skipped"; marker: string }
  | { status: "paused"; approvals: string[] }
  | { status: "ignored" };

type MarkerDeps = Pick<TurnContext, "daemon" | "runId" | "client">;

interface Docs {
  soul: string;
  memory: string;
  memoryVersion: number;
  thread: Thread;
}

export function isStale(deadlineAt: string, now: Date): boolean {
  return new Date(deadlineAt).getTime() < now.getTime();
}

export function turnInputFor(event: TurnEvent): TurnInput {
  const thread = event.data.thread;
  if ("schedule" in event.data) return { thread, text: `Run schedule ${event.data.schedule}` };
  return { thread, text: "text" in event.data ? event.data.text : "" };
}

export async function runTurn(ctx: TurnContext, event: TurnEvent): Promise<TurnResult> {
  if (event.name === Events.ScheduleFired && "deadline_at" in event.data && (await stale(ctx, event.data))) {
    return skipStale(ctx, event.data);
  }
  if (event.name === Events.ApprovalAnswered && "approval" in event.data) return resume(ctx, event.data);
  const input = turnInputFor(event);
  const docs = await ctx.step.run("load", () => loadDocs(ctx, input.thread));
  const agent = agentFor(ctx, docs);
  return conclude(ctx, docs, await runAgent(ctx, agent, input.text));
}

function stale(ctx: TurnContext, data: ScheduleFiredPayload): Promise<boolean> {
  const now = ctx.now ?? (() => new Date());
  return ctx.step.run("stale", async () => isStale(data.deadline_at, now()));
}

async function resume(ctx: TurnContext, data: ApprovalAnsweredPayload): Promise<TurnResult> {
  const approval = await ctx.step.run("approval", () => ctx.client.getApproval(data.approval));
  if (approval.status === "pending" || approval.state === undefined) return { status: "ignored" };
  const docs = await ctx.step.run("load", () => loadDocs(ctx, data.thread));
  const agent = agentFor(ctx, docs);
  const state = await RunState.fromString(agent, approval.state);
  const item = state.getInterruptions().find((i) => callIdOf(i) === approval.call.id);
  if (!item) return { status: "ignored" };
  decide(state, item, approval);
  return conclude(ctx, docs, await runAgent(ctx, agent, state));
}

function decide(state: RunState<unknown, Agent>, item: RunToolApprovalItem, approval: Approval): void {
  if (approval.status === "approved") state.approve(item);
  else state.reject(item);
}

function agentFor(ctx: TurnContext, docs: Docs): Agent {
  const deps: ToolDeps = {
    step: ctx.step,
    client: ctx.client,
    daemon: ctx.daemon,
    thread: docs.thread.id,
    cwd: docs.thread.directory,
    host: docs.thread.host,
  };
  const tools = ctx.tools.map((make) => make(deps));
  const instructions = instructionsFor({ daemon: ctx.daemon, soul: docs.soul, memory: docs.memory, tools: tools.map((t) => t.name) });
  return new Agent({ name: ctx.daemon, instructions, tools });
}

async function runAgent(ctx: TurnContext, agent: Agent, input: string | AgentInputItem[] | RunState<unknown, Agent>) {
  const runner = new Runner({ model: ctx.model, tracingDisabled: true });
  try {
    return await runner.run(agent, input, { maxTurns: ctx.maxTurns });
  } catch (err) {
    if (err instanceof MaxTurnsExceededError) throw new NonRetriableError(err.message, { cause: err });
    throw err;
  }
}

type Run = Awaited<ReturnType<typeof runAgent>>;

async function conclude(ctx: TurnContext, docs: Docs, result: Run): Promise<TurnResult> {
  if (result.interruptions.length > 0) return pause(ctx, docs.thread, result);
  const reply = String(result.finalOutput ?? "");
  await ctx.step.run("reply", () => ctx.client.reply(docs.thread.id, reply, `${ctx.runId}:reply`));
  await remember(ctx, docs, result.history);
  return { status: "replied", reply };
}

async function pause(ctx: TurnContext, thread: Thread, result: Run): Promise<TurnResult> {
  const calls = result.interruptions.map((item) => callFor(item, thread));
  const state = result.state.toString();
  const { approvals } = await ctx.step.run("pause", () => ctx.client.requestApproval(thread.id, calls, state));
  await ctx.step.run("mark-paused", () => mark(ctx, "turn.paused", thread.id, approvals.join(","), { calls: calls.map((c) => c.id) }));
  return { status: "paused", approvals };
}

function callFor(item: RunToolApprovalItem, thread: Thread): ToolCall {
  return {
    id: callIdOf(item),
    tool: item.name ?? "unknown",
    args: parseArgs(item.arguments),
    context: { cwd: thread.directory, host: thread.host },
  };
}

function callIdOf(item: RunToolApprovalItem): string {
  if ("callId" in item.rawItem) return item.rawItem.callId;
  throw new Error(`approval item without callId: ${item.rawItem.type}`);
}

const Args = z.record(z.string(), z.unknown());

function parseArgs(text: string | undefined): Record<string, unknown> {
  return Args.parse(JSON.parse(text ?? "{}"));
}

async function remember(ctx: TurnContext, docs: Docs, history: AgentInputItem[]): Promise<void> {
  const agent = new Agent({ name: `${ctx.daemon}-memory`, instructions: memoryInstructions(ctx.daemon, docs.memory) });
  const result = await runAgent(ctx, agent, history);
  const next = String(result.finalOutput ?? "").trim();
  if (next.length === 0 || next === docs.memory.trim()) return;
  await ctx.step.run("put-memory", () => writeMemory(ctx, docs.thread.id, next, docs.memoryVersion));
}

function memoryInstructions(daemon: string, memory: string): string {
  return [
    `You maintain the memory document of ${daemon}, an aether daemon. The conversation is the turn it just completed.`,
    "Reply with the full updated memory document and nothing else: short, factual, free of secrets, no headings about the task. Return the current document unchanged when nothing is worth keeping.",
    `--- current memory ---\n${memory.trim().slice(0, DocLimit)}`,
  ].join("\n\n");
}

export async function skipStale(ctx: TurnContext, data: ScheduleFiredPayload): Promise<TurnResult> {
  const marker = await ctx.step.run("mark-stale", () =>
    mark(ctx, "schedule.stale", data.thread, data.schedule, { due_at: data.due_at, deadline_at: data.deadline_at }),
  );
  return { status: "skipped", marker };
}

async function loadDocs(ctx: TurnContext, thread: string): Promise<Docs> {
  const [soul, memory, t] = await Promise.all([
    ctx.client.getSoul(ctx.daemon).catch(emptyOn404),
    ctx.client.getMemory(ctx.daemon),
    ctx.client.getThread(thread),
  ]);
  return { soul: soul.content, memory: memory.content, memoryVersion: memory.version, thread: t };
}

function emptyOn404(err: unknown) {
  if (err instanceof AetherHttpError && err.status === 404) return { content: "", version: 0 };
  throw err;
}

async function writeMemory(ctx: TurnContext, thread: string, text: string, version: number): Promise<void> {
  try {
    await ctx.client.putMemory(ctx.daemon, text, version);
  } catch (err) {
    if (!(err instanceof AetherHttpError && err.status === 409)) throw err;
    await mark(ctx, "memory.conflict", thread, `${version}`, { expected_version: version });
  }
}

export async function mark(ctx: MarkerDeps, kind: MarkerKind, thread: string, ref: string, detail: unknown): Promise<string> {
  const id = `${ctx.runId}:${kind}`;
  await ctx.client.putMarker(ctx.daemon, { id, kind, thread, ref, detail: JSON.stringify(detail) });
  return id;
}

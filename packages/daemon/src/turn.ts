import { AetherHttpError, type AetherClient, type Approval, type MarkerKind } from "./client";
import {
  Events,
  type ApprovalAnsweredPayload,
  type MessageSentPayload,
  type ScheduleFiredPayload,
  type ToolCall,
} from "./contract";
import type { Model, ModelOutput, ToolRequest, ToolResult, TurnInput } from "./model";
import type { Tool } from "./tools";

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
  tools: Record<string, Tool>;
  step: StepLike;
  now?: () => Date;
}

export type TurnResult =
  | { status: "replied"; reply: string }
  | { status: "skipped"; marker: string }
  | { status: "paused"; approvals: string[] };

export function isStale(deadlineAt: string, now: Date): boolean {
  return new Date(deadlineAt).getTime() < now.getTime();
}

export function turnInputFor(event: TurnEvent): TurnInput {
  const base = { daemon: event.data.daemon, thread: event.data.thread };
  if (event.name === Events.ScheduleFired) {
    const data = event.data as ScheduleFiredPayload;
    return { ...base, kind: "schedule", schedule: data.schedule, text: `Run schedule ${data.schedule}` };
  }
  if (event.name === Events.ApprovalAnswered) {
    return { ...base, kind: "approval", text: (event.data as ApprovalAnsweredPayload).decision };
  }
  return { ...base, kind: "message", text: (event.data as MessageSentPayload).text };
}

export async function runTurn(ctx: TurnContext, event: TurnEvent): Promise<TurnResult> {
  const now = ctx.now ?? (() => new Date());
  if (event.name === Events.ScheduleFired && isStale((event.data as ScheduleFiredPayload).deadline_at, now())) {
    return skipStale(ctx, event.data as ScheduleFiredPayload);
  }
  const input = turnInputFor(event);
  const docs = await ctx.step.run("load", () => loadDocs(ctx));
  const model = (results?: ToolResult[]) =>
    ctx.model({ soul: docs.soul, memory: docs.memory, input, now: now(), ...(results ? { results } : {}) });
  if (event.name === Events.ApprovalAnswered) {
    return finish(ctx, input, docs.memoryVersion, await model([await resume(ctx, event.data as ApprovalAnsweredPayload)]));
  }
  const output = await ctx.step.run("model", () => model());
  if (!output.calls?.length) return finish(ctx, input, docs.memoryVersion, output);
  const calls = await ctx.step.run("calls", () => buildCalls(ctx, input.thread, output.calls ?? []));
  const pending = await ctx.step.run("match", () => unmatched(ctx, input.thread, calls));
  if (pending.length > 0) return pause(ctx, input.thread, pending);
  const results = await executeAll(ctx, calls);
  return finish(ctx, input, docs.memoryVersion, await ctx.step.run("model-results", () => model(results)));
}

async function finish(ctx: TurnContext, input: TurnInput, version: number, output: ModelOutput): Promise<TurnResult> {
  await ctx.step.run("reply", () => ctx.client.reply(input.thread, output.reply, `${ctx.runId}:reply`));
  await ctx.step.run("memory", () => writeMemory(ctx, input.thread, output, version));
  return { status: "replied", reply: output.reply };
}

async function buildCalls(ctx: TurnContext, thread: string, requests: ToolRequest[]): Promise<ToolCall[]> {
  const t = await ctx.client.getThread(thread);
  return requests.map((r, i) => ({
    id: `${ctx.runId}:call:${i}`,
    tool: r.tool,
    args: r.args,
    context: { cwd: t.directory, host: t.host },
  }));
}

async function unmatched(ctx: TurnContext, thread: string, calls: ToolCall[]): Promise<ToolCall[]> {
  const matches = await Promise.all(calls.map((c) => ctx.client.matchCall(ctx.daemon, thread, c)));
  return calls.filter((_, i) => !matches[i]?.allowed);
}

async function pause(ctx: TurnContext, thread: string, calls: ToolCall[]): Promise<TurnResult> {
  const { approvals } = await ctx.step.run("pause", () => ctx.client.requestApproval(thread, calls));
  await ctx.step.run("mark-paused", () => mark(ctx, "turn.paused", thread, approvals.join(","), { calls: calls.map((c) => c.id) }));
  return { status: "paused", approvals };
}

async function executeAll(ctx: TurnContext, calls: ToolCall[]): Promise<ToolResult[]> {
  const results: ToolResult[] = [];
  for (const call of calls) results.push(await ctx.step.run(`exec:${call.id}`, () => execute(ctx, call)));
  return results;
}

export async function execute(ctx: TurnContext, call: ToolCall): Promise<ToolResult> {
  if (!(await ctx.client.claimOperation(call.id, "tool.call"))) {
    return { call, status: "skipped", output: "already executed; output unavailable" };
  }
  const tool = ctx.tools[call.tool];
  if (!tool) return { call, status: "error", output: `unknown tool ${call.tool}` };
  try {
    return { call, status: "ok", output: await tool.run(call.args, { cwd: call.context.cwd }) };
  } catch (err) {
    return { call, status: "error", output: (err as Error).message };
  }
}

async function resume(ctx: TurnContext, data: ApprovalAnsweredPayload): Promise<ToolResult> {
  const approval = await ctx.step.run("approval", () => ctx.client.getApproval(data.approval));
  if (approval.status !== "approved") return denied(approval);
  return ctx.step.run(`exec:${approval.call.id}`, () => execute(ctx, approval.call));
}

function denied(approval: Approval): ToolResult {
  return { call: approval.call, status: "denied", output: `denied by operator (${approval.status})` };
}

async function skipStale(ctx: TurnContext, data: ScheduleFiredPayload): Promise<TurnResult> {
  const marker = await ctx.step.run("mark-stale", () =>
    mark(ctx, "schedule.stale", data.thread, data.schedule, { due_at: data.due_at, deadline_at: data.deadline_at }),
  );
  return { status: "skipped", marker };
}

async function loadDocs(ctx: TurnContext) {
  const [soul, memory] = await Promise.all([
    ctx.client.getSoul(ctx.daemon).catch(emptyOn404),
    ctx.client.getMemory(ctx.daemon),
  ]);
  return { soul: soul.content, memory: memory.content, memoryVersion: memory.version };
}

function emptyOn404(err: unknown) {
  if (err instanceof AetherHttpError && err.status === 404) return { content: "", version: 0 };
  throw err;
}

async function writeMemory(ctx: TurnContext, thread: string, output: ModelOutput, version: number) {
  if (output.memory === undefined) return;
  try {
    await ctx.client.putMemory(ctx.daemon, output.memory, version);
  } catch (err) {
    if (!(err instanceof AetherHttpError && err.status === 409)) throw err;
    await mark(ctx, "memory.conflict", thread, `${version}`, { expected_version: version });
  }
}

export async function mark(ctx: TurnContext, kind: MarkerKind, thread: string, ref: string, detail: unknown) {
  const id = `${ctx.runId}:${kind}`;
  await ctx.client.putMarker(ctx.daemon, { id, kind, thread, ref, detail: JSON.stringify(detail) });
  return id;
}

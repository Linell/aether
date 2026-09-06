import { AetherHttpError, type AetherClient, type MarkerKind } from "./client";
import { Events, type MessageSentPayload, type ScheduleFiredPayload } from "./contract";
import type { Model, ModelOutput, TurnInput } from "./model";

export interface StepLike {
  run<T>(id: string, fn: () => Promise<T>): Promise<T>;
}

export interface TurnEvent {
  name: string;
  data: MessageSentPayload | ScheduleFiredPayload;
}

export interface TurnContext {
  daemon: string;
  runId: string;
  client: AetherClient;
  model: Model;
  step: StepLike;
  now?: () => Date;
}

export type TurnResult = { status: "replied"; reply: string } | { status: "skipped"; marker: string };

export function isStale(deadlineAt: string, now: Date): boolean {
  return new Date(deadlineAt).getTime() < now.getTime();
}

export function turnInputFor(event: TurnEvent): TurnInput {
  const base = { daemon: event.data.daemon, thread: event.data.thread };
  if (event.name === Events.ScheduleFired) {
    const data = event.data as ScheduleFiredPayload;
    return { ...base, kind: "schedule", schedule: data.schedule, text: `Run schedule ${data.schedule}` };
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
  const output = await ctx.step.run("model", () =>
    ctx.model({ soul: docs.soul, memory: docs.memory, input, now: now() }),
  );
  await ctx.step.run("reply", () => ctx.client.reply(input.thread, output.reply, `${ctx.runId}:reply`));
  await ctx.step.run("memory", () => writeMemory(ctx, input.thread, output, docs.memoryVersion));
  return { status: "replied", reply: output.reply };
}

async function skipStale(ctx: TurnContext, data: ScheduleFiredPayload): Promise<TurnResult> {
  const marker = `${ctx.runId}:schedule.stale`;
  await ctx.step.run("mark-stale", () =>
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

export function mark(ctx: TurnContext, kind: MarkerKind, thread: string, ref: string, detail: unknown) {
  return ctx.client.putMarker(ctx.daemon, {
    id: `${ctx.runId}:${kind}`,
    kind,
    thread,
    ref,
    detail: JSON.stringify(detail),
  });
}

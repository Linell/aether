import { Inngest, NonRetriableError } from "inngest";
import { z } from "zod";
import { connect, type WorkerConnection } from "inngest/connect";
import { createClient, type AetherClient, type Daemon } from "./client";
import { Events, TurnConcurrency } from "./contract";
import { modelFor, modelSpecFor, type Model } from "./model";
import { scheduleDelete, scheduleList, schedulePut } from "./schedules";
import { editFile, readFile, writeFile } from "./files";
import { shell, type ToolFactory } from "./tools";
import { mark, runTurn, type StepLike, type TurnContext, type TurnEvent } from "./turn";

export interface DefineDaemonOptions {
  name: string;
  model?: Model;
  tools?: ToolFactory[];
}

export interface DefinedDaemon {
  name: string;
  model?: Model;
  tools: ToolFactory[];
  _brand: "aether-daemon";
}

export const DefaultMaxTurns = 50;

const Ids = { daemon: z.string(), thread: z.string() };

const TurnPayload = z.union([
  z.object({ ...Ids, message: z.string(), text: z.string() }),
  z.object({ ...Ids, schedule: z.string(), due_at: z.string(), deadline_at: z.string(), prompt: z.string() }),
  z.object({ ...Ids, call: z.string(), approval: z.string(), decision: z.enum(["approved", "denied"]) }),
]);

export function turnEventOf(event: { name: string; data?: unknown }): TurnEvent {
  const parsed = TurnPayload.safeParse(event.data);
  if (!parsed.success) throw new NonRetriableError(`${event.name}: payload outside the contract`, { cause: parsed.error });
  return { name: event.name, data: parsed.data };
}

export function threadOf(event: { data?: unknown }): string {
  const parsed = z.object({ thread: z.string() }).safeParse(event.data);
  return parsed.success ? parsed.data.thread : "";
}

export function defineDaemon(opts: DefineDaemonOptions): DefinedDaemon {
  const base: DefinedDaemon = { name: opts.name, tools: opts.tools ?? [shell, readFile, editFile, writeFile, scheduleList, schedulePut, scheduleDelete], _brand: "aether-daemon" };
  return opts.model === undefined ? base : { ...base, model: opts.model };
}

export function appId(name: string): string {
  return `daemon-${name}`;
}

export function maxTurnsFrom(env: Record<string, string | undefined>): number {
  const raw = env.AETHER_MAX_TURNS;
  if (raw === undefined || raw === "") return DefaultMaxTurns;
  const n = Number(raw);
  if (!Number.isInteger(n) || n < 1) throw new Error(`AETHER_MAX_TURNS: expected a positive integer, got ${JSON.stringify(raw)}`);
  return n;
}

export function turnConfig(name: string) {
  const filter = `event.data.daemon == ${JSON.stringify(name)}`;
  return {
    id: "turn",
    triggers: [
      { event: Events.MessageSent, if: filter },
      { event: Events.ScheduleFired, if: filter },
      { event: Events.ApprovalAnswered, if: filter },
    ],
    concurrency: { key: TurnConcurrency.key, limit: TurnConcurrency.limit },
    retries: 5 as const,
  };
}

export function modelResolver(daemon: DefinedDaemon, step: StepLike, env: Record<string, string | undefined>): (doc: Daemon, scope?: string) => Model {
  return (doc, scope) => daemon.model ?? modelFor(modelSpecFor(doc, env), step, scope);
}

function contextFor(daemon: DefinedDaemon, client: AetherClient, runId: string, step: StepLike, env: Record<string, string | undefined>): TurnContext {
  const resolveModel = modelResolver(daemon, step, env);
  return { daemon: daemon.name, runId, client, resolveModel, tools: daemon.tools, step, maxTurns: maxTurnsFrom(env) };
}

export function buildFunctions(inngest: Inngest, daemon: DefinedDaemon, client: AetherClient, env: Record<string, string | undefined> = process.env) {
  const turn = inngest.createFunction(
    {
      ...turnConfig(daemon.name),
      onFailure: async ({ event, runId }) => {
        await mark({ daemon: daemon.name, runId, client }, "turn.failed", threadOf(event.data.event), event.data.run_id, event.data.error);
      },
    },
    async ({ event, step, runId }) => {
      const steps: StepLike = { run: (id, fn) => step.run(id, fn) as Promise<never> };
      return runTurn(contextFor(daemon, client, runId, steps, env), turnEventOf(event));
    },
  );
  return [turn];
}

export function clientFromEnv(env: Record<string, string | undefined>): AetherClient {
  const baseUrl = env.AETHER_URL;
  const token = env.AETHER_TOKEN;
  if (!baseUrl || !token) throw new Error("AETHER_URL and AETHER_TOKEN are required");
  return createClient({ baseUrl, token });
}

export async function start(daemon: DefinedDaemon, env = process.env): Promise<WorkerConnection> {
  const client = clientFromEnv(env);
  const inngest = new Inngest({ id: appId(daemon.name) });
  const conn = await connect({
    apps: [{ client: inngest, functions: buildFunctions(inngest, daemon, client, env) }],
    instanceId: daemon.name,
    maxWorkerConcurrency: 1,
  });
  console.log(`${appId(daemon.name)} connected (${conn.connectionId})`);
  return conn;
}

import { Inngest } from "inngest";
import { connect, type WorkerConnection } from "inngest/connect";
import { createClient, type AetherClient } from "./client";
import { Events, TurnConcurrency } from "./contract";
import { templateModel, type Model } from "./model";
import { shell, toolMap, type Tool } from "./tools";
import { mark, runTurn, type StepLike, type TurnEvent } from "./turn";

export interface DefineDaemonOptions {
  name: string;
  model?: Model;
  tools?: Tool[];
}

export interface DefinedDaemon {
  name: string;
  model: Model;
  tools: Record<string, Tool>;
  _brand: "aether-daemon";
}

export function defineDaemon(opts: DefineDaemonOptions): DefinedDaemon {
  return { name: opts.name, model: opts.model ?? templateModel, tools: toolMap(opts.tools ?? [shell]), _brand: "aether-daemon" };
}

export function appId(name: string): string {
  return `daemon-${name}`;
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
    retries: 3 as const,
  };
}

export function buildFunctions(inngest: Inngest, daemon: DefinedDaemon, client: AetherClient) {
  const turn = inngest.createFunction(
    {
      ...turnConfig(daemon.name),
      onFailure: async ({ event, runId }) => {
        const original = event.data.event as unknown as TurnEvent;
        const ctx = { daemon: daemon.name, runId, client, model: daemon.model, tools: daemon.tools, step: passthrough };
        await mark(ctx, "turn.failed", original.data.thread, event.data.run_id, event.data.error);
      },
    },
    async ({ event, step, runId }) => {
      const steps: StepLike = { run: (id, fn) => step.run(id, fn) as Promise<never> };
      const ctx = { daemon: daemon.name, runId, client, model: daemon.model, tools: daemon.tools, step: steps };
      return runTurn(ctx, event as unknown as TurnEvent);
    },
  );
  return [turn];
}

const passthrough: StepLike = { run: (_id, fn) => fn() };

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
    apps: [{ client: inngest, functions: buildFunctions(inngest, daemon, client) }],
    instanceId: daemon.name,
  });
  console.log(`${appId(daemon.name)} connected (${conn.connectionId})`);
  return conn;
}

import { tool } from "@openai/agents";
import { z } from "zod";
import { AetherHttpError, type ScheduleInput } from "./client";
import { hostTimeZone } from "./instructions";
import { callIdFrom, type ToolDeps, type ToolFactory } from "./tools";

const Slug = z.string().regex(/^[a-z][a-z0-9-]{0,63}$/, "lowercase letters, digits, hyphens, starting with a letter");

const PutArgs = z.object({
  id: Slug,
  cron: z.string().min(1),
  tz: z.string().min(1).nullable(),
  prompt: z.string().min(1),
});

const DeleteArgs = z.object({ id: Slug });

export const scheduleList: ToolFactory = (deps) =>
  tool({
    name: "schedule_list",
    description: "List your schedules: id, cron, tz, prompt, next_run_at.",
    parameters: z.object({}),
    execute: (_args, _ctx, details) => deps.step.run(`schedules:${callIdFrom(details)}`, () => list(deps)),
  });

export const schedulePut: ToolFactory = (deps) =>
  tool({
    name: "schedule_put",
    description: `Create or replace one of your schedules. \`cron\` is standard five-field cron, \`tz\` an IANA zone (null means the host zone, ${hostTimeZone()}), \`prompt\` the request you will receive when it fires.`,
    parameters: PutArgs,
    execute: (args, _ctx, details) => claimed(deps, callIdFrom(details), () => put(deps, args)),
  });

export const scheduleDelete: ToolFactory = (deps) =>
  tool({
    name: "schedule_delete",
    description: "Delete one of your schedules by id.",
    parameters: DeleteArgs,
    execute: (args, _ctx, details) => claimed(deps, callIdFrom(details), () => remove(deps, args.id)),
  });

function claimed(deps: ToolDeps, id: string, fn: () => Promise<string>): Promise<string> {
  return deps.step.run(`schedule:${id}`, async () => {
    if (!(await deps.client.claimOperation(id, "tool.call"))) return "already executed";
    return fn().catch(describe);
  });
}

async function list(deps: ToolDeps): Promise<string> {
  const schedules = await deps.client.listSchedules(deps.daemon);
  if (schedules.length === 0) return "no schedules";
  return schedules.map((s) => `${s.id}: ${s.cron} ${s.tz} next ${s.next_run_at}\n  ${s.prompt}`).join("\n");
}

async function put(deps: ToolDeps, args: z.infer<typeof PutArgs>): Promise<string> {
  const input: ScheduleInput = { thread: deps.thread, cron: args.cron, tz: args.tz ?? hostTimeZone(), policy: "queue", prompt: args.prompt };
  const saved = await deps.client.putSchedule(deps.daemon, args.id, input);
  return `saved ${saved.id}: ${saved.cron} ${saved.tz}, next ${saved.next_run_at}`;
}

async function remove(deps: ToolDeps, id: string): Promise<string> {
  await deps.client.deleteSchedule(deps.daemon, id);
  return `deleted ${id}`;
}

function describe(err: unknown): string {
  if (err instanceof AetherHttpError) return `error ${err.status}: ${err.body}`;
  return `error: ${err instanceof Error ? err.message : String(err)}`;
}

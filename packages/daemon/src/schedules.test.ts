import { expect, test } from "bun:test";
import { RunContext, type Tool } from "@openai/agents";
import type { AetherClient, Schedule, ScheduleInput } from "./client";
import { AetherHttpError } from "./client";
import { hostTimeZone } from "./instructions";
import { scheduleDelete, schedulePut } from "./schedules";
import type { ToolDeps } from "./tools";

type Fn = Extract<Tool, { type: "function" }>;

function setup(putResult: (input: ScheduleInput) => Promise<Schedule>) {
  const puts: { id: string; input: ScheduleInput }[] = [];
  const deletes: string[] = [];
  const claims: string[] = [];
  const client = {
    async claimOperation(id: string) {
      const fresh = !claims.includes(id);
      claims.push(id);
      return fresh;
    },
    async putSchedule(_daemon: string, id: string, input: ScheduleInput) {
      puts.push({ id, input });
      return putResult(input);
    },
    async deleteSchedule(_daemon: string, id: string) {
      deletes.push(id);
    },
  } as Partial<AetherClient> as AetherClient;
  const deps: ToolDeps = { step: { run: (_id, fn) => fn() }, client, daemon: "sage", thread: "t1", cwd: "/srv", host: "laptop" };
  return { puts, deletes, put: schedulePut(deps) as Fn, del: scheduleDelete(deps) as Fn };
}

const run = (t: Fn, args: unknown, callId: string) =>
  t.invoke(new RunContext(), JSON.stringify(args), { toolCall: { type: "function_call", callId, name: t.name, arguments: "" } });

test("put binds the daemon's thread, defaults tz, and claims once", async () => {
  const saved: Schedule = { id: "morning", daemon: "sage", thread: "t1", cron: "0 7 * * *", tz: hostTimeZone(), next_run_at: "n", policy: "queue", prompt: "Review" };
  const { puts, put } = setup(async () => saved);
  const args = { id: "morning", cron: "0 7 * * *", tz: null, prompt: "Review" };
  expect(await run(put, args, "c1")).toContain("saved morning");
  expect(await run(put, args, "c1")).toBe("already executed");
  expect(puts).toEqual([{ id: "morning", input: { thread: "t1", cron: "0 7 * * *", tz: hostTimeZone(), policy: "queue", prompt: "Review" } }]);
});

test("api rejections come back as text and bad ids never reach the api", async () => {
  const { puts, deletes, put, del } = setup(() => Promise.reject(new AetherHttpError(400, `invalid cron "x"`)));
  expect(await run(put, { id: "m", cron: "x", tz: "UTC", prompt: "p" }, "c2")).toBe(`error 400: invalid cron "x"`);
  expect(String(await run(del, { id: "Bad Id" }, "c3"))).toContain("Invalid");
  expect(puts).toHaveLength(1);
  expect(deletes).toEqual([]);
});

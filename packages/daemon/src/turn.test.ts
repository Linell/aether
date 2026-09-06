import { describe, expect, test } from "bun:test";
import { AetherHttpError, type AetherClient, type Approval, type Marker } from "./client";
import { Events, type ToolCall } from "./contract";
import { templateModel } from "./model";
import type { Tool } from "./tools";
import { isStale, runTurn, type TurnContext } from "./turn";

const call: ToolCall = { id: "run-0:call:0", tool: "echo", args: { argv: ["echo", "hi"] }, context: { cwd: "/srv/foo", host: "h1" } };

function fakeClient(opts: { allowed?: boolean; approval?: Approval; memoryVersion?: number } = {}) {
  const calls = {
    replies: [] as { thread: string; text: string; id?: string }[],
    memory: [] as string[],
    markers: [] as Marker[],
    approvals: [] as ToolCall[],
    claims: [] as string[],
  };
  const client: Partial<AetherClient> = {
    async reply(thread, text, id) {
      calls.replies.push({ thread, text, ...(id === undefined ? {} : { id }) });
    },
    async putMarker(_daemon, marker) {
      calls.markers.push(marker);
    },
    async requestApproval(_thread, requested) {
      calls.approvals.push(...requested);
      return { approval: "a1", approvals: ["a1"] };
    },
    async getThread(id) {
      return { id, daemon: "foo", host: "h1", directory: "/srv/foo" };
    },
    async getApproval() {
      if (!opts.approval) throw new AetherHttpError(404, "not found");
      return opts.approval;
    },
    async matchCall() {
      return { allowed: opts.allowed ?? false };
    },
    async claimOperation(id) {
      const fresh = !calls.claims.includes(id);
      calls.claims.push(id);
      return fresh;
    },
    async getSoul() {
      throw new AetherHttpError(404, "not found");
    },
    async getMemory() {
      return { content: "", version: opts.memoryVersion ?? 0 };
    },
    async putMemory(_daemon, body, version) {
      calls.memory.push(body);
      return { content: body, version: version + 1 };
    },
  };
  return { client: client as AetherClient, calls };
}

const echo: Tool = { name: "echo", run: async (args) => `ran ${(args.argv as string[]).join(" ")}` };
const shellish: Tool = { name: "shell", run: async (args) => `ran ${(args.argv as string[]).join(" ")}` };

function ctx(client: AetherClient, now: Date, runId = "run-1"): TurnContext {
  const tools = { echo, shell: shellish };
  return { daemon: "foo", runId, client, model: templateModel, tools, step: { run: (_id, fn) => fn() }, now: () => now };
}

const now = new Date("2026-09-06T07:00:00Z");
const message = (text: string) => ({ name: Events.MessageSent, data: { daemon: "foo", thread: "t1", message: "m1", text } });

describe("runTurn", () => {
  test("message turn replies with a stable id and writes memory", async () => {
    const { client, calls } = fakeClient();
    const result = await runTurn(ctx(client, now), message("hello"));
    expect(result.status).toBe("replied");
    expect(calls.replies).toEqual([{ thread: "t1", text: "Noted: hello", id: "run-1:reply" }]);
    expect(calls.memory).toEqual(["2026-09-06: heard hello"]);
  });

  test("stale schedule writes a marker and no reply", async () => {
    const { client, calls } = fakeClient();
    const result = await runTurn(ctx(client, now), {
      name: Events.ScheduleFired,
      data: { daemon: "foo", thread: "t1", schedule: "s1", due_at: "2026-09-06T06:00:00Z", deadline_at: "2026-09-06T06:05:00Z" },
    });
    expect(result.status).toBe("skipped");
    expect(calls.replies).toEqual([]);
    expect(calls.markers.map((m) => [m.kind, m.ref])).toEqual([["schedule.stale", "s1"]]);
  });

  test("allowed tool call runs once and feeds the reply", async () => {
    const { client, calls } = fakeClient({ allowed: true });
    const result = await runTurn(ctx(client, now), message("run echo hi"));
    expect(result.status).toBe("replied");
    expect(calls.claims).toEqual(["run-1:call:0"]);
    expect(calls.replies[0]?.text).toBe("shell echo hi → ok\nran echo hi");
  });

  test("unmatched tool call requests approval and pauses", async () => {
    const { client, calls } = fakeClient({ allowed: false });
    const result = await runTurn(ctx(client, now), message("run rm -rf x"));
    expect(result).toEqual({ status: "paused", approvals: ["a1"] });
    expect(calls.approvals.map((c) => [c.id, c.context.cwd])).toEqual([["run-1:call:0", "/srv/foo"]]);
    expect(calls.markers.map((m) => [m.kind, m.ref])).toEqual([["turn.paused", "a1"]]);
    expect(calls.replies).toEqual([]);
  });

  test("approval answered executes the approved call exactly once", async () => {
    const approval: Approval = { id: "a1", daemon: "foo", thread: "t1", status: "approved", call };
    const { client, calls } = fakeClient({ approval });
    const event = { name: Events.ApprovalAnswered, data: { daemon: "foo", thread: "t1", call: call.id, approval: "a1", decision: "approved" as const } };
    await runTurn(ctx(client, now, "run-2"), event);
    await runTurn(ctx(client, now, "run-3"), event);
    expect(calls.replies.map((r) => r.text)).toEqual([
      "echo echo hi → ok\nran echo hi",
      "echo echo hi → skipped\nalready executed; output unavailable",
    ]);
  });

  test("denied approval replies without running the tool", async () => {
    const approval: Approval = { id: "a1", daemon: "foo", thread: "t1", status: "denied", call };
    const { client, calls } = fakeClient({ approval });
    await runTurn(ctx(client, now), {
      name: Events.ApprovalAnswered,
      data: { daemon: "foo", thread: "t1", call: call.id, approval: "a1", decision: "denied" },
    });
    expect(calls.claims).toEqual([]);
    expect(calls.replies[0]?.text).toContain("denied");
  });
});

test("isStale compares deadline to now", () => {
  expect(isStale("2026-09-06T06:59:59Z", now)).toBe(true);
  expect(isStale("2026-09-06T07:00:00Z", now)).toBe(false);
});

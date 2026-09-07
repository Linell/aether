import { describe, expect, test } from "bun:test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { AetherHttpError, type AetherClient, type Approval, type Daemon, type HistoryQuery, type Marker, type Message } from "./client";
import { Events, type ToolCall } from "./contract";
import { modelFor } from "./model";
import { shell } from "./tools";
import { isStale, runTurn, turnInputFor, type StepLike, type TurnContext } from "./turn";

interface Seen {
  replies: { thread: string; text: string; id?: string }[];
  memory: string[];
  markers: Marker[];
  approvals: { calls: ToolCall[]; state: string }[];
  claims: string[];
  history: HistoryQuery[];
}

const dir = mkdtempSync(join(tmpdir(), "aether-turn-"));
const now = new Date("2026-09-06T07:00:00Z");

function fakeClient(opts: { allowed?: boolean; approval?: Approval; history?: Message[] } = {}) {
  const seen: Seen = { replies: [], memory: [], markers: [], approvals: [], claims: [], history: [] };
  const unused = () => Promise.reject(new Error("unused"));
  const client: AetherClient = {
    async reply(thread, text, id) {
      seen.replies.push({ thread, text, ...(id === undefined ? {} : { id }) });
    },
    async putMarker(_daemon, marker) {
      seen.markers.push(marker);
    },
    async requestApproval(_thread, calls, state) {
      seen.approvals.push({ calls, state });
      return { approval: "a1", approvals: calls.map((c) => `a:${c.id}`) };
    },
    getThread: async (id) => ({ id, daemon: "foo", host: "h1", directory: dir }),
    async listMessages(_thread, query) {
      seen.history.push(query ?? {});
      return opts.history ?? [];
    },
    getDaemon: async (name) => ({ name, host: "h1", class: "worker", offline_policy: "skip", status: "online", model: "scripted" }),
    async getApproval() {
      if (!opts.approval) throw new AetherHttpError(404, "not found");
      return opts.approval;
    },
    matchCall: async () => ({ allowed: opts.allowed ?? false }),
    async claimOperation(id) {
      const fresh = !seen.claims.includes(id);
      seen.claims.push(id);
      return fresh;
    },
    getSoul: () => Promise.reject(new AetherHttpError(404, "not found")),
    getMemory: async () => ({ content: "", version: 3 }),
    async putMemory(_daemon, body, version) {
      seen.memory.push(body);
      return { content: body, version: version + 1 };
    },
    listSchedules: unused,
    putSchedule: unused,
    deleteSchedule: unused,
  };
  return { client, seen };
}

function ctx(client: AetherClient, runId = "run-1", resolved: Daemon[] = [], ids: string[] = []): TurnContext {
  const step: StepLike = { run: (id, fn) => (ids.push(id), fn()) };
  const resolveModel = (doc: Daemon, scope?: string) => {
    resolved.push(doc);
    return modelFor({ provider: "scripted", name: "scripted" }, step, scope);
  };
  return { daemon: "foo", runId, client, resolveModel, tools: [shell], step, maxTurns: 10, now: () => now };
}

const message = { name: Events.MessageSent, data: { daemon: "foo", thread: "t1", message: "m1", text: "hello" } };

async function paused() {
  const { client, seen } = fakeClient({ allowed: false });
  const result = await runTurn(ctx(client), message);
  return { result, seen };
}

describe("runTurn", () => {
  test("allowed calls run and the reply carries their output", async () => {
    const { client, seen } = fakeClient({ allowed: true });
    const resolved: Daemon[] = [];
    const ids: string[] = [];
    const result = await runTurn(ctx(client, "run-1", resolved, ids), message);
    expect(result).toEqual({ status: "replied", reply: "done: alpha, beta" });
    expect(resolved.map((d) => d.model)).toEqual(["scripted", "scripted"]);
    expect(ids).toEqual(["load", "model-1", "match:call_a", "match:call_b", "exec:call_a", "exec:call_b", "model-2", "reply", "memory-1", "put-memory"]);
    expect(seen.claims).toEqual(["call_a", "call_b"]);
    expect(seen.replies).toEqual([{ thread: "t1", text: "done: alpha, beta", id: "run-1:reply" }]);
    expect(seen.memory).toHaveLength(1);
  });

  test("denied calls pause with the run state and a marker", async () => {
    const { result, seen } = await paused();
    expect(result).toEqual({ status: "paused", approvals: ["a:call_a", "a:call_b"] });
    expect(seen.approvals[0]?.calls.map((c) => [c.id, c.tool, c.args, c.context.cwd])).toEqual([
      ["call_a", "shell", { argv: ["echo", "alpha"] }, dir],
      ["call_b", "shell", { argv: ["echo", "beta"] }, dir],
    ]);
    expect(seen.approvals[0]?.state.length).toBeGreaterThan(0);
    expect(seen.markers.map((m) => [m.kind, m.ref])).toEqual([["turn.paused", "a:call_a,a:call_b"]]);
    expect(seen.replies).toEqual([]);
  });

  test("an approved call runs and the rest pause again with fresh state", async () => {
    const first = await paused();
    const [state, call] = [first.seen.approvals[0]?.state ?? "", first.seen.approvals[0]?.calls[0]];
    if (!call) throw new Error("no paused call");
    const approval: Approval = { id: "a:call_a", daemon: "foo", thread: "t1", status: "approved", call, state };
    const { client, seen } = fakeClient({ approval });
    const result = await runTurn(ctx(client, "run-2"), {
      name: Events.ApprovalAnswered,
      data: { daemon: "foo", thread: "t1", call: "call_a", approval: "a:call_a", decision: "approved" },
    });
    expect(result).toEqual({ status: "paused", approvals: ["a:call_b"] });
    expect(seen.claims).toEqual(["call_a"]);
    expect(seen.approvals[0]?.calls.map((c) => c.id)).toEqual(["call_b"]);
    expect(seen.approvals[0]?.state).not.toBe(state);
  });

  test("an answer for a call not in the state is ignored", async () => {
    const first = await paused();
    const call: ToolCall = { id: "call_z", tool: "shell", args: {}, context: { cwd: dir, host: "h1" } };
    const approval: Approval = { id: "a9", daemon: "foo", thread: "t1", status: "approved", call, state: first.seen.approvals[0]?.state ?? "" };
    const { client, seen } = fakeClient({ approval });
    const result = await runTurn(ctx(client, "run-3"), {
      name: Events.ApprovalAnswered,
      data: { daemon: "foo", thread: "t1", call: "call_z", approval: "a9", decision: "approved" },
    });
    expect(result).toEqual({ status: "ignored" });
    expect(seen.claims).toEqual([]);
  });

  test("stale schedule writes a marker and no reply", async () => {
    const { client, seen } = fakeClient();
    const result = await runTurn(ctx(client), {
      name: Events.ScheduleFired,
      data: { daemon: "foo", thread: "t1", schedule: "s1", due_at: "2026-09-06T06:00:00Z", deadline_at: "2026-09-06T06:05:00Z", prompt: "Review" },
    });
    expect(result).toEqual({ status: "skipped", marker: "run-1:schedule.stale" });
    expect(seen.replies).toEqual([]);
    expect(seen.markers.map((m) => [m.kind, m.ref])).toEqual([["schedule.stale", "s1"]]);
  });
});

test("a message turn loads history before the triggering message", async () => {
  const { client, seen } = fakeClient({ history: [{ id: "m0", role: "user", text: "earlier", created_at: "2026-09-06T06:00:00Z" }] });
  await runTurn(ctx(client), message);
  expect(seen.history).toEqual([{ before: "m1" }]);
});

test("a fired schedule turns into its prompt", () => {
  const data = { daemon: "foo", thread: "t1", schedule: "s1", due_at: "2026-09-06T06:00:00Z", deadline_at: "2026-09-06T06:05:00Z", prompt: "Review the day" };
  expect(turnInputFor({ name: Events.ScheduleFired, data })).toEqual({ thread: "t1", text: "Schedule s1 fired (due 2026-09-06T06:00:00Z): Review the day" });
});

test("isStale compares deadline to now", () => {
  expect(isStale("2026-09-06T06:59:59Z", now)).toBe(true);
  expect(isStale("2026-09-06T07:00:00Z", now)).toBe(false);
});

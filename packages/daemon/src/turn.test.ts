import { describe, expect, test } from "bun:test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Usage, type AgentInputItem, type AgentOutputItem } from "@openai/agents";
import { NonRetriableError } from "inngest";
import { AetherHttpError, type AetherClient, type ApprovalGroup, type Daemon, type HistoryQuery, type Marker, type Message } from "./client";
import { Events, type Decision, type ToolCall } from "./contract";
import { contentText } from "./history";
import { modelFor, type Model } from "./model";
import { shell } from "./tools";
import { ContinuePrompt, DeniedMessage, MaxContinues, isStale, runTurn, turnInputFor, type StepLike, type TurnContext } from "./turn";

interface Seen {
  replies: { thread: string; text: string; id?: string }[];
  memory: string[];
  markers: Marker[];
  approvals: { calls: ToolCall[]; state: string; run: string }[];
  claims: string[];
  history: HistoryQuery[];
}

type Input = string | AgentInputItem[];
type Respond = (input: Input, n: number) => AgentOutputItem[];

interface Options {
  runId?: string;
  ids?: string[];
  resolved?: Daemon[];
  inputs?: Input[];
  main?: Respond;
  memory?: Respond;
  maxTurns?: number;
}

const dir = mkdtempSync(join(tmpdir(), "aether-turn-"));
const now = new Date("2026-09-06T07:00:00Z");

function fakeClient(opts: { allowed?: boolean; group?: ApprovalGroup; history?: Message[] } = {}) {
  const seen: Seen = { replies: [], memory: [], markers: [], approvals: [], claims: [], history: [] };
  const unused = () => Promise.reject(new Error("unused"));
  const client: AetherClient = {
    async reply(thread, text, id) {
      seen.replies.push({ thread, text, ...(id === undefined ? {} : { id }) });
    },
    async putMarker(_daemon, marker) {
      seen.markers.push(marker);
    },
    async requestApproval(_thread, calls, state, run) {
      seen.approvals.push({ calls, state, run });
      return { group: `g:${run}`, approvals: calls.map((c) => `a:${c.id}`) };
    },
    getThread: async (id) => ({ id, daemon: "foo", host: "h1", directory: dir }),
    async listMessages(_thread, query) {
      seen.history.push(query ?? {});
      return opts.history ?? [];
    },
    getDaemon: async (name) => ({ name, host: "h1", class: "worker", offline_policy: "skip", status: "online", model: "scripted" }),
    getApproval: unused,
    async getApprovalGroup() {
      if (!opts.group) throw new AetherHttpError(404, "not found");
      return opts.group;
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

function say(text: string): AgentOutputItem[] {
  return [{ type: "message", role: "assistant", status: "completed", content: [{ type: "output_text", text }] }];
}

function reply(status: "working" | "done", text: string): AgentOutputItem[] {
  return say(JSON.stringify({ status, text }));
}

function shellCall(callId: string, argv: string[]): AgentOutputItem[] {
  return [{ type: "function_call", callId, name: "shell", status: "completed", arguments: JSON.stringify({ argv }) }];
}

function textIn(input: Input): string {
  if (typeof input === "string") return input;
  return input.map((item) => ("content" in item ? contentText(item.content) : item.type === "function_call_result" ? contentText(item.output) : "")).join("\n");
}

function resultsIn(input: Input): string[] {
  if (typeof input === "string") return [];
  return input.flatMap((item) => (item.type === "function_call_result" ? [contentText(item.output)] : []));
}

const rememberAll: Respond = (input) => say(`memory: ${textIn(input)}`);

function fakeModel(step: StepLike, scope: string, respond: Respond, inputs: Input[]): Model {
  let n = 0;
  return {
    async getResponse(request) {
      n += 1;
      inputs.push(request.input);
      const output = await step.run(`${scope}:${n}`, async () => respond(request.input, n));
      return { output, usage: new Usage() };
    },
    getStreamedResponse() {
      throw new Error("never streams");
    },
  };
}

function ctx(client: AetherClient, opts: Options = {}): TurnContext {
  const ids = opts.ids ?? [];
  const step: StepLike = { run: (id, fn) => (ids.push(id), fn()) };
  const resolveModel = (doc: Daemon, scope?: string) => {
    opts.resolved?.push(doc);
    if (scope === "memory") return fakeModel(step, "memory", opts.memory ?? rememberAll, opts.inputs ?? []);
    if (opts.main) return fakeModel(step, "main", opts.main, opts.inputs ?? []);
    return modelFor({ provider: "scripted", name: "scripted" }, step, scope);
  };
  return { daemon: "foo", runId: opts.runId ?? "run-1", client, resolveModel, tools: [shell], step, maxTurns: opts.maxTurns ?? 10, now: () => now };
}

const message = { name: Events.MessageSent, data: { daemon: "foo", thread: "t1", message: "m1", text: "hello" } };

async function paused(opts: Options = {}) {
  const { client, seen } = fakeClient({ allowed: false });
  const result = await runTurn(ctx(client, opts), message);
  return { result, seen };
}

function decidedGroup(seen: Seen, ...decisions: [string, Decision["decision"]][]): ApprovalGroup {
  const state = seen.approvals[0]?.state ?? "";
  return { id: "g:run-1", thread: "t1", status: "decided", state, decisions: decisions.map(([call, decision]) => ({ call, approval: `a:${call}`, decision })) };
}

const answered = { name: Events.ApprovalAnswered, data: { daemon: "foo", thread: "t1", group: "g:run-1", decisions: [] } };

describe("runTurn", () => {
  test("allowed calls run and the reply carries their output", async () => {
    const { client, seen } = fakeClient({ allowed: true });
    const resolved: Daemon[] = [];
    const ids: string[] = [];
    const result = await runTurn(ctx(client, { resolved, ids }), message);
    expect(result).toEqual({ status: "replied", reply: "done: alpha, beta" });
    expect(resolved.map((d) => d.model)).toEqual(["scripted", "scripted"]);
    expect(ids).toEqual(["load", "model:1", "match:call_a", "match:call_b", "exec:call_a", "exec:call_b", "model:5", "reply", "memory:1", "put-memory"]);
    expect(seen.claims).toEqual(["call_a", "call_b"]);
    expect(seen.replies).toEqual([{ thread: "t1", text: "done: alpha, beta", id: "run-1:reply" }]);
    expect(seen.markers).toEqual([]);
    const turn = seen.memory[0] ?? "";
    expect(turn).toStartWith('memory: operator: hello\ntool shell {"argv":["echo","alpha"]}\ntool shell {"argv":["echo","beta"]}\n-> alpha');
    expect(turn).toContain("-> beta");
    expect(turn).toEndWith("assistant: done: alpha, beta");
  });

  test("a working reply continues the run and only the done text is delivered", async () => {
    const { client, seen } = fakeClient();
    const inputs: Input[] = [];
    const ids: string[] = [];
    const main: Respond = (_input, n) => (n === 1 ? reply("working", "checking") : reply("done", "all clear"));
    const result = await runTurn(ctx(client, { main, inputs, ids }), message);
    expect(result).toEqual({ status: "replied", reply: "all clear" });
    expect(ids).toEqual(["load", "main:1", "main:2", "reply", "memory:1", "put-memory"]);
    expect(seen.replies).toEqual([{ thread: "t1", text: "all clear", id: "run-1:reply" }]);
    expect(textIn(inputs[1] ?? "")).toEndWith(ContinuePrompt);
    expect(seen.memory[0]).toBe("memory: operator: hello\nassistant: checking\nassistant: all clear");
    expect(seen.markers).toEqual([]);
  });

  test("still working after the last continue posts the text with an incomplete marker", async () => {
    const { client, seen } = fakeClient();
    const inputs: Input[] = [];
    const result = await runTurn(ctx(client, { main: () => reply("working", "still going"), inputs }), message);
    expect(result).toEqual({ status: "replied", reply: "still going" });
    expect(inputs).toHaveLength(1 + MaxContinues + 1);
    expect(seen.replies.map((r) => r.text)).toEqual(["still going"]);
    expect(seen.markers.map((m) => [m.kind, m.ref, m.detail])).toEqual([["turn.incomplete", "run-1", `{"continues":${MaxContinues}}`]]);
  });

  test("an empty reply writes a marker instead of a message", async () => {
    const { client, seen } = fakeClient();
    const result = await runTurn(ctx(client, { main: () => reply("done", "  ") }), message);
    expect(result).toEqual({ status: "empty", marker: "run-1:turn.empty" });
    expect(seen.replies).toEqual([]);
    expect(seen.markers.map((m) => [m.kind, m.ref])).toEqual([["turn.empty", "run-1"]]);
  });

  test("a memory failure leaves a marker and keeps the reply", async () => {
    const { client, seen } = fakeClient();
    const memory: Respond = () => {
      throw new Error("provider down");
    };
    const result = await runTurn(ctx(client, { main: () => reply("done", "ok"), memory }), message);
    expect(result).toEqual({ status: "replied", reply: "ok" });
    expect(seen.memory).toEqual([]);
    expect(seen.markers.map((m) => [m.kind, m.ref, m.detail])).toEqual([["memory.failed", "run-1", '{"error":"provider down"}']]);
  });

  test("exhausting max turns replies once and fails the run for good", async () => {
    const { client, seen } = fakeClient({ allowed: true });
    const main: Respond = (_input, n) => shellCall(`call_${n}`, ["echo", `${n}`]);
    await expect(runTurn(ctx(client, { main, maxTurns: 2 }), message)).rejects.toBeInstanceOf(NonRetriableError);
    expect(seen.replies).toEqual([{ thread: "t1", text: "stopped after 2 model calls without finishing", id: "run-1:reply" }]);
    expect(seen.memory).toEqual([]);
  });

  test("denied calls pause with the run state and a marker", async () => {
    const { result, seen } = await paused();
    expect(result).toEqual({ status: "paused", group: "g:run-1", approvals: ["a:call_a", "a:call_b"] });
    expect(seen.approvals[0]?.calls.map((c) => [c.id, c.tool, c.args, c.context.cwd])).toEqual([
      ["call_a", "shell", { argv: ["echo", "alpha"] }, dir],
      ["call_b", "shell", { argv: ["echo", "beta"] }, dir],
    ]);
    expect(seen.approvals[0]?.run).toBe("run-1");
    expect(seen.approvals[0]?.state.length).toBeGreaterThan(0);
    expect(seen.markers.map((m) => [m.kind, m.ref])).toEqual([["turn.paused", "g:run-1"]]);
    expect(seen.replies).toEqual([]);
  });

  test("a decided group applies every decision in one run", async () => {
    const first = await paused();
    const { client, seen } = fakeClient({ group: decidedGroup(first.seen, ["call_a", "approved"], ["call_b", "denied"]) });
    const result = await runTurn(ctx(client, { runId: "run-2" }), answered);
    expect(result).toEqual({ status: "replied", reply: `done: alpha, ${DeniedMessage}` });
    expect(seen.claims).toEqual(["call_a"]);
    expect(seen.approvals).toEqual([]);
  });

  test("a second pause from the restored state opens a new group", async () => {
    const main: Respond = (input) => {
      const results = resultsIn(input);
      if (results.length === 0) return shellCall("call_a", ["echo", "alpha"]);
      if (results.length === 1) return shellCall("call_c", ["echo", "gamma"]);
      return reply("done", results.join(" "));
    };
    const first = await paused({ main });
    const { client, seen } = fakeClient({ group: decidedGroup(first.seen, ["call_a", "approved"]) });
    const result = await runTurn(ctx(client, { runId: "run-2", main }), answered);
    expect(result).toEqual({ status: "paused", group: "g:run-2", approvals: ["a:call_c"] });
    expect(seen.claims).toEqual(["call_a"]);
    expect(seen.approvals[0]?.run).toBe("run-2");
    expect(seen.approvals[0]?.state).not.toBe(first.seen.approvals[0]?.state);
  });

  test("a denied call tells the model why and the turn goes on", async () => {
    const main: Respond = (input) => {
      const [denied] = resultsIn(input);
      return denied === undefined ? shellCall("call_a", ["echo", "alpha"]) : reply("done", denied);
    };
    const first = await paused({ main });
    const { client, seen } = fakeClient({ group: decidedGroup(first.seen, ["call_a", "denied"]) });
    const result = await runTurn(ctx(client, { runId: "run-2", main }), answered);
    expect(result).toEqual({ status: "replied", reply: DeniedMessage });
    expect(seen.claims).toEqual([]);
  });

  test("a group still pending leaves a marker and is ignored", async () => {
    const first = await paused();
    const { client, seen } = fakeClient({ group: { ...decidedGroup(first.seen), status: "pending" } });
    const result = await runTurn(ctx(client, { runId: "run-3" }), answered);
    expect(result).toEqual({ status: "ignored", marker: "run-3:approval.ignored" });
    expect(seen.markers.map((m) => [m.kind, m.ref])).toEqual([["approval.ignored", "g:run-1"]]);
    expect(seen.claims).toEqual([]);
  });

  test("decisions for calls not in the state leave a marker and are ignored", async () => {
    const first = await paused();
    const { client, seen } = fakeClient({ group: decidedGroup(first.seen, ["call_z", "approved"]) });
    const result = await runTurn(ctx(client, { runId: "run-3" }), answered);
    expect(result).toEqual({ status: "ignored", marker: "run-3:approval.ignored" });
    expect(seen.markers.map((m) => m.kind)).toEqual(["approval.ignored"]);
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

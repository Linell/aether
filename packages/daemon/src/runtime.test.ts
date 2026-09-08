import { expect, test } from "bun:test";
import { Inngest, NonRetriableError } from "inngest";
import { Events } from "./contract";
import { Usage, type ModelRequest } from "@openai/agents";
import { appId, buildFunctions, defineDaemon, maxTurnsFrom, modelResolver, threadOf, turnConfig, turnEventOf } from "./runtime";
import { createClient, type Daemon } from "./client";
import type { Responder } from "./model";
import type { StepLike } from "./turn";

const step: StepLike = { run: (_id, fn) => fn() };
const doc: Daemon = { name: "foo", host: "h1", class: "worker", offline_policy: "skip", status: "online", model: "scripted" };
const request: ModelRequest = { input: "hi", modelSettings: {}, tools: [], outputType: "text", handoffs: [], tracing: false };

test("the daemon doc's model wins over AETHER_MODEL", async () => {
  const resolve = modelResolver(defineDaemon({ name: "foo" }), step, { AETHER_MODEL: "openai:gpt-5.4" });
  const res = await resolve(doc).getResponse(request);
  expect(res.output.map((o) => o.type)).toEqual(["function_call", "function_call"]);
});

test("defineDaemon({ model }) wins over the daemon doc and runs inside a step", async () => {
  const ids: string[] = [];
  const recording: StepLike = { run: (id, fn) => (ids.push(id), fn()) };
  const model: Responder = { getResponse: async () => ({ output: [], usage: new Usage() }) };
  const resolve = modelResolver(defineDaemon({ name: "foo", model }), recording, {});
  const res = await resolve({ ...doc, model: "anthropic:claude-sonnet-5" }, "memory").getResponse(request);
  expect(res.output).toEqual([]);
  expect(ids).toEqual(["memory:0"]);
});

test("turn filters all triggers on the daemon, serializes per thread, and retries five times", () => {
  const config = turnConfig("foo");
  expect(config.triggers.map((t) => t.event)).toEqual([Events.MessageSent, Events.ScheduleFired, Events.ApprovalAnswered]);
  for (const t of config.triggers) expect(t.if).toBe('event.data.daemon == "foo"');
  expect(config.concurrency).toEqual({ key: "event.data.thread", limit: 1 });
  expect(config.retries).toBe(5);
});

test("daemon registers one turn function on its own app", () => {
  const inngest = new Inngest({ id: appId("foo") });
  const client = createClient({ baseUrl: "http://example.test", token: "t" });
  const fns = buildFunctions(inngest, defineDaemon({ name: "foo" }), client);
  expect(inngest.id).toBe("daemon-foo");
  expect(fns.map((f) => f.id())).toEqual(["turn"]);
});

test("maxTurns comes from AETHER_MAX_TURNS and defaults to 50", () => {
  expect(maxTurnsFrom({})).toBe(50);
  expect(maxTurnsFrom({ AETHER_MAX_TURNS: "7" })).toBe(7);
  expect(() => maxTurnsFrom({ AETHER_MAX_TURNS: "zero" })).toThrow("AETHER_MAX_TURNS");
});

test("turnEventOf rejects payloads outside the contract", () => {
  const data = { daemon: "foo", thread: "t1", message: "m1", text: "hi" };
  expect(turnEventOf({ name: Events.MessageSent, data })).toEqual({ name: Events.MessageSent, data });
  expect(() => turnEventOf({ name: Events.MessageSent, data: { daemon: "foo" } })).toThrow(NonRetriableError);
  expect(threadOf({ data: { daemon: "foo", thread: "t1" } })).toBe("t1");
  expect(threadOf({ data: { daemon: "foo" } })).toBe("");
});

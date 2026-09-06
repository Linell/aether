import { expect, test } from "bun:test";
import { Inngest, NonRetriableError } from "inngest";
import { Events } from "./contract";
import { appId, buildFunctions, defineDaemon, maxTurnsFrom, turnConfig, turnEventOf } from "./runtime";
import { createClient } from "./client";

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
});

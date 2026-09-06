import { expect, test } from "bun:test";
import { Inngest } from "inngest";
import { Events } from "./contract";
import { appId, buildFunctions, defineDaemon, turnConfig } from "./runtime";
import { createClient } from "./client";

test("turn filters both triggers on the daemon and serializes per thread", () => {
  const config = turnConfig("foo");
  expect(config.triggers.map((t) => t.event)).toEqual([Events.MessageSent, Events.ScheduleFired]);
  for (const t of config.triggers) expect(t.if).toBe('event.data.daemon == "foo"');
  expect(config.concurrency).toEqual({ key: "event.data.thread", limit: 1 });
});

test("daemon registers one turn function on its own app", () => {
  const inngest = new Inngest({ id: appId("foo") });
  const client = createClient({ baseUrl: "http://example.test", token: "t" });
  const fns = buildFunctions(inngest, defineDaemon({ name: "foo" }), client);
  expect(inngest.id).toBe("daemon-foo");
  expect(fns.map((f) => f.id())).toEqual(["turn"]);
});

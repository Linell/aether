import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  Events,
  type EventName,
  type MessageSentPayload,
  type ScheduleFiredPayload,
  type ApprovalAnsweredPayload,
  type MessageRepliedPayload,
  type ApprovalRequestedPayload,
  type ConjureRequestedPayload,
  type DeployRequestedPayload,
  type DismissRequestedPayload,
  type ChangeRequestedPayload,
} from "./contract";

interface EventsJson {
  events: Record<string, { from: string; to: string; fields: string[] }>;
}

const contractPath = join(import.meta.dir, "../../../contract/events.json");
const contractJson = JSON.parse(readFileSync(contractPath, "utf8")) as EventsJson;

test("Events values equal contract/events.json event names", () => {
  const fromContract = new Set<string>(Object.keys(contractJson.events));
  const fromTs = new Set<string>(Object.values(Events));
  expect(fromTs).toEqual(fromContract);
});
const samples: Record<EventName, Record<string, unknown>> = {
  [Events.MessageSent]: {
    daemon: "foo",
    thread: "t1",
    message: "m1",
    text: "hello",
  } satisfies MessageSentPayload,
  [Events.ScheduleFired]: {
    daemon: "foo",
    thread: "t1",
    schedule: "s1",
    due_at: "2026-09-05T00:00:00Z",
    deadline_at: "2026-09-05T00:05:00Z",
    prompt: "Review the day",
  } satisfies ScheduleFiredPayload,
  [Events.ApprovalAnswered]: {
    daemon: "foo",
    thread: "t1",
    call: "c1",
    approval: "a1",
    decision: "approved",
  } satisfies ApprovalAnsweredPayload,
  [Events.MessageReplied]: {
    daemon: "foo",
    thread: "t1",
    reply: "r1",
    text: "hello back",
  } satisfies MessageRepliedPayload,
  [Events.ApprovalRequested]: {
    daemon: "foo",
    thread: "t1",
    calls: [
      { id: "c1", tool: "shell", args: {}, context: { cwd: "/tmp", host: "h1" } },
    ],
  } satisfies ApprovalRequestedPayload,
  [Events.ConjureRequested]: {
    daemon: "foo",
    host: "h1",
    language: "ts",
  } satisfies ConjureRequestedPayload,
  [Events.DeployRequested]: {
    daemon: "foo",
    host: "h1",
  } satisfies DeployRequestedPayload,
  [Events.DismissRequested]: {
    daemon: "foo",
    host: "h1",
  } satisfies DismissRequestedPayload,
  [Events.ChangeRequested]: {
    daemon: "foo",
    patch: "diff",
    tests: ["test1"],
  } satisfies ChangeRequestedPayload,
};

describe("payload fields match contract/events.json fields", () => {
  for (const [name, def] of Object.entries(contractJson.events)) {
    test(name, () => {
      const sample = samples[name as EventName];
      expect(sample).toBeDefined();
      expect(new Set(Object.keys(sample!))).toEqual(new Set(def.fields));
    });
  }
});

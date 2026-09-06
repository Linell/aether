import { describe, expect, test } from "bun:test";
import { AetherHttpError, type AetherClient, type Marker } from "./client";
import { Events } from "./contract";
import { templateModel } from "./model";
import { isStale, runTurn, type TurnContext } from "./turn";

function fakeClient(memoryVersion = 0) {
  const calls: { replies: { thread: string; text: string; id?: string }[]; memory: string[]; markers: Marker[] } = {
    replies: [],
    memory: [],
    markers: [],
  };
  const client: Partial<AetherClient> = {
    async reply(thread, text, id) {
      calls.replies.push({ thread, text, ...(id === undefined ? {} : { id }) });
    },
    async putMarker(_daemon, marker) {
      calls.markers.push(marker);
    },
    async getSoul() {
      throw new AetherHttpError(404, "not found");
    },
    async getMemory() {
      return { content: "", version: memoryVersion };
    },
    async putMemory(_daemon, body, version) {
      calls.memory.push(body);
      return { content: body, version: version + 1 };
    },
  };
  return { client: client as AetherClient, calls };
}

function ctx(client: AetherClient, now: Date): TurnContext {
  return { daemon: "foo", runId: "run-1", client, model: templateModel, step: { run: (_id, fn) => fn() }, now: () => now };
}

const now = new Date("2026-09-06T07:00:00Z");

describe("runTurn", () => {
  test("message turn replies with a stable id and writes memory", async () => {
    const { client, calls } = fakeClient();
    const result = await runTurn(ctx(client, now), {
      name: Events.MessageSent,
      data: { daemon: "foo", thread: "t1", message: "m1", text: "hello" },
    });
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
});

test("isStale compares deadline to now", () => {
  expect(isStale("2026-09-06T06:59:59Z", now)).toBe(true);
  expect(isStale("2026-09-06T07:00:00Z", now)).toBe(false);
});

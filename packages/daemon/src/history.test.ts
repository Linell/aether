import { expect, test } from "bun:test";
import type { Message } from "./client";
import { historyItems, parseReply, sinceLastUser, transcript, ToolOutputClip } from "./history";

const at = "2026-09-06T07:00:00Z";
const msg = (id: string, role: Message["role"], text: string): Message => ({ id, role, text, created_at: at });

test("history keeps the newest messages within the byte budget, oldest first", () => {
  const items = historyItems([msg("m3", "assistant", "ccc"), msg("m1", "user", "aaaa")], 5);
  expect(items).toEqual([{ role: "assistant", status: "completed", content: [{ type: "output_text", text: "ccc" }] }]);
});

test("this turn starts at the last user message", () => {
  const items = historyItems([msg("m2", "user", "now"), msg("m1", "user", "before")], 100);
  const tail = [{ role: "assistant" as const, status: "completed" as const, content: [{ type: "output_text" as const, text: "ok" }] }];
  expect(sinceLastUser([...items, ...tail])).toEqual([{ role: "user", content: "now" }, ...tail]);
});

test("sinceLastUser can skip user messages that are not the request", () => {
  const items = historyItems([msg("m2", "user", "continue"), msg("m1", "user", "request")], 100);
  expect(sinceLastUser(items, (t) => t !== "continue")).toEqual([{ role: "user", content: "request" }, { role: "user", content: "continue" }]);
});

test("parseReply reads the structured reply and falls back to plain text", () => {
  expect(parseReply('{"status":"working","text":"looking"}')).toEqual({ status: "working", text: "looking" });
  expect(parseReply({ status: "done", text: "ok" })).toEqual({ status: "done", text: "ok" });
  expect(parseReply("just prose")).toEqual({ status: "done", text: "just prose" });
  expect(parseReply(undefined)).toEqual({ status: "done", text: "" });
});

test("transcript renders the turn as plain text with clipped tool output", () => {
  const long = "x".repeat(ToolOutputClip + 10);
  const items = [
    { role: "user" as const, content: "list files" },
    { type: "function_call" as const, callId: "c1", name: "shell", status: "completed" as const, arguments: '{"argv":["ls"]}' },
    { type: "function_call_result" as const, callId: "c1", name: "shell", status: "completed" as const, output: { type: "text" as const, text: long } },
    { role: "assistant" as const, status: "completed" as const, content: [{ type: "output_text" as const, text: '{"status":"done","text":"two files"}' }] },
  ];
  expect(transcript(items)).toBe(`operator: list files\ntool shell {"argv":["ls"]}\n-> ${"x".repeat(ToolOutputClip)}…\nassistant: two files`);
});

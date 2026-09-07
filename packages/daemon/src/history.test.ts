import { expect, test } from "bun:test";
import type { Message } from "./client";
import { historyItems, sinceLastUser } from "./history";

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

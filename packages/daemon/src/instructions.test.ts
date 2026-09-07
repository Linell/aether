import { expect, test } from "bun:test";
import { DocLimit, instructionsFor, localTime } from "./instructions";

const now = new Date("2026-09-06T14:05:00Z");
const thread = { id: "t1", daemon: "sage", host: "laptop", directory: "/srv/sage" };

test("absent soul and memory leave no header; present ones are clipped", () => {
  const bare = instructionsFor({ daemon: "sage", soul: "", memory: "  ", tools: ["shell"], now, thread });
  expect(bare).toContain("You are sage");
  expect(bare).toContain("shell");
  expect(bare).toContain(`Now: ${localTime(now)}. Thread directory: /srv/sage on host laptop.`);
  expect(bare).not.toContain("---");

  const full = instructionsFor({ daemon: "sage", soul: "Be kind.", memory: "x".repeat(DocLimit + 5), tools: [], now, thread });
  expect(full).toContain("--- who you are ---\nBe kind.");
  expect(full).toContain("--- your memory ---\n");
  expect(full.length).toBeLessThan(bare.length + DocLimit + 200);
});

test("localTime renders in the given zone", () => {
  expect(localTime(now, "America/Chicago")).toBe("Sunday, September 6, 2026 at 09:05 (America/Chicago)");
});

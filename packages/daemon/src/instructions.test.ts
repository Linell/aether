import { expect, test } from "bun:test";
import { DocLimit, instructionsFor } from "./instructions";

test("absent soul and memory leave no header; present ones are clipped", () => {
  const bare = instructionsFor({ daemon: "sage", soul: "", memory: "  ", tools: ["shell"] });
  expect(bare).toContain("You are sage");
  expect(bare).toContain("shell");
  expect(bare).not.toContain("---");

  const full = instructionsFor({ daemon: "sage", soul: "Be kind.", memory: "x".repeat(DocLimit + 5), tools: [] });
  expect(full).toContain("--- who you are ---\nBe kind.");
  expect(full).toContain("--- your memory ---\n");
  expect(full.length).toBeLessThan(bare.length + DocLimit + 200);
});

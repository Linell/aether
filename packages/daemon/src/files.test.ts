import { expect, test } from "bun:test";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { RunContext, type Tool } from "@openai/agents";
import type { AetherClient } from "./client";
import { editFile, readFile, writeFile } from "./files";
import type { ToolDeps } from "./tools";

type Fn = Extract<Tool, { type: "function" }>;

function setup() {
  const cwd = mkdtempSync(join(tmpdir(), "aether-files-"));
  const client = { claimOperation: async () => true } as unknown as AetherClient;
  const deps: ToolDeps = { step: { run: (_id, fn) => fn() }, client, daemon: "sage", thread: "t1", cwd, host: "laptop" };
  const fn = (t: Tool): Fn => t as Fn;
  return { cwd, read: fn(readFile(deps)), edit: fn(editFile(deps)), write: fn(writeFile(deps)) };
}

function invoke(t: Fn, args: Record<string, unknown>) {
  const input = JSON.stringify(args);
  return t.invoke(new RunContext(), input, { toolCall: { type: "function_call", callId: "c1", name: t.name, arguments: input } });
}

test("read_file pages by line and reports the range", async () => {
  const { cwd, read } = setup();
  writeFileSync(join(cwd, "a.txt"), "one\ntwo\nthree\nfour");
  expect(await invoke(read, { path: "a.txt", offset: 2, limit: 2 })).toBe("two\nthree\n[lines 2-3 of 4]");
  writeFileSync(join(cwd, "b.txt"), "one\n");
  expect(await invoke(read, { path: "b.txt" })).toBe("one\n[lines 1-1 of 1]");
  writeFileSync(join(cwd, "empty"), "");
  expect(await invoke(read, { path: "empty" })).toBe("\n[lines 1-0 of 0]");
  expect(await invoke(read, { path: "../etc/passwd" })).toContain("outside root");
});

test("edit_file replaces exactly one occurrence", async () => {
  const { cwd, edit } = setup();
  writeFileSync(join(cwd, "a.txt"), "x = 1\ny = 1\n");
  expect(await invoke(edit, { path: "a.txt", old: "= 1", new: "= 2" })).toContain("matches 2 times");
  expect(await invoke(edit, { path: "a.txt", old: "z", new: "q" })).toContain("not found");
  expect(await invoke(edit, { path: "a.txt", old: "x = 1", new: "x = 2" })).toBe("replaced 1 occurrence in a.txt");
  expect(readFileSync(join(cwd, "a.txt"), "utf8")).toBe("x = 2\ny = 1\n");
  expect(readdirSync(cwd)).toEqual(["a.txt"]);
});

test("write_file creates parents and refuses symlinks out of root", async () => {
  const { cwd, write } = setup();
  expect(await invoke(write, { path: "sub/dir/b.txt", content: "hi" })).toBe("wrote 2 bytes to sub/dir/b.txt");
  expect(readFileSync(join(cwd, "sub/dir/b.txt"), "utf8")).toBe("hi");
  const outside = mkdtempSync(join(tmpdir(), "aether-outside-"));
  mkdirSync(join(cwd, "links"));
  writeFileSync(join(outside, "target"), "");
  symlinkSync(join(outside, "target"), join(cwd, "links/out"));
  expect(await invoke(write, { path: "links/out", content: "x" })).toContain("outside root");
});

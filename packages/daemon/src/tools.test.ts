import { expect, test } from "bun:test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { RunContext, type Tool } from "@openai/agents";
import type { AetherClient } from "./client";
import type { ToolCall } from "./contract";
import { shell, truncate, type ToolDeps } from "./tools";

interface Fake {
  claimed: boolean;
  allowed: boolean;
  matched: ToolCall[];
  steps: string[];
}

function fakeClient(fake: Fake): AetherClient {
  const unused = () => Promise.reject(new Error("unused"));
  return {
    reply: unused,
    putMarker: unused,
    requestApproval: unused,
    getThread: unused,
    listMessages: unused,
    getDaemon: unused,
    getApproval: unused,
    getApprovalGroup: unused,
    async matchCall(_daemon, _thread, call) {
      fake.matched.push(call);
      return { allowed: fake.allowed };
    },
    claimOperation: async () => fake.claimed,
    getSoul: unused,
    getMemory: unused,
    putMemory: unused,
    listSchedules: unused,
    putSchedule: unused,
    deleteSchedule: unused,
  };
}

function setup(fake: Fake) {
  const cwd = mkdtempSync(join(tmpdir(), "aether-shell-"));
  const deps: ToolDeps = {
    step: {
      run: (id, fn) => {
        fake.steps.push(id);
        return fn();
      },
    },
    client: fakeClient(fake),
    daemon: "sage",
    thread: "t1",
    cwd,
    host: "laptop",
  };
  return { cwd, tool: functionTool(shell(deps)) };
}

type Fn = Extract<Tool, { type: "function" }>;

function functionTool(t: Tool): Fn {
  if (t.type !== "function") throw new Error(`expected function tool, got ${t.type}`);
  return t;
}

function invoke(t: Fn, callId: string, args: Record<string, unknown>) {
  const input = JSON.stringify(args);
  return t.invoke(new RunContext(), input, { toolCall: { type: "function_call", callId, name: "shell", arguments: input } });
}

test("execute runs claimed calls in the thread directory with a scrubbed env", async () => {
  const fake: Fake = { claimed: true, allowed: true, matched: [], steps: [] };
  const { cwd, tool } = setup(fake);
  process.env.AETHER_TOKEN_TEST_LEAK = "leak";
  const out = await invoke(tool, "c1", { argv: ["sh", "-c", "pwd; echo token=$AETHER_TOKEN_TEST_LEAK"] });
  expect(out).toContain("token=\n");
  expect(String(out).split("\n")[0]?.endsWith(cwd.split("/").pop() ?? "")).toBe(true);
  expect(await invoke(tool, "c2", { argv: ["sh", "-c", "exit 3"] })).toBe("exit 3\n");
  expect(await invoke(tool, "c3", { argv: ["ls"], cwd: "../.." })).toContain("outside root");
  expect(fake.steps).toEqual(["exec:c1", "exec:c2", "exec:c3"]);
});

test("execute reports unclaimed calls without running them", async () => {
  const fake: Fake = { claimed: false, allowed: true, matched: [], steps: [] };
  const { tool } = setup(fake);
  expect(await invoke(tool, "c1", { argv: ["sh", "-c", "exit 1"] })).toBe("already executed; output unavailable");
});

test("needsApproval asks aether with the contract call shape", async () => {
  const fake: Fake = { claimed: true, allowed: false, matched: [], steps: [] };
  const { cwd, tool } = setup(fake);
  const args = { argv: ["ls"] };
  expect(await tool.needsApproval(new RunContext(), args, "c9")).toBe(true);
  expect(fake.matched).toEqual([{ id: "c9", tool: "shell", args, context: { cwd, host: "laptop" } }]);
  expect(fake.steps).toEqual(["match:c9"]);
});

test("truncate caps long output", () => {
  expect(truncate("abcdef", 3)).toBe("abc\n[truncated]");
});

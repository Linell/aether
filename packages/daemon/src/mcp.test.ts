import { expect, test } from "bun:test";
import { RunContext, type MCPServer, type Tool } from "@openai/agents";
import type { AetherClient } from "./client";
import type { ToolCall } from "./contract";
import { mcpTools } from "./mcp";
import type { ToolDeps } from "./tools";

interface Fake {
  claimed: boolean;
  allowed: boolean;
  matched: ToolCall[];
  steps: string[];
  connects: number;
  calls: unknown[];
}

function fakeServer(fake: Fake): MCPServer {
  return {
    name: "fake",
    cacheToolsList: true,
    connect: async () => {
      fake.connects++;
    },
    close: async () => {},
    invalidateToolsCache: async () => {},
    listTools: async () => [
      {
        name: "echo",
        description: "Echo text.",
        inputSchema: { type: "object", properties: { text: { type: "string" } }, required: ["text"], additionalProperties: false },
      },
    ],
    callTool: async (name, args) => {
      fake.calls.push([name, args]);
      return [{ type: "text", text: `echo:${String(args?.text)}` }];
    },
  };
}

function depsFor(fake: Fake): ToolDeps {
  const client = {
    async matchCall(_daemon: string, _thread: string, call: ToolCall) {
      fake.matched.push(call);
      return { allowed: fake.allowed };
    },
    claimOperation: async () => fake.claimed,
  } as unknown as AetherClient;
  return {
    step: {
      run: (id, fn) => {
        fake.steps.push(id);
        return fn();
      },
    },
    client,
    daemon: "cleo",
    thread: "t1",
    cwd: "/tmp",
    host: "laptop",
  };
}

type Fn = Extract<Tool, { type: "function" }>;

function only(tools: Tool[]): Fn {
  const t = tools[0];
  if (tools.length !== 1 || t?.type !== "function") throw new Error("expected one function tool");
  return t;
}

test("mcp tools go through match, claim, and step plumbing", async () => {
  const fake: Fake = { claimed: true, allowed: false, matched: [], steps: [], connects: 0, calls: [] };
  const factory = mcpTools(fakeServer(fake));
  const deps = depsFor(fake);
  const tool = only(await factory(deps));
  only(await factory(deps));
  expect(fake.connects).toBe(1);
  expect(tool.name).toBe("echo");

  const input = JSON.stringify({ text: "hi" });
  const details = { toolCall: { type: "function_call" as const, callId: "c1", name: "echo", arguments: input } };
  expect(await tool.needsApproval(new RunContext(), { text: "hi" }, "c1")).toBe(true);
  expect(fake.matched[0]).toEqual({ id: "c1", tool: "echo", args: { text: "hi" }, context: { cwd: "/tmp", host: "laptop" } });
  expect(await tool.invoke(new RunContext(), input, details)).toBe("echo:hi");
  expect(fake.calls).toEqual([["echo", { text: "hi" }]]);
  expect(fake.steps).toEqual(["match:c1", "exec:c1"]);

  fake.claimed = false;
  expect(await tool.invoke(new RunContext(), input, { ...details, toolCall: { ...details.toolCall, callId: "c2" } })).toBe("already executed; output unavailable");
  expect(fake.calls).toHaveLength(1);
});

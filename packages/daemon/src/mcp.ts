import { getAllMcpTools, MCPServerStdio, MCPServerStreamableHttp, type MCPServer, type RunContext, type Tool } from "@openai/agents";
import { scrubEnv } from "./policy";
import { callIdFrom, execute, needsApproval, required, truncate, type ToolDeps } from "./tools";

export type McpSource = { name: string; command: string; args?: string[] } | { name: string; url: string };

export const McpOutputLimit = 32000;

export type McpTools = (deps: ToolDeps) => Promise<Tool[]>;

export function mcp(source: McpSource): McpTools {
  return mcpTools(serverFor(source));
}

export function mcpTools(server: MCPServer): McpTools {
  let listed: Promise<Tool[]> | undefined;
  return async (deps) => {
    listed ??= list(server).catch((err) => {
      listed = undefined;
      throw err;
    });
    return (await listed).map((t) => guard(deps, functionTool(t)));
  };
}

function serverFor(source: McpSource): MCPServer {
  if ("url" in source) return new MCPServerStreamableHttp({ name: source.name, url: source.url, cacheToolsList: true });
  return new MCPServerStdio({ name: source.name, command: source.command, args: source.args ?? [], env: scrubEnv(process.env), cacheToolsList: true });
}

async function list(server: MCPServer): Promise<Tool[]> {
  await server.connect();
  return getAllMcpTools([server]);
}

type Fn = Extract<Tool, { type: "function" }>;

function functionTool(t: Tool): Fn {
  if (t.type !== "function") throw new Error(`mcp: expected function tool, got ${t.type}`);
  return t;
}

function guard(deps: ToolDeps, t: Fn): Tool {
  return {
    ...t,
    needsApproval: (_ctx: RunContext, args: unknown, callId?: string) => needsApproval(deps, t.name, args as Record<string, unknown>, required(callId)),
    invoke: (ctx, input, details) => execute(deps, callIdFrom(details), async () => truncate(textOf(await t.invoke(ctx, input, details)), McpOutputLimit)),
  };
}

function textOf(output: unknown): string {
  if (typeof output === "string") return output;
  const items = Array.isArray(output) ? output : [output];
  return items.map((item) => (isText(item) ? item.text : JSON.stringify(item))).join("\n");
}

function isText(item: unknown): item is { text: string } {
  return typeof item === "object" && item !== null && "type" in item && item.type === "text" && "text" in item && typeof item.text === "string";
}

import { tool, type Tool } from "@openai/agents";
import { z } from "zod";
import type { AetherClient } from "./client";
import type { ToolCall } from "./contract";
import { resolveWithin, scrubEnv } from "./policy";
import type { StepLike } from "./turn";

export interface ToolDeps {
  step: StepLike;
  client: AetherClient;
  daemon: string;
  thread: string;
  cwd: string;
  host: string;
}

export type ToolFactory = (deps: ToolDeps) => Tool;

export type ToolSource = ToolFactory | ((deps: ToolDeps) => Promise<Tool[]>);

export async function toolsFor(sources: ToolSource[], deps: ToolDeps): Promise<Tool[]> {
  const made = await Promise.all(sources.map((make) => make(deps)));
  return made.flat();
}

export const OutputLimit = 4000;

const ShellArgs = z.object({
  argv: z.array(z.string()).min(1),
  cwd: z.string().optional(),
});

type ShellArgs = z.infer<typeof ShellArgs>;

export const shell: ToolFactory = (deps) =>
  tool({
    name: "shell",
    description:
      "Run a program by argv (no shell expansion) inside the thread directory, optionally in a subdirectory `cwd`. Output is truncated at 4000 chars; a non-zero exit is reported as `exit N`.",
    parameters: ShellArgs,
    needsApproval: (_ctx, args, callId) => needsApproval(deps, "shell", args, required(callId)),
    execute: (args, _ctx, details) => execute(deps, callIdFrom(details), () => runShell(deps.cwd, args)),
  });

export function needsApproval(deps: ToolDeps, tool: string, args: Record<string, unknown>, callId: string): Promise<boolean> {
  return deps.step.run(`match:${callId}`, async () => {
    const match = await deps.client.matchCall(deps.daemon, deps.thread, callFor(deps, callId, tool, args));
    return !match.allowed;
  });
}

function callFor(deps: ToolDeps, id: string, tool: string, args: Record<string, unknown>): ToolCall {
  return { id, tool, args, context: { cwd: deps.cwd, host: deps.host } };
}

export function execute(deps: ToolDeps, callId: string, fn: () => Promise<string>): Promise<string> {
  return deps.step.run(`exec:${callId}`, () => runClaimed(deps, callId, fn));
}

async function runClaimed(deps: ToolDeps, callId: string, fn: () => Promise<string>): Promise<string> {
  if (!(await deps.client.claimOperation(callId, "tool.call"))) return "already executed; output unavailable";
  try {
    return await fn();
  } catch (err) {
    return `error: ${message(err)}`;
  }
}

async function runShell(root: string, args: ShellArgs): Promise<string> {
  const cwd = resolveWithin(root, args.cwd ?? ".");
  const proc = Bun.spawn(args.argv, { cwd, env: scrubEnv(process.env), stdin: "ignore", stdout: "pipe", stderr: "pipe" });
  const [stdout, stderr, code] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  const output = truncate(stdout + stderr);
  return code === 0 ? output : `exit ${code}\n${output}`;
}

export function required(callId: string | undefined): string {
  if (!callId) throw new Error("tool call without callId");
  return callId;
}

export function callIdFrom(details: { toolCall?: { callId?: string } } | undefined): string {
  return required(details?.toolCall?.callId);
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function truncate(text: string, limit = OutputLimit): string {
  return text.length <= limit ? text : `${text.slice(0, limit)}\n[truncated]`;
}

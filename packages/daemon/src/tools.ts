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
    needsApproval: (_ctx, args, callId) => needsApproval(deps, args, required(callId)),
    execute: (args, _ctx, details) => execute(deps, args, required(details?.toolCall?.callId)),
  });

function needsApproval(deps: ToolDeps, args: ShellArgs, callId: string): Promise<boolean> {
  return deps.step.run(`match:${callId}`, async () => {
    const match = await deps.client.matchCall(deps.daemon, deps.thread, callFor(deps, callId, args));
    return !match.allowed;
  });
}

function callFor(deps: ToolDeps, id: string, args: ShellArgs): ToolCall {
  return { id, tool: "shell", args, context: { cwd: deps.cwd, host: deps.host } };
}

function execute(deps: ToolDeps, args: ShellArgs, callId: string): Promise<string> {
  return deps.step.run(`exec:${callId}`, () => runClaimed(deps, callId, args));
}

async function runClaimed(deps: ToolDeps, callId: string, args: ShellArgs): Promise<string> {
  if (!(await deps.client.claimOperation(callId, "tool.call"))) return "already executed; output unavailable";
  try {
    return await runShell(deps.cwd, args);
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

function required(callId: string | undefined): string {
  if (!callId) throw new Error("tool call without callId");
  return callId;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function truncate(text: string, limit = OutputLimit): string {
  return text.length <= limit ? text : `${text.slice(0, limit)}\n[truncated]`;
}

import type { ToolCall } from "./contract";

export type TurnKind = "message" | "schedule" | "approval";

export interface TurnInput {
  kind: TurnKind;
  daemon: string;
  thread: string;
  text: string;
  schedule?: string;
}

export interface ToolRequest {
  tool: string;
  args: Record<string, unknown>;
}

export type ToolStatus = "ok" | "error" | "denied" | "skipped";

export interface ToolResult {
  call: ToolCall;
  status: ToolStatus;
  output: string;
}

export interface ModelInput {
  soul: string;
  memory: string;
  input: TurnInput;
  now: Date;
  results?: ToolResult[];
}

export interface ModelOutput {
  reply: string;
  memory?: string;
  calls?: ToolRequest[];
}

export type Model = (input: ModelInput) => Promise<ModelOutput>;

export const templateModel: Model = async ({ memory, input, now, results }) => {
  const day = now.toISOString().slice(0, 10);
  if (results) return { reply: renderResults(results), memory: appendLine(memory, `${day}: ${noteResults(results)}`) };
  const argv = shellRequest(input);
  if (argv) return { reply: "", calls: [{ tool: "shell", args: { argv } }] };
  const note = input.kind === "schedule" ? `${day}: ran schedule ${input.schedule}` : `${day}: heard ${input.text}`;
  return { reply: render(input, memory), memory: appendLine(memory, note) };
};

function shellRequest(input: TurnInput): string[] | undefined {
  if (input.kind !== "message" || !input.text.startsWith("run ")) return undefined;
  const argv = input.text.slice(4).trim().split(/\s+/);
  return argv[0] ? argv : undefined;
}

function renderResults(results: ToolResult[]): string {
  return results.map((r) => `${describe(r.call)} → ${r.status}\n${r.output}`.trim()).join("\n\n");
}

function noteResults(results: ToolResult[]): string {
  return results.map((r) => `${describe(r.call)} ${r.status}`).join("; ");
}

export function describe(call: ToolCall): string {
  const argv = call.args.argv;
  return Array.isArray(argv) ? `${call.tool} ${argv.join(" ")}` : `${call.tool} ${JSON.stringify(call.args)}`;
}

function render(input: TurnInput, memory: string): string {
  if (input.kind === "schedule") {
    return `Morning review for ${input.daemon}.\n${summarize(memory)}`;
  }
  return `Noted: ${input.text}`;
}

function summarize(memory: string): string {
  const lines = memory.split("\n").filter((l) => l.length > 0);
  if (lines.length === 0) return "No memory yet.";
  return `Memory has ${lines.length} note(s). Latest: ${lines[lines.length - 1]}`;
}

function appendLine(memory: string, line: string): string {
  return memory.length === 0 ? line : `${memory}\n${line}`;
}

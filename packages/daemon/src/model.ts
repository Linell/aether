export type TurnKind = "message" | "schedule";

export interface TurnInput {
  kind: TurnKind;
  daemon: string;
  thread: string;
  text: string;
  schedule?: string;
}

export interface ModelInput {
  soul: string;
  memory: string;
  input: TurnInput;
  now: Date;
}

export interface ModelOutput {
  reply: string;
  memory?: string;
}

export type Model = (input: ModelInput) => Promise<ModelOutput>;

export const templateModel: Model = async ({ memory, input, now }) => {
  const day = now.toISOString().slice(0, 10);
  const note = input.kind === "schedule" ? `${day}: ran schedule ${input.schedule}` : `${day}: heard ${input.text}`;
  return { reply: render(input, memory), memory: appendLine(memory, note) };
};

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

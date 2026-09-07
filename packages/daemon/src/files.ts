import { closeSync, constants, fstatSync, mkdirSync, openSync, readFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { tool } from "@openai/agents";
import { z } from "zod";
import { relativeTo, resolveWithin } from "./policy";
import { callIdFrom, execute, needsApproval, required, truncate, type ToolDeps, type ToolFactory } from "./tools";

export const DefaultReadLines = 200;

const ReadArgs = z.object({
  path: z.string(),
  offset: z.number().int().min(1).optional(),
  limit: z.number().int().min(1).optional(),
});

const EditArgs = z.object({ path: z.string(), old: z.string().min(1), new: z.string() });

const WriteArgs = z.object({ path: z.string(), content: z.string() });

type Args = Record<string, unknown>;

function fileTool<T extends Args>(deps: ToolDeps, name: string, description: string, parameters: z.ZodType<T>, run: (args: T) => string) {
  return tool({
    name,
    description,
    parameters: parameters as never,
    needsApproval: (_ctx, args, callId) => needsApproval(deps, name, args as Args, required(callId)),
    execute: (args, _ctx, details) => execute(deps, callIdFrom(details), async () => run(args as T)),
  });
}

export const readFile: ToolFactory = (deps) =>
  fileTool(
    deps,
    "read_file",
    `Read a text file inside the thread directory by line range. \`offset\` is the 1-based first line, \`limit\` the line count (default ${DefaultReadLines}). Output ends with \`[lines a-b of n]\` for paging.`,
    ReadArgs,
    (args) => readLines(deps.cwd, args),
  );

export const editFile: ToolFactory = (deps) =>
  fileTool(
    deps,
    "edit_file",
    "Replace one exact occurrence of `old` with `new` in a file inside the thread directory. Fails when `old` is missing or ambiguous; include enough context to make it unique.",
    EditArgs,
    (args) => replaceOnce(deps.cwd, args),
  );

export const writeFile: ToolFactory = (deps) =>
  fileTool(
    deps,
    "write_file",
    "Create or replace a file inside the thread directory with `content`, creating parent directories as needed.",
    WriteArgs,
    (args) => writeWhole(deps.cwd, args),
  );

function readLines(root: string, args: z.infer<typeof ReadArgs>): string {
  const lines = splitLines(readRegular(resolveWithin(root, args.path)).text);
  const from = args.offset ?? 1;
  const to = Math.min(lines.length, from + (args.limit ?? DefaultReadLines) - 1);
  const body = truncate(lines.slice(from - 1, to).join("\n"));
  return `${body}\n[lines ${from}-${Math.max(to, from - 1)} of ${lines.length}]`;
}

function splitLines(text: string): string[] {
  if (text.length === 0) return [];
  return (text.endsWith("\n") ? text.slice(0, -1) : text).split("\n");
}

function replaceOnce(root: string, args: z.infer<typeof EditArgs>): string {
  const full = resolveWithin(root, args.path);
  const name = relativeTo(root, full);
  const { text, mode } = readRegular(full);
  const count = text.split(args.old).length - 1;
  if (count === 0) return `error: old text not found in ${name}`;
  if (count > 1) return `error: old text matches ${count} times in ${name}; add context`;
  writeAtomic(full, text.replace(args.old, () => args.new), mode);
  return `replaced 1 occurrence in ${name}`;
}

function writeWhole(root: string, args: z.infer<typeof WriteArgs>): string {
  const full = resolveWithin(root, args.path);
  const mode = existingMode(full) ?? 0o644;
  mkdirSync(dirname(full), { recursive: true });
  writeAtomic(full, args.content, mode);
  return `wrote ${Buffer.byteLength(args.content)} bytes to ${relativeTo(root, full)}`;
}

function existingMode(full: string): number | undefined {
  try {
    return readRegular(full).mode;
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    throw err;
  }
}

function readRegular(full: string): { text: string; mode: number } {
  const fd = openSync(full, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(fd);
    if (!stat.isFile()) throw new Error(`${full} is not a regular file`);
    return { text: readFileSync(fd, "utf8"), mode: stat.mode & 0o777 };
  } finally {
    closeSync(fd);
  }
}

function writeAtomic(full: string, content: string, mode: number): void {
  const tmp = join(dirname(full), `.${basename(full)}.${crypto.randomUUID()}.tmp`);
  try {
    writeFileSync(tmp, content, { mode });
    renameSync(tmp, full);
  } catch (err) {
    unlinkQuiet(tmp);
    throw err;
  }
}

function unlinkQuiet(p: string): void {
  try {
    unlinkSync(p);
  } catch {}
}

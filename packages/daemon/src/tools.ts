import { resolveWithin, scrubEnv } from "./policy";

export interface ToolContext {
  cwd: string;
}

export interface Tool {
  name: string;
  run(args: Record<string, unknown>, ctx: ToolContext): Promise<string>;
}

export const OutputLimit = 4000;

export const shell: Tool = {
  name: "shell",
  async run(args, ctx) {
    const argv = argvOf(args);
    const cwd = resolveWithin(ctx.cwd, typeof args.cwd === "string" ? args.cwd : ".");
    const proc = Bun.spawn(argv, { cwd, env: scrubEnv(process.env), stdin: "ignore", stdout: "pipe", stderr: "pipe" });
    const [stdout, stderr, code] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
      proc.exited,
    ]);
    const output = truncate(stdout + stderr);
    if (code !== 0) throw new Error(`exit ${code}\n${output}`);
    return output;
  },
};

function argvOf(args: Record<string, unknown>): string[] {
  const argv = args.argv;
  if (!Array.isArray(argv) || argv.length === 0 || !argv.every((a) => typeof a === "string")) {
    throw new Error("shell: argv must be a non-empty string array");
  }
  return argv as string[];
}

export function truncate(text: string, limit = OutputLimit): string {
  return text.length <= limit ? text : `${text.slice(0, limit)}\n[truncated]`;
}

export function toolMap(tools: Tool[]): Record<string, Tool> {
  return Object.fromEntries(tools.map((t) => [t.name, t]));
}

import { lstatSync, realpathSync } from "node:fs";
import { basename, dirname, isAbsolute, join, normalize, relative, resolve, sep } from "node:path";

export const DefaultEnvAllow = ["PATH", "HOME", "LANG", "TERM", "TZ"];

export function scrubEnv(env: Record<string, string | undefined>, allow = DefaultEnvAllow): Record<string, string> {
  const out: Record<string, string> = {};
  for (const name of allow) {
    const value = env[name];
    if (value !== undefined) out[name] = value;
  }
  return out;
}

export class OutsideRootError extends Error {
  constructor(root: string, path: string) {
    super(`policy: path ${JSON.stringify(path)} is outside root ${JSON.stringify(root)}`);
    this.name = "OutsideRootError";
  }
}

export function resolveWithin(root: string, p: string): string {
  const absRoot = resolve(root);
  const resolvedRoot = realpathSync(absRoot);
  const full = resolvePath(isAbsolute(p) ? normalize(p) : join(absRoot, p));
  if (!contains(resolvedRoot, full)) throw new OutsideRootError(resolvedRoot, full);
  return full;
}

function contains(root: string, p: string): boolean {
  const rel = relative(root, p);
  return rel === "" || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}

function resolvePath(p: string): string {
  const [ancestor, remainder] = deepestExisting(p);
  return join(realpathSync(ancestor), ...remainder);
}

function deepestExisting(p: string): [string, string[]] {
  const remainder: string[] = [];
  let cur = p;
  while (!exists(cur) && dirname(cur) !== cur) {
    remainder.unshift(basename(cur));
    cur = dirname(cur);
  }
  return [cur, remainder];
}

function exists(p: string): boolean {
  try {
    lstatSync(p);
    return true;
  } catch {
    return false;
  }
}

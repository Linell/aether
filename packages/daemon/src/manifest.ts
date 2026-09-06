export interface Manifest {
  name: string;
  run: string;
}

const NAME_PATTERN = /^[a-z][a-z0-9-]{0,63}$/;

export function parseManifest(text: string): Manifest {
  const parsed = parseObject(text);
  const name = requireString(parsed, "name");
  const run = requireString(parsed, "run");
  if (!NAME_PATTERN.test(name)) {
    throw new Error(
      `aether.json: "name" must match ${NAME_PATTERN} (lowercase letters, digits, hyphens, starting with a letter), got "${name}"`,
    );
  }
  return { name, run };
}

function parseObject(text: string): Record<string, unknown> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new Error(`aether.json is not valid JSON: ${(err as Error).message}`);
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("aether.json must contain a JSON object");
  }
  return parsed as Record<string, unknown>;
}

function requireString(obj: Record<string, unknown>, key: string): string {
  const value = obj[key];
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`aether.json: "${key}" must be a non-empty string`);
  }
  return value;
}

export function defaultManifest(name: string): Manifest {
  return { name, run: "bun run start" };
}

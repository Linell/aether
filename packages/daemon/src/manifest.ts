
export interface Manifest {
  name: string;
  run: string;
}

const NAME_PATTERN = /^[a-z][a-z0-9-]{0,63}$/;

export function parseManifest(text: string): Manifest {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new Error(
      `aether.json is not valid JSON: ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("aether.json must contain a JSON object");
  }

  const { name, run } = parsed as Record<string, unknown>;

  if (typeof name !== "string" || name.length === 0) {
    throw new Error("aether.json: \"name\" must be a non-empty string");
  }
  if (typeof run !== "string" || run.length === 0) {
    throw new Error("aether.json: \"run\" must be a non-empty string");
  }
  if (!NAME_PATTERN.test(name)) {
    throw new Error(
      `aether.json: "name" must match ${NAME_PATTERN} (lowercase letters, digits, hyphens, starting with a letter), got "${name}"`,
    );
  }

  return { name, run };
}

export function defaultManifest(name: string): Manifest {
  return { name, run: "bun run start" };
}

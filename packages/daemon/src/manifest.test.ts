import { describe, expect, test } from "bun:test";
import { defaultManifest, parseManifest } from "./manifest";

describe("parseManifest", () => {
  test("valid manifest", () => {
    const manifest = parseManifest(JSON.stringify({ name: "foo-bar", run: "bun run start" }));
    expect(manifest).toEqual({ name: "foo-bar", run: "bun run start" });
  });

  test("missing run", () => {
    expect(() => parseManifest(JSON.stringify({ name: "foo" }))).toThrow(
      /"run" must be a non-empty string/,
    );
  });

  test("missing name", () => {
    expect(() => parseManifest(JSON.stringify({ run: "bun run start" }))).toThrow(
      /"name" must be a non-empty string/,
    );
  });

  test("bad name", () => {
    expect(() => parseManifest(JSON.stringify({ name: "Foo_Bar!", run: "bun run start" }))).toThrow(
      /"name" must match/,
    );
  });

  test("invalid json", () => {
    expect(() => parseManifest("not json")).toThrow(/not valid JSON/);
  });
});

describe("defaultManifest", () => {
  test("returns default run command", () => {
    expect(defaultManifest("foo")).toEqual({ name: "foo", run: "bun run start" });
  });
});

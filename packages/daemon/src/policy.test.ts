import { expect, test } from "bun:test";
import { mkdtempSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OutsideRootError, resolveWithin, scrubEnv } from "./policy";

test("scrubEnv keeps only allowed names", () => {
  expect(scrubEnv({ PATH: "/bin", AETHER_TOKEN: "secret", HOME: "/home/x" })).toEqual({ PATH: "/bin", HOME: "/home/x" });
});

test("resolveWithin allows children and rejects escapes", () => {
  const parent = mkdtempSync(join(tmpdir(), "aether-policy-"));
  const root = join(parent, "root");
  writeFileSync(join(parent, "secret"), "x");
  Bun.spawnSync(["mkdir", root]);
  symlinkSync(join(parent, "secret"), join(root, "escape"));

  expect(resolveWithin(root, "new/child").endsWith(join("root", "new", "child"))).toBe(true);
  expect(() => resolveWithin(root, "../secret")).toThrow(OutsideRootError);
  expect(() => resolveWithin(root, "escape")).toThrow(OutsideRootError);
  expect(() => resolveWithin(root, "/etc/passwd")).toThrow(OutsideRootError);
});

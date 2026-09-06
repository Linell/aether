import { expect, test } from "bun:test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { shell, truncate } from "./tools";

test("shell runs in the thread directory with a scrubbed env", async () => {
  const cwd = mkdtempSync(join(tmpdir(), "aether-shell-"));
  process.env.AETHER_TOKEN_TEST_LEAK = "leak";
  const out = await shell.run({ argv: ["sh", "-c", "pwd; echo token=$AETHER_TOKEN_TEST_LEAK"] }, { cwd });
  expect(out).toContain("token=\n");
  expect(out.split("\n")[0]?.endsWith(cwd.split("/").pop() ?? "")).toBe(true);
  await expect(shell.run({ argv: ["ls"], cwd: "../.." }, { cwd })).rejects.toThrow("outside root");
  await expect(shell.run({ argv: ["sh", "-c", "exit 3"] }, { cwd })).rejects.toThrow("exit 3");
});

test("truncate caps long output", () => {
  expect(truncate("abcdef", 3)).toBe("abc\n[truncated]");
});

import { expect, test } from "bun:test";
import type { ModelRequest } from "@openai/agents";
import type { Daemon } from "./client";
import { modelFor, modelSpecFor, parseModelSpec } from "./model";
import type { StepLike } from "./turn";

const step: StepLike = { run: (_id, fn) => fn() };

function request(input: ModelRequest["input"]): ModelRequest {
  return { input, modelSettings: {}, tools: [], outputType: "text", handoffs: [], tracing: false };
}

test("parseModelSpec defaults to openai and splits provider from name", () => {
  expect(parseModelSpec(undefined)).toEqual({ provider: "openai", name: "gpt-5.4-mini" });
  expect(parseModelSpec("anthropic:claude-sonnet-5")).toEqual({ provider: "anthropic", name: "claude-sonnet-5" });
  expect(parseModelSpec("scripted")).toEqual({ provider: "scripted", name: "scripted" });
  expect(() => parseModelSpec("gemini:pro")).toThrow("unrecognized");
});

const doc: Daemon = { name: "foo", host: "h1", class: "worker", offline_policy: "skip", status: "online" };

test("modelSpecFor prefers the daemon doc, then the env, then the default", () => {
  const env = { AETHER_MODEL: "openai:gpt-5.4" };
  expect(modelSpecFor({ ...doc, model: "scripted" }, env)).toEqual({ provider: "scripted", name: "scripted" });
  expect(modelSpecFor(doc, env)).toEqual({ provider: "openai", name: "gpt-5.4" });
  expect(modelSpecFor(undefined, {})).toEqual({ provider: "openai", name: "gpt-5.4-mini" });
});

test("scripted model asks for two shell calls, then reports their output", async () => {
  const model = modelFor({ provider: "scripted", name: "scripted" }, step);
  const first = await model.getResponse(request("hi"));
  expect(first.output.map((o) => (o.type === "function_call" ? o.arguments : o.type))).toEqual([
    JSON.stringify({ argv: ["echo", "alpha"] }),
    JSON.stringify({ argv: ["echo", "beta"] }),
  ]);
  const second = await model.getResponse(
    request([
      { type: "message", role: "user", content: "hi" },
      { type: "function_call_result", name: "shell", callId: "call_a", status: "completed", output: "alpha\n" },
      { type: "function_call_result", name: "shell", callId: "call_b", status: "completed", output: { type: "text", text: "beta\n" } },
    ]),
  );
  const [reply] = second.output;
  expect(reply?.type === "message" && reply.role === "assistant" && reply.content[0]?.type === "output_text" ? reply.content[0].text : "").toBe(
    "done: alpha, beta",
  );
  expect(() => model.getStreamedResponse(request("hi"))).toThrow("never used");
});

import {
  OpenAIProvider,
  Usage,
  type AgentInputItem,
  type AgentOutputItem,
  type Model,
  type ModelRequest,
  type ModelResponse,
  type protocol,
} from "@openai/agents";
import { aisdk } from "@openai/agents-extensions/ai-sdk";
import { createAnthropic } from "@ai-sdk/anthropic";
import type { Daemon } from "./client";
import type { StepLike } from "./turn";

export type { Model } from "@openai/agents";

export type ModelProvider = "openai" | "anthropic" | "scripted";

export interface ModelSpec {
  provider: ModelProvider;
  name: string;
}

export const DefaultModel = "openai:gpt-5.4-mini";

export function modelSpecFor(daemon: Daemon | undefined, env: Record<string, string | undefined>): ModelSpec {
  return parseModelSpec(daemon?.model ?? env.AETHER_MODEL);
}

export function parseModelSpec(raw: string | undefined = DefaultModel): ModelSpec {
  if (raw === "scripted") return { provider: "scripted", name: "scripted" };
  const sep = raw.indexOf(":");
  const provider = raw.slice(0, sep);
  const name = raw.slice(sep + 1);
  if ((provider === "openai" || provider === "anthropic") && name.length > 0) return { provider, name };
  throw new Error(`model spec: unrecognized ${JSON.stringify(raw)}`);
}

export function modelFor(spec: ModelSpec, step: StepLike): Model {
  if (spec.provider === "scripted") return scriptedModel(step);
  return stepped(step, providerModel(spec));
}

interface Responder {
  getResponse(request: ModelRequest): Promise<Responded>;
}

interface Responded {
  output: AgentOutputItem[];
  responseId?: string | undefined;
  usage: Usage;
}

function providerModel(spec: ModelSpec): () => Promise<Responder> {
  if (spec.provider === "anthropic") {
    const inner = aisdk(createAnthropic()(spec.name));
    return async () => inner;
  }
  const provider = new OpenAIProvider({ useResponses: true });
  return () => provider.getModel(spec.name);
}

interface Memoized {
  output: AgentOutputItem[];
  responseId?: string;
  usage: { input_tokens: number; output_tokens: number; total_tokens: number };
}

function stepped(step: StepLike, inner: () => Promise<Responder>): Model {
  return {
    async getResponse(request) {
      const done = await step.run("model", async () => memoize(await (await inner()).getResponse(request)));
      return restore(done);
    },
    getStreamedResponse: neverStreams,
  };
}

function memoize(res: Responded): Memoized {
  const { inputTokens, outputTokens, totalTokens } = res.usage;
  const usage = { input_tokens: inputTokens, output_tokens: outputTokens, total_tokens: totalTokens };
  return res.responseId === undefined ? { output: res.output, usage } : { output: res.output, responseId: res.responseId, usage };
}

function restore(done: Memoized): ModelResponse {
  const usage = new Usage({ requests: 1, ...done.usage });
  return done.responseId === undefined ? { output: done.output, usage } : { output: done.output, responseId: done.responseId, usage };
}

function neverStreams(): never {
  throw new Error("streaming is never used");
}

function scriptedModel(step: StepLike): Model {
  return {
    async getResponse(request) {
      const output = await step.run("model", async () => (hasToolResults(request) ? sayDone(request) : callBoth()));
      return { output, usage: new Usage() };
    },
    getStreamedResponse: neverStreams,
  };
}

function callBoth(): AgentOutputItem[] {
  return [shellCall("call_a", ["echo", "alpha"]), shellCall("call_b", ["echo", "beta"])];
}

function shellCall(callId: string, argv: string[]): AgentOutputItem {
  return { type: "function_call", callId, name: "shell", status: "completed", arguments: JSON.stringify({ argv }) };
}

function sayDone(request: ModelRequest): AgentOutputItem[] {
  const outputs = thisTurn(request).filter(isResult).map((item) => textOf(item.output).trim());
  return [
    {
      type: "message",
      role: "assistant",
      status: "completed",
      content: [{ type: "output_text", text: `done: ${outputs.join(", ")}` }],
    },
  ];
}

function hasToolResults(request: ModelRequest): boolean {
  return thisTurn(request).some(isResult);
}

function thisTurn(request: ModelRequest): AgentInputItem[] {
  if (typeof request.input === "string") return [];
  let start = 0;
  request.input.forEach((item, i) => {
    if (isUser(item)) start = i + 1;
  });
  return request.input.slice(start);
}

function isUser(item: AgentInputItem): boolean {
  return "role" in item && item.role === "user";
}

function isResult(item: AgentInputItem): item is protocol.FunctionCallResultItem {
  return item.type === "function_call_result";
}

function textOf(output: protocol.FunctionCallResultItem["output"]): string {
  if (typeof output === "string") return output;
  if (Array.isArray(output)) return output.flatMap((o) => (o.type === "input_text" ? [o.text] : [])).join(" ");
  return output.type === "text" ? output.text : "";
}

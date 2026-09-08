import type { AgentInputItem } from "@openai/agents";
import { z } from "zod";
import type { Message } from "./client";

export const Reply = z.object({ status: z.enum(["working", "done"]), text: z.string() });
export type Reply = z.infer<typeof Reply>;

export const ToolOutputClip = 1000;

export function historyItems(newestFirst: Message[], budget: number): AgentInputItem[] {
  const kept: Message[] = [];
  let used = 0;
  for (const m of newestFirst) {
    used += m.text.length;
    if (used > budget) break;
    kept.unshift(m);
  }
  return kept.map(itemOf);
}

function itemOf(m: Message): AgentInputItem {
  if (m.role === "user") return { role: "user", content: m.text };
  return { role: "assistant", status: "completed", content: [{ type: "output_text", text: m.text }] };
}

export function sinceLastUser(items: AgentInputItem[], isRequest: (text: string) => boolean = () => true): AgentInputItem[] {
  let at = 0;
  items.forEach((item, i) => {
    if (isUser(item) && isRequest(contentText(item.content))) at = i;
  });
  return items.slice(at);
}

export function parseReply(output: unknown): Reply {
  const parsed = Reply.safeParse(typeof output === "string" ? tryJson(output) : output);
  if (parsed.success) return parsed.data;
  return { status: "done", text: output === undefined || output === null ? "" : String(output) };
}

function tryJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

export function transcript(items: AgentInputItem[]): string {
  return items.flatMap(lineOf).join("\n");
}

function lineOf(item: AgentInputItem): string[] {
  if (isUser(item)) return [`operator: ${contentText(item.content)}`];
  if ("role" in item && item.role === "assistant") return [`assistant: ${parseReply(contentText(item.content)).text}`];
  if (item.type === "function_call") return [`tool ${item.name} ${item.arguments}`];
  if (item.type === "function_call_result") return [`-> ${clip(contentText(item.output), ToolOutputClip)}`];
  return [];
}

function isUser(item: AgentInputItem): item is Extract<AgentInputItem, { role: "user" }> {
  return "role" in item && item.role === "user";
}

function clip(text: string, limit: number): string {
  return text.length <= limit ? text : `${text.slice(0, limit)}…`;
}

export function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) return content.map(partText).filter((t) => t.length > 0).join(" ");
  return partText(content);
}

function partText(part: unknown): string {
  if (typeof part !== "object" || part === null || !("type" in part)) return "";
  const p = part as { type: string; text?: unknown };
  return (p.type === "input_text" || p.type === "output_text" || p.type === "text") && typeof p.text === "string" ? p.text : "";
}

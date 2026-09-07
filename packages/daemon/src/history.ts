import type { AgentInputItem } from "@openai/agents";
import type { Message } from "./client";

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

export function sinceLastUser(items: AgentInputItem[]): AgentInputItem[] {
  let at = 0;
  items.forEach((item, i) => {
    if ("role" in item && item.role === "user") at = i;
  });
  return items.slice(at);
}

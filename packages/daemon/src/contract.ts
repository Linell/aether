
export const Events = {
  MessageSent: "aether/message.sent",
  ScheduleFired: "aether/schedule.fired",
  ApprovalAnswered: "aether/approval.answered",
  MessageReplied: "daemon/message.replied",
  ApprovalRequested: "daemon/approval.requested",
  ConjureRequested: "daemon/conjure.requested",
  DeployRequested: "daemon/deploy.requested",
  DismissRequested: "daemon/dismiss.requested",
  ChangeRequested: "daemon/change.requested",
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

export interface ToolCall {
  id: string;
  tool: string;
  args: Record<string, unknown>;
  context: {
    cwd: string;
    host: string;
  };
}

export interface MessageSentPayload {
  daemon: string;
  thread: string;
  message: string;
  text: string;
}

export interface ScheduleFiredPayload {
  daemon: string;
  thread: string;
  schedule: string;
  due_at: string;
  deadline_at: string;
  prompt: string;
}

export interface Decision {
  call: string;
  approval: string;
  decision: "approved" | "denied";
}

export interface ApprovalAnsweredPayload {
  daemon: string;
  thread: string;
  group: string;
  decisions: Decision[];
}

export interface MessageRepliedPayload {
  daemon: string;
  thread: string;
  reply: string;
  text: string;
}

export interface ApprovalRequestedPayload {
  daemon: string;
  thread: string;
  calls: ToolCall[];
}

export interface ConjureRequestedPayload {
  daemon: string;
  host: string;
  language: string;
}

export interface DeployRequestedPayload {
  daemon: string;
  host: string;
}

export interface DismissRequestedPayload {
  daemon: string;
  host: string;
}

export interface ChangeRequestedPayload {
  daemon: string;
  patch: string;
  tests: string[];
}

export type Payloads = {
  [Events.MessageSent]: MessageSentPayload;
  [Events.ScheduleFired]: ScheduleFiredPayload;
  [Events.ApprovalAnswered]: ApprovalAnsweredPayload;
  [Events.MessageReplied]: MessageRepliedPayload;
  [Events.ApprovalRequested]: ApprovalRequestedPayload;
  [Events.ConjureRequested]: ConjureRequestedPayload;
  [Events.DeployRequested]: DeployRequestedPayload;
  [Events.DismissRequested]: DismissRequestedPayload;
  [Events.ChangeRequested]: ChangeRequestedPayload;
};
export const TurnConcurrency = {
  key: "event.data.thread",
  limit: 1,
} as const;

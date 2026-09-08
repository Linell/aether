import type { Decision, ToolCall } from "./contract";

export interface VersionedDocument {
  content: string;
  version: number;
}

export interface Schedule {
  id: string;
  daemon: string;
  thread: string;
  cron: string;
  tz: string;
  next_run_at: string;
  policy: string;
  ttl_seconds?: number | null;
  prompt: string;
}

export type ScheduleInput = Omit<Schedule, "id" | "daemon" | "next_run_at">;

export type MarkerKind =
  | "schedule.stale"
  | "turn.failed"
  | "turn.paused"
  | "turn.incomplete"
  | "turn.empty"
  | "memory.conflict"
  | "memory.failed"
  | "approval.ignored";

export interface Marker {
  id: string;
  kind: MarkerKind;
  thread: string;
  ref: string;
  detail: string;
}

export interface Daemon {
  name: string;
  host: string;
  class: string;
  offline_policy: string;
  status: string;
  model?: string;
}

export interface Message {
  id: string;
  role: "user" | "assistant";
  text: string;
  created_at: string;
}

export interface HistoryQuery {
  before?: string;
  limit?: number;
}

export interface Thread {
  id: string;
  daemon: string;
  host: string;
  directory: string;
}

export interface Approval {
  id: string;
  daemon: string;
  thread: string;
  status: "pending" | "approved" | "denied";
  call: ToolCall;
  group?: string;
  remaining: number;
}

export interface ApprovalGroup {
  id: string;
  thread: string;
  status: "pending" | "decided";
  state: string;
  decisions: Decision[];
}

export interface Paused {
  group: string;
  approvals: string[];
}

export interface MatchResult {
  allowed: boolean;
  rule?: string;
  reason?: string;
}

export class AetherHttpError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(`aether request failed: ${status} ${body}`);
    this.name = "AetherHttpError";
    this.status = status;
    this.body = body;
  }
}

export interface AetherClient {
  reply(thread: string, text: string, id?: string): Promise<void>;
  putMarker(daemon: string, marker: Marker): Promise<void>;
  requestApproval(thread: string, calls: ToolCall[], state: string, run: string): Promise<Paused>;
  getThread(id: string): Promise<Thread>;
  listMessages(thread: string, query?: HistoryQuery): Promise<Message[]>;
  getDaemon(name: string): Promise<Daemon>;
  getApproval(id: string): Promise<Approval>;
  getApprovalGroup(id: string): Promise<ApprovalGroup>;
  matchCall(daemon: string, thread: string, call: ToolCall): Promise<MatchResult>;
  claimOperation(id: string, kind: string): Promise<boolean>;
  getSoul(daemon: string): Promise<VersionedDocument>;
  getMemory(daemon: string): Promise<VersionedDocument>;
  putMemory(daemon: string, body: string, expectedVersion: number): Promise<VersionedDocument>;
  listSchedules(daemon: string): Promise<Schedule[]>;
  putSchedule(daemon: string, id: string, schedule: ScheduleInput): Promise<Schedule>;
  deleteSchedule(daemon: string, id: string): Promise<void>;
}

export interface CreateClientOptions {
  baseUrl: string;
  token: string;
  fetch?: typeof fetch;
}

export function createClient(options: CreateClientOptions): AetherClient {
  const { baseUrl, token } = options;
  const doFetch = options.fetch ?? fetch;

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await doFetch(`${baseUrl}/v1${path}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) throw new AetherHttpError(res.status, text);
    return (text.length === 0 ? undefined : JSON.parse(text)) as T;
  }

  return {
    async reply(thread, text, id) {
      await request("POST", `/threads/${thread}/reply`, { id, text });
    },
    async putMarker(daemon, marker) {
      await request("POST", `/daemons/${daemon}/markers`, marker);
    },
    requestApproval: (thread, calls, state, run) => request("POST", `/threads/${thread}/approvals`, { calls, state, run }),
    getThread: (id) => request("GET", `/threads/${id}`),
    async listMessages(thread, query = {}) {
      const res = await request<{ messages: Message[] }>("GET", `/threads/${thread}/messages${historyParams(query)}`);
      return res.messages;
    },
    getDaemon: (name) => request("GET", `/daemons/${name}`),
    getApproval: (id) => request("GET", `/approvals/${id}`),
    getApprovalGroup: (id) => request("GET", `/approvals/groups/${id}`),
    matchCall: (daemon, thread, call) => request("POST", `/daemons/${daemon}/allowlist/match`, { thread, call }),
    async claimOperation(id, kind) {
      const res = await request<{ claimed: boolean }>("POST", "/operations", { id, kind });
      return res.claimed;
    },
    getSoul: (daemon) => request("GET", `/daemons/${daemon}/soul`),
    getMemory: (daemon) => request("GET", `/daemons/${daemon}/memory`),
    putMemory: (daemon, body, expectedVersion) =>
      request("PUT", `/daemons/${daemon}/memory`, { content: body, expected_version: expectedVersion }),
    listSchedules: (daemon) => request("GET", `/daemons/${daemon}/schedules`),
    putSchedule: (daemon, id, schedule) => request("PUT", `/daemons/${daemon}/schedules/${id}`, schedule),
    async deleteSchedule(daemon, id) {
      await request("DELETE", `/daemons/${daemon}/schedules/${id}`);
    },
  };
}

function historyParams({ before, limit }: HistoryQuery): string {
  const params = new URLSearchParams();
  if (before !== undefined) params.set("before", before);
  if (limit !== undefined) params.set("limit", String(limit));
  const text = params.toString();
  return text.length === 0 ? "" : `?${text}`;
}

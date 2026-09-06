import type { ToolCall } from "./contract";

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
}

export type ScheduleInput = Omit<Schedule, "id" | "daemon" | "next_run_at">;

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
  reply(thread: string, text: string): Promise<void>;
  requestApproval(thread: string, calls: ToolCall[]): Promise<{ approval: string }>;
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
    async reply(thread, text) {
      await request("POST", `/threads/${thread}/reply`, { text });
    },
    requestApproval: (thread, calls) => request("POST", `/threads/${thread}/approvals`, { calls }),
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

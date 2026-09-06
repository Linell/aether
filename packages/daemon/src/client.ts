
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

const routes = {
  reply: (thread: string) => `/v1/threads/${thread}/reply`,
  approval: (thread: string) => `/v1/threads/${thread}/approvals`,
  soul: (daemon: string) => `/v1/daemons/${daemon}/soul`,
  memory: (daemon: string) => `/v1/daemons/${daemon}/memory`,
  schedules: (daemon: string) => `/v1/daemons/${daemon}/schedules`,
  schedule: (daemon: string, id: string) => `/v1/daemons/${daemon}/schedules/${id}`,
} as const;

export interface CreateClientOptions {
  baseUrl: string;
  token: string;
  fetch?: typeof fetch;
}

export function createClient(options: CreateClientOptions): AetherClient {
  const { baseUrl, token } = options;
  const doFetch = options.fetch ?? fetch;

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await doFetch(`${baseUrl}${path}`, {
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
      await request<void>("POST", routes.reply(thread), { text });
    },

    async requestApproval(thread, calls) {
      return request<{ approval: string }>("POST", routes.approval(thread), { calls });
    },

    async getSoul(daemon) {
      return request<VersionedDocument>("GET", routes.soul(daemon));
    },

    async getMemory(daemon) {
      return request<VersionedDocument>("GET", routes.memory(daemon));
    },

    async putMemory(daemon, body, expectedVersion) {
      return request<VersionedDocument>("PUT", routes.memory(daemon), {
        content: body,
        expected_version: expectedVersion,
      });
    },

    async listSchedules(daemon) {
      return request<Schedule[]>("GET", routes.schedules(daemon));
    },

    async putSchedule(daemon, id, schedule) {
      return request<Schedule>("PUT", routes.schedule(daemon, id), schedule);
    },

    async deleteSchedule(daemon, id) {
      await request<void>("DELETE", routes.schedule(daemon, id));
    },
  };
}

-- 0001_init.sql: initial schema for aether's application state.
-- SQLite owns application state; Inngest Cloud owns durable execution.

CREATE TABLE hosts (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE daemons (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    host_id        TEXT NOT NULL REFERENCES hosts(id),
    class          TEXT NOT NULL CHECK (class IN ('anchored', 'opportunistic')),
    offline_policy TEXT NOT NULL CHECK (offline_policy IN ('queue', 'skip', 'ttl')),
    status         TEXT NOT NULL DEFAULT 'active',
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_daemons_host_id ON daemons(host_id);

-- Soul and memory are both whole documents in rows, append-only versioned.
CREATE TABLE souls (
    id         TEXT PRIMARY KEY,
    daemon_id  TEXT NOT NULL REFERENCES daemons(id),
    version    INTEGER NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (daemon_id, version)
);

CREATE INDEX idx_souls_daemon_id ON souls(daemon_id, version);

CREATE TABLE memory (
    id         TEXT PRIMARY KEY,
    daemon_id  TEXT NOT NULL REFERENCES daemons(id),
    version    INTEGER NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (daemon_id, version)
);

CREATE INDEX idx_memory_daemon_id ON memory(daemon_id, version);

CREATE TABLE threads (
    id         TEXT PRIMARY KEY,
    daemon_id  TEXT NOT NULL REFERENCES daemons(id),
    host_id    TEXT NOT NULL REFERENCES hosts(id),
    directory  TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_threads_daemon_id ON threads(daemon_id);

CREATE TABLE messages (
    id         TEXT PRIMARY KEY,
    thread_id  TEXT NOT NULL REFERENCES threads(id),
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    text       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'sent',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_messages_thread_id ON messages(thread_id, created_at);

-- Approval is out of band: a row, answerable from any surface, no timeout.
CREATE TABLE approvals (
    id          TEXT PRIMARY KEY,
    thread_id   TEXT NOT NULL REFERENCES threads(id),
    call_id     TEXT NOT NULL UNIQUE,
    tool        TEXT NOT NULL,
    args        TEXT NOT NULL,
    context     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    decision_at TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_approvals_thread_id ON approvals(thread_id);
CREATE INDEX idx_approvals_status ON approvals(status);

CREATE TABLE schedules (
    id             TEXT PRIMARY KEY,
    daemon_id      TEXT NOT NULL REFERENCES daemons(id),
    cron           TEXT NOT NULL,
    tz             TEXT NOT NULL,
    next_run_at    TEXT NOT NULL,
    offline_policy TEXT NOT NULL CHECK (offline_policy IN ('queue', 'skip', 'ttl')),
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_schedules_daemon_id ON schedules(daemon_id);
CREATE INDEX idx_schedules_next_run_at ON schedules(next_run_at) WHERE enabled = 1;

-- operations: retry dedupe for effects that must not double-run.
CREATE TABLE operations (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- outbox: state changes and dispatch intent commit together in one
-- transaction; publish with stable event IDs.
CREATE TABLE outbox (
    id           TEXT PRIMARY KEY,
    event_name   TEXT NOT NULL,
    payload      TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    published_at TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_outbox_unpublished ON outbox(created_at) WHERE published_at IS NULL;

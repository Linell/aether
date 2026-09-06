ALTER TABLE schedules ADD COLUMN thread_id TEXT NOT NULL DEFAULT '';
ALTER TABLE schedules ADD COLUMN ttl_seconds INTEGER;

CREATE TABLE markers (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    daemon_id  TEXT NOT NULL REFERENCES daemons(id),
    thread_id  TEXT NOT NULL DEFAULT '',
    ref_id     TEXT NOT NULL,
    detail     TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_markers_daemon_id ON markers(daemon_id, created_at);

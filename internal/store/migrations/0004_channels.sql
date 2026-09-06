CREATE TABLE channels (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    external_id TEXT NOT NULL,
    thread_id   TEXT NOT NULL REFERENCES threads(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (kind, external_id)
);

CREATE INDEX idx_channels_thread_id ON channels(thread_id);

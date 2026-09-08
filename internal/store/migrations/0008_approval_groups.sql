CREATE TABLE approval_groups (
    id         TEXT PRIMARY KEY,
    thread_id  TEXT NOT NULL REFERENCES threads(id),
    run_id     TEXT NOT NULL UNIQUE,
    state      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'decided')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
ALTER TABLE approvals ADD COLUMN group_id TEXT REFERENCES approval_groups(id);
CREATE INDEX idx_approvals_group_id ON approvals(group_id);

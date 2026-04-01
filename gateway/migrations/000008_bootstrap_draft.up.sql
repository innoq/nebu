-- gateway/migrations/000008_bootstrap_draft.up.sql
-- Stores bootstrap wizard draft data (encrypted where sensitive).
-- Replaces the in-memory sync.Map secretStore; persists across gateway restarts.

CREATE TABLE bootstrap_draft (
    key    VARCHAR(255) PRIMARY KEY,
    value  TEXT         NOT NULL,
    set_at BIGINT       NOT NULL
);

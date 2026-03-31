-- gateway/migrations/000007_invalidated_tokens.up.sql
-- Stores explicitly invalidated tokens (logout denylist).
-- Replaces the in-memory sync.Map; persists across gateway restarts
-- and is shared across all gateway instances.

CREATE TABLE invalidated_tokens (
    token_hash  TEXT    PRIMARY KEY,
    expires_at  BIGINT  NOT NULL   -- Unix milliseconds; expired rows are ignored
);

CREATE INDEX invalidated_tokens_expires_at_idx ON invalidated_tokens (expires_at);

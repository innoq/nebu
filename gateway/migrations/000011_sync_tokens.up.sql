-- gateway/migrations/000011_sync_tokens.up.sql
-- sync_tokens: PostgreSQL persistence for since-token checkpointing.
-- Enables /sync incremental resume after gateway or core restarts.
-- user_id FK to users ensures referential integrity.

CREATE TABLE sync_tokens (
    user_id       TEXT    PRIMARY KEY REFERENCES users(user_id),
    since_token   TEXT    NOT NULL,
    last_event_id TEXT,
    updated_at    BIGINT  NOT NULL
);

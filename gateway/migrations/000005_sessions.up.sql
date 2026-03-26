-- gateway/migrations/000005_sessions.up.sql
-- sessions: ETS + PostgreSQL hybrid since-token checkpointing for /sync recovery.
-- user_id FK to users ensures referential integrity.

CREATE TABLE sessions (
    session_id      TEXT    PRIMARY KEY,
    user_id         TEXT    NOT NULL REFERENCES users(user_id),
    since_token     TEXT,
    device_id       TEXT    NOT NULL,
    last_active_at  BIGINT  NOT NULL,
    created_at      BIGINT  NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

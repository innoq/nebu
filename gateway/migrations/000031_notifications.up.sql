-- Migration 000031: notifications table for per-user notification history.
--
-- Each delivered event generates one notification row per recipient.
-- The endpoint GET /_matrix/client/v3/notifications reads from this table
-- with cursor-based pagination (newest-first by id DESC).
--
-- Cursor encoding: the BIGSERIAL id is encoded as a base64url string
-- to form the opaque next_token. The gateway decodes it on the next request.
--
-- Row filtering is enforced by the WHERE user_id = $1 clause in every query.
-- RLS via app.user_id GUC is deferred until per-request SET LOCAL is wired
-- into the pgx connection pool (follow-up to story 7-24 account_data gap).

CREATE TABLE notifications (
    id          BIGSERIAL   PRIMARY KEY,
    user_id     TEXT        NOT NULL,
    room_id     TEXT        NOT NULL,
    event_id    TEXT        NOT NULL,
    event_json  JSONB       NOT NULL,
    actions     JSONB       NOT NULL DEFAULT '["notify"]',
    read        BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index supports the paginated cursor query: newest-first per user.
CREATE INDEX notifications_user_created
    ON notifications (user_id, created_at DESC);

-- Grant DML access to nebu_app (no DELETE — notifications are append-only).
GRANT SELECT, INSERT, UPDATE ON notifications TO nebu_app;
GRANT USAGE, SELECT ON SEQUENCE notifications_id_seq TO nebu_app;

-- Migration 000029: room_account_data table for per-room user account data.
--
-- Stores arbitrary JSON content for (userId, roomId, eventType) triples.
-- Surfaced in /sync under rooms.join.{roomId}.account_data.events.
-- Upsert semantics via INSERT … ON CONFLICT DO UPDATE (last write wins).
--
-- RLS: nebu_app may read/write only rows where user_id = current_setting('app.user_id').
-- nebu_migrate retains full access as the table owner.

CREATE TABLE room_account_data (
    user_id     TEXT        NOT NULL,
    room_id     TEXT        NOT NULL,
    event_type  TEXT        NOT NULL,
    content     JSONB       NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, room_id, event_type)
);

-- Grant full DML access to nebu_app (SELECT, INSERT, UPDATE — DELETE handled by 000027).
GRANT SELECT, INSERT, UPDATE ON room_account_data TO nebu_app;

-- Row-level security: nebu_app may only touch its own rows.
ALTER TABLE room_account_data ENABLE ROW LEVEL SECURITY;

-- Force RLS even for table owner connections that run as nebu_app role.
ALTER TABLE room_account_data FORCE ROW LEVEL SECURITY;

CREATE POLICY room_account_data_nebu_app_policy ON room_account_data
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));

-- 000036_rooms_admin_columns.up.sql
-- Story 6.7: Extends rooms table with admin-facing columns.
-- max_members, status, archive_reason are also used by Stories 6.8 and 6.9.

ALTER TABLE rooms ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT '';
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS creator_user_id TEXT;
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS max_members INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS archive_reason TEXT;

ALTER TABLE rooms
    ADD CONSTRAINT rooms_status_check
    CHECK (status IN ('active', 'archived'));

CREATE INDEX rooms_status_idx ON rooms (status);
CREATE INDEX rooms_created_at_id_idx ON rooms (created_at DESC, room_id);

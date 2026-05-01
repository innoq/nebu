-- 000036_rooms_admin_columns.down.sql
-- Story 6.7: Reverses the rooms table extensions added by the up migration.

ALTER TABLE rooms DROP CONSTRAINT IF EXISTS rooms_status_check;
DROP INDEX IF EXISTS rooms_status_idx;
DROP INDEX IF EXISTS rooms_created_at_id_idx;
ALTER TABLE rooms DROP COLUMN IF EXISTS archive_reason;
ALTER TABLE rooms DROP COLUMN IF EXISTS status;
ALTER TABLE rooms DROP COLUMN IF EXISTS max_members;
ALTER TABLE rooms DROP COLUMN IF EXISTS creator_user_id;
ALTER TABLE rooms DROP COLUMN IF EXISTS topic;

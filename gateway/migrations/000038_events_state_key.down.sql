-- Rollback Story 9-7: remove state_key column from events table.
DROP INDEX IF EXISTS events_room_state_idx;
ALTER TABLE events DROP COLUMN IF EXISTS state_key;

-- gateway/migrations/000030_device_display_name.up.sql
-- Story 7-26: Add device_display_name to sessions table for Matrix device rename.
-- Nullable — existing rows keep NULL until a client sets a display name.

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_display_name TEXT;

-- gateway/migrations/000030_device_display_name.down.sql
-- Reverses 000030_device_display_name.up.sql

ALTER TABLE sessions DROP COLUMN IF EXISTS device_display_name;

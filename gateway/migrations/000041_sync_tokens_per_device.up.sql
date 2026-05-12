-- Add device_id to sync_tokens for per-device sync checkpoint storage.
-- Existing rows are assigned device_id = '' (legacy) to preserve data.
-- The new composite PK (user_id, device_id) ensures each device has an
-- independent checkpoint that parallel sessions cannot overwrite.

ALTER TABLE sync_tokens
  ADD COLUMN device_id TEXT NOT NULL DEFAULT '';

ALTER TABLE sync_tokens DROP CONSTRAINT sync_tokens_pkey;
ALTER TABLE sync_tokens ADD PRIMARY KEY (user_id, device_id);

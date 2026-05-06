-- Revert per-device sync_tokens: drop device_id, restore user_id-only PK.
-- Rows with device_id != '' are dropped (they have no legacy equivalent).

DELETE FROM sync_tokens WHERE device_id != '';
ALTER TABLE sync_tokens DROP CONSTRAINT sync_tokens_pkey;
ALTER TABLE sync_tokens ALTER COLUMN device_id DROP NOT NULL;
ALTER TABLE sync_tokens DROP COLUMN device_id;
ALTER TABLE sync_tokens ADD PRIMARY KEY (user_id);

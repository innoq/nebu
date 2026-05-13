-- Revert: remove column comment added in migration 000047.
COMMENT ON COLUMN media_files.uploader_user_id IS NULL;

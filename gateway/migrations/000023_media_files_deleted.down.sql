-- Story 5.8: Reverse migration — remove deleted column from media_files table.
ALTER TABLE media_files DROP COLUMN IF EXISTS deleted;

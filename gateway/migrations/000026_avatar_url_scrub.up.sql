-- Migration 000026: scrub unsafe avatar_url values from profiles table.
-- FB-58-02 (Story 5.29b): avatar_url values that pass the old "mxc://" prefix-only check
-- but contain path-traversal characters are set to NULL.
-- This is a one-time data correction; the new PutAvatarURL handler now validates safe
-- mxc:// format at write time, so no new unsafe values can be stored.
--
-- Scrub patterns (TEA-MINOR-7, Story 5.29b code review):
--   - dotdot traversal:  ".." or "/../"
--   - backslash separator (Windows path): '\\'
--   - more than one '/' AFTER the mxc:// prefix (a safe URI is mxc://server/mediaId)
--
-- NUL-byte check intentionally omitted: PostgreSQL TEXT/VARCHAR columns reject
-- 0x00 at the type-system level, so an avatar_url containing NUL cannot exist
-- in the database in the first place. The PutAvatarURL handler still rejects
-- NUL at write time as defense-in-depth.
--
-- Irreversible: down-migration is a no-op (NULL is the safe default).
UPDATE profiles
SET avatar_url = NULL
WHERE avatar_url IS NOT NULL
  AND avatar_url LIKE 'mxc://%'
  AND (
        avatar_url LIKE '%..%'
     OR avatar_url LIKE '%/../%'
     OR avatar_url LIKE E'%\\\\%'      -- backslash anywhere (raw \ in literal needs E'\\\\')
     OR (length(avatar_url) - length(replace(avatar_url, '/', ''))) > 3
        -- a safe mxc URI has exactly 3 forward slashes: "mxc://server/mediaId"
        --                                                     ^^      ^
        -- More than 3 slashes implies an extra path segment (e.g. "mxc://s/a/b").
  );

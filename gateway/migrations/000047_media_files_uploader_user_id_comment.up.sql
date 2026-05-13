-- Story 12.9 — document canonical Matrix user ID format for media_files.uploader_user_id.
--
-- From Story 12.9 onwards, the media gateway constructs @localpart:server before inserting.
-- No data migration: historical rows pre-12.9 may contain raw OIDC sub/name claims.
-- These are grandfathered. Only rows inserted after Story 12.9 deployment contain
-- canonical @localpart:server format.
COMMENT ON COLUMN media_files.uploader_user_id
    IS 'Canonical Matrix user ID (@localpart:server). Historical rows pre-12.9 may contain raw OIDC sub/name claims — grandfathered.';

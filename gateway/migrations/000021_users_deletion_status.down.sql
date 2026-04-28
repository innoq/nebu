-- gateway/migrations/000021_users_deletion_status.down.sql
-- Story 5.7: Reverse migration — drops deletion_status and keys_deleted_at from users table.

ALTER TABLE users
    DROP COLUMN IF EXISTS deletion_status,
    DROP COLUMN IF EXISTS keys_deleted_at;

-- gateway/migrations/000021_users_deletion_status.up.sql
-- Story 5.7: Atomare DSGVO-Deletion — adds deletion_status + keys_deleted_at to users table.
--
-- deletion_status: tracks the deletion state machine:
--   NULL              → active (default for existing rows — no DEFAULT specified, treat NULL as 'active')
--   'deletion_in_progress' → Multi-TX started but not completed (transient state; rolled back on failure)
--   'keys_deleted'    → private keys permanently soft-deleted
--
-- keys_deleted_at: epoch milliseconds (BIGINT NULL) — consistent with existing
--   deleted_at BIGINT in user_keys table. NULL until keys are deleted.
--
-- NOT VALID on the CHECK constraint: non-blocking for existing rows (no full-table scan);
-- constraint is validated lazily on UPDATE/INSERT. This avoids an ACCESS EXCLUSIVE lock
-- on large tables during migration.

ALTER TABLE users
    ADD COLUMN deletion_status TEXT,
    ADD COLUMN keys_deleted_at BIGINT;

ALTER TABLE users
    ADD CONSTRAINT users_deletion_status_check
    CHECK (deletion_status IN ('deletion_in_progress', 'keys_deleted'))
    NOT VALID;

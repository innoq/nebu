-- Migration 000028 down: no-op.
-- Reverting ownership of audit_log_purge is not meaningful — the function body
-- is identical to the 000025 version. Rolling back to 000025 will re-apply the
-- 000025 function body and grants. This down migration satisfies golang-migrate
-- schema_migrations tracking without performing any schema changes.
SELECT 1;

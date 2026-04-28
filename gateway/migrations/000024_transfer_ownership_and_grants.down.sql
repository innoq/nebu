-- gateway/migrations/000024_transfer_ownership_and_grants.down.sql
-- Reverting this migration is intentionally a no-op.
-- Transferring ownership back to nebu would re-enable BYPASSRLS behavior and
-- invalidate the trust model established by Story 5.29a.
-- Manual rollback requires re-running the forward migration.
SELECT 1;

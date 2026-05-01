-- Story 6.5: Rollback user deactivation columns

ALTER TABLE users DROP COLUMN IF EXISTS deactivation_reason;
ALTER TABLE users DROP COLUMN IF EXISTS deactivated_at;

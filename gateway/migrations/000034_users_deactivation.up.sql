-- Story 6.5: User Deactivation + Reactivation
-- Adds deactivated_at and deactivation_reason columns to the users table.
-- Both columns are NULL for existing rows (no backfill required).
-- deactivated_at: epoch milliseconds — set on deactivation, cleared on reactivation.
-- deactivation_reason: free-text reason — set on deactivation, cleared on reactivation.

ALTER TABLE users ADD COLUMN deactivated_at BIGINT;
ALTER TABLE users ADD COLUMN deactivation_reason TEXT;

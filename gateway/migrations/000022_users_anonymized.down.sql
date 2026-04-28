-- Story 5.8: Reverse migration — remove anonymized_at column from users table.
ALTER TABLE users DROP COLUMN IF EXISTS anonymized_at;

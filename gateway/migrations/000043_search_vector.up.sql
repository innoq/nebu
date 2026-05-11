-- gateway/migrations/000042_search_vector.up.sql
-- Story 11.1: Add tsvector FTS column + GIN index + trigger to events table.
-- ADR-010: PostgreSQL native tsvector/tsquery approach selected (accepted 2026-05-08).
-- Configuration: pg_catalog.simple — language-agnostic, no stemming, multilingual-safe.

-- Step 1: Add the search_vector column (nullable during migration; trigger fills it on insert).
ALTER TABLE events ADD COLUMN search_vector tsvector;

-- Step 2: Custom trigger function — extracts body from JSONB content.
-- NOTE: tsvector_update_trigger() only works on plain TEXT columns; since body lives
-- inside the JSONB content column, a custom PL/pgSQL function is required.
-- COALESCE(content->>'body', '') ensures non-message events get an empty tsvector (not NULL).
CREATE OR REPLACE FUNCTION events_search_vector_update()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
  NEW.search_vector := to_tsvector('pg_catalog.simple',
    coalesce(NEW.content->>'body', ''));
  RETURN NEW;
END;
$$;

-- Step 3: Attach trigger (BEFORE INSERT OR UPDATE OF content, per-row).
DROP TRIGGER IF EXISTS events_search_vector_trigger ON events;
CREATE TRIGGER events_search_vector_trigger
  BEFORE INSERT OR UPDATE OF content ON events
  FOR EACH ROW
  EXECUTE FUNCTION events_search_vector_update();

-- Step 4: GIN index for efficient FTS queries.
-- NOTE: CREATE INDEX CONCURRENTLY cannot run inside a transaction block.
-- golang-migrate wraps each migration in a transaction by default, so we use
-- plain CREATE INDEX here to stay within the transaction boundary.
CREATE INDEX IF NOT EXISTS events_search_vector_gin_idx
  ON events USING GIN (search_vector);

-- Step 5: Backfill all existing events.
-- Message events (m.room.message) get their body indexed; all other events
-- (state events etc.) get an empty tsvector — do NOT leave them NULL.
UPDATE events
  SET search_vector = to_tsvector('pg_catalog.simple',
    coalesce(content->>'body', ''));

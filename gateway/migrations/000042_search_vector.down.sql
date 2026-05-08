-- gateway/migrations/000042_search_vector.down.sql
-- Rollback Story 11.1: remove search_vector column, GIN index, and trigger from events.

DROP TRIGGER IF EXISTS events_search_vector_trigger ON events;
DROP FUNCTION IF EXISTS events_search_vector_update();
DROP INDEX IF EXISTS events_search_vector_gin_idx;
ALTER TABLE events DROP COLUMN IF EXISTS search_vector;

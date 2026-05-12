-- Story 9-28: expression index for JSONB thread-relation queries.
-- fetch_events_by_relation and count_thread_children filter on
-- content->'m.relates_to'->>'event_id' — without this index each query
-- does a sequential scan over the room's events table partition.
CREATE INDEX CONCURRENTLY IF NOT EXISTS events_relates_to_event_id_idx
  ON events ((content->'m.relates_to'->>'event_id'))
  WHERE content ? 'm.relates_to';

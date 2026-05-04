-- Story 9-7: Add state_key column to events table.
-- State events (m.room.name, m.room.topic, m.room.join_rules, etc.) have a state_key
-- that identifies which "slot" they occupy in room state (e.g. "" for m.room.name,
-- user_id for m.room.member). Regular events have NULL state_key.
ALTER TABLE events ADD COLUMN state_key TEXT;

-- Index to support DISTINCT ON (event_type, state_key) queries used by build_state_events.
CREATE INDEX events_room_state_idx ON events (room_id, event_type, state_key, origin_server_ts DESC)
  WHERE state_key IS NOT NULL;

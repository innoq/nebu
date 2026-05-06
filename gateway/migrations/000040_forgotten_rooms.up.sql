-- gateway/migrations/000040_forgotten_rooms.up.sql
-- Tracks rooms the user has forgotten (§11.3). Filtered from all /sync sections.
CREATE TABLE forgotten_rooms (
    user_id         TEXT    NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    room_id         TEXT    NOT NULL,
    forgotten_at_ms BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1000)::BIGINT,
    PRIMARY KEY (user_id, room_id)
);

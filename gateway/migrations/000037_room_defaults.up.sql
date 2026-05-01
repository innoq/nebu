-- 000037_room_defaults.up.sql
-- Story 6.8: Mutable server-wide room defaults (separate from immutable server_config).
-- server_config uses INSERT-only RLS policy (cannot UPDATE) — room defaults need to be mutable.

CREATE TABLE room_defaults (
    id                    SERIAL  PRIMARY KEY,
    default_max_members   INTEGER NOT NULL DEFAULT 0
        CHECK (default_max_members >= 0),
    default_visibility    TEXT    NOT NULL DEFAULT 'private'
        CHECK (default_visibility IN ('public', 'private')),
    set_at                BIGINT  NOT NULL
);

-- Seed with a single row (the system default)
INSERT INTO room_defaults (default_max_members, default_visibility, set_at)
VALUES (0, 'private', 0);

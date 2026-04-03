CREATE TABLE rooms (
    room_id      TEXT    PRIMARY KEY,
    name         TEXT,
    visibility   TEXT    NOT NULL DEFAULT 'private',
    created_at   BIGINT  NOT NULL,
    archived_at  BIGINT
);

ALTER TABLE rooms
    ADD CONSTRAINT rooms_visibility_check
    CHECK (visibility IN ('public', 'private'));

CREATE TABLE room_members (
    room_id    TEXT    NOT NULL REFERENCES rooms(room_id),
    user_id    TEXT    NOT NULL REFERENCES users(user_id),
    joined_at  BIGINT  NOT NULL,
    left_at    BIGINT,
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX room_members_user_id_idx ON room_members (user_id);

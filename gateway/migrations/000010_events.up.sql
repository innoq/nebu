CREATE TABLE events (
    event_id         TEXT    PRIMARY KEY,
    room_id          TEXT    NOT NULL REFERENCES rooms(room_id),
    sender           TEXT    NOT NULL,
    event_type       TEXT    NOT NULL,
    content          JSONB   NOT NULL,
    origin_server_ts BIGINT  NOT NULL,
    signatures       JSONB
);

CREATE INDEX events_room_id_ts_idx ON events (room_id, origin_server_ts);

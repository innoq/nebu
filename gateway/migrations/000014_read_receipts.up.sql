CREATE TABLE read_receipts (
    room_id      TEXT   NOT NULL,
    user_id      TEXT   NOT NULL,
    event_id     TEXT   NOT NULL,
    receipt_type TEXT   NOT NULL DEFAULT 'm.read',
    received_at  BIGINT NOT NULL,
    PRIMARY KEY (room_id, user_id, receipt_type)
);

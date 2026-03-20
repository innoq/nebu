-- gateway/migrations/000002_message_buffer.up.sql
-- Message buffer for gateway resilience (ADR G12/G13)
-- Used by Epic 4's drain strategy to hold messages during Core unavailability

CREATE TABLE message_buffer (
    id           BIGSERIAL PRIMARY KEY,
    txn_id       TEXT      NOT NULL,
    room_id      TEXT      NOT NULL,
    sender       TEXT      NOT NULL,
    payload      JSONB     NOT NULL,
    received_at  BIGINT    NOT NULL,
    status       TEXT      NOT NULL DEFAULT 'pending',
    retry_count  SMALLINT  NOT NULL DEFAULT 0,
    processed_at BIGINT,
    CONSTRAINT message_buffer_status_check CHECK (status IN ('pending', 'held'))
);

CREATE TABLE message_dead_letter (
    id         BIGSERIAL PRIMARY KEY,
    buffer_id  BIGINT    NOT NULL,
    txn_id     TEXT      NOT NULL,
    payload    JSONB     NOT NULL,
    failed_at  BIGINT    NOT NULL,
    last_error TEXT
);

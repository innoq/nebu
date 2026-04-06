CREATE TABLE room_invitations (
    room_id      TEXT    NOT NULL REFERENCES rooms(room_id),
    inviter_id   TEXT    NOT NULL REFERENCES users(user_id),
    invitee_id   TEXT    NOT NULL REFERENCES users(user_id),
    invited_at   BIGINT  NOT NULL,
    accepted_at  BIGINT,
    rejected_at  BIGINT,
    PRIMARY KEY (room_id, invitee_id)
);

CREATE INDEX room_invitations_invitee_idx ON room_invitations (invitee_id);

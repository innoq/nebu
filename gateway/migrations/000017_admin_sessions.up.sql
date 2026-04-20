CREATE TABLE admin_sessions (
    sid        TEXT        PRIMARY KEY,
    user_id    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_admin_sessions_expires_at ON admin_sessions (expires_at);

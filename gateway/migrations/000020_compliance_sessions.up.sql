-- gateway/migrations/000020_compliance_sessions.up.sql
-- compliance_sessions: stores time-bounded compliance access tokens (Story 5.5)
--
-- Each row represents one issued JWT session for a compliance access request.
-- token_hash (SHA-256, 32 bytes) identifies the issued JWT without storing the
-- raw token. A partial unique index on (request_id) WHERE revoked_at IS NULL
-- enforces the "at most one active session per request" invariant at DB level —
-- prevents TOCTOU race between two concurrent POST /session calls.
--
-- RLS design:
--   INSERT  — open (application role inserts when issuing a session)
--   SELECT  — open (validation path reads token_hash for lookup in Story 5.6)
--   UPDATE  — open (revocation in Story 5.7 sets revoked_at)
--   DELETE  — USING (false): permanent audit trail; no deletion permitted.

CREATE TABLE compliance_sessions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id  UUID        NOT NULL REFERENCES compliance_requests(id),
    token_hash  BYTEA       NOT NULL,
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ
);

-- Partial unique index: only one active (non-revoked) session per request.
-- This enforces AC5 atomically at DB level — belt-and-suspenders with the
-- application-level SELECT check in PostSession.
CREATE UNIQUE INDEX compliance_sessions_active_request_idx
    ON compliance_sessions (request_id)
    WHERE revoked_at IS NULL;

-- Index to accelerate expiry worker scan (Story 5.5, AC9):
--   SELECT id FROM compliance_sessions WHERE expires_at <= NOW() AND revoked_at IS NULL
CREATE INDEX compliance_sessions_expires_at_idx
    ON compliance_sessions (expires_at)
    WHERE revoked_at IS NULL;

ALTER TABLE compliance_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY compliance_sessions_insert ON compliance_sessions
    FOR INSERT WITH CHECK (true);

CREATE POLICY compliance_sessions_select ON compliance_sessions
    FOR SELECT USING (true);

CREATE POLICY compliance_sessions_update ON compliance_sessions
    FOR UPDATE USING (true);

-- DELETE is permanently denied — compliance_sessions are an immutable audit trail.
-- Expiry is signalled by expires_at; explicit revocation sets revoked_at.
CREATE POLICY compliance_sessions_no_delete ON compliance_sessions
    FOR DELETE USING (false);

-- Story 6.6: User Role Assignment API — role_overrides table
-- Stores explicit role assignments that override or supplement OIDC JWT claims.
-- A row here grants the user the named role regardless of what the OIDC provider emits.
--
-- Design decisions:
--   - PRIMARY KEY (user_id, role): idempotent grant via ON CONFLICT DO UPDATE
--   - CHECK constraint: only the two supported system roles are allowed
--   - granted_at TIMESTAMPTZ: standard PostgreSQL timestamp (unlike the BIGINT epoch ms
--     used in the legacy users table — this is a new table with no legacy constraint)
--   - No FOREIGN KEY to users.user_id: allows pre-granting roles before first login,
--     and avoids cascade complications from user deletion workflows.
CREATE TABLE IF NOT EXISTS role_overrides (
    user_id    TEXT        NOT NULL,
    role       TEXT        NOT NULL,
    granted_by TEXT        NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role),
    CONSTRAINT role_overrides_role_check
        CHECK (role IN ('instance_admin', 'compliance_officer'))
);

-- Index to support fast lookup of all overrides for a given user (used by middleware
-- and the GET /admin/users endpoints that merge overrides into the roles field).
CREATE INDEX IF NOT EXISTS role_overrides_user_id_idx ON role_overrides (user_id);

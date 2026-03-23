-- gateway/migrations/000003_server_config.up.sql
-- server_config: key-value store for immutable instance configuration (ADR G8)
-- Primary use: server_name immutability; later: bootstrap_completed, oidc_issuer, etc.

CREATE TABLE server_config (
    key     TEXT   PRIMARY KEY,
    value   TEXT   NOT NULL,
    set_at  BIGINT NOT NULL
);

-- Enable Row Level Security
ALTER TABLE server_config ENABLE ROW LEVEL SECURITY;

-- CRITICAL: FORCE ensures the table owner (nebu user) is also subject to RLS.
-- Without FORCE, the owner bypasses the policy and can still UPDATE/DELETE.
-- The acceptance criteria requires UPDATE and DELETE to be rejected for the app user.
ALTER TABLE server_config FORCE ROW LEVEL SECURITY;

-- Allow all users (including owner under FORCE RLS) to read config values.
-- Without this policy, FORCE ROW LEVEL SECURITY causes default-deny on SELECT,
-- which would prevent the gateway from reading back the server_name after insertion.
CREATE POLICY config_read_all ON server_config FOR SELECT USING (true);

-- Only INSERT is allowed. No UPDATE, no DELETE policy → those operations are denied.
CREATE POLICY config_insert_only ON server_config FOR INSERT WITH CHECK (true);

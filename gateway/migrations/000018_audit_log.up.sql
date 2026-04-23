-- gateway/migrations/000018_audit_log.up.sql
-- audit_log: append-only compliance event log (Story 5.1)
-- RLS enforces immutability: INSERT allowed, UPDATE and DELETE denied for all roles
-- including the table owner (FORCE ROW LEVEL SECURITY).

CREATE TABLE audit_log (
    id            BIGSERIAL    PRIMARY KEY,
    event_time    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    actor_user_id TEXT         NOT NULL,
    action        TEXT         NOT NULL,
    target_type   TEXT,
    target_id     TEXT,
    metadata      JSONB,
    outcome       TEXT         NOT NULL,
    error_detail  TEXT
);

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;

-- FORCE ensures the table owner (nebu user) is also subject to RLS.
-- Without FORCE the owner bypasses the policy, which would make the
-- DELETE-denied acceptance test a false negative.
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;

-- Application role may insert audit events.
CREATE POLICY audit_log_insert_allow ON audit_log FOR INSERT WITH CHECK (true);

-- Application role may read audit events (required for compliance queries).
CREATE POLICY audit_log_select_allow ON audit_log FOR SELECT USING (true);

-- Explicitly deny UPDATE — the audit log is append-only.
CREATE POLICY audit_log_no_update ON audit_log FOR UPDATE USING (false);

-- Explicitly deny DELETE — audit records must be immutable until purged via
-- the privileged audit_log_purge function (see below), which runs as SECURITY DEFINER
-- bypassing RLS, so that the retention cleanup can delete old records without
-- violating the app-role constraint.
CREATE POLICY audit_log_no_delete ON audit_log FOR DELETE USING (false);

-- audit_log_purge: SECURITY DEFINER function used by the retention cleanup goroutine.
-- Running as the function owner (table owner) bypasses FORCE RLS, allowing DELETE
-- of expired rows without granting the app role a blanket DELETE privilege.
-- This is the chosen approach over a separate privileged DB role, because:
--   1. No additional DB role management required in Compose/Kubernetes.
--   2. The function is narrow (only deletes rows older than the specified interval).
--   3. The caller (Go) cannot inject arbitrary SQL — the retention_days parameter is typed INT.
--
-- Security hardening notes (CVE-2018-1058 class defenses):
--   - SET search_path = pg_catalog, public is MANDATORY for any SECURITY DEFINER
--     function. Without it, a role with CREATE on any schema earlier in the caller's
--     search_path can shadow `audit_log` or `make_interval` and hijack the function's
--     privileges. pg_catalog is pinned first so trusted catalog functions are resolved
--     even if a malicious public schema object exists.
--   - EXECUTE is revoked from PUBLIC and granted only to the nebu application role.
--     By default PostgreSQL grants EXECUTE on new functions to PUBLIC; combined with
--     SECURITY DEFINER this would let any connected role elevate to owner. The REVOKE
--     + explicit GRANT enforces the principle of least privilege.
CREATE OR REPLACE FUNCTION audit_log_purge(retention_days INT)
RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count BIGINT;
BEGIN
    IF retention_days IS NULL OR retention_days < 1 THEN
        RAISE EXCEPTION 'audit_log_purge: retention_days must be a positive integer (got %)', retention_days
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    DELETE FROM audit_log
    WHERE event_time < NOW() - make_interval(days => retention_days);
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$;

-- Lock down who can call the function. PUBLIC must not retain the default EXECUTE
-- privilege on a SECURITY DEFINER function, otherwise any role can trigger purges.
REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;

-- Migration 000028: re-assert audit_log_purge definition + grants under nebu_migrate.
--
-- Background (Story 7-16e):
-- audit_log_purge is a SECURITY DEFINER function. For SECURITY DEFINER elevation
-- to actually bypass FORCE ROW LEVEL SECURITY on audit_log (which has DELETE
-- USING (false) and UPDATE USING (false) policies), the function owner must
-- carry BYPASSRLS. nebu_migrate has BYPASSRLS (dev/postgres/init/01-roles.sql,
-- Story 5.29a).
--
-- Why this migration exists:
--   In fresh deployments (Docker Compose from scratch) golang-migrate runs every
--   migration as nebu_migrate, so audit_log_purge is already owned by nebu_migrate
--   from the moment migration 000018 (or 000024's ownership transfer) ran. In that
--   common case this migration is a benign idempotent re-assertion of the function
--   body and EXECUTE grants — and serves as an explicit on-record check that the
--   ownership invariant holds going forward.
--
-- IMPORTANT — what CREATE OR REPLACE FUNCTION does and does NOT do:
--   CREATE OR REPLACE FUNCTION updates the body/attributes of an existing function
--   but DOES NOT change its owner (PostgreSQL docs). Re-owning a function still
--   requires ALTER FUNCTION ... OWNER TO, executed by a role that owns the function
--   or a superuser. We deliberately avoid ALTER FUNCTION OWNER here because if a
--   legacy upgrade somehow left the function owned by nebu (superuser), nebu_migrate
--   cannot ALTER it anyway, and a hard migration failure is preferable to a silent
--   skip. In standard deployments nebu_migrate is already the owner, so the
--   CREATE OR REPLACE below succeeds without ownership churn.
--
-- Re-grant EXECUTE to nebu_app, nebu_migrate, and (legacy) nebu after the body
-- re-assertion. Migrations 000018, 000024 and 000025 already handle these grants;
-- repeating them here keeps this migration self-contained and audit-friendly.

CREATE OR REPLACE FUNCTION audit_log_purge(retention_days INT)
RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count BIGINT;
BEGIN
    IF retention_days IS NULL OR retention_days < 1 OR retention_days > 36500 THEN
        RAISE EXCEPTION 'audit_log_purge: retention_days must be between 1 and 36500 (got %)', retention_days
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    DELETE FROM audit_log
    WHERE event_time < NOW() - make_interval(days => retention_days);
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$;

-- Lock down EXECUTE: PUBLIC must not call a SECURITY DEFINER + BYPASSRLS function.
REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_migrate;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_app;
-- backward compat: keep grant to legacy nebu superuser role.
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;

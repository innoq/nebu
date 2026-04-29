-- gateway/migrations/000025_audit_log_event_time_trigger.up.sql
-- Story 5.29c: FB-51-02 — enforce event_time and retention upper-bound (AC6, AC7)
--
-- AC6: BEFORE INSERT trigger sets event_time := NOW() unconditionally, preventing
--      backdating or placement into the purge window via caller-supplied timestamps.
--
-- AC7 (SQL side): Update audit_log_purge to reject retention_days > 36500 (~100 years),
--      mirroring the Go-side guard in audit.RunCleanup. Prevents make_interval overflow.
--
-- Note: The trigger fires for all INSERTs, including those from test seeds that
-- provide explicit timestamps. Tests that require historical rows must use a
-- privileged connection that can set session_replication_role = replica to bypass
-- the trigger for seed data. The openPrivilegedDB helper in testhelpers_test.go
-- is the designated path for this in integration tests.

-- AC6: Trigger function — forces event_time to NOW() on every INSERT.
-- SECURITY DEFINER is NOT required here (the trigger runs as the table owner automatically).
CREATE OR REPLACE FUNCTION audit_log_event_time_force()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
  NEW.event_time := NOW();
  RETURN NEW;
END;
$$;

-- Attach the trigger to audit_log (BEFORE INSERT, per-row).
-- DROP + CREATE pattern avoids "trigger already exists" errors on re-apply.
DROP TRIGGER IF EXISTS audit_log_event_time_force_trigger ON audit_log;
CREATE TRIGGER audit_log_event_time_force_trigger
  BEFORE INSERT ON audit_log
  FOR EACH ROW
  EXECUTE FUNCTION audit_log_event_time_force();

-- AC7 (SQL side): Replace audit_log_purge with a version that also rejects
-- retention_days > 36500, matching the Go-side upper-bound guard.
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

-- Preserve privilege constraints from migration 000018.
REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;

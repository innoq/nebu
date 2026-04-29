-- gateway/migrations/000025_audit_log_event_time_trigger.down.sql
-- Reversal: remove event_time trigger and restore original audit_log_purge.

DROP TRIGGER IF EXISTS audit_log_event_time_force_trigger ON audit_log;
DROP FUNCTION IF EXISTS audit_log_event_time_force();

-- Restore original audit_log_purge from migration 000018 (without upper-bound).
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

REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;

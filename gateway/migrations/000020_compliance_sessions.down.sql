-- gateway/migrations/000020_compliance_sessions.down.sql
-- Reverses 000020_compliance_sessions.up.sql
-- Drops indexes before table (required when using DROP TABLE CASCADE is not desired).

DROP INDEX IF EXISTS compliance_sessions_expires_at_idx;
DROP INDEX IF EXISTS compliance_sessions_active_request_idx;
DROP TABLE IF EXISTS compliance_sessions;

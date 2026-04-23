-- gateway/migrations/000018_audit_log.down.sql
-- Reverse of 000018_audit_log.up.sql

DROP FUNCTION IF EXISTS audit_log_purge(INT);
DROP TABLE IF EXISTS audit_log;

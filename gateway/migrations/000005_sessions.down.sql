-- gateway/migrations/000005_sessions.down.sql
-- Drop sessions (no dependents at this stage).

DROP TABLE IF EXISTS sessions;

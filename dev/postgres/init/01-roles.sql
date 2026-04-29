-- dev/postgres/init/01-roles.sql
-- Runs as postgres superuser at first container start
-- (mounted via docker-entrypoint-initdb.d).
--
-- DEV ONLY: passwords are hardcoded here for development convenience.
-- Production deployments MUST use secrets management (Kubernetes Secrets,
-- Vault, AWS Secrets Manager) — never hardcode credentials in production.
--
-- Two roles:
--   nebu_migrate — owns all tables, runs golang-migrate at deploy/startup,
--                  is the OWNER of SECURITY DEFINER functions (e.g. audit_log_purge).
--                  Needs CREATEDB so it can create the DB on first run if needed.
--                  Has BYPASSRLS — REQUIRED so SECURITY DEFINER functions like
--                  audit_log_purge() can DELETE rows from audit_log despite
--                  FORCE ROW LEVEL SECURITY + DELETE USING (false). Without
--                  BYPASSRLS, the function elevates ownership but PostgreSQL
--                  still filters the DELETE to 0 rows silently — audit
--                  retention would be broken without warning.
--                  (Kassandra HIGH-1 fix, 2026-04-23.)
--   nebu_app    — runtime gateway connection. NOT superuser, NOT BYPASSRLS.
--                  Only SELECT/INSERT/UPDATE on tables owned by nebu_migrate.
--
-- Story 5.29a — AC1 (FB-51-01)

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nebu_migrate') THEN
    CREATE ROLE nebu_migrate LOGIN PASSWORD 'nebu_migrate_dev_pw' CREATEDB BYPASSRLS;
  ELSE
    -- Ensure existing nebu_migrate has BYPASSRLS (devs upgrading from an earlier
    -- 5-29a iteration may have it without). Idempotent.
    ALTER ROLE nebu_migrate BYPASSRLS;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nebu_app') THEN
    CREATE ROLE nebu_app LOGIN PASSWORD 'nebu_app_dev_pw'
      NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  ELSE
    -- Defense-in-depth: ensure existing nebu_app retains the safe defaults.
    ALTER ROLE nebu_app NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
  END IF;
END $$;

-- Grant connection privileges to the nebu database (created by POSTGRES_DB env var).
GRANT CONNECT ON DATABASE nebu TO nebu_app;
GRANT CONNECT ON DATABASE nebu TO nebu_migrate;

-- Grant USAGE on public schema so roles can reference objects in it.
GRANT USAGE ON SCHEMA public TO nebu_app;
GRANT USAGE ON SCHEMA public TO nebu_migrate;

-- PostgreSQL 15+ revoked the default CREATE on schema public from non-owners.
-- nebu_migrate runs golang-migrate at startup and must be able to CREATE
-- the schema_migrations table and all migration-scoped objects.
GRANT CREATE ON SCHEMA public TO nebu_migrate;

-- Default privileges: tables/sequences/functions created by nebu_migrate
-- will automatically be accessible to nebu_app at SELECT/INSERT/UPDATE/DELETE
-- level. The append-only invariant on audit_log is enforced by FORCE RLS plus
-- explicit REVOKE in migration 000024 — not by withholding DELETE globally,
-- because many normal-operation flows (session cleanup, JWT denylist rollover,
-- bootstrap_draft clear, etc.) need DELETE on other tables.
ALTER DEFAULT PRIVILEGES FOR ROLE nebu_migrate IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nebu_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nebu_migrate IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO nebu_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nebu_migrate IN SCHEMA public
  GRANT EXECUTE ON FUNCTIONS TO nebu_app;

-- gateway/migrations/000024_transfer_ownership_and_grants.up.sql
-- Story 5.29a — AC5: Transfer table/sequence ownership to nebu_migrate and
-- grant nebu_app the runtime privileges it needs on all existing objects.
--
-- Background:
--   Migrations 000001..000023 were created when nebu was the only role (superuser).
--   All tables/sequences were owned by nebu. With the role split (Story 5.29a):
--     - nebu_migrate: table owner, runs migrations, SECURITY DEFINER functions
--     - nebu_app:     runtime role, non-superuser, NOBYPASSRLS — subject to FORCE RLS
--
--   This migration runs as nebu_migrate (NEBU_DB_URL_MIGRATE), which must already
--   exist when this migration is applied (provisioned by dev/postgres/init/01-roles.sql
--   or the equivalent production secrets workflow).
--
-- RLS note on audit_log:
--   audit_log has FORCE ROW LEVEL SECURITY. After this migration:
--     - nebu_app is NOT the table owner → FORCE RLS applies → DELETE denied
--     - nebu_app CAN INSERT (INSERT policy allows it)
--     - nebu_app CAN SELECT (SELECT policy allows it)
--     - nebu_app CAN call audit_log_purge() (SECURITY DEFINER, runs as owner)
--   audit_log_purge is re-granted to nebu_app; the old grant to nebu is kept
--   for backward compatibility with any running instances that have not yet
--   been updated to use nebu_app.

-- ─── 1. Transfer table ownership ────────────────────────────────────────────
DO $$
DECLARE r record;
BEGIN
  FOR r IN (
    SELECT tablename
    FROM pg_tables
    WHERE schemaname = 'public'
      AND tableowner != 'nebu_migrate'
  ) LOOP
    EXECUTE format('ALTER TABLE public.%I OWNER TO nebu_migrate', r.tablename);
  END LOOP;
END $$;

-- ─── 2. Transfer sequence ownership ─────────────────────────────────────────
DO $$
DECLARE r record;
BEGIN
  FOR r IN (
    SELECT sequence_name
    FROM information_schema.sequences
    WHERE sequence_schema = 'public'
  ) LOOP
    EXECUTE format('ALTER SEQUENCE public.%I OWNER TO nebu_migrate', r.sequence_name);
  END LOOP;
END $$;

-- ─── 2a. Transfer function ownership (SECURITY DEFINER must NOT elevate to legacy nebu) ─
-- SECURITY DEFINER functions execute with the privileges of the function OWNER.
-- If audit_log_purge() stays owned by `nebu` (the legacy superuser), the elevation
-- target is the very superuser the role-split was meant to retire. Transfer ownership
-- of every function in `public` to nebu_migrate so SECURITY DEFINER elevates to a
-- non-superuser owner that can still bypass FORCE RLS as the table owner.
DO $$
DECLARE r record;
BEGIN
  FOR r IN (
    SELECT n.nspname AS schema_name,
           p.proname AS func_name,
           pg_get_function_identity_arguments(p.oid) AS args,
           pg_get_userbyid(p.proowner) AS owner_name
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public'
      AND pg_get_userbyid(p.proowner) <> 'nebu_migrate'
      -- Skip functions owned by extensions (pgcrypto, uuid-ossp) — their
      -- ownership is managed by the extension and ALTER FUNCTION OWNER fails
      -- with "must be owner of function". The functions we care about
      -- (audit_log_purge etc.) are non-extension and pass this filter.
      AND NOT EXISTS (
        SELECT 1 FROM pg_depend d
        WHERE d.classid = 'pg_proc'::regclass
          AND d.objid = p.oid
          AND d.deptype = 'e'
      )
  ) LOOP
    EXECUTE format('ALTER FUNCTION public.%I(%s) OWNER TO nebu_migrate',
                   r.func_name, r.args);
  END LOOP;
END $$;

-- ─── 3. Grant runtime privileges to nebu_app on all existing objects ─────────
-- SELECT / INSERT / UPDATE on all current tables (future tables covered by
-- ALTER DEFAULT PRIVILEGES in dev/postgres/init/01-roles.sql).
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO nebu_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO nebu_app;

-- ─── 4. Explicit privilege restrictions on audit_log (append-only) ───────────
-- nebu_app must NOT have DELETE or UPDATE on audit_log — enforced by FORCE RLS
-- (no DELETE/UPDATE policy exists), but also explicitly revoked as defense-in-depth.
REVOKE UPDATE, DELETE ON audit_log FROM nebu_app;

-- ─── 5. Re-grant EXECUTE on audit_log_purge to nebu_app ──────────────────────
-- audit_log_purge is SECURITY DEFINER (owner = nebu_migrate after step 2 above
-- transferred function ownership along with schema objects). nebu_app can call
-- the function for controlled purges (retention cleanup goroutine).
-- The old grant to nebu is kept for sessions that still connect as nebu.
REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_migrate;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_app;
-- backward compat: keep existing grant to legacy nebu superuser role
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;

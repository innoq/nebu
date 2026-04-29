-- Migration 000027: grant DELETE on all non-immutable tables to nebu_app.
--
-- Background: Story 5.29a (migration 000024) introduced the nebu_app / nebu_migrate
-- role split and granted nebu_app SELECT/INSERT/UPDATE on all tables. DELETE was
-- omitted because the only intentionally append-only table is audit_log.
--
-- Reality check: many normal-operation flows DELETE rows — sessions expire and
-- get purged, JWT denylist rolls over, bootstrap_draft is cleared after wizard
-- completion, password-reset tokens are consumed, etc. Without DELETE, the
-- gateway crashes on every cleanup path. The user-visible symptom that
-- triggered this fix:
--
--   ERROR claim selection: transaction failed
--   err="clear draft: ERROR: permission denied for table bootstrap_draft"
--
-- Fix: grant DELETE on every existing table except audit_log, and add DELETE
-- to the default privileges so future tables created by nebu_migrate are also
-- DELETE-able by nebu_app. audit_log immutability is preserved by the explicit
-- REVOKE below combined with the existing FORCE ROW LEVEL SECURITY policy.

-- ─── 1. Grant DELETE on every existing non-audit table ──────────────────────
DO $$
DECLARE r record;
BEGIN
  FOR r IN (
    SELECT tablename
    FROM pg_tables
    WHERE schemaname = 'public'
      AND tablename NOT IN ('audit_log', 'schema_migrations')
  ) LOOP
    EXECUTE format('GRANT DELETE ON public.%I TO nebu_app', r.tablename);
  END LOOP;
END $$;

-- ─── 2. Future-proof default privileges ─────────────────────────────────────
-- New tables created by nebu_migrate will automatically be DELETE-able by nebu_app.
ALTER DEFAULT PRIVILEGES FOR ROLE nebu_migrate IN SCHEMA public
  GRANT DELETE ON TABLES TO nebu_app;

-- ─── 3. Defense-in-depth: re-revoke DELETE on audit_log ─────────────────────
-- Migration 000024 already does this, but step 1 above might have re-granted
-- it via the loop. Re-revoke explicitly to make the invariant unambiguous.
REVOKE DELETE ON audit_log FROM nebu_app;

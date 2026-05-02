-- Down: revoke DELETE on all tables from nebu_app.
ALTER DEFAULT PRIVILEGES FOR ROLE nebu_migrate IN SCHEMA public
  REVOKE DELETE ON TABLES FROM nebu_app;

DO $$
DECLARE r record;
BEGIN
  FOR r IN (
    SELECT tablename FROM pg_tables WHERE schemaname = 'public'
  ) LOOP
    EXECUTE format('REVOKE DELETE ON public.%I FROM nebu_app', r.tablename);
  END LOOP;
END $$;

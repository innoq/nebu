-- dev/postgres/init/02-extensions.sql
-- Runs as postgres superuser at first container start
-- (mounted via docker-entrypoint-initdb.d).
--
-- Pre-installs PostgreSQL extensions that gateway/migrations/000001_init.up.sql
-- references. Required because nebu_migrate (the migration role) does not have
-- SUPERUSER and the managed-Postgres pattern in production also pre-installs
-- extensions out-of-band.
--
-- Migration 000001 uses CREATE EXTENSION IF NOT EXISTS so it is a no-op once
-- the extensions are already present.

\connect nebu

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- dev/postgres/init/02-extensions.sql
-- Runs as postgres superuser at first container start.
-- Pre-creates extensions required by nebu migrations so that
-- nebu_migrate (non-superuser) can run golang-migrate without permission errors.
--
-- pgcrypto: used by migration 000001 (gen_random_uuid, crypt functions)
-- uuid-ossp: used by migration 000001 (uuid_generate_v4)

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

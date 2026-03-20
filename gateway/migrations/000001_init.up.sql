-- gateway/migrations/000001_init.up.sql
-- Enable PostgreSQL extensions required by Nebu
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

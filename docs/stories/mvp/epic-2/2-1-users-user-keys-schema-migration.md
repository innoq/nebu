# Story 2.1: users + user_keys Schema Migration

Status: done

## Story

As a developer,
I want the core user and key tables created via migration,
so that user provisioning and cryptographic key storage have a schema foundation for all subsequent auth stories.

## Acceptance Criteria

1. **Given** migration `000004_users.up.sql`, **When** it runs, **Then** `users` table exists with columns: `user_id TEXT PRIMARY KEY`, `display_name_encrypted BYTEA`, `display_name_nonce BYTEA`, `avatar_url_encrypted BYTEA`, `avatar_url_nonce BYTEA`, `system_role TEXT NOT NULL DEFAULT 'user'`, `is_active BOOLEAN NOT NULL DEFAULT true`, `signing_key_id TEXT`, `encryption_key_id TEXT`, `created_at BIGINT NOT NULL`, `last_seen_at BIGINT`

2. **Given** the same migration, **When** it runs, **Then** `user_keys` table exists with columns: `key_id TEXT PRIMARY KEY`, `user_id TEXT NOT NULL REFERENCES users(user_id)`, `key_type TEXT NOT NULL`, `algorithm TEXT NOT NULL`, `public_key BYTEA NOT NULL`, `private_key BYTEA` (nullable — NULL after DSGVO deletion), `created_at BIGINT NOT NULL`, `deleted_at BIGINT`

3. **Given** the `system_role` column, **When** a value other than `'user'`, `'instance_admin'`, or `'compliance_officer'` is inserted, **Then** the database rejects it with a CHECK constraint violation

4. **Given** the `key_type` column, **When** a value other than `'signing'` or `'encryption'` is inserted, **Then** the database rejects it with a CHECK constraint violation

5. **Given** a corresponding `000004_users.down.sql`, **When** it runs, **Then** both `user_keys` and `users` tables are dropped cleanly

## Tasks / Subtasks

- [x] Create `gateway/migrations/000004_users.up.sql` (AC: #1, #2, #3, #4)
  - [x] CREATE TABLE users with all 11 columns (exact types per AC)
  - [x] ALTER TABLE users ADD CONSTRAINT users_system_role_check CHECK (system_role IN ('user', 'instance_admin', 'compliance_officer'))
  - [x] CREATE TABLE user_keys with all 8 columns, FK user_id REFERENCES users(user_id)
  - [x] ALTER TABLE user_keys ADD CONSTRAINT user_keys_key_type_check CHECK (key_type IN ('signing', 'encryption'))

- [x] Create `gateway/migrations/000004_users.down.sql` (AC: #5)
  - [x] DROP TABLE IF EXISTS user_keys (must precede users — FK constraint)
  - [x] DROP TABLE IF EXISTS users

- [x] Update `gateway/migrations/migrations_test.go` (testing standard)
  - [x] Add `"000004_users.up.sql"` and `"000004_users.down.sql"` to the files slice in `TestFS_ContainsExpectedMigrationFiles`

## Dev Notes

### Exact SQL: `000004_users.up.sql`

```sql
-- gateway/migrations/000004_users.up.sql
-- users: core user identity, system roles, encrypted PII column placeholders, and key references.
-- user_keys: Ed25519 signing + X25519 encryption keypairs per user (two rows per user).

CREATE TABLE users (
    user_id                   TEXT    PRIMARY KEY,
    display_name_encrypted    BYTEA,
    display_name_nonce        BYTEA,
    avatar_url_encrypted      BYTEA,
    avatar_url_nonce          BYTEA,
    system_role               TEXT    NOT NULL DEFAULT 'user',
    is_active                 BOOLEAN NOT NULL DEFAULT true,
    signing_key_id            TEXT,
    encryption_key_id         TEXT,
    created_at                BIGINT  NOT NULL,
    last_seen_at              BIGINT
);

ALTER TABLE users
    ADD CONSTRAINT users_system_role_check
    CHECK (system_role IN ('user', 'instance_admin', 'compliance_officer'));

CREATE TABLE user_keys (
    key_id      TEXT   PRIMARY KEY,
    user_id     TEXT   NOT NULL REFERENCES users(user_id),
    key_type    TEXT   NOT NULL,
    algorithm   TEXT   NOT NULL,
    public_key  BYTEA  NOT NULL,
    private_key BYTEA,
    created_at  BIGINT NOT NULL,
    deleted_at  BIGINT
);

ALTER TABLE user_keys
    ADD CONSTRAINT user_keys_key_type_check
    CHECK (key_type IN ('signing', 'encryption'));
```

### Exact SQL: `000004_users.down.sql`

```sql
-- gateway/migrations/000004_users.down.sql
-- Drop order: user_keys first (FK user_id → users.user_id), then users.

DROP TABLE IF EXISTS user_keys;
DROP TABLE IF EXISTS users;
```

### Project Structure Notes

**Files to create/modify:**

```
gateway/migrations/
  000004_users.up.sql          ← CREATE (new)
  000004_users.down.sql        ← CREATE (new)
  migrations_test.go           ← UPDATE: add 000004 file names to TestFS_ContainsExpectedMigrationFiles
  migrations.go                ← NO CHANGE (//go:embed *.sql picks up new files automatically)
```

**Do NOT touch:** Any Go application code, `docker-compose.yml`, `go.mod`, `go.sum`, `Makefile`, or any Elixir files.

### Critical Design Constraints

**No circular FK — `signing_key_id` and `encryption_key_id` are plain TEXT:**
`users.signing_key_id` and `users.encryption_key_id` intentionally have no `REFERENCES user_keys`. Reason: at user creation time (Story 2.12), the user row is inserted first; keypairs are generated and back-filled in Story 2.13. A circular FK (`users → user_keys → users`) would deadlock the initial INSERT.

**`private_key` nullable by design:**
DSGVO/GDPR cryptographic deletion (Story 5.7) sets `private_key = NULL` for both signing and encryption keys. Public keys are retained permanently — required for signature verification and audit trail integrity.

**`system_role` CHECK is exhaustive:**
Only `'user'`, `'instance_admin'`, `'compliance_officer'` are valid. Adding a new role in the future requires a new migration to ALTER the CHECK constraint.

**All timestamps are `BIGINT` (Unix milliseconds):**
Consistent with all existing migrations (000001–000003). The `TIMESTAMPTZ` exception applies only to `audit_log.event_time` (Story 5.1, compliance requirement). Do not use `TIMESTAMPTZ` or `TEXT` here.

**No RLS on `users` or `user_keys`:**
Unlike `server_config`, these tables require INSERT, UPDATE (profile edits, key NULL-out on DSGVO deletion), and DELETE of keys. No Row Level Security is applied.

**No `IF NOT EXISTS` on table creation:**
golang-migrate tracks versions via the `schema_migrations` table and never re-runs a migration. `IF NOT EXISTS` is only used in `000001_init.up.sql` for extensions.

### Migration Naming Pattern

6-digit zero-padded sequence: `000004` is next after `000003_server_config`.
The pattern `gateway/migrations/migrations.go` uses `//go:embed *.sql` — new files are automatically embedded at compile time, no changes to `migrations.go` required.

### References

- [Source: epics.md#Story-2.1] Authoritative AC — exact column names, types, nullability, CHECK constraints
- [Source: gateway/migrations/000003_server_config.up.sql] Pattern: file header comment, ALTER TABLE for CHECK constraints, no IF NOT EXISTS
- [Source: gateway/migrations/000003_server_config.down.sql] Pattern: `DROP TABLE IF EXISTS table_name;`
- [Source: gateway/migrations/migrations.go] `//go:embed *.sql` — no code change needed
- [Source: gateway/migrations/migrations_test.go] `TestFS_ContainsExpectedMigrationFiles` — add 000004 files to slice
- [Source: architecture.md#Migration-Patterns] golang-migrate pgx/v5 driver, embed pattern, Go Gateway as sole schema owner

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

_none_

### Completion Notes List

- Created `000004_users.up.sql`: `users` table (11 columns, BIGINT timestamps, no RLS) + `user_keys` table (8 columns, FK to users, nullable private_key for GDPR deletion) with CHECK constraints on `system_role` and `key_type`.
- Created `000004_users.down.sql`: drops `user_keys` first (FK dependency), then `users`.
- Updated `migrations_test.go`: added both 000004 files to `TestFS_ContainsExpectedMigrationFiles`. All Go unit tests pass (`ok github.com/nebu/nebu/migrations`).
- `migrations.go` untouched — `//go:embed *.sql` auto-picks up new files.
- [Code Review] Added missing 000002 and 000003 migration files to `TestFS_ContainsExpectedMigrationFiles` (pre-existing gap). All tests pass.

### File List

- gateway/migrations/000004_users.up.sql (created)
- gateway/migrations/000004_users.down.sql (created)
- gateway/migrations/migrations_test.go (modified)

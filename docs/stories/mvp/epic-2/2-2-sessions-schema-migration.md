# Story 2.2: sessions Schema Migration

Status: done

## Story

As a developer,
I want a sessions table created via migration,
so that Epic 4's `/sync` since-token checkpointing has its schema ready without requiring later migrations.

## Acceptance Criteria

1. **Given** migration `000005_sessions.up.sql`, **When** it runs, **Then** `sessions` table exists with columns: `session_id TEXT PRIMARY KEY`, `user_id TEXT NOT NULL REFERENCES users(user_id)`, `since_token TEXT`, `device_id TEXT NOT NULL`, `last_active_at BIGINT NOT NULL`, `created_at BIGINT NOT NULL`

2. **Given** an index on `user_id`, **When** the migration runs, **Then** `CREATE INDEX sessions_user_id_idx ON sessions (user_id)` is applied

3. **Given** a corresponding down migration, **When** it runs, **Then** the `sessions` table is dropped cleanly

## Tasks / Subtasks

- [x] Create `gateway/migrations/000005_sessions.up.sql` (AC: #1, #2)
  - [x] CREATE TABLE sessions with all 6 columns (exact types per AC)
  - [x] CREATE INDEX sessions_user_id_idx ON sessions (user_id)

- [x] Create `gateway/migrations/000005_sessions.down.sql` (AC: #3)
  - [x] DROP TABLE IF EXISTS sessions

- [x] Update `gateway/migrations/migrations_test.go` (testing standard)
  - [x] Add `"000005_sessions.up.sql"` and `"000005_sessions.down.sql"` to the files slice in `TestFS_ContainsExpectedMigrationFiles`

## Dev Notes

### Exact SQL: `000005_sessions.up.sql`

```sql
-- gateway/migrations/000005_sessions.up.sql
-- sessions: ETS + PostgreSQL hybrid since-token checkpointing for /sync recovery.
-- user_id FK to users ensures referential integrity.

CREATE TABLE sessions (
    session_id      TEXT    PRIMARY KEY,
    user_id         TEXT    NOT NULL REFERENCES users(user_id),
    since_token     TEXT,
    device_id       TEXT    NOT NULL,
    last_active_at  BIGINT  NOT NULL,
    created_at      BIGINT  NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
```

### Exact SQL: `000005_sessions.down.sql`

```sql
-- gateway/migrations/000005_sessions.down.sql
-- Drop sessions (no dependents at this stage).

DROP TABLE IF EXISTS sessions;
```

### Project Structure Notes

**Files to create/modify:**

```
gateway/migrations/
  000005_sessions.up.sql        ← CREATE (new)
  000005_sessions.down.sql      ← CREATE (new)
  migrations_test.go            ← UPDATE: add 000005 file names to TestFS_ContainsExpectedMigrationFiles
  migrations.go                 ← NO CHANGE (//go:embed *.sql picks up new files automatically)
```

**Do NOT touch:** Any Go application code, `docker-compose.yml`, `go.mod`, `go.sum`, `Makefile`, or any Elixir files.

### Critical Design Constraints

**Naming: `000005` follows `000004_users`:**
Strictly sequential 6-digit zero-padded naming. The `//go:embed *.sql` in `migrations.go` picks up new files automatically — no changes to `migrations.go` required.

**No `IF NOT EXISTS` on table creation:**
golang-migrate tracks versions via `schema_migrations` and never re-runs a migration. `IF NOT EXISTS` is only used in `000001_init.up.sql` for extensions. Do not add it here.

**All timestamps are `BIGINT` (Unix milliseconds):**
Consistent with all existing migrations (000001–000004). Do not use `TIMESTAMPTZ` or `TEXT`.

**`since_token` is nullable by design:**
A new session has no sync checkpoint yet — `since_token` is NULL until the first `/sync` response. The Elixir Session Manager GenServer writes this checkpoint on each successful sync poll, enabling incremental recovery after Elixir restarts.

**`user_id` FK references `users(user_id)`:**
The `users` table is created in migration `000004`. Down migration order is not relevant here (no tables depend on `sessions` yet), but the index must be created in the same migration as the table.

**No RLS, no CHECK constraints:**
Unlike `server_config` (RLS) and `users` (CHECK on `system_role`/`key_type`), `sessions` has no such requirements. Index on `user_id` only.

**`migrations_test.go` test update is required:**
`TestFS_ContainsExpectedMigrationFiles` asserts all migration files are embedded. Add both `000005_sessions.up.sql` and `000005_sessions.down.sql` to the `files` slice — this is a required testing standard, not optional.

### Previous Story Intelligence (Story 2.1)

Story 2.1 established these patterns — follow them exactly:

- File header: `-- gateway/migrations/NNNNN_name.up.sql` on line 1, then a descriptive comment line
- `ALTER TABLE ... ADD CONSTRAINT` is used for CHECK constraints — but Story 2.2 has no CHECK constraints, only an index
- Down migration: `DROP TABLE IF EXISTS table_name;` with a header comment
- `migrations_test.go` update: add both `.up.sql` and `.down.sql` file names to `files` slice in `TestFS_ContainsExpectedMigrationFiles`
- The code review for Story 2.1 found that previous migration files (000002, 000003) had been missing from the test — ensuring all files are listed is critical

### References

- [Source: epics.md#Story-2.2] Authoritative AC — exact column names, types, nullability, index definition
- [Source: architecture.md#G1] Since-token is for ETS + PostgreSQL hybrid sync recovery — `since_token TEXT` nullable
- [Source: gateway/migrations/000004_users.up.sql] Pattern: file header comment, CREATE TABLE, no IF NOT EXISTS
- [Source: gateway/migrations/000004_users.down.sql] Pattern: `DROP TABLE IF EXISTS table_name;`
- [Source: gateway/migrations/migrations.go] `//go:embed *.sql` — no code change needed
- [Source: gateway/migrations/migrations_test.go] `TestFS_ContainsExpectedMigrationFiles` — add 000005 files to slice
- [Source: architecture.md#Migration-Patterns] golang-migrate pgx/v5, embed pattern, Go Gateway as sole schema owner

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

_No blockers encountered._

### Completion Notes List

- Created `000005_sessions.up.sql`: CREATE TABLE sessions (6 columns, exact types per AC) + CREATE INDEX sessions_user_id_idx ON sessions (user_id)
- Created `000005_sessions.down.sql`: DROP TABLE IF EXISTS sessions
- Updated `migrations_test.go`: Added both 000005 file names to TestFS_ContainsExpectedMigrationFiles files slice
- All unit tests pass (`make test-unit-go`), no regressions

### File List

- gateway/migrations/000005_sessions.up.sql (created)
- gateway/migrations/000005_sessions.down.sql (created)
- gateway/migrations/migrations_test.go (modified)

### Change Log

- 2026-03-26: Implemented sessions schema migration (000005) — up/down SQL files and test coverage added

# Story 1.4: message_buffer + message_dead_letter Schema Migration

Status: done

## Story

As a developer,
I want the message buffer tables created via migration,
so that Epic 4's gateway resilience implementation has its schema ready without requiring further migrations.

## Acceptance Criteria

1. **Given** migration `000002_message_buffer.up.sql`, **when** it runs, **then** `message_buffer` table exists with columns: `id BIGSERIAL PRIMARY KEY`, `txn_id TEXT NOT NULL`, `room_id TEXT NOT NULL`, `sender TEXT NOT NULL`, `payload JSONB NOT NULL`, `received_at BIGINT NOT NULL`, `status TEXT NOT NULL DEFAULT 'pending'`, `retry_count SMALLINT NOT NULL DEFAULT 0`, `processed_at BIGINT`

2. **Given** the same migration, **when** it runs, **then** `message_dead_letter` table exists with columns: `id BIGSERIAL PRIMARY KEY`, `buffer_id BIGINT NOT NULL`, `txn_id TEXT NOT NULL`, `payload JSONB NOT NULL`, `failed_at BIGINT NOT NULL`, `last_error TEXT`

3. **Given** the `status` column on `message_buffer`, **when** an INSERT with a value other than `'pending'` or `'held'` is attempted, **then** the database rejects it with a CHECK constraint violation

4. **Given** a corresponding `000002_message_buffer.down.sql`, **when** it runs, **then** both tables are dropped cleanly (rollback support)

## Tasks / Subtasks

- [x] Task 1: Create `gateway/migrations/000002_message_buffer.up.sql` (AC: #1, #2, #3)
  - [x] CREATE TABLE message_buffer with all specified columns and CHECK constraint on status
  - [x] CREATE TABLE message_dead_letter with all specified columns
- [x] Task 2: Create `gateway/migrations/000002_message_buffer.down.sql` (AC: #4)
  - [x] DROP TABLE message_dead_letter (drop first — references message_buffer)
  - [x] DROP TABLE message_buffer
- [x] Task 3: Verify migration runs end-to-end
  - [x] `docker compose up postgres -d` then run gateway; confirm migration version 2 recorded
  - [x] Test DOWN migration works cleanly
  - [x] Test CHECK constraint rejects invalid status values

## Dev Notes

### What This Story Is (and Is NOT)

This story is **SQL-only**. No Go source code changes are needed. The migration framework from Story 1.3 already handles:
- Embedding SQL files via `gateway/migrations/migrations.go` (`//go:embed *.sql` picks up new files automatically)
- Running all pending migrations at startup via `gateway/internal/db/db.go:RunMigrations()`
- The `pgx5URL()` helper for driver URL conversion

**Do NOT modify:** `main.go`, `db.go`, `migrations.go`, `go.mod`, `go.sum`, or any existing migration file.

### Migration File — Up

**File:** `gateway/migrations/000002_message_buffer.up.sql`

```sql
-- gateway/migrations/000002_message_buffer.up.sql
-- Message buffer for gateway resilience (ADR G12/G13)
-- Used by Epic 4's drain strategy to hold messages during Core unavailability

CREATE TABLE message_buffer (
    id           BIGSERIAL PRIMARY KEY,
    txn_id       TEXT      NOT NULL,
    room_id      TEXT      NOT NULL,
    sender       TEXT      NOT NULL,
    payload      JSONB     NOT NULL,
    received_at  BIGINT    NOT NULL,
    status       TEXT      NOT NULL DEFAULT 'pending',
    retry_count  SMALLINT  NOT NULL DEFAULT 0,
    processed_at BIGINT,
    CONSTRAINT message_buffer_status_check CHECK (status IN ('pending', 'held'))
);

CREATE TABLE message_dead_letter (
    id         BIGSERIAL PRIMARY KEY,
    buffer_id  BIGINT    NOT NULL,
    txn_id     TEXT      NOT NULL,
    payload    JSONB     NOT NULL,
    failed_at  BIGINT    NOT NULL,
    last_error TEXT
);
```

**Critical notes:**
- `received_at` and `failed_at` are `BIGINT` (Unix milliseconds), NOT `TIMESTAMPTZ` — follows architecture timestamp decision
- `processed_at` is nullable `BIGINT` — no `NOT NULL` constraint
- `buffer_id` references `message_buffer(id)` logically but **no FOREIGN KEY constraint** — rows in `message_buffer` may be deleted after drain; the dead-letter store must survive independently
- No explicit indexes in this story — drain queries (`ORDER BY received_at WHERE status = 'pending'`) will get an index in Story 1-16 (drain strategy implementation)
- `status` CHECK constraint name must be explicit: `message_buffer_status_check` (aids error messages)

### Migration File — Down

**File:** `gateway/migrations/000002_message_buffer.down.sql`

```sql
-- gateway/migrations/000002_message_buffer.down.sql
DROP TABLE IF EXISTS message_dead_letter;
DROP TABLE IF EXISTS message_buffer;
```

**Drop order matters:** `message_dead_letter` first (it references `message_buffer`), then `message_buffer`. Even without a FK constraint, this ordering is semantically correct and safe.

### Migration Naming Convention

golang-migrate format: `{version}_{title}.up.sql` / `{version}_{title}.down.sql`

Use **6-digit zero-padded version**:
- ✅ `000002_message_buffer.up.sql`
- ❌ `002_message_buffer.up.sql`
- ❌ `2_message_buffer.up.sql`

Consistent with existing `000001_init.up.sql`.

### How the Embed Works

`gateway/migrations/migrations.go` uses `//go:embed *.sql` — adding new `.sql` files in this directory is **sufficient**; no code changes needed. The embed FS is rebuilt at compile time.

### Critical: pgx5 URL Scheme (from Story 1.3)

The `db.go:pgx5URL()` helper already converts `postgres://` → `pgx5://`. The golang-migrate pgx/v5 driver registers as `pgx5`, not `postgres`. This is already handled — do NOT change anything in `db.go`.

### Docker Build System

All commands run in Docker containers. Never run Go commands locally.

To verify migration locally:
```bash
# 1. Start postgres
docker compose up postgres -d

# 2. Run the gateway (applies migrations)
NEBU_DB_URL="postgres://nebu:nebu_dev_password@localhost:5432/nebu?sslmode=disable" \
  docker run --rm --network host \
    -e NEBU_DB_URL="postgres://nebu:nebu_dev_password@localhost:5432/nebu?sslmode=disable" \
    -v $(PWD)/gateway:/workspace -w /workspace \
    golang:1.26-alpine go run ./cmd/gateway/

# 3. Verify tables exist
docker compose exec postgres psql -U nebu -d nebu \
  -c "\d message_buffer" \
  -c "\d message_dead_letter" \
  -c "SELECT * FROM schema_migrations ORDER BY version;"
# Expected: versions 1 and 2, dirty=false

# 4. Test CHECK constraint
docker compose exec postgres psql -U nebu -d nebu \
  -c "INSERT INTO message_buffer (txn_id, room_id, sender, payload, received_at, status) \
      VALUES ('txn1', '!room:server', '@user:server', '{}', 0, 'invalid');"
# Expected: ERROR: new row for relation "message_buffer" violates check constraint "message_buffer_status_check"
```

### Go Module Import Path

`github.com/nebu/nebu` (from `gateway/go.mod`). No imports needed for this story — pure SQL.

### Architecture Context

This story implements tables for the Gateway Resilience mechanism (ADR G12):

| Gateway State | Condition | Behavior |
|---|---|---|
| GREEN | gRPC EventBus stream active | Normal operation |
| YELLOW | Stream broken, unary fallback succeeds | Write to `message_buffer` proactively |
| RED | Stream AND unary fail | All writes to `message_buffer`, return 200 OK + event_id |
| GREEN after RED | Stream re-established | Drain worker processes `message_buffer` FIFO |

The `status` values:
- `'pending'` — buffered, awaiting drain
- `'held'` — temporarily held (e.g., during rate-limited drain)

The drain strategy (Story 1-16, Epic 4) queries: `SELECT ... FROM message_buffer WHERE status = 'pending' ORDER BY received_at LIMIT N`.

Dead-letter flow: after `retry_count >= 3` (configurable default), message is moved to `message_dead_letter` and a Prometheus metric is emitted.

### Previous Story Intelligence (Story 1.3)

**Critical findings to apply:**

1. **golang-migrate v4.19.1 used** — compatible with Go 1.26. No version changes needed.
2. **pgx/v5 driver registers as `pgx5`** — URL scheme must be `pgx5://`. Already handled by `pgx5URL()` helper in `db.go`.
3. **`000001_init.up.sql` enables `pgcrypto` and `uuid-ossp`** — do NOT re-enable these in migration 2; they're already active.
4. **`//go:embed *.sql` in `migrations.go`** — new `.sql` files are automatically picked up; no code change needed.
5. **Migration version 1** is already recorded in `schema_migrations`; migration 2 will append cleanly.

### Project Structure Notes

**Files to create (only these two):**
```
gateway/migrations/
  000002_message_buffer.up.sql    ← new
  000002_message_buffer.down.sql  ← new
```

**Files NOT to touch:**
- `gateway/migrations/migrations.go` (embed package — no changes needed)
- `gateway/migrations/migrations_test.go` (existing tests — do not regress)
- `gateway/migrations/000001_init.up.sql` / `000001_init.down.sql`
- `gateway/internal/db/db.go`
- `gateway/cmd/gateway/main.go`
- `gateway/go.mod` / `gateway/go.sum`
- `docker-compose.yml`
- All `core/` files
- All `media/` files

### References

- message_buffer schema: [Source: architecture.md — "Go Gateway Status-Modell (G12)" + SQL schema definition]
- Drain strategy context: [Source: architecture.md — "Buffer-Drain-Strategie (G13)"]
- Acceptance Criteria: [Source: epics.md — Epic 1, Story 1.4]
- Migration naming format: [Source: epics.md — Story 1.3 AC, confirmed by existing `000001_init.up.sql`]
- Migration embed pattern: [Source: `gateway/migrations/migrations.go` — `//go:embed *.sql`]
- pgx5 URL requirement: [Source: `gateway/internal/db/db.go` — `pgx5URL()` helper]
- Timestamp as BIGINT: [Source: architecture.md — timestamp decision, all timestamps are Unix milliseconds BIGINT]
- Go module path: [Source: `gateway/go.mod` — `github.com/nebu/nebu`]
- No FK constraint: [Source: architecture.md G13 — dead-letter survives buffer deletion]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

No issues encountered. Pure SQL migration, framework handles embed automatically via `//go:embed *.sql`.

### Completion Notes List

- Created `000002_message_buffer.up.sql` with `message_buffer` (BIGSERIAL PK, txn_id, room_id, sender, payload JSONB, received_at BIGINT, status TEXT with CHECK constraint `message_buffer_status_check`, retry_count SMALLINT, processed_at BIGINT nullable) and `message_dead_letter` (BIGSERIAL PK, buffer_id BIGINT, txn_id, payload JSONB, failed_at BIGINT, last_error TEXT nullable) — no FK constraint by design (ADR G13).
- Created `000002_message_buffer.down.sql` dropping `message_dead_letter` first, then `message_buffer`.
- Verified via Docker: gateway applies migration cleanly to version 2 (dirty=false).
- CHECK constraint `message_buffer_status_check` correctly rejects status='invalid' with `violates check constraint` error.
- DOWN migration verified: both tables dropped cleanly, re-up to version 2 succeeds.
- All existing Go unit tests pass (no regressions): `ok github.com/nebu/nebu/migrations`.

### File List

- gateway/migrations/000002_message_buffer.up.sql (new)
- gateway/migrations/000002_message_buffer.down.sql (new)

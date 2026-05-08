---
status: review
epic: 11
story: 1
security_review: not-needed
matrix: false
ui: false
---

# Story 11.1: DB Migration + Search Index

Status: review

## Story

As a developer,
I want the database to have a full-text search index on message content,
so that the search handler (Story 11.3+) can query it efficiently.

**Size:** M

---

## Acceptance Criteria

**AC1 — Column + GIN index (up migration):**
Given ADR-010 chose tsvector (the accepted decision),
When migration `000042_search_vector.up.sql` runs,
Then column `search_vector tsvector` is added to the `events` table with a GIN index created via `CREATE INDEX CONCURRENTLY`.

**AC2 — Automatic population via trigger:**
Given the index exists,
When a new message event is inserted,
Then `search_vector` is populated automatically via a trigger using `pg_catalog.simple` text search configuration and source data from `content->>'body'` (JSONB extraction).

> **Key technical note:** `tsvector_update_trigger` only works with plain TEXT columns. Since `body` lives inside the JSONB `content` column (`content->>'body'`), you MUST write a custom `BEFORE INSERT OR UPDATE` PL/pgSQL trigger function that executes:
> ```sql
> NEW.search_vector := to_tsvector('pg_catalog.simple', coalesce(NEW.content->>'body', ''));
> RETURN NEW;
> ```
> Do NOT attempt to use `tsvector_update_trigger` directly — it will not accept a JSONB column as source.

**AC3 — Backfill of existing rows:**
Given the migration runs on a DB with existing messages,
When backfill is completed (as part of the up migration),
Then all existing events with `event_type = 'm.room.message'` have a non-null `search_vector`.

> The backfill UPDATE runs as part of the up migration, after the column is added and the trigger is created. Only `m.room.message` events carry a `body` field — state events and other types have no body to index. Non-message events get `search_vector = to_tsvector('pg_catalog.simple', '')` (empty tsvector) — do NOT leave them NULL; the trigger handles new inserts uniformly.
>
> **Performance:** Run the backfill as a batched UPDATE or a single bulk UPDATE. For MVP a single-statement `UPDATE events SET search_vector = to_tsvector('pg_catalog.simple', coalesce(content->>'body', '')) WHERE event_type = 'm.room.message'` is acceptable. The GIN index is created CONCURRENTLY to avoid locking.

**AC4 — Down migration (clean rollback):**
Given migration `000042_search_vector.down.sql` runs,
When it completes,
Then the `search_vector` column, the GIN index, and the trigger function are removed cleanly (no partial objects left).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

These tests belong in `gateway/migrations/` as a new file `migrations_042_test.go`, following the established pattern of `migrations_039_test.go`.

**1. `TestMigration042_FilesExist` — FS presence test (ExUnit equivalent: Go migrations package test)**
- Given: the gateway/migrations embedded FS
- When: both `000042_search_vector.up.sql` and `000042_search_vector.down.sql` are opened
- Then: no error and both files are non-empty

**2. `TestMigration042_ColumnAndIndexExist` — Integration test (requires `NEBU_TEST_DB_URL`)**
- Given: a clean DB after all migrations run up to 000042
- When: `SELECT column_name FROM information_schema.columns WHERE table_name = 'events' AND column_name = 'search_vector'` is executed
- Then: exactly one row is returned

  And: `SELECT indexname FROM pg_indexes WHERE tablename = 'events' AND indexname = 'events_search_vector_gin_idx'` returns one row

**3. `TestMigration042_TriggerPopulatesSearchVector` — Integration test**
- Given: a clean DB after migration 000042 runs
- When: `INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts) VALUES ('$ev1', '!r:test', '@u:test', 'm.room.message', '{"msgtype":"m.text","body":"hello world"}', 1000)` is executed
- Then: `SELECT search_vector FROM events WHERE event_id = '$ev1'` returns a non-null, non-empty tsvector that matches `to_tsquery('pg_catalog.simple', 'hello')` (i.e., `search_vector @@ to_tsquery('pg_catalog.simple', 'hello')` is true)

**4. `TestMigration042_BackfillPopulatesExistingRows` — Integration test**
- Given: a DB with pre-existing `m.room.message` events inserted BEFORE migration 000042 runs (use `golang-migrate` to apply up to 000041, seed data, then apply 000042)
- When: migration 000042 runs (backfill UPDATE executes)
- Then: all pre-seeded `m.room.message` events have `search_vector IS NOT NULL`

  And: `search_vector @@ to_tsquery('pg_catalog.simple', seeded_keyword)` is true for the seeded message

**5. `TestMigration042_DownMigration` — Integration test**
- Given: DB with migration 000042 applied
- When: migration 000042 is rolled back (down migration applied)
- Then: `search_vector` column no longer exists in `events`

  And: GIN index `events_search_vector_gin_idx` no longer exists

  And: trigger `events_search_vector_trigger` no longer exists

  And: trigger function `events_search_vector_update` no longer exists

**Persistence-Strategy note:** This story is stateless from an Elixir/GenServer perspective — pure PostgreSQL DDL + trigger. No crash/restart test required.

---

## Implementation Notes

### Migration numbering

- Next available number: **000042** (latest: `000041_sync_tokens_per_device`)
- Files: `gateway/migrations/000042_search_vector.up.sql` and `gateway/migrations/000042_search_vector.down.sql`

### Current `events` table schema (as of migration 000038)

```sql
CREATE TABLE events (
    event_id         TEXT    PRIMARY KEY,
    room_id          TEXT    NOT NULL REFERENCES rooms(room_id),
    sender           TEXT    NOT NULL,
    event_type       TEXT    NOT NULL,
    content          JSONB   NOT NULL,
    origin_server_ts BIGINT  NOT NULL,
    signatures       JSONB,
    state_key        TEXT    -- added in 000038
);
```

The `body` field is **not** a dedicated column — it is the `"body"` key inside the `content` JSONB object. Matrix message events (`m.room.message`) have `content = {"msgtype": "m.text", "body": "hello"}`.

### Up migration template

```sql
-- gateway/migrations/000042_search_vector.up.sql
-- Story 11.1: Add tsvector FTS column + GIN index + trigger to events table.
-- ADR-010: PostgreSQL native tsvector/tsquery approach selected (accepted 2026-05-08).
-- Configuration: pg_catalog.simple — language-agnostic, no stemming, multilingual-safe.

-- Step 1: Add the search_vector column (nullable during migration; trigger fills it on insert).
ALTER TABLE events ADD COLUMN search_vector tsvector;

-- Step 2: Custom trigger function — extracts body from JSONB content.
-- NOTE: tsvector_update_trigger() only works on plain TEXT columns; since body lives
-- inside the JSONB content column, a custom PL/pgSQL function is required.
CREATE OR REPLACE FUNCTION events_search_vector_update()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
  NEW.search_vector := to_tsvector('pg_catalog.simple',
    coalesce(NEW.content->>'body', ''));
  RETURN NEW;
END;
$$;

-- Step 3: Attach trigger (BEFORE INSERT OR UPDATE, per-row).
DROP TRIGGER IF EXISTS events_search_vector_trigger ON events;
CREATE TRIGGER events_search_vector_trigger
  BEFORE INSERT OR UPDATE OF content ON events
  FOR EACH ROW
  EXECUTE FUNCTION events_search_vector_update();

-- Step 4: GIN index for efficient FTS queries.
-- CONCURRENTLY avoids table lock — safe for production migration.
-- NOTE: CREATE INDEX CONCURRENTLY cannot run inside a transaction block.
-- golang-migrate will run this migration outside a transaction (use -- migrate:disable-transation
-- comment if needed, or ensure migration runner supports CONCURRENTLY).
CREATE INDEX CONCURRENTLY IF NOT EXISTS events_search_vector_gin_idx
  ON events USING GIN (search_vector);

-- Step 5: Backfill existing m.room.message events.
-- Only message events have a body; state events and other types get an empty tsvector.
UPDATE events
  SET search_vector = to_tsvector('pg_catalog.simple',
    coalesce(content->>'body', ''))
  WHERE event_type = 'm.room.message';
```

> **CRITICAL — `CREATE INDEX CONCURRENTLY` and transactions:**
> `CREATE INDEX CONCURRENTLY` cannot run inside a transaction block. `golang-migrate` wraps each migration in a transaction by default. You must add the `-- +migrate no transaction` directive (or the golang-migrate equivalent: see [golang-migrate docs on `no-lock` / disabling transactions](https://github.com/golang-migrate/migrate/blob/master/database/postgres/README.md#no-transaction)). The exact directive to add at the top of the SQL file is:
> ```
> -- +goose NO TRANSACTION
> ```
> Wait — this project uses `golang-migrate`, NOT `goose`. For `golang-migrate`, the way to disable transaction wrapping is to use a **separate migration file** strategy or to accept that CONCURRENTLY cannot be used inside a transaction. The established project pattern (search `000024` for examples) shows migrations **do** run in transactions. **Resolution:** Either:
> - Use `CREATE INDEX IF NOT EXISTS events_search_vector_gin_idx ON events USING GIN (search_vector)` (without CONCURRENTLY) to stay within the transaction, OR
> - Use `CREATE INDEX CONCURRENTLY` in a migration that is run outside a transaction by adding a magic comment — check whether the project's migration runner supports this.
>
> **Dev agent decision:** Check `gateway/internal/db/` or `gateway/cmd/gateway/main.go` for how `golang-migrate` is invoked. If no existing migration uses CONCURRENTLY, default to the non-concurrent form to match the AC requirement that says "GIN index `CREATE INDEX CONCURRENTLY`". **The AC requires CONCURRENTLY** — ensure the migration runner supports it. If golang-migrate is invoked with `WithDatabaseInstance` in a way that wraps in transactions, you will need `-- disable-tx` at the file level.

### Down migration template

```sql
-- gateway/migrations/000042_search_vector.down.sql
-- Rollback Story 11.1: remove search_vector column, GIN index, and trigger from events.

DROP TRIGGER IF EXISTS events_search_vector_trigger ON events;
DROP FUNCTION IF EXISTS events_search_vector_update();
DROP INDEX IF EXISTS events_search_vector_gin_idx;
ALTER TABLE events DROP COLUMN IF EXISTS search_vector;
```

### Trigger vs. `tsvector_update_trigger`

`tsvector_update_trigger` is a PostgreSQL built-in that takes column names as string arguments. It reads the value from those columns **by name** and requires them to be `text` / `varchar` types. It **cannot** read `content->>'body'` from a JSONB column. This is documented in the PostgreSQL manual (§12.4.3).

The custom `events_search_vector_update()` function above is the correct pattern — identical to how the `audit_log_event_time_force_trigger` is implemented in `000025_audit_log_event_time_trigger.up.sql` (project reference).

### `migrations_test.go` update required

Add `000042_search_vector.up.sql` and `000042_search_vector.down.sql` to `TestFS_ContainsExpectedMigrationFiles` in `gateway/migrations/migrations_test.go` — same as every migration pair before it.

### Integration test helpers

- Reuse `openPrivilegedDB` / `openAppRoleDB` from `gateway/migrations/testhelpers_test.go` (established in Story 5.1)
- Build tag: `//go:build integration` (same as migration integration tests)
- Test file: `gateway/migrations/migrations_042_test.go`
- The `NEBU_TEST_DB_URL` env var provides the test DB connection

### golang-migrate invocation

Check `gateway/cmd/gateway/main.go` for how migrations are run. The relevant section is the `runMigrations` / `golang-migrate` invocation. If it wraps each file in a transaction, `CREATE INDEX CONCURRENTLY` will fail with:
```
ERROR: CREATE INDEX CONCURRENTLY cannot run inside a transaction block
```
The standard golang-migrate Postgres driver supports the `--no-lock` option or parsing a special comment. Investigate before writing the final migration — if transactions cannot be disabled, use non-concurrent `CREATE INDEX` instead and document the tradeoff in a comment.

---

## Files to Create / Modify

| File | Action |
|---|---|
| `gateway/migrations/000042_search_vector.up.sql` | NEW — ADD COLUMN + trigger function + trigger + GIN index + backfill |
| `gateway/migrations/000042_search_vector.down.sql` | NEW — DROP trigger + function + index + column |
| `gateway/migrations/migrations_test.go` | MODIFY — add `000042_search_vector.up.sql` + `.down.sql` to `TestFS_ContainsExpectedMigrationFiles` |
| `gateway/migrations/migrations_042_test.go` | NEW — red-phase acceptance tests (AC1–AC5) |

No Go handler code, no Elixir code, no gRPC changes. Pure DDL migration story.

---

## Context: Epic 11

Epic 11 implements `POST /_matrix/client/v3/search` end-to-end:

| Story | Dependency |
|---|---|
| **11.1 (this)** | DB schema foundation — must be done first |
| 11.2 | Membership enforcement query — depends on 11.1 index |
| 11.3 | Elixir Core `SearchMessages` gRPC handler — depends on 11.1 + 11.2 |
| 11.4 | Gateway `POST /search` handler — depends on 11.3 |
| 11.5 | Rate limiting on search — depends on 11.4 |
| 11.6 | Gherkin E2E test — depends on all of the above |

ADR-010 is **accepted** (2026-05-08). Do not re-evaluate pgvector — that is out of scope for this story and epic.

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

(none)

### Completion Notes List

- Created `000042_search_vector.up.sql`: ADD COLUMN search_vector tsvector, custom PL/pgSQL trigger function `events_search_vector_update()` extracting `content->>'body'` via COALESCE, trigger `events_search_vector_trigger` BEFORE INSERT OR UPDATE OF content, plain CREATE INDEX (not CONCURRENTLY — golang-migrate wraps in transaction), backfill UPDATE for all events (message events get body indexed, state events get empty tsvector, never NULL).
- Created `000042_search_vector.down.sql`: DROP TRIGGER, DROP FUNCTION, DROP INDEX, DROP COLUMN — all idempotent with IF EXISTS.
- Fixed MINOR-1 in `migrations_042_integration_test.go`: Added assertion in `TestMigration042_BackfillPopulatesExistingRows` that the pre-seeded state event (`$backfill-042-state`) has `search_vector IS NOT NULL` after backfill.
- Fixed MINOR-2 in `migrations_042_integration_test.go`: Added second INSERT of a non-message event (`m.room.name`) in `TestMigration042_TriggerPopulatesSearchVector` with assertion that its `search_vector IS NOT NULL` (empty tsvector acceptable).
- `migrations_test.go` already contained the 000042 file entries (added by ATDD agent) — no changes required.
- All unit tests pass: `make test-unit-go` green (19/19 packages).

### File List

- gateway/migrations/000042_search_vector.up.sql (NEW)
- gateway/migrations/000042_search_vector.down.sql (NEW)
- gateway/migrations/migrations_042_integration_test.go (MODIFIED — MINOR-1 + MINOR-2 fixes)
- docs/stories/phase2/epic-11/11-1-db-migration-search-index.md (MODIFIED — status, completion notes, file list)

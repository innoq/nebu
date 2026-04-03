# Story 4.2: Room GenServer — Lifecycle (create, join, leave)

Status: done

## Story

As a Core developer,
I want `Nebu.Room.Server` to manage room membership state with PostgreSQL persistence,
so that Rooms can be created, users can join and leave, and the current member list is always available in memory.

## Acceptance Criteria

1. `Nebu.Room.Server` state is `%{room_id: String.t(), members: MapSet.t(), power_levels: map(), created_at: DateTime.t()}`.
2. `init/1` loads existing membership from PostgreSQL (`SELECT user_id FROM room_members WHERE room_id = $1`). If the room does not exist in the DB, it inserts a new row into `rooms` and returns empty members.
3. Handles the following `call` messages:
   - `:get_state` → returns full state map
   - `{:join, user_id}` → adds `user_id` to `members`, inserts into `room_members` DB table, returns `:ok` or `{:error, :already_member}`
   - `{:leave, user_id}` → removes from `members`, soft-deletes from `room_members` (sets `left_at = NOW()`), returns `:ok` or `{:error, :not_member}`
4. On any DB write error, the GenServer returns `{:error, reason}` and does **not** update in-memory state (fail-safe, no partial state).
5. `mix test --warnings-as-errors` passes. Unit tests cover:
   - `join/2` idempotency: second join returns `{:error, :already_member}`
   - `leave/2` from non-member returns `{:error, :not_member}`
   - State recovery from DB on `init/1` (start a room, join user, stop room, restart — member still present)
6. A new SQL migration (`000009_rooms.up.sql`) creates the `rooms` and `room_members` tables.
7. `Nebu.Room.RoomSupervisor.start_room/1` still works (no regression to Story 4-1).

---

## Tasks / Subtasks

- [x] Add SQL migration for `rooms` and `room_members` tables (AC: #6)
  - [x] Create `gateway/migrations/000009_rooms.up.sql`
  - [x] Create `gateway/migrations/000009_rooms.down.sql`
  - [x] `rooms` table: `room_id TEXT PRIMARY KEY, name TEXT, visibility TEXT, created_at BIGINT NOT NULL, archived_at BIGINT`
  - [x] `room_members` table: `room_id TEXT NOT NULL REFERENCES rooms(room_id), user_id TEXT NOT NULL REFERENCES users(user_id), joined_at BIGINT NOT NULL, left_at BIGINT, PRIMARY KEY (room_id, user_id)`

- [x] Add `nebu_db` dependency to `room_manager/mix.exs` (AC: #2, #3)
  - [x] Add `{:nebu_db, in_umbrella: true}` to `deps`

- [x] Implement `Nebu.Room.Server` GenServer with full lifecycle (AC: #1–#4)
  - [x] Update `core/apps/room_manager/lib/nebu/room/server.ex`
  - [x] Define state struct: `%{room_id: String.t(), members: MapSet.t(), power_levels: map(), created_at: DateTime.t()}`
  - [x] `init/1`: query `room_members WHERE room_id = $1`; if room not found → insert into `rooms`; populate `members` MapSet
  - [x] `handle_call(:get_state, ...)` → returns full state
  - [x] `handle_call({:join, user_id}, ...)` → check MapSet first (`:already_member`), write DB, update state
  - [x] `handle_call({:leave, user_id}, ...)` → check MapSet first (`:not_member`), soft-delete DB, update state
  - [x] DB errors → return `{:error, reason}`, do NOT update MapSet

- [x] Add public API functions (convenience wrappers) to `Nebu.Room.Server`
  - [x] `get_state(room_id)` — `GenServer.call(via(room_id), :get_state)`
  - [x] `join(room_id, user_id)` — `GenServer.call(via(room_id), {:join, user_id})`
  - [x] `leave(room_id, user_id)` — `GenServer.call(via(room_id), {:leave, user_id})`
  - [x] `defp via(room_id)` helper: `{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}`

- [x] Write unit tests (AC: #5, #7)
  - [x] Use Mox or a test double for DB calls (do NOT require a real PostgreSQL in unit tests)
  - [x] Test: `:get_state` returns `%{room_id, members, power_levels, created_at}`
  - [x] Test: `join` happy path adds user to MapSet
  - [x] Test: `join` idempotency returns `{:error, :already_member}` on second call
  - [x] Test: `leave` happy path removes user from MapSet
  - [x] Test: `leave` from non-member returns `{:error, :not_member}`
  - [x] Test: DB write error → state unchanged, returns `{:error, reason}`
  - [x] Test: `init/1` restores members from DB (stub DB returning pre-seeded rows)
  - [x] Regression: existing `Nebu.Room.RoomSupervisor` tests still pass (start_room, lookup_room)

- [x] Run `mix test --warnings-as-errors` (AC: #5)

---

## Dev Notes

### Critical: Correct Module Names (From Story 4-1 Learning)

The epics.md acceptance criteria use `RoomManager.RoomServer` — this is **WRONG** for this codebase.

All modules follow `Nebu.{Domain}.{Name}` pattern. Use:

| Epic Spec Name | Correct Name |
|---|---|
| `RoomManager.RoomServer` | `Nebu.Room.Server` (already exists) |
| `RoomManager.Registry` | `Nebu.Room.Registry` |
| `RoomManager.Supervisor` | `Nebu.Room.HordeSupervisor` |

**File to modify:** `core/apps/room_manager/lib/nebu/room/server.ex` — **do NOT create a new file, replace the stub**.

### State Design

```elixir
# State struct (Story 4-2 adds members, power_levels, created_at to stub's %{room_id: room_id})
%{
  room_id: "!abc123:server.name",
  members: MapSet.new(["@user1:server.name"]),
  power_levels: %{},        # empty map — Story 4-13 fills this
  created_at: ~U[2026-04-02 12:00:00Z]  # DateTime.utc_now() on first creation
}
```

`power_levels` is initialized as `%{}` — power level enforcement is Story 4-13. Do not implement now.

### DB Access Pattern

This project uses raw SQL via `Ecto.Adapters.SQL.query/3` — NOT Ecto schemas or changesets.

Pattern established in Story 2-12 (`Nebu.Session.UserStore.Postgres`):

```elixir
@sql_select "SELECT user_id FROM room_members WHERE room_id = $1 AND left_at IS NULL"

def load_members(room_id) do
  case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_select, [room_id]) do
    {:ok, %{rows: rows}} -> {:ok, Enum.map(rows, fn [uid] -> uid end)}
    {:error, reason}     -> {:error, reason}
  end
end
```

**Key: `Nebu.Repo` is the Ecto repo.** Add `{:nebu_db, in_umbrella: true}` to `room_manager/mix.exs` deps (same as `session_manager/mix.exs`).

### Timestamps: BIGINT milliseconds (not TIMESTAMPTZ)

Per architecture enforcement rule #1:

```elixir
# ✅ Correct — all PostgreSQL timestamps are BIGINT milliseconds
now_ms = Nebu.DB.Helpers.now_ms()   # = System.system_time(:millisecond)

# ❌ Wrong — never TIMESTAMPTZ or TEXT in PostgreSQL
```

`created_at` in **in-memory state** is `DateTime.utc_now()` (for GenServer use).
`joined_at`/`left_at`/`created_at` in **PostgreSQL** are `BIGINT` (milliseconds).

### SQL Migration (Migration 000009)

**Up migration:**
```sql
-- gateway/migrations/000009_rooms.up.sql
CREATE TABLE rooms (
    room_id      TEXT    PRIMARY KEY,
    name         TEXT,
    visibility   TEXT    NOT NULL DEFAULT 'private',
    created_at   BIGINT  NOT NULL,
    archived_at  BIGINT
);

ALTER TABLE rooms
    ADD CONSTRAINT rooms_visibility_check
    CHECK (visibility IN ('public', 'private'));

CREATE TABLE room_members (
    room_id    TEXT    NOT NULL REFERENCES rooms(room_id),
    user_id    TEXT    NOT NULL REFERENCES users(user_id),
    joined_at  BIGINT  NOT NULL,
    left_at    BIGINT,
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX room_members_room_id_idx ON room_members (room_id);
CREATE INDEX room_members_user_id_idx ON room_members (user_id);
```

**Down migration** must DROP in reverse order (FK constraint: members before rooms):
```sql
-- gateway/migrations/000009_rooms.down.sql
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
```

Migration goes in `gateway/migrations/` — Go is the sole schema owner. Elixir has **no schema write access**.

### `init/1` Logic

```elixir
@impl GenServer
def init(room_id) do
  # 1. Load existing members (soft-delete aware: left_at IS NULL)
  case load_members(room_id) do
    {:ok, user_ids} ->
      # Room exists (has rows or was previously created)
      members = MapSet.new(user_ids)
      {:ok, %{room_id: room_id, members: members, power_levels: %{}, created_at: DateTime.utc_now()}}

    {:error, :not_found} ->
      # Room does not exist → create it
      case insert_room(room_id) do
        {:ok, created_at} ->
          {:ok, %{room_id: room_id, members: MapSet.new(), power_levels: %{}, created_at: created_at}}
        {:error, reason} ->
          {:stop, reason}   # Let Horde supervisor handle the crash + retry
      end

    {:error, reason} ->
      {:stop, reason}
  end
end
```

If `init/1` returns `{:stop, reason}`, OTP lets the supervisor restart it. This is correct "let it crash" behavior.

**Note:** `load_members/1` returns `{:ok, []}` for a room that exists with no active members. Return `{:error, :not_found}` only if the room does not exist in the `rooms` table.

### join/leave DB SQL

```elixir
@sql_join """
INSERT INTO room_members (room_id, user_id, joined_at)
VALUES ($1, $2, $3)
ON CONFLICT (room_id, user_id) DO NOTHING
RETURNING user_id
"""

@sql_leave """
UPDATE room_members SET left_at = $3
WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL
RETURNING user_id
"""
```

For `join`: If `ON CONFLICT DO NOTHING` returns 0 rows, the user is already an active member → return `{:error, :already_member}` (check MapSet first for performance).

For `leave`: If `UPDATE` returns 0 rows, user was not an active member → return `{:error, :not_member}` (check MapSet first).

**Fail-safe rule:** Update in-memory `members` MapSet **only after** a successful DB write. Never update state if DB write fails.

### Test Strategy (No Real DB Required)

Story 4-1 tests used the app-started Horde processes. Story 4-2 adds DB calls — the unit tests must NOT require a live PostgreSQL.

**Pattern: Dependency injection via module attribute or application config:**

```elixir
# In server.ex — configurable DB module
@db_module Application.compile_env(:room_manager, :db_module, Nebu.Room.DB)

# In test — inject fake
Application.put_env(:room_manager, :db_module, Nebu.Room.FakeDB)
```

Or use Mox (already in deps if session_manager tests use it — check `core/mix.lock`).

**Alternative (simpler):** Use `start_supervised` with a mock DB module and test the GenServer logic without real SQL. The DB SQL itself can be tested via separate integration tests.

**Check `core/mix.lock` for Mox:** If `mox` is already a dep of any app, add it to `room_manager` test deps. If not, use a simple fake module approach.

### Horde Registration (from Story 4-1)

The `via` tuple pattern for calling the GenServer:

```elixir
defp via(room_id), do: {:via, Horde.Registry, {Nebu.Room.Registry, room_id}}

def join(room_id, user_id), do: GenServer.call(via(room_id), {:join, user_id})
```

Always use `via/1` — never use `GenServer.call(pid, ...)` directly in public API.

### DB init_room SQL

```elixir
@sql_insert_room """
INSERT INTO rooms (room_id, visibility, created_at)
VALUES ($1, 'private', $2)
ON CONFLICT (room_id) DO NOTHING
RETURNING created_at
"""
```

`ON CONFLICT DO NOTHING` ensures idempotency if two processes race to create the same room.

### Elixir Conventions (from CLAUDE.md)

- GenServer state: always via `handle_*` callbacks, never directly
- Errors: let it crash + Supervisor, no `try/rescue` in GenServer
- No process registration without via-tuple or Registry
- Tagged tuples: `{:ok, result}` / `{:error, reason}` for all business logic
- Supervisor strategy: `:one_for_one` default (already set in Story 4-1)

### Regression Guard: Story 4-1 Tests Must Still Pass

The existing `Nebu.RoomTest` in `core/apps/room_manager/test/nebu_room_test.exs` tests:
- `start_room/1` returns `{:ok, pid}`
- `lookup_room/1` returns `{:ok, pid}`
- Manager delegates work

After this story, `Nebu.Room.Server.init/1` calls the DB. In the test environment, the Horde processes are started by the app. DB calls from `init/1` **will fail** unless a DB is available OR the DB module is injected.

**Action required:** Use the configurable `@db_module` pattern so existing tests can override the DB to a fake that returns `{:ok, []}` (empty member list = new room).

If the existing `nebu_room_test.exs` relies on `start_room/1` which triggers `init/1` → DB call, the tests will fail without a DB mock. Update `test_helper.exs` or `setup` block to inject a fake DB module.

### File Structure

Only modify/create these files:

```
core/apps/room_manager/
  lib/nebu/room/
    server.ex               ← MODIFY: replace stub with full lifecycle
  mix.exs                   ← MODIFY: add {:nebu_db, in_umbrella: true}
  test/
    nebu_room_test.exs      ← MODIFY: add new lifecycle tests, maintain 4-1 tests

gateway/migrations/
  000009_rooms.up.sql       ← CREATE: rooms + room_members tables
  000009_rooms.down.sql     ← CREATE: DROP both tables
```

**Do NOT modify:** `room_supervisor.ex`, `manager.ex`, `application.ex`, `mix.lock`, any other app.

### Build Command

```bash
make test-unit-elixir   # runs: mix test in container with --warnings-as-errors
```

All tests run inside Docker containers — no local Elixir install needed.

---

## Previous Story Intelligence (Story 4-1)

**Key learnings from Story 4-1 implementation:**

1. **Horde resolves to 0.10.0** (semver compatible with `~> 0.9`). Use `Horde.DynamicSupervisor` and `Horde.Registry` as already configured.

2. **`Horde.Registry.lookup/2` returns `[{pid, value}]` list** — empty list = not found. This is already handled in `room_supervisor.ex`.

3. **`child_spec/1` must override `id`** — `{__MODULE__, room_id}` not `__MODULE__`. Already done in stub. Keep this.

4. **Test isolation:** Use `on_exit` + `GenServer.stop` to clean up Room processes after tests. The existing pattern in `nebu_room_test.exs` with `start_and_track/1` is correct. Extend it.

5. **`async: false` required** — Horde uses global named processes. All room tests must use `async: false`.

6. **App-started Horde:** The umbrella test runner starts `Nebu.Room.Application`, which starts `Horde.Registry` and `Horde.DynamicSupervisor`. Tests run against these. Do NOT `start_supervised` them again.

7. **Module naming:** `Nebu.Room.HordeSupervisor` for the `Horde.DynamicSupervisor` (not `Nebu.Room.Supervisor` — that's the OTP root supervisor).

**Files modified/created in Story 4-1:**
- `core/apps/room_manager/lib/nebu/room/application.ex` — has Horde.Registry + Horde.DynamicSupervisor
- `core/apps/room_manager/lib/nebu/room/room_supervisor.ex` — has start_room/1 + lookup_room/1
- `core/apps/room_manager/lib/nebu/room/manager.ex` — delegates to RoomSupervisor
- `core/apps/room_manager/lib/nebu/room/server.ex` — **stub** to be replaced in this story
- `core/apps/room_manager/test/nebu_room_test.exs` — 6 tests, all passing

---

## Architecture Compliance

| Requirement | Source |
|---|---|
| All PostgreSQL timestamps as BIGINT milliseconds | `architecture.md#Enforcement rule #1` |
| `Nebu.{Domain}.{Name}` module naming | `architecture.md#Naming Conventions > Elixir` |
| Raw SQL via `Ecto.Adapters.SQL.query/3` | `session_manager/lib/nebu/session/user_store/postgres.ex` |
| `Nebu.Repo` as the Ecto repo | `nebu_db/lib/nebu/repo.ex` |
| Horde via-tuple registration | `architecture.md#G3 — Room-Autorität: Horde` |
| Let it crash — no try/rescue in GenServer | `CLAUDE.md#Elixir Conventions` |
| DB schema ownership by Go Gateway migrations | `architecture.md#Resolved: Migrations` |
| Tagged tuples `{:ok, result}` / `{:error, reason}` | `architecture.md#Enforcement rule #6` |
| Go is sole schema owner — Elixir read-only via Repo | `architecture.md#Resolved: Migrations` |
| `snake_case` plural table names | `architecture.md#Naming Conventions > PostgreSQL` |

---

## Dev Agent Record

### Implementation Plan

1. Created `gateway/migrations/000009_rooms.up.sql` with `rooms` and `room_members` tables (BIGINT timestamps, FK constraints, visibility CHECK, indexes).
2. Created `gateway/migrations/000009_rooms.down.sql` (DROP in FK-safe order: members first, then rooms).
3. Added `{:nebu_db, in_umbrella: true}` to `room_manager/mix.exs` deps.
4. Created `core/apps/room_manager/lib/nebu/room/db.ex` — PostgreSQL persistence layer with raw SQL (load_members, insert_room, insert_member, delete_member). Separate module for testability.
5. Replaced `Nebu.Room.Server` stub with full lifecycle GenServer. DB module resolved at runtime via `Application.get_env/3` (not compile_env) to allow test injection.
6. Updated `nebu_room_test.exs` with 10 new lifecycle tests + ETS-backed FakeDB + FailingWriteDB for fail-safe testing. All 6 Story 4-1 regression tests retained.

### Key Technical Decision: Runtime DB Injection

The story spec suggested `Application.compile_env` for the `@db_module` attribute. This was changed to a `defp db_module/0` function using `Application.get_env/3` at runtime. Reason: `compile_env` is resolved at compile time and cannot be overridden at test runtime via `Application.put_env`. The runtime approach is the correct pattern for this project (see session_manager tests for the same pattern).

### Completion Notes

- All 16 room_manager tests pass (6 Story 4-1 regressions + 10 new Story 4-2 lifecycle tests)
- All 70 umbrella tests pass with `--warnings-as-errors`
- SQL migrations follow exact schema from Dev Notes
- DB layer extracted to `Nebu.Room.DB` for clean separation
- Fail-safe: DB errors never corrupt in-memory MapSet state
- State recovery from DB verified: restart-persistence test passes via FakeDB ETS simulation

## File List

- `gateway/migrations/000009_rooms.up.sql` — NEW: rooms + room_members tables
- `gateway/migrations/000009_rooms.down.sql` — NEW: DROP both tables (FK-safe order)
- `core/apps/room_manager/mix.exs` — MODIFIED: added `{:nebu_db, in_umbrella: true}`
- `core/apps/room_manager/lib/nebu/room/server.ex` — MODIFIED: replaced stub with full lifecycle GenServer
- `core/apps/room_manager/lib/nebu/room/db.ex` — NEW: PostgreSQL persistence layer
- `core/apps/room_manager/test/nebu_room_test.exs` — MODIFIED: 10 new tests + FakeDB + FailingWriteDB

### Review Findings

- [x] [Review][Patch][MAJOR] Rejoin nach Leave nicht moeglich — `ON CONFLICT DO NOTHING` ignoriert soft-deleted Row, State/DB-Divergenz bei Restart [db.ex:25-32] — FIXED: changed to `ON CONFLICT DO UPDATE SET left_at = NULL`
- [x] [Review][Patch][MINOR] Redundanter Index `room_members_room_id_idx` — PK-Prefix liefert bereits B-tree auf `room_id` [000009_rooms.up.sql:20] — FIXED: removed
- [x] [Review][Patch][MINOR] `created_at` im State bei Recovery immer `DateTime.utc_now()` statt DB-Wert — echtes Erstellungsdatum geht bei Restart verloren [server.ex:84-86, db.ex:47] — FIXED: `load_members` gibt `created_at_ms` zurueck, `init/1` konvertiert via `DateTime.from_unix!/2`
- [x] [Review][Defer][INFO] Kein `@behaviour`-Modul fuer DB-Interface — FakeDB/FailingWriteDB implementieren gleiche API ohne Vertrag — deferred, pre-existing pattern

## Change Log

- 2026-04-03: Story created — ready-for-dev
- 2026-04-03: Story implemented — all tasks complete, 16/16 tests pass, status → review
- 2026-04-03: Code review — 1 MAJOR + 2 MINOR fixed, 1 INFO deferred

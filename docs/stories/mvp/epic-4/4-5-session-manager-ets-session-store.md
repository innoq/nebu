# Story 4.5: Session Manager: ETS Session Store

Status: review

## Story

As a Core developer,
I want an in-memory ETS-backed Session Store for active user sessions,
so that session lookups during /sync are O(1) and do not hit PostgreSQL on every request.

## Acceptance Criteria

1. A new module `Nebu.Session.EtsStore` is created at `core/apps/session_manager/lib/nebu/session/ets_store.ex`:
   - ETS table `NebuSessions` created at application start with type `:set`, access `:public`
   - `put_session/2` — upserts `{user_id, session_map}` where `session_map` includes `access_token_hash`, `device_id`, `created_at_ms`, `last_seen_at_ms`
   - `get_session/1` → `{:ok, session_map}` or `{:error, :not_found}`
   - `delete_session/1` → `:ok`
   - `list_sessions/0` → `[session_map]` (for Admin metrics)

2. `access_token_hash` stores `Base.encode16(:crypto.hash(:sha256, access_token))` — never the raw token in ETS.

3. `Nebu.Session.Application` is updated to start the `NebuSessions` ETS table before any children:
   - Create `:ets.new(:NebuSessions, [:named_table, :set, :public])` in `Application.start/2` with the same guard pattern used by `NebuTxnDedup` (check `:ets.whereis/1` before creating)
   - `Nebu.Session.EtsStore` is started as a supervised worker under `Nebu.Session.Supervisor`

4. Unit tests in `core/apps/session_manager/test/nebu/session/ets_store_test.exs` cover:
   - `put_session/2` + `get_session/1` round-trip returns the stored session_map
   - `delete_session/1` removes the entry; subsequent `get_session/1` returns `{:error, :not_found}`
   - `get_session/1` on a missing key returns `{:error, :not_found}`
   - `list_sessions/0` returns all current sessions
   - `put_session/2` called twice with the same user_id results in a single entry (upsert, not duplicate)
   - `access_token_hash` is the SHA-256 hash encoded as Base16, not the raw token

5. A crash/restart test verifies that `NebuSessions` ETS table survives a GenServer crash:
   - The `Nebu.Session.Application` owns the ETS table (not a GenServer), so if `Nebu.Session.EtsStore` crashes and is restarted by the supervisor, the `NebuSessions` table still exists and previously inserted data remains accessible.

6. `mix test --warnings-as-errors` passes for the full umbrella. All existing Story 4-1 through 4-4 tests continue to pass unchanged.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Happy-path round-trip — ExUnit**
- Given: ETS table `NebuSessions` exists (created by Application start)
- When: `Nebu.Session.EtsStore.put_session("@kai:nebu.local", %{access_token_hash: "abc123", device_id: "DEVICE_1", created_at_ms: 1000, last_seen_at_ms: 1000})` is called
- Then: `Nebu.Session.EtsStore.get_session("@kai:nebu.local")` returns `{:ok, %{access_token_hash: "abc123", device_id: "DEVICE_1", created_at_ms: 1000, last_seen_at_ms: 1000}}`

**2. Missing key — ExUnit**
- Given: ETS table `NebuSessions` is empty (or key was never inserted)
- When: `Nebu.Session.EtsStore.get_session("@unknown:nebu.local")` is called
- Then: returns `{:error, :not_found}`

**3. Delete removes entry — ExUnit**
- Given: a session for `"@kai:nebu.local"` has been put into ETS
- When: `Nebu.Session.EtsStore.delete_session("@kai:nebu.local")` is called, then `get_session/1` is called
- Then: `delete_session/1` returns `:ok`; subsequent `get_session/1` returns `{:error, :not_found}`

**4. List returns all sessions — ExUnit**
- Given: two sessions put for `"@kai:nebu.local"` and `"@alex:nebu.local"`
- When: `Nebu.Session.EtsStore.list_sessions()` is called
- Then: returns a list of length 2 containing both session maps

**5. Upsert — no duplicate on repeated put — ExUnit**
- Given: `put_session("@kai:nebu.local", session_map_v1)` called first
- When: `put_session("@kai:nebu.local", session_map_v2)` called second with updated `last_seen_at_ms`
- Then: `list_sessions()` returns exactly 1 entry; `get_session/1` returns `session_map_v2`

**6. Token hash — raw token is never stored — ExUnit**
- Given: raw access token `"secret_token_value"`
- When: the hash is computed as `Base.encode16(:crypto.hash(:sha256, "secret_token_value"), case: :lower)`
- Then: the stored `access_token_hash` equals this hex string, NOT the raw token string

**7. Crash/restart test — required for GenServer state (ETS owned by Application) — ExUnit**
- Given: `Nebu.Session.EtsStore` worker is started and a session for `"@kai:nebu.local"` has been inserted via `put_session/2`
- When: the `Nebu.Session.EtsStore` GenServer process is killed with `Process.exit(pid, :kill)`
- Then: the Supervisor restarts `Nebu.Session.EtsStore` automatically; after restart, `get_session("@kai:nebu.local")` still returns `{:ok, session_map}` because `NebuSessions` is owned by the Application process and survives the GenServer crash

---

## Tasks / Subtasks

- [x] Create `Nebu.Session.EtsStore` module (AC: #1, #2)
  - [x] Create `core/apps/session_manager/lib/nebu/session/ets_store.ex`
  - [x] Implement `put_session/2` with atom-keyed session_map
  - [x] Implement `get_session/1` returning `{:ok, map}` or `{:error, :not_found}`
  - [x] Implement `delete_session/1` returning `:ok`
  - [x] Implement `list_sessions/0` returning `[map]`
  - [x] Add `use GenServer` + `start_link/1` + minimal GenServer lifecycle (state: none — ETS is the store)
  - [x] Add `@spec` for all public functions

- [x] Update `Nebu.Session.Application` (AC: #3)
  - [x] Add `NebuSessions` ETS creation BEFORE `Supervisor.start_link/2` (guard with `:ets.whereis/1`)
  - [x] Add `Nebu.Session.EtsStore` to children list

- [x] Write failing unit tests FIRST (AC: #4, #5, #6)
  - [x] Create `core/apps/session_manager/test/nebu/session/ets_store_test.exs`
  - [x] Add setup to clean `:NebuSessions` between tests via `:ets.delete_all_objects(:NebuSessions)`
  - [x] Write 7 test cases as described in Acceptance Tests section (write BEFORE implementation)
  - [x] Run tests — verify they FAIL (red phase)

- [x] Implement until all tests pass (AC: #1–#6)
  - [x] Implement the EtsStore functions against the failing tests

- [x] Run `mix test --warnings-as-errors` for full umbrella (AC: #6)
  - [x] Confirm existing room_manager tests (4-1 through 4-4) pass unchanged

---

## Dev Notes

### CRITICAL: Module Naming — epics.md vs. Codebase Reality

The epics.md acceptance criteria reference `SessionManager.EtsStore` — this is **WRONG** for this codebase. The codebase established the `Nebu.{Domain}.{Name}` convention in Epic 2 and all subsequent epics follow it:

| Epics.md Spec | Correct Name in Codebase |
|---|---|
| `SessionManager.EtsStore` | `Nebu.Session.EtsStore` |
| `SessionManager.Application` | `Nebu.Session.Application` |
| `SessionManager.PgStore` (Story 4-6) | `Nebu.Session.PgStore` |
| `SessionManager.SessionSupervisor` (Story 4-6) | `Nebu.Session.SessionSupervisor` |

This pattern is established without exception across: `Nebu.Room.*`, `Nebu.Session.*`, `Nebu.Signature.*`, `Nebu.Permissions.*`, `Nebu.Presence.*`.

### File Location

```
core/apps/session_manager/
  lib/nebu/session/
    ets_store.ex                       ← CREATE (new module)
    application.ex                     ← MODIFY: add ETS creation + worker
    manager.ex                         ← placeholder, no changes needed
  test/nebu/session/
    ets_store_test.exs                 ← CREATE (new test file)
```

**Do NOT create:**
- `core/apps/session_manager/lib/session_manager/ets_store.ex` (wrong path — this is the epics.md path, NOT the codebase path)

**Existing files — do NOT modify unless necessary:**
- `test/nebu/session/user_store_test.exs` — already has its own test isolation; leave untouched
- `test/nebu/session/token_validator_test.exs` — leave untouched
- `mix.exs` — no new dependencies needed (`:crypto` is OTP built-in, all deps already present)

### ETS Table: `NebuSessions`

**Creation pattern (in `Nebu.Session.Application.start/2`, BEFORE `Supervisor.start_link/2`):**

```elixir
# In Nebu.Session.Application.start/2:
# Guard pattern from Story 4-4 (NebuTxnDedup) — prevents crash on Application restart in same VM:
if :ets.whereis(:NebuSessions) == :undefined do
  :ets.new(:NebuSessions, [:named_table, :set, :public])
end
```

The table is owned by the Application process (not by `Nebu.Session.EtsStore` GenServer). This is the **critical architectural decision**: if the GenServer crashes and restarts, the ETS data is not lost.

**ETS key structure:**
```
Key:   user_id :: String.t()          (e.g. "@kai:nebu.local")
Value: session_map :: map()           (atom-keyed)
```

**ETS entry format:**
```elixir
{user_id, %{
  access_token_hash: "abc123...",    # Base16-encoded SHA-256, never raw token
  device_id: "DEVICE_1",
  created_at_ms: 1_712_000_000_000, # BIGINT — milliseconds since epoch
  last_seen_at_ms: 1_712_000_000_000
}}
```

**Lookup pattern:**
```elixir
case :ets.lookup(:NebuSessions, user_id) do
  [{^user_id, session_map}] -> {:ok, session_map}
  [] -> {:error, :not_found}
end
```

**Insert/Upsert pattern** (`:set` type auto-overwrites on same key):
```elixir
:ets.insert(:NebuSessions, {user_id, session_map})
```

**Delete pattern:**
```elixir
:ets.delete(:NebuSessions, user_id)
:ok
```

**List all sessions:**
```elixir
:ets.tab2list(:NebuSessions) |> Enum.map(fn {_user_id, session_map} -> session_map end)
```

### `access_token_hash` Computation

**Architecture requirement (epics.md AC #2):** Never store raw tokens in ETS — always hash first.

```elixir
@spec hash_token(String.t()) :: String.t()
defp hash_token(access_token) do
  Base.encode16(:crypto.hash(:sha256, access_token), case: :lower)
end
```

This produces a 64-character lowercase hex string. `:crypto` is OTP built-in — no additional dependency.

**Note on case:** The epics.md references `Base16.encode(...)` — this module does NOT exist in the Elixir standard library. The correct Elixir standard library function is `Base.encode16/2`. Use `case: :lower` for consistency with hex conventions.

### `Nebu.Session.EtsStore` as GenServer Worker

The `Nebu.Session.EtsStore` is started as a supervised worker but holds **no state** itself — ETS is the store. The GenServer exists so the Application supervisor can manage it. Minimal implementation:

```elixir
defmodule Nebu.Session.EtsStore do
  @moduledoc """
  ETS-backed session store for active user sessions.

  ETS table :NebuSessions is created in Nebu.Session.Application.start/2
  (owned by the Application process) so data survives GenServer restarts.

  This GenServer is a supervised worker with no internal state.
  All operations are direct ETS reads/writes.
  """

  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, opts)
  end

  @impl true
  def init(:ok), do: {:ok, :no_state}

  # Public API functions are pure ETS operations (no GenServer.call needed):
  @spec put_session(String.t(), map()) :: :ok
  def put_session(user_id, session_map) do
    :ets.insert(:NebuSessions, {user_id, session_map})
    :ok
  end

  @spec get_session(String.t()) :: {:ok, map()} | {:error, :not_found}
  def get_session(user_id) do
    case :ets.lookup(:NebuSessions, user_id) do
      [{^user_id, session_map}] -> {:ok, session_map}
      [] -> {:error, :not_found}
    end
  end

  @spec delete_session(String.t()) :: :ok
  def delete_session(user_id) do
    :ets.delete(:NebuSessions, user_id)
    :ok
  end

  @spec list_sessions() :: [map()]
  def list_sessions do
    :ets.tab2list(:NebuSessions)
    |> Enum.map(fn {_user_id, session_map} -> session_map end)
  end
end
```

**Important architectural note:** The public API functions (`put_session/2`, `get_session/1`, `delete_session/1`, `list_sessions/0`) call ETS **directly** without going through `GenServer.call/2`. This is intentional and correct — ETS tables with `:public` access can be read/written from any process without message passing. This keeps the hot path for `/sync` session lookups at O(1) without GenServer bottleneck.

### Updating `Nebu.Session.Application`

```elixir
defmodule Nebu.Session.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # ETS table for active user sessions (hot-path for /sync lookups).
    # Created here (owned by Application process) so it survives
    # Nebu.Session.EtsStore GenServer crashes/restarts.
    # Type :set auto-upserts on same key. Access :public allows any process.
    if :ets.whereis(:NebuSessions) == :undefined do
      :ets.new(:NebuSessions, [:named_table, :set, :public])
    end

    children = [
      Nebu.Session.EtsStore
    ]

    opts = [strategy: :one_for_one, name: Nebu.Session.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Test File Structure and Patterns

The existing tests in `session_manager` use:
- `use ExUnit.Case, async: false` — required because Application env is process-global
- ETS table setup/teardown via `:ets.new/2` in `setup` with `if :ets.whereis/1` guard
- `on_exit` callbacks for cleanup

For `ets_store_test.exs`, the pattern differs slightly — `NebuSessions` is created by `Application.start/2` at test suite boot (since the Application is started by `ExUnit.start()`). Tests should NOT create/delete the table themselves. Instead, use:

```elixir
setup do
  # Clear all entries between tests to ensure isolation
  :ets.delete_all_objects(:NebuSessions)
  :ok
end
```

**Note:** Unlike the other test modules (user_store, token_validator), `Nebu.Session.EtsStore` does NOT need an injected fake DB module — it operates directly on ETS. No `Application.put_env` manipulation needed.

### Crash/Restart Test Pattern

For AC #5 (the mandatory crash/restart test for ETS-state stories):

```elixir
test "ETS data survives EtsStore GenServer crash and supervisor restart" do
  # Insert a session before crash
  session = %{
    access_token_hash: "test_hash",
    device_id: "DEVICE_X",
    created_at_ms: System.system_time(:millisecond),
    last_seen_at_ms: System.system_time(:millisecond)
  }
  :ok = Nebu.Session.EtsStore.put_session("@crash_test:nebu.local", session)

  # Verify it's stored
  assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@crash_test:nebu.local")

  # Kill the EtsStore GenServer process
  pid = Process.whereis(Nebu.Session.EtsStore)
  # Note: If EtsStore is started without a name, find it via the supervisor.
  # If started with name: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  assert pid != nil
  Process.exit(pid, :kill)

  # Give supervisor time to restart the worker
  Process.sleep(50)

  # ETS data must still be accessible — table is owned by Application process
  assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@crash_test:nebu.local")
end
```

**For the Process.whereis/1 call to work:** `Nebu.Session.EtsStore` must be started with a registered name. Use `name: __MODULE__` in `start_link/1` and register it:

```elixir
def start_link(opts \\ []) do
  GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
end
```

This allows `Process.whereis(Nebu.Session.EtsStore)` in the test.

**Why the data survives:** ETS tables are owned by the process that called `:ets.new/2`. In this architecture, `Nebu.Session.Application.start/2` creates the table — so the Application process owns it. When `Nebu.Session.EtsStore` (the GenServer) is killed, the table is NOT deleted because the EtsStore process does NOT own the table. The Application process continues running, keeping the table alive.

### `async: false` Requirement

All session_manager tests use `async: false` because:
1. `:NebuSessions` is a named global ETS table — concurrent modifications would cause race conditions between test cases
2. `Application.put_env` is process-global — other test files in session_manager set/unset env vars

This matches the pattern in all existing session_manager tests (`user_store_test.exs`, `token_validator_test.exs`, `bootstrap_checker_test.exs`).

### Architecture Compliance Rules

| Rule | Requirement | Source |
|---|---|---|
| Rule #1 | Timestamps as BIGINT (milliseconds) — `created_at_ms`, `last_seen_at_ms` as integer, NOT DateTime.t() | `architecture.md#Enforcement rule #1` |
| Rule #6 | `{:ok, result}` / `{:error, reason}` — no raise/throw for business logic | `architecture.md#Enforcement rule #6` |
| ADR-002 | ETS replaces Redis — `NebuSessions` is the session hot-path cache; no external deps | `architecture.md#ADR-002` |
| No raw tokens | `access_token_hash` = `Base.encode16(:crypto.hash(:sha256, token), case: :lower)` | Epic 4 AC #2 |
| Naming | All modules follow `Nebu.Session.*` — ignore `SessionManager.*` in epics.md | Code convention |
| ETS ownership | Table created in Application.start/2 (NOT in GenServer.init/1) — crash safety | Epic 4, Story 4-4 precedent |
| async: false | Required for all session_manager tests — shared ETS + global env | All existing tests |

### Timestamps: Integer Milliseconds (NOT DateTime)

Architecture rule #1 mandates BIGINT timestamps. In Elixir, use:
```elixir
System.system_time(:millisecond)  # returns integer ms since epoch
```

Session map fields must be integer millisecond values:
- `created_at_ms: integer()` — NOT `%DateTime{}`
- `last_seen_at_ms: integer()` — NOT `%DateTime{}`

This is consistent with `Nebu.Room.DB` (established in Story 4-2) and `events` table (established in Story 4-4).

### Cross-Story Context

| Story | Relationship to 4-5 |
|---|---|
| Story 2-2 | Created `sessions` table in PostgreSQL — Story 4-5 creates the ETS layer on top |
| Story 2-19 | `POST /logout` invalidates sessions — Story 4-6 will call `delete_session/1` (this story) |
| Story 4-4 | Established ETS table created in Application.start/2 pattern — follow identically |
| Story 4-6 | PostgreSQL `sync_tokens` persistence + `invalidate_session/1` — builds on `delete_session/1` from this story |
| Story 4-14/15 | `/sync` lookups will call `get_session/1` — O(1) ETS hot-path |

### What Story 4-5 Does NOT implement

- No `sync_tokens` table or since-token persistence (that is Story 4-6)
- No `invalidate_session/1` that writes to PostgreSQL (that is Story 4-6)
- No `SessionSupervisor.create_session/2` or `destroy_session/1` (that is Story 4-6)
- No gRPC handler wiring (Story 4-8 and later)
- No migration for `sync_tokens` (Story 4-6)

The `sessions` table was already created in Story 2-2. Story 4-5 adds only the **ETS in-memory layer** on top. No new SQL migration is needed for this story.

### Build & Test Commands

```bash
# Run session_manager tests only (fast, targeted):
make test-unit-elixir

# Run full umbrella (before marking complete):
make test-unit-elixir
```

All tests run inside Docker containers — no local Elixir install needed.

**Expected result after implementation:**
- All existing Story 4-1 through 4-4 tests pass (no regression)
- New EtsStore tests pass (7 test cases: round-trip, missing key, delete, list, upsert, hash, crash/restart)
- 0 failures, 0 warnings

---

## Previous Story Intelligence (Story 4-4)

Key learnings from Story 4-4 implementation and code review that directly impact Story 4-5:

1. **ETS guard pattern is mandatory** — Story 4-4 code review found a MAJOR bug: `NebuTxnDedup` creation had no guard, crashing on Application restart in the same VM. Always use:
   ```elixir
   if :ets.whereis(:NebuSessions) == :undefined do
     :ets.new(:NebuSessions, [:named_table, :set, :public])
   end
   ```

2. **ETS table owned by Application, NOT GenServer** — This is the established pattern. The ETS table must be created in `Application.start/2` before `Supervisor.start_link/2`, not in `GenServer.init/1`.

3. **ETS cleanup in tests** — Named ETS tables persist across tests. Use `:ets.delete_all_objects(:NebuSessions)` in `setup` to clear between tests. Do NOT call `:ets.delete/1` on the table itself (it would destroy the table, not just its contents).

4. **`async: false` is required** — All session_manager tests share a named ETS table. Concurrency between test cases would cause flaky failures.

5. **`:ets.delete_all_objects/1` vs. `:ets.delete/1`** — Use `delete_all_objects` in `setup` (wipes entries, table survives). Do NOT use `delete` (destroys table). Story 4-4 code review noted a MINOR: the `on_exit` called `delete_all_objects` without checking if table exists — always guard with `:ets.whereis/1`.

6. **Existing session_manager module structure** — `Nebu.Session.Application` only has a placeholder comment and empty children list. The `Nebu.Session.Manager` module is also a placeholder. Do not confuse with the real EtsStore being created in this story.

7. **No `@behaviour` for EtsStore** — Given the direct ETS access pattern (no DB injection needed), a behaviour interface is not required here. The module is not a swappable adapter. This matches the architecture: ETS operations are always direct.

---

## Architecture References

- `_bmad-output/planning-artifacts/architecture.md` — G1 (Sync-API: Hybrid ETS + PostgreSQL), ADR-002 (no Redis), Enforcement rule #1 (BIGINT timestamps), Rule #6 (no raise)
- `_bmad-output/planning-artifacts/epics.md` — Story 4.5 Acceptance Criteria (line ~1913)
- `core/apps/session_manager/lib/nebu/session/application.ex` — Current Application placeholder to extend
- `core/apps/session_manager/lib/nebu/session/manager.ex` — Placeholder only, no changes needed
- `core/apps/session_manager/mix.exs` — No new deps required (`:crypto` is OTP built-in)
- `core/apps/session_manager/test/nebu/session/user_store_test.exs` — ETS test pattern to follow (setup/teardown)
- `core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs` — Session_manager test style reference
- `core/apps/room_manager/lib/nebu/room/application.ex` — ETS creation in Application.start/2 with guard (Story 4-4 pattern to replicate)
- `core/apps/room_manager/test/nebu_room_test.exs` — ETS crash test pattern reference (Story 4-4)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m] (Claude Sonnet 4.6, 1M context)

### Completion Notes List

- Created `Nebu.Session.EtsStore` GenServer at `core/apps/session_manager/lib/nebu/session/ets_store.ex`. The module exposes four public API functions (`put_session/2`, `get_session/1`, `delete_session/1`, `list_sessions/0`) as direct ETS operations with no GenServer.call overhead. Registered under its module name via `Keyword.put_new(opts, :name, __MODULE__)` in `start_link/1` to enable `Process.whereis/1` in crash/restart test.
- Updated `Nebu.Session.Application` to create `:NebuSessions` ETS table (type `:set`, access `:public`) before `Supervisor.start_link/2`, guarded with `:ets.whereis(:NebuSessions) == :undefined` following the Story 4-4 pattern from `NebuTxnDedup`. Added `Nebu.Session.EtsStore` to the children list.
- All 8 acceptance tests pass (26 session_manager tests total). Full umbrella: 0 failures, 0 warnings (`mix test --warnings-as-errors`). Room_manager tests (4-1 through 4-4) pass unchanged (22 tests, 0 failures).

### File List

Files created or modified:

```
core/apps/session_manager/lib/nebu/session/ets_store.ex     ← CREATED
core/apps/session_manager/lib/nebu/session/application.ex   ← MODIFIED
core/apps/session_manager/test/nebu/session/ets_store_test.exs  ← pre-existing (failing tests, unchanged)
_bmad-output/implementation-artifacts/4-5-session-manager-ets-session-store.md  ← MODIFIED (this file)
_bmad-output/implementation-artifacts/sprint-status.yaml    ← MODIFIED (status → review)
```

### Change Log

- 2026-04-03: Story 4-5 created — Session Manager ETS Session Store
- 2026-04-03: Implementation complete — Nebu.Session.EtsStore created, Application updated, all 8 acceptance tests pass; status set to review

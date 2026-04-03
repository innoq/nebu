# Story 4.6: Session Manager: PostgreSQL since-Token + Invalidation

Status: review

## Story

As a Core developer,
I want the Session Manager to persist since-tokens to PostgreSQL and support session invalidation,
so that incremental /sync correctly resumes after gateway restarts and logout invalidates the session cluster-wide.

## Acceptance Criteria

1. A new module `Nebu.Session.PgStore` is created at `core/apps/session_manager/lib/nebu/session/pg_store.ex`:
   - `persist_since_token(user_id, since_token, last_event_id)` — upserts a row in the `sync_tokens` table
   - `get_since_token(user_id)` → `{:ok, %{since_token: String.t(), last_event_id: String.t() | nil}}` or `{:error, :not_found}`
   - `invalidate_session(user_id)` — deletes from both `sync_tokens` AND `sessions` tables in a single PostgreSQL transaction; also calls `Nebu.Session.EtsStore.delete_session/1` to evict from the in-memory cache

2. `since_token` is an opaque string generated as: `Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)` — never a sequential integer, never guessable.

3. A new SQL migration `000011_sync_tokens.up.sql` creates the `sync_tokens` table:
   ```sql
   CREATE TABLE sync_tokens (
     user_id       TEXT PRIMARY KEY REFERENCES users(user_id),
     since_token   TEXT NOT NULL,
     last_event_id TEXT,
     updated_at    BIGINT NOT NULL
   );
   ```
   - `updated_at` uses BIGINT milliseconds (architecture enforcement rule #1 — never TIMESTAMPTZ)
   - `user_id` FK to `users` ensures referential integrity and cascades on user deletion
   - Corresponding `000011_sync_tokens.down.sql` with `DROP TABLE IF EXISTS sync_tokens;`

4. A new module `Nebu.Session.SessionSupervisor` is created at `core/apps/session_manager/lib/nebu/session/session_supervisor.ex`:
   - `create_session(user_id, session_map)` — calls `Nebu.Session.EtsStore.put_session/2` (ETS write for hot-path) and returns `:ok`
   - `destroy_session(user_id)` — delegates to `Nebu.Session.PgStore.invalidate_session/1` which handles both ETS eviction and DB cleanup in a transaction

5. Unit tests in `core/apps/session_manager/test/nebu/session/pg_store_test.exs` cover (all using a `FakePgStore` or injected fake DB — no real PostgreSQL connection in unit tests):
   - `persist_since_token/3` + `get_since_token/1` round-trip returns the stored data
   - `get_since_token/1` on a missing user returns `{:error, :not_found}`
   - `invalidate_session/1` removes from both ETS and PG (verified via fake DB + ETS state)
   - since_token is not a plain sequential integer (opaque, contains colons, base64-encoded)

6. `mix test --warnings-as-errors` passes for the full umbrella. All existing Story 4-1 through 4-5 tests continue to pass unchanged.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. persist_since_token + get_since_token round-trip — ExUnit**
- Given: `sync_tokens` table exists (via migration) and no entry for `"@kai:nebu.local"` yet
- When: `Nebu.Session.PgStore.persist_since_token("@kai:nebu.local", "opaque_token_v1", "last_event_id_1")` is called via the fake DB module
- Then: `Nebu.Session.PgStore.get_since_token("@kai:nebu.local")` returns `{:ok, %{since_token: "opaque_token_v1", last_event_id: "last_event_id_1"}}`

**2. get_since_token on missing user — ExUnit**
- Given: `sync_tokens` table is empty (or key was never inserted)
- When: `Nebu.Session.PgStore.get_since_token("@unknown:nebu.local")` is called
- Then: returns `{:error, :not_found}`

**3. invalidate_session removes from ETS and PG — ExUnit**
- Given: a session for `"@kai:nebu.local"` exists in ETS (`Nebu.Session.EtsStore.put_session/2` called) AND a sync_token row exists in the fake PG store
- When: `Nebu.Session.PgStore.invalidate_session("@kai:nebu.local")` is called
- Then: `Nebu.Session.EtsStore.get_session("@kai:nebu.local")` returns `{:error, :not_found}` AND the fake PG store no longer contains the user's sync_token AND the fake PG store no longer contains the user's session row

**4. since_token is opaque (not a sequential integer) — ExUnit**
- Given: `user_id = "@kai:nebu.local"`, `last_event_id = "$abc123"`
- When: `Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)` is computed
- Then: the resulting string is NOT parseable as an integer (`Integer.parse/1` returns `:error`), is not guessable (contains `:` separators), and is a valid base64url string

**5. upsert: second persist_since_token replaces the first — ExUnit**
- Given: `persist_since_token("@kai:nebu.local", "token_v1", "event_1")` called first
- When: `persist_since_token("@kai:nebu.local", "token_v2", "event_2")` called second
- Then: `get_since_token("@kai:nebu.local")` returns `{:ok, %{since_token: "token_v2", last_event_id: "event_2"}}` (upsert, not duplicate)

**6. SessionSupervisor.create_session writes to ETS — ExUnit**
- Given: `:NebuSessions` ETS table is empty
- When: `Nebu.Session.SessionSupervisor.create_session("@kai:nebu.local", %{access_token_hash: "h", device_id: "D1", created_at_ms: 1000, last_seen_at_ms: 1000})` is called
- Then: `Nebu.Session.EtsStore.get_session("@kai:nebu.local")` returns `{:ok, _session_map}` — ETS is populated

**7. SessionSupervisor.destroy_session removes from ETS — ExUnit**
- Given: a session exists in ETS via `put_session/2`
- When: `Nebu.Session.SessionSupervisor.destroy_session("@kai:nebu.local")` is called (using fake PG module)
- Then: `Nebu.Session.EtsStore.get_session("@kai:nebu.local")` returns `{:error, :not_found}`

**8. DB failure test: invalidate_session DB error does not corrupt ETS — ExUnit**
- Given: a session exists in ETS; the fake PG module returns `{:error, :db_connection_lost}` for the transaction
- When: `Nebu.Session.PgStore.invalidate_session("@kai:nebu.local")` is called with the failing DB module
- Then: returns `{:error, :db_connection_lost}` AND the ETS entry must NOT have been deleted (atomicity: either both succeed or neither succeeds)

---

## Tasks / Subtasks

- [x] Write failing unit tests FIRST (ATDD gate — tests must be red before any implementation)
  - [x] Create `core/apps/session_manager/test/nebu/session/pg_store_test.exs`
  - [x] Create `core/apps/session_manager/test/nebu/session/session_supervisor_test.exs`
  - [x] Write 8 test cases as described in Acceptance Tests section
  - [x] Run tests — verify they FAIL (red phase)

- [x] Create SQL migration for `sync_tokens` table (AC: #3)
  - [x] Create `gateway/migrations/000011_sync_tokens.up.sql`
  - [x] Create `gateway/migrations/000011_sync_tokens.down.sql`
  - [x] Verify migration sequence: highest existing is `000010_events`, so next is `000011`

- [x] Create `Nebu.Session.PgStore` module (AC: #1)
  - [x] Create `core/apps/session_manager/lib/nebu/session/pg_store.ex`
  - [x] Implement `persist_since_token/3` with UPSERT (ON CONFLICT DO UPDATE)
  - [x] Implement `get_since_token/1` returning `{:ok, map}` or `{:error, :not_found}`
  - [x] Implement `invalidate_session/1` using `Nebu.Repo.transaction/1` (delete from sync_tokens + sessions in one transaction, then call EtsStore.delete_session/1)
  - [x] Add `@behaviour` + `@callback` definitions (mirrors pattern of `UserStore`, `TokenValidator`)
  - [x] Add `@spec` for all public functions

- [x] Create `Nebu.Session.SessionSupervisor` module (AC: #4)
  - [x] Create `core/apps/session_manager/lib/nebu/session/session_supervisor.ex`
  - [x] Implement `create_session/2` (wraps EtsStore.put_session/2)
  - [x] Implement `destroy_session/1` (wraps PgStore.invalidate_session/1)
  - [x] Add `@spec` for all public functions

- [x] Implement until all tests pass (AC: #1–#5)

- [x] Run `mix test --warnings-as-errors` for full umbrella (AC: #6)
  - [x] Confirm existing room_manager tests (4-1 through 4-4) pass unchanged
  - [x] Confirm existing session_manager tests (4-5) pass unchanged

---

## Dev Notes

### CRITICAL: Module Naming — epics.md vs. Codebase Reality

The `epics.md` acceptance criteria reference `SessionManager.PgStore` and `SessionManager.SessionSupervisor`. These are WRONG for this codebase. All modules follow the `Nebu.{Domain}.{Name}` convention without exception:

| Epics.md Spec | Correct Name in Codebase |
|---|---|
| `SessionManager.PgStore` | `Nebu.Session.PgStore` |
| `SessionManager.EtsStore` | `Nebu.Session.EtsStore` (already exists — Story 4-5) |
| `SessionManager.SessionSupervisor` | `Nebu.Session.SessionSupervisor` |

This convention is established by `Nebu.Room.*`, `Nebu.Session.*`, `Nebu.Signature.*`, `Nebu.Permissions.*`, `Nebu.Presence.*`.

### File Locations

```
core/apps/session_manager/
  lib/nebu/session/
    pg_store.ex                              ← CREATE (new module)
    session_supervisor.ex                    ← CREATE (new module)
    ets_store.ex                             ← DO NOT MODIFY (Story 4-5, complete)
    application.ex                           ← DO NOT MODIFY (Story 4-5, complete)
  test/nebu/session/
    pg_store_test.exs                        ← CREATE (new test file)
    session_supervisor_test.exs              ← CREATE (new test file)

gateway/migrations/
  000011_sync_tokens.up.sql                  ← CREATE
  000011_sync_tokens.down.sql                ← CREATE
```

**Do NOT create:**
- Any file under `core/apps/session_manager/lib/session_manager/` (wrong path — epics.md naming, not codebase convention)

**Existing files — do NOT modify unless necessary:**
- `lib/nebu/session/ets_store.ex` — complete, stable, used by `invalidate_session/1` via direct call
- `lib/nebu/session/application.ex` — complete, no new children needed for this story
- `lib/nebu/session/manager.ex` — placeholder, leave untouched
- `mix.exs` — `nebu_db` is already a dependency; no new deps needed

### Migration: `000011_sync_tokens.up.sql`

```sql
-- gateway/migrations/000011_sync_tokens.up.sql
-- sync_tokens: PostgreSQL persistence for since-token checkpointing.
-- Enables /sync incremental resume after gateway or core restarts.
-- user_id FK to users ensures referential integrity.

CREATE TABLE sync_tokens (
    user_id       TEXT    PRIMARY KEY REFERENCES users(user_id),
    since_token   TEXT    NOT NULL,
    last_event_id TEXT,
    updated_at    BIGINT  NOT NULL
);
```

```sql
-- gateway/migrations/000011_sync_tokens.down.sql
DROP TABLE IF EXISTS sync_tokens;
```

**Key decisions:**
- `user_id TEXT PRIMARY KEY` — one active since-token per user (per architecture G1: session-level checkpointing)
- `updated_at BIGINT NOT NULL` — architecture enforcement rule #1: BIGINT milliseconds, NOT TIMESTAMPTZ
- `last_event_id TEXT` — nullable: NULL until first event is processed after session creation
- No `TIMESTAMPTZ DEFAULT NOW()` — architecture forbids TIMESTAMPTZ; use `updated_at BIGINT`

**Note on sessions table:** The `sessions` table (created in Story 2-2 migration `000005_sessions.up.sql`) already exists. `invalidate_session/1` will DELETE rows from it. No schema change to `sessions` is needed — it already has the correct schema.

### `since_token` Generation

Per epics.md AC: `Base64.encode("#{user_id}:#{last_event_id}:#{System.monotonic_time()}")`. In Elixir, the correct standard library function is:

```elixir
Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)
```

**Why this is opaque and non-guessable:**
- `System.monotonic_time()` returns a unique integer monotonically increasing within a VM session — not system clock, not guessable across restarts
- `Base.encode64/2` with `padding: false` produces a URL-safe-compatible string (though this is standard base64, not base64url — either is acceptable per the spec's intent)
- The combination is non-guessable by external clients

**Important:** `Base64` is NOT an Elixir standard library module. The correct module is `Base`. Use `Base.encode64/2`.

### `Nebu.Session.PgStore` — Architecture Pattern

Follow the established pattern for DB modules in `session_manager`: a `@behaviour` + public API module that delegates to a configurable DB implementation module, enabling unit testing without a real PostgreSQL connection.

```
Nebu.Session.PgStore          ← public API + @behaviour (like UserStore, TokenValidator)
  delegates to: pg_store_module() = Application.get_env(:session_manager, :pg_store_module, Nebu.Session.PgStore.Postgres)
Nebu.Session.PgStore.Postgres ← production implementation (like UserStore.Postgres, etc.)
```

**File structure:**
```
lib/nebu/session/
  pg_store.ex           ← @behaviour + public delegation module
  pg_store/
    postgres.ex         ← @behaviour Nebu.Session.PgStore implementation
```

This mirrors the existing `user_store.ex` / `user_store/postgres.ex` split exactly.

### `Nebu.Session.PgStore` — Behaviour Definition

```elixir
defmodule Nebu.Session.PgStore do
  @moduledoc """
  Behaviour and delegation module for PostgreSQL since-token persistence.

  Real implementation: Nebu.Session.PgStore.Postgres (uses Nebu.Repo).
  Test implementation: configured via Application.put_env in test setup.
  """

  @callback persist_since_token(user_id :: String.t(), since_token :: String.t(), last_event_id :: String.t() | nil) ::
              :ok | {:error, term()}

  @callback get_since_token(user_id :: String.t()) ::
              {:ok, %{since_token: String.t(), last_event_id: String.t() | nil}} | {:error, :not_found}

  @callback invalidate_session(user_id :: String.t()) ::
              :ok | {:error, term()}

  @spec persist_since_token(String.t(), String.t(), String.t() | nil) :: :ok | {:error, term()}
  def persist_since_token(user_id, since_token, last_event_id) do
    pg_store_module().persist_since_token(user_id, since_token, last_event_id)
  end

  @spec get_since_token(String.t()) :: {:ok, map()} | {:error, :not_found}
  def get_since_token(user_id) do
    pg_store_module().get_since_token(user_id)
  end

  @spec invalidate_session(String.t()) :: :ok | {:error, term()}
  def invalidate_session(user_id) do
    pg_store_module().invalidate_session(user_id)
  end

  defp pg_store_module do
    Application.get_env(:session_manager, :pg_store_module, Nebu.Session.PgStore.Postgres)
  end
end
```

### `Nebu.Session.PgStore.Postgres` — SQL Implementation

```elixir
defmodule Nebu.Session.PgStore.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.PgStore."

  @behaviour Nebu.Session.PgStore

  @upsert_since_token_sql """
  INSERT INTO sync_tokens (user_id, since_token, last_event_id, updated_at)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (user_id) DO UPDATE
    SET since_token   = EXCLUDED.since_token,
        last_event_id = EXCLUDED.last_event_id,
        updated_at    = EXCLUDED.updated_at
  """

  @get_since_token_sql """
  SELECT since_token, last_event_id FROM sync_tokens WHERE user_id = $1
  """

  @delete_sync_token_sql """
  DELETE FROM sync_tokens WHERE user_id = $1
  """

  @delete_session_sql """
  DELETE FROM sessions WHERE user_id = $1
  """

  @impl Nebu.Session.PgStore
  def persist_since_token(user_id, since_token, last_event_id) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @upsert_since_token_sql,
           [user_id, since_token, last_event_id, now_ms]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @impl Nebu.Session.PgStore
  def get_since_token(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @get_since_token_sql, [user_id]) do
      {:ok, %{rows: [[since_token, last_event_id]]}} ->
        {:ok, %{since_token: since_token, last_event_id: last_event_id}}
      {:ok, %{rows: []}} ->
        {:error, :not_found}
      {:error, reason} ->
        {:error, reason}
    end
  end

  @impl Nebu.Session.PgStore
  def invalidate_session(user_id) do
    result = Nebu.Repo.transaction(fn ->
      with {:ok, _} <- query(@delete_sync_token_sql, [user_id]),
           {:ok, _} <- query(@delete_session_sql, [user_id]) do
        :ok
      else
        {:error, reason} -> Nebu.Repo.rollback(reason)
      end
    end)

    case result do
      {:ok, :ok} ->
        # Both DB deletes succeeded — now evict from ETS
        Nebu.Session.EtsStore.delete_session(user_id)
        :ok
      {:error, reason} ->
        # Transaction rolled back — do NOT evict from ETS (atomicity)
        {:error, reason}
    end
  end

  defp query(sql, params) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, params) do
      {:ok, _} = ok -> ok
      {:error, _} = err -> err
    end
  end
end
```

**Critical atomicity rule:** ETS eviction via `Nebu.Session.EtsStore.delete_session/1` MUST happen AFTER the PostgreSQL transaction commits successfully. If the DB transaction fails, the ETS entry must NOT be deleted. This maintains consistency: ETS is the hot-path view of what is currently active; PG is the authoritative store.

### `Nebu.Session.SessionSupervisor` — Orchestration Module

This is a plain module (NOT a GenServer or OTP Supervisor) — despite its name, it is a thin coordination layer that orchestrates ETS and PG operations. The name follows the epics.md spec.

```elixir
defmodule Nebu.Session.SessionSupervisor do
  @moduledoc """
  Orchestration layer for session lifecycle management.

  create_session/2: writes to ETS (hot-path cache) for immediate /sync access.
  destroy_session/1: delegates to PgStore.invalidate_session/1 which handles
    both DB cleanup (sync_tokens + sessions in a transaction) and ETS eviction.
  """

  @spec create_session(String.t(), map()) :: :ok
  def create_session(user_id, session_map) do
    Nebu.Session.EtsStore.put_session(user_id, session_map)
  end

  @spec destroy_session(String.t()) :: :ok | {:error, term()}
  def destroy_session(user_id) do
    Nebu.Session.PgStore.invalidate_session(user_id)
  end
end
```

**Note:** `SessionSupervisor` is NOT added to the `Nebu.Session.Application` children list. It is a module with plain functions, not a process. No `start_link` or `use GenServer`.

### Test Pattern: Fake DB Module for Unit Tests

Unit tests MUST NOT require a live PostgreSQL connection. Follow the same fake-DB injection pattern established by `UserStore`, `TokenValidator`, and `BootstrapChecker`:

```elixir
defmodule Nebu.Session.PgStoreTest do
  use ExUnit.Case, async: false
  # async: false — Application.put_env is process-global; :NebuSessions is a named ETS table

  # ---------------------------------------------------------------------------
  # In-memory fake PG store using ETS for unit test isolation
  # ---------------------------------------------------------------------------
  defmodule FakePgStore do
    @behaviour Nebu.Session.PgStore

    @impl Nebu.Session.PgStore
    def persist_since_token(user_id, since_token, last_event_id) do
      case :ets.whereis(:pg_store_test) do
        :undefined -> {:error, :table_not_found}
        _ ->
          :ets.insert(:pg_store_test, {{:sync_token, user_id}, %{since_token: since_token, last_event_id: last_event_id}})
          :ok
      end
    end

    @impl Nebu.Session.PgStore
    def get_since_token(user_id) do
      case :ets.lookup(:pg_store_test, {:sync_token, user_id}) do
        [{{:sync_token, ^user_id}, data}] -> {:ok, data}
        [] -> {:error, :not_found}
      end
    end

    @impl Nebu.Session.PgStore
    def invalidate_session(user_id) do
      :ets.delete(:pg_store_test, {:sync_token, user_id})
      :ets.delete(:pg_store_test, {:session, user_id})
      Nebu.Session.EtsStore.delete_session(user_id)
      :ok
    end
  end

  defmodule FailingPgStore do
    @behaviour Nebu.Session.PgStore

    @impl Nebu.Session.PgStore
    def persist_since_token(_user_id, _since_token, _last_event_id),
      do: {:error, :db_connection_lost}

    @impl Nebu.Session.PgStore
    def get_since_token(_user_id), do: {:error, :db_connection_lost}

    @impl Nebu.Session.PgStore
    def invalidate_session(_user_id), do: {:error, :db_connection_lost}
  end

  setup do
    # ETS for fake PG store
    if :ets.whereis(:pg_store_test) != :undefined, do: :ets.delete(:pg_store_test)
    :ets.new(:pg_store_test, [:named_table, :set, :public])

    # Clear :NebuSessions (exists because Application is running)
    if :ets.whereis(:NebuSessions) != :undefined do
      :ets.delete_all_objects(:NebuSessions)
    end

    Application.put_env(:session_manager, :pg_store_module, FakePgStore)

    on_exit(fn ->
      Application.delete_env(:session_manager, :pg_store_module)
      if :ets.whereis(:pg_store_test) != :undefined, do: :ets.delete(:pg_store_test)
      if :ets.whereis(:NebuSessions) != :undefined do
        :ets.delete_all_objects(:NebuSessions)
      end
    end)

    :ok
  end
end
```

**Key test patterns to follow from existing tests:**
- `Application.put_env(:session_manager, :pg_store_module, FakePgStore)` in setup
- `Application.delete_env(:session_manager, :pg_store_module)` in `on_exit`
- Create a temporary ETS table for the fake PG store state (`:pg_store_test`)
- `:ets.delete/1` the temporary table in `on_exit` (it's a test-only table — safe to delete)
- `:ets.delete_all_objects(:NebuSessions)` (do NOT delete the shared ETS table itself)

### Architecture Compliance Rules

| Rule | Requirement | Source |
|---|---|---|
| Rule #1 | `updated_at` in `sync_tokens` is BIGINT milliseconds — NOT TIMESTAMPTZ | `architecture.md#Enforcement rule #1` |
| Rule #6 | `{:ok, result}` / `{:error, reason}` — no raise/throw for business logic | `architecture.md#Enforcement rule #6` |
| ADR-002 | ETS is hot-path cache; PostgreSQL is the authoritative store | `architecture.md#ADR-002` |
| G1 | Session invalidation removes from both ETS and PG | `architecture.md#G1` |
| Naming | All modules follow `Nebu.Session.*` — ignore `SessionManager.*` in epics.md | Code convention |
| Transactions | Use `Nebu.Repo.transaction/1` for multi-table deletes (atomicity) | `user_provisioner/postgres.ex` |
| No Ecto schemas | Use raw SQL via `Ecto.Adapters.SQL.query/3` — no Ecto schemas or changesets | `Nebu.Room.DB` pattern |
| async: false | All session_manager tests — shared ETS + global env | All existing tests |
| ETS eviction after TX | `EtsStore.delete_session/1` called ONLY after successful DB transaction | Atomicity requirement |

### `Nebu.Repo.transaction/1` Pattern

From `user_provisioner/postgres.ex` (the canonical established pattern):

```elixir
Nebu.Repo.transaction(fn ->
  with {:ok, _} <- query(sql_1, params_1),
       {:ok, _} <- query(sql_2, params_2) do
    :ok
  else
    {:error, reason} -> Nebu.Repo.rollback(reason)
  end
end)
# Returns: {:ok, :ok} on success, {:error, reason} on rollback
```

The return value is `{:ok, inner_value}` when the transaction commits, or `{:error, reason}` when `Nebu.Repo.rollback/1` is called. Pattern-match accordingly.

### `Nebu.Repo.rollback/1` and Error Handling

`Nebu.Repo.rollback/1` is a non-local return — it raises an exception that Ecto catches and converts to `{:error, reason}`. Do NOT pattern-match its return value (it doesn't return). The `with/else` pattern shown above is correct — the `else` branch calls `Nebu.Repo.rollback(reason)` and that is all.

### `sessions` Table: Delete Pattern

The `sessions` table (migration `000005_sessions.up.sql`) has:
- `session_id TEXT PRIMARY KEY`
- `user_id TEXT NOT NULL REFERENCES users(user_id)`

`invalidate_session/1` deletes by `user_id`, which may delete multiple sessions for the same user (multi-device scenario). This is correct behavior — logout invalidates ALL sessions for the user:

```sql
DELETE FROM sessions WHERE user_id = $1
```

The `DELETE` returns rows affected but the count is not important — zero rows deleted is also valid (idempotent).

### `since_token` Token Contract

The `since_token` is an opaque string the client treats as a black box. The `/sync` handler (Stories 4-14/4-15) will look it up via `PgStore.get_since_token/1`. The format is:

```elixir
defp generate_since_token(user_id, last_event_id) do
  Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)
end
```

This function lives in whatever module generates the token (likely the gRPC handler in a later story). `PgStore.persist_since_token/3` accepts it as an opaque string — it does NOT generate it. The generation is the caller's responsibility.

### `updated_at` Field in `sync_tokens`

Architecture rule #1 requires BIGINT milliseconds. Use `Nebu.DB.Helpers.now_ms()` (already available via `{:nebu_db, in_umbrella: true}` in `mix.exs`):

```elixir
now_ms = Nebu.DB.Helpers.now_ms()
# passes now_ms as the $4 parameter to @upsert_since_token_sql
```

Do NOT use `DateTime.utc_now()`, `:os.system_time(:millisecond)`, or `NaiveDateTime`. The established pattern throughout this codebase is `Nebu.DB.Helpers.now_ms()`.

### `async: false` Requirement

All `session_manager` tests must use `async: false` because:
1. `:NebuSessions` is a global named ETS table — concurrent writes would cause race conditions
2. `Application.put_env/3` is process-global — concurrent tests would override each other's module configuration

This matches every existing test in `session_manager`: `user_store_test.exs`, `token_validator_test.exs`, `bootstrap_checker_test.exs`, `ets_store_test.exs`.

### What Story 4-6 Does NOT Implement

- No gRPC handler wiring (that is Story 4-8 and later)
- No `/sync` HTTP handler (Stories 4-14, 4-15)
- No `/logout` HTTP handler change — Story 2-19 already calls `delete_session` at the Go layer; the Elixir `invalidate_session` will be called when gRPC handlers are wired in Story 4-8
- No `PgStore` writing to the `sessions` table on create (the gateway Go layer writes there during login via Story 2-18); the Elixir side only deletes from `sessions` on invalidation
- No `SessionManager.Application` changes — `Nebu.Session.SessionSupervisor` is not a supervised process, it is a plain module

### Cross-Story Context

| Story | Relationship to 4-6 |
|---|---|
| Story 2-2 | Created `sessions` table (migration 000005) — `invalidate_session/1` deletes from it |
| Story 2-19 | Go `POST /logout` calls gRPC — Elixir side currently a stub; full wiring in Story 4-8 |
| Story 4-5 | Created `Nebu.Session.EtsStore` — `invalidate_session/1` calls `delete_session/1` to evict |
| Story 4-8 | gRPC EventBus wiring — will call `SessionSupervisor.create_session/2` on login events |
| Story 4-14 | Initial /sync — calls `PgStore.get_since_token/1` to check for existing checkpoint |
| Story 4-15 | Incremental /sync — calls `PgStore.persist_since_token/3` after delivering events |

### Build & Test Commands

```bash
# Run session_manager tests only (fast, targeted):
make test-unit-elixir

# Run full umbrella (before marking complete):
make test-unit-elixir
```

All tests run inside Docker containers — no local Elixir install needed.

**Expected result after implementation:**
- All existing Story 4-1 through 4-5 tests pass (no regression)
- New `pg_store_test.exs` tests pass (≥5 test cases)
- New `session_supervisor_test.exs` tests pass (≥2 test cases)
- 0 failures, 0 warnings with `--warnings-as-errors`

---

## Previous Story Intelligence (Story 4-5)

Key learnings from Story 4-5 implementation that directly impact Story 4-6:

1. **`Base16.encode/1` does NOT exist** — use `Base.encode16/2`. Similarly, `Base64.encode/1` does NOT exist — use `Base.encode64/2`. These are `Base` module functions.

2. **ETS ownership is Application, not GenServer** — `Nebu.Session.EtsStore` holds no state itself. `invalidate_session/1` can call `Nebu.Session.EtsStore.delete_session/1` directly (pure ETS operation, no GenServer.call needed).

3. **`async: false` is mandatory** — all session_manager tests share `:NebuSessions` (global named ETS) and `Application.put_env`. Never use `async: true`.

4. **ETS cleanup pattern:** In test `setup`, use `:ets.delete_all_objects(:NebuSessions)` — NOT `:ets.delete/1` which would destroy the shared table.

5. **Fake DB injection pattern** — `Application.put_env(:session_manager, :pg_store_module, FakePgStore)` in setup + `Application.delete_env(:session_manager, :pg_store_module)` in `on_exit`. The real implementation is the default when no env is set. Test-only ETS tables (`:pg_store_test`) are safe to create/delete within tests — only `:NebuSessions` must survive across tests.

6. **No new mix.exs dependencies** — `nebu_db` (provides `Nebu.Repo` and `Nebu.DB.Helpers`) is already a dependency of `session_manager`. `:crypto` is OTP built-in. No new hex packages needed.

7. **`Nebu.Repo.transaction/1` return value is `{:ok, inner}` or `{:error, reason}`** — NOT just `:ok`. Always pattern-match the full tuple. From `bootstrap_checker/postgres.ex`: the inner return of the transaction block is wrapped in `{:ok, ...}`.

---

## Architecture References

- `_bmad-output/planning-artifacts/architecture.md` — G1 (Sync-API: Hybrid ETS + PostgreSQL), ADR-002 (no Redis), Enforcement rule #1 (BIGINT timestamps), Rule #6 (no raise)
- `_bmad-output/planning-artifacts/epics.md` — Story 4.6 Acceptance Criteria (line ~1935)
- `core/apps/session_manager/lib/nebu/session/user_store.ex` — @behaviour + delegation pattern to replicate exactly
- `core/apps/session_manager/lib/nebu/session/user_store/postgres.ex` — raw SQL via Ecto.Adapters.SQL.query/3 pattern
- `core/apps/session_manager/lib/nebu/session/user_provisioner/postgres.ex` — Nebu.Repo.transaction/1 + Nebu.Repo.rollback/1 pattern
- `core/apps/session_manager/test/nebu/session/user_store_test.exs` — fake DB module injection test pattern
- `core/apps/session_manager/lib/nebu/session/ets_store.ex` — Story 4-5 module to call in invalidate_session
- `core/apps/session_manager/lib/nebu/session/application.ex` — Story 4-5 Application (do not modify)
- `core/apps/nebu_db/lib/nebu/db_helpers.ex` — Nebu.DB.Helpers.now_ms() source
- `gateway/migrations/000010_events.up.sql` — highest existing migration (next is 000011)
- `gateway/migrations/000005_sessions.up.sql` — sessions table schema (deleted by invalidate_session)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m] (Claude Sonnet 4.6, 1M context)

### Completion Notes List

- Implemented `Nebu.Session.PgStore` behaviour + delegation module at `core/apps/session_manager/lib/nebu/session/pg_store.ex`. Follows the `UserStore` / `TokenValidator` pattern: `@callback` definitions + public delegation functions + `pg_store_module()` private helper reading `Application.get_env(:session_manager, :pg_store_module, Nebu.Session.PgStore.Postgres)`.
- Implemented `Nebu.Session.PgStore.Postgres` at `core/apps/session_manager/lib/nebu/session/pg_store/postgres.ex`. Uses raw SQL via `Ecto.Adapters.SQL.query/3` (no Ecto schemas). `persist_since_token/3` uses UPSERT (`ON CONFLICT (user_id) DO UPDATE`). `invalidate_session/1` wraps both DELETE statements in `Nebu.Repo.transaction/1`; ETS eviction via `Nebu.Session.EtsStore.delete_session/1` happens only after `{:ok, :ok}` — atomicity rule enforced.
- Implemented `Nebu.Session.SessionSupervisor` at `core/apps/session_manager/lib/nebu/session/session_supervisor.ex` — plain module (not OTP Supervisor/GenServer). `create_session/2` delegates to `EtsStore.put_session/2`; `destroy_session/1` delegates to `PgStore.invalidate_session/1`.
- Created SQL migration `000011_sync_tokens.up.sql` with `updated_at BIGINT NOT NULL` (architecture rule #1 — no TIMESTAMPTZ). `user_id TEXT PRIMARY KEY REFERENCES users(user_id)` for FK integrity. Corresponding `000011_sync_tokens.down.sql` drops the table.
- Tests (pre-written, already in repo): `pg_store_test.exs` (8 ExUnit tests covering round-trip, not_found, invalidate atomicity, since_token opaqueness, upsert semantics, DB failure non-corruption of ETS) + `session_supervisor_test.exs` (4 ExUnit tests covering create_session ETS write, exact session map storage, destroy_session ETS removal, destroy_session PG removal).
- Full umbrella test run: 38 session_manager tests, 22 room_manager tests, 21 signature tests — 0 failures, 0 warnings with `--warnings-as-errors`.

### File List

Files to create or modify:

```
core/apps/session_manager/lib/nebu/session/pg_store.ex              ← CREATE
core/apps/session_manager/lib/nebu/session/pg_store/postgres.ex     ← CREATE
core/apps/session_manager/lib/nebu/session/session_supervisor.ex    ← CREATE
core/apps/session_manager/test/nebu/session/pg_store_test.exs       ← CREATE
core/apps/session_manager/test/nebu/session/session_supervisor_test.exs  ← CREATE
gateway/migrations/000011_sync_tokens.up.sql                        ← CREATE
gateway/migrations/000011_sync_tokens.down.sql                      ← CREATE
_bmad-output/implementation-artifacts/4-6-session-manager-postgresql-since-token-invalidation.md  ← MODIFIED (this file)
_bmad-output/implementation-artifacts/sprint-status.yaml            ← MODIFIED (status → ready-for-dev)
```

### Change Log

- 2026-04-03: Story 4-6 created — Session Manager PostgreSQL since-Token + Invalidation
- 2026-04-03: Story 4-6 implemented — All 7 files created, 38 session_manager tests pass (0 failures, 0 warnings)

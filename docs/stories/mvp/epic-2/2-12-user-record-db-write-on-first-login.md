# Story 2.12: User-Record DB-Write on First Login

Status: done

## Story

As a developer,
I want the Elixir core to create a user record in the database on first login,
so that subsequent requests have a persistent user identity to reference.

## Acceptance Criteria

1. **Given** a `ValidateToken` gRPC call with a `user_id` that does not exist in the `users` table,
   **When** processed by the `session_manager` app,
   **Then** a new row is INSERTed into `users` with `user_id`, `system_role`, `created_at = now_ms()`, `is_active = true`

2. **Given** a `ValidateToken` gRPC call with a `user_id` that already exists,
   **When** processed,
   **Then** `last_seen_at` is updated and the existing record is returned — no duplicate INSERT

3. **Given** an INSERT that conflicts on `user_id` (concurrent first-login race condition),
   **When** processed,
   **Then** the INSERT uses `ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at` to resolve safely

4. **Given** a unit test with a mock DB,
   **When** first-login provisioning is called twice with the same `user_id`,
   **Then** exactly one user record exists after both calls

## Tasks / Subtasks

- [x] Create `core/apps/nebu_db/` umbrella app (AC: #1, #2, #3)
  - [x] Create `core/apps/nebu_db/mix.exs` with `ecto_sql` and `postgrex` deps
  - [x] Create `core/apps/nebu_db/lib/nebu/repo.ex` — `Nebu.Repo` using `Ecto.Repo`
  - [x] Create `core/apps/nebu_db/lib/nebu/db_helpers.ex` — `now_ms/0` helper

- [x] Wire `nebu_db` into the umbrella (AC: #1)
  - [x] Add `nebu_db` to releases in `core/mix.exs`
  - [x] Add `ecto_sql ~> 3.12` and `postgrex ~> 0.19` to umbrella `deps` in `core/mix.exs`
  - [x] Add `NEBU_DB_URL` config block to `core/config/runtime.exs`

- [x] Create `Nebu.Session.UserStore` in session_manager (AC: #1, #2, #3)
  - [x] Create `core/apps/session_manager/lib/nebu/session/user_store.ex`
    - [x] Define `@callback upsert_user(user_id, system_role)` behaviour
    - [x] Implement delegating `upsert_user/2` via `Application.get_env(:session_manager, :db_module, UserStore.Postgres)`
  - [x] Create `core/apps/session_manager/lib/nebu/session/user_store/postgres.ex`
    - [x] Implement `upsert_user/2` with raw SQL `INSERT ... ON CONFLICT DO UPDATE` via `Ecto.Adapters.SQL.query/3`
    - [x] Use `System.system_time(:millisecond)` for `created_at` and `last_seen_at`
    - [x] Return `{:ok, user_id}` on success, `{:error, reason}` on failure
  - [x] Add `{:nebu_db, in_umbrella: true}` to `session_manager/mix.exs` deps

- [x] Write unit tests (AC: #4)
  - [x] Create `core/apps/session_manager/test/nebu/session/user_store_test.exs`
  - [x] Define `FakeUserDB` stub in the test file (ETS-backed, no external deps)
  - [x] Add `setup` that creates named ETS table and configures `:db_module`
  - [x] Test: `upsert_user` twice with same `user_id` → exactly one record in ETS table
  - [x] Test: second call updates `last_seen_at` without creating duplicate
  - [x] Remove placeholder test from `test/nebu_session_test.exs`
  - [x] Confirm `mix test apps/session_manager --warnings-as-errors` passes

## Dev Notes

### Scope

This story does **two things**:
1. Creates the `nebu_db` umbrella app — the first Elixir DB access infrastructure
2. Implements `Nebu.Session.UserStore.upsert_user/2` — the user-record upsert on login

**NOT in this story:**
- Calling `upsert_user` from the `ValidateToken` gRPC handler (Story 2.14)
- Keypair generation + PII encryption on provisioning (Story 2.13)
- Bootstrap mode / auto instance_admin assignment (Story 2.15)
- Any changes to the `users` schema — it was created in Story 2.1 migration

### The `users` Table Schema (Already Exists)

The `users` table was created in Story 2.1 migration `000004_users.up.sql`. Do NOT recreate or modify it.

```sql
CREATE TABLE users (
    user_id                   TEXT    PRIMARY KEY,
    display_name_encrypted    BYTEA,           -- filled by Story 2.13
    display_name_nonce        BYTEA,
    avatar_url_encrypted      BYTEA,
    avatar_url_nonce          BYTEA,
    system_role               TEXT    NOT NULL DEFAULT 'user',
    is_active                 BOOLEAN NOT NULL DEFAULT true,
    signing_key_id            TEXT,             -- filled by Story 2.13
    encryption_key_id         TEXT,             -- filled by Story 2.13
    created_at                BIGINT  NOT NULL, -- ← this story fills it
    last_seen_at              BIGINT            -- ← this story fills it
);
```

This story writes `user_id`, `system_role`, `created_at`, `is_active`. All other columns stay NULL until Story 2.13.

### Step 1: Create `nebu_db` Umbrella App

Create the directory structure:
```
core/apps/nebu_db/
  mix.exs
  lib/nebu/
    repo.ex
    db_helpers.ex
```

**`core/apps/nebu_db/mix.exs`:**
```elixir
defmodule Nebu.DB.MixProject do
  use Mix.Project

  def project do
    [
      app: :nebu_db,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {Nebu.DB.Application, []}
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"}
    ]
  end
end
```

**`core/apps/nebu_db/lib/nebu/repo.ex`:**
```elixir
defmodule Nebu.Repo do
  use Ecto.Repo,
    otp_app: :nebu_db,
    adapter: Ecto.Adapters.Postgres
end
```

**`core/apps/nebu_db/lib/nebu/db_helpers.ex`:**
```elixir
defmodule Nebu.DB.Helpers do
  @moduledoc "Shared DB utility functions used across all apps."

  @doc "Current UTC time in milliseconds (BIGINT for PostgreSQL)."
  @spec now_ms() :: integer()
  def now_ms, do: System.system_time(:millisecond)
end
```

**`core/apps/nebu_db/lib/nebu/db/application.ex`:**
```elixir
defmodule Nebu.DB.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [Nebu.Repo]
    opts = [strategy: :one_for_one, name: Nebu.DB.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 2: Update Umbrella `mix.exs`

**`core/mix.exs`** — add `nebu_db` to releases and umbrella deps:

```elixir
defmodule Nebu.MixProject do
  use Mix.Project

  def project do
    [
      apps_path: "apps",
      version: "0.1.0",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: [
        nebu: [
          applications: [
            nebu_db: :permanent,        # ← ADD THIS FIRST (dependency order)
            event_dispatcher: :permanent,
            permissions: :permanent,
            presence: :permanent,
            room_manager: :permanent,
            session_manager: :permanent,
            signature: :permanent
          ]
        ]
      ]
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"}
    ]
  end
end
```

**IMPORTANT:** `nebu_db` must be listed BEFORE apps that depend on it in the releases list, to ensure correct startup order.

### Step 3: Configure DB URL in `runtime.exs`

Add to `core/config/runtime.exs` (after existing PII key block):

```elixir
# DB configuration — required for all environments (nebu_db app)
if config_env() in [:prod, :dev] do
  config :nebu_db, Nebu.Repo,
    url: System.get_env("NEBU_DB_URL") || raise("NEBU_DB_URL is not set"),
    pool_size: 10
end
```

**Do NOT add a test env DB config** — Story 2.12 unit tests use a fake DB, not a real Postgrex connection.

### Step 4: Create `Nebu.Session.UserStore`

**`core/apps/session_manager/lib/nebu/session/user_store.ex`:**

```elixir
defmodule Nebu.Session.UserStore do
  @moduledoc """
  Manages user record persistence on first login.

  Delegates to a configurable DB module for testability.
  Real implementation: `Nebu.Session.UserStore.Postgres` (uses Nebu.Repo).
  Test implementation: configured via Application.put_env in test setup.
  """

  @callback upsert_user(user_id :: String.t(), system_role :: String.t()) ::
              {:ok, String.t()} | {:error, term()}

  @doc """
  Upserts a user record on login.
  - Inserts on first login (created_at = now_ms, is_active = true).
  - Updates last_seen_at on subsequent logins.
  - ON CONFLICT (user_id) resolves concurrent first-login race conditions.

  Returns {:ok, user_id} on success, {:error, reason} on failure.
  """
  @spec upsert_user(String.t(), String.t()) :: {:ok, String.t()} | {:error, term()}
  def upsert_user(user_id, system_role) do
    db_module().upsert_user(user_id, system_role)
  end

  defp db_module do
    Application.get_env(:session_manager, :db_module, Nebu.Session.UserStore.Postgres)
  end
end
```

**`core/apps/session_manager/lib/nebu/session/user_store/postgres.ex`:**

```elixir
defmodule Nebu.Session.UserStore.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.UserStore."

  @behaviour Nebu.Session.UserStore

  @sql """
  INSERT INTO users (user_id, system_role, created_at, is_active)
  VALUES ($1, $2, $3, true)
  ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.created_at
  RETURNING user_id
  """

  @impl Nebu.Session.UserStore
  def upsert_user(user_id, system_role) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql, [user_id, system_role, now_ms]) do
      {:ok, %{rows: [[^user_id]]}} -> {:ok, user_id}
      {:error, reason} -> {:error, reason}
    end
  end
end
```

**Why raw SQL, not Ecto.Schema?** Go gateway owns all schema migrations. Elixir has no schema-write access. Defining Ecto.Schema in Elixir would duplicate the schema definition and create drift risk. Raw SQL via `Ecto.Adapters.SQL.query/3` is intentional.

**Why `ON CONFLICT ... SET last_seen_at = EXCLUDED.created_at`?** The `EXCLUDED` table references the value that WOULD have been inserted. Since we pass `now_ms` as `$3` (mapped to `created_at` in the INSERT), we reuse that same timestamp for `last_seen_at` in the UPDATE path. This is idiomatic PostgreSQL — no separate UPDATE needed.

**Why `RETURNING user_id`?** Confirms the row was actually written/updated. Pattern-matches on `[[^user_id]]` to verify the result.

### Step 5: Update `session_manager/mix.exs`

Add `nebu_db` as an in-umbrella dependency:

```elixir
defp deps do
  [
    {:nebu_db, in_umbrella: true}
  ]
end
```

### Step 6: Unit Tests (Mock DB Pattern)

**`core/apps/session_manager/test/nebu/session/user_store_test.exs`:**

```elixir
defmodule Nebu.Session.UserStoreTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.UserStore

  # ETS-backed fake DB — no Postgrex connection needed for unit tests
  defmodule FakeUserDB do
    @behaviour Nebu.Session.UserStore

    @impl Nebu.Session.UserStore
    def upsert_user(user_id, _system_role) do
      now_ms = System.system_time(:millisecond)

      case :ets.lookup(:user_store_test, user_id) do
        [] ->
          :ets.insert(:user_store_test, {user_id, now_ms})
          {:ok, user_id}

        [{^user_id, _created_at}] ->
          # Simulate ON CONFLICT DO UPDATE SET last_seen_at
          :ets.update_element(:user_store_test, user_id, {2, now_ms})
          {:ok, user_id}
      end
    end
  end

  setup do
    # Create fresh ETS table for each test
    if :ets.whereis(:user_store_test) != :undefined do
      :ets.delete(:user_store_test)
    end

    :ets.new(:user_store_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :db_module, FakeUserDB)

    on_exit(fn ->
      Application.delete_env(:session_manager, :db_module)
      if :ets.whereis(:user_store_test) != :undefined do
        :ets.delete(:user_store_test)
      end
    end)

    :ok
  end

  describe "upsert_user/2" do
    test "inserts new user record on first login" do
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")
      assert [{"@kai:nebu.local", _}] = :ets.lookup(:user_store_test, "@kai:nebu.local")
    end

    test "idempotent: two calls with same user_id result in exactly one record" do
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")

      # ON CONFLICT: exactly one row, not two
      assert 1 == length(:ets.tab2list(:user_store_test))
    end

    test "second call updates last_seen_at (upsert, no duplicate)" do
      assert {:ok, "@alex:nebu.local"} = UserStore.upsert_user("@alex:nebu.local", "instance_admin")
      [{_, ts1}] = :ets.lookup(:user_store_test, "@alex:nebu.local")

      Process.sleep(1)  # ensure different timestamp

      assert {:ok, "@alex:nebu.local"} = UserStore.upsert_user("@alex:nebu.local", "instance_admin")
      [{_, ts2}] = :ets.lookup(:user_store_test, "@alex:nebu.local")

      assert ts2 >= ts1
    end
  end
end
```

**Why `async: false`?** `Application.put_env/3` is process-global — running concurrent tests with different `:db_module` configs would cause flakiness.

**Why ETS, not Mox?** No external dep needed. The ETS table perfectly models the `ON CONFLICT` idempotency semantics (`[:set]` ETS table allows only one entry per key, exactly like PostgreSQL's `ON CONFLICT (user_id)`).

### Step 7: Remove Placeholder Test

The existing `core/apps/session_manager/test/nebu_session_test.exs` has a placeholder test. **REMOVE IT** entirely — the new `user_store_test.exs` replaces it.

Replace content of `core/apps/session_manager/test/nebu_session_test.exs` with just:
```elixir
# Tests for session_manager are in test/nebu/session/user_store_test.exs
```

OR delete the file entirely. Either is fine — just don't leave conflicting placeholder tests.

### Architecture Compliance

**AI-Constraint #1:** `created_at` stored as `BIGINT` (milliseconds) — `System.system_time(:millisecond)` → `$3` in SQL → `BIGINT NOT NULL` column ✓

**AI-Constraint #2:** Auth token NEVER forwarded to Elixir. This story deals only with `user_id` + `system_role` from gRPC metadata — both are safe derived values, not the raw JWT ✓

**AI-Constraint #6:** All fallible operations return `{:ok, result}` / `{:error, reason}` — no throw/raise for business logic ✓

**AI-Constraint #4:** Env var follows `NEBU_{COMPONENT}_{KEY}` pattern → `NEBU_DB_URL` ✓

### Critical Anti-Patterns to Avoid

**NEVER define `Ecto.Schema` in Elixir** — Go owns the schema, Elixir uses raw SQL only:
```elixir
# ❌ WRONG — schema duplication, Go owns migrations
defmodule Nebu.User do
  use Ecto.Schema
  schema "users" do ...
end

# ✅ CORRECT — raw SQL via Ecto.Adapters.SQL.query
Ecto.Adapters.SQL.query(Nebu.Repo, "INSERT INTO users ...", params)
```

**NEVER use `Repo.insert/2` or `Repo.update/2`** — these require Ecto.Schema which we don't have:
```elixir
# ❌ WRONG — requires Ecto.Schema
Nebu.Repo.insert(%User{user_id: id})

# ✅ CORRECT — raw SQL
Ecto.Adapters.SQL.query(Nebu.Repo, @sql, [user_id, system_role, now_ms])
```

**NEVER use `DateTime.utc_now()`** for timestamps — all timestamps are `BIGINT` milliseconds:
```elixir
# ❌ WRONG — wrong type for BIGINT column
created_at = DateTime.utc_now()

# ✅ CORRECT — milliseconds since epoch
created_at = System.system_time(:millisecond)
```

**NEVER raise or throw on DB error in business logic** — always return error tuple:
```elixir
# ❌ WRONG — raises on failure
Ecto.Adapters.SQL.query!(Nebu.Repo, @sql, params)

# ✅ CORRECT — returns {:error, reason}
case Ecto.Adapters.SQL.query(Nebu.Repo, @sql, params) do
  {:ok, _} -> {:ok, user_id}
  {:error, reason} -> {:error, reason}
end
```

**NEVER test against a real PostgreSQL in unit tests** — unit tests use the FakeDB. Integration tests (Story 2.21) will test against real DB.

### Previous Story Intelligence (2.11)

- **Test module structure:** `use ExUnit.Case, async: true` is the default in this project. **However, Story 2.12 must use `async: false`** because `Application.put_env` is global state (deviates from previous stories).
- **`--warnings-as-errors` discipline:** All unused variables must be prefixed with `_`. In the ETS-based FakeDB mock, check that all function args are used or prefixed.
- **`@behaviour`:** The `@behaviour Nebu.Session.UserStore` annotation on `Postgres` and `FakeUserDB` gets compiler-checked — correct callback signatures are enforced at compile time.

### Build & Test Commands

```bash
# Run session_manager tests only:
cd core && mix test apps/session_manager --warnings-as-errors

# Run ALL Elixir unit tests (no regressions):
make test-unit-elixir

# Install new deps after editing mix.exs:
cd core && mix deps.get
```

**Expected output (session_manager):** `3 tests, 0 failures`

**Regression check:** `make test-unit-elixir` must pass for all apps (signature, event_dispatcher, permissions, presence, room_manager, session_manager, nebu_db).

### Files to Create / Modify

**CREATE:**
```
core/apps/nebu_db/
  mix.exs
  lib/nebu/
    repo.ex
    db_helpers.ex
    db/
      application.ex
core/apps/session_manager/lib/nebu/session/
  user_store.ex
  user_store/
    postgres.ex
core/apps/session_manager/test/nebu/session/
  user_store_test.exs
```

**MODIFY:**
```
core/mix.exs                                              ← add nebu_db to releases + deps
core/config/runtime.exs                                   ← add NEBU_DB_URL config
core/apps/session_manager/mix.exs                         ← add nebu_db in_umbrella dep
core/apps/session_manager/test/nebu_session_test.exs      ← remove placeholder test
```

**DO NOT TOUCH:**
- `core/apps/signature/` — no changes
- `core/apps/event_dispatcher/` — no changes (ValidateToken wiring is Story 2.14)
- Any gateway files or migrations — Go owns migrations
- `core/apps/session_manager/lib/nebu/session/application.ex` — no changes needed
- Any `*_test.exs` in other apps

### Project Structure Notes

- `nebu_db` is an umbrella app — its `mix.exs` must reference umbrella's `build_path`, `config_path`, `deps_path`, and `lockfile` (same pattern as all other apps in `core/apps/`)
- `Nebu.Repo` starts under `Nebu.DB.Application`'s supervisor — add `nebu_db: :permanent` BEFORE dependent apps in the umbrella release definition so it starts first
- `Application.get_env(:session_manager, :db_module, ...)` — `compile_env` is NOT used here because the db_module must be swappable at test runtime via `put_env`

### References

- [Source: epics.md#Story-2.12] Authoritative user story, acceptance criteria
- [Source: epics.md#Story-2.13] Scope boundary — keypair + PII encryption is next story
- [Source: epics.md#Story-2.14] Scope boundary — ValidateToken gRPC wiring is that story
- [Source: architecture.md#Project-Structure] `nebu_db` app in directory tree (line ~996)
- [Source: architecture.md#Enforcement rule 1] BIGINT timestamps — `System.system_time(:millisecond)`
- [Source: architecture.md#Enforcement rule 6] `{:ok, result}` / `{:error, reason}` for all fallible ops
- [Source: architecture.md#Resolved-Migrations] Go owns schema, Elixir has no schema-write access → raw SQL only
- [Source: architecture.md#Daten-Grenze] `Elixir Core → [TLS] → PostgreSQL — Business-Logic-Writes`
- [Source: gateway/migrations/000004_users.up.sql] `users` table schema (Story 2.1 created it)
- [Source: implementation-artifacts/2-11-*.md] Umbrella dep pattern, `--warnings-as-errors`, `@behaviour`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

### Completion Notes List

- Created `nebu_db` umbrella app with `Nebu.Repo` (Ecto.Repo/Postgres), `Nebu.DB.Helpers.now_ms/0`, and `Nebu.DB.Application` supervisor.
- Added `ecto_sql ~> 3.12` and `postgrex ~> 0.19` to umbrella `mix.exs` deps; `nebu_db: :permanent` first in releases list for correct startup order.
- Added `NEBU_DB_URL` runtime config block for `:prod` and `:dev` envs only (no test-env DB config — unit tests use FakeUserDB).
- Implemented `Nebu.Session.UserStore` (behaviour + delegation via `Application.get_env`) and `Nebu.Session.UserStore.Postgres` (raw SQL `INSERT ... ON CONFLICT DO UPDATE` — no Ecto.Schema by design).
- 3 unit tests (ETS-backed FakeUserDB, `async: false`): insert on first login, idempotent upsert (exactly 1 record), `last_seen_at` updated on second call.
- Placeholder test in `nebu_session_test.exs` replaced with comment.
- `make test-unit-elixir`: all apps green — 3 tests (session_manager), 0 failures; no regressions.

### File List

core/apps/nebu_db/mix.exs
core/apps/nebu_db/lib/nebu/repo.ex
core/apps/nebu_db/lib/nebu/db_helpers.ex
core/apps/nebu_db/lib/nebu/db/application.ex
core/apps/session_manager/lib/nebu/session/user_store.ex
core/apps/session_manager/lib/nebu/session/user_store/postgres.ex
core/apps/session_manager/test/nebu/session/user_store_test.exs
core/mix.exs
core/config/runtime.exs
core/apps/session_manager/mix.exs
core/apps/session_manager/test/nebu_session_test.exs
core/mix.lock

## Change Log

- 2026-03-27: Created `nebu_db` umbrella app (Ecto.Repo, db_helpers, Application supervisor). Wired into umbrella mix.exs (releases + deps). Added NEBU_DB_URL runtime config. Implemented `Nebu.Session.UserStore` behaviour + Postgres implementation (raw SQL upsert, ON CONFLICT). Added 3 ETS-backed unit tests. Removed placeholder session_manager test.
- 2026-03-27: **Code Review (claude-opus-4-6)** — 2 fixes applied: (1) `Nebu.DB.Application` now conditionally starts `Nebu.Repo` only when config is present (eliminates 10 Postgrex error lines in test env); (2) `postgres.ex` now uses `Nebu.DB.Helpers.now_ms/0` instead of direct `System.system_time(:millisecond)` call. Added `core/mix.lock` to File List. All 38 tests pass, 0 failures.

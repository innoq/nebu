# Story 2.15: Bootstrap Mode — Auto Instance Admin Assignment

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the first OIDC login to automatically receive `instance_admin` rights,
so that a fresh deployment can be configured without a chicken-and-egg problem.

## Acceptance Criteria

1. **Given** a fresh deployment with no users in the `users` table,
   **When** the first `ValidateToken` call is processed,
   **Then** the resulting user record is assigned `system_role = 'instance_admin'` regardless of the `nebu_role` claim

2. **Given** bootstrap mode is active,
   **When** checked via `server_config` table,
   **Then** `SELECT value FROM server_config WHERE key = 'bootstrap_active'` returns `'true'`

3. **Given** concurrent first-login requests (race condition),
   **When** two requests arrive simultaneously,
   **Then** exactly one user receives `instance_admin` — the other receives the role from their `nebu_role` claim (atomic check using PostgreSQL transaction)

4. **Given** bootstrap mode is active and `GET /admin/bootstrap` is called,
   **When** processed,
   **Then** it responds `200 OK` (bootstrap route is accessible)

## Tasks / Subtasks

- [x] Task 1: Create `Nebu.Session.BootstrapChecker` behaviour + Postgres implementation (AC: #1, #2, #3)
  - [x] Create `core/apps/session_manager/lib/nebu/session/bootstrap_checker.ex` (behaviour + delegation)
  - [x] Create `core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex` (advisory lock + atomic SQL)

- [x] Task 2: Modify `TokenValidator.Postgres.provision_new_user/4` to use BootstrapChecker (AC: #1, #3)
  - [x] Modify `core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex`

- [x] Task 3: Add `GET /admin/bootstrap` endpoint in Go gateway (AC: #4)
  - [x] Create `gateway/internal/admin/bootstrap.go`
  - [x] Register route in `gateway/cmd/gateway/main.go`

- [x] Task 4: Write Elixir unit tests (AC: #1, #2, #3)
  - [x] Create `core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs`
  - [x] Update `core/apps/session_manager/test/nebu/session/token_validator_test.exs` if needed — not needed, FakeValidator bypasses Postgres path
  - [x] Confirm `mix test apps/session_manager --warnings-as-errors` passes (via make test-unit-elixir)

- [x] Task 5: Write Go unit test (AC: #4)
  - [x] Create `gateway/internal/admin/bootstrap_test.go`
  - [x] Confirm `make test-unit-go` passes

## Dev Notes

### Scope

This story does **three things**:
1. Creates a `BootstrapChecker` module that atomically detects bootstrap mode and overrides `system_role` to `instance_admin` for the first user
2. Integrates bootstrap checking into the existing `TokenValidator.Postgres` new-user flow
3. Adds a `GET /admin/bootstrap` endpoint in Go gateway to check bootstrap status

**NOT in this story:**
- Permanent deactivation via `bootstrap_completed` INSERT — Story 2.16
- `GET /admin/bootstrap` returning 404 after deactivation — Story 2.16
- Bootstrap UI welcome page — Story 3.7 (Epic 3)
- Any changes to `UserProvisioner` — Story 2.13 code is frozen

### Task 1: BootstrapChecker Behaviour + Postgres Implementation

Follows the established pattern from `UserStore`, `UserProvisioner`, and `TokenValidator`: behaviour module with delegation + Postgres implementation.

**`core/apps/session_manager/lib/nebu/session/bootstrap_checker.ex`** (behaviour + delegation):

```elixir
defmodule Nebu.Session.BootstrapChecker do
  @moduledoc """
  Checks bootstrap mode and resolves the effective system_role for a new user.

  Bootstrap mode is active when:
  - No 'bootstrap_completed' row exists in server_config (forward-compat for Story 2.16)
  - No users exist in the users table

  When bootstrap triggers: inserts 'bootstrap_active' = 'true' into server_config,
  and returns 'instance_admin' as the resolved role.

  Uses pg_advisory_xact_lock for race condition safety.
  """

  @callback upsert_with_bootstrap(
              user_id :: String.t(),
              system_role :: String.t()
            ) :: {:ok, {String.t(), String.t()}} | {:error, term()}

  @spec upsert_with_bootstrap(String.t(), String.t()) ::
          {:ok, {String.t(), String.t()}} | {:error, term()}
  def upsert_with_bootstrap(user_id, system_role) do
    impl_module().upsert_with_bootstrap(user_id, system_role)
  end

  defp impl_module do
    Application.get_env(:session_manager, :bootstrap_checker_module,
      Nebu.Session.BootstrapChecker.Postgres)
  end
end
```

**`core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex`:**

The Postgres implementation wraps bootstrap check + user upsert in a single transaction with an advisory lock. This is the **critical atomicity requirement** from AC #3.

**Advisory lock approach:** `pg_advisory_xact_lock(N)` is transaction-scoped. The lock is held until the transaction commits or rolls back. If two concurrent requests try to bootstrap, the second one blocks until the first commits. When the second proceeds, it sees the first user and skips bootstrap.

```elixir
@bootstrap_lock_id 2015  # Arbitrary constant, unique to bootstrap checking

# SQL: Check if bootstrap conditions are met
@check_sql """
SELECT
  NOT EXISTS(SELECT 1 FROM server_config WHERE key = 'bootstrap_completed')
  AND NOT EXISTS(SELECT 1 FROM users)
AS is_bootstrap
"""

# SQL: Upsert user (same as UserStore.Postgres but with resolved role)
@upsert_sql """
INSERT INTO users (user_id, system_role, created_at, is_active)
VALUES ($1, $2, $3, true)
ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.created_at
RETURNING user_id, system_role
"""

# SQL: Record bootstrap activation in server_config
@flag_sql """
INSERT INTO server_config (key, value, set_at)
VALUES ('bootstrap_active', 'true', $1)
ON CONFLICT (key) DO NOTHING
"""
```

**Transaction flow:**
1. Acquire advisory lock `pg_advisory_xact_lock(@bootstrap_lock_id)` — serializes concurrent bootstrap checks
2. Execute `@check_sql` — returns `true` if no `bootstrap_completed` AND no users
3. Determine `resolved_role`: `"instance_admin"` if bootstrap, else the OIDC-derived `system_role`
4. Execute `@upsert_sql` with `resolved_role` — creates user record
5. If bootstrap triggered: execute `@flag_sql` — records `bootstrap_active = 'true'` in `server_config`
6. Return `{:ok, {user_id, resolved_role}}`
7. Transaction commits — advisory lock released, next request proceeds

**Use `Nebu.DB.Helpers.now_ms()`** for timestamps (same as `UserStore.Postgres`).

**`ON CONFLICT (key) DO NOTHING`** on the flag SQL: The `server_config` table has RLS with INSERT-only policy. If `bootstrap_active` already exists (shouldn't happen due to lock, but defensive), it silently skips.

**Why NOT a CTE-based single SQL:** A single SQL statement is atomic within its implicit transaction, but `pg_advisory_xact_lock` in a CTE can have surprising behavior with query planning. An explicit `Nebu.Repo.transaction/1` with sequential statements is clearer and correctly holds the lock across the full sequence.

### Task 2: Modify TokenValidator.Postgres

**Modify `core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex` — change `provision_new_user/4`:**

Replace the direct `UserStore.upsert_user/2` call with `BootstrapChecker.upsert_with_bootstrap/2`:

```elixir
defp provision_new_user(user_id, system_role, display_name, email) do
  server_key = Application.get_env(:signature, :pii_encryption_key)

  with {:ok, {^user_id, resolved_role}} <-
         Nebu.Session.BootstrapChecker.upsert_with_bootstrap(user_id, system_role),
       {:ok, :provisioned} <-
         Nebu.Session.UserProvisioner.provision_user(user_id, display_name, email, server_key) do
    {:ok, %{
      user_id: user_id,
      system_role: resolved_role,
      display_name: display_name,
      is_active: true
    }}
  else
    {:error, reason} -> {:error, reason}
  end
end
```

**Critical change vs. old code:** The old code returned `system_role` (the OIDC claim) directly. The new code returns `resolved_role` (from the DB/bootstrap check). This is the fix noted in Story 2.14 dev notes: "Bootstrap mode will need to override system_role for the first user to instance_admin."

**`UserStore.upsert_user/2` is NOT called directly anymore** for new users — `BootstrapChecker.upsert_with_bootstrap/2` subsumes that responsibility (same SQL, wrapped in bootstrap logic). `UserStore` remains unchanged and is still used by other code paths if needed.

### Task 3: Go Gateway Bootstrap Endpoint

**Create `gateway/internal/admin/bootstrap.go`:**

Simple handler that checks `server_config` for bootstrap status.

```go
// BootstrapHandler checks if bootstrap mode is currently active.
// Returns 200 OK with {"bootstrap_active": true} if active.
// Returns 200 OK with {"bootstrap_active": false} if not active.
//
// Bootstrap is active when:
// - 'bootstrap_active' exists in server_config AND
// - 'bootstrap_completed' does NOT exist in server_config
//
// Before first login: no 'bootstrap_active' row exists yet, but the system IS
// in a bootstrappable state. The handler checks the users table as fallback:
// if no users exist and no bootstrap_completed → bootstrap is available.
```

**SQL for the handler (2 queries, same DB connection gateway already uses):**
```sql
-- Query 1: Check for bootstrap flags
SELECT key, value FROM server_config WHERE key IN ('bootstrap_active', 'bootstrap_completed');

-- Query 2: Check if any users exist (only if no bootstrap_completed found)
SELECT EXISTS(SELECT 1 FROM users);
```

**Logic:**
- If `bootstrap_completed` exists → `{"bootstrap_active": false}`
- If `bootstrap_active` exists AND no `bootstrap_completed` → `{"bootstrap_active": true}`
- If neither exists AND no users → `{"bootstrap_active": true}` (pre-first-login state)
- If neither exists AND users exist → `{"bootstrap_active": false}` (bootstrap window passed)

**Response format:** `Content-Type: application/json`, status 200 in all cases. The AC says "responds 200 OK" when active. For the not-active case, also return 200 with `bootstrap_active: false` (Story 2.16 will change this to 404).

**The handler needs a `*sql.DB` parameter.** Follow the existing pattern from `serverconfig.go` — accept `dbURL string` and open a connection, OR better: accept a `*sql.DB` that's already open from the gateway's main setup. Check how `main.go` currently initializes DB and pass the same connection.

**Register in `gateway/cmd/gateway/main.go`:**
```go
mux.HandleFunc("GET /admin/bootstrap", bootstrapHandler.Handler)
```

Place it after the existing admin auth routes (line 117).

### Task 4: Elixir Unit Tests

**`core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs`:**

Follow the established pattern from `token_validator_test.exs` and `user_store_test.exs`: use `FakeBootstrapChecker` with ETS for recording calls, configure via `Application.put_env`.

Test cases:
1. **Bootstrap active — first user gets instance_admin:** Call `upsert_with_bootstrap("@kai:nebu.local", "user")` when fake returns bootstrap-active → verify returns `{:ok, {"@kai:nebu.local", "instance_admin"}}`
2. **Bootstrap not active — OIDC role preserved:** Call with `"user"` when fake returns not-bootstrap → verify returns `{:ok, {"@kai:nebu.local", "user"}}`
3. **Bootstrap not active — admin role preserved:** Call with `"instance_admin"` when not bootstrap → verify returns `{:ok, {"@kai:nebu.local", "instance_admin"}}`
4. **Delegation works:** Verify `Application.get_env(:session_manager, :bootstrap_checker_module)` is respected

**Note on race condition testing (AC #3):** True race condition testing requires integration tests with a real PostgreSQL instance and concurrent connections. The unit test level verifies the interface contract and delegation. The advisory lock atomicity is a PostgreSQL guarantee — no need to test PostgreSQL itself. Story 2.21 (Gherkin E2E) would be the appropriate place for an integration test of concurrent bootstrap.

**Update `token_validator_test.exs` if needed:** The existing tests for `TokenValidator` use a `FakeValidator` that bypasses `provision_new_user`. If there are tests that exercise the Postgres implementation path through mocks, update them to configure `bootstrap_checker_module` as well.

### Task 5: Go Unit Test

**`gateway/internal/admin/bootstrap_test.go`:**

Follow the pattern from `auth_test.go`: use `httptest.NewRequest` + `httptest.NewRecorder`.

Since the handler queries PostgreSQL, the unit test needs either:
- A mock/interface for the DB query (preferred for unit tests)
- Or: accept a `BootstrapChecker` interface in Go

**Simplest approach:** The handler accepts a function or interface for querying bootstrap status. In tests, provide a fake. In production, provide the real SQL-backed implementation.

Test cases:
1. Bootstrap active → response contains `"bootstrap_active": true` and status 200
2. Bootstrap not active → response contains `"bootstrap_active": false` and status 200
3. DB error → appropriate error response

### Project Structure Notes

All new files align with existing project structure:

| New File | Pattern Source |
|----------|---------------|
| `core/apps/session_manager/lib/nebu/session/bootstrap_checker.ex` | Same as `user_store.ex`, `user_provisioner.ex` |
| `core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex` | Same as `user_store/postgres.ex` |
| `core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs` | Same as `user_store_test.exs` |
| `gateway/internal/admin/bootstrap.go` | Same as `auth.go` (in admin package) |
| `gateway/internal/admin/bootstrap_test.go` | Same as `auth_test.go` |

No new dependencies. No new mix.exs changes. `session_manager` already depends on `nebu_db` (for `Nebu.Repo`) and `signature` (not needed here).

### Previous Story Intelligence

**From Story 2.14 (ValidateToken gRPC Handler):**
- Dev notes explicitly state: "Note for Story 2.15: Bootstrap mode will need to override system_role for the first user to instance_admin. When that story is implemented, provision_new_user may need to re-read the DB role instead of returning the OIDC claim directly." → This is addressed by returning `resolved_role` from `BootstrapChecker.upsert_with_bootstrap/2` instead of the OIDC-derived `system_role`.
- `TokenValidator.Postgres.provision_new_user/4` currently returns the OIDC `system_role` directly (line 62). Must change to return `resolved_role`.
- `UserStore.upsert_user/2` stores whatever `system_role` is passed in. Bootstrap logic must resolve the role BEFORE the upsert.

**From Story 2.12 (User Record DB Write):**
- `UserStore.Postgres.upsert_user/2` uses `ON CONFLICT (user_id) DO UPDATE SET last_seen_at`. The bootstrap checker reuses the exact same SQL pattern.

**From Story 1.5 (server_config + RLS):**
- `server_config` has RLS with INSERT-only policy. `FORCE ROW LEVEL SECURITY` is enabled, so even the table owner cannot UPDATE/DELETE. The `ON CONFLICT (key) DO NOTHING` in `@flag_sql` is correct — it doesn't attempt an UPDATE on conflict.

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 2.15 (lines 1252-1275)]
- [Source: _bmad-output/planning-artifacts/architecture.md — server_config table + RLS (lines 302-310)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Bootstrap Mode (line 588)]
- [Source: _bmad-output/planning-artifacts/architecture.md — File structure: gateway/internal/auth/bootstrap.go (line 914)]
- [Source: _bmad-output/implementation-artifacts/2-14-validatetoken-grpc-handler.md — Bootstrap note (line 292)]
- [Source: core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex — provision_new_user (lines 55-69)]
- [Source: core/apps/session_manager/lib/nebu/session/user_store/postgres.ex — upsert SQL (lines 6-11)]
- [Source: gateway/internal/db/serverconfig.go — InitServerConfig pattern (lines 19-53)]
- [Source: gateway/migrations/000003_server_config.up.sql — RLS policy (lines 1-26)]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6 (1M context)

### Debug Log References
- RED-GREEN-REFACTOR cycle followed for all tasks
- Elixir tests written first (RED: 5 failures), then implementation (GREEN: 0 failures)
- Go tests written alongside handler, all passed on first compile
- No regressions in existing test suites (54 Elixir tests, 9 Go packages)

### Completion Notes List
- Task 1: Created BootstrapChecker behaviour module with configurable impl_module pattern (matching UserStore, UserProvisioner, TokenValidator). Postgres implementation uses pg_advisory_xact_lock(2015) for atomic bootstrap detection, user upsert, and server_config flag insertion within a single Ecto transaction.
- Task 2: Modified TokenValidator.Postgres.provision_new_user/4 to call BootstrapChecker.upsert_with_bootstrap/2 instead of UserStore.upsert_user/2. Now returns resolved_role from DB (instance_admin if bootstrap, else OIDC claim) instead of always returning the OIDC system_role.
- Task 3: Created BootstrapHandler with BootstrapStatusChecker interface for testability. PostgresBootstrapChecker implements the 4-state bootstrap logic (completed → false, active → true, neither+no users → true, neither+users → false). Registered GET /admin/bootstrap route in main.go.
- Task 4: 5 unit tests for BootstrapChecker: bootstrap active/first user gets instance_admin, role preserved when not active (user + admin), bootstrap triggers only once, delegation works. All pass via make test-unit-elixir.
- Task 5: 3 unit tests for Go bootstrap handler: active returns true, not active returns false, DB error returns 500. All pass via make test-unit-go.

### Change Log
- 2026-03-30: Code review passed — fixed pin match race condition in BootstrapChecker.Postgres (^resolved_role → stored_role), moved Go test doubles from production to test file
- 2026-03-30: Story 2-15 implemented — BootstrapChecker module, TokenValidator integration, GET /admin/bootstrap endpoint, full test coverage

### File List
- core/apps/session_manager/lib/nebu/session/bootstrap_checker.ex (new)
- core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex (new)
- core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex (modified)
- core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs (new)
- gateway/internal/admin/bootstrap.go (new)
- gateway/internal/admin/bootstrap_test.go (new)
- gateway/cmd/gateway/main.go (modified)

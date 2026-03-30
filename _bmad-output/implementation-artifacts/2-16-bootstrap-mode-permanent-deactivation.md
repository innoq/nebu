# Story 2.16: Bootstrap Mode — Permanent Deactivation

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want bootstrap mode to deactivate permanently after the first admin is established,
so that no subsequent login can inadvertently gain admin rights through the bootstrap mechanism.

## Acceptance Criteria

1. **Given** the first `instance_admin` user has been created,
   **When** provisioning completes,
   **Then** `INSERT INTO server_config (key, value, set_at) VALUES ('bootstrap_completed', 'true', <now_ms>)` is executed

2. **Given** `bootstrap_completed` is set in `server_config`,
   **When** any subsequent `ValidateToken` call is processed,
   **Then** bootstrap mode is NOT triggered — roles are assigned solely from `nebu_role` OIDC claim

3. **Given** RLS on `server_config` (Story 1.5),
   **When** `bootstrap_completed` is inserted,
   **Then** it cannot be updated or deleted — deactivation is permanent and irreversible (NFR-S6)

4. **Given** `GET /admin/bootstrap` after deactivation,
   **When** called,
   **Then** it returns `404 Not Found` — the bootstrap route no longer exists

## Tasks / Subtasks

- [x] Task 1: Add `bootstrap_completed` INSERT to Elixir BootstrapChecker.Postgres (AC: #1, #2)
  - [x] Add `@complete_sql` constant for `INSERT INTO server_config (key, value, set_at) VALUES ('bootstrap_completed', 'true', $1) ON CONFLICT (key) DO NOTHING`
  - [x] Execute `@complete_sql` inside the existing transaction when `is_bootstrap == true`, immediately after `@flag_sql`

- [x] Task 2: Add Elixir unit tests for permanent deactivation (AC: #1, #2, #3)
  - [x] Extend `FakeBootstrapChecker` in `bootstrap_checker_test.exs` to track `bootstrap_completed` flag in ETS
  - [x] Test: bootstrap triggers → `bootstrap_completed` recorded
  - [x] Test: after `bootstrap_completed`, subsequent calls never trigger bootstrap (role passthrough)
  - [x] Confirm `make test-unit-elixir` passes

- [x] Task 3: Update Go `BootstrapHandler` to return 404 after deactivation (AC: #4)
  - [x] Modify `Handler()` in `gateway/internal/admin/bootstrap.go`: when `IsBootstrapActive()` returns `false`, return `404 Not Found` instead of `200 OK`
  - [x] Keep 200 OK response only when bootstrap IS active

- [x] Task 4: Update Go unit tests for 404 behavior (AC: #4)
  - [x] Change `TestBootstrapHandler_NotActive` to expect 404 instead of 200
  - [x] Add test: completed bootstrap returns 404 with no JSON body (or empty body)
  - [x] Confirm `make test-unit-go` passes

## Dev Notes

### Scope

This story does **two things**:
1. Inserts `bootstrap_completed = 'true'` into `server_config` immediately after the first admin is bootstrapped (Elixir, within existing transaction)
2. Changes `GET /admin/bootstrap` to return 404 when bootstrap is no longer active (Go handler)

**NOT in this story:**
- Bootstrap UI welcome page → Story 3.7 (Epic 3)
- Bootstrap detection middleware for admin UI → Story 3.6 (Epic 3)
- Any changes to `TokenValidator` or `UserProvisioner` — the existing flow in `token_validator/postgres.ex` delegates to `BootstrapChecker` which handles everything

### Task 1: Add `bootstrap_completed` INSERT to BootstrapChecker.Postgres

**File: `core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex`**

Add a new SQL constant alongside the existing `@flag_sql`:

```elixir
@complete_sql """
INSERT INTO server_config (key, value, set_at)
VALUES ('bootstrap_completed', 'true', $1)
ON CONFLICT (key) DO NOTHING
"""
```

Modify the `upsert_with_bootstrap/2` function: inside the existing `Nebu.Repo.transaction/1` block, after the `@flag_sql` execution (line 48), add a second INSERT for `bootstrap_completed`:

```elixir
if is_bootstrap do
  Ecto.Adapters.SQL.query!(Nebu.Repo, @flag_sql, [now_ms])
  Ecto.Adapters.SQL.query!(Nebu.Repo, @complete_sql, [now_ms])
end
```

**Why within the same transaction:** Both `bootstrap_active` and `bootstrap_completed` are inserted atomically. If the transaction fails, neither flag exists and bootstrap can retry. If the transaction succeeds, both flags exist and bootstrap is permanently sealed.

**Why `ON CONFLICT (key) DO NOTHING`:** The `server_config` table has `FORCE ROW LEVEL SECURITY` with INSERT-only policy (no UPDATE/DELETE). `ON CONFLICT ... DO NOTHING` is safe because a conflict would mean `bootstrap_completed` already exists (shouldn't happen due to advisory lock, but defensive). An `ON CONFLICT ... DO UPDATE` would be rejected by RLS.

**RLS guarantees permanence (AC #3):** Once `bootstrap_completed` is inserted, the `FORCE ROW LEVEL SECURITY` on `server_config` with no UPDATE or DELETE policy means no code path — not even the table owner — can modify or remove it. This is the NFR-S6 irreversibility guarantee. No application code change can undo this; only a superuser bypassing RLS could (which is outside the threat model).

**The `@check_sql` already handles this (AC #2):** The existing check in `postgres.ex` line 9 already queries `NOT EXISTS(SELECT 1 FROM server_config WHERE key = 'bootstrap_completed')`. Once `bootstrap_completed` is inserted, `is_bootstrap` returns `false` for all future calls, and `resolved_role = system_role` (the OIDC claim).

### Task 2: Elixir Unit Tests

**File: `core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs`**

Extend the existing `FakeBootstrapChecker` to track `bootstrap_completed` in ETS:

```elixir
# Inside FakeBootstrapChecker.upsert_with_bootstrap/2:
# After setting bootstrap_active to false, also insert bootstrap_completed
:ets.insert(:bootstrap_test, {:bootstrap_completed, true})
```

Add tests in the existing `describe "upsert_with_bootstrap/2"` block:

1. **`bootstrap_completed recorded after bootstrap triggers`**: Assert that after a bootstrap call with `:bootstrap_active = true`, the ETS table contains `{:bootstrap_completed, true}`

2. **`after bootstrap_completed, subsequent calls use OIDC role`**: First call triggers bootstrap (gets `instance_admin`). Second call should return the OIDC role, not `instance_admin`. This is already covered by the existing `"bootstrap triggers only once"` test — confirm it still passes after the FakeBootstrapChecker change.

**No new test file needed** — extend the existing test module.

### Task 3: Go Handler 404 Response

**File: `gateway/internal/admin/bootstrap.go`**

Change the `Handler()` method: when `IsBootstrapActive()` returns `false`, return 404 instead of 200.

Current code (line 37-41):
```go
w.Header().Set("Content-Type", "application/json")
_ = json.NewEncoder(w).Encode(bootstrapResponse{BootstrapActive: active})
```

New logic:
```go
if !active {
    http.NotFound(w, r)
    return
}
w.Header().Set("Content-Type", "application/json")
_ = json.NewEncoder(w).Encode(bootstrapResponse{BootstrapActive: true})
```

**Why `http.NotFound`:** AC #4 specifies "the bootstrap route no longer exists" — standard HTTP 404 is the correct semantic. Using `http.NotFound(w, r)` returns `404 page not found\n` with `text/plain` content type, which is idiomatic Go.

**No change to `PostgresBootstrapChecker.IsBootstrapActive()`** — the existing logic already returns `false` when `bootstrap_completed` exists (line 83-85 in `bootstrap.go`).

### Task 4: Go Unit Tests

**File: `gateway/internal/admin/bootstrap_test.go`**

1. **Update `TestBootstrapHandler_NotActive`**: Change expected status from `http.StatusOK` (200) to `http.StatusNotFound` (404). Remove the JSON response body check — 404 returns `text/plain`.

2. **`TestBootstrapHandler_Active` stays the same**: 200 OK with `{"bootstrap_active": true}` JSON response.

3. **`TestBootstrapHandler_Error` stays the same**: 500 Internal Server Error on DB failure.

### Project Structure Notes

No new files. All changes are modifications to existing files from Story 2-15:

| File | Change |
|------|--------|
| `core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex` | Add `@complete_sql`, execute after `@flag_sql` |
| `core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs` | Extend fake + add completion tests |
| `gateway/internal/admin/bootstrap.go` | Return 404 when not active |
| `gateway/internal/admin/bootstrap_test.go` | Update not-active test to expect 404 |

No new dependencies. No mix.exs changes. No go.mod changes. No new migrations (RLS already enforced by Story 1.5's `000003_server_config.up.sql`).

### Previous Story Intelligence

**From Story 2.15 (Bootstrap Mode — Auto Instance Admin Assignment):**
- `BootstrapChecker.Postgres` already checks for `bootstrap_completed` in `@check_sql` (forward-compatible, noted in moduledoc line 6)
- `@flag_sql` uses `ON CONFLICT (key) DO NOTHING` — use the same pattern for `@complete_sql`
- Advisory lock `pg_advisory_xact_lock(2015)` serializes concurrent requests — `@complete_sql` runs inside this lock, so no race condition
- Go handler `IsBootstrapActive()` already returns `false` when `bootstrap_completed` exists
- Code review feedback from 2-15: fixed pin match `^resolved_role → stored_role` — no impact on this story
- Timestamp pattern: use `Nebu.DB.Helpers.now_ms()` (same `now_ms` variable already in scope)
- Test pattern: ETS-backed fake with `Application.put_env` swap, `async: false`

**From Story 1.5 (server_config + RLS):**
- `FORCE ROW LEVEL SECURITY` means even the table owner is subject to policies
- Only INSERT and SELECT policies exist — no UPDATE, no DELETE
- `ON CONFLICT ... DO UPDATE` would fail under RLS — must use `DO NOTHING`

### Git Intelligence

Recent commits follow pattern: `Story 2-N` as commit message. Files show consistent patterns:
- Elixir: behaviour module + postgres impl + tests
- Go: handler with interface + tests
- No breaking changes across stories — each story builds on previous

### References

- [Source: _bmad-output/planning-artifacts/epics.md — Story 2.16 acceptance criteria]
- [Source: _bmad-output/planning-artifacts/architecture.md — server_config table + RLS (G8)]
- [Source: _bmad-output/planning-artifacts/architecture.md — Bootstrap Mode permanent deactivation]
- [Source: _bmad-output/implementation-artifacts/2-15-bootstrap-mode-auto-instance-admin-assignment.md — Previous story learnings]
- [Source: gateway/migrations/000003_server_config.up.sql — RLS policy (FORCE ROW LEVEL SECURITY, INSERT-only)]
- [Source: core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex — @check_sql already checks bootstrap_completed (line 10)]
- [Source: gateway/internal/admin/bootstrap.go — IsBootstrapActive already returns false when bootstrap_completed exists (line 83)]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- All Elixir tests pass (55 tests, 0 failures) — `make test-unit-elixir`
- All Go tests pass (all packages ok) — `make test-unit-go`

### Completion Notes List

- Task 1: Added `@complete_sql` constant and execute it atomically within the existing transaction after `@flag_sql`. Both `bootstrap_active` and `bootstrap_completed` are inserted together under advisory lock — if transaction fails, neither exists; if it succeeds, bootstrap is permanently sealed.
- Task 2: Extended `FakeBootstrapChecker` to insert `{:bootstrap_completed, true}` into ETS when bootstrap triggers. Added 2 new tests: (1) verifies `bootstrap_completed` is recorded after bootstrap, (2) verifies subsequent calls after completion use OIDC role passthrough. Total: 17 session_manager tests pass.
- Task 3: Changed Go `BootstrapHandler.Handler()` to return `http.NotFound(w, r)` (404) when `IsBootstrapActive()` returns false. 200 OK with JSON only returned when bootstrap IS active.
- Task 4: Updated `TestBootstrapHandler_NotActive` to expect `http.StatusNotFound` (404) instead of 200. Removed JSON body assertion — 404 returns plain text.

### Change Log

- 2026-03-30: Story 2-16 implemented — bootstrap permanent deactivation via `bootstrap_completed` server_config entry + Go 404 response

### File List

- core/apps/session_manager/lib/nebu/session/bootstrap_checker/postgres.ex (modified)
- core/apps/session_manager/test/nebu/session/bootstrap_checker_test.exs (modified)
- gateway/internal/admin/bootstrap.go (modified)
- gateway/internal/admin/bootstrap_test.go (modified)

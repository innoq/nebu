---
id: 7-35
type: fix
security_review: not-needed
created: 2026-04-30
sec_gate_ref: _bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md
---

# Story 7.35: Wire SET LOCAL app.user_id per-request + enable RLS on notifications/push_rules/pushers + fix broken room_account_data RLS

Status: ready-for-dev

## Story

As the Nebu gateway DB layer,
I want every user-scoped database call to run inside a transaction that first executes `SET LOCAL app.user_id = $1`,
so that PostgreSQL Row-Level Security policies are active and correctly enforced for `room_account_data`, `notifications`, `push_rules`, and `pushers` tables.

## Context / Background

**SEC Gate 2 finding (MEDIUM × 2, Kassandra, 2026-04-30):**

**Finding A — RLS missing on notifications/push_rules/pushers:**
`notifications`, `push_rules`, and `pushers` tables have no RLS policy. All queries use `WHERE user_id = $1` at the application layer, which works today, but a single future query that forgets the filter would silently expose all users' data. `room_account_data` (migration 000029) sets the correct pattern — the new tables must follow it.

**Finding B — room_account_data RLS is active but GUC is never set → table is silently broken:**
Migration 000029 enables RLS with `FORCE ROW LEVEL SECURITY` and a policy `USING (user_id = current_setting('app.user_id', true))`. The gateway runs as `nebu_app` (no `BYPASSRLS`). Because `current_setting('app.user_id', true)` returns `NULL` (GUC never set), the comparison `user_id = NULL` evaluates to NULL → treated as FALSE → every `SELECT` returns 0 rows, every `INSERT` violates `WITH CHECK`. Account-data is silently broken in production.

**Root cause of both:** The gateway uses `*sql.DB`, which does not support per-connection session variables. The fix is a `withUserDB` helper that wraps every user-scoped operation in a transaction, issues `SET LOCAL app.user_id = $1` (scoped to that transaction), then runs the actual query.

**Source:** [epic-7b-security-review-2026-04-30.md — Finding MEDIUM #51 and #60]

## Acceptance Criteria

1. A new helper `withUserDB(ctx context.Context, db *sql.DB, userID string, fn func(*sql.Tx) error) error` exists in `gateway/internal/db/user_tx.go`:
   - Begins a transaction with `db.BeginTx(ctx, nil)`.
   - Executes `SET LOCAL app.user_id = $1` with `userID` as the argument.
   - Calls `fn(tx)`.
   - Commits on success; rolls back on any error from fn or from commit.
   - Returns the error from `fn` or commit unchanged.

2. All methods in `account_data_store.go` (`GetAccountData`, `PutAccountData`) use `withUserDB` to wrap their queries. The `tx` object replaces direct `p.db` calls inside the fn closure.

3. All methods in `notifications_store.go` (`GetNotifications`) use `withUserDB`.

4. All methods in `push_rules_store.go` (`SeedDefaultRules`, `GetAllRules`, `GetRule`, `PutRule`, `DeleteRule`, `SetRuleEnabled`, `SetRuleActions`) use `withUserDB`. Note: `PutRule` already opens its own transaction for the `SELECT FOR UPDATE` immutability check — this inner transaction must become a savepoint or be refactored to re-use the outer `withUserDB` transaction (see Dev Notes for the correct approach).

5. All methods in `pushers_store.go` (`GetPushers`, `SetPusher`, `DeletePusher`) use `withUserDB`.

6. A new migration `gateway/migrations/000033_rls_enable_user_tables.up.sql` adds RLS to `notifications`, `push_rules`, and `pushers`:
   - `ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;`
   - `ALTER TABLE notifications FORCE ROW LEVEL SECURITY;`
   - `CREATE POLICY notifications_nebu_app_policy ON notifications FOR ALL TO nebu_app USING (user_id = current_setting('app.user_id', true)) WITH CHECK (user_id = current_setting('app.user_id', true));`
   - Same three statements for `push_rules` and `pushers`.

7. A rollback migration `gateway/migrations/000033_rls_enable_user_tables.down.sql` reverses: `DROP POLICY … ON notifications/push_rules/pushers; ALTER TABLE … DISABLE ROW LEVEL SECURITY;`

8. An integration test (new Godog scenario in `gateway/features/account_data.feature`) verifies:
   - PUT room_account_data → GET room_account_data → response body contains the stored value (proves room_account_data RLS round-trip now works with GUC wiring).

9. An integration test (new Godog scenario in `gateway/features/notifications.feature`) verifies:
   - After `kai has 1 notification in the database`, `GET /notifications` returns a non-empty `notifications` array (proves notifications RLS works after GUC wiring).

10. `make test-unit-go` passes (no compilation errors, no regressions in existing unit tests).

11. The existing integration scenarios for account_data, notifications, push_rules, pushers continue to pass unchanged.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [account_data RLS round-trip — PUT then GET returns stored value] — Godog (`gateway/features/account_data.feature`)
   - Given: kai is authenticated via OIDC and has created a room
   - When: kai PUTs room account data type "m.rls_test" with body `{"verified":true}` for the created room
   - Then: HTTP 200 `{}`
   - When: kai GETs room account data type "m.rls_test" for the created room
   - Then: HTTP 200, response body contains "verified"
   - *(This scenario already exists in the feature file as `PutGet_RoomAccountData`; the ATDD gate passes once GUC is wired. No new scenario file needed — confirm existing scenario is GREEN.)*

2. [notifications RLS — GET /notifications after seed returns rows] — Godog (`gateway/features/notifications.feature`)
   - Given: kai is authenticated via OIDC AND kai has 1 notification in the database
   - When: kai calls GET /_matrix/client/v3/notifications
   - Then: HTTP 200, notifications array has at least 1 item
   - *(Covered by existing `GetNotifications_ReturnsPagedList` scenario — must become GREEN once RLS is wired. Confirm that scenario passes.)*

3. [push_rules GET returns seeded defaults] — Godog (`gateway/features/push_rules.feature`)
   - Given: kai is authenticated via OIDC
   - When: kai calls GET /_matrix/client/v3/pushrules/
   - Then: HTTP 200, response contains expected default rules
   - *(Covered by existing push_rules feature; confirm GREEN.)*

## Tasks / Subtasks

- [ ] Task 1: Create `gateway/internal/db/user_tx.go` with the `withUserDB` helper (AC: #1)
  - [ ] Package `db`, function signature: `func withUserDB(ctx context.Context, db *sql.DB, userID string, fn func(*sql.Tx) error) error`
  - [ ] BeginTx → SET LOCAL → fn(tx) → Commit (or Rollback on error)
  - [ ] `defer tx.Rollback()` pattern (safe to call after Commit — returns `ErrTxDone`)

- [ ] Task 2: Refactor `account_data_store.go` to use `withUserDB` (AC: #2)
  - [ ] `GetAccountData`: wrap `QueryRowContext` call in `withUserDB`; use `tx.QueryRowContext` inside fn
  - [ ] `PutAccountData`: wrap `ExecContext` call in `withUserDB`; use `tx.ExecContext` inside fn
  - [ ] Verify: existing `PutGet_RoomAccountData` Godog scenario passes (was silently broken)

- [ ] Task 3: Refactor `notifications_store.go` to use `withUserDB` (AC: #3)
  - [ ] `GetNotifications`: wrap `QueryContext` in `withUserDB`; use `tx.QueryContext` inside fn

- [ ] Task 4: Refactor `push_rules_store.go` to use `withUserDB` (AC: #4)
  - [ ] `SeedDefaultRules`: wrap loop in `withUserDB`; use `tx.ExecContext` inside fn
  - [ ] `GetAllRules`: wrap `QueryContext` in `withUserDB`; use `tx.QueryContext` inside fn
  - [ ] `GetRule`: wrap `QueryRowContext` in `withUserDB`; use `tx.QueryRowContext` inside fn
  - [ ] `PutRule`: this method already calls `p.db.BeginTx` internally — refactor it to use `withUserDB` as the outer transaction and perform the `SELECT FOR UPDATE` + `INSERT … ON CONFLICT` within that single transaction (eliminate the nested BeginTx)
  - [ ] `DeleteRule`: wrap both the `DELETE` and the existence-check `SELECT` in `withUserDB`
  - [ ] `SetRuleEnabled`: wrap `UPDATE` in `withUserDB`
  - [ ] `SetRuleActions`: wrap `UPDATE` in `withUserDB`

- [ ] Task 5: Refactor `pushers_store.go` to use `withUserDB` (AC: #5)
  - [ ] `GetPushers`: wrap `QueryContext` in `withUserDB`; use `tx.QueryContext` inside fn
  - [ ] `SetPusher`: wrap `ExecContext` in `withUserDB`; use `tx.ExecContext` inside fn
  - [ ] `DeletePusher`: wrap `ExecContext` in `withUserDB`; use `tx.ExecContext` inside fn

- [ ] Task 6: Write migration 000033 (AC: #6, #7)
  - [ ] `gateway/migrations/000033_rls_enable_user_tables.up.sql` — ENABLE + FORCE RLS + CREATE POLICY for notifications, push_rules, pushers
  - [ ] `gateway/migrations/000033_rls_enable_user_tables.down.sql` — DROP POLICY + DISABLE RLS
  - [ ] Verify `migrations_test.go` still passes (auto-discovers all migration files)

- [ ] Task 7: Confirm integration test scenarios are GREEN (AC: #8, #9, #10, #11)
  - [ ] Confirm `PutGet_RoomAccountData` Godog scenario is GREEN (was broken by RLS without GUC)
  - [ ] Confirm `GetNotifications_ReturnsPagedList` Godog scenario is GREEN (was broken by RLS without GUC)
  - [ ] Confirm all push_rules and pushers scenarios remain GREEN
  - [ ] Run `make test-unit-go`

## Dev Notes

### withUserDB helper — exact pattern

```go
// user_tx.go
package db

import (
    "context"
    "database/sql"
)

// withUserDB runs fn inside a PostgreSQL transaction that has SET LOCAL app.user_id = $1
// set for the duration of the transaction. This is required for Row-Level Security policies
// that use current_setting('app.user_id', true) — see migrations 000029 and 000033.
//
// SET LOCAL is used (not SET) so the GUC is automatically reset when the transaction ends,
// preventing GUC leakage to subsequent connections returned to the pool.
func withUserDB(ctx context.Context, db *sql.DB, userID string, fn func(*sql.Tx) error) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback() //nolint:errcheck — safe after Commit (returns ErrTxDone)

    if _, err := tx.ExecContext(ctx, `SET LOCAL app.user_id = $1`, userID); err != nil {
        return err
    }
    if err := fn(tx); err != nil {
        return err
    }
    return tx.Commit()
}
```

### PutRule — eliminating the nested transaction

`PutRule` currently calls `p.db.BeginTx` internally to do `SELECT FOR UPDATE` + `INSERT … ON CONFLICT`. When wrapping with `withUserDB`, you must NOT call `db.BeginTx` again inside the fn closure — PostgreSQL does not support true nested transactions via `*sql.DB` (it would start a new connection-level transaction, which would ignore the outer `SET LOCAL`).

**Correct approach:** Pass the `tx` from `withUserDB` into the inner logic directly. Rewrite `PutRule` to accept a `tx *sql.Tx` internally, or use a single closure:

```go
func (p *PostgresPushRulesDB) PutRule(ctx context.Context, userID string, row matrix.PushRuleRow) error {
    // ... build conditions/actions ...
    return withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
        var isDefault bool
        err := tx.QueryRowContext(ctx,
            `SELECT default_rule FROM push_rules
              WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4
              FOR UPDATE`,
            userID, row.Scope, row.Kind, row.RuleID,
        ).Scan(&isDefault)
        if err != nil && !errors.Is(err, sql.ErrNoRows) {
            return err
        }
        if isDefault {
            return matrix.ErrDefaultRuleImmutable
        }
        _, err = tx.ExecContext(ctx,
            `INSERT INTO push_rules ... ON CONFLICT ... DO UPDATE ...`,
            // same args as before
        )
        return err
    })
}
```

The `SELECT FOR UPDATE` and `INSERT` now run in the same transaction as the `SET LOCAL`, which is correct.

### DeleteRule — two queries in one transaction

`DeleteRule` currently does a `DELETE` and then (conditionally) a `SELECT EXISTS`. Both must run in the same `withUserDB` transaction:

```go
func (p *PostgresPushRulesDB) DeleteRule(ctx context.Context, userID, scope, kind, ruleID string) error {
    return withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
        res, err := tx.ExecContext(ctx,
            `DELETE FROM push_rules WHERE user_id = $1 AND scope = $2 AND kind = $3 AND rule_id = $4 AND NOT default_rule`,
            userID, scope, kind, ruleID,
        )
        if err != nil {
            return err
        }
        n, _ := res.RowsAffected()
        if n > 0 {
            return nil
        }
        // check if default rule exists
        var exists bool
        err = tx.QueryRowContext(ctx,
            `SELECT EXISTS(SELECT 1 FROM push_rules WHERE user_id=$1 AND scope=$2 AND kind=$3 AND rule_id=$4)`,
            userID, scope, kind, ruleID,
        ).Scan(&exists)
        if err != nil {
            return err
        }
        if exists {
            return matrix.ErrDefaultRuleImmutable
        }
        return matrix.ErrPushRuleNotFound
    })
}
```

### Migration 000033 — exact pattern to follow

Migration 000029 is the reference. Follow the same structure for notifications, push_rules, pushers:

```sql
-- For each of the three tables:
ALTER TABLE <table> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <table> FORCE ROW LEVEL SECURITY;
CREATE POLICY <table>_nebu_app_policy ON <table>
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));
```

**Important:** `nebu_app` already has `GRANT SELECT, INSERT, UPDATE, DELETE` on these tables (migration 000032). The grants do NOT need to be repeated in 000033.

**Important:** The `notifications` table already has `GRANT SELECT, INSERT, UPDATE` (no DELETE, as notifications are append-only per migration 000031 comment). The RLS policy `FOR ALL` covers SELECT/INSERT/UPDATE only — there is no DELETE on that table, so WITH CHECK on INSERT is the relevant constraint.

**Rollback:** Must DROP POLICY before DISABLE ROW LEVEL SECURITY or PG will error.

```sql
-- down.sql
DROP POLICY IF EXISTS notifications_nebu_app_policy ON notifications;
ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS push_rules_nebu_app_policy ON push_rules;
ALTER TABLE push_rules DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS pushers_nebu_app_policy ON pushers;
ALTER TABLE pushers DISABLE ROW LEVEL SECURITY;
```

### account_data_store.go — both global AND per-room data use same table

`GetAccountData` and `PutAccountData` handle BOTH global (roomID = "") and per-room (roomID != "") account data. Migration 000029 already has FORCE RLS. The wrapping with `withUserDB` will fix both variants at once — no separate code path needed.

### Why SET LOCAL (not SET)

`SET` (without LOCAL) persists for the lifetime of the connection, even after transaction end. Because `*sql.DB` pools connections, a `SET app.user_id = '@user1'` would leak to the next request that reuses the same connection. `SET LOCAL` is automatically reset when the transaction commits or rolls back, making it connection-pool safe.

### Existing integration test patterns for DB seeding

The existing notification step `kaiHasNNotificationsInDB` uses `migrationDBURL` (nebu_migrate role, BYPASSRLS) for direct inserts. This is correct — the seeding bypasses RLS so it works both before and after migration 000033. No change needed in the step definition.

### Files to modify (UPDATE, not NEW except user_tx.go and the two migration files)

| File | Action |
|------|--------|
| `gateway/internal/db/user_tx.go` | **NEW** |
| `gateway/internal/db/account_data_store.go` | UPDATE — wrap GetAccountData + PutAccountData |
| `gateway/internal/db/notifications_store.go` | UPDATE — wrap GetNotifications |
| `gateway/internal/db/push_rules_store.go` | UPDATE — wrap all 7 methods; refactor PutRule inner tx |
| `gateway/internal/db/pushers_store.go` | UPDATE — wrap GetPushers + SetPusher + DeletePusher |
| `gateway/migrations/000033_rls_enable_user_tables.up.sql` | **NEW** |
| `gateway/migrations/000033_rls_enable_user_tables.down.sql` | **NEW** |

No changes to handlers (`matrix/` package), no changes to `cmd/gateway/main.go`, no changes to Elixir code, no changes to `go.mod`.

### Existing tests that should turn GREEN (were silently broken)

The following Godog scenarios in existing feature files were broken because room_account_data has RLS with FORCE but the GUC was never set. After this story they should pass:

- `gateway/features/account_data.feature` — ALL scenarios (they were failing silently in prod but may have passed in integration because the nebu_migrate seed bypasses RLS, or because the feature was never actually exercised end-to-end with nebu_app role)
- `gateway/features/notifications.feature` — scenarios that require seeded data (`GetNotifications_ReturnsPagedList`, `GetNotifications_FromCursor_SecondPage`, `GetNotifications_OnlyHighlight_FiltersCorrectly`)

Note: If these scenarios were passing before, it could be because the integration test stack runs migrations as nebu_migrate (BYPASSRLS) and the test runner connects as nebu_migrate for the `migrationDBURL` seed, but the gateway itself connects as nebu_app. Verify the actual test behavior.

### package db import — no new imports needed in store files

The `withUserDB` helper uses only `context`, `database/sql` — no new imports. The store files already import these packages. The only import change needed in store files is: no longer need to call `p.db.BeginTx` / `p.db.ExecContext` / `p.db.QueryContext` directly — they all go through the `tx` parameter. This doesn't change imports.

### Compile-time interface checks

`push_rules_store.go` and `pushers_store.go` have no explicit `var _ interface{...} = (...)` compile-time checks currently. After refactoring, verify the existing `var _ interface{...} = (*PostgresPushersDB)(nil)` block at the bottom of `pushers_store.go` still compiles.

## Previous Story Context

**Story 7-33 (MEDIUM SEC Gate 2 fix, done 2026-04-30):**
- `make test-unit-elixir` and `make test-unit-go` were both clean at merge.
- The `FakeRoomDB` ETS + `build_stream/2` pattern for ExUnit was used — not relevant to this story.

**Story 7-30 (done 2026-04-30):**
- `push_rules_store.go` and `pushers_store.go` were created.
- Migration 000032 explicitly noted "Queries use WHERE user_id=$1 directly (no GUC / RLS)" — this story is the follow-up that closes that gap.
- `PutRule` uses `p.db.BeginTx` for `SELECT FOR UPDATE` — must be eliminated by refactoring into `withUserDB`.

**Story 7-24 (done 2026-04-30):**
- `account_data_store.go` was created.
- Migration 000029 enabled RLS with FORCE but GUC was never wired — this story is the fix.

**Commit pattern:**
- `fix(7-35): <short description>` — type fix, no security review needed.

## Architecture Compliance

- Go conventions: errors explicit, context as first parameter, no panic in library code.
- The `withUserDB` helper is defined in `gateway/internal/db/` (same package as all store files) — no inter-package dependency added.
- `SET LOCAL` is the correct PostgreSQL primitive for per-transaction GUC values in a connection pool.
- Migration numbering is sequential: last migration is 000032, next is 000033.
- Migration files must be placed in `gateway/migrations/` (the `migrations.FS` embed in `migrations.go` picks them up automatically).
- `nebu_app` role is the runtime role for the gateway — RLS is enforced against it (no BYPASSRLS).
- `nebu_migrate` role has BYPASSRLS (table owner) — integration tests use `migrationDBURL` with this role for seeding.

---
security_review: required
---

# Story 5.11: Transactional Bootstrap Completion

Status: ready-for-dev

## Story

As an instance admin,
I want `ClaimSelectionHandler` to perform `SaveBootstrapConfig`, `SaveAdminGroupClaim`, and `CompleteBootstrap` in a single DB transaction,
so that a failing `CompleteBootstrap` (e.g. bootstrap already completed) rolls back the config and claim writes.

---

## Background / Motivation

Security audit (2026-04-20) found that `ClaimSelectionHandler` (`admin/auth.go:570–653`) writes OIDC config and `admin_group_claim` via `ON CONFLICT DO UPDATE` **before** calling `CompleteBootstrap`. If the completion check fails (row-count == 0 because bootstrap already done), the config rows are already committed.

Story 5.10 closes the entry points, but defense-in-depth requires the writes to be atomic. If any future code path bypasses the guard, the TX boundary prevents the overwrite.

---

## Acceptance Criteria

1. `SaveBootstrapConfig`, `SaveAdminGroupClaim`, and the `CompleteBootstrap` check run inside a single `sql.Tx` in `ClaimSelectionHandler`.

2. If `CompleteBootstrap` returns `ErrAlreadyCompleted` (or `rows==0`), the transaction is rolled back — no changes to `server_config` or `bootstrap_draft` persist.

3. If the transaction is rolled back, the handler responds with **403 Forbidden** and renders the standard error page.

4. `ClearDraft` runs inside the same TX on the success path and its failure aborts the TX (no warn-and-continue).

5. Unit test: inject a mock that makes `CompleteBootstrap` fail. Verify `server_config` is unchanged after the call.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestClaimSelection_TXRollbackOnCompleteBootstrapFailure` — Go httptest + real Postgres (via test container)
   - Given: `server_config.bootstrap_completed=true`, existing OIDC config `oidc_issuer=https://old.example.com`
   - When: `POST /admin/bootstrap/select-claim` with valid state cookie + attacker values `oidc_issuer=https://attacker.com`
   - Then: 403; `SELECT value FROM server_config WHERE key='oidc_issuer'` still returns `https://old.example.com`

2. `TestClaimSelection_TXCommitsOnSuccess` — Go httptest + Postgres
   - Given: `bootstrap_active=true`, draft rows present
   - When: POST succeeds
   - Then: all three writes (config, claim, completion) visible; draft cleared

---

## Implementation Notes

- `db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})` at the top of `ClaimSelectionHandler`
- Pass `tx` into `SaveBootstrapConfig`, `SaveAdminGroupClaim`, `CompleteBootstrap`, `ClearDraft` (extend their signatures to accept `sq` interface that matches both `*sql.DB` and `*sql.Tx`)
- Defer rollback; explicit commit only on full success
- `CompleteBootstrap` must return a typed `ErrAlreadyCompleted` sentinel so the handler can map it to 403

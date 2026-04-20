# Security Review — Story 5.11: Transactional Bootstrap Completion — 2026-04-20

**Agent:** Kassandra
**Diff base:** `git diff --staged` (6 files, 564 insertions, 56 deletions)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.11 wraps `SaveBootstrapConfig`, `SaveAdminGroupClaim`, `ClearDraft`, and `CompleteBootstrap` into a single `sql.Tx` inside `ClaimSelectionHandler`. The transaction boundary is correctly implemented: `defer tx.Rollback()` with explicit `Commit()` only on full success, `ErrAlreadyCompleted` sentinel maps to 403, and `ClearDraft` failure aborts the transaction instead of warn-and-continue. The pre-existing TOCTOU on draft reads (outside the TX) was already acknowledged and deferred by the code review — it is not a new introduction and is mitigated by Story 5.10's entry-point guards. No CRITICAL or HIGH findings.

## Findings

### [MEDIUM] TOCTOU: Draft reads outside transaction boundary

- **CWE / OWASP:** CWE-367 (Time-of-Check Time-of-Use) / A04:2021 (Insecure Design)
- **File:** `gateway/internal/admin/auth.go:651-659`
- **Description:** `ClaimSelectionHandler` reads draft values (`instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`) via `a.draftStore.LoadDraft()` outside the transaction, then writes them inside the TX via `saveBootstrapConfigTx()`. A concurrent request could theoretically read stale or manipulated draft data between the read and the transactional write.
- **Impact:** Bounded. Bootstrap runs exactly once by a single admin during initial setup. Story 5.10 guards the entry points (`BootstrapGuard` redirects when `bootstrap_completed=true`). A concurrent race would require the attacker to have already passed the OIDC callback flow and to write to `bootstrap_draft` in the narrow window between the reads and the TX commit. The operational scenario (single admin, first setup) makes this impractical.
- **Recommendation:** Move the `LoadDraft` calls inside the transaction boundary and read from the same `sqlQuerier` (`q`). This would require either extending `clearDraftTx` to also return the draft values before deletion, or adding a `loadDraftTx` helper. Acknowledged as deferred by the code review.
- **Reference:** CWE-367, OWASP ASVS V1.2.1 (design-level threat analysis)

### [MEDIUM] Missing CSRF token on POST /admin/bootstrap/select-claim

- **CWE / OWASP:** CWE-352 / A01:2021 (Broken Access Control)
- **File:** `gateway/internal/admin/auth.go:626` (handler) / `gateway/internal/admin/templates/bootstrap-claims.html:32,79` (forms)
- **Description:** The `POST /admin/bootstrap/select-claim` endpoint does not validate a CSRF token. The `SameSite=Lax` cookie attribute on `admin_session` provides browser-level CSRF protection for same-site POST requests, but this is not a defense-in-depth layer — older browsers or misconfigured reverse proxies may not enforce SameSite.
- **Impact:** Bounded. This endpoint is only reachable during the one-time bootstrap flow. Story 5.13 (`csrf-middleware-admin-post`) is explicitly planned to add CSRF middleware to all admin POST endpoints. The attack window is the single bootstrap session of a single admin.
- **Recommendation:** Story 5.13 addresses this. No action required in 5.11 — but Story 5.13 must not exclude this route.
- **Reference:** CWE-352, OWASP ASVS V4.2.2

### [INFO] Correct transactional design: defer Rollback + explicit Commit

- **File:** `gateway/internal/admin/auth.go:179-189`
- **Description:** The `runInTx` closure follows the Go best practice: `defer tx.Rollback()` immediately after `BeginTx`, execute all writes, then `tx.Commit()` only on success. The `errcheck` nolint is correctly placed and documented — the deferred Rollback is best-effort after a successful Commit (which is a no-op on an already-committed TX).
- **Impact:** Positive finding. This pattern ensures no partial writes persist on any error path.
- **Reference:** Go database/sql documentation, OWASP ASVS V1.2.1

### [INFO] ErrAlreadyCompleted sentinel correctly preserves unwrapped error identity

- **File:** `gateway/internal/admin/auth.go:678`
- **Description:** Inside the `runInTx` callback, `completeBootstrapTx` errors are returned without `fmt.Errorf` wrapping (`return err`), preserving `errors.Is(txErr, ErrAlreadyCompleted)` identity at the call site. All other errors are wrapped with context. This is intentional and correct.
- **Impact:** Positive finding. Ensures the 403 mapping works reliably.
- **Reference:** Go errors package documentation

### [INFO] Transaction isolation level: READ COMMITTED

- **File:** `gateway/internal/admin/auth.go:180`
- **Description:** `sql.LevelReadCommitted` is used. This is appropriate for the bootstrap TX — it prevents dirty reads while avoiding the overhead and deadlock risk of SERIALIZABLE. The bootstrap flow is a one-time operation with no concurrent writers in normal operation.
- **Impact:** Appropriate isolation level for the use case.
- **Reference:** PostgreSQL Transaction Isolation documentation

### [INFO] Test file models transactional semantics correctly

- **File:** `gateway/internal/admin/claim_selection_tx_test.go`
- **Description:** The `txAwareConfigStore` test double models the pending/committed state machine correctly: writes go to `pending*` fields, `commit()` promotes them, `rollback()` discards them. The three tests (rollback-on-failure, commit-on-success, ClearDraft-failure-aborts) cover all acceptance criteria. The `buildClaimSelectionRequest` helper does not forge cookies or seed DB state — it tests the handler's TX logic directly, which is appropriate for unit-level verification.
- **Impact:** Positive finding. Test integrity is maintained.
- **Reference:** Nebu TDD Standard (CLAUDE.md)

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — no new compliance-scoped tables or queries |
| `reason` field on compliance access         | ✅ N/A — no compliance data access in this diff |
| Audit-log immutability                      | ✅ N/A — no audit table modifications |
| `instance_admin` notification (if in-scope) | ✅ N/A — no scope escalation paths added |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ N/A — no new OIDC token handlers; existing CallbackHandler unchanged |
| Matrix Power Level checks                   | ✅ N/A — no room-scoped operations |
| No hardcoded secrets                        | ✅ Verified — no string literals containing keys, tokens, or passwords in production code. Test file uses obvious test fixtures (`"test-secret-key"`, `"test-client-id"`) |
| TLS 1.3 enforcement                         | ✅ N/A — no TLS configuration changes |
| AES-256-GCM correctness                     | ✅ N/A — no encryption logic changes; `encryptedSecret` is read from draft (already encrypted by Step 2 of bootstrap wizard) |
| Ed25519 verify-before-accept                | ✅ N/A — no signature operations |
| No secrets in logs / error messages          | ✅ Verified — `slog.Error` calls log only error messages and context keys, not draft values, tokens, or secrets. `http.Error` responses return generic messages (`"internal error"`, `"Bootstrap already completed"`) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 0 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed. The two MEDIUM findings are: (1) a pre-existing TOCTOU already deferred by the code review, and (2) missing CSRF which is explicitly addressed by the upcoming Story 5.13.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*

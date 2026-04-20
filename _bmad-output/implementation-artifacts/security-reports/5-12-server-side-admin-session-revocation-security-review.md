# Security Review — Story 5.12: Server-side Admin Session Revocation — 2026-04-20

**Agent:** Kassandra
**Diff base:** git diff --staged (branch: main)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.12 replaces the stateless HMAC-signed admin session cookie with a server-side session store backed by PostgreSQL. The core implementation is solid: SID generation uses `crypto/rand` (32 bytes), cookies are HMAC-SHA256 signed, the middleware correctly validates revocation and expiry on every request, and SQL queries are fully parameterized. One HIGH finding: the `ClaimSelectionHandler` (bootstrap flow) still creates a legacy stateless cookie even though `SessionGuardWithStore` is now the active middleware, which means the first admin session after bootstrap completion is immediately rejected by the guard. One MEDIUM finding: the `admin_sessions` table lacks Row Level Security, which is acceptable for admin-only infrastructure but deviates from the project's security-first posture.

## Findings

### [HIGH] Bootstrap flow creates incompatible legacy cookie — session rejected by SessionGuardWithStore

- **CWE / OWASP:** CWE-287 (Improper Authentication) / A07:2021 (Identification and Authentication Failures)
- **File:** `gateway/internal/admin/auth.go:750-770` (`ClaimSelectionHandler`)
- **Description:** After bootstrap completion, `ClaimSelectionHandler` creates a legacy `adminSessionCookie` (containing `sub`, `email`, `role`, `exp`) and writes it to the browser. However, `main.go:191` now wires `SessionGuardWithStore` as the active guard. This middleware expects an `adminSessionSIDCookie` (containing only `sid`) and performs a DB lookup. When it encounters the legacy cookie, `json.Unmarshal` into `adminSessionSIDCookie` will produce an empty `SID` field, causing the guard to reject the request with a 302 redirect to `/admin/login`. The admin who just completed bootstrap is immediately locked out of the dashboard.
- **Impact:** The first-time admin who completes the bootstrap wizard cannot access the dashboard without logging out and logging in again through the normal OIDC flow. This is a usability and authentication-flow integrity issue. In the worst case, if the normal login flow is also misconfigured during first setup, the admin is locked out entirely.
- **Recommendation:** In `ClaimSelectionHandler`, when `a.sessionStore != nil`, create a server-side session via `a.sessionStore.Create(ctx, sub, expiresAt)` and write an `adminSessionSIDCookie` — mirroring the logic already implemented in `CallbackHandler` (lines 596-624). Apply the same `min(idToken.Exp, now+8h)` cap. The bootstrap flow does not have access to `idToken.Expiry`, so use `now+8h` as the default and document the divergence.
- **Reference:** OWASP ASVS V3.2 (Session Binding), CWE-287

### [MEDIUM] admin_sessions table has no Row Level Security

- **CWE / OWASP:** CWE-285 (Improper Authorization) / NIST AC-3 (Access Enforcement)
- **File:** `gateway/migrations/000017_admin_sessions.up.sql:1-9`
- **Description:** The `admin_sessions` table is created without `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`. While admin sessions are not user-scoped compliance data and the table is only accessed by the application role through parameterized queries, the absence of RLS means any SQL role with SELECT/UPDATE/DELETE grants on this table could read or modify all sessions. Given Nebu's security-first posture and the pattern established by other tables, this is a defense-in-depth gap.
- **Impact:** Low direct risk — the attack requires a compromised DB role or SQL injection elsewhere (none found in this diff). However, if a future migration grants broader access or another code path introduces a vulnerability, the lack of RLS provides no secondary barrier.
- **Recommendation:** Add `ALTER TABLE admin_sessions ENABLE ROW LEVEL SECURITY; ALTER TABLE admin_sessions FORCE ROW LEVEL SECURITY;` and a policy restricting access to the application role. Alternatively, document the explicit exemption as an ADR note.
- **Reference:** NIST AC-3, Nebu Compliance RSP invariant (partial applicability)

### [LOW] Revoke does not verify rows affected — silent success on unknown SID

- **CWE / OWASP:** CWE-754 (Improper Check for Unusual or Exceptional Conditions)
- **File:** `gateway/internal/db/admin_session_store.go:74-83`
- **Description:** `Revoke()` executes `UPDATE ... SET revoked_at WHERE sid = $2` and discards the `sql.Result`. If the SID does not exist (already cleaned up, or malformed), the operation succeeds silently with 0 rows affected. The `LogoutHandler` then reports successful logout to the user.
- **Impact:** Negligible security impact — the session was already gone. This is a logging/observability gap: a revocation that affects zero rows may indicate a replay or timing issue that the system should log for audit trail completeness.
- **Recommendation:** Check `result.RowsAffected()`. If zero, log at INFO level (not error — it is not a failure). Do not change the HTTP response; the user should still see a successful logout.
- **Reference:** OWASP ASVS V7.1 (Error Handling)

### [INFO] Cleanup goroutine uses context.Background — not bound to request lifecycle

- **File:** `gateway/cmd/gateway/main.go:200`
- **Description:** `sessionStore.CleanupExpired(context.Background())` uses a fresh background context rather than deriving from the shutdown context `ctx`. This is acceptable because the cleanup operation is a short-lived DB DELETE and the goroutine already exits via `case <-ctx.Done()`. However, if the DB connection is closed during shutdown before the ticker fires, the `ExecContext` will return an error that is logged as a warning — correct behavior.
- **Impact:** None. Observation for completeness.

### [INFO] SID entropy is adequate (256-bit, crypto/rand)

- **File:** `gateway/internal/db/admin_session_store.go:26-31`
- **Description:** Session IDs are generated via `crypto/rand.Read(32 bytes)` and encoded as base64url without padding. This provides 256 bits of entropy — well above the OWASP ASVS V3.2.1 minimum of 128 bits for session identifiers. The implementation is correct.
- **Impact:** Positive finding — no action needed.

### [INFO] Cookie attributes correctly configured

- **File:** `gateway/internal/admin/auth.go:614-622`
- **Description:** The SID cookie is set with `HttpOnly: true`, `SameSite: Lax`, `Secure: r.TLS != nil`, `Path: /admin`, and a `MaxAge` derived from the session expiry. The `SameSite=Lax` choice is correct because the OIDC callback redirect is cross-site (from Dex). The comment documents this reasoning.
- **Impact:** Positive finding — cookie hardening is appropriate.

### [INFO] HMAC-SHA256 cookie signing uses constant-time comparison

- **File:** `gateway/internal/admin/middleware.go:48` and `gateway/internal/admin/auth.go:254`
- **Description:** Both `verifyCookie` and `verifySessionCookie` use `hmac.Equal()` for signature comparison, which is constant-time. This prevents timing attacks on the cookie HMAC.
- **Impact:** Positive finding — no action needed.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ⚠️     |
| `reason` field on compliance access         | ✅     |
| Audit-log immutability                      | ✅     |
| `instance_admin` notification (if in-scope) | ✅     |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅     |
| Matrix Power Level checks                   | ✅     |
| No hardcoded secrets                        | ✅     |
| TLS 1.3 enforcement                         | ✅     |
| AES-256-GCM correctness                     | ✅     |
| Ed25519 verify-before-accept                | ✅     |
| No secrets in logs / error messages         | ✅     |

**Compliance RSP — ⚠️:** The new `admin_sessions` table does not enable RLS. This table stores infrastructure session data (SID, user_id, timestamps), not user-scoped compliance data. RLS is not strictly required by the Nebu invariant, but the absence is noted as MEDIUM above for defense-in-depth. Verification: add RLS or document the explicit exemption.

**OIDC token validation — ✅:** The OIDC token is verified via `provider.Verifier(&oidc.Config{ClientID: clientID}).Verify()` which checks `iss`, `aud`, `exp`, and signature. The session expiry is correctly capped to `min(idToken.Expiry, now+8h)`.

**No secrets in logs — ✅:** Log statements in the diff use structured fields (`"err"`, `"err"`) and do not log the SID, cookie value, or any token material. The `slog.Warn` in `LogoutHandler` logs only the error message, not the SID.

**No hardcoded secrets — ✅:** All secret material (HMAC key, SID) is generated at runtime via `crypto/rand` or read from the PSK file. No string literals contain credentials.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0     |
| HIGH      | 1     |
| MEDIUM    | 1     |
| LOW       | 1     |
| INFO      | 3     |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The HIGH finding (bootstrap flow creating incompatible legacy cookie) must be fixed before the next story that depends on the bootstrap-to-dashboard transition, or scheduled as an immediate follow-up story.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*

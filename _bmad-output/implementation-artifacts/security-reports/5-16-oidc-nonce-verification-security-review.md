# Security Review — Story 5.16: OIDC Nonce Verification — 2026-04-20

**Agent:** Kassandra
**Diff base:** `git diff --staged` (4 files, +350 -2)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.16 adds OIDC nonce generation, storage, and verification to both the new `LoginStartHandler` and the legacy `LoginHandler`. The implementation is sound: 32-byte `crypto/rand` nonces, base64url encoding, stored in the HMAC-signed state cookie, passed via `oidc.Nonce()`, and verified in `CallbackHandler` with an explicit empty-nonce guard (AC5) before comparison (AC4). No security-relevant issues found. The diff is narrow, focused, and correctly applied to both login code paths.

## Findings

### [INFO] Nonce values logged on mismatch

- **CWE / OWASP:** CWE-532 / A09:2021 (Security Logging and Monitoring Failures)
- **File:** `gateway/internal/admin/auth.go:536`
- **Description:** The nonce mismatch warning logs both the expected and received nonce values: `slog.Warn("callback: nonce mismatch", "expected", sc.Nonce, "got", idToken.Nonce)`. OIDC nonces are single-use random values with a 10-minute lifetime, not long-lived secrets. Logging them is standard debugging practice and does not create an exploitable path.
- **Impact:** None. The nonce is ephemeral and cannot be replayed (the authorization code is consumed by the OIDC provider, and the state cookie is deleted after callback).
- **Recommendation:** No action required. If a future log aggregation policy restricts all token-adjacent values, consider replacing the nonce values with a boolean `"match"=false` indicator.
- **Reference:** OWASP ASVS V7.1.1

### [INFO] AC5 empty-nonce guard instead of cookie version bump

- **CWE / OWASP:** N/A (design observation)
- **File:** `gateway/internal/admin/auth.go:528`
- **Description:** The story's AC5 specifies "signed-cookie struct version is bumped so older cookies are rejected as invalid." The implementation uses an explicit `sc.Nonce == ""` check instead. Both achieve the same security property: old cookies without a nonce field deserialize with `Nonce: ""` (JSON zero-value) and are rejected before the comparison. The empty-nonce guard is arguably more explicit and self-documenting.
- **Impact:** None. The security property (rejecting pre-nonce cookies) is fully enforced.
- **Recommendation:** No action required. Consider documenting this design choice in the story implementation notes for traceability.

### [INFO] Nonce comparison uses direct string equality

- **CWE / OWASP:** N/A (positive observation)
- **File:** `gateway/internal/admin/auth.go:535`
- **Description:** `idToken.Nonce != sc.Nonce` uses Go's built-in `!=` operator instead of `hmac.Equal()` or `subtle.ConstantTimeCompare()`. This is correct: the nonce is a random, single-use value that is not a shared secret. The attacker does not have a timing oracle — they cannot repeatedly probe the same nonce against the same cookie (the code is single-use, the cookie is consumed). Constant-time comparison is unnecessary here and would be over-engineering.
- **Impact:** None.
- **Recommendation:** No action required.

### [INFO] Both login code paths emit nonce — correct dual coverage

- **CWE / OWASP:** N/A (positive observation)
- **File:** `gateway/internal/admin/auth.go:334-339` (LoginStartHandler), `gateway/internal/admin/auth.go:400-405` (LoginHandler)
- **Description:** Nonce generation and `oidc.Nonce()` parameter injection are applied to both `LoginStartHandler` (canonical path, `/admin/login/start`) and `LoginHandler` (legacy path, `/admin/auth/login`). Both store the nonce in the same `oidcStateCookie` struct, and `CallbackHandler` (shared by both routes) verifies it uniformly. No code path can bypass the nonce check.
- **Impact:** Positive — complete coverage of all OIDC login entry points.
- **Recommendation:** None.

### [INFO] Test coverage for nonce security properties

- **CWE / OWASP:** N/A (positive observation)
- **File:** `gateway/internal/admin/nonce_test.go` (304 lines), `gateway/internal/admin/auth_test.go`, `gateway/internal/admin/callback_test.go`
- **Description:** Three dedicated nonce tests cover: (1) nonce emission in AuthCodeURL and state cookie, (2) 403 rejection on nonce mismatch with no session cookie set, (3) successful flow with matching nonce. Existing callback tests are updated with `testNonce` constant to pass through the AC5 guard. The test helpers `buildStateCookieWithNonce` and `setupOIDCServerWithNonce` correctly inject nonce values at both the cookie and JWT levels.
- **Impact:** Positive — security properties are explicitly tested.
- **Recommendation:** None.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — no DB queries in diff |
| `reason` field on compliance access         | ✅ N/A — no compliance data access |
| Audit-log immutability                      | ✅ N/A — no migration in diff |
| `instance_admin` notification (if in-scope) | ✅ N/A — no scope escalation |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ Existing `provider.Verifier(&oidc.Config{ClientID: clientID}).Verify()` at auth.go:517 validates iss, aud, exp, signature. Nonce verification added on top (auth.go:535). |
| Matrix Power Level checks                   | ✅ N/A — no room operations |
| No hardcoded secrets                        | ✅ No secrets in diff. Test constants (`testNonce`) are test-only values. |
| TLS 1.3 enforcement                        | ✅ N/A — no TLS config changes |
| AES-256-GCM correctness                    | ✅ N/A — no encryption changes |
| Ed25519 verify-before-accept               | ✅ N/A — no signature operations |
| No secrets in logs / error messages         | ✅ Nonce values in log (auth.go:536) are ephemeral random values, not secrets. Error responses to clients are generic ("Forbidden"). |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 0 |
| INFO      | 5 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*

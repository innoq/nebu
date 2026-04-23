# Security Review — Story 5-26: User Directory Search — LIKE Wildcard Escape + Input Validation — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (6 files, +918 / -39)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

This diff is a focused security remediation: it replaces an inline user-directory handler in `main.go` that had three distinct vulnerabilities (LIKE pattern injection, panic on malformed UID, missing input limits) with a properly structured handler/DB-layer split. The LIKE escaping implementation (`EscapeLIKE` via `strings.NewReplacer` with backslash-first ordering + `ESCAPE '\'` clause) is correct and PostgreSQL-idiomatic. SQL queries use parameterized `$1`/`$2` placeholders exclusively. The test suite (702 lines) covers all attack vectors comprehensively, including auth enforcement. No findings above INFO.

## Findings

### [INFO] Correctly remediated LIKE pattern injection (CWE-89, OWASP A03:2021)

- **CWE / OWASP:** CWE-89 (SQL Injection — LIKE wildcard variant) / A03:2021 (Injection)
- **File:** `gateway/internal/matrix/user_directory.go:62-67`, `gateway/internal/db/user_directory_store.go:32`
- **Description:** The previous inline handler used `fmt.Sprintf("%%%s%%", req.SearchTerm)` with no escaping, allowing `%` or `_` in user input to act as LIKE wildcards — enabling full user-table enumeration. The new implementation applies `EscapeLIKE()` (backslash-first ordering: `\` -> `\\`, `%` -> `\%`, `_` -> `\_`) before wrapping in `%...%`, and the SQL uses `ILIKE $1 ESCAPE '\'`. This is the correct approach for PostgreSQL.
- **Impact:** Vulnerability fully remediated. No residual risk.
- **Verification:**
  - `strings.NewReplacer` processes replacements left-to-right per character, not in sequence — backslash-first in the argument list ensures `\` is escaped before `%` and `_` rules add new backslashes. Correct.
  - The `ESCAPE '\'` clause in `user_directory_store.go:32` makes the escaping semantically effective at the PostgreSQL level.
  - Parameterized query (`$1`, `$2`) prevents classical SQL injection. No string concatenation in the SQL path.
  - Test `TestUserDirectory_EscapePatternForwardedToDBWithPercent` (line 595) proves the escaped pattern reaches the DB layer correctly for `%`, `_`, and `\` inputs.

### [INFO] Panic guard for malformed UID correctly implemented

- **CWE / OWASP:** CWE-20 (Improper Input Validation)
- **File:** `gateway/internal/matrix/user_directory.go:134-137`
- **Description:** The previous code used `uid[1:strings.Index(uid, ":")]` which panics when `uid` has no `:` (IndexByte returns -1, slice bounds out of range). The fix uses `strings.IndexByte(uid, ':')` with an explicit `i <= 0` guard that skips malformed rows. This also correctly handles the edge case where `:` is at index 0 (no localpart).
- **Impact:** Latent crash eliminated. Tested via `TestUserDirectory_NoPanic_OnMalformedUID` and `TestUserDirectory_NoPanic_OnMissingColon`.

### [INFO] Input validation and result cap correctly applied

- **CWE / OWASP:** CWE-20 (Improper Input Validation), CWE-400 (Uncontrolled Resource Consumption)
- **File:** `gateway/internal/matrix/user_directory.go:89-110`
- **Description:** Search term: trimmed, min 2 runes, max 64 runes. Result limit: default 10, max 100, negative values -> 10. These bounds prevent excessive DB load and enumeration via short/empty queries. The rune-based length check correctly handles multi-byte Unicode characters.
- **Impact:** DoS mitigation via input validation. Combined with the existing `bodyLimit1MiB` wrapper (visible at `main.go:430`), the endpoint has adequate defense-in-depth.

### [INFO] Auth enforcement verified on the route

- **CWE / OWASP:** A07:2021 (Identification and Authentication Failures)
- **File:** `gateway/cmd/gateway/main.go:430`
- **Description:** The route registration is `bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(userDirHandler.Search)))`. The `jwtMiddleware` wraps the handler — unauthenticated requests are rejected before reaching `Search()`. Test `TestUserDirectory_Unauthenticated_Returns401` (line 520) confirms this via the real JWT middleware wiring.
- **Impact:** No auth bypass possible on this endpoint.

### [INFO] Error handling does not leak sensitive information

- **CWE / OWASP:** CWE-209 (Generation of Error Message Containing Sensitive Information)
- **File:** `gateway/internal/matrix/user_directory.go:117-122`
- **Description:** On DB error, the handler logs `slog.Error("user_directory search failed", "err", err)` server-side and returns a static `{"results":[],"limited":false}` to the client. No internal error details, stack traces, or connection strings are exposed. The `writeMatrixError` calls for validation failures use hardcoded messages only.
- **Impact:** Clean error handling. No information disclosure.

### [INFO] No other unescaped LIKE patterns remain in the codebase

- **CWE / OWASP:** CWE-89
- **File:** (codebase-wide search)
- **Description:** A codebase-wide search for `Sprintf.*%%%s%%` returned zero results. All `ILIKE` usage in `.go` files is now in `user_directory_store.go` with proper `ESCAPE '\'` and parameterization. The old vulnerable pattern in `main.go` has been completely removed.
- **Impact:** No residual LIKE injection risk elsewhere.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — `users` and `profiles` are not compliance tables |
| `reason` field on compliance access         | ✅ N/A — no compliance data accessed |
| Audit-log immutability                      | ✅ N/A — no audit table changes |
| `instance_admin` notification (if in-scope) | ✅ N/A — no scope escalation |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — JWT middleware applied at route level (`main.go:430`), test confirms 401 without token |
| Matrix Power Level checks                   | ✅ N/A — read-only search, no room mutation |
| No hardcoded secrets                        | ✅ — no secrets in diff |
| TLS 1.3 enforcement                         | ✅ N/A — no TLS config changes |
| AES-256-GCM correctness                     | ✅ N/A — no crypto operations |
| Ed25519 verify-before-accept                | ✅ N/A — no signature verification |
| No secrets in logs / error messages         | ✅ — `slog.Error` logs only `"err"` key (DB error, no credentials); client receives static JSON |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 0 |
| INFO      | 6 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The diff is a well-executed security remediation. The LIKE escaping implementation is correct, the SQL is parameterized, input validation is comprehensive, auth is enforced, and the test suite covers all identified attack vectors including metacharacter injection, panic paths, and auth bypass attempts.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*

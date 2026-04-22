# Security Review — Story 5.17: OIDC Issuer HTTPS Enforcement + Provider Caching — 2026-04-20

**Agent:** Kassandra
**Diff base:** `--staged` (6 files, +540 / -10)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

The HTTPS enforcement logic in `validateIssuerURL` is sound for the stated threat model: `http://localhost.evil.com` is correctly rejected because `Hostname()` returns `"localhost.evil.com"` (no substring match), and `http://[::1]` is correctly permitted. The provider cache uses a clean lock-release-before-network-call pattern and negative caching prevents DoS amplification. Two findings require attention: the `LoginStartHandler` leaks upstream OIDC error details to the client (MEDIUM), and the `globalProviderCache` is a package-level `var` that cannot be swapped in integration tests, which creates a minor coupling issue but no runtime risk (INFO).

## Findings

### [HIGH] Duplicate `validateIssuerURL` — divergence risk between admin and auth packages

- **CWE / OWASP:** CWE-1188 (Insecure Default Initialization of Resource) — applicable as architectural smell leading to future security inconsistency
- **File:** `gateway/internal/admin/validation.go:11` and `gateway/internal/auth/oidc.go:25`
- **Description:** The function `validateIssuerURL` is implemented twice — once in `admin/validation.go` and once in `auth/oidc.go` — with identical logic. Both are package-private (`validateIssuerURL`, lowercase). If one is patched (e.g., to add `[::ffff:127.0.0.1]` support or to harden against IP-encoding bypasses), the other remains unpatched. The auth package copy is used at startup in `NewProvider()`; the admin copy is used in three hot-path handlers (Bootstrap, LoginStart, Callback). A divergence between the two creates a TOCTOU gap where one entry point enforces a stricter policy than the other.
- **Impact:** A future developer patches one copy but not the other, silently reopening the HTTP issuer vector on one of the two code paths. The duplication is invisible unless you know to search both packages.
- **Recommendation:** Extract `validateIssuerURL` into a shared internal package (e.g., `gateway/internal/oidcutil/validate.go`) or export it from `admin` and import it in `auth`. One source of truth for issuer validation.
- **Reference:** OWASP A04:2021 (Insecure Design), DRY principle for security-critical functions

### [MEDIUM] OIDC discovery error message leaked to client in `LoginStartHandler`

- **CWE / OWASP:** CWE-209 (Generation of Error Message Containing Sensitive Information)
- **File:** `gateway/internal/admin/auth.go:320`
- **Description:** When `globalProviderCache.load()` fails, the handler responds with `"OIDC provider discovery failed: " + err.Error()`. The `err` from `oidc.NewProvider` may contain the full issuer URL, the HTTP status code from the discovery endpoint, TLS handshake details, or internal network topology information (e.g., `dial tcp 10.0.1.42:443: connection refused`). This leaks server-side infrastructure details to the client.
- **Impact:** An attacker probing the `/admin/login/start` endpoint can learn internal hostnames, IP addresses, or port configurations of the OIDC provider. This is reconnaissance value, not a direct exploit.
- **Recommendation:** Replace with a generic message: `http.Error(w, "OIDC provider discovery failed. Check server configuration.", http.StatusServiceUnavailable)`. Log the full error server-side via `slog.Error` (already done in the Callback path at line 508 — apply the same pattern to LoginStart).
- **Reference:** OWASP A05:2021 (Security Misconfiguration), CWE-209

### [MEDIUM] `LoadOIDCConfig` error message leaked to client in `LoginStartHandler`

- **CWE / OWASP:** CWE-209
- **File:** `gateway/internal/admin/auth.go:307`
- **Description:** Same pattern as above: `"Failed to load OIDC configuration: " + err.Error()` may leak database error details (connection strings, table names, column names) if `LoadOIDCConfig` encounters a DB error.
- **Impact:** Same as above — reconnaissance value via error probing.
- **Recommendation:** Return a generic message. The `ErrOIDCConfigMissing` sentinel is already handled separately (line 303); for all other errors, use a generic `"Failed to load OIDC configuration. Contact the operator."` and log the full error server-side.
- **Reference:** OWASP A05:2021, CWE-209

### [MEDIUM] Provider cache has no maximum-size bound — unbounded memory growth

- **CWE / OWASP:** CWE-400 (Uncontrolled Resource Consumption)
- **File:** `gateway/internal/admin/oidc_cache.go:24` (`entries map[string]*cacheEntry`)
- **Description:** The `oidcProviderCache.entries` map grows without bound. Each unique issuer URL adds an entry that lives for 10 minutes (positive) or 30 seconds (negative). In the current architecture, the issuer URL comes from `server_config` (operator-controlled), so this is not directly exploitable. However, if a future code path passes user-controlled input as the issuer key, an attacker could fill memory with arbitrary cache entries.
- **Impact:** No direct exploit path today — the issuer value is read from DB config, not from the HTTP request. Defense-in-depth concern only.
- **Recommendation:** Add a `maxEntries` field (e.g., 100) and evict the oldest entry when the limit is reached. Alternatively, document explicitly that the issuer key must always come from trusted configuration.
- **Reference:** CWE-400, OWASP A04:2021

### [INFO] `validateIssuerURL` correctly handles the `http://localhost.evil.com` bypass attempt

- **CWE / OWASP:** n/a (positive finding)
- **File:** `gateway/internal/admin/validation.go:27`
- **Description:** The implementation uses `parsed.Hostname()` which returns `"localhost.evil.com"` for `http://localhost.evil.com` — this is an exact string comparison against `"localhost"`, so the subdomain-based bypass is correctly rejected. Similarly, `http://127.0.0.1.evil.com` returns hostname `"127.0.0.1.evil.com"`, which does not match `"127.0.0.1"`. The `http://[::1]:5556` case is correctly handled: `Hostname()` returns `"::1"`, matching the allowlist.
- **Reference:** Verified empirically via Go `net/url.ParseRequestURI` behavior.

### [INFO] Alternative IP representations of 127.0.0.1 are correctly rejected

- **CWE / OWASP:** n/a (positive finding)
- **File:** `gateway/internal/admin/validation.go:27`
- **Description:** `http://0x7f000001`, `http://2130706433`, `http://0177.0.0.1` (hex, decimal, octal representations of 127.0.0.1) are all rejected because Go's `Hostname()` returns the literal string without resolving it. The allowlist only matches `"127.0.0.1"` exactly. This is the correct behavior — no DNS resolution or IP normalization occurs.
- **Reference:** CWE-918 (SSRF) defense-in-depth verification

### [INFO] Cache race condition is benign — last-writer-wins is acceptable

- **CWE / OWASP:** n/a (positive finding)
- **File:** `gateway/internal/admin/oidc_cache.go:73-86`
- **Description:** The lock is released before calling `newFn` (line 71), which means two concurrent requests for the same expired issuer can both perform OIDC discovery. The last writer wins when they re-acquire the lock. This is a deliberate and documented design choice (comment at line 73). Both providers are valid — the only cost is one redundant network call during the narrow TTL-expiry window. No data corruption, no stale-provider risk.
- **Reference:** Standard cache-stampede tradeoff — acceptable given that issuer changes are infrequent.

### [INFO] AC2 call-site coverage is complete

- **CWE / OWASP:** n/a (positive finding)
- **File:** `gateway/internal/admin/auth.go:311`, `gateway/internal/admin/auth.go:499`, `gateway/internal/admin/bootstrap.go:168`, `gateway/internal/auth/oidc.go:51`
- **Description:** All four code paths that consume an OIDC issuer URL now call `validateIssuerURL` before use: `LoginStartHandler`, `CallbackHandler`, `StepHandler` (bootstrap step 2), and `auth.NewProvider` (startup). No unprotected call-site was found.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ n/a — no DB schema or compliance data changes |
| `reason` field on compliance access         | ✅ n/a — no compliance data access |
| Audit-log immutability                      | ✅ n/a — no audit table changes |
| `instance_admin` notification (if in-scope) | ✅ n/a — no scope escalation |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — this diff improves OIDC validation by adding issuer URL scheme enforcement at all entry points |
| Matrix Power Level checks                   | ✅ n/a — no room operations |
| No hardcoded secrets                        | ✅ — no secrets in source; test values are clearly fake (`"s3cr3t"`) |
| TLS 1.3 enforcement                         | ✅ n/a — no `tls.Config` changes |
| AES-256-GCM correctness                     | ✅ n/a — no encryption changes |
| Ed25519 verify-before-accept                | ✅ n/a — no signature operations |
| No secrets in logs / error messages          | ⚠️ — issuer URL is logged in `slog.Error` at lines 312 and 500 (acceptable for operator diagnostics, but issuer URL is configuration data, not a secret). However, `err.Error()` from OIDC discovery is returned to the client at line 320 — see MEDIUM finding above. |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 3 |
| LOW       | 0 |
| INFO      | 4 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The HIGH finding (duplicate `validateIssuerURL`) is an architectural smell that creates future divergence risk. It should be addressed before the next epic to prevent one copy from falling out of sync. The MEDIUM findings (error message leakage) should be fixed in this story or as a follow-up within the current sprint.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*

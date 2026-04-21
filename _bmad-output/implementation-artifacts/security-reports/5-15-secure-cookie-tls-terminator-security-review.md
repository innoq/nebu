# Security Review — Story 5.15: Secure Cookie Flag Behind TLS Terminator — 2026-04-20

**Agent:** Kassandra
**Diff base:** `git diff --staged` (6 files)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.15 introduces `isRequestSecure()` — a centralized helper that gates the `Secure` cookie flag and HSTS emission on either direct TLS (`r.TLS != nil`) or an explicitly opted-in proxy trust (`NEBU_TRUSTED_PROXY=true` + `X-Forwarded-Proto: https`). The implementation is fail-closed by design: without the env var, the `X-Forwarded-Proto` header is ignored. All nine cookie-emitting sites in `admin/auth.go` and the CSRF cookie in `middleware.go` have been migrated. The redirect_uri scheme construction (three occurrences in `auth.go`) likewise uses the new helper. Test coverage is thorough — four unit tests for the helper itself plus two integration tests that exercise `LoginHandler` end-to-end with and without proxy trust.

One observation outside the staged diff scope: `internal/matrix/sso.go:120` still uses `r.TLS != nil` to construct the Matrix SSO `redirect_uri`. This file is not part of this story, but will need the same treatment if the gateway is deployed behind a TLS-terminating proxy for Matrix API traffic (noted as INFO).

## Findings

### [INFO] Matrix SSO redirect_uri not migrated to isRequestSecure

- **CWE / OWASP:** CWE-319 / A05:2021 (Security Misconfiguration)
- **File:** `gateway/internal/matrix/sso.go:120`
- **Description:** `ssoCallbackURL()` still derives the scheme from `r.TLS != nil` directly, without consulting `NEBU_TRUSTED_PROXY`. This means that when the gateway sits behind a TLS-terminating proxy, the Matrix SSO redirect_uri will be constructed as `http://` — causing the OIDC callback to fail or be downgraded. The function is in the `matrix` package (not `admin`), so it cannot call `admin.isRequestSecure()` directly.
- **Impact:** Matrix SSO login will fail in proxy-terminated deployments because Dex will reject the mismatched redirect_uri, or the callback will arrive on an insecure scheme. No cookie theft vector here (SSO uses server-side state, not cookies), but functionality is broken.
- **Recommendation:** Extract `isRequestSecure` into a shared package (e.g., `internal/httputil`) or duplicate the logic in the `matrix` package. Track as a follow-up task — this is a deployment correctness issue, not a security vulnerability.
- **Reference:** OWASP A05:2021

### [INFO] os.Getenv called per-request in isRequestSecure

- **CWE / OWASP:** N/A (performance observation)
- **File:** `gateway/internal/admin/secure.go:15`
- **Description:** `isRequestSecure()` reads `os.Getenv("NEBU_TRUSTED_PROXY")` on every invocation. Environment variables do not change at runtime in a containerized deployment, so this is functionally correct and safe. However, reading the env var once at startup (e.g., as a package-level variable or a field on `AdminAuth`) would be marginally cleaner and would prevent a hypothetical env-injection scenario where a compromised process modifies the environment mid-flight.
- **Impact:** None in practice. The current approach is correct for an MVP.
- **Recommendation:** Consider caching the value at startup in a future refactoring pass. No action required now.

### [INFO] Positive — fail-closed design correctly implemented

- **CWE / OWASP:** N/A
- **File:** `gateway/internal/admin/secure.go:11-18`
- **Description:** The helper returns `false` when `NEBU_TRUSTED_PROXY` is unset and `r.TLS` is nil, even if `X-Forwarded-Proto: https` is present. This prevents an attacker from injecting the header on a non-proxied deployment to trick the gateway into setting `Secure` cookies (which would be harmless) or — more importantly — prevents trusting the proto header for scheme decisions when no proxy is configured. Test `TestIsRequestSecure_NoTrustNoProxy` explicitly covers this.
- **Impact:** Positive security property.

### [INFO] Positive — startup misconfiguration warning

- **CWE / OWASP:** N/A
- **File:** `gateway/cmd/gateway/main.go:77-80`
- **Description:** When `NEBU_TRUSTED_PROXY=true` but the OIDC issuer or public base URL starts with `http://`, the gateway logs a warning. This is good operational hygiene — the most common misconfiguration (proxy terminates TLS but the app URLs still say `http://`) will be flagged at startup.
- **Impact:** Positive operational property.

### [INFO] Positive — redirect_uri scheme fix (Code-Review MAJOR)

- **CWE / OWASP:** N/A
- **File:** `gateway/internal/admin/auth.go:261-264`, `:311-314`, `:472-475`
- **Description:** The previous code defaulted to `scheme := "https"` and fell back to `"http"` when `r.TLS == nil`. This was inverted: behind a proxy, the scheme would default to HTTPS even without verification that the external hop was actually secure. The new code defaults to `"http"` and promotes to `"https"` only when `isRequestSecure(r)` returns true. This is the correct fail-closed default for redirect_uri construction.
- **Impact:** Positive — fixes the MAJOR finding from code review.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — no DB / compliance changes |
| `reason` field on compliance access         | ✅ N/A — no compliance data access |
| Audit-log immutability                      | ✅ N/A — no migration / audit changes |
| `instance_admin` notification (if in-scope) | ✅ N/A — no scope escalation paths |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ Not affected — existing OIDC validation unchanged |
| Matrix Power Level checks                   | ✅ N/A — no room mutation handlers |
| No hardcoded secrets                        | ✅ No secrets in diff |
| TLS 1.3 enforcement                         | ✅ N/A — no `tls.Config` changes |
| AES-256-GCM correctness                     | ✅ N/A — no crypto changes |
| Ed25519 verify-before-accept                | ✅ N/A — no signature handling |
| No secrets in logs / error messages         | ✅ Startup warning logs only env-var names, not values |

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

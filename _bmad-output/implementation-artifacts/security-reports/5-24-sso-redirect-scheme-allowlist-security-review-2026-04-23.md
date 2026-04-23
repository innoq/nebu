# Security Review — 5-24 SSO Redirect Scheme Allowlist — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (HEAD = 00bdd47)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`

## Executive Summary

Story 5.24 replaces a wide-open scheme policy (anything except non-loopback http/https) with a strict allowlist plus a hardcoded denylist for dangerous schemes. The implementation is sound: the denylist takes precedence over operator configuration, the error response does not echo user input, and logging uses only the parsed scheme rather than the raw URL. One minor regression was found: IPv6 loopback (`::1`) was previously allowed for `http://` redirects and is now silently dropped. No exploitable vulnerability was identified.

## Findings

### [LOW] IPv6 loopback `::1` removed from http:// allowlist — regression from prior behavior

- **CWE / OWASP:** CWE-284 (Improper Access Control) — functional regression, not a vulnerability
- **File:** `gateway/internal/matrix/sso.go:221-222`
- **Description:** The previous implementation allowed `http://` redirects to `localhost`, `127.0.0.1`, and `::1`. The new implementation checks only `localhost` and `127.0.0.1`. On systems where a local development client binds to `[::1]` (IPv6 loopback), `http://[::1]:8080/callback` will be rejected. This is a functional regression, not a security issue — the old behavior was intentional and safe.
- **Impact:** Developers using IPv6 loopback addresses for local Matrix client testing will receive a 400 error. No security impact since `::1` is a legitimate loopback address.
- **Recommendation:** Add `host == "::1"` to the loopback check at line 222:
  ```go
  return host == "localhost" || host == "127.0.0.1" || host == "::1"
  ```
  Add a test case for `http://[::1]:8080/callback` to the test suite.
- **Reference:** RFC 6761 (localhost), RFC 4291 (IPv6 loopback)

### [INFO] Operator-configured schemes are not validated at startup

- **CWE / OWASP:** N/A — defense-in-depth observation
- **File:** `gateway/internal/config/config.go:74-89`
- **Description:** `getEnvStringSlice("NEBU_SSO_REDIRECT_SCHEMES")` accepts any string values from the environment variable without validation. An operator could configure `"javascript,data"` — these would be passed to `isRedirectURLAllowedWithSchemes` but would be neutralized by the hardcoded `schemeDenylist` at runtime (line 210). The defense-in-depth works correctly, but a startup log warning when a configured scheme collides with the denylist would help operators notice misconfigurations earlier.
- **Impact:** None — the denylist takes precedence at runtime regardless of configuration. This is a usability observation, not a vulnerability.
- **Recommendation:** Consider logging a warning at startup when any value in `NEBU_SSO_REDIRECT_SCHEMES` matches a denylist entry. This prevents silent misconfiguration.

### [INFO] Attack surface observation — `https://` is allowed to any host

- **CWE / OWASP:** CWE-601 (Open Redirect) — accepted risk, documented in code
- **File:** `gateway/internal/matrix/sso.go:214-216`
- **Description:** `https://` URLs are allowed to any host. This means an attacker can set `redirectUrl=https://evil.example.com/harvest` and the SSO flow will redirect the user there with a valid `loginToken`. The loginToken is opaque and single-use (30s TTL from Story MAJOR-2), which limits the window, but the redirect itself is open. This is documented in the code comment as intentional: "HTTPS is safe for open-redirect since TLS prevents MITM token capture and the host is validated by the browser." This is an accepted architectural decision, not a finding — but worth noting for the audit trail. The Matrix Client-Server spec requires `redirectUrl` to accept arbitrary https:// origins because clients may be hosted on any domain.
- **Impact:** An attacker could craft a phishing link that redirects through the SSO endpoint to an attacker-controlled https site. The loginToken in the URL would be captured. However, the token is single-use and expires in 5 minutes (line 364 — note: the code comment says 30 seconds but the TTL is `5*time.Minute`).
- **Recommendation:** No code change required for the https allowance per Matrix spec. However, the loginToken TTL of 5 minutes at line 364 may be more generous than intended — the comment on line 83 says "30 seconds" but the actual TTL is `5*time.Minute`. If the intent was 30 seconds, this should be corrected separately (pre-existing, not introduced by this story).

### [INFO] Comprehensive test coverage for security-critical logic

- **File:** `gateway/internal/matrix/sso_redirect_test.go` (466 lines)
- **Description:** The test suite covers all acceptance criteria with well-structured categories: HTTPS allowance, HTTP loopback-only, default deep-link schemes, configured custom schemes, rejection of unconfigured schemes, all denylist entries, case-insensitive scheme matching, blocklist-wins-over-allowlist defense-in-depth, HTTP handler response validation (no scheme echo in error body), and errcode verification. The two-layer testing approach (function-level + handler-level for XSS reflection) is a strong pattern.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ — not applicable (no DB access in this diff) |
| `reason` field on compliance access         | ✅ — not applicable |
| Audit-log immutability                      | ✅ — not applicable |
| `instance_admin` notification (if in-scope) | ✅ — not applicable |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — not affected; OIDC validation unchanged |
| Matrix Power Level checks                   | ✅ — not applicable (SSO redirect is pre-auth) |
| No hardcoded secrets                        | ✅ — no secrets in diff |
| TLS 1.3 enforcement                        | ✅ — not affected |
| AES-256-GCM correctness                    | ✅ — not applicable |
| Ed25519 verify-before-accept               | ✅ — not applicable |
| No secrets in logs / error messages         | ✅ — log now uses `schemeOf()` which returns only the parsed scheme, never the full URL. Error response uses a static message without echoing user input. Improvement over the previous `"url", clientRedirectURL` log pattern. |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**CLEAN** -- no CRITICAL / HIGH findings. Pipeline may proceed.

The single LOW finding (IPv6 `::1` loopback regression) is a functional issue, not a security vulnerability. It should be addressed but does not block the pipeline.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*

---
security_review: required
---

# Story 5.24: SSO Redirect Scheme Allowlist

Status: review

## Story

As a security-conscious operator,
I want the Matrix SSO redirect URL validator to allowlist only `https://` + configured Matrix-client deep-link schemes,
so that `javascript:`, `intent:`, `file:`, `data:` and other hostile schemes cannot be used to exfiltrate the SSO `loginToken`.

---

## Background / Motivation

Security audit (2026-04-20): `matrix/sso.go:137–156` accepts any non-empty URL scheme that is not `http`:

```go
if u.Scheme != "http" && u.Scheme != "https" { return u.Scheme != "" }
```

The in-code comment argues custom schemes are safe because "browsers do not follow them", but this is incorrect for:
- Embedded webviews (e.g. mobile Matrix apps)
- Non-browser clients (curl, scanners)
- Browsers that do follow `intent://` (Android) — the `loginToken` query param is then harvested by the receiving app

The correct pattern is strict allowlist of known Matrix-client schemes, configurable by the operator.

---

## Acceptance Criteria

1. `matrix/sso.go:isRedirectURLAllowed` rejects any URL whose scheme is not in:
   - `https` (always allowed)
   - `http` — only if host is `localhost` or `127.0.0.1` (development)
   - The allowlist of deep-link schemes configured in `NEBU_SSO_REDIRECT_SCHEMES` (comma-separated)

2. Default allowlist includes well-known Matrix-client schemes:
   - `element` (Element)
   - `io.element.fluffychat`
   - `fluffychat`
   - (list finalized during implementation, confirmed via Matrix client docs)

3. Explicit deny of `javascript`, `data`, `file`, `vbscript`, `blob` — these return an error even if an operator accidentally adds them to the allowlist (hardcoded blocklist wins).

4. Rejected redirect → 400 with error body `M_INVALID_PARAM`, scheme name NOT echoed back (no XSS vector).

5. Unit tests cover every scheme category:
   - `TestSSORedirect_AllowsHTTPS`
   - `TestSSORedirect_AllowsConfiguredCustomScheme`
   - `TestSSORedirect_RejectsJavaScript`
   - `TestSSORedirect_RejectsDataURL`
   - `TestSSORedirect_RejectsUnconfiguredCustomScheme`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. Table-driven test covering 10+ scheme/host combinations with expected allow/deny

2. `TestSSORedirect_ErrorDoesNotLeakScheme` — response body for `javascript:alert(1)` does not contain `javascript` or `alert`

---

## Implementation Notes

- Blocklist takes precedence over allowlist (defense in depth)
- Configure via env var at startup; parse once
- Regression: FluffyChat + Element smoke tests (Story 4-24/4-28) must still pass

---

## Dev Agent Record

**Date:** 2026-04-23
**Agent:** Amelia (Senior Developer)

### Implementation Summary

**Files changed:**

1. `gateway/internal/matrix/sso.go`
   - Replaced old `isRedirectURLAllowed` (which accepted any non-http scheme) with a strict allowlist implementation
   - Added `defaultDeepLinkSchemes` (`element`, `io.element.fluffychat`, `fluffychat`)
   - Added `schemeDenylist` (`javascript`, `data`, `file`, `vbscript`, `blob`) — blocklist wins over allowlist
   - Added `isRedirectURLAllowedWithSchemes(raw string, extraSchemes []string) bool` for operator-configured schemes
   - `isRedirectURLAllowed` delegates to `isRedirectURLAllowedWithSchemes(raw, nil)`
   - `GetSSORedirect` now calls `isRedirectURLAllowedWithSchemes(clientRedirectURL, h.ssoRedirectSchemes)` and does NOT echo the scheme in the error response (AC 4 — XSS prevention)

2. `gateway/internal/matrix/login.go`
   - Added `ssoRedirectSchemes []string` field to `LoginHandler`
   - Added `SSORedirectSchemes []string` field to `LoginConfig`
   - `NewLoginHandler` wires the field from config

3. `gateway/internal/config/config.go`
   - Added `SSORedirectSchemes []string` field (`NEBU_SSO_REDIRECT_SCHEMES`, comma-separated)
   - Added `getEnvStringSlice` helper that splits on comma and trims whitespace
   - Added `strings` import

4. `gateway/cmd/gateway/main.go`
   - Wired `cfg.SSORedirectSchemes` into `matrix.LoginConfig.SSORedirectSchemes`

### Test Results

All 76 SSO redirect tests pass. Full test suite: 14 packages, 0 failures.

### AC Coverage

- AC 1 (https always allowed, http only loopback, configured schemes): covered by `TestSSORedirect_AllowsHTTPS`, `TestSSORedirect_AllowsHTTPLocalhost`, `TestSSORedirect_AllowsConfiguredCustomScheme`
- AC 2 (default allowlist `element`, `io.element.fluffychat`, `fluffychat`): covered by `TestSSORedirect_AllowsDefaultDeepLinkSchemes`
- AC 3 (hard deny blocklist): covered by `TestSSORedirect_RejectsJavaScript`, `TestSSORedirect_RejectsDataURL`, `TestSSORedirect_RejectsBlocklistedSchemes`, `TestSSORedirect_BlocklistWinsOverAllowlist`
- AC 4 (400 + M_INVALID_PARAM + scheme not echoed): covered by `TestSSORedirect_ErrorDoesNotLeakScheme`, `TestSSORedirect_ErrorCodeIsInvalidParam`
- AC 5 (named unit tests for every category): all named tests present and passing

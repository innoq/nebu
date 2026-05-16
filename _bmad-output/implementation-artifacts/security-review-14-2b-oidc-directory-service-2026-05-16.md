# Security Review — Story 14.2b: Gateway OIDC Directory Service + Cache + Rate Limit
**Reviewer:** Kassandra (Security Agent)
**Date:** 2026-05-16
**Story:** 14.2b
**Files reviewed:** gateway/internal/admin/oidc_directory.go, gateway/internal/admin/oidc_directory_test.go

## Verdict: CLEAN — 0 CRITICAL, 0 HIGH

## CRITICAL Requirements Audit

| Req | Description | Status |
|-----|-------------|--------|
| CR-1 | HTTPS-only — validate at each call; fail hard | ✓ PASS |
| CR-2 | No redirect following — ErrUseLastResponse | ✓ PASS |
| CR-3 | Bearer token never in logs — secretString type | ✓ PASS |
| CR-4 | 10 MB response cap — io.LimitReader | ✓ PASS |
| CR-5 | Rate limit keyed on verified session ID (not IP) | ✓ PASS |

## HIGH Requirements Audit

| Req | Description | Status |
|-----|-------------|--------|
| HR-1 | Admin-only access gate | DEFERRED to handler (LOW advisory) |
| HR-2 | SSRF scope limitation | ✓ Option B documented (acceptable for Epic 14) |
| HR-3 | Claim values untrusted — length limits | ✓ PASS (truncate at 512 bytes) |

## MEDIUM Requirements Audit

| Req | Description | Status |
|-----|-------------|--------|
| MR-1 | Cache keyed on endpoint + token | ✓ PASS |
| MR-2 | 10-second timeout | ✓ PASS |
| MR-3 | Explicit status code handling | ✓ PASS |
| MR-4 | singleflight concurrent refresh | ✓ PASS |

## Findings

### LOW-1: HR-1 admin-only access gate must be enforced in handler (not service)
This story creates the OIDC directory service layer only — no HTTP handler is wired.
The next story implementing the search endpoint MUST place the service call behind the
admin auth middleware. The service has no way to enforce this itself.
**Action:** Next story (search handler) MUST verify admin auth middleware is in the router chain.

### MEDIUM-1 (advisory): Caller MUST call Allow() before FetchUsers()
The rate limit is separated from FetchUsers by design (documented in the API). However,
if a future handler calls FetchUsers without calling Allow() first, rate limiting is silently
bypassed. The search handler story MUST include an integration test that verifies rate limiting
is enforced (T5 from the security guide testing checklist).
**Action:** Track as follow-up for search handler story. Include in story acceptance criteria.

## Additional Security Checks (all N/A or PASS)
- SQL injection: no SQL in this file
- XSS: no HTML template rendering; callers warned in docstring
- CSRF: no state-changing endpoints in this file
- Timing attacks: no secret comparison; SHA-256 cache key is not security-critical
- Open redirects: CheckRedirect blocks all redirects
- Weak crypto: SHA-256 for cache key (not a security primitive)
- Path traversal: no filesystem access
- JWT validation: handled by admin middleware (not in scope for this file)
- Missing body-size limits: io.LimitReader enforces 10 MB cap

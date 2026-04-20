---
security_review: required
---

# Story 5.13: CSRF Double-Submit-Cookie Middleware for Admin POST Endpoints

Status: ready-for-dev

## Story

As an instance admin,
I want every state-changing POST/PUT/DELETE under `/admin/*` to require a CSRF token,
so that a malicious site cannot trigger admin actions on behalf of a logged-in admin.

---

## Background / Motivation

Security audit (2026-04-20): zero CSRF protection across `/admin/*` POST endpoints. `grep -r csrf` returns no matches. `SameSite=Lax` cookies partially mitigate cross-origin POST form submissions in modern browsers but do not cover every attack surface (subdomains, older browsers, top-level auto-submitted forms with GET-like semantics).

Concrete exposed endpoints:
- `POST /admin/bootstrap` (StepHandler)
- `POST /admin/bootstrap/select-claim` (ClaimSelectionHandler) — high-impact, combined with Story 5.10/5.11
- `POST /admin/logout` (LogoutHandler)

---

## Acceptance Criteria

1. `admin/middleware.go` adds `CSRFMiddleware` implementing double-submit-cookie:
   - On GET to any `/admin/*` page that renders a form: issue cookie `csrf_token=<random32bytes>; Path=/admin; HttpOnly=false; SameSite=Strict; Secure=<see 5-15>`
   - Inject a matching hidden input `<input type="hidden" name="_csrf" value="…">` into every form via template helper
2. On POST/PUT/DELETE to `/admin/*`: reject with **403 Forbidden** if:
   - `csrf_token` cookie missing
   - `_csrf` form value missing
   - The two values are not equal (constant-time compare via `subtle.ConstantTimeCompare`)

3. The following endpoints are wrapped by `CSRFMiddleware`:
   - `POST /admin/bootstrap`
   - `POST /admin/bootstrap/select-claim`
   - `POST /admin/logout`

4. All existing admin HTML form templates (`bootstrap.html`, `bootstrap-claims.html`, any logout form) include the `_csrf` hidden field rendered via the new template helper.

5. SSE endpoint `/admin/metrics/stream` (GET-only) is **not** wrapped (SSE requires no CSRF token).

6. The CSRF cookie is rotated on login (new token after `/admin/callback` success) to bind it to the session.

7. Unit tests:
   - `TestCSRF_RejectsPOSTWithoutToken`
   - `TestCSRF_RejectsMismatchedToken`
   - `TestCSRF_AcceptsMatchingToken`
   - `TestCSRF_RotatesOnLogin`

8. E2E test: full bootstrap flow in Playwright still passes green (the form submission includes the `_csrf` field).

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestCSRF_RejectsPOSTWithoutToken` — Go httptest
   - Given: valid admin session, POST `/admin/logout` without `_csrf` field
   - Then: 403

2. `TestCSRF_RejectsMismatchedToken` — Go httptest
   - Given: cookie `csrf_token=A`, form `_csrf=B`
   - Then: 403, constant-time compare (document in PR)

3. Playwright E2E: `admin-bootstrap.spec.ts` — full wizard, token extracted from cookie and compared to form hidden input

---

## Implementation Notes

- Generate token: `crypto/rand.Read([32]byte)` → `base64.RawURLEncoding`
- Template helper: `{{ csrfField . }}` emits `<input type="hidden" name="_csrf" value="{{.CSRFToken}}">`
- Inject `CSRFToken` into page-data struct from the middleware via request context
- `POST /admin/bootstrap/select-claim` will already be behind `BootstrapGuard` (Story 5.10) — CSRF applies in addition

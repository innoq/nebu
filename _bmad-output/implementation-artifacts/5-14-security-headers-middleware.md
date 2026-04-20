---
security_review: required
---

# Story 5.14: Security Headers Middleware for `/admin/*`

Status: ready-for-dev

## Story

As an instance admin,
I want every HTML response under `/admin/*` to carry standard security headers (CSP, X-Frame-Options, HSTS, X-Content-Type-Options, Referrer-Policy),
so that clickjacking, MIME-sniffing, and future XSS attempts are mitigated by default.

---

## Background / Motivation

Security audit (2026-04-20): `grep -E 'X-Frame-Options|Content-Security-Policy|X-Content-Type-Options|Referrer-Policy|Strict-Transport-Security'` across `gateway/internal/` returns zero matches (only `node_modules`). `admin/handler.go:94` (`render()`) sets only `Content-Type: text/html; charset=utf-8`.

---

## Acceptance Criteria

1. `admin/middleware.go` adds `SecurityHeadersMiddleware` that sets on every `/admin/*` response:
   - `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'`
   - `X-Frame-Options: DENY`
   - `X-Content-Type-Options: nosniff`
   - `Referrer-Policy: no-referrer`
   - `Strict-Transport-Security: max-age=63072000; includeSubDomains` (only when the request is served over HTTPS — check `r.TLS != nil` OR `X-Forwarded-Proto == https`)

2. Middleware is applied to all `/admin/*` routes in `cmd/gateway/main.go`.

3. The inline `oninput` handler in `bootstrap-claims.html:66` is moved to an external JS module so it passes CSP `script-src 'self'` (no `unsafe-inline`).

4. SSE endpoint (`/admin/metrics/stream`) still works — CSP `connect-src 'self'` allows it.

5. Unit test: every `/admin/*` response carries all five headers. Verify via `httptest.NewRecorder`.

6. Browser E2E: dashboard loads, no CSP violations in console.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestSecurityHeaders_AllPresentOnAdminPages` — Go httptest, table-driven over dashboard/bootstrap/login/errors

2. `TestHSTS_OnlyOnHTTPS` — HTTPS request carries HSTS, plain HTTP does not

3. Playwright: `browser_console_messages` during dashboard load contains no CSP violations

---

## Implementation Notes

- Middleware order: `SecurityHeadersMiddleware` outermost (before `SessionGuard` / `BootstrapGuard` so even 302 redirects carry headers)
- Move `bootstrap-claims.html:66` inline handler to `admin/static/js/bootstrap-claims.js`; served via existing static handler
- Keep `'self'` strict — do NOT add `unsafe-inline` or `unsafe-eval`. If a widget needs it, refactor the widget.

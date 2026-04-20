---
security_review: required
---

# Story 5.15: Secure Cookie Flag Behind TLS Terminator

Status: ready-for-dev

## Story

As an operator running Nebu behind a reverse proxy that terminates TLS,
I want the `Secure` flag on admin cookies to reflect the external scheme (`X-Forwarded-Proto`),
so that session cookies are not sent over plaintext on the external hop.

---

## Background / Motivation

Security audit (2026-04-20): `Secure: r.TLS != nil` (`admin/auth.go:536`, `:643`, `:664`). In a standard prod deployment (Ingress/Nginx/Caddy → gateway over HTTP on an internal network), `r.TLS==nil` → cookie issued without `Secure` flag → a MITM on any plaintext hop (even internal) can steal it. Proxy-forwarded-proto trust is the standard fix.

---

## Acceptance Criteria

1. New env var `NEBU_TRUSTED_PROXY` (default: empty). Values:
   - empty / unset → trust only `r.TLS != nil`
   - `true` → also trust `X-Forwarded-Proto == https`

2. All three cookie-emitting sites in `admin/auth.go` (`admin_oidc_state`, `admin_session`, CSRF token from Story 5.13) derive `Secure` from a shared helper `isRequestSecure(r *http.Request) bool`.

3. When `NEBU_TRUSTED_PROXY=true` but `X-Forwarded-Proto` is missing, the helper returns `false` (fail-closed, no assumption).

4. Config validation: if `NEBU_TRUSTED_PROXY=true` and `NEBU_OIDC_ISSUER` or `NEBU_PUBLIC_BASE_URL` begins with `http://`, log a startup warning (likely misconfiguration).

5. Unit tests:
   - `TestIsRequestSecure_TLSDirect`
   - `TestIsRequestSecure_ForwardedProtoHTTPS`
   - `TestIsRequestSecure_FailClosedWhenHeaderMissing`
   - `TestIsRequestSecure_NoTrustNoProxy`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestCookie_SecureFlagWithForwardedProto` — Go httptest, `NEBU_TRUSTED_PROXY=true`, request with `X-Forwarded-Proto: https`, verify `Secure` flag set on all three cookies

2. `TestCookie_NoSecureFlagWithoutTrust` — `NEBU_TRUSTED_PROXY=` (unset), same headers, verify `Secure=false`

---

## Implementation Notes

- Add `isRequestSecure(r)` in `admin/util.go` (or wherever shared helpers live)
- Do not implement `X-Forwarded-For` parsing here; only `-Proto` is needed
- Document in `docs/deployment.md` that operators running behind TLS-terminating proxies must set `NEBU_TRUSTED_PROXY=true`

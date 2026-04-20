---
security_review: required
---

# Story 5.16: OIDC Nonce Verification on Admin Callback

Status: ready-for-dev

## Story

As an instance admin,
I want the admin OIDC flow to include and verify the `nonce` claim per OIDC spec,
so that an attacker cannot login-fixate me into their Dex account via a prepared `state`/`code` combination.

---

## Background / Motivation

Security audit (2026-04-20): `admin/auth.go:348–383` verifies `queryState == sc.State` but never sets `oidc.Nonce(...)` in `AuthCodeURL` and never checks `idToken.Nonce` in the callback. Per OIDC spec, `nonce` binds the ID token to the browser session. Without it, a login-CSRF scenario is possible: attacker initiates OIDC on their own browser, gets the `state`, then tricks a victim into loading the callback URL — the victim's browser exchanges the code in Dex using the attacker's state cookie, resulting in the victim being logged in as the attacker's user.

---

## Acceptance Criteria

1. `LoginStartHandler` generates a per-request `nonce` (32 random bytes, base64url).

2. `nonce` is stored in the signed state cookie alongside `state`, `verifier`, and `mode`.

3. `AuthCodeURL` is built with `oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.AccessTypeOnline)`.

4. `CallbackHandler` verifies `idToken.Nonce == sc.Nonce` after signature verification. Mismatch → 403 Forbidden.

5. The signed-cookie struct version is bumped so older cookies (without `nonce`) are rejected as invalid.

6. Unit tests:
   - `TestCallback_RejectsMismatchingNonce`
   - `TestCallback_AcceptsMatchingNonce`
   - `TestLoginStart_EmitsNonceInAuthCodeURL`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestLoginStart_EmitsNonceInAuthCodeURL` — Go httptest
   - Given: login start request
   - Then: redirect URL contains `nonce=<...>` query param AND the state cookie contains a matching nonce

2. `TestCallback_RejectsMismatchingNonce` — Go httptest + stubbed `oidc.IDTokenVerifier`
   - Given: callback with ID token whose `nonce` does not match state cookie
   - Then: 403, no session created

3. Playwright: full admin OIDC flow still works end-to-end (regression)

---

## Implementation Notes

- `admin/auth.go:stateCookie` struct — add `Nonce string`
- Bump HMAC-signed cookie version byte to invalidate old cookies
- `idToken.Nonce` available after `verifier.Verify` — compare before `idToken.Claims(&claims)`

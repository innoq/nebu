---
security_review: required
---

# Story 5.17: OIDC Issuer HTTPS Enforcement + Provider Caching

Status: ready-for-dev

## Story

As an instance admin,
I want Bootstrap to reject non-HTTPS OIDC issuer URLs (except `localhost` for dev),
and I want the OIDC provider metadata cached so that login does not break on every IdP hiccup,
so that production deployments cannot leak tokens over plaintext HTTP and login stays fast + resilient.

---

## Background / Motivation

Security audit (2026-04-20) found two issues in one code path:

1. `admin/auth.go:244–249` and `:392–398`: `oidc.NewProvider(ctx, issuer)` uses `http.DefaultClient` with no scheme enforcement. A misconfigured operator setting `oidc_issuer=http://dex:5556` leaks all tokens + client_secret over plaintext.
2. Same call sites fetch `.well-known/openid-configuration` **on every admin login/callback**. A brief IdP outage cascades into a full admin-login outage and amplifies IdP load.

---

## Acceptance Criteria

1. Bootstrap API (`POST /admin/bootstrap`, `StepHandler` step 2) rejects `oidc_issuer` values that do not begin with:
   - `https://`
   - `http://localhost` or `http://127.0.0.1` (dev allowance)
   - Returns 400 `M_BAD_JSON` with a clear message when rejected.

2. Runtime load of `oidc_issuer` (`admin/auth.go:LoadOIDCConfig` and `matrix/login.go` equivalents) performs the same check at read time. If a legacy non-HTTPS value is in the DB, startup logs an error and admin login returns 500 with an operator-actionable message ("reconfigure OIDC issuer to HTTPS").

3. `admin/auth.go` caches the `*oidc.Provider` in a package-level `sync.Map` keyed by issuer URL with a 10-minute TTL. Cache miss → call `oidc.NewProvider` and populate. Cache hit → reuse. Negative-cache failures for 30s to avoid hammering a down IdP.

4. Unit tests:
   - `TestBootstrap_RejectsHTTPIssuer`
   - `TestBootstrap_AllowsLocalhost`
   - `TestOIDCProviderCache_ReusesInstance`
   - `TestOIDCProviderCache_NegativeCacheOnFailure`

5. Existing Gherkin auth flow passes (regression).

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestBootstrap_RejectsHTTPIssuer` — Go httptest, body `{"oidc_issuer":"http://evil.example","..."}`, expect 400

2. `TestOIDCProviderCache_ReusesInstance` — call `loadProvider(issuer)` twice; assert underlying `oidc.NewProvider` is called only once

3. `TestOIDCProviderCache_NegativeCacheOnFailure` — first call fails with network error; second call within 30s returns the cached error without re-calling

---

## Implementation Notes

- `admin/oidc_cache.go` — new file, small cache with TTL + negative cache
- Validation helper `validateIssuerURL(s string) error` in `admin/validation.go`; reused by bootstrap API and LoadOIDCConfig
- Do not use `context7` MCP here — the go-oidc API has not changed; just wrap it

---
security_review: required
---

# Story 5.18: OIDC JWT Algorithm Pinning

Status: ready-for-dev

## Story

As a security-conscious operator,
I want the OIDC verifier to accept only a configured whitelist of JWT signing algorithms,
so that algorithm-confusion attacks (e.g. adding HS256 to a compromised JWKS) are blocked.

---

## Background / Motivation

Security audit (2026-04-20): `gateway/internal/middleware/auth.go:73` constructs the verifier via `inner.Verifier(&oidc.Config{ClientID: clientID})` — no `SupportedSigningAlgs` set. go-oidc therefore accepts any algorithm advertised in the IdP JWKS. If the IdP ever exposes HS256 alongside RS256, algorithm-confusion is possible. Explicit pinning is defense in depth and removes a whole class of future regressions.

---

## Acceptance Criteria

1. `middleware/auth.go` passes `SupportedSigningAlgs: []string{"RS256"}` (Dex default) to `oidc.Config` when building the verifier.

2. Same change applied to `admin/auth.go` verifier construction.

3. `matrix/login.go` verifier construction (for raw-JWT login path) applies the same pin.

4. Env var `NEBU_OIDC_SUPPORTED_ALGS` (default: `RS256`) allows operators to override if they use Keycloak/Azure with ES256, etc. Comma-separated values.

5. Unit test: construct a signed HS256 token with the same secret as the verifier "public" key — expect `Verify` to fail with `unsupported alg` error.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestJWT_HS256Rejected` — construct HS256 token, run through `JWTMiddleware`, expect 401 `M_UNKNOWN_TOKEN`

2. `TestJWT_RS256Accepted` — sign with test RSA key, expect 200

3. `TestSupportedAlgs_OverrideViaEnv` — set `NEBU_OIDC_SUPPORTED_ALGS=ES256`; RS256 now rejected, ES256 accepted

---

## Implementation Notes

- go-oidc's `oidc.Config.SupportedSigningAlgs` is documented; no library change
- Parse env var once at startup and pass to every verifier builder
- Keep the default conservative: `RS256` only

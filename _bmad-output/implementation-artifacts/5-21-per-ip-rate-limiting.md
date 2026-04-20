---
security_review: required
---

# Story 5.21: Per-IP Rate Limiting for Public Endpoints

Status: ready-for-dev

## Story

As an operator running Nebu in production,
I want a per-IP rate limit on `/login`, `/login/sso/*`, `/admin/login/*`, and `/admin/bootstrap`,
so that brute-force and SSO-state-flooding are rate-limited at the gateway.

---

## Background / Motivation

Security audit (2026-04-20): no `golang.org/x/time/rate` usage in the repo. `POST /_matrix/client/v3/login`, `GET /_matrix/client/v3/login/sso/redirect`, `POST /admin/bootstrap`, and `GET /admin/login/start` accept unlimited requests per IP. `sso.globalSSOState` grows unauthenticated up to its 10-minute TTL expiry — memory DoS.

CLAUDE.md architecture already listed `rate limiting` as a planned middleware — this story delivers it.

---

## Acceptance Criteria

1. `gateway/internal/middleware/ratelimit.go` implements a per-IP token-bucket limiter using `golang.org/x/time/rate`:
   - Shared LRU of `*rate.Limiter` keyed by client IP
   - IP extracted via `realip.FromRequest(r, trustedProxy)` (respect `X-Forwarded-For` when `NEBU_TRUSTED_PROXY=true` — see Story 5.15)
   - LRU capacity 10 000 entries; evicted entries start fresh

2. Three rate-limit tiers applied via per-route wrapping:
   - **strict** (5 req/min, burst 3): `/_matrix/client/v3/login`, `/admin/login/start`, `/admin/callback`, `/admin/bootstrap`, `/admin/bootstrap/select-claim`
   - **medium** (30 req/min, burst 10): `/_matrix/client/v3/login/sso/redirect`, `/_matrix/client/v3/login/sso/callback`, `GET /_matrix/client/v3/profile/{userId}` (unauthenticated profile reads)
   - **loose** (300 req/min, burst 100): fallback for all remaining non-authenticated public endpoints

3. Rate-limit exceeded → 429 `M_LIMIT_EXCEEDED` with `Retry-After` header.

4. Authenticated Matrix endpoints (`/_matrix/client/v3/*` with valid JWT) are NOT rate-limited by this middleware — per-user limits are out of scope for this story.

5. `ssoStateStore` in `matrix/sso.go` caps total entries at 10 000 and rejects new entries with 429 when full (independent of the HTTP rate-limit, defense in depth).

6. Metrics: expose `nebu_rate_limit_total{tier="strict|medium|loose",decision="allow|deny"}` for ops observability.

7. Unit tests:
   - `TestRateLimit_StrictTier_BlocksAfter5`
   - `TestRateLimit_MediumTier_IndependentPerIP`
   - `TestRateLimit_RetryAfterHeader`
   - `TestSSOStateStore_CapacityCap`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestRateLimit_Login_429After5Requests` — 5 successful 200s, 6th → 429 with `Retry-After`

2. `TestRateLimit_DifferentIPs_NotShared` — requests from IP A do not affect bucket for IP B

3. `TestSSOStateStore_Rejects10001stEntry` — fill store to 10 000, next save → error returned to client as 429

---

## Implementation Notes

- Use `golang.org/x/time/rate` stdlib; no external dep
- LRU: `github.com/hashicorp/golang-lru/v2` (already a common Go dep — confirm via context7 if in doubt)
- Expose `NEBU_RATE_LIMIT_DISABLED=true` for local dev so E2E tests don't flake — default OFF, production must never set this
- Prometheus metrics via existing registry

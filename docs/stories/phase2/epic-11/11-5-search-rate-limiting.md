---
status: ready-for-dev
epic: 11
story: 5
security_review: required
matrix: true
ui: false
---

# Story 11.5: Search Rate-Limiting

Status: ready-for-dev

## Story

As a system operator,
I want full-text search to be rate-limited per authenticated user,
So that expensive PostgreSQL tsvector queries cannot be used to DoS the database.

**Size:** S

---

## Acceptance Criteria

**AC1 — 429 on 11th request within one minute:**
Given a user sends 10 search requests within a 60-second window,
When the 11th request arrives within the same window,
Then the server returns `429 M_LIMIT_EXCEEDED` with body `{"errcode":"M_LIMIT_EXCEEDED","error":"Search rate limit exceeded","retry_after_ms":60000}`

**AC2 — retry_after_ms present and positive:**
Given the rate limiter blocks a request,
When the 429 response is examined,
Then the JSON body contains `"retry_after_ms"` set to a positive integer (≥1000 ms)

**AC3 — per-user buckets are independent:**
Given user Alice and user Bob each send 10 requests per minute,
When their requests are processed concurrently,
Then neither user is blocked — their rate-limit buckets are independent (keyed by user_id, not by IP)

**AC4 — under-limit requests pass through:**
Given a user sends fewer than 10 requests per minute,
When each request is processed,
Then all requests return the normal search response (200 OK)

**AC5 — reuse existing RateLimitConfig / NewIPRateLimiter infrastructure:**
Given the rate limiter is implemented,
When the implementation is inspected,
Then it uses `middleware.NewUserRateLimiter` (a new per-user variant of the existing `NewIPRateLimiter`) with `middleware.RateLimitConfig{Rate: rate.Limit(10.0/60.0), Burst: 10}` — no new rate-limiter data structure from scratch

**AC6 — rate limiter applies AFTER jwtWithStatusCheck:**
Given the middleware chain for `POST /_matrix/client/v3/search` is inspected in main.go,
When the chain is read left-to-right,
Then the order is: `searchRL(bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))`
(rate limiter is outermost so unauthenticated requests are rejected by JWT middleware before consuming a rate-limit slot)

**AC7 — NEBU_RATE_LIMIT_DISABLED=true disables the limiter (dev/test):**
Given `NEBU_RATE_LIMIT_DISABLED=true` is set,
When more than 10 requests are sent in one minute,
Then all requests pass through (no-op middleware, consistent with existing IP limiter behavior)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**Location:** `gateway/internal/middleware/ratelimit_test.go` — add new test functions to the existing file.

**1. `TestUserRateLimit_BlocksAfter10` (AC1 + AC4)**
- Given: `NewUserRateLimiter` with `Rate: rate.Limit(10.0/60.0), Burst: 10`, tier `"search"`
- Given: request context contains `userID = "@alice:test.local"` (set via `context.WithValue(r.Context(), ContextKeyUserID, "@alice:test.local")`)
- When: 10 requests are sent with the same userID — all must return 200 OK
- When: the 11th request is sent with the same userID
- Then: HTTP 429
- Then: body contains `"errcode": "M_LIMIT_EXCEEDED"`
- Then: body contains `"retry_after_ms"` with a value > 0 (integer or float)
- Then: `Retry-After` header is present (seconds, ≥ 1)

**2. `TestUserRateLimit_DifferentUsers_IndependentBuckets` (AC3)**
- Given: `NewUserRateLimiter` with `Burst: 2`, tier `"search"`
- Given: userA = `"@alice:test.local"`, userB = `"@bob:test.local"`
- When: userA sends 2 requests (exhausts their bucket)
- When: userB sends 2 requests
- Then: all 4 requests return 200 OK (buckets are independent)

**3. `TestUserRateLimit_RetryAfterMs_InBody` (AC2)**
- Given: `NewUserRateLimiter` with `Burst: 1`
- Given: context has `userID = "@carol:test.local"`
- When: first request passes (200 OK)
- When: second request is blocked (429)
- Then: `json.Unmarshal` of the response body succeeds
- Then: `body["retry_after_ms"]` is present (type `float64` when decoded into `map[string]any`) and > 0

**4. `TestUserRateLimit_NoUserID_FallsBackToIP` (defense-in-depth)**
- Given: request context has NO `ContextKeyUserID` value (empty string or not set)
- When: requests arrive from the same IP
- Then: the limiter falls back to IP-based keying (same behavior as `NewIPRateLimiter`) — no panic, no bypass

**5. `TestUserRateLimit_Disabled_NoOp` (AC7)**
- Given: `NEBU_RATE_LIMIT_DISABLED=true` is set via `t.Setenv`
- Given: `NewUserRateLimiter` is called with `Burst: 1`
- When: 5 requests are sent from the same user
- Then: all 5 return 200 OK (no-op middleware)

**Test helpers available in `middleware` package tests:**
- `okHandler` — already defined in `ratelimit_test.go` (`http.HandlerFunc` returning 200)
- For user context injection: use `context.WithValue(r.Context(), middleware.ContextKeyUserID, userID)` — `ContextKeyUserID` is exported from `auth.go`

---

## Tasks / Subtasks

- [ ] Task 1: Write failing ATDD tests first (AC1–AC7)
  - [ ] Add `TestUserRateLimit_BlocksAfter10` to `gateway/internal/middleware/ratelimit_test.go`
  - [ ] Add `TestUserRateLimit_DifferentUsers_IndependentBuckets`
  - [ ] Add `TestUserRateLimit_RetryAfterMs_InBody`
  - [ ] Add `TestUserRateLimit_NoUserID_FallsBackToIP`
  - [ ] Add `TestUserRateLimit_Disabled_NoOp`
  - [ ] Verify tests FAIL (red phase — `NewUserRateLimiter` doesn't exist yet)

- [ ] Task 2: Implement `NewUserRateLimiter` in `gateway/internal/middleware/ratelimit.go` (AC1–AC7)
  - [ ] Add `NewUserRateLimiter(cfg RateLimitConfig, trustedProxy bool, tier string) func(http.Handler) http.Handler`
  - [ ] Key: `userID` from `r.Context().Value(ContextKeyUserID).(string)` — fall back to `extractClientIP(r, trustedProxy)` if empty
  - [ ] Return `retry_after_ms` (milliseconds) in the JSON body: `int(math.Ceil(delay.Seconds())) * 1000`
  - [ ] Keep `Retry-After` header (seconds) consistent with existing `writeTooManyRequests`
  - [ ] Re-use existing `lruCapacity`, `rateLimitTotal`, `writeTooManyRequests` helpers — do NOT redefine
  - [ ] `NEBU_RATE_LIMIT_DISABLED=true` → return no-op (same as `NewIPRateLimiter`)

- [ ] Task 3: Wire `searchRL` in `gateway/cmd/gateway/main.go` (AC6)
  - [ ] Add after the existing RL definitions (around line 223): `searchRL := middleware.NewUserRateLimiter(middleware.RateLimitConfig{Rate: rate.Limit(10.0 / 60.0), Burst: 10}, trustedProxy, "search")`
  - [ ] Update the `/search` route registration from: `bodyLimit1MiB(jwtWithStatusCheck(...))` to: `searchRL(bodyLimit1MiB(jwtWithStatusCheck(...)))`
  - [ ] Update comment above the route to mention Story 11.5

- [ ] Task 4: Run tests and verify green
  - [ ] `make test-unit-go` passes (all 5 new middleware tests green, no regressions)
  - [ ] `make build-gateway` succeeds

---

## Dev Notes

### Why a new `NewUserRateLimiter` instead of extending `NewIPRateLimiter`

The existing `NewIPRateLimiter` keys on the client IP address. For `POST /search`, which is always authenticated, per-user fairness is required: user A should not be blocked because user B is on the same corporate NAT IP. The new function is identical to `NewIPRateLimiter` except:

1. The cache key is `userID` (from `ContextKeyUserID` context value) instead of the extracted client IP.
2. The 429 response body includes `retry_after_ms` (milliseconds) in addition to the existing `Retry-After` header (seconds) — the Matrix CS API §11.14 expects `retry_after_ms` in the body.
3. Fallback to IP if `userID` is empty (defense-in-depth — JWT middleware should have rejected the request before this runs, but belt-and-suspenders).

### `NewUserRateLimiter` implementation sketch

```go
// NewUserRateLimiter returns a per-user token-bucket middleware.
// The rate-limit key is the Matrix user_id from the request context
// (set by JWTMiddleware as ContextKeyUserID). Falls back to client IP
// if the user_id is absent (e.g. middleware bypass in tests).
//
// The 429 body includes "retry_after_ms" (milliseconds) in addition to
// the Retry-After header (seconds) — required by Matrix CS API §11.14.
//
// Dev/test escape: NEBU_RATE_LIMIT_DISABLED=true → no-op (same as NewIPRateLimiter).
func NewUserRateLimiter(cfg RateLimitConfig, trustedProxy bool, tier string) func(http.Handler) http.Handler {
    if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
        return func(next http.Handler) http.Handler { return next }
    }

    cache, _ := lru.New(lruCapacity)
    var mu sync.Mutex

    getLimiter := func(key string) *rate.Limiter {
        mu.Lock()
        defer mu.Unlock()
        if val, ok := cache.Get(key); ok {
            return val.(*rate.Limiter)
        }
        lim := rate.NewLimiter(cfg.Rate, cfg.Burst)
        cache.Add(key, lim)
        return lim
    }

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Key on user_id; fall back to IP if missing.
            key, _ := r.Context().Value(ContextKeyUserID).(string)
            if key == "" {
                key = extractClientIP(r, trustedProxy)
            }
            lim := getLimiter(key)

            reservation := lim.Reserve()
            if !reservation.OK() {
                rateLimitTotal.WithLabelValues(tier, "deny").Inc()
                writeUserTooManyRequests(w, 1)
                return
            }

            delay := reservation.Delay()
            if delay > 0 {
                reservation.Cancel()
                retryAfterSeconds := int(math.Ceil(delay.Seconds()))
                if retryAfterSeconds < 1 {
                    retryAfterSeconds = 1
                }
                rateLimitTotal.WithLabelValues(tier, "deny").Inc()
                writeUserTooManyRequests(w, retryAfterSeconds)
                return
            }

            rateLimitTotal.WithLabelValues(tier, "allow").Inc()
            next.ServeHTTP(w, r)
        })
    }
}

// writeUserTooManyRequests writes a 429 Matrix error with Retry-After header
// AND retry_after_ms in the JSON body (required by Matrix CS API for search).
func writeUserTooManyRequests(w http.ResponseWriter, retryAfterSeconds int) {
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
    w.WriteHeader(http.StatusTooManyRequests)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "errcode":        "M_LIMIT_EXCEEDED",
        "error":          "Search rate limit exceeded",
        "retry_after_ms": retryAfterSeconds * 1000,
    })
}
```

### middleware chain order — CRITICAL

```
searchRL(bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))
```

Execution order (outermost first):
1. `searchRL` — rate limiter (outermost — reads userID from context set by JWT middleware… BUT JWT middleware is inner. See note below.)
2. `bodyLimit1MiB` — body size cap
3. `jwtWithStatusCheck` — sets `ContextKeyUserID` in context, rejects unauthenticated
4. `searchHandler.PostSearch` — handler

**IMPORTANT ORDERING NOTE:** Because `searchRL` is outermost, it runs BEFORE `jwtWithStatusCheck`. The `userID` context value is NOT yet set when `searchRL` executes. This means unauthenticated requests will fall back to IP-based keying in the rate limiter. Since `jwtWithStatusCheck` rejects them with 401 before `searchHandler.PostSearch` is called, this is safe:
- Unauthenticated requests: rate-limited by IP (correct behavior, prevents DoS on the JWT validator)
- Authenticated requests: `jwtWithStatusCheck` runs AFTER the rate limiter, so the context is NOT yet populated

**RESOLUTION:** To correctly key on `userID`, place `searchRL` INSIDE `jwtWithStatusCheck`:

```go
// Correct chain — searchRL inside JWT so userID is in context:
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(searchRL(http.HandlerFunc(searchHandler.PostSearch)))))
```

This is the correct ordering. The JWT middleware runs first, sets `ContextKeyUserID`, then `searchRL` keys on it. Unauthenticated requests never reach `searchRL` (rejected by JWT with 401 first).

**AC6 should specify this ordering.** The story text has been corrected accordingly:
- Correct chain: `bodyLimit1MiB(jwtWithStatusCheck(searchRL(http.HandlerFunc(searchHandler.PostSearch))))`

### Rate: 10 req/min, Burst: 10

- `Rate: rate.Limit(10.0 / 60.0)` — 10 requests per 60 seconds steady-state refill
- `Burst: 10` — allows up to 10 back-to-back requests before blocking; bucket starts full
- This means the 11th consecutive request (with no delay) is blocked — consistent with AC1

### `retry_after_ms` calculation

The existing `writeTooManyRequests` only writes `Retry-After` in seconds. The new `writeUserTooManyRequests` also writes `retry_after_ms` in the body. For the `Burst: 10, Rate: 10/60` config, the typical delay for the 11th request is ~6 seconds, so `retry_after_ms = 6000`. The 429 body from Story 11.4 (gRPC ResourceExhausted path) hardcodes `"retry_after_ms": 60000` — the new middleware path returns the actual wait duration converted to milliseconds, which is more accurate.

### What NOT to do

- Do NOT touch `NewIPRateLimiter` — it is used by existing routes (login, SSO, public endpoints). Only add `NewUserRateLimiter`.
- Do NOT create a new `writeTooManyRequests` — the existing one handles IP-based 429 responses. Add a separate `writeUserTooManyRequests` for the body format required by Matrix (with `retry_after_ms`).
- Do NOT add `retry_after_ms` to the existing `writeTooManyRequests` — that would change the response format for all existing rate-limited routes (REGRESSION).
- Do NOT register the route without `jwtWithStatusCheck` — search is always authenticated.
- Do NOT use `sync.Map` for the LRU cache — the existing `lru.New` + `sync.Mutex` pattern is established (avoids the >32-key test gap found in Epic 4 retro).
- Do NOT add Godog scenarios in this story — Story 11.6 handles E2E tests (including the rate-limit scenario).
- Do NOT remove the gRPC `ResourceExhausted → 429` mapping in `search.go` — that handles Core-side rate limiting. This story adds a Gateway-side HTTP middleware BEFORE the gRPC call.

---

## Files to Create / Modify

| File | Action | Notes |
|---|---|---|
| `gateway/internal/middleware/ratelimit.go` | MODIFY | Add `NewUserRateLimiter` + `writeUserTooManyRequests` |
| `gateway/internal/middleware/ratelimit_test.go` | MODIFY | Add 5 acceptance tests — written FIRST (red phase) |
| `gateway/cmd/gateway/main.go` | MODIFY | Add `searchRL`, update `/search` route chain |

No new files. No Elixir changes. No proto changes. No migrations. No Playwright tests (ui: false).

---

## Previous Story Intelligence (11.4)

Story 11.4 registered `POST /_matrix/client/v3/search` in main.go as:
```go
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))
```

Story 11.5 updates this to:
```go
// Story 11.5: searchRL wraps the JWT-authenticated handler so userID is in context.
// 10 req/min per user, burst 10. Keyed on user_id (not IP) for per-user fairness.
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(searchRL(http.HandlerFunc(searchHandler.PostSearch)))))
```

Story 11.4 already maps `codes.ResourceExhausted → 429 M_LIMIT_EXCEEDED` (with `retry_after_ms: 60000`). That path handles Core-side throttling (e.g. if Core itself limits concurrent search queries). This story adds the Gateway-side HTTP middleware that fires BEFORE the gRPC call ever happens.

---

## Architecture References

- [Source: gateway/internal/middleware/ratelimit.go] — `NewIPRateLimiter`, `RateLimitConfig`, `writeTooManyRequests`, `lruCapacity`, `rateLimitTotal` — all must be reused, not duplicated
- [Source: gateway/internal/middleware/ratelimit_test.go] — test structure for `TestRateLimit_*` — new tests follow same pattern
- [Source: gateway/internal/middleware/auth.go:142] — `ContextKeyUserID contextKey = "user_id"` — the context key for per-user keying
- [Source: gateway/cmd/gateway/main.go:210-223] — existing RL tier definitions (strictRL, complianceRL, adminRL, mediumRL, looseRL) — add `searchRL` in the same block
- [Source: gateway/cmd/gateway/main.go:726-733] — current Story 11.4 `/search` route registration — update chain to include `searchRL`
- [Source: docs/architecture/adr/ADR-010-fts-strategy.md] — FTS architecture context

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

## Change Log

| Date | Change |
|---|---|
| 2026-05-11 | Story created: ready-for-dev |

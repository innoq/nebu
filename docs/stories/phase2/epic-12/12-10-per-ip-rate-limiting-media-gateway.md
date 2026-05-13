---
id: "12-10"
epic: 12
title: "Per-IP Rate Limiting on Media Gateway"
status: ready-for-dev
security_review: required
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.10 — Per-IP Rate Limiting on Media Gateway

## User Story

As a system operator,
I want per-IP rate limiting on the media gateway's upload, download, and thumbnail endpoints,
So that a single client cannot exhaust server resources through concurrent or high-frequency requests.

## Acceptance Criteria

**AC-1** — Upload rate limit (10 req/s per IP):
- Given a single IP sends more than 10 upload requests per second
- When the 11th request arrives within the same second
- Then the gateway returns 429 with `Retry-After` header and `M_LIMIT_EXCEEDED` JSON body

**AC-2** — Download/thumbnail rate limit (100 req/s per IP):
- Given a single IP sends more than 100 download/thumbnail requests per second
- When the 101st request arrives within the same second
- Then the gateway returns 429 with `Retry-After` header and `M_LIMIT_EXCEEDED` JSON body

**AC-3** — Per-IP isolation:
- Given two different IPs each send 10 upload requests per second
- When both are within their individual limits
- Then both receive 200 responses (limits are per-IP, not global)

**AC-4** — Token bucket refill:
- Given a rate-limited IP waits 1 second
- When it sends a new request
- Then it receives 200 (token bucket refills)

## Technical Context

### Pattern to reuse: `gateway/internal/middleware/ratelimit.go`

The API gateway already implements per-IP token-bucket rate limiting using `golang.org/x/time/rate` and a `sync.Map` (actually LRU cache) keyed by IP. The media gateway MUST reuse the same pattern but implement it locally (the media module is a separate Go module with its own `go.mod`).

**Key differences from gateway implementation:**
- The gateway uses `github.com/hashicorp/golang-lru` for LRU eviction; the story spec calls for `sync.Map` + background cleanup goroutine (simpler, no extra dep needed for media).
- The story spec explicitly calls out a background cleanup goroutine for stale entries (IPs not seen for >5 minutes).
- The media gateway does NOT have Prometheus yet — no need to register metrics counters (keep it simple).
- Two tiers: upload (10 req/s, burst 5) and download/thumbnail (100 req/s, burst 20).

### Rate limits (from story spec)
| Endpoint | Rate | Burst |
|---|---|---|
| `POST /_matrix/media/v3/upload` | 10 req/s | 5 |
| `GET /_matrix/media/v3/download/…` | 100 req/s | 20 |
| `GET /_matrix/media/v3/thumbnail/…` | 100 req/s | 20 |

### IP extraction
Extract client IP from `X-Forwarded-For` header with fallback to `RemoteAddr` (same as gateway middleware `extractClientIP`).

For the media gateway: use a simple approach — take the first (leftmost) non-empty value from `X-Forwarded-For`, fall back to stripping port from `RemoteAddr`. This is consistent with the story spec which says "extract client IP from X-Forwarded-For with fallback to RemoteAddr".

### Response format on 429
```json
{"errcode": "M_LIMIT_EXCEEDED", "error": "Too many requests"}
```
Header: `Retry-After: 1`

Note: The gateway uses `"error": "Rate limit exceeded"` but the story spec explicitly requires `"error": "Too many requests"` for the media gateway.

### Implementation location
- **New file:** `media/internal/ratelimit/ratelimit.go` — contains the rate limiter middleware + IP extraction + cleanup goroutine
- **New file:** `media/internal/ratelimit/ratelimit_test.go` — unit tests (ATDD tests go here)
- **Modified:** `media/cmd/media/main.go` — wire the middleware, wrap the mux before `http.Server`
- **Modified:** `media/go.mod` + `media/go.sum` — add `golang.org/x/time` dependency

### Wiring in main.go
The middleware wraps the mux before passing to `http.Server`:

```go
uploadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{
    Rate:  10,   // req/s
    Burst: 5,
}, "upload")
downloadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{
    Rate:  100,  // req/s
    Burst: 20,
}, "download")

mux := http.NewServeMux()
mux.Handle("POST /_matrix/media/v3/upload", uploadRL(uploadHandler))
mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", downloadRL(downloadHandler))
mux.Handle("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", downloadRL(thumbnailHandler))
```

### sync.Map + cleanup goroutine pattern

The story requires `sync.Map` keyed by IP with cleanup of stale entries (>5 minutes). Pattern:

```go
type ipEntry struct {
    limiter  *rate.Limiter
    lastSeen time.Time
}

type IPRateLimiter struct {
    mu      sync.Map  // map[string]*ipEntry
    rate    rate.Limit
    burst   int
}
```

Background cleanup goroutine: starts in `NewIPRateLimiter`, runs every 1 minute, deletes entries where `lastSeen > 5 minutes ago`.

### Escape hatch
Support `NEBU_RATE_LIMIT_DISABLED=true` (same as gateway) for dev/test bypass.

### go.mod dependency
Add to `media/go.mod`:
```
golang.org/x/time v0.12.0
```
(check `gateway/go.mod` for the exact version in use)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-12-10-1** — Upload rate limit blocks 11th request — [Go unit test in `media/internal/ratelimit/ratelimit_test.go`]
   - Given: IPRateLimiter configured for upload (10 req/s, burst 10 for test clarity)
   - When: 11 requests from the same IP arrive immediately
   - Then: First 10 return 200, 11th returns 429 with `Retry-After` header and `{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}` body

2. **AT-12-10-2** — Download/thumbnail rate limit blocks 101st request — [Go unit test]
   - Given: IPRateLimiter configured for download (100 req/s, burst 100 for test)
   - When: 101 requests from the same IP arrive immediately
   - Then: First 100 return 200, 101st returns 429

3. **AT-12-10-3** — Per-IP isolation: two IPs do not share buckets — [Go unit test]
   - Given: IPRateLimiter with burst=5
   - When: IP A makes 5 requests AND IP B makes 5 requests
   - Then: All 10 requests return 200 (each IP has its own bucket)

4. **AT-12-10-4** — 429 response format: correct JSON body and Retry-After header — [Go unit test]
   - Given: Rate limiter with burst=1
   - When: IP exhausts burst (1 request), then makes 2nd request
   - Then: 429 response has `Content-Type: application/json`, `Retry-After` header with integer >= 1, body `{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}`

5. **AT-12-10-5** — NEBU_RATE_LIMIT_DISABLED=true makes middleware a no-op — [Go unit test]
   - Given: NEBU_RATE_LIMIT_DISABLED=true environment variable
   - When: IPRateLimiter is constructed and 20 requests are sent from same IP (burst=1)
   - Then: All 20 requests return 200 (limiter is disabled)

## Dev Notes

### golang.org/x/time version
Check `gateway/go.mod` for the exact `golang.org/x/time` version and use the same one in `media/go.mod` to stay consistent.

### sync.Map thread safety
`sync.Map` is safe for concurrent reads and writes. However, the Get+Store sequence for new IPs has a TOCTOU race (two goroutines for the same new IP can both call Store). Use `sync.Map.LoadOrStore` to atomically set the entry only if it doesn't exist.

### lastSeen update
Update `entry.lastSeen` on every request (under a mutex on the entry or using atomic). The simplest approach: use a `sync.Mutex` inside `ipEntry` to protect `lastSeen`.

### Test determinism
Use `rate.NewLimiter` directly in tests. Do NOT rely on real-time sleeps to verify token bucket refill in fast unit tests. For AC-4 (token refill), use `rate.Limiter.SetLimit` or just verify the mathematical property: burst=N allows exactly N back-to-back requests; a new request after waiting 1/rate seconds succeeds.

### Existing test pattern for main_test.go
`media/cmd/media/main_test.go` uses the subprocess pattern (os.Exec) for fatal-exit tests. New unit tests for rate limiting go in `media/internal/ratelimit/ratelimit_test.go`, not in `main_test.go`.

### No Prometheus in media gateway
The media module does NOT import `github.com/prometheus/client_golang`. Do not add it. The rate limiter in media should have no Prometheus instrumentation (YAGNI for this story).

### X-Forwarded-For extraction
The story spec says "same as gateway middleware". The gateway uses rightmost-minus-1 (spoofing-resistant). For the media gateway, use the same strategy: if `X-Forwarded-For` has 2+ comma-separated entries, take the last one (proxy-appended). If only 1 entry or no header, fall back to `RemoteAddr` (strip port). This is the same semantics as `extractClientIP(r, trustedProxy=true)` in the gateway.

## Files to Create / Modify

| Action | File | Notes |
|---|---|---|
| CREATE | `media/internal/ratelimit/ratelimit.go` | Rate limiter middleware, IP extraction, cleanup goroutine |
| CREATE | `media/internal/ratelimit/ratelimit_test.go` | Unit tests (AT-12-10-1 through AT-12-10-5) |
| MODIFY | `media/cmd/media/main.go` | Import and wire ratelimit middleware |
| MODIFY | `media/go.mod` | Add `golang.org/x/time` |
| MODIFY | `media/go.sum` | Updated by `go mod tidy` |

## Previous Story Intelligence

Story 12.9 (canonical Matrix user ID in media audit trail) established:
- `formatMatrixUserID` pattern in `upload.go`
- `NEBU_SERVER_NAME` is now mandatory (fatal exit on empty)
- All existing tests in `media/cmd/media/main_test.go` are green

Story 12.8 (OIDC fail-open hardening) established:
- `initOIDCVerifier` + `initOIDCVerifierWith` pattern for testable startup code
- Subprocess pattern for fatal-exit tests in `main_test.go`

Story 5.21 (per-IP rate limiting for gateway) established the exact LRU-based pattern in `gateway/internal/middleware/ratelimit.go` — use it as the reference implementation, but adapt for the media module (no LRU dep, use sync.Map + cleanup goroutine as spec'd).

## Definition of Done

- [ ] `media/internal/ratelimit/ratelimit.go` created with `NewIPRateLimiter`, `extractClientIP`, cleanup goroutine
- [ ] All AT-12-10-1 through AT-12-10-5 tests pass
- [ ] `media/cmd/media/main.go` wires the middleware for upload (10/s, burst 5) and download/thumbnail (100/s, burst 20)
- [ ] `make test-unit-go` passes (all media + gateway + matrix packages green)
- [ ] `NEBU_RATE_LIMIT_DISABLED=true` disables rate limiting (verified by AT-12-10-5)

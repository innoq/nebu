---
status: ready-for-dev
epic: 14
story: 2b
security_review: required
matrix: false
ui: false
---

# Story 14.2b: Gateway OIDC Directory Service + Cache + Rate Limit

Status: ready-for-dev

## Story

As an instance admin,
I want the Gateway to fetch the OIDC user directory from the configured endpoint with caching and rate limiting,
So that Admin UI user searches can include OIDC users without overloading the OIDC provider.

**Size:** S
**security_review:** required

---

## Acceptance Criteria

**AC1 — Directory service fetches and caches:**
Given `gateway/internal/admin/oidc_directory.go` is implemented,
When `oidc_directory_enabled: true` and the endpoint is reachable,
Then the service fetches the user list via HTTP Bearer auth, caches the result for 30 seconds, and enforces a rate limit of 5 requests per second per admin session.

**AC2 — Unreachable endpoint returns empty list:**
Given the OIDC endpoint is unreachable,
When the directory service is called,
Then it returns an empty list and logs a warning — no error is propagated to the caller.

**AC3 — Non-HTTPS endpoint rejected:**
Given the configured `oidc_directory_endpoint` does not use HTTPS,
When the service validates the configuration,
Then the endpoint is rejected with a validation error (CR-1 from security guide).

**AC4 — Unit tests for directory service pass:**
Given Go unit tests for the OIDC directory service,
When `make test-unit-go` runs,
Then the following test cases pass: cache hit (no second HTTP call), cache miss (HTTP call made), unreachable endpoint (empty list returned), rate limit enforcement.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. TestOIDCDirectoryService_CacheHit — httptest (Unit)**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: A running httptest.Server serving a valid user list JSON
- When: The directory service is called twice within the 30-second cache TTL
- Then: The httptest server receives exactly ONE HTTP request (cache hit on second call)

**2. TestOIDCDirectoryService_CacheMiss — httptest (Unit)**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: A running httptest.Server serving a valid user list JSON
- When: The directory service is called with a cold cache (first call, or cache expired)
- Then: The httptest server receives exactly one HTTP request and users are returned

**3. TestOIDCDirectoryService_UnreachableEndpoint — Unit**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: The configured endpoint is an unreachable URL
- When: The directory service is called
- Then: An empty list is returned (no panic), and a warning is logged

**4. TestOIDCDirectoryService_NonHTTPSEndpoint — Unit**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: The service is initialized with an HTTP (non-HTTPS) endpoint
- When: Validate is called (or FetchUsers is called)
- Then: A validation error is returned; no HTTP call is made

**5. TestOIDCDirectoryService_RateLimitEnforcement — Unit**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: A rate limiter configured at 5 req/s per session
- When: 6 requests from the same session ID arrive within 1 second
- Then: The 6th request is rate-limited (returns false from Allow() or equivalent)

**6. TestOIDCDirectoryService_BearerTokenNotLogged — Unit (inspection)**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: A bearer token configured for the OIDC directory service
- When: FetchUsers is called (including an error path)
- Then: The bearer token does NOT appear in any log output (CR-3 from security guide)

**7. TestOIDCDirectoryService_ResponseSizeLimit — Unit**
Location: `gateway/internal/admin/oidc_directory_test.go`
- Given: An httptest.Server returning more than 10 MB of data
- When: The directory service fetches from it
- Then: The response is truncated gracefully at 10 MB (no OOM, no panic); a warning is logged

---

## Dev Notes

### Implementation File
Create: `gateway/internal/admin/oidc_directory.go`

### Key Types
```go
// OIDCDirectoryService fetches the user list from the OIDC directory endpoint.
// Security requirements: HTTPS-only, no redirect following, bearer token masked in logs,
// 10 MB response cap, per-session rate limiting (5 req/s), 30-second cache, singleflight.
type OIDCDirectoryService struct { ... }

// OIDCDirectoryUser represents a single user entry from the OIDC directory response.
type OIDCDirectoryUser struct {
    Sub         string `json:"sub"`
    DisplayName string `json:"display_name"`
    Email       string `json:"email"`
}

// OIDCDirectoryConfig holds the configuration for the service.
type OIDCDirectoryConfig struct {
    Endpoint    string       // must be HTTPS
    BearerToken secretString // masked in logs
    Enabled     bool
}
```

### Security Requirements (from _bmad-output/implementation-artifacts/security-guide-oidc-directory-2026-05-16.md)
- **CR-1**: HTTPS-only — validate at init time + each call; fail hard (not warn)
- **CR-2**: No redirect following — `CheckRedirect: func(...) error { return http.ErrUseLastResponse }`
- **CR-3**: Bearer token masked in logs — use `type secretString string` with `String() string { return "[REDACTED]" }`
- **CR-4**: 10 MB response cap — `io.LimitReader(resp.Body, 10*1024*1024)`
- **CR-5**: Rate limit keyed on verified session ID from JWT (not IP, not header)
- **HR-1**: Admin-only access gate — functions only called from admin middleware-protected paths
- **HR-2**: Document SSRF trust boundary (Option B acceptable for Epic 14)
- **HR-3**: Claim values are untrusted strings — enforce length limits; do not inject raw into templates
- **MR-1**: Cache keyed on endpoint URL + bearer token hash
- **MR-2**: 10-second timeout on outbound call
- **MR-3**: Explicit HTTP response status handling (200 → parse; 401/403/404/429/5xx → empty list + warn)
- **MR-4**: singleflight.Group to collapse concurrent cache refreshes

### Rate Limiting
Use `golang.org/x/time/rate` — already in go.mod (check) or add if missing.
Rate limit: 5 req/s per session ID → `rate.NewLimiter(rate.Limit(5), 1)` per session key.
Store limiters in a `sync.Map` (session ID → *rate.Limiter).

### Cache
30-second TTL cache. Type: `struct { users []OIDCDirectoryUser; fetchedAt time.Time }`.
Cache key: SHA-256 hash of (endpoint URL + bearer token) → string.
singleflight key: same cache key string.

### Test Infrastructure
Tests use `httptest.NewServer` for fake OIDC provider.
Use `slog.New(slog.NewTextHandler(&buf, nil))` to capture log output and assert bearer token is absent.

### Dependencies
- `golang.org/x/time/rate` — rate limiting
- `golang.org/x/sync/singleflight` — concurrent cache refresh coalescing
- Standard library: `crypto/sha256`, `sync`, `net/http`, `io`, `log/slog`

### File Location
`gateway/internal/admin/oidc_directory.go` (same package as config.go, auth.go, etc.)
Test: `gateway/internal/admin/oidc_directory_test.go`

---

## Prerequisites / Dependencies

- Story 14.2a must be complete (oidc_directory_enabled + oidc_directory_endpoint in DB and served by GET/PATCH /api/v1/admin/config) ✅ Done

---

## Out of Scope

- Admin UI user search endpoint wiring (separate story)
- SCIM 2.0 integration (separate story)
- Private IP SSRF blocking (HR-2 Option B documented; Option A tracked as follow-up)

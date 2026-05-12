# Story 2.19: POST /_matrix/client/v3/logout — Session Invalidation

Status: done

## Story

As an end-user,
I want to log out and invalidate my session,
So that my access token can no longer be used after I explicitly log out.

## Acceptance Criteria

1. **Given** `POST /_matrix/client/v3/logout` with a valid `Authorization: Bearer <token>` header,
   **When** processed,
   **Then** the token is added to a short-lived server-side denylist (keyed by token hash, TTL = token expiry) and response is `200 OK` with `{}`

2. **Given** a denylisted token,
   **When** used in any subsequent Matrix API request,
   **Then** the middleware returns `401 Unauthorized` with `{"errcode": "M_UNKNOWN_TOKEN", "error": "Token has been logged out"}`

3. **Given** `POST /_matrix/client/v3/logout` with no `Authorization` header,
   **When** processed,
   **Then** response is `401 Unauthorized` with `{"errcode": "M_MISSING_TOKEN", "error": "Missing access token"}`

## Tasks / Subtasks

- [x] Task 1: Create in-memory Denylist (AC: 1, 2)
  - [x] 1.1 Create `gateway/internal/middleware/denylist.go` with `Denylist` struct (sync.Map + SHA-256 hash + TTL)
  - [x] 1.2 Implement `NewDenylist()`, `Add(rawToken string, expiresAt time.Time)`, `Contains(rawToken string) bool` (lazy expiry cleanup)
  - [x] 1.3 Create `gateway/internal/middleware/denylist_test.go` with unit tests
- [x] Task 2: Extend JWTMiddleware with denylist check (AC: 2, 3)
  - [x] 2.1 Add `ContextKeyTokenExpiry contextKey` to `auth.go`
  - [x] 2.2 Change `JWTMiddleware` signature to accept `denylist *Denylist` as 4th param (nil = disabled)
  - [x] 2.3 Add denylist check after token extraction, before OIDC verification: if `denylist != nil && denylist.Contains(rawToken)` → 401 `M_UNKNOWN_TOKEN` "Token has been logged out"
  - [x] 2.4 Put `idToken.Expiry` in context: `context.WithValue(ctx, ContextKeyTokenExpiry, idToken.Expiry)`
  - [x] 2.5 Update all `auth_test.go` calls to `JWTMiddleware(...)` — add `nil` as 4th argument (6 existing test functions)
  - [x] 2.6 Add `TestJWTMiddleware_DenylistedToken` test case in `auth_test.go`
- [x] Task 3: Create LogoutHandler (AC: 1, 3)
  - [x] 3.1 Create `gateway/internal/matrix/logout.go`
  - [x] 3.2 `LogoutHandler` struct with `denylist *middleware.Denylist`
  - [x] 3.3 `NewLogoutHandler(denylist *middleware.Denylist) *LogoutHandler`
  - [x] 3.4 `PostLogout(w, r)`: extract raw token from `Authorization` header, read expiry from `ContextKeyTokenExpiry`, call `denylist.Add(...)`, return `200 OK` with `{}`
  - [x] 3.5 Create `gateway/internal/matrix/logout_test.go` with unit tests
- [x] Task 4: Register route in main.go (AC: 1, 2, 3)
  - [x] 4.1 Add `denylist := middleware.NewDenylist()` in `main()`
  - [x] 4.2 Create `logoutHandler := matrix.NewLogoutHandler(denylist)`
  - [x] 4.3 Register: `mux.Handle("POST /_matrix/client/v3/logout", middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, denylist)(http.HandlerFunc(logoutHandler.PostLogout)))`
  - [x] 4.4 Confirm `make test-unit-go` passes

## Dev Notes

### Architecture — Key Design Decisions

**In-memory denylist, no Redis (ADR-002):** No Redis, no NATS per architecture decision. The denylist MUST be `sync.Map` keyed by `sha256(rawToken)` hex string with TTL = token expiry. This is intentionally in-memory — TTL is bounded by the OIDC token lifetime (typically ≤1h), so memory pressure is negligible.

**Horizontal scalability trade-off:** NFR-SC1 says "no session affinity". An in-memory denylist breaks strict statelessness — logged-out tokens are only denied on the same gateway instance. This is the accepted MVP trade-off; the AC explicitly specifies "short-lived server-side denylist" — do NOT attempt to use a shared store. The denylist is cleared on restart (acceptable since tokens are short-lived).

**NFR-S4 compliance:** Stateless JWT validation per request is preserved. The denylist is an additive check BEFORE OIDC verification — it does not create session state.

**Denylist check position in JWTMiddleware:** Check AFTER extracting `rawToken` from the Authorization header, BEFORE calling `verifier.Verify()`. Order: (1) missing token check → (2) denylist check → (3) OIDC provider nil check → (4) OIDC verify → (5) claims extraction.

**`ContextKeyTokenExpiry`:** JWTMiddleware puts `idToken.Expiry` (a `time.Time`) in context after successful verification. The LogoutHandler reads this to set the denylist TTL. This avoids a second `verifier.Verify()` call in the handler.

**No import cycle:** `matrix` package imports `middleware` (for `*Denylist`) — this is fine. `middleware` does NOT import `matrix`.

**AC 3 is handled automatically:** The logout route is behind JWTMiddleware. JWTMiddleware already returns `401 M_MISSING_TOKEN` for missing Authorization header. The handler never sees requests without a valid token.

### Task 1: Denylist Implementation

**File:** `gateway/internal/middleware/denylist.go`

```go
package middleware

import (
    "crypto/sha256"
    "fmt"
    "sync"
    "time"
)

type denylistEntry struct {
    expiresAt time.Time
}

// Denylist is a thread-safe in-memory token denylist keyed by SHA-256 hash.
// Entries expire lazily when Contains is called.
type Denylist struct {
    entries sync.Map
}

func NewDenylist() *Denylist {
    return &Denylist{}
}

// Add registers a token hash with the given expiry. rawToken is hashed before storage.
func (d *Denylist) Add(rawToken string, expiresAt time.Time) {
    d.entries.Store(tokenHash(rawToken), denylistEntry{expiresAt: expiresAt})
}

// Contains returns true if the token is denylisted and not yet expired.
// Expired entries are removed lazily.
func (d *Denylist) Contains(rawToken string) bool {
    hash := tokenHash(rawToken)
    val, ok := d.entries.Load(hash)
    if !ok {
        return false
    }
    entry := val.(denylistEntry)
    if time.Now().After(entry.expiresAt) {
        d.entries.Delete(hash)
        return false
    }
    return true
}

func tokenHash(rawToken string) string {
    h := sha256.Sum256([]byte(rawToken))
    return fmt.Sprintf("%x", h)
}
```

### Task 2: JWTMiddleware Modification

**File:** `gateway/internal/middleware/auth.go`

**2.1 New context key** (add to const block):
```go
ContextKeyTokenExpiry contextKey = "token_expiry"
```

**2.2 New signature:**
```go
func JWTMiddleware(provider *auth.Provider, clientID string, claimName string, denylist *Denylist) func(http.Handler) http.Handler {
```

**2.3 Denylist check** (insert between token extraction and provider nil check):
```go
rawToken := strings.TrimPrefix(authHeader, "Bearer ")

// Denylist check: token was explicitly logged out
if denylist != nil && denylist.Contains(rawToken) {
    writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token has been logged out")
    return
}
```

**2.4 Store expiry in context** (add after successful `verifier.Verify()`):
```go
idToken, err := verifier.Verify(r.Context(), rawToken)
if err != nil {
    // ... existing error handling unchanged ...
}

// Store token expiry for downstream handlers (e.g. LogoutHandler)
ctx = context.WithValue(ctx, ContextKeyTokenExpiry, idToken.Expiry)
```

**2.5 Test call update** — ALL 6 existing `JWTMiddleware(...)` calls in `auth_test.go` must add `nil`:
```go
// Before:
middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role")
// After:
middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)
```

The 6 test functions that need updating:
- `TestJWTMiddleware_MissingToken` (line 89)
- `TestJWTMiddleware_ValidToken` (line 117)
- `TestJWTMiddleware_ExpiredToken` (line 156)
- `TestJWTMiddleware_InvalidSignature` (line 193)
- `TestJWTMiddleware_NilProvider` (line 223)
- `TestJWTMiddleware_CustomClaimName` (line 275)
- `TestJWTMiddleware_SystemRoleInContext` (line 301)

**2.6 New test: `TestJWTMiddleware_DenylistedToken`** (add to `auth_test.go`):
```go
func TestJWTMiddleware_DenylistedToken(t *testing.T) {
    srv, key := setupOIDCServer(t)
    provider := auth.NewProvider(context.Background(), srv.URL)
    rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

    denylist := middleware.NewDenylist()
    denylist.Add(rawToken, time.Now().Add(time.Hour))

    handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", denylist)(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            t.Error("handler should not be called for denylisted token")
            w.WriteHeader(http.StatusOK)
        }),
    )

    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer "+rawToken)
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusUnauthorized {
        t.Errorf("expected 401, got %d", rr.Code)
    }
    var body map[string]string
    if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
        t.Fatalf("decode body: %v", err)
    }
    if body["errcode"] != "M_UNKNOWN_TOKEN" {
        t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
    }
    if body["error"] != "Token has been logged out" {
        t.Errorf("expected 'Token has been logged out', got %q", body["error"])
    }
}
```

### Task 3: LogoutHandler

**File:** `gateway/internal/matrix/logout.go`

```go
package matrix

import (
    "encoding/json"
    "net/http"
    "strings"
    "time"

    "github.com/nebu/nebu/internal/middleware"
)

type LogoutHandler struct {
    denylist *middleware.Denylist
}

func NewLogoutHandler(denylist *middleware.Denylist) *LogoutHandler {
    return &LogoutHandler{denylist: denylist}
}

func (h *LogoutHandler) PostLogout(w http.ResponseWriter, r *http.Request) {
    rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    expiry, _ := r.Context().Value(middleware.ContextKeyTokenExpiry).(time.Time)
    h.denylist.Add(rawToken, expiry)
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(struct{}{})
}
```

**Note:** No error handling needed. By the time this handler is called, JWTMiddleware has already:
1. Confirmed the Authorization header exists (no empty rawToken)
2. Confirmed the token is valid and not denylisted
3. Put `idToken.Expiry` in context (safe zero-value fallback: zero `time.Time` → denylist entry expires immediately, which is safe)

### Task 3 Tests

**File:** `gateway/internal/matrix/logout_test.go`

Pattern: mock denylist behavior, use `httptest` (same pattern as `login_test.go`).

**Test cases:**
| Test | Setup | Expected |
|------|-------|----------|
| `TestPostLogout_ValidToken` | Valid token header, expiry in context | 200 `{}`, token in denylist |
| `TestPostLogout_AddsCorrectExpiry` | Expiry set in context | Denylist entry has matching expiry |
| `TestPostLogout_EmptyBody` | Valid token header | 200, response body is `{}` (not `null`, not empty string) |

**Denylist check in test:** After calling `PostLogout`, call `denylist.Contains(rawToken)` to verify the token was added.

**Context setup in test:**
```go
ctx := context.WithValue(r.Context(), middleware.ContextKeyTokenExpiry, time.Now().Add(time.Hour))
req = req.WithContext(ctx)
```

**Response body assertion:** The handler returns `json.Encode(struct{}{})` which produces `{}`. Assert the body equals `{}\n` (json.Encoder appends `\n`).

### Task 4: Route Registration

**File:** `gateway/cmd/gateway/main.go`

Add after the existing login handler setup (around line 130):
```go
denylist := middleware.NewDenylist()
logoutHandler := matrix.NewLogoutHandler(denylist)

jwtMiddleware := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, denylist)
mux.Handle("POST /_matrix/client/v3/logout", jwtMiddleware(http.HandlerFunc(logoutHandler.PostLogout)))
```

**CRITICAL NOTES for main.go:**
1. `denylist` is shared between `JWTMiddleware` (checks it) and `LogoutHandler` (writes to it) — same pointer
2. The `jwtMiddleware` variable is a local variable wrapping the middleware function — this is a convenience pattern for readability
3. Future Matrix API routes that require authentication will also use `jwtMiddleware(http.HandlerFunc(handler.Method))`
4. The `loginHandler.PostLogin` route remains unchanged — it is NOT behind JWTMiddleware (token arrives in body, not header)

### Project Structure Notes

| File | Action | Reason |
|------|--------|--------|
| `gateway/internal/middleware/denylist.go` | CREATE | In-memory token denylist (sync.Map + SHA-256 + TTL) |
| `gateway/internal/middleware/denylist_test.go` | CREATE | Unit tests for Denylist.Add/Contains/expiry |
| `gateway/internal/middleware/auth.go` | MODIFY | Add denylist param, ContextKeyTokenExpiry, denylist check |
| `gateway/internal/middleware/auth_test.go` | MODIFY | Add `nil` to 7 JWTMiddleware calls + new denylist test |
| `gateway/internal/matrix/logout.go` | CREATE | LogoutHandler with PostLogout method |
| `gateway/internal/matrix/logout_test.go` | CREATE | Unit tests for PostLogout |
| `gateway/cmd/gateway/main.go` | MODIFY | Create denylist, create logoutHandler, register route behind JWTMiddleware |

**No new external dependencies. No go.mod changes. No Elixir changes. No DB migrations. No proto changes.**

### Denylist Unit Tests (`denylist_test.go`)

```go
func TestDenylist_ContainsMissingToken(t *testing.T) { /* false for unknown token */ }
func TestDenylist_AddAndContains(t *testing.T) { /* true after Add */ }
func TestDenylist_ExpiredEntry(t *testing.T) { /* false and cleaned up after expiry */ }
func TestDenylist_TwoDistinctTokens(t *testing.T) { /* adding token A doesn't deny token B */ }
func TestDenylist_RepeatedAdd(t *testing.T) { /* idempotent — repeated Add doesn't error */ }
```

### Previous Story Intelligence

**From Story 2-18 (POST /login — immediate predecessor):**
- `writeMatrixError` is already defined in `matrix/login.go` (unexported in that package) — NOT needed in `logout.go`; logout handler only returns success
- `LoginHandler` in `login.go` uses `go-oidc` verifier — LogoutHandler does NOT verify (middleware already did it)
- Test pattern: `httptest.NewRequest` + `httptest.NewRecorder` — use same pattern for `logout_test.go`
- Route NOT behind JWTMiddleware for login (user isn't authenticated yet) — OPPOSITE for logout: MUST be behind JWTMiddleware
- Matrix error format: `{"errcode": "...", "error": "..."}` — logout handler never needs to return errors (JWTMiddleware handles them all)

**From Story 2-4 (JWT Middleware — original middleware):**
- `middleware/auth.go` has existing `writeMatrixError` helper (same name as in matrix package — intentional duplication, different packages)
- Context keys use `contextKey` type (not exported string) — new `ContextKeyTokenExpiry` must use same type
- Tests use `context.Background()` + `auth.NewProvider()` + mock OIDC server pattern

**From Story 2-2 (Sessions Schema):**
- `sessions` table exists with `device_id`, `since_token` etc. — but logout does NOT write to sessions table in this story
- Session DB invalidation (`invalidate_session`) is deferred to Story 4.6 (Elixir side)
- This story only implements the gateway-side denylist — no DB writes, no gRPC calls

### Architecture Enforcement Rules

1. **ADR-002:** No Redis, no NATS → denylist MUST be `sync.Map` (in-memory) ✓
2. **NFR-S4:** Stateless JWT validation → denylist check is additive, not a replacement for OIDC validation ✓
3. **Rule: Consumer-defined interfaces** → LogoutHandler only needs `*middleware.Denylist` directly (not an interface; single implementation) ✓
4. **Rule: Context as first param** → all methods pass `context.Context` via `r.Context()` ✓
5. **Rule: Table-driven tests** → use table-driven tests for denylist_test.go where multiple code paths exist ✓

### NOT In Scope

- Session DB invalidation (Elixir `invalidate_session/1`) → Story 4.6
- `POST /_matrix/client/v3/logout/all` (logout all devices) → not in MVP
- Cluster-wide denylist synchronization → Phase 2 (NFR-SC2)
- Deactivated user check on logout → not in AC
- `device_id` tracking on logout → Epic 4

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.19 — Session Invalidation acceptance criteria]
- [Source: _bmad-output/planning-artifacts/architecture.md — ADR-002: No Redis, no NATS]
- [Source: _bmad-output/planning-artifacts/architecture.md — NFR-S4: stateless JWT validation]
- [Source: _bmad-output/planning-artifacts/architecture.md — NFR-SC1: horizontal scalability without session affinity]
- [Source: gateway/internal/middleware/auth.go — JWTMiddleware full implementation to extend]
- [Source: gateway/internal/middleware/auth_test.go — OIDC test server setup pattern + existing test signatures]
- [Source: gateway/internal/matrix/login.go — LogoutHandler struct pattern reference]
- [Source: gateway/cmd/gateway/main.go:130-139 — LoginHandler setup and route registration pattern]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- Implemented thread-safe in-memory denylist using `sync.Map` + SHA-256 token hash + lazy TTL expiry (ADR-002 compliant, no Redis)
- Extended `JWTMiddleware` with optional 4th `*Denylist` param; denylist check runs AFTER header extraction, BEFORE OIDC verification
- Added `ContextKeyTokenExpiry` to pass `idToken.Expiry` downstream; `LogoutHandler` reads this to set denylist TTL
- Route `POST /_matrix/client/v3/logout` is behind `JWTMiddleware` — AC 3 (missing token → 401) handled automatically by middleware
- All 7 existing `JWTMiddleware(...)` calls in `auth_test.go` updated to pass `nil` as 4th arg; `TestJWTMiddleware_DenylistedToken` added
- `make test-unit-go` passes all packages with `-race` flag

### File List

- `gateway/internal/middleware/denylist.go` (created)
- `gateway/internal/middleware/denylist_test.go` (created)
- `gateway/internal/middleware/auth.go` (modified)
- `gateway/internal/middleware/auth_test.go` (modified)
- `gateway/internal/matrix/logout.go` (created)
- `gateway/internal/matrix/logout_test.go` (created)
- `gateway/cmd/gateway/main.go` (modified)

## Change Log

- 2026-03-30: Story implemented — in-memory denylist, JWTMiddleware extended with denylist check + ContextKeyTokenExpiry, LogoutHandler, route registered; all unit tests pass

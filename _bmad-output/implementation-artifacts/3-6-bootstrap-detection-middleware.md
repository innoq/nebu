# Story 3.6: Bootstrap Detection Middleware

Status: done

## Story

As a gateway,
I want to detect whether the server is in Bootstrap Mode on every admin request,
so that unauthenticated operators are redirected to the Bootstrap Wizard and authenticated operators see the full Admin UI.

## Acceptance Criteria

1. `gateway/internal/admin/middleware.go` contains a `BootstrapGuard` middleware function
2. `BootstrapGuard` queries `server_config` via the existing DB connection: `SELECT value FROM server_config WHERE key = 'bootstrap_completed'`
3. If `bootstrap_completed` is not set (or `false`) AND the request path is not `/admin/bootstrap*`, redirect `302` to `/admin/bootstrap`
4. If `bootstrap_completed = true` AND the request path is `/admin/bootstrap*`, redirect `302` to `/admin/login`
5. The middleware is registered on the `/admin/*` route group before any page handler in `gateway/cmd/gateway/main.go`
6. A unit test with a mock DB covers: (a) bootstrap not complete → redirect to `/admin/bootstrap`, (b) bootstrap complete, accessing bootstrap URL → redirect to `/admin/login`, (c) bootstrap complete, accessing dashboard → no redirect (next handler called)

## Tasks / Subtasks

- [x] Task 1: Create `gateway/internal/admin/middleware.go` with `BootstrapGuard` (AC: 1, 2, 3, 4)
  - [x] 1.1 Define `BootstrapGuard(checker BootstrapStatusChecker) func(http.Handler) http.Handler`
  - [x] 1.2 Query `server_config` key `bootstrap_completed` (using `checker.IsBootstrapActive(ctx)`)
  - [x] 1.3 On DB error: return `500 Internal Server Error`, do NOT continue to next handler
  - [x] 1.4 Bootstrap not complete + path is NOT `/admin/bootstrap*` → `302` redirect to `/admin/bootstrap`
  - [x] 1.5 Bootstrap complete + path IS `/admin/bootstrap*` → `302` redirect to `/admin/login`
  - [x] 1.6 All other cases → call `next.ServeHTTP(w, r)` (pass-through)

- [x] Task 2: Register `BootstrapGuard` in `main.go` (AC: 5)
  - [x] 2.1 Create a `bootstrapGuardMiddleware` wrapping all `/admin/*` routes
  - [x] 2.2 Use the existing `bootstrapDB` + `PostgresBootstrapChecker` that are already initialized for `BootstrapHandler`
  - [x] 2.3 Apply the middleware BEFORE registering any admin page handler (auth, callback, bootstrap page, static assets)
  - [x] 2.4 Static assets (`/admin/static/*`) and auth endpoints (`/admin/auth/*`) must be EXCLUDED from the guard to avoid redirect loops — see critical note below

- [x] Task 3: Write unit tests (AC: 6)
  - [x] 3.1 In `gateway/internal/admin/middleware_test.go`, implement `TestBootstrapGuard`
  - [x] 3.2 Test case (a): bootstrap not complete + path `/admin/dashboard` → 302 redirect to `/admin/bootstrap`
  - [x] 3.3 Test case (b): bootstrap not complete + path `/admin/bootstrap` → no redirect (pass-through)
  - [x] 3.4 Test case (c): bootstrap complete + path `/admin/bootstrap` → 302 redirect to `/admin/login`
  - [x] 3.5 Test case (d): bootstrap complete + path `/admin/dashboard` → no redirect (pass-through)
  - [x] 3.6 Test case (e): DB error → 500 Internal Server Error
  - [x] 3.7 Use `fakeBootstrapChecker` from `bootstrap_test.go` as model (or define it in `middleware_test.go`)
  - [x] 3.8 Run `make test-unit-go` → all tests green, zero regressions

## Dev Notes

### CRITICAL: `BootstrapStatusChecker` Interface Already Exists

`gateway/internal/admin/bootstrap.go` already defines:
```go
type BootstrapStatusChecker interface {
    IsBootstrapActive(ctx context.Context) (bool, error)
}
```
And `PostgresBootstrapChecker` already implements it. **Do NOT redefine this interface.** `BootstrapGuard` must accept `BootstrapStatusChecker` — same type, same package.

The `IsBootstrapActive` method already queries `server_config` correctly:
- `bootstrap_completed` present → returns `false` (bootstrap done)
- `bootstrap_active` present (no `bootstrap_completed`) → returns `true`
- Neither flag + no users → returns `true`
- Neither flag + users exist → returns `false`

**Do NOT change `bootstrap.go`.** The existing logic covers all cases.

### CRITICAL: `BootstrapGuard` Lives in the `admin` Package

Create `gateway/internal/admin/middleware.go` (package `admin`). The middleware is internal to admin — it uses the same `BootstrapStatusChecker` interface already defined in the same package. Placing it in `gateway/internal/middleware/` would create a circular import (that package has no DB dependency).

### CRITICAL: Path Exclusions to Prevent Redirect Loops

The guard MUST NOT apply to:
- `/admin/static/*` — CSS and font assets needed to render the bootstrap page itself
- `/admin/auth/*` — OIDC login/callback (these are post-bootstrap; guard applies only after bootstrap completes)

Two strategies for registration in `main.go`:

**Strategy A (recommended): Apply guard selectively per route group**

Wrap only the page routes that need guarding, not every single `HandleFunc`:
```go
// Define the BootstrapGuard middleware
guard := admin.BootstrapGuard(admin.NewPostgresBootstrapChecker(bootstrapDB))

// Static assets — NO guard (avoid breaking page styles)
mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)
mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)

// Auth endpoints — NO guard (post-bootstrap flow)
mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)
mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)

// Bootstrap page — guard checks bootstrap state (allow through when incomplete)
mux.Handle("GET /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.Handler)))

// Future page handlers added in 3.7+ — wrap with guard:
// mux.Handle("GET /admin/dashboard", guard(http.HandlerFunc(dashboardHandler.Handler)))
```

**Strategy B: Implement path-prefix exclusion inside `BootstrapGuard` itself**

`BootstrapGuard` checks `strings.HasPrefix(r.URL.Path, "/admin/static/")` and `strings.HasPrefix(r.URL.Path, "/admin/auth/")` → skip guard if true. This keeps main.go cleaner but couples the middleware to specific path prefixes.

**Use Strategy A** — it is more explicit and aligns with the existing registration style in `main.go` (each handler registered individually with `mux.Handle`).

### Middleware Signature Pattern

Follow the established pattern from `gateway/internal/middleware/psk.go` and `auth.go`:
```go
func BootstrapGuard(checker BootstrapStatusChecker) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // logic here
            next.ServeHTTP(w, r)
        })
    }
}
```

### Bootstrap Redirect Logic

```go
active, err := checker.IsBootstrapActive(r.Context())
if err != nil {
    slog.Error("bootstrap guard: status check failed", "err", err)
    http.Error(w, "Internal Server Error", http.StatusInternalServerError)
    return
}

isBootstrapPath := strings.HasPrefix(r.URL.Path, "/admin/bootstrap")

if active && !isBootstrapPath {
    // Not yet bootstrapped, send to wizard
    http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
    return
}
if !active && isBootstrapPath {
    // Already bootstrapped, redirect away from wizard
    http.Redirect(w, r, "/admin/login", http.StatusFound)
    return
}

next.ServeHTTP(w, r)
```

Note: `/admin/login` does not yet exist as a full page handler (that is Story 3.9). Using `"/admin/login"` as redirect target is correct per the AC — the handler will be added later. The middleware issues the redirect; the 404 is a known temporary state until Story 3.9 is complete.

### `server_config` Table Schema

```sql
CREATE TABLE server_config (
    key    TEXT PRIMARY KEY,
    value  TEXT NOT NULL,
    set_at BIGINT NOT NULL
);
-- RLS: INSERT-only (UPDATE and DELETE denied)
-- config_read_all policy: SELECT allowed
```

The table uses append-only RLS (Story 1-5). `bootstrap_completed` is inserted (never updated) when bootstrap completes (Story 3.8). The `IsBootstrapActive` method reads this correctly.

### Unit Test Pattern

The `fakeBootstrapChecker` from `bootstrap_test.go` can be copy-referenced or re-declared in `middleware_test.go`. Since both are in `package admin`, a single declaration per file is needed — **do not duplicate** if both test files are in the same package. Either:
- Declare `fakeBootstrapChecker` in `bootstrap_test.go` only (already there) — it is accessible from `middleware_test.go` since both are `package admin`
- Or declare it in a shared `testhelpers_test.go` if you want to be explicit

```go
// middleware_test.go
package admin

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestBootstrapGuard(t *testing.T) {
    tests := []struct {
        name         string
        bootstrapActive bool
        checkerErr   error
        path         string
        wantCode     int
        wantLocation string
        wantNext     bool
    }{
        {"incomplete + non-bootstrap path → redirect to bootstrap", true, nil, "/admin/dashboard", http.StatusFound, "/admin/bootstrap", false},
        {"incomplete + bootstrap path → pass-through", true, nil, "/admin/bootstrap", http.StatusOK, "", true},
        {"complete + bootstrap path → redirect to login", false, nil, "/admin/bootstrap", http.StatusFound, "/admin/login", false},
        {"complete + non-bootstrap path → pass-through", false, nil, "/admin/dashboard", http.StatusOK, "", true},
        {"checker error → 500", false, errFakeDB, "/admin/dashboard", http.StatusInternalServerError, "", false},
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            checker := &fakeBootstrapChecker{active: tc.bootstrapActive, err: tc.checkerErr}
            nextCalled := false
            next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                nextCalled = true
                w.WriteHeader(http.StatusOK)
            })
            handler := BootstrapGuard(checker)(next)

            req := httptest.NewRequest("GET", tc.path, nil)
            rr := httptest.NewRecorder()
            handler.ServeHTTP(rr, req)

            if rr.Code != tc.wantCode {
                t.Errorf("status: got %d, want %d", rr.Code, tc.wantCode)
            }
            if tc.wantLocation != "" {
                loc := rr.Header().Get("Location")
                if loc != tc.wantLocation {
                    t.Errorf("Location: got %q, want %q", loc, tc.wantLocation)
                }
            }
            if tc.wantNext != nextCalled {
                t.Errorf("next called: got %v, want %v", nextCalled, tc.wantNext)
            }
        })
    }
}
```

Note: `fakeBootstrapChecker` and `errFakeDB` are declared in `bootstrap_test.go` (package `admin`) — they are accessible here without redeclaration.

### Integration in `main.go` — Minimal Change

`main.go` already has:
```go
bootstrapDB, err := sql.Open("pgx", cfg.DBURL)
// ...
bootstrapHandler := admin.NewBootstrapHandler(admin.NewPostgresBootstrapChecker(bootstrapDB))
mux.HandleFunc("GET /admin/bootstrap", bootstrapHandler.Handler)
```

The `PostgresBootstrapChecker` is already created. Reuse it for the guard:
```go
checker := admin.NewPostgresBootstrapChecker(bootstrapDB)
bootstrapHandler := admin.NewBootstrapHandler(checker)
guard := admin.BootstrapGuard(checker)

// Static assets — no guard
mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)
mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)

// Auth — no guard
mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)
mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)

// Bootstrap page — guarded
mux.Handle("GET /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.Handler)))
```

This replaces the existing `mux.HandleFunc("GET /admin/bootstrap", bootstrapHandler.Handler)` registration. Existing behavior is preserved because `BootstrapGuard` with a complete-bootstrap state passes through to the handler normally.

**The current `BootstrapHandler.Handler` returns JSON** (from Story 2.16). That JSON endpoint is the existing contract. Wrapping it with `BootstrapGuard` does not change its behavior when guard passes through — it still returns JSON. The full HTML bootstrap page is Story 3.7.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/middleware.go` | CREATE — new file with `BootstrapGuard` function |
| `gateway/internal/admin/middleware_test.go` | CREATE — unit tests for `BootstrapGuard` |
| `gateway/cmd/gateway/main.go` | MODIFY — replace bare `HandleFunc` with guarded `Handle` for `/admin/bootstrap`; reuse existing `PostgresBootstrapChecker` |

**Do NOT modify:**
- `gateway/internal/admin/bootstrap.go` — interface and checker unchanged
- `gateway/internal/admin/bootstrap_test.go` — existing tests unchanged
- `gateway/internal/admin/handler.go` — `TemplateHandler` unchanged
- `gateway/internal/admin/page_data.go` — `PageData` struct unchanged
- `gateway/internal/admin/static.go` — font/CSS serving unchanged
- `gateway/internal/middleware/` — this middleware belongs in `admin` package, not the generic middleware package

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.6] Authoritative ACs (lines 1571–1587)
- [Source: gateway/internal/admin/bootstrap.go] `BootstrapStatusChecker` interface + `PostgresBootstrapChecker` + `IsBootstrapActive` logic
- [Source: gateway/internal/admin/bootstrap_test.go] `fakeBootstrapChecker` + `errFakeDB` test helpers (reusable in `middleware_test.go`)
- [Source: gateway/internal/middleware/psk.go] `func(http.Handler) http.Handler` curried middleware pattern
- [Source: gateway/internal/middleware/auth.go:59] `JWTMiddleware` signature — same pattern to follow
- [Source: gateway/cmd/gateway/main.go:127-128] Current bootstrap handler registration (to be replaced)
- [Source: gateway/migrations/000003_server_config.up.sql] `server_config` table schema: INSERT-only RLS
- [Source: _bmad-output/planning-artifacts/architecture.md#line-929] `gateway/internal/admin/bootstrap.go` maps to FR5-6
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.7] Story 3.7 (`GET /admin/bootstrap` HTML page) depends on this middleware being in place

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

_No issues encountered._

### Completion Notes List

- Task 1: Created `gateway/internal/admin/middleware.go` with `BootstrapGuard` function. Uses the existing `BootstrapStatusChecker` interface from `bootstrap.go`. Implements all four logic branches: active+non-bootstrap-path → 302 to /admin/bootstrap; complete+bootstrap-path → 302 to /admin/login; DB error → 500; all other cases → pass-through.
- Task 2: Modified `gateway/cmd/gateway/main.go` to reuse a single `PostgresBootstrapChecker` instance for both `BootstrapHandler` and `BootstrapGuard`. Static assets (`/admin/static/*`) and auth endpoints (`/admin/auth/*`) registered without guard (Strategy A). Bootstrap page wrapped with guard via `mux.Handle`.
- Task 3: Created `gateway/internal/admin/middleware_test.go` with `TestBootstrapGuard` covering all 5 test cases (a-e). Reuses `fakeBootstrapChecker` and `errFakeDB` from `bootstrap_test.go` (same package). All tests pass, zero regressions (`make test-unit-go` green).

### File List

- `gateway/internal/admin/middleware.go` (CREATED)
- `gateway/internal/admin/middleware_test.go` (CREATED)
- `gateway/cmd/gateway/main.go` (MODIFIED)

## Change Log

- 2026-03-31: Story 3-6 implemented. Created BootstrapGuard middleware, unit tests (5 cases), and updated main.go registration (Strategy A). All tests pass.
- 2026-03-31: Code review passed. All 6 ACs verified, 5 unit tests green, no regressions. Clean implementation following established middleware patterns (PSKMiddleware, JWTMiddleware). Strategy A correctly applied. No MAJOR or MINOR issues found.

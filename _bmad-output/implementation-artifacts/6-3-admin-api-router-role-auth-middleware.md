---
security_review: required
---

# Story 6.3: Admin API Router + Role-Auth Middleware

Status: ready-for-dev

## Story

As a gateway developer,
I want the Admin API routes to be registered via the oapi-codegen `StrictHandler` and protected by a role-checking middleware,
so that every route is automatically wired to its handler and access is restricted by role without per-handler boilerplate.

## Acceptance Criteria

1. `gateway/internal/api/server.go` already defines `AdminServer` — it continues to exist unchanged; all operations still return `501 Not Implemented` (unimplemented stubs remain; this story only wires routing, not real handler logic)

2. `gateway/internal/api/middleware.go` contains `RequireRole(role string) func(http.Handler) http.Handler`:
   - Reads `ContextKeySystemRole` from the request context (populated by the existing `JWTMiddleware` in `gateway/internal/middleware/auth.go`)
   - If `systemRole != role` → writes `403` with JSON body `{"errcode":"M_FORBIDDEN","error":"<role> role required"}` and returns (do NOT call `next`)
   - If missing token (context value is empty string or absent) → writes `401` with `{"errcode":"M_MISSING_TOKEN","error":"Missing access token"}` and returns
   - If role matches → calls `next.ServeHTTP(w, r)`

3. `gateway/internal/api/router.go` exports `RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler)`:
   - Creates a `StrictHandler` via `NewStrictHandler(adminServer, nil)` (no strict middleware needed)
   - Registers all routes by calling `HandlerFromMuxWithBaseURL(strictHandler, mux, "/api/v1")` — this wires all operations from the generated spec to `mux`
   - Routes requiring `instance_admin`: `/api/v1/admin/*` group
   - Routes requiring `compliance_officer`: `/api/v1/compliance/*` group
   - `GET /api/v1/health` is unauthenticated (no JWT, no role check)
   - Role middleware is applied as: `jwtMW(RequireRole("instance_admin")(handler))` — JWT validates first, then role is checked

4. `gateway/cmd/gateway/main.go` calls `api.RegisterAdminRoutes(mux, &api.AdminServer{}, jwtMiddleware)` after existing route registrations, replacing the manually-registered `GET /api/v1/openapi.yaml` handler (which is now covered by the generated router) — **but keep the `openapi.yaml` handler separate since it is unauthenticated and not in the generated spec's protected group**

5. All Admin API routes are mounted under `/api/v1/` alongside the existing Matrix routes (same port `:8008`, no conflict)

6. Unit tests in `gateway/internal/api/middleware_test.go` cover:
   - Request with no JWT context → 401 `M_MISSING_TOKEN`
   - Request with `system_role = "user"` → 403 `M_FORBIDDEN`
   - Request with `system_role = "instance_admin"` and `RequireRole("instance_admin")` → handler called (200)
   - Request with `system_role = "instance_admin"` and `RequireRole("compliance_officer")` → 403 `M_FORBIDDEN`

7. `go build ./...` and `make test-unit-go` pass with zero failures after this story

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **No JWT context → 401** — Go unit test (`net/http/httptest`)
   - Given: request context has no `ContextKeySystemRole` value (empty string)
   - When: `RequireRole("instance_admin")(nextHandler)` handles the request
   - Then: status 401, body contains `"M_MISSING_TOKEN"`, `nextHandler` is NOT called

2. **Wrong role → 403** — Go unit test
   - Given: request context has `ContextKeySystemRole = "user"` (set by `JWTMiddleware`)
   - When: `RequireRole("instance_admin")(nextHandler)` handles the request
   - Then: status 403, body contains `"M_FORBIDDEN"`, `nextHandler` is NOT called

3. **Correct role → handler called** — Go unit test
   - Given: request context has `ContextKeySystemRole = "instance_admin"`
   - When: `RequireRole("instance_admin")(nextHandler)` handles the request
   - Then: status 200, `nextHandler.ServeHTTP` was called exactly once

4. **Cross-role rejection** — Go unit test
   - Given: request context has `ContextKeySystemRole = "instance_admin"`
   - When: `RequireRole("compliance_officer")(nextHandler)` handles the request
   - Then: status 403, body contains `"M_FORBIDDEN"`

## Tasks / Subtasks

- [ ] Create `gateway/internal/api/middleware.go` (AC: #2)
  - [ ] Define `RequireRole(role string) func(http.Handler) http.Handler`
  - [ ] Read `ContextKeySystemRole` from `r.Context()` using `middleware.ContextKeySystemRole` (import `github.com/nebu/nebu/internal/middleware`)
  - [ ] Empty/missing system role → `401 M_MISSING_TOKEN` JSON response (use `writeAdminError` helper or inline)
  - [ ] Role mismatch → `403 M_FORBIDDEN` JSON body: `{"errcode":"M_FORBIDDEN","error":"<role> role required"}`
  - [ ] Role match → `next.ServeHTTP(w, r)`

- [ ] Write unit tests `gateway/internal/api/middleware_test.go` (AC: #6)
  - [ ] Test: no context value → 401 M_MISSING_TOKEN, next NOT called
  - [ ] Test: `system_role = "user"` with `RequireRole("instance_admin")` → 403 M_FORBIDDEN, next NOT called
  - [ ] Test: `system_role = "instance_admin"` with `RequireRole("instance_admin")` → next called
  - [ ] Test: `system_role = "instance_admin"` with `RequireRole("compliance_officer")` → 403 M_FORBIDDEN

- [ ] Create `gateway/internal/api/router.go` (AC: #3, #5)
  - [ ] Define `RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler)`
  - [ ] Create strict handler: `sh := NewStrictHandler(adminServer, nil)`
  - [ ] Register all routes via `HandlerFromMuxWithBaseURL(sh, mux, "/api/v1")` — this registers all 6 operations from `api_gen.go`
  - [ ] NOTE: `HandlerFromMuxWithBaseURL` registers routes WITHOUT auth middleware — but the generated `HandlerWithOptions` uses `ServerInterfaceWrapper` which has `HandlerMiddlewares`. Pass role middleware via `StdHTTPServerOptions.Middlewares` field for route groups — OR wrap the mux after registration. See Dev Notes for the correct approach.

- [ ] Update `gateway/cmd/gateway/main.go` (AC: #4)
  - [ ] Call `apihandler.RegisterAdminRoutes(mux, &apihandler.AdminServer{}, jwtMiddleware)` — add after existing route registrations (around line 1103, near existing `GET /api/v1/openapi.yaml` registration)
  - [ ] Keep the `GET /api/v1/openapi.yaml` registration as-is (it is unauthenticated and separate from the role-gated routes)
  - [ ] Do NOT remove any existing compliance/admin routes registered manually — `HandlerFromMuxWithBaseURL` only adds the spec-defined operations; manually registered routes coexist

- [ ] Run `make test-unit-go` — all tests must pass green (AC: #7)

## Dev Notes

### Critical: What Already Exists vs What This Story Creates

| Item | Current State | This Story's Action |
|---|---|---|
| `gateway/internal/api/server.go` | EXISTS — `AdminServer` stub, all methods return 501 | **READ ONLY** — do not modify |
| `gateway/internal/api/api_gen.go` | EXISTS — generated; has `HandlerFromMuxWithBaseURL`, `NewStrictHandler`, `StdHTTPServerOptions` | **READ ONLY** — never edit |
| `gateway/internal/api/response.go` | EXISTS — `APIResponse[T]`, `Meta`, `APIError`, `WriteJSON`, `ErrorResponse` | **READ ONLY** |
| `gateway/internal/api/pagination.go` | EXISTS — `EncodeCursor`, `DecodeCursor`, `ErrInvalidCursor` | **READ ONLY** |
| `gateway/internal/api/middleware.go` | MISSING | **CREATE** |
| `gateway/internal/api/router.go` | MISSING | **CREATE** |
| `gateway/internal/api/middleware_test.go` | MISSING | **CREATE** |
| `gateway/cmd/gateway/main.go` | EXISTS — registers routes manually | **MODIFY** (add `RegisterAdminRoutes` call only) |

### Critical: Role Check Implementation Pattern

**Do NOT reinvent the role check.** The compliance handlers in `gateway/internal/compliance/handler.go` already use the established pattern:

```go
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
if systemRole != "compliance_officer" {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
    return
}
```

`RequireRole` is the reusable, middleware-wrapped version of this same pattern. Import `github.com/nebu/nebu/internal/middleware` to access `middleware.ContextKeySystemRole`.

**Error response format:** Admin API uses the Admin response format (`{"errcode": "...", "error": "..."}` — same as Matrix error format, NOT the Admin envelope `{"data": null, "error": {...}}`). This is consistent with how existing compliance handlers respond to 401/403 (they use `writeComplianceError` which writes Matrix-style errors). The 401/403 responses from middleware are not Admin API business responses — they are HTTP-layer auth rejections. Use the Matrix error format for consistency with existing middleware patterns.

**401 trigger condition:** The `ContextKeySystemRole` value is an empty string `""` when no JWT is present (the context key has a zero value if `JWTMiddleware` wasn't run). So empty string = unauthenticated. Check: `if systemRole == ""` → 401.

### Critical: Route Registration Strategy

The generated `HandlerFromMuxWithBaseURL` in `api_gen.go` uses `http.ServeMux.HandleFunc` to register routes. It registers these 6 routes:
- `GET /api/v1/admin/config`
- `GET /api/v1/admin/metrics`
- `GET /api/v1/admin/rooms`
- `GET /api/v1/admin/users`
- `GET /api/v1/compliance/access-requests`
- `GET /api/v1/health`

**Problem:** `HandlerFromMuxWithBaseURL` registers handlers directly without role middleware. The `StdHTTPServerOptions.Middlewares` field applies `MiddlewareFunc` to each operation wrapper, but they run AFTER the route is matched (they are operation-level middleware, not transport-level).

**Recommended approach for `RegisterAdminRoutes`:**

Option A (simplest): Register routes via `HandlerFromMuxWithBaseURL` into a **sub-mux**, then wrap the sub-mux with a path-based role dispatcher before adding to the main mux. This avoids per-route repetition.

Option B (most explicit): Do NOT use `HandlerFromMuxWithBaseURL`. Instead use `NewStrictHandler` to get the `ServerInterface`, then manually register each route on `mux` with the appropriate middleware chain. This is explicit and gives full control.

**Use Option B** — it matches the story AC exactly ("registers all routes by calling `RegisterHandlersWithBaseURL`" but with middleware). However, since the generated `HandlerFromMuxWithBaseURL` has no per-route middleware injection, the simplest correct implementation is:

```go
// router.go
func RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler) {
    sh := NewStrictHandler(adminServer, nil)

    // /api/v1/health — unauthenticated (no JWT, no role)
    mux.HandleFunc("GET /api/v1/health", sh.GetHealth)

    // /api/v1/admin/* — instance_admin role required
    adminMW := jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminConfig)))
    mux.Handle("GET /api/v1/admin/config", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminConfig))))
    mux.Handle("GET /api/v1/admin/metrics", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminMetrics))))
    mux.Handle("GET /api/v1/admin/rooms", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.ListAdminRooms))))
    mux.Handle("GET /api/v1/admin/users", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.ListAdminUsers))))

    // /api/v1/compliance/* — compliance_officer role required
    mux.Handle("GET /api/v1/compliance/access-requests", jwtMW(RequireRole("compliance_officer")(http.HandlerFunc(sh.ListComplianceAccessRequests))))
}
```

This approach is clear, consistent with existing route registrations in `main.go`, and does not fight against the generated code.

**IMPORTANT:** `sh.GetAdminConfig` etc. are methods on `*strictHandler` (private type) — they implement `ServerInterface`. To call them as `http.HandlerFunc`, you need to use the `ServerInterface` methods directly. Since `NewStrictHandler` returns `ServerInterface`, use:

```go
sh := NewStrictHandler(adminServer, nil)
mux.Handle("GET /api/v1/admin/config", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminConfig))))
```

### Naming Convention for Error Response

The `RequireRole` middleware writes errors directly. Use `encoding/json` inline or a helper. The error format must be:
- 401: `{"errcode":"M_MISSING_TOKEN","error":"Missing access token"}` (matches `writeMatrixError` in `middleware/auth.go`)
- 403: `{"errcode":"M_FORBIDDEN","error":"instance_admin role required"}` (role name inserted)

Do NOT use `WriteJSON` from `response.go` with `APIResponse` wrapper — that is for business responses, not auth gate responses.

Define a private helper in `middleware.go`:

```go
func writeAdminError(w http.ResponseWriter, status int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{"errcode": errcode, "error": message})
}
```

### Package and Build Constraints

All files in `gateway/internal/api/` use `//go:build go1.22` as the first non-comment line (established in Story 6.1 via `api_gen.go`). New files must include this constraint.

Module path: `github.com/nebu/nebu` (from `gateway/go.mod`). Package declaration: `package api`.

### Route Conflict Prevention

`HandlerFromMuxWithBaseURL` also registers `GET /api/v1/health` on the provided mux. If `main.go` calls both `apihandler.RegisterAdminRoutes` and has any `GET /api/v1/health` registration, there will be a panic (`http.ServeMux` panics on duplicate patterns in Go 1.22+). **Do NOT register `GET /api/v1/health` elsewhere** — `RegisterAdminRoutes` owns it.

The existing `GET /api/v1/openapi.yaml` handler registered manually in `main.go` is NOT defined in `openapi.yaml` as a path, so it does NOT conflict with the generated registration.

### Existing Route Coexistence

`main.go` currently registers these `/api/v1/*` routes manually:
- `POST /api/v1/compliance/access-requests` — compliance handler
- `GET /api/v1/compliance/access-requests` — compliance handler (via `accessRequestHandler.GetAccessRequests`)
- `POST /api/v1/compliance/access-requests/{requestId}/approve` — compliance handler
- `POST /api/v1/compliance/access-requests/{requestId}/reject` — compliance handler
- `POST /api/v1/compliance/access-requests/{requestId}/session` — session handler
- `GET /api/v1/compliance/export` — export handler
- `POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` — revoke handler
- `DELETE /api/v1/admin/users/{userId}/keys` — key deletion handler
- `POST /api/v1/admin/users/{userId}/anonymize` — anonymization handler
- `GET /api/v1/openapi.yaml` — openapi handler

**The generated router registers `GET /api/v1/compliance/access-requests`.** This CONFLICTS with `accessRequestHandler.GetAccessRequests` already registered in `main.go`.

Resolution: The generated `GET /api/v1/compliance/access-requests` should **replace** the existing manual registration. When `RegisterAdminRoutes` is called, remove the manual `GET /api/v1/compliance/access-requests` registration from `main.go` (it is now handled by the generated stub returning `501` — consistent with the story's intent).

**All other existing routes are fine** — they use HTTP methods or path segments not present in the generated spec (POST, DELETE, paths with `{requestId}` etc.).

### Testing Pattern (matches Stories 6.1 and 6.2)

```go
//go:build go1.22

package api_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/nebu/nebu/internal/api"
    "github.com/nebu/nebu/internal/middleware"
)

func TestRequireRole_MissingToken_Returns401(t *testing.T) {
    nextCalled := false
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        nextCalled = true
        w.WriteHeader(http.StatusOK)
    })

    handler := api.RequireRole("instance_admin")(next)

    req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
    // No context value for ContextKeySystemRole — simulates missing JWT
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    if w.Code != http.StatusUnauthorized {
        t.Errorf("expected 401, got %d", w.Code)
    }
    if nextCalled {
        t.Error("next handler must not be called on missing token")
    }
}

func TestRequireRole_WrongRole_Returns403(t *testing.T) {
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    handler := api.RequireRole("instance_admin")(next)

    req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
    ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "user")
    req = req.WithContext(ctx)

    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    if w.Code != http.StatusForbidden {
        t.Errorf("expected 403, got %d", w.Code)
    }
}
```

Note: `middleware.ContextKeySystemRole` is typed as `contextKey` (private type in `middleware` package). Use it directly via the exported constant — Go type system handles this correctly since the test imports `middleware`.

### Security Review Note

`security_review: required` — this story creates the role-auth middleware that gates all Admin API endpoints. The `RequireRole` function is a security-critical path: a bug here (e.g., empty string considered a valid role, or early return path skipping the check) could allow unauthorized access to all admin operations. The reviewer must verify:
- Empty string `systemRole` → 401, not 403 and not pass-through
- Middleware wrapping order in `RegisterAdminRoutes`: JWT runs before role check (JWTMiddleware populates `ContextKeySystemRole`, so it must wrap outer)
- All `/api/v1/admin/*` routes require `instance_admin` — none are accidentally unauthenticated
- `GET /api/v1/health` is intentionally unauthenticated (documented in spec as `security: []`)

### Project Structure Notes

**Files to CREATE:**
- `gateway/internal/api/middleware.go` — `RequireRole` middleware
- `gateway/internal/api/middleware_test.go` — 4 unit tests
- `gateway/internal/api/router.go` — `RegisterAdminRoutes`

**Files to MODIFY:**
- `gateway/cmd/gateway/main.go` — add `RegisterAdminRoutes` call; remove conflicting `GET /api/v1/compliance/access-requests` manual registration

**Files NOT to touch:**
- `gateway/internal/api/api_gen.go` — generated; never edit
- `gateway/internal/api/server.go` — AdminServer stub; Stories 6.4+ will extend
- `gateway/internal/api/openapi_handler.go` — unchanged
- `gateway/internal/api/response.go` — unchanged
- `gateway/internal/api/pagination.go` — unchanged
- `gateway/api/openapi.yaml` — no new paths needed
- `Makefile` — no changes needed

### References

- [Source: epics.md#Story-6.3] Full Acceptance Criteria (lines 2698–2717)
- [Source: epics.md#Story-6.6] `role_overrides` table — Story 6.3 AC says to check JWT `roles` claim AND `role_overrides` table; **however Story 6.6 creates the `role_overrides` table** — this story's middleware only reads `ContextKeySystemRole` from context (JWT-derived). Role override integration is deferred to Story 6.6.
- [Source: gateway/internal/api/api_gen.go] `HandlerFromMuxWithBaseURL`, `NewStrictHandler`, `StdHTTPServerOptions`, `ServerInterface` — the exact generated function signatures this story uses
- [Source: gateway/internal/middleware/auth.go] `ContextKeySystemRole`, `JWTMiddleware`, `writeMatrixError` — established JWT middleware and context key
- [Source: gateway/internal/compliance/handler.go#PostAccessRequest] Existing role-gate pattern: `systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)`
- [Source: gateway/cmd/gateway/main.go lines 940–1103] Existing route registrations — must not conflict; `GET /api/v1/compliance/access-requests` must be deregistered from manual list
- [Source: architecture.md rule 11] "Admin API Implementation muss `ServerInterface` aus `api_gen.go` erfüllen — kein freestyle routing"
- [Source: architecture.md rule 3] "Matrix-Endpunkte geben Matrix-Format zurück, Admin-Endpunkte geben Admin-Format zurück — kein Mischen" (but auth rejections at middleware layer use Matrix error format — see Dev Notes)
- [Source: 6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint.md] Package structure, build constraints, module path
- [Source: 6-2-admin-api-response-format-cursor-pagination.md] `WriteJSON`, `ErrorResponse`, `APIResponse[T]` helpers available in `response.go`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

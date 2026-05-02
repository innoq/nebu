---
security_review: required
---

# Story 6.5: User Deactivation + Reactivation + Session-Invalidierung

Status: review

## Story

As an instance admin,
I want to deactivate and reactivate user accounts,
so that I can immediately revoke access for a user without permanently deleting their data.

## Acceptance Criteria

1. `POST /api/v1/admin/users/{userId}/deactivate` — `instance_admin` role required; body: `{"reason": "..."}` (required, min 10 chars):
   - Sets `users.is_active = false`, `users.deactivated_at = <now_ms>`, `users.deactivation_reason = reason` in a single DB transaction
   - Calls `gRPC CoreService.InvalidateUserSessions(user_id)` → Core invalidates all active ETS sessions for the user and deletes from `sync_tokens` + `sessions` tables
   - Returns `200 {"data": {"user_id": "...", "status": "deactivated"}}`
   - Returns `404 M_NOT_FOUND` if user does not exist
   - Returns `409 M_CONFLICT` if user is already deactivated (`is_active = false`)
   - Returns `400 M_BAD_JSON` if body is missing or `reason` is shorter than 10 chars
   - Calls `audit.LogEvent` with `action="user_deactivated"`, `target_type="user"`, `target_id=userId`, `metadata={"reason": reason}`, `outcome="success"`

2. `POST /api/v1/admin/users/{userId}/reactivate` — `instance_admin` role required; no body required:
   - Sets `users.is_active = true`, `users.deactivated_at = NULL`, `users.deactivation_reason = NULL` in a single DB transaction
   - Returns `200 {"data": {"user_id": "...", "status": "active"}}`
   - Returns `404 M_NOT_FOUND` if user does not exist
   - Returns `409 M_CONFLICT` if user is in `keys_deleted` or `anonymized` state (irreversible DSGVO states — cannot reactivate)
   - Returns `409 M_CONFLICT` if user is already active (`is_active = true`)
   - Calls `audit.LogEvent` with `action="user_reactivated"`, `target_type="user"`, `target_id=userId`, `outcome="success"`

3. A new migration `000034_users_deactivation.up.sql` adds `deactivated_at BIGINT` and `deactivation_reason TEXT` columns to the `users` table (columns are NULL for existing rows).

4. `proto/core.proto` adds `rpc InvalidateUserSessions(InvalidateUserSessionsRequest) returns (InvalidateUserSessionsResponse)` and the corresponding message types; `make proto` regenerates Go and Elixir stubs.

5. The Elixir `Nebu.EventDispatcher.Server` implements the `invalidate_user_sessions/2` handler, which calls `Nebu.Session.SessionSupervisor.destroy_session/1` (already implemented) to evict ETS + delete from DB.

6. After deactivation, any Matrix API or Admin API request using the deactivated user's JWT token returns `401 M_UNKNOWN_TOKEN`. This is achieved by adding an `is_active` check in the Go gateway's JWT middleware (`JWTMiddleware` in `gateway/internal/middleware/auth.go`) via a DB lookup after the denylist check. The check uses an in-process `sync.Map` cache with 60-second TTL to avoid per-request DB hits.

7. `gateway/api/openapi.yaml` is updated to add `POST /admin/users/{userId}/deactivate` and `POST /admin/users/{userId}/reactivate` paths with request/response schemas; `make gen-api` is run to regenerate `api_gen.go`.

8. Both endpoints are registered in `gateway/internal/api/router.go` via `RegisterAdminRoutes`.

9. Unit tests in `gateway/internal/api/deactivation_handler_test.go`:
   - Deactivate active user → 200, status="deactivated"
   - Deactivate already-deactivated user → 409
   - Deactivate non-existent user → 404
   - Deactivate with too-short reason → 400
   - Reactivate deactivated user → 200, status="active"
   - Reactivate anonymized user → 409
   - Reactivate keys_deleted user → 409
   - Reactivate already-active user → 409

10. Unit test in `gateway/internal/middleware/auth_deactivated_test.go` verifies that a request carrying a valid JWT for a deactivated user is rejected with 401 `M_UNKNOWN_TOKEN`.

11. ExUnit test in Elixir for `invalidate_user_sessions` gRPC handler: given sessions in ETS + DB, when handler is called, sessions are removed from ETS and DB tables.

12. `go build ./...` and `make test-unit-go` pass with zero failures after this story. `make test-unit-elixir` passes.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **Deactivate active user → 200** — Go unit test (mock `UserRepository`)
   - Given: user `@alice:example.com` with `is_active=true` exists
   - When: `POST /api/v1/admin/users/@alice:example.com/deactivate` with `{"reason": "Security incident"}`
   - Then: status 200, body `{"data": {"user_id": "@alice:example.com", "status": "deactivated"}}`

2. **Deactivate already-deactivated → 409** — Go unit test
   - Given: user exists with `is_active=false`
   - When: `POST /api/v1/admin/users/@alice:example.com/deactivate`
   - Then: status 409, body `{"error": {"code": "M_CONFLICT", "message": "User is already deactivated"}}`

3. **Deactivate non-existent user → 404** — Go unit test
   - Given: `@ghost:example.com` does not exist
   - When: `POST /api/v1/admin/users/@ghost:example.com/deactivate`
   - Then: status 404, body `{"error": {"code": "M_NOT_FOUND", "message": "User not found"}}`

4. **Deactivate with short reason → 400** — Go unit test
   - Given: body `{"reason": "too short"}`
   - When: `POST /api/v1/admin/users/@alice:example.com/deactivate`
   - Then: status 400, body `{"error": {"code": "M_BAD_JSON", "message": "reason must be at least 10 characters"}}`

5. **Reactivate deactivated user → 200** — Go unit test
   - Given: user exists with `is_active=false`
   - When: `POST /api/v1/admin/users/@alice:example.com/reactivate`
   - Then: status 200, body `{"data": {"user_id": "@alice:example.com", "status": "active"}}`

6. **Reactivate anonymized user → 409** — Go unit test
   - Given: user exists with `anonymized_at IS NOT NULL`
   - When: `POST /api/v1/admin/users/@alice:example.com/reactivate`
   - Then: status 409, body `{"error": {"code": "M_CONFLICT", "message": "Cannot reactivate: user is in anonymized state"}}`

7. **Reactivate keys_deleted user → 409** — Go unit test
   - Given: user exists with `deletion_status='keys_deleted'`
   - When: `POST /api/v1/admin/users/@alice:example.com/reactivate`
   - Then: status 409, body `{"error": {"code": "M_CONFLICT", "message": "Cannot reactivate: user is in keys_deleted state"}}`

8. **JWT middleware rejects deactivated user → 401** — Go unit test (auth middleware test)
   - Given: valid JWT for `@alice:example.com` (passes signature check + denylist), but DB lookup returns `is_active=false`
   - When: any request with that JWT
   - Then: status 401, body `{"errcode": "M_UNKNOWN_TOKEN", "error": "Account deactivated"}`

9. **Session invalidation after deactivation** — ExUnit test
   - Given: ETS contains a session for `@alice:example.com`; `sessions` and `sync_tokens` rows exist
   - When: `InvalidateUserSessions` gRPC handler is called with `user_id="@alice:example.com"`
   - Then: ETS entry removed, `sessions` row deleted, `sync_tokens` row deleted

## Tasks / Subtasks

- [x] Write migration `000034_users_deactivation.up.sql` (AC: #3)
  - [x] `ALTER TABLE users ADD COLUMN deactivated_at BIGINT;`
  - [x] `ALTER TABLE users ADD COLUMN deactivation_reason TEXT;`
  - [x] Write corresponding `.down.sql`

- [x] Update `proto/core.proto` (AC: #4)
  - [x] Add `rpc InvalidateUserSessions(InvalidateUserSessionsRequest) returns (InvalidateUserSessionsResponse)`
  - [x] Add message types (see Dev Notes for exact field definitions)
  - [x] Run `make proto` to regenerate `gateway/internal/grpc/pb/core.pb.go` and Elixir stubs

- [x] Implement Elixir `invalidate_user_sessions/2` handler in `Nebu.EventDispatcher.Server` (AC: #5)
  - [x] Follow existing pattern for `session_supervisor_module` configurable dependency injection
  - [x] Call `Nebu.Session.SessionSupervisor.destroy_session(user_id)` — already handles ETS + DB
  - [x] Write ExUnit test (AC: #11, Acceptance Test #9)

- [x] Add `is_active` check to Go JWT middleware (AC: #6)
  - [x] Add `UserStatusChecker` interface to `gateway/internal/middleware/` (DB query: `SELECT is_active FROM users WHERE user_id = $1`)
  - [x] Add per-middleware-instance `sync.Map` cache with 60-second TTL (closure variable, not package-level, for test isolation)
  - [x] Implement as `WithUserStatusCheck` wrapper (not modifying JWTMiddleware signature)
  - [x] Check runs AFTER denylist check — only for cryptographically verified tokens
  - [x] If `is_active = false` → `401 M_UNKNOWN_TOKEN "Account deactivated"`
  - [x] Wire real `DBUserStatusChecker` (DB-backed) in `main.go`
  - [x] Write unit test (AC: #10, Acceptance Test #8)

- [x] Update `gateway/api/openapi.yaml` (AC: #7)
  - [x] Add `DeactivateUserRequest` schema: `{"reason": string, minLength: 10}`
  - [x] Add `UserStatusResponse` schema: `{"data": {"user_id": string, "status": string}}`
  - [x] Add `POST /admin/users/{userId}/deactivate` path
  - [x] Add `POST /admin/users/{userId}/reactivate` path
  - [x] Run `make gen-api` to regenerate `api_gen.go`

- [x] Implement `DeactivateAdminUser` and `ReactivateAdminUser` in `gateway/internal/api/server.go` (AC: #1, #2)
  - [x] Add `DeactivationRepository` interface with `DeactivateUser`, `ReactivateUser`, `GetUserStatus` methods
  - [x] Implement methods in `gateway/internal/api/deactivation_repo.go`
  - [x] Implement handlers with full validation logic
  - [x] Add `Deactivation DeactivationRepository` field to `AdminServer`
  - [x] Wire in `main.go`

- [x] Register new routes in `gateway/internal/api/router.go` (AC: #8)
  - [x] `POST /api/v1/admin/users/{userId}/deactivate` — `instance_admin` required
  - [x] `POST /api/v1/admin/users/{userId}/reactivate` — `instance_admin` required

- [x] Write Go unit tests `gateway/internal/api/deactivation_handler_test.go` (AC: #9)
  - [x] All 7 handler-level acceptance tests (#1–#7)
  - [x] Audit log verified on success cases

- [x] Run `make test-unit-go` and `make test-unit-elixir` — zero failures (AC: #12)

## Dev Notes

### Critical: Database Schema State After This Story

The `users` table after migration `000034`:
```sql
-- Existing columns (from earlier migrations):
user_id TEXT PRIMARY KEY
system_role TEXT NOT NULL DEFAULT 'user'
is_active BOOLEAN NOT NULL DEFAULT true  -- ← deactivation sets this to false
created_at BIGINT NOT NULL
last_seen_at BIGINT
deletion_status TEXT  -- 'keys_deleted' blocks reactivation (migration 000021)
anonymized_at BIGINT  -- NOT NULL blocks reactivation (migration 000022)

-- New columns from this story:
deactivated_at BIGINT  -- NULL until first deactivation; epoch ms
deactivation_reason TEXT  -- NULL until first deactivation; cleared on reactivate
```

The `deriveStatus` function in `users_repo.go` already handles `is_active=false` → `"deactivated"`. No changes needed there.

### Critical: State Machine for Reactivation

```
active → deactivated: SET is_active=false, deactivated_at=now_ms, deactivation_reason=reason
deactivated → active: SET is_active=true, deactivated_at=NULL, deactivation_reason=NULL

Blocked transitions (return 409 M_CONFLICT):
  keys_deleted → active: BLOCKED (deletion_status='keys_deleted')
  anonymized → active: BLOCKED (anonymized_at IS NOT NULL)
  active → active: BLOCKED (already active)
  deactivated → deactivated: BLOCKED (already deactivated)
```

**Reactivation check order** (in SQL or Go, before UPDATE):
1. `anonymized_at IS NOT NULL` → 409 "Cannot reactivate: user is in anonymized state"
2. `deletion_status = 'keys_deleted'` → 409 "Cannot reactivate: user is in keys_deleted state"
3. `is_active = true` → 409 "User is already active"
4. Otherwise: proceed with UPDATE

**Deactivation check order**:
1. User does not exist → 404 M_NOT_FOUND
2. `is_active = false` → 409 "User is already deactivated"
3. Otherwise: proceed with UPDATE + gRPC InvalidateUserSessions

### Critical: Proto Changes

Add to `proto/core.proto` after the existing `ForgetRoom` messages:
```proto
// InvalidateUserSessions — Story 6.5: Admin deactivation revokes all active sessions.
// Calls SessionManager.destroy_session/1 for the target user.
// Returns ok=true on success; gRPC error on DB failure.
message InvalidateUserSessionsRequest {
  string user_id = 1;  // Matrix user ID (@localpart:server)
}

message InvalidateUserSessionsResponse {
  bool ok = 1;
}
```

And in the `service CoreService` block:
```proto
// InvalidateUserSessions — Story 6.5: revoke all sessions for a user (admin deactivation)
rpc InvalidateUserSessions(InvalidateUserSessionsRequest) returns (InvalidateUserSessionsResponse);
```

After `make proto`, the Go generated code in `gateway/internal/grpc/pb/core.pb.go` and `core_grpc.pb.go` will contain the new types. The Elixir generated stubs in `core/apps/event_dispatcher/lib/pb/core.pb.ex` and `core_grpc.pb.ex` will also update.

### Critical: Elixir Handler Pattern

`Nebu.EventDispatcher.Server` uses a configurable-module pattern for all dependencies. Add:

```elixir
# ─── Configurable SessionSupervisor module for testability ─────────────────
defp session_supervisor_module do
  Application.get_env(:event_dispatcher, :session_supervisor_module, Nebu.Session.SessionSupervisor)
end

def invalidate_user_sessions(%Core.InvalidateUserSessionsRequest{} = req, _stream) do
  case session_supervisor_module().destroy_session(req.user_id) do
    :ok ->
      Core.InvalidateUserSessionsResponse.new(ok: true)
    {:error, reason} ->
      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "session invalidation failed: #{inspect(reason)}"
  end
end
```

`Nebu.Session.SessionSupervisor.destroy_session/1` already handles the full flow:
- `Nebu.Session.PgStore.invalidate_session/1` → single PostgreSQL transaction deletes `sync_tokens` + `sessions` rows
- On success: calls `Nebu.Session.EtsStore.delete_session/1` to evict ETS (atomicity guarantee: ETS only evicted after DB commits)

**ExUnit test pattern** (inject fake via `Application.put_env`):
```elixir
setup do
  Application.put_env(:event_dispatcher, :session_supervisor_module, FakeSessionSupervisor)
  on_exit(fn -> Application.delete_env(:event_dispatcher, :session_supervisor_module) end)
  :ok
end
```

### Critical: Go JWT Middleware Enhancement

The current `JWTMiddleware` in `gateway/internal/middleware/auth.go` checks:
1. JWT signature + expiry (OIDC verifier)
2. Denylist (logout tokens)

This story adds step 3: **account status check** (is_active=false → 401).

**Design: `UserStatusChecker` interface + in-process cache**

```go
// UserStatusChecker checks if a user account is active.
// nil implementation = check disabled (tests, backward compat).
type UserStatusChecker interface {
    IsUserActive(ctx context.Context, userID string) (bool, error)
}

// statusCacheEntry pairs the cached result with its expiry time.
type statusCacheEntry struct {
    isActive  bool
    expiresAt time.Time
}
```

Cache implementation (inside middleware closure, not exported):
```go
var statusCache sync.Map  // key: userID (string) → value: statusCacheEntry

func checkUserActive(ctx context.Context, checker UserStatusChecker, userID string) (bool, error) {
    // Check cache first (60s TTL)
    if v, ok := statusCache.Load(userID); ok {
        entry := v.(statusCacheEntry)
        if time.Now().Before(entry.expiresAt) {
            return entry.isActive, nil
        }
        statusCache.Delete(userID)  // expired — remove and re-fetch
    }
    isActive, err := checker.IsUserActive(ctx, userID)
    if err != nil {
        return true, err  // on DB error: fail open (allow request, log warning)
    }
    statusCache.Store(userID, statusCacheEntry{
        isActive:  isActive,
        expiresAt: time.Now().Add(60 * time.Second),
    })
    return isActive, nil
}
```

**Important: fail-open on DB error** — if the `users` DB lookup fails, allow the request through (log warning). This prevents a DB outage from locking out all users. The story AC requires 401 for deactivated users specifically, not for DB errors.

**Injection into `JWTMiddleware`**: Add optional final parameter:
```go
func JWTMiddleware(provider *auth.Provider, clientID, claimName string, store TokenStore, serverName ...string) func(http.Handler) http.Handler
// Changes to:
func JWTMiddleware(provider *auth.Provider, clientID, claimName string, store TokenStore, statusChecker UserStatusChecker, serverName ...string) func(http.Handler) http.Handler
```

OR (preferred — avoids breaking existing callers): use a separate functional-options pattern:

**Preferred approach: separate `WithUserStatusCheck` wrapper** that wraps around the existing `JWTMiddleware` result:
```go
// WithUserStatusCheck wraps an existing JWT middleware and adds an is_active check.
// This avoids changing JWTMiddleware's signature (no existing callers break).
func WithUserStatusCheck(next func(http.Handler) http.Handler, checker UserStatusChecker) func(http.Handler) http.Handler {
    return func(h http.Handler) http.Handler {
        return next(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // At this point JWT is verified and ContextKeyUserID is populated.
            userID, _ := r.Context().Value(ContextKeyUserID).(string)
            if checker != nil && userID != "" {
                active, err := checkUserActive(r.Context(), checker, userID)
                if err != nil {
                    slog.Warn("user status check failed — failing open", "user_id", userID, "err", err)
                } else if !active {
                    writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Account deactivated")
                    return
                }
            }
            h.ServeHTTP(w, r)
        }))
    }
}
```

This keeps `JWTMiddleware` signature stable (all existing callers in `main.go` still compile unchanged). In `main.go`, wrap the existing `jwtMiddleware` variable:
```go
jwtMiddleware = middleware.WithUserStatusCheck(
    middleware.JWTMiddleware(oidcProvider, oidcClientID, roleClaim, tokenStore, cfg.ServerName),
    &middleware.DBUserStatusChecker{DB: db},
)
```

**DB implementation**:
```go
type DBUserStatusChecker struct {
    DB *sql.DB
}

func (c *DBUserStatusChecker) IsUserActive(ctx context.Context, userID string) (bool, error) {
    var isActive bool
    err := c.DB.QueryRowContext(ctx, "SELECT is_active FROM users WHERE user_id = $1", userID).Scan(&isActive)
    if errors.Is(err, sql.ErrNoRows) {
        return true, nil  // unknown user: fail open (let downstream handle 404)
    }
    return isActive, err
}
```

### Critical: DeactivationRepository Interface

Create `gateway/internal/api/deactivation_repo.go`:

```go
type DeactivationRepository interface {
    // GetUserStatus returns (is_active, deletion_status, anonymized_at) for user.
    // Returns (false, "", 0, ErrUserNotFound) if user does not exist.
    GetUserStatus(ctx context.Context, userID string) (isActive bool, deletionStatus string, anonymizedAt int64, err error)
    // DeactivateUser sets is_active=false, deactivated_at=nowMs, deactivation_reason=reason.
    // Caller must verify user is currently active before calling.
    DeactivateUser(ctx context.Context, userID, reason string, nowMs int64) error
    // ReactivateUser sets is_active=true, deactivated_at=NULL, deactivation_reason=NULL.
    // Caller must verify user is in deactivated state before calling.
    ReactivateUser(ctx context.Context, userID string) error
}

var ErrUserNotFound = errors.New("user not found")
```

The SQL for `DeactivateUser`:
```sql
UPDATE users
SET is_active = false, deactivated_at = $2, deactivation_reason = $3
WHERE user_id = $1
```

The SQL for `ReactivateUser`:
```sql
UPDATE users
SET is_active = true, deactivated_at = NULL, deactivation_reason = NULL
WHERE user_id = $1
```

The SQL for `GetUserStatus`:
```sql
SELECT is_active, COALESCE(deletion_status, ''), COALESCE(anonymized_at, 0)
FROM users WHERE user_id = $1
```

### Critical: Handler Logic

`DeactivateAdminUser` (implements generated `StrictServerInterface` after `make gen-api`):
```go
func (s *AdminServer) DeactivateAdminUser(ctx context.Context, req DeactivateAdminUserRequestObject) (DeactivateAdminUserResponseObject, error) {
    if s.Deactivation == nil {
        return DeactivateAdminUser501Response{}, nil  // pre-wiring guard (router_test compat)
    }

    userID := req.UserId

    // 1. Parse + validate body
    reason := ""
    if req.Body != nil {
        reason = strings.TrimSpace(req.Body.Reason)
    }
    if len(reason) < 10 {
        return &deactivate400Resp{msg: "reason must be at least 10 characters"}, nil
    }

    // 2. Check current status
    isActive, deletionStatus, anonymizedAt, err := s.Deactivation.GetUserStatus(ctx, userID)
    if errors.Is(err, ErrUserNotFound) {
        return &deactivate404Resp{}, nil
    }
    if err != nil { return nil, err }
    _ = deletionStatus; _ = anonymizedAt  // not needed for deactivation
    if !isActive {
        return &deactivate409Resp{msg: "User is already deactivated"}, nil
    }

    // 3. DB update
    nowMs := time.Now().UnixMilli()
    if err := s.Deactivation.DeactivateUser(ctx, userID, reason, nowMs); err != nil {
        return nil, err
    }

    // 4. gRPC: invalidate sessions (best-effort — log on failure, do not block)
    if s.CoreClient != nil {
        _, grpcErr := s.CoreClient.InvalidateUserSessions(ctx, &pb.InvalidateUserSessionsRequest{UserId: userID})
        if grpcErr != nil {
            slog.Warn("InvalidateUserSessions failed", "user_id", userID, "err", grpcErr)
        }
    }

    // 5. Audit log (never-raise)
    if s.CoreClient != nil {
        actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
        _ = audit.LogEvent(ctx, s.CoreClient, actorID, "user_deactivated", "user", userID,
            map[string]any{"reason": reason}, "success", "")
    }

    return &deactivate200Resp{userID: userID, status: "deactivated"}, nil
}
```

`ReactivateAdminUser` follows the same pattern — checks `anonymized_at IS NOT NULL`, `deletion_status='keys_deleted'`, `is_active=true` (all → 409), then updates DB and logs.

### Critical: OpenAPI Spec Changes

Add two new paths under `/admin/users/{userId}` in `openapi.yaml`:

```yaml
/admin/users/{userId}/deactivate:
  post:
    operationId: DeactivateAdminUser
    summary: Deactivate user account (instance_admin required)
    parameters:
      - name: userId
        in: path
        required: true
        schema:
          type: string
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/DeactivateUserRequest"
    responses:
      "200":
        description: User deactivated
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/UserStatusResponse"
      "400":
        description: Bad request (missing/short reason)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "404":
        description: User not found
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "409":
        description: Conflict (already deactivated)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"

/admin/users/{userId}/reactivate:
  post:
    operationId: ReactivateAdminUser
    summary: Reactivate user account (instance_admin required)
    parameters:
      - name: userId
        in: path
        required: true
        schema:
          type: string
    responses:
      "200":
        description: User reactivated
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/UserStatusResponse"
      "404":
        description: User not found
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "409":
        description: Conflict (irreversible state or already active)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
```

New schemas to add under `components/schemas`:
```yaml
DeactivateUserRequest:
  type: object
  required:
    - reason
  properties:
    reason:
      type: string
      minLength: 10
UserStatusResponse:
  type: object
  required:
    - data
  properties:
    data:
      type: object
      required:
        - user_id
        - status
      properties:
        user_id:
          type: string
        status:
          type: string
```

### Critical: Route Registration Pattern

Follow the existing pattern in `router.go` (wrapper functions for param extraction):

```go
mux.Handle("POST /api/v1/admin/users/{userId}/deactivate",
    jwtMW(RequireRole("instance_admin")(deactivateAdminUserHandler(sh))))

mux.Handle("POST /api/v1/admin/users/{userId}/reactivate",
    jwtMW(RequireRole("instance_admin")(reactivateAdminUserHandler(sh))))
```

The generated `ServerInterface` will define:
```go
DeactivateAdminUser(w http.ResponseWriter, r *http.Request, userId string)
ReactivateAdminUser(w http.ResponseWriter, r *http.Request, userId string)
```

Wrapper functions parse `r.PathValue("userId")` and `r.Body` (for deactivate), then delegate to the strict handler.

### Critical: AdminServer Struct

Add `Deactivation DeactivationRepository` to `AdminServer` in `server.go`:
```go
type AdminServer struct {
    DB          *sql.DB
    CoreClient  pb.CoreServiceClient
    Users       UserRepository
    Deactivation DeactivationRepository  // Story 6.5
}
```

Wire in `main.go`:
```go
adminSrv := &apihandler.AdminServer{
    DB:          db,
    CoreClient:  coreClient.CoreServiceClient(),
    Users:       apihandler.NewUserRepo(db),
    Deactivation: apihandler.NewDeactivationRepo(db),  // Story 6.5
}
```

### Critical: Existing router_test.go Compatibility

`router_test.go` creates `&api.AdminServer{}` (all fields nil) and expects new routes to return 501. Add nil-guard in both new handlers (same pattern as `ListAdminUsers`):
```go
if s.Deactivation == nil {
    return DeactivateAdminUser501Response{}, nil
}
```

The `router_test.go` tests **will fail at compile time** if the `StrictServerInterface` is not satisfied — adding these two new operations to the spec means implementing them in `server.go` is mandatory before `go build` passes.

### Critical: Spec-First Workflow

**Mandatory order**:
1. Edit `proto/core.proto` → run `make proto`
2. Edit `gateway/api/openapi.yaml` → run `make gen-api`
3. Add failing unit tests (ATDD: write RED tests first)
4. Implement migration, Elixir handler, Go handler, middleware enhancement
5. Run `make test-unit-go && make test-unit-elixir`

**Do NOT hand-write types that `make gen-api` will produce.**

### Critical: gRPC CoreClient Interface

The `AdminServer` uses `pb.CoreServiceClient` (the generated protobuf interface). After `make proto`, this interface gains `InvalidateUserSessions`. No additional wrapper needed — call directly:
```go
s.CoreClient.InvalidateUserSessions(ctx, &pb.InvalidateUserSessionsRequest{UserId: userID})
```

### Critical: Admin UI Stub (Story 7.x)

`gateway/internal/admin/users.go` contains stub `DeactivateUserHandler` and `ReactivateUserHandler` for the Admin UI routes (`POST /admin/users/{userId}/deactivate`). These routes use a different namespace (`/admin/` not `/api/v1/admin/`) and are registered separately in `main.go` lines 319–320. Do NOT touch those handlers — they are the UI stub, not the API implementation.

Story 7.x (Epic 7 Admin UI) will replace the stub with real API calls. For now, the two systems coexist: UI stub (`/admin/...`) and API (`/api/v1/admin/...`).

### Critical: TTL Cache Invalidation Guarantee

The 60-second TTL cache means a deactivated user could still make requests for up to 60 seconds after deactivation before the cache expires. This is an explicit design trade-off (from the Epic 6 epics spec: "cached in ETS with 60s TTL to avoid per-request DB hits"). The InvalidateUserSessions gRPC call revokes the ETS-side session immediately; the 60s window only applies to subsequent JWT-based requests from clients that still hold valid tokens.

The `statusCache` is a closure-local `sync.Map` — one per Go process (not shared across instances). This is acceptable for MVP; future distributed caching is a Phase 2 concern.

### Build and Package Constraints

- All new Go files under `gateway/internal/api/` and `gateway/internal/middleware/`: include `//go:build go1.22` as first non-comment line
- Package declaration: `package api` (api files), `package middleware` (middleware files)
- Module: `github.com/nebu/nebu` (from `gateway/go.mod`)
- Test packages: `package api_test` and `package middleware_test` (external test packages, consistent with existing test files)
- Migration numbering: next available is `000034` (check `ls gateway/migrations/` — highest is `000033`)
- Elixir: new test file under `core/apps/event_dispatcher/test/` following existing pattern

### Project Structure Notes

**Files to CREATE:**
- `gateway/migrations/000034_users_deactivation.up.sql` — new columns
- `gateway/migrations/000034_users_deactivation.down.sql` — rollback
- `gateway/internal/api/deactivation_repo.go` — `DeactivationRepository` interface + implementation
- `gateway/internal/api/deactivation_handler_test.go` — all 7 handler acceptance tests
- `gateway/internal/middleware/auth_deactivated_test.go` — middleware rejection test

**Files to MODIFY:**
- `proto/core.proto` — add `InvalidateUserSessions` RPC + message types
- `gateway/internal/grpc/pb/core.pb.go` — REGENERATED by `make proto` (do NOT hand-edit)
- `gateway/internal/grpc/pb/core_grpc.pb.go` — REGENERATED by `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — REGENERATED by `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — REGENERATED by `make proto`
- `gateway/api/openapi.yaml` — add two new paths + two new schemas
- `gateway/internal/api/api_gen.go` — REGENERATED by `make gen-api`
- `gateway/internal/api/server.go` — add `Deactivation` field; implement `DeactivateAdminUser`, `ReactivateAdminUser`; add response types
- `gateway/internal/api/router.go` — register two new POST routes + two new wrapper handlers
- `gateway/internal/middleware/auth.go` — add `UserStatusChecker` interface, `WithUserStatusCheck` wrapper, `DBUserStatusChecker`
- `gateway/internal/api/users_repo.go` — no changes needed (status derivation already handles `is_active`)
- `gateway/cmd/gateway/main.go` — wire `Deactivation` into `AdminServer`; wrap `jwtMiddleware` with `WithUserStatusCheck`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — add `invalidate_user_sessions/2` + configurable module

**Files to ADD Elixir tests:**
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs` — new ExUnit test

**Files NOT to touch:**
- `gateway/internal/admin/users.go` — UI stub, separate namespace (`/admin/` not `/api/v1/`)
- `gateway/internal/api/users_repo.go` — no schema changes needed
- `gateway/internal/api/pagination.go` — unchanged
- `gateway/internal/api/response.go` — unchanged
- `gateway/internal/api/middleware.go` — unchanged (this is `RequireRole`, not JWT)

### Cross-Story Dependencies

- **Story 6.4 (done):** `AdminServer` struct, `UserRepository`, `audit.LogEvent` pattern established — follow exactly
- **Story 6.6 (future):** Will add `role_overrides` table — no conflict with this story
- **Story 7.x (future):** Admin UI stub handlers in `admin/users.go` will be replaced with real API calls
- **Stories 5.7/5.8 (done):** `deletion_status` and `anonymized_at` columns already exist — reactivation must check both
- **Story 4.6 (done):** `Nebu.Session.PgStore.invalidate_session/1` already implemented — the Elixir handler just calls it
- **Story 2.19 (done):** Logout flow also calls `destroy_session` — consistent pattern

### References

- [Source: epics.md#Story-6.5] Lines 2745–2768 — full acceptance criteria
- [Source: epics.md#Story-6.11] Lines 2907–2927 — Gherkin scenario includes deactivate/reactivate flow (Story 6.11 test)
- [Source: gateway/internal/api/server.go] `AdminServer` struct + audit pattern (nil-guard, never-raise)
- [Source: gateway/internal/api/router.go] Route registration wrapper pattern
- [Source: gateway/internal/api/users_repo.go] `deriveStatus` function — already handles `is_active=false`
- [Source: gateway/internal/middleware/auth.go] `JWTMiddleware` chain: verify → denylist → (new) status check
- [Source: gateway/internal/middleware/denylist.go] TokenStore pattern (basis for `UserStatusChecker`)
- [Source: proto/core.proto] Existing RPC definitions — follow exact naming/style
- [Source: core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex] Configurable-module pattern for testability
- [Source: core/apps/session_manager/lib/nebu/session/session_supervisor.ex] `destroy_session/1` — already handles ETS + DB
- [Source: core/apps/session_manager/lib/nebu/session/pg_store.ex] `invalidate_session/1` atomicity contract
- [Source: core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex] Existing `is_active=false` → `{:error, :deactivated}` pattern (confirms DB column semantics)
- [Source: gateway/migrations/000021_users_deletion_status.up.sql] `deletion_status` column
- [Source: gateway/migrations/000022_users_anonymized.up.sql] `anonymized_at` column
- [Source: gateway/migrations/000033_rls_enable_user_tables.up.sql] Latest migration — new migration is `000034`
- [Source: gateway/internal/admin/users.go#181] UI stub handler — do NOT replace with this story
- [Source: gateway/cmd/gateway/main.go#319] Existing UI stub routes (`/admin/...` namespace)
- [Source: gateway/cmd/gateway/main.go#1058] Pattern for wiring handlers with CoreClient
- [Source: gateway/internal/audit/writer.go] `LogEvent` signature and never-raise contract
- [Source: 6-4-user-list-get-api.md#Dev Notes] `AdminServer` Option A pattern, build constraints, spec-first workflow

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

1. Hand-written proto shim files (`event_context.go`, `public_rooms.go` in `gateway/internal/grpc/pb/`) redeclared types now generated by `make proto`. Resolved by emptying those files — they were already marked "to be replaced once make proto runs".
2. `event_context_test.go` and `event_context.go` used `SyncRoomStateEvent` for the `GetEventContextResponse.State` field, but the generated struct uses `ContextStateEvent`. Fixed field type references and `.Type` → `.EventType` accessor.
3. Existing mock clients in `stream_test.go`, `writer_test.go`, `auth_audit_test.go`, and `handler_test.go` did not implement the new `InvalidateUserSessions` method. Added stub implementations.
4. Package-level `sync.Map` cache caused test isolation failures (cached `isActive=false` leaked between tests). Resolved by making the cache a per-middleware-instance closure variable.

### Completion Notes List

- **Migration 000034**: Two `ALTER TABLE` statements add `deactivated_at BIGINT` and `deactivation_reason TEXT` to `users`. No backfill. Down migration drops both.
- **proto/core.proto**: Added `rpc InvalidateUserSessions` + `InvalidateUserSessionsRequest` + `InvalidateUserSessionsResponse`. `make proto` regenerated Go + Elixir stubs; hand-written shim files in `gateway/internal/grpc/pb/` were emptied (types now fully generated).
- **Elixir handler**: `invalidate_user_sessions/2` added to `Nebu.EventDispatcher.Server` using the existing `session_supervisor_module` configurable-module pattern. Calls `destroy_session(user_id)` → `:ok` returns `%Core.InvalidateUserSessionsResponse{ok: true}`; `{:error, _}` raises `GRPC.RPCError` with `internal` status. 5 ExUnit tests pass.
- **Go middleware**: `UserStatusChecker` interface + `DBUserStatusChecker` struct + `WithUserStatusCheck` wrapper added to `gateway/internal/middleware/auth.go`. Cache is per-middleware-instance (closure `sync.Map`, 60s TTL). Fail-open on DB error. 5 middleware tests pass.
- **openapi.yaml + gen-api**: Added `DeactivateUserRequest` + `UserStatusResponse` schemas; added `POST /admin/users/{userId}/deactivate` and `POST /admin/users/{userId}/reactivate` paths. `make gen-api` regenerated `api_gen.go` with new request/response types and 501 stubs.
- **deactivation_repo.go**: `DeactivationRepository` interface + `dbDeactivationRepo` SQL implementation (`GetUserStatus`, `DeactivateUser`, `ReactivateUser`). `ErrUserNotFound` sentinel error.
- **server.go**: `AdminServer` gains `Deactivation DeactivationRepository` field. `DeactivateAdminUser` and `ReactivateAdminUser` handlers implement full AC#1/#2 logic (nil guard → 501, reason validation, state machine, gRPC InvalidateUserSessions, audit log). Response types for 200/400/404/409 added.
- **router.go**: Two new routes registered (`POST /api/v1/admin/users/{userId}/deactivate`, `POST /api/v1/admin/users/{userId}/reactivate`) with `jwtWithStatusCheck` + `RequireRole("instance_admin")` guards.
- **main.go**: `adminSrv` wired with `Deactivation: apihandler.NewDeactivationRepo(bootstrapDB)`. `jwtMiddleware` wrapped with `middleware.WithUserStatusCheck(..., &middleware.DBUserStatusChecker{DB: bootstrapDB})`.
- **All 13 Go handler tests** pass (deactivation_handler_test.go). **All 5 Go middleware tests** pass (auth_deactivated_test.go). **All 5 Elixir invalidate_user_sessions tests** pass. Full `make test-unit-go` and `make test-unit-elixir` green.

### File List

**Created:**
- `gateway/migrations/000034_users_deactivation.up.sql`
- `gateway/migrations/000034_users_deactivation.down.sql`
- `gateway/internal/api/deactivation_repo.go`

**Modified:**
- `proto/core.proto` — added `InvalidateUserSessions` RPC + message types
- `gateway/internal/grpc/pb/core.pb.go` — REGENERATED by `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — REGENERATED by `make proto`
- `gateway/internal/grpc/pb/event_context.go` — emptied (types now generated by proto)
- `gateway/internal/grpc/pb/public_rooms.go` — emptied (types now generated by proto)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — REGENERATED by `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — REGENERATED by `make proto`
- `gateway/api/openapi.yaml` — added 2 schemas + 2 paths
- `gateway/internal/api/api_gen.go` — REGENERATED by `make gen-api`
- `gateway/internal/api/server.go` — Deactivation field + DeactivateAdminUser/ReactivateAdminUser handlers + response types
- `gateway/internal/api/router.go` — 2 new POST routes + wrapper functions
- `gateway/internal/middleware/auth.go` — UserStatusChecker interface + DBUserStatusChecker + WithUserStatusCheck
- `gateway/cmd/gateway/main.go` — Deactivation wiring + jwtWithStatusCheck
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — session_supervisor_module + invalidate_user_sessions/2
- `gateway/internal/matrix/event_context.go` — `.Type` → `.EventType` fix for generated ContextStateEvent
- `gateway/internal/matrix/event_context_test.go` — SyncRoomStateEvent → ContextStateEvent type fix
- `gateway/internal/grpc/stream_test.go` — InvalidateUserSessions stub added to mockCoreClient
- `gateway/internal/audit/writer_test.go` — InvalidateUserSessions stub added to mockCoreClient
- `gateway/internal/admin/auth_audit_test.go` — InvalidateUserSessions stub added to mockCoreClient
- `gateway/internal/compliance/handler_test.go` — InvalidateUserSessions stub added to mockCoreClient

**Pre-existing (unchanged — these were the failing tests):**
- `gateway/internal/api/deactivation_handler_test.go` — 13 acceptance tests (RED phase, now GREEN)
- `gateway/internal/middleware/auth_deactivated_test.go` — 5 acceptance tests (RED phase, now GREEN)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs` — 5 ExUnit tests (RED phase, now GREEN)

### Change Log

- 2026-05-01: Story 6.5 implemented — user deactivation/reactivation endpoints, session invalidation via gRPC, JWT middleware status check with 60s TTL cache. All 23 acceptance tests (13 Go handler + 5 middleware + 5 Elixir) pass. Full test suites green. Side-effect: removed hand-written proto shims for Stories 7-27/7-28 (types now generated by proto); fixed ContextStateEvent field references in matrix/event_context.go.

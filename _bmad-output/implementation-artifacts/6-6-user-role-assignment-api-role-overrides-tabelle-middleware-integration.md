---
security_review: required
---

# Story 6.6: User Role Assignment API (role_overrides Tabelle + Middleware-Integration)

Status: review

## Story

As an instance admin,
I want to explicitly assign or revoke roles for users independent of their OIDC claims,
so that I can grant `compliance_officer` or `instance_admin` to users whose OIDC provider does not emit the required claims.

## Acceptance Criteria

1. Migration `000035_role_overrides.up.sql` creates table `role_overrides`:
   - `user_id TEXT NOT NULL`
   - `role TEXT NOT NULL` — allowed values: `instance_admin` | `compliance_officer`
   - `granted_by TEXT NOT NULL` — Matrix user ID of the admin who made the grant
   - `granted_at TIMESTAMPTZ DEFAULT NOW()`
   - `PRIMARY KEY (user_id, role)`
   - CHECK constraint: `role IN ('instance_admin', 'compliance_officer')`

2. `POST /api/v1/admin/users/{userId}/roles` — `instance_admin` role required; body: `{"role": "instance_admin"|"compliance_officer", "action": "grant"|"revoke"}`:
   - `grant`: upserts into `role_overrides` (ON CONFLICT DO UPDATE SET granted_by, granted_at); returns `200 {"data": {"user_id": "...", "role": "...", "action": "granted"}}`
   - `revoke`: deletes from `role_overrides`; returns `200 {"data": {"user_id": "...", "role": "...", "action": "revoked"}}`
   - Returns `404 M_NOT_FOUND` with message "Role override not found" if revoke is attempted and no override exists for (userId, role)
   - Returns `400 M_BAD_JSON` if body is missing or `role`/`action` fields are invalid
   - Returns `404 M_NOT_FOUND` if `userId` does not exist in the `users` table
   - An admin cannot revoke their own `instance_admin` role (self-revoke protection); returns `403 M_FORBIDDEN` with message "Cannot revoke your own admin role"
   - Calls `audit.LogEvent` with action=`"role_granted"` or `"role_revoked"`, `target_type="user"`, `target_id=userId`, `metadata={"role": role}`, `outcome="success"`

3. `RequireRole` middleware in `gateway/internal/api/middleware.go` is extended to check `role_overrides` in addition to `ContextKeySystemRole` (JWT claim):
   - Current flow (Story 6.3): reads `ContextKeySystemRole` from context (set by JWT middleware); if it matches → allow
   - Extended flow (Story 6.6): if `ContextKeySystemRole` does not match the required role, additionally queries `role_overrides` for the requesting user (`ContextKeyUserID`); if an override row exists with the matching role → allow
   - DB lookup result is cached in a per-middleware-instance `sync.Map` with 60-second TTL (same pattern as `WithUserStatusCheck` in `gateway/internal/middleware/auth.go`)
   - On DB error: fail-open (log warning, do not block the request — prevents DB outage from locking out all admins)
   - `RequireRole` signature changes: now accepts a `RoleOverrideChecker` interface (DB-backed in production, nil-able for tests that only test JWT-claim-based access)

4. `GET /api/v1/admin/users/{userId}` (Story 6.4, `GetAdminUser`) reflects effective roles in its `roles` array: merges JWT claim system role + any `role_overrides` rows for the user. If both JWT and override agree on a role, it appears only once.

5. `GET /api/v1/admin/users` (Story 6.4, `ListAdminUsers`) also merges role_overrides into the `roles` field for each user.

6. `gateway/api/openapi.yaml` is updated to add `POST /admin/users/{userId}/roles` path with request/response schemas; `make gen-api` is run to regenerate `api_gen.go`.

7. Unit tests in `gateway/internal/api/roles_handler_test.go`:
   - Grant role to existing user → 200, action="granted"
   - Grant role to non-existent user → 404
   - Grant with invalid role value → 400
   - Grant with invalid action value → 400
   - Revoke existing override → 200, action="revoked"
   - Revoke non-existent override → 404
   - Self-revoke own instance_admin → 403
   - Self-revoke own instance_admin (role override path) → 403
   - Audit log is called on grant
   - Audit log is called on revoke

8. Unit tests in `gateway/internal/api/middleware_role_override_test.go`:
   - User with JWT role `instance_admin` → still passes without DB lookup
   - User without JWT role but with DB override `instance_admin` → passes (DB lookup called)
   - User without JWT role and without DB override → 403
   - DB error during override lookup → fail-open (request passes, warning logged)
   - Override lookup result is cached (DB called only once for two consecutive requests within 60s)

9. `go build ./...` and `make test-unit-go` pass with zero failures after this story.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **Grant role to existing user → 200** — Go unit test (mock `RoleOverrideRepository`)
   - Given: user `@alice:example.com` exists; actor is `@admin:example.com` with `instance_admin`
   - When: `POST /api/v1/admin/users/@alice:example.com/roles` body `{"role":"compliance_officer","action":"grant"}`
   - Then: status 200, body `{"data":{"user_id":"@alice:example.com","role":"compliance_officer","action":"granted"}}`

2. **Revoke existing override → 200** — Go unit test
   - Given: `role_overrides` has `(@alice:example.com, compliance_officer)`
   - When: `POST /api/v1/admin/users/@alice:example.com/roles` body `{"role":"compliance_officer","action":"revoke"}`
   - Then: status 200, body `{"data":{"user_id":"@alice:example.com","role":"compliance_officer","action":"revoked"}}`

3. **Revoke non-existent override → 404** — Go unit test
   - Given: no `role_overrides` row for `(@alice:example.com, instance_admin)`
   - When: `POST /api/v1/admin/users/@alice:example.com/roles` body `{"role":"instance_admin","action":"revoke"}`
   - Then: status 404, body `{"error":{"code":"M_NOT_FOUND","message":"Role override not found"}}`

4. **Self-revoke own instance_admin → 403** — Go unit test
   - Given: actor is `@admin:example.com`; userId in path is also `@admin:example.com`; role is `instance_admin`; action is `revoke`
   - When: `POST /api/v1/admin/users/@admin:example.com/roles`
   - Then: status 403, body `{"error":{"code":"M_FORBIDDEN","message":"Cannot revoke your own admin role"}}`

5. **Invalid role value → 400** — Go unit test
   - Given: body `{"role":"superadmin","action":"grant"}`
   - When: `POST /api/v1/admin/users/@alice:example.com/roles`
   - Then: status 400, body `{"error":{"code":"M_BAD_JSON","message":"invalid role: must be instance_admin or compliance_officer"}}`

6. **DB override lookup allows access** — Go unit test (middleware test)
   - Given: request context has `ContextKeySystemRole = ""` (no JWT role) but `ContextKeyUserID = "@alice:example.com"`; mock DB returns override row for `(alice, instance_admin)`
   - When: `RequireRole("instance_admin", checker)(next)` handles request
   - Then: next handler is called (200)

7. **DB override lookup blocks access when no override** — Go unit test
   - Given: context has no JWT role, no DB override for the user
   - When: `RequireRole("instance_admin", checker)(next)` handles request
   - Then: 403 M_FORBIDDEN, next handler NOT called

8. **Override lookup is cached (60s TTL)** — Go unit test
   - Given: mock DB checker returns override on first call
   - When: two consecutive requests within 60 seconds
   - Then: DB checker is called exactly once (cache hit on second request)

## Tasks / Subtasks

- [x] Write migration `000035_role_overrides.up.sql` (AC: #1)
  - [x] `CREATE TABLE role_overrides (user_id TEXT NOT NULL, role TEXT NOT NULL, granted_by TEXT NOT NULL, granted_at TIMESTAMPTZ DEFAULT NOW(), PRIMARY KEY (user_id, role), CONSTRAINT role_overrides_role_check CHECK (role IN ('instance_admin', 'compliance_officer')))`
  - [x] Write corresponding `.down.sql` (DROP TABLE role_overrides)

- [x] Write FAILING tests first — `gateway/internal/api/roles_handler_test.go` (AC: #7, Acceptance Tests #1–#5)
  - [x] All test cases for grant/revoke including audit log verification

- [x] Write FAILING tests first — `gateway/internal/api/middleware_role_override_test.go` (AC: #8, Acceptance Tests #6–#8)
  - [x] DB lookup allows access, blocks access, caching behavior

- [x] Update `gateway/api/openapi.yaml` (AC: #6)
  - [x] Add `AssignUserRoleRequest` schema: `{"role": string, "action": string}`
  - [x] Add `AssignUserRoleResponse` schema: `{"data": {"user_id": string, "role": string, "action": string}}`
  - [x] Add `POST /admin/users/{userId}/roles` path
  - [x] Run `make gen-api` to regenerate `api_gen.go`

- [x] Create `RoleOverrideRepository` interface + implementation in `gateway/internal/api/roles_repo.go` (AC: #2)
  - [x] `GrantRoleOverride(ctx, userID, role, grantedBy string) error`
  - [x] `RevokeRoleOverride(ctx, userID, role string) error` — returns `ErrRoleOverrideNotFound` if no row
  - [x] `GetRoleOverrides(ctx, userID string) ([]string, error)` — returns list of roles for user
  - [x] `GetAllRoleOverridesForUsers(ctx, userIDs []string) (map[string][]string, error)` — batch for ListUsers

- [x] Extend `RequireRole` in `gateway/internal/api/middleware.go` (AC: #3)
  - [x] Add `RoleOverrideChecker` interface: `HasRoleOverride(ctx context.Context, userID, role string) (bool, error)`
  - [x] Change `RequireRole` signature to `RequireRole(role string, checker RoleOverrideChecker) func(http.Handler) http.Handler`
  - [x] Add per-instance `sync.Map` cache (60s TTL) inside `RequireRole` closure — same pattern as `makeStatusChecker` in `auth.go`
  - [x] Fall back to DB check when JWT role doesn't match
  - [x] Update all call sites: `router.go` (pass checker), `router_test.go` (pass nil), `middleware_test.go` (pass nil), `deactivation_handler_test.go` (pass nil), `users_handler_test.go` (pass nil)

- [x] Implement `AssignAdminUserRole` handler in `gateway/internal/api/server.go` (AC: #2)
  - [x] Add `Roles RoleOverrideRepository` field to `AdminServer`
  - [x] Validate body: role must be `instance_admin` or `compliance_officer`; action must be `grant` or `revoke`
  - [x] Self-revoke protection: compare `userId` path value with `ContextKeyUserID` from context
  - [x] Call `audit.LogEvent` on success
  - [x] Returns 501 if `Roles` repository is nil (stub safety)

- [x] Register new route in `gateway/internal/api/router.go` (AC: #6)
  - [x] `POST /api/v1/admin/users/{userId}/roles` — `instance_admin` required

- [x] Update `users_repo.go` to merge role_overrides into `roles` field (AC: #4, #5)
  - [x] `ListUsers`: batch-load overrides via `GetAllRoleOverridesForUsers`; merge into each user's `Roles` slice
  - [x] `GetUser`: load overrides via `GetRoleOverrides`; merge into `Roles` slice (dedup)

- [x] Wire in `gateway/cmd/gateway/main.go`:
  - [x] Instantiate `rolesRepo := apihandler.NewRoleOverrideRepo(bootstrapDB)`
  - [x] Add `Roles: rolesRepo` field to `adminSrv`
  - [x] Pass `rolesRepo` as checker to `RegisterAdminRoutes` (satisfies `RoleOverrideChecker` via structural typing)

- [x] Run `make test-unit-go` — zero failures (AC: #9)

## Dev Notes

### Critical: Database Schema After This Story

New table from migration `000035`:
```sql
CREATE TABLE role_overrides (
    user_id    TEXT NOT NULL,
    role       TEXT NOT NULL,
    granted_by TEXT NOT NULL,
    granted_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (user_id, role),
    CONSTRAINT role_overrides_role_check CHECK (role IN ('instance_admin', 'compliance_officer'))
);
```

Note: `granted_at TIMESTAMPTZ` — NOT epoch ms. This table uses standard PostgreSQL timestamp type, not the BIGINT epoch ms used in the `users` table. This is intentional (no legacy constraint on this new table).

The epic spec says `PRIMARY KEY (user_id, role)` — this means a user can only have ONE override per role (idempotent grant via ON CONFLICT DO UPDATE).

### Critical: RequireRole Signature Change

The current `RequireRole` signature (Story 6.3):
```go
func RequireRole(role string) func(http.Handler) http.Handler
```

After Story 6.6:
```go
func RequireRole(role string, checker RoleOverrideChecker) func(http.Handler) http.Handler
```

`RoleOverrideChecker` interface:
```go
// RoleOverrideChecker checks if a user has a role override in the database.
// Pass nil to disable DB override checking (JWT-only mode — for tests and stages
// where role_overrides table does not exist yet).
type RoleOverrideChecker interface {
    HasRoleOverride(ctx context.Context, userID, role string) (bool, error)
}
```

**Impact on existing tests in `middleware_test.go`:** All existing `RequireRole` call sites in tests pass `nil` as checker. Nil checker means: no DB lookup, JWT-only mode. The existing test behavior is preserved.

**Impact on `router.go`:** All `RequireRole("instance_admin")` calls become `RequireRole("instance_admin", checker)` where `checker` is injected via the `RegisterAdminRoutes` function signature. Update `RegisterAdminRoutes`:
```go
func RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler, checker api.RoleOverrideChecker)
```

Or pass checker via `AdminServer` struct — the simpler option is to pass it via a new field on `AdminServer` and have `router.go` pull it from `adminServer.Roles`. Either approach works; the recommended approach is to create a dedicated `DBRoleOverrideChecker` type that implements both `RoleOverrideChecker` and the repo's `HasRoleOverride` method, and pass it in the same spot.

### Critical: Cache Pattern (Copy from WithUserStatusCheck)

The 60s TTL cache in `RequireRole` must follow the same per-instance `sync.Map` pattern from `gateway/internal/middleware/auth.go`:

```go
func RequireRole(role string, checker RoleOverrideChecker) func(http.Handler) http.Handler {
    // Per-instance cache — not package-level! One cache per RequireRole call.
    var cache sync.Map
    var checkFn func(ctx context.Context, userID string) (bool, error)
    if checker != nil {
        checkFn = func(ctx context.Context, userID string) (bool, error) {
            cacheKey := userID + ":" + role
            if v, ok := cache.Load(cacheKey); ok {
                entry := v.(roleOverrideCacheEntry)
                if time.Now().Before(entry.expiresAt) {
                    return entry.hasRole, nil
                }
                cache.Delete(cacheKey)
            }
            hasRole, err := checker.HasRoleOverride(ctx, userID, role)
            if err != nil {
                return false, err // caller handles fail-open
            }
            cache.Store(cacheKey, roleOverrideCacheEntry{
                hasRole:   hasRole,
                expiresAt: time.Now().Add(60 * time.Second),
            })
            return hasRole, nil
        }
    }
    // ...
}
```

Cache key must be `userID + ":" + role` (not just `userID`) since one `RequireRole` instance is per-role, but it's safer to include role in the key in case the same cache is ever shared.

### Critical: Self-Revoke Protection Logic

```go
// Self-revoke check must run BEFORE the DB call.
actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
if action == "revoke" && role == "instance_admin" && actorID == req.UserId {
    return &assignRole403Resp{msg: "Cannot revoke your own admin role"}, nil
}
```

The `actorID` is the Matrix user ID of the requesting admin (from JWT context). The `req.UserId` is the path parameter. Both are Matrix user IDs (`@localpart:server`). The comparison is exact string equality.

Note: the spec says "admin cannot revoke their own `instance_admin` role". This check is ONLY for action=`revoke` + role=`instance_admin`. Revoking own `compliance_officer` is allowed. Granting own roles is allowed.

### Critical: Roles Merge Logic in users_repo.go

Both `ListUsers` and `GetUser` must merge JWT system_role + DB role_overrides into the `Roles` field:

```go
// After fetching user row(s):
// roles: deduplicated union of system_role + role_overrides for user

// For ListUsers — batch approach to avoid N+1:
//   1. Collect all userIDs from the page
//   2. Call GetAllRoleOverridesForUsers(ctx, userIDs) → map[userID][]string
//   3. For each user, merge: roles = dedup(append([]string{systemRole}, overrides[uid]...))

// For GetUser — single user:
//   1. Call GetRoleOverrides(ctx, userID) → []string
//   2. Merge: roles = dedup(append([]string{systemRole}, overrides...))
```

Dedup function:
```go
func dedupeRoles(roles []string) []string {
    seen := make(map[string]bool)
    out := roles[:0]
    for _, r := range roles {
        if !seen[r] {
            seen[r] = true
            out = append(out, r)
        }
    }
    return out
}
```

Empty `system_role` (e.g., `""`) should NOT be included in the `roles` array. Filter it:
```go
if systemRole != "" {
    roles = append(roles, systemRole)
}
roles = append(roles, overrides...)
```

### Critical: OpenAPI Changes and make gen-api

Add to `gateway/api/openapi.yaml` under `components.schemas`:
```yaml
AssignUserRoleRequest:
  type: object
  required:
    - role
    - action
  properties:
    role:
      type: string
      enum: [instance_admin, compliance_officer]
    action:
      type: string
      enum: [grant, revoke]
AssignUserRoleResponse:
  type: object
  required:
    - data
  properties:
    data:
      type: object
      required:
        - user_id
        - role
        - action
      properties:
        user_id:
          type: string
        role:
          type: string
        action:
          type: string
```

Add to `paths`:
```yaml
/admin/users/{userId}/roles:
  post:
    operationId: AssignAdminUserRole
    summary: Grant or revoke a role override for a user (instance_admin required)
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
            $ref: "#/components/schemas/AssignUserRoleRequest"
    responses:
      "200":
        description: Role assigned or revoked
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/AssignUserRoleResponse"
      "400":
        description: Bad request (invalid role or action)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "403":
        description: Forbidden (self-revoke protection)
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "404":
        description: User or role override not found
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/EmptyResponse"
      "501":
        description: Not implemented
```

After editing, run `make gen-api` inside the Docker container. The generated `api_gen.go` will include:
- `AssignAdminUserRoleRequestObject` (body parsed from JSON)
- `AssignAdminUserRoleResponseObject` interface
- `AssignAdminUserRole200JSONResponse`, etc.
- `AssignAdminUserRole501Response` stub

### Critical: StrictServerInterface Compliance

`AdminServer` must implement `AssignAdminUserRole` once the OpenAPI spec is updated and `api_gen.go` is regenerated. The compile-time check is:
```go
var _ StrictServerInterface = (*AdminServer)(nil)
```

Until the handler is implemented, this will fail. Add the handler to `server.go` immediately after `make gen-api`.

### Critical: main.go Wiring Changes

The `adminSrv` creation (currently at line ~1123 in `main.go`) must add the `Roles` field:
```go
rolesRepo := apihandler.NewRoleOverrideRepo(bootstrapDB)
adminSrv := &apihandler.AdminServer{
    DB:           bootstrapDB,
    CoreClient:   coreClient.CoreServiceClient(),
    Users:        apihandler.NewUserRepo(bootstrapDB),
    Deactivation: apihandler.NewDeactivationRepo(bootstrapDB),
    Roles:        rolesRepo, // Story 6.6
}
```

The `RegisterAdminRoutes` call must also pass the checker. The recommended pattern:
- Add a `RoleOverrideChecker` field to `AdminServer` (or create a thin wrapper)
- In `router.go`, read `adminServer.Roles` as the checker directly if `RoleOverrideRepository` also implements `RoleOverrideChecker`

The simplest solution: make `dbRoleOverrideRepo` implement BOTH the full `RoleOverrideRepository` (for handler operations) AND the lighter `RoleOverrideChecker` interface (for middleware). Go structural typing makes this zero-cost.

### Critical: Test Strategy for middleware_test.go (Existing Tests)

Existing tests in `middleware_test.go` call `RequireRole("instance_admin")` with ONE argument. After this story, `RequireRole` takes TWO arguments. You MUST update all existing test call sites to `RequireRole("instance_admin", nil)`. Passing `nil` for checker preserves existing test behavior (JWT-claim-only mode).

**Do NOT change the test assertions** — the existing tests cover AC#2 from Story 6.3 and must still pass.

### File Inventory

**Files to CREATE (new):**
- `gateway/migrations/000035_role_overrides.up.sql`
- `gateway/migrations/000035_role_overrides.down.sql`
- `gateway/internal/api/roles_repo.go` — `RoleOverrideRepository` interface + `dbRoleOverrideRepo` implementation
- `gateway/internal/api/roles_handler_test.go` — ATDD tests for AC#7 (written first)
- `gateway/internal/api/middleware_role_override_test.go` — ATDD tests for AC#8 (written first)

**Files to UPDATE:**
- `gateway/api/openapi.yaml` — add `AssignAdminUserRole` endpoint + schemas
- `gateway/api/api_gen.go` — regenerated via `make gen-api` (do not edit manually)
- `gateway/internal/api/middleware.go` — extend `RequireRole` with `RoleOverrideChecker` param + 60s TTL cache
- `gateway/internal/api/server.go` — add `Roles RoleOverrideRepository` field + `AssignAdminUserRole` handler
- `gateway/internal/api/router.go` — update `RegisterAdminRoutes` to pass checker to `RequireRole`; register new POST route
- `gateway/internal/api/middleware_test.go` — update existing `RequireRole(...)` calls to `RequireRole(..., nil)`
- `gateway/internal/api/router_test.go` — update if `RegisterAdminRoutes` signature changes
- `gateway/internal/api/users_repo.go` — merge role_overrides into `roles` field in `ListUsers` + `GetUser`
- `gateway/cmd/gateway/main.go` — wire `Roles` field in `adminSrv`

### Existing Code Context: audit.LogEvent

The audit log pattern (already used in Story 6.4 and 6.5):
```go
if s.CoreClient != nil {
    actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
    _ = audit.LogEvent(ctx, s.CoreClient, actorID, "role_granted", "user", userID,
        map[string]any{"role": role}, "success", "")
}
```

For revoke: action = `"role_revoked"`.

### Existing Code Context: Error Sentinels

Follow the `ErrUserNotFound` pattern from `deactivation_repo.go`:
```go
// In roles_repo.go:
var ErrRoleOverrideNotFound = errors.New("role override not found")
```

The handler returns `404 M_NOT_FOUND "Role override not found"` when `RevokeRoleOverride` returns `ErrRoleOverrideNotFound`.

The handler also returns `404 M_NOT_FOUND "User not found"` when the target `userId` does not exist in the `users` table. The simplest implementation is to check `users` table existence by calling the existing `DeactivationRepository.GetUserStatus` (which returns `ErrUserNotFound`) — or add a `UserExists(ctx, userID) (bool, error)` helper to `RoleOverrideRepository`. Either approach is acceptable; re-using `DeactivationRepository.GetUserStatus` avoids duplicating SQL.

### Previous Story Learnings (6-5)

1. **MINOR-6 fix (6.5 code review):** The strict handler's `json.NewDecoder` returns plain-text HTTP errors on missing body. Pre-check in the router wrapper before delegating, as done in `deactivateAdminUserHandler`. Do the same for `assignUserRoleHandler` in `router.go`.

2. **MINOR-1 (6.5):** Always log a warning when `CoreClient` is nil and audit logging is skipped. Use `slog.Warn("AssignAdminUserRole audit skipped — CoreClient is nil", ...)`.

3. **Pattern for router wrapper functions:** Follow the established pattern in `router.go`: each non-trivial route gets a named wrapper function (`assignAdminUserRoleHandler(sh ServerInterface) http.Handler`) that extracts path values and delegates to `sh.AssignAdminUserRole(w, r, userId)`.

4. **The `var _ StrictServerInterface = (*AdminServer)(nil)` compile check** catches missing implementations immediately. After `make gen-api`, this will fail until you add `AssignAdminUserRole` to `server.go`.

5. **router_test.go:** Story 6.3 tests use `AdminServer{}` (empty struct). After adding `Roles` field, ensure the empty struct still compiles and routes still return 501 (stub safety pattern is in place in `server.go`).

### Git Context

Recent commits relevant to this story:
- `012bce0 feat(6-5): User Deactivation + Reactivation + Session Invalidierung` — established the `Deactivation` field pattern on `AdminServer`, the per-instance `sync.Map` TTL cache pattern in `WithUserStatusCheck`, and the router wrapper + handler decomposition pattern
- `ed4f383 feat(6-4): User List + Get API` — established `UserRepository`, `roles: []string{systemRole}` in user rows (to be extended here)
- `e46b90d feat(6-3): Admin API Router + RequireRole middleware` — `RequireRole` currently takes one argument; this story extends it to two

## Dev Agent Record

### Completion Notes

- Migration `000035_role_overrides.up.sql` created with PRIMARY KEY (user_id, role), CHECK constraint for valid roles, and index on user_id. Corresponding down.sql drops the table.
- ATDD tests written first (RED phase): 10 tests in `roles_handler_test.go` (all `t.Skip`), 9 tests in `middleware_role_override_test.go` (all `t.Skip`).
- `roles_repo.go` created: `RoleOverrideRepository` and `RoleOverrideChecker` interfaces; `dbRoleOverrideRepo` implements both (structural typing). `RevokeRoleOverride` uses `RowsAffected()` to detect not-found and return `ErrRoleOverrideNotFound`. `GetAllRoleOverridesForUsers` uses `ANY($1)` with a `pq.Array` for batch lookup.
- OpenAPI spec updated with `AssignUserRoleRequest` (role+action enums), `AssignUserRoleResponse` (data object), and `POST /admin/users/{userId}/roles` path. `make gen-api` regenerated typed Go enums.
- `RequireRole` middleware restructured: DB override slow path now runs when `checkFn != nil` and `userID != ""` regardless of whether JWT systemRole is absent or wrong. 401 (M_MISSING_TOKEN) is only returned in JWT-only mode (nil checker) when systemRole is empty. This preserves Story 6.3 backward compatibility.
- `AssignAdminUserRole` handler implemented with full validation order: nil Roles → 501; missing/invalid body → 400; UserExists → 404; self-revoke protection (actor==userId && role==instance_admin && action==revoke) → 403; grant/revoke operations with ErrRoleOverrideNotFound → 404; audit log (fail-soft, warn if CoreClient nil).
- All existing test call sites updated: `router_test.go` (6 calls), `middleware_test.go` (6 calls), `deactivation_handler_test.go` (16 calls), `users_handler_test.go` (15 calls) — all now pass `nil` as second arg to `RequireRole`/4th arg to `RegisterAdminRoutes`.
- `mockRoleOverrideRepository` in `roles_handler_test.go` was missing `HasRoleOverride` method — added to satisfy `RoleOverrideRepository` interface.
- `users_repo.go` updated: `NewUserRepoWithRoles` constructor added; `ListUsers` batch-loads role overrides via `GetAllRoleOverridesForUsers` and merges with `mergeRoles` helper (dedup); `GetUser` merges via `GetRoleOverrides`. Both fail-open on DB error (return user with system_role only).
- `main.go` updated: `rolesRepo` instantiated via `NewRoleOverrideRepo`; passed to `NewUserRepoWithRoles` and as `Roles` field on `adminSrv`; passed as 4th arg to `RegisterAdminRoutes` (satisfies `RoleOverrideChecker`).
- GREEN phase: `t.Skip` calls removed from both test files; all 17 packages pass `make test-unit-go` with zero failures (including race detector).

### File List

| File | Action | Notes |
|------|--------|-------|
| `gateway/migrations/000035_role_overrides.up.sql` | CREATE | role_overrides table schema |
| `gateway/migrations/000035_role_overrides.down.sql` | CREATE | DROP TABLE role_overrides |
| `gateway/internal/api/roles_repo.go` | CREATE | RoleOverrideRepository + RoleOverrideChecker interfaces + dbRoleOverrideRepo |
| `gateway/internal/api/roles_handler_test.go` | CREATE | ATDD tests for AC#7 (10 tests, GREEN phase) |
| `gateway/internal/api/middleware_role_override_test.go` | CREATE | ATDD tests for AC#8 (9 tests, GREEN phase) |
| `gateway/api/openapi.yaml` | UPDATE | AssignUserRoleRequest/Response schemas + POST route |
| `gateway/internal/api/api_gen.go` | REGEN | Regenerated via make gen-api (typed enums for role/action) |
| `gateway/internal/api/middleware.go` | UPDATE | RequireRole extended with RoleOverrideChecker param + 60s TTL cache + restructured logic |
| `gateway/internal/api/server.go` | UPDATE | Roles field on AdminServer; AssignAdminUserRole handler |
| `gateway/internal/api/router.go` | UPDATE | RegisterAdminRoutes passes checker; POST /api/v1/admin/users/{userId}/roles registered |
| `gateway/internal/api/middleware_test.go` | UPDATE | RequireRole calls updated to pass nil as 2nd arg (6 occurrences) |
| `gateway/internal/api/router_test.go` | UPDATE | RegisterAdminRoutes calls updated to pass nil as 4th arg (6 occurrences) |
| `gateway/internal/api/deactivation_handler_test.go` | UPDATE | RegisterAdminRoutes calls updated to pass nil as 4th arg (16 occurrences) |
| `gateway/internal/api/users_handler_test.go` | UPDATE | RegisterAdminRoutes calls updated to pass nil as 4th arg (15 occurrences) |
| `gateway/internal/api/users_repo.go` | UPDATE | NewUserRepoWithRoles; ListUsers+GetUser merge role_overrides; mergeRoles helper |
| `gateway/cmd/gateway/main.go` | UPDATE | rolesRepo wired to adminSrv.Roles, NewUserRepoWithRoles, RegisterAdminRoutes 4th arg |

## Change Log

| Date | Author | Change |
|------|--------|--------|
| 2026-05-01 | Story context engine | Story created from epics.md AC analysis + full codebase read |
| 2026-05-01 | Dev agent (Amelia) | Full implementation: migration, repo, middleware extension, handler, router, test wiring, main.go — all ACs satisfied; 17/17 packages green |

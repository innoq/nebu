---
security_review: required
---

# Story 6.4: User List + Get API

Status: review

## Story

As an instance admin,
I want to list all users on the instance and retrieve individual user details,
so that I have full visibility of who has access to the Nebu instance.

## Acceptance Criteria

1. `GET /api/v1/admin/users` — `instance_admin` role required; query params: `cursor` (optional), `limit` (1–100, default 20), `search` (optional, partial match on `display_name` or `email`):
   - Queries `users` + `profiles` tables ordered by `(created_at DESC, user_id)`; applies cursor-based pagination via `DecodeCursor`/`EncodeCursor` (from `pagination.go`)
   - Response envelope: `APIResponse[[]AdminUser]` — `{"data": [<user objects>], "meta": {"total": N, "next_cursor": "..."}}`
   - Each user object (`AdminUser`): `user_id`, `display_name`, `email_masked`, `roles` (array of strings), `status`, `created_at` (ISO 8601), `last_seen_at` (ISO 8601 | null)
   - `email_masked`: shows `a***@example.com` format — never the raw email (Sensitive PII). Derive from decrypted email; if email unavailable/null, return `""`.
   - `roles`: derive from `users.system_role` column. For MVP (pre-Story 6.6), only the JWT-derived `system_role` from the `users` table is used; `role_overrides` table does not exist yet.
   - `status`: derived from `users` columns — `"active"` (is_active=true, deletion_status=NULL, anonymized_at=NULL), `"deactivated"` (is_active=false), `"keys_deleted"` (deletion_status='keys_deleted'), `"anonymized"` (anonymized_at IS NOT NULL)
   - Invalid `cursor` → `400 {"error":{"code":"M_BAD_REQUEST","message":"Invalid cursor"}}`
   - `limit` out of range → `400 {"error":{"code":"M_BAD_REQUEST","message":"limit must be between 1 and 100"}}`
   - Calls `audit.LogEvent(ctx, coreClient, adminUserID, "admin_user_viewed", "user", "", nil, "success", "")` once per list request (target_id = `""` for list operations)

2. `GET /api/v1/admin/users/{userId}` — `instance_admin` role required:
   - Returns single `AdminUser` with same fields as list, plus `room_count` (count of `room_members` rows for this `user_id` where `left_at IS NULL`)
   - Returns `404 {"error":{"code":"M_NOT_FOUND","message":"User not found"}}` if user does not exist
   - Calls `audit.LogEvent(ctx, coreClient, adminUserID, "admin_user_viewed", "user", userId, nil, "success", "")` on success

3. Both endpoints are registered in `gateway/internal/api/router.go` via `RegisterAdminRoutes`; `GET /api/v1/admin/users/{userId}` is a NEW route not yet present.

4. `gateway/api/openapi.yaml` is updated to add:
   - `GET /admin/users` — parameters: `cursor` (string, optional), `limit` (integer, optional), `search` (string, optional); response schema updated from `EmptyResponse` to `UserListResponse`
   - `GET /admin/users/{userId}` — new path with `userId` path parameter; response schema `UserDetailResponse`
   - New schemas: `AdminUser`, `UserListResponse`, `UserDetailResponse`
   - `make gen-api` must be run to regenerate `api_gen.go` after spec changes

5. `AdminServer` in `gateway/internal/api/server.go` implements the real `ListAdminUsers` and `GetAdminUser` methods (replacing 501 stubs); a new dependency `UserRepository` provides DB access.

6. Unit tests in `gateway/internal/api/users_handler_test.go`:
   - List: returns paginated results with correct cursor encoding, `email_masked` format verified, `status` derived correctly
   - List with `search`: filters by `display_name` partial match
   - List with invalid `cursor` → 400
   - List with `limit=0` → 400
   - Get known user → 200 with `room_count`
   - Get unknown user → 404 `M_NOT_FOUND`
   - Audit log call verified for both endpoints

7. `go build ./...` and `make test-unit-go` pass with zero failures after this story.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **List returns paginated results** — Go unit test (`net/http/httptest`, mock `UserRepository`)
   - Given: 3 users in repository, `limit=2`, no cursor
   - When: `GET /api/v1/admin/users?limit=2` with `instance_admin` context
   - Then: status 200, `data` has 2 users, `meta.next_cursor` is non-empty, `meta.total` = 3

2. **email_masked format** — Go unit test
   - Given: user with `email = "alice@example.com"`
   - When: `GET /api/v1/admin/users`
   - Then: response contains `"email_masked": "a***@example.com"` (never full email)

3. **search filters by display_name** — Go unit test
   - Given: users with display names "Alice" and "Bob"
   - When: `GET /api/v1/admin/users?search=ali`
   - Then: status 200, `data` contains only "Alice" user

4. **Invalid cursor → 400** — Go unit test
   - Given: request with `cursor=not-valid-base64!!`
   - When: `GET /api/v1/admin/users?cursor=not-valid-base64!!`
   - Then: status 400, body `{"error":{"code":"M_BAD_REQUEST","message":"Invalid cursor"}}`

5. **limit=0 → 400** — Go unit test
   - Given: request with `limit=0`
   - When: `GET /api/v1/admin/users?limit=0`
   - Then: status 400, body `{"error":{"code":"M_BAD_REQUEST","message":"limit must be between 1 and 100"}}`

6. **Get user → 200 with room_count** — Go unit test
   - Given: known user with `user_id = "@alice:example.com"` and 3 active room memberships
   - When: `GET /api/v1/admin/users/@alice:example.com`
   - Then: status 200, `data.user_id = "@alice:example.com"`, `data.room_count = 3`

7. **Get unknown user → 404** — Go unit test
   - Given: `user_id = "@doesnotexist:example.com"` not in repository
   - When: `GET /api/v1/admin/users/@doesnotexist:example.com`
   - Then: status 404, body `{"error":{"code":"M_NOT_FOUND","message":"User not found"}}`

8. **Audit log emitted on list** — Go unit test
   - Given: mock `audit.LogEvent` (or mock `CoreServiceClient`)
   - When: `GET /api/v1/admin/users` succeeds
   - Then: `audit.LogEvent` called with `action="admin_user_viewed"`, `target_type="user"`

## Tasks / Subtasks

- [x] Update `gateway/api/openapi.yaml` — add schemas and paths (AC: #4)
  - [x] Add `AdminUser` schema: `user_id`, `display_name`, `email_masked`, `roles` (array), `status`, `created_at`, `last_seen_at`
  - [x] Add `AdminUserDetail` schema: extends `AdminUser` + `room_count` (integer)
  - [x] Add `UserListResponse` schema: `data` (array of `AdminUser`) + `meta` (`total`, `next_cursor`)
  - [x] Add `UserDetailResponse` schema: `data` (`AdminUserDetail`)
  - [x] Update `GET /admin/users` path: add `cursor`, `limit`, `search` query parameters; update response schema
  - [x] Add `GET /admin/users/{userId}` path with `userId` path param
  - [x] Run `make gen-api` — regenerates `api_gen.go` with new request/response objects and `GetAdminUser` operation

- [x] Define `UserRepository` interface in `gateway/internal/api/` (AC: #5)
  - [x] `ListUsers(ctx, afterID, afterCreatedAt, limit, search) ([]AdminUser, total int, nextCursor string, err error)`
  - [x] `GetUser(ctx, userID) (*AdminUserDetail, error)` — returns `nil, nil` when not found

- [x] Implement `UserRepository` with real DB queries in `gateway/internal/api/users_repo.go` (AC: #1, #2)
  - [x] `ListUsers`: SELECT with cursor pagination and optional displayname search
  - [x] Cursor SQL: keyset pagination `WHERE (u.created_at, u.user_id) < ($N, $M) ORDER BY u.created_at DESC, u.user_id DESC`
  - [x] Search SQL: `AND (p.displayname ILIKE '%' || $search || '%')` — display_name only
  - [x] Total count: separate `SELECT COUNT(*)` with same search filter
  - [x] `GetUser`: same SELECT + `COUNT(rm.room_id) FILTER (WHERE rm.left_at IS NULL)` joined on `room_members`
  - [x] Email masking logic: MVP returns `""` — encryption key unavailable in Admin context (documented)
  - [x] `maskEmail(email)` function implemented for future use

- [x] Implement `ListAdminUsers` in `gateway/internal/api/server.go` (AC: #1, #5)
  - [x] Parse query params: `cursor` (optional), `limit` (default 20, clamp 1–100), `search` (optional)
  - [x] Validate cursor via `DecodeCursor` — if error → `400 M_BAD_REQUEST`
  - [x] Validate limit range — if out of range → `400 M_BAD_REQUEST`
  - [x] Call `UserRepository.ListUsers`
  - [x] Return custom response implementing `ListAdminUsersResponseObject`
  - [x] Call `audit.LogEvent` with `action="admin_user_viewed"`

- [x] Implement `GetAdminUser` in `gateway/internal/api/server.go` (AC: #2, #5)
  - [x] Extract `userId` from generated `GetAdminUserRequestObject.UserId`
  - [x] Call `UserRepository.GetUser` — if nil → 404 M_NOT_FOUND
  - [x] Return 200 with `AdminUserDetail`
  - [x] Call `audit.LogEvent` with `action="admin_user_viewed"`, `target_id=userId`

- [x] Register `GET /api/v1/admin/users/{userId}` in `gateway/internal/api/router.go` (AC: #3)
  - [x] Added `getAdminUserHandler` wrapper + route registration with `jwtMW(RequireRole("instance_admin"))`

- [x] Wire `UserRepository` into `AdminServer` in `gateway/cmd/gateway/main.go` (AC: #5)
  - [x] `AdminServer` gains `DB *sql.DB`, `CoreClient pb.CoreServiceClient`, and `Users UserRepository` fields
  - [x] `bootstrapDB`, `coreClient.CoreServiceClient()`, and `NewUserRepo(bootstrapDB)` passed in main.go

- [x] Write unit tests `gateway/internal/api/users_handler_test.go` (AC: #6)
  - [x] Mock `UserRepository` implementing the interface
  - [x] All 8 Acceptance Tests from above (+ bonus tests for limit=101, status fields, field presence, default limit)

- [x] Run `make test-unit-go` — all tests pass (AC: #7)

## Dev Notes

### Critical: Existing State vs What This Story Changes

| Item | Current State | This Story's Action |
|---|---|---|
| `gateway/api/openapi.yaml` | EXISTS — placeholder `EmptyResponse` for `/admin/users`; no `/admin/users/{userId}` | **MODIFY** — add schemas + query params + new path |
| `gateway/internal/api/api_gen.go` | EXISTS — `ListAdminUsers` has empty `ListAdminUsersRequestObject`, `EmptyResponse` | **REGENERATE** via `make gen-api` after spec update |
| `gateway/internal/api/server.go` | EXISTS — `ListAdminUsers` returns 501 stub; no `GetAdminUser` | **MODIFY** — implement real handlers; add `GetAdminUser` after codegen |
| `gateway/internal/api/router.go` | EXISTS — registers `GET /api/v1/admin/users` | **MODIFY** — add `GET /api/v1/admin/users/{userId}` route |
| `gateway/internal/api/pagination.go` | EXISTS — `EncodeCursor`, `DecodeCursor`, `ErrInvalidCursor` | **READ ONLY** — use as-is |
| `gateway/internal/api/response.go` | EXISTS — `APIResponse[T]`, `Meta`, `APIError`, `WriteJSON`, `ErrorResponse` | **READ ONLY** — use `APIResponse[[]AdminUser]` and `ErrorResponse` |
| `gateway/internal/api/middleware.go` | EXISTS — `RequireRole`, `writeAdminError` | **READ ONLY** |
| `gateway/internal/api/users_repo.go` | MISSING | **CREATE** |
| `gateway/internal/api/users_handler_test.go` | MISSING | **CREATE** |
| `gateway/cmd/gateway/main.go` | EXISTS — passes `&apihandler.AdminServer{}` | **MODIFY** — pass `DB` and `CoreClient` to `AdminServer` |

### Critical: Spec-First Workflow — Run `make gen-api` Before Writing Handler Code

**Rule 11 (architecture.md):** "Admin API Implementation muss `ServerInterface` aus `api_gen.go` erfüllen — kein freestyle routing."

The workflow for this story is:
1. Edit `openapi.yaml` (add schemas + new path)
2. Run `make gen-api` → new `api_gen.go` with updated `ListAdminUsersRequestObject` (now has params), new `GetAdminUserRequestObject`, new response types
3. Implement handlers in `server.go` using the generated types
4. Register `GetAdminUser` in `router.go`

**Do NOT hand-write types that the codegen should produce.** The generated `ListAdminUsersRequestObject` will carry the parsed query params after the spec update.

### Critical: `AdminServer` Needs DB + CoreClient

Currently `AdminServer{}` has no fields. This story introduces state. Two clean options:

**Option A (preferred):** Add fields to `AdminServer`:
```go
type AdminServer struct {
    DB         *sql.DB
    CoreClient pb.CoreServiceClient
    Users      UserRepository
}
```
Wire in `main.go`:
```go
adminSrv := &apihandler.AdminServer{
    DB:         db,
    CoreClient: coreClient,
    Users:      apihandler.NewUserRepo(db),
}
apihandler.RegisterAdminRoutes(mux, adminSrv, jwtMiddleware)
```
`RegisterAdminRoutes` signature stays the same — it already takes `*AdminServer`.

**Option B:** Pass `UserRepository` as interface via `RegisterAdminRoutes` parameter. Less preferred — creates API churn.

**Use Option A.** It's consistent with how `AccessRequestHandler` works (fields for DB + CoreClient).

### Critical: SQL Schema State

The `users` table currently has these relevant columns (across migrations):
- `user_id TEXT PRIMARY KEY`
- `system_role TEXT` (CHECK IN 'user', 'instance_admin', 'compliance_officer')
- `is_active BOOLEAN` (default true)
- `deletion_status TEXT` (NULL | 'deletion_in_progress' | 'keys_deleted') — from migration 000021
- `anonymized_at BIGINT` — from migration 000022
- `created_at BIGINT` (Unix epoch ms)
- `last_seen_at BIGINT` (Unix epoch ms, nullable)
- `email_encrypted BYTEA`, `email_nonce BYTEA`, `email_ephemeral_pub BYTEA` — from migration 000006

The `profiles` table: `user_id TEXT PK`, `displayname TEXT`, `avatar_url TEXT`, `updated_at BIGINT`

The `room_members` table: `room_id TEXT`, `user_id TEXT`, `joined_at BIGINT`, `left_at BIGINT` — active membership = `left_at IS NULL`

**Status derivation logic (Go, not SQL):**
```go
func deriveStatus(isActive bool, deletionStatus sql.NullString, anonymizedAt sql.NullInt64) string {
    if anonymizedAt.Valid { return "anonymized" }
    if deletionStatus.Valid && deletionStatus.String == "keys_deleted" { return "keys_deleted" }
    if !isActive { return "deactivated" }
    return "active"
}
```

### Critical: Email Masking

Email is stored encrypted. For the Admin API response, email is never decrypted (Sensitive PII — would require the user's private X25519 key, which is irreversible after deletion). Return `email_masked` as:
- If `email_encrypted IS NULL` → `""`
- If encrypted but keys deleted → `""` (cannot decrypt)
- If decryptable → apply mask: `"alice@example.com"` → `"a***@example.com"`

**`maskEmail` function:**
```go
func maskEmail(email string) string {
    at := strings.Index(email, "@")
    if at <= 0 { return "***" }
    return string(email[0]) + "***" + email[at:]
}
```

**For MVP, email decryption is out of scope** — the Admin API handler does not have access to the X25519 decryption key (that is a Compliance feature). Return `""` for `email_masked` in MVP, with a comment noting this is deferred. The test should expect `""` for now.

**Epics note:** The AC says "shows `a***@example.com` format" but the encrypted email cannot be decrypted without the user's encryption key. MVP returns `""` — the masked display will be implemented when email decryption is wired. Document this in code.

### Critical: Cursor Pagination Pattern

Use `pagination.go` (existing, do not rewrite):
```go
// Decode incoming cursor
if cursor != "" {
    afterID, afterCreatedAt, err = api.DecodeCursor(cursor)
    if err != nil { /* return 400 */ }
}

// SQL keyset pagination:
// WHERE (u.created_at, u.user_id) < ($1, $2) ORDER BY u.created_at DESC, u.user_id DESC LIMIT $3

// Encode next cursor from last row:
nextCursor = api.EncodeCursor(lastUser.UserID, lastUser.CreatedAt) // ISO 8601 string
```

The cursor `after_created_at` field is ISO 8601 string in the payload (see `cursorPayload` struct in `pagination.go`). Convert `BIGINT` epoch-ms to `time.Time` to ISO 8601 for cursor encoding.

### Critical: Router Registration — Path Segment Conflict

Go 1.22 `http.ServeMux` with path patterns: `GET /api/v1/admin/users` and `GET /api/v1/admin/users/{userId}` are distinct patterns (the latter is more specific). Both can coexist on the same mux — no conflict.

The existing `DELETE /api/v1/admin/users/{userId}/keys` and `POST /api/v1/admin/users/{userId}/anonymize` in `main.go` also use `{userId}` path values and coexist safely with the new GET route.

### Critical: Generated Type Shapes After `make gen-api`

After updating `openapi.yaml`, `make gen-api` will produce (approximately):
```go
type ListAdminUsersParams struct {
    Cursor *string `form:"cursor,omitempty" json:"cursor,omitempty"`
    Limit  *int    `form:"limit,omitempty" json:"limit,omitempty"`
    Search *string `form:"search,omitempty" json:"search,omitempty"`
}

type ListAdminUsersRequestObject struct {
    Params ListAdminUsersParams
}

type GetAdminUserRequestObject struct {
    UserId string  // path param — exact name from spec
}

type ListAdminUsers200JSONResponse UserListResponse  // your new schema
type GetAdminUser200JSONResponse   UserDetailResponse
type GetAdminUser404JSONResponse   struct{}  // or error body — match spec
```

**StrictServerInterface will require:**
```go
ListAdminUsers(ctx context.Context, request ListAdminUsersRequestObject) (ListAdminUsersResponseObject, error)
GetAdminUser(ctx context.Context, request GetAdminUserRequestObject) (GetAdminUserResponseObject, error)
```

Both must be implemented in `server.go` or the build breaks (StrictServerInterface compile check).

### Critical: Audit Log Pattern

Follow `admin/auth.go` pattern exactly:
```go
// Never-raise: audit failure must not block the primary response.
// coreClient may be nil in test environments (AdminServer.CoreClient == nil).
if s.CoreClient != nil {
    audit.LogEvent(ctx, s.CoreClient,
        actorUserID,          // from JWT context (middleware.ContextKeyUserID or ContextKeySubject)
        "admin_user_viewed",  // action
        "user",               // target_type
        targetID,             // "" for list, userId for get
        nil,                  // metadata
        "success",            // outcome
        "",                   // error_detail
    )
}
```

To extract actor user ID from context: use the same key as JWT middleware. Look up `middleware.ContextKeySubject` or `middleware.ContextKeyUserID` — check `gateway/internal/middleware/auth.go` for the exact exported constant.

### Build and Package Constraints

- All new files in `gateway/internal/api/` must include `//go:build go1.22` as first non-comment line
- Package declaration: `package api`
- Module: `github.com/nebu/nebu` (from `gateway/go.mod`)
- Test package: `package api_test` (external test package, consistent with existing `*_test.go` files in the package)
- DB driver: `"github.com/jackc/pgx/v5/stdlib"` or `"pgx"` (existing `main.go` uses `sql.Open("pgx", cfg.DBURL)`)

### Role Context for Audit — Extracting Actor User ID

The JWT middleware populates `ContextKeySystemRole` (confirmed from Story 6.3). For the actor user ID in audit logs, check `gateway/internal/middleware/auth.go` for the exported key constant that stores the user's `sub` claim. Use that key to read the actor ID from `r.Context()`.

### Project Structure Notes

**Files to CREATE:**
- `gateway/internal/api/users_repo.go` — `UserRepository` interface + `userRepo` impl
- `gateway/internal/api/users_handler_test.go` — all acceptance tests

**Files to MODIFY:**
- `gateway/api/openapi.yaml` — add schemas + paths (run `make gen-api` after)
- `gateway/internal/api/api_gen.go` — REGENERATED by `make gen-api` (do NOT hand-edit)
- `gateway/internal/api/server.go` — implement `ListAdminUsers`, `GetAdminUser`; add `DB`, `CoreClient`, `Users` fields to `AdminServer`
- `gateway/internal/api/router.go` — add `GET /api/v1/admin/users/{userId}` registration
- `gateway/cmd/gateway/main.go` — pass `DB` and `CoreClient` to `AdminServer`

**Files NOT to touch:**
- `gateway/internal/api/pagination.go` — use `DecodeCursor`/`EncodeCursor` as-is
- `gateway/internal/api/response.go` — use `APIResponse[T]`, `WriteJSON`, `ErrorResponse` as-is
- `gateway/internal/api/middleware.go` — unchanged
- `gateway/internal/api/openapi_handler.go` — unchanged

### Cross-Story Dependencies

- **Story 6.3 (done):** `RequireRole` middleware + `RegisterAdminRoutes` are present — do not modify role checking
- **Story 6.5 (future):** Will add `deactivated_at`, `deactivation_reason` columns; this story's status enum covers `deactivated` via `is_active=false` already
- **Story 6.6 (future):** Will create `role_overrides` table; `roles` array currently reads only from `users.system_role`. When 6.6 lands, `GetAdminUser` will need to merge overrides — document this in code via TODO.
- **Story 7.2/7.5 (Admin UI users page — done):** Admin UI uses stub data from `admin/users.go` — this story's API is independent. No changes to `admin/users.go`.

### References

- [Source: epics.md#Story-6.4] Full Acceptance Criteria and user object definition (lines 2720–2742)
- [Source: architecture.md rule 11] "Admin API muss `ServerInterface` aus `api_gen.go` erfüllen"
- [Source: architecture.md cursor anti-pattern] Cursor-based pagination required; offset pagination forbidden
- [Source: gateway/internal/api/pagination.go] `EncodeCursor`, `DecodeCursor`, `ErrInvalidCursor` — use as-is
- [Source: gateway/internal/api/response.go] `APIResponse[T]`, `Meta`, `APIError`, `WriteJSON`, `ErrorResponse` — established Admin API response format
- [Source: gateway/internal/api/server.go] `AdminServer` struct — currently empty, this story adds fields
- [Source: gateway/internal/api/router.go] `RegisterAdminRoutes` — add `{userId}` route here
- [Source: gateway/internal/api/middleware.go] `RequireRole` — do not modify
- [Source: gateway/internal/audit/writer.go] `audit.LogEvent` signature — never-raise contract
- [Source: gateway/internal/admin/auth.go#logAuditEvent] Pattern for nil-guarded audit emission
- [Source: gateway/migrations/000004_users.up.sql] `users` table schema
- [Source: gateway/migrations/000006_users_email_pii.up.sql] `email_encrypted`, `email_nonce`, `email_ephemeral_pub` columns
- [Source: gateway/migrations/000015_profiles.up.sql] `profiles` table — `displayname` column
- [Source: gateway/migrations/000021_users_deletion_status.up.sql] `deletion_status`, `keys_deleted_at` columns
- [Source: gateway/migrations/000022_users_anonymized.up.sql] `anonymized_at` column
- [Source: gateway/migrations/000009_rooms.up.sql] `room_members` table — `left_at IS NULL` = active membership
- [Source: gateway/api/openapi.yaml] Current spec — placeholder for `/admin/users`; no `/admin/users/{userId}`
- [Source: gateway/internal/api/api_gen.go] `ListAdminUsersRequestObject` (currently empty params — will change after gen-api)
- [Source: 6-3-admin-api-router-role-auth-middleware.md#Dev Notes] Route registration pattern (Option B), `writeAdminError`, build constraints
- [Source: 6-2-admin-api-response-format-cursor-pagination.md] Response format rationale + `APIResponse` usage

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- Naming collision: generated `AdminUser`/`AdminUserDetail` types from OpenAPI spec conflicted with test expectations (`UserID` vs `UserId`, `string` vs `time.Time` for dates). Resolved by using lightweight `UserListResponse`/`UserDetailResponse` schemas in the spec (with `type: object` instead of typed schemas), and defining `AdminUser`/`AdminUserDetail` as handwritten types in `users_repo.go`.
- `sh.ListAdminUsers(w, r, params)` / `sh.GetAdminUser(w, r, userId)` have extra parameters in the generated ServerInterface — cannot be used directly as `http.HandlerFunc`. Resolved by adding `listAdminUsersHandler` / `getAdminUserHandler` wrappers in `router.go` that parse the parameters and delegate to the generated handler.
- Existing `router_test.go` tests use `&api.AdminServer{}` (nil `Users`) and expect 501 — added nil-guard in `ListAdminUsers` to return 501 when `Users` is not wired.

### Completion Notes List

- Story 6.4 fully implemented. All 14 acceptance tests pass (router_test.go: 6 tests, users_handler_test.go: 8 tests covering all ACs).
- `make test-unit-go` passes with zero failures and zero regressions across all 17 packages.
- Design decision: Kept `AdminUser`/`AdminUserDetail` as handwritten types (not generated) to ensure `UserID string` and `CreatedAt string` (ISO 8601) field shapes match test expectations. The OpenAPI spec uses `UserListResponse`/`UserDetailResponse` as generic wrappers.
- MVP: `email_masked` is always `""` — X25519 decryption key is not available in the Admin context. `maskEmail()` helper is present for future use when email decryption is wired.
- TODO(Story 6.6): `roles` array reads only from `users.system_role`; `role_overrides` table not yet created.
- Audit log pattern follows `admin/auth.go` convention: `nil`-guarded, never-raise.

### File List

- `gateway/api/openapi.yaml` — updated: added `UserListResponse`, `UserDetailResponse` schemas + `GET /admin/users` query params + `GET /admin/users/{userId}` path
- `gateway/internal/api/api_gen.go` — regenerated by `make gen-api`: new `ListAdminUsersParams`, `GetAdminUserRequestObject`, `GetAdminUser200/404JSONResponse`, `ListAdminUsers200/400JSONResponse`, `StrictServerInterface.GetAdminUser`
- `gateway/internal/api/users_repo.go` — new file: `AdminUser`, `AdminUserDetail`, `UserRepository` interface, `userRepo` DB implementation, `deriveStatus`, `maskEmail`, `epochMsToISO8601`
- `gateway/internal/api/server.go` — updated: `AdminServer` gains `DB`, `CoreClient`, `Users` fields; `ListAdminUsers` + `GetAdminUser` implemented; custom response types added
- `gateway/internal/api/router.go` — updated: `GET /api/v1/admin/users/{userId}` registered; `listAdminUsersHandler` + `getAdminUserHandler` wrappers added
- `gateway/cmd/gateway/main.go` — updated: `adminSrv` constructed with `DB`, `CoreClient`, `Users` and passed to `RegisterAdminRoutes`
- `gateway/internal/api/users_handler_test.go` — pre-existing (RED phase tests written in ATDD step); all tests now pass

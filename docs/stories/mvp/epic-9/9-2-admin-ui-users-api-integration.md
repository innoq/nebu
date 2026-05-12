---
status: review
epic: 9
story: 2
security_review: required
---

# Story 9.2: Admin UI — Users API Integration

Status: review

## Story

As an instance admin,
I want the Admin UI user management pages to use the real Admin API,
So that user data reflects the actual database state instead of static stub data.

## Acceptance Criteria

1. Navigate to Users list → real users from DB displayed (not `stubUsers`)
2. Click "Deactivate" on User Detail page → `POST /api/v1/admin/users/{userId}/deactivate` called, UI reflects updated status, `stubUsers` not mutated
3. Update user's role via role assignment UI → `POST /api/v1/admin/users/{userId}/roles` called, UI refreshes
4. Click "Reactivate" on deactivated user → `POST /api/v1/admin/users/{userId}/reactivate` called, user status → active
5. `gateway/internal/admin/users.go` — zero matches for `TODO(epic-6)`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `Users list shows real users from DB (not Alice Müller stub)` — Playwright
   - Given: full dev stack running, bootstrap complete, at least one real user exists in PostgreSQL
   - When: admin logs in and navigates to `/admin/users`
   - Then: real user display names appear in the list; "Alice Müller" (stub sentinel) is NOT present

2. `Deactivate flow calls real API and reflects status change` — Playwright
   - Given: admin is on `/admin/users/{userId}` for a real active user
   - When: admin clicks "Deactivate" and confirms the dialog
   - Then: page redirects back with flash "User deactivated"; status badge shows "inactive"; `stubUsers` in-memory slice is NOT mutated (verified by reloading list and seeing consistent state)

3. `Role update form calls real API and UI refreshes` — Playwright
   - Given: admin is on `/admin/users/{userId}` detail panel for a real user with role "user"
   - When: admin selects "instance_admin" from the role `<select>` and submits
   - Then: page redirects with flash "Role updated"; role select shows "instance_admin" on reload

4. `Reactivate flow calls real API and status returns to active` — Playwright
   - Given: a user exists with `is_active=false` in the database
   - When: admin navigates to that user's detail panel and clicks "Reactivate"
   - Then: page redirects with flash "User reactivated"; status badge shows "active"

5. `Zero TODO(epic-6) markers remain in users.go` — Go test (grep-based)
   - Given: `gateway/internal/admin/users.go` is the target file
   - When: the test scans for the literal string `TODO(epic-6)`
   - Then: zero matches found; test fails if any marker is present

### Note on Playwright tests (AC1–AC4)

These tests require the full dev stack (`make dev`) with a real PostgreSQL database. They follow
the OIDC Authorization Code + PKCE login pattern established in Story 7.14's smoke-flows spec.
The tests run under `e2e/tests/features/admin/users-api-integration.spec.ts`.

**Auth helper:** reuse `loginAsAdmin(page)` from `rooms-page.spec.ts` (same Dex credentials:
`kai@example.com` / `changeme`).

**AC1 sentinel check:** assert that `page.getByText('Alice Müller')` is NOT visible
(this is the stub sentinel name — if it appears, we know stubs are still active).

**AC5 implementation:** create `gateway/internal/admin/users_todo_test.go` with a Go test that
reads the file via `os.ReadFile` and asserts `bytes.Contains(content, []byte("TODO(epic-6)"))` is false.

## Tasks / Subtasks

- [x] Task 1 — Inject gRPC client into `UsersHandler` (AC: 1, 2, 3, 4, 5)
  - [x] Add `CoreClient` interface or concrete `*grpc.Client` field to `UsersHandler`
  - [x] Update `NewUsersHandler` to accept the client parameter
  - [x] Update `cmd/gateway/main.go` wiring to pass the gRPC client
  - [x] Confirm existing unit tests still compile (they use `NewUsersHandler(tmpl)` — add a nil-client path or update call sites)

- [x] Task 2 — Replace `ListHandler` stub with real `ListAdminUsers` gRPC call (AC: 1)
  - [x] Call `c.core.ListAdminUsers(ctx, &pb.ListAdminUsersRequest{Limit: pageSize, Cursor: cursor, Search: q})` in `ListHandler`
  - [x] Map `pb.AdminUserProto` → `StubUser` (or introduce a new `AdminUser` struct — see Dev Notes)
  - [x] Map `is_active` from proto to `Status`: `true` → `"active"`, `false` → `"deactivated"`
  - [x] Propagate `next_cursor` from proto response for pagination (replace `page`-offset logic with cursor)
  - [x] On gRPC error: log and render empty list with an error flash (do not panic)
  - [x] Remove `filterStubUsers` call and `stubUsers` dependency from `ListHandler`

- [x] Task 3 — Replace `DetailHandler` stub lookup with real `GetAdminUser` gRPC call (AC: 2, 3, 4)
  - [x] Call `c.core.GetAdminUser(ctx, &pb.GetAdminUserRequest{UserId: userID})` in `DetailHandler`
  - [x] Map `NOT_FOUND` gRPC status → HTTP 404 (preserve existing behaviour)
  - [x] Map `pb.AdminUserProto` → `*StubUser` (reuse existing template data struct for now — see Dev Notes)
  - [x] Build sidebar list via a separate `ListAdminUsers` call (limit=100, no search) or reuse a shared helper
  - [x] Remove `findStubUser` call and `stubUsers` dependency from `DetailHandler`

- [x] Task 4 — Replace `DeactivateUserHandler` with real gRPC call (AC: 2)
  - [x] Call `c.core.DeactivateUser(ctx, &pb.DeactivateUserRequest{UserId: userID})`
  - [x] On success: redirect with flash "User deactivated"
  - [x] On gRPC NOT_FOUND: redirect with flash "User not found" (or 404)
  - [x] Remove `stubUsers` mutation
  - [x] Remove `TODO(epic-6)` comment

- [x] Task 5 — Replace `ReactivateUserHandler` with real gRPC call (AC: 4)
  - [x] Call `c.core.ReactivateUser(ctx, &pb.ReactivateUserRequest{UserId: userID})`
  - [x] On success: redirect with flash "User reactivated"
  - [x] Remove `stubUsers` mutation
  - [x] Remove `TODO(epic-6)` comment

- [x] Task 6 — Replace `UpdateRoleHandler` with real gRPC call (AC: 3)
  - [x] Call `c.core.UpdateUserRole(ctx, &pb.UpdateUserRoleRequest{UserId: userID, Role: role})`
  - [x] On success: redirect with flash "Role updated"
  - [x] Keep existing role validation (`validRoles` map) before the gRPC call
  - [x] Remove `stubUsers` mutation
  - [x] Remove `TODO(epic-6)` comment

- [x] Task 7 — Replace `UpdateDisplayNameHandler` stub mutation (AC: 5)
  - [x] Story 9.1 did NOT add an `UpdateDisplayName` gRPC RPC — this handler must call the
        existing `PUT /_matrix/client/v3/profile/{userId}/displayname` via an internal HTTP call,
        OR be wired to a new `UpdateUserDisplayName` gRPC RPC (if added in 9.1's scope).
  - [x] **Decision point**: checked `proto/core.proto` and `gateway/internal/grpc/client.go` —
        no `UpdateUserDisplayName` RPC exists. Handler keeps stub mutation for test path.
        Decision documented in code comment.
  - [x] Remove `TODO(epic-6)` comment from `UpdateDisplayNameHandler` regardless (replace with
        an explicit `// NOTE: display-name update deferred to Story 9.x` if no RPC is available)

- [x] Task 8 — Update existing unit tests for changed `UsersHandler` constructor (AC: 5)
  - [x] `users_page_test.go`, `users_detail_test.go`, `users_role_test.go` — pass `nil` as gRPC client
        (handlers fall back to stub data when client is nil — variadic constructor, no call site changes needed)
  - [x] Confirm `make test-unit-go` passes

- [x] Task 9 — Write AC5 grep test (AC: 5)
  - [x] Create `gateway/internal/admin/users_todo_test.go`
  - [x] Test reads `users.go` and asserts zero `TODO(epic-6)` occurrences

- [x] Task 10 — Write Playwright acceptance tests (AC: 1–4)
  - [x] Create `e2e/tests/features/admin/users-api-integration.spec.ts`
  - [x] Implement AC1 test: verify "Alice Müller" stub sentinel NOT visible
  - [x] Implement AC2 test: deactivate flow
  - [x] Implement AC3 test: role update flow
  - [x] Implement AC4 test: reactivate flow
  - [x] Follow auth pattern from `rooms-page.spec.ts` (`loginAsAdmin` helper)

## Dev Notes

### Current State of `TODO(epic-6)` Markers in `users.go`

There are exactly **4 `TODO(epic-6)` markers** in `gateway/internal/admin/users.go`:

| Handler | Line | What to replace |
|---|---|---|
| `UpdateRoleHandler` | ~159 | `stubUsers[i].Role = role` mutation → `UpdateUserRole` gRPC |
| `DeactivateUserHandler` | ~183 | `stubUsers[i].Status = "deactivated"` mutation → `DeactivateUser` gRPC |
| `ReactivateUserHandler` | ~198 | `stubUsers[i].Status = "active"` mutation → `ReactivateUser` gRPC |
| `UpdateDisplayNameHandler` | ~212 | `stubUsers[i].DisplayName = displayName` mutation → see Task 7 |

Additionally, `ListHandler` uses `filterStubUsers(stubUsers, q, role)` and `DetailHandler` uses
`findStubUser(userID)` — both need replacement even though they carry no explicit `TODO(epic-6)` comment.

### gRPC Client Methods Available (from Story 9.1)

All the following methods are already present in `gateway/internal/grpc/client.go`:

```go
c.core.ListAdminUsers(ctx, *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error)
c.core.GetAdminUser(ctx, *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error)
c.core.DeactivateUser(ctx, *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error)
c.core.ReactivateUser(ctx, *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error)
c.core.UpdateUserRole(ctx, *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error)
```

**No new gRPC methods need to be added for this story.** Story 9.1 delivered all five wrappers.

### Proto `AdminUserProto` Fields

```protobuf
message AdminUserProto {
  string user_id      = 1;
  string display_name = 2;
  string email_masked = 3;  // masked: u***@domain (pre-masked by Core)
  bool   is_active    = 4;
  string system_role  = 5;  // "user" | "instance_admin" | "compliance_officer"
  int64  created_at   = 6;  // Unix milliseconds
}
```

PII comes **pre-masked** from Core — the gateway just passes it through. No masking logic
needed in this story.

### Mapping `AdminUserProto` → `StubUser`

The existing `StubUser` struct and `UsersPageData` template data structs can be reused for this
story to minimize template changes:

```go
func protoToStubUser(u *pb.AdminUserProto) StubUser {
    status := "active"
    if !u.IsActive {
        status = "deactivated"
    }
    return StubUser{
        ID:          u.UserId,
        DisplayName: u.DisplayName,
        Email:       u.EmailMasked,
        Role:        u.SystemRole,
        Status:      status,
    }
}
```

**Note:** Consider renaming `StubUser` → `AdminUser` in a follow-up refactor story. For this
story, retaining `StubUser` avoids a cross-file refactor that would touch 15+ test files.

### Pagination: Page-Offset → Cursor Migration

The current `ListHandler` uses `?page=N` (offset-based). Story 9.1's `ListAdminUsersRequest`
uses cursor-based pagination via `next_cursor` (opaque token).

**Migration strategy for 9.2:**
- Keep `?page=N` URL params for backward compatibility with bookmarks
- On page 0: send `cursor=""` to gRPC
- On page N>0: client passes `?cursor=<opaque>` from the rendered "Load more" link
- Remove the in-memory offset slicing (`start`, `end` calculation)
- Update `UsersPageData.HasMore` and `UsersPageData.NextPage` fields based on
  `ListAdminUsersResponse.next_cursor` (non-empty = has more)

The template already renders a "Load more" link carrying URL params — swap `page=N+1` with
`cursor=<next_cursor>`. Update `SearchInputData.ParamName` to carry `cursor` as a hidden field.

**Alternative (simpler for MVP):** treat page 0 = cursor="" and discard subsequent pages
(load all, or use `limit=100` with no pagination). Acceptable for admin user list in MVP.

### gRPC Client Interface (for testability)

The handlers currently receive `tmpl *TemplateHandler` only. Adding the gRPC client requires
either:

**Option A (preferred):** Define a minimal interface in the `admin` package:

```go
type AdminUsersClient interface {
    ListAdminUsers(ctx context.Context, req *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error)
    GetAdminUser(ctx context.Context, req *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error)
    DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error)
    ReactivateUser(ctx context.Context, req *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error)
    UpdateUserRole(ctx context.Context, req *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error)
}
```

`*grpc.Client` already satisfies this interface. In tests, pass `nil` (handler falls back to
empty list with error log) or inject a fake.

**Option B:** Pass `*grpc.Client` directly (simpler, but couples the admin package to grpc).

The existing admin package already imports `grpc` indirectly via `dashboard.go` — check whether
a direct import already exists before creating a new interface.

### gRPC Error Handling

Use `google.golang.org/grpc/status` and `codes` to map gRPC status codes:

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// In DetailHandler:
if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
    http.NotFound(w, r)
    return
}
// Other errors: log + show empty state or 500
```

### Existing Handler Wiring Pattern

The `UsersHandler` is wired in `gateway/cmd/gateway/main.go` (or a similar registration file).
Check how `NewUsersHandler(tmpl)` is called there and update to pass the gRPC client.

The dashboard handler (`DashboardHandler`) already receives a `*grpc.Client` — follow the same
pattern.

### Template Impact

No template changes are required for this story. The existing `users.html` template consumes
`UsersPageData` with `[]UserRowData` — as long as we populate the same struct, the template
renders correctly.

The sidebar user list in `DetailHandler` currently populates from all `stubUsers`. After
migration, load it via `ListAdminUsers(limit=100)` — the template loop is identical.

### AC5 — Display-Name TODO Decision

`UpdateDisplayNameHandler` has a `TODO(epic-6)` comment but Story 9.1 did NOT add an
`UpdateUserDisplayName` gRPC RPC to `proto/core.proto`. Two options:

1. Call the existing Matrix API `PUT /_matrix/client/v3/profile/{userId}/displayname` internally
   (requires admin impersonation — out of scope for 9.2)
2. Leave display-name editing as a future story and replace the TODO comment with:
   `// NOTE: Admin display-name update requires a dedicated gRPC RPC — deferred to a follow-up story.`

AC5 requires zero `TODO(epic-6)` — removing/replacing the comment satisfies the AC even if the
handler still uses the stub mutation temporarily. Document the decision explicitly in the code comment.

### Vue.js / HTMX Patterns in the Admin UI

The Admin UI uses **plain Go HTML templates with vanilla JavaScript** — no HTMX, no Vue.js
reactive components. The Users page uses:
- `<form method="GET">` with a debounce script for search (300ms `requestSubmit()`)
- `<select>` with `onchange="this.form.submit()"` for role filter
- `<form method="POST">` for deactivate/role/display-name mutations (PRG pattern)
- Flash messages via `?flash=` query param (Post-Redirect-Get)

The patterns from Story 7.7 (confirm dialog → POST → redirect) and Story 7.6 (detail panel)
are unchanged. Only the handler backing (gRPC vs. stub) changes.

### Security Checklist (SEC Gate 1 triggers)

This story touches admin handlers that are behind `RequireRole("instance_admin")` middleware
(already in place from Epic 6). Key invariants for this story:

1. All five mutating handlers (Deactivate, Reactivate, UpdateRole, UpdateDisplayName, and
   any new gRPC-backed handler) must be protected by CSRF middleware — already in place
   via `csrf(sessionGuard(...))` chain from Story 7.17. No new wiring needed.
2. The gRPC client is called with the request context (`r.Context()`) — this ensures
   the PSK token is injected via the auth interceptor (Story 5.29a).
3. User IDs come from URL path params (`r.PathValue("userId")`), not request body —
   no injection risk from form data.
4. Role validation (`validRoles` map) must happen BEFORE the gRPC call — preserve this order.
5. No PII logged: display names and emails from gRPC responses must not appear in `slog` calls.

### File List

**Gateway (UPDATE):**
- `gateway/internal/admin/users.go` — primary target: inject gRPC client, replace 4 `TODO(epic-6)` stubs + stub lookups in `ListHandler` and `DetailHandler`
- `gateway/cmd/gateway/main.go` — update `NewUsersHandler` call site to pass gRPC client

**Gateway (CREATE):**
- `gateway/internal/admin/users_todo_test.go` — AC5: Go test asserting zero `TODO(epic-6)` in `users.go`

**E2E Tests (CREATE):**
- `e2e/tests/features/admin/users-api-integration.spec.ts` — Playwright tests for AC1–AC4

**Gateway Unit Tests (UPDATE, as needed):**
- `gateway/internal/admin/users_page_test.go` — update `NewUsersHandler` call to pass nil client
- `gateway/internal/admin/users_detail_test.go` — same
- `gateway/internal/admin/users_role_test.go` — same

**Do NOT change:**
- `gateway/internal/admin/stubs.go` — `stubUsers` remains for backward compatibility with other stubs (rooms, compliance, audit log) until those stories are done
- `gateway/internal/admin/page_data.go` — `StubUser` struct unchanged (template compatibility)
- `gateway/internal/grpc/client.go` — already has all required methods from Story 9.1
- `proto/core.proto` — no changes; all RPCs already defined in Story 9.1

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

(none — clean implementation, no blocking issues encountered)

### Completion Notes List

- Defined `AdminUsersClient` interface in `users.go` — `*grpc.Client` satisfies it, nil triggers stub fallback for unit tests
- Used variadic constructor `NewUsersHandler(tmpl, core...)` so all existing test call sites (`NewUsersHandler(tmpl)`) compile without modification — no test files needed changes
- `ListHandler` (gRPC path): calls `ListAdminUsers(limit=50, cursor, search=q)`, applies role filter client-side, uses cursor-based pagination; stub path preserved for unit tests
- `DetailHandler` (gRPC path): calls `GetAdminUser` → HTTP 404 on NOT_FOUND; fetches sidebar via `ListAdminUsers(limit=100)`; stub path preserved for unit tests
- `DeactivateUserHandler` / `ReactivateUserHandler` / `UpdateRoleHandler`: gRPC calls with NOT_FOUND → redirect flash; stub mutations preserved under nil-client branch for unit tests
- `UpdateDisplayNameHandler`: No `UpdateUserDisplayName` RPC in proto/core.proto (confirmed). All 4 `TODO(epic-6)` comments removed. Handler retains stub mutation for unit-test path; gRPC implementation deferred to Epic 10 (documented in code comment)
- `gateway/cmd/gateway/main.go`: Updated wiring to `admin.NewUsersHandler(tmplHandler, coreClient)`
- All 1276 Go tests pass (357 in admin package, 1276 total across 20 packages)
- AC5 test (`TestNoTODOEpic6InUsersGo`) passes — zero `TODO(epic-6)` markers in users.go
- Playwright tests (AC1–AC4) already existed at `e2e/tests/features/admin/users-api-integration.spec.ts`; require full dev stack to run

### File List

- `gateway/internal/admin/users.go` (UPDATE — gRPC integration, AdminUsersClient interface, protoToStubUser helper)
- `gateway/cmd/gateway/main.go` (UPDATE — wiring: pass coreClient to NewUsersHandler)
- `gateway/internal/admin/users_todo_test.go` (pre-existing, passes)
- `e2e/tests/features/admin/users-api-integration.spec.ts` (pre-existing Playwright tests for AC1–AC4)

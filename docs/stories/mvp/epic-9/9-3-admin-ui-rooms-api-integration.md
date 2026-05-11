---
status: ready-for-dev
epic: 9
story: 3
security_review: required
---

# Story 9.3: Admin UI — Rooms API Integration

Status: ready-for-dev

## Story

As an instance admin,
I want the Admin UI room management pages to use the real Admin API,
So that room data reflects actual state and mutations are persisted.

## Acceptance Criteria

1. Navigate to Rooms list → real rooms from DB displayed (not `stubRooms`)
2. Click "Archive" on Room Detail page → `POST /api/v1/admin/rooms/{roomId}/archive` called, room status changes to `archived` in UI
3. Update room settings (max_members, visibility) → `PATCH /api/v1/admin/rooms/{roomId}` called
4. `gateway/internal/admin/rooms.go` — zero matches for `TODO(epic-6)`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `Rooms list shows real rooms from DB (not stub sentinel)` — Playwright
   - Given: full dev stack running, bootstrap complete, at least one real room exists in PostgreSQL
   - When: admin logs in and navigates to `/admin/rooms`
   - Then: real room names appear in the list; the stub sentinel room name (e.g. `"General"` from `stubRooms`) is NOT present as a stub entry — real data is shown instead

2. `Archive flow calls real API and reflects status change` — Playwright
   - Given: admin is on `/admin/rooms/{roomId}` for a real active room
   - When: admin clicks "Archive" and confirms the dialog
   - Then: page redirects back with flash "Room archived"; status badge shows "inactive" (archived); `stubRooms` in-memory slice is NOT mutated (verified by reloading list and seeing consistent state driven by the real API)

3. `Update room settings calls PATCH and UI reflects changes` — Playwright
   - Given: admin is on `/admin/rooms/{roomId}` detail panel for a real active room
   - When: admin updates max_members (e.g. sets to 50) and/or changes visibility, then submits
   - Then: page redirects with flash confirming update; room detail panel reflects new max_members / visibility values on reload; `PATCH /api/v1/admin/rooms/{roomId}` was called (verified via server-side behavior: the new value persists after page reload)

4. `Zero TODO(epic-6) markers remain in rooms.go` — Go test (grep-based)
   - Given: `gateway/internal/admin/rooms.go` is the target file
   - When: the test scans for the literal string `TODO(epic-6)`
   - Then: zero matches found; test fails if any marker is present

### Note on Playwright tests (AC1–AC3)

These tests require the full dev stack (`make dev`) with a real PostgreSQL database. They follow
the OIDC Authorization Code + PKCE login pattern established in Story 7.14's smoke-flows spec.
The tests run under `e2e/tests/features/admin/rooms-api-integration.spec.ts`.

**Auth helper:** reuse `loginAsAdmin(page)` extracted during Story 9.2 from `rooms-page.spec.ts`
(same Dex credentials: `kai@example.com` / `changeme`).

**AC1 sentinel check:** assert that the rooms listed are sourced from the database, not from
`stubRooms`. A reliable approach: create a room via the Matrix API, then verify it appears in
the admin Rooms list.

**AC4 implementation:** create `gateway/internal/admin/rooms_todo_test.go` with a Go test that
reads the file via `os.ReadFile` and asserts `bytes.Contains(content, []byte("TODO(epic-6)"))` is false.
Mirror the exact pattern from `gateway/internal/admin/users_todo_test.go`.

## Tasks / Subtasks

- [ ] Task 1 — Inject gRPC client into `RoomsHandler` (AC: 1, 2, 3, 4)
  - [ ] Define `AdminRoomsClient` interface in `gateway/internal/admin/rooms.go` (mirror `AdminUsersClient` pattern from Story 9.2)
  - [ ] Add `core AdminRoomsClient` field to `RoomsHandler`
  - [ ] Update `NewRoomsHandler` to use variadic constructor: `NewRoomsHandler(tmpl *TemplateHandler, core ...AdminRoomsClient) *RoomsHandler`
  - [ ] Update `cmd/gateway/main.go` wiring to pass the gRPC client to `NewRoomsHandler`
  - [ ] Confirm existing unit tests still compile (they use `NewRoomsHandler(tmpl)` — variadic constructor means no call site changes needed)

- [ ] Task 2 — Replace `ListHandler` stub with real `ListAdminRooms` gRPC call (AC: 1)
  - [ ] Call `c.core.ListAdminRooms(ctx, &pb.ListAdminRoomsRequest{Limit: pageSize, Cursor: cursor, Search: q, StatusFilter: statusFilter})` in `ListHandler`
  - [ ] Map `pb.AdminRoomProto` → `StubRoom` via a `protoToStubRoom` helper
  - [ ] Map `status` from proto: `"active"` → `"active"`, `"archived"` → `"archived"` (direct pass-through)
  - [ ] Propagate `next_cursor` from proto response for pagination (replace `page`-offset logic with cursor, following the same strategy as Story 9.2)
  - [ ] On gRPC error: log and render empty list with an error flash (do not panic)
  - [ ] Remove `filterStubRooms` call and `stubRooms` dependency from `ListHandler`

- [ ] Task 3 — Replace `DetailHandler` stub lookup with real `GetAdminRoom` gRPC call (AC: 2, 3)
  - [ ] Call `c.core.GetAdminRoom(ctx, &pb.GetAdminRoomRequest{RoomId: roomID})` in `DetailHandler`
  - [ ] Map `NOT_FOUND` gRPC status → HTTP 404 (preserve existing behaviour)
  - [ ] Map `pb.AdminRoomDetailProto` → `*StubRoom` (reuse existing template data struct to minimize template changes)
  - [ ] Build sidebar list via a separate `ListAdminRooms` call (limit=100, no search)
  - [ ] Remove `findStubRoom` call and `stubRooms` dependency from `DetailHandler`

- [ ] Task 4 — Replace `ArchiveRoomHandler` stub mutation with real gRPC call (AC: 2)
  - [ ] Add `ArchiveRoom(ctx, *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error)` to `AdminRoomsClient` interface
  - [ ] Add wrapper method `ArchiveRoom` to `gateway/internal/grpc/client.go` (Story 9.1 did NOT add ArchiveRoom/UnarchiveRoom/UpdateRoomSettings — these must be added now)
  - [ ] Call `c.core.ArchiveRoom(ctx, &pb.ArchiveRoomRequest{RoomId: roomID})` in `ArchiveRoomHandler`
  - [ ] On success: redirect with flash "Room archived"
  - [ ] On gRPC NOT_FOUND: HTTP 404
  - [ ] Remove `stubRooms` mutation
  - [ ] Remove `TODO(epic-6)` comment

- [ ] Task 5 — Replace `UnarchiveRoomHandler` stub mutation with real gRPC call (AC: 4)
  - [ ] Add `UnarchiveRoom(ctx, *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error)` to `AdminRoomsClient` interface
  - [ ] Add wrapper method `UnarchiveRoom` to `gateway/internal/grpc/client.go`
  - [ ] Call `c.core.UnarchiveRoom(ctx, &pb.UnarchiveRoomRequest{RoomId: roomID})` in `UnarchiveRoomHandler`
  - [ ] On success: redirect with flash "Room unarchived"
  - [ ] Remove `stubRooms` mutation
  - [ ] Remove `TODO(epic-6)` comment

- [ ] Task 6 — Implement `UpdateRoomSettingsHandler` via `UpdateRoomSettings` gRPC call (AC: 3)
  - [ ] Determine where the form lives: the existing `UpdateRoomNameHandler` covers name; a new form submission for max_members/visibility may need a new handler or an extended existing one
  - [ ] Add `UpdateRoomSettings(ctx, *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error)` to `AdminRoomsClient` interface
  - [ ] Add wrapper method `UpdateRoomSettings` to `gateway/internal/grpc/client.go`
  - [ ] Parse and validate `max_members` (≥0) and `visibility` ("public"|"private") from form values
  - [ ] Call `c.core.UpdateRoomSettings(ctx, &pb.UpdateRoomSettingsRequest{RoomId: roomID, MaxMembers: maxMembers})` — note: proto field is `max_members` only; visibility is not in `UpdateRoomSettingsRequest` (see Dev Notes)
  - [ ] On success: redirect with flash confirming update
  - [ ] Remove `TODO(epic-6)` comment from `UpdateRoomNameHandler`

- [ ] Task 7 — Update existing unit tests for changed `RoomsHandler` constructor (AC: 4)
  - [ ] `rooms_page_test.go`, `rooms_detail_test.go` — confirm `NewRoomsHandler(tmpl)` still compiles (variadic constructor, nil-client path falls back to stub data)
  - [ ] Confirm `make test-unit-go` passes

- [ ] Task 8 — Write AC4 grep test (AC: 4)
  - [ ] Create `gateway/internal/admin/rooms_todo_test.go`
  - [ ] Test reads `rooms.go` and asserts zero `TODO(epic-6)` occurrences (exact pattern from `users_todo_test.go`)

- [ ] Task 9 — Write Playwright acceptance tests (AC: 1–3)
  - [ ] Create `e2e/tests/features/admin/rooms-api-integration.spec.ts`
  - [ ] Implement AC1 test: real rooms visible (not stub-only)
  - [ ] Implement AC2 test: archive flow
  - [ ] Implement AC3 test: update settings flow (max_members)
  - [ ] Follow auth pattern from `loginAsAdmin` helper established in Story 9.2

- [ ] Task 10 — Add audit log calls in Core `server.ex` (see Dev Notes / Lesson from 9.2)
  - [ ] Confirm `ArchiveRoom` and `UnarchiveRoom` handlers in `core/apps/room_manager/lib/room_manager/grpc/server.ex` emit audit log entries
  - [ ] If missing, add audit log calls matching the pattern used for user deactivate/reactivate in Story 9.2

## Dev Notes

### Current State of `TODO(epic-6)` Markers in `rooms.go`

There are exactly **3 `TODO(epic-6)` markers** in `gateway/internal/admin/rooms.go`:

| Handler | Line | What to replace |
|---|---|---|
| `UpdateRoomNameHandler` | ~148 | `stubRooms[i].Name = name` mutation → `UpdateRoomName`/`UpdateRoomSettings` gRPC |
| `ArchiveRoomHandler` | ~178 | `stubRooms[i].Status = "archived"` mutation → `ArchiveRoom` gRPC |
| `UnarchiveRoomHandler` | ~199 | `stubRooms[i].Status = "active"` mutation → `UnarchiveRoom` gRPC |

Additionally, `ListHandler` uses `filterStubRooms(stubRooms, q, visibility)` (line ~35) and
`DetailHandler` uses `findStubRoom(roomID)` (line ~84) — both need replacement even though they
carry no explicit `TODO(epic-6)` comment.

There is also a non-epic-6 `TODO:` on line ~125 (rune-aware initials helper) — this is NOT an
`TODO(epic-6)` and does NOT need removal for AC4.

### gRPC Client Methods Available vs. Missing

From Story 9.1, `gateway/internal/grpc/client.go` provides:

```go
c.ListAdminRooms(ctx, *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error)
c.GetAdminRoom(ctx, *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error)
```

**NOT yet in `client.go` — must be added in this story (Tasks 4–6):**

```go
c.ArchiveRoom(ctx, *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error)
c.UnarchiveRoom(ctx, *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error)
c.UpdateRoomSettings(ctx, *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error)
```

All three RPCs are fully defined in `proto/core.proto` (lines 89, 95, 100) and were implemented in
Core during Epic 6. They just need gateway wrapper methods.

### Proto Message Fields

**`AdminRoomProto`** (for list view — from `ListAdminRoomsResponse`):
```protobuf
message AdminRoomProto {
  string room_id      = 1;
  string name         = 2;
  string status       = 3;  // "active" | "archived"
  int32  member_count = 4;
  int64  created_at   = 5;  // Unix milliseconds
}
```

**`AdminRoomDetailProto`** (for detail panel — from `GetAdminRoomResponse`):
```protobuf
message AdminRoomDetailProto {
  string room_id      = 1;
  string name         = 2;
  string status       = 3;
  int32  member_count = 4;
  int32  max_members  = 5;  // 0 = no limit
  string visibility   = 6;  // "public" | "private"
  int64  created_at   = 7;
}
```

**`UpdateRoomSettingsRequest`** — note: only `max_members` is in the proto; `visibility` is NOT a field in this RPC:
```protobuf
message UpdateRoomSettingsRequest {
  string room_id     = 1;
  int32  max_members = 2;  // 0 = no limit
}
```

**Implication for AC3:** The epics.md AC says "update max_members, visibility" → `PATCH /api/v1/admin/rooms/{roomId}`. The existing `UpdateRoomSettings` gRPC RPC only covers `max_members`. Visibility changes may need a separate call or be deferred. Document the decision in code comments. For this story, AC3 is satisfied if `max_members` updates are wired to the gRPC call; visibility-only changes can note this limitation.

### Mapping `AdminRoomDetailProto` → `StubRoom`

The existing `StubRoom` struct can be reused for this story to minimize template changes:

```go
func protoToStubRoom(r *pb.AdminRoomDetailProto) *StubRoom {
    return &StubRoom{
        ID:          r.RoomId,
        Name:        r.Name,
        Visibility:  r.Visibility,
        MemberCount: int(r.MemberCount),
        Status:      r.Status,
    }
}
```

For the list view, map `AdminRoomProto` (no Visibility/MaxMembers):

```go
func protoToStubRoomSummary(r *pb.AdminRoomProto) StubRoom {
    return StubRoom{
        ID:          r.RoomId,
        Name:        r.Name,
        MemberCount: int(r.MemberCount),
        Status:      r.Status,
    }
}
```

### gRPC Client Interface (for testability)

Follow the exact pattern from Story 9.2's `AdminUsersClient`:

```go
type AdminRoomsClient interface {
    ListAdminRooms(ctx context.Context, req *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error)
    GetAdminRoom(ctx context.Context, req *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error)
    ArchiveRoom(ctx context.Context, req *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error)
    UnarchiveRoom(ctx context.Context, req *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error)
    UpdateRoomSettings(ctx context.Context, req *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error)
}
```

`*grpc.Client` must satisfy this interface. In unit tests, pass `nil` (variadic constructor
allows nil — handler falls back to stub data when `c.core == nil`).

### Pagination: Page-Offset → Cursor Migration

The current `ListHandler` uses `?page=N` (offset-based). `ListAdminRoomsRequest` uses cursor-based
pagination via `next_cursor`.

Follow the same strategy as Story 9.2:
- `?page=0` → `cursor=""`
- `?cursor=<opaque>` → passed through on subsequent pages
- Update `RoomsPageData.HasMore` and `RoomsPageData.NextPage` based on `ListAdminRoomsResponse.next_cursor` (non-empty = has more)

Alternatively (simpler): use `limit=100` with no pagination for MVP admin use case.

### Lesson from Story 9.2 — Audit Log Calls in Core

Story 9.2's Kassandra (security) review flagged a HIGH-1 finding: the Core `server.ex` was missing
audit log calls for `deactivate_user`, `reactivate_user`, and `update_user_role`. These had to be
added as a fix during the 9.2 pipeline.

**For 9.3:** Before marking the story `done`, verify that the Core `server.ex` (or equivalent
Elixir handler) emits audit log entries for `ArchiveRoom` and `UnarchiveRoom`. If not present,
add them as part of this story's scope. Check `core/apps/room_manager/lib/room_manager/grpc/server.ex`.

### gRPC Error Handling

Use the same pattern as Story 9.2:

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
// Other errors: log + render empty state or flash error
```

### Existing Handler Wiring Pattern

`NewRoomsHandler(tmpl)` is called in `gateway/cmd/gateway/main.go`. The `DashboardHandler` already
receives a `*grpc.Client` — follow the same pattern as done for `NewUsersHandler` in Story 9.2.

### Template Impact

No template changes are required for this story. The existing `rooms.html` template consumes
`RoomsPageData` with `[]RoomRowData` — as long as we populate the same struct, the template
renders correctly.

The sidebar room list in `DetailHandler` currently populates from all `stubRooms`. After migration,
load it via `ListAdminRooms(limit=100)`.

### Security Checklist (SEC Gate 1 triggers)

This story modifies admin handlers behind `RequireRole("instance_admin")` middleware. Key invariants:

1. `ArchiveRoomHandler`, `UnarchiveRoomHandler`, and `UpdateRoomSettingsHandler` must remain
   protected by CSRF middleware (already in place via `csrf(sessionGuard(...))` from Story 7.17).
2. The gRPC client is called with `r.Context()` — ensures PSK token is injected via auth interceptor (Story 5.29a).
3. Room IDs come from `r.PathValue("roomId")`, not request body — no injection risk from form data.
4. `max_members` input from form must be validated (≥0, reasonable upper bound) before the gRPC call.
5. No room names or member data should appear in `slog` error calls.

### File List

**Gateway (UPDATE):**
- `gateway/internal/admin/rooms.go` — primary target: inject gRPC client, replace 3 `TODO(epic-6)` stubs + stub lookups in `ListHandler` and `DetailHandler`
- `gateway/internal/grpc/client.go` — add `ArchiveRoom`, `UnarchiveRoom`, `UpdateRoomSettings` wrapper methods
- `gateway/cmd/gateway/main.go` — update `NewRoomsHandler` call site to pass gRPC client

**Gateway (CREATE):**
- `gateway/internal/admin/rooms_todo_test.go` — AC4: Go test asserting zero `TODO(epic-6)` in `rooms.go`

**E2E Tests (CREATE):**
- `e2e/tests/features/admin/rooms-api-integration.spec.ts` — Playwright tests for AC1–AC3

**Gateway Unit Tests (UPDATE, if needed):**
- `gateway/internal/admin/rooms_page_test.go` — verify `NewRoomsHandler(tmpl)` still compiles
- `gateway/internal/admin/rooms_detail_test.go` — same

**Elixir Core (VERIFY / UPDATE if needed):**
- `core/apps/room_manager/lib/room_manager/grpc/server.ex` — verify audit log calls for ArchiveRoom/UnarchiveRoom

**Do NOT change:**
- `gateway/internal/admin/stubs.go` — `stubRooms` remains for backward compatibility with other stubs until 9-4/9-5 are done
- `gateway/internal/admin/page_data.go` — `StubRoom` struct unchanged (template compatibility)
- `proto/core.proto` — no changes; all RPCs already defined in Epic 6

---
status: review
epic: 9
story: 18
security_review: not-needed
---

# Story 9.18: Admin UI — Room Detail: Member List

Status: review

## Story

As an **instance admin**,
I want the Room Detail panel in the Admin UI to display a list of the room's current members,
so that I can see who is in a room without leaving the Admin UI or querying the database directly.

**Size:** S

---

## Background

The Room Detail page (`GET /admin/rooms/{roomId}`) currently shows only a numeric `MemberCount` field. There is no way to see which users are actually in a room. This story adds a member list section to the detail panel.

**Current state (read before implementing):**
- `GetAdminRoom` → `AdminRoomDetailProto` contains `member_count int32` but no member list.
- No `ListAdminRoomMembers` gRPC RPC exists anywhere (proto, generated Go stubs, Elixir server, Go client).
- `room_members` table: `(room_id TEXT, user_id TEXT, joined_at BIGINT, left_at BIGINT)` — active members have `left_at IS NULL`.
- `users` table: `display_name_encrypted` / `email_encrypted` are AES-256-GCM encrypted; the Elixir Core decrypts them before sending to Go (see how `ListAdminUsers` works in `server.ex`).

---

## Acceptance Criteria

**AC1 — New `ListAdminRoomMembers` gRPC RPC in proto:**
`proto/core.proto` defines:
```proto
rpc ListAdminRoomMembers(ListAdminRoomMembersRequest) returns (ListAdminRoomMembersResponse);

message AdminRoomMemberProto {
  string user_id      = 1;
  string display_name = 2;  // decrypted; empty string if decryption fails
  int64  joined_at    = 3;  // Unix milliseconds
}

message ListAdminRoomMembersRequest  { string room_id = 1; }
message ListAdminRoomMembersResponse { repeated AdminRoomMemberProto members = 1; }
```
After `make proto`, generated Go stubs (`core.pb.go`, `core_grpc.pb.go`) and Elixir stubs (`core.pb.ex`, `core_grpc.pb.ex`) compile without errors.

**AC2 — Elixir Core implements `list_admin_room_members`:**
`Nebu.Admin.DB.list_room_members/1` queries:
```sql
SELECT rm.user_id, u.display_name_encrypted, u.display_name_nonce,
       u.email_ephemeral_pub, rm.joined_at
FROM room_members rm
JOIN users u ON u.user_id = rm.user_id
WHERE rm.room_id = $1 AND rm.left_at IS NULL
ORDER BY rm.joined_at ASC
```
The gRPC handler in `server.ex` calls `admin_db_module().list_room_members(room_id)`, decrypts `display_name_encrypted` using the same X25519/AES-256-GCM helper used by `get_admin_user`, and returns `ListAdminRoomMembersResponse`.
If the room has no members, it returns an empty list (not an error).

**AC3 — Go gRPC client wrapper:**
`gateway/internal/grpc/client.go` has a new method:
```go
func (c *Client) ListAdminRoomMembers(ctx context.Context, req *pb.ListAdminRoomMembersRequest) (*pb.ListAdminRoomMembersResponse, error) {
    return c.core.ListAdminRoomMembers(ctx, req)
}
```

**AC4 — `AdminRoomsClient` interface extended:**
`gateway/internal/admin/rooms.go` adds `ListAdminRoomMembers` to the `AdminRoomsClient` interface so the mock/stub path still compiles.

**AC5 — `DetailHandler` fetches and passes member list:**
`RoomsHandler.DetailHandler` (gRPC path, `h.core != nil`) calls `ListAdminRoomMembers` after `GetAdminRoom`. The result is mapped to `[]RoomMemberData` and stored in `RoomsPageData.ActiveRoomMembers`. On gRPC error, it logs a warning and continues with an empty list (detail panel still renders).

**AC6 — `RoomsPageData` extended:**
`gateway/internal/admin/page_data.go` adds:
```go
// RoomMemberData holds one member row for the Room Detail member list (Story 9.18).
type RoomMemberData struct {
    UserID      string
    DisplayName string // empty string if unavailable
    JoinedAt    int64  // Unix milliseconds
}
```
`RoomsPageData` gains:
```go
// ActiveRoomMembers is the list of current members for the selected room (Story 9.18).
// Nil/empty in list mode or when the gRPC call fails (detail panel still renders).
ActiveRoomMembers []RoomMemberData
```

**AC7 — Room Detail template renders member list:**
`gateway/internal/admin/templates/rooms.html` `detail_content` block gains a Members section below the `<dl>` metadata block and above the settings form:
```html
{{ if .ActiveRoomMembers }}
<div class="mt-4">
  <h3 class="text-sm font-semibold text-base-content/60 uppercase tracking-wide mb-2">Members ({{ len .ActiveRoomMembers }})</h3>
  <ul class="divide-y divide-base-300">
    {{ range .ActiveRoomMembers }}
    <li class="py-2 flex items-center gap-2 text-sm">
      <a href="/admin/users/{{ .UserID }}" class="font-medium hover:underline truncate">
        {{ if .DisplayName }}{{ .DisplayName }}{{ else }}{{ .UserID }}{{ end }}
      </a>
    </li>
    {{ end }}
  </ul>
</div>
{{ end }}
```
Each member's display name is a link to `/admin/users/{userId}`. If `DisplayName` is empty, fall back to `UserID`.

**AC8 — Stub fallback includes stub members:**
`stubs.go` adds:
```go
var stubRoomMembers = map[string][]RoomMemberData{
    "room-001": {
        {UserID: "usr-001", DisplayName: "Alice Müller", JoinedAt: 1714560000000},
        {UserID: "usr-003", DisplayName: "Carla Reiter", JoinedAt: 1714646400000},
    },
    "room-002": {
        {UserID: "usr-002", DisplayName: "Bob Wagner", JoinedAt: 1714560000000},
    },
}
```
`DetailHandler` stub path (`h.core == nil`) populates `ActiveRoomMembers` from `stubRoomMembers[roomID]` (nil if not found — empty slice, no crash).

**AC9 — Unit test: member list renders:**
`gateway/internal/admin/rooms_detail_test.go` gains `TestRoomDetailMemberListRenders`:
- `GET /admin/rooms/room-001` (stub path) returns 200.
- Body contains `"Alice Müller"` and a link to `/admin/users/usr-001`.
- Body contains `"Carla Reiter"`.

**AC10 — Unit test: room with no stub members renders gracefully:**
`TestRoomDetailNoMembers` — `GET /admin/rooms/room-003` (has no entry in `stubRoomMembers`) returns 200 without crashing. Body does NOT contain the Members section heading (the `{{ if .ActiveRoomMembers }}` guard prevents rendering).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestRoomDetailMemberListRenders** — Go httptest (stub path)
   - Given: `room-001` exists in `stubRooms` with two entries in `stubRoomMembers`
   - When: `GET /admin/rooms/room-001`
   - Then: 200, body contains "Alice Müller" and "/admin/users/usr-001"

2. **TestRoomDetailNoMembers** — Go httptest (stub path)
   - Given: `room-003` exists in `stubRooms` but has no entry in `stubRoomMembers`
   - When: `GET /admin/rooms/room-003`
   - Then: 200, body does NOT contain "Members (" heading

3. **ExUnit: ListAdminRoomMembers returns members** — `admin_grpc_test.exs`
   - Given: a room with 2 joined members in the DB
   - When: `list_admin_room_members` is called with that room_id
   - Then: response contains 2 `AdminRoomMemberProto` entries with correct `user_id` and `joined_at`

4. **ExUnit: ListAdminRoomMembers empty room** — `admin_grpc_test.exs`
   - Given: a room with 0 joined members
   - When: `list_admin_room_members` is called
   - Then: response has empty `members` list (no error)

---

## Tasks / Subtasks

- [x] **Task 1 — Proto: add `ListAdminRoomMembers` RPC** (AC1)
  - [x] 1.1 Add `AdminRoomMemberProto` message to `proto/core.proto`
  - [x] 1.2 Add `ListAdminRoomMembersRequest` / `ListAdminRoomMembersResponse` messages
  - [x] 1.3 Add `ListAdminRoomMembers` to the `CoreService` service block
  - [x] 1.4 Run `make proto` — verify generated Go + Elixir stubs compile

- [x] **Task 2 — Elixir Core: DB query + gRPC handler** (AC2)
  - [x] 2.1 Add `list_room_members/1` to `Nebu.Admin.DB` (SQL JOIN query, returns list of maps)
  - [x] 2.2 Add `list_admin_room_members/2` handler in `server.ex` (decrypt display_name, return proto)
  - [x] 2.3 Write ExUnit tests in `admin_grpc_test.exs` (happy path + empty room) — RED first
  - [x] 2.4 Make tests green

- [x] **Task 3 — Go: gRPC client + interface** (AC3, AC4)
  - [x] 3.1 Add `ListAdminRoomMembers` method to `gateway/internal/grpc/client.go`
  - [x] 3.2 Add `ListAdminRoomMembers` to `AdminRoomsClient` interface in `rooms.go`
  - [x] 3.3 Add no-op stub to all test fakes that implement `AdminRoomsClient` (search for `mockCoreClient` and `captureContextClient` in test files)

- [x] **Task 4 — Go: page data types** (AC6)
  - [x] 4.1 Add `RoomMemberData` struct to `page_data.go`
  - [x] 4.2 Add `ActiveRoomMembers []RoomMemberData` field to `RoomsPageData`

- [x] **Task 5 — Go: DetailHandler + stub fallback** (AC5, AC8)
  - [x] 5.1 Add `stubRoomMembers` map to `stubs.go`
  - [x] 5.2 Extend `DetailHandler` gRPC path to call `ListAdminRoomMembers`, map result to `[]RoomMemberData`, assign to `data.ActiveRoomMembers`
  - [x] 5.3 Extend `DetailHandler` stub path to populate `ActiveRoomMembers` from `stubRoomMembers[roomID]`

- [x] **Task 6 — Template: member list section** (AC7)
  - [x] 6.1 Add Members section to `rooms.html` `detail_content` block

- [x] **Task 7 — Go unit tests** (AC9, AC10)
  - [x] 7.1 Write `TestRoomDetailMemberListRenders` — RED first
  - [x] 7.2 Write `TestRoomDetailNoMembers` — RED first
  - [x] 7.3 Make both tests green
  - [x] 7.4 Run `make test-unit-go` — all existing tests still pass

---

## Dev Notes

### Proto extension pattern (follow existing conventions exactly)

In `proto/core.proto`:
- Add the new RPC to the `CoreService` service block **before** the closing `}` of the service, after the existing `UpgradeRoom` RPC (line ~139).
- Add the new messages near the bottom, after `UpgradeRoomResponse` (line ~635).
- Field numbers must not conflict with existing messages. Use field `1` for `room_id` in the request (consistent with all other request messages).
- After editing the proto, run `make proto`. This regenerates:
  - `gateway/internal/grpc/pb/core.pb.go`
  - `gateway/internal/grpc/pb/core_grpc.pb.go`
  - `core/apps/event_dispatcher/lib/pb/core.pb.ex`
  - `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex`
- **Do not hand-edit the generated files.**

### Elixir: display_name decryption

Look at how `get_admin_user` / `list_admin_users` in `server.ex` decrypts `display_name_encrypted`:
- File: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
- Search for `decrypt_display_name` or `Nebu.Crypto` or `display_name_encrypted` to find the decryption helper.
- Use the **same** decryption call for `list_admin_room_members`. Do not invent a new approach.
- If decryption fails for a member, set `display_name = ""` (do not crash the handler — other members still render).

### Elixir: DB module DI pattern

All DB calls in `server.ex` go through `admin_db_module()`:
```elixir
def admin_db_module do
  Application.get_env(:event_dispatcher, :admin_db_module, Nebu.Admin.DB)
end
```
Tests override this with `Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)`. Add `list_room_members/1` to both `Nebu.Admin.DB` and to any `FakeAdminDB` used in existing tests.

### Go: fake client in tests

Two test fakes implement `AdminRoomsClient` (and/or `pb.CoreServiceClient`):
- `captureContextClient` in `admin_grpc_actor_identity_test.go` (~line 38)
- `mockCoreClient` in a test helper file

Both embed no-op stubs. After adding `ListAdminRoomMembers` to the interface, add a no-op stub to each:
```go
func (c *captureContextClient) ListAdminRoomMembers(_ context.Context, _ *pb.ListAdminRoomMembersRequest) (*pb.ListAdminRoomMembersResponse, error) {
    return &pb.ListAdminRoomMembersResponse{}, nil
}
```
Run `make test-unit-go` to catch any other fakes that need the same addition.

### Go: `DetailHandler` gRPC path structure

Current `DetailHandler` (gRPC path) in `rooms.go`:
1. Calls `GetAdminRoom` → returns `room *StubRoom`
2. Calls `ListAdminRooms` → populates sidebar

Add step 1b between steps 1 and 2:
```go
// Fetch member list (Story 9.18) — non-fatal on error
var members []RoomMemberData
membResp, membErr := h.core.ListAdminRoomMembers(r.Context(), &pb.ListAdminRoomMembersRequest{RoomId: roomID})
if membErr != nil {
    slog.Warn("admin: ListAdminRoomMembers gRPC error", "room_id", roomID, "err", membErr)
    // Continue with empty list; detail panel still renders.
} else {
    for _, m := range membResp.GetMembers() {
        members = append(members, RoomMemberData{
            UserID:      m.GetUserId(),
            DisplayName: m.GetDisplayName(),
            JoinedAt:    m.GetJoinedAt(),
        })
    }
}
```
Then pass `members` to `data.ActiveRoomMembers`.

### Template: DaisyUI conventions

Follow the same patterns used elsewhere in `rooms.html` and `users.html`:
- Use `divide-y divide-base-300` for list separators.
- Use `text-sm` for body text, `text-base-content/60 text-xs uppercase tracking-wide` for section headers.
- Links use `hover:underline` — no DaisyUI `link` class (it adds an underline by default which clashes with the truncate class).
- Do NOT add a new route or handler for this — it is purely template data rendered in `DetailHandler`.

### Security: no new sensitive data exposure

`display_name` is Tier 1 PII (operational, decrypted in flight). The Admin UI already displays decrypted display names in the Users list (Story 9.2). This is consistent. No `security_review: required` needed.

Do NOT return `email_encrypted` or any Tier 2 PII in `AdminRoomMemberProto`.

### Build commands

```bash
make proto              # regenerate after proto change
make test-unit-go       # Go unit tests
make test-unit-elixir   # Elixir unit tests
```

---

### Project Structure Notes

**Files to CREATE:** none  
**Files to MODIFY:**

| File | Change |
|---|---|
| `proto/core.proto` | Add `AdminRoomMemberProto`, `ListAdminRoomMembersRequest/Response`, `ListAdminRoomMembers` RPC |
| `gateway/internal/grpc/pb/core.pb.go` | Regenerated by `make proto` — do NOT hand-edit |
| `gateway/internal/grpc/pb/core_grpc.pb.go` | Regenerated by `make proto` — do NOT hand-edit |
| `core/apps/event_dispatcher/lib/pb/core.pb.ex` | Regenerated by `make proto` — do NOT hand-edit |
| `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` | Regenerated by `make proto` — do NOT hand-edit |
| `core/apps/event_dispatcher/lib/nebu/admin/db.ex` | Add `list_room_members/1` |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | Add `list_admin_room_members/2` handler |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/admin_grpc_test.exs` | Add ExUnit tests |
| `gateway/internal/grpc/client.go` | Add `ListAdminRoomMembers` method |
| `gateway/internal/admin/rooms.go` | Extend `AdminRoomsClient` interface; extend `DetailHandler` |
| `gateway/internal/admin/page_data.go` | Add `RoomMemberData`, `ActiveRoomMembers` field |
| `gateway/internal/admin/stubs.go` | Add `stubRoomMembers` map |
| `gateway/internal/admin/templates/rooms.html` | Add Members section to `detail_content` |
| `gateway/internal/admin/rooms_detail_test.go` | Add AC9 + AC10 unit tests |
| `gateway/internal/admin/admin_grpc_actor_identity_test.go` | Add no-op stub for new interface method |

Check for additional fakes: `grep -rn "ListAdminRooms\b" gateway/internal/admin/ --include="*_test.go"` — every file returned likely needs a `ListAdminRoomMembers` no-op stub added.

---

### References

- Proto file: `proto/core.proto` lines 124–139 (existing admin RPCs), 564–599 (AdminRoomProto messages)
- Elixir Admin DB: `core/apps/event_dispatcher/lib/nebu/admin/db.ex` — `get_room/1` (line 221) shows the DB pattern
- Elixir gRPC server: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` lines 1967–2022 (`list_admin_rooms`, `get_admin_room`)
- Go rooms handler: `gateway/internal/admin/rooms.go` lines 197–302 (`DetailHandler`)
- Go page data: `gateway/internal/admin/page_data.go` lines 164–220 (`StubRoom`, `RoomsPageData`)
- Go stubs: `gateway/internal/admin/stubs.go` lines 17–51 (`stubRooms`)
- Room detail template: `gateway/internal/admin/templates/rooms.html` lines 57–114 (`detail_content`)
- room_members schema: `gateway/migrations/000009_rooms.up.sql` lines 13–21
- Fake client with no-ops: `gateway/internal/admin/admin_grpc_actor_identity_test.go` line 38+

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

(none — clean implementation, no debug detours)

### Completion Notes List

- Task 1: Added `AdminRoomMemberProto`, `ListAdminRoomMembersRequest`, `ListAdminRoomMembersResponse` messages and `ListAdminRoomMembers` RPC to `proto/core.proto`. `make proto` regenerated Go and Elixir stubs successfully.
- Task 2: Added `list_room_members/1` to `Nebu.Admin.DB` with the SQL JOIN query (room_members JOIN users WHERE left_at IS NULL ORDER BY joined_at ASC). Added `list_admin_room_members/2` handler in `server.ex` that uses the existing `decrypt_display_name/1` helper — decryption failure yields `""` (non-fatal, consistent with existing pattern). 28 ExUnit tests in `admin_grpc_test.exs` pass including the 3 new Story 9.18 tests (AT#12, AT#13, display_name type safety).
- Task 3: Added `ListAdminRoomMembers` method to `gateway/internal/grpc/client.go` and to `AdminRoomsClient` interface in `rooms.go`. Added no-op stubs to 5 test fakes: `captureContextClient` (admin_grpc_actor_identity_test.go — pre-existing RED stub), `mockCoreClient` in auth_audit_test.go (pre-existing RED stub), plus newly added stubs in grpc/stream_test.go, audit/writer_test.go, compliance/handler_test.go.
- Task 4: Added `RoomMemberData` struct and `ActiveRoomMembers []RoomMemberData` field to `page_data.go`.
- Task 5: Added `stubRoomMembers` map (room-001: Alice Müller + Carla Reiter; room-002: Bob Wagner) to `stubs.go`. Hoisted `var members []RoomMemberData` to outer scope in `DetailHandler` so both gRPC and stub paths populate it. gRPC path calls `ListAdminRoomMembers` non-fatally; stub path uses `stubRoomMembers[roomID]` (nil for missing rooms). Passed `ActiveRoomMembers: members` to `RoomsPageData`.
- Task 6: Added `{{ if .ActiveRoomMembers }}` guarded Members section to `rooms.html` `detail_content` block following DaisyUI conventions.
- Task 7: Both `TestRoomDetailMemberListRenders` and `TestRoomDetailNoMembers` were pre-written as RED tests. All 16 Go packages pass `make test-unit-go` with no regressions.

### File List

- `proto/core.proto` — Added `AdminRoomMemberProto`, `ListAdminRoomMembersRequest/Response`, `ListAdminRoomMembers` RPC
- `gateway/internal/grpc/pb/core.pb.go` — Regenerated by `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — Regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — Regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — Regenerated by `make proto`
- `core/apps/event_dispatcher/lib/nebu/admin/db.ex` — Added `list_room_members/1`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — Added `list_admin_room_members/2` handler
- `gateway/internal/grpc/client.go` — Added `ListAdminRoomMembers` method
- `gateway/internal/admin/rooms.go` — Extended `AdminRoomsClient` interface; extended `DetailHandler`
- `gateway/internal/admin/page_data.go` — Added `RoomMemberData` struct and `ActiveRoomMembers` field
- `gateway/internal/admin/stubs.go` — Added `stubRoomMembers` map
- `gateway/internal/admin/templates/rooms.html` — Added Members section to `detail_content`
- `gateway/internal/grpc/stream_test.go` — Added `ListAdminRoomMembers` no-op stub
- `gateway/internal/audit/writer_test.go` — Added `ListAdminRoomMembers` no-op stub
- `gateway/internal/compliance/handler_test.go` — Added `ListAdminRoomMembers` no-op stub

## Change Log

| Date | Change |
|---|---|
| 2026-05-05 | Story implemented: Added `ListAdminRoomMembers` gRPC RPC end-to-end (proto → Elixir Core → Go gRPC client → `AdminRoomsClient` interface → `DetailHandler` → `RoomsPageData.ActiveRoomMembers` → `rooms.html` Members section). All 7 tasks complete. `make test-unit-go` (16 packages, 0 failures) and `make test-unit-elixir` (admin_grpc_test.exs 28 tests, 0 failures) green. |

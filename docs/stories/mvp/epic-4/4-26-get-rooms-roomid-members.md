# Story 4.26: GET /rooms/{roomId}/members — Mitgliederliste für Element Web

Status: done

## Story

As a developer,
I want `GET /_matrix/client/v3/rooms/{roomId}/members` to return the current member list from Core,
so that Element Web correctly populates the member sidebar instead of showing "Room members will appear incomplete."

---

## Background / Motivation

After entering a room, Element Web calls `GET /rooms/{roomId}/members` to populate the member panel.
Without this endpoint, the request returned 404 → member list stayed empty → "Fetching room members failed" error in console.

The `GetRoomState` gRPC call (proto + generated code) already exists and returns `repeated string members`.

---

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/members` is registered in `main.go` with JWT middleware.

2. The handler calls `Core.GetRoomState(roomId)` and shapes each member ID into a Matrix `m.room.member` state event with `"membership": "join"`.

3. Response format: `{"chunk": [{...m.room.member event...}]}` — `chunk` is always an array, never null.

4. Empty room → 200 `{"chunk": []}`.

5. gRPC `NotFound` → 404 `M_NOT_FOUND`.

6. gRPC `PermissionDenied` → 403 `M_FORBIDDEN`.

7. gRPC `Unavailable` → 503 `M_UNAVAILABLE`.

8. Unauthenticated → 401 `M_MISSING_TOKEN`.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestGetRoomMembers_HappyPath` — Go httptest
   - Given: mock Core returns `["@alice:test.local","@bob:test.local"]`
   - Then: 200, chunk has 2 entries, each with `type: "m.room.member"`, `state_key`, `content.membership: "join"`
   - And: `capturedReq.RoomId == "!room1:test.local"`

2. `TestGetRoomMembers_EmptyRoom` — Go httptest
   - Given: mock returns empty members slice
   - Then: 200, `chunk` is empty array (not null)

3. `TestGetRoomMembers_RoomNotFound` — gRPC NotFound → 404 M_NOT_FOUND

4. `TestGetRoomMembers_NotMember` — gRPC PermissionDenied → 403 M_FORBIDDEN

5. `TestGetRoomMembers_Unauthenticated` — 401, Core must NOT be called

6. `TestGetRoomMembers_CoreUnavailable` — gRPC Unavailable → 503 M_UNAVAILABLE

7. Browser E2E: "Member list populated after joining room"
   - Given: SSO login, room created, navigate to room
   - When: member panel opened
   - Then: at least one member visible (not empty/error)

---

## Implementation Notes

- Handler: `gateway/internal/matrix/members.go` — `GetRoomMembersHandler`
- Interface: `GetMembersCoreClient` (consumer-defined, minimal — Go ADR-009)
- Uses existing `pb.GetRoomStateRequest/Response` — no proto changes needed
- Registered in `main.go` alongside receipt handler

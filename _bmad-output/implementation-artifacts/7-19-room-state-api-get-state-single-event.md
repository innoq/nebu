---
id: 7-19
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.19: Room State API — GET /rooms/{roomId}/state

Status: review

## Story

As an end-user,
I want to retrieve the current state of a room (all state events, or a single state event by type),
so that my Matrix client can display room metadata, power levels, and membership correctly.

## Context / Background

Matrix clients use the `/state` endpoints to bootstrap room metadata before rendering a room view.
Two variants exist:

- `GET /rooms/{roomId}/state` — returns all current state events as an array.
- `GET /rooms/{roomId}/state/{eventType}/{stateKey}` and the empty-stateKey form
  `GET /rooms/{roomId}/state/{eventType}` — return just the `content` block of a single state event.

The existing `GetRoomState` gRPC call in `proto/core.proto` returns `members` + `power_levels_json`
and is designed for the `/members` handler. It needs to be **extended** so it can also return a full
array of typed state events with their state_key and content, and support filtered lookups by
`event_type` + `state_key`. The `SyncRoomStateEvent` message already exists in the proto and can be
reused as the element type.

The handler goes into `gateway/internal/matrix/rooms.go` (existing file, alongside other room
handlers). Route registration in `gateway/cmd/gateway/main.go` uses `jwtMiddleware`.

Existing `GetRoomStateRequest` / `GetRoomStateResponse` must be extended without breaking callers:

```protobuf
message GetRoomStateRequest {
  string room_id    = 1;
  string event_type = 2;  // optional filter; empty = return all
  string state_key  = 3;  // optional; only meaningful when event_type is set
}
message GetRoomStateResponse {
  repeated string            members           = 1;  // kept for /members backward compat
  string                     power_levels_json = 2;  // kept for backward compat
  string                     room_name         = 3;  // kept for backward compat
  repeated SyncRoomStateEvent state_events     = 4;  // NEW: full state array
}
```

The Elixir `Room.GenServer` already tracks the full state map — populating `state_events` is a matter
of serialising that map in the gRPC handler.

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/state` returns HTTP 200 with a JSON array of all current
   state events (`type`, `state_key`, `content`, `sender`) for authenticated room members.

2. `GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}` returns HTTP 200 with only
   the `content` JSON object of the matching state event (Matrix spec: just the content, no envelope).

3. `GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}` (no stateKey path segment) is
   equivalent to stateKey `""` — same response shape as AC2.

4. Returns `403 M_FORBIDDEN` with Matrix error body when the requesting user is not a room member.

5. Returns `404 M_NOT_FOUND` when the room does not exist.

6. Returns `404 M_NOT_FOUND` when a specific `eventType`/`stateKey` combination has no current
   state event in the room.

7. JWT required — requests without a valid token are rejected by `jwtMiddleware` before the handler
   is reached (no change to middleware layer).

8. Proto: `GetRoomStateRequest` gains optional `event_type` (field 2) and `state_key` (field 3)
   without breaking the existing `/members` caller (both fields default to empty string = return all).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GetRoomState_AllEvents] — Godog
   - Given: authenticated user who is a member of room `!test:server`
   - When: `GET /_matrix/client/v3/rooms/!test:server/state`
   - Then: HTTP 200; response is a JSON array containing at least one event with keys
     `type`, `state_key`, `content`, `sender`

2. [GetRoomState_SingleEvent_WithStateKey] — Godog
   - Given: authenticated user who is a member, room has `m.room.member` state for `@alice:server`
   - When: `GET /_matrix/client/v3/rooms/!test:server/state/m.room.member/@alice:server`
   - Then: HTTP 200; response body is the raw content object (e.g. `{"membership":"join"}`)

3. [GetRoomState_SingleEvent_EmptyStateKey] — Godog
   - Given: authenticated user who is a member, room has an `m.room.name` state event
   - When: `GET /_matrix/client/v3/rooms/!test:server/state/m.room.name`
   - Then: HTTP 200; response body is the content of `m.room.name` with state_key `""`

4. [GetRoomState_Forbidden_NonMember] — Godog
   - Given: authenticated user who is NOT a member of `!private:server`
   - When: `GET /_matrix/client/v3/rooms/!private:server/state`
   - Then: HTTP 403 `{"errcode":"M_FORBIDDEN",...}`

5. [GetRoomState_NotFound_UnknownEventType] — Godog
   - Given: authenticated member of `!test:server`
   - When: `GET /_matrix/client/v3/rooms/!test:server/state/m.room.nonexistent/`
   - Then: HTTP 404 `{"errcode":"M_NOT_FOUND",...}`

## Implementation Notes

**Files to create/modify:**

- `proto/core.proto` — Extend `GetRoomStateRequest` with optional `event_type` (field 2) and
  `state_key` (field 3). Extend `GetRoomStateResponse` with `repeated SyncRoomStateEvent state_events`
  (field 4). Run `make proto` to regenerate stubs.
- `gateway/internal/matrix/rooms.go` — Add `GetRoomStateHandler` struct + `GetRoomState` and
  `GetRoomStateSingleEvent` methods. The `GetMembersCoreClient` interface in `members.go` already
  declares `GetRoomState` — the new handler needs its own minimal interface declaration.
- `gateway/cmd/gateway/main.go` (~line 404+) — Register three routes:
  ```
  GET /_matrix/client/v3/rooms/{roomId}/state
  GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}
  GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}
  ```
  all wrapped in `jwtMiddleware`.
- `core/apps/room_manager/` — Extend gRPC handler to populate `state_events` from the GenServer
  state map; apply `event_type`/`state_key` filter when non-empty.
- `gateway/internal/matrix/rooms_test.go` — Unit tests covering all AC using `httptest`.
- `gateway/features/room_state.feature` — Godog feature file (tests written first, red phase).

**Error-mapping pattern** (consistent with existing handlers in `rooms.go`):
- `codes.PermissionDenied` → 403 `M_FORBIDDEN`
- `codes.NotFound` → 404 `M_NOT_FOUND`
- `codes.Unavailable` → 503 `M_UNAVAILABLE`
- default → 500 `M_UNKNOWN`

**Note on empty state_events:** When `event_type` is set but no matching event exists, the Elixir
handler should return `codes.NotFound`. When returning all state events, an empty array is valid
(e.g. newly created room before any state is set beyond the initial events).

## Tasks/Subtasks

- [x] Task 1: Extend proto/core.proto with event_type, state_key, state_events fields
  - [x] Add event_type (field 2) and state_key (field 3) to GetRoomStateRequest
  - [x] Add repeated SyncRoomStateEvent state_events (field 4) to GetRoomStateResponse
  - [x] Run make proto to regenerate Go and Elixir stubs
- [x] Task 2: Implement GetRoomStateHandler in gateway/internal/matrix/rooms.go
  - [x] Declare GetRoomStateCoreClient interface (consumer-defined, ADR-009)
  - [x] Implement GetRoomStateConfig, GetRoomStateHandler, NewGetRoomStateHandler
  - [x] Implement GetRoomState (AC1: 200 JSON array of all state events)
  - [x] Implement GetRoomStateSingleEvent (AC2/AC3: 200 raw content object)
  - [x] Implement grpcErrToMatrixState error mapper (AC4/AC5/AC6)
- [x] Task 3: Register routes in gateway/cmd/gateway/main.go
  - [x] GET /rooms/{roomId}/state
  - [x] GET /rooms/{roomId}/state/{eventType}/{stateKey}
  - [x] GET /rooms/{roomId}/state/{eventType}/  (trailing-slash subtree for empty stateKey)
  - [x] GET /rooms/{roomId}/state/{eventType}
- [x] Task 4: Extend Elixir gRPC handler in event_dispatcher/server.ex
  - [x] Populate state_events in GetRoomStateResponse from build_state_events
  - [x] Apply event_type/state_key filter when non-empty
  - [x] Raise GRPC.RPCError not_found when filter matches no events
- [x] Task 5: Remove duplicate GetRoomStateCoreClient interface from test file
  - [x] Interface moved to rooms.go; test file redacted duplicate declaration

## Dev Agent Record

### Implementation Plan

1. Extended `proto/core.proto`: added `event_type` (field 2) and `state_key` (field 3) to `GetRoomStateRequest`; added `repeated SyncRoomStateEvent state_events` (field 4) to `GetRoomStateResponse`. Ran `make proto` — both Go and Elixir stubs regenerated cleanly.

2. Added `GetRoomStateCoreClient` interface, `GetRoomStateConfig`, `GetRoomStateHandler`, `NewGetRoomStateHandler`, `GetRoomState`, `GetRoomStateSingleEvent`, and `grpcErrToMatrixState` to `gateway/internal/matrix/rooms.go`.

3. `GetRoomState` (AC1): sends `GetRoomStateRequest{RoomId: roomID}` (empty event_type/state_key = return all), builds a `[]stateEventJSON` array with matrix envelope shape, returns `[]` (never null) for empty rooms.

4. `GetRoomStateSingleEvent` (AC2/AC3): passes event_type+state_key to Core, returns raw content object (no envelope). Empty stateKey when `{stateKey}` path segment is absent. Returns 404 if Core returns empty state_events list.

5. Route registration: four patterns in main.go — including `{eventType}/` (trailing-slash subtree) to handle paths like `state/m.room.nonexistent/` where stateKey is an empty segment (Go 1.22 mux does not match `{wildcard}` to empty segments).

6. Elixir `get_room_state/2`: extended to build `all_state_events = build_state_events(state, room_id)`, apply event_type/state_key filter when non-empty, raise `GRPC.RPCError not_found` when no events match, populate `state_events:` in response.

7. Removed duplicate `GetRoomStateCoreClient` interface declaration from test file (it was the placeholder until rooms.go was implemented).

### Completion Notes

- All 10 unit tests pass (TestGetRoomState_* in room_state_test.go).
- Full test suite: make test-unit-go — all packages pass, no regressions.
- Backward compat verified: GetRoomMembersHandler still sends RoomId-only request (EventType/StateKey default to ""); existing members tests pass.
- Proto AC8 backward compat: new fields are optional with empty-string defaults; existing /members caller unaffected.
- Trailing-slash handling: Go 1.22 mux `{wildcard}` requires non-empty segments, so a dedicated `{eventType}/` subtree route handles the `state/m.room.nonexistent/` case.

## File List

- `proto/core.proto` — extended GetRoomStateRequest (fields 2+3) and GetRoomStateResponse (field 4)
- `gateway/internal/grpc/pb/core.pb.go` — regenerated by make proto
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated by make proto
- `gateway/internal/matrix/rooms.go` — added GetRoomStateHandler, GetRoomStateCoreClient, GetRoomStateConfig, GetRoomState, GetRoomStateSingleEvent, grpcErrToMatrixState
- `gateway/cmd/gateway/main.go` — registered 4 GET /state routes
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — extended get_room_state/2 to populate state_events with optional filter
- `gateway/internal/matrix/room_state_test.go` — removed duplicate GetRoomStateCoreClient interface; added trailing-slash route to test mux

## Change Log

- 2026-04-30: Implemented story 7-19 — Room State API (GET /rooms/{roomId}/state). Extended proto, added Go handler + 4 routes, extended Elixir gRPC handler with state_events population and event_type/state_key filtering. All 10 unit tests pass, no regressions.

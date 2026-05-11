---
status: ready-for-dev
epic: 9
story: 7
security_review: optional
---

# Story 9.7: Room State Event Types — Full Implementation

Status: ready-for-dev

## Story

As a Matrix client user,
I want the server to correctly store and return all standard Matrix room state event types,
So that room name, topic, avatar, join rules, and history visibility behave as per spec.

## Acceptance Criteria

1. `PUT /rooms/{roomId}/state/m.room.name` with `{"name": "My Room"}` → state persisted in Core and subsequent `GET /rooms/{roomId}/state/m.room.name` returns `{"name": "My Room"}`
2. `PUT /rooms/{roomId}/state/m.room.encryption` with `{"algorithm": "m.megolm.v1.aes-sha2"}` → stores and returns 200 with an event_id — NOT 501
3. `PUT /rooms/{roomId}/state/m.room.join_rules` with `{"join_rule": "invite"}` → state persisted and `GET /sync` reflects the updated join_rules in the room state
4. `gateway/internal/matrix/rooms.go` line ~397: the 501 fallback is replaced by a Core delegation for all whitelisted types
5. All 16 whitelisted state event types sent in sequence → `GET /rooms/{roomId}/state` returns all state events with correct `type`, `content`, and `state_key` fields

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AC1 — PutSetRoomState_mRoomName_Returns200** — Go unit test (httptest)
   - Given: `PUT /rooms/!room1:test.local/state/m.room.name` with valid JWT and `{"name":"My Room"}`
   - When: handler processes the request with Core.SendEvent mock returning success
   - Then: response is 200 `{"event_id":""}` (NOT 501)

2. **AC2 — PutSetRoomState_mRoomEncryption_Returns200** — Go unit test (httptest)
   - Given: `PUT /rooms/!room1:test.local/state/m.room.encryption` with `{"algorithm":"m.megolm.v1.aes-sha2"}`
   - When: handler processes the request
   - Then: response is 200 `{"event_id":""}` (NOT 501)

3. **AC4 — No501Fallback** — Go unit test (httptest)
   - Given: `PUT /rooms/!room1:test.local/state/m.room.topic` with `{"topic":"Welcome"}`
   - When: handler processes the request
   - Then: response is NOT 501 (the fallback must be gone)

4. **AC5 — Godog E2E: set_room_state_full.feature** — Godog integration test
   - Scenario 1: kai sets m.room.name → GET /state/m.room.name returns `{"name":"..."}` 200
   - Scenario 2: kai sets m.room.topic → GET /state/m.room.topic returns `{"topic":"..."}` 200
   - Scenario 3: kai sets m.room.join_rules → GET /sync includes updated join_rules in room state
   - Scenario 4: non-member gets 403 on PUT /state/m.room.name

## Technical Implementation Plan

### Root Cause

`PutSetRoomState` in `gateway/internal/matrix/rooms.go` (line ~396) currently has:
```go
// For other state event types, return 501 Not Implemented (MVP scope).
writeMatrixError(w, http.StatusNotImplemented, "M_UNRECOGNIZED", "Unsupported state event type")
```

This fallback must be replaced with a `Core.SendEvent` gRPC call.

### Protocol Decision: Use Existing `SendEvent` RPC (NOT a new RPC)

The proto has NO dedicated `SetRoomState` RPC. The correct approach is to reuse `SendEvent` (already used by `PutSendEventHandler`). State events ARE events — they go through the Room GenServer `send_event/5` path, which:
1. Signs the event (Ed25519)
2. Persists to DB via `insert_event`
3. Sets `state_key` in the event map (persisted as part of the signed event)
4. Broadcasts via `:pg`

The `stateKey` URL param from `r.PathValue("stateKey")` becomes the `state_key` field. For most state event types (m.room.name, m.room.topic, etc.) the state_key is `""` (empty string). For `m.room.member`, the state_key is the user_id.

### Key Implementation Note on `state_key`

The current `SendEventRequest` proto message does NOT have a `state_key` field:
```protobuf
message SendEventRequest {
  string room_id    = 1;
  string sender_id  = 2;
  string event_type = 3;
  string txn_id     = 4;
  bytes  content    = 5;
  int64  origin_ts  = 6;
}
```

**State events need a `state_key`.** Two options:
- **Option A (preferred, no proto change needed):** Encode the state_key into the content JSON alongside the real content, and have the Elixir handler extract it. BUT this would be a protocol hack.
- **Option B (clean, requires proto change):** Add `string state_key = 7` to `SendEventRequest`. This requires `make proto` and Elixir Core change.
- **Option C (simplest):** Pass the state_key as part of the `txn_id` field (hack, not recommended).
- **Option D (correct and clean):** Add `state_key` to `SendEventRequest` protobuf. This is the right call.

**Use Option B (add `state_key` field 7 to `SendEventRequest`).** Proto changes flow through:
1. `proto/core.proto` — add field 7 `string state_key`
2. `make proto` — regenerates `gateway/internal/grpc/pb/core.pb.go` + `core/apps/event_dispatcher/lib/pb/core.pb.ex`
3. Elixir `send_event/2` handler in `server.ex` — extract `state_key = request.state_key`; pass to `Nebu.Room.Server.send_event/6` (or include in event_map directly)
4. `Nebu.Room.Server.handle_call(:send_event)` — add `state_key` to the event_map so it's persisted and signed correctly
5. Go handler — set `StateKey` in `SendEventRequest`

### Alternative: Embed state_key in content (avoid proto change)

To avoid proto + Elixir changes, one could encode `__state_key__` in the content JSON, but this would corrupt the event content stored in DB. **Do NOT do this.**

### Preferred Approach Summary

1. **Proto**: Add `string state_key = 7` to `SendEventRequest`
2. **Elixir Core** `send_event/2` in `server.ex`: extract `state_key` from request and include it in the event_map: `"state_key" => state_key`
3. **Elixir Core** `Nebu.Room.Server.handle_call({:send_event,...})`: the event_map already includes `state_key` when provided
4. **Go Gateway** `SetRoomStateCoreClient` interface: add `SendEvent` method
5. **Go Gateway** `SetRoomStateHandler`: replace 501 fallback with `SendEvent` gRPC call

### Files to Change

#### NEW files:
- `gateway/internal/matrix/set_room_state_full_test.go` — unit tests AC1-AC4 (httptest)
- `gateway/features/set_room_state_full.feature` — Godog scenarios AC5
- `gateway/test/integration/set_room_state_full_steps_test.go` — Godog step definitions

#### MODIFIED files:
- `proto/core.proto` — add `string state_key = 7` to `SendEventRequest`
- `gateway/internal/grpc/pb/core.pb.go` — regenerated (via `make proto`)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated (via `make proto`)
- `gateway/internal/matrix/rooms.go` — `SetRoomStateCoreClient` interface + `PutSetRoomState` handler
- `gateway/internal/grpc/client.go` — `Client.SendEvent` already exists, just ensure interface matches
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `send_event/2`: extract `state_key`
- `core/apps/room_manager/lib/nebu/room/server.ex` — `handle_call(:send_event)`: include `state_key` in event_map

#### Optionally regenerated (NOT manually edited):
- `gateway/internal/grpc/pb/core_grpc.pb.go`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex`

## Go Gateway Changes

### 1. Extend `SetRoomStateCoreClient` interface

Current interface (rooms.go ~line 306):
```go
type SetRoomStateCoreClient interface {
    SetPowerLevels(ctx context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error)
}
```

New interface:
```go
type SetRoomStateCoreClient interface {
    SetPowerLevels(ctx context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error)
    SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error)
}
```

`gateway/internal/grpc.Client` already implements both — no change needed in `client.go`.

### 2. Replace 501 fallback in `PutSetRoomState`

The existing `PutSetRoomState` flow after Story 9-6:
1. Whitelist check → 400 if unknown type
2. Extract userID, systemRole from context
3. Decode JSON body
4. If `m.room.power_levels`: call `SetPowerLevels`
5. **ELSE: return 501** ← REPLACE THIS

Replace step 5 with:
```go
// For all other whitelisted state event types: delegate to Core via SendEvent.
// State events follow the same persistence path as regular events — the Room
// GenServer signs, persists, and broadcasts them. The state_key identifies
// which "slot" the event occupies in the room state (e.g. "" for m.room.name,
// userId for m.room.member).
stateKey := r.PathValue("stateKey")

contentJSON, err := json.Marshal(body)
if err != nil {
    writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Cannot encode state event content")
    return
}

resp, err := h.coreClient.SendEvent(grpcCtx, &pb.SendEventRequest{
    RoomId:    roomID,
    SenderId:  userID,
    EventType: eventType,
    TxnId:     "", // state events are idempotent by (roomId, eventType, stateKey) — txn_id not needed
    Content:   contentJSON,
    OriginTs:  time.Now().UnixMilli(),
    StateKey:  stateKey,
})
if err != nil {
    st, _ := status.FromError(err)
    switch st.Code() {
    case codes.PermissionDenied:
        writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to set this state event")
    case codes.NotFound:
        writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
    default:
        writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
    }
    return
}

w.Header().Set("Content-Type", "application/json")
_ = json.NewEncoder(w).Encode(setRoomStateResponse{EventID: resp.EventId})
```

Add `"time"` import if not already present.

### 3. `txn_id` for state events

State events are idempotent by `(room_id, event_type, state_key)` — the Room GenServer will overwrite the previous state on each write (latest write wins, same as Matrix spec). The `txn_id` field can be left as `""` for state events. The existing ETS dedup in Core is keyed on `{room_id, user_id, txn_id}` — with `txn_id=""`, the ETS dedup is effectively disabled for state events (each PUT creates a new event). This matches the Matrix spec: state events are not idempotent by txn_id, they're idempotent by `(type, state_key)`.

## Elixir Core Changes

### 1. `proto/core.proto` — add `state_key` to `SendEventRequest`

```protobuf
message SendEventRequest {
  string room_id    = 1;
  string sender_id  = 2;
  string event_type = 3;
  string txn_id     = 4;
  bytes  content    = 5;
  int64  origin_ts  = 6;
  string state_key  = 7;  // Story 9-7: state events only; empty for regular events
}
```

Run `make proto` after this change. This regenerates:
- `gateway/internal/grpc/pb/core.pb.go` (Go side)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` (Elixir side)

### 2. `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `send_event/2`

Current (line ~72):
```elixir
def send_event(request, _stream) do
  room_id = request.room_id
  sender_id = request.sender_id
  event_type = request.event_type
  txn_id = request.txn_id
  content = ...
```

Add extraction of `state_key` (will be `""` for non-state events):
```elixir
state_key = Map.get(request, :state_key, "")
```

Then pass it to `Nebu.Room.Server.send_event/6`:
```elixir
case Nebu.Room.Server.send_event(room_id, sender_id, event_type, content, txn_id, state_key) do
```

### 3. `core/apps/room_manager/lib/nebu/room/server.ex` — `send_event/6` and `handle_call`

Current public API (line ~147):
```elixir
def send_event(room_id, user_id, event_type, content, txn_id) do
  GenServer.call(via(room_id), {:send_event, user_id, event_type, content, txn_id})
end
```

Add `state_key` parameter (default `""`):
```elixir
def send_event(room_id, user_id, event_type, content, txn_id, state_key \\ "") do
  GenServer.call(via(room_id), {:send_event, user_id, event_type, content, txn_id, state_key})
end
```

Update `handle_call` pattern:
```elixir
def handle_call({:send_event, user_id, event_type, content, txn_id, state_key}, _from, state) do
```

In the event_map construction (Step 2), add `state_key`:
```elixir
event_map = %{
  "room_id"          => room_id,
  "type"             => event_type,
  "state_key"        => state_key,   # NEW: "" for regular events, set for state events
  "sender"           => user_id,
  "content"          => content,
  "origin_server_ts" => Nebu.DB.Helpers.now_ms()
}
```

**Backward compatibility**: The existing `handle_call({:send_event, user_id, event_type, content, txn_id}, ...)` pattern (5-arg) must still be handled OR all callers updated. Since `Nebu.Room.Server.send_event/5` is called internally by `create_room` (not directly but via `set_power_levels` etc.), ensure the default parameter handles this.

The cleanest approach: use a 5-arg + 6-arg multi-head pattern:
```elixir
def handle_call({:send_event, user_id, event_type, content, txn_id}, from, state) do
  handle_call({:send_event, user_id, event_type, content, txn_id, ""}, from, state)
end
def handle_call({:send_event, user_id, event_type, content, txn_id, state_key}, _from, state) do
  # ... real implementation
end
```

### 4. `build_state_events` in `server.ex` — no change needed for MVP

The `build_state_events/2` function currently assembles state events from DB lookups (room_name) and in-memory state (members, power_levels). State events set via `PUT /state/{eventType}` are persisted by `insert_event` and will flow through the `get_room_state` query path via the DB. For a more robust implementation, state events should be loaded from the `events` table via a query like:

```sql
SELECT DISTINCT ON (event_type, state_key) event_type, state_key, content, sender
FROM events
WHERE room_id = $1 AND state_key IS NOT NULL
ORDER BY event_type, state_key, origin_server_ts DESC
```

However, for this story's AC, the key requirement is that `PUT /state/m.room.name` stores the event AND `GET /state/m.room.name` returns it. The existing `build_state_events` already reads `m.room.name` from the DB via `get_room_name/1`. For other types (topic, join_rules, etc.), the events table stores them but `build_state_events` doesn't yet read them back.

**Scope decision for Story 9-7:** The story AC says the state is "persisted in Core" and GET returns it. Full coverage of `build_state_events` reading all whitelisted event types from the DB is REQUIRED for AC5 (GET /sync reflects updated state). This means `build_state_events` must be extended to include a DB query for additional state event types.

The simplest extension: add a helper `get_generic_state_events(room_id)` that reads from the events table the most recent event per `(event_type, state_key)` for all non-member, non-power_levels, non-create events.

## Unit Test Pattern for Go (httptest)

The new unit tests must extend the existing `mockSetRoomStateCoreClient`:

```go
// Extended mock to capture SendEvent calls
type mockSetRoomStateCoreClientV2 struct {
    setPowerLevelsResp *pb.SetPowerLevelsResponse
    setPowerLevelsErr  error
    capturedPLReq      *pb.SetPowerLevelsRequest

    sendEventResp *pb.SendEventResponse
    sendEventErr  error
    capturedSEReq *pb.SendEventRequest
}

func (m *mockSetRoomStateCoreClientV2) SetPowerLevels(ctx context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error) {
    m.capturedPLReq = req
    return m.setPowerLevelsResp, m.setPowerLevelsErr
}

func (m *mockSetRoomStateCoreClientV2) SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
    m.capturedSEReq = req
    return m.sendEventResp, m.sendEventErr
}
```

**IMPORTANT**: The existing `mockSetRoomStateCoreClient` in `rooms_test.go` only implements `SetPowerLevels`. After this story, `SetRoomStateCoreClient` requires BOTH `SetPowerLevels` and `SendEvent`. The existing mock must be updated to also implement `SendEvent`, OR a new mock is created in the new test file. Preferred: update the existing mock in `rooms_test.go` to add the `SendEvent` method (returns `nil, nil` by default so existing tests are unaffected).

## Godog Feature File

`gateway/features/set_room_state_full.feature`:

```gherkin
Feature: Room State Event Full Implementation — PUT /rooms/{roomId}/state/{eventType}
  As a Matrix client user
  I want to set and retrieve room state events for all standard types
  So that room name, topic, join rules, and other state persists correctly

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "state-impl-test-room"

  Scenario: SetRoomName_Persisted — m.room.name state event is stored and retrievable
    When kai sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Test Room"}
    Then the response status is 200
    And the response body contains "event_id"
    When kai calls GET /rooms/{roomId}/state/m.room.name
    Then the response status is 200
    And the response body contains "Test Room"

  Scenario: SetRoomTopic_Persisted — m.room.topic state event is stored
    When kai sends PUT /rooms/{roomId}/state/m.room.topic with body {"topic":"Welcome to the room"}
    Then the response status is 200
    When kai calls GET /rooms/{roomId}/state/m.room.topic
    Then the response status is 200
    And the response body contains "Welcome to the room"

  Scenario: SetEncryption_NotRejectedWith501 — m.room.encryption returns 200 not 501
    When kai sends PUT /rooms/{roomId}/state/m.room.encryption with body {"algorithm":"m.megolm.v1.aes-sha2"}
    Then the response status is 200

  Scenario: SetRoomName_NonMemberForbidden — non-member cannot set state
    Given marie is authenticated via OIDC
    When marie sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Hijacked"}
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"
```

## Integration Test Steps

`gateway/test/integration/set_room_state_full_steps_test.go` must define:
- `kaiSendsPutRoomState(eventType, body string) error` — PUT with kai's access token
- `marieSendsPutRoomState(eventType, body string) error` — PUT with marie's access token
- `theResponseBodyContains(substr string) error` — reuse existing step if present

Check `room_flow_steps_test.go` for existing steps like `theResponseBodyContains` — avoid duplicates.

## Critical: Do NOT Duplicate Existing Steps

Many generic assertion steps exist in `gateway/test/integration/steps_test.go` and `room_flow_steps_test.go`:
- `theResponseStatusIs(code int)` — already registered
- `theResponseBodyContains(substr)` — check if already registered before adding
- `theDockerComposeStackIsStarted` — already in Background handling

Only register new steps that don't already exist.

## Error Mapping (Go side)

Map gRPC errors for state events identically to how `SetPowerLevels` maps them:
- `codes.PermissionDenied` → `403 M_FORBIDDEN`
- `codes.NotFound` → `404 M_NOT_FOUND`
- other → `500 M_UNKNOWN`

## Important Constraints

### What changes are REQUIRED by this story:
1. Proto `SendEventRequest` gets `state_key` field 7
2. `make proto` regenerates both Go and Elixir stubs
3. Elixir `send_event/2` gRPC handler extracts `state_key`
4. Elixir `Room.Server.send_event` includes `state_key` in event_map
5. `build_state_events` extended to read generic state events from DB
6. Go `SetRoomStateCoreClient` interface gets `SendEvent` method
7. Go `PutSetRoomState` replaces 501 with `SendEvent` gRPC call
8. Existing `mockSetRoomStateCoreClient` updated with `SendEvent` stub
9. New unit tests + Godog feature + Godog step definitions

### What must NOT be broken:
- `TestPutSetRoomState_HappyPath` — uses `m.room.power_levels`, must still return 200
- `TestPutSetRoomState_Forbidden` — power_levels path, must still return 403
- `TestPutSetRoomState_Unauthenticated` — JWT path, must still return 401
- `TestPutSetRoomState_RoomNotFound` — NotFound path, still 404
- All whitelist tests in `state_event_whitelist_test.go` must remain green
- Existing `SendEvent` unit tests in `rooms_test.go` for `PutSendEventHandler` — unaffected (different handler)
- All Godog scenarios in `state_event_whitelist.feature` must remain green

### Build / test commands:
```bash
make proto            # regenerate pb stubs after proto change
make test-unit-go     # run all Go unit tests
make test-unit-elixir # run all Elixir unit tests
make test-integration # full stack E2E (docker compose + Godog)
```

## DB Persistence Notes

State events set via `PUT /state/{eventType}` are persisted by `insert_event` in the `events` table. The `state_key` column must exist. Check the current DB schema:

```sql
-- events table (from migrations)
-- Verify these columns exist:
-- state_key TEXT (nullable for regular events, "" for most state events)
```

Run:
```bash
grep -r "state_key" /path/to/migrations/ --include="*.sql"
```

If `state_key` column is missing from the `events` table, a new migration is required. Based on existing code (`create_room` already inserts events with `state_key` fields), the column should already exist.

## Previous Story Context (9-6)

Story 9-6 added:
- `gateway/internal/matrix/state_event_types.go` — `allowedStateEventTypes` map
- Whitelist check in `PutSetRoomState` before any processing
- Tests in `state_event_whitelist_test.go`
- Godog feature `state_event_whitelist.feature`

Story 9-7 builds directly on 9-6 and must NOT modify the whitelist or the whitelist check. The 501 fallback to be replaced is the code AFTER the whitelist check (line ~396 in rooms.go).

## Dev Notes

- The `time` package import may need adding to `rooms.go` if not already imported (for `time.Now().UnixMilli()`)
- The proto field number 7 (`state_key`) must not conflict with existing fields in `SendEventRequest` (fields 1-6 are already in use)
- After `make proto`, verify that the generated Go code has `GetStateKey() string` method on `SendEventRequest` — use this in the handler
- The Elixir side: `request.state_key` will be an empty string `""` for all non-state-event calls (backward compatible since the event_map will include `"state_key" => ""`); this is fine because regular events don't have `state_key` fields and the DB column is nullable
- In the Godog step for `kaiSendsPutRoomState`, the request URL pattern is `/_matrix/client/v3/rooms/{roomId}/state/{eventType}` (without stateKey for most types); for member events the pattern would need `/{stateKey}` — MVP: only test types with empty stateKey

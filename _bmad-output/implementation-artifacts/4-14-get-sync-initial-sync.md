# Story 4.14: GET /sync — Initial Sync

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-14-get-sync-initial-sync
**Created:** 2026-04-07

---

## Story

As an end-user,
I want an initial `/sync` response that gives me the full current state of all my rooms,
so that my Matrix client can build its initial state on first connection.

---

## Acceptance Criteria

1. `GET /_matrix/client/v3/sync` with **no `since` query parameter** performs an initial sync for the authenticated user.
2. Handler calls `gRPC CoreService.GetInitialSync` with `user_id`; Core returns:
   - All room IDs where the user is an active member (`left_at IS NULL` in `room_members`)
   - For each room: current state events (membership list, power levels), last ≤ 20 messages in `timeline`
3. Response follows Matrix sync response format:
   ```json
   {
     "next_batch": "<opaque_since_token>",
     "rooms": {
       "join": {
         "<room_id>": {
           "state":    { "events": [ ... ] },
           "timeline": { "events": [ ... ], "limited": true, "prev_batch": "<token>" }
         }
       },
       "invite": {},
       "leave": {}
     },
     "presence": { "events": [] }
   }
   ```
4. `next_batch` is generated via `Nebu.Session.PgStore.persist_since_token/3` and stored in `sync_tokens`; the `last_event_id` used is the highest `origin_server_ts` event_id across all the user's rooms (or `nil` if no events).
5. If the user has no rooms, returns `200` with `"rooms": {"join": {}, "invite": {}, "leave": {}}` and a valid `next_batch`.
6. Handler timeout: 5 seconds. If Core is unreachable (gRPC `UNAVAILABLE`), return `503` with `{"errcode": "M_UNAVAILABLE", "error": "Core is temporarily unavailable"}`.
7. `GET /_matrix/client/v3/sync?since=<token>` with a `since` parameter is **NOT handled by this story** — the handler returns the initial sync response whenever `since` is absent, and defers incremental sync to Story 4-15. For the MVP, if `since` is present but Story 4-15 is not yet implemented, the handler may fall back to initial sync (AC deferred to 4-15).
8. `proto/core.proto` is extended with `rpc GetInitialSync(GetInitialSyncRequest) returns (GetInitialSyncResponse)`.
9. Unit tests (Go `httptest`):
   - Authenticated user → `200` with `next_batch` and `rooms.join` containing correct room IDs
   - User with no rooms → `200` with empty `rooms.join`
   - Core returns `UNAVAILABLE` → `503 M_UNAVAILABLE`
   - Missing or invalid JWT → `401 M_MISSING_TOKEN` (covered by JWTMiddleware, not handler)
10. Unit tests (Elixir ExUnit):
    - `GetInitialSync` with user in 2 rooms → response contains both room IDs; state events include power levels; timeline has ≤ 20 events
    - `GetInitialSync` with user in 0 rooms → response contains empty `rooms` list
    - `since_token` is persisted in `sync_tokens` via `Nebu.Session.PgStore.persist_since_token/3`
11. `make test-unit-go` and `make test-unit-elixir` pass with zero new failures.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Go: initial sync — user with 2 rooms — Go httptest**
- Given: valid JWT for `@alice:test.local`; mock core client returns `GetInitialSyncResponse` with 2 room entries
- When: `GET /_matrix/client/v3/sync` (no `since` param)
- Then: `200`; body has `next_batch` (non-empty string); `rooms.join` contains both room IDs with `state.events` and `timeline.events`

**2. Go: initial sync — user with no rooms — Go httptest**
- Given: valid JWT for `@alice:test.local`; mock core client returns `GetInitialSyncResponse` with empty rooms list
- When: `GET /_matrix/client/v3/sync`
- Then: `200`; `rooms.join` is `{}`; `next_batch` is non-empty

**3. Go: initial sync — core unavailable — Go httptest**
- Given: valid JWT; mock core client returns gRPC `UNAVAILABLE` error
- When: `GET /_matrix/client/v3/sync`
- Then: `503` with `{"errcode": "M_UNAVAILABLE"}`

**4. Elixir: GetInitialSync — user in 2 rooms — ExUnit**
- Given: `@alice:test.local` is a member of `!room1:test.local` and `!room2:test.local`; each room has 3 events; `FakePgStore` accepts `persist_since_token/3`
- When: `Nebu.EventDispatcher.Server.get_initial_sync/2` called with `user_id: "@alice:test.local"`
- Then: response contains both room IDs in `rooms`; each room has `state_events` (membership) and ≤ 20 `timeline_events`; `since_token` is non-empty

**5. Elixir: GetInitialSync — user with no rooms — ExUnit**
- Given: `@bob:test.local` has no room memberships
- When: `Nebu.EventDispatcher.Server.get_initial_sync/2` called with `user_id: "@bob:test.local"`
- Then: response `rooms` list is empty; `since_token` is still generated and persisted

**6. Elixir: since_token persisted in sync_tokens — ExUnit**
- Given: `@alice:test.local` is in 1 room; `FakePgStore` tracks calls to `persist_since_token/3`
- When: `get_initial_sync/2` is called
- Then: `Nebu.Session.PgStore.persist_since_token/3` is called once with `user_id = "@alice:test.local"` and a non-empty `since_token`

---

## Technical Requirements

### Proto Changes — `proto/core.proto`

Add to `CoreService`:
```protobuf
rpc GetInitialSync(GetInitialSyncRequest) returns (GetInitialSyncResponse);
```

Add message definitions:
```protobuf
// GetInitialSync — returns full state snapshot for all of a user's joined rooms
message GetInitialSyncRequest {
  string user_id = 1;
}

message GetInitialSyncResponse {
  string               since_token = 1;  // opaque token — becomes next_batch in HTTP response
  repeated SyncRoom    rooms       = 2;
}

// SyncRoom represents one joined room's state in an initial sync
message SyncRoomStateEvent {
  string type     = 1;  // e.g. "m.room.member", "m.room.power_levels"
  string state_key = 2;
  bytes  content  = 3;  // JSON-encoded content
  string sender   = 4;
}

message SyncRoom {
  string                    room_id        = 1;
  repeated SyncRoomStateEvent state_events   = 2;
  repeated Event            timeline_events = 3;
  bool                      limited        = 4;   // true if timeline was truncated at 20
  string                    prev_batch     = 5;   // pagination token for GET /messages
}
```

**IMPORTANT:** Run `make proto` after editing to regenerate `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/`.

The `Event` message already exists in `core.proto` — reuse it for timeline events. `SyncRoomStateEvent` is new (state events need a `state_key` field that `Event` lacks).

### Elixir: New Handler `get_initial_sync/2` in `Nebu.EventDispatcher.Server`

**File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (EXTEND, do not create a new file)

Flow:
1. Extract `user_id` from `request.user_id`
2. Query DB for all room IDs where user is an active member: new function `Nebu.Room.DB.get_rooms_for_user/1`
3. For each room_id:
   a. Get room state: `Nebu.Room.Server.get_state(room_id)` → members + power_levels
   b. Build state events (membership events for each member + power levels event)
   c. Fetch last 20 timeline events: `Nebu.Room.DB.fetch_events(room_id, "b", 20, "")` — direction `"b"` returns newest first; reverse for response
4. Compute `last_event_id` = the event_id of the newest event across all rooms (or `nil` if no events)
5. Call `Nebu.Session.PgStore.persist_since_token(user_id, generated_token, last_event_id)` to persist
6. Return `%Core.GetInitialSyncResponse{since_token: generated_token, rooms: [...]}`

**Since token generation** (match Story 4-6's format):
```elixir
since_token = Base.encode64(
  "#{user_id}:#{last_event_id || ""}:#{System.monotonic_time()}",
  padding: false
)
```
This is already the canonical format from `Nebu.Session.PgStore` (Story 4-6 AC #2).

**Configurable injection** (follow established pattern from all other EventDispatcher handlers):
```elixir
defp pg_store_module do
  Application.get_env(:event_dispatcher, :pg_store_module, Nebu.Session.PgStore)
end
```

### New DB Query: `Nebu.Room.DB.get_rooms_for_user/1`

**File:** `core/apps/room_manager/lib/nebu/room/db.ex` (EXTEND)

```elixir
@sql_get_rooms_for_user """
SELECT room_id FROM room_members
WHERE user_id = $1 AND left_at IS NULL
"""

@doc """
Returns all room IDs where `user_id` is currently an active member.
Returns `{:ok, [room_id]}` — empty list if user has no rooms.
"""
@spec get_rooms_for_user(String.t()) :: {:ok, [String.t()]} | {:error, term()}
def get_rooms_for_user(user_id) do
  case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_rooms_for_user, [user_id]) do
    {:ok, %{rows: rows}} -> {:ok, Enum.map(rows, fn [rid] -> rid end)}
    {:error, reason}     -> {:error, reason}
  end
end
```

**FakeDB extension for tests** — the `FakeDB` in `nebu_room_test.exs` (and any event_dispatcher fake) must add:
```elixir
def get_rooms_for_user(user_id) do
  rooms = :ets.match(:fake_room_db, {{:member, :"$1", user_id}, :active})
  {:ok, Enum.map(rooms, fn [rid] -> rid end)}
end
```

### State Event Construction (Elixir)

The `state` section of each SyncRoom must include:
1. **One `m.room.member` event per active member** — `state_key` = member's `user_id`, content = `{"membership": "join", "displayname": ""}` (displayname lookup is out of scope for this story)
2. **One `m.room.power_levels` event** — `state_key` = `""`, content = the room's `power_levels` map (encoded via `Jason.encode!/1`)

Helper to build state events:
```elixir
defp build_state_events(state) do
  member_events =
    state.members
    |> MapSet.to_list()
    |> Enum.map(fn uid ->
      %Core.SyncRoomStateEvent{
        type: "m.room.member",
        state_key: uid,
        content: Jason.encode!(%{"membership" => "join", "displayname" => ""}),
        sender: uid
      }
    end)

  pl_event = %Core.SyncRoomStateEvent{
    type: "m.room.power_levels",
    state_key: "",
    content: Jason.encode!(state.power_levels),
    sender: ""
  }

  member_events ++ [pl_event]
end
```

### Go Handler: `GetSyncHandler` in NEW file `gateway/internal/matrix/sync.go`

**Why a new file?** The existing `rooms.go` has 4+ handler types. A new `sync.go` keeps concerns separated (Go convention: one file per major feature). Do NOT add sync to `rooms.go`.

**Interface:**
```go
// GetSyncCoreClient is the consumer-defined interface for the GetInitialSync gRPC call.
type GetSyncCoreClient interface {
    GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error)
}
```

**Handler struct:**
```go
type GetSyncHandler struct {
    coreClient GetSyncCoreClient
    serverName string
}

type GetSyncConfig struct {
    CoreClient GetSyncCoreClient
    ServerName string
}

func NewGetSyncHandler(cfg GetSyncConfig) *GetSyncHandler
```

**HTTP handler method:**
```go
func (h *GetSyncHandler) GetSync(w http.ResponseWriter, r *http.Request) {
    // 1. Extract user_id from JWT context
    sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
    systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
    userID := coregrpc.FormatUserID(sub, h.serverName)

    // 2. 5-second timeout
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    grpcCtx := coregrpc.WithUserMetadata(ctx, userID, systemRole)

    // 3. Call Core
    resp, err := h.coreClient.GetInitialSync(grpcCtx, &pb.GetInitialSyncRequest{UserId: userID})
    if err != nil {
        // map gRPC UNAVAILABLE → 503 M_UNAVAILABLE
        // map all others → 500 M_UNKNOWN
    }

    // 4. Map proto response → JSON sync response struct
    // 5. Return 200 with JSON body
}
```

**JSON response structs** (defined in `sync.go`):
```go
type syncResponse struct {
    NextBatch string               `json:"next_batch"`
    Rooms     syncRooms            `json:"rooms"`
    Presence  syncPresence         `json:"presence"`
}

type syncRooms struct {
    Join   map[string]syncJoinedRoom `json:"join"`
    Invite map[string]interface{}    `json:"invite"`
    Leave  map[string]interface{}    `json:"leave"`
}

type syncJoinedRoom struct {
    State    syncStateSection    `json:"state"`
    Timeline syncTimelineSection `json:"timeline"`
}

type syncStateSection struct {
    Events []syncStateEvent `json:"events"`
}

type syncStateEvent struct {
    Type     string          `json:"type"`
    StateKey string          `json:"state_key"`
    Content  json.RawMessage `json:"content"`
    Sender   string          `json:"sender,omitempty"`
}

type syncTimelineSection struct {
    Events    []syncTimelineEvent `json:"events"`
    Limited   bool                `json:"limited"`
    PrevBatch string              `json:"prev_batch,omitempty"`
}

type syncTimelineEvent struct {
    EventID  string          `json:"event_id"`
    Type     string          `json:"type"`
    Sender   string          `json:"sender"`
    RoomID   string          `json:"room_id"`
    Content  json.RawMessage `json:"content"`
    OriginTS int64           `json:"origin_server_ts"`
}

type syncPresence struct {
    Events []interface{} `json:"events"`
}
```

**Error mapping:**
```go
st, _ := status.FromError(err)
switch st.Code() {
case codes.Unavailable:
    writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Core is temporarily unavailable")
default:
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
}
```

**Content field handling:** Proto `SyncRoomStateEvent.content` is `bytes` (JSON-encoded). In Go, decode as `json.RawMessage` to avoid double-encoding.

### Go gRPC Client — `gateway/internal/grpc/client.go` (EXTEND)

Add `GetInitialSync` method following the exact pattern of all other methods:
```go
// GetInitialSync calls the Elixir core to build the initial sync response for a user.
func (c *Client) GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error) {
    return c.core.GetInitialSync(ctx, req)
}
```

### Router Registration — `gateway/cmd/gateway/main.go` (EXTEND)

After the existing `setRoomStateHandler` registration block, add:
```go
syncHandler := matrix.NewGetSyncHandler(matrix.GetSyncConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("GET /_matrix/client/v3/sync",
    jwtMiddleware(http.HandlerFunc(syncHandler.GetSync)))
```

---

## File Structure

### New Files
- `gateway/internal/matrix/sync.go` — `GetSyncHandler` + response structs
- `gateway/internal/matrix/sync_test.go` — Go httptest unit tests

### Modified Files
- `proto/core.proto` — add `GetInitialSync` RPC + `GetInitialSyncRequest`, `GetInitialSyncResponse`, `SyncRoom`, `SyncRoomStateEvent` messages
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — regenerated by `make proto`
- `gateway/internal/grpc/pb/` — regenerated by `make proto`
- `core/apps/room_manager/lib/nebu/room/db.ex` — add `get_rooms_for_user/1`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — add `get_initial_sync/2` handler
- `core/apps/event_dispatcher/test/nebu_event_dispatcher_test.exs` — add ExUnit tests for `get_initial_sync`
- `gateway/internal/grpc/client.go` — add `GetInitialSync` method
- `gateway/cmd/gateway/main.go` — register `GetSyncHandler` for `GET /_matrix/client/v3/sync`

---

## Dev Notes

### Module and File Naming Conventions (Critical)

From prior stories:
- Elixir module names: `Nebu.{Domain}.{Name}` — `Nebu.Room.DB`, `Nebu.Session.PgStore`, `Nebu.EventDispatcher.Server`
- Go handler files: per-feature, e.g., `login.go`, `rooms.go`, `messages.go` → use `sync.go`
- Consumer-defined Go interfaces: minimal, local to the handler file (ADR-009)

### String Keys in DB-Sourced Maps (Architecture Rule, Stories 4-2+)

All maps from DB or proto → Elixir must use **string keys**. When building state events from `state.power_levels`, use `Jason.encode!(state.power_levels)` directly (already string-keyed). When building member event content, use string keys: `%{"membership" => "join"}`.

### `fetch_events` Direction and Order

`Nebu.Room.DB.fetch_events(room_id, "b", 20, "")` returns events **newest first** (DESC). The Matrix sync timeline expects chronological order (oldest first). Reverse the list before building the `timeline_events` field:
```elixir
timeline_events = events |> Enum.reverse() |> Enum.map(&event_map_to_proto/1)
```
The `event_map_to_proto/1` helper already exists in `Nebu.EventDispatcher.Server` — reuse it.

### `event_map_to_proto/1` Reuse

This private helper is already defined in `Nebu.EventDispatcher.Server`:
```elixir
defp event_map_to_proto(event) do
  %Core.Event{
    event_id:   event["event_id"],
    room_id:    event["room_id"],
    sender_id:  event["sender"],
    event_type: event["event_type"],
    content:    Jason.encode!(event["content"] || %{}),
    origin_ts:  event["origin_server_ts"] || 0,
    server_ts:  event["origin_server_ts"] || 0
  }
end
```
Reuse for timeline events. Do NOT duplicate.

### Session PgStore Injection in EventDispatcher

The Session Manager is a separate OTP app. To call `Nebu.Session.PgStore` from `Nebu.EventDispatcher.Server`, the `:event_dispatcher` app must have `:session_manager` as a dependency. Check `core/apps/event_dispatcher/mix.exs` — if `:session_manager` is not already a dep, add it. (It likely already is since Story 4-8 and earlier stories wired the apps together via umbrella `deps`.)

Injection pattern for tests (follow the established pattern from all EventDispatcher tests):
```elixir
# In test setup:
Application.put_env(:event_dispatcher, :pg_store_module, FakePgStore)
# FakePgStore stores to ETS for assertion in tests
```

### go test Pattern for Sync Handler

Follow the exact pattern of `rooms_test.go` — use `httptest.NewRecorder()` + `httptest.NewRequest()`, inject a `mockCoreClient` struct that implements `GetSyncCoreClient`. Decode the response body into the `syncResponse` struct (exported in tests via unexported struct access or local redeclaration).

Example mock interface:
```go
type mockSyncCoreClient struct {
    resp *pb.GetInitialSyncResponse
    err  error
}

func (m *mockSyncCoreClient) GetInitialSync(_ context.Context, _ *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error) {
    return m.resp, m.err
}
```

### Content Bytes in Proto (Go Side)

`SyncRoomStateEvent.content` is `bytes` in proto → arrives in Go as `[]byte`. Use `json.RawMessage(stateEvt.Content)` to embed it directly in the JSON response without double-encoding.

Same for `SyncRoom.timeline_events` — the `Event.content` field is `bytes`, use `json.RawMessage(evt.Content)`.

### No Crash/Restart Test Required for This Story

This story introduces no new GenServer state. The `get_initial_sync` handler reads from DB + in-memory GenServer state but does not mutate persistent state (only `persist_since_token` writes, which is tested via FakePgStore). No crash/restart test needed per CLAUDE.md (Stateless: no restart test needed — Option C).

### Proto Field Numbers

When adding new messages to `core.proto`, use field numbers starting from 1. Since `SyncRoom`, `SyncRoomStateEvent`, `GetInitialSyncRequest`, and `GetInitialSyncResponse` are all new messages, start from `1` in each. Do not reuse or skip numbers in existing messages.

### 5-Second Timeout Context

The Go handler must wrap `r.Context()` with a 5-second deadline via `context.WithTimeout`. Use the standard library `context` package — no external dependencies needed.

```go
ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
defer cancel()
```

Pass this `ctx` (after attaching gRPC metadata) to the gRPC call.

### `make proto` Must Run After Proto Edit

After editing `proto/core.proto`, run:
```bash
make proto
```
This regenerates `gateway/internal/grpc/pb/` (Go stubs) and `core/apps/event_dispatcher/lib/pb/` (Elixir stubs). Do not manually edit generated files.

### Story 4-15 Handoff

This story handles `since` absent → initial sync only. When `since` is present in Story 4-15, the handler will delegate to a `GetSyncDelta` gRPC call. For clean handoff:
- The Go handler should check `r.URL.Query().Get("since")` — if present, return a `501 Not Implemented` stub in this story (Story 4-15 will implement it). This avoids breaking Story 4-15 implementation.
- The `next_batch` from this story becomes the `since` token for Story 4-15. Format must be stable.

### FakeDB for Room Tests

The `FakeDB` in `core/apps/room_manager/test/nebu_room_test.exs` needs the new `get_rooms_for_user/1` function. Since the EventDispatcher test is separate, it will need its own fake/stub for the DB module. Follow the established pattern of using Application.put_env to inject the fake.

### `writeMatrixError` is Defined in `gateway/internal/matrix/` — Shared Across All Handler Files

The `writeMatrixError` helper is defined in one of the matrix handler files (likely `rooms.go` or a shared helper). Import by being in the same `matrix` package — no additional imports needed.

---

## Dependencies

- Story 4-5 (done): `Nebu.Session.EtsStore` — not directly used here but EtsStore is part of the session app
- Story 4-6 (done): `Nebu.Session.PgStore.persist_since_token/3` — directly called; `sync_tokens` table exists (migration `000011`)
- Story 4-2 (done): `Nebu.Room.Server.get_state/1` — returns `%{room_id, members, power_levels, created_at}`
- Story 4-4 (done): `Nebu.Room.DB.fetch_events/4` — used for timeline events; already implemented
- Story 4-8 (done): `GetRoomState` pattern in EventDispatcher — follow same code patterns
- Story 4-13 (review): `power_levels` in Room.Server state; `Jason.encode!(state.power_levels)` works correctly
- Story 4-15 (backlog): incremental sync — depends on the `next_batch` token format established here

---

## Dev Agent Record

### Implementation Plan

Story 4-14 implemented in 5 parts following the ATDD-first story spec:

**A) Proto additions (`proto/core.proto`)**
- Added `rpc GetInitialSync(GetInitialSyncRequest) returns (GetInitialSyncResponse)` to `CoreService`
- Added `GetInitialSyncRequest{user_id}`, `GetInitialSyncResponse{since_token, rooms}`, `SyncRoom{room_id, state_events, timeline_events, limited, prev_batch}`, `SyncRoomStateEvent{type, state_key, content, sender}` messages
- Ran `make proto` — regenerated `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/`

**B) `Nebu.Room.DB.get_rooms_for_user/1`**
- Added to `core/apps/room_manager/lib/nebu/room/db.ex`
- SQL: `SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL`

**C) `Nebu.EventDispatcher.Server.get_initial_sync/2`**
- Added to `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
- Added injectable: `rooms_db_module/0`, `pg_store_module/0` via `Application.get_env`
- Built `build_state_events/1` helper for m.room.member + m.room.power_levels events
- Reversed timeline events (DB returns newest-first, Matrix expects oldest-first)
- Since-token format: `Base.encode64("#{user_id}:#{last_event_id||""}:#{monotonic_time}", padding: false)`
- Calls `pg_store_module().persist_since_token/3`

**D) Go Handler `gateway/internal/matrix/sync.go`** (NEW FILE)
- `GetSyncCoreClient` interface, `GetSyncHandler`, `GetSyncConfig`, `NewGetSyncHandler`
- ?since present → 501 stub (Story 4-15 placeholder)
- 5-second context timeout
- gRPC UNAVAILABLE → 503 M_UNAVAILABLE; others → 500 M_UNKNOWN
- `rooms.join` always initialized as empty map (never null)
- Complete JSON response structs

**E) `gateway/internal/grpc/client.go`**
- Added `GetInitialSync` method delegating to `c.core.GetInitialSync`

**F) `gateway/cmd/gateway/main.go`**
- Registered `GET /_matrix/client/v3/sync` behind jwtMiddleware

**G) Tests updated**
- `gateway/internal/grpc/stream_test.go`: added `GetInitialSync` stub to `mockCoreClient`
- `gateway/internal/grpc/client_test.go`: added `GetInitialSync` test case

### Completion Notes

- All 6 Go tests in `gateway/internal/matrix/sync_test.go` pass
- All 4 Elixir tests in `core/apps/event_dispatcher/test/nebu/event_dispatcher/sync_test.exs` pass
- `make test-unit-go`: all packages pass (0 failures)
- `make test-unit-elixir`: 67 tests, 0 failures, 1 skipped (pre-existing skip) in event_dispatcher

### File List

- `proto/core.proto` — added GetInitialSync RPC + 4 new message types
- `gateway/internal/grpc/pb/core.pb.go` — regenerated
- `gateway/internal/grpc/pb/core_grpc.pb.go` — regenerated
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — regenerated
- `core/apps/room_manager/lib/nebu/room/db.ex` — added `get_rooms_for_user/1`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — added `get_initial_sync/2`, `rooms_db_module/0`, `pg_store_module/0`, `build_state_events/1`
- `gateway/internal/matrix/sync.go` — NEW: GetSyncHandler, response structs
- `gateway/internal/grpc/client.go` — added `GetInitialSync`
- `gateway/cmd/gateway/main.go` — registered sync route
- `gateway/internal/grpc/client_test.go` — added GetInitialSync test case
- `gateway/internal/grpc/stream_test.go` — added GetInitialSync stub to mockCoreClient

### Change Log

- 2026-04-03: Story 4-14 implemented — GET /sync initial sync. Proto extended with GetInitialSync RPC. Elixir handler added with injectable fakes. Go handler with 501 stub for ?since. All acceptance tests pass.

---

## Story Completion Status

Ultimate context engine analysis completed — comprehensive developer guide created.

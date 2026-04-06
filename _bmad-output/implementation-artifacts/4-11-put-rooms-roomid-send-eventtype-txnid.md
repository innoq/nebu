# Story 4.11: PUT /rooms/{roomId}/send/{eventType}/{txnId}

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-11-put-rooms-roomid-send-eventtype-txnid
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to send messages (and other events) to a room,
so that other members can read my messages in real time.

---

## Acceptance Criteria

1. `PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` is protected by `JWTMiddleware` — unauthenticated requests return `401 M_MISSING_TOKEN`.
2. Handler extracts `roomId`, `eventType`, `txnId` from the URL path using Go 1.22+ `r.PathValue(...)` and `user_id` from the JWT context (`middleware.ContextKeySub`).
3. Handler decodes the JSON request body as event content (`map[string]any`); returns `400 M_BAD_JSON` on invalid JSON.
4. Handler calls `gRPC CoreService.SendEvent` with `room_id`, `sender_id` (formatted user ID), `event_type`, `txn_id`, `content` (JSON-encoded), and `origin_ts` (current Unix milliseconds).
5. Returns `200 {"event_id": "$<hash>"}` on success — `event_id` comes from `SendEventResponse.EventId`.
6. Returns `403 M_FORBIDDEN` if the user is not a room member (gRPC `PERMISSION_DENIED` status).
7. Returns `404 M_NOT_FOUND` if the room does not exist (gRPC `NOT_FOUND` status).
8. Returns `429 M_LIMIT_EXCEEDED` if the rate limit is exceeded (gRPC `RESOURCE_EXHAUSTED` status).
9. **Idempotency**: duplicate `txn_id` (same `user_id` + `room_id`) returns `200` with the same `event_id` — no second event persisted (handled entirely by Elixir `Room.Server.send_event/5` + ETS `NebuTxnDedup`, already implemented in Story 4-4).
10. The `gRPC CoreService.SendEvent` stub in `gateway/internal/grpc/client.go` is wired to call `c.core.SendEvent(ctx, req)` (currently returns `nil, nil`).
11. The Elixir `send_event/2` handler in `Nebu.EventDispatcher.Server` replaces the current stub: calls `Nebu.Room.Server.send_event/5` and returns `%Core.SendEventResponse{event_id: event_id}`.
12. The Elixir handler returns gRPC `NOT_FOUND` if the Room GenServer is not running, and `PERMISSION_DENIED` if the user is not a room member.
13. Unit tests (Go `httptest`): happy path → 200 with `event_id`; unauthenticated → 401; bad JSON → 400; room not found → 404; not a member → 403; duplicate `txn_id` → 200 same `event_id`.
14. Unit tests (Elixir ExUnit): `send_event/2` succeeds → event_id returned; room not found → `GRPC.RPCError NOT_FOUND`; not a member → `GRPC.RPCError PERMISSION_DENIED`; idempotent txn_id → same event_id.
15. `make test-unit-go` and `make test-unit-elixir` pass with zero new test failures.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Happy path: send a message — Go unit test (httptest)**
- Given: valid JWT + mock core client returns `SendEventResponse{event_id: "$abc123"}`
- When: `PUT /_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1` with body `{"msgtype":"m.text","body":"hello"}`
- Then: `200` with body `{"event_id": "$abc123"}`

**2. Unauthenticated request — Go unit test (httptest)**
- Given: no `Authorization` header
- When: `PUT /_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1`
- Then: `401` with `{"errcode": "M_MISSING_TOKEN"}`

**3. Invalid JSON body — Go unit test (httptest)**
- Given: valid JWT
- When: `PUT /_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1` with body `{not json`
- Then: `400` with `{"errcode": "M_BAD_JSON"}`

**4. Room not found — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `NOT_FOUND` error
- When: `PUT /_matrix/client/v3/rooms/!nonexistent:test.local/send/m.room.message/txn1`
- Then: `404` with `{"errcode": "M_NOT_FOUND"}`

**5. Not a member (forbidden) — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `PERMISSION_DENIED` error
- When: `PUT /_matrix/client/v3/rooms/!private:test.local/send/m.room.message/txn1`
- Then: `403` with `{"errcode": "M_FORBIDDEN"}`

**6. Idempotent txn_id — Go unit test (httptest)**
- Given: valid JWT + mock core client always returns `SendEventResponse{event_id: "$abc123"}`
- When: `PUT ... /send/m.room.message/txn1` is called twice
- Then: both responses are `200` with `{"event_id": "$abc123"}` (no error on second call)

**7. Elixir: send_event succeeds — ExUnit**
- Given: Room GenServer running for `"!room1:test.local"` with FakeDB; `@alice:test.local` is a member
- When: `Nebu.EventDispatcher.Server.send_event(%Core.SendEventRequest{room_id: "!room1:test.local", sender_id: "@alice:test.local", event_type: "m.room.message", txn_id: "txn1", content: ~s({"body":"hi"}), origin_ts: 0}, stream)` is called
- Then: returns `%Core.SendEventResponse{event_id: event_id}` where `event_id` starts with `"$"`

**8. Elixir: room not found → GRPC.RPCError NOT_FOUND — ExUnit**
- Given: no Room GenServer running for `"!ghost:test.local"`
- When: `send_event/2` is called for that room_id
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.not_found()`

**9. Elixir: not a member → GRPC.RPCError PERMISSION_DENIED — ExUnit**
- Given: Room GenServer running for `"!room2:test.local"` with FakeDB; `@bob:test.local` is NOT a member
- When: `send_event/2` is called with `sender_id: "@bob:test.local"`
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()`

**10. Elixir: idempotent txn_id returns same event_id — ExUnit**
- Given: Room GenServer running, `@alice:test.local` is a member; first `send_event/2` call succeeded and returned `event_id_1`
- When: `send_event/2` is called again with the same `{room_id, sender_id, txn_id}`
- Then: returns `%Core.SendEventResponse{event_id: event_id_1}` (same value, no new event)

---

## Technical Requirements

### Go Handler — `gateway/internal/matrix/rooms.go` (EXTEND EXISTING)

**CRITICAL:** `rooms.go` already exists with `CreateRoomHandler`, `JoinRoomHandler`, and `InviteUserHandler` from Stories 4-9 and 4-10. Do NOT create a new file. Add the `SendEventHandler` to the **same file**.

Architecture rule: `rooms.go` is the file for all room-related Matrix API handlers.

#### Handler struct and types

```go
// SendEventCoreClient is a consumer-defined interface for the SendEvent gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type SendEventCoreClient interface {
    SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error)
}

// sendEventResponse is the JSON response for a successful event send.
type sendEventResponse struct {
    EventID string `json:"event_id"`
}

// SendEventHandler handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
type SendEventHandler struct {
    coreClient SendEventCoreClient
    serverName string
}

// SendEventConfig holds dependencies for NewSendEventHandler.
type SendEventConfig struct {
    CoreClient SendEventCoreClient
    ServerName string
}

// NewSendEventHandler constructs a SendEventHandler from the provided config.
func NewSendEventHandler(cfg SendEventConfig) *SendEventHandler {
    return &SendEventHandler{
        coreClient: cfg.CoreClient,
        serverName: cfg.ServerName,
    }
}
```

#### Handler logic (PutSendEvent)

```go
// PutSendEvent handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
//
// Flow:
//  1. Extract roomId, eventType, txnId from URL path via Go 1.22+ r.PathValue.
//  2. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  3. Decode JSON body → content map; 400 M_BAD_JSON on failure.
//  4. Build gRPC request: JSON-encode content bytes, use time.Now().UnixMilli() as origin_ts.
//  5. Call Core.SendEvent — map gRPC errors to Matrix error codes.
//  6. Return 200 {"event_id": resp.EventId} on success.
func (h *SendEventHandler) PutSendEvent(w http.ResponseWriter, r *http.Request) {
    roomID    := r.PathValue("roomId")
    eventType := r.PathValue("eventType")
    txnID     := r.PathValue("txnId")

    sub, _        := r.Context().Value(middleware.ContextKeySub).(string)
    systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
    userID        := coregrpc.FormatUserID(sub, h.serverName)
    grpcCtx       := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

    var content map[string]any
    if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
        writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
        return
    }

    contentBytes, err := json.Marshal(content)
    if err != nil {
        writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Cannot encode event content")
        return
    }

    resp, err := h.coreClient.SendEvent(grpcCtx, &pb.SendEventRequest{
        RoomId:    roomID,
        SenderId:  userID,
        EventType: eventType,
        TxnId:     txnID,
        Content:   contentBytes,
        OriginTs:  time.Now().UnixMilli(),
    })
    if err != nil {
        st, _ := status.FromError(err)
        switch st.Code() {
        case codes.NotFound:
            writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
        case codes.PermissionDenied:
            writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not allowed to send events to this room")
        case codes.ResourceExhausted:
            writeMatrixError(w, http.StatusTooManyRequests, "M_LIMIT_EXCEEDED", "Rate limit exceeded")
        default:
            writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
        }
        return
    }
    if resp == nil {
        writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
        return
    }

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(sendEventResponse{EventID: resp.EventId})
}
```

**Required imports (add to existing import block if missing):**
- `"time"` — for `time.Now().UnixMilli()`
- All other imports (`context`, `encoding/json`, `net/http`, `coregrpc`, `pb`, `middleware`, `codes`, `status`) are already present from the existing handlers in `rooms.go`.

**Do NOT re-import** packages already in the file's import block. Go will fail to compile on duplicate imports.

### Go gRPC Client — `gateway/internal/grpc/client.go` (MODIFY)

The `SendEvent` stub currently returns `nil, nil`. Wire it to the real gRPC call:

```go
// SendEvent calls the Elixir core to process and persist a room event.
func (c *Client) SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
    return c.core.SendEvent(ctx, req)
}
```

**Also update `client_test.go`:** The `TestStubsReturnNil` test currently expects `nil, nil` for `SendEvent`. After wiring, update this entry to expect a connection error (same pattern as `CreateRoom`, `JoinRoom`, `InviteUser`):

```go
{
    name: "SendEvent",
    call: func() error {
        // SendEvent is wired to the real gRPC client (Story 4-11),
        // so it returns a connection error when no server is running.
        _, err := c.SendEvent(ctx, &pb.SendEventRequest{})
        if err == nil {
            return fmt.Errorf("want connection error; got nil")
        }
        return nil
    },
},
```

### Go Router — `gateway/cmd/gateway/main.go` (MODIFY)

Register the new handler behind `jwtMiddleware`. Add after the existing invite handler block:

```go
sendEventHandler := matrix.NewSendEventHandler(matrix.SendEventConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}",
    jwtMiddleware(http.HandlerFunc(sendEventHandler.PutSendEvent)))
```

**IMPORTANT:** `coreClient`, `serverName`, and `jwtMiddleware` are already constructed — reuse, do NOT redeclare. `*coregrpc.Client` satisfies `SendEventCoreClient` once `SendEvent` is wired.

**Route pattern notes:**
- Go 1.22+ `net/http` mux supports multi-segment wildcards via `{param}` syntax.
- `{roomId}` captures room IDs like `!abc123:server.name` (includes `!` and `:`).
- `{eventType}` captures event types like `m.room.message`.
- `{txnId}` captures the client-generated transaction ID.
- All three are extracted in the handler via `r.PathValue(...)`.

### Proto — `proto/core.proto`

`SendEvent` RPC and its messages (`SendEventRequest`, `SendEventResponse`) are **ALREADY FULLY DEFINED** in `proto/core.proto`. Do NOT add duplicates. The existing definition:

```protobuf
rpc SendEvent(SendEventRequest) returns (SendEventResponse);

message SendEventRequest {
  string room_id    = 1;
  string sender_id  = 2;
  string event_type = 3;
  string txn_id     = 4;  // idempotency key
  bytes  content    = 5;
  int64  origin_ts  = 6;  // Unix milliseconds
}
message SendEventResponse {
  string event_id = 1;
}
```

**No proto changes needed. Do NOT run `make proto`.** The generated stubs in `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/` already include `SendEvent`.

### Elixir gRPC Handler — `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (MODIFY)

Replace the current stub `send_event/2`:

```elixir
def send_event(request, _stream) do
  room_id   = request.room_id
  sender_id = request.sender_id
  event_type = request.event_type
  txn_id    = request.txn_id

  # Decode content bytes from protobuf (bytes field → binary → decode to map).
  content =
    case Jason.decode(request.content) do
      {:ok, map} -> map
      {:error, _} -> %{}
    end

  # Verify the room exists (Room GenServer must be running).
  case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
    {:error, :not_found} ->
      raise GRPC.RPCError,
        status: GRPC.Status.not_found(),
        message: "room not found: #{room_id}"

    {:ok, _pid} ->
      # Membership check: sender must be a room member before sending.
      state = Nebu.Room.Server.get_state(room_id)

      unless MapSet.member?(state.members, sender_id) do
        raise GRPC.RPCError,
          status: GRPC.Status.permission_denied(),
          message: "#{sender_id} is not a member of #{room_id}"
      end

      # Delegate to Room.Server — handles idempotency, signing, persistence, broadcast.
      case Nebu.Room.Server.send_event(room_id, sender_id, event_type, content, txn_id) do
        {:ok, event_id} ->
          %Core.SendEventResponse{event_id: event_id}

        {:error, reason} ->
          raise GRPC.RPCError,
            status: GRPC.Status.internal(),
            message: "send_event failed: #{inspect(reason)}"
      end
  end
end
```

**Configurable room_registry_module pattern:** The `send_event/2` handler accesses `Nebu.Room.RoomSupervisor` and `Nebu.Room.Server` directly (same pattern as `invite_user/2`). For unit tests, the `room_registry_module/0` function already exists in the server — use the same `room_registry_module().get_state(room_id)` approach for the membership check in tests:

```elixir
# Use room_registry_module() for get_state so tests can inject a fake:
state = room_registry_module().get_state(room_id)
```

This allows test overrides via `Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)`.

**Content decoding:** The `content` field in `SendEventRequest` is `bytes` (protobuf). In Elixir, this arrives as a binary. Use `Jason.decode/1` to convert JSON bytes to a map before passing to `Nebu.Room.Server.send_event/5`, which expects a map.

### Elixir Unit Tests — `core/apps/event_dispatcher/test/nebu/nebu_event_test.exs` (MODIFY)

Add tests for `send_event/2`. Follow the pattern of the existing `join_room` / `invite_user` tests in the same file.

**Test setup requirements:**
- Start a Room GenServer via `Nebu.Room.RoomSupervisor.start_room/1` in the test `setup`.
- Use the `FakeRoomRegistry` pattern (already in the file) for isolation.
- Set `Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)` if needed.
- `async: false` is required — the event_dispatcher test file already uses `async: false` due to shared Horde state.

**Content encoding for gRPC request in tests:**
```elixir
content_bytes = Jason.encode!(%{"msgtype" => "m.text", "body" => "hello"})
request = %Core.SendEventRequest{
  room_id:    "!room1:test.local",
  sender_id:  "@alice:test.local",
  event_type: "m.room.message",
  txn_id:     "txn-test-1",
  content:    content_bytes,
  origin_ts:  System.system_time(:millisecond)
}
```

---

## File Locations

### Modify (existing files):

```
gateway/
  cmd/gateway/main.go                               ← Register PUT /rooms/{roomId}/send/{eventType}/{txnId}
  internal/
    grpc/
      client.go                                     ← Wire SendEvent stub (remove nil,nil)
      client_test.go                                ← Update SendEvent test case (nil,nil → connection error)
    matrix/
      rooms.go                                      ← Add SendEventHandler, SendEventCoreClient, PutSendEvent method
      rooms_test.go                                 ← Add SendEvent test cases

core/apps/event_dispatcher/
  lib/nebu/event_dispatcher/server.ex               ← Replace send_event/2 stub with full implementation
  test/nebu/nebu_event_test.exs                     ← Add send_event Elixir unit tests
```

### Do NOT create new files:
- No new proto files — `SendEvent` is already defined.
- No new migrations — `events` table exists (`000010_events.up.sql` from Story 4-4).
- No new Elixir modules — `Nebu.Room.Server.send_event/5` is fully implemented (Story 4-4).
- Do NOT modify any files in `core/apps/room_manager/` — `send_event/5` is complete and tested.
- Do NOT modify any files in `core/apps/signature/` — `Nebu.EventId` is complete.

---

## Architecture Compliance

| Rule | Requirement |
|---|---|
| Rule #1 | Timestamps as BIGINT — already enforced in `events` table schema |
| Rule #7 | Event IDs always via `Nebu.EventId.generate/1` — already enforced in `Room.Server.send_event/5` |
| Rule #6 | `{:ok, result}` / `{:error, reason}` — no raise/throw in `Room.Server` |
| ADR-002 | No Redis — ETS `NebuTxnDedup` handles idempotency in-memory |
| ADR-005 | `:pg` OTP module for room broadcast (already in `Room.Server`) |
| ADR-009 | Consumer-defined interface `SendEventCoreClient` — minimal, defined in handler file |
| Go conventions | Errors explicit, context threaded, interfaces minimal |
| Elixir conventions | Let-it-crash + Supervisor, Changesets where applicable, `via` tuple registration |
| No panic in library code | `writeMatrixError` + return pattern, no `panic()` anywhere |
| Proto field names | `snake_case` fields: `room_id`, `sender_id`, `event_type`, `txn_id`, `origin_ts`, `event_id` |
| Go field names | `RoomId`, `SenderId`, `EventType`, `TxnId`, `OriginTs`, `EventId` (protobuf generated PascalCase) |

---

## Previous Story Intelligence

### From Story 4-4 (Room.Server.send_event — DONE, do not re-implement):

The Elixir `send_event/5` pipeline is **complete and tested** in `core/apps/room_manager/`:

1. ETS `NebuTxnDedup` idempotency check on `{room_id, user_id, txn_id}`
2. Event map construction (string keys)
3. `Nebu.EventId.generate/1` for content-hash event_id
4. Ed25519 signing via `:crypto.sign(:eddsa, :none, ...)`
5. `Nebu.Room.DB.insert_event/1` for PostgreSQL append
6. ETS insert + `:pg` broadcast (only on DB success)

**Do not touch `core/apps/room_manager/`** — any changes there are out of scope and risk breaking 21 passing tests.

### From Story 4-10 (JoinRoom/InviteUser — done, shows exact patterns for this story):

- Consumer-defined interfaces live in the same `rooms.go` file alongside the handler struct.
- gRPC error mapping pattern: `status.FromError(err)` → `switch st.Code()` — same approach for `SendEvent`.
- `r.PathValue("roomId")` — Go 1.22+ standard; room IDs containing `!` and `:` work correctly.
- `coregrpc.FormatUserID(sub, serverName)` + `coregrpc.WithUserMetadata(...)` — call both before every gRPC call.
- `writeMatrixError` is in `login.go` in the same `matrix` package — already accessible without import.
- The Elixir handler uses `room_registry_module().get_state(room_id)` for membership checks — injectable in tests.

### From Story 4-9 (CreateRoom — shows handler skeleton):

- Handler struct + config pattern: `CreateRoomCoreClient` interface → `CreateRoomHandler` struct → `CreateRoomConfig` → `NewCreateRoomHandler` constructor — follow exact same pattern for `SendEventHandler`.
- The `rooms.go` import block already includes all needed packages. Add `"time"` if missing.

### From Story 4-4 code review findings (prevent repeating):

- **Sign `event_map` (without event_id), not `event_with_id`** — already fixed in `Room.Server`. No action needed here; just know the signing is correct.
- **ETS guard against table-already-exists** — already fixed in `Room.Application`. No action needed here.
- **`Jason.encode!` in DB layer** can raise on non-serializable content — handle in the Elixir EventDispatcher: use `Jason.decode/1` with `{:ok, map}` pattern to safely decode the `content` bytes from the gRPC request before passing to `send_event/5`.

### Module naming (critical — epics.md uses wrong names):

| Epics.md Name | Correct Codebase Name |
|---|---|
| `RoomManager.RoomServer` | `Nebu.Room.Server` |
| `RoomManager.Application` | `Nebu.Room.Application` |
| `CoreService.SendEvent` (Elixir) | `Nebu.EventDispatcher.Server.send_event/2` |

---

## Key Cross-Story Context

| Story | Relationship to 4-11 |
|---|---|
| Story 4-4 (done) | `Nebu.Room.Server.send_event/5` is the backend — complete, tested, do not modify |
| Story 4-4 (done) | `events` table (`000010_events.up.sql`) exists — no new migration needed |
| Story 4-4 (done) | `NebuTxnDedup` ETS table handles idempotency — transparent to Go layer |
| Story 4-9 (done) | `rooms.go` established the handler file — extend, not replace |
| Story 4-10 (review) | `JoinRoomHandler`, `InviteUserHandler` in same `rooms.go` — exact patterns to follow |
| Story 4-13 (backlog) | Power level enforcement will add `PERMISSION_DENIED` from `Room.Server` — `send_event/2` handler already maps `PERMISSION_DENIED` → 403 correctly |
| Story 4-21 (backlog) | End-to-end Gherkin test will exercise this endpoint — handler must match Matrix spec exactly |

---

## Go Test Pattern (rooms_test.go extension)

```go
// ─── Story 4-11: PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId} ──

type mockSendEventCoreClient struct {
    resp        *pb.SendEventResponse
    err         error
    capturedReq *pb.SendEventRequest
}

func (m *mockSendEventCoreClient) SendEvent(_ context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
    m.capturedReq = req
    return m.resp, m.err
}

func buildSendEventHandler(mock *mockSendEventCoreClient) *SendEventHandler {
    return NewSendEventHandler(SendEventConfig{
        CoreClient: mock,
        ServerName: "test.local",
    })
}

func buildAuthedSendEventHandler(t *testing.T, mock *mockSendEventCoreClient) (http.Handler, func() string) {
    t.Helper()
    oidcSrv, privateKey := setupOIDCServer(t)
    t.Cleanup(oidcSrv.Close)
    provider := auth.NewProvider(context.Background(), oidcSrv.URL)
    handler := buildSendEventHandler(mock)
    authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)(
        http.HandlerFunc(handler.PutSendEvent),
    )
    makeToken := func() string {
        return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
    }
    return authed, makeToken
}
```

**URL construction in tests:** Go 1.22+ `httptest.NewRequest` does NOT process `PathValue` automatically — use `mux.ServeHTTP` or set path values manually. Pattern from rooms_test.go (Story 4-9/4-10):

```go
// Option A: Use a real mux (preferred — exercises the full routing):
mux := http.NewServeMux()
mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}",
    authed)
req := httptest.NewRequest(http.MethodPut,
    "/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
    strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer "+makeToken())
rr := httptest.NewRecorder()
mux.ServeHTTP(rr, req)
// assert rr.Code == 200
```

Check `rooms_test.go` (Stories 4-9, 4-10) for the exact helper pattern — `buildAuthedHandler`, `setupOIDCServer`, `signJWT` are all in the `matrix` package and reusable.

---

## Build & Test Commands

```bash
# Run Go tests (must pass before marking story done):
make test-unit-go

# Run Elixir tests (must pass before marking story done):
make test-unit-elixir
```

All builds and tests run inside Docker containers — no local Go or Elixir installation needed.

**Expected results after implementation:**
- All existing Story 4-9 and 4-10 Go tests continue to pass (no regression in `rooms_test.go`).
- All existing Story 4-4 Elixir tests continue to pass (no changes to `room_manager`).
- 6+ new Go `httptest` tests pass (AC #1, #2, #3, #4, #5, #6, idempotency).
- 4+ new Elixir ExUnit tests pass (AC #7–#10).
- `TestStubsReturnNil` for `SendEvent` updated to expect connection error (not `nil,nil`).

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Completion Notes List

- Implemented `SendEventCoreClient` interface, `SendEventHandler` struct, `SendEventConfig`, `NewSendEventHandler`, and `PutSendEvent` method in `gateway/internal/matrix/rooms.go`. Added `"time"` import for `time.Now().UnixMilli()`.
- Wired `SendEvent` stub in `gateway/internal/grpc/client.go` from `return nil, nil` to `return c.core.SendEvent(ctx, req)`.
- Updated `TestStubsReturnNil` in `gateway/internal/grpc/client_test.go` to expect connection error (not `nil, nil`) for `SendEvent`.
- Registered `PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` in `gateway/cmd/gateway/main.go` behind `jwtMiddleware`.
- Replaced stub `send_event/2` in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` with full implementation: room existence check via `RoomSupervisor.lookup_room`, membership check via `room_registry_module().get_state`, JSON content decode via `Jason.decode/1`, delegate to `Nebu.Room.Server.send_event/5`.
- `make test-unit-go`: all 6 new `TestPutSendEvent_*` tests pass (happy path, unauthenticated, bad JSON, not found, not member, idempotency). No regressions.
- `make test-unit-elixir`: all 4 new `send_event` ExUnit tests pass (happy path, not found, not member, idempotent txn_id). 52 tests total in event_dispatcher, 0 failures. No regressions across all Elixir apps.

### File List

- `gateway/internal/matrix/rooms.go` — added `SendEventCoreClient`, `sendEventResponse`, `SendEventHandler`, `SendEventConfig`, `NewSendEventHandler`, `PutSendEvent`; added `"time"` import
- `gateway/internal/grpc/client.go` — wired `SendEvent` stub to `c.core.SendEvent(ctx, req)`
- `gateway/internal/grpc/client_test.go` — updated `SendEvent` test case to expect connection error
- `gateway/cmd/gateway/main.go` — registered `PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` route
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — replaced stub `send_event/2` with full implementation
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — updated `4-11` status to `review`
- `_bmad-output/implementation-artifacts/4-11-put-rooms-roomid-send-eventtype-txnid.md` — status updated to `review`, Dev Agent Record filled

### Change Log

- 2026-04-03: Story 4-11 implemented. Added Go `SendEventHandler` + route registration + gRPC client wiring. Implemented Elixir `send_event/2` gRPC handler with room existence check, membership check, JSON content decoding, and delegation to `Nebu.Room.Server.send_event/5`. All acceptance tests pass (`make test-unit-go` + `make test-unit-elixir`).

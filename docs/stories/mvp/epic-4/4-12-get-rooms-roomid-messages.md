# Story 4.12: GET /rooms/{roomId}/messages

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-12-get-rooms-roomid-messages
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to fetch the paginated message history of a room,
so that I can read previous messages when opening a conversation.

---

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/messages` is protected by `JWTMiddleware` — unauthenticated requests return `401 M_MISSING_TOKEN`.
2. Handler extracts `roomId` from the URL path using Go 1.22+ `r.PathValue("roomId")` and `user_id` from the JWT context (`middleware.ContextKeySub`).
3. Query parameters:
   - `from` (pagination token, optional — empty string if absent)
   - `dir` (`"b"` backward or `"f"` forward; default `"b"` if absent or empty)
   - `limit` (integer 1–100; default `10` if absent or unparseable; clamp to range 1–100)
   - `to` (optional end token — pass through to Core as `to_token`)
4. Handler calls `gRPC CoreService.GetMessages` with `room_id`, `from_token`, `direction`, `limit`, `to_token`.
5. Returns `200` with Matrix-standard body:
   ```json
   {
     "start": "<token>",
     "end": "<token>",
     "chunk": [<event objects>],
     "state": []
   }
   ```
   - `start` = `GetMessagesResponse.prev_batch`
   - `end` = `GetMessagesResponse.next_batch`
   - `chunk` = array of Matrix-format event objects (see Event Mapping below)
   - `state` = always `[]` (room state events out of scope for this story)
6. Each event in `chunk` is a Matrix room event object:
   ```json
   {
     "event_id": "$...",
     "room_id": "!...",
     "sender": "@...",
     "type": "m.room.message",
     "content": { ... },
     "origin_server_ts": 1234567890000
   }
   ```
   - `content` is decoded from the `Event.content` bytes (JSON-encoded) → `map[string]any`
   - `origin_server_ts` comes from `Event.origin_ts`
7. Returns `403 M_FORBIDDEN` if the user is not a room member (gRPC `PERMISSION_DENIED`).
8. Returns `404 M_NOT_FOUND` if the room does not exist (gRPC `NOT_FOUND`).
9. Returns `400 M_INVALID_PARAM` if `limit` is provided but not parseable as an integer (e.g., `limit=abc`).
10. The `gRPC CoreService.GetMessages` stub in `gateway/internal/grpc/client.go` is wired to call `c.core.GetMessages(ctx, req)` (currently returns `nil, nil`).
11. The Elixir `get_messages/2` handler in `Nebu.EventDispatcher.Server` replaces the current stub: queries the `events` table via a new `Nebu.Room.DB.fetch_events/4` function using keyset pagination on `(origin_server_ts, event_id)`.
12. Membership check in the Elixir handler: user must be a member of the room; if not, return gRPC `PERMISSION_DENIED`.
13. Room existence check in the Elixir handler: if the Room GenServer is not running, return gRPC `NOT_FOUND`.
14. Keyset pagination token format: `v1_<base64url(origin_server_ts:event_id)>` — opaque to the client. When `from_token` is empty, fetch the most recent `limit` events (direction `"b"`) or the oldest `limit` events (direction `"f"`).
15. Unit tests (Go `httptest`): happy path → 200 with `chunk`, `start`, `end`, `state`; unauthenticated → 401; non-member → 403; room not found → 404; invalid limit → 400; empty room returns `chunk: []` and empty `end`.
16. Unit tests (Elixir ExUnit): `get_messages/2` happy path → events returned in correct order; non-member → `GRPC.RPCError PERMISSION_DENIED`; room not found → `GRPC.RPCError NOT_FOUND`; pagination: second page with `from_token` returns next batch.
17. `make test-unit-go` and `make test-unit-elixir` pass with zero new test failures.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Happy path: fetch messages — Go unit test (httptest)**
- Given: valid JWT + mock core client returns `GetMessagesResponse` with 2 events, `next_batch="v1_abc"`, `prev_batch="v1_xyz"`
- When: `GET /_matrix/client/v3/rooms/!room1:test.local/messages?dir=b&limit=10`
- Then: `200` with body `{"chunk": [<2 events>], "start": "v1_xyz", "end": "v1_abc", "state": []}`

**2. Unauthenticated request — Go unit test (httptest)**
- Given: no `Authorization` header
- When: `GET /_matrix/client/v3/rooms/!room1:test.local/messages`
- Then: `401` with `{"errcode": "M_MISSING_TOKEN"}`

**3. Non-member (forbidden) — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `PERMISSION_DENIED` error
- When: `GET /_matrix/client/v3/rooms/!private:test.local/messages`
- Then: `403` with `{"errcode": "M_FORBIDDEN"}`

**4. Room not found — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `NOT_FOUND` error
- When: `GET /_matrix/client/v3/rooms/!nonexistent:test.local/messages`
- Then: `404` with `{"errcode": "M_NOT_FOUND"}`

**5. Invalid limit parameter — Go unit test (httptest)**
- Given: valid JWT
- When: `GET /_matrix/client/v3/rooms/!room1:test.local/messages?limit=abc`
- Then: `400` with `{"errcode": "M_INVALID_PARAM"}`

**6. Empty room — Go unit test (httptest)**
- Given: valid JWT + mock core client returns `GetMessagesResponse` with 0 events, `next_batch=""`, `prev_batch=""`
- When: `GET /_matrix/client/v3/rooms/!empty:test.local/messages`
- Then: `200` with `{"chunk": [], "start": "", "end": "", "state": []}`

**7. Default direction and limit — Go unit test (httptest)**
- Given: valid JWT + mock core client captures `GetMessagesRequest`
- When: `GET /_matrix/client/v3/rooms/!room1:test.local/messages` (no params)
- Then: captured request has `direction="b"` and `limit=10`

**8. Elixir: get_messages happy path → events in backward order — ExUnit**
- Given: Room GenServer running for `"!msgtest:test.local"`; `@alice:test.local` is a member; 3 events were sent previously and persisted to FakeDB
- When: `Nebu.EventDispatcher.Server.get_messages(%Core.GetMessagesRequest{room_id: "!msgtest:test.local", direction: "b", limit: 10, from_token: "", to_token: ""}, stream)` is called
- Then: returns `%Core.GetMessagesResponse{events: [<3 events in reverse ts order>], next_batch: token, prev_batch: ""}`

**9. Elixir: non-member → GRPC.RPCError PERMISSION_DENIED — ExUnit**
- Given: Room GenServer running; `@bob:test.local` is NOT a member
- When: `get_messages/2` is called with `sender_id: "@bob:test.local"` (via gRPC metadata)
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()`

**10. Elixir: room not found → GRPC.RPCError NOT_FOUND — ExUnit**
- Given: no Room GenServer running for `"!ghost:test.local"`
- When: `get_messages/2` is called for that room
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.not_found()`

**11. Elixir: pagination — second page with from_token returns next batch — ExUnit**
- Given: Room GenServer running; 5 events in FakeDB; first call returned `next_batch="v1_abc"`
- When: `get_messages/2` is called with `from_token: "v1_abc"`, `limit: 3`
- Then: returns events not in the first batch, `prev_batch` is non-empty

---

## Technical Requirements

### CRITICAL: File Location — NEW FILE `gateway/internal/matrix/messages.go`

**The architecture explicitly assigns `messages.go` as the file for this handler** (see architecture.md, directory tree section):

```
gateway/internal/matrix/
  messages.go   ← GET /rooms/{id}/messages   ← THIS STORY
  rooms.go      ← POST /createRoom, POST /join/{id}, PUT /send/{eventType}/{txnId}
```

Do NOT add `GetMessagesHandler` to `rooms.go`. Create `messages.go` as a NEW file in the same `matrix` package. This follows the one-handler-per-logical-resource pattern established by `login.go`, `logout.go`.

### Go Handler — `gateway/internal/matrix/messages.go` (NEW FILE)

**Handler struct and types:**

```go
package matrix

import (
    "context"
    "encoding/json"
    "net/http"
    "strconv"

    coregrpc "github.com/nebu/nebu/internal/grpc"
    pb "github.com/nebu/nebu/internal/grpc/pb"
    "github.com/nebu/nebu/internal/middleware"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// GetMessagesCoreClient is the consumer-defined interface for the GetMessages gRPC call.
// Keep minimal — only what this handler needs (Go interface convention, ADR-009).
type GetMessagesCoreClient interface {
    GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error)
}

// matrixEvent is the Matrix Client-Server API event format for /messages chunks.
type matrixEvent struct {
    EventID        string         `json:"event_id"`
    RoomID         string         `json:"room_id"`
    Sender         string         `json:"sender"`
    Type           string         `json:"type"`
    Content        map[string]any `json:"content"`
    OriginServerTS int64          `json:"origin_server_ts"`
}

// getMessagesResponse is the JSON response for GET /rooms/{roomId}/messages.
type getMessagesResponse struct {
    Start string        `json:"start"`
    End   string        `json:"end"`
    Chunk []matrixEvent `json:"chunk"`
    State []any         `json:"state"`
}

// GetMessagesHandler handles GET /_matrix/client/v3/rooms/{roomId}/messages.
type GetMessagesHandler struct {
    coreClient GetMessagesCoreClient
    serverName string
}

// GetMessagesConfig holds dependencies for NewGetMessagesHandler.
type GetMessagesConfig struct {
    CoreClient GetMessagesCoreClient
    ServerName string
}

// NewGetMessagesHandler constructs a GetMessagesHandler from the provided config.
func NewGetMessagesHandler(cfg GetMessagesConfig) *GetMessagesHandler {
    return &GetMessagesHandler{
        coreClient: cfg.CoreClient,
        serverName: cfg.ServerName,
    }
}
```

**Handler logic (GetRoomMessages):**

```go
// GetRoomMessages handles GET /_matrix/client/v3/rooms/{roomId}/messages.
//
// Flow:
//  1. Extract roomId from URL path via Go 1.22+ r.PathValue.
//  2. Parse query params: from, dir (default "b"), limit (default 10, clamp 1-100), to.
//  3. Return 400 M_INVALID_PARAM if limit is provided but non-numeric.
//  4. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  5. Call Core.GetMessages — map gRPC errors to Matrix error codes.
//  6. Map response events to Matrix format; return 200.
func (h *GetMessagesHandler) GetRoomMessages(w http.ResponseWriter, r *http.Request) {
    roomID := r.PathValue("roomId")

    q := r.URL.Query()

    // Parse direction — default "b"
    dir := q.Get("dir")
    if dir == "" {
        dir = "b"
    }

    // Parse limit — default 10, clamp 1–100, error on non-numeric
    limitStr := q.Get("limit")
    limit := int32(10)
    if limitStr != "" {
        parsed, err := strconv.Atoi(limitStr)
        if err != nil {
            writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "limit must be an integer")
            return
        }
        if parsed < 1 {
            parsed = 1
        }
        if parsed > 100 {
            parsed = 100
        }
        limit = int32(parsed)
    }

    fromToken := q.Get("from")
    toToken   := q.Get("to")

    sub, _        := r.Context().Value(middleware.ContextKeySub).(string)
    systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
    userID        := coregrpc.FormatUserID(sub, h.serverName)
    grpcCtx       := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

    resp, err := h.coreClient.GetMessages(grpcCtx, &pb.GetMessagesRequest{
        RoomId:    roomID,
        FromToken: fromToken,
        ToToken:   toToken,
        Limit:     limit,
        Direction: dir,
    })
    if err != nil {
        st, _ := status.FromError(err)
        switch st.Code() {
        case codes.NotFound:
            writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
        case codes.PermissionDenied:
            writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
        default:
            writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
        }
        return
    }

    // Map proto events to Matrix JSON format.
    chunk := make([]matrixEvent, 0, len(resp.Events))
    for _, e := range resp.Events {
        var content map[string]any
        if len(e.Content) > 0 {
            _ = json.Unmarshal(e.Content, &content)
        }
        if content == nil {
            content = map[string]any{}
        }
        chunk = append(chunk, matrixEvent{
            EventID:        e.EventId,
            RoomID:         e.RoomId,
            Sender:         e.SenderId,
            Type:           e.EventType,
            Content:        content,
            OriginServerTS: e.OriginTs,
        })
    }

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(getMessagesResponse{
        Start: resp.PrevBatch,
        End:   resp.NextBatch,
        Chunk: chunk,
        State: []any{},
    })
}
```

**Note:** `writeMatrixError` is defined in `login.go` in the same `matrix` package — already accessible without additional import.

### Go gRPC Client — `gateway/internal/grpc/client.go` (MODIFY)

Wire the `GetMessages` stub (currently returns `nil, nil`) to the real gRPC call:

```go
// GetMessages fetches paginated room history from Elixir Core.
func (c *Client) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
    return c.core.GetMessages(ctx, req)
}
```

**Also update `client_test.go`:** The `TestStubsReturnNil` test currently expects `nil, nil` for `GetMessages`. After wiring, update this entry to expect a connection error (same pattern as `CreateRoom`, `JoinRoom`, `InviteUser`, `SendEvent`):

```go
{
    name: "GetMessages",
    call: func() error {
        // GetMessages is wired to the real gRPC client (Story 4-12),
        // so it returns a connection error when no server is running.
        _, err := c.GetMessages(ctx, &pb.GetMessagesRequest{})
        if err == nil {
            return fmt.Errorf("want connection error; got nil")
        }
        return nil
    },
},
```

### Go Router — `gateway/cmd/gateway/main.go` (MODIFY)

Register the new handler behind `jwtMiddleware`. Add after the `sendEventHandler` block:

```go
messagesHandler := matrix.NewGetMessagesHandler(matrix.GetMessagesConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/messages",
    jwtMiddleware(http.HandlerFunc(messagesHandler.GetRoomMessages)))
```

**IMPORTANT:** `coreClient`, `serverName`, and `jwtMiddleware` are already constructed — reuse, do NOT redeclare. `*coregrpc.Client` satisfies `GetMessagesCoreClient` once `GetMessages` is wired.

### Proto — `proto/core.proto`

The `GetMessages` RPC and its messages are **ALREADY FULLY DEFINED**. Do NOT add duplicates. Existing definition (field names matter for gRPC stub calls):

```protobuf
rpc GetMessages(GetMessagesRequest) returns (GetMessagesResponse);

message GetMessagesRequest {
  string          room_id    = 1;
  string          from_token = 2;
  optional string to_token   = 3;
  int32           limit      = 4;
  string          direction  = 5;  // "b" (backward) or "f" (forward)
}
message GetMessagesResponse {
  repeated Event events     = 1;
  string         next_batch = 2;
  string         prev_batch = 3;
}
```

**Proto field name mapping (protobuf generated PascalCase in Go):**
- `room_id` → `RoomId`
- `from_token` → `FromToken`
- `to_token` → `ToToken`
- `limit` → `Limit`
- `direction` → `Direction`
- `next_batch` → `NextBatch`
- `prev_batch` → `PrevBatch`
- `events` → `Events`

**No proto changes needed. Do NOT run `make proto`.** The generated stubs in `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/` already include `GetMessages`.

### Elixir gRPC Handler — `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (MODIFY)

Replace the current stub `get_messages/2`:

```elixir
def get_messages(request, stream) do
  room_id    = request.room_id
  from_token = request.from_token
  to_token   = request.to_token
  limit      = max(1, min(request.limit, 100))
  direction  = if request.direction in ["f", "b"], do: request.direction, else: "b"

  # Extract caller identity from gRPC metadata (set by Go JWTMiddleware).
  {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  # Room existence check — Room GenServer must be running.
  case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
    {:error, :not_found} ->
      raise GRPC.RPCError,
        status: GRPC.Status.not_found(),
        message: "room not found: #{room_id}"

    {:ok, _pid} ->
      # Membership check — caller must be a room member.
      state = room_registry_module().get_state(room_id)

      unless MapSet.member?(state.members, user_id) do
        raise GRPC.RPCError,
          status: GRPC.Status.permission_denied(),
          message: "#{user_id} is not a member of #{room_id}"
      end

      # Fetch events from PostgreSQL via the configurable db_module.
      {:ok, events, next_batch, prev_batch} =
        db_module().fetch_events(room_id, from_token, direction, limit)

      proto_events = Enum.map(events, &event_map_to_proto/1)

      %Core.GetMessagesResponse{
        events:     proto_events,
        next_batch: next_batch,
        prev_batch: prev_batch
      }
  end
end
```

**Configurable db_module:** Add a new `db_module/0` private function alongside the existing `room_registry_module/0` and `db_module_invite/0`:

```elixir
# Override via Application.put_env(:event_dispatcher, :db_module, FakeDB) in tests.
defp db_module do
  Application.get_env(:event_dispatcher, :db_module, Nebu.Room.DB)
end
```

**Event mapping helper:**

```elixir
defp event_map_to_proto(event) do
  # event is a map with string keys from Ecto.Adapters.SQL.query rows
  content_json = Jason.encode!(Map.get(event, "content", %{}))

  %Core.Event{
    event_id:   Map.get(event, "event_id", ""),
    room_id:    Map.get(event, "room_id", ""),
    sender_id:  Map.get(event, "sender", ""),
    event_type: Map.get(event, "event_type", ""),
    content:    content_json,
    origin_ts:  Map.get(event, "origin_server_ts", 0),
    server_ts:  System.system_time(:millisecond)
  }
end
```

**IMPORTANT — `user_id` from metadata, NOT from request:** Unlike `send_event/2` which receives `sender_id` explicitly in the proto request, `get_messages` does NOT have a `user_id` field in `GetMessagesRequest`. Extract the caller's identity via `Nebu.Grpc.Metadata.trusted_identity(stream)` — this returns `{user_id, system_role}` from the `x-user-id` gRPC metadata header set by the Go JWTMiddleware. Pattern identical to `validate_token/2`.

### Elixir DB — `core/apps/room_manager/lib/nebu/room/db.ex` (MODIFY)

Add a new `fetch_events/4` function for keyset-paginated event retrieval:

```elixir
@doc """
Fetches paginated events from the `events` table for the given room.

Pagination is keyset-based on `(origin_server_ts, event_id)` — both fields
together provide a stable, unique cursor that avoids duplicates across pages.

Token format: `"v1_" <> Base.url_encode64(ts_str <> ":" <> event_id, padding: false)`
Empty token means "start from the beginning" (direction-dependent).

Direction "b" (backward): newest first (DESC origin_server_ts, DESC event_id).
Direction "f" (forward):  oldest first (ASC  origin_server_ts, ASC  event_id).

Returns `{:ok, events, next_batch, prev_batch}` where:
- `events` is a list of maps with string keys matching the `events` table columns
- `next_batch` is the cursor for the next page (empty string if no more pages)
- `prev_batch` is the cursor for the previous page (the `from_token` echoed back)
"""
@spec fetch_events(String.t(), String.t(), String.t(), integer()) ::
        {:ok, [map()], String.t(), String.t()}
def fetch_events(room_id, from_token, direction, limit) do
  {order, compare_op, ts_field_alias} =
    if direction == "f" do
      {"ASC", ">", "first_ts"}
    else
      {"DESC", "<", "last_ts"}
    end

  {where_clause, params} =
    case decode_token(from_token) do
      {:ok, cursor_ts, cursor_event_id} ->
        # Keyset: fetch events strictly before/after the cursor.
        # direction "b": WHERE (origin_server_ts, event_id) < (cursor_ts, cursor_event_id)
        # direction "f": WHERE (origin_server_ts, event_id) > (cursor_ts, cursor_event_id)
        clause =
          "(origin_server_ts, event_id) #{compare_op} ($2, $3)"
        {clause, [room_id, cursor_ts, cursor_event_id]}

      :empty ->
        {"TRUE", [room_id]}
    end

  param_offset = length(params)  # $1 = room_id; cursor params start at $2/$3
  limit_param  = "$#{param_offset + 1}"

  sql = """
  SELECT event_id, room_id, sender, event_type, content, origin_server_ts
  FROM events
  WHERE room_id = $1 AND #{where_clause}
  ORDER BY origin_server_ts #{order}, event_id #{order}
  LIMIT #{limit_param}
  """

  all_params = params ++ [limit + 1]  # fetch limit+1 to detect if there is a next page

  case Ecto.Adapters.SQL.query(Nebu.Repo, sql, all_params) do
    {:ok, %{columns: cols, rows: rows}} ->
      all_events =
        Enum.map(rows, fn row ->
          cols
          |> Enum.zip(row)
          |> Map.new()
        end)

      {page_events, has_more} =
        if length(all_events) > limit do
          {Enum.take(all_events, limit), true}
        else
          {all_events, false}
        end

      next_batch =
        if has_more do
          last = List.last(page_events)
          encode_token(last["origin_server_ts"], last["event_id"])
        else
          ""
        end

      prev_batch =
        case page_events do
          [] -> from_token
          [first | _] -> encode_token(first["origin_server_ts"], first["event_id"])
        end

      {:ok, page_events, next_batch, prev_batch}

    {:error, reason} ->
      {:error, reason}
  end
end

defp encode_token(ts, event_id) when is_integer(ts) and is_binary(event_id) do
  raw = "#{ts}:#{event_id}"
  "v1_" <> Base.url_encode64(raw, padding: false)
end

defp decode_token(""), do: :empty
defp decode_token(nil), do: :empty

defp decode_token("v1_" <> encoded) do
  case Base.url_decode64(encoded, padding: false) do
    {:ok, raw} ->
      case String.split(raw, ":", parts: 2) do
        [ts_str, event_id] ->
          case Integer.parse(ts_str) do
            {ts, ""} -> {:ok, ts, event_id}
            _ -> :empty
          end
        _ -> :empty
      end
    _ -> :empty
  end
end

defp decode_token(_), do: :empty
```

**IMPORTANT:** The `content` column in the `events` table is `JSONB`. Postgrex returns JSONB columns as already-decoded Elixir maps (not strings). When mapping rows to event maps, `content` will be a `%{}` map — NOT a string. Use `Jason.encode!/1` to re-serialize for the proto `content` bytes field.

**IMPORTANT:** Do NOT add `insert_event/1`-style logic. `fetch_events/4` is read-only.

### Elixir Unit Tests — `core/apps/event_dispatcher/test/nebu/event_dispatcher/get_messages_test.exs` (NEW FILE)

Create a dedicated test file for `get_messages/2`. Follow the exact same `FakeDB` + `setup` pattern as `send_event_test.exs`.

**FakeDB must implement `fetch_events/4`** in addition to the existing callbacks. The fake should store events in ETS (keyed by `{:event, room_id, origin_ts, event_id}`) and return them in the correct order based on `direction`.

**Test setup requirements:**
- `async: false` — shared Horde state
- Start Room GenServer via `Nebu.Room.RoomSupervisor.start_room/1` in setup
- Inject `FakeDB` via `Application.put_env(:event_dispatcher, :db_module, FakeDB)` (new env key — separate from `:room_manager, :db_module`)
- Inject `FakeRoomRegistry` via `Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)` for membership checks
- Build a fake gRPC stream with `x-user-id` metadata so `Nebu.Grpc.Metadata.trusted_identity(stream)` returns the test user

**Fake stream with metadata (critical — `get_messages/2` calls `trusted_identity/1`):**

```elixir
defp build_stream(user_id) do
  %{http_request_headers: %{"x-user-id" => user_id, "x-system-role" => "user"}}
end
```

---

## File Locations

### New files:

```
gateway/
  internal/
    matrix/
      messages.go         ← New: GetMessagesHandler, GetMessagesCoreClient, GetRoomMessages
      messages_test.go    ← New: Go httptest tests for GetRoomMessages

core/apps/event_dispatcher/
  test/nebu/event_dispatcher/
    get_messages_test.exs ← New: Elixir ExUnit tests for get_messages/2
```

### Modify (existing files):

```
gateway/
  cmd/gateway/main.go                          ← Register GET /rooms/{roomId}/messages
  internal/
    grpc/
      client.go                               ← Wire GetMessages stub (remove nil,nil)
      client_test.go                          ← Update GetMessages test case (nil,nil → connection error)

core/apps/event_dispatcher/
  lib/nebu/event_dispatcher/server.ex         ← Replace get_messages/2 stub; add db_module/0; add event_map_to_proto/1

core/apps/room_manager/
  lib/nebu/room/db.ex                         ← Add fetch_events/4, encode_token/2, decode_token/1
```

### Do NOT create new files beyond the above:
- No new proto files — `GetMessages` is already fully defined.
- No new migrations — `events` table exists (`000010_events.up.sql` from Story 4-4). The index `events_room_id_ts_idx ON events (room_id, origin_server_ts)` covers the keyset query.
- Do NOT modify any files in `core/apps/signature/` — event signing is not relevant here (read path only).

---

## Architecture Compliance

| Rule | Requirement |
|---|---|
| Architecture tree | `messages.go` is the designated file for `GET /rooms/{id}/messages` — do NOT add to `rooms.go` |
| Rule #1 | Timestamps as BIGINT — `events.origin_server_ts` is already BIGINT; cursor encodes as integer ms |
| Rule #3 | Matrix endpoints return Matrix format; never Admin format (`{"errcode": ...}`) |
| ADR-002 | No Redis — keyset cursor is stateless; no session-side state needed |
| ADR-003 | Event IDs are content-hash based (stored, not recomputed here) |
| ADR-009 | Consumer-defined interface `GetMessagesCoreClient` — minimal, defined in `messages.go` |
| Go conventions | Errors explicit, context threaded, interfaces minimal (consumer-defined) |
| Elixir conventions | Let-it-crash + Supervisor; no defensive try/rescue in happy path |
| No panic in library code | `writeMatrixError` + return, no `panic()` anywhere |
| Matrix API response format | `{"start": "...", "end": "...", "chunk": [...], "state": []}` — exact Matrix spec |
| `state` field | Always `[]` (empty) — room state event population is out of scope |

---

## Previous Story Intelligence

### From Story 4-11 (SendEvent — just completed, establishes exact patterns):

- **Consumer-defined interface pattern:** `SendEventCoreClient` in `rooms.go` → use same pattern for `GetMessagesCoreClient` in `messages.go`.
- **Error mapping:** `status.FromError(err)` → `switch st.Code()` — exact same approach.
- **`r.PathValue("roomId")`** — Go 1.22+ standard; confirmed working for room IDs with `!` and `:`.
- **`coregrpc.FormatUserID(sub, serverName)` + `coregrpc.WithUserMetadata(...)`** — call before every gRPC call.
- **`writeMatrixError`** is in `login.go` in the same `matrix` package — already accessible.
- **Test pattern:** `buildAuthedHandler` with real OIDC test server + `signJWT` + `setupOIDCServer` — all in the `matrix` package's `rooms_test.go`. Use same helpers in `messages_test.go`.
- **mux routing in tests:** Use a real `http.NewServeMux()` to exercise `r.PathValue(...)` correctly in httptest.

### From Story 4-10 (JoinRoom/InviteUser — shows Elixir `trusted_identity` pattern):

- `Nebu.Grpc.Metadata.trusted_identity(stream)` returns `{user_id, system_role}` from the stream's `http_request_headers`. This is how `validate_token/2` and other handlers extract the caller identity. **Use this in `get_messages/2` for the membership check** (unlike `send_event/2` which had `sender_id` in the request body).
- In tests, simulate this by building the stream map with `%{http_request_headers: %{"x-user-id" => user_id, "x-system-role" => "user"}}`.

### From Story 4-4 (Room.Server.send_event — DB patterns):

- `Nebu.Room.DB` uses `Ecto.Adapters.SQL.query/3` with raw SQL — no Ecto schemas. Follow the same pattern for `fetch_events/4`.
- Postgrex returns JSONB columns as decoded Elixir maps, NOT as strings. When building the proto event, call `Jason.encode!/1` on `content` before assigning to `Core.Event.content` (which is a `bytes` field).
- The `events` table has `event_type` as the column name (not `type`). The `sender` column holds the user_id string.

### From Story 4-9 (CreateRoom — handler skeleton for new Go files):

- New handler file pattern: define consumer interface, response struct, handler struct, config struct, constructor, then ServeHTTP method. See `rooms.go` for the established pattern.
- Route registration in `main.go`: place after all existing room-related handlers.

### Known module naming (critical — epics.md may use wrong names):

| Epics.md Name | Correct Codebase Name |
|---|---|
| `RoomManager.RoomServer` | `Nebu.Room.Server` |
| `CoreService.GetRoomMessages` (epics) | `Nebu.EventDispatcher.Server.get_messages/2` (Elixir handler) |
| `GetRoomMessages` (epics) | `GetMessages` (actual proto RPC name) |

---

## Key Cross-Story Context

| Story | Relationship to 4-12 |
|---|---|
| Story 4-4 (done) | `events` table + `000010_events.up.sql` exists — no new migration; `events_room_id_ts_idx` covers the keyset query |
| Story 4-4 (done) | `Nebu.Room.DB` is the persistence layer to extend with `fetch_events/4` |
| Story 4-9/4-10/4-11 (done) | `rooms.go` is complete — do NOT add to it; `messages.go` is the new file |
| Story 4-11 (done) | gRPC client pattern, error mapping, JWTMiddleware wrapping — exact reuse |
| Story 4-13 (backlog) | Power level enforcement will add more `PERMISSION_DENIED` cases from `Room.Server` — `get_messages/2` membership check handles this |
| Story 4-14/4-15 (backlog) | `/sync` will fetch recent messages for initial/incremental sync — may reuse `fetch_events/4` |
| Story 6-9 (backlog) | Room archival story: `GET /messages` must still return `200` after archival (read-only) — the current implementation naturally supports this |
| Story 4-21 (backlog) | End-to-end Gherkin test will exercise this endpoint |

---

## Go Test Pattern — `messages_test.go`

```go
package matrix

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/nebu/nebu/internal/auth"
    pb "github.com/nebu/nebu/internal/grpc/pb"
    "github.com/nebu/nebu/internal/middleware"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type mockGetMessagesCoreClient struct {
    resp        *pb.GetMessagesResponse
    err         error
    capturedReq *pb.GetMessagesRequest
}

func (m *mockGetMessagesCoreClient) GetMessages(_ context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
    m.capturedReq = req
    return m.resp, m.err
}

func buildGetMessagesHandler(mock *mockGetMessagesCoreClient) *GetMessagesHandler {
    return NewGetMessagesHandler(GetMessagesConfig{
        CoreClient: mock,
        ServerName: "test.local",
    })
}

// buildAuthedGetMessagesHandler wraps the handler in JWTMiddleware and returns
// the handler chain + a token factory. Reuses setupOIDCServer + signJWT from
// rooms_test.go (same package — no re-import needed).
func buildAuthedGetMessagesHandler(t *testing.T, mock *mockGetMessagesCoreClient) (http.Handler, func() string) {
    t.Helper()
    oidcSrv, privateKey := setupOIDCServer(t)
    t.Cleanup(oidcSrv.Close)
    provider := auth.NewProvider(context.Background(), oidcSrv.URL)
    handler := buildGetMessagesHandler(mock)
    authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)(
        http.HandlerFunc(handler.GetRoomMessages),
    )
    makeToken := func() string {
        return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
    }
    return authed, makeToken
}
```

**URL construction in tests (critical):** Use a real `http.NewServeMux()` to process `PathValue`:

```go
mux := http.NewServeMux()
mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/messages", authed)
req := httptest.NewRequest(http.MethodGet,
    "/_matrix/client/v3/rooms/!room1:test.local/messages?dir=b&limit=10", nil)
req.Header.Set("Authorization", "Bearer "+makeToken())
rr := httptest.NewRecorder()
mux.ServeHTTP(rr, req)
// assert rr.Code == 200
```

**Decoding the response body in tests:**

```go
var body struct {
    Start string          `json:"start"`
    End   string          `json:"end"`
    Chunk []map[string]any `json:"chunk"`
    State []any            `json:"state"`
}
require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
```

**Mock response with events:**

```go
protoEvent := &pb.Event{
    EventId:   "$abc123",
    RoomId:    "!room1:test.local",
    SenderId:  "@alice:test.local",
    EventType: "m.room.message",
    Content:   []byte(`{"msgtype":"m.text","body":"hello"}`),
    OriginTs:  1700000000000,
}
mock := &mockGetMessagesCoreClient{
    resp: &pb.GetMessagesResponse{
        Events:    []*pb.Event{protoEvent},
        NextBatch: "v1_abc",
        PrevBatch: "v1_xyz",
    },
}
```

---

## Elixir Test — Key Setup Differences from `send_event_test.exs`

1. **New `db_module` env key:** `Application.put_env(:event_dispatcher, :db_module, FakeDB)` — this is a NEW application env key used by `get_messages/2`. It does NOT conflict with `:room_manager, :db_module` used by Room.Server.

2. **`FakeDB` must implement `fetch_events/4`:**

```elixir
defmodule FakeDB do
  def fetch_events(room_id, _from_token, direction, limit) do
    # Read all events for room from ETS, sort by origin_server_ts
    events = :ets.match_object(:get_msg_test_db, {{:event, room_id, :_, :_}, :_})
    |> Enum.map(fn {_, event} -> event end)
    |> Enum.sort_by(fn e -> e["origin_server_ts"] end,
         if(direction == "f", do: :asc, else: :desc))
    |> Enum.take(limit)

    next_batch = if length(events) == limit, do: "v1_next", else: ""
    {:ok, events, next_batch, ""}
  end
end
```

3. **Fake stream with user_id metadata:**

```elixir
defp build_stream(user_id) do
  %{http_request_headers: %{"x-user-id" => user_id, "x-system-role" => "user"}}
end
```

4. **Insert test events into FakeDB ETS:**

```elixir
defp insert_test_event(room_id, event_id, sender, ts) do
  event = %{
    "event_id" => event_id,
    "room_id"  => room_id,
    "sender"   => sender,
    "event_type" => "m.room.message",
    "content"  => %{"msgtype" => "m.text", "body" => "hello"},
    "origin_server_ts" => ts
  }
  :ets.insert(:get_msg_test_db, {{:event, room_id, ts, event_id}, event})
end
```

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
- All existing Story 4-9/4-10/4-11 Go tests continue to pass (no regression in `rooms_test.go`).
- All existing Story 4-4 Elixir tests continue to pass (no changes to `Nebu.Room.Server`).
- 6+ new Go `httptest` tests in `messages_test.go` pass (AC #1–#7).
- 4+ new Elixir ExUnit tests in `get_messages_test.exs` pass (AC #8–#11).
- `TestStubsReturnNil` for `GetMessages` updated to expect connection error (not `nil, nil`).

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

Fixed FakeDB `prev_batch` logic in `get_messages_test.exs`: the original FakeDB computed `prev_batch` as the token of the NEWEST event in the page (`List.first(page)` for dir=b), which caused overlapping events when the second page was fetched. Fixed to use `List.last(page)` (oldest event), consistent with keyset pagination semantics. Updated production `fetch_events/4` in `db.ex` to match the same `List.last` convention.

Removed duplicate `GetMessagesCoreClient` interface declaration from `messages_test.go` (it was pre-written in the test file, but must live in production `messages.go` since test-only types cannot be referenced from non-test code).

### Completion Notes List

All 7 Go httptest tests in `gateway/internal/matrix/messages_test.go` pass: happy path (200 with chunk/start/end/state), unauthenticated (401), room not found (404), non-member (403), invalid dir (400), empty room (200 with chunk=[]), default limit (10 forwarded to gRPC).

All 4 Elixir ExUnit tests in `get_messages_test.exs` pass: happy path (events in reverse chronological order), empty room (empty chunk), room not found (GRPC NOT_FOUND), non-member (GRPC PERMISSION_DENIED), pagination (no overlap between pages).

`TestStubsReturnNil` updated: GetMessages now expects connection error (not nil,nil), matching the wired real gRPC call.

make test-unit-go: 10 packages, 0 failures
make test-unit-elixir: 58 tests, 0 failures, 1 skipped (pre-existing skip)

### File List

- gateway/internal/matrix/messages.go (NEW)
- gateway/internal/matrix/messages_test.go (MODIFIED — removed duplicate interface, fixed comment)
- gateway/internal/grpc/client.go (MODIFIED — wired GetMessages stub)
- gateway/internal/grpc/client_test.go (MODIFIED — updated GetMessages test expectation)
- gateway/cmd/gateway/main.go (MODIFIED — registered GET /rooms/{roomId}/messages route)
- core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex (MODIFIED — implemented get_messages/2, added messages_db_module/0, added event_map_to_proto/1)
- core/apps/room_manager/lib/nebu/room/db.ex (MODIFIED — added fetch_events/4, encode_token/2, decode_token/1)
- core/apps/event_dispatcher/test/nebu/event_dispatcher/get_messages_test.exs (MODIFIED — fixed FakeDB prev_batch pagination logic)
- _bmad-output/implementation-artifacts/sprint-status.yaml (MODIFIED — 4-12 status updated)

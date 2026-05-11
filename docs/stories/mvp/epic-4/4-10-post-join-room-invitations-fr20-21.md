# Story 4.10: POST /join + Room Invitations (FR20/21)

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-10-post-join-room-invitations-fr20-21
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to join a room directly or accept an invitation,
so that I can participate in conversations I've been invited to or discover public rooms.

---

## Acceptance Criteria

1. `POST /_matrix/client/v3/join/{roomIdOrAlias}` is protected by `JWTMiddleware` — unauthenticated requests return `401 M_MISSING_TOKEN`.
2. `POST /_matrix/client/v3/join/{roomIdOrAlias}` calls `gRPC CoreService.JoinRoom` with `user_id` (from JWT) and `room_id_or_alias`; returns `200 {"room_id": "!<id>:<server>"}` on success.
3. `POST /_matrix/client/v3/join/{roomIdOrAlias}` returns `404 M_NOT_FOUND` if the room does not exist (Core returns `NOT_FOUND` gRPC status).
4. `POST /_matrix/client/v3/join/{roomIdOrAlias}` returns `403 M_FORBIDDEN` if the room is private and the user has not been invited (Core returns `PERMISSION_DENIED` gRPC status).
5. `POST /_matrix/client/v3/rooms/{roomId}/join` (accept invitation) calls the same `gRPC CoreService.JoinRoom`; returns `200 {"room_id": ...}` or `403 M_FORBIDDEN` if no invitation exists.
6. `POST /_matrix/client/v3/rooms/{roomId}/invite` is protected by `JWTMiddleware`; body `{"user_id": "@target:<server>"}` calls `gRPC CoreService.InviteUser`; returns `200 {}` on success, `403 M_FORBIDDEN` if caller lacks invite power level, `400 M_BAD_JSON` on invalid body, `404 M_NOT_FOUND` if room does not exist.
7. If the user is already a member of the room (idempotent join): `JoinRoom` in Core returns `:ok` (not an error), and Go returns `200 {"room_id": ...}` — Matrix spec requires idempotent join.
8. The gRPC `JoinRoom` stub in `gateway/internal/grpc/client.go` is wired up to call `c.core.JoinRoom(ctx, req)`.
9. A new `InviteUser` method is added to `gateway/internal/grpc/client.go` calling `c.core.InviteUser(ctx, req)`.
10. The Elixir `join_room/2` handler in `Nebu.EventDispatcher.Server` is implemented: looks up the Room GenServer via `Nebu.Room.RoomSupervisor.lookup_room/1`, calls `Nebu.Room.Server.join/2`, and returns `%Core.JoinRoomResponse{room_id: room_id}`.
11. The Elixir `invite_user/2` handler in `Nebu.EventDispatcher.Server` is implemented: validates caller is a member of the room, inserts an invitation record into `room_invitations` DB table, and returns `%Core.InviteUserResponse{}`.
12. `proto/core.proto` must be updated to add `rpc InviteUser(InviteUserRequest) returns (InviteUserResponse)` with appropriate message types; `make proto` must be run to regenerate stubs.
13. A new DB migration `000012_room_invitations.up.sql` is created for the `room_invitations` table.
14. Unit tests (Go `httptest`): join existing room → 200; room not found → 404; private room, no invite → 403; missing auth → 401; invite user → 200; invite with bad JSON → 400.
15. Unit tests (Elixir ExUnit): join_room succeeds → room_id returned; join_room room not found → GRPC.RPCError NOT_FOUND; join_room already member → idempotent success; invite_user succeeds → invitation stored.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Happy path: join a room — Go unit test (httptest)**
- Given: valid JWT + mock core client returns `JoinRoomResponse{room_id: "!abc:test.local"}`
- When: `POST /_matrix/client/v3/join/!abc:test.local`
- Then: response is `200` with body `{"room_id": "!abc:test.local"}`

**2. Unauthenticated request — Go unit test (httptest)**
- Given: no `Authorization` header
- When: `POST /_matrix/client/v3/join/!abc:test.local`
- Then: `401` with `{"errcode": "M_MISSING_TOKEN"}`

**3. Room not found — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `NOT_FOUND` error
- When: `POST /_matrix/client/v3/join/!nonexistent:test.local`
- Then: `404` with `{"errcode": "M_NOT_FOUND"}`

**4. Forbidden (no invite, private room) — Go unit test (httptest)**
- Given: valid JWT + mock core client returns gRPC `PERMISSION_DENIED` error
- When: `POST /_matrix/client/v3/join/!private:test.local`
- Then: `403` with `{"errcode": "M_FORBIDDEN"}`

**5. Invite user — Go unit test (httptest)**
- Given: valid JWT + mock core client returns `InviteUserResponse{}`
- When: `POST /_matrix/client/v3/rooms/!abc:test.local/invite` with body `{"user_id": "@bob:test.local"}`
- Then: `200` with body `{}`

**6. Invite with bad JSON — Go unit test (httptest)**
- Given: valid JWT
- When: `POST /_matrix/client/v3/rooms/!abc:test.local/invite` with body `{not json`
- Then: `400` with `{"errcode": "M_BAD_JSON"}`

**7. Elixir: join_room succeeds — ExUnit**
- Given: Room GenServer running for `"!abc:test.local"` with FakeDB, `@alice:test.local` not yet a member
- When: `Nebu.EventDispatcher.Server.join_room(%Core.JoinRoomRequest{user_id: "@alice:test.local", room_id_or_alias: "!abc:test.local"}, stream)` is called
- Then: returns `%Core.JoinRoomResponse{room_id: "!abc:test.local"}` and `Nebu.Room.Server.get_state("!abc:test.local").members` contains `"@alice:test.local"`

**8. Elixir: join_room room not found → GRPC.RPCError NOT_FOUND — ExUnit**
- Given: no Room GenServer running for the given room_id
- When: `join_room/2` is called for that room_id
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.not_found()`

**9. Elixir: join_room already member (idempotent) — ExUnit**
- Given: `@alice:test.local` is already a member of the room
- When: `join_room/2` is called again for the same user + room
- Then: returns `%Core.JoinRoomResponse{room_id: ...}` (no error — Matrix idempotency)

**10. Elixir: invite_user stores invitation — ExUnit**
- Given: `@alice:test.local` is a member of the room, `@bob:test.local` is not
- When: `invite_user/2` is called with caller=alice, invitee=bob
- Then: returns `%Core.InviteUserResponse{}` and invitation exists in `room_invitations` table

---

## Technical Requirements

### Go Handlers — `gateway/internal/matrix/rooms.go` (EXTEND EXISTING)

**CRITICAL:** `rooms.go` was created in Story 4-9 for `CreateRoomHandler`. Do NOT create a new file. Add the new handlers to the SAME file `gateway/internal/matrix/rooms.go`. The architecture spec says: `rooms.go ← POST /createRoom, POST /join/{id}`.

#### JoinRoomHandler

```go
// JoinRoomCoreClient is a consumer-defined interface for join + invite gRPC calls.
type JoinRoomCoreClient interface {
    JoinRoom(ctx context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error)
}

type JoinRoomResponse struct {
    RoomID string `json:"room_id"`
}

type JoinRoomHandler struct {
    coreClient JoinRoomCoreClient
    serverName string
}

type JoinRoomConfig struct {
    CoreClient JoinRoomCoreClient
    ServerName string
}

func NewJoinRoomHandler(cfg JoinRoomConfig) *JoinRoomHandler
```

**Handler logic (PostJoinRoom — for both `/join/{roomIdOrAlias}` and `/rooms/{roomId}/join`):**

1. Extract `roomIdOrAlias` from URL path: `r.PathValue("roomIdOrAlias")` for `/join/{roomIdOrAlias}`; `r.PathValue("roomId")` for `/rooms/{roomId}/join`.
2. Extract `sub` + `systemRole` from context via `middleware.ContextKeySub` / `middleware.ContextKeySystemRole`.
3. Build `userID` via `coregrpc.FormatUserID(sub, h.serverName)`.
4. Attach gRPC metadata via `coregrpc.WithUserMetadata(...)`.
5. Call `h.coreClient.JoinRoom(grpcCtx, &pb.JoinRoomRequest{UserId: userID, RoomIdOrAlias: roomIdOrAlias})`.
6. Map gRPC errors:
   - `codes.NotFound` → `404 M_NOT_FOUND`
   - `codes.PermissionDenied` → `403 M_FORBIDDEN`
   - other → `500 M_UNKNOWN`
7. Return `200 {"room_id": resp.RoomId}`.

**NOTE about Go 1.22+ path parameters:** Use `r.PathValue("roomIdOrAlias")` — this is the Go 1.22+ standard `net/http` mux wildcard syntax. The existing routes in `main.go` use this pattern. The room ID contains `!` and `:` characters — the Go mux treats the entire segment as the value without URL-encoding issues when using `{param}` syntax.

#### InviteUserHandler

```go
// InviteUserCoreClient is a consumer-defined interface for InviteUser gRPC call.
type InviteUserCoreClient interface {
    InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error)
}

type InviteUserRequest struct {
    UserID string `json:"user_id"`
}

type InviteUserHandler struct {
    coreClient InviteUserCoreClient
    serverName string
}
```

**Handler logic (PostInviteUser):**

1. Extract `roomId` from path: `r.PathValue("roomId")`.
2. Decode JSON body → `400 M_BAD_JSON` on error.
3. Build caller `userID` from JWT context.
4. Call `h.coreClient.InviteUser(grpcCtx, &pb.InviteUserRequest{RoomId: roomId, InviterId: callerUserID, InviteeId: req.UserID})`.
5. Map gRPC errors: `codes.PermissionDenied` → `403 M_FORBIDDEN`; `codes.NotFound` → `404 M_NOT_FOUND`; other → `500 M_UNKNOWN`.
6. Return `200 {}` (empty JSON object: `w.Write([]byte("{}\n"))`).

### Go gRPC Client — `gateway/internal/grpc/client.go` (MODIFY)

Wire two stubs:

```go
// JoinRoom calls the Elixir core to join a room or accept an invitation.
func (c *Client) JoinRoom(ctx context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error) {
    return c.core.JoinRoom(ctx, req)
}

// InviteUser calls the Elixir core to invite a user to a room.
func (c *Client) InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
    return c.core.InviteUser(ctx, req)
}
```

**Also update `client_test.go`:** The `TestStubsReturnNil` test currently expects `nil, nil` for `JoinRoom`. After wiring, the test must be updated to expect a connection error (same pattern as `CreateRoom` in Story 4-9):

```go
{
    name: "JoinRoom",
    call: func() error {
        _, err := c.JoinRoom(ctx, &pb.JoinRoomRequest{})
        if err == nil {
            return fmt.Errorf("want connection error; got nil")
        }
        return nil
    },
},
```

Add a new entry for `InviteUser` with the same connection-error pattern.

### Go Router — `gateway/cmd/gateway/main.go` (MODIFY)

Register three new routes behind `jwtMiddleware`:

```go
joinRoomHandler := matrix.NewJoinRoomHandler(matrix.JoinRoomConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
// FR20: Join by room ID or alias directly
mux.Handle("POST /_matrix/client/v3/join/{roomIdOrAlias}",
    jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoom)))
// Accept invitation via /rooms/{roomId}/join
mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/join",
    jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoomById)))

inviteHandler := matrix.NewInviteUserHandler(matrix.InviteUserConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/invite",
    jwtMiddleware(http.HandlerFunc(inviteHandler.PostInviteUser)))
```

**IMPORTANT:** `coreClient`, `serverName`, and `jwtMiddleware` are already constructed earlier in `main.go` — reuse them directly. Do not redeclare. The `coreClient` variable is of type `*coregrpc.Client`; the consumer-defined interfaces `JoinRoomCoreClient` and `InviteUserCoreClient` are satisfied by `*coregrpc.Client` once the methods are added.

### Proto — `proto/core.proto` (MODIFY) + Run `make proto`

`JoinRoom` RPC and its messages (`JoinRoomRequest`, `JoinRoomResponse`) are **ALREADY DEFINED** in `proto/core.proto`. Do NOT duplicate them.

`InviteUser` is **MISSING** — add it:

```protobuf
// InviteUser
message InviteUserRequest {
  string room_id    = 1;
  string inviter_id = 2;  // caller (must be room member)
  string invitee_id = 3;  // user to invite
}
message InviteUserResponse {}
```

And in the `service CoreService` block, add:
```protobuf
rpc InviteUser(InviteUserRequest) returns (InviteUserResponse);
```

**THEN run `make proto`** to regenerate Go and Elixir stubs. This is mandatory since new proto messages are added. The generated files in `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/` will be updated automatically.

### DB Migration — `gateway/migrations/000012_room_invitations.up.sql` (CREATE NEW)

```sql
CREATE TABLE room_invitations (
    room_id      TEXT    NOT NULL REFERENCES rooms(room_id),
    inviter_id   TEXT    NOT NULL REFERENCES users(user_id),
    invitee_id   TEXT    NOT NULL REFERENCES users(user_id),
    invited_at   BIGINT  NOT NULL,
    accepted_at  BIGINT,
    rejected_at  BIGINT,
    PRIMARY KEY (room_id, invitee_id)
);

CREATE INDEX room_invitations_invitee_idx ON room_invitations (invitee_id);
```

Also create `000012_room_invitations.down.sql`:

```sql
DROP TABLE IF EXISTS room_invitations;
```

Migration runs automatically at gateway startup — no manual step needed. The migration number must be `000012` (after `000011_sync_tokens`).

### Elixir gRPC Handler — `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (MODIFY)

Replace the stub `join_room/2` and add `invite_user/2`:

```elixir
def join_room(request, _stream) do
  room_id = request.room_id_or_alias
  user_id = request.user_id

  case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
    {:error, :not_found} ->
      raise GRPC.RPCError,
        status: GRPC.Status.not_found(),
        message: "room not found: #{room_id}"

    {:ok, _pid} ->
      case Nebu.Room.Server.join(room_id, user_id) do
        :ok ->
          %Core.JoinRoomResponse{room_id: room_id}

        {:error, :already_member} ->
          # Matrix spec: idempotent — joining an already-joined room is success
          %Core.JoinRoomResponse{room_id: room_id}

        {:error, reason} ->
          raise GRPC.RPCError,
            status: GRPC.Status.internal(),
            message: "join failed: #{inspect(reason)}"
      end
  end
end

def invite_user(request, _stream) do
  room_id   = request.room_id
  inviter   = request.inviter_id
  invitee   = request.invitee_id

  # Verify inviter is a member of the room
  case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
    {:error, :not_found} ->
      raise GRPC.RPCError,
        status: GRPC.Status.not_found(),
        message: "room not found: #{room_id}"

    {:ok, _pid} ->
      state = Nebu.Room.Server.get_state(room_id)

      unless MapSet.member?(state.members, inviter) do
        raise GRPC.RPCError,
          status: GRPC.Status.permission_denied(),
          message: "you are not a member of this room"
      end

      case db_module_invite().insert_invitation(room_id, inviter, invitee) do
        :ok ->
          %Core.InviteUserResponse{}

        {:error, reason} ->
          raise GRPC.RPCError,
            status: GRPC.Status.internal(),
            message: "invite failed: #{inspect(reason)}"
      end
  end
end
```

**DB module for invitations** — the `invite_user/2` handler needs access to a DB module for `room_invitations`. Use the same injectable pattern as `db_module/0`:

```elixir
defp db_module_invite do
  Application.get_env(:event_dispatcher, :invite_db_module, Nebu.Room.InviteDB)
end
```

Create `Nebu.Room.InviteDB` module in `core/apps/room_manager/lib/nebu/room/invite_db.ex`:

```elixir
defmodule Nebu.Room.InviteDB do
  def insert_invitation(room_id, inviter_id, invitee_id) do
    now_ms = Nebu.DB.Helpers.now_ms()
    # Insert into room_invitations table via Ecto or raw SQL
    # Uses the same DB pool as Nebu.Room.DB
    ...
  end
end
```

**ALTERNATIVE — simpler MVP scope:** If implementing a full `Nebu.Room.InviteDB` is too broad for this story, the `invite_user/2` handler can store invitations in ETS (volatile, cleared on restart) for MVP and note that Story 4-10 is MVP-scoped. The `room_invitations` DB migration must still be created (for future use in sync). Use this approach only if the DB write path is blocked by missing DB infrastructure.

### Elixir Test File — `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` (CREATE NEW)

Mirror the structure of `create_room_test.exs` exactly:

- `async: false` — Horde + Application.put_env + ETS are process-global
- Same `FakeDB` module for `room_manager` DB injection
- Same `FakeInviteDB` for `invite_db_module` injection
- Setup: inject FakeDB via `Application.put_env(:room_manager, :db_module, FakeDB)` + start a room via `Nebu.Room.RoomSupervisor.start_room/1`
- Teardown: terminate all created rooms via `Horde.DynamicSupervisor.terminate_child`

---

## File Locations

### Create (new files):

```
gateway/internal/matrix/rooms_join_test.go        ← Go unit tests for join + invite handlers
gateway/migrations/000012_room_invitations.up.sql  ← room_invitations table
gateway/migrations/000012_room_invitations.down.sql
core/apps/room_manager/lib/nebu/room/invite_db.ex  ← InviteDB module (if DB path chosen)
core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs
```

**NOTE on test file naming:** The existing `rooms_test.go` already covers `CreateRoom`. Create a SEPARATE file `rooms_join_test.go` (same package `matrix`) for `JoinRoom` + `InviteUser` tests to avoid merge conflicts and keep test files focused. Both files live in `gateway/internal/matrix/`.

### Modify (existing files):

```
gateway/internal/matrix/rooms.go           ← Add JoinRoomHandler + InviteUserHandler (EXTEND, do not replace)
gateway/internal/grpc/client.go            ← Wire JoinRoom + add InviteUser
gateway/internal/grpc/client_test.go       ← Update JoinRoom stub test + add InviteUser test
gateway/cmd/gateway/main.go                ← Register 3 new routes
proto/core.proto                           ← Add InviteUser RPC + messages
gateway/internal/grpc/pb/                  ← Regenerated by make proto (do not edit manually)
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex  ← Implement join_room/2 + invite_user/2
```

### Do NOT touch:

```
gateway/internal/matrix/rooms.go (CreateRoomHandler section)  ← Do not modify existing CreateRoom code
proto/core.proto (JoinRoom, JoinRoomRequest, JoinRoomResponse)  ← Already correct, do not duplicate
core/apps/event_dispatcher/test/nebu/event_dispatcher/create_room_test.exs  ← Not affected
```

---

## Proto Contract

### Already Exists (DO NOT CHANGE):

```protobuf
// JoinRoom
message JoinRoomRequest {
  string user_id          = 1;
  string room_id_or_alias = 2;
}
message JoinRoomResponse {
  string room_id = 1;
}
```

The `JoinRoom` RPC is also already declared in `service CoreService`. Verify this before adding again.

### Must Add (MISSING from proto):

```protobuf
// InviteUser
message InviteUserRequest {
  string room_id    = 1;
  string inviter_id = 2;
  string invitee_id = 3;
}
message InviteUserResponse {}
```

Add `rpc InviteUser(InviteUserRequest) returns (InviteUserResponse);` to `service CoreService`.

**AFTER modifying proto: run `make proto`** — this regenerates both Go (`pb/core.pb.go`, `pb/core_grpc.pb.go`) and Elixir (`lib/pb/`) stubs. The `client.go` method `InviteUser` and `server.ex` function `invite_user/2` will compile against the new generated types.

---

## Existing Patterns to Follow

### Pattern: URL path parameters (established by Go 1.22+ mux)

```go
// In the handler, extract route wildcard:
roomID := r.PathValue("roomId")           // for {roomId}
roomIDOrAlias := r.PathValue("roomIdOrAlias")  // for {roomIdOrAlias}
```

This is the idiomatic Go 1.22+ approach already used in the codebase (see `main.go` route registration patterns). Do NOT use gorilla/mux or chi — they are not dependencies.

### Pattern: JWT context extraction (from `rooms.go` — Story 4-9)

```go
sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
userID := coregrpc.FormatUserID(sub, h.serverName)
grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
```

Reuse exactly — do NOT reinvent.

### Pattern: `writeMatrixError` (defined in `login.go`, used in `rooms.go`)

`writeMatrixError(w, statusCode, errcode, message)` is already in the `matrix` package. Do NOT redefine it.

### Pattern: gRPC error mapping

```go
st, _ := status.FromError(err)
switch st.Code() {
case codes.NotFound:
    writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
case codes.PermissionDenied:
    writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Not allowed to join this room")
default:
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
}
```

### Pattern: consumer-defined interface (established in `rooms.go`)

Define `JoinRoomCoreClient` with only `JoinRoom` method and `InviteUserCoreClient` with only `InviteUser` method. Do NOT use the full `*grpc.Client` type. This is the Go interface-minimality convention (CLAUDE.md).

### Pattern: Elixir room lookup (`get_room_state` in `server.ex` — Story 4-8)

The `get_room_state/2` handler uses `catch :exit, {:noproc, _}` to detect a missing room. For `join_room/2`, use `Nebu.Room.RoomSupervisor.lookup_room/1` instead — it returns `{:error, :not_found}` cleanly without requiring a try/catch. This is the preferred pattern from Story 4-9.

### Pattern: Elixir `db_module` injection (from `room_manager/server.ex`)

```elixir
defp db_module, do: Application.get_env(:room_manager, :db_module, Nebu.Room.DB)
```

Use the same `Application.get_env` injection pattern for `invite_db_module` in `event_dispatcher/server.ex`.

### Pattern: `room_registry_module` injection (from `server.ex` — Story 4-8)

```elixir
defp room_registry_module do
  Application.get_env(:event_dispatcher, :room_registry_module, Nebu.Room.Server)
end
```

For `join_room/2`, use `Nebu.Room.RoomSupervisor.lookup_room/1` directly — the supervisor is the correct entry point for room existence checks (not the registry module override). Only use `room_registry_module()` for `get_state`-style calls where test override is needed.

---

## Test Patterns

### Go unit test pattern (`rooms_join_test.go`)

Create a NEW file `rooms_join_test.go` in package `matrix`. Do NOT modify `rooms_test.go`.

Mock clients:

```go
type mockJoinRoomCoreClient struct {
    resp *pb.JoinRoomResponse
    err  error
    capturedReq *pb.JoinRoomRequest
}

func (m *mockJoinRoomCoreClient) JoinRoom(_ context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error) {
    m.capturedReq = req
    return m.resp, m.err
}

type mockInviteUserCoreClient struct {
    resp *pb.InviteUserResponse
    err  error
}

func (m *mockInviteUserCoreClient) InviteUser(_ context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
    return m.resp, m.err
}
```

Auth test helpers: reuse `setupOIDCServer` and `signJWT` from `login_test.go` (same package — directly accessible, no import needed).

gRPC error simulation:
```go
notFoundErr := status.Error(codes.NotFound, "room not found")
permDeniedErr := status.Error(codes.PermissionDenied, "not invited")
```

URL path parameters with Go mux: When using `httptest`, the `r.PathValue()` method only works when the request is routed through a `*http.ServeMux` that registered the pattern. For unit tests, register the handler on a test mux:

```go
testMux := http.NewServeMux()
testMux.Handle("POST /_matrix/client/v3/join/{roomIdOrAlias}",
    jwtMiddleware(http.HandlerFunc(handler.PostJoinRoom)))
req := httptest.NewRequest("POST", "/_matrix/client/v3/join/!abc:test.local", nil)
w := httptest.NewRecorder()
testMux.ServeHTTP(w, req)
```

This is the ONLY way `r.PathValue("roomIdOrAlias")` returns the correct value in tests. Direct `handler.ServeHTTP(w, req)` bypasses the mux and returns empty string from `PathValue`.

### Elixir unit test pattern (`join_room_test.exs`)

```elixir
defmodule Nebu.EventDispatcher.JoinRoomTest do
  use ExUnit.Case, async: false

  alias Nebu.EventDispatcher.Server

  # Same FakeDB as in create_room_test.exs — copy the module verbatim
  # (or extract to a shared test support file if preferred)
  defmodule FakeDB do
    # ... same as create_room_test.exs FakeDB
  end

  defmodule FakeInviteDB do
    def insert_invitation(room_id, inviter_id, invitee_id) do
      :ets.insert(:join_room_test_invitations, {room_id, inviter_id, invitee_id})
      :ok
    end
  end

  setup do
    :ets.new(:join_room_test_db, [:named_table, :public, :set])
    :ets.new(:join_room_test_invitations, [:named_table, :public, :bag])
    Application.put_env(:room_manager, :db_module, FakeDB)
    Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB)
    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      :ets.delete(:join_room_test_db)
      :ets.delete(:join_room_test_invitations)
    end)
    :ok
  end
  # ... tests
end
```

Use `Nebu.Room.RoomSupervisor.start_room/1` to create test rooms. Clean up with `Nebu.Room.RoomSupervisor.lookup_room/1` + `Horde.DynamicSupervisor.terminate_child`.

---

## Previous Story Intelligence (4-9)

From Story 4-9 dev notes and implementation:

- **`rooms.go` exists** — extend it, do NOT create a new file. The file already imports `coregrpc`, `pb`, `middleware`, `codes`, `status`, `proto`.
- **`generate_room_id/0`** is a private function in `server.ex` — not relevant for join, but the file structure is established.
- **`Nebu.Room.RoomSupervisor.start_room/1` returns `{:ok, pid}` for both new and already-running rooms.** For join, use `lookup_room/1` instead — it returns `{:error, :not_found}` if the room doesn't exist. This correctly maps to `GRPC.Status.not_found()`.
- **`Nebu.Room.Server.join/2` returns `{:error, :already_member}` if already joined.** The Matrix spec says join is idempotent — return success (not error) in this case.
- **`writeMatrixError` is in `login.go`** in the `matrix` package — accessible in `rooms.go` without import.
- **`client_test.go` TestStubsReturnNil**: The `JoinRoom` entry currently expects `nil, nil`. After wiring, update to expect a connection error (same as `CreateRoom` was updated in 4-9). ADD a new test entry for `InviteUser` also expecting a connection error.
- **`make proto` is required** for this story because `InviteUser` messages are new. In Story 4-9, proto was NOT regenerated (all stubs already existed). This story is different.
- **Server name for room IDs:** `server_name` is read from `Application.get_env(:event_dispatcher, :server_name, "nebu.local")` — same for `join_room` tests, inject `"test.local"` via `Application.put_env`.
- **`FakeDB` for create_room tests** is in `create_room_test.exs` — copy the pattern verbatim. Using `async: false` + ETS for isolation.
- **`GRPC.Status.not_found()`**, **`GRPC.Status.permission_denied()`**, **`GRPC.Status.internal()`** are the correct Elixir status helpers (not atoms, not integers).

---

## Architecture Compliance Checklist

- [ ] `JoinRoomHandler` and `InviteUserHandler` added to existing `gateway/internal/matrix/rooms.go` (same file as `CreateRoomHandler` per architecture spec)
- [ ] Consumer-defined interfaces `JoinRoomCoreClient` and `InviteUserCoreClient` — minimal, single-method each
- [ ] `r.PathValue()` used for URL path parameters (Go 1.22+ net/http mux)
- [ ] Test mux used in Go unit tests so `PathValue()` works correctly
- [ ] No `panic` in handler code (Go convention per CLAUDE.md)
- [ ] Context passed as first parameter in all functions
- [ ] `writeMatrixError` reused from `login.go`, NOT redefined
- [ ] `JWTMiddleware` wrapping all three new routes in `main.go`
- [ ] `make proto` run after proto changes
- [ ] Go stubs in `client.go` wired (not returning `nil, nil`)
- [ ] `client_test.go` updated for `JoinRoom` (connection error, not nil/nil) + new `InviteUser` entry
- [ ] Elixir: `raise GRPC.RPCError` for errors, NO `try/rescue` (let it crash principle)
- [ ] Elixir: `{:error, :already_member}` from `Room.Server.join/2` handled as SUCCESS (idempotency)
- [ ] DB migration `000012` created with correct sequential numbering
- [ ] `make test-unit-go` and `make test-unit-elixir` pass before done

---

## Tasks / Subtasks

- [x] Write failing tests FIRST (ATDD gate):
  - [x] Create `gateway/internal/matrix/rooms_join_test.go` with tests 1–6 (Go) — tests were added to `rooms_test.go` by ATDD gate
  - [x] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` with tests 7–10 (Elixir)
  - [x] Run tests — verify RED (failing)

- [x] Update proto + regenerate stubs (prerequisite for all implementation):
  - [x] Add `InviteUser` RPC + messages to `proto/core.proto`
  - [x] Run `make proto` — verify Go and Elixir pb files updated

- [x] Create DB migration:
  - [x] Create `gateway/migrations/000012_room_invitations.up.sql`
  - [x] Create `gateway/migrations/000012_room_invitations.down.sql`

- [x] Implement Go `JoinRoomHandler` (AC #1–5, #7):
  - [x] Add to `gateway/internal/matrix/rooms.go`
  - [x] Define `JoinRoomCoreClient` interface + `JoinRoomHandler` struct + `NewJoinRoomHandler` + `PostJoinRoom` + `PostJoinRoomById`

- [x] Implement Go `InviteUserHandler` (AC #6):
  - [x] Add to `gateway/internal/matrix/rooms.go`
  - [x] Define `InviteUserCoreClient` interface + `InviteUserHandler` struct + `NewInviteUserHandler` + `PostInviteUser`

- [x] Wire Go gRPC client (AC #8, #9):
  - [x] Modify `gateway/internal/grpc/client.go`: replace `JoinRoom` stub with `c.core.JoinRoom(ctx, req)`, add `InviteUser`
  - [x] Update `gateway/internal/grpc/client_test.go`: fix `JoinRoom` test case, add `InviteUser` test case

- [x] Register routes in main (AC routing):
  - [x] Modify `gateway/cmd/gateway/main.go`: add 3 routes for join + invite

- [x] Implement Elixir `join_room/2` (AC #10):
  - [x] Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
  - [x] Handle `{:error, :not_found}` from `lookup_room/1` → `GRPC.Status.not_found()`
  - [x] Handle `{:error, :already_member}` from `Room.Server.join/2` → idempotent success

- [x] Implement Elixir `invite_user/2` (AC #11):
  - [x] Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
  - [x] Create `core/apps/room_manager/lib/nebu/room/invite_db.ex`
  - [x] Add `db_module_invite/0` private helper to `server.ex`

- [x] Run all tests green (AC #14, #15):
  - [x] `make test-unit-go` — all gateway tests pass
  - [x] `make test-unit-elixir` — all umbrella tests pass

---

## Notes / Open Questions

1. **`PostJoinRoom` vs `PostJoinRoomById`:** The Matrix spec defines two separate endpoints that both join a room: `POST /join/{roomIdOrAlias}` (discovery join) and `POST /rooms/{roomId}/join` (accept invitation). Both call `JoinRoom` gRPC with the same logic. The difference is only in how the room ID is extracted from the URL path. Create two handler methods on `JoinRoomHandler` to handle both routes, or a single shared private method that both public methods call after extracting the room ID.

2. **Room alias resolution (MVP scope):** The Matrix spec allows `{roomIdOrAlias}` to be either a room ID (`!xxx:server`) or a room alias (`#alias:server`). In MVP, aliases are not registered in a separate table. The Elixir `join_room/2` handler should pass `room_id_or_alias` directly to `Nebu.Room.RoomSupervisor.lookup_room/1` — this only works for direct room IDs. Aliases will return `{:error, :not_found}` in MVP. Document this as a known limitation; alias resolution is a future story.

3. **`make proto` side effects:** Running `make proto` regenerates ALL generated files in `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/`. This is expected and correct. Do not manually edit any `_pb.go` or `.pb.ex` files.

4. **`InviteUser` in Elixir — MVP scope for broadcast:** The epics specify that `invite_user/2` should broadcast an `m.room.member` event to the invitee's sync stream. For MVP Story 4-10, this is out of scope — store the invitation record and return success. The sync broadcasting will be implemented in Story 4-14/4-15 (sync endpoint).

5. **`room_invitations` FK constraint on `users`:** The migration references `users(user_id)`. In tests, FakeDB does not write to the actual `users` table. Test the Elixir `invite_user/2` with FakeInviteDB (ETS-backed, no FK enforcement) to avoid PostgreSQL dependencies in unit tests.

6. **OpenAPI spec:** Same policy as Story 4-9 — do NOT add Matrix endpoints to `openapi.yaml`. The Matrix API uses direct `mux.Handle` registration.

---

## File List

### New files:
- `gateway/migrations/000012_room_invitations.up.sql`
- `gateway/migrations/000012_room_invitations.down.sql`
- `core/apps/room_manager/lib/nebu/room/invite_db.ex`

### Modified files:
- `gateway/internal/matrix/rooms.go` — added `JoinRoomHandler` + `InviteUserHandler` with all interfaces, structs, and handlers
- `gateway/internal/grpc/client.go` — wired `JoinRoom` to `c.core.JoinRoom`, added `InviteUser` method
- `gateway/internal/grpc/client_test.go` — updated `JoinRoom` stub test to expect connection error, added `InviteUser` connection error test
- `gateway/internal/grpc/stream_test.go` — added `InviteUser` stub to `mockCoreClient` to satisfy updated interface
- `gateway/cmd/gateway/main.go` — registered 3 new routes: `POST /join/{roomIdOrAlias}`, `POST /rooms/{roomId}/join`, `POST /rooms/{roomId}/invite`
- `proto/core.proto` — added `InviteUser` RPC, `InviteUserRequest`, `InviteUserResponse` messages
- `gateway/internal/grpc/pb/core.pb.go` — regenerated by `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — regenerated by `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — regenerated by `make proto`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — implemented `join_room/2` + `invite_user/2` + `db_module_invite/0`
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` — added `FakeInviteDB`, setup for invite ETS table, `invite_user/2` tests (AT #10)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — status updated to `review`

---

## Change Log

- 2026-04-03: Story 4-10 created — ready-for-dev.
- 2026-04-03: Story 4-10 implemented — all 15 ACs satisfied, all tests green (make test-unit-go + make test-unit-elixir pass). Status → review.

## Dev Agent Record

### Implementation Plan

Implemented in the following order:

1. **proto/core.proto** — added `InviteUser` RPC, `InviteUserRequest`, `InviteUserResponse`. Ran `make proto` to regenerate all Go and Elixir pb stubs.

2. **DB migrations** — created `000012_room_invitations.up.sql` (table + index) and `000012_room_invitations.down.sql`.

3. **gateway/internal/matrix/rooms.go** — extended existing file with:
   - `JoinRoomCoreClient` interface, `JoinRoomResponse`, `JoinRoomHandler`, `JoinRoomConfig`, `NewJoinRoomHandler`
   - `postJoinRoomWithID` shared private method (extracts user from JWT, calls gRPC, maps errors)
   - `PostJoinRoom` (extracts roomIdOrAlias from path) and `PostJoinRoomById` (extracts roomId from path)
   - `InviteUserCoreClient` interface, `InviteUserHandler`, `InviteUserConfig`, `NewInviteUserHandler`, `PostInviteUser`
   - Key: `AlreadyExists` gRPC status → 200 success (Matrix spec idempotency)

4. **gateway/internal/grpc/client.go** — replaced `JoinRoom` stub with `c.core.JoinRoom(ctx, req)`, added `InviteUser` method.

5. **gateway/internal/grpc/client_test.go** — updated `JoinRoom` test to expect connection error (not nil/nil), added `InviteUser` connection error test.

6. **gateway/internal/grpc/stream_test.go** — added `InviteUser` stub to `mockCoreClient` to satisfy the new `pb.CoreServiceClient` interface.

7. **gateway/cmd/gateway/main.go** — registered 3 new routes behind `jwtMiddleware`.

8. **core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex** — replaced stub `join_room/2` with full implementation using `RoomSupervisor.lookup_room/1` + `Room.Server.join/2`. Added full `invite_user/2` with member validation + `db_module_invite()` injection. Added `db_module_invite/0` private helper.

9. **core/apps/room_manager/lib/nebu/room/invite_db.ex** — created `Nebu.Room.InviteDB` with `insert_invitation/3` using raw SQL.

10. **join_room_test.exs** — added `FakeInviteDB` module, ETS table setup for invitations, updated setup/teardown, and added invite_user tests (AT #10: happy path, permission denied, room not found).

### Completion Notes

- All 15 Acceptance Criteria satisfied.
- `make test-unit-go`: all packages pass (no regressions, 7 new join/invite tests in rooms_test.go green).
- `make test-unit-elixir`: 48 event_dispatcher tests pass (join_room + invite_user tests included).
- Matrix idempotency correctly handled: `AlreadyExists` gRPC → 200 with room_id.
- Room alias resolution is MVP-scoped (passes room_id_or_alias directly to lookup_room; aliases not supported).
- MVP scope: invite_user stores to DB (PostgreSQL in production, FakeInviteDB in tests); no event broadcast yet (Story 4-14/4-15).

# Story 4.9: POST /createRoom — Matrix Room Creation Endpoint

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-9-post-createroom
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to create a new room via the Matrix API,
so that I have a space to invite others and exchange messages.

---

## Acceptance Criteria

1. `POST /_matrix/client/v3/createRoom` is protected by the JWT middleware (`JWTMiddleware`) — unauthenticated requests return `401 M_MISSING_TOKEN`.
2. Request body (JSON): `room_alias_name` (optional string), `name` (optional string), `topic` (optional string), `visibility` (`"public"` | `"private"`, default `"private"`), `invite` (optional `[]string` of user IDs), `preset` (`"private_chat"` | `"public_chat"` | `"trusted_private_chat"`, optional).
3. Handler calls `gRPC CoreService.CreateRoom` with `creator_id` (from JWT context), `name`, `topic`; Core starts a new `RoomServer` via `RoomSupervisor.start_room/1` and registers the room.
4. Returns `200 {"room_id": "!<random_opaque>:<server_name>"}` on success.
5. Returns `400 M_BAD_JSON` if the body cannot be decoded as valid JSON.
6. Returns `400 M_ROOM_IN_USE` if `room_alias_name` is already taken (Core returns `ALREADY_EXISTS` gRPC status).
7. Returns `403 M_FORBIDDEN` if the user is not allowed to create rooms (Core returns `PERMISSION_DENIED` gRPC status — all authenticated users can create rooms by default in MVP).
8. The gRPC `CreateRoom` stub in `gateway/internal/grpc/client.go` is wired up to call `c.core.CreateRoom(ctx, req)` (currently returns `nil, nil`).
9. The Elixir `create_room/2` handler in `Nebu.EventDispatcher.Server` is implemented: generates a `room_id`, calls `Nebu.Room.RoomSupervisor.start_room/1`, auto-joins the creator, and returns `%Core.CreateRoomResponse{room_id: room_id}`.
10. Unit tests (Go `httptest`): valid request → 200 with `room_id`; duplicate alias → 400 `M_ROOM_IN_USE`; missing auth → 401.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Happy path: authenticated user creates a room — Go unit test (httptest)**
- Given: a valid JWT is provided via `Authorization: Bearer <token>` and the mock core client returns `CreateRoomResponse{room_id: "!abc123:test.local"}`
- When: `POST /_matrix/client/v3/createRoom` with body `{"name": "Test Room"}`
- Then: response is `200` with body `{"room_id": "!abc123:test.local"}`

**2. Unauthenticated request — Go unit test (httptest)**
- Given: no `Authorization` header is provided
- When: `POST /_matrix/client/v3/createRoom` with body `{"name": "Test Room"}`
- Then: response is `401` with `{"errcode": "M_MISSING_TOKEN", ...}`

**3. Invalid JSON body — Go unit test (httptest)**
- Given: a valid JWT is provided
- When: `POST /_matrix/client/v3/createRoom` with body `{not valid json}`
- Then: response is `400` with `{"errcode": "M_BAD_JSON", ...}`

**4. Duplicate alias — Go unit test (httptest)**
- Given: a valid JWT and the mock core client returns a gRPC `ALREADY_EXISTS` error
- When: `POST /_matrix/client/v3/createRoom` with body `{"room_alias_name": "existing-room"}`
- Then: response is `400` with `{"errcode": "M_ROOM_IN_USE", ...}`

**5. Elixir: create_room generates room_id and starts RoomServer — ExUnit**
- Given: no Room GenServer is running for the new room_id
- When: `Nebu.EventDispatcher.Server.create_room(%Core.CreateRoomRequest{creator_id: "@alice:test.local", name: "My Room"}, stream)` is called
- Then: returns `%Core.CreateRoomResponse{room_id: room_id}` where `room_id` matches `~r/^![a-z0-9]+:test\.local$/` and the Room GenServer is running in Horde

**6. Elixir: creator is auto-joined to the new room — ExUnit**
- Given: `create_room/2` succeeds for `@alice:test.local`
- When: `Nebu.Room.Server.get_state(room_id)` is called
- Then: `state.members` contains `"@alice:test.local"`

---

## Technical Requirements

### Go Handler — `gateway/internal/matrix/rooms.go` (CREATE NEW)

Architecture rule: one Matrix feature group per file. The architecture document specifies `rooms.go` for `POST /createRoom` and `POST /join/{id}`.

**Handler struct:**

```go
type CreateRoomHandler struct {
    coreClient  CreateRoomCoreClient  // consumer-defined interface (small interface principle)
    serverName  string
}

// Consumer-defined interface — keep it minimal
type CreateRoomCoreClient interface {
    CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error)
}
```

**Request/Response types:**

```go
type CreateRoomRequest struct {
    RoomAliasName string   `json:"room_alias_name,omitempty"`
    Name          string   `json:"name,omitempty"`
    Topic         string   `json:"topic,omitempty"`
    Visibility    string   `json:"visibility,omitempty"`  // default "private"
    Invite        []string `json:"invite,omitempty"`
    Preset        string   `json:"preset,omitempty"`
}

type CreateRoomResponse struct {
    RoomID string `json:"room_id"`
}
```

**Handler logic (PostCreateRoom):**

1. Decode JSON body → `400 M_BAD_JSON` on error (use `writeMatrixError` from `login.go` in the same `matrix` package — already exported)
2. Extract `sub` from context: `r.Context().Value(middleware.ContextKeySub).(string)` — set by `JWTMiddleware`
3. Format user ID: `coregrpc.FormatUserID(sub, h.serverName)`
4. Attach gRPC metadata: `coregrpc.WithUserMetadata(r.Context(), userID, systemRole)` — `systemRole` from `r.Context().Value(middleware.ContextKeySystemRole).(string)`
5. Call `h.coreClient.CreateRoom(grpcCtx, &pb.CreateRoomRequest{CreatorId: userID, Name: req.Name, Topic: req.Topic})`
6. Map gRPC errors to Matrix errors:
   - `codes.AlreadyExists` → `400 M_ROOM_IN_USE`
   - `codes.PermissionDenied` → `403 M_FORBIDDEN`
   - Any other error → `500 M_UNKNOWN`
7. Return `200 {"room_id": resp.RoomId}`

**gRPC error code mapping — import required:**

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

st, _ := status.FromError(err)
switch st.Code() {
case codes.AlreadyExists:
    writeMatrixError(w, http.StatusBadRequest, "M_ROOM_IN_USE", "Room alias already in use")
case codes.PermissionDenied:
    writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to create rooms")
default:
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
}
```

### Go gRPC Client — `gateway/internal/grpc/client.go` (MODIFY)

The `CreateRoom` stub currently returns `nil, nil`. Wire it up:

```go
func (c *Client) CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
    return c.core.CreateRoom(ctx, req)
}
```

### Go Router — `gateway/cmd/gateway/main.go` (MODIFY)

Register the new handler behind `jwtMiddleware`:

```go
createRoomHandler := matrix.NewCreateRoomHandler(matrix.CreateRoomConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("POST /_matrix/client/v3/createRoom",
    jwtMiddleware(http.HandlerFunc(createRoomHandler.PostCreateRoom)))
```

Note: `jwtMiddleware` and `tokenStore` are already constructed before this point in `main.go`.

### Elixir gRPC Handler — `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (MODIFY)

Replace the existing `create_room/2` stub:

```elixir
def create_room(request, _stream) do
  room_id = generate_room_id()
  creator_id = request.creator_id

  case Nebu.Room.RoomSupervisor.start_room(room_id) do
    {:ok, _pid} ->
      # Auto-join the creator
      :ok = Nebu.Room.Server.join(room_id, creator_id)
      %Core.CreateRoomResponse{room_id: room_id}

    {:error, reason} ->
      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "Failed to start room: #{inspect(reason)}"
  end
end

defp generate_room_id do
  server_name = Application.get_env(:event_dispatcher, :server_name, "nebu.local")
  opaque = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
  "!#{opaque}:#{server_name}"
end
```

**Server name config:** The `server_name` is needed in Core to build the room ID. Read from `Application.get_env(:event_dispatcher, :server_name, "nebu.local")`. This env key should be set in `config/config.exs` or passed via Docker environment — check how other Elixir apps read it. Currently `Nebu.Node.Registration` reads `server_name` from the gateway's DB; for room ID generation, use `Application.get_env` with a default.

**Alias collision check:** MVP does not implement a room alias registry. The `M_ROOM_IN_USE` path is triggered when Core raises `GRPC.RPCError` with `status: GRPC.Status.already_exists()`. In MVP, since room IDs are random and unique, this error path is only tested via mock — no actual alias table exists yet.

---

## File Locations

### Create (new files):

```
gateway/internal/matrix/rooms.go              ← CreateRoomHandler, PostCreateRoom
gateway/internal/matrix/rooms_test.go         ← Go unit tests (httptest)
```

### Modify (existing files):

```
gateway/internal/grpc/client.go               ← Wire up CreateRoom stub
gateway/cmd/gateway/main.go                   ← Register POST /createRoom route
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex  ← Implement create_room/2
core/apps/event_dispatcher/test/nebu/event_dispatcher/server_test.exs  ← Elixir unit tests (create or modify)
```

### Do NOT touch:

```
proto/core.proto                ← CreateRoom RPC already exists, NO changes needed
gateway/internal/grpc/pb/       ← Already generated, NO regeneration needed
core/apps/event_dispatcher/lib/pb/  ← Already generated, NO changes needed
```

---

## Proto Contract — Already Exists

**CRITICAL:** `rpc CreateRoom(CreateRoomRequest) returns (CreateRoomResponse)` is ALREADY in `proto/core.proto`. Do NOT run `make proto` for this story — the stubs are already generated and present.

Current proto definition:

```protobuf
message CreateRoomRequest {
  string          creator_id = 1;
  optional string name       = 2;
  optional string topic      = 3;
  bool            is_direct  = 4;
}
message CreateRoomResponse {
  string room_id = 1;
}
```

The `is_direct` field is defined but not used in MVP (Story 4-9). Set it to `false` / leave at default. `room_alias_name`, `visibility`, `invite`, and `preset` from the HTTP request body are NOT forwarded to Core in MVP — only `name` and `topic` are included. This is intentional scope reduction; alias and invitation features are Story 4-10.

---

## Existing Patterns to Follow

### Pattern: JWT context extraction (established in `login.go`)

The `JWTMiddleware` populates context keys. Extract user identity in handler:

```go
sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
userID := coregrpc.FormatUserID(sub, h.serverName)
grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
```

### Pattern: `writeMatrixError` (defined in `login.go`)

`writeMatrixError(w, statusCode, errcode, message)` is in the `matrix` package and accessible from `rooms.go` without duplication — do NOT redefine it.

The middleware package also has its own `writeMatrixError`. Use the one from `login.go` (same package = `matrix`).

### Pattern: consumer-defined interface (established in `login.go`)

```go
type CoreClient interface {
    ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error)
}
```

Define `CreateRoomCoreClient` with only `CreateRoom` — not the full `*grpc.Client`. This keeps the interface minimal (Go convention, ADR-009 + CLAUDE.md).

### Pattern: handler construction (established in `login.go`)

```go
type CreateRoomConfig struct {
    CoreClient CreateRoomCoreClient
    ServerName string
}

func NewCreateRoomHandler(cfg CreateRoomConfig) *CreateRoomHandler {
    return &CreateRoomHandler{
        coreClient: cfg.CoreClient,
        serverName: cfg.ServerName,
    }
}
```

### Pattern: Elixir gRPC server stubs (established in `server.ex`)

The `Nebu.EventDispatcher.Server` currently has skeleton stubs returning empty structs. When implementing `create_room/2`, raise `GRPC.RPCError` for error cases — do NOT use defensive try/rescue (Elixir convention: let it crash).

### Pattern: Room GenServer interaction (Story 4-2)

`Nebu.Room.RoomSupervisor.start_room(room_id)` starts and registers the GenServer. Returns `{:ok, pid}` if new or already running. `Nebu.Room.Server.join(room_id, user_id)` adds the creator as a member.

---

## Test Patterns

### Go unit test pattern (`rooms_test.go`)

Mirror `login_test.go` structure:
- `mockCreateRoomCoreClient` struct implementing `CreateRoomCoreClient`
- Use `httptest.NewRecorder()` and `httptest.NewRequest()`
- Wire `JWTMiddleware` around the handler for auth tests (reuse `setupOIDCServer` / `signJWT` helpers from `login_test.go`)

**Mock client:**

```go
type mockCreateRoomCoreClient struct {
    resp *pb.CreateRoomResponse
    err  error
}

func (m *mockCreateRoomCoreClient) CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
    return m.resp, m.err
}
```

**gRPC error simulation (for M_ROOM_IN_USE test):**

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

alreadyExistsErr := status.Error(codes.AlreadyExists, "alias taken")
```

### Elixir unit test pattern

Test file: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_test.exs` (may already exist from Story 4-8). Add `create_room` tests as new `describe "create_room/2"` block.

Use the existing `db_module` injection pattern for testability:

```elixir
setup do
  Application.put_env(:event_dispatcher, :server_name, "test.local")
  on_exit(fn -> Application.delete_env(:event_dispatcher, :server_name) end)
end
```

Use `Nebu.Room.RoomSupervisor.lookup_room/1` to verify the Room GenServer started. Use `Nebu.Room.Server.get_state/1` to verify creator membership.

---

## Previous Story Intelligence (4-8)

From Story 4-8 dev notes:

- `Nebu.Room.Server.get_state/1` is the public API to inspect room state in tests.
- `Nebu.Room.RoomSupervisor.start_room/1` returns `{:ok, pid}` for both new and already-running rooms (idempotent).
- The `room_registry_module` injection pattern in `server.ex` is used for test isolation: `Application.put_env(:event_dispatcher, :room_registry_module, FakeModule)`.
- `GRPC.Status.not_found()` / `GRPC.Status.internal()` / `GRPC.Status.already_exists()` are the correct status helpers for `GRPC.RPCError`.
- Go: `grpclib.ServerStreamingClient[pb.Event]` is the type for streaming clients — not relevant for unary, but note the import alias `grpclib "google.golang.org/grpc"` used in `client.go`.

---

## Architecture Compliance Checklist

- [ ] New file `rooms.go` in `gateway/internal/matrix/` (matches architecture directory spec: `rooms.go ← POST /createRoom, POST /join/{id}`)
- [ ] Consumer-defined interface `CreateRoomCoreClient` with only `CreateRoom` method
- [ ] No `panic` in handler code (Go convention per CLAUDE.md)
- [ ] Context passed as first parameter in all functions
- [ ] `writeMatrixError` reused from `login.go`, not redefined
- [ ] `JWTMiddleware` wrapping the new route in `main.go`
- [ ] Elixir: `raise GRPC.RPCError` for errors, no `try/rescue`
- [ ] Room ID format: `!{8-byte-hex}:{server_name}` using `:crypto.strong_rand_bytes/1`
- [ ] Creator is auto-joined as first member (AC #9)
- [ ] `make test-unit-go` and `make test-unit-elixir` must pass before done

---

## Tasks / Subtasks

- [x] Write failing tests FIRST (ATDD gate):
  - [x] Create `gateway/internal/matrix/rooms_test.go` with tests 1–4 (Go)
  - [x] Add `describe "create_room/2"` block to Elixir server tests (tests 5–6)
  - [x] Run tests — verify RED (failing)

- [x] Implement Go `CreateRoomHandler` (AC #1–7):
  - [x] Create `gateway/internal/matrix/rooms.go`
  - [x] Define `CreateRoomCoreClient` interface
  - [x] Implement `PostCreateRoom` with JSON decode, JWT context extraction, gRPC call, error mapping

- [x] Wire Go gRPC client (AC #8):
  - [x] Modify `gateway/internal/grpc/client.go`: replace stub `CreateRoom` with `c.core.CreateRoom(ctx, req)`

- [x] Register route in main (AC routing):
  - [x] Modify `gateway/cmd/gateway/main.go`: add `POST /_matrix/client/v3/createRoom` with `jwtMiddleware`

- [x] Implement Elixir `create_room/2` (AC #9):
  - [x] Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
  - [x] Add `generate_room_id/0` private helper using `:crypto.strong_rand_bytes`
  - [x] Call `Nebu.Room.RoomSupervisor.start_room/1` and `Nebu.Room.Server.join/2`

- [x] Run all tests green (AC #10):
  - [x] `make test-unit-go` — all gateway tests pass
  - [x] `make test-unit-elixir` — all umbrella tests pass

---

## Notes / Open Questions

1. **`server_name` in Elixir:** The room ID needs `server_name`. Check if `Application.get_env(:event_dispatcher, :server_name, "nebu.local")` is already set in the Docker compose environment for the `core` service. If not, add it to `config/runtime.exs` reading from `System.get_env("NEBU_SERVER_NAME", "nebu.local")`. This is consistent with how Go reads `NEBU_SERVER_NAME`.

2. **`room_alias_name` scope:** The HTTP request body accepts `room_alias_name` but MVP does NOT implement alias registration in Core. Parse it in Go but do not forward it to Core via gRPC in Story 4-9. The `M_ROOM_IN_USE` error in AC #6 is only reachable if Core raises `ALREADY_EXISTS` — which in MVP only happens via mock. This is documented in the epics as acceptable scope reduction.

3. **OpenAPI spec (`gateway/api/openapi.yaml`):** The spec currently only covers the Admin API health endpoint. Per ADR-009 (spec-first), the Matrix API should ideally be in the spec too. However, given that `oapi-codegen` is set up for the Admin API path and the Matrix API is implemented via direct handler registration in `main.go`, **do NOT add Matrix endpoints to `openapi.yaml` in this story** — that would require significant refactoring of the router architecture. The existing pattern (direct `mux.Handle` registration) is consistent with all prior Matrix endpoint stories.

---

## File List

### New files:
- `gateway/internal/matrix/rooms.go`

### Modified files:
- `gateway/internal/grpc/client.go` — wired `CreateRoom` stub to `c.core.CreateRoom(ctx, req)`
- `gateway/internal/grpc/client_test.go` — updated `TestStubsReturnNil/CreateRoom` to expect connection error (now wired)
- `gateway/cmd/gateway/main.go` — registered `POST /_matrix/client/v3/createRoom` with `jwtMiddleware`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — implemented `create_room/2` and `generate_room_id/0`
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — updated `4-9-post-createroom` to `review`

### Pre-existing (tests written before implementation, not modified):
- `gateway/internal/matrix/rooms_test.go`
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/create_room_test.exs`

---

## Dev Agent Record

### Implementation Plan

Followed the red-green-refactor cycle against the pre-written failing tests:

1. **Go handler** (`rooms.go`): Implemented `CreateRoomCoreClient` consumer-defined interface, `CreateRoomHandler` struct, `NewCreateRoomHandler` constructor, and `PostCreateRoom` method. Used `proto.String()` to handle `optional string` proto fields for `Name` and `Topic`. Reused `writeMatrixError` from `login.go`. Mapped `codes.AlreadyExists` → `400 M_ROOM_IN_USE`, `codes.PermissionDenied` → `403 M_FORBIDDEN`, everything else → `500 M_UNKNOWN`.

2. **gRPC client** (`client.go`): Replaced `return nil, nil` stub with `return c.core.CreateRoom(ctx, req)`. Updated `TestStubsReturnNil` to expect connection error for `CreateRoom` (consistent with `ValidateToken` and `EventBus` patterns).

3. **Route registration** (`main.go`): Added `CreateRoomHandler` construction using existing `coreClient` and `serverName` variables, registered under `POST /_matrix/client/v3/createRoom` behind `jwtMiddleware`.

4. **Elixir handler** (`server.ex`): Replaced stub `create_room/2` with full implementation: generates `room_id` using `generate_room_id/0` private helper (`!{16-hex-chars}:{server_name}`), calls `Nebu.Room.RoomSupervisor.start_room/1`, auto-joins creator via `Nebu.Room.Server.join/2`, returns `%Core.CreateRoomResponse{room_id: room_id}`. Uses `GRPC.Status.internal()` on supervisor failure.

### Completion Notes

- All 6 Go tests pass (`make test-unit-go` green).
- All 9 new Elixir tests pass in `create_room_test.exs` plus all 31 existing Elixir tests (`make test-unit-elixir` green, 40 event_dispatcher tests total).
- `server_name` is read from `Application.get_env(:event_dispatcher, :server_name, "nebu.local")` — injectable in tests via `Application.put_env`.
- `room_alias_name` is parsed in Go request body but not forwarded to Core in MVP (AC scope documented in story).
- No new dependencies introduced.

---

## Change Log

- 2026-04-03: Story 4-9 implemented — `POST /_matrix/client/v3/createRoom` handler, gRPC wire-up, Elixir create_room/2. All tests green.

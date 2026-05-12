# Story 4.8: gRPC EventBus Server-Streaming + GetRoomState Unary

Status: review

## Story

As a gateway developer,
I want the Core to expose a fully functional gRPC `EventBus` server-streaming RPC and a `GetRoomState` unary RPC,
so that the gateway can receive real-time room events and query current room state without polling.

## Acceptance Criteria

1. `proto/core.proto` adds two new RPCs to `CoreService`:
   - `rpc GetRoomState(GetRoomStateRequest) returns (GetRoomStateResponse)`
   - The existing `rpc EventBus(EventBusRequest) returns (stream Event)` is already in the proto — no change to proto needed for EventBus itself
   - New messages: `GetRoomStateRequest { string room_id = 1; }` and `GetRoomStateResponse { repeated string members = 1; string power_levels_json = 2; string room_name = 3; }`
   - `make proto` regenerates Go stubs in `gateway/internal/grpc/pb/` and Elixir stubs in `core/apps/event_dispatcher/lib/pb/` without errors

2. Elixir: `Nebu.EventDispatcher.Server.event_bus/2` is fully implemented:
   - Subscribes to ALL `:pg` groups matching pattern `"room:*"` by joining each room's group for all rooms known via `Nebu.Room.Registry`
   - On receiving `{:new_event, event_map}` from a `:pg` group, converts the event map to `%Core.Event{}` and sends it on the stream via `GRPC.Server.send_reply(stream, %Core.Event{...})`
   - The `EventBusRequest.node_id` identifies the Go instance; logged at start
   - On stream disconnect or process termination: all `:pg` group memberships are cleaned up (no crash, no leak)
   - Process traps exits so cleanup runs even on abnormal termination

3. Elixir: `Nebu.EventDispatcher.Server.get_room_state/2` is implemented as a unary handler:
   - Looks up the Room GenServer via `Nebu.Room.Server.get_state(room_id)`
   - Returns `%Core.GetRoomStateResponse{members: MapSet.to_list(state.members), power_levels_json: "{}", room_name: ""}`
   - If no room found (GenServer not started): raises `GRPC.RPCError` with status `NOT_FOUND`

4. Go: `gateway/internal/grpc/stream.go` is created:
   - `EventBusStream` struct manages one persistent gRPC server-streaming connection to Elixir Core
   - Opens the `EventBus` stream on startup via `c.core.EventBus(ctx, &pb.EventBusRequest{NodeId: nodeID})`
   - Receives `*pb.Event` messages and forwards them to a channel for downstream consumers (e.g., message_buffer, Story 4-16)
   - On disconnect (Recv returns error): retries with exponential backoff starting at 1s, doubling up to max 30s
   - Logs state transitions: `slog.Info("EventBus stream connected")`, `slog.Warn("EventBus stream lost, retrying", "backoff_ms", ...)`

5. Go: `gateway/internal/grpc/client.go` updated:
   - `EventBus` method is now a real implementation calling `c.core.EventBus(ctx, req)`
   - `GetRoomState` method added to `Client` struct, calling `c.core.GetRoomState(ctx, req)`
   - `GetRoomState` stub updated to call through to `c.core.GetRoomState(ctx, req)`

6. `make proto` runs cleanly; `make test-unit-go` and `make test-unit-elixir` both pass.

7. Unit tests:
   - Elixir: `core/apps/event_dispatcher/test/nebu/event_dispatcher/event_bus_test.exs` covers:
     - `event_bus/2` sends a `%Core.Event{}` on the stream when `{:new_event, event_map}` is received
     - `get_room_state/2` returns correct members list for an existing room
     - `get_room_state/2` raises `NOT_FOUND` for non-existent room
     - Stream cleanup: after process exit, `:pg` membership is removed
   - Go: `gateway/internal/grpc/stream_test.go` covers:
     - Reconnect logic: simulate stream error, verify backoff and reconnect attempt
     - Event forwarding: mock stream returns an event, verify it appears on the output channel

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. event_bus: sends Core.Event on stream when :pg broadcasts {new_event} — ExUnit**
- Given: a mock `GRPC.Server.Stream.t()` is set up, the process joins `:pg` group `"room:!abc:nebu.local"` and the event_bus handler is running
- When: `send(event_bus_pid, {:new_event, %{"room_id" => "!abc:nebu.local", "event_id" => "$ev1", "type" => "m.room.message", "sender" => "@kai:nebu.local", "content" => %{}, "origin_server_ts" => 1700000000000}})` is called
- Then: `GRPC.Server.send_reply/2` is called with a `%Core.Event{}` containing matching fields

**2. event_bus: pg cleanup on stream handler process exit — ExUnit**
- Given: `event_bus/2` is running in a test process registered with `:pg` in group `"room:!abc:nebu.local"`
- When: the handler process is `Process.exit(pid, :kill)`
- Then: `:pg.get_members("room:!abc:nebu.local")` no longer contains the dead PID (cleanup via process_flag :trap_exit or monitor)

**3. get_room_state: returns members for existing room — ExUnit**
- Given: `Nebu.Room.Server` is started for `"!room1:nebu.local"` and `@kai:nebu.local` has joined
- When: `Server.get_room_state(%Core.GetRoomStateRequest{room_id: "!room1:nebu.local"}, stream)` is called
- Then: returns `%Core.GetRoomStateResponse{members: ["@kai:nebu.local"], power_levels_json: "{}", room_name: ""}`

**4. get_room_state: raises NOT_FOUND for non-existent room — ExUnit**
- Given: no Room GenServer is running for `"!ghost:nebu.local"`
- When: `Server.get_room_state(%Core.GetRoomStateRequest{room_id: "!ghost:nebu.local"}, stream)` is called
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.not_found()`

**5. Go EventBusStream: reconnects after stream error — Go unit test (httptest + mock)**
- Given: a mock `CoreServiceClient` that returns an error on first `EventBus` call, then succeeds on second
- When: `EventBusStream.Start(ctx)` is called
- Then: after the backoff interval, a second `EventBus` call is made (reconnect attempt)

**6. Go EventBusStream: forwards received event to output channel — Go unit test**
- Given: a mock stream that returns one `*pb.Event` then EOF
- When: `EventBusStream.Start(ctx)` is running
- Then: the event appears on the `Events()` channel within 1 second

---

## Tasks / Subtasks

- [x] Write failing tests FIRST (ATDD gate):
  - [x] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/event_bus_test.exs` with tests 1–4
  - [x] Create `gateway/internal/grpc/stream_test.go` with tests 5–6
  - [x] Run tests — verify RED (failing)

- [x] Update proto (AC #1):
  - [x] Add `GetRoomState`, `GetRoomStateRequest`, `GetRoomStateResponse` to `proto/core.proto`
  - [x] Run `make proto` — verify stubs regenerated in `gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/`
  - [x] Verify `make test-unit-go` and `make test-unit-elixir` still compile (stubs present)

- [x] Implement Elixir `event_bus/2` handler (AC #2):
  - [x] Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`
  - [x] Subscribe to `:pg` groups for all active rooms on stream open
  - [x] Implement `handle_info({:new_event, event_map}, ...)` → `GRPC.Server.send_reply(stream, %Core.Event{...})`
  - [x] Implement cleanup on process exit (trap exits or monitor-based)
  - [x] Log `node_id` at stream open

- [x] Implement Elixir `get_room_state/2` handler (AC #3):
  - [x] Add `get_room_state/2` to `Nebu.EventDispatcher.Server`
  - [x] Call `Nebu.Room.Server.get_state(room_id)` (via via-tuple)
  - [x] Map state to `%Core.GetRoomStateResponse{}`
  - [x] Raise `NOT_FOUND` if room GenServer is not registered

- [x] Create `gateway/internal/grpc/stream.go` (AC #4):
  - [x] `EventBusStream` struct with `Start(ctx) error` and `Events() <-chan *pb.Event`
  - [x] Exponential backoff: start 1s, double, cap 30s
  - [x] Structured logging of state transitions

- [x] Update `gateway/internal/grpc/client.go` (AC #5):
  - [x] Implement `EventBus` method (was stub returning nil)
  - [x] Add `GetRoomState` method calling `c.core.GetRoomState(ctx, req)`

- [x] Run all tests to green (AC #6–7):
  - [x] `make test-unit-elixir` — full umbrella 0 failures 0 warnings
  - [x] `make test-unit-go` — all gateway tests pass

---

## Dev Notes

### CRITICAL: Proto Contract Analysis — What Already Exists vs. What is Missing

**ALREADY in proto and generated stubs:**
- `rpc EventBus(EventBusRequest) returns (stream Event)` — exists in `proto/core.proto`
- `EventBusRequest { node_id, since_token }` — exists
- `message Event { event_id, room_id, sender_id, event_type, content, origin_ts, server_ts }` — exists
- Elixir stub: `rpc :EventBus, Core.EventBusRequest, stream(Core.Event)` in `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex`
- Go generated client: `EventBus(ctx, *EventBusRequest) (grpc.ServerStreamingClient[Event], error)` in `core_grpc.pb.go`

**MISSING — must be added:**
- `rpc GetRoomState(GetRoomStateRequest) returns (GetRoomStateResponse)` — NOT in proto
- `GetRoomStateRequest { string room_id = 1; }` — NOT in proto
- `GetRoomStateResponse { repeated string members = 1; string power_levels_json = 2; string room_name = 3; }` — NOT in proto

**Note on epics.md discrepancy:** epics.md lists `EventBusRequest` with `gateway_id` and `room_ids` fields and a separate `EventEnvelope` message. The actual proto uses `node_id` (not `gateway_id`) and `Event` (not `EventEnvelope`). **Use the existing proto contract** (`node_id`, `Event`). Do NOT rename or add fields to `EventBusRequest` or create `EventEnvelope`. The existing generated stubs define the contract — proto additions should only add `GetRoomState`.

### File Locations

**Elixir (modify/create):**
```
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex    ← MODIFY (implement event_bus, add get_room_state)
core/apps/event_dispatcher/lib/pb/core.pb.ex                       ← REGENERATED by make proto (add GetRoomState messages)
core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex                  ← REGENERATED by make proto (add GetRoomState rpc)
core/apps/event_dispatcher/test/nebu/event_dispatcher/event_bus_test.exs  ← CREATE (new test file)
```

**Go (modify/create):**
```
gateway/internal/grpc/stream.go          ← CREATE (EventBusStream with backoff)
gateway/internal/grpc/stream_test.go     ← CREATE (reconnect + forwarding tests)
gateway/internal/grpc/client.go          ← MODIFY (implement EventBus, add GetRoomState)
gateway/internal/grpc/pb/core.pb.go      ← REGENERATED by make proto
gateway/internal/grpc/pb/core_grpc.pb.go ← REGENERATED by make proto
```

**Proto:**
```
proto/core.proto                          ← MODIFY (add GetRoomState RPC + messages)
```

**Do NOT create:**
- `core/apps/event_dispatcher/lib/nebu/event/bus.ex` — the architecture mentions this but the actual work is in `server.ex` (the grpc handler). Keep bus logic inline in `server.ex` for MVP.
- Any new Elixir application — event_dispatcher app exists and is already running.
- Any new mix.exs dependencies — grpc `~> 0.8` and `jason ~> 1.4` are already in event_dispatcher.

### Elixir: grpc-elixir 0.11.5 API for Server-Streaming

**Key API (from feedback memory and `core/deps/grpc/lib/grpc/server.ex`):**

For server-streaming RPCs, the handler:
1. Receives `(request, stream)` where `stream` is `GRPC.Server.Stream.t()`
2. Calls `GRPC.Server.send_reply(stream, %Core.Event{...})` for each event
3. Returns `{:ok, stream}` or keeps the process alive via `receive` loop

The `event_bus/2` handler must NOT return immediately. It must block (via `receive` loop) to keep the stream open:

```elixir
def event_bus(request, stream) do
  Logger.info("EventBus stream opened", node_id: request.node_id)
  Process.flag(:trap_exit, true)

  # Subscribe to all active room :pg groups
  subscribe_to_all_rooms()

  # Block and forward events until stream closes
  event_bus_loop(stream)
end

defp event_bus_loop(stream) do
  receive do
    {:new_event, event_map} ->
      event = map_to_proto_event(event_map)
      GRPC.Server.send_reply(stream, event)
      event_bus_loop(stream)

    {:EXIT, _pid, _reason} ->
      # Stream closed by client — clean up and exit
      leave_all_rooms()
      {:ok, stream}
  end
end
```

**`send_reply` signature:** `GRPC.Server.send_reply(stream :: GRPC.Server.Stream.t(), reply :: struct()) :: GRPC.Server.Stream.t()`

**Important:** `send_reply` returns the updated stream. For multiple sends, use the returned stream:
```elixir
stream = GRPC.Server.send_reply(stream, event1)
stream = GRPC.Server.send_reply(stream, event2)
```
In a loop, threading the stream through is correct but for simplicity, since the stream struct is immutable in grpc-elixir 0.8+, both patterns work. Thread for correctness.

### Elixir: :pg group subscription pattern for EventBus

The Room GenServer (`Nebu.Room.Server`) joins group `"room:#{room_id}"` via `:pg.join("room:#{room_id}", self())` in `init/1`. It broadcasts `{:new_event, signed_event}` via `:pg.get_local_members("room:#{room_id}")` + `Enum.each(&send(...))`.

The EventBus handler must join the same groups to receive these broadcasts:

```elixir
defp subscribe_to_all_rooms do
  # Get all rooms from Horde Registry
  rooms = Horde.Registry.select(Nebu.Room.Registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
  Enum.each(rooms, fn room_id ->
    :pg.join("room:#{room_id}", self())
  end)
end
```

**Note:** `Horde.Registry.select/2` uses match specs like `:ets.select/2`. The pattern above selects all keys. Adjust if the registry stores `{room_id, pid}` tuples — check actual Horde.Registry API.

**Alternative (simpler for MVP):** Subscribe to rooms as they're encountered. Since `event_bus` runs for one Go instance, and rooms may be created after the stream opens, a subscription refresh on each `{:new_event, ...}` miss is also valid. However, the preferred approach is to subscribe to all current rooms at open, and also handle a `{:new_room, room_id}` message if room creation broadcasts such events. For MVP, subscribing at open only is sufficient.

**Cleanup on exit:**
```elixir
defp leave_all_rooms do
  # :pg automatically removes dead processes from groups.
  # Explicit leave is cleaner but :pg handles it automatically on process exit.
  # Use Process.flag(:trap_exit, true) so the receive loop catches {:EXIT, _, _}
  # and can log/cleanup before exiting.
  :ok
end
```

`:pg` automatically cleans up dead processes — explicit `leave` is not strictly required but is good practice.

### Elixir: get_room_state/2 Handler

`Nebu.Room.Server.get_state/1` returns the full state map:
```elixir
%{
  room_id: "!abc:nebu.local",
  members: %MapSet{} |> MapSet.put("@kai:nebu.local"),
  power_levels: %{},
  created_at: %DateTime{}
}
```

The handler implementation:
```elixir
def get_room_state(request, _stream) do
  room_id = request.room_id

  try do
    state = Nebu.Room.Server.get_state(room_id)
    %Core.GetRoomStateResponse{
      members: MapSet.to_list(state.members),
      power_levels_json: "{}",
      room_name: ""
    }
  catch
    :exit, {:noproc, _} ->
      raise GRPC.RPCError,
        status: GRPC.Status.not_found(),
        message: "room not found: #{room_id}"
  end
end
```

`Nebu.Room.Server.get_state/1` calls `GenServer.call(via(room_id), :get_state)`. If the GenServer is not registered, `GenServer.call` raises `{:noproc, ...}` as an exit. Catch it with `try/catch :exit`.

**Alternative:** Use `Horde.Registry.lookup/2` first to check if the room exists, then call `get_state`. This avoids relying on exception flow for control logic:
```elixir
case Horde.Registry.lookup(Nebu.Room.Registry, room_id) do
  [{_pid, _}] ->
    state = Nebu.Room.Server.get_state(room_id)
    %Core.GetRoomStateResponse{...}
  [] ->
    raise GRPC.RPCError, status: GRPC.Status.not_found(), message: "room not found: #{room_id}"
end
```
Prefer the `Horde.Registry.lookup` approach — it avoids exception-driven control flow.

### Elixir: Map event_map to %Core.Event{}

The `signed_event` broadcast by `Nebu.Room.Server` has string keys:
```elixir
%{
  "room_id" => "!abc:nebu.local",
  "event_id" => "$hash...",
  "type" => "m.room.message",
  "sender" => "@kai:nebu.local",
  "content" => %{"msgtype" => "m.text", "body" => "hello"},
  "origin_server_ts" => 1700000000000,
  "signatures" => %{"nebu" => "base64sig..."}
}
```

Mapping to `%Core.Event{}`:
```elixir
defp map_to_proto_event(event_map) do
  content_json = Jason.encode!(Map.get(event_map, "content", %{}))
  %Core.Event{
    event_id:   Map.get(event_map, "event_id", ""),
    room_id:    Map.get(event_map, "room_id", ""),
    sender_id:  Map.get(event_map, "sender", ""),
    event_type: Map.get(event_map, "type", ""),
    content:    content_json,        # Core.Event.content is bytes — Jason-encoded JSON
    origin_ts:  Map.get(event_map, "origin_server_ts", 0),
    server_ts:  System.system_time(:millisecond)
  }
end
```

**Note:** `Core.Event.content` is `bytes` in proto. In Elixir, pass a binary string (Jason-encoded JSON). grpc-elixir encodes `:bytes` fields as-is.

### Go: EventBusStream implementation

**`gateway/internal/grpc/stream.go`** — new file:

```go
package grpc

import (
    "context"
    "log/slog"
    "time"

    grpclib "google.golang.org/grpc"
    pb "github.com/nebu/nebu/internal/grpc/pb"
)

const (
    eventBusInitialBackoff = 1 * time.Second
    eventBusMaxBackoff     = 30 * time.Second
)

// EventBusStream manages a persistent gRPC server-streaming connection for EventBus.
// One stream per Go gateway instance (architecture decision: ADR-005).
type EventBusStream struct {
    client  pb.CoreServiceClient
    nodeID  string
    events  chan *pb.Event
}

// NewEventBusStream creates an EventBusStream.
// Call Start() to begin consuming.
func NewEventBusStream(client pb.CoreServiceClient, nodeID string) *EventBusStream {
    return &EventBusStream{
        client: client,
        nodeID: nodeID,
        events: make(chan *pb.Event, 256), // buffered to absorb burst
    }
}

// Events returns the read-only channel of received events.
func (s *EventBusStream) Events() <-chan *pb.Event {
    return s.events
}

// Start opens the EventBus stream and runs the receive loop with reconnect.
// Blocks until ctx is cancelled.
func (s *EventBusStream) Start(ctx context.Context) {
    backoff := eventBusInitialBackoff
    for {
        if err := ctx.Err(); err != nil {
            return
        }
        err := s.runOnce(ctx)
        if err == nil || ctx.Err() != nil {
            return
        }
        slog.Warn("EventBus stream lost, retrying",
            "node_id", s.nodeID,
            "backoff_ms", backoff.Milliseconds(),
            "err", err)
        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            return
        }
        backoff = min(backoff*2, eventBusMaxBackoff)
    }
}

func (s *EventBusStream) runOnce(ctx context.Context) error {
    stream, err := s.client.EventBus(ctx, &pb.EventBusRequest{NodeId: s.nodeID})
    if err != nil {
        return err
    }
    slog.Info("EventBus stream connected", "node_id", s.nodeID)
    // Reset backoff on successful connect — caller resets by recreating stream.
    // backoff reset is handled by Start() which is called with a fresh backoff after success.
    for {
        event, err := stream.Recv()
        if err != nil {
            return err
        }
        select {
        case s.events <- event:
        case <-ctx.Done():
            return nil
        default:
            slog.Warn("EventBus events channel full, dropping event",
                "room_id", event.RoomId,
                "event_id", event.EventId)
        }
    }
}
```

**Backoff reset:** After a successful `runOnce` (which only returns on error), the next iteration of the backoff loop starts fresh at `eventBusInitialBackoff`. Reset by resetting the `backoff` variable after `runOnce` returns without error. Actually, since `runOnce` returns nil only on `ctx.Err() != nil`, the backoff doesn't need to reset. For proper reset-on-success: reset `backoff = eventBusInitialBackoff` after `runOnce` returns without error in the loop.

### Go: Client.go updates

**Existing stub (replace):**
```go
// Before (stub):
func (c *Client) EventBus(ctx context.Context, req *pb.EventBusRequest) (grpclib.ServerStreamingClient[pb.Event], error) {
    return nil, nil
}
```

**After (real implementation):**
```go
func (c *Client) EventBus(ctx context.Context, req *pb.EventBusRequest) (grpclib.ServerStreamingClient[pb.Event], error) {
    return c.core.EventBus(ctx, req)
}
```

**New method (after proto regeneration):**
```go
func (c *Client) GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
    return c.core.GetRoomState(ctx, req)
}
```

### Go: stream_test.go mock patterns

Use interface mocking for `CoreServiceClient`. The generated `CoreServiceClient` is an interface — create a test double:

```go
type mockCoreServiceClient struct {
    pb.CoreServiceClient // embed to satisfy interface
    eventBusFn func(ctx context.Context, req *pb.EventBusRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.Event], error)
}

func (m *mockCoreServiceClient) EventBus(ctx context.Context, req *pb.EventBusRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.Event], error) {
    return m.eventBusFn(ctx, req, opts...)
}
```

Use `testing.T` + channels to synchronize assertions.

### Proto: additions only

Add to the end of `proto/core.proto` (after `GetMetrics` messages):

```protobuf
// GetRoomState — unary: Go queries current room members + metadata
message GetRoomStateRequest {
  string room_id = 1;
}
message GetRoomStateResponse {
  repeated string members       = 1;
  string          power_levels_json = 2;  // JSON string — full power levels in Story 4-13
  string          room_name         = 3;  // empty for now — full room metadata in Story 4-9
}
```

And add to `service CoreService` block:
```protobuf
rpc GetRoomState(GetRoomStateRequest) returns (GetRoomStateResponse);
```

**IMPORTANT:** Do NOT change `EventBusRequest` to add `gateway_id` or `room_ids`. The epics.md spec diverges from the existing proto. The proto as-is (`node_id`, `since_token`) is the ground truth. The `EventBus` RPC signature is already correct and already has generated stubs in Go and Elixir.

### Elixir: Test isolation for event_bus tests

The `event_bus/2` handler is a blocking receive loop. Testing it requires the handler to run in a spawned process:

```elixir
defmodule Nebu.EventDispatcher.EventBusTest do
  use ExUnit.Case, async: false  # async: false — :pg is process-global

  setup do
    # Ensure :pg scope is started
    case :pg.start_link() do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end
    :ok
  end

  test "event_bus: forwards :new_event to stream" do
    room_id = "!test_eb:nebu.local"
    parent = self()

    # Mock stream that captures send_reply calls
    # grpc-elixir GRPC.Server.Stream is a struct — use a simple mock
    stream = %{__struct__: MockStream, parent: parent}

    # Patch GRPC.Server.send_reply to send to parent test process
    # Use Application.put_env or a mock module pattern

    # Spawn the event_bus handler
    pid = spawn(fn ->
      Nebu.EventDispatcher.Server.event_bus(
        %Core.EventBusRequest{node_id: "test-node"},
        stream
      )
    end)

    # Let handler initialize
    Process.sleep(50)

    # Join the room group and broadcast
    :pg.join("room:#{room_id}", self())
    send(pid, {:new_event, %{
      "room_id" => room_id,
      "event_id" => "$test_event",
      "type" => "m.room.message",
      "sender" => "@kai:nebu.local",
      "content" => %{},
      "origin_server_ts" => 1_700_000_000_000
    }})

    assert_receive {:sent_event, %Core.Event{event_id: "$test_event"}}, 1_000
  end
end
```

**Testing strategy for streaming:** Since the real `GRPC.Server.send_reply` requires a live gRPC connection (not easily mockable in unit tests), consider two approaches:
- **Option A (preferred):** Extract event conversion and routing logic into pure helper functions that are testable without a real stream. Test helpers unit, test the loop integration with a mock.
- **Option B:** Test only the pure conversion (`map_to_proto_event`) and the :pg subscription setup. Integration-level stream tests via Godog Story 4-21.

**For the ATDD gate:** Write tests that verify the `:pg` subscription is set up correctly, and that the event conversion function maps correctly. The actual stream send can be integration-tested in Story 4-21.

### Architecture Compliance

| Rule | Requirement |
|---|---|
| ADR-005 | EventBus: one server-streaming connection per Go instance, not per client |
| ADR-002 | `:pg` Process Groups for fan-out — no Phoenix.PubSub, no Redis |
| Rule #6 | Tagged tuples `{:ok, ...}` / `{:error, ...}` — no `raise` for business logic (only for gRPC errors) |
| Rule #1 | Timestamps as BIGINT (milliseconds) — `origin_ts`, `server_ts` as `int64` milliseconds |
| grpc-elixir | Unary handlers return struct directly; streaming handlers call `GRPC.Server.send_reply/2`; metadata via `stream.http_request_headers` |
| Go convention | `context.Context` as first parameter, explicit error handling, no `panic` in library code |
| Logging | Go: `slog.Info/Warn/Error` with structured key-value pairs; Elixir: `Logger.info/warning/error` with keyword metadata |

### Stream Disconnect and GELB/ROT Status

When the EventBus Go stream is lost:
- Go logs `slog.Warn("EventBus stream lost, switching to polling")` — this is the GELB status trigger
- Go falls back to `GetPendingEvents` unary polling (already stubbed in `client.go`)
- Matrix clients get empty sync responses with `retry_after_ms` — Matrix-compliant
- GELB/ROT status is derived directly from gRPC connection state, not from arbitrary thresholds (ADR-005)

The `stream.go` should expose a method like `Status() string` returning `"green"` or `"yellow"` based on whether the stream is currently connected, for integration with `/ready` health endpoint.

### Dependency on Room GenServer (:pg groups)

The EventBus handler subscribes to `:pg` groups that are managed by `Nebu.Room.Server`. These groups follow the pattern `"room:#{room_id}"`. This convention is established in:
- `core/apps/room_manager/lib/nebu/room/server.ex` line 108: `:pg.join("room:#{room_id}", self())`
- `core/apps/room_manager/lib/nebu/room/server.ex` line 221: `:pg.get_local_members("room:#{room_id}")`

The event_dispatcher app must depend on room_manager app to call `Nebu.Room.Server.get_state/1` and `Horde.Registry` lookup. Check `core/apps/event_dispatcher/mix.exs`:
- Currently has `{:session_manager, in_umbrella: true}` — room_manager is NOT listed
- **Must add `{:room_manager, in_umbrella: true}`** to `event_dispatcher/mix.exs` deps for `get_room_state/2` and `Horde.Registry` access
- Also add `{:presence, in_umbrella: true}` only if `SetPresence` handler calls Presence API (not needed in this story)

### Existing Tests — Must NOT Break

All existing tests in event_dispatcher must keep passing:
- `test/nebu/event_dispatcher/validate_token_test.exs` — 4 tests (validate_token handler)
- `test/nebu/grpc/metadata_test.exs` — metadata extraction tests
- `test/nebu/health_test.exs` — health endpoint tests
- `test/nebu/node_registration_test.exs` — node registration tests
- `test/nebu_event_test.exs` — placeholder test

Full umbrella: currently ~122 tests (4-7 status). After 4-8 expect ~130+.

### What Story 4-8 Does NOT Implement

- No Matrix API HTTP handlers (`POST /createRoom`, `/sync`, etc.) — those are Stories 4-9 through 4-18
- No message_buffer integration — Story 4-16 connects EventBus output channel to buffer
- No `SetPresence` gRPC handler real implementation (stub returns empty response) — that's integrated with Presence Manager in a later story
- No `SendEvent`, `CreateRoom`, `JoinRoom` gRPC handler real implementations — those are Stories 4-9 through 4-11
- No room subscriptions filter by `room_ids` from `EventBusRequest` — MVP: subscribe to all rooms; filtered subscription is a Phase 2 optimization
- No `since_token` replay on reconnect — `EventBusRequest.since_token` is present but not used in MVP EventBus handler

### Build & Test Commands

```bash
# Regenerate proto stubs (both Go and Elixir):
make proto

# Run Elixir tests only (fast, targeted):
make test-unit-elixir

# Run Go tests only:
make test-unit-go

# Run both (before marking story complete):
make test-unit-elixir && make test-unit-go
```

All runs inside Docker containers — no local Go or Elixir install needed.

---

## Previous Story Intelligence

Key learnings from Story 4-7 (Presence Manager) directly relevant to 4-8:

1. **`:pg.start_link()` guard** — Always guard with `{:error, {:already_started, _}}` match. Already present in `Nebu.Room.Application` and `Nebu.Presence.Application`. The event_dispatcher's `Nebu.Event.Application` does NOT start `:pg` — it relies on `room_manager` starting it. Since `event_dispatcher` now depends on `room_manager`, `:pg` will be started. If standalone in test, add the same guard.

2. **`async: false` for :pg tests** — Any test using `:pg` process groups (joining, subscribing) must use `async: false`. The EventBus tests involve `:pg` and must be `async: false`.

3. **ETS guard pattern** — This story does NOT add ETS tables, but the guard pattern from Stories 4-4/4-5 is established: always check before creating.

4. **grpc-elixir 0.11.5 API** (from memory `feedback_grpc_elixir_api.md`):
   - Unary handlers return the response struct directly (NOT `{:ok, struct}`)
   - Metadata headers accessed via `stream.http_request_headers` (NOT `GRPC.Service.metadata/1`)
   - These patterns are already used in `validate_token/2` — follow them exactly.

5. **Module naming** — All modules use `Nebu.EventDispatcher.*` prefix. The `server.ex` already uses `defmodule Nebu.EventDispatcher.Server`. The test file should use `Nebu.EventDispatcher.EventBusTest`.

6. **test helper_test setup** — The existing `test/test_helper.exs` already starts the app. No changes needed there.

7. **No direct DB calls in EventBus** — The event_dispatcher is purely a routing layer. It receives events from `:pg` (which Room GenServer writes to DB first) and routes to gRPC streams. No Ecto calls in this story.

---

## Architecture References

- `proto/core.proto` — existing EventBus and Event definitions; add GetRoomState here
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — extend this file
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — existing gRPC service stubs (regenerated)
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — existing proto message stubs (regenerated)
- `core/apps/room_manager/lib/nebu/room/server.ex` — :pg group names, broadcast pattern, get_state API
- `core/apps/room_manager/lib/nebu/room/application.ex` — :pg.start_link() guard pattern
- `gateway/internal/grpc/client.go` — existing gRPC client stubs to extend
- `gateway/internal/grpc/pb/core_grpc.pb.go` — generated Go client interface
- `_bmad-output/planning-artifacts/architecture.md` — G2 (gRPC EventBus), ADR-005, GRÜN/GELB/ROT status definitions
- `_bmad-output/planning-artifacts/epics.md` — Story 4.8 acceptance criteria (lines ~1976–2007)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m] (Claude Sonnet 4.6 with 1M context)

### Completion Notes List

- Added `GetRoomState` RPC + `GetRoomStateRequest`/`GetRoomStateResponse` messages to `proto/core.proto`; ran `make proto` which regenerated Go stubs in `gateway/internal/grpc/pb/` and Elixir message stubs in `core/apps/event_dispatcher/lib/pb/core.pb.ex`
- Manually added `rpc :GetMetrics` and `rpc :GetRoomState` entries to `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (the protoc_gen_elixir grpc step does not auto-run in the `make proto` Makefile target for this project)
- Added `{:room_manager, in_umbrella: true}` dependency to `core/apps/event_dispatcher/mix.exs` for `Horde.Registry` access and `Nebu.Room.Server.get_state/1`
- Implemented `event_bus/2` in `server.ex`: traps exits, joins `"event_bus:gateways"` :pg group, subscribes to all active room groups via `Horde.Registry.select/2`, enters blocking receive loop forwarding `{:new_event, event_map}` to stream, cleans up :pg memberships on `{:EXIT, ...}`
- Implemented `get_room_state/2` in `server.ex`: delegates to configurable `room_registry_module` (default `Nebu.Room.Server`; override via `Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)` in tests), maps state to `%Core.GetRoomStateResponse{}`
- Added `do_send_reply/2` dispatcher: in test mode (stream has `:grpc_reply_interceptor` key), sends `{:grpc_reply, event}` to test PID instead of calling real `GRPC.Server.send_reply/2` which requires a live gRPC connection
- Created `gateway/internal/grpc/stream.go`: `EventBusStream` with `NewEventBusStream/3` (accepts options), `WithMinBackoff/WithMaxBackoff` options for test-overridable backoff, `Start(ctx)` launches background goroutine (non-blocking), `Events() <-chan *pb.Event`, exponential backoff reconnect loop, structured slog logging; also `NewClientWithCore(core)` constructor for test injection
- Updated `gateway/internal/grpc/client.go`: `EventBus` now calls `c.core.EventBus(ctx, req)` (real implementation); added `GetRoomState` method calling `c.core.GetRoomState(ctx, req)`
- Updated `gateway/internal/grpc/client_test.go`: `TestStubsReturnNil/EventBus` updated to expect a connection error (like `ValidateToken`) now that EventBus is wired to real gRPC
- `make test-unit-elixir`: 32 tests in event_dispatcher, 0 failures, 0 warnings (full umbrella: 129 tests)
- `make test-unit-go`: all 11 packages pass, 0 failures

### File List

Files created or modified:

```
proto/core.proto                                                          ← MODIFIED (added GetRoomState RPC + messages)
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex            ← MODIFIED (implemented event_bus/2, get_room_state/2, get_metrics/2)
core/apps/event_dispatcher/lib/pb/core.pb.ex                              ← REGENERATED (GetRoomStateRequest, GetRoomStateResponse added)
core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex                         ← MODIFIED (manually added GetMetrics + GetRoomState rpcs)
core/apps/event_dispatcher/mix.exs                                        ← MODIFIED (added room_manager in_umbrella dep)
core/apps/event_dispatcher/test/nebu/event_dispatcher/grpc_handler_test.exs ← PRE-EXISTING (ATDD tests, now passing)
gateway/internal/grpc/stream.go                                           ← CREATED (EventBusStream + NewClientWithCore)
gateway/internal/grpc/stream_test.go                                      ← PRE-EXISTING (ATDD tests, now passing)
gateway/internal/grpc/client.go                                           ← MODIFIED (EventBus wired; GetRoomState added)
gateway/internal/grpc/client_test.go                                      ← MODIFIED (EventBus test updated to expect connection error)
gateway/internal/grpc/pb/core.pb.go                                       ← REGENERATED (GetRoomStateRequest, GetRoomStateResponse)
gateway/internal/grpc/pb/core_grpc.pb.go                                  ← REGENERATED (GetRoomState client/server methods)
_bmad-output/implementation-artifacts/sprint-status.yaml                  ← MODIFIED (4-8 → review)
_bmad-output/implementation-artifacts/4-8-grpc-eventbus-server-streaming-getroomstate-unary.md ← MODIFIED (status, tasks, dev record)
```

### Change Log

- 2026-04-03: Story 4-8 created — gRPC EventBus Server-Streaming + GetRoomState Unary

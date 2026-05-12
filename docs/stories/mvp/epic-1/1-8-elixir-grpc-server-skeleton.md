# Story 1.8: Elixir gRPC Server Skeleton

Status: done

## Story

As a developer,
I want the Elixir core to have a configured gRPC server that accepts connections from the Go gateway,
so that subsequent stories can add individual RPC handlers without server infrastructure setup.

## Acceptance Criteria

1. **Given** a gRPC server library (`grpc` hex package) added to `core/apps/event_dispatcher/mix.exs`, **when** `mix deps.get` runs from `core/`, **then** it completes successfully with no conflicts

2. **Given** generated Elixir stubs from Story 1.6 in `core/apps/event_dispatcher/lib/pb/`, **when** `Nebu.EventDispatcher.Server` implements the generated gRPC behaviour, **then** `mix compile` completes with 0 errors and 0 warnings

3. **Given** Elixir core application start, **when** the supervision tree starts, **then** the gRPC server process starts and listens on port 9000

4. **Given** all RPC handler stubs, **when** each returns `{:ok, %{}}` (empty response), **then** the server starts without errors and accepts incoming TCP connections

5. **Given** `mix test` in the `event_dispatcher` app, **when** run, **then** existing tests pass with stub handlers in place

## Tasks / Subtasks

- [x] Task 1: Add `:grpc` dependency to `core/apps/event_dispatcher/mix.exs` (AC: #1)
  - [x] Add `{:grpc, "~> 0.8"}` to `defp deps` list
  - [x] Run `make test-unit-elixir` to verify `mix deps.get` completes with no conflicts

- [x] Task 2: Create gRPC service descriptor `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (AC: #2)
  - [x] Define `Core.CoreService.Service` module using `use GRPC.Service, name: "core.CoreService"`
  - [x] Declare all 8 unary RPCs with correct request/response message module names
  - [x] Declare `EventBus` streaming RPC using `stream(Core.Event)`
  - [x] Define `Core.CoreService.Stub` using `use GRPC.Stub, service: Core.CoreService.Service`

- [x] Task 3: Create gRPC server module `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (AC: #2, #4)
  - [x] Define `Nebu.EventDispatcher.Server` using `use GRPC.Server, service: Core.CoreService.Service`
  - [x] Implement all 8 unary RPC stubs returning empty response structs (`%Core.XyzResponse{}`)
  - [x] Implement `event_bus/2` stub for server-streaming RPC

- [x] Task 4: Create gRPC endpoint `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` (AC: #3)
  - [x] Define `Nebu.EventDispatcher.Endpoint` using `use GRPC.Endpoint` (grpc 0.11.5 API — `GRPC.Server.Endpoint` was renamed)
  - [x] Add `run(Nebu.EventDispatcher.Server)` directive

- [x] Task 5: Wire up gRPC server in `core/apps/event_dispatcher/lib/nebu/event/application.ex` (AC: #3)
  - [x] Add `{GRPC.Server.Supervisor, endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}` to children (grpc 0.11.5 keyword API — tuple form removed)
  - [x] Keep supervisor strategy `:one_for_one`

- [x] Task 6: Verify `make test-unit-elixir` passes (AC: #5)
  - [x] Existing placeholder test `"placeholder: event_dispatcher app starts"` must continue to pass
  - [x] `mix compile` with 0 errors and 0 warnings across all umbrella apps

## Dev Notes

### Critical: Service Descriptor Must Be Created Manually

The Makefile `proto:` target generates Elixir message types (`core.pb.ex`) but does NOT generate Elixir gRPC service stubs. The `--elixir_out` flag from `protoc-gen-elixir` only handles message types, not service behaviours.

**You must manually create `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex`** containing:
1. `Core.CoreService.Service` — service descriptor (used by server to implement behaviour)
2. `Core.CoreService.Stub` — client stub (not needed for this story but by convention included)

Do NOT run `make proto` again or attempt to modify the Makefile proto target for this story. [Source: Story 1.6 Dev Agent Record — Completion Notes, Makefile proto: target]

### Critical: `:protobuf` Transitive Dependency

`core/apps/event_dispatcher/mix.exs` currently has `deps: []`. The already-generated `core.pb.ex` uses `use Protobuf` which requires the `:protobuf` hex package at compile time.

Adding `{:grpc, "~> 0.8"}` resolves this because `grpc-elixir` depends on `:protobuf` transitively — `mix deps.get` will fetch `:protobuf` as part of the gRPC package's dependency tree. This is why AC #1 specifies only the `:grpc` package. [Source: grpc-elixir hex.pm dependency graph]

### gRPC Package Selection

Use the `grpc` hex package by tony612 (the `elixir-grpc/grpc` GitHub project):

```elixir
# In core/apps/event_dispatcher/mix.exs
defp deps do
  [
    {:grpc, "~> 0.8"}
  ]
end
```

This is the de-facto standard for gRPC servers in Elixir. It provides `GRPC.Server`, `GRPC.Service`, `GRPC.Server.Endpoint`, and `GRPC.Server.Supervisor`. [Source: epics.md — Story 1.8 AC #1]

### File: `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex`

```elixir
defmodule Core.CoreService.Service do
  use GRPC.Service, name: "core.CoreService", protoc_gen_elixir_version: "0.16.0"

  rpc :SendEvent, Core.SendEventRequest, Core.SendEventResponse
  rpc :CreateRoom, Core.CreateRoomRequest, Core.CreateRoomResponse
  rpc :JoinRoom, Core.JoinRoomRequest, Core.JoinRoomResponse
  rpc :GetMessages, Core.GetMessagesRequest, Core.GetMessagesResponse
  rpc :SetPresence, Core.SetPresenceRequest, Core.SetPresenceResponse
  rpc :SetTyping, Core.SetTypingRequest, Core.SetTypingResponse
  rpc :ValidateToken, Core.ValidateTokenRequest, Core.ValidateTokenResponse
  rpc :GetPendingEvents, Core.GetPendingEventsRequest, Core.GetPendingEventsResponse
  rpc :EventBus, Core.EventBusRequest, stream(Core.Event)
end

defmodule Core.CoreService.Stub do
  use GRPC.Stub, service: Core.CoreService.Service
end
```

**Critical message module names** — verify against `core.pb.ex` before writing:
- `Core.GetPendingEventsRequest` / `Core.GetPendingEventsResponse` (NOT `GetPendingRequest`/`GetPendingResponse` — renamed during Story 1.6 buf lint fixes) [Source: Story 1.6 Dev Agent Record — Debug Log; Story 1.7 Dev Notes]
- All other modules follow `Core.{MessageName}` pattern
- `stream(Core.Event)` for the `EventBus` streaming RPC response type

### File: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`

```elixir
defmodule Nebu.EventDispatcher.Server do
  use GRPC.Server, service: Core.CoreService.Service

  def send_event(_request, _stream) do
    {:ok, Core.SendEventResponse.new()}
  end

  def create_room(_request, _stream) do
    {:ok, Core.CreateRoomResponse.new()}
  end

  def join_room(_request, _stream) do
    {:ok, Core.JoinRoomResponse.new()}
  end

  def get_messages(_request, _stream) do
    {:ok, Core.GetMessagesResponse.new()}
  end

  def set_presence(_request, _stream) do
    {:ok, Core.SetPresenceResponse.new()}
  end

  def set_typing(_request, _stream) do
    {:ok, Core.SetTypingResponse.new()}
  end

  def validate_token(_request, _stream) do
    {:ok, Core.ValidateTokenResponse.new()}
  end

  def get_pending_events(_request, _stream) do
    {:ok, Core.GetPendingEventsResponse.new()}
  end

  def event_bus(_request, stream) do
    # Placeholder — Epic 4 Story 4.8 implements full streaming EventBus logic
    {:ok, stream}
  end
end
```

**Module naming note:** The AC specifies `Nebu.EventDispatcher.Server`, which diverges from the existing `Nebu.Event.*` convention used in this app (`Nebu.Event.Application`, `Nebu.Event.Dispatcher`). Follow the AC — this is the authoritative name. Place the file at `lib/nebu/event_dispatcher/server.ex`. [Source: epics.md — Story 1.8 AC #2]

### File: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex`

```elixir
defmodule Nebu.EventDispatcher.Endpoint do
  use GRPC.Server.Endpoint

  run(Nebu.EventDispatcher.Server)
end
```

The endpoint groups server modules. Port 9000 is configured in the supervision child spec (not here). [Source: architecture.md — G2]

### File: `core/apps/event_dispatcher/lib/nebu/event/application.ex` (modified)

```elixir
defmodule Nebu.Event.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {GRPC.Server.Supervisor, {Nebu.EventDispatcher.Endpoint, 9000}}
    ]

    opts = [strategy: :one_for_one, name: Nebu.Event.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

Port `9000` is fixed — matches `NEBU_CORE_GRPC_ADDR: core:9000` in the Go gateway config. Do NOT make this configurable for MVP. [Source: architecture.md — G2; Story 1.7 Dev Notes — Config Already Present]

### All 9 RPC Stubs: Exact Method Names

The gRPC method names in Elixir are `snake_case` conversions of the proto RPC names:

| Proto RPC | Elixir function | Response module |
|-----------|----------------|-----------------|
| `SendEvent` | `send_event/2` | `Core.SendEventResponse` |
| `CreateRoom` | `create_room/2` | `Core.CreateRoomResponse` |
| `JoinRoom` | `join_room/2` | `Core.JoinRoomResponse` |
| `GetMessages` | `get_messages/2` | `Core.GetMessagesResponse` |
| `SetPresence` | `set_presence/2` | `Core.SetPresenceResponse` |
| `SetTyping` | `set_typing/2` | `Core.SetTypingResponse` |
| `ValidateToken` | `validate_token/2` | `Core.ValidateTokenResponse` |
| `GetPendingEvents` | `get_pending_events/2` | `Core.GetPendingEventsResponse` |
| `EventBus` | `event_bus/2` | N/A (streaming) |

All unary stubs: `{:ok, Core.XyzResponse.new()}` — use `.new()` constructor, not `%Core.XyzResponse{}`, as the Protobuf macro may require initialization. Both forms work but `.new()` is idiomatic. [Source: protobuf hex package pattern]

### Build and Test Commands

```bash
# Fetch dependencies (must pass per AC #1)
make test-unit-elixir  # internally: mix local.hex --force && mix test

# Build inside container
make build-core  # internally: mix local.hex --force && mix deps.get && mix compile
```

Run via Docker containers — no local Elixir/Mix installation needed:
```bash
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
```

[Source: Makefile — DOCKER_ELIXIR, test-unit-elixir, build-core targets]

### No TLS for Internal gRPC

MVP: insecure gRPC (no TLS). Phase 2 adds mTLS per ADR 008. The Go gateway already uses `insecure.NewCredentials()` on its client side (Story 1.7). The Elixir server must NOT require TLS for MVP. [Source: architecture.md — ADR 008; Story 1.7 Dev Notes]

### Logging Convention

Use Elixir's `Logger` (not `IO.puts`, not `dbg`):

```elixir
require Logger
Logger.info("gRPC server started", port: 9000)
Logger.warning("event_bus stub called — not yet implemented")
```

[Source: architecture.md — Logging section]

### Project Structure Notes

**Files to create:**
```
core/apps/event_dispatcher/lib/pb/
  core_grpc.pb.ex          ← new (service descriptor + stub — manually written, NOT generated)

core/apps/event_dispatcher/lib/nebu/event_dispatcher/
  server.ex                ← new (Nebu.EventDispatcher.Server with 9 RPC stubs)
  endpoint.ex              ← new (Nebu.EventDispatcher.Endpoint)
```

**Files to modify:**
```
core/apps/event_dispatcher/mix.exs                    ← add {:grpc, "~> 0.8"} to deps
core/apps/event_dispatcher/lib/nebu/event/application.ex  ← add GRPC.Server.Supervisor to children
```

**Files NOT to touch:**
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — generated by `make proto`, do NOT edit manually
- `core/apps/event_dispatcher/lib/nebu/event/dispatcher.ex` — placeholder for Epic 4, do NOT touch
- All other `core/apps/*/mix.exs` — only `event_dispatcher/mix.exs` gets the `:grpc` dep
- `proto/` directory — no proto changes needed
- Go gateway code — no Go changes needed

**Why `core_grpc.pb.ex` in `lib/pb/`:** Co-locating service descriptor with message types follows the protobuf convention for generated files. Even though it's manually written, it logically belongs with the proto-derived types. [Source: grpc-elixir project structure conventions]

### References

- CoreService RPC list: [Source: proto/core.proto]
- Message type module names: [Source: core/apps/event_dispatcher/lib/pb/core.pb.ex]
- `GetPendingEventsRequest` naming: [Source: Story 1.6 Dev Agent Record — Debug Log; Story 1.7 Dev Notes]
- Port 9000 requirement: [Source: architecture.md — G2; CLAUDE.md — NEBU_CORE_GRPC_ADDR default]
- No TLS for MVP: [Source: architecture.md — ADR 008]
- Elixir logging with Logger: [Source: architecture.md — Logging section]
- Supervision strategy: [Source: architecture.md — G11 OTP Supervisor Trees]
- Build containers: [Source: Makefile — DOCKER_ELIXIR, build-core, test-unit-elixir]
- Go gRPC client uses `core:9000`: [Source: Story 1.7 Dev Notes — Config Already Present]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- grpc 0.11.5 (resolved from `~> 0.8`) has renamed `GRPC.Server.Endpoint` → `GRPC.Endpoint`. Endpoint was updated accordingly.
- grpc 0.11.5 no longer accepts tuple form `{GRPC.Server.Supervisor, {Endpoint, port}}`. Now requires keyword list: `endpoint: ..., port: ..., start_server: true`.
- Elixir 1.19 type checker reports `Core.XyzResponse.new/0` as "undefined or private" (macro-generated functions not visible to type checker). Switched to struct literals `%Core.XyzResponse{}` to eliminate warnings — both forms work at runtime.

### Completion Notes List

- Task 1: Added `{:grpc, "~> 0.8"}` dep — resolved to grpc 0.11.5 with protobuf 0.16.0 transitively. `mix deps.get` succeeded.
- Task 2: Created `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` manually with `Core.CoreService.Service` (9 RPCs incl. streaming EventBus) and `Core.CoreService.Stub`.
- Task 3: Created `Nebu.EventDispatcher.Server` with 8 unary stubs (struct literals) + `event_bus/2` streaming stub.
- Task 4: Created `Nebu.EventDispatcher.Endpoint` using `use GRPC.Endpoint` (corrected from story spec).
- Task 5: Wired `GRPC.Server.Supervisor` in `Nebu.Event.Application` using keyword API with `start_server: true`.
- Task 6: `make test-unit-elixir` — all 6 umbrella apps: 1 test each, 0 failures. `mix compile` 0 errors 0 warnings in our code.

### File List

- `core/apps/event_dispatcher/mix.exs` (modified — added `:grpc` dependency)
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (new — service descriptor + stub)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (new — gRPC server with 9 RPC stubs)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` (new — gRPC endpoint)
- `core/apps/event_dispatcher/lib/nebu/event/application.ex` (new — wired GRPC.Server.Supervisor; listed as "modified" in story spec but file was never committed in Story 1.2)
- `core/mix.lock` (modified — new dependencies resolved)

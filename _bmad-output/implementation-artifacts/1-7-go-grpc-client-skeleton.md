# Story 1.7: Go gRPC Client Skeleton

Status: done

## Story

As a developer,
I want the Go gateway to have a configured gRPC client package that connects to the Elixir core,
so that subsequent stories can implement individual RPC calls without repeating connection setup.

## Acceptance Criteria

1. **Given** `NEBU_CORE_GRPC_ADDR` env var (default: `core:9000`), **when** gateway starts, **then** a gRPC `ClientConn` is established to that address using the generated `CoreService` client

2. **Given** the `gateway/internal/grpc/` package, **when** it is inspected, **then** it exports a `Client` struct with methods for all `CoreService` RPCs (stub implementations returning `nil, nil` or an empty response struct)

3. **Given** gateway startup, **when** gRPC connection to core cannot be established within 5 seconds, **then** gateway logs a warning but does NOT exit — connection is non-blocking (lazy dial)

4. **Given** the gRPC client, **when** it is used in later stories, **then** it accepts a `context.Context` as the first parameter on all method calls (standard Go gRPC pattern)

5. **Given** Go naming conventions, **when** the package is inspected, **then** package name is `grpc` (lowercase), exported type is `Client`

## Tasks / Subtasks

- [x] Task 1: Create `gateway/internal/grpc/client.go` (AC: #1, #2, #3, #4, #5)
  - [x] Package declaration `package grpc`
  - [x] `Client` struct holding `*grpc.ClientConn` and `pb.CoreServiceClient`
  - [x] `New(addr string) (*Client, error)` using `grpc.NewClient()` (lazy, non-blocking)
  - [x] Non-blocking startup goroutine: probe for 5s, `slog.Warn` if not reachable, do NOT exit
  - [x] `Close() error` method delegating to `conn.Close()`
  - [x] Stub methods for all 9 CoreService RPCs returning `nil, nil`

- [x] Task 2: Wire up gRPC client in `gateway/cmd/gateway/main.go` (AC: #1)
  - [x] Import `coregrpc "github.com/nebu/nebu/internal/grpc"` (alias to avoid name clash)
  - [x] Call `coregrpc.New(cfg.CoreGRPCAddr)` after DB init
  - [x] `slog.Info("gRPC client initialized", "addr", cfg.CoreGRPCAddr)`
  - [x] `defer coreClient.Close()`
  - [x] Assign `_ = coreClient` to prevent unused-variable error (HTTP server wired in Story 1.11)

- [x] Task 3: Create `gateway/internal/grpc/client_test.go` (architecture testing mandate)
  - [x] `TestNew_returnsWithoutBlocking`: call `New("localhost:19999")`, verify no error, verify returns quickly (lazy)
  - [x] Table-driven `TestStubsReturnNil`: iterate all 9 stub methods, verify each returns `nil, nil`
  - [x] Run `make test-unit-go` to confirm all existing tests still pass

## Dev Notes

### Package Name / Import Alias Pattern

The package is named `grpc` (required by AC #5), which conflicts with the import identifier of `google.golang.org/grpc`. Inside `client.go`, alias the stdlib import:

```go
package grpc

import (
    grpclib "google.golang.org/grpc"
    "google.golang.org/grpc/connectivity"
    "google.golang.org/grpc/credentials/insecure"
    pb "github.com/nebu/nebu/internal/grpc/pb"
)
```

In `main.go`, alias the local package to avoid collision with future `grpc` imports:

```go
import (
    coregrpc "github.com/nebu/nebu/internal/grpc"
)
```

### Generated Stub Location (from Story 1.6)

The generated Go stubs from `make proto` live at:
- `gateway/internal/grpc/pb/core.pb.go` — message types
- `gateway/internal/grpc/pb/core_grpc.pb.go` — `CoreServiceClient` interface + `NewCoreServiceClient()`

The architecture doc shows `proto/gen/go/` as an alternative — **ignore it**. The ACs are authoritative. The stubs are at `gateway/internal/grpc/pb/`. [Source: epics.md — Story 1.6 AC #4, Story 1.6 Dev Notes]

### gRPC Dependency Versions (already in go.mod)

`gateway/go.mod` already has — do NOT `go get` again:
- `google.golang.org/grpc v1.79.3`
- `google.golang.org/protobuf v1.36.11`

Required additional imports (packages within grpc module, no new deps needed):
- `google.golang.org/grpc/connectivity` — for `connectivity.Ready` state check
- `google.golang.org/grpc/credentials/insecure` — for `insecure.NewCredentials()`

### Modern gRPC Dial API (v1.79.3)

`grpc.Dial()` and `grpc.DialContext()` are **deprecated** since v1.63. Use `grpc.NewClient()`:

```go
conn, err := grpclib.NewClient(
    addr,
    grpclib.WithTransportCredentials(insecure.NewCredentials()),
)
```

`grpc.NewClient()` is **lazy by default** — it does NOT open a TCP connection until the first RPC call. This satisfies AC #3 (non-blocking, gateway does not exit on unavailable core).

No TLS for internal gRPC (MVP). Phase 2 adds mTLS per ADR 008. [Source: architecture.md — ADR 008]

### 5-Second Startup Probe (AC #3)

Since `NewClient` is lazy, add a background goroutine to trigger the handshake and warn if unavailable:

```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    conn.Connect() // explicitly trigger lazy connection attempt
    for {
        s := conn.GetState()
        if s == connectivity.Ready {
            return
        }
        if !conn.WaitForStateChange(ctx, s) {
            slog.Warn("gRPC core not reachable at startup, continuing", "addr", addr)
            return
        }
    }
}()
```

**Critical**: This goroutine is fire-and-forget. `New()` returns immediately without waiting for it. Gateway startup continues regardless of outcome. [Source: epics.md — Story 1.7 AC #3]

### All 9 CoreService RPC Stubs

All methods on `Client` must wrap the corresponding `pb.CoreServiceClient` call. For this skeleton story, every stub just returns `nil, nil`. Actual implementation deferred to Epics 2 and 4.

```
SendEvent(ctx, *pb.SendEventRequest)           → (*pb.SendEventResponse, error)
CreateRoom(ctx, *pb.CreateRoomRequest)         → (*pb.CreateRoomResponse, error)
JoinRoom(ctx, *pb.JoinRoomRequest)             → (*pb.JoinRoomResponse, error)
GetMessages(ctx, *pb.GetMessagesRequest)       → (*pb.GetMessagesResponse, error)
SetPresence(ctx, *pb.SetPresenceRequest)       → (*pb.SetPresenceResponse, error)
SetTyping(ctx, *pb.SetTypingRequest)           → (*pb.SetTypingResponse, error)
ValidateToken(ctx, *pb.ValidateTokenRequest)   → (*pb.ValidateTokenResponse, error)
GetPendingEvents(ctx, *pb.GetPendingEventsRequest) → (*pb.GetPendingEventsResponse, error)
EventBus(ctx, *pb.EventBusRequest)             → (grpclib.ServerStreamingClient[pb.Event], error)
```

Note: `GetPendingEventsRequest/Response` (not `GetPendingRequest/Response`) — buf lint in Story 1.6 renamed these. Verify against `core_grpc.pb.go`. [Source: story 1-6 Dev Agent Record — Debug Log]

### Config Already Present

`NEBU_CORE_GRPC_ADDR` is already in `config.Config.CoreGRPCAddr` with default `core:9000`. No config changes needed. [Source: gateway/internal/config/config.go:7-18]

### Logging Convention

Use `log/slog` (stdlib). Pattern from existing code in `main.go`:
- `slog.Info("gRPC client initialized", "addr", cfg.CoreGRPCAddr)`
- `slog.Warn("gRPC core not reachable at startup, continuing", "addr", addr)`

Do NOT use `fmt.Println`, `log.Printf`, or any other logger. [Source: architecture.md — Logging section]

### main.go Integration

Current `main.go` ends with `// HTTP listener started in Story 1.11`. Insert gRPC init after `InitServerConfig` and before that comment:

```go
coreClient, err := coregrpc.New(cfg.CoreGRPCAddr)
if err != nil {
    slog.Error("failed to create gRPC client", "err", err)
    os.Exit(1)
}
defer coreClient.Close()
slog.Info("gRPC client initialized", "addr", cfg.CoreGRPCAddr)

_ = coreClient // passed to HTTP handlers in Story 1.11
```

`grpc.NewClient` can return an error only for invalid options — not for unreachable addresses. The `os.Exit(1)` is a safeguard for misconfiguration, NOT for core unavailability. [Source: grpc-go v1.79 docs]

### Project Structure Notes

**Files to create:**
```
gateway/internal/grpc/client.go       ← new (gRPC Client struct + New + stubs)
gateway/internal/grpc/client_test.go  ← new (unit tests)
```

**Files to modify:**
```
gateway/cmd/gateway/main.go           ← add coregrpc.New() call
```

**Files NOT to touch:**
- `gateway/internal/grpc/pb/` — generated, do NOT edit manually
- `gateway/internal/grpc/.gitkeep` — harmless, leave it
- `gateway/internal/grpc/stream.go` / `fallback.go` — Story 4.x territory
- All other gateway packages (config, db, migrations)

**No new Go module dependencies** — all required packages already in `go.mod`.

### Testing

Architecture mandates table-driven tests. Run via:
```bash
make test-unit-go  # runs: docker run golang:1.26-alpine sh -c "cd gateway && go test ./..."
```

Since `grpc.NewClient()` is lazy, `TestNew_returnsWithoutBlocking` can use a non-existent address and still succeed. Include `t.Helper()` in test helpers.

Existing tests (config, db, migrations) must continue to pass — this story does not touch those packages.

### References

- CoreService RPC signatures: [Source: gateway/internal/grpc/pb/core_grpc.pb.go:39-55]
- `GetPendingEventsRequest` name: [Source: story 1-6 Dev Agent Record — buf lint fixes]
- gRPC addr config: [Source: gateway/internal/config/config.go:7-18]
- Non-blocking dial requirement: [Source: epics.md — Story 1.7 AC #3]
- Insecure MVP / mTLS Phase 2: [Source: architecture.md — ADR 008]
- `grpc.NewClient` replaces `grpc.Dial`: [Source: grpc-go changelog v1.63+]
- GRÜN/GELB/ROT status model: [Source: architecture.md — G12]
- Logging with slog: [Source: architecture.md — Logging section, gateway/cmd/gateway/main.go]
- Build/test container: [Source: Makefile — DOCKER_GO, test-unit-go targets]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

No issues encountered. `grpc.NewClient()` is lazy by default — `TestNew_returnsWithoutBlocking` uses `localhost:19999` (non-existent) and succeeds without blocking, confirming AC #3.

### Completion Notes List

- Created `gateway/internal/grpc/client.go`: `Client` struct with `New()` (lazy dial via `grpclib.NewClient()`), background 5s startup probe with `slog.Warn`, `Close()`, and 9 stub methods all returning `nil, nil`.
- Updated `gateway/cmd/gateway/main.go`: added `coregrpc` import alias, `coregrpc.New(cfg.CoreGRPCAddr)` call with error guard, `defer Close()`, and `_ = coreClient` placeholder.
- Created `gateway/internal/grpc/client_test.go`: `TestNew_returnsWithoutBlocking` (lazy dial timing) + table-driven `TestStubsReturnNil` (9 RPCs).
- All tests pass: `make test-unit-go` — `ok github.com/nebu/nebu/internal/grpc 0.002s`, no regressions in config/db/migrations packages.

### File List

- gateway/internal/grpc/client.go (new)
- gateway/internal/grpc/client_test.go (new)
- gateway/cmd/gateway/main.go (modified)

## Change Log

- 2026-03-23: Implemented Go gRPC client skeleton — `Client` struct, lazy `New()` dial, 5s startup probe, 9 RPC stubs, `main.go` wiring, unit tests (Date: 2026-03-23)

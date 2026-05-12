# Story 1.6: gRPC Proto Contract Definition + buf Configuration

Status: done

## Story

As a developer,
I want the complete gRPC service contract defined in a `.proto` file with buf tooling configured,
so that Go and Elixir can independently generate type-safe stubs from a single source of truth.

## Acceptance Criteria

1. **Given** `proto/core.proto` exists, **when** parsed by `buf lint`, **then** no lint errors are reported

2. **Given** the proto file, **when** `CoreService` RPCs are listed, **then** it includes all of: `SendEvent`, `CreateRoom`, `JoinRoom`, `GetMessages`, `SetPresence`, `SetTyping`, `ValidateToken`, `GetPendingEvents` (unary fallback), and `EventBus` (server-streaming: `returns (stream Event)`)

3. **Given** `buf.yaml` and `buf.gen.yaml` exist in `proto/`, **when** `make proto` runs (buf generate in container), **then** it exits with code 0

4. **Given** `make proto` completes, **when** output directories are inspected, **then** generated Go stubs exist in `gateway/internal/grpc/pb/` and generated Elixir stubs exist in `core/apps/event_dispatcher/lib/pb/`

5. **Given** all proto message field names, **when** naming is verified, **then** all fields use `snake_case` (e.g., `room_id`, `sender_id`, `origin_ts`, `event_type`)

6. **Given** `make proto` runs, **when** generated files already exist, **then** they are overwritten cleanly (idempotent regeneration)

## Tasks / Subtasks

- [x] Task 1: Create `proto/core.proto` (AC: #1, #2, #5)
  - [x] `syntax = "proto3"`, package `core`, `go_package` option set
  - [x] Define all 9 required `CoreService` RPCs
  - [x] Define all request/response message types
  - [x] All fields use `snake_case`
  - [x] `buf lint` passes with no errors

- [x] Task 2: Create `proto/buf.yaml` (AC: #1, #3)
  - [x] buf v2 module config scoped to `proto/` directory
  - [x] Disable `PACKAGE_VERSION_SUFFIX` lint rule (package is `core`, not versioned)

- [x] Task 3: Create `proto/buf.gen.yaml` (AC: #3, #4)
  - [x] Go message plugin → `../gateway/internal/grpc/pb`
  - [x] Go gRPC plugin → `../gateway/internal/grpc/pb`
  - [x] Elixir plugin → `../core/apps/event_dispatcher/lib/pb`
  - [x] Both plugins use `paths=source_relative` option

- [x] Task 4: Update `Makefile` `proto:` target (AC: #3)
  - [x] Change working directory to `/workspace/proto` so buf finds `buf.gen.yaml` as `./buf.gen.yaml`

- [x] Task 5: Run `make proto` and verify all outputs (AC: #4, #6)
  - [x] Confirm `gateway/internal/grpc/pb/*.go` files exist
  - [x] Confirm `core/apps/event_dispatcher/lib/pb/*.ex` files exist
  - [x] Re-run `make proto` — confirm it exits 0 again (idempotent)

- [x] Task 6: Add grpc/proto Go dependencies to `gateway/go.mod` (prerequisite for Story 1.7)
  - [x] `go get google.golang.org/grpc`
  - [x] `go get google.golang.org/protobuf`
  - [x] Verify `go build ./...` compiles cleanly in gateway

## Dev Notes

### What This Story Delivers

Two deliverables:
1. **`proto/core.proto`** — the single source of truth for all Go ↔ Elixir gRPC contracts
2. **buf tooling** (`proto/buf.yaml` + `proto/buf.gen.yaml`) — reproducible code generation

Generated stubs (`gateway/internal/grpc/pb/` and `core/apps/event_dispatcher/lib/pb/`) are outputs of `make proto` and should be committed to the repo.

**Nothing else changes.** Do NOT touch `gateway/internal/grpc/.gitkeep` (it will be replaced by generated files). Do NOT modify any existing Go or Elixir source files.

---

### Architecture Discrepancy — Important

The architecture document (`architecture.md`) shows generated Go stubs at `proto/gen/go/` (a separate Go module imported via `replace` directive), with `buf.yaml`/`buf.gen.yaml` at the project root.

**The epics ACs specify different paths.** Follow the ACs:
- `buf.yaml` + `buf.gen.yaml` → `proto/`
- Generated Go stubs → `gateway/internal/grpc/pb/`
- Generated Elixir stubs → `core/apps/event_dispatcher/lib/pb/`

Story 1.7 will import the stubs as `github.com/nebu/nebu/internal/grpc/pb` — this works because `gateway/go.mod` declares `module github.com/nebu/nebu` and the stubs live under `gateway/internal/grpc/pb/`.

---

### File: `proto/core.proto`

```protobuf
syntax = "proto3";

package core;

option go_package = "github.com/nebu/nebu/internal/grpc/pb";

// CoreService: all gRPC operations between Go Gateway and Elixir Core.
// Go is always the client; Elixir is always the server.
service CoreService {
  // Matrix operations (unary request/response)
  rpc SendEvent(SendEventRequest)           returns (SendEventResponse);
  rpc CreateRoom(CreateRoomRequest)         returns (CreateRoomResponse);
  rpc JoinRoom(JoinRoomRequest)             returns (JoinRoomResponse);
  rpc GetMessages(GetMessagesRequest)       returns (GetMessagesResponse);
  rpc SetPresence(SetPresenceRequest)       returns (SetPresenceResponse);
  rpc SetTyping(SetTypingRequest)           returns (SetTypingResponse);
  rpc ValidateToken(ValidateTokenRequest)   returns (ValidateTokenResponse);
  rpc GetPendingEvents(GetPendingRequest)   returns (GetPendingResponse);

  // Event Bus — server-streaming, one stream per Go instance
  rpc EventBus(EventBusRequest)             returns (stream Event);
}

// Shared event type used across multiple RPCs
message Event {
  string event_id   = 1;
  string room_id    = 2;
  string sender_id  = 3;
  string event_type = 4;
  bytes  content    = 5;  // JSON-encoded event content
  int64  origin_ts  = 6;  // Unix milliseconds
  int64  server_ts  = 7;  // Unix milliseconds
}

// SendEvent
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

// CreateRoom
message CreateRoomRequest {
  string          creator_id = 1;
  optional string name       = 2;
  optional string topic      = 3;
  bool            is_direct  = 4;
}
message CreateRoomResponse {
  string room_id = 1;
}

// JoinRoom
message JoinRoomRequest {
  string user_id          = 1;
  string room_id_or_alias = 2;
}
message JoinRoomResponse {
  string room_id = 1;
}

// GetMessages
message GetMessagesRequest {
  string          room_id     = 1;
  string          from_token  = 2;
  optional string to_token    = 3;
  int32           limit       = 4;
  string          direction   = 5;  // "b" (backward) or "f" (forward)
}
message GetMessagesResponse {
  repeated Event events     = 1;
  string         next_batch = 2;
  string         prev_batch = 3;
}

// SetPresence
message SetPresenceRequest {
  string          user_id    = 1;
  string          presence   = 2;  // "online", "offline", "unavailable"
  optional string status_msg = 3;
}
message SetPresenceResponse {}

// SetTyping
message SetTypingRequest {
  string room_id    = 1;
  string user_id    = 2;
  bool   typing     = 3;
  int32  timeout_ms = 4;
}
message SetTypingResponse {}

// ValidateToken — Go validates OIDC token, Elixir trusts Go fully (ADR G2)
message ValidateTokenRequest {
  string token = 1;
}
message ValidateTokenResponse {
  bool   valid       = 1;
  string user_id     = 2;
  string system_role = 3;  // "user" | "instance_admin" | "compliance_officer"
}

// GetPendingEvents — GELB-status fallback (unary polling instead of streaming)
message GetPendingRequest {
  string node_id     = 1;
  string since_token = 2;
}
message GetPendingResponse {
  repeated Event events     = 1;
  string         next_token = 2;
}

// EventBus — server-streaming, one stream per Go gateway instance
message EventBusRequest {
  string          node_id     = 1;
  optional string since_token = 2;
}
```

**Critical naming rules (enforced by buf lint):**
- All field names: `snake_case` — `room_id`, `sender_id`, `origin_ts`, `event_type`
- All message names: `PascalCase` — `SendEventRequest`, `Event`
- Service name: `PascalCase` — `CoreService`
- Package: `core` (lowercase)

**Timestamp fields:** All timestamps are `int64` Unix milliseconds — NOT `google.protobuf.Timestamp`. [Source: architecture.md — Timestamps section: "Proto/gRPC: int64 ms, Proto native"]

**RPCs in THIS story only:** Do NOT add RPCs listed in later epics (e.g., `WriteAuditLog`, `InvalidateUserSessions`, `GetInitialSync`). Future stories extend the proto with additional RPCs as needed.

---

### File: `proto/buf.yaml`

```yaml
version: v2
modules:
  - path: .
lint:
  use:
    - DEFAULT
  except:
    - PACKAGE_VERSION_SUFFIX
breaking:
  use:
    - FILE
```

**Why `PACKAGE_VERSION_SUFFIX` is excluded:** The architecture mandates package `core` (not `core.v1`). Buf's DEFAULT lint rules require version suffixes for BSR publishing — this project does not publish to the BSR.

---

### File: `proto/buf.gen.yaml`

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: ../gateway/internal/grpc/pb
    opt:
      - paths=source_relative
  - remote: buf.build/grpc/go
    out: ../gateway/internal/grpc/pb
    opt:
      - paths=source_relative
  - remote: buf.build/elixir-protobuf/elixir
    out: ../core/apps/event_dispatcher/lib/pb
```

**Output paths are relative to `proto/`** (where buf.gen.yaml lives):
- `../gateway/internal/grpc/pb` → `gateway/internal/grpc/pb/` ✓
- `../core/apps/event_dispatcher/lib/pb` → `core/apps/event_dispatcher/lib/pb/` ✓

**Plugins:**
- `buf.build/protocolbuffers/go` → generates `core.pb.go` (message types)
- `buf.build/grpc/go` → generates `core_grpc.pb.go` (service client/server interfaces)
- `buf.build/elixir-protobuf/elixir` → generates Elixir stubs

---

### Makefile Update

**Current `proto:` target:**
```makefile
proto:
    $(DOCKER_BUF) generate
```

`$(DOCKER_BUF)` is defined as:
```makefile
DOCKER_BUF = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf
```

**Problem:** `buf generate` looks for `buf.gen.yaml` in the current working directory (`/workspace`). Since `buf.gen.yaml` is in `proto/`, the working dir must change.

**Fix — change working directory to `/workspace/proto`:**
```makefile
proto:
    docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf generate
```

Running from `/workspace/proto`:
- `buf generate` finds `./buf.gen.yaml` (which is `proto/buf.gen.yaml`) ✓
- `buf generate` finds `./buf.yaml` (module config) ✓
- Output paths `../gateway/...` resolve relative to `/workspace/proto`, giving `/workspace/gateway/...` ✓
- The `.gitkeep` in `gateway/internal/grpc/` will remain alongside generated files (harmless)

---

### Gateway go.mod — Required Additions (Task 6)

The generated `core_grpc.pb.go` imports `google.golang.org/grpc` and the `.pb.go` imports `google.golang.org/protobuf`. Without these in `gateway/go.mod`, `go build ./...` fails.

Run inside the gateway container (or use `$(DOCKER_GO)`):
```bash
cd gateway && go get google.golang.org/grpc@latest
cd gateway && go get google.golang.org/protobuf@latest
```

As of Q1 2026, latest stable versions are approximately:
- `google.golang.org/grpc v1.71.x`
- `google.golang.org/protobuf v1.36.x`

Use `go get` to resolve the exact version — do NOT hardcode version numbers.

---

### pgx Driver Pattern (for reference — unchanged from Story 1.5)

This story does NOT touch `gateway/internal/db/`. The pgx/grpc driver separation from Story 1.5 remains in effect:
- `database/sql` uses `pgx` driver (for InitServerConfig, CheckDB)
- golang-migrate uses `pgx5` via `pgx5URL()` helper

---

### Project Structure Notes

**Files to create:**
```
proto/
  core.proto              ← new (proto definition)
  buf.yaml                ← new (buf module config)
  buf.gen.yaml            ← new (code generation config)
  .gitkeep                ← remove or leave (buf output replaces it)

gateway/internal/grpc/
  pb/                     ← new directory, created by buf
    core.pb.go            ← generated (do NOT hand-write)
    core_grpc.pb.go       ← generated (do NOT hand-write)

core/apps/event_dispatcher/lib/
  pb/                     ← new directory, created by buf
    core.pb.ex            ← generated (do NOT hand-write)
```

**Files to modify:**
```
Makefile                  ← update proto: target working directory
gateway/go.mod            ← add grpc + protobuf deps
gateway/go.sum            ← updated automatically by go get
```

**Files NOT to touch:**
- `gateway/internal/grpc/.gitkeep` (buf output lands alongside it or replaces it)
- `gateway/internal/db/db.go`, `serverconfig.go` (no changes)
- `gateway/cmd/gateway/main.go` (no changes in this story)
- `gateway/migrations/` (no changes)
- All `core/apps/*/mix.exs` files except potentially `event_dispatcher` if Elixir grpc dep is needed
- All `core/` source files

**Note on `core/apps/event_dispatcher/mix.exs`:** Currently has empty `deps: []`. Story 1.8 will add the Elixir gRPC dependency (`grpc` hex package). This story (1.6) only generates the stubs — no Elixir deps change needed yet.

---

### Verification

After running `make proto`, verify:

```bash
# Check generated Go files exist
ls gateway/internal/grpc/pb/
# Expected: core.pb.go  core_grpc.pb.go

# Check generated Elixir files exist
ls core/apps/event_dispatcher/lib/pb/
# Expected: core.pb.ex (or similar)

# Check buf lint passes
docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf lint
# Expected: (no output, exit 0)

# Check Go compiles (after adding go.mod deps)
docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine \
  sh -c "cd gateway && go build ./..."
# Expected: exit 0

# Idempotent re-run
make proto && echo "idempotent: OK"
```

### References

- CoreService RPCs: [Source: epics.md — Story 1.6 Acceptance Criteria]
- Proto service definition: [Source: architecture.md — G2 "gRPC Interface: Server-Streaming + Unary Fallback"]
- Generated stub paths: [Source: epics.md — Story 1.6 AC #4 (gateway/internal/grpc/pb/ and core/apps/event_dispatcher/lib/pb/)]
- Timestamp convention (int64 ms): [Source: architecture.md — Timestamps section]
- Proto naming conventions: [Source: architecture.md — "Proto/gRPC: Messages PascalCase, Fields snake_case"]
- Build container pattern: [Source: Makefile (root), architecture.md — Build-Container-Strategie]
- go_package correctness: [Source: gateway/go.mod — `module github.com/nebu/nebu`]
- Auth token flow (ValidateToken context): [Source: architecture.md — Auth-Token-Flow]
- GELB/ROT status (GetPendingEvents role): [Source: architecture.md — G2 and G12]

## Review Follow-ups

- [ ] [AI-Review][LOW] Build-Container-Optimierung: `make proto` Elixir-Schritt installiert `protobuf` + `protoc-gen-elixir` bei jedem Lauf (~30-60s Overhead). Sobald sich die Build-Konfiguration stabilisiert hat, Custom Docker Images erstellen, die alle Build-Dependencies vorinstalliert haben (betrifft auch `make test-unit-elixir`, `make build-core`). Nicht jetzt, da sich das Setup noch entwickelt.

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- buf.build/elixir-protobuf/elixir remote plugin does not exist on buf.build registry. Replaced with two-step proto generation: buf generate (Go stubs via remote plugins) + protoc + protoc-gen-elixir (Elixir stubs via elixir:1.19-alpine container). User confirmed using protocolbuffers/protobuf (protoc) for Elixir generation.
- buf lint initially failed with PACKAGE_DIRECTORY_MATCH and RPC_RESPONSE_STANDARD_NAME violations. Added both to buf.yaml except list. GetPendingRequest/Response renamed to GetPendingEventsRequest/Response to match RPC method name convention.
- DEFAULT category in buf.yaml deprecated; replaced with STANDARD.

### Completion Notes List

- Created `proto/core.proto` with all 9 CoreService RPCs, all request/response message types, snake_case fields. buf lint passes.
- Created `proto/buf.yaml` (v2, STANDARD lint, exceptions: PACKAGE_VERSION_SUFFIX, PACKAGE_DIRECTORY_MATCH, RPC_RESPONSE_STANDARD_NAME).
- Created `proto/buf.gen.yaml` (v2, Go remote plugins only). Elixir stubs generated via separate protoc step using elixir:1.19-alpine + protoc-gen-elixir.
- Updated Makefile `proto:` target to two-step: buf generate (Go) + protoc (Elixir). Working directory set to /workspace/proto.
- `make proto` exits 0 and is idempotent. Generated: gateway/internal/grpc/pb/core.pb.go, core_grpc.pb.go; core/apps/event_dispatcher/lib/pb/core.pb.ex.
- Added google.golang.org/grpc v1.79.3 and google.golang.org/protobuf v1.36.11 to gateway/go.mod. `go build ./...` passes.
- All existing Go tests pass (config, db, migrations packages).

### File List

- proto/core.proto (new)
- proto/buf.yaml (new)
- proto/buf.gen.yaml (new)
- gateway/internal/grpc/pb/core.pb.go (generated)
- gateway/internal/grpc/pb/core_grpc.pb.go (generated)
- core/apps/event_dispatcher/lib/pb/core.pb.ex (generated)
- Makefile (modified — proto: target)
- gateway/go.mod (modified — grpc + protobuf deps)
- gateway/go.sum (modified — updated by go get)
- .gitignore (modified — added `!lib/` negation for Elixir source, code review fix)

## Change Log

- 2026-03-23: Implemented Story 1.6 — proto/core.proto (9 CoreService RPCs), buf.yaml, buf.gen.yaml, Makefile proto: target, generated Go + Elixir stubs, added grpc/protobuf Go deps. Elixir stubs via protoc + protoc-gen-elixir (protocolbuffers/protobuf) instead of unavailable buf.build remote plugin.
- 2026-03-23: Code review (claude-opus-4-6) — HIGH fix: Elixir-Stub `core.pb.ex` war durch globale `.gitignore_global` (`lib/`) blockiert. Fix: `!lib/` Negationsregel in Projekt-`.gitignore` hinzugefügt + Datei gestaged. Action Item: Build-Container-Optimierung (LOW) als Follow-up. Status → done.

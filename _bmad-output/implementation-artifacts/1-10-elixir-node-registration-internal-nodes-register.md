# Story 1.10: Elixir Node Registration (/internal/nodes/register)

Status: done

## Story

As an operator,
I want the Elixir core to register itself with the Go gateway on startup,
so that the gateway knows the core is available and can begin routing requests.

## Acceptance Criteria

1. **Given** the gateway HTTP server is running, **when** `POST /internal/nodes/register` receives a request with a valid `Authorization: Bearer <psk>` header, **then** it responds `200 OK` with `{"status": "registered"}`

2. **Given** `POST /internal/nodes/register` receives a request with wrong or missing authorization, **when** processed by the gateway, **then** it responds `401 Unauthorized` with no body leaking internal details

3. **Given** Elixir core application startup, **when** the Application `start/2` callback completes, **then** a startup task calls `POST /internal/nodes/register` on the gateway URL configured via `NEBU_GATEWAY_INTERNAL_URL`

4. **Given** the registration request fails (gateway not yet ready), **when** the startup task receives a connection error, **then** Elixir retries up to 5 times with 2-second backoff before logging `"Gateway registration failed after retries"` and continuing startup without crashing

5. **Given** `GET /internal/nodes` with valid PSK, **when** called on the gateway after Elixir has registered, **then** it returns a JSON list containing the registered node entry

## Tasks / Subtasks

- [x] Task 1: Go — PSK Middleware (AC: #1, #2)
  - [x] Create `gateway/internal/middleware/psk.go` with `PSKMiddleware(secret string) func(http.Handler) http.Handler`
  - [x] Read and compare `Authorization: Bearer <psk>` header using `strings.TrimSpace` and `subtle.ConstantTimeCompare`
  - [x] Return 401 with empty body on mismatch — no error message in response body
  - [x] Create `gateway/internal/middleware/psk_test.go` with tests for valid PSK, missing header, wrong PSK

- [x] Task 2: Go — Node Registry (AC: #1, #5)
  - [x] Create `gateway/internal/registry/registry.go` with thread-safe `Registry` type (sync.RWMutex + map)
  - [x] `NodeEntry` struct: `Addr string`, `RegisteredAt time.Time`, JSON tags
  - [x] Methods: `Register(addr string)`, `List() []NodeEntry`
  - [x] Create `gateway/internal/registry/handler.go` with `NewHandler(reg *Registry) http.Handler`
  - [x] `POST /internal/nodes/register` → extract `X-Forwarded-For` or `RemoteAddr` as addr, call `reg.Register()`, respond `{"status": "registered"}`
  - [x] `GET /internal/nodes` → call `reg.List()`, respond JSON array
  - [x] Create `gateway/internal/registry/registry_test.go` with unit tests for Register, List, handler responses

- [x] Task 3: Go — Start HTTP Server in main.go (AC: #1, #2, #5)
  - [x] Read PSK from file at startup: `os.ReadFile(cfg.InternalSecretFile)`, `strings.TrimSpace(string(psk))`
  - [x] Create `http.NewServeMux()` and register routes with PSK middleware applied to `/internal/` prefix
  - [x] Replace `select {}` with `http.ListenAndServe(":8008", mux)` and `slog.Error` + `os.Exit(1)` on failure
  - [x] Pass registry handler: `mux.Handle("/internal/nodes/", middleware.PSKMiddleware(secret)(registry.NewHandler(reg)))`

- [x] Task 4: Elixir — Node Registration Module (AC: #3, #4)
  - [x] Create `core/apps/event_dispatcher/lib/nebu/node_registration.ex` — module `Nebu.NodeRegistration`
  - [x] `register_with_gateway(retries_left \\ 5)` — reads PSK from `NEBU_INTERNAL_SECRET_FILE`, POSTs to `NEBU_GATEWAY_INTERNAL_URL/internal/nodes/register`
  - [x] Use `:httpc` (built-in `:inets`) — no new Hex dependencies
  - [x] On connection error: log warning, `Process.sleep(2_000)`, recurse with `retries_left - 1`
  - [x] On 0 retries left: `Logger.error("Gateway registration failed after retries: #{reason}")`
  - [x] On success (HTTP 200): `Logger.info("Registered with gateway")`
  - [x] Create `core/apps/event_dispatcher/test/nebu/node_registration_test.exs` with ExUnit tests

- [x] Task 5: Elixir — Application Startup Hook (AC: #3)
  - [x] Update `core/apps/event_dispatcher/lib/nebu/event/application.ex`: after `Supervisor.start_link`, add `Task.start(fn -> Nebu.NodeRegistration.register_with_gateway() end)`
  - [x] Add `:inets` to `extra_applications` in `core/apps/event_dispatcher/mix.exs`
  - [x] Add `NEBU_GATEWAY_INTERNAL_URL` env var to `core` service in `docker-compose.yml`

- [x] Task 6: Validate (AC: all)
  - [x] Run `make test-unit-go` — all tests pass
  - [x] Run `make dev` and confirm `docker compose ps` shows all 4 services running
  - [x] Verify `POST http://localhost:8008/internal/nodes/register` with correct PSK → 200
  - [x] Verify `POST http://localhost:8008/internal/nodes/register` with wrong PSK → 401
  - [x] Verify `GET http://localhost:8008/internal/nodes` after core startup → JSON list with entry

## Dev Notes

### Critical: This Story Starts the HTTP Server

The `select {}` in `gateway/cmd/gateway/main.go` (line 49) is the placeholder for the HTTP server start. This story replaces it. The comment says "replaced by http.ListenAndServe in Story 1.11" — that was written before Story 1.10 was scoped. **Story 1.10 starts the HTTP server on `:8008`; Story 1.11 adds `/health` and `/ready` endpoints to the same mux.**

Replace `select {}` with:
```go
slog.Info("HTTP server starting", "addr", ":8008")
if err := http.ListenAndServe(":8008", mux); err != nil {
    slog.Error("HTTP server failed", "err", err)
    os.Exit(1)
}
```

### PSK Loading: File Read at Startup

Read the PSK once at startup, not on every request:

```go
// In main.go after config load:
pskBytes, err := os.ReadFile(cfg.InternalSecretFile)
if err != nil {
    slog.Error("failed to read internal secret file", "path", cfg.InternalSecretFile, "err", err)
    os.Exit(1)
}
internalSecret := strings.TrimSpace(string(pskBytes))
```

**`strings.TrimSpace` is mandatory** — `openssl rand -hex 32` produces a 32-byte hex string with a trailing newline. Comparison without trimming will always fail.

### PSK Middleware: Constant-Time Comparison

Use `crypto/subtle.ConstantTimeCompare` to prevent timing attacks:

```go
// gateway/internal/middleware/psk.go
package middleware

import (
    "crypto/subtle"
    "net/http"
)

func PSKMiddleware(secret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            expected := "Bearer " + secret
            if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) != 1 {
                w.WriteHeader(http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**NEVER** return an error message in the 401 body — AC #2 requires no leaking of internal details.

### Go HTTP Routing: Go 1.22+ Pattern Matching

This project uses Go 1.26+. Use the new `http.NewServeMux()` method-specific routing:

```go
// In main.go
mux := http.NewServeMux()
reg := registry.New()
regHandler := registry.NewHandler(reg)
pskHandler := middleware.PSKMiddleware(internalSecret)(regHandler)

mux.Handle("POST /internal/nodes/register", pskHandler)
mux.Handle("GET /internal/nodes", pskHandler)
```

Do NOT use third-party routers (chi, gorilla/mux) — they are not in the project and not in the architecture for MVP.

### Node Registry: Thread-Safe In-Memory Store

```go
// gateway/internal/registry/registry.go
package registry

import (
    "sync"
    "time"
)

type NodeEntry struct {
    Addr         string    `json:"addr"`
    RegisteredAt time.Time `json:"registered_at"`
}

type Registry struct {
    mu    sync.RWMutex
    nodes map[string]NodeEntry
}

func New() *Registry {
    return &Registry{nodes: make(map[string]NodeEntry)}
}

func (r *Registry) Register(addr string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.nodes[addr] = NodeEntry{Addr: addr, RegisteredAt: time.Now().UTC()}
}

func (r *Registry) List() []NodeEntry {
    r.mu.RLock()
    defer r.mu.RUnlock()
    entries := make([]NodeEntry, 0, len(r.nodes))
    for _, e := range r.nodes {
        entries = append(entries, e)
    }
    return entries
}
```

### Registry Handler: Addr Extraction

For the `POST /internal/nodes/register` handler, extract the caller's address:

```go
// Use r.RemoteAddr as the node address (strips port if needed)
// In Docker Compose the core reaches gateway at its container IP
addr := r.RemoteAddr
```

The handler returns `{"status": "registered"}` (JSON) with `Content-Type: application/json`. Use `encoding/json` (standard library, no deps).

### Elixir: `:httpc` Usage Pattern

`:httpc` is part of Erlang/OTP's `:inets` application. It must be started before use:

```elixir
defmodule Nebu.NodeRegistration do
  require Logger

  @max_retries 5
  @retry_delay_ms 2_000

  def register_with_gateway(retries_left \\ @max_retries) do
    gateway_url = System.get_env("NEBU_GATEWAY_INTERNAL_URL", "http://gateway:8008")
    secret_file = System.get_env("NEBU_INTERNAL_SECRET_FILE")

    with {:ok, psk} <- read_psk(secret_file),
         :ok <- do_register(gateway_url, String.trim(psk)) do
      Logger.info("Registered with gateway")
    else
      {:error, reason} when retries_left > 0 ->
        Logger.warning("Gateway registration failed: #{reason}, retrying (#{retries_left} left)")
        Process.sleep(@retry_delay_ms)
        register_with_gateway(retries_left - 1)

      {:error, reason} ->
        Logger.error("Gateway registration failed after retries: #{reason}")
    end
  end

  defp read_psk(nil), do: {:error, "NEBU_INTERNAL_SECRET_FILE not set"}
  defp read_psk(path) do
    case File.read(path) do
      {:ok, content} -> {:ok, content}
      {:error, reason} -> {:error, "cannot read PSK file: #{reason}"}
    end
  end

  defp do_register(gateway_url, psk) do
    url = String.to_charlist("#{gateway_url}/internal/nodes/register")
    headers = [{'authorization', String.to_charlist("Bearer #{psk}")}]
    request = {url, headers, 'application/json', '{}'}

    case :httpc.request(:post, request, [{:timeout, 5_000}], []) do
      {:ok, {{_, 200, _}, _, _}} -> :ok
      {:ok, {{_, status, _}, _, _}} -> {:error, "unexpected status: #{status}"}
      {:error, reason} -> {:error, inspect(reason)}
    end
  end
end
```

**Important:** `:inets` must be in `extra_applications` in `core/apps/event_dispatcher/mix.exs`:
```elixir
def application do
  [
    extra_applications: [:logger, :inets],  # Add :inets here
    mod: {Nebu.Event.Application, []}
  ]
end
```

### Elixir: Application Startup Pattern

Start registration as a fire-and-forget Task (does NOT block supervisor startup):

```elixir
# core/apps/event_dispatcher/lib/nebu/event/application.ex
def start(_type, _args) do
  children = [
    {GRPC.Server.Supervisor, endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}
  ]

  opts = [strategy: :one_for_one, name: Nebu.Event.Supervisor]
  result = Supervisor.start_link(children, opts)

  # Fire-and-forget: does not block or crash supervisor on failure
  Task.start(fn -> Nebu.NodeRegistration.register_with_gateway() end)

  result
end
```

**Use `Task.start/1` (not `Task.start_link/1`)** — start_link would crash the supervisor if the task fails. Registration failure must not crash the core application (AC #4: "continuing startup").

### Docker Compose: Add Core Env Var

Add to `core` service `environment:` in `docker-compose.yml`:
```yaml
NEBU_GATEWAY_INTERNAL_URL: "http://gateway:8008"
```

The `core` service already has `NEBU_INTERNAL_SECRET_FILE: "/run/secrets/internal_secret"` from Story 1.9 — do NOT change that.

### Go Testing Patterns (from existing tests)

Follow the patterns in `gateway/internal/grpc/client_test.go`:
- Standard `testing` package, no test framework
- Table-driven tests with `t.Run`
- `t.Helper()` in setup helpers
- `t.Fatalf` / `t.Errorf` for assertions
- No mocks — test the actual struct behavior

Example for PSK middleware test:
```go
func TestPSKMiddleware_ValidToken(t *testing.T) {
    secret := "test-secret"
    called := false
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
    })
    handler := PSKMiddleware(secret)(next)
    req := httptest.NewRequest("POST", "/internal/test", nil)
    req.Header.Set("Authorization", "Bearer test-secret")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
    if !called { t.Error("expected next handler to be called") }
    if rr.Code != http.StatusOK { t.Errorf("got %d, want 200", rr.Code) }
}
```

### What Already Exists — Do NOT Change

- `gateway/internal/config/config.go` — `InternalSecretFile` field already defined — use it as-is
- `core/apps/event_dispatcher/lib/nebu/event/application.ex` — modify ONLY to add `Task.start` after `Supervisor.start_link`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` — gRPC server — do NOT touch
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — gRPC handlers — do NOT touch
- `docker-compose.yml` — add ONLY `NEBU_GATEWAY_INTERNAL_URL` to `core` environment — do NOT restructure

### Project Structure Notes

**New files to create:**
```
gateway/internal/middleware/
  psk.go                        ← PSK validation middleware
  psk_test.go                   ← unit tests

gateway/internal/registry/
  registry.go                   ← thread-safe node registry
  handler.go                    ← HTTP handlers for /internal/nodes/*
  registry_test.go              ← unit tests

core/apps/event_dispatcher/lib/nebu/
  node_registration.ex          ← registration with retry logic

core/apps/event_dispatcher/test/nebu/
  node_registration_test.exs    ← ExUnit tests
```

**Files to modify:**
```
gateway/cmd/gateway/main.go                           ← replace select{} with HTTP server
core/apps/event_dispatcher/lib/nebu/event/application.ex  ← add Task.start
core/apps/event_dispatcher/mix.exs                   ← add :inets to extra_applications
docker-compose.yml                                   ← add NEBU_GATEWAY_INTERNAL_URL to core
```

**Files NOT to touch:**
- `gateway/internal/config/config.go` — complete as-is
- `gateway/internal/grpc/` — gRPC client — not in scope
- `gateway/internal/db/` — database layer — not in scope
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/` — gRPC server — not in scope
- Any other Elixir app modules (room_manager, session_manager, etc.)
- `Makefile` — no changes needed

### Go Module Path

The Go module path is `github.com/nebu/nebu` (from `gateway/go.mod`). New packages follow the pattern:
- `github.com/nebu/nebu/internal/middleware`
- `github.com/nebu/nebu/internal/registry`

### Build Validation

```bash
make test-unit-go    # runs go test ./... in container
make dev             # docker compose up — verifies Elixir registration on startup
```

Check gateway logs for: `"HTTP server starting" addr=:8008`
Check core logs for: `"Registered with gateway"` or `"Gateway registration failed after retries"`

### References

- PSK file loading pattern: [Source: architecture.md — Code Patterns: Secret Handling, lines 858-864]
- Node registration security model: [Source: architecture.md — V3 — Elixir Node-Registrierung: Security-Modell]
- Registry directory: [Source: architecture.md — Middleware and Directory Structure — `registry/registry.go`]
- PSK never from env var: [Source: gateway/internal/config/config.go — comment on InternalSecretFile]
- Go HTTP routing: [Source: Go 1.22+ stdlib — method-specific ServeMux patterns]
- Story 1.9 learnings: [Source: 1-9-docker-compose-stack-make-setup-psk-node-security.md — Dev Notes]
- `:inets` startup: [Source: Erlang/OTP documentation — `:httpc` module]
- `Task.start` vs `Task.start_link`: [Source: Elixir docs — Task module]
- NEBU_GATEWAY_INTERNAL_URL: [Source: epics.md — Story 1.10 AC #3]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

None — implementation followed Dev Notes exactly; no blockers encountered.

### Completion Notes List

- Implemented PSK middleware using `crypto/subtle.ConstantTimeCompare`; 401 returns empty body (AC #2 satisfied)
- Node Registry is thread-safe via `sync.RWMutex`; `Register` upserts by addr key (deduplication)
- HTTP server starts on `:8008` with method-specific Go 1.22+ `ServeMux` routing
- `main.go` reads PSK once at startup with `strings.TrimSpace` to handle trailing newlines from `openssl rand`
- Elixir `Nebu.NodeRegistration` uses `:httpc` (`:inets`) — zero new Hex deps
- `Task.start/1` (not `start_link`) ensures registration failure cannot crash the supervisor tree
- Integration validated: gateway logs `HTTP server starting addr=:8008`; core logs `Registered with gateway`
- All Go tests: 7 packages pass; all Elixir tests: 4 tests pass (0 failures)

### File List

gateway/internal/middleware/psk.go
gateway/internal/middleware/psk_test.go
gateway/internal/registry/registry.go
gateway/internal/registry/handler.go
gateway/internal/registry/registry_test.go
gateway/cmd/gateway/main.go
core/apps/event_dispatcher/lib/nebu/node_registration.ex
core/apps/event_dispatcher/test/nebu/node_registration_test.exs
core/apps/event_dispatcher/lib/nebu/event/application.ex
core/apps/event_dispatcher/mix.exs
docker-compose.yml

## Change Log

- 2026-03-23: Implemented Story 1.10 — Go PSK middleware, node registry, HTTP server start; Elixir node registration with retry logic and application startup hook; docker-compose.yml env var addition. All ACs validated end-to-end.
- 2026-03-23: Code Review (AI) — 0 HIGH, 1 MEDIUM, 3 LOW findings. Fixed MEDIUM (non-retriable errors no longer trigger retry loop in node_registration.ex) and LOW #1 (RemoteAddr port stripping in handler.go). Remaining LOW issues (double-mux routing, test assertion depth) accepted for MVP. Status → done.

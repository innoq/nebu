# Story 1.12: Health Endpoint — Elixir Core

Status: done

## Story

As an operator,
I want a health endpoint on the Elixir core,
so that Docker Compose and the Go gateway can monitor core liveness and component status.

## Acceptance Criteria

1. **GET :4000/health — UP state:** When all components are healthy → `200 OK` with body:
   ```json
   {"status":"UP","load_factor":1.0,"version":"0.1.0","node":"nonode@nohost","components":{"database":{"status":"UP"},"room_registry":{"status":"UP","room_count":0},"event_bus":{"status":"UP","connected_gateways":0}}}
   ```
   `Content-Type: application/json`

2. **GET :4000/health — DEGRADED state:** When a non-critical component is degraded → `200 OK` with `"status":"DEGRADED"` (not 503).

3. **GET :4000/health — DOWN state:** When a critical component is down → `503 Service Unavailable` with `"status":"DOWN"`.

4. **load_factor:** Always returns `1.0` in MVP (Phase 2: real adaptive calculation based on scheduler utilization).

5. **docker-compose.yml healthcheck** for `core` service added:
   ```yaml
   healthcheck:
     test: ["CMD", "curl", "-f", "http://localhost:4000/health"]
     interval: 10s
     timeout: 5s
     retries: 3
     start_period: 30s
   ```
   Docker reports core as `healthy` after startup.

6. **Response time:** ≤200ms under normal load (NFR-O2).

## Tasks / Subtasks

- [x] Add `jason` as direct dep in `event_dispatcher/mix.exs` (AC: #1)
  - [x] Add `{:jason, "~> 1.4"}` to deps (it's already in mix.lock as transitive dep — just elevate to direct)
- [x] Create `Nebu.Health` module (AC: #1, #2, #3, #4)
  - [x] Implement `check/0` returning health map with status, load_factor, version, node, components
  - [x] MVP component checks: database, room_registry, event_bus — all return UP stubs
  - [x] Status logic: UP if all UP; DEGRADED if any DEGRADED; DOWN if any DOWN
  - [x] `load_factor` hardcoded to `1.0`
- [x] Create `Nebu.Health.Server` GenServer (AC: #1, #6)
  - [x] Listen on port 4000 using `:gen_tcp` (`packet: :http`, `reuseaddr: true`)
  - [x] Spawn acceptor loop in `Task.start_link/1` from init
  - [x] Handle each connection in a spawned process
  - [x] Route: `GET /health` → call `Nebu.Health.check()`, encode JSON, respond
  - [x] All other paths → `404 Not Found`
- [x] Integrate `Nebu.Health.Server` into supervisor tree (AC: #1)
  - [x] Add as first child in `Nebu.Event.Application.start/2`
- [x] Add unit tests `test/nebu/health_test.exs` (AC: #1, #2, #3, #4)
  - [x] Test `Nebu.Health.check/0` returns correct shape for UP state
  - [x] Test `load_factor` is always `1.0`
  - [x] Test `version` is `"0.1.0"`
  - [x] Test `node` field is present (string)
  - [x] Test status becomes `"DEGRADED"` when a component is degraded
  - [x] Test status becomes `"DOWN"` when a component is down
- [x] Update `docker-compose.yml` with core healthcheck (AC: #5)

## Dev Notes

### HTTP Server: Zero-Dependency Approach with `:gen_tcp`

The project uses minimal external deps. Port 4000 serves a single health endpoint — a full web framework (Plug, Bandit) is overkill. Use OTP's built-in `:gen_tcp` with `packet: :http` parsing.

**Why `:gen_tcp` over `:inets`/`:httpd`:** The `:inets` httpd requires file-based config and mod_esi callbacks which are more complex than a direct `:gen_tcp` GenServer for a single-endpoint server.

**Critical `:gen_tcp` HTTP parsing:**
```elixir
# :gen_tcp with packet: :http parses the request line automatically
# First recv returns: {:http_request, method, path, vsn}
# e.g. {:http_request, :GET, {:abs_path, "/health"}, {1, 1}}
# Subsequent recvs return: {:http_header, _, field, _, value}
# End of headers: :http_eoh
```

**Implementation location:** All health code lives in the `event_dispatcher` app since that's the app that owns the network-facing runtime (gRPC server, node registration).

**File structure:**
```
core/apps/event_dispatcher/lib/nebu/health/
  server.ex     ← GenServer + :gen_tcp listener
  health.ex     ← Nebu.Health.check/0 with component checks
core/apps/event_dispatcher/test/nebu/
  health_test.exs  ← ExUnit tests for Nebu.Health
```

### Nebu.Health.Server Implementation Pattern

```elixir
defmodule Nebu.Health.Server do
  use GenServer
  require Logger

  @port 4000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    port = Application.get_env(:event_dispatcher, :health_port, @port)
    case :gen_tcp.listen(port, [:binary, packet: :http, active: false, reuseaddr: true]) do
      {:ok, listen_socket} ->
        Task.start_link(fn -> accept(listen_socket) end)
        {:ok, listen_socket}
      {:error, reason} ->
        Logger.error("Health server failed to listen on port #{port}: #{inspect(reason)}")
        {:stop, reason}
    end
  end

  defp accept(listen_socket) do
    case :gen_tcp.accept(listen_socket) do
      {:ok, socket} ->
        spawn(fn -> handle_connection(socket) end)
        accept(listen_socket)
      {:error, :closed} ->
        :ok
    end
  end

  defp handle_connection(socket) do
    case :gen_tcp.recv(socket, 0, 5_000) do
      {:ok, {:http_request, :GET, {:abs_path, "/health"}, _}} ->
        drain_headers(socket)
        health = Nebu.Health.check()
        {status_code, status_text} =
          if health.status == "DOWN", do: {503, "Service Unavailable"}, else: {200, "OK"}
        body = Jason.encode!(health)
        response =
          "HTTP/1.1 #{status_code} #{status_text}\r\n" <>
          "Content-Type: application/json\r\n" <>
          "Content-Length: #{byte_size(body)}\r\n" <>
          "Connection: close\r\n\r\n" <>
          body
        :gen_tcp.send(socket, response)
      _ ->
        :gen_tcp.send(socket, "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
    end
    :gen_tcp.close(socket)
  end

  # Consume all HTTP headers before sending response
  defp drain_headers(socket) do
    case :gen_tcp.recv(socket, 0, 2_000) do
      {:ok, :http_eoh} -> :ok
      {:ok, {:http_header, _, _, _, _}} -> drain_headers(socket)
      _ -> :ok
    end
  end
end
```

### Nebu.Health Implementation Pattern

```elixir
defmodule Nebu.Health do
  @version "0.1.0"

  def check do
    components = %{
      database: check_database(),
      room_registry: check_room_registry(),
      event_bus: check_event_bus()
    }

    %{
      status: overall_status(components),
      load_factor: 1.0,
      version: @version,
      node: to_string(node()),
      components: components
    }
  end

  # MVP stubs — real checks wired in Epic 2/4 stories
  defp check_database, do: %{status: "UP"}
  defp check_room_registry, do: %{status: "UP", room_count: 0}
  defp check_event_bus, do: %{status: "UP", connected_gateways: 0}

  defp overall_status(components) do
    statuses = components |> Map.values() |> Enum.map(& &1.status)
    cond do
      "DOWN" in statuses -> "DOWN"
      "DEGRADED" in statuses -> "DEGRADED"
      true -> "UP"
    end
  end
end
```

### Supervisor Integration

In `core/apps/event_dispatcher/lib/nebu/event/application.ex`, add `Nebu.Health.Server` as the first child:

```elixir
children = [
  Nebu.Health.Server,
  {GRPC.Server.Supervisor, endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}
]
```

### event_dispatcher/mix.exs — Add Jason as Direct Dep

Jason is already in `mix.lock` (transitive dep of grpc). Add it as a direct dependency:

```elixir
defp deps do
  [
    {:grpc, "~> 0.8"},
    {:jason, "~> 1.4"}   # JSON encoding for health endpoint; already in mix.lock
  ]
end
```

**Do NOT run `mix deps.get`** — jason is already fetched. The lock file already has the resolved version (`1.4.4`).

### Test Pattern

Tests should test `Nebu.Health` directly (pure functions, no HTTP needed):

```elixir
defmodule Nebu.HealthTest do
  use ExUnit.Case, async: true

  describe "check/0" do
    test "returns UP status when all components healthy" do
      health = Nebu.Health.check()
      assert health.status == "UP"
      assert health.load_factor == 1.0
      assert health.version == "0.1.0"
      assert is_binary(health.node)
      assert health.components.database.status == "UP"
      assert health.components.room_registry.status == "UP"
      assert health.components.room_registry.room_count == 0
      assert health.components.event_bus.status == "UP"
      assert health.components.event_bus.connected_gateways == 0
    end

    test "load_factor is always 1.0 in MVP" do
      assert Nebu.Health.check().load_factor == 1.0
    end
  end
end
```

**Testing DEGRADED/DOWN states:** The MVP stubs always return UP. To test DEGRADED/DOWN paths, test `overall_status/1` if made public or accessible, OR structure `Nebu.Health` so component checks are injectable. Simplest approach: make the status logic testable by passing component map directly in a separate helper function.

### docker-compose.yml Change

Add to the `core` service (already has port `4000:4000`):

```yaml
core:
  ...
  ports:
    - "9000:9000"
    - "4000:4000"
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:4000/health"]
    interval: 10s
    timeout: 5s
    retries: 3
    start_period: 30s
```

The `core` Dockerfile uses `alpine:3.23` runtime — `curl` is NOT included by default. Add it:

```dockerfile
FROM alpine:3.23 AS runtime
RUN apk add --no-cache libstdc++ openssl ncurses curl
```

### Architecture Compliance

- [Source: architecture.md#Health/Readiness] `GET :4000/health` → Liveness + component status
- [Source: architecture.md#Health/Readiness] Response JSON: `status: UP|DEGRADED|DOWN`, `load_factor: 0.0-1.0`, `version`, `node`, `components`
- [Source: architecture.md#Health/Readiness] HTTP 200 for UP/DEGRADED, 503 for DOWN
- [Source: architecture.md#Node-Registrierung] Go gateway polls `GET :4000/health` every 5s to determine core status
- [Source: epics.md#Story 1.12] `load_factor` always `1.0` in MVP; Phase 2 calculates real adaptive value
- [Source: architecture.md#NFR-O2] Health endpoint: ≤200ms response time

### Project Structure Notes

- Health module in `event_dispatcher` app — this is correct. The `event_dispatcher` app owns the network runtime (gRPC on 9000, HTTP health on 4000, node registration)
- Module name: `Nebu.Health` (not `Nebu.Event.Health`) — health is not event-specific, it's a cross-cutting concern
- The `Nebu.Health.Server` module lives in `lib/nebu/health/server.ex`
- The `Nebu.Health` module lives in `lib/nebu/health/health.ex`
- Test: `test/nebu/health_test.exs` (consistent with `test/nebu/node_registration_test.exs` pattern)

### Previous Story Intelligence (from 1-11)

- The Go gateway health endpoint (1-11) served on `:8080` — the Elixir equivalent serves on `:4000`
- Go used a `Handler` struct with `Health()` and `Ready()` methods — Elixir uses `Nebu.Health.check/0` function
- Go's health response has `database` + `core_grpc` components; Elixir's has `database` + `room_registry` + `event_bus`
- The Go gateway maps gRPC state to GRÜN/GELB/ROT; the Elixir core maps component states to UP/DEGRADED/DOWN
- Docker healthcheck for gateway used `/healthcheck` binary; for core uses `curl` (add to Alpine image)
- All tests in 1-11 were table-driven; Elixir equivalent uses `describe`/`test` blocks with `ExUnit.Case, async: true`
- `make test-unit-elixir` runs `mix local.hex --force && mix test` inside the Elixir container

### References

- [Source: epics.md#Story-1.12] Full AC and acceptance scenarios
- [Source: architecture.md#Health-Response-Format] JSON schema and HTTP status mapping
- [Source: architecture.md#Elixir-Core-HTTP] Port 4000 for health endpoint
- [Source: core/apps/event_dispatcher/lib/nebu/node_registration.ex] Established pattern for :inets/:httpc HTTP client; follow same style for HTTP server
- [Source: core/apps/event_dispatcher/lib/nebu/event/application.ex] Supervisor tree where Health.Server must be added as first child
- [Source: docker-compose.yml] Core service already has `4000:4000` port mapping — only healthcheck stanza needs to be added
- [Source: core/Dockerfile] Runtime is `alpine:3.23` — must add `curl` for healthcheck CMD

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None — implementation proceeded without issues.

### Completion Notes List

- Implemented `Nebu.Health` module with `check/0` and public `overall_status/1` for testability
- Implemented `Nebu.Health.Server` GenServer using `:gen_tcp` with HTTP packet parsing on port 4000
- `Nebu.Health.Server` registered as first child in `Nebu.Event.Application` supervisor tree
- Jason elevated to direct dependency in `event_dispatcher/mix.exs` (was already in mix.lock as transitive dep of grpc)
- Added `curl` to Alpine runtime image in `core/Dockerfile` for Docker healthcheck
- Added `healthcheck` stanza to `core` service in `docker-compose.yml`
- 12 new unit tests covering: UP/DEGRADED/DOWN status logic, shape validation, load_factor, version, node field
- All 16 event_dispatcher tests pass (0 failures, 0 regressions)

### File List

- `core/apps/event_dispatcher/mix.exs` (modified — added jason dep)
- `core/apps/event_dispatcher/lib/nebu/health/health.ex` (new)
- `core/apps/event_dispatcher/lib/nebu/health/server.ex` (new)
- `core/apps/event_dispatcher/lib/nebu/event/application.ex` (modified — added Health.Server as first child)
- `core/apps/event_dispatcher/test/nebu/health_test.exs` (new)
- `docker-compose.yml` (modified — added core healthcheck stanza)
- `core/Dockerfile` (modified — added curl to runtime apk install)

## Change Log

- 2026-03-24: Implemented health endpoint for Elixir core (Story 1-12) — Nebu.Health module, Nebu.Health.Server gen_tcp listener on port 4000, supervisor integration, unit tests, docker-compose healthcheck, Dockerfile curl addition.
- 2026-03-24: Code review (AI) — 0 HIGH, 1 MEDIUM, 2 LOW. Fixed: M1 unstaged unrelated gateway/cmd/healthcheck/main.go, L1 added drain_headers for 404 responses, L2 replaced bare spawn with Task.start for connection handlers. All ACs verified as implemented. Status → done.

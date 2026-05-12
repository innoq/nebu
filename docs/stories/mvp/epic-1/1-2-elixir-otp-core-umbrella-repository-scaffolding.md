# Story 1.2: Elixir/OTP Core Umbrella — Repository Scaffolding

Status: done

## Story

As a developer,
I want the Elixir/OTP umbrella project initialized with all 6 sub-applications,
so that Elixir development can begin with a correct supervision tree structure.

## Acceptance Criteria

1. **Given** a fresh `core/` directory, **when** `mix new core --umbrella` is run, **then** `core/mix.exs` exists as an umbrella project configuration

2. **Given** the umbrella exists, **when** 6 apps are created with `--sup` flag (`room_manager`, `session_manager`, `presence`, `event_dispatcher`, `signature`, `permissions`), **then** each app directory exists under `core/apps/` with its own `mix.exs` and supervision tree

3. **Given** all 6 apps exist, **when** `mix compile` is run from `core/`, **then** compilation succeeds with 0 errors and 0 warnings

4. **Given** each app, **when** its `application.ex` is inspected, **then** it defines a `Supervisor` as the root process with `strategy: :one_for_one`

5. **Given** all apps, **when** `mix test` is run from `core/`, **then** all placeholder test suites pass (0 failures)

6. **Given** Elixir modules across all apps, **when** module naming is verified, **then** modules follow the `Nebu.{Domain}.{Name}` pattern (e.g., `Nebu.Room.Manager`, `Nebu.Session.Manager`, `Nebu.Signature`)

## Tasks / Subtasks

- [x] Task 1: Initialize Elixir/OTP umbrella project (AC: #1)
  - [x] Create `core/` directory structure via `mix new core --umbrella`
  - [x] Verify `core/mix.exs` exists as umbrella config
  - [x] Create `core/config/config.exs`, `core/config/dev.exs`, `core/config/prod.exs`, `core/config/runtime.exs`

- [x] Task 2: Create all 6 sub-applications with supervision trees (AC: #2, #4, #6)
  - [x] `room_manager` app — module root `Nebu.Room`
  - [x] `session_manager` app — module root `Nebu.Session`
  - [x] `presence` app — module root `Nebu.Presence`
  - [x] `event_dispatcher` app — module root `Nebu.Event`
  - [x] `signature` app — module root `Nebu.Signature`
  - [x] `permissions` app — module root `Nebu.Permissions`
  - [x] Each app's `application.ex` uses `Supervisor` with `strategy: :one_for_one`

- [x] Task 3: Apply Nebu module naming convention (AC: #6)
  - [x] Rename default `RoomManager` → `Nebu.Room.Manager` (and `Application` → `Nebu.Room.Application`)
  - [x] Rename default `SessionManager` → `Nebu.Session.Manager`
  - [x] Rename default `Presence` → `Nebu.Presence` (Application → `Nebu.Presence.Application`)
  - [x] Rename default `EventDispatcher` → `Nebu.Event.Dispatcher`
  - [x] Rename default `Signature` → `Nebu.Signature` (Application → `Nebu.Signature.Application`)
  - [x] Rename default `Permissions` → `Nebu.Permissions` (Application → `Nebu.Permissions.Application`)

- [x] Task 4: Create core Dockerfile (multi-stage) (needed for `build-core` Makefile target)
  - [x] Stage 1 `builder`: `elixir:1.19-alpine`
  - [x] Stage 2 `runtime`: `alpine:3.19` (with libstdc++, openssl, ncurses)
  - [x] Output: OTP release via `mix release`

- [x] Task 5: Update root Makefile — fill in `build-core` and `test-unit-elixir` targets (AC: #3, #5)
  - [x] `build-core` target: `$(DOCKER_ELIXIR) sh -c "cd core && mix deps.get && mix compile"`
  - [x] `test-unit-elixir` target: `$(DOCKER_ELIXIR) sh -c "cd core && mix test"`
  - [x] Verify: `make -n build-core` and `make -n test-unit-elixir` do not error

- [x] Task 6: Verify compilation and tests pass (AC: #3, #5)
  - [x] `mix compile` from `core/` returns 0 errors, 0 warnings
  - [x] `mix test` from `core/` returns 0 failures

## Dev Notes

### CRITICAL: How to Generate the Umbrella Structure

**Do NOT run mix locally** — all commands run inside Docker containers (hard requirement from Story 1.1, no local tooling required). The structure must be created manually by writing files directly:

```
core/
├── apps/
│   ├── room_manager/
│   │   ├── lib/nebu/room/
│   │   │   └── application.ex   ← Nebu.Room.Application
│   │   ├── test/
│   │   │   └── nebu_room_test.exs
│   │   └── mix.exs
│   ├── session_manager/
│   │   ├── lib/nebu/session/
│   │   │   └── application.ex   ← Nebu.Session.Application
│   │   ├── test/
│   │   │   └── nebu_session_test.exs
│   │   └── mix.exs
│   ├── presence/
│   │   ├── lib/nebu/presence/
│   │   │   └── application.ex   ← Nebu.Presence.Application
│   │   ├── test/
│   │   │   └── nebu_presence_test.exs
│   │   └── mix.exs
│   ├── event_dispatcher/
│   │   ├── lib/nebu/event/
│   │   │   └── application.ex   ← Nebu.Event.Application
│   │   ├── test/
│   │   │   └── nebu_event_test.exs
│   │   └── mix.exs
│   ├── signature/
│   │   ├── lib/nebu/
│   │   │   └── application.ex   ← Nebu.Signature.Application
│   │   ├── test/
│   │   │   └── nebu_signature_test.exs
│   │   └── mix.exs
│   └── permissions/
│       ├── lib/nebu/permissions/
│       │   └── application.ex   ← Nebu.Permissions.Application
│       ├── test/
│       │   └── nebu_permissions_test.exs
│       └── mix.exs
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── prod.exs
│   └── runtime.exs
├── mix.exs               ← Umbrella root
├── mix.lock              ← Empty initially (no deps yet)
└── Dockerfile
```

### Module Naming Convention (MANDATORY)

**Pattern:** `Nebu.{Domain}.{Name}` — This is a hard requirement from the architecture.

| App Directory | Application Module | Primary Module |
|---|---|---|
| `room_manager` | `Nebu.Room.Application` | `Nebu.Room.Manager` |
| `session_manager` | `Nebu.Session.Application` | `Nebu.Session.Manager` |
| `presence` | `Nebu.Presence.Application` | `Nebu.Presence` |
| `event_dispatcher` | `Nebu.Event.Application` | `Nebu.Event.Dispatcher` |
| `signature` | `Nebu.Signature.Application` | `Nebu.Signature` |
| `permissions` | `Nebu.Permissions.Application` | `Nebu.Permissions` |

**NEVER** use the auto-generated default names (`RoomManager`, `SessionManager`, `EventDispatcher`) — they violate the `Nebu.*` namespace pattern.

[Source: architecture.md, lines 482-505]

### Application.ex Pattern (MANDATORY for each app)

Each `application.ex` MUST define a `Supervisor` with `strategy: :one_for_one`:

```elixir
defmodule Nebu.Room.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — Room GenServer processes added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Room.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

**Same pattern applies to all 6 apps** — only module names and supervisor names differ.
[Source: architecture.md, lines 113-125]

### Umbrella Root mix.exs

```elixir
defmodule Nebu.MixProject do
  use Mix.Project

  def project do
    [
      apps_path: "apps",
      version: "0.1.0",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  defp deps do
    []
    # Dependencies added in subsequent stories:
    # Story 1.3: ecto_sql, postgrex (database)
    # Story 1.6: grpc, protobuf (gRPC)
    # Story 4.x: horde, libcluster (clustering)
  end
end
```

**NO dependencies yet** — this is scaffolding only. Dependencies will be added story-by-story.

### Per-App mix.exs Pattern

```elixir
defmodule Nebu.Room.MixProject do
  use Mix.Project

  def project do
    [
      app: :room_manager,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {Nebu.Room.Application, []}
    ]
  end

  defp deps do
    []
  end
end
```

**Key:** `build_path`, `config_path`, `deps_path`, `lockfile` all point to umbrella root (`../../`). This is standard Elixir umbrella structure.

### Config Files

**`core/config/config.exs`** — shared config (import_config for env-specific):
```elixir
import Config

import_config "#{config_env()}.exs"
```

**`core/config/dev.exs`**:
```elixir
import Config

config :logger, level: :debug
```

**`core/config/prod.exs`**:
```elixir
import Config

config :logger, level: :info
```

**`core/config/runtime.exs`** — NEBU_* env vars read at startup:
```elixir
import Config

# Placeholder — NEBU_* env vars added as each story requires them
# e.g., Story 1.3 adds database URL:
# config :room_manager, Nebu.Repo,
#   url: System.get_env("NEBU_DB_URL") || raise "NEBU_DB_URL not set"
```

[Source: architecture.md, lines 1037-1041]

### Elixir Logging Pattern

Use `Logger` with keyword metadata — NEVER use `IO.puts` or plain strings:

```elixir
Logger.info("app started", app: :room_manager)
Logger.warning("service degraded", reason: "dependency unavailable")
Logger.error("startup failed", error: inspect(err))
```

Log levels:
- `DEBUG`: development only, never credentials or PII
- `INFO`: normal operations
- `WARNING`: degraded state, retry, fallback
- `ERROR`: unexpected failures

[Source: architecture.md, lines 722-743]

### Core Dockerfile

```dockerfile
# core/Dockerfile
FROM elixir:1.19-alpine AS builder
WORKDIR /app
RUN mix local.hex --force && mix local.rebar --force
COPY mix.exs mix.lock ./
COPY apps/*/mix.exs apps/
RUN mix deps.get --only prod
COPY . .
RUN MIX_ENV=prod mix release

FROM alpine:3.19 AS runtime
RUN apk add --no-cache libstdc++ openssl ncurses
COPY --from=builder /app/_build/prod/rel/nebu ./
ENTRYPOINT ["./bin/nebu", "start"]
```

**NOTE:** `mix release` requires a `releases` config in the umbrella `mix.exs` for Phase 2. For now, the Dockerfile is sufficient as a scaffold — the actual release config is added when Docker Compose is wired in Story 1.9.
[Source: architecture.md, lines 1168-1182]

### Makefile Updates (Mandatory)

Story 1.1 created **stub/placeholder** Makefile targets. Story 1.2 MUST fill in the Elixir-specific ones:

Existing variable from Story 1.1:
```makefile
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
```

Update these targets:
```makefile
build-core:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix compile"

test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix test"
```

**DO NOT** modify `build-gateway`, `test-unit-go`, `gen-api`, `proto`, `dev`, `setup`, `test-integration` — those are owned by other stories.

### Placeholder Test Pattern

Each app gets a minimal placeholder test that passes:

```elixir
# core/apps/room_manager/test/nebu_room_test.exs
defmodule Nebu.RoomTest do
  use ExUnit.Case

  test "placeholder: room_manager app starts" do
    assert :ok == :ok
  end
end
```

Pattern: `use ExUnit.Case`, single `test` block with trivial assertion. Tests will be replaced with real unit tests in Epic 4 stories.

### Previous Story Intelligence (Story 1.1)

From Story 1.1 (done), the following is already in place — DO NOT recreate:

- Root `Makefile` with all 9 targets already defined (including `build-core` and `test-unit-elixir` as stubs)
- `DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine` variable already defined in Makefile
- `proto/` directory already exists with `.gitkeep`
- `gateway/go.mod` (module `github.com/nebu/nebu`), `media/go.mod` already in place
- `.gitignore` exists (added during Story 1.1 code review)

**Build system pattern:** All Makefile commands use Docker containers — no local tool installation required. The `DOCKER_ELIXIR` variable is already defined in the Makefile.

### Architecture Decision — 6 Apps (not 7)

The epics AC specifies exactly 6 apps. The architecture detailed structure (lines 994-1051) also shows a `nebu_db` app for shared Ecto Repo. **For this story, implement only the 6 apps per the AC.** The `nebu_db` app will be evaluated when Ecto/PostgreSQL is introduced in Story 1.3.

### Elixir Coding Conventions

| Item | Convention | Example |
|---|---|---|
| Module names | `Nebu.{Domain}.{Name}` | `Nebu.Room.Manager`, `Nebu.Signature` |
| Functions | `snake_case` | `send_event/2`, `validate_token/1` |
| Variables | `snake_case` | `room_id`, `sender_id` |
| Module files | `snake_case.ex` | `room_manager.ex`, `event_dispatcher.ex` |

[Source: architecture.md, lines 482-505]

### Project Structure Notes

- `core/` directory does NOT yet exist — must be created from scratch
- `proto/` directory exists with `.gitkeep` (from Story 1.1) — leave untouched
- `core/` is a sibling to `gateway/` and `media/` at the repo root
- This story does NOT add gRPC dependencies — those come in Story 1.6
- This story does NOT add Ecto/PostgreSQL — those come in Story 1.3
- `mix.lock` starts empty (no dependencies)

### References

- Elixir/OTP umbrella structure: [Source: architecture.md, lines 994-1051]
- Core Dockerfile: [Source: architecture.md, lines 1168-1182]
- Module naming convention: [Source: architecture.md, lines 482-505]
- Logging pattern: [Source: architecture.md, lines 722-743]
- Makefile build targets: [Source: architecture.md, lines 1128-1149]
- Config files: [Source: architecture.md, lines 1037-1041]
- ADR-001 (Elixir/OTP): [Source: architecture.md, ADR section]
- ADR-004 (Horde): [Source: architecture.md, ADR section]
- Epic 1 overview: [Source: epics.md, Epic 1 section]
- Story 1.2 AC: [Source: epics.md, lines 262-293]
- Mandatory build rules: [Source: CLAUDE.md, Commands section]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- `config/test.exs` was missing — `mix test` (MIX_ENV=test) requires it. Added with `config :logger, level: :warning`.

### Completion Notes List

- Created Elixir/OTP umbrella manually (no `mix new` locally — Docker-only build system).
- All 6 apps scaffolded: `room_manager`, `session_manager`, `presence`, `event_dispatcher`, `signature`, `permissions`.
- All module names follow `Nebu.{Domain}.{Name}` pattern from architecture.
- Each `application.ex` uses `Supervisor` with `strategy: :one_for_one` and named supervisor.
- `core/config/test.exs` added (required for `mix test`, not mentioned in story but standard Elixir requirement).
- `make build-core` → `mix compile`: 0 errors, 0 warnings (verified in Docker container).
- `make test-unit-elixir` → `mix test`: 6 tests, 0 failures (verified in Docker container).
- Makefile `build-core` changed from `docker build` to `$(DOCKER_ELIXIR) mix compile` per Dev Notes.

### File List

- `core/mix.exs`
- `core/mix.lock`
- `core/Dockerfile`
- `core/config/config.exs`
- `core/config/dev.exs`
- `core/config/prod.exs`
- `core/config/runtime.exs`
- `core/config/test.exs`
- `core/apps/room_manager/mix.exs`
- `core/apps/room_manager/lib/nebu/room/application.ex`
- `core/apps/room_manager/lib/nebu/room/manager.ex`
- `core/apps/room_manager/test/test_helper.exs`
- `core/apps/room_manager/test/nebu_room_test.exs`
- `core/apps/session_manager/mix.exs`
- `core/apps/session_manager/lib/nebu/session/application.ex`
- `core/apps/session_manager/lib/nebu/session/manager.ex`
- `core/apps/session_manager/test/test_helper.exs`
- `core/apps/session_manager/test/nebu_session_test.exs`
- `core/apps/presence/mix.exs`
- `core/apps/presence/lib/nebu/presence/application.ex`
- `core/apps/presence/lib/nebu/presence.ex`
- `core/apps/presence/test/test_helper.exs`
- `core/apps/presence/test/nebu_presence_test.exs`
- `core/apps/event_dispatcher/mix.exs`
- `core/apps/event_dispatcher/lib/nebu/event/application.ex`
- `core/apps/event_dispatcher/lib/nebu/event/dispatcher.ex`
- `core/apps/event_dispatcher/test/test_helper.exs`
- `core/apps/event_dispatcher/test/nebu_event_test.exs`
- `core/apps/signature/mix.exs`
- `core/apps/signature/lib/nebu/signature/application.ex`
- `core/apps/signature/lib/nebu/signature.ex`
- `core/apps/signature/test/test_helper.exs`
- `core/apps/signature/test/nebu_signature_test.exs`
- `core/apps/permissions/mix.exs`
- `core/apps/permissions/lib/nebu/permissions/application.ex`
- `core/apps/permissions/lib/nebu/permissions.ex`
- `core/apps/permissions/test/test_helper.exs`
- `core/apps/permissions/test/nebu_permissions_test.exs`
- `Makefile` (updated `build-core` and `test-unit-elixir` targets)
- `.gitignore` (added Elixir build artifact patterns)

## Change Log

- 2026-03-20: Implemented Story 1.2 — Elixir/OTP umbrella scaffolding. Created `core/` with 6 sub-apps (room_manager, session_manager, presence, event_dispatcher, signature, permissions), all configs, Dockerfile, and placeholder tests. Updated Makefile build-core/test-unit-elixir targets. All 6 tests pass, 0 compile warnings.
- 2026-03-20: Code Review fixes applied — (1) Added Elixir patterns to `.gitignore` (`_build/`, `deps/`, `*.beam`, etc.), (2) Moved `signature/lib/nebu/application.ex` → `signature/lib/nebu/signature/application.ex` for consistency with all other apps, (3) Fixed Dockerfile `COPY apps/*/mix.exs apps/` glob flattening bug — replaced with per-app COPY statements. All 6 tests still pass, 0 compile warnings.

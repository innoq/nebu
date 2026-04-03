# Story 4.1: Horde Registry + DynamicSupervisor Setup

Status: done

## Story

As a Core developer,
I want Horde Registry and DynamicSupervisor configured in the `room_manager` OTP application,
so that Room GenServers can be started, found, and recovered across a clustered Elixir node topology.

## Acceptance Criteria

1. `core/apps/room_manager/mix.exs` declares `{:horde, "~> 0.9"}` as a dependency.
2. `Nebu.Room.Application` starts two supervised children: `Horde.Registry` (name: `Nebu.Room.Registry`) and `Horde.DynamicSupervisor` (name: `Nebu.Room.Supervisor`, strategy: `:one_for_one`).
3. Both are configured with `members: :auto` so they automatically join all nodes in the libcluster topology.
4. A `Nebu.Room.RoomSupervisor` module exposes:
   - `start_room(room_id)` — calls `Horde.DynamicSupervisor.start_child/2` with a `{Nebu.Room.Server, room_id}` child spec; the child registers itself under `Nebu.Room.Registry` with key `room_id`.
   - `lookup_room(room_id)` — calls `Horde.Registry.lookup(Nebu.Room.Registry, room_id)`; returns `{:ok, pid}` or `{:error, :not_found}`.
5. A placeholder `Nebu.Room.Server` module exists (stub GenServer — no business logic yet; full implementation in Story 4-2) that registers itself in `Nebu.Room.Registry` on `init/1`.
6. `mix test` passes with `--warnings-as-errors`; a unit test verifies `start_room/1` registers the PID in `Nebu.Room.Registry` and `lookup_room/1` returns `{:ok, pid}`.
7. The placeholder test in `test/nebu_room_test.exs` is replaced by real tests.

## Tasks / Subtasks

- [x] Add Horde dependency to `core/apps/room_manager/mix.exs` (AC: #1)
  - [x] Add `{:horde, "~> 0.9"}` to `defp deps do` list
  - [x] Run `mix deps.get` inside the container to update `core/mix.lock`

- [x] Update `Nebu.Room.Application` to start Horde children (AC: #2, #3)
  - [x] Replace placeholder comment in `core/apps/room_manager/lib/nebu/room/application.ex`
  - [x] Add `{Horde.Registry, [name: Nebu.Room.Registry, keys: :unique, members: :auto]}` as first child
  - [x] Add `{Horde.DynamicSupervisor, [name: Nebu.Room.Supervisor, strategy: :one_for_one, members: :auto]}` as second child
  - [x] Keep root supervisor name as `Nebu.Room.Supervisor` — **CONFLICT**: the `Nebu.Room.Supervisor` name is used for both the OTP Supervisor and the Horde DynamicSupervisor; use `Nebu.Room.HordeSupervisor` for Horde to avoid collision

- [x] Create `Nebu.Room.RoomSupervisor` module (AC: #4)
  - [x] Create `core/apps/room_manager/lib/nebu/room/room_supervisor.ex`
  - [x] Implement `start_room/1` calling `Horde.DynamicSupervisor.start_child(Nebu.Room.HordeSupervisor, {Nebu.Room.Server, room_id})`
  - [x] Implement `lookup_room/1` calling `Horde.Registry.lookup(Nebu.Room.Registry, room_id)`; return `{:ok, pid}` or `{:error, :not_found}`

- [x] Create placeholder `Nebu.Room.Server` GenServer (AC: #5)
  - [x] Create `core/apps/room_manager/lib/nebu/room/server.ex`
  - [x] Implement as `GenServer` with `use GenServer`
  - [x] `init/1` receives `room_id`; registers via `{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}`
  - [x] Return `{:ok, %{room_id: room_id}}` from `init/1` — no DB calls yet (Story 4-2)
  - [x] `child_spec/1` must return spec with `id: room_id` (not module default) so multiple rooms can coexist under Horde

- [x] Write unit tests (AC: #6, #7)
  - [x] Replace `test/nebu_room_test.exs` placeholder test
  - [x] Test: `start_room("test-room-1")` returns `{:ok, pid}`
  - [x] Test: `lookup_room("test-room-1")` returns `{:ok, pid}` after start
  - [x] Test: `lookup_room("nonexistent-room")` returns `{:error, :not_found}`
  - [x] Use `ExUnit.Case, async: false` — Horde uses global named processes

- [x] Run `mix test --warnings-as-errors` (AC: #6)
  - [x] Verify all tests pass; fix any compiler warnings

## Dev Notes

### Critical: Module Naming Convention

**The epics.md acceptance criteria uses `RoomManager.Application`, `RoomManager.Registry` etc. — this is WRONG for this codebase.**

All existing modules follow `Nebu.{Domain}.{Name}` pattern:
- `Nebu.Room.Application` (already exists at `core/apps/room_manager/lib/nebu/room/application.ex`)
- `Nebu.Session.Manager`, `Nebu.Session.Application` (session_manager app)
- `Nebu.Signature.Application` (signature app)

**Use these names:**
| Epic Spec Name | Correct Name (this codebase) |
|---|---|
| `RoomManager.Application` | `Nebu.Room.Application` (already exists) |
| `RoomManager.Registry` | `Nebu.Room.Registry` |
| `RoomManager.Supervisor` | `Nebu.Room.HordeSupervisor` (Horde DS; avoid collision with OTP root supervisor) |
| `RoomManager.RoomSupervisor` | `Nebu.Room.RoomSupervisor` |
| `RoomManager.RoomServer` | `Nebu.Room.Server` |

The OTP root supervisor (`Supervisor.start_link`) already uses `Nebu.Room.Supervisor` in the existing `application.ex`. The Horde DynamicSupervisor must use a different name — use `Nebu.Room.HordeSupervisor`.

### Architecture Directory Mapping

Per architecture.md, the expected file layout for room_manager is:
```
core/apps/room_manager/lib/nebu/room/
  ├── application.ex    ← Nebu.Room.Application (EXISTS — update it)
  ├── manager.ex        ← Nebu.Room.Manager (EXISTS as stub — becomes Horde.DynamicSupervisor wrapper or repurpose as RoomSupervisor)
  ├── server.ex         ← Nebu.Room.Server (Room GenServer — CREATE stub in this story)
  └── (power_level.ex)  ← Story 4-13
```

**Note:** `manager.ex` already exists as a stub. The architecture spec says `manager.ex` is the `Horde.DynamicSupervisor`. You can either:
- Repurpose `manager.ex` as `Nebu.Room.Manager` containing the `start_room/1`/`lookup_room/1` API (functionally equivalent to `RoomSupervisor`), or
- Create a new `room_supervisor.ex` and keep `manager.ex` for future high-level room management operations.

**Recommendation:** Repurpose `manager.ex` as `Nebu.Room.Manager` — it already exists and avoids an extra file. This aligns with the architecture tree (`manager.ex ← Horde.DynamicSupervisor`).

### Horde Dependency

**Version:** `{:horde, "~> 0.9"}` — as specified in epics.md.

Horde 0.9.x API (confirmed stable):
- `Horde.Registry` — distributed registry using CRDTs; `keys: :unique` for Room GenServers
- `Horde.DynamicSupervisor` — distributed supervisor
- Both use `members: :auto` for libcluster auto-discovery (Phase 2 clustering requires zero code change)
- Child registration via `{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}`
- `Horde.Registry.lookup(name, key)` returns `[{pid, value}]` (list) — extract `pid` from first element; empty list = not found

**`child_spec` with unique ID:** The `Nebu.Room.Server` must override `child_spec/1` with `id: {Nebu.Room.Server, room_id}` — otherwise all Room GenServers would have the same child id (`Nebu.Room.Server`) and Horde would refuse to start a second one.

### Test Strategy

Horde uses named global processes. Tests must:
- `use ExUnit.Case, async: false` — no async parallelism with named Horde processes
- Start Horde in test application supervision tree OR start it manually per test with `start_supervised/1`
- Horde starts as part of `room_manager` app — if the app is started in `test_helper.exs` this works automatically

The existing `test_helper.exs` only calls `ExUnit.start()` — you may need to ensure the application starts. Check if umbrella test config starts apps.

**Minimal test approach:** Use `start_supervised/1` in setup to start Horde explicitly in test context, avoiding dependency on app startup order.

```elixir
setup do
  start_supervised!({Horde.Registry, [name: Nebu.Room.Registry, keys: :unique, members: :auto]})
  start_supervised!({Horde.DynamicSupervisor, [name: Nebu.Room.HordeSupervisor, strategy: :one_for_one, members: :auto]})
  :ok
end
```

### `mix.lock` Update

Adding `:horde` will update `core/mix.lock`. The Horde 0.9.x dependency tree includes:
- `:delta_crdt` (CRDT implementation) — will be added automatically

Run `mix deps.get` and commit the updated `mix.lock`.

### Existing Placeholder to Replace

`core/apps/room_manager/test/nebu_room_test.exs` currently has:
```elixir
test "placeholder: room_manager app starts" do
  assert :ok == :ok
end
```
Replace this entirely with real Horde integration tests.

### What This Story Does NOT Include

- Room state management (Story 4-2)
- PostgreSQL DB calls in `init/1` (Story 4-2)
- Ed25519 signing (Story 4-3 / 4-4)
- ETS TxnDedup table (Story 4-4)
- Any gRPC integration (Story 4-8)

The `Nebu.Room.Server` created here is a **stub** — it only registers itself in Horde Registry and holds `%{room_id: room_id}` state. Story 4-2 adds real lifecycle logic.

### Elixir Conventions (from CLAUDE.md)

- GenServer state: always via `handle_*` callbacks, never directly
- Errors: let it crash + Supervisor, no defensive `try/rescue` in GenServer
- No process registration without via-tuple or Registry — use `{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}`
- Supervisor strategies: `one_for_one` default

### Build Command

```bash
make test-unit-elixir   # runs: mix test in container with --warnings-as-errors
```

All tests run inside Docker containers — no local Elixir install needed.

### Project Structure Notes

- `core/apps/room_manager/` — target app for all changes in this story
- `core/mix.lock` — will be updated by `mix deps.get`; commit this change
- Do NOT modify: `core/mix.exs` (umbrella root deps), other apps, gateway code

### References

- Story 4.1 acceptance criteria: [Source: _bmad-output/planning-artifacts/epics.md#Story 4.1]
- Architecture Horde decision (G3): [Source: _bmad-output/planning-artifacts/architecture.md#G3 — Room-Autorität: Horde]
- Architecture directory layout: [Source: _bmad-output/planning-artifacts/architecture.md#room_manager/]
- Module naming convention `Nebu.{Domain}.{Name}`: [Source: _bmad-output/planning-artifacts/architecture.md#Naming Conventions > Elixir]
- Pinned versions (Elixir 1.19, OTP 27): [Source: _bmad-output/planning-artifacts/architecture.md#Pinned Versions]
- Elixir conventions: [Source: CLAUDE.md#Elixir Conventions]
- Existing `Nebu.Room.Application`: [Source: core/apps/room_manager/lib/nebu/room/application.ex]
- Existing `Nebu.Room.Manager` stub: [Source: core/apps/room_manager/lib/nebu/room/manager.ex]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

- First test run failed with `{:already_started, pid}` because `Nebu.Room.Application` already starts Horde on app boot via the umbrella test runner. Resolved by removing `start_supervised!` calls from test `setup` block — tests now run against the app-started Horde instances directly.

### Completion Notes List

- Added `{:horde, "~> 0.9"}` to `room_manager/mix.exs`; resolved to Horde 0.10.0 with delta_crdt 0.6.5, libring 1.7.0, merkle_map 0.2.2 — all added to `core/mix.lock`
- `Nebu.Room.Application` updated to start `Horde.Registry` (name: `Nebu.Room.Registry`) and `Horde.DynamicSupervisor` (name: `Nebu.Room.HordeSupervisor`) as supervised children with `members: :auto`; root OTP supervisor retains name `Nebu.Room.Supervisor` (no collision)
- Created `Nebu.Room.RoomSupervisor` in `room_supervisor.ex` with `start_room/1` and `lookup_room/1` implementing the AC-specified API
- Repurposed `Nebu.Room.Manager` in `manager.ex` as a thin facade delegating to `RoomSupervisor` via `defdelegate` — keeps the architecture-prescribed file in place while satisfying AC #4
- Created `Nebu.Room.Server` stub GenServer in `server.ex`; overrides `child_spec/1` with `id: {__MODULE__, room_id}` for unique Horde child ids; registers via `{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}`
- Replaced placeholder test with 6 real ExUnit tests covering `start_room/1`, `lookup_room/1` (found + not_found), idempotent start, and `Manager` delegates; all pass with `--warnings-as-errors`, exit 0

### File List

- `core/apps/room_manager/mix.exs` — modified: added `{:horde, "~> 0.9"}` dependency
- `core/mix.lock` — modified: added horde 0.10.0, delta_crdt 0.6.5, libring 1.7.0, merkle_map 0.2.2
- `core/apps/room_manager/lib/nebu/room/application.ex` — modified: added Horde.Registry and Horde.DynamicSupervisor children
- `core/apps/room_manager/lib/nebu/room/room_supervisor.ex` — created: Nebu.Room.RoomSupervisor with start_room/1 and lookup_room/1
- `core/apps/room_manager/lib/nebu/room/manager.ex` — modified: repurposed as thin facade delegating to RoomSupervisor
- `core/apps/room_manager/lib/nebu/room/server.ex` — created: Nebu.Room.Server stub GenServer
- `core/apps/room_manager/test/nebu_room_test.exs` — modified: replaced placeholder test with 6 real Horde integration tests

### Review Findings

- [x] [Review][Patch] Test isolation: Room processes not cleaned up after tests; fixed with unique IDs + `on_exit` cleanup [nebu_room_test.exs]
- [x] [Review][Dismiss] lookup_room pattern-match does not cover >1 results — not a real risk with `keys: :unique`
- [x] [Review][Dismiss] Architecture discrepancy room_supervisor.ex vs manager.ex — intentional, documented
- [x] [Review][Dismiss] Horde 0.10.0 resolved instead of 0.9.x — correct per semver
- [x] [Review][Dismiss] Story file verbose — consistent with project standard

## Change Log

- 2026-04-03: Code review passed (0 MAJOR, 1 MINOR fixed); test isolation improved with unique room IDs + on_exit cleanup
- 2026-04-03: Implemented Story 4-1 — added Horde dependency, updated Application to start Registry + DynamicSupervisor, created RoomSupervisor API, created Server stub GenServer, replaced placeholder test with 6 passing unit tests; `mix test --warnings-as-errors` exits 0

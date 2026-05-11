# Story 4.7: Presence Manager

Status: review

## Story

As a Core developer,
I want a Presence Manager that tracks and broadcasts user online/offline status,
so that Matrix clients can display accurate presence indicators.

## Acceptance Criteria

1. `core/apps/presence/lib/nebu/presence/manager.ex` implements `Nebu.Presence.Manager` as a `GenServer`:
   - State: ETS table `NebuPresence` with entries `{user_id, status, last_active_at}` where `status ∈ [:online, :offline, :unavailable]`
   - `set_presence(user_id, status)` — upserts the ETS entry; broadcasts `{:presence_update, user_id, status}` via `:pg` Process Group `"presence"`
   - `get_presence(user_id)` → `{:ok, %{status: atom(), last_active_at: integer()}}` or `{:error, :not_found}`; missing users default to `:offline` (i.e., return `{:ok, %{status: :offline, last_active_at: nil}}`)
   - Heartbeat: if no `set_presence` call for a user within 60 seconds, auto-transition to `:unavailable`; after 5 minutes with no activity, transition to `:offline`

2. ETS table `NebuPresence` is created in `Nebu.Presence.Application.start/2` BEFORE `Supervisor.start_link/2`:
   - Type `:set`, access `:public`
   - Guard: `if :ets.whereis(:NebuPresence) == :undefined do :ets.new(...) end`
   - The `:pg` process group scope is started in `Application.start/2` (same pattern as `Nebu.Room.Application`)
   - Table is owned by Application process (NOT by the GenServer) so data survives GenServer crashes/restarts

3. `Nebu.Presence.Application` starts `Nebu.Presence.Manager` as a supervised worker under `Nebu.Presence.Supervisor`

4. Unit tests in `core/apps/presence/test/nebu/presence/manager_test.exs` cover:
   - `set_presence/2` online → `get_presence/1` returns `{:ok, %{status: :online, last_active_at: integer()}}`
   - `get_presence/1` on missing user returns `{:ok, %{status: :offline, last_active_at: nil}}`
   - Heartbeat expiry: mock timeout triggers transition from `:online` → `:unavailable` → `:offline`
   - Crash/restart: `Process.exit(pid, :kill)` → ETS data survives (supervisor restarts GenServer, table still exists)
   - `:pg` broadcast: after `set_presence/2`, a subscriber to group `"presence"` receives `{:presence_update, user_id, status}`

5. `mix test --warnings-as-errors` passes for the full umbrella. All existing Story 4-1 through 4-6 tests pass unchanged.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. set_presence online → get returns online — ExUnit**
- Given: `NebuPresence` ETS table is empty for `"@kai:nebu.local"`
- When: `Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)` is called
- Then: `Nebu.Presence.Manager.get_presence("@kai:nebu.local")` returns `{:ok, %{status: :online, last_active_at: last_active_at}}` where `last_active_at` is an integer millisecond timestamp

**2. Missing user defaults to offline — ExUnit**
- Given: no presence entry exists for `"@unknown:nebu.local"` in ETS
- When: `Nebu.Presence.Manager.get_presence("@unknown:nebu.local")` is called
- Then: returns `{:ok, %{status: :offline, last_active_at: nil}}`

**3. Heartbeat: mock timeout triggers unavailable transition — ExUnit**
- Given: `set_presence("@kai:nebu.local", :online)` was called (user is online)
- When: a `:check_heartbeats` message is sent directly to the GenServer via `Process.send/3`, simulating the 60-second timer firing; the `last_active_at` in ETS is backdated to be >60 seconds ago
- Then: `get_presence("@kai:nebu.local")` returns `{:ok, %{status: :unavailable, last_active_at: _}}`

**4. Heartbeat: unavailable → offline after 5 minutes — ExUnit**
- Given: user is in `:unavailable` state with `last_active_at` more than 5 minutes ago (300 seconds)
- When: a `:check_heartbeats` message is sent to the GenServer
- Then: `get_presence("@kai:nebu.local")` returns `{:ok, %{status: :offline, last_active_at: _}}`

**5. Crash/Restart: ETS data survives GenServer crash — ExUnit (mandatory for ETS-state stories)**
- Given: `set_presence("@kai:nebu.local", :online)` called; `Nebu.Presence.Manager` is a running supervised GenServer
- When: `Process.exit(pid, :kill)` is called on the GenServer PID
- Then: the Supervisor restarts the GenServer; `get_presence("@kai:nebu.local")` still returns `{:ok, %{status: :online, last_active_at: _}}` (ETS owned by Application process, not the GenServer)

**6. :pg broadcast on set_presence — ExUnit**
- Given: the test process has joined the `:pg` Process Group `"presence"` via `:pg.join("presence", self())`
- When: `Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)` is called
- Then: the test process receives message `{:presence_update, "@kai:nebu.local", :online}` (via `:pg.send/2` or direct message from broadcast)

---

## Tasks / Subtasks

- [x] Write failing unit tests FIRST (ATDD gate — tests must be red before any implementation)
  - [x] Create `core/apps/presence/test/nebu/presence/manager_test.exs`
  - [x] Write 6 test cases as described in Acceptance Tests section
  - [x] Run tests — verify they FAIL (red phase)

- [x] Create ETS table + :pg scope in `Nebu.Presence.Application` (AC: #2)
  - [x] Modify `core/apps/presence/lib/nebu/presence/application.ex`
  - [x] Add `NebuPresence` ETS creation with guard pattern before `Supervisor.start_link/2`
  - [x] Add `:pg.start_link()` with `{:error, {:already_started, _}}` guard
  - [x] Add `Nebu.Presence.Manager` to children list

- [x] Create `Nebu.Presence.Manager` GenServer (AC: #1)
  - [x] Create `core/apps/presence/lib/nebu/presence/manager.ex`
  - [x] Implement `start_link/1` with `name: __MODULE__` registration
  - [x] Implement `init/1` with `Process.send_after(self(), :check_heartbeats, 60_000)` to start heartbeat timer
  - [x] Implement `set_presence/2` public API (GenServer.cast for non-blocking upsert)
  - [x] Implement `get_presence/1` public API (direct ETS lookup, no GenServer.call overhead)
  - [x] Implement `handle_cast({:set_presence, user_id, status}, state)` — upserts ETS, broadcasts via `:pg`
  - [x] Implement `handle_info(:check_heartbeats, state)` — scans ETS, transitions stale entries, reschedules timer
  - [x] Add `@spec` for all public functions

- [x] Implement until all tests pass (AC: #1–#4)

- [x] Run `mix test --warnings-as-errors` for full umbrella (AC: #5)
  - [x] Confirm existing room_manager tests (4-1 through 4-4) pass unchanged
  - [x] Confirm existing session_manager tests (4-5 through 4-6) pass unchanged

---

## Dev Notes

### CRITICAL: Module Naming — epics.md vs. Codebase Reality

The `epics.md` acceptance criteria reference `Presence.Manager` and `Presence.Application`. These are WRONG for this codebase. All modules follow the `Nebu.{Domain}.{Name}` convention without exception:

| Epics.md Spec | Correct Name in Codebase |
|---|---|
| `Presence.Manager` | `Nebu.Presence.Manager` |
| `Presence.Application` | `Nebu.Presence.Application` (already exists — placeholder) |

This convention is established by `Nebu.Room.*`, `Nebu.Session.*`, `Nebu.Signature.*`.

### File Locations

```
core/apps/presence/
  lib/nebu/presence/
    application.ex                        ← MODIFY (add ETS + :pg + Manager child)
    manager.ex                            ← CREATE (new GenServer)
  lib/nebu/
    presence.ex                           ← DO NOT MODIFY (top-level placeholder)
  test/nebu/presence/
    manager_test.exs                      ← CREATE (new test file)
  test/
    test_helper.exs                       ← DO NOT MODIFY
    nebu_presence_test.exs                ← DO NOT MODIFY (keep placeholder test)
  mix.exs                                 ← DO NOT MODIFY (no new deps needed)
```

**Do NOT create:**
- `core/apps/presence/lib/presence/manager.ex` (wrong path — epics.md convention, not codebase convention)
- Any `Presence.Manager` module (missing `Nebu.` prefix)

### ETS Table: `NebuPresence`

**Creation pattern in `Nebu.Presence.Application.start/2` (BEFORE `Supervisor.start_link/2`):**

```elixir
# Guard prevents ArgumentError if Application restarts in same VM (test reloads, hot-code)
if :ets.whereis(:NebuPresence) == :undefined do
  :ets.new(:NebuPresence, [:named_table, :set, :public])
end
```

Table is **owned by the Application process**, NOT by `Nebu.Presence.Manager`. This is the mandatory pattern established in Story 4-4 (`NebuTxnDedup`) and Story 4-5 (`NebuSessions`). If the Manager GenServer crashes and restarts, the ETS data is preserved.

**ETS key structure:**
```
Key:   user_id :: String.t()          (e.g. "@kai:nebu.local")
Value: {status, last_active_at}
       status          :: :online | :offline | :unavailable
       last_active_at  :: integer() (BIGINT ms since epoch) | nil
```

**ETS entry stored as tuple:**
```elixir
# Insert/upsert:
:ets.insert(:NebuPresence, {user_id, status, last_active_at})

# Lookup:
case :ets.lookup(:NebuPresence, user_id) do
  [{^user_id, status, last_active_at}] -> {:ok, %{status: status, last_active_at: last_active_at}}
  [] -> {:ok, %{status: :offline, last_active_at: nil}}
end
```

**IMPORTANT:** `get_presence/1` returns `{:ok, ...}` for missing users (defaults to `:offline`) — NOT `{:error, :not_found}`. This is the spec from epics.md ("missing users default to offline"). This is different from `EtsStore.get_session/1` which returns `{:error, :not_found}` for missing keys.

### :pg Process Groups — Broadcast Pattern

`:pg` is an OTP built-in (OTP 23+) — no external dependency. The `:pg` scope must be started before use.

**Start in `Application.start/2` (same as `Nebu.Room.Application`):**
```elixir
case :pg.start_link() do
  {:ok, _pid} -> :ok
  {:error, {:already_started, _pid}} -> :ok
end
```

**Broadcast in `handle_cast({:set_presence, user_id, status}, state)`:**
```elixir
# Broadcast presence update to all subscribers in the "presence" group
members = :pg.get_members("presence")
Enum.each(members, fn pid ->
  send(pid, {:presence_update, user_id, status})
end)
```

**Subscribe in tests (for AC #6 test):**
```elixir
# In test setup:
:pg.join("presence", self())

# After set_presence call:
assert_receive {:presence_update, "@kai:nebu.local", :online}, 500
```

**Why `:pg` (not Phoenix.PubSub, not GenEvent):** ADR-002 mandates pg Process Groups as the Pub/Sub mechanism — no NATS, no Redis, no Phoenix.PubSub dependency. `:pg` is OTP built-in and sufficient for intra-node broadcast in MVP.

### Heartbeat Timer Pattern

The Manager uses `Process.send_after/3` for a self-scheduling heartbeat check:

```elixir
# In init/1:
@impl true
def init(:ok) do
  Process.send_after(self(), :check_heartbeats, heartbeat_interval())
  {:ok, :no_state}
end

# heartbeat_interval/0 reads from Application env for testability:
defp heartbeat_interval do
  Application.get_env(:presence, :heartbeat_interval_ms, 60_000)
end

# In handle_info:
@impl true
def handle_info(:check_heartbeats, state) do
  now_ms = System.system_time(:millisecond)
  unavailable_threshold_ms = Application.get_env(:presence, :unavailable_threshold_ms, 60_000)
  offline_threshold_ms = Application.get_env(:presence, :offline_threshold_ms, 300_000)

  :ets.tab2list(:NebuPresence)
  |> Enum.each(fn {user_id, status, last_active_at} ->
    age_ms = now_ms - last_active_at

    new_status =
      cond do
        status == :online and age_ms >= unavailable_threshold_ms -> :unavailable
        status == :unavailable and age_ms >= offline_threshold_ms -> :offline
        true -> status
      end

    if new_status != status do
      :ets.insert(:NebuPresence, {user_id, new_status, last_active_at})
      # Optionally broadcast the auto-transition
    end
  end)

  # Reschedule
  Process.send_after(self(), :check_heartbeats, heartbeat_interval())
  {:noreply, state}
end
```

**Config injection for testability (Story 4-6 pattern):** Use `Application.get_env` for all timeout values. In tests, override via `Application.put_env(:presence, :heartbeat_interval_ms, 100)` to run fast.

**Heartbeat test strategy:** Rather than waiting 60 real seconds, send `:check_heartbeats` directly to the GenServer and manually backdate the `last_active_at` in ETS before the send:

```elixir
test "heartbeat: online → unavailable after 60s" do
  :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)

  # Backdate last_active_at by 61 seconds
  now_ms = System.system_time(:millisecond)
  stale_ts = now_ms - 61_000
  :ets.insert(:NebuPresence, {"@kai:nebu.local", :online, stale_ts})

  # Override unavailable threshold for test
  Application.put_env(:presence, :unavailable_threshold_ms, 60_000)

  # Trigger heartbeat check synchronously
  send(Process.whereis(Nebu.Presence.Manager), :check_heartbeats)

  # Give GenServer time to process
  Process.sleep(50)

  assert {:ok, %{status: :unavailable}} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
end
```

### `set_presence/2` Implementation Options

**Option A (GenServer.cast — recommended):** Non-blocking. Client does not wait for ETS write. Suitable for presence updates which are fire-and-forget:

```elixir
@spec set_presence(String.t(), :online | :offline | :unavailable) :: :ok
def set_presence(user_id, status) do
  GenServer.cast(__MODULE__, {:set_presence, user_id, status})
end
```

**Option B (direct ETS write in public function):** Skips GenServer for the ETS write but must still call GenServer for `:pg` broadcast. This splits responsibilities — avoid.

**Use Option A.** The GenServer handles both the ETS write AND the `:pg` broadcast atomically (within one message).

### `get_presence/1` Implementation

`get_presence/1` reads ETS **directly** without `GenServer.call/2`. This is consistent with `EtsStore.get_session/1` (Story 4-5 pattern) — ETS with `:public` access allows any process to read without message passing:

```elixir
@spec get_presence(String.t()) :: {:ok, %{status: atom(), last_active_at: integer() | nil}}
def get_presence(user_id) do
  case :ets.lookup(:NebuPresence, user_id) do
    [{^user_id, status, last_active_at}] -> {:ok, %{status: status, last_active_at: last_active_at}}
    [] -> {:ok, %{status: :offline, last_active_at: nil}}
  end
end
```

Note: this function does NOT need to be a GenServer callback. It's a plain public function on the module.

### Crash/Restart Test Pattern

Follow the exact pattern established in Story 4-5:

```elixir
test "ETS data survives Manager GenServer crash and supervisor restart" do
  :ok = Nebu.Presence.Manager.set_presence("@crash_test:nebu.local", :online)
  assert {:ok, %{status: :online}} = Nebu.Presence.Manager.get_presence("@crash_test:nebu.local")

  # Kill the Manager process
  pid = Process.whereis(Nebu.Presence.Manager)
  assert pid != nil
  Process.exit(pid, :kill)

  # Wait for supervisor to restart
  Process.sleep(50)

  # ETS data must survive — table owned by Application process, not GenServer
  assert {:ok, %{status: :online}} = Nebu.Presence.Manager.get_presence("@crash_test:nebu.local")
end
```

For `Process.whereis(Nebu.Presence.Manager)` to work, start with `name: __MODULE__`:

```elixir
def start_link(opts \\ []) do
  GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
end
```

### Test Setup and Isolation

```elixir
defmodule Nebu.Presence.ManagerTest do
  use ExUnit.Case, async: false  # async: false — shared named ETS + :pg scope

  setup do
    # Clear all ETS entries between tests for isolation
    # Do NOT call :ets.delete/1 — that destroys the table entirely
    if :ets.whereis(:NebuPresence) != :undefined do
      :ets.delete_all_objects(:NebuPresence)
    end
    :ok
  end
end
```

**`async: false` is REQUIRED** because:
1. `:NebuPresence` is a named global ETS table — concurrent test writes cause race conditions
2. `:pg` group membership is process-global — concurrent test subscribes interfere

### Updated `Nebu.Presence.Application`

```elixir
defmodule Nebu.Presence.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # ETS table for presence state.
    # Created here (owned by Application process) so it survives
    # Nebu.Presence.Manager GenServer crashes/restarts.
    # Type :set auto-upserts on same key. Access :public allows any process.
    if :ets.whereis(:NebuPresence) == :undefined do
      :ets.new(:NebuPresence, [:named_table, :set, :public])
    end

    # Start :pg scope for presence broadcast (ADR-002, ADR-005).
    # :pg is OTP built-in (OTP 23+) — no external dependency.
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
    end

    children = [
      Nebu.Presence.Manager
    ]

    opts = [strategy: :one_for_one, name: Nebu.Presence.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Architecture Compliance Rules

| Rule | Requirement | Source |
|---|---|---|
| Rule #1 | Timestamps as BIGINT (milliseconds) — `last_active_at` as `integer()`, NOT `DateTime.t()` | `architecture.md#Enforcement rule #1` |
| Rule #6 | `{:ok, result}` / `{:error, reason}` — no raise/throw for business logic | `architecture.md#Enforcement rule #6` |
| ADR-002 | No Redis, no NATS — ETS for state, `:pg` Process Groups for broadcast | `architecture.md#ADR-002` |
| ETS ownership | Table created in Application.start/2 (NOT in GenServer.init/1) — crash safety | Story 4-4, 4-5 precedent |
| Naming | All modules follow `Nebu.Presence.*` — ignore `Presence.*` in epics.md | Code convention |
| async: false | Required for presence tests — shared ETS + :pg global state | Story 4-5 precedent |
| Config injection | All timeouts via `Application.get_env(:presence, :key, default)` for testability | Story 4-6 pattern |

### Timestamps: Integer Milliseconds (NOT DateTime)

Architecture rule #1 mandates BIGINT timestamps. In Elixir, use:
```elixir
System.system_time(:millisecond)  # returns integer ms since epoch
```

`last_active_at` MUST be `integer()` (milliseconds), NOT `%DateTime{}` or any struct.

### No Database / No Migration Needed

Presence state is **volatile ETS only** — no PostgreSQL persistence. There is no migration required for this story. Presence state is lost on Application restart (acceptable for presence — clients re-announce their status on reconnect). This is consistent with the epics.md spec which specifies only ETS state.

### What Story 4-7 Does NOT Implement

- No gRPC handler for presence (that is Story 4-8 `SetPresence` RPC)
- No `GET /_matrix/client/v3/presence/{userId}/status` Go handler (that is Story 4-18)
- No PostgreSQL persistence for presence state (volatile ETS is the spec)
- No cross-node presence synchronization (single-node MVP; clustering is Phase 2)
- No `Nebu.Presence.Manager` behaviour/adapter split (unlike `PgStore` — presence is ETS-only, no DB injection needed)

### mix.exs Dependencies

No new dependencies are needed. `:pg` is OTP built-in (OTP 23+). `:crypto` is OTP built-in. The `presence` app currently has empty deps — this story adds no new entries.

### Build & Test Commands

```bash
# Run presence tests only (fast, targeted):
make test-unit-elixir

# Run full umbrella (before marking complete):
make test-unit-elixir
```

All tests run inside Docker containers — no local Elixir install needed.

**Expected result after implementation:**
- All existing Story 4-1 through 4-6 tests pass (no regression)
- New Manager tests pass (6 test cases: online round-trip, offline default, unavailable heartbeat, offline heartbeat, crash/restart, :pg broadcast)
- 0 failures, 0 warnings

---

## Previous Story Intelligence (Stories 4-5, 4-6)

Key learnings from Stories 4-5 and 4-6 that directly impact Story 4-7:

1. **ETS guard pattern is mandatory** — Always use `if :ets.whereis(:NebuPresence) == :undefined` before creating the table. Story 4-4 code review found a MAJOR: missing guard caused crash on Application restart in same VM.

2. **ETS owned by Application, NOT GenServer** — The ETS table must be created in `Application.start/2` before `Supervisor.start_link/2`. If created inside `GenServer.init/1`, the data would be lost on every GenServer crash.

3. **`delete_all_objects` vs `delete` in tests** — Use `:ets.delete_all_objects(:NebuPresence)` in `setup` (wipes entries, table survives). NEVER use `:ets.delete(:NebuPresence)` (destroys the table, next lookup crashes).

4. **`async: false` is required** — All presence tests share a named ETS table and `:pg` group memberships. Concurrent tests would cause flaky failures.

5. **Config injection for testability** — Story 4-6 established the pattern: read timeout values from `Application.get_env(:presence, :key, default)` in runtime functions. Override in tests via `Application.put_env/3`. Do NOT hardcode timeout constants in function bodies.

6. **Module naming is `Nebu.Presence.*`** — epics.md uses `Presence.Manager`, codebase uses `Nebu.Presence.Manager`. This is consistent with all other epic 4 stories.

7. **`:pg` is already used in `Nebu.Room.Application`** — Copy the exact `case :pg.start_link() do` guard pattern from `core/apps/room_manager/lib/nebu/room/application.ex`. Do NOT invent a new guard style.

8. **Crash/restart test requires named registration** — `Process.whereis(Nebu.Presence.Manager)` only works if the GenServer is registered with `name: __MODULE__`. Use `Keyword.put_new(opts, :name, __MODULE__)` in `start_link/1`.

---

## Architecture References

- `_bmad-output/planning-artifacts/architecture.md` — Enforcement rule #1 (BIGINT timestamps), Rule #6 (no raise), ADR-002 (ETS + pg Process Groups)
- `_bmad-output/planning-artifacts/epics.md` — Story 4.7 Acceptance Criteria (line ~1956)
- `core/apps/presence/lib/nebu/presence/application.ex` — Current placeholder to extend
- `core/apps/presence/lib/nebu/presence.ex` — Top-level placeholder, DO NOT MODIFY
- `core/apps/room_manager/lib/nebu/room/application.ex` — `:pg.start_link()` guard pattern to replicate exactly
- `core/apps/session_manager/lib/nebu/session/ets_store.ex` — Direct ETS public API pattern (no GenServer.call in hot-path)
- `core/apps/session_manager/lib/nebu/session/application.ex` — ETS creation guard pattern reference (Story 4-5)
- `core/apps/room_manager/test/nebu_room_test.exs` — Crash/restart test pattern (Story 4-4)
- `core/apps/session_manager/test/nebu/session/ets_store_test.exs` — ETS crash/restart + delete_all_objects pattern (Story 4-5)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m] (2026-04-03)

### Completion Notes List

- Implemented `Nebu.Presence.Application.start/2`: creates `:NebuPresence` ETS table with idempotency guard before `Supervisor.start_link/2`; starts `:pg` scope with `{:error, {:already_started, _}}` guard (identical to `Nebu.Room.Application` pattern); adds `Nebu.Presence.Manager` as supervised child.
- Implemented `Nebu.Presence.Manager` GenServer: `start_link/1` registers with `name: __MODULE__`; `init/1` schedules heartbeat via `Process.send_after`; `set_presence/2` is a non-blocking cast; `get_presence/1` reads ETS directly (no GenServer round-trip); heartbeat thresholds read from `Application.get_env` for testability.
- Test file was already written (ATDD gate satisfied). All 14 tests in `manager_test.exs` pass (14/14), plus all existing app tests pass (22 room_manager, 38 session_manager, 21 signature, 6 event_dispatcher, 1 permissions). Full umbrella: 122 tests, 0 failures, 0 warnings with `--warnings-as-errors`.
- ETS table ownership: owned by Application process — confirmed ETS data survives `Process.exit(pid, :kill)` on the GenServer (crash/restart test green).
- :pg broadcast: `handle_cast({:set_presence, ...})` uses `:pg.get_members("presence")` and `Enum.each(&send(&1, ...))` — confirmed by 3 :pg broadcast tests all passing.
- `mix.exs` and umbrella `core/mix.exs` already included `:presence` — no changes needed.

### File List

Files created or modified:

```
core/apps/presence/lib/nebu/presence/application.ex     ← MODIFIED (added ETS + :pg + Manager child)
core/apps/presence/lib/nebu/presence/manager.ex         ← CREATED (new GenServer)
core/apps/presence/test/nebu/presence/manager_test.exs  ← was already written (ATDD gate)
_bmad-output/implementation-artifacts/4-7-presence-manager.md  ← MODIFIED (this file)
_bmad-output/implementation-artifacts/sprint-status.yaml       ← MODIFIED (status → review)
```

### Change Log

- 2026-04-03: Story 4-7 created — Presence Manager
- 2026-04-03: Story 4-7 implemented — Nebu.Presence.Application (ETS + :pg setup) and Nebu.Presence.Manager (GenServer with heartbeat, :pg broadcast, direct ETS read); 14/14 tests green, full umbrella 0 failures 0 warnings

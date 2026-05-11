---
id: 7-33
type: fix
security_review: not-needed
created: 2026-04-30
sec_gate_ref: _bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md
---

# Story 7.33: Fix server-internal event fanout regression from Story 7-19 IDOR fix

Status: review

## Story

As the Nebu Gateway event fanout goroutine,
I want to call `GetRoomState` with a `system` role so that the Elixir Core recognises the caller as a trusted internal process,
so that events published to a room are correctly fanned out to all members via the MessageBuffer and `/sync` long-poll clients wake up reliably.

## Context / Background

**SEC Gate 2 finding (MEDIUM):** During the epic-7b security review (Kassandra, 2026-04-30), a MEDIUM finding was raised:

> Story 7-19 added a correct membership check to `get_room_state` in `server.ex`. The Go gateway's EventBus fanout goroutine calls the same `GetRoomState` RPC internally (via `coreRoomStateLookup`) WITHOUT setting `coregrpc.WithUserMetadata`. With no `x-user-id` header, the Elixir side gets `caller_id = nil`, the membership check (`MapSet.member?(state.members, nil)`) fails immediately, `permission_denied` is returned, and `RouteEventToUsers` logs a warning and drops every event silently.

**Impact:** Every published message silently disappears from long-poll `/sync` delivery. Clients only see events on cold polls. The regression was introduced together with the correct IDOR fix for 7-19 and affects the production EventBus fanout path.

**Root cause flow:**
1. `gateway/cmd/gateway/main.go:146–149` — the fanout goroutine calls `buffer.RouteEventToUsers(ctx, event, msgBuf, roomLookup)` where `roomLookup` is `&coreRoomStateLookup{client: coreClient}`.
2. `coreRoomStateLookup.GetRoomState` (main.go:56–62) calls `a.client.GetRoomState(ctx, &pb.GetRoomStateRequest{RoomId: roomID})` using the raw context — **no `coregrpc.WithUserMetadata` call**.
3. On the Elixir side, `Nebu.Grpc.Metadata.trusted_identity(stream)` returns `{nil, "user"}` because no `x-user-id` header is present.
4. The membership check `MapSet.member?(state.members, nil)` is false → `permission_denied` raised.
5. `RouteEventToUsers` catches the error, logs a warning, and skips the event.

**Kassandra's recommended fix (chosen approach — option a):** Add a `system_role == "system"` bypass in `get_room_state/2`. When the metadata identifies a system caller (`system_role == "system"`), skip the membership check entirely and return the full member list. This matches the existing `system_role` field already defined in `Nebu.Grpc.Metadata` and propagated via `coregrpc.WithUserMetadata`.

**Source:** [epic-7b-security-review-2026-04-30.md — Finding MEDIUM: "Story 7-19 IDOR fix breaks server-internal event fanout"]

## Acceptance Criteria

1. `get_room_state/2` in `server.ex` — when stream metadata has `x-system-role = "system"` (and `x-user-id` is nil or empty), the membership check is skipped and the full `GetRoomStateResponse` (members + state_events) is returned.

2. `get_room_state/2` — when stream metadata has a real `x-user-id` (non-system caller), the membership check runs exactly as before (Story 7-19 IDOR fix is preserved). Non-members still receive `permission_denied`.

3. `coreRoomStateLookup.GetRoomState` in `gateway/cmd/gateway/main.go` — the function adds `coregrpc.WithUserMetadata(ctx, "", "system")` to its context before calling `a.client.GetRoomState`, so Core receives `x-system-role: system`.

4. An ExUnit test in a new file `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs` verifies: when `get_room_state/2` is called with a stream whose `x-system-role = "system"` and no `x-user-id`, for a room the caller is NOT a member of, the handler returns a `%Core.GetRoomStateResponse{}` (not a `GRPC.RPCError`).

5. An ExUnit test in the same file verifies: when `get_room_state/2` is called with `x-system-role = "user"` and `x-user-id` of a non-member, the handler still raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()` (regression guard for Story 7-19 IDOR fix).

6. `make test-unit-elixir` passes with all new tests green.

7. `make test-unit-go` passes (no regressions; the change to `coreRoomStateLookup.GetRoomState` is a one-liner and covered by existing drain tests via interface).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [get_room_state — system role bypasses membership check] — ExUnit
   - Given: Room `!fanout-sys:test.local` with `@member:test.local` joined. Stream metadata: `x-system-role = "system"`, `x-user-id = nil` (not set). Request has `room_id = "!fanout-sys:test.local"`, no `event_type`.
   - When: `Server.get_room_state/2` is called.
   - Then: returns `%Core.GetRoomStateResponse{}` with `members` containing `"@member:test.local"` — no exception raised.

2. [get_room_state — regular non-member still gets permission_denied] — ExUnit
   - Given: Room `!fanout-perm:test.local` with `@member:test.local` joined. Stream metadata: `x-system-role = "user"`, `x-user-id = "@nonmember:test.local"`.
   - When: `Server.get_room_state/2` is called.
   - Then: raises `GRPC.RPCError` with `status: GRPC.Status.permission_denied()` (7-19 IDOR fix preserved).

## Tasks / Subtasks

- [x] Task 1: Write failing ExUnit tests (AC: #4, #5) — write BEFORE implementing Tasks 2–3
  - [x] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs`
  - [x] Use `FakeRoomDB` ETS pattern (see dev notes below for exact structure)
  - [x] Implement `build_stream/2` helper (same signature as other test files)
  - [x] Write `get_room_state — system role bypasses membership check` (AC #4)
  - [x] Write `get_room_state — non-member still gets permission_denied` (AC #5)
  - [x] Run `make test-unit-elixir` — confirm first test FAILS, second PASSES (red phase for AC #4)

- [x] Task 2: Fix `get_room_state/2` in `server.ex` (AC: #1, #2)
  - [x] After `{caller_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` (line ~719), add system-role bypass before the membership check
  - [x] Bypass: `if system_role != "system" do … membership check … end`
  - [x] Verify handler compiles and the non-system path is unchanged
  - [x] Run `make test-unit-elixir` — confirm both new tests pass (green phase)

- [x] Task 3: Fix `coreRoomStateLookup.GetRoomState` in `gateway/cmd/gateway/main.go` (AC: #3)
  - [x] In `GetRoomState` (line ~56): replace `a.client.GetRoomState(ctx, ...)` with `a.client.GetRoomState(coregrpc.WithUserMetadata(ctx, "", "system"), ...)`
  - [x] Run `make test-unit-go` — confirm no regressions

- [x] Task 4: Verify no regressions (AC: #6, #7)
  - [x] Run `make test-unit-elixir` — all tests green
  - [x] Run `make test-unit-go` — all tests green

## Dev Notes

### Files to modify — exhaustive list

| File | Change | AC |
|---|---|---|
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | Add system-role bypass before membership check in `get_room_state/2` | 1, 2 |
| `gateway/cmd/gateway/main.go` | Add `coregrpc.WithUserMetadata(ctx, "", "system")` in `coreRoomStateLookup.GetRoomState` | 3 |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs` | New ExUnit test file | 4, 5 |

**No proto changes.** No new migrations. No additional Go handler changes.

### Elixir change — exact location in server.ex

Current code at line ~713–735:

```elixir
def get_room_state(request, stream) do
  room_id = request.room_id
  event_type = Map.get(request, :event_type, "")
  state_key = Map.get(request, :state_key, "")
  mod = room_registry_module()

  {caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  state = ...  # get_state or raise not_found

  unless MapSet.member?(state.members, caller_id) do
    raise GRPC.RPCError,
      status: GRPC.Status.permission_denied(),
      message: "#{caller_id} is not a member of #{room_id}"
  end
  ...
end
```

Target code after fix:

```elixir
def get_room_state(request, stream) do
  room_id = request.room_id
  event_type = Map.get(request, :event_type, "")
  state_key = Map.get(request, :state_key, "")
  mod = room_registry_module()

  {caller_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  state = ...  # get_state or raise not_found

  # System-role callers (internal gateway fanout) skip the membership check.
  # User-role callers must be room members (Story 7-19 IDOR fix preserved).
  unless system_role == "system" do
    unless MapSet.member?(state.members, caller_id) do
      raise GRPC.RPCError,
        status: GRPC.Status.permission_denied(),
        message: "#{caller_id} is not a member of #{room_id}"
    end
  end
  ...
end
```

**Important:** Change `_system_role` to `system_role` (remove the underscore) on the `trusted_identity` line — the variable is now used.

### Go change — exact location in main.go

Current code at lines 55–62:

```go
func (a *coreRoomStateLookup) GetRoomState(ctx context.Context, roomID string) ([]string, error) {
	resp, err := a.client.GetRoomState(ctx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		return nil, err
	}
	return resp.GetMembers(), nil
}
```

Target code after fix:

```go
func (a *coreRoomStateLookup) GetRoomState(ctx context.Context, roomID string) ([]string, error) {
	sysCtx := coregrpc.WithUserMetadata(ctx, "", "system")
	resp, err := a.client.GetRoomState(sysCtx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		return nil, err
	}
	return resp.GetMembers(), nil
}
```

`coregrpc` is already imported at line ~31 as `coregrpc "github.com/nebu/nebu/internal/grpc"`. No new import needed.

### ExUnit test file — complete structure

The new test file must follow the exact pattern of `server_moderation_metadata_test.exs` and `server_set_typing_test.exs`:

```elixir
defmodule Nebu.EventDispatcher.ServerGetRoomStateSystemTest do
  use ExUnit.Case, async: false

  alias Nebu.EventDispatcher.Server

  defmodule FakeRoomDB do
    def load_members(room_id) do
      case :ets.lookup(:get_room_state_system_test_db, {:room, room_id}) do
        [] -> {:error, :not_found}
        [{_, created_at_ms}] ->
          members = :ets.match(:get_room_state_system_test_db, {{:member, room_id, :"$1"}, :active})
          pl_json =
            case :ets.lookup(:get_room_state_system_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end
          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end
    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:get_room_state_system_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end
    def insert_member(room_id, user_id) do
      :ets.insert(:get_room_state_system_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end
    def delete_member(room_id, user_id) do
      :ets.delete(:get_room_state_system_test_db, {:member, room_id, user_id})
      :ok
    end
    def insert_event(event) do
      :ets.insert(:get_room_state_system_test_db, {{:event, event["event_id"]}, event})
      :ok
    end
    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:get_room_state_system_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end
  end

  setup do
    if :ets.info(:get_room_state_system_test_db) != :undefined do
      :ets.delete(:get_room_state_system_test_db)
    end
    :ets.new(:get_room_state_system_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeRoomDB)
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      if :ets.info(:get_room_state_system_test_db) != :undefined do
        :ets.delete(:get_room_state_system_test_db)
      end
      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  defp build_stream(user_id, system_role \\ "user") do
    %{http_request_headers: %{"x-user-id" => user_id, "x-system-role" => system_role}}
  end

  defp build_system_stream() do
    # system caller: no x-user-id, system role only
    %{http_request_headers: %{"x-system-role" => "system"}}
  end

  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, _pid} ->
        on_exit(fn ->
          if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
            case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
              {:ok, pid} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
              _ -> :ok
            end
          end
        end)
        :ok
      error -> error
    end
  end

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  describe "get_room_state/2 — system-role bypass" do
    test "system caller (no x-user-id) bypasses membership check and gets response" do
      room_id = "!fanout-sys:test.local"
      :ok = setup_room_with_member(room_id, "@member:test.local")

      request = %Core.GetRoomStateRequest{room_id: room_id}
      response = Server.get_room_state(request, build_system_stream())

      assert %Core.GetRoomStateResponse{} = response
      assert "@member:test.local" in response.members
    end
  end

  describe "get_room_state/2 — non-member still gets permission_denied (7-19 regression guard)" do
    test "regular non-member user receives permission_denied" do
      room_id = "!fanout-perm:test.local"
      :ok = setup_room_with_member(room_id, "@member:test.local")

      request = %Core.GetRoomStateRequest{room_id: room_id}
      stream = build_stream("@nonmember:test.local", "user")

      error =
        try do
          Server.get_room_state(request, stream)
          flunk("expected GRPC.RPCError to be raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied()
    end
  end
end
```

### What must NOT change

- The `event_type` / `state_key` filter logic in `get_room_state/2` — unchanged.
- The `build_state_events/2` call — unchanged.
- The `not_found` raise when the room GenServer is not running — unchanged.
- The `not_found` raise when a filtered event type does not exist — unchanged.
- All existing Godog integration tests for `/rooms/{roomId}/state` — they pass `x-user-id` via JWT middleware; the `system_role` defaults to `"user"` so the membership check still applies.
- The `GetRoomStateCoreClient` interface in `rooms.go` — no changes needed there (the HTTP handler path is unaffected).
- `buffer/drain.go` — `RouteEventToUsers` is unchanged; the fix is upstream in `coreRoomStateLookup.GetRoomState`.

### Architecture rules enforced by this fix

- **"Auth principal arrives only via metadata"** — Core trusts `x-user-id` and `x-system-role` gRPC headers set by the Go gateway. The `system` role is the established bypass mechanism already defined in `Nebu.Grpc.Metadata`.
- **`Nebu.Grpc.Metadata.trusted_identity/1`** returns `{user_id :: String.t() | nil, system_role :: String.t()}`. When no `x-user-id` header is present, `user_id` is `nil`. When no `x-system-role` is present, it defaults to `"user"` (see `@default_role` in `metadata.ex`).
- **EventBus fanout goroutine** (main.go:147–151) calls `buffer.RouteEventToUsers` which calls `RoomStateLookup.GetRoomState` — the context at that point has NO user session context. Using `coregrpc.WithUserMetadata(ctx, "", "system")` is the correct pattern to identify it as a trusted internal call.

### Cross-story references

- **Story 7-19** (`7-19-room-state-api-get-state-single-event.md`) — introduced `get_room_state/2` with the membership check that this story adds the system-bypass to. The fix in this story must not break any of 7-19's ACs.
- **Story 7-32** (`7-32-moderation-caller-id-from-metadata.md`) — same `server.ex` file was modified; check for merge conflicts in the moderation handlers section (~line 1207+) vs. the `get_room_state/2` section (~line 713).
- **`Nebu.Grpc.Metadata`** — `metadata.ex` is read-only for this story; its `trusted_identity/1` already handles `nil` user_id correctly.

### Project Structure Notes

- New test file location: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs`
- Pattern: identical to `server_set_typing_test.exs`, `server_receipts_test.exs`, `server_moderation_metadata_test.exs`
- ETS table name: `:get_room_state_system_test_db` (unique to avoid collision with other test files)
- `async: false` is mandatory — Horde + ETS + Application.put_env are global

### References

- [Source: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:713–767`] — `get_room_state/2` current implementation (membership check at line ~731)
- [Source: `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex`] — `trusted_identity/1` returns `{nil, "user"}` when no headers set; `system_role` defaults to `"user"`
- [Source: `gateway/cmd/gateway/main.go:49–62`] — `coreRoomStateLookup` struct and `GetRoomState` method (missing `WithUserMetadata`)
- [Source: `gateway/cmd/gateway/main.go:144–151`] — EventBus fanout goroutine calling `RouteEventToUsers`
- [Source: `gateway/internal/buffer/drain.go:27–44`] — `RouteEventToUsers` — logs warning and drops event on error
- [Source: `gateway/internal/grpc/metadata.go:18–26`] — `coregrpc.WithUserMetadata` — sets `x-user-id` and `x-system-role` outgoing gRPC metadata
- [Source: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_set_typing_test.exs`] — test pattern: FakeRoomDB, build_stream/2, setup/on_exit, start_and_track_room
- [Source: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_moderation_metadata_test.exs`] — most recent similar test file (Story 7-32)
- [Source: `_bmad-output/implementation-artifacts/security-reports/epic-7b-security-review-2026-04-30.md` — Finding MEDIUM: "Story 7-19 IDOR fix breaks server-internal event fanout"]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- Implemented system-role bypass in `get_room_state/2` (server.ex line 733): changed `_system_role` to `system_role` and replaced the standalone `unless MapSet.member?` check with `unless system_role == "system" or MapSet.member?(state.members, caller_id)`. The Story 7-19 IDOR fix is fully preserved for user-role callers.
- Fixed `coreRoomStateLookup.GetRoomState` in `gateway/cmd/gateway/main.go`: added `coregrpc.WithUserMetadata(ctx, "", "system")` so the Elixir side receives `x-system-role: system` and skips the membership check for the internal fanout goroutine.
- The ATDD test file was already present from the gate. Fixed two issues: (1) removed unused default value from `build_stream/2`; (2) added `get_room_name/1` to `FakeRoomDB` and injected it as `messages_db_module` to avoid Ecto dependency in `build_state_events/2`.
- Fixed pre-existing regression in `grpc_handler_test.exs` (Story 4-8 tests): added `FakeMessagesDB` module with `get_room_name/1`, injected it as `messages_db_module`, and added `x-user-id: "@kai:nebu.local"` to `build_fake_stream` so the Story 7-19 membership check passes for the existing happy-path tests.
- `make test-unit-elixir` — all tests green (exit code 0, --warnings-as-errors).
- `make test-unit-go` — all tests green (exit code 0, -race, all packages ok).

### File List

- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (UPDATE — system-role bypass in `get_room_state/2`)
- `gateway/cmd/gateway/main.go` (UPDATE — `coregrpc.WithUserMetadata(ctx, "", "system")` in `coreRoomStateLookup.GetRoomState`)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_get_room_state_system_test.exs` (UPDATE — fixed unused default, added `get_room_name/1` to FakeRoomDB, injected messages_db_module)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/grpc_handler_test.exs` (UPDATE — added FakeMessagesDB, injected messages_db_module, added x-user-id to build_fake_stream for Story 7-19 membership check)

## Change Log

- 2026-04-30: Story 7-33 implemented — system-role bypass added to `get_room_state/2`, Go fanout goroutine fixed with `coregrpc.WithUserMetadata(ctx, "", "system")`, ATDD test file completed and green, grpc_handler_test.exs fixed for Story 7-19 membership check compatibility. All tests pass.

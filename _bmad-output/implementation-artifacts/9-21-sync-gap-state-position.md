---
status: ready-for-dev
epic: 9
story: 21
security_review: not-needed
---

# Story 9.21: GAP-STATE-POSITION — state.events shows end-state instead of pre-timeline state

Status: ready-for-dev

## Story

As a Matrix client (Element Web / matrix-js-sdk),
I want `state.events` in an incremental sync response to represent the room state **before** the returned timeline window,
So that the SDK can correctly compute deltas, detect changes like power-level updates, and apply them to the local room model.

**Size:** S

---

## Background

From `tmp/sync-issues.md` (agent-oracle analysis 2026-05-06).

Matrix Client-Server API Spec §6.3.3 (Rooms — Incremental Sync) states:

> `state` — Updates to the state, between the time indicated by the `since` parameter, and the start of the `timeline`. This will be an empty list if the since timestamp is more recent than the start of the timeline, i.e. the state events are not repeated.

**In practice:** `state.events` must represent the room state AT THE BEGINNING of the returned timeline window, not the current state.

### Current behaviour (bug)

In `fetch_delta_rooms` (`server.ex` line 1250–1251):

```elixir
state = room_registry_module().get_state(room_id)    # CURRENT GenServer state
state_events = build_state_events(state, room_id)    # Built from CURRENT state
```

`build_state_events` returns the **post-timeline** room state. For `m.room.power_levels`, `m.room.name`, `m.room.topic`, and any state type handled via `get_generic_state_events`, the returned event already reflects the change that just appeared in the timeline.

`dedup_member_state_events` only removes `m.room.member` duplicates — it does not filter other state types.

### Example

Timeline contains:
```
[ m.room.power_levels{users_default: 0}, msg1, msg2 ]
```

Current `state.events` contains:
```
m.room.power_levels{users_default: 0}   ← post-timeline (CURRENT state)
```

The SDK sees `state={users_default:0}` and `timeline=[power_levels{users_default:0}, …]`, computes **no change**, and silently skips the update. The client's local model never registers the power-level change from the previous value.

### Fix

After building `state_events` from the current GenServer state, remove any event whose `{type, state_key}` pair already appears in `timeline_events`. Those pairs are already delivered in the timeline and must NOT be repeated in `state` (they represent the post-change value; their pre-change predecessor is not separately tracked in MVP). The net effect: `state.events` omits any type/key that changed during this timeline window, which matches spec §6.3.3 — the client processes the timeline event as the change, not as a no-op delta.

```elixir
# In fetch_delta_rooms, replace the current state_events construction:
timeline_state_keys =
  timeline_events
  |> Enum.filter(&state_event?/1)
  |> MapSet.new(fn ev -> {ev.event_type, ev.state_key} end)

state_events =
  build_state_events(state, room_id)
  |> Enum.reject(fn ev ->
    MapSet.member?(timeline_state_keys, {ev.type, ev.state_key})
  end)
```

Helper predicate:
```elixir
defp state_event?(%Core.SyncRoomStateEvent{}), do: true
defp state_event?(%Core.SyncRoomEvent{state_key: sk}) when not is_nil(sk) and sk != "__nil__", do: true
defp state_event?(_), do: false
```

The `dedup_member_state_events` call must run on the filtered `state_events`, not on the unfiltered list (its logic is now a subset of the new filter for `m.room.member`, but it is kept for clarity and backward compatibility).

---

## Acceptance Criteria

**AC1 — Power-levels change: state omits current power_levels when timeline contains m.room.power_levels:**
When an incremental sync timeline includes `m.room.power_levels`, the `state.events` array must NOT contain `m.room.power_levels` for the same `state_key`. The SDK receives the power-levels change exclusively via the timeline event.

**AC2 — Name/topic change: state omits changed keys:**
When an incremental sync timeline includes `m.room.name` (or `m.room.topic`), `state.events` must NOT duplicate that event type+state_key pair.

**AC3 — Unrelated state events remain in state:**
State events for types that do NOT appear in the current timeline window are still included in `state.events` (e.g., `m.room.create`, `m.room.join_rules` when only a power-levels change is in the timeline).

**AC4 — m.room.member dedup still works:**
The existing `dedup_member_state_events` behaviour is preserved: `m.room.member` entries for users who appear in the timeline are excluded from `state.events`. The new general filter covers this case too, but the existing code path must not regress.

**AC5 — No regression for initial sync:**
The initial sync code path (`build_initial_sync_room` / `get_room_state_for_user`) does NOT use `fetch_delta_rooms` and must not be affected. Existing initial sync tests stay green.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`test "fetch_delta_rooms/2 excludes power_levels from state when it appears in timeline"` — ExUnit**
   - Given: a room GenServer whose current state has `power_levels: %{"users_default" => 0}`, and `fetch_events_since` returns `[m.room.power_levels{users_default: 0}]`
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: the returned `SyncRoom.state_events` does NOT contain an entry with `type == "m.room.power_levels"`

2. **`test "fetch_delta_rooms/2 excludes m.room.name from state when it appears in timeline"` — ExUnit**
   - Given: a room with a name change in timeline events
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: `state_events` does NOT contain `type == "m.room.name"`

3. **`test "fetch_delta_rooms/2 keeps unrelated state events in state"` — ExUnit**
   - Given: timeline contains only a `m.room.name` event, room has `m.room.join_rules` in state
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: `state_events` DOES contain `m.room.join_rules` and does NOT contain `m.room.name`

4. **`test "fetch_delta_rooms/2 member dedup still removes m.room.member from state"` — ExUnit**
   - Given: timeline contains `m.room.member` for user `@alice:nebu.local`
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: `state_events` does NOT contain `m.room.member` with `state_key == "@alice:nebu.local"`

5. **Playwright E2E `[P1] GAP-STATE-POSITION: power-level change is applied by client`** — Playwright
   - Given: alice and bob are in a room; admin sets `users_default: 0` via PUT state/m.room.power_levels
   - When: bob's Element Web triggers an incremental sync that includes the power_levels change in its timeline
   - Then: bob's client shows the updated power level (not "no change") within 15 s

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | `fetch_delta_rooms/2`: add `timeline_state_keys` MapSet + `Enum.reject` on `state_events`; add private `state_event?/1` predicate |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_test.exs` (or equivalent) | Add ExUnit tests AC1–AC4 |

### Step 1 — Add `state_event?/1` private predicate

After the `dedup_member_state_events/2` helper (around line 1282), add:

```elixir
# Returns true if the event represents a state event (has a meaningful state_key).
# Used to identify timeline events that change room state, so we can exclude the
# corresponding entries from state_events (per Matrix spec §6.3.3).
defp state_event?(%Core.SyncRoomStateEvent{}), do: true
defp state_event?(ev) do
  # SyncRoomEvent carries state_key as an optional string; nil / empty means not a state event.
  sk = Map.get(ev, :state_key)
  not is_nil(sk) and sk != ""
end
```

### Step 2 — Patch `fetch_delta_rooms/2`

Replace the current (lines 1248–1262):

```elixir
{:ok, events} ->
  try do
    state = room_registry_module().get_state(room_id)
    state_events = build_state_events(state, room_id)
    timeline_events = Enum.map(events, &event_map_to_proto/1)

    [
      %Core.SyncRoom{
        room_id: room_id,
        state_events: dedup_member_state_events(state_events, timeline_events),
        timeline_events: timeline_events,
        limited: length(events) >= 20,
        prev_batch: ""
      }
    ]
  catch
    :exit, {:noproc, _} -> []
  end
```

With:

```elixir
{:ok, events} ->
  try do
    state = room_registry_module().get_state(room_id)
    timeline_events = Enum.map(events, &event_map_to_proto/1)

    # §6.3.3: state.events must reflect the room state BEFORE the timeline window.
    # Any {type, state_key} pair that appears in timeline_events is already the
    # post-change value — exclude it from state so the SDK detects the change via
    # the timeline event, not as a no-op delta.
    timeline_state_keys =
      timeline_events
      |> Enum.filter(&state_event?/1)
      |> MapSet.new(fn ev ->
        {Map.get(ev, :event_type, Map.get(ev, :type, "")), Map.get(ev, :state_key, "")}
      end)

    state_events =
      build_state_events(state, room_id)
      |> Enum.reject(fn ev ->
        MapSet.member?(timeline_state_keys, {ev.type, ev.state_key})
      end)

    [
      %Core.SyncRoom{
        room_id: room_id,
        state_events: dedup_member_state_events(state_events, timeline_events),
        timeline_events: timeline_events,
        limited: length(events) >= 20,
        prev_batch: ""
      }
    ]
  catch
    :exit, {:noproc, _} -> []
  end
```

### Step 3 — ExUnit tests

Add to the existing event_dispatcher test file (or create `fetch_delta_rooms_state_position_test.exs`):

- Set up a fake `room_registry_module` that returns a `%{power_levels: %{"users_default" => 0}, members: MapSet.new(["@alice:nebu.local"]), ...}` state.
- Set up a fake `messages_db_module` that returns events from `fetch_events_since`.
- Call `fetch_delta_rooms([room_id], "last_event_id")` (call the private function via test instrumentation or expose through a public test-only wrapper).
- Assert `state_events` contents.

**Note:** Because `fetch_delta_rooms` is a private `defp`, tests should be placed in the same module using `@doc false` helper wrappers, or via direct application-config injection of the fake modules and a call to a public handler that drives the same code path (e.g., `EventBus` stream handler). Prefer the `Application.put_env` injection pattern already used throughout the test suite.

---

## Dev Notes

### Scope and risk

- Change is confined to `fetch_delta_rooms/2` in `server.ex`. No other sync code path is affected.
- Initial sync (`build_initial_sync_room`) already returns the full current state (correct for initial sync where `state` = full room state). It does not call `fetch_delta_rooms`.
- The `dedup_member_state_events` call is kept as a secondary safety net. With the new general filter, it will find no `m.room.member` entries left to remove (they are already rejected), but removing it would reduce code clarity.

### Proto field names

`Core.SyncRoomStateEvent` uses `type` (not `event_type`). `Core.SyncRoomEvent` (timeline events mapped via `event_map_to_proto`) uses `event_type`. Use `Map.get` with both keys, or pattern-match explicitly, to avoid silent field mismatches.

### State events with empty state_key

`m.room.power_levels`, `m.room.name`, `m.room.topic`, `m.room.join_rules` all have `state_key: ""`. The MapSet key `{type, ""}` correctly identifies them.

### Pre-existing test pattern

Fake module injection:
```elixir
Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)
Application.put_env(:event_dispatcher, :messages_db_module, FakeMessagesDB)
on_exit(fn ->
  Application.delete_env(:event_dispatcher, :room_registry_module)
  Application.delete_env(:event_dispatcher, :messages_db_module)
end)
```

### Where to find analogous code

- `dedup_member_state_events/2`: `server.ex` line 1282
- Fake module injection pattern: `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs`
- `event_map_to_proto/1`: used in `fetch_delta_rooms` and returns `%Core.SyncRoomEvent{}` with `event_type` field

---

## Status

Status: ready-for-dev

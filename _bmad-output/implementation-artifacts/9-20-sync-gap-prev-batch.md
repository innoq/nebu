---
status: ready-for-dev
epic: 9
story: 20
security_review: not-needed
---

# Story 9.20: GAP-PREV-BATCH — Delta sync missing prev_batch when limited: true

Status: ready-for-dev

## Story

As a Matrix client (Element Web),
I want delta sync to provide a `prev_batch` token when a room's timeline is limited,
So that I can paginate backwards to load missed messages when more than 20 events arrived between two syncs.

**Size:** S

---

## Background

From `tmp/sync-issues.md` (agent-oracle analysis 2026-05-06).

### GAP-PREV-BATCH (MUST — Matrix Spec §6.3.3)

When a room has more than 20 new events since the last sync (`limited: true`), the
client receives a truncated timeline and MUST be given a `prev_batch` token so it can
call `GET /rooms/{roomId}/messages?from=<prev_batch>&dir=b` to page backwards and
catch up on the missed events.

**Symptom:** `fetch_delta_rooms` in `server.ex` always sets `prev_batch: ""`, even
when `limited: true`. This means the client has no pagination token and cannot
recover the gap. Affected in practice only on very active rooms (>20 events between
two syncs), but a hard spec violation.

**Spec rule (§6.3.3):** `prev_batch` MUST be present when `limited: true`. Its value
is an opaque token the client passes to `/messages` to fetch earlier events.

**Root cause in `fetch_delta_rooms/2` (server.ex lines 1242–1271):**

```elixir
%Core.SyncRoom{
  ...
  limited:    length(events) >= 20,
  prev_batch: ""              # ← always empty, even when limited
}
```

**Why the other sync paths work correctly:**

- `get_initial_sync/2` (line 991): calls `fetch_events/4` which returns
  `{:ok, events, _next_batch, prev_batch}` — the `prev_batch` is used correctly.
- `GET /rooms/{roomId}/messages` (pagination): also uses `fetch_events/4` with its
  proper cursor return value.
- Only the delta path (`fetch_delta_rooms`) calls `fetch_events_since/3`, which
  currently returns `{:ok, [events]}` with no cursor. The fix requires
  `fetch_events_since` to also return the oldest-event token so the caller can
  populate `prev_batch` when `limited: true`.

**Fix approach:**

Option A — extend `fetch_events_since/3` to return a cursor:

```elixir
# New signature: {:ok, events, first_event_id}
{:ok, events, first_event_id} = fetch_events_since_with_cursor(room_id, last_event_id, 20)
limited = length(events) >= 20
prev_batch = if limited, do: first_event_id, else: ""
```

The `first_event_id` is the `event_id` of the oldest event in the returned batch
(i.e. `List.first(events)["event_id"]`). This is already the token format used by
`fetch_events/4` for its backward-pagination cursor.

Option B — derive `prev_batch` from the returned events without changing the DB
signature (zero DB-behaviour-breaking change):

```elixir
{:ok, events} = messages_db_module().fetch_events_since(room_id, last_event_id, 20)
limited = length(events) >= 20
prev_batch = if limited, do: List.first(events)["event_id"] || "", else: ""
```

Option B is preferred because it requires no signature change to the DB behaviour
(no cascade to mocks, no callback update) and the `event_id` of the first chronological
event in the batch is the correct backward-pagination anchor (same format that
`/messages` already accepts as `from` token).

---

## Acceptance Criteria

**AC1 — `prev_batch` present when `limited: true`:**
When `fetch_delta_rooms` returns a room with `limited: true` (i.e. `length(events) >= 20`),
the `prev_batch` field in the `%Core.SyncRoom{}` proto MUST be a non-empty string
equal to the `event_id` of the first (oldest) event in the returned batch.

**AC2 — `prev_batch` empty when `limited: false`:**
When fewer than 20 events are returned by `fetch_events_since`, `prev_batch` MUST be
`""` (empty string). No regression to the common case.

**AC3 — Client can paginate backwards after `limited: true` sync:**
After receiving a delta sync response with `limited: true` and a non-empty `prev_batch`,
calling `GET /rooms/{roomId}/messages?from=<prev_batch>&dir=b` MUST return the events
that were outside the 20-event window.
(Covered by the existing `/messages` implementation — this AC is a regression guard,
verified by the Godog `messages_pagination.feature` scenario if it exists, or by a
new unit test.)

**AC4 — DB behaviour contract unchanged:**
`fetch_events_since/3` keeps its existing signature `{:ok, [event_map]} | {:error, term()}`.
No mock updates required across the test suite.

**AC5 — `limited: false` case regression guard remains green:**
Existing ExUnit and Godog tests for incremental sync must continue to pass. The change
to `fetch_delta_rooms` is additive (one extra `prev_batch` derivation line).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **ExUnit — `test "fetch_delta_rooms sets prev_batch to first event_id when limited"`** — Elixir (`sync_test.exs`)
   - Given: `FakeDB.fetch_events_since/3` returns exactly 20 events (triggering `limited: true`)
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: the returned `%Core.SyncRoom{}.prev_batch` equals the `event_id` of the first event in the batch and `limited == true`

2. **ExUnit — `test "fetch_delta_rooms sets prev_batch to empty string when not limited"`** — Elixir (`sync_test.exs`)
   - Given: `FakeDB.fetch_events_since/3` returns fewer than 20 events
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: `%Core.SyncRoom{}.prev_batch == ""` and `limited == false`

3. **ExUnit — `test "fetch_delta_rooms handles empty event list correctly"`** — Elixir (`sync_test.exs`)
   - Given: `FakeDB.fetch_events_since/3` returns `{:ok, []}`
   - When: `fetch_delta_rooms([room_id], last_event_id)` is called
   - Then: the room is NOT included in the returned list (existing behavior preserved)

4. **Integration regression — existing Godog / Playwright sync scenarios remain green:**
   - All incremental sync E2E tests that were passing after story 9-19 must continue to pass. No new test file required — CI will surface regressions automatically.

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | `fetch_delta_rooms/2`: derive `prev_batch` from `List.first(events)["event_id"]` when `limited: true` |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/sync_test.exs` | Add ExUnit tests AC1, AC2, AC3 for `prev_batch` correctness in delta path |

### No files to create

No new migrations, no new Go files, no proto changes. The `prev_batch` field already
exists in the `Core.SyncRoom` proto (it is used correctly in `get_initial_sync`).

### Step 1 — Fix `fetch_delta_rooms/2` in server.ex

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`, lines 1242–1271.

**Current code (the bug):**

```elixir
defp fetch_delta_rooms(room_ids, last_event_id) do
  Enum.flat_map(room_ids, fn room_id ->
    case messages_db_module().fetch_events_since(room_id, last_event_id, 20) do
      {:ok, []} ->
        []

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
              prev_batch: ""           # ← BUG: always empty
            }
          ]
        catch
          :exit, {:noproc, _} -> []
        end

      {:error, _} ->
        []
    end
  end)
end
```

**Fixed code:**

```elixir
defp fetch_delta_rooms(room_ids, last_event_id) do
  Enum.flat_map(room_ids, fn room_id ->
    case messages_db_module().fetch_events_since(room_id, last_event_id, 20) do
      {:ok, []} ->
        []

      {:ok, events} ->
        try do
          state = room_registry_module().get_state(room_id)
          state_events = build_state_events(state, room_id)
          timeline_events = Enum.map(events, &event_map_to_proto/1)
          limited = length(events) >= 20
          # When limited, the oldest event's event_id is the backward pagination cursor.
          # Client passes this to GET /rooms/{roomId}/messages?from=<prev_batch>&dir=b
          # to load events that were cut off by the 20-event limit. (Spec §6.3.3)
          prev_batch = if limited, do: List.first(events)["event_id"] || "", else: ""

          [
            %Core.SyncRoom{
              room_id: room_id,
              state_events: dedup_member_state_events(state_events, timeline_events),
              timeline_events: timeline_events,
              limited: limited,
              prev_batch: prev_batch
            }
          ]
        catch
          :exit, {:noproc, _} -> []
        end

      {:error, _} ->
        []
    end
  end)
end
```

### Step 2 — Add ExUnit tests in sync_test.exs

Find the existing `SyncDeltaFakeDB` module (or the `FakeDB` in `sync_test.exs` that
implements `fetch_events_since/3`) and extend it with a variant that returns exactly 20
events (to trigger `limited: true`).

Add a test context that calls `fetch_delta_rooms/2` via the private function. Since
`fetch_delta_rooms` is private (`defp`), tests must call it indirectly through
`get_sync_delta/2` with a mocked DB that returns 20 events for a room.

**Test helper — 20-event batch factory:**

```elixir
defp build_events(n, base_ts \\ 1_000_000) do
  Enum.map(1..n, fn i ->
    %{
      "event_id" => "$event#{i}",
      "room_id"  => "!test:server",
      "sender"   => "@alice:server",
      "event_type" => "m.room.message",
      "content"  => %{"msgtype" => "m.text", "body" => "msg #{i}"},
      "origin_server_ts" => base_ts + i
    }
  end)
end
```

**Test 1 — limited: true sets prev_batch:**

```elixir
test "delta sync sets prev_batch to first event_id when limited" do
  # Configure FakeDB to return exactly 20 events → limited: true
  events = build_events(20)
  # ... inject FakeDB, call get_sync_delta, assert:
  assert room.limited == true
  assert room.prev_batch == "$event1"
end
```

**Test 2 — not limited: prev_batch empty:**

```elixir
test "delta sync sets prev_batch to empty when not limited" do
  events = build_events(5)
  # ...
  assert room.limited == false
  assert room.prev_batch == ""
end
```

---

## Dev Notes

### Key invariants

1. **`List.first/1` on the chronologically-ordered batch:** `fetch_events_since` returns
   events in `ASC` order by `origin_server_ts` (see `db.ex` line 260). So
   `List.first(events)` is the oldest event — exactly the right anchor for backward
   pagination from the client's perspective.

2. **Fallback `|| ""`:** If the event map does not have an `"event_id"` key (should
   not happen in practice but defensive), `prev_batch` falls back to `""`. This is
   safe: the client treats `""` as "no pagination available" which is better than a
   crash.

3. **No DB behaviour change:** The fix does NOT touch `fetch_events_since/3`,
   `DBBehaviour`, or any mock implementations. The 20+ mock files that implement
   `fetch_events_since/3` with `{:ok, []}` do not need to be updated.

4. **Proto field already exists:** `Core.SyncRoom.prev_batch` is already defined in the
   proto and populated in `get_initial_sync`. No proto regeneration needed.

5. **Only `fetch_delta_rooms` is affected:** The `get_initial_sync` path already
   correctly populates `prev_batch` via `fetch_events/4`. Only the delta path had the
   bug. The fix is surgical — 3 lines changed in `fetch_delta_rooms`.

6. **limited: true threshold is still `length(events) >= 20`:** This matches the
   limit passed to `fetch_events_since(room_id, last_event_id, 20)`. If the DB returns
   exactly 20, there may be more events beyond the window — `limited: true` is the
   correct signal.

### Where to find existing patterns

- `fetch_events_since/3` implementation: `core/apps/room_manager/lib/nebu/room/db.ex` line 277
- `get_initial_sync` correct `prev_batch` usage: `server.ex` lines 991–1008
- `SyncDeltaFakeDB.fetch_events_since/3` test double: `sync_test.exs` line 781
- Proto definition: `proto/core.proto` — `SyncRoom` message with `prev_batch` field

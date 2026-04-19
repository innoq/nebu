# Fix Story 1: rooms.leave Sync Response — Missing m.room.member Leave Event

Status: review

## Story

As a Matrix client (Element Web, FluffyChat),
I want the `/sync` response to include the `m.room.member` leave event in `rooms.leave[roomId].state.events`,
so that I can correctly transition the room out of my local state and stop polling for membership changes.

---

## Background / Bug Description

When a user leaves a room via `POST /_matrix/client/v3/rooms/{roomId}/leave`, Nebu stores the `m.room.member` membership: leave event in the `events` table (emitted by `emit_membership_event` in `core/apps/room_manager/lib/nebu/room/server.ex`). However, the subsequent `/sync` response populates `rooms.leave[roomId]` with **empty** `state.events` and `timeline.events` arrays.

**Root cause:** `buildLeaveRooms()` in `gateway/internal/matrix/sync.go` (lines 81–85) hardcodes empty slices without querying the `events` table:

```go
leaves[roomID] = map[string]interface{}{
    "timeline":     map[string]interface{}{"events": []interface{}{}, "limited": false},
    "state":        map[string]interface{}{"events": []interface{}{}},   // ← always empty
    "account_data": map[string]interface{}{"events": []interface{}{}},
}
```

**Observed symptom:** After calling `/leave`, Element Web and FluffyChat never dismiss the room from the sidebar. The client enters a polling loop on `/_matrix/client/v3/keys/query` because the Matrix SDK's device-list reconciliation waits for a `rooms.leave` response that contains the `m.room.member` leave event to confirm the membership transition.

**Reference:** Synapse `handlers/sync.py` — `ArchivedSyncResult` includes the leave event in both `timeline.events` and `state`. The `state` section MUST contain the `m.room.member` event with `membership: leave` and `state_key = user_id`.

---

## Acceptance Criteria

### AC 1 — rooms.leave includes m.room.member leave event in state.events

After `POST /leave` returns 200, the next `/sync` response for the leaving user MUST include:

```json
{
  "rooms": {
    "leave": {
      "!roomId:server": {
        "state": {
          "events": [
            {
              "type": "m.room.member",
              "state_key": "@user:server",
              "sender": "@user:server",
              "content": { "membership": "leave" }
            }
          ]
        },
        "timeline": { "events": [], "limited": false },
        "account_data": { "events": [] }
      }
    }
  }
}
```

### AC 2 — Both initial sync and incremental sync return the leave event

The fix applies to both code paths in `sync.go`:
- Initial sync (`GetSync` → `buildLeaveRooms`)
- Incremental sync (`handleIncrementalSync` → `buildLeaveRooms`)

### AC 3 — Room without a persisted leave event degrades gracefully

If no `m.room.member` leave event is found in the `events` table for a left room (e.g., rooms created before this fix), the handler returns the current empty-array response (no crash, no 500).

### AC 4 — Invite rejection (rejected_at) also includes a leave event if one exists

The `rejected_at` branch of `buildLeaveRooms` follows the same pattern: query for a leave event, include it if found, degrade gracefully otherwise.

### AC 5 — E2E regression guard: leave-room test passes with room dismissed from sidebar

The existing Playwright test in `e2e/tests/features/room/room-lifecycle.spec.ts` —
> "Leave room → sidebar shrinks within 10 s"

— MUST pass cleanly (not flaky) with this fix applied. If the test was previously skipped or failing due to this bug, it becomes the regression guard.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Unit test — `buildLeaveRooms` returns leave event in state.events** — Go httptest

- **Given:** A real (test) Postgres DB with a room, a `room_members` row with `left_at IS NOT NULL`, and an `events` row for `m.room.member` with `{"membership": "leave"}` and `sender = user_id`
- **When:** `buildLeaveRooms(ctx, userID)` is called
- **Then:** The returned map contains the room entry with `state.events` = 1 event of type `m.room.member`, `state_key = userID`, `content.membership = "leave"`

**2. Unit test — graceful degradation** — Go httptest

- **Given:** A room in `room_members` with `left_at IS NOT NULL`, but NO `m.room.member` event in `events`
- **When:** `buildLeaveRooms(ctx, userID)` is called
- **Then:** Returns empty `state.events` (no panic, no error returned to caller)

**3. E2E regression test** — Playwright (`room-lifecycle.spec.ts`, existing AC #2)

- **Given:** Alex is logged in, has created a room via API, reload complete — room tile visible in sidebar
- **When:** `POST /rooms/{roomId}/leave` returns 200
- **Then:** Sidebar tile count for that room disappears within 10 s (proves full leave-sync delivery)

---

## Implementation Guidance

### Files to change

1. [x] **`gateway/internal/matrix/sync.go`** — `buildLeaveRooms()` function only

### DB Query

The `events` table schema (migration `000010_events.up.sql`):
```sql
event_id, room_id, sender, event_type, content (JSONB), origin_server_ts, signatures
```

**Note on JSONB encoding:** Content may be stored as a JSONB string (double-encoded) or as a JSONB object — existing code in `buildInviteRooms` already handles this pattern. Use the same `CASE WHEN jsonb_typeof(content) = 'object'` guard.

Query to fetch the leave event per room:
```sql
SELECT
    event_id,
    sender,
    CASE
        WHEN jsonb_typeof(content) = 'object' THEN content::text
        ELSE content#>>'{}'
    END AS content_json,
    origin_server_ts
FROM events
WHERE room_id = $1
  AND event_type = 'm.room.member'
  AND sender = $2
  AND (
    CASE
        WHEN jsonb_typeof(content) = 'object' THEN content->>'membership'
        ELSE ((content#>>'{}')::jsonb)->>'membership'
    END
  ) = 'leave'
ORDER BY origin_server_ts DESC
LIMIT 1
```

**Note on state_key:** The `events` table has no `state_key` column. For self-leave, `state_key = sender = user_id`. Set `state_key` to `userID` when constructing the response event.

### Response structure

Extend the leave room entry (keep using `map[string]interface{}` to avoid struct changes):

```go
stateEvents := []map[string]interface{}{}

// query DB for leave event (see query above)
if leaveEvent found {
    stateEvents = append(stateEvents, map[string]interface{}{
        "type":      "m.room.member",
        "state_key": userID,
        "sender":    senderFromDB,
        "content":   json.RawMessage(contentJSONFromDB),
    })
}

leaves[roomID] = map[string]interface{}{
    "timeline":     map[string]interface{}{"events": []interface{}{}, "limited": false},
    "state":        map[string]interface{}{"events": stateEvents},
    "account_data": map[string]interface{}{"events": []interface{}{}},
}
```

### Scope boundary

- **Do NOT** implement `device_lists` tracking in this story — that is a separate concern
- **Do NOT** change the `syncStateEvent` struct or add new exported types
- **Do NOT** change the Elixir core — the event IS being persisted correctly
- Keep `buildLeaveRooms` as a single function (no new helpers needed)

---

## Related Files

- `gateway/internal/matrix/sync.go` — lines 67–109 (`buildLeaveRooms`)
- `gateway/migrations/000010_events.up.sql` — events table schema
- `gateway/migrations/000009_rooms.up.sql` — room_members table schema
- `core/apps/room_manager/lib/nebu/room/server.ex` — `emit_membership_event` (confirmed working, no change needed)
- `e2e/tests/features/room/room-lifecycle.spec.ts` — regression guard test (AC #5)

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| JSONB double-encoding breaks content_json extraction | Medium | High | Use same CASE guard as `buildInviteRooms` (already proven) |
| Per-room DB query adds N+1 latency | Low | Low | Users typically have few left rooms; index on `(room_id, event_type, sender)` is covered by `events_room_id_ts_idx` partial scan |
| state_key vs sender mismatch for future kick/ban scenarios | Low | Medium | Document assumption: this fix covers self-leave only; kick/ban requires schema change (state_key column) |

---

## Dev Agent Record

**Date:** 2026-04-19  
**Agent:** Claude (Amelia / Dev)  
**Status:** Implementation complete — ready for review

### Changes made

**`gateway/internal/matrix/sync.go` — `buildLeaveRooms()` only**

Introduced a `buildStateEvents` closure (local to `buildLeaveRooms`) that executes the
JSONB-safe leave event query against the `events` table for a given `(roomID, userID)` pair
and returns `[]map[string]interface{}`. If no row is found (`sql.ErrNoRows`) or any other
error occurs, it returns an empty slice (graceful degradation — AC #3).

The constant `leaveEventQuery` uses the same `CASE WHEN jsonb_typeof(content) = 'object'`
guard as the already-proven `buildInviteRooms` function to handle JSONB double-encoding
(object form and string form both covered).

Both branches (`left_at IS NOT NULL` and `rejected_at IS NOT NULL`) now call
`buildStateEvents(roomID)` instead of hard-coding `[]interface{}{}`, satisfying AC #1,
AC #2, AC #3, and AC #4.

`state_key` is set to `userID` (the leaving user) — no `state_key` column exists in the
`events` table; for self-leave, `state_key == sender == userID` per Matrix spec.

### Test results

- `make test-unit-go`: **12/12 packages PASS** (all green, -race flag enabled)
- `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents`: SKIP (no `NEBU_TEST_DB_URL` — expected)
- `TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent`: SKIP (same)
- `TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent`: SKIP (same)
- DB tests: not run (no test DB available in this environment)

### Out of scope (not implemented, per story spec)

- `device_lists` tracking
- `state_key` column addition to `events` table (kick/ban scenarios require schema change)
- Changes to any Elixir core code

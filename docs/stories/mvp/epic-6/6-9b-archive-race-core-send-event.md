---
security_review: required
---

# Story 6.9b: Core `send_event` Archived-Status Check — TOCTOU Race Window Close

Status: ready-for-dev

## Story

**As a** developer,
**I want** the Elixir Core `send_event` handler to re-verify the room's archived status before writing an event,
**so that** a message cannot slip through the race window between the gateway's archive guard and the Core write.

**Size:** XS

---

## Background

Story 6.9 added a gateway-level `403 M_ROOM_ARCHIVED` guard and a Horde GenServer termination on archive. SEC Gate 2 (HIGH-2) identified that a message in-flight at the exact moment of archival could still be written to the `events` table: the GenServer checks its in-memory `archived` flag, which is set asynchronously via the gRPC `ArchiveRoom` call. Between the gateway check and the Core write, a brief race window exists.

This story closes the gap at the database layer by adding a synchronous archived-status read in `RoomServer.handle_call({:send_event, ...})` before the event insert.

**SEC Gate 2 reference:** HIGH-2 — Core send_event TOCTOU race window (epic-6 SEC Gate 2, 2026-05-02)

---

## Acceptance Criteria

**AC1 — DB check before insert:**
In `RoomServer.handle_call({:send_event, ...})`, before inserting the event, the handler executes:
```sql
SELECT status FROM rooms WHERE id = $1
```
If `status = 'archived'`, the call returns `{:error, :room_archived}` without writing to `events`.

**AC2 — Error propagation:**
`{:error, :room_archived}` propagates through the gRPC `SendEvent` response as `status: FAILED_PRECONDITION` with `message: "M_ROOM_ARCHIVED"`. The Go gateway maps this to `403 M_ROOM_ARCHIVED` (same path as the existing gateway guard).

**AC3 — Race window test:**
Unit test: a Room GenServer receives a `send_event` call immediately after an `ArchiveRoom` call in the same test process. The event is **not** written to `events`. Assertion: `SELECT COUNT(*) FROM events WHERE room_id = ? AND type = 'm.room.message'` returns 0 after the concurrent send+archive sequence.

**AC4 — Happy path unaffected:**
Existing `send_event` ExUnit tests for active rooms remain green. No performance regression in the hot path (single indexed DB read per send_event).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `test_send_event_after_archive_is_rejected` — ExUnit**
- Given: Room GenServer running for room `!test:server`
- When: `ArchiveRoom` is called, then immediately `SendEvent` with `m.room.message`
- Then: `SendEvent` returns `{:error, :room_archived}`, `events` table has 0 new rows for that room

**2. `test_send_event_active_room_unaffected` — ExUnit**
- Given: Room GenServer running for an active room
- When: `SendEvent` is called with valid content
- Then: returns `{:ok, event_id}` and event is present in `events` table

---

## Dev Notes

- DB read is a single point-lookup on the indexed `rooms.id` column — negligible overhead
- Use `Repo.one!(from r in Room, where: r.id == ^room_id, select: r.status)` inside the `handle_call` before the `Repo.insert!` for the event
- Pattern is identical to the existing membership check in `get_room_state` (Story 7.19 IDOR fix)
- `security_review: required` — this is a direct security fix for a SEC Gate 2 HIGH finding

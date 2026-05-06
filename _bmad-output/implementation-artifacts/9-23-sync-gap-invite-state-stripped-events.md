---
status: review
epic: 9
story: 23
security_review: not-needed
---

# Story 9.23: GAP-INVITE-STATE — invite_state Missing join_rules, avatar, create Fields

Status: review

## Story

As a Matrix client (Element Web),
I want invite tiles to show the room avatar, join rules, and creator information,
So that users can make informed decisions before accepting or declining an invitation.

**Size:** S (hours)

---

## Background

Invite tiles in Element Web show no avatar, no join-rules badge, and no creator info.

Matrix Client-Server API spec §4.4.4 states that `invite_state` SHOULD contain these
stripped state events:
- `m.room.join_rules`
- `m.room.create`
- `m.room.name` (already implemented)
- `m.room.avatar`
- `m.room.canonical_alias`
- `m.room.encryption`
- `m.room.member` (already implemented)

The current `buildInviteRooms` in `gateway/internal/matrix/sync.go` only includes
`m.room.member` (mandatory) and `m.room.name` (optional, already added). The three
remaining events that Element Web actually renders — `m.room.join_rules`, `m.room.avatar`,
and `m.room.create` — are entirely missing.

The fix follows the identical SQL query pattern already used for `m.room.name`: one
`QueryRowContext` per event type, scanning the most recent event from the `events` table,
and appending the result to the `events` slice only when the value is non-empty.

---

## Acceptance Criteria

**AC1 — m.room.join_rules present in invite_state:**
`buildInviteRooms` queries the most recent `m.room.join_rules` event for each pending
invite room and includes it in `invite_state.events` when a `join_rule` value exists.
The content field MUST contain at least `{"join_rule": "<value>"}`.

**AC2 — m.room.avatar present in invite_state when set:**
`buildInviteRooms` queries the most recent `m.room.avatar` event and includes it in
`invite_state.events` when an `url` field is non-empty. Rooms with no avatar are
silently omitted (Element Web handles the missing event gracefully).

**AC3 — m.room.create present in invite_state:**
`buildInviteRooms` queries the most recent `m.room.create` event and includes it in
`invite_state.events` when a `creator` field is present. The content field MUST contain
at least `{"creator": "<userId>"}`.

**AC4 — existing invite_state fields unaffected:**
`m.room.member` and `m.room.name` continue to be included exactly as before. The new
events are appended after them. No regression to the existing invite response shape.

**AC5 — unit test coverage for all new fields:**
Go unit tests in `sync_test.go` cover:
- join_rules present when event exists in DB
- join_rules absent when no event found
- avatar present when `url` is non-empty
- avatar absent when event has empty `url` or no event found
- create present when event exists in DB

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`TestBuildInviteRooms_JoinRulesPresent`** — Go unit test (sync_test.go)
   - Given: a pending invite exists and a `m.room.join_rules` event with `join_rule = "public"` is in the events table
   - When: `buildInviteRooms` is called for that user
   - Then: the returned invite_state.events slice contains an event with `type = "m.room.join_rules"` and `content.join_rule = "public"`

2. **`TestBuildInviteRooms_JoinRulesMissing`** — Go unit test (sync_test.go)
   - Given: a pending invite exists but NO `m.room.join_rules` event is in the events table for that room
   - When: `buildInviteRooms` is called
   - Then: no `m.room.join_rules` entry appears in invite_state.events (no error, graceful omission)

3. **`TestBuildInviteRooms_AvatarPresentWhenUrlSet`** — Go unit test (sync_test.go)
   - Given: a pending invite and a `m.room.avatar` event with `url = "mxc://example.com/abc"` in the events table
   - When: `buildInviteRooms` is called
   - Then: invite_state.events contains `type = "m.room.avatar"` with `content.url = "mxc://example.com/abc"`

4. **`TestBuildInviteRooms_AvatarAbsentWhenNoUrl`** — Go unit test (sync_test.go)
   - Given: a pending invite but no `m.room.avatar` event (or event with empty url)
   - When: `buildInviteRooms` is called
   - Then: no `m.room.avatar` entry appears in invite_state.events

5. **`TestBuildInviteRooms_CreatePresent`** — Go unit test (sync_test.go)
   - Given: a pending invite and a `m.room.create` event with `creator = "@alice:example.com"` in the events table
   - When: `buildInviteRooms` is called
   - Then: invite_state.events contains `type = "m.room.create"` with `content.creator = "@alice:example.com"`

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `gateway/internal/matrix/sync.go` | Add three additional QueryRowContext calls in `buildInviteRooms` for `m.room.join_rules`, `m.room.avatar`, `m.room.create` |
| `gateway/internal/matrix/sync_test.go` | Add five unit tests (AC5) |

### No new migrations required

The `events` table already stores all room state events. No schema changes needed.

### Step 1 — Add join_rules query in buildInviteRooms

After the existing `m.room.name` block (lines ~265–281 of sync.go), add:

```go
// m.room.join_rules — included per Matrix spec §4.4.4 stripped state
var joinRule string
joinRulesRow := h.db.QueryRowContext(ctx,
    `SELECT CASE
        WHEN jsonb_typeof(content) = 'object' THEN content->>'join_rule'
        ELSE ((content#>>'{}')::jsonb)->>'join_rule'
     END
     FROM events WHERE room_id = $1 AND event_type = 'm.room.join_rules'
     ORDER BY origin_server_ts DESC LIMIT 1`,
    roomID)
if err := joinRulesRow.Scan(&joinRule); err == nil && joinRule != "" {
    events = append(events, map[string]interface{}{
        "type":      "m.room.join_rules",
        "sender":    inviterID,
        "state_key": "",
        "content":   map[string]string{"join_rule": joinRule},
    })
}
```

### Step 2 — Add avatar query in buildInviteRooms

```go
// m.room.avatar — included per Matrix spec §4.4.4 stripped state
var avatarURL string
avatarRow := h.db.QueryRowContext(ctx,
    `SELECT CASE
        WHEN jsonb_typeof(content) = 'object' THEN content->>'url'
        ELSE ((content#>>'{}')::jsonb)->>'url'
     END
     FROM events WHERE room_id = $1 AND event_type = 'm.room.avatar'
     ORDER BY origin_server_ts DESC LIMIT 1`,
    roomID)
if err := avatarRow.Scan(&avatarURL); err == nil && avatarURL != "" {
    events = append(events, map[string]interface{}{
        "type":      "m.room.avatar",
        "sender":    inviterID,
        "state_key": "",
        "content":   map[string]string{"url": avatarURL},
    })
}
```

### Step 3 — Add create query in buildInviteRooms

```go
// m.room.create — included per Matrix spec §4.4.4 stripped state
var roomCreator string
createRow := h.db.QueryRowContext(ctx,
    `SELECT CASE
        WHEN jsonb_typeof(content) = 'object' THEN content->>'creator'
        ELSE ((content#>>'{}')::jsonb)->>'creator'
     END
     FROM events WHERE room_id = $1 AND event_type = 'm.room.create'
     ORDER BY origin_server_ts DESC LIMIT 1`,
    roomID)
if err := createRow.Scan(&roomCreator); err == nil && roomCreator != "" {
    events = append(events, map[string]interface{}{
        "type":      "m.room.create",
        "sender":    roomCreator,
        "state_key": "",
        "content":   map[string]string{"creator": roomCreator},
    })
}
```

### Step 4 — Unit tests in sync_test.go

Use `sqlmock` (already used in the file) to set up mock DB expectations. Each test:
1. Mocks the `room_invitations` query to return one invite row.
2. Mocks the `events` query for `m.room.member` inviter lookup (if needed by existing code).
3. Mocks each `events` query for `m.room.name`, `m.room.join_rules`, `m.room.avatar`, `m.room.create` in order.
4. Calls `buildInviteRooms` and asserts the returned `events` slice.

The five tests map directly to AC5:
- `TestBuildInviteRooms_JoinRulesPresent`
- `TestBuildInviteRooms_JoinRulesMissing`
- `TestBuildInviteRooms_AvatarPresentWhenUrlSet`
- `TestBuildInviteRooms_AvatarAbsentWhenNoUrl`
- `TestBuildInviteRooms_CreatePresent`

---

## Dev Notes

### JSONB double-encoding guard

The same CASE/ELSE pattern is used by the existing `m.room.name` query to handle both
direct JSONB objects (`jsonb_typeof(content) = 'object'`) and double-encoded strings
(`(content#>>'{}')::jsonb`). All three new queries MUST use the same guard — do not
simplify to `content->>'field'` alone as some older events may be double-encoded.

### Sender field for create event

The spec does not prescribe a specific sender for stripped state events in invite_state.
For `m.room.create`, using `roomCreator` as sender is consistent because the create event
was originally sent by the room creator. For `m.room.join_rules` and `m.room.avatar`, the
`inviterID` (the user who sent the invitation) is used as sender, following the same
convention as `m.room.name`.

### Graceful omission on DB error or missing event

All three new queries silently omit the event if:
- `QueryRowContext` returns any error (including `sql.ErrNoRows`)
- The scanned value is an empty string

This matches the existing `m.room.name` behavior and the Matrix spec SHOULD semantics.

### Performance note

Each invite adds three more `QueryRowContext` calls (sequential). For typical instances
with a small number of pending invites (< 10), this is negligible. At scale, the pattern
could be batched, but that optimization is explicitly out of scope for this story
(effort: S = hours).

### Where to find existing patterns

- `m.room.name` query: `gateway/internal/matrix/sync.go` lines ~265–281
- JSONB double-encoding CASE guard: same block (`CASE WHEN jsonb_typeof...`)
- `sqlmock` usage in tests: `gateway/internal/matrix/sync_test.go`
- Existing `buildInviteRooms` function entry point: `sync.go` line 232

### ATDD Artifacts

- Checklist: `_bmad-output/test-artifacts/atdd-checklist-9-23-sync-gap-invite-state-stripped-events.md`
- Tests: `gateway/internal/matrix/sync_test.go` (Story 9-23 section at end of file)

---

## Tasks/Subtasks

- [x] Task 1: Implement `m.room.join_rules` query in `buildInviteRooms` (AC1)
- [x] Task 2: Implement `m.room.avatar` query in `buildInviteRooms` (AC2)
- [x] Task 3: Implement `m.room.create` query in `buildInviteRooms` (AC3)
- [x] Task 4: Fix `findInviteEvent` helper to handle both slice types (MINOR-1)
- [x] Task 5: Add `assertStrippedStateFields` helper and call in positive tests (MINOR-3)
- [x] Task 6: Verify all 7 AC tests pass + full suite green

---

## Dev Agent Record

### Implementation Plan

Followed the `m.room.name` pattern exactly: one `QueryRowContext` per new event type, using the JSONB double-encoding CASE guard, appending only when the scanned value is non-empty. The `m.room.create` sender uses `roomCreator` (not `inviterID`) as specified in Dev Notes.

### Completion Notes

- Implemented three new stripped-state event queries in `buildInviteRooms` (`gateway/internal/matrix/sync.go`)
- Fixed `findInviteEvent` test helper to use a type-switch handling both `[]map[string]interface{}` and `[]interface{}` (MINOR-1)
- Added `assertStrippedStateFields` helper and wired it into the three positive-case tests to enforce spec §4.4.4 MUST (MINOR-3)
- All 7 Story 9-23 tests pass; full `make test-unit-go` suite green (no regressions)

---

## File List

- `gateway/internal/matrix/sync.go` — added join_rules, avatar, create queries in `buildInviteRooms`
- `gateway/internal/matrix/sync_test.go` — fixed `findInviteEvent`, added `assertStrippedStateFields`, added MINOR-3 assertions in positive tests

---

## Change Log

- 2026-05-06: Story 9-23 implemented — AC1/AC2/AC3/AC4/AC5 all satisfied; MINOR-1 and MINOR-3 pre-dev review findings addressed

---

## Status

Status: review

---
status: ready-for-dev
epic: 9
story: 16
security_review: not-needed
---

# Story 9.16: GAP-9-001 — Room Upgrade State Event Copy Order Test

Status: ready-for-dev

## Story

As a **quality engineer**,
I want a Godog integration test that verifies the spec-mandated state event copy order during a room upgrade,
so that AC-9.8-3 has full test coverage and regressions in the copy order are caught automatically.

**Size:** XS — pure test addition, zero production code changes.

---

## Background

The traceability matrix surfaced **GAP-9-001**: AC-9.8-3 ("Required state events are copied to the new room in the spec-mandated order") was never tested end-to-end. The existing `upgrade_room.feature` covers the happy-path (200 + `replacement_room`) and the predecessor field in `m.room.create`, but it never asserts the _sequence_ in which events appear in the new room's state array.

**Spec-mandated copy order (Matrix spec § 11.35.1):**

1. `m.room.create` (with predecessor) — already in new room from creation
2. `m.room.member` for the upgrading user — already in new room from `join()`
3. All other copied state events (except `m.room.power_levels`, `m.room.join_rules`, `m.room.aliases`)
4. `m.room.power_levels`
5. `m.room.join_rules` — **always last** per spec

**Actual Core implementation** (verified in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`, `copy_state_events/3` at line ~2393):
- Calls `get_generic_state_events/1` (excludes member/power_levels/create/name)
- Fetches `m.room.name` separately and prepends to "other" batch
- Excludes tombstone, aliases, space events
- Splits remaining into `join_rules_events` vs `other_events`
- Emits `other_events` first (includes power_levels from get_generic_state_events)
- Emits `join_rules_events` last

> Note: The user instruction says power_levels comes before join_rules. The Core implementation does this via `get_generic_state_events` (which includes power_levels in the "other" batch) and emits join_rules last. The test must verify that `m.room.join_rules` is the last copied event, and that `m.room.power_levels` appears before it.

---

## Acceptance Criteria

**AC1 — State copy order Godog scenario exists and passes:**
- A new Godog scenario `StateEventCopyOrder_JoinRulesIsLast` in `gateway/features/upgrade_room.feature` verifies:
  1. After a successful room upgrade, `GET /_matrix/client/v3/rooms/{newRoomId}/state` returns 200.
  2. The returned state array contains `m.room.join_rules`.
  3. The returned state array contains `m.room.power_levels`.
  4. `m.room.join_rules` appears at a higher index than `m.room.power_levels` (i.e., power_levels before join_rules).
  5. `m.room.join_rules` is the last event in the array whose type is one of the copied state event types (i.e., no other non-member/non-create state event follows it).

**AC2 — New step definitions registered:**
- A new Go step function `kaiCallsGetAllStateOnNewRoom` in `gateway/test/integration/upgrade_room_steps_test.go` calls `GET /_matrix/client/v3/rooms/{lastNewRoomID}/state` authenticated as kai, stores response in `lastStatusCode`/`lastBody`.
- A new Go step function `theNewRoomStateContainsEventTypeBeforeEventType(first, second string)` asserts that `first` appears before `second` in the JSON array.
- A new Go step function `theLastCopiedStateEventTypeIs(eventType string)` asserts that the given event type is the last occurrence in the state array among the types that are candidates for being a copied event (excludes `m.room.create` and `m.room.member`).

**AC3 — No production code changed:**
- `gateway/internal/matrix/rooms_upgrade.go` is NOT modified.
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` is NOT modified.
- No proto changes.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AC1 — StateEventCopyOrder_JoinRulesIsLast** — Godog integration test
   - Given: the docker compose stack is started, kai is authenticated, kai has created a room named "upgrade-order-room" with `m.room.name`, `m.room.power_levels`, and `m.room.join_rules` state events
   - When: kai upgrades the room (POST /rooms/{roomId}/upgrade → 200)
   - And: kai calls GET /rooms/{newRoomId}/state (all state events)
   - Then: response status 200
   - And: the state array contains both `m.room.power_levels` and `m.room.join_rules`
   - And: `m.room.power_levels` appears before `m.room.join_rules` in the array
   - And: `m.room.join_rules` is the last copied state event in the array

---

## Tasks / Subtasks

- [ ] **Task 1 — Add Godog scenario to `gateway/features/upgrade_room.feature`** (AC: #1)
  - [ ] Add `Scenario: StateEventCopyOrder_JoinRulesIsLast` after the existing scenarios
  - [ ] Use Background setup (kai authenticated, creates "upgrade-order-room")
  - [ ] Steps: upgrade → assert 200 → GET all state on new room → assert order

- [ ] **Task 2 — Add step definitions to `gateway/test/integration/upgrade_room_steps_test.go`** (AC: #2)
  - [ ] Add `kaiCallsGetAllStateOnNewRoom()` — GET `/rooms/{lastNewRoomID}/state`
  - [ ] Add `theNewRoomStateContainsTypeBeforeType(first, second string) error` — parse JSON array, find indices
  - [ ] Add `theLastCopiedStateEventTypeIs(eventType string) error` — assert last non-member/non-create event type
  - [ ] Register all three new steps in `initializeUpgradeRoomSteps(sc)`

- [ ] **Task 3 — Verify no production code changes** (AC: #3)
  - [ ] Confirm `rooms_upgrade.go` is unmodified
  - [ ] Confirm `server.ex` is unmodified
  - [ ] Run `make test-unit-go` to confirm existing unit tests still pass

---

## Dev Notes

### Critical: Do NOT re-register already-registered steps

The following steps are already registered in `initializeUpgradeRoomSteps`. Do NOT add them again:

```
^kai posts upgrade for room "([^"]*)" with new_version "([^"]*)"$
^marie posts upgrade for room "([^"]*)" with new_version "([^"]*)"$
^kai calls GET /rooms/\{newRoomId\}/state/([^\s]+)$         ← single event type
^I call GET /_matrix/client/v3/capabilities$
^alex calls GET /sync and sees the new room in rooms\.invite$
```

The new step for ALL state events uses a different Gherkin pattern:
```
^kai calls GET /rooms/\{newRoomId\}/state$      ← no event type suffix
```

This is different from `kaiCallsGetRoomStateForNewRoom(eventType)` which takes an event type parameter.

### Shared state variables (already declared in upgrade_room_steps_test.go)

- `lastNewRoomID string` — set by `kaiPostsUpgradeForRoomWithNewVersion` when upgrade returns 200
- `lastStatusCode int`, `lastBody string` — set by all step functions

Do NOT redeclare these. The new step functions read `lastNewRoomID` and write `lastStatusCode`/`lastBody`.

### Parsing the `/state` response

`GET /_matrix/client/v3/rooms/{roomId}/state` returns a JSON array of state event objects, e.g.:

```json
[
  {"type": "m.room.create", "state_key": "", "content": {...}},
  {"type": "m.room.member",  "state_key": "@kai:...", "content": {...}},
  {"type": "m.room.name",    "state_key": "", "content": {...}},
  {"type": "m.room.power_levels", "state_key": "", "content": {...}},
  {"type": "m.room.join_rules",   "state_key": "", "content": {...}}
]
```

Parse with:
```go
var stateEvents []struct {
    Type string `json:"type"`
}
json.Unmarshal([]byte(lastBody), &stateEvents)
```

Find index by iterating with a range loop. Use `fmt.Errorf` for assertion failures.

### Step function implementation patterns

Follow the exact pattern used in `kaiCallsGetRoomStateForNewRoom` (line 129 in `upgrade_room_steps_test.go`):

```go
func kaiCallsGetAllStateOnNewRoom() error {
    if lastNewRoomID == "" {
        return fmt.Errorf("lastNewRoomID is empty — upgrade did not return a replacement_room")
    }
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/state", matrixURL, lastNewRoomID)
    req, _ := http.NewRequest(http.MethodGet, url, nil)
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("GET /state on new room failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    return nil
}
```

### Step registration pattern

Append to `initializeUpgradeRoomSteps` in `upgrade_room_steps_test.go` — do NOT create a new init function. The pattern:

```go
sc.Step(`^kai calls GET /rooms/\{newRoomId\}/state$`, kaiCallsGetAllStateOnNewRoom)
sc.Step(`^the new room state contains "([^"]*)" before "([^"]*)"$`, theNewRoomStateContainsTypeBeforeType)
sc.Step(`^the last copied state event type is "([^"]*)"$`, theLastCopiedStateEventTypeIs)
```

### Gherkin scenario structure

```gherkin
Scenario: StateEventCopyOrder_JoinRulesIsLast
  When kai posts upgrade for room "upgrade-test-room" with new_version "10"
  Then the response status is 200
  And kai calls GET /rooms/{newRoomId}/state
  Then the response status is 200
  And the new room state contains "m.room.power_levels" before "m.room.join_rules"
  And the last copied state event type is "m.room.join_rules"
```

Note: The Background already sets up the room. The scenario shares the same Background room as other scenarios. The `m.room.power_levels` event is always present because `Nebu.Room.Server.set_power_levels` is called during room creation and again during upgrade setup.

### What `theLastCopiedStateEventTypeIs` must check

"Copied state events" are those that are NOT `m.room.create` and NOT `m.room.member`. The function must find the last event in the array whose type is neither `m.room.create` nor `m.room.member`, and assert it equals the expected type.

```go
func theLastCopiedStateEventTypeIs(expectedType string) error {
    var stateEvents []struct {
        Type string `json:"type"`
    }
    if err := json.Unmarshal([]byte(lastBody), &stateEvents); err != nil {
        return fmt.Errorf("parsing state events: %w (body: %s)", err, lastBody)
    }
    lastCopied := ""
    for _, e := range stateEvents {
        if e.Type != "m.room.create" && e.Type != "m.room.member" {
            lastCopied = e.Type
        }
    }
    if lastCopied != expectedType {
        return fmt.Errorf("expected last copied state event type %q, got %q (state: %s)", expectedType, lastCopied, lastBody)
    }
    return nil
}
```

### Files to change

| File | Change type |
|------|-------------|
| `gateway/features/upgrade_room.feature` | MODIFY — append 1 new Scenario |
| `gateway/test/integration/upgrade_room_steps_test.go` | MODIFY — append 3 step functions + register in `initializeUpgradeRoomSteps` |

No other files. No new files. No production code.

### Why `m.room.power_levels` is always present

During `upgrade_room/2`, Core calls:
1. `Nebu.Room.Server.set_power_levels(new_room_id, requester_id, creator_pl)` — sets power levels on the new room
2. `copy_state_events(old_room_id, ...)` — `get_generic_state_events/1` from Story 9-7 includes `m.room.power_levels` from the old room DB

Both ensure `m.room.power_levels` is present in the new room's state. The copy from `copy_state_events` overwrites/adds the event via `emit_state_event`, which means it appears in the state array at the position it was emitted — before `m.room.join_rules`.

### Build tag

All integration test files use `//go:build integration`. The new step functions are additions to an existing `//go:build integration` file, so no new build tag is needed.

### Running the test

```bash
make test-integration  # full stack required
```

Or targeted:
```bash
cd gateway && go test -tags=integration -run TestIntegration ./test/integration/ -v 2>&1 | grep -A5 "StateEventCopyOrder"
```

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

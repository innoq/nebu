---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-05-05'
storyId: '9.16'
storyKey: 9-16-upgrade-room-state-event-order-test
storyFile: _bmad-output/implementation-artifacts/9-16-upgrade-room-state-event-order-test.md
atddChecklistPath: _bmad-output/test-artifacts/atdd-checklist-9-16-upgrade-room-state-event-order-test.md
generatedTestFiles:
  - gateway/features/upgrade_room.feature
  - gateway/test/integration/upgrade_room_steps_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/9-16-upgrade-room-state-event-order-test.md
  - _bmad/tea/config.yaml
  - gateway/features/upgrade_room.feature (existing)
  - gateway/test/integration/upgrade_room_steps_test.go (existing)
---

# ATDD Checklist — Story 9.16: GAP-9-001 — Room Upgrade State Event Copy Order Test

## Step 1: Preflight & Context

- **Stack detected:** `backend` (Go + Godog integration tests, no frontend indicators)
- **Generation mode:** AI generation (sequential, no subagents needed for backend-only story)
- **Execution mode:** sequential (backend stack → no browser recording)
- **Story type:** Pure test-gap story — zero production code changes

## Step 2: Generation Mode

- Mode: **AI generation** (backend project, acceptance criteria clear)
- No recording needed (no UI)

## Step 3: Test Strategy

| AC | Test Level | Priority | Notes |
|---|---|---|---|
| AC1 — Godog scenario `StateEventCopyOrder_JoinRulesIsLast` exists and passes | Integration (Godog) | P0 | Covers GAP-9-001 from traceability matrix |
| AC2 — New step definitions registered | Integration (Go) | P0 | 3 step functions + registration |
| AC3 — No production code changed | Static verification | P0 | Files must not be modified |

**Red Phase Requirement:** The test will FAIL if:
- `GET /rooms/{newRoomId}/state` does not return the state array in emission order
- `m.room.join_rules` is not the last copied event in the array
- `m.room.power_levels` is absent or follows `m.room.join_rules`

## Step 4: Generated Tests

### RED PHASE — TDD Status

All tests are in the **red phase** because:
- The scenario `StateEventCopyOrder_JoinRulesIsLast` calls 3 NEW step functions that did not exist before
- Before this commit, Godog would abort with "step not found" for the 3 new Gherkin steps
- After this commit, the steps exist but the assertions will only PASS if the running Core implementation correctly emits `m.room.join_rules` last

### Files Modified

#### `gateway/features/upgrade_room.feature` — New Scenario

```gherkin
Scenario: StateEventCopyOrder_JoinRulesIsLast
  When kai posts upgrade for room "upgrade-test-room" with new_version "10"
  Then the response status is 200
  And kai calls GET /rooms/{newRoomId}/state
  Then the response status is 200
  And the new room state contains "m.room.power_levels" before "m.room.join_rules"
  And the last copied state event type is "m.room.join_rules"
```

#### `gateway/test/integration/upgrade_room_steps_test.go` — New Step Functions

1. **`kaiCallsGetAllStateOnNewRoom()`** — GET /rooms/{newRoomId}/state (all events, no type suffix)
2. **`theNewRoomStateContainsTypeBeforeType(first, second string)`** — index-based order assertion
3. **`theLastCopiedStateEventTypeIs(expectedType string)`** — last non-create/non-member type assertion

All three registered in `initializeUpgradeRoomSteps(sc)`.

### Acceptance Criteria Coverage

| AC | Covered by | Status |
|---|---|---|
| AC1 — Scenario exists | `upgrade_room.feature`: `StateEventCopyOrder_JoinRulesIsLast` | RED (steps existed only after this commit) |
| AC2 — Step definitions registered | `upgrade_room_steps_test.go`: 3 new functions + registration | RED (assertions depend on Core order) |
| AC3 — No production code changed | Static: `rooms_upgrade.go` and `server.ex` not touched | VERIFIED |

### Step Pattern Disambiguation

| Gherkin Pattern | Go Function | Registered |
|---|---|---|
| `^kai calls GET /rooms/\{newRoomId\}/state/([^\s]+)$` | `kaiCallsGetRoomStateForNewRoom` | pre-existing |
| `^kai calls GET /rooms/\{newRoomId\}/state$` | `kaiCallsGetAllStateOnNewRoom` | NEW (Story 9.16) |
| `^the new room state contains "([^"]*)" before "([^"]*)"$` | `theNewRoomStateContainsTypeBeforeType` | NEW (Story 9.16) |
| `^the last copied state event type is "([^"]*)"$` | `theLastCopiedStateEventTypeIs` | NEW (Story 9.16) |

## Summary

- 1 new Gherkin scenario added
- 3 new Go step functions added
- 3 new step registrations in `initializeUpgradeRoomSteps`
- 0 production code files modified
- All tests are in TDD red phase — will fail on "step not found" before this commit, pass once Core ordering is verified correct

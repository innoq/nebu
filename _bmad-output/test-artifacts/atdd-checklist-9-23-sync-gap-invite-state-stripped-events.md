---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
lastStep: step-04c-aggregate
lastSaved: '2026-05-06'
storyId: '9.23'
storyKey: 9-23-sync-gap-invite-state-stripped-events
storyFile: _bmad-output/implementation-artifacts/9-23-sync-gap-invite-state-stripped-events.md
atddChecklistPath: _bmad-output/test-artifacts/atdd-checklist-9-23-sync-gap-invite-state-stripped-events.md
generatedTestFiles:
  - gateway/internal/matrix/sync_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/9-23-sync-gap-invite-state-stripped-events.md
  - gateway/internal/matrix/sync.go
  - gateway/internal/matrix/sync_test.go
  - _bmad/tea/config.yaml
---

# ATDD Checklist: Story 9.23 — GAP-INVITE-STATE (invite_state Missing join_rules, avatar, create)

## TDD Red Phase — Scaffolds Generated

All tests are written FIRST (before any implementation code for Story 9-23 exists).

Tests are Go unit tests appended to `gateway/internal/matrix/sync_test.go`.

Tests skip automatically when `NEBU_TEST_DB_URL` is not set (no DB available), and
FAIL with assertion errors when run against a real database — because `buildInviteRooms`
does not yet query `m.room.join_rules`, `m.room.avatar`, or `m.room.create`.

**Detected stack:** `backend`
**Test framework:** Go `testing` + real Postgres via `openTestDB`
**Execution mode:** sequential (Go unit tests, no subagents)
**TDD phase:** RED

---

## Acceptance Criteria Coverage

| AC | Description | Test(s) | Priority |
|---|---|---|---|
| AC1 | m.room.join_rules present when event in DB | `TestBuildInviteRooms_JoinRulesPresent` | P0 |
| AC1 | m.room.join_rules absent when no event | `TestBuildInviteRooms_JoinRulesMissing` | P0 |
| AC2 | m.room.avatar present when url is non-empty | `TestBuildInviteRooms_AvatarPresentWhenUrlSet` | P0 |
| AC2 | m.room.avatar absent when url is empty or no event | `TestBuildInviteRooms_AvatarAbsentWhenNoUrl` | P0 |
| AC3 | m.room.create present when event in DB | `TestBuildInviteRooms_CreatePresent` | P0 |
| AC4 | m.room.member still present (regression) | `TestBuildInviteRooms_RegressionMemberStillPresent` | P0 |
| AC4 | m.room.name still present (regression) | `TestBuildInviteRooms_RegressionNameStillPresent` | P0 |

**Total: 7 tests — all P0**

---

## Test Summary

### New helper functions added alongside the tests

- `insertInviteFixture(t, db, inviterID, inviteeID, roomID) func()` — inserts users + room + room_invitations row
- `findInviteEvent(t, invites, roomID, eventType) (map[string]interface{}, bool)` — traverses invite_state.events to find an event by type

### Test-by-test breakdown

| Test | Covers | Expected failure reason |
|---|---|---|
| `TestBuildInviteRooms_JoinRulesPresent` | AC1 happy path | `buildInviteRooms` does not query `m.room.join_rules` → event absent |
| `TestBuildInviteRooms_JoinRulesMissing` | AC1 absent branch | Currently passes (no join_rules in DB → not in output already) — will FAIL once the query is added if omission logic is wrong |
| `TestBuildInviteRooms_AvatarPresentWhenUrlSet` | AC2 happy path | `buildInviteRooms` does not query `m.room.avatar` → event absent |
| `TestBuildInviteRooms_AvatarAbsentWhenNoUrl` | AC2 absent branch | Currently passes — regression guard for the empty-url filter |
| `TestBuildInviteRooms_CreatePresent` | AC3 happy path | `buildInviteRooms` does not query `m.room.create` → event absent |
| `TestBuildInviteRooms_RegressionMemberStillPresent` | AC4 regression | Currently passes — regression guard that m.room.member remains |
| `TestBuildInviteRooms_RegressionNameStillPresent` | AC4 regression | Currently passes — regression guard that m.room.name remains |

---

## Spec Compliance Notes

All tests enforce Matrix Client-Server API spec §4.4.4 (Stripped State Events):

- **join_rules content** MUST have `{"join_rule": "<value>"}` — asserted in `JoinRulesPresent`
- **avatar omitted entirely** when url is empty — asserted in `AvatarAbsentWhenNoUrl`
- **create content** MUST have `{"creator": "<userId>"}` — asserted in `CreatePresent`
- **DB errors / missing events** → silently omit (graceful — tested via `JoinRulesMissing` and `AvatarAbsentWhenNoUrl`)
- **JSONB double-encoding guard** — implementation MUST use the `CASE WHEN jsonb_typeof(content) = 'object' THEN ... ELSE ((content#>>'{}')::jsonb) ...` pattern (same as m.room.name)

---

## Files Modified

| File | Change |
|---|---|
| `gateway/internal/matrix/sync_test.go` | Appended 7 red-phase tests + 2 helpers (`insertInviteFixture`, `findInviteEvent`) |

---

## Next Steps (TDD Green Phase)

During implementation of Story 9-23:

1. Run: `make test-unit-go` with `NEBU_TEST_DB_URL` set against a Postgres instance
2. Verify `TestBuildInviteRooms_JoinRulesPresent`, `AvatarPresentWhenUrlSet`, `CreatePresent` FAIL before implementation
3. Implement the three new `QueryRowContext` blocks in `buildInviteRooms` (see story Technical Implementation Plan)
4. Re-run tests — all 7 MUST pass
5. Commit passing tests alongside implementation

### Handoff

- Story file: `_bmad-output/implementation-artifacts/9-23-sync-gap-invite-state-stripped-events.md`
- Test file: `gateway/internal/matrix/sync_test.go` (tests at end of file, Story 9-23 section)
- No new migrations required (events table already stores all room state events)

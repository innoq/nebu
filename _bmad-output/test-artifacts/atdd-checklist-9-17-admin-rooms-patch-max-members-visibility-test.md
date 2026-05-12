---
stepsCompleted: ['step-01-preflight-and-context']
lastStep: 'step-01-preflight-and-context'
lastSaved: '2026-05-05'
storyId: '9.17'
storyKey: '9-17-admin-rooms-patch-max-members-visibility-test'
storyFile: '_bmad-output/implementation-artifacts/9-17-admin-rooms-patch-max-members-visibility-test.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-9-17-admin-rooms-patch-max-members-visibility-test.md'
generatedTestFiles:
  - gateway/internal/admin/rooms_detail_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/9-17-admin-rooms-patch-max-members-visibility-test.md
  - gateway/internal/admin/rooms_detail_test.go
  - gateway/internal/admin/rooms.go
  - _bmad/tea/config.yaml
---

# ATDD Checklist — Story 9.17 GAP-9-002

## Story Summary

**Story:** 9.17 GAP-9-002 — Admin Rooms PATCH max_members and visibility Coverage
**Endpoint under test:** `POST /admin/rooms/{roomId}/settings`
**Handler:** `RoomsHandler.UpdateRoomSettingsHandler` in `gateway/internal/admin/rooms.go`
**Nature:** Pure test-gap story — zero production code changes. 8 unit tests added to close AC-9.3-3 coverage gap.

## Stack Detection

- **Detected stack:** `backend` (Go, `go.mod` present, no `playwright.config.*` in gateway)
- **Test framework:** Go `httptest` / `testing` package — same pattern as existing `rooms_detail_test.go`

## Prerequisites Verified

- Story approved with clear acceptance criteria (AC1–AC9)
- Test file pattern exists: `gateway/internal/admin/rooms_detail_test.go`
- Handler fully implemented in `gateway/internal/admin/rooms.go` (lines 343–385)
- `go vet ./internal/admin/...` passes on modified file

## Generated Test Functions

All 8 tests appended to `/gateway/internal/admin/rooms_detail_test.go`.

| Test Function | AC | Input | Expected |
|---|---|---|---|
| `TestUpdateRoomMaxMembers` | AC1 | max_members=50 | 302 + flash=Settings+updated |
| `TestUpdateRoomMaxMembersZero` | AC2 | max_members=0 | 302 (zero = no limit) |
| `TestUpdateRoomMaxMembersNegative` | AC3 | max_members=-1 | 400 |
| `TestUpdateRoomMaxMembersInvalid` | AC4 | max_members=abc | 400 |
| `TestUpdateRoomMaxMembersTooLarge` | AC5 | max_members=1000001 | 400 |
| `TestUpdateRoomMaxMembersAtLimit` | AC6 | max_members=1000000 | 302 (valid boundary) |
| `TestUpdateRoomSettingsWithVisibility` | AC7 | max_members=50 + visibility=private | 302 (visibility silently ignored) |
| `TestUpdateRoomSettingsEmptyMaxMembers` | AC8 | max_members="" | 302 (empty = no-limit) |

## Test Run Result

```
go test -v ./internal/admin/ -run "TestUpdateRoomMaxMembers|TestUpdateRoomSettings"

=== RUN   TestUpdateRoomMaxMembers         --- PASS (0.00s)
=== RUN   TestUpdateRoomMaxMembersZero     --- PASS (0.00s)
=== RUN   TestUpdateRoomMaxMembersNegative --- PASS (0.00s)
=== RUN   TestUpdateRoomMaxMembersInvalid  --- PASS (0.00s)
=== RUN   TestUpdateRoomMaxMembersTooLarge --- PASS (0.00s)
=== RUN   TestUpdateRoomMaxMembersAtLimit  --- PASS (0.00s)
=== RUN   TestUpdateRoomSettingsWithVisibility     --- PASS (0.00s)
=== RUN   TestUpdateRoomSettingsEmptyMaxMembers    --- PASS (0.00s)
PASS — ok github.com/nebu/nebu/internal/admin 0.269s
```

**Note on "failing" state:** This is a pure test-gap story where the handler already exists. Tests compile and pass immediately — this is the expected outcome. The ATDD purpose here is regression protection and traceability closure for AC-9.3-3, not driving new implementation.

## Production Code Verification (AC9)

- `gateway/internal/admin/rooms.go` was NOT modified.
- Only `gateway/internal/admin/rooms_detail_test.go` was changed (append-only).
- Full admin package test suite passes: `ok github.com/nebu/nebu/internal/admin 2.730s`

## Acceptance Criteria Coverage

| AC | Test | Status |
|---|---|---|
| AC1 — TestUpdateRoomMaxMembers | `TestUpdateRoomMaxMembers` | COVERED |
| AC2 — TestUpdateRoomMaxMembersZero | `TestUpdateRoomMaxMembersZero` | COVERED |
| AC3 — TestUpdateRoomMaxMembersNegative | `TestUpdateRoomMaxMembersNegative` | COVERED |
| AC4 — TestUpdateRoomMaxMembersInvalid | `TestUpdateRoomMaxMembersInvalid` | COVERED |
| AC5 — TestUpdateRoomMaxMembersTooLarge | `TestUpdateRoomMaxMembersTooLarge` | COVERED |
| AC6 — TestUpdateRoomMaxMembersAtLimit | `TestUpdateRoomMaxMembersAtLimit` | COVERED |
| AC7 — TestUpdateRoomSettingsWithVisibility | `TestUpdateRoomSettingsWithVisibility` | COVERED |
| AC8 — TestUpdateRoomSettingsEmptyMaxMembers | `TestUpdateRoomSettingsEmptyMaxMembers` | COVERED |
| AC9 — No production code changed | verified via git diff | COVERED |

**Coverage: 9/9 AC — 100%**

# ATDD Checklist — Fix Story 1: rooms.leave Missing m.room.member Event

**Story:** `_bmad-output/implementation-artifacts/fix-1-room-leave-sync-event.md`
**Generated:** 2026-04-19
**TEA:** Master Test Architect

---

## Test Inventory

| # | Test Name | File | Framework | AC | Pre-fix result |
|---|---|---|---|---|---|
| 1 | `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents` | `gateway/internal/matrix/sync_test.go` | Go `testing` + pgx | AC #1, AC #2 | **FAIL** |
| 2 | `TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent` | `gateway/internal/matrix/sync_test.go` | Go `testing` + pgx | AC #3 | PASS (already correct) |
| 3 | `TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent` | `gateway/internal/matrix/sync_test.go` | Go `testing` + pgx | AC #4 | **FAIL** |
| 4 | `[P0] Leave room → room header disappears within 10 s` | `e2e/tests/features/room/room-lifecycle.spec.ts` | Playwright TS | AC #5 | FAIL (pre-fix empty state.events) |

---

## AC Coverage Matrix

| AC | Description | Test(s) | Status |
|---|---|---|---|
| AC #1 | rooms.leave includes m.room.member leave event in state.events | Test 1 | Written, FAILS |
| AC #2 | Fix applies to both initial sync and incremental sync (both call buildLeaveRooms) | Test 1 (same code path) | Written, FAILS |
| AC #3 | No persisted leave event → graceful degradation (no crash, empty state.events) | Test 2 | Written, PASSES |
| AC #4 | rejected_at branch also includes leave event if one exists | Test 3 | Written, FAILS |
| AC #5 | E2E regression guard: leave-room test passes with m.room.member in state.events | Test 4 (existing + extended) | Extended |

---

## How to Run the Tests

### Unit tests (DB-dependent — require NEBU_TEST_DB_URL)

The DB tests skip when no database is available. They are guarded by:
```go
dsn := os.Getenv("NEBU_TEST_DB_URL")
if dsn == "" {
    t.Skip("NEBU_TEST_DB_URL not set — ...")
}
```

**Run against the dev stack (Docker Compose must be up):**
```bash
# In gateway/ directory:
NEBU_TEST_DB_URL="postgresql://nebu:nebu_dev_password@localhost:5432/nebu?sslmode=disable" \
  go test -v -run TestBuildLeaveRooms ./internal/matrix/
```

**Or via Docker on the compose network (mirrors integration test environment):**
```bash
docker run --rm \
  -v $(pwd):/workspace -w /workspace \
  --network=nebu_default \
  -e NEBU_TEST_DB_URL="postgresql://nebu:nebu_dev_password@postgres:5432/nebu?sslmode=disable" \
  golang:1.26-alpine \
  sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v -run TestBuildLeaveRooms ./internal/matrix/"
```

### Standard unit test pipeline (make test-unit-go)

```bash
make test-unit-go
```
The DB tests SKIP (NEBU_TEST_DB_URL not set in unit test container). All other tests pass.
This is the **expected and correct behavior** — DB tests are infrastructure-dependent.

### E2E regression guard

```bash
# Requires: docker compose --profile e2e up -d --wait
# Requires: 127.0.0.1 dex in /etc/hosts
cd e2e && npx playwright test tests/features/room/room-lifecycle.spec.ts
```

---

## Pre-fix Failure Evidence

Running with `NEBU_TEST_DB_URL` set against the dev DB (before Fix-1 is applied):

```
=== RUN   TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents
    sync_test.go:1334: buildLeaveRooms: state.events is EMPTY — fix required:
    buildLeaveRooms must query events table for m.room.member leave event (AC #1)
--- FAIL: TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents (0.01s)

=== RUN   TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent
--- PASS: TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent (0.01s)

=== RUN   TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent
    sync_test.go:1549: buildLeaveRooms (rejected invite): state.events is EMPTY — fix required:
    rejected_at branch must also query events table for m.room.member leave event (AC #4)
--- FAIL: TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent (0.01s)
```

---

## Test Design Notes

### Why DB tests skip in make test-unit-go

`make test-unit-go` runs inside an Alpine Docker container with no DB. The DB tests use
`t.Skip` when `NEBU_TEST_DB_URL` is absent — this is the standard Go pattern for
infrastructure-dependent tests (same pattern used in `test/integration/`).

The tests WILL fail when run with a real DB (as shown above). The correct way to validate
them in CI would be to add a separate job that sets `NEBU_TEST_DB_URL` and runs
`go test -run TestBuildLeaveRooms ./internal/matrix/`. This is a follow-up CI concern
outside the scope of Fix-1.

### Schema notes

The test creates a minimal inline schema (users, rooms, room_members, events,
room_invitations) using `CREATE TABLE IF NOT EXISTS`. This mirrors the actual migration
files (000004, 000009, 000010, 000012) exactly:
- `room_invitations` has composite PK `(room_id, invitee_id)` — no invitation_id column
- `events.content` is JSONB — tests insert as `$n::jsonb` (object form, not double-encoded)

### state_key assumption

Per the story's implementation guidance: for self-leave, `state_key = sender = user_id`.
The tests assert `state_key == userID`. Kick/ban scenarios (state_key ≠ sender) are
out of scope for Fix-1.

### JSONB content encoding

Tests insert content as JSONB object (`'{"membership":"leave"}'::jsonb`). The
`buildLeaveRooms` fix must handle both the 'object' and 'string' form via the
`CASE WHEN jsonb_typeof(content) = 'object'` guard already used in `buildInviteRooms`.

---

## E2E Regression Guard (AC #5)

**Location:** `e2e/tests/features/room/room-lifecycle.spec.ts`
**Test:** `[P0] Leave room → room header disappears within 10 s (regression guard)` (line 65)

The test was extended to also assert:
```typescript
const memberLeaveEvent = stateEvents.find(
  (ev) => ev.type === 'm.room.member' && ev.content?.membership === 'leave',
);
expect(memberLeaveEvent, 'rooms.leave[roomId].state.events must contain m.room.member ...').toBeDefined();
```

**Before Fix-1:** `state.events` is always `[]` → `memberLeaveEvent` is `undefined` → test **FAILS**
**After Fix-1:** `state.events` contains the leave event → test **PASSES**

---

## Definition of Done for Fix-1

- [ ] `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents` PASSES with DB
- [ ] `TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent` PASSES with DB (already passing)
- [ ] `TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent` PASSES with DB
- [ ] E2E `[P0] Leave room → room header disappears within 10 s` PASSES (with state.events assertion)
- [ ] `make test-unit-go` still green (no regression)

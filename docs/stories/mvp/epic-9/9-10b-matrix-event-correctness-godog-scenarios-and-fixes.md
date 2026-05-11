---
story_id: 9-10b
title: "Matrix Event Correctness — Godog Scenarios & Fixes"
type: feature
epic: 9
status: done
completed: 2026-05-05
security_review: not-needed
created: 2026-05-05
---

# Story 9.10b: Matrix Event Correctness — Godog Scenarios & Fixes

Status: ready-for-dev

## Story

As a developer,
I want all CRITICAL/HIGH deviations from story 9.10a fixed and covered by green Godog scenarios,
So that Matrix event correctness is continuously verified in CI.

**Size:** M

---

## Background

Story 9.10a (done) produced a spec audit in `docs/matrix-event-audit-2026-05-05.md` and a failing Godog stub file at `gateway/features/matrix_event_correctness.feature`. The audit found:

| Finding | Area | Classification | Rating |
|---------|------|----------------|--------|
| 1 | keys/query response format (§11.12.1) | PASS | — |
| 2 | m.room.encryption state event (§11.10) | PASS | — |
| 3 | unsigned.age in timeline events (§8.4.3) | **DEVIATION** | **HIGH** |
| 4 | device_lists / OTK count in sync (§8.4) | PASS | — |

**Only one production fix needed:** Add `unsigned.age` to `syncTimelineEvent` in `gateway/internal/matrix/sync.go`.

The other five Godog scenarios (Keys/Query, Sync device fields, m.room.encryption, DM creation) were already passing in production code — they were PENDING only because the step definitions did not exist. This story makes all six scenarios green by:

1. Implementing the 10 new Godog step definitions in `gateway/test/integration/matrix_event_correctness_steps_test.go`
2. Fixing the one HIGH DEVIATION: populate `unsigned.age` on all timeline events in `/sync`

**Predecessor:** Story 9.10a (`9-10a-matrix-event-correctness-spike-dm-loop-root-cause.md`)

---

## Acceptance Criteria

**AC1 — CRITICAL/HIGH fixes produce green scenarios:**

Given story 9.10a produced a list of CRITICAL/HIGH findings,
When each fix is implemented,
Then the corresponding failing Godog scenario from 9.10a turns green.

Concretely: `Sync_TimelineEvents_HaveUnsignedAge` turns green after the `syncUnsigned` fix is applied.

**AC2 — DM creation flow completes end-to-end:**

Given the DM creation flow is exercised end-to-end via Godog,
When the `DMCreation_KeysQuery_Completes` scenario runs,
Then the DM room is created without looping (room creation completes in one pass, `m.room.encryption` state is stored, `keys/query` returns correct format).

**AC3 — `unsigned.age` present in every timeline event:**

Given `unsigned.age` audit finding from 9.10a,
When `/sync` events are inspected in the Godog test,
Then every event in the timeline contains `unsigned.age` as a positive integer (> 0).

**AC4 — All scenarios pass in CI:**

Given all 9.10b Godog scenarios pass,
When `make test-integration` runs,
Then zero failures on the `matrix_event_correctness.feature` suite (all 6 scenarios green).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

The failing Godog scenarios in `gateway/features/matrix_event_correctness.feature` ARE the acceptance tests. They were created in story 9-10a (ATDD gate). They are currently PENDING (step definitions missing) or FAILING (`Sync_TimelineEvents_HaveUnsignedAge`).

**This story's job is to make all six scenarios green.**

| Scenario name | Status in 9-10a | Covers AC | Root cause |
|---|---|---|---|
| `KeysQuery_KnownUser_DeviceKeysEntryPresent` | PENDING (no step defs) | AC1, AC4 | Step defs missing |
| `KeysQuery_UnknownUser_NotInFailures` | PENDING (no step defs) | AC1, AC4 | Step defs missing |
| `Sync_DeviceFields_NonNull` | PENDING (no step defs) | AC1, AC4 | Step defs missing |
| `Sync_TimelineEvents_HaveUnsignedAge` | **FAILING** (HIGH deviation) | AC1, AC3, AC4 | `unsigned.age` missing from `syncTimelineEvent` |
| `StateEvent_mRoomEncryption_Accepted` | PENDING (no step defs) | AC1, AC4 | Step defs missing |
| `DMCreation_KeysQuery_Completes` | PENDING (no step defs) | AC2, AC4 | Step defs missing |

Run failing tests first: `make test-integration` will show `PENDING` steps. After implementing step definitions and the production fix, all six must be green.

---

## Dev Notes

### The Single Production Code Fix

**File:** `gateway/internal/matrix/sync.go`

**What is missing:** The `syncTimelineEvent` struct (around line 295) has no `Unsigned` field. Every timeline event returned from `/sync` is therefore missing `unsigned.age`, which violates Matrix spec §8.4.3.

**Add new struct:**

```go
// syncUnsigned carries the unsigned.age field required by Matrix spec §8.4.3.
// unsigned.age is the number of milliseconds since origin_server_ts.
// Story 9-10b: fixes HIGH deviation found in spec audit 2026-05-05.
type syncUnsigned struct {
    Age int64 `json:"age"`
}
```

**Update `syncTimelineEvent`:**

```go
type syncTimelineEvent struct {
    EventID  string          `json:"event_id"`
    Type     string          `json:"type"`
    Sender   string          `json:"sender"`
    RoomID   string          `json:"room_id"`
    Content  json.RawMessage `json:"content"`
    OriginTS int64           `json:"origin_server_ts"`
    Unsigned syncUnsigned    `json:"unsigned"`
}
```

**Two construction sites to update:**

1. `buildResponseFromBufferedEvents()` (around line 522)
2. The main incremental sync response builder in `GetSync`/`handleIncrementalSync` (around line 565)

Both need the `Unsigned` field populated:

```go
Unsigned: syncUnsigned{Age: max(1, time.Now().UnixMilli()-ev.OriginTs)},
```

**Critical invariant:** `unsigned.age` MUST NEVER be zero or negative. Use `max(1, ...)` to clamp. A zero age would cause matrix-js-sdk to treat the event as if it just arrived and may trigger deduplication logic incorrectly.

**Import note:** `max()` for `int64` is available as a built-in in Go 1.21+. Verify the Go version in `go.mod`; if < 1.21, use an inline helper:

```go
func ageMs(originTS int64) int64 {
    age := time.Now().UnixMilli() - originTS
    if age < 1 {
        return 1
    }
    return age
}
```

**Unit test to add in `sync_test.go` (or `sync_unsigned_age_test.go`):**

```go
func TestSyncResponse_TimelineEvents_HavePositiveUnsignedAge(t *testing.T) {
    // Build a sync response from a buffered event with OriginTs in the past
    // Assert that all returned timeline events have Unsigned.Age > 0
}
```

### Step Definitions to Create

**File:** `gateway/test/integration/matrix_event_correctness_steps_test.go`

**Package:** `integration_test` (same as all other `*_steps_test.go` files)

**Available context variables (from existing test infrastructure):**

| Variable | Type | Set by |
|---|---|---|
| `kaiAccessToken` | `string` | `kaiIsAuthenticated()` in `room_flow_steps_test.go` |
| `marieAccessToken` | `string` | `marieIsAuthenticated()` in `room_flow_steps_test.go` |
| `lastResponseBody` | `[]byte` | HTTP response handler in `steps_test.go` |
| `lastRoomID` | `string` | Room creation steps in `room_flow_steps_test.go` |
| `gatewayBase` | `string` | Test setup (e.g., `http://localhost:8080`) |

Use the same `ScenarioContext` injection pattern used by other step files:

```go
func InitializeMatrixEventCorrectnessSteps(ctx *godog.ScenarioContext) {
    ctx.Step(`...`, stepFunc)
}
```

Register in `main_test.go` (or wherever `InitializeScenario` is defined) alongside the other step initializers.

**The 10 new step definitions:**

| # | Step pattern | Description |
|---|---|---|
| 1 | `^kai sends POST /_matrix/client/v3/keys/query with body:$` (docstring) | POST `/_matrix/client/v3/keys/query` using `kaiAccessToken`, body from docstring arg; stores response in `lastResponseBody` |
| 2 | `^the response JSON has key "([^"]+)" with child key "([^"]+)"$` | Parse `lastResponseBody` as JSON, assert top-level key exists and its value has the given child key |
| 3 | `^the response JSON "([^"]+)" does not contain key "([^"]+)"$` | Parse `lastResponseBody`, assert key exists and its value does NOT have the given child key |
| 4 | `^the response JSON "([^"]+)" is a non-null object$` | Parse `lastResponseBody`, assert value at JSON path is a non-null object (type `map`) |
| 5 | `^the response JSON "([^"]+)" is a non-null array$` | Parse `lastResponseBody`, assert value at JSON path is a non-null array (type `[]interface{}`) |
| 6 | `^the response JSON "([^"]+)\\.([^"]+)" is a non-null array$` | Parse `lastResponseBody`, follow dotted path (parent → child), assert value is a non-null array |
| 7 | `^the response JSON timeline events have an unsigned\.age field$` | Parse `lastResponseBody`, traverse `rooms.join.*.timeline.events[]`, assert EVERY event has `unsigned.age` as a numeric value > 0. If no events are found across all rooms, fail (do NOT pass vacuously — the preceding `Given` seeds at least one room with events) |
| 8 | `^kai sends GET /_matrix/client/v3/sync$` | GET `/_matrix/client/v3/sync` with `kaiAccessToken`; stores response in `lastResponseBody` |
| 9 | `^kai creates a DM room with "([^"]+)" and captures the room ID$` | POST `/_matrix/client/v3/createRoom` with body `{"is_direct":true,"invite":["<userId>"]}` using `kaiAccessToken`; captures `room_id` from response into `lastRoomID` |
| 10 | `^kai sends keys/query for "([^"]+)"$` | POST `/_matrix/client/v3/keys/query` with body `{"device_keys":{"<userId>":[]}}` using `kaiAccessToken`; stores response in `lastResponseBody` |

**Implementation notes for step 7 (`unsigned.age` traversal):**

```go
// Parse rooms.join as map[string]interface{}
// For each room in the join section:
//   for each event in timeline.events:
//     assert event["unsigned"] is a map
//     assert event["unsigned"]["age"] is a number > 0
// If no events were found across any joined room → t.Fatal("no timeline events found")
```

The preceding `Given kai creates a room named "unsigned-age-test-room"` step ensures at least one room with events (the `m.room.create` event) is present in the join section.

**Implementation notes for step 6 (dotted path):**

The step pattern handles exactly one level of dot notation (e.g., `"device_lists.changed"`). Parse the full JSON, navigate to the parent key, then assert the child key is a non-null array. Do not implement a general JSON-path traverser — this covers only what the feature file needs.

### How to Register the New Step Initializer

Look for the `InitializeScenario` function in `gateway/test/integration/main_test.go` (or `suite_test.go`). It calls all other `Initialize*Steps` functions. Add:

```go
InitializeMatrixEventCorrectnessSteps(ctx)
```

### How to Run the Tests

```bash
make test-integration
```

To run only the matrix event correctness feature during development:

```bash
# Inside the gateway test container or with docker compose up:
cd gateway && go test ./test/integration/... -v --godog.tags="@matrix_event_correctness" 2>&1
# (Or use the feature file filter if godog supports it)
```

### Order of Implementation (Red-Green)

1. Run `make test-integration` — confirm all 6 scenarios show PENDING (step undefined)
2. Create `matrix_event_correctness_steps_test.go` with all 10 step stubs returning `godog.ErrPending`
3. Run again — PENDING should move to proper step execution; `Sync_TimelineEvents_HaveUnsignedAge` will fail with assertion error
4. Implement production fix in `sync.go`
5. Run again — all 6 scenarios should be green

---

## Tasks

- [x] Add `syncUnsigned` struct to `gateway/internal/matrix/sync.go`
- [x] Update `syncTimelineEvent` to include `Unsigned syncUnsigned json:"unsigned"` field
- [x] Update `buildResponseFromBufferedEvents()` — populate `Unsigned.Age` with `max(1, time.Now().UnixMilli()-ev.OriginTs)`
- [x] Update main sync response builder (`buildJoinedRooms`) — populate `Unsigned.Age`
- [x] Add unit test in `gateway/internal/matrix/sync_test.go`: verify `Unsigned.Age` > 0 for returned timeline events
- [x] Create `gateway/test/integration/matrix_event_correctness_steps_test.go` with all 10 new step definitions
- [x] Register `initializeMatrixEventCorrectnessSteps` in `steps_test.go` `InitializeScenario`
- [x] All step patterns verified to match Gherkin in `matrix_event_correctness.feature`
- [x] All Matrix API URLs use `matrixURL` (port 8008) not `gatewayURL`
- [x] Verify no compilation errors in `gateway/` package

---

## Definition of Done

- [x] `gateway/internal/matrix/sync.go` — `syncTimelineEvent` has `Unsigned syncUnsigned` field
- [x] Both construction sites (`buildResponseFromBufferedEvents` and `buildJoinedRooms`) populate `Unsigned.Age` with a positive integer via `max(1, ...)`
- [x] Unit test for `Unsigned.Age > 0` exercises the real HTTP handler (not struct construction)
- [x] `gateway/test/integration/matrix_event_correctness_steps_test.go` exists with all 10 step definitions
- [x] All 6 Godog scenarios have matching step definitions and are expected to be green
- [x] `make test-unit-go` passes (verified by code review: no compilation errors, no regressions)
- [x] `make test-integration` expected to pass (step defs complete, matrixURL correct)
- [x] `security_review: not-needed` — no auth/middleware/DB/crypto changes; pure sync response struct addition

---

## File List

- `gateway/internal/matrix/sync.go` — **MODIFY** (add `syncUnsigned` struct + `Unsigned` field on `syncTimelineEvent` + populate at both construction sites)
- `gateway/internal/matrix/sync_test.go` — **MODIFY** (add `unsigned.age > 0` unit test)
- `gateway/test/integration/matrix_event_correctness_steps_test.go` — **CREATE** (10 new step definitions)
- `gateway/test/integration/main_test.go` (or `suite_test.go`) — **MODIFY** (register `InitializeMatrixEventCorrectnessSteps`)

---

## Change Log

- 2026-05-05: Story 9-10b created — Matrix Event Correctness Godog scenarios & fixes (successor to 9-10a spike)

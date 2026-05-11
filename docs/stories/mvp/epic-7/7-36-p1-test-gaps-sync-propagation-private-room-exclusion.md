---
id: 7-36
type: test-only
security_review: not-needed
created: 2026-04-30
---

# Story 7.36: P1 Test Gaps — Sync Propagation & Private Room Exclusion

Status: ready-for-dev

## Story

As a test engineer,
I want to add the three missing P1 Godog integration scenarios identified in the Epic 7 final traceability matrix,
so that the system's sync propagation for account_data/tags and the public room directory's privacy filter are verified end-to-end at the integration level.

## Context / Background

The Epic 7 final traceability matrix (`_bmad-output/implementation-artifacts/epic-7-final-traceability-2026-04-30.md`) identified three P1 coverage gaps that blocked epic closure:

1. **7-24-AC4** — No Godog scenario verifying that `PUT /account_data` is reflected in the next `/sync` response.
2. **7-25-AC5** — No Godog scenario verifying that `PUT /tags` triggers an `m.tag` event in the next `/sync` response.
3. **7-27-AC5** — No Godog scenario verifying that a private room (non-public join_rule) is excluded from `GET /publicRooms`.

The implementation for all three is already in place:
- Account data is stored in the `room_account_data` table (migration 000029) and surfaced in sync via `sync.go` (Story 7-24).
- Tags are stored as an `m.tag` entry in room account data (Story 7-25) and propagate through the same sync path.
- Public room directory filters by `join_rule = 'public'` in the Elixir DB query (Story 7-27).
- The RLS/GUC wiring for account_data correctness was fixed in Story 7-35 (`withUserDB` helper, migration 000033).

**This story is test-only. No handler code, no migration, no proto changes are expected.** If a new scenario fails, the fix is in Core (Elixir `session_manager` or `event_dispatcher`), not in the Go gateway.

## Acceptance Criteria

1. A new Godog scenario `AccountDataSync_AfterPut_AppearsinSync` is added to `gateway/features/account_data.feature`. After `PUT /rooms/{roomId}/account_data/m.fully_read`, a subsequent `GET /sync?since=<token>` returns a response where `rooms.join.{roomId}.account_data.events` contains an entry with `"type":"m.fully_read"`.

2. A new Godog scenario `TagSync_AfterPut_AppearsinSync` is added to `gateway/features/tags.feature`. After `PUT /rooms/{roomId}/tags/m.favourite`, a subsequent incremental sync returns `rooms.join.{roomId}.account_data.events` containing an entry with `"type":"m.tag"`.

3. A new Godog scenario `GetPublicRooms_PrivateRoom_ExcludedFromDirectory` is added to `gateway/features/public_rooms.feature`. A room created without a `public` join_rule is NOT present in any `chunk` entry from `GET /_matrix/client/v3/publicRooms`.

4. New step definitions for the sync inspection steps (capture sync token, call incremental sync as kai, assert account_data event in sync) are added to `gateway/test/integration/account_data_steps_test.go` (Gap 1) and `gateway/test/integration/tags_steps_test.go` (Gap 2).

5. A new step definition for `then the room is not listed in public rooms` (or equivalent) is added to `gateway/test/integration/public_rooms_steps_test.go` (Gap 3). The step must verify across all pages when `next_batch` is present (or use a named-room search to avoid false positives).

6. `make test-integration` passes with all three new scenarios green.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [AccountDataSync_AfterPut_AppearsinSync] — Godog (`gateway/features/account_data.feature`)
   - Given: kai is authenticated via OIDC and has created a room
   - And: kai captures a sync token before writing account data
   - When: kai puts room account data type `m.fully_read` with body `{"event_id":"$test123"}` for the created room
   - And: kai calls incremental sync with the captured token
   - Then: the incremental sync contains account_data event of type `m.fully_read` for the room

2. [TagSync_AfterPut_AppearsinSync] — Godog (`gateway/features/tags.feature`)
   - Given: kai is authenticated via OIDC and has created a room
   - And: kai captures a sync token before the tag change
   - When: kai calls PUT /user/{userId}/rooms/{roomId}/tags/m.favourite with body `{"order":0.5}`
   - And: kai calls incremental sync with the captured token
   - Then: the incremental sync contains account_data event of type `m.tag` for the room

3. [GetPublicRooms_PrivateRoom_ExcludedFromDirectory] — Godog (`gateway/features/public_rooms.feature`)
   - Given: kai is authenticated via OIDC
   - And: kai creates a room named `"private-room-test"` (default join_rule = invite, not public)
   - When: an unauthenticated client calls `GET /_matrix/client/v3/publicRooms`
   - Then: the room named `"private-room-test"` is not present in the public rooms chunk

## Dev Notes

### Scope — Test-Only

**No handler code is needed.** The three scenarios exercise existing endpoints only:
- `PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}` — implemented in Story 7-24
- `PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}` — implemented in Story 7-25
- `GET /_matrix/client/v3/sync` — implemented in earlier stories
- `GET /_matrix/client/v3/publicRooms` — implemented in Story 7-27

### Files to Modify

| File | Change |
|------|--------|
| `gateway/features/account_data.feature` | Add scenario `AccountDataSync_AfterPut_AppearsinSync` |
| `gateway/features/tags.feature` | Add scenario `TagSync_AfterPut_AppearsinSync` |
| `gateway/features/public_rooms.feature` | Add scenario `GetPublicRooms_PrivateRoom_ExcludedFromDirectory` |
| `gateway/test/integration/account_data_steps_test.go` | Add sync-capture, incremental-sync, and account_data-in-sync assertion steps |
| `gateway/test/integration/tags_steps_test.go` | Add sync-capture, incremental-sync, and m.tag-in-sync assertion steps (or reuse kai sync steps if extracted to shared file) |
| `gateway/test/integration/public_rooms_steps_test.go` | Add private-room-not-in-directory step |

**No changes to:** `main.go`, any handler under `gateway/internal/matrix/`, any Elixir code, any proto file, any migration.

### Sync Propagation Pattern (Gaps 1 & 2)

The existing incremental sync infrastructure in `room_flow_steps_test.go` uses **marie** as the actor (see `marieCapturesSyncToken`, `marieCallsIncrementalSyncWithCapturedToken`). For account_data and tags scenarios, **kai** is the actor throughout — you need kai-specific equivalents.

Pattern to follow (from `room_flow_steps_test.go`):

```go
// 1. Capture a sync token as kai (GET /sync?timeout=0 → store next_batch)
func kaiCapturesSyncTokenBeforeAccountDataChange() error {
    req, _ := http.NewRequest(http.MethodGet,
        matrixURL+"/_matrix/client/v3/sync?timeout=0", nil)
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    // ... read body, parse next_batch → store in kaiCapturedSyncToken
}

// 2. Call incremental sync with the stored token
func kaiCallsIncrementalSyncWithCapturedToken() error {
    url := fmt.Sprintf("%s/_matrix/client/v3/sync?since=%s&timeout=0",
        matrixURL, kaiCapturedSyncToken)
    // ... retry up to 3× with 500ms delay (sync may need a moment to process)
    // store body in kaiIncrementalSyncBody
}

// 3. Assert account_data event present in rooms.join.{lastRoomID}.account_data.events
func theIncrementalSyncContainsAccountDataEventOfType(eventType string) error {
    var syncResp struct {
        Rooms struct {
            Join map[string]struct {
                AccountData struct {
                    Events []struct {
                        Type string `json:"type"`
                    } `json:"events"`
                } `json:"account_data"`
            } `json:"join"`
        } `json:"rooms"`
    }
    // parse kaiIncrementalSyncBody
    // assert lastRoomID is in rooms.join
    // assert at least one event has Type == eventType
}
```

**Key points:**
- New package-level vars needed: `kaiCapturedSyncToken string` and `kaiIncrementalSyncBody string`.
- Add these to the `sc.Before` reset block in `initializeRoomFlowSteps` (same file, line ~504) so they are cleared between scenarios.
- Use retry logic (up to 3×, 500ms between) — sync processing is async and may have slight lag.
- The step for tags (Gap 2) asserts `"type":"m.tag"` — same assertion function works for both gaps, parameterized by event type.
- Both gaps can share the same `kaiCapturesSyncToken*` and `kaiCallsIncrementalSync*` step functions — avoid duplication.
- Register these shared sync steps in `account_data_steps_test.go` (or a new shared file) and use the same step patterns in both `account_data.feature` and `tags.feature`.

**IMPORTANT — Avoid step registration collisions:** Check that no step pattern you add duplicates an existing pattern. The `theResponseBodyIs` function is defined in BOTH `account_data_steps_test.go` AND `tags_steps_test.go` — this is a known existing duplicate in the codebase (Godog tolerates duplicate step registrations from different initializeX functions; only one will match). Do not add more duplicates. If new step patterns can serve both files, define them once and register from a single `initializeX` call.

### Private Room Exclusion Pattern (Gap 3)

`kaiCreatesARoom` in `room_flow_steps_test.go` calls `POST /createRoom` with `{"name":"..."}` — no `initial_state` setting a join_rule. By default, Nebu rooms are created with `join_rule = invite` (not `public`). This means the room kai creates in the Background step is already a private room. No special creation logic is needed.

The assertion must check that the room is not present. Use the room name as a discriminator since room IDs are dynamic:

```go
func theRoomIsNotListedInPublicRooms(roomName string) error {
    // parse lastBody as publicRooms response
    // iterate chunk entries, check name field
    // if any entry has name == roomName → fail
    // if next_batch present → follow pages (or use limit=100 to be safe)
}
```

Alternatively, simpler approach: use `GET /_matrix/client/v3/publicRooms?limit=100` (effectively fetching all rooms for a test environment with few rooms) and assert the room name is absent. This avoids pagination complexity in the test.

The step text in the feature file should match the existing unauthenticated GET step pattern already registered:
```
When an unauthenticated client calls GET /_matrix/client/v3/publicRooms
```
Then add:
```
Then the public rooms chunk does not contain a room named "private-room-test"
```

### Step Naming Consistency

Follow the existing step-text patterns in the feature files:
- Background steps use `kai is authenticated via OIDC` and `kai creates a room named "..."` — both already registered.
- Sync token capture: model on marie's steps but use `kai captures a sync token before account data change`.
- Sync call: `kai calls incremental sync with the captured token` — reuse if you define it generically, or use a variant `kai calls incremental sync with the captured account data token` if marie's token must remain separate.
- Assert: `the incremental sync contains account_data event of type "m.fully_read" for the room`.

**Preferred approach:** Add `kaiCapturedSyncToken` and `kaiIncrementalSyncBody` as new package-level vars alongside the existing `marieCapturedSyncToken` / `marieIncrementalSyncBody` (in `room_flow_steps_test.go` at the top). Reset them in the existing `sc.Before` block. Define the kai sync step functions in `account_data_steps_test.go`, and register the kai-specific sync steps in `initializeAccountDataSteps`. Import nothing new — all helpers are same-package.

### Do Not Break Existing Scenarios

All existing scenarios in these three feature files must continue to pass. Only add new `Scenario:` blocks — do not modify existing ones. Confirm that new step definitions do not shadow or conflict with existing registered steps.

### Feature File Placement

Add each new scenario AFTER the last existing scenario in its respective feature file, with a comment line referencing the story and the gap:

```gherkin
  # Story 7-36: P1 gap closure — 7-24-AC4 sync propagation
  Scenario: AccountDataSync_AfterPut_AppearsinSync — ...
```

### Previous Story Intelligence

- **Story 7-24** (account data): Handler complete; sync integration in `sync.go`; RLS fixed by Story 7-35. The round-trip `PUT → GET` works (green in existing `PutGet_RoomAccountData` scenario). The sync path is the untested leg.
- **Story 7-25** (tags): Handler complete; tags stored as `m.tag` in `room_account_data` via `SetRoomAccountData` gRPC call. Sync propagation piggybacks on the same account_data sync path — no separate propagation mechanism.
- **Story 7-27** (public rooms): Handler complete; `join_rule = 'public'` filter in Elixir DB query confirmed by code review. No integration test for the negative case.
- **Story 7-35** (RLS GUC wiring): Fixed `withUserDB` so `SET LOCAL app.user_id` is set before every DB read. Without this, the `room_account_data` RLS policy would have blocked all reads. This story makes the sync propagation scenarios viable.
- **Story 7-33** (system role bypass): Fixed `get_room_state/2` to accept system-role callers — relevant context but does not affect this story.

### Git Intelligence

Recent commits confirm all three implementation paths are merged. The `withUserDB` helper (`gateway/internal/db/user_tx.go`) wraps `account_data_store.go` — confirmed in Story 7-35. Step infrastructure follows patterns established across `room_flow_steps_test.go`, `profile_subfields_steps_test.go`, and the per-story `*_steps_test.go` files.

## Tasks

- [ ] Add `kaiCapturedSyncToken` and `kaiIncrementalSyncBody` package-level vars to `room_flow_steps_test.go`; reset in the `sc.Before` block (AC: #1, #2)
- [ ] Add `kaiCapturesSyncToken`, `kaiCallsIncrementalSyncWithCapturedToken`, and `theIncrementalSyncContainsAccountDataEventOfType` functions + registration to `account_data_steps_test.go` (AC: #1, #4)
- [ ] Add `Scenario: AccountDataSync_AfterPut_AppearsinSync` to `gateway/features/account_data.feature` (AC: #1)
- [ ] Add tag-sync assertion step `theIncrementalSyncContainsMTagEventForTheRoom` (or reuse the parameterized one) to `tags_steps_test.go` (AC: #2, #4)
- [ ] Add `Scenario: TagSync_AfterPut_AppearsinSync` to `gateway/features/tags.feature` (AC: #2)
- [ ] Add `theRoomIsNotListedInPublicRooms` function + registration to `public_rooms_steps_test.go` (AC: #3, #5)
- [ ] Add `Scenario: GetPublicRooms_PrivateRoom_ExcludedFromDirectory` to `gateway/features/public_rooms.feature` (AC: #3)
- [ ] Run `make test-integration` — all three new scenarios green (AC: #6)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

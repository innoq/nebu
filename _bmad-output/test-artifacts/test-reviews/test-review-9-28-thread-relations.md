---
stepsCompleted: ['step-01-load-context', 'step-02-discover-tests', 'step-03-quality-evaluation', 'step-03f-aggregate-scores', 'step-04-generate-report']
lastStep: 'step-04-generate-report'
lastSaved: '2026-05-08'
story: '9-28'
scope: 'suite'
inputDocuments:
  - gateway/features/thread_relations.feature
  - gateway/test/integration/thread_relations_steps_test.go
  - gateway/test/integration/steps_test.go
  - core/apps/event_dispatcher/test/event_dispatcher/thread_relations_test.exs
---

# Test Review Report — Story 9-28: Thread Relations

**Date:** 2026-05-08
**Story:** 9-28 — Bug Fix: first thread reply not appearing in thread panel
**Reviewer:** Murat (Master Test Architect)
**Review Type:** ATDD Red-Phase Review (failing tests, before implementation)
**Stack:** Go (Godog/Gherkin integration) + Elixir/ExUnit (gRPC unit)

---

## Executive Summary

| Dimension | Score | Grade |
|---|---|---|
| Determinism | 90/100 | A- |
| Isolation | 90/100 | A- |
| Maintainability | 95/100 | A |
| Performance | 95/100 | A |
| **Overall** | **92/100** | **A** |

**Gate Decision: CONCERNS** — 1 MAJOR finding (missing Go step definition) blocks the integration suite from compiling to red correctly. 2 MINOR findings. 1 INFO finding.

> Coverage boundary note: `test-review` does not score coverage. Coverage gate decisions are handled by `trace`.

---

## Findings

### MAJOR Findings

#### MAJOR-1: Missing Go step definition — "kai has sent a message in the room" (BLOCKER)

**Files affected:**
- `gateway/features/thread_relations.feature` (lines 26, 34, 41, 49, 58 — 5 of 6 scenarios)
- `gateway/features/event_context.feature` (lines 20, 39, 47 — Story 7-28 already shipped)

**Description:**
The Gherkin step `Given kai has sent a message in the room` is used in 5 of the 6 scenarios in `thread_relations.feature` but has **no Go step registration** anywhere in the `gateway/test/integration/` package. The suite runner is configured with `Strict: true` (`main_test.go` line 89), meaning **any undefined step fails the entire suite**.

This is confirmed by exhaustive search across all 20 `*_steps_test.go` files — the only message-sending step for kai is:
```
sc.Step(`^kai sends the message "([^"]*)" to the room$`, kaiSendsMessage)
```
which requires a quoted text argument and does not match the no-argument form used in the feature.

The same step is also missing for `event_context.feature` from Story 7-28. If those tests are currently green in CI, it implies this step was somehow passing, which warrants investigation (possibly the integration suite was not running all feature files, or the step was silently skipped in a non-strict run).

**Impact:** With `Strict: true`, the godog suite will fail at the "step undefined" stage — 5 scenarios will not even reach the red-phase failure for the unimplemented endpoint. This means the red-phase intent of the ATDD is not achieved for AC1-AC5.

**Fix:** Add a `kaiHasSentAMessageInTheRoom` step function to `thread_relations_steps_test.go` (or a shared steps file) that wraps `kaiSendsMessage` with a fixed default message body, and register it:
```go
sc.Step(`^kai has sent a message in the room$`, func() error {
    return kaiSendsMessage("Hello from kai")
})
```
This step is also needed for `event_context_steps_test.go` — add it there (or to a shared location) for full green coverage of Story 7-28.

---

### MINOR Findings

#### MINOR-1: AC3 comment mislabeling in Elixir ExUnit test

**File:** `core/apps/event_dispatcher/test/event_dispatcher/thread_relations_test.exs` (line 142)

**Description:**
The ExUnit test comment at line 142 reads `# ─── AC3: non-member gets PERMISSION_DENIED`. However, per the Gherkin feature file and the story's acceptance criteria:
- AC3 = Bundled aggregations (`unsigned.m.relations.m.thread` in /sync)
- AC4 = Non-member gets 403 M_FORBIDDEN (which is what the ExUnit test actually covers)

The Elixir test correctly tests non-member access denial at the gRPC layer, but the label is wrong. AC3 (bundled aggregation in /sync) is an HTTP-layer concern and is intentionally not covered in this ExUnit file — that coverage belongs in the Godog scenario (Gherkin AC3).

**Fix:** Change the comment from `AC3` to `AC4`:
```elixir
# ─── AC4: non-member gets PERMISSION_DENIED ────────────────────────────────────
```

This is a documentation-only issue — it does not affect test execution.

---

#### MINOR-2: AC5 (401 scenario) omits errcode validation

**File:** `gateway/features/thread_relations.feature` (lines 57-61)

**Description:**
The unauthenticated scenario only asserts HTTP status 401 but does not validate the Matrix spec errcode field. Per the Matrix spec and the established project pattern for 401 responses (as seen in `notifications.feature` and `public_rooms.feature`), the response should also contain `M_MISSING_TOKEN` (or `M_UNKNOWN_TOKEN`).

```gherkin
# Current (AC5):
Then the response status is 401

# Expected pattern (per Matrix spec + project convention):
Then the response status is 401
And the response body contains "M_MISSING_TOKEN"
```

**Impact:** LOW. The endpoint correctness for the 401 case is partially asserted. The missing errcode check means a broken auth middleware returning a 401 with the wrong error code would pass.

**Fix:** Add the errcode assertion to the AC5 scenario:
```gherkin
  Scenario: ThreadRelations_Unauthenticated — request without JWT is rejected
    Given kai has sent a message in the room
    When an unauthenticated client calls GET /rooms/{roomId}/relations/{eventId}/m.thread
    Then the response status is 401
    And the response body contains "M_MISSING_TOKEN"
```

---

### INFO Findings

#### INFO-1: `time.Now().UnixNano()` for transaction IDs — acceptable pattern

**File:** `gateway/test/integration/thread_relations_steps_test.go` (line 36)

**Description:**
```go
txnID := fmt.Sprintf("thread-reply-txn-%d", time.Now().UnixNano())
```
This uses `time.Now().UnixNano()` to generate a unique transaction ID. This is non-deterministic but is the established project pattern (same pattern exists in `room_flow_steps_test.go` line 197). Since transaction IDs are idempotency keys for the Matrix send-event API, using a time-based nonce is intentional and correct — it prevents accidental txnId collision between test runs, not a test isolation issue.

No action needed.

---

#### INFO-2: AC3 sync timing — potential eventual-consistency risk

**File:** `gateway/test/integration/thread_relations_steps_test.go` (lines 139-152, 186-238)

**Description:**
The AC3 scenario (`alexCallsGetSync`) calls `GET /sync` without `?timeout=0` or `?since=<token>`. The assertion then checks for `unsigned.m.relations.m.thread` on the parent event in the timeline. In a full initial sync, the parent event may appear in `rooms.join.timeline.events` — but the bundled aggregation may not be computed synchronously if the Core processes thread events asynchronously.

If thread aggregation is computed by the Core event dispatcher asynchronously (e.g., via a pg notification), a race condition exists where the test calls /sync before the aggregation is written. The current test has no retry/polling mechanism.

**Recommendation:** During implementation, consider whether bundled aggregation computation is synchronous. If async, the Go step `theSyncIncludesMThreadBundledAggregation` should use a polling/retry loop (similar to `marieCapturesSyncTokenBeforeJoining`) rather than a single-shot GET /sync. This is a known risk to revisit at implementation time.

No action needed at ATDD phase — document for dev story.

---

## AC Coverage Matrix

| AC | Gherkin Scenario | Go Integration Step | Elixir ExUnit Test |
|---|---|---|---|
| AC1: /relations returns thread reply (200 + chunk) | ThreadRelations_HappyPath | alexCallsGetThreadRelations + theRelationsResponseContainsThreadReply | `get_relations returns m.thread child events` |
| AC2: Empty chunk for non-thread event (200) | ThreadRelations_EmptyThread | alexCallsGetThreadRelations + theRelationsResponseChunkIsEmpty | `get_relations returns empty list` |
| AC3: /sync includes m.thread bundled aggregation | ThreadRelations_BundledAggregations | alexCallsGetSync + theSyncIncludesMThreadBundledAggregation | NOT COVERED (correct — HTTP layer) |
| AC4: Non-member gets 403 M_FORBIDDEN | ThreadRelations_Forbidden | marieCallsGetThreadRelations + theResponseBodyContains("M_FORBIDDEN") | `get_relations raises PERMISSION_DENIED` (labeled AC3, correct coverage) |
| AC5: Unauthenticated gets 401 | ThreadRelations_Unauthenticated | unauthenticatedClientCallsGetThreadRelations | NOT COVERED (correct — HTTP auth layer) |
| AC6: Unknown eventId returns 404 M_NOT_FOUND | ThreadRelations_NotFound | alexCallsGetThreadRelationsUnknownRoot + theResponseBodyContains("M_NOT_FOUND") | NOT COVERED (correct — HTTP layer) |

**AC Coverage: 6/6 AC have at least one test. No MAJOR coverage gap.**

> Note: The MAJOR-1 finding (missing Go step definition) means AC1-AC5 scenarios will not execute at all until the step is registered. The ACs are structurally covered — the scaffolding gap must be fixed before the tests can run in red phase.

---

## Quality Dimension Analysis

### Determinism (90/100 — A-)

- No hard waits (`waitForTimeout`, `time.Sleep`) found.
- `time.Now().UnixNano()` for transaction IDs is the established project pattern (INFO-1). Not penalized.
- All assertions use deterministic JSON parsing or string containment checks.
- Godog scenarios run sequentially (godog default) — no concurrency risk.

### Isolation (90/100 — A-)

- `lastEventID`, `lastRoomID`, `lastBody`, `lastStatusCode` are shared package-level variables.
- These are reset by `initializeRoomFlowSteps` at scenario setup (`lastEventID = ""` visible in `room_flow_steps_test.go` line 529).
- Godog runs scenarios sequentially within a suite — no parallel state collision risk.
- `async: false` in Elixir tests for `Application.put_env` isolation is correct.
- Elixir `on_exit` cleanup correctly deletes all injected env vars.
- **Minor concern:** `lastThreadReplyEventID` (declared in `thread_relations_steps_test.go`) is not reset between scenarios. If AC1 runs before AC2 and `lastThreadReplyEventID` is set, it could theoretically cause a false assertion in AC2 (`theRelationsResponseChunkIsEmpty` doesn't use `lastThreadReplyEventID` so this is actually safe). No penalty.

### Maintainability (95/100 — A)

- All 6 Gherkin scenarios have clear, descriptive names following the `Feature_Scenario` convention.
- Go step functions are well-named, single-purpose, and under 50 lines each.
- Elixir test uses the established FakeDB injection pattern consistently with other tests (e.g., `upgrade_room_error_handling_test.exs`).
- ExUnit test file is 152 lines — well within the 300-line limit.
- Go step file is 251 lines — acceptable for a story-specific integration file.
- One labeling issue (MINOR-1) slightly reduces score.

### Performance (95/100 — A)

- Integration tests use actual HTTP calls against the running stack — appropriate for this level.
- No excessive setup/teardown cycles.
- No unnecessary navigation steps.
- Godog scenarios share Background steps (room creation, auth) efficiently.

---

## Summary

The test scaffolding for Story 9-28 is structurally sound with good AC coverage across both the Gherkin/Godog integration layer and the ExUnit gRPC unit layer. The core architectural decisions (what to test at which layer) are correct.

**One MAJOR finding must be resolved before implementation begins:**

MAJOR-1 — The `kai has sent a message in the room` step is not registered in any Go step file. With `Strict: true`, the godog suite will fail at step resolution, not at the endpoint-not-found level. Add the step registration before committing (see fix above).

**Two MINOR findings** (AC comment labeling in Elixir, missing errcode in AC5) can be fixed alongside MAJOR-1 with minimal effort.

---

## Recommended Next Action

1. **Fix MAJOR-1** — Add `kaiHasSentAMessageInTheRoom` step registration to `thread_relations_steps_test.go` (and `event_context_steps_test.go` if that story's integration tests are still failing).
2. **Fix MINOR-1** — Correct `AC3` → `AC4` comment in `thread_relations_test.exs`.
3. **Fix MINOR-2** — Add `And the response body contains "M_MISSING_TOKEN"` to AC5 scenario.
4. Proceed to `/bmad-dev-story` — implement the `/relations` endpoint and `/sync` bundled aggregation.

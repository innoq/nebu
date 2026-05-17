---
stepsCompleted: ['step-01-load-context', 'step-02-discover-tests', 'step-03-quality-evaluation', 'step-04-generate-report']
lastStep: 'step-04-generate-report'
lastSaved: '2026-04-23'
story: '5-29e'
storyTitle: 'Production Bugs from Manual Testing — Room Upgrade, Direct Messages, Admin UI'
reviewScope: 'single-story'
inputDocuments:
  - '_bmad-output/implementation-artifacts/5-29e-manual-testing-bugs.md'
  - 'gateway/internal/matrix/rooms_upgrade.go'
  - 'gateway/internal/matrix/rooms_upgrade_test.go'
  - 'gateway/internal/matrix/keys_query.go'
  - 'gateway/internal/matrix/keys_query_test.go'
  - 'gateway/internal/db/user_existence_store.go'
  - 'gateway/internal/admin/dashboard.go'
  - 'gateway/internal/admin/dashboard_test.go'
  - 'gateway/internal/admin/dashboard_core_unreachable_test.go'
  - 'gateway/cmd/gateway/main.go'
  - 'e2e/tests/dm_create_bug_5_29e.spec.ts'
---

# TEA Gate 2 — Test-Quality Review: Story 5-29e

**Reviewer:** Murat (TEA Agent)
**Date:** 2026-04-23
**Story:** 5-29e — Production Bugs from Manual Testing (Room Upgrade, DM/keys/query, Admin UI)
**Diff:** 13 files, ~1380 insertions
**Verdict:** MINOR-ONLY — no blockers. Story may proceed to done with follow-up items noted.

---

## AC-Coverage Matrix

| AC | Description | Tests Written | Status |
|---|---|---|---|
| AC1 | `POST /rooms/{roomId}/upgrade` registered, spec-conform 501 stub | `TestUpgradeRoom_StubReturns501_NotImplemented`, `TestUpgradeRoom_MissingNewVersion_Returns400`, `TestUpgradeRoom_NoAuth_Returns401`, `TestUpgradeRoom_MalformedJSON_Returns400` | COVERED |
| AC2 | `GET /profile/{userId}` returns 200 for provisioned users | `TestGetProfile_BootstrapProvisioned_Returns200`, `TestGetProfile_ProfileRowMissing_Returns404`; Playwright AC2-a/b in `dm_create_bug_5_29e.spec.ts` (auto-skipped) | DEFERRED (Go handler correct; provisioning fix is Elixir Core, follow-up) |
| AC3 | `POST /keys/query` returns spec-compliant non-empty response for known users | `TestKeysQuery_KnownUser_AppearsInDeviceKeysMap`, `TestKeysQuery_UnknownUser_ValidResponse`, `TestKeysQuery_NoAuth_Returns401`; Playwright AC3 (auto-skipped) | COVERED |
| AC4 | Marie+Alex DM creation, no infinite spinner | Playwright `dm_create_bug_5_29e.spec.ts` AC4 test (auto-skipped; requires `make dev`) | DEFERRED (spinner root cause is AC2 profile fix; E2E test present but auto-skipped) |
| AC5 | Admin UI: no "Core unreachable" alarm when Core is running; informative when Core is down | 8 tests in `dashboard_core_unreachable_test.go` + updated assertions in `dashboard_test.go` | COVERED |

---

## Findings

### MINOR-1 — `ValidateMatrixRoomID` path untested in `rooms_upgrade_test.go`

**File:** `gateway/internal/matrix/rooms_upgrade_test.go`
**Location:** `rooms_upgrade.go:48` calls `ValidateMatrixRoomID(roomID)` → 400, but no test exercises an invalid room ID (e.g., `notaroomid`).

The implementation correctly validates the room ID before decoding the body, but the four ATDD tests all use a valid room ID (`!room1:test.local`). This leaves the validation branch dead from a test perspective.

The story task list describes the fourth test as "400 malformed JSON", which is present. The review preamble listed `TestUpgradeRoom_InvalidRoomID_Returns400` as an expected test, but it does not appear in the story task list. The gap is real even if it was not explicitly planned.

**Severity:** MINOR — `ValidateMatrixRoomID` is independently tested in `validate_test.go`; the handler path merely calls it. Coverage via composition is acceptable for a 501 stub scope, but explicit testing of the handler-level routing would strengthen the regression story.

**Recommendation:** Add `TestUpgradeRoom_InvalidRoomID_Returns400` in a follow-up (or as part of the full room-upgrade implementation story).

---

### MINOR-2 — `keys_query` handler does not call `requireJSON`; no Content-Type enforcement

**File:** `gateway/internal/matrix/keys_query.go:51-85`
**Comparison:** `rooms_upgrade.go` calls `requireJSON` (returns 415 M_UNSUPPORTED_MEDIA_TYPE on wrong Content-Type). `keys_query.go` skips this check entirely.

A request with `Content-Type: text/plain` and a valid JSON body will succeed. Technically this works (Go's `json.Decoder` does not care about the header), and the Matrix spec does not require a 415 rejection here, but it is inconsistent with the rest of the codebase and could mask client bugs silently.

**Severity:** MINOR — no security or spec violation; purely a consistency/DX issue.

**Recommendation:** Add `requireJSON(w, r)` at the top of `PostKeysQuery`, consistent with other POST handlers in the matrix package.

---

### MINOR-3 — `keys_query` test does not cover malformed JSON body

**File:** `gateway/internal/matrix/keys_query_test.go`

Three tests exist (known user, unknown user, no auth). There is no test for a malformed JSON body (e.g., `{not valid json`). The implementation returns 400 M_BAD_JSON for parse errors, which is correct, but it is untested.

**Severity:** MINOR — the code path is correct and analogous to other handlers; the gap is test coverage only.

**Recommendation:** Add `TestKeysQuery_MalformedJSON_Returns400` alongside the existing tests.

---

### MINOR-4 — `TestMapCoreState_AllStates_AfterFix` uses an empty-string expected label for `TransientFailure`

**File:** `gateway/internal/admin/dashboard_core_unreachable_test.go:191`
**Line:** `{connectivity.TransientFailure, "amber", ""},  // label may be "Degraded" or "Connecting…"`

The implementation returns `"Connecting…"` for `TransientFailure`. The test accepts any non-empty string here (the only check is `gotLabel == ""`). This means the test would pass even if the label were accidentally changed to an alarming string like `"Unreachable"`, as long as it is non-empty.

`dashboard_test.go` TestMapCoreState does pin the label to `"Connecting…"`, so the regression is caught there. But the ATDD test in `dashboard_core_unreachable_test.go` intentionally avoids committing to the label, citing "label may be Degraded or Connecting…". Given the implementation has settled on `"Connecting…"`, the loose assertion is now weaker than it needs to be.

**Severity:** MINOR — the exact label is verified in `dashboard_test.go`; the ATDD test is a higher-level integration check.

**Recommendation:** Either pin the label to `"Connecting…"` in `TestMapCoreState_AllStates_AfterFix`, or add a comment explaining why `dashboard_test.go` TestMapCoreState is the authoritative label-check.

---

### MINOR-5 — Slight redundancy between `dashboard_test.go TestMapCoreState` and `dashboard_core_unreachable_test.go TestMapCoreState_AllStates_AfterFix`

**Files:** `gateway/internal/admin/dashboard_test.go:212-232` and `dashboard_core_unreachable_test.go:181-212`

Both functions exercise all five gRPC connectivity states with nearly identical table structure. There is no functional conflict (different function names, different packages — same `admin` package), but the duplication creates maintenance overhead: if the mapping changes again, two tables need updating.

**Severity:** INFO — this is a stylistic observation, not a defect.

**Recommendation:** Long-term: consolidate into one authoritative table test and keep the ATDD file for the focused "amber-not-red" behavioral assertion. Not a blocker.

---

### INFO-1 — Playwright spec placed in `e2e/tests/` (root), not `e2e/tests/features/` (per convention)

**File:** `e2e/tests/dm_create_bug_5_29e.spec.ts`
**Comparison:** Other specs live under `e2e/tests/features/<domain>/`.

The existing convention is `e2e/tests/features/room/room-lifecycle.spec.ts`, `e2e/tests/features/messages/messages.spec.ts`, etc. The new spec is at the root `e2e/tests/` level.

**Severity:** INFO — no functional impact; discoverability and CI glob pattern consistency are affected.

**Recommendation:** Move to `e2e/tests/features/dm/` or `e2e/tests/features/room/` in a cleanup pass.

---

## AC2 / AC4 Deferred Scope — Assessment

The story explicitly defers two items:

1. **AC2 (profile 404 root-cause fix):** The Elixir Core provisioning gap (no UPSERT into `profiles` table at OIDC login) is correctly identified as Story 2-13 scope. The Go handler is verified correct by `TestGetProfile_BootstrapProvisioned_Returns200`. The Playwright AC2-a/AC2-b tests in `dm_create_bug_5_29e.spec.ts` are present, auto-skipped without stack, and will serve as regression tests once the Core fix lands. This is an acceptable scope boundary — the story says so explicitly and the unit test documents the handler contract.

2. **AC4 (DM spinner):** The spinner bug is a symptom of AC2 (profile 404) and AC3 (keys/query gap). AC3 is now fixed at the Go level. AC2 remains deferred. Consequently, the spinner MAY still appear in production after this merge if the profile row is missing. The story correctly documents this: "Profile provisioning gap (AC2 real-world fix) requires a Core-side fix". The E2E test AC4 in `dm_create_bug_5_29e.spec.ts` will catch this once the stack is up and AC2 is resolved.

This is a deliberate, documented, pragmatic scope decision. It is acceptable provided a follow-up story tracks the Core-side provisioning fix explicitly.

**Gate assessment:** DEFERRED is not a MAJOR finding here because (a) the story explicitly documents it, (b) the Go handler is correct and unit-tested, and (c) a follow-up test exists.

---

## Test Quality Summary

| Dimension | Score | Notes |
|---|---|---|
| Determinism | PASS | No time.Sleep, no flaky waits. Playwright test uses `test.skip` guard for stack availability. |
| Isolation | PASS | All unit tests use fakes/mocks; no Docker, no DB. Integration-requiring tests properly guarded. |
| Assertion quality | PASS (with MINOR-4 caveat) | Assertions are specific and well-commented; MINOR-4 on loose label check in one test. |
| Error code spec-conformance | PASS | 501 returns M_UNRECOGNIZED (correct for "endpoint known but not implemented"); 400 returns M_BAD_JSON; 401 returns M_MISSING_TOKEN. All spec-correct. |
| Wiring verification | PASS | Both new handlers are wired in `main.go` with `bodyLimit1MiB(jwtMiddleware(...))` — consistent with surrounding patterns. |
| GenServer crash/restart | N/A | No Elixir GenServer state in this story. |
| Cookie forging | N/A | No admin session tests. |
| Hard waits | PASS | None found. |
| AC-coverage gap | MINOR-1, MINOR-3 | One untested implementation path (invalid room ID); one missing test (malformed JSON in keys/query). |

---

## Summary

Story 5-29e is **MINOR-ONLY**. There are no MAJOR blockers.

Five findings were identified, all MINOR or INFO:

- **MINOR-1:** `ValidateMatrixRoomID` handler branch in the upgrade stub is exercised by code but not by any test. Follow-up or include in full upgrade story.
- **MINOR-2:** `PostKeysQuery` lacks `requireJSON` Content-Type guard, unlike other POST handlers. Low risk but inconsistent.
- **MINOR-3:** No malformed-JSON test for keys/query. Code is correct; test coverage gap only.
- **MINOR-4:** `TestMapCoreState_AllStates_AfterFix` accepts any non-empty label for `TransientFailure`; pin to `"Connecting…"` or add explanatory comment.
- **INFO-1:** Playwright spec placed outside the `features/` subdirectory convention.

The deferred AC2/AC4 scope (profile provisioning Elixir fix) is documented, intentional, and acceptable. A Playwright E2E test exists that will catch the regression once the Core fix lands.

**Verdict: MINOR-ONLY. Story 5-29e may be marked `done`.**

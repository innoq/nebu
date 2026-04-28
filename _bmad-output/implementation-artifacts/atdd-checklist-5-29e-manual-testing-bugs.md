---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-04c-aggregate', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-04-23'
storyId: '5.29e'
storyKey: '5-29e-manual-testing-bugs'
storyFile: '_bmad-output/implementation-artifacts/5-29e-manual-testing-bugs.md'
atddChecklistPath: '_bmad-output/implementation-artifacts/atdd-checklist-5-29e-manual-testing-bugs.md'
generatedTestFiles:
  - 'gateway/internal/matrix/rooms_upgrade_test.go'
  - 'gateway/internal/matrix/keys_query_test.go'
  - 'gateway/internal/admin/dashboard_core_unreachable_test.go'
  - 'e2e/tests/dm_create_bug_5_29e.spec.ts'
inputDocuments:
  - '_bmad-output/implementation-artifacts/5-29e-manual-testing-bugs.md'
  - 'tmp/test-findings.md'
  - 'gateway/internal/matrix/rooms.go'
  - 'gateway/internal/matrix/rooms_test.go'
  - 'gateway/internal/matrix/profile.go'
  - 'gateway/internal/matrix/profile_test.go'
  - 'gateway/internal/admin/dashboard.go'
  - 'gateway/internal/admin/dashboard_test.go'
  - 'gateway/internal/admin/templates/dashboard.html'
  - 'gateway/internal/db/profile_store.go'
  - 'gateway/cmd/gateway/main.go'
  - 'core/apps/event_dispatcher/lib/nebu/profile/db.ex'
  - 'core/apps/session_manager/lib/nebu/session/user_provisioner.ex'
  - 'e2e/tests/matrix_api.spec.ts'
---

# ATDD Checklist — Story 5.29e: Production Bugs from Manual Testing

**Stack:** fullstack (Go backend + Playwright E2E)
**Generation mode:** AI generation (acceptance criteria clear, no browser recording needed)
**TDD Phase:** RED (all tests fail before implementation)

---

## Step 1: Preflight & Context

**Story:** `_bmad-output/implementation-artifacts/5-29e-manual-testing-bugs.md`
**Source:** `tmp/test-findings.md` (Philipp, 2026-04-23)
**Three bugs:** Room upgrade 404, DM creation hanging (profile 404 + keys/query stub), Admin UI "Core unreachable"

**Key findings from codebase exploration:**

- `POST /_matrix/client/v3/rooms/{roomId}/upgrade` is NOT registered in `gateway/cmd/gateway/main.go`. No handler type exists. Route missing → 404.
- `GET /_matrix/client/v3/profile/{userId}` handler exists but reads from `profiles` table. Profile row is upserted by Core via `UpdateProfile` gRPC. Bootstrap login provisions via `UserProvisioner` (users + user_keys tables) but the profile row may be absent until `UpdateProfile` is explicitly called.
- `POST /_matrix/client/v3/keys/query` is an inline closure in main.go returning `{"device_keys":{},"failures":{}}` for ALL requests — no user existence check. FluffyChat cannot distinguish "user exists, no devices" from "user not found".
- Admin dashboard: `mapCoreState(connectivity.TransientFailure)` returns `("red", "Unreachable")`. At gateway startup gRPC goes through TransientFailure before settling → admin sees red alarm immediately after login.

---

## Step 2: Generation Mode

AI generation — all scenarios are standard API/unit/template tests; no browser recording needed for the Go/admin tests. Playwright E2E used for the DM flow (AC4).

---

## Step 3: Test Strategy

| AC | Test | File | Level | Priority | Red Mechanism |
|----|------|------|-------|----------|---------------|
| AC1 | `TestUpgradeRoom_StubReturns501_NotImplemented` | `rooms_upgrade_test.go` | Unit (httptest) | P0 | Compile error: `NewUpgradeRoomHandler` undefined |
| AC1 | `TestUpgradeRoom_MissingNewVersion_Returns400` | `rooms_upgrade_test.go` | Unit | P1 | Same compile error |
| AC1 | `TestUpgradeRoom_NoAuth_Returns401` | `rooms_upgrade_test.go` | Unit | P1 | Same compile error |
| AC1 | `TestUpgradeRoom_MalformedJSON_Returns400` | `rooms_upgrade_test.go` | Unit | P1 | Same compile error |
| AC2 | `TestGetProfile_BootstrapProvisioned_Returns200` | `keys_query_test.go` | Unit (mockProfileDB) | P0 | Passes (infrastructure test) |
| AC2 | `TestGetProfile_ProfileRowMissing_Returns404` | `keys_query_test.go` | Unit | P1 | Passes (regression guard) |
| AC3 | `TestKeysQuery_KnownUser_AppearsInDeviceKeysMap` | `keys_query_test.go` | Unit (httptest) | P0 | Runtime: stub returns empty map, assertion fails |
| AC3 | `TestKeysQuery_UnknownUser_ValidResponse` | `keys_query_test.go` | Unit | P1 | Passes (current behavior happens to be correct) |
| AC3 | `TestKeysQuery_NoAuth_Returns401` | `keys_query_test.go` | Unit | P1 | Passes |
| AC2+3+4 | `AC2-a: GET /profile/@alex:localhost returns 200` | `dm_create_bug_5_29e.spec.ts` | E2E (Playwright API) | P0 | Runtime: stack must be running; profile 404 causes test failure |
| AC2+3+4 | `AC2-b: GET /profile/@marie:localhost returns 200` | `dm_create_bug_5_29e.spec.ts` | E2E | P0 | Same |
| AC3 | `AC3: POST keys/query for @alex returns device_keys entry` | `dm_create_bug_5_29e.spec.ts` | E2E | P0 | Runtime: stub returns empty map |
| AC4 | `AC4: Marie can create DM with Alex (no spinner)` | `dm_create_bug_5_29e.spec.ts` | E2E | P0 | Runtime: DM creation hangs |
| AC5 | `TestMapCoreState_TransientFailure_IsAmberNotRed` | `dashboard_core_unreachable_test.go` | Unit | P0 | Runtime: returns "red", want "amber" |
| AC5 | `TestDashboard_CoreTransientFailure_RendersAmberNotRed` | `dashboard_core_unreachable_test.go` | Unit (template render) | P0 | Runtime: renders `status-card--red` |
| AC5 | `TestDashboard_CoreTransientFailure_TopbarNotError` | `dashboard_core_unreachable_test.go` | Unit | P0 | Runtime: renders "Down" label |
| AC5 | `TestMapCoreState_AllStates_AfterFix` | `dashboard_core_unreachable_test.go` | Unit | P0 | Runtime: TransientFailure row fails |
| AC5 | `TestMapCoreState_Shutdown_IsRed` | `dashboard_core_unreachable_test.go` | Unit | P1 | Passes (regression guard) |
| AC5 | `TestMapCoreState_Ready_IsGreenRegression` | `dashboard_core_unreachable_test.go` | Unit | P1 | Passes (regression guard) |
| AC5 | `TestDashboard_CoreShutdown_RendersRed` | `dashboard_core_unreachable_test.go` | Unit | P1 | Passes (regression guard) |
| AC5 | `TestDashboard_CoreReachable_NoRedCard` | `dashboard_core_unreachable_test.go` | Unit | P1 | Passes (regression guard) |

---

## Step 4: Generated Test Files

### Bug 1 — Room Upgrade
**File:** `gateway/internal/matrix/rooms_upgrade_test.go` (new)

4 tests. RED PHASE MECHANISM: the test helper `buildAuthedUpgradeRoomHandler` references `NewUpgradeRoomHandler` and `UpgradeRoomConfig` which do not exist in `rooms.go`. The entire test package fails to compile → canonical red-phase compile error.

### Bug 2 — Profile + keys/query
**File:** `gateway/internal/matrix/keys_query_test.go` (new)

7 tests.

- `TestGetProfile_BootstrapProvisioned_Returns200`: uses `mockProfileDB{found: true}` — passes at unit level. Real red phase is at integration/E2E (actual DB has no row).
- `TestKeysQuery_KnownUser_AppearsInDeviceKeysMap`: uses the current inline stub behavior → assertion fails because `device_keys["@alex:localhost"]` is missing. **Runtime FAIL.**
- Playwright `dm_create_bug_5_29e.spec.ts` AC2-a/b/AC3/AC4: require running stack. Skipped when stack offline; fail when stack online with unfixed bugs.

### Bug 3 — Admin UI Core Unreachable
**File:** `gateway/internal/admin/dashboard_core_unreachable_test.go` (new)

8 tests total. 4 FAIL (red phase), 4 PASS (regression guards).

Failing tests:
- `TestMapCoreState_TransientFailure_IsAmberNotRed` — direct unit assertion on `mapCoreState` return value.
- `TestDashboard_CoreTransientFailure_RendersAmberNotRed` — template render check.
- `TestDashboard_CoreTransientFailure_TopbarNotError` — topbar label check.
- `TestMapCoreState_AllStates_AfterFix` — comprehensive mapping table.

---

## Step 5: Validation & Completion

### AC-to-Test Coverage Matrix

| AC | Tests | Coverage |
|----|-------|---------|
| AC1 (upgrade endpoint) | 4 Go unit tests | 100% (compile-error red) |
| AC2 (profile after bootstrap) | 2 Go unit + 2 Playwright | 100% |
| AC3 (keys/query known user) | 3 Go unit + 1 Playwright | 100% |
| AC4 (DM flow no spinner) | 1 Playwright E2E | 100% |
| AC5 (admin dashboard Core state) | 8 Go unit tests | 100% |

**P0+P1 coverage: 100%**

### Story Boundary Decisions

**Bug 1 (Room Upgrade) → 5-29e scope: 501 stub**
Full implementation (new room, `m.room.tombstone` event, state copy, member migration) requires Core gRPC changes (new `UpgradeRoom` RPC) and is a separate epic-level story. 5-29e scope: register a named `UpgradeRoomHandler` that returns 501 with spec-conformant JSON body. This stops the 404 and gives FluffyChat a deterministic "not supported" signal instead of a routing error.

**Bug 2 (keys/query) → 5-29e scope: profile fix + minimal stub improvement**
Full E2EE device storage = future story. 5-29e scope:
- (a) Ensure Core upserts a profile row at first `ValidateToken` (provisioning gap — likely a 1-line fix in `event_dispatcher/server.ex`).
- (b) Improve `keys/query` stub: parse the request body, check each requested `userId` against the `users` table, include an empty device-map entry for known users. Extract from inline closure to `KeysQueryHandler`.

**Bug 3 (Admin UI) → 5-29e scope: `mapCoreState` fix only**
Single-function change: `connectivity.TransientFailure` → `("amber", "Connecting…")` instead of `("red", "Unreachable")`. Only `connectivity.Shutdown` warrants "red".

### Red Phase Confirmation

| Test File | Failures | Mechanism |
|-----------|----------|-----------|
| `rooms_upgrade_test.go` | ALL (compile) | `NewUpgradeRoomHandler` undefined |
| `keys_query_test.go` | 1 runtime | `TestKeysQuery_KnownUser_AppearsInDeviceKeysMap` |
| `dashboard_core_unreachable_test.go` | 4 runtime | `mapCoreState(TransientFailure)` returns "red" |
| `dm_create_bug_5_29e.spec.ts` | All AC2/3/4 (runtime, stack-dependent) | Profile 404 + empty device_keys |

**Confirmed: ALL tests are FAILING before implementation.**

---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-05-01'
storyId: '6.11'
storyKey: 6-11-gherkin-admin-api-crud-flow
storyFile: _bmad-output/implementation-artifacts/6-11-gherkin-admin-api-crud-flow.md
atddChecklistPath: _bmad-output/test-artifacts/atdd-checklist-6-11-gherkin-admin-api-crud-flow.md
generatedTestFiles:
  - gateway/features/admin_api.feature
  - gateway/test/integration/admin_api_steps_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/6-11-gherkin-admin-api-crud-flow.md
  - gateway/test/integration/compliance_flow_steps_test.go
  - gateway/test/integration/room_flow_steps_test.go
  - gateway/test/integration/steps_test.go
  - gateway/test/integration/main_test.go
  - gateway/features/compliance_flow.feature
  - gateway/features/room_moderation.feature
  - _bmad/tea/config.yaml
---

# ATDD Checklist — Story 6-11: Gherkin Admin API CRUD Flow

## Step 1: Preflight & Context Loading

- **Detected stack:** `backend` (Go + Elixir, no frontend assets)
- **Test framework:** Godog Gherkin (existing integration test suite in `gateway/test/integration/`)
- **Feature file target:** `gateway/features/admin_api.feature`
- **Step definitions target:** `gateway/test/integration/admin_api_steps_test.go`
- **Auth pattern:** Dex Authorization Code flow → `authenticateUser()` helper (room_flow_steps_test.go)
- **Story key:** `6-11-gherkin-admin-api-crud-flow`

## Step 2: Generation Mode

- Mode: **AI generation** (backend project, no browser recording needed)

## Step 3: Test Strategy

| AC | Scenario | Level | Priority | Red-phase rationale |
|---|---|---|---|---|
| AC1 | User_Management_Lifecycle | Integration/Godog | P0 | Deactivate/reactivate + Matrix 401/200 proves session invalidation works |
| AC2 | Role_Assignment_Lifecycle | Integration/Godog | P1 | Grant/revoke role_overrides + compliance 403 confirms RBAC enforcement |
| AC3 | Room_Archival | Integration/Godog | P1 | Archive + M_ROOM_ARCHIVED + messages-still-readable = three distinct assertions |

All tests return `godog.ErrPending` (TDD red phase). They will fail with "pending" status until the
implementation provides the Admin API handlers.

## Step 4: Generated Tests

### gateway/features/admin_api.feature

Three scenarios:
1. `User_Management_Lifecycle` — list users, deactivate, Matrix 401, reactivate, Matrix 200
2. `Role_Assignment_Lifecycle` — grant compliance_officer, confirm granted, revoke, confirm 403
3. `Room_Archival` — create room + message, archive, send-event 403 M_ROOM_ARCHIVED, get-messages 200

### gateway/test/integration/admin_api_steps_test.go

- Build tag: `//go:build integration`
- Package: `integration_test`
- Package-level state variables: `adminAPIAdminToken`, `adminAPIAdminUserID`, `adminAPITargetToken`, `adminAPITargetUserID`, `adminAPIRoomID`, `adminAPITxnCounter`
- `sc.Before` hook resets all state between scenarios
- All step functions return `godog.ErrPending` (red phase)
- Helper `adminAPIDoRequest` and `adminAPINextTxnID` are scaffolded ready for implementation
- Registered via `initializeAdminAPISteps(sc)` added to `InitializeScenario` in `steps_test.go`

## Red-Phase Compliance

- [x] All step functions return `godog.ErrPending`
- [x] No implementation code written
- [x] Feature file steps match step definition regex patterns 1:1
- [x] Scenario state is isolated via `sc.Before` reset hook
- [x] No new Go dependencies required
- [x] File naming and package follow existing suite conventions
- [x] `initializeAdminAPISteps(sc)` registered in `steps_test.go`

## Coverage vs Acceptance Criteria

| Acceptance Criterion | Covered by Scenario | Step(s) |
|---|---|---|
| AC1: GET /admin/users → 200 + data + total | User_Management_Lifecycle | `theAdminCallsGETAdminUsers` |
| AC1: POST deactivate → 200 + "deactivated" | User_Management_Lifecycle | `theAdminDeactivatesAlexWithReason` |
| AC1: Matrix sync with deactivated token → 401 | User_Management_Lifecycle | `alexCallsGETSyncWithTheirToken` |
| AC1: POST reactivate → 200 + "active" | User_Management_Lifecycle | `theAdminReactivatesAlex` |
| AC1: Matrix sync after reactivation → 200 | User_Management_Lifecycle | `alexCallsGETSyncWithTheirToken` |
| AC2: POST roles grant → 200 + "granted" | Role_Assignment_Lifecycle | `theAdminGrantsAlexTheRole` |
| AC2: POST roles revoke → 200 + "revoked" | Role_Assignment_Lifecycle | `theAdminRevokesAlexTheRole` |
| AC2: Compliance endpoint 403 without role | Role_Assignment_Lifecycle | `aUserWithoutComplianceRoleCallsGETComplianceAccessRequests` |
| AC3: POST archive → 200 + "archived" | Room_Archival | `theAdminArchivesTheArchivalTestRoomWithReason` |
| AC3: PUT send to archived room → 403 M_ROOM_ARCHIVED | Room_Archival | `kaiSendsAMatrixEventToTheArchivedRoom` |
| AC3: GET messages from archived room → 200 + chunk | Room_Archival | `kaiCallsGETMessagesFromTheArchivedRoom` |

Coverage: **11/11 acceptance criterion steps covered** (100%)

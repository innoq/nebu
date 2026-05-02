---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-05-01'
storyId: '6.5'
storyKey: 6-5-user-deactivation-reactivation-session-invalidierung
storyFile: _bmad-output/implementation-artifacts/6-5-user-deactivation-reactivation-session-invalidierung.md
atddChecklistPath: _bmad-output/test-artifacts/atdd-checklist-6-5-user-deactivation-reactivation-session-invalidierung.md
generatedTestFiles:
  - gateway/internal/api/deactivation_handler_test.go
  - gateway/internal/middleware/auth_deactivated_test.go
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs
inputDocuments:
  - _bmad-output/implementation-artifacts/6-5-user-deactivation-reactivation-session-invalidierung.md
  - _bmad/tea/config.yaml
  - gateway/internal/api/users_handler_test.go
  - gateway/internal/middleware/auth_test.go
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/grpc_handler_test.exs
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/write_audit_log_grpc_test.exs
---

# ATDD Checklist — Story 6.5: User Deactivation + Reactivation + Session-Invalidierung

## Step 1: Preflight & Context

**Detected stack:** `fullstack` (Go gateway backend + Elixir/OTP core)
**Test execution mode:** sequential (no subagent capabilities)
**Story key:** `6-5-user-deactivation-reactivation-session-invalidierung`
**Story ID:** `6.5`
**Story status:** ready-for-dev

### Prerequisites — verified

- [x] Story approved with clear acceptance criteria (12 ACs)
- [x] Go test pattern: `*_test.go` files in `package api_test` / `package middleware_test`
- [x] Elixir test pattern: ExUnit under `core/apps/event_dispatcher/test/`
- [x] `api.ErrUserNotFound` does NOT exist yet → red phase confirmed
- [x] `api.DeactivationRepository` interface does NOT exist yet → red phase confirmed
- [x] `middleware.WithUserStatusCheck` does NOT exist yet → red phase confirmed
- [x] `middleware.UserStatusChecker` does NOT exist yet → red phase confirmed
- [x] `pb.InvalidateUserSessionsRequest` does NOT exist yet → red phase confirmed
- [x] `Nebu.EventDispatcher.Server.invalidate_user_sessions/2` does NOT exist yet → red phase confirmed

## Step 2: Generation Mode

**Mode chosen:** AI generation (backend stack, all ACs are standard CRUD/API/gRPC patterns)

## Step 3: Test Strategy

### Acceptance Criteria → Test Mapping

| AC | Description | Test Level | Priority | Test Name |
|----|-------------|------------|----------|-----------|
| AC#1 | POST /deactivate — 200 success | Unit (Go handler) | P0 | `TestDeactivateAdminUser_ActiveUser_Returns200` |
| AC#1 | POST /deactivate — 409 already deactivated | Unit (Go handler) | P0 | `TestDeactivateAdminUser_AlreadyDeactivated_Returns409` |
| AC#1 | POST /deactivate — 404 not found | Unit (Go handler) | P0 | `TestDeactivateAdminUser_UserNotFound_Returns404` |
| AC#1 | POST /deactivate — 400 short reason | Unit (Go handler) | P0 | `TestDeactivateAdminUser_ShortReason_Returns400` |
| AC#1 | POST /deactivate — 400 missing body | Unit (Go handler) | P0 | `TestDeactivateAdminUser_MissingBody_Returns400` |
| AC#1 | POST /deactivate — audit log emitted | Unit (Go handler) | P0 | `TestDeactivateAdminUser_AuditLogEmitted` |
| AC#1 | POST /deactivate — gRPC InvalidateUserSessions called | Unit (Go handler) | P0 | `TestDeactivateAdminUser_InvalidateSessionsCalled` |
| AC#2 | POST /reactivate — 200 success | Unit (Go handler) | P0 | `TestReactivateAdminUser_DeactivatedUser_Returns200` |
| AC#2 | POST /reactivate — 409 anonymized | Unit (Go handler) | P0 | `TestReactivateAdminUser_AnonymizedUser_Returns409` |
| AC#2 | POST /reactivate — 409 keys_deleted | Unit (Go handler) | P0 | `TestReactivateAdminUser_KeysDeletedUser_Returns409` |
| AC#2 | POST /reactivate — 409 already active | Unit (Go handler) | P0 | `TestReactivateAdminUser_AlreadyActive_Returns409` |
| AC#2 | POST /reactivate — 404 not found | Unit (Go handler) | P0 | `TestReactivateAdminUser_UserNotFound_Returns404` |
| AC#2 | POST /reactivate — audit log emitted | Unit (Go handler) | P0 | `TestReactivateAdminUser_AuditLogEmitted` |
| AC#6 | JWT middleware rejects deactivated user → 401 M_UNKNOWN_TOKEN | Unit (Go middleware) | P0 | `TestWithUserStatusCheck_DeactivatedUser_Returns401` |
| AC#6 | Active user passes through middleware | Unit (Go middleware) | P0 | `TestWithUserStatusCheck_ActiveUser_PassesThrough` |
| AC#6 | nil checker → pass through (backward compat) | Unit (Go middleware) | P1 | `TestWithUserStatusCheck_NilChecker_PassesThrough` |
| AC#6 | DB error → fail open | Unit (Go middleware) | P1 | `TestWithUserStatusCheck_DBError_FailsOpen` |
| AC#6 | Empty userID → skip check | Unit (Go middleware) | P1 | `TestWithUserStatusCheck_EmptyUserID_PassesThrough` |
| AC#8 | Routes registered (not 404 without role) | Unit (Go router) | P0 | `TestDeactivateRoutes_Registered` |
| AC#8 | Routes reject wrong role with 403 | Unit (Go router) | P1 | `TestDeactivateRoutes_RequireInstanceAdmin` |
| AC#8/compat | nil Deactivation field → 501 stub | Unit (Go handler) | P1 | `TestDeactivateAdminUser_NilDeactivationRepo_Returns501` |
| AC#5 / AC#11 | ExUnit: invalidate_user_sessions happy path | Unit (Elixir) | P0 | `invalidate_user_sessions — returns ok: true and calls destroy_session/1` |
| AC#5 | ExUnit: destroy_session called with correct user_id | Unit (Elixir) | P0 | `invalidate_user_sessions — destroy_session/1 receives the exact user_id` |
| AC#5 | ExUnit: DB failure → GRPC.RPCError internal | Unit (Elixir) | P0 | `invalidate_user_sessions — DB failure raises GRPC.RPCError` |
| AC#5 | ExUnit: RPCError has internal status code | Unit (Elixir) | P0 | `invalidate_user_sessions — raised GRPC.RPCError has internal status code` |
| AC#11 | ExUnit: AT#9 full invalidation contract | Unit (Elixir) | P0 | `invalidate_user_sessions — AC#11/AT#9: handler delegates to SessionSupervisor` |

### ACs covered vs. not directly unit-tested

| AC | Coverage | Note |
|----|----------|------|
| AC#3 | Not unit-tested | SQL migration — verified by `migrations_test.go` at compile/migrate time |
| AC#4 | Not unit-tested | Proto codegen — verified by `make proto` + compile |
| AC#7 | Not unit-tested | OpenAPI codegen — verified by `make gen-api` + compile (router_test.go catches missing interface methods) |
| AC#12 | Not unit-tested | Build pass — verified by `make test-unit-go && make test-unit-elixir` |

All P0 and P1 acceptance criteria have at least one test. AC#3, #4, #7, #12 are infrastructure/codegen ACs with no directly assertable unit test logic.

## Step 4: Generated Tests

### TDD Red Phase Status

All generated test files contain tests that will **fail before implementation** because the required types, functions, and interfaces do not exist yet. Specific compile-time and runtime failure modes:

**`gateway/internal/api/deactivation_handler_test.go`** — will NOT compile until:
- `api.ErrUserNotFound` sentinel error is defined
- `api.DeactivationRepository` interface is defined
- `api.AdminServer` gains `Deactivation DeactivationRepository` field
- `api.RegisterAdminRoutes` registers the two new POST routes
- `make gen-api` regenerates `api_gen.go` with DeactivateAdminUser/ReactivateAdminUser
- `pb.InvalidateUserSessionsRequest` / `pb.InvalidateUserSessionsResponse` exist (generated by `make proto`)

**`gateway/internal/middleware/auth_deactivated_test.go`** — will NOT compile until:
- `middleware.UserStatusChecker` interface is defined
- `middleware.WithUserStatusCheck` function is defined

**`core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs`** — will NOT compile until:
- `make proto` regenerates Elixir stubs with `Core.InvalidateUserSessionsRequest` and `Core.InvalidateUserSessionsResponse`
- `Nebu.EventDispatcher.Server.invalidate_user_sessions/2` handler is implemented

### File Summary

| File | Package | Tests | Priority |
|------|---------|-------|----------|
| `gateway/internal/api/deactivation_handler_test.go` | `api_test` | 13 | P0/P1 |
| `gateway/internal/middleware/auth_deactivated_test.go` | `middleware_test` | 5 | P0/P1 |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs` | ExUnit | 5 | P0 |

## Step 5: Validation

### Checklist

- [x] Every story Acceptance Criterion has at least one test (P0/P1)
- [x] No hard waits (`Process.sleep` used only for Elixir polling helper, not in new tests)
- [x] All tests are deterministic (no random state, no shared ETS between tests — `async: true`)
- [x] No DB-seeding shortcuts: mock interfaces injected via Go struct fields / Application.put_env
- [x] No cookie forging or E2E browser tests (all handler-level unit tests)
- [x] Elixir tests use configurable-module pattern (FakeSessionSupervisor via Application.put_env)
- [x] Go tests use external test packages (`package api_test`, `package middleware_test`)
- [x] Build constraints present (`//go:build go1.22`) on all Go test files
- [x] `nil`-guard test verifies backward compat with router_test.go's `&AdminServer{}`
- [x] Fail-open test for DB outage scenario (AC#6 Dev Notes requirement)

### Key Risks & Assumptions

1. **Proto codegen order:** `make proto` must run before Go tests compile (pb.InvalidateUserSessionsRequest must exist).
2. **OpenAPI codegen order:** `make gen-api` must run before Go tests compile (generated DeactivateAdminUserRequestObject etc. must exist).
3. **Mandatory spec-first order:** proto → openapi → failing tests → implementation.
4. **AdminServer field name:** Tests assume `Deactivation DeactivationRepository` — must match server.go exactly.
5. **ErrUserNotFound sentinel:** Tests use `api.ErrUserNotFound` — must be exported from `deactivation_repo.go`.
6. **Elixir test isolation:** `async: true` — each test injects its own FakeSessionSupervisor and __test_pid__; safe for parallel execution.
7. **mockCoreClientForDeactivation in api_test package:** This type overlaps structurally with `mockCoreClient` in `users_handler_test.go`. Since both are in `package api_test`, the name must not conflict. The new type is `mockCoreClientForDeactivation` (distinct name).

### Next Recommended Workflow

`/bmad-dev-story` — implement migration, proto, codegen, handlers, middleware in the order specified by Dev Notes "Mandatory spec-first workflow".

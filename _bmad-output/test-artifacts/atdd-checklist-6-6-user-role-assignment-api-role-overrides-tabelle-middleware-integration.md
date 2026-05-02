---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04c-aggregate']
lastStep: 'step-04c-aggregate'
lastSaved: '2026-05-01'
storyId: '6.6'
storyKey: '6-6-user-role-assignment-api-role-overrides-tabelle-middleware-integration'
storyFile: '_bmad-output/implementation-artifacts/6-6-user-role-assignment-api-role-overrides-tabelle-middleware-integration.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-6-6-user-role-assignment-api-role-overrides-tabelle-middleware-integration.md'
generatedTestFiles:
  - gateway/internal/api/roles_handler_test.go
  - gateway/internal/api/middleware_role_override_test.go
inputDocuments:
  - _bmad-output/planning-artifacts/epics.md
  - _bmad-output/implementation-artifacts/6-6-user-role-assignment-api-role-overrides-tabelle-middleware-integration.md
  - _bmad-output/implementation-artifacts/6-5-user-deactivation-reactivation-session-invalidierung.md
  - gateway/internal/api/middleware.go
  - gateway/internal/api/server.go
  - gateway/internal/api/router.go
  - gateway/internal/api/users_repo.go
  - gateway/internal/api/deactivation_repo.go
  - gateway/internal/middleware/auth.go
---

# ATDD Checklist: Story 6.6 — User Role Assignment API

## TDD Red Phase (Current State)

Red-phase test scaffolds generated and committed.

- API/Handler Tests: 10 tests — `gateway/internal/api/roles_handler_test.go` (all `t.Skip`ped)
- Middleware Tests: 9 tests — `gateway/internal/api/middleware_role_override_test.go` (all `t.Skip`ped)
- Total: 19 red-phase test scaffolds

**Expected compile error (intentional RED phase):**
```
internal/api/roles_handler_test.go:130:16: undefined: api.RoleOverrideRepository
```
This error is expected and will be resolved once `roles_repo.go` is created.

## Acceptance Criteria Coverage

| AC | Test | Priority | Test Function |
|----|------|---------|--------------|
| AC#1 migration | Manual (SQL) | — | Verified via `make test-unit-go` migrations test |
| AC#2 grant → 200 | Unit | P0 | `TestAssignAdminUserRole_GrantRole_Returns200` |
| AC#2 revoke → 200 | Unit | P0 | `TestAssignAdminUserRole_RevokeRole_Returns200` |
| AC#2 revoke not found → 404 | Unit | P0 | `TestAssignAdminUserRole_RevokeNonExistent_Returns404` |
| AC#2 self-revoke → 403 | Unit | P0 | `TestAssignAdminUserRole_SelfRevoke_Returns403` |
| AC#2 invalid role → 400 | Unit | P1 | `TestAssignAdminUserRole_InvalidRole_Returns400` |
| AC#2 invalid action → 400 | Unit | P1 | `TestAssignAdminUserRole_InvalidAction_Returns400` |
| AC#2 missing body → 400 | Unit | P1 | `TestAssignAdminUserRole_MissingBody_Returns400` |
| AC#2 unknown user → 404 | Unit | P1 | `TestAssignAdminUserRole_UnknownUser_Returns404` |
| AC#2 audit log grant | Unit | P1 | `TestAssignAdminUserRole_Grant_CallsAuditLog` |
| AC#2 audit log revoke | Unit | P1 | `TestAssignAdminUserRole_Revoke_CallsAuditLog` |
| AC#3 DB override allows | Unit | P0 | `TestRequireRole_DBOverride_AllowsAccess` |
| AC#3 no JWT, no DB → 403 | Unit | P0 | `TestRequireRole_NoJWTRole_NoDBOverride_Returns403` |
| AC#3 JWT match skips DB | Unit | P0 | `TestRequireRole_JWTRoleMatch_SkipsDBLookup` |
| AC#3 DB error fail-open | Unit | P1 | `TestRequireRole_DBError_FailsOpen` |
| AC#3 cache 60s TTL | Unit | P1 | `TestRequireRole_OverrideLookup_IsCached` |
| AC#3 cache per-role key | Unit | P2 | `TestRequireRole_CacheKey_IsPerRole` |
| AC#3 nil checker backward compat | Unit | P0 | `TestRequireRole_NilChecker_JWTOnly_Allow`, `_Block`, `_EmptyRole_Returns401` |
| AC#4,#5 roles merged | Unit | P1 | Via `users_handler_test.go` (to be extended) |
| AC#6 openapi + gen | Build | — | `make gen-api` + `go build ./...` |
| AC#9 build passes | Build | — | `make test-unit-go` |

## Next Steps (Task-by-Task Activation)

During implementation of each task:

1. Remove `t.Skip("RED PHASE...")` from the test for the current task
2. Run: `cd gateway && go test ./internal/api/... -run TestAssignAdminUserRole`
3. Verify the activated test **fails** first (RED), then passes after implementation (GREEN)
4. Commit passing tests

### Activation order:

1. **Migration** (`000035_role_overrides.up.sql`) — no test, verified by `make test-unit-go` migrations test
2. **roles_repo.go** (`RoleOverrideRepository` + `RoleOverrideChecker` interfaces) — unblocks compile
3. **server.go** (`AssignAdminUserRole`) — activate handler tests
4. **middleware.go** (`RequireRole` extended) — activate middleware override tests
5. **router.go** (register route + pass checker) — handler tests become callable via mux
6. **users_repo.go** (roles merge) — extend `users_handler_test.go`
7. **main.go** (wire `Roles` field) — integration

## Implementation Endpoints to Implement

- `POST /api/v1/admin/users/{userId}/roles` — grant/revoke role override (AC#2)
- `RequireRole(role, checker)` — extended middleware (AC#3)
- `RoleOverrideRepository` — DB interface + implementation (AC#2, #4, #5)

## Generated Test Files

- `gateway/internal/api/roles_handler_test.go` — handler tests (RED phase, 10 tests)
- `gateway/internal/api/middleware_role_override_test.go` — middleware tests (RED phase, 9 tests)

### ATDD Artifacts

- Checklist: `_bmad-output/test-artifacts/atdd-checklist-6-6-user-role-assignment-api-role-overrides-tabelle-middleware-integration.md`
- Handler tests: `gateway/internal/api/roles_handler_test.go`
- Middleware tests: `gateway/internal/api/middleware_role_override_test.go`

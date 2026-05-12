---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-05-12'
storyId: '11.10'
storyKey: '11-10-oidc-claim-mapping'
storyFile: 'docs/stories/phase2/epic-11/11-10-oidc-claim-mapping.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-11-10-oidc-claim-mapping.md'
generatedTestFiles:
  - gateway/internal/admin/claim_mapping_handler_test.go
  - gateway/internal/grpc/metadata_test.go  # extended with new FormatUserIDFromClaims tests
  - gateway/features/claim_mapping.feature
  - e2e/features/admin/claim-mapping.feature
  - e2e/step-definitions/admin/claim-mapping.steps.ts
inputDocuments:
  - docs/stories/phase2/epic-11/11-10-oidc-claim-mapping.md
  - gateway/internal/admin/role_mapping.go
  - gateway/internal/admin/role_mapping_test.go
  - gateway/internal/grpc/metadata.go
  - gateway/internal/grpc/metadata_test.go
  - gateway/internal/admin/page_data.go
  - gateway/internal/admin/auth.go
  - gateway/internal/admin/flash.go
  - gateway/internal/admin/stubs.go
  - e2e/features/admin/bootstrap.feature
  - e2e/step-definitions/admin/bootstrap.steps.ts
tddPhase: RED
---

# ATDD Checklist — Story 11-10: OIDC Claim Mapping Configuration

## Summary

- **Story:** 11.10 — OIDC Claim Mapping Configuration
- **Size:** M
- **TDD Phase:** RED (all tests fail before implementation)
- **Stack:** fullstack (Go gateway + Playwright+Cucumber E2E)
- **AC Count:** 10
- **Tests Generated:** 5 files, 25+ test cases

## AC-to-Test Coverage Matrix

| AC | Description | Test File | Test Name(s) | Status |
|----|-------------|-----------|--------------|--------|
| AC1 | Bootstrap Wizard Step 3 shows form with defaults | e2e/features/admin/claim-mapping.feature | Scenario: Bootstrap Wizard shows Claim Mapping step 3 | RED |
| AC2 | Wizard step 3 persists claim mapping in same transaction | gateway/features/claim_mapping.feature | (covered via bootstrap E2E flow completing; unit test in TestBootstrapTransaction_PersistsClaimMapping — TODO: add as integration test stub in next iteration) | PARTIAL |
| AC3 | Admin UI GET/POST /admin/config/claim-mapping | claim_mapping_handler_test.go | TestClaimMappingHandler_GetDefaults, _GetFlash, _PostValid, gateway/features/claim_mapping.feature scenarios 1+2 | RED |
| AC4 | OIDC callback reads claim from DB | (covered by startup loading in AC5) | — | N/A |
| AC5 | JWTMiddleware + login handler use DB-loaded claim | gateway/features/claim_mapping.feature | Scenario: User logs in after oidc_user_id_claim set | RED |
| AC6 | FormatUserIDFromClaims new signature | metadata_test.go | TestFormatUserIDFromClaims_Configured, _FallbackToSub, _FallbackWhenClaimEmptyString, _EmptySubFallback, _SubClaim, _NonStringClaimValueFallsBack | RED |
| AC7 | Backward compat: missing key falls back to name claim | metadata_test.go + claim_mapping.feature | TestFormatUserIDFromClaims_FallbackToSub, Scenario: Gateway falls back… | RED |
| AC8 | Validation rules (required, length, regex, dot) | claim_mapping_handler_test.go | TestClaimMappingHandler_PostInvalid_Empty*, _TooLong, _IllegalChars, _DotInClaimName; claim-mapping.feature validation scenarios; E2E @ac8 scenario | RED |
| AC9 | Playwright+Cucumber E2E: Bootstrap wizard claim step | e2e/features/admin/claim-mapping.feature | Scenario @ac9-bootstrap-claim-mapping | RED |
| AC10 | Playwright+Cucumber E2E: Admin UI claim settings page | e2e/features/admin/claim-mapping.feature | Scenarios @ac10-claim-mapping-settings, @ac10-claim-mapping-update | RED |

## Red Phase Verification

### Go unit tests (gateway/internal/admin/...)
```
internal/admin/claim_mapping_handler_test.go:31:44: undefined: ClaimMappingHandler
internal/admin/claim_mapping_handler_test.go:37:9: undefined: NewClaimMappingHandler
```
**Status: RED (compile error) ✓**

### Go unit tests (gateway/internal/grpc/...)
```
internal/grpc/metadata_test.go:91:54: cannot use claims (variable of type map[string]interface{}) as string value
internal/grpc/metadata_test.go:108:54: cannot use claims (variable of type map[string]interface{}) as string value
... (6 errors total)
```
**Status: RED (compile error) ✓**

### Godog feature (gateway/features/claim_mapping.feature)
- Steps referencing new routes and DB state will fail at integration test run.
**Status: RED (undefined steps + unimplemented routes) ✓**

### Playwright+Cucumber E2E
- Step definitions throw `NOT IMPLEMENTED (Story 11-10 RED)` immediately.
**Status: RED (all step bodies throw) ✓**

## Files Created

| File | AC Coverage | Type | Lines |
|------|-------------|------|-------|
| `gateway/internal/admin/claim_mapping_handler_test.go` | AC3, AC8 | Go httptest unit | ~230 |
| `gateway/internal/grpc/metadata_test.go` (extended) | AC5, AC6, AC7 | Go unit | +120 |
| `gateway/features/claim_mapping.feature` | AC3, AC5, AC7, AC8 | Godog/Gherkin | ~80 |
| `e2e/features/admin/claim-mapping.feature` | AC1, AC8, AC9, AC10 | Playwright+Cucumber | ~75 |
| `e2e/step-definitions/admin/claim-mapping.steps.ts` | AC1, AC8, AC9, AC10 | Cucumber step defs | ~150 |

## Gaps & Notes

- **AC2** (bootstrap transaction atomicity): A dedicated integration test
  `TestBootstrapTransaction_PersistsClaimMapping` was listed in the story's Acceptance Tests
  section. This is best implemented as a Go integration test in
  `gateway/internal/admin/claim_selection_tx_test.go` (which already tests similar
  bootstrap transaction patterns). The dev implementing this story should add it there.
  It is not generated here to avoid orphaned tests in a file that needs real DB setup.

- **AC4** (OIDC callback reads claim): Fully covered by AC5's startup loading mechanism.
  The callback uses the same `userIDClaim` loaded at startup — no separate test needed.

- **Flash allowlist**: `"Claim mapping updated"` must be added to `allowedFlashMessages`
  in `gateway/internal/admin/flash.go` for `TestClaimMappingHandler_GetFlash` to pass.

- **Step reuse**: The E2E steps defined in `bootstrap.steps.ts` (e.g. "the Nebu admin UI
  is accessible", "the operator navigates to", "the operator clicks", "the page shows")
  are automatically shared by playwright-bdd and do NOT need to be redefined in
  `claim-mapping.steps.ts`.

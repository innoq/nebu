---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-05-01'
storyId: '6.2'
storyKey: 6-2-admin-api-response-format-cursor-pagination
storyFile: _bmad-output/implementation-artifacts/6-2-admin-api-response-format-cursor-pagination.md
atddChecklistPath: _bmad-output/test-artifacts/atdd-checklist-6-2-admin-api-response-format-cursor-pagination.md
generatedTestFiles:
  - gateway/internal/api/pagination_test.go
  - gateway/internal/api/response_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/6-2-admin-api-response-format-cursor-pagination.md
  - gateway/internal/api/openapi_handler_test.go
  - gateway/internal/api/server.go
  - _bmad/tea/config.yaml
---

# ATDD Checklist — Story 6.2: Admin API Response Format + Cursor-Pagination

## Step 1: Preflight & Context

**Stack detection:** `backend` (Go 1.26 project, no frontend indicators)

**Prerequisites:**
- Story 6.2 in `ready-for-dev` state with clear acceptance criteria ✅
- Test framework: Go `testing` package, `package api_test` (established pattern from Story 6.1) ✅
- No implementation of `api.EncodeCursor`, `api.DecodeCursor`, `api.ErrInvalidCursor`, `api.APIResponse`, `api.Meta`, `api.APIError` exists ✅

**Story key:** `6-2-admin-api-response-format-cursor-pagination`

**Story file:** `_bmad-output/implementation-artifacts/6-2-admin-api-response-format-cursor-pagination.md`

---

## Step 2: Generation Mode

**Mode selected:** AI Generation (Sequential)

**Reason:** Pure backend project; all scenarios are unit-level Go tests for pure functions and struct serialization. No browser/UI interaction, no Playwright, no Pact. Sequential mode chosen (tea_execution_mode: auto → backend → sequential).

---

## Step 3: Test Strategy

### Acceptance Criteria → Test Scenario Mapping

| AC | Description | Test Level | Priority | Test Name |
|---|---|---|---|---|
| AC#1 | `APIResponse[T]`, `Meta`, `APIError` types with exact JSON tags | Unit | P0 | `TestAPIResponse_StructFieldNames` |
| AC#1 | `meta` absent when nil (omitempty) | Unit | P1 | `TestAPIResponse_ErrorResponse_MetaAbsent` |
| AC#1 | `error` absent on success (omitempty) | Unit | P0 | `TestAPIResponse_SuccessResponse_ErrorAbsent` |
| AC#1 | `meta` included when non-nil | Unit | P1 | `TestAPIResponse_MetaIncludedWhenNonNil` |
| AC#1 | Meta zero-fields omitted (omitempty) | Unit | P2 | `TestAPIResponse_MetaOmitsZeroFields` |
| AC#2 | `EncodeCursor`/`DecodeCursor` round-trip | Unit | P0 | `TestEncodeCursor_DecodeCursor_RoundTrip` |
| AC#2 | `ErrInvalidCursor` is package-level var | Unit | P0 | `TestErrInvalidCursor_IsPackageLevelVar` |
| AC#2 | Cursor is Base64URL no-pad | Unit | P1 | `TestEncodeCursor_IsBase64URLNoPad` |
| AC#3 | Malformed base64 → `ErrInvalidCursor` | Unit | P0 | `TestDecodeCursor_MalformedBase64_ReturnsErrInvalidCursor` |
| AC#3 | Valid base64 but invalid JSON → `ErrInvalidCursor` | Unit | P0 | `TestDecodeCursor_ValidBase64ButInvalidJSON_ReturnsErrInvalidCursor` |
| AC#3 | Valid JSON but missing fields → `ErrInvalidCursor` | Unit | P1 | `TestDecodeCursor_ValidJSONButMissingFields_ReturnsErrInvalidCursor` |
| AC#3 | Empty string → `ErrInvalidCursor` | Unit | P1 | `TestDecodeCursor_EmptyString_ReturnsErrInvalidCursor` |
| AC#4 | Error response: `data` is null (not omitted) | Unit | P0 | `TestAPIResponse_ErrorResponse_DataIsNull` |
| AC#5 | Round-trip test (explicit in AC) | Unit | P0 | `TestEncodeCursor_DecodeCursor_RoundTrip` (same) |
| AC#5 | `data:null` on error (explicit in AC) | Unit | P0 | `TestAPIResponse_ErrorResponse_DataIsNull` (same) |

**Notes on AC#3 (400 M_BAD_JSON on list endpoints):** AC#3 states that invalid cursors yield 400 M_BAD_JSON on list endpoints (Stories 6.4, 6.7). This story only defines the helpers; the HTTP-level integration of `ErrInvalidCursor → 400` is tested in 6.4/6.7. Here we only test the helper's contract.

**AC#5 coverage:** All 5 explicit sub-requirements from AC#5 are covered:
- Round-trip: `TestEncodeCursor_DecodeCursor_RoundTrip`
- Malformed base64: `TestDecodeCursor_MalformedBase64_ReturnsErrInvalidCursor`
- Invalid JSON: `TestDecodeCursor_ValidBase64ButInvalidJSON_ReturnsErrInvalidCursor`
- `data:null` on error: `TestAPIResponse_ErrorResponse_DataIsNull`

---

## Step 4: Red-Phase Test Generation

**TDD Phase:** RED

**Execution Mode:** Sequential (backend stack, no subagents needed)

### Generated Test Files

#### `gateway/internal/api/pagination_test.go`

Covers: AC#2, AC#3, AC#5 (pagination/cursor helpers)

| Test | AC | Priority | Red Mechanism |
|---|---|---|---|
| `TestEncodeCursor_DecodeCursor_RoundTrip` | AC#2, AC#5 | P0 | `t.Skip` + compile error (`api.EncodeCursor` undefined) |
| `TestDecodeCursor_MalformedBase64_ReturnsErrInvalidCursor` | AC#3, AC#5 | P0 | `t.Skip` + compile error (`api.ErrInvalidCursor` undefined) |
| `TestDecodeCursor_ValidBase64ButInvalidJSON_ReturnsErrInvalidCursor` | AC#3 | P0 | `t.Skip` + compile error |
| `TestDecodeCursor_ValidJSONButMissingFields_ReturnsErrInvalidCursor` | AC#3 | P1 | `t.Skip` + compile error |
| `TestDecodeCursor_EmptyString_ReturnsErrInvalidCursor` | AC#3 | P1 | `t.Skip` + compile error |
| `TestEncodeCursor_IsBase64URLNoPad` | AC#2 | P1 | `t.Skip` + compile error |
| `TestErrInvalidCursor_IsPackageLevelVar` | AC#2 | P0 | `t.Skip` + compile error |

#### `gateway/internal/api/response_test.go`

Covers: AC#1, AC#4, AC#5 (response envelope types)

| Test | AC | Priority | Red Mechanism |
|---|---|---|---|
| `TestAPIResponse_ErrorResponse_DataIsNull` | AC#4, AC#5 | P0 | `t.Skip` + compile error (`api.APIResponse` undefined) |
| `TestAPIResponse_ErrorResponse_MetaAbsent` | AC#1 | P1 | `t.Skip` + compile error |
| `TestAPIResponse_SuccessResponse_ErrorAbsent` | AC#1 | P0 | `t.Skip` + compile error |
| `TestAPIResponse_MetaIncludedWhenNonNil` | AC#1 | P1 | `t.Skip` + compile error |
| `TestAPIResponse_MetaOmitsZeroFields` | AC#1 | P2 | `t.Skip` + compile error |
| `TestAPIResponse_StructFieldNames` | AC#1 | P0 | `t.Skip` + compile error |

### Red Phase Verified

```
$ docker run --rm -v "$(pwd)/gateway:/app" -w /app golang:1.26 go test ./internal/api/...

# github.com/nebu/nebu/internal/api_test [github.com/nebu/nebu/internal/api.test]
internal/api/pagination_test.go:33:16: undefined: api.EncodeCursor
internal/api/pagination_test.go:39:34: undefined: api.DecodeCursor
internal/api/pagination_test.go:58:34: undefined: api.DecodeCursor
internal/api/pagination_test.go:60:25: undefined: api.ErrInvalidCursor
...
FAIL github.com/nebu/nebu/internal/api [build failed]
```

Tests are **definitively failing** before implementation exists. Compile errors ensure no accidental green.

---

## Coverage Summary

| AC | Covered | Tests | Gap |
|---|---|---|---|
| AC#1 | YES | 5 tests | none |
| AC#2 | YES | 3 tests | none |
| AC#3 | YES | 4 tests | none |
| AC#4 | YES | 1 test | none |
| AC#5 | YES | all sub-requirements covered via AC#2–AC#4 tests | none |

**Overall AC Coverage: 5/5 (100%)**

**Priority breakdown:**
- P0: 7 tests
- P1: 5 tests
- P2: 1 test
- P3: 0 tests

---

## Developer Handoff Instructions

When implementing Story 6.2:

1. Create `gateway/internal/api/pagination.go` — implement `ErrInvalidCursor`, `EncodeCursor`, `DecodeCursor`
2. Create `gateway/internal/api/response.go` — implement `APIResponse[T]`, `Meta`, `APIError`, `WriteJSON`
3. Remove `t.Skip(...)` from each test as you implement the corresponding symbol
4. Run `make test-unit-go` after each file to confirm green phase
5. All 13 tests must pass green before the story is `done`

**File to activate first (build-blocking):** Both files must compile before any test can run. Implement skeleton types/functions first, then refine until tests pass.

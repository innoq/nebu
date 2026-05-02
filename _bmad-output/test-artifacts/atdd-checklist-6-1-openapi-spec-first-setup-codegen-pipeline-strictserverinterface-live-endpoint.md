---
stepsCompleted: ['step-01-preflight-and-context', 'step-03-test-strategy', 'step-04c-aggregate']
lastStep: 'step-04c-aggregate'
lastSaved: '2026-05-01'
storyId: '6.1'
storyKey: '6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint'
storyFile: '_bmad-output/implementation-artifacts/6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint.md'
generatedTestFiles:
  - gateway/internal/api/openapi_handler_test.go
---

# ATDD Checklist: Story 6.1 — OpenAPI Spec-First Setup

## Context

- **Stack:** Backend (Go 1.26)
- **Test Framework:** Go standard `testing` + `net/http/httptest`
- **TDD Phase:** RED — all tests skip with `t.Skip()` until implementation exists
- **Execution Mode:** Sequential (backend-only project, no subagents needed)

---

## TDD Red Phase Status

All red-phase test scaffolds generated. `go test ./internal/api/...` currently exits with a **build failure** because no non-test Go files exist in `gateway/internal/api/` yet.

This is the correct TDD RED state:

```
api [build failed]
  github.com/nebu/nebu/internal/api: no non-test Go files in .../internal/api
```

---

## Acceptance Criteria Coverage

| AC | Description | Test(s) | Priority | Status |
|---|---|---|---|---|
| AC#1 | openapi.yaml is OpenAPI 3.1, contains title/version/servers/BearerAuth/placeholder paths | `TestOpenAPIYAMLHandler_SpecIsOpenAPI31`, `TestOpenAPIYAMLHandler_SpecContainsAdminPaths`, `TestOpenAPIYAMLHandler_SpecContainsBearerAuth` | P1 | RED |
| AC#2 | `make gen-api` runs oapi-codegen with config file → output to `internal/api/api_gen.go` | Build verification (`go build ./...`) | P0 | Not in unit test — compile gate |
| AC#3 | `gateway/api/oapi-codegen.yaml` exists with `strict-server: true` | Config file existence + gen-api success | P0 | Not in unit test — file gate |
| AC#4 | `GET /api/v1/openapi.yaml` — unauthenticated, Content-Type: application/yaml | `TestOpenAPIYAMLHandler_ServeSpec`, `TestOpenAPIYAMLHandler_NoAuthRequired` | P0 | RED |
| AC#5 | `make build-gateway` has `gen-api` as prerequisite | Makefile inspection | P1 | Not in unit test — Makefile gate |
| AC#6 | `go build ./...` succeeds after `make gen-api` | Build gate (compile check) | P0 | Not in unit test — compile gate |
| AC#7 | Unit test in `gateway/internal/api/` asserts 200 + body contains "Nebu Admin API" | `TestOpenAPIYAMLHandler_ServeSpec` | P0 | RED |

---

## Generated Test File

**`gateway/internal/api/openapi_handler_test.go`** — package `api_test`

### Tests

| Test Name | Priority | AC Covered | Failure Reason (RED) |
|---|---|---|---|
| `TestOpenAPIYAMLHandler_ServeSpec` | P0 | AC#4, AC#7 | `api.OpenAPIYAMLHandler` undefined — package has no Go source files |
| `TestOpenAPIYAMLHandler_NoAuthRequired` | P0 | AC#4 (FR51) | Same — compile error |
| `TestOpenAPIYAMLHandler_SpecIsOpenAPI31` | P1 | AC#1 | Same — compile error; also spec not yet upgraded |
| `TestOpenAPIYAMLHandler_SpecContainsAdminPaths` | P1 | AC#1 | Same — placeholder paths not added |
| `TestOpenAPIYAMLHandler_SpecContainsBearerAuth` | P1 | AC#1 | Same — BearerAuth not added |

---

## Notes on Non-Unit-Test Gates

AC#2, AC#3, AC#5, AC#6 are verified by the build pipeline, not unit tests:

- **AC#2 + AC#6:** `make gen-api && go build ./...` — will fail until `oapi-codegen.yaml` and the updated Makefile exist.
- **AC#3:** `gateway/api/oapi-codegen.yaml` must exist for `make gen-api` to succeed.
- **AC#5:** Verified by inspecting the Makefile after the update task.

The StrictServerInterface compile check (story Acceptance Test #2) is enforced by the Go compiler: once `make gen-api` produces `api_gen.go` with `StrictServerInterface`, the build will fail until `AdminServer` in `server.go` implements every method.

---

## Next Steps — Task-by-Task Activation

During implementation of each Story 6.1 task, activate tests in this order:

### Task 1: Upgrade openapi.yaml to 3.1 + add Admin API paths + BearerAuth

Activate: `TestOpenAPIYAMLHandler_SpecIsOpenAPI31`, `TestOpenAPIYAMLHandler_SpecContainsAdminPaths`, `TestOpenAPIYAMLHandler_SpecContainsBearerAuth`

```go
// Remove this line in each test:
t.Skip("[RED] openapi.yaml not upgraded to 3.1 yet — Story 6.1 in progress")
```

### Task 2: Create gateway/api/oapi-codegen.yaml + update Makefile gen-api

Gate: `make gen-api` runs successfully and generates `gateway/internal/api/api_gen.go`.
No unit test — verify via `make gen-api && go build ./...`.

### Task 3: Create gateway/api/spec.go (go:embed) + gateway/internal/api/openapi_handler.go

Activate: `TestOpenAPIYAMLHandler_ServeSpec`, `TestOpenAPIYAMLHandler_NoAuthRequired`

```go
// Remove this line in each test:
t.Skip("[RED] OpenAPIYAMLHandler not implemented yet — Story 6.1 in progress")
```

### Task 4: Create gateway/internal/api/server.go — AdminServer stub

Gate: `go build ./...` must succeed. The compiler will flag every unimplemented method in `StrictServerInterface`.

### Task 5: Register GET /api/v1/openapi.yaml in main.go + run full test suite

Run: `go test ./...` — all activated tests must pass (green phase).

---

## Implementation Guidance

### Endpoints to implement

- `GET /api/v1/openapi.yaml` — served by `api.OpenAPIYAMLHandler`, unauthenticated

### Files to create

- `gateway/api/oapi-codegen.yaml` — codegen config
- `gateway/api/spec.go` — `package apispec`, `//go:embed openapi.yaml var Spec []byte`
- `gateway/internal/api/api_gen.go` — generated by `make gen-api` (commit this)
- `gateway/internal/api/server.go` — `AdminServer` stub implementing `StrictServerInterface`
- `gateway/internal/api/openapi_handler.go` — `OpenAPIYAMLHandler` serving embedded spec

### Files to modify

- `gateway/api/openapi.yaml` — upgrade to 3.1, add paths + BearerAuth
- `Makefile` — update gen-api target, add gen-api to build-gateway prerequisites

### Files to delete

- `gateway/internal/admin/api_gen.go` — superseded (zero external references, safe to delete)

### Module path

```
module github.com/nebu/nebu
```

Import for the new package: `github.com/nebu/nebu/internal/api`

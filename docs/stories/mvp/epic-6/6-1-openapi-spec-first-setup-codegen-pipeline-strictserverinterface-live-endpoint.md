---
security_review: required
---

# Story 6.1: OpenAPI Spec-First Setup (codegen Pipeline + StrictServerInterface + Live-Endpoint)

Status: review

## Story

As an instance admin,
I want the Admin API to be defined by an OpenAPI 3.1 specification that is both the source of truth for generated server code and live-browsable,
so that the API contract is always consistent with the implementation and tooling can be generated automatically.

## Acceptance Criteria

1. `gateway/api/openapi.yaml` is an OpenAPI **3.1** document defining at minimum:
   - `info.title: "Nebu Admin API"`, `info.version: "1.0.0"`
   - `servers: [{url: "/api/v1"}]`
   - Security scheme: `BearerAuth` (JWT, `http` scheme, `bearer` bearerFormat)
   - Placeholder paths for all Admin API route groups: `/admin/users`, `/admin/rooms`, `/admin/config`, `/admin/metrics`, `/compliance/access-requests`

2. `make gen-api` runs oapi-codegen (via `DOCKER_GO` container, no local install) with `--generate strict-server,types,spec` against `openapi.yaml`; output goes to `gateway/internal/api/api_gen.go` (new package `api`, not the existing `admin` package)

3. `gateway/api/oapi-codegen.yaml` exists as an oapi-codegen config file specifying `output-options.strict-server: true` so the generated `StrictServerInterface` requires every operation to be fully implemented — missing implementations cause compile errors

4. `GET /api/v1/openapi.yaml` is a live unauthenticated endpoint (FR51): serves the raw `openapi.yaml` content embedded via `go:embed`, with `Content-Type: application/yaml`

5. `make build-gateway` declares `gen-api` as a prerequisite so stubs are always regenerated before Docker image build

6. `go build ./...` succeeds after `make gen-api` with zero compiler errors

7. A unit test in `gateway/internal/api/` fetches `GET /api/v1/openapi.yaml` and asserts the response body contains `"Nebu Admin API"` and status is `200`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **openapi.yaml live endpoint** — Go unit test (`net/http/httptest`)
   - Given: handler wired with embedded `openapi.yaml`
   - When: `GET /api/v1/openapi.yaml` is requested without Authorization header
   - Then: status 200, `Content-Type: application/yaml`, body contains `"Nebu Admin API"`

2. **StrictServerInterface compile check**
   - Given: `make gen-api` runs with the full openapi.yaml spec
   - When: `go build ./...` is executed inside the Go container
   - Then: build exits 0 — this proves `StrictServerInterface` is satisfied (the AdminServer stub implements all operations)

## Tasks / Subtasks

- [x] Upgrade `gateway/api/openapi.yaml` to OpenAPI 3.1 with full Admin API placeholder paths (AC: #1)
  - [x] Change `openapi: "3.0.3"` to `openapi: "3.1.0"`
  - [x] Update `info.version` to `"1.0.0"`, keep `info.title: "Nebu Admin API"`
  - [x] Add `servers: [{url: "/api/v1"}]`
  - [x] Add `BearerAuth` security scheme under `components.securitySchemes`
  - [x] Add placeholder GET/POST operations for each route group (see Dev Notes for full spec)
  - [x] Keep the existing `GET /api/v1/health` operation (do not remove)

- [x] Create `gateway/api/oapi-codegen.yaml` config file (AC: #3)
  - [x] Set `package: api`
  - [x] Set `output: gateway/internal/api/api_gen.go` (workspace-relative path for Docker container)
  - [x] Set `generate: [strict-server, std-http-server, models, embedded-spec]` (oapi-codegen v2 YAML format)
  - [x] Set `strict-server: true` (within generate block — generates StrictServerInterface)

- [x] Update `Makefile` `gen-api` target to use config file and new output path (AC: #2)
  - [x] Replace current inline flags with `--config gateway/api/oapi-codegen.yaml` invocation
  - [x] Output to `gateway/internal/api/api_gen.go`

- [x] Add `gen-api` as prerequisite to `build-gateway` in Makefile (AC: #5)

- [x] Create new package `gateway/internal/api/` (AC: #2, #6)
  - [x] Run `make gen-api` to generate `api_gen.go` in new package
  - [x] Create `gateway/internal/api/server.go` — `AdminServer` stub implementing `StrictServerInterface`
  - [x] Every operation method returns `501 Not Implemented` by default (AC: #6)

- [x] Create `gateway/internal/api/openapi_handler.go` — `GET /api/v1/openapi.yaml` handler (AC: #4)
  - [x] Use Option C: `gateway/api/spec.go` (package apispec) with `//go:embed openapi.yaml var Spec []byte`
  - [x] Handler serves content with `Content-Type: application/yaml`
  - [x] No authentication required

- [x] Register `GET /api/v1/openapi.yaml` route in `gateway/cmd/gateway/main.go` (AC: #4)

- [x] Delete old `gateway/internal/admin/api_gen.go` — it is superseded by the new `gateway/internal/api/api_gen.go` (regression prevention)

- [x] Write unit test `gateway/internal/api/openapi_handler_test.go` (AC: #7)
  - [x] Test: GET /api/v1/openapi.yaml → 200, application/yaml, body contains "Nebu Admin API"

## Dev Notes

### Context: What Exists vs What This Story Changes

| Item | Current State | This Story's Action |
|---|---|---|
| `gateway/api/openapi.yaml` | EXISTS — OpenAPI 3.0.3, only `GET /api/v1/health` | **REPLACE** — upgrade to 3.1, add full Admin API paths |
| `gateway/api/oapi-codegen.yaml` | MISSING | **CREATE** |
| `gateway/internal/admin/api_gen.go` | EXISTS — package `admin`, `ServerInterface` (non-strict) | **DELETE** — superseded by new package |
| `gateway/internal/api/` | MISSING | **CREATE** — new package for Admin API codegen |
| `Makefile` `gen-api` target | EXISTS — `types,std-http-server`, package `admin`, output to `internal/admin/api_gen.go` | **REPLACE** — use config file, strict-server, output to `internal/api/api_gen.go` |
| `Makefile` `build-gateway` target | `build-admin-css download-vendor` prerequisites | **ADD** `gen-api` prerequisite |
| `GET /api/v1/openapi.yaml` endpoint | MISSING | **CREATE** |

**CRITICAL: The existing `gateway/internal/admin/api_gen.go` is not referenced by any Go code outside itself (verified: no import of `ServerInterface`, `HealthResponse`, or `HandlerWithOptions` in main.go or any other package).** Deleting it is safe. Do not keep both files.

### oapi-codegen v2 Config File Format

Create `gateway/api/oapi-codegen.yaml`:

```yaml
package: api
output: ../internal/api/api_gen.go
generate:
  - strict-server
  - types
  - spec
output-options:
  strict-server: true
```

**Why a config file instead of CLI flags?** The AC explicitly requires `gateway/api/oapi-codegen.yaml`. The config-file approach is also the canonical oapi-codegen v2 usage pattern and avoids long CLI flag strings in the Makefile.

### Updated Makefile `gen-api` Target

```makefile
## gen-api: Generate Go server stubs from openapi.yaml (oapi-codegen, strict-server)
gen-api:
	$(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
		--config gateway/api/oapi-codegen.yaml \
		gateway/api/openapi.yaml"
```

### Updated Makefile `build-gateway` Target

```makefile
## build-gateway: Build the Go Gateway Docker image (multi-stage)
build-gateway: gen-api build-admin-css download-vendor
	docker build -t nebu-gateway:dev ./gateway
```

**Note:** `gen-api` is added as the first prerequisite. Order matters — generated code must exist before `docker build` copies `gateway/` into the image.

### OpenAPI 3.1 Spec — Full Content for `gateway/api/openapi.yaml`

```yaml
openapi: "3.1.0"
info:
  title: "Nebu Admin API"
  version: "1.0.0"
servers:
  - url: "/api/v1"
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
security:
  - BearerAuth: []
paths:
  /health:
    get:
      operationId: GetHealth
      summary: Health check (unauthenticated)
      security: []
      responses:
        "200":
          description: Health status
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"
  /admin/users:
    get:
      operationId: ListAdminUsers
      summary: List users (placeholder — Story 6.4)
      responses:
        "200":
          description: User list
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
  /admin/rooms:
    get:
      operationId: ListAdminRooms
      summary: List rooms (placeholder — Story 6.7)
      responses:
        "200":
          description: Room list
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
  /admin/config:
    get:
      operationId: GetAdminConfig
      summary: Get server config (placeholder — Story 6.10)
      responses:
        "200":
          description: Config
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
  /admin/metrics:
    get:
      operationId: GetAdminMetrics
      summary: Get metrics (placeholder — Story 6.10)
      responses:
        "200":
          description: Metrics
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
  /compliance/access-requests:
    get:
      operationId: ListComplianceAccessRequests
      summary: List compliance access requests (placeholder — Story 6.x)
      responses:
        "200":
          description: Access requests
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/EmptyResponse"
        "501":
          description: Not implemented
components:
  schemas:
    HealthResponse:
      type: object
      required:
        - status
      properties:
        status:
          type: string
    EmptyResponse:
      type: object
      description: Placeholder response; replaced in Epic 6 sub-stories
```

**Path prefix clarification:** `servers: [{url: "/api/v1"}]` means paths in the spec are relative to `/api/v1`. So `/admin/users` in the spec resolves to `/api/v1/admin/users` in the actual HTTP router. The old `GET /api/v1/health` from Story 1.17 becomes `GET /health` in the spec (since the base URL already adds `/api/v1`).

### StrictServerInterface: AdminServer Stub

After running `make gen-api`, the generated code will contain `StrictServerInterface` with one method per `operationId`. Create `gateway/internal/api/server.go`:

```go
package api

import (
    "net/http"
)

// AdminServer implements StrictServerInterface.
// All operations return 501 Not Implemented until Epic 6 sub-stories wire real handlers.
type AdminServer struct{}

// Implement each generated method with 501 Not Implemented.
// The exact method signatures are determined by oapi-codegen output.
// Run `make gen-api` first, then implement each method.
// Example pattern:
//
//   func (s *AdminServer) ListAdminUsers(w http.ResponseWriter, r *http.Request, params ListAdminUsersParams) {
//       http.Error(w, "not implemented", http.StatusNotImplemented)
//   }
```

**IMPORTANT:** With `strict-server: true`, oapi-codegen generates a `StrictServerInterface` (not `ServerInterface`). The strict variant wraps each operation in a typed request/response struct. Each method receives typed params and returns typed responses instead of raw `http.ResponseWriter`. The exact signature depends on the spec — run `make gen-api` first, then implement what the compiler requires.

For operations that take no query params and return a simple response, the pattern is:

```go
func (s *AdminServer) ListAdminUsers(ctx context.Context, request ListAdminUsersRequestObject) (ListAdminUsersResponseObject, error) {
    return ListAdminUsers501Response{}, nil
}
```

The build will fail to compile until ALL methods in `StrictServerInterface` are implemented. That is the point — the compiler enforces completeness.

### openapi.yaml Live Endpoint: go:embed Pattern

**IMPORTANT:** The `go:embed` directive must point to the `openapi.yaml` file **relative to the Go source file containing the directive**. Since the handler will live in `gateway/internal/api/openapi_handler.go`, the embed path must cross directory boundaries.

Option A — embed from the handler file using a relative path (preferred if Go supports it):
```go
// In gateway/internal/api/openapi_handler.go
//go:embed ../../../api/openapi.yaml  // relative from internal/api/ up to gateway root, then into api/
var openapiSpec []byte
```

Wait — `go:embed` does NOT support `..` (parent directory references). This means the handler cannot embed from `gateway/api/openapi.yaml` directly.

**Option B — copy openapi.yaml into the api package at gen-api time:**
Update the `gen-api` Makefile target to also copy the spec into the package:

```makefile
gen-api:
	$(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
		--config gateway/api/oapi-codegen.yaml \
		gateway/api/openapi.yaml && \
		cp gateway/api/openapi.yaml gateway/internal/api/openapi.yaml"
```

Then in `gateway/internal/api/openapi_handler.go`:
```go
//go:embed openapi.yaml
var openapiSpec []byte
```

**Option C — embed openapi.yaml in the `gateway/api/` package (define a separate small package there):**
Create `gateway/api/spec.go` with `//go:embed openapi.yaml` and expose `var Spec []byte`. Then import it from the handler. This keeps the embed co-located with the spec file.

**Recommended: Option C** — cleanest separation. No file copying, no Makefile complexity.

```go
// gateway/api/spec.go
package apispec

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
```

Then the handler:
```go
// gateway/internal/api/openapi_handler.go
package api

import (
    apispec "github.com/nebu/nebu/api"
    "net/http"
)

func OpenAPIYAMLHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/yaml")
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write(apispec.Spec)
}
```

**Check module path:** The `gateway/go.mod` module is `github.com/nebu/nebu`. Verify the exact module name before coding the import. The `gateway/api/` package import path is `github.com/nebu/nebu/api`.

### Route Registration in main.go

Add to `gateway/cmd/gateway/main.go` in the route group after the compliance routes (no authentication required per FR51):

```go
// Story 6.1 — Admin API OpenAPI spec endpoint (unauthenticated, FR51)
import apihandler "github.com/nebu/nebu/internal/api"
// ...
mux.HandleFunc("GET /api/v1/openapi.yaml", apihandler.OpenAPIYAMLHandler)
```

**Do NOT put this under JWT middleware.** FR51 explicitly requires no authentication for the spec endpoint.

### Regression Risk: Removing api_gen.go from admin Package

The old `gateway/internal/admin/api_gen.go` defines `ServerInterface`, `MiddlewareFunc`, `HandlerWithOptions`, etc. **None of these are referenced outside the file itself** (verified by grepping all Go files). Deletion is safe.

However, verify with `go build ./...` after deletion — the compiler will immediately catch any broken reference. The build must pass.

### Unit Test Pattern

```go
// gateway/internal/api/openapi_handler_test.go
package api_test

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestOpenAPIYAMLHandler(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
    w := httptest.NewRecorder()
    OpenAPIYAMLHandler(w, req)
    resp := w.Result()
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
    ct := resp.Header.Get("Content-Type")
    if !strings.Contains(ct, "application/yaml") {
        t.Errorf("expected Content-Type application/yaml, got %s", ct)
    }
    body := w.Body.String()
    if !strings.Contains(body, "Nebu Admin API") {
        t.Errorf("expected body to contain 'Nebu Admin API', got: %s", body)
    }
}
```

### oapi-codegen v2 Strict-Server Mode: Key Differences from Story 1.17

Story 1.17 used `-generate types,std-http-server` which produces the classic `ServerInterface` (raw `http.ResponseWriter` + `*http.Request` args per handler). Story 6.1 uses `strict-server` which produces:

- `StrictServerInterface` — methods take typed request objects and return typed response objects
- Each response type is a struct that implements a `VisitXxxResponse(http.ResponseWriter)` method
- The `StrictHandler` wrapper handles encoding/decoding between HTTP and the typed interface
- **The key benefit:** if a new operation is added to the spec, the Go compiler refuses to build until the handler implements the new method — zero silent omissions

The generated `RegisterHandlersWithBaseURL(router, StrictHandler(adminServer, middlewares), "/api/v1")` call is what wires everything. Do NOT use `HandlerWithOptions` from Story 1.17 — the strict-server variant uses different wiring.

### Existing Route Conflicts

The current `main.go` has **no** routes under `/api/v1/admin/*` served via the oapi-codegen `Handler`. The compliance and admin endpoints at `/api/v1/compliance/*` and `/api/v1/admin/users/{userId}/keys` etc. are registered directly on the mux via `mux.Handle(...)`. Story 6.1 does NOT need to migrate those — this story only establishes the codegen infrastructure. Subsequent stories (6.3+) will wire real handlers through the `StrictServerInterface`.

**Do NOT register `RegisterHandlersWithBaseURL` in main.go in this story** — that comes in Story 6.3 (Admin API Router). For now, only register the `GET /api/v1/openapi.yaml` endpoint manually.

### Module Name Check

Verify in `gateway/go.mod`:
```
module github.com/nebu/nebu
```
All import paths use this prefix. The new `gateway/api/spec.go` would be at `github.com/nebu/nebu/api`.

### Project Structure Notes

**Files to CREATE:**
- `gateway/api/oapi-codegen.yaml` — oapi-codegen v2 config
- `gateway/api/spec.go` — `package apispec` with `//go:embed openapi.yaml var Spec []byte`
- `gateway/internal/api/` — new directory (created by `make gen-api`)
- `gateway/internal/api/api_gen.go` — generated by `make gen-api` (commit this)
- `gateway/internal/api/server.go` — `AdminServer` stub implementing `StrictServerInterface`
- `gateway/internal/api/openapi_handler.go` — `OpenAPIYAMLHandler`
- `gateway/internal/api/openapi_handler_test.go` — unit test

**Files to MODIFY:**
- `gateway/api/openapi.yaml` — upgrade to OpenAPI 3.1, add all Admin API paths
- `Makefile` — update `gen-api` target, add `gen-api` to `build-gateway` prerequisites

**Files to DELETE:**
- `gateway/internal/admin/api_gen.go` — superseded; zero references outside the file

**Files NOT to touch:**
- `gateway/internal/admin/` (everything else) — handler.go, auth.go, bootstrap.go, etc. remain as-is
- `gateway/cmd/gateway/main.go` — only add the single `GET /api/v1/openapi.yaml` route registration; do not reorganize existing routes

### Security Review Flag

`security_review: required` — this story creates a new public endpoint (`GET /api/v1/openapi.yaml`) and a new codegen pipeline affecting the API surface. The SEC Gate per-story review must verify that the openapi.yaml endpoint does not leak sensitive configuration and that the strict-server stub does not accidentally expose unfinished routes.

### References

- [Source: epics.md#Story-6.1] Full Acceptance Criteria definition
- [Source: epics.md#Epic-6] Epic objectives and FR coverage (FR22, FR23, FR24, FR36, FR37, FR38, FR39, FR40, FR51, FR52)
- [Source: architecture.md#API-Spec-First-Workflow] `gen-api` workflow, Makefile pattern, `gateway/api/openapi.yaml` as source of truth
- [Source: architecture.md#Build-Container-Strategie] `DOCKER_GO` = `docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine`
- [Source: architecture.md#Requirements-Mapping] FR48–52 → `gateway/api/openapi.json` (note: story uses .yaml; architecture references .json — .yaml is canonical per AC)
- [Source: architecture.md#Enforced-Implementation-Rules rule 11] "Admin API implementation MUST fulfill `ServerInterface` from `api_gen.go`"
- [Source: 1-17-make-gen-api-oapi-codegen-build-target.md] Previous gen-api implementation, current state of openapi.yaml and api_gen.go
- [Source: Makefile] Current `gen-api` target: `types,std-http-server`, package `admin`, output `internal/admin/api_gen.go`
- [Source: gateway/api/openapi.yaml] Current spec: OpenAPI 3.0.3, single `GET /api/v1/health` path
- [Source: gateway/internal/admin/api_gen.go] Existing generated file: package `admin`, `ServerInterface` (non-strict) — to be deleted
- [Source: gateway/internal/admin/handler.go] Existing `//go:embed templates` pattern — reference for go:embed usage
- [Source: gateway/go.mod] Module path: `github.com/nebu/nebu`

### ATDD Artifacts

- Checklist: `_bmad-output/test-artifacts/atdd-checklist-6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint.md`
- Unit tests: `gateway/internal/api/openapi_handler_test.go`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- oapi-codegen v2.6.0 YAML config format differs from the story's Dev Notes: the generate block uses `strict-server: true` as a key (not as list item), and `output-options.strict-server: true` is not valid. Discovered correct format via `oapi-codegen -output-config`.
- oapi-codegen v2.6.0 does not generate `StrictServerInterface` for OpenAPI 3.1 specs when using `strict-server: true` alone — `std-http-server: true` must also be enabled. Both together produce `ServerInterface` + `StrictServerInterface` + `NewStrictHandler`.
- `output` path in oapi-codegen.yaml must be workspace-relative (from Docker -w /workspace) not spec-relative.
- `github.com/oapi-codegen/runtime` and `github.com/getkin/kin-openapi` were missing from go.mod; added via `go get` inside Docker Go container.

### Completion Notes List

- Upgraded `gateway/api/openapi.yaml` from 3.0.3 to 3.1.0 with BearerAuth security scheme, `servers: [{url: "/api/v1"}]`, and all five Admin API placeholder paths plus `/health`.
- Created `gateway/api/oapi-codegen.yaml` with the correct oapi-codegen v2 YAML format (map-style generate block, not sequence).
- `make gen-api` now uses `--config gateway/api/oapi-codegen.yaml` and outputs to `gateway/internal/api/api_gen.go`.
- `build-gateway` depends on `gen-api` as the first prerequisite.
- Created `gateway/api/spec.go` (package `apispec`) with `//go:embed openapi.yaml` — Option C from Dev Notes; cleanest separation, no Makefile file-copy step.
- Created `gateway/internal/api/api_gen.go` via `make gen-api` — contains `StrictServerInterface` with 6 typed operations.
- Created `gateway/internal/api/server.go` — `AdminServer` struct implementing all `StrictServerInterface` methods returning 501 (except `GetHealth` which returns 200 ok). Compile-time guard via `var _ StrictServerInterface = (*AdminServer)(nil)`.
- Created `gateway/internal/api/openapi_handler.go` — `OpenAPIYAMLHandler` serving embedded spec bytes.
- Registered `GET /api/v1/openapi.yaml` in `main.go` (unauthenticated, before security header middleware).
- Deleted `gateway/internal/admin/api_gen.go` (zero references confirmed; build passes after deletion).
- Added `github.com/oapi-codegen/runtime v1.4.0` and `github.com/getkin/kin-openapi v0.137.0` to `gateway/go.mod`.
- All 5 ATDD unit tests + full regression suite (18 packages) pass green.

### File List

- gateway/api/openapi.yaml (modified — upgraded to 3.1.0)
- gateway/api/oapi-codegen.yaml (created)
- gateway/api/spec.go (created)
- gateway/internal/api/api_gen.go (created — generated by make gen-api)
- gateway/internal/api/server.go (created)
- gateway/internal/api/openapi_handler.go (created)
- gateway/internal/api/openapi_handler_test.go (modified — removed t.Skip(), green phase)
- gateway/internal/admin/api_gen.go (deleted)
- gateway/cmd/gateway/main.go (modified — import + route registration)
- Makefile (modified — gen-api target + build-gateway prerequisite)
- gateway/go.mod (modified — oapi-codegen/runtime + getkin/kin-openapi added)
- gateway/go.sum (modified — new dependency checksums)

## Change Log

- 2026-05-01: Story implemented by claude-sonnet-4-6. Established OpenAPI 3.1 spec-first pipeline with StrictServerInterface codegen, live unauthenticated spec endpoint, and removal of legacy admin/api_gen.go. All 5 ATDD acceptance tests pass, full regression suite green (18 packages).

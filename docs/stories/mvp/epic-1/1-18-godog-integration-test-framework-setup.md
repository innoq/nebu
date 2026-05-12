# Story 1.18: Godog Integration Test Framework Setup

Status: done

## Story

As a developer,
I want the Godog BDD framework configured and runnable against the Docker Compose stack,
so that Gherkin scenarios can be executed in CI without manual stack management.

## Acceptance Criteria

1. **Given** `github.com/cucumber/godog` added to `gateway/go.mod`, **When** `go build ./...` runs, **Then** it compiles successfully.

2. **Given** `gateway/features/` directory exists, **When** it contains a placeholder `health.feature` file, **Then** it is valid Gherkin syntax (parseable by godog).

3. **Given** `gateway/test/integration/main_test.go` with Godog `TestSuite` configuration, **When** `go test ./test/integration/... -v` runs (without a live stack), **Then** it compiles and reports "no scenarios" or skipped/pending scenarios without panicking.

4. **Given** `make test-integration` target in Makefile, **When** executed, **Then** it: (1) runs `docker compose up -d --wait` (waits for all service healthchecks to pass), (2) runs Godog test suite, (3) runs `docker compose down` regardless of test result.

5. **Given** `NEBU_TEST_GATEWAY_URL` env var, **When** set to a custom URL, **Then** Godog uses it as the base URL for all HTTP calls (default: `http://localhost:8080`).

## Tasks / Subtasks

- [x] Add `github.com/cucumber/godog` to `gateway/go.mod` (AC: #1)
  - [x] Run `go get github.com/cucumber/godog@latest` inside DOCKER_GO container from `/workspace/gateway`
  - [x] Run `go mod tidy` to clean up and update `go.sum`
  - [x] Verify `go build ./...` compiles clean

- [x] Create `gateway/features/health.feature` placeholder (AC: #2)
  - [x] Valid Gherkin with the exact steps from Story 1.19 spec, tagged `@pending`
  - [x] Verify godog can parse it without error

- [x] Create `gateway/test/integration/main_test.go` (AC: #3, #5)
  - [x] `TestMain`: reads `NEBU_TEST_GATEWAY_URL` env var (default: `http://localhost:8080`), stores in package-level `gatewayURL`
  - [x] `TestIntegrationSuite`: `godog.TestSuite` with `Strict: false`, `Paths: []string{"../../features"}`
  - [x] Empty `InitializeScenario` func (step definitions added in Story 1.19)
  - [x] Verify `go test ./test/integration/... -v` compiles and reports pending/skipped without panic

- [x] Update `Makefile` `test-integration` target (AC: #4)
  - [x] Replace `docker compose -f docker-compose.test.yml up --abort-on-container-exit` placeholder
  - [x] Use `docker compose up -d --wait` for health-aware stack startup
  - [x] Use inline `docker run` with `--add-host=host.docker.internal:host-gateway` so the test container can reach the compose-published ports on the host
  - [x] Guarantee `docker compose down` runs even on test failure via `EXIT=$$?; docker compose down; exit $$EXIT` pattern

## Dev Notes

### Current State — What Exists vs What's Missing

| Item | State | Action |
|---|---|---|
| `Makefile` `test-integration` | EXISTS — placeholder: `docker compose -f docker-compose.test.yml up --abort-on-container-exit` | **REPLACE** with working implementation |
| `docker-compose.test.yml` | MISSING — referenced by placeholder but not created | **NOT NEEDED** — drop this approach; use `docker compose up -d --wait` instead |
| `gateway/features/` | MISSING | **CREATE** directory + `health.feature` |
| `gateway/test/integration/` | MISSING | **CREATE** directory + `main_test.go` |
| `github.com/cucumber/godog` | NOT IN `gateway/go.mod` | **ADD** via `go get` |

### Adding Godog to `gateway/go.mod`

Run inside the DOCKER_GO container:
```bash
docker run --rm -v $(PWD):/workspace -w /workspace/gateway golang:1.26-alpine \
  sh -c "go get github.com/cucumber/godog@latest && go mod tidy"
```

This updates `gateway/go.mod` and `gateway/go.sum`. Commit both files.

**Why from `/workspace/gateway`?** `gateway/go.mod` is the Go module root. Running from the project root (as DOCKER_GO defaults) would target the wrong directory. The `-w /workspace/gateway` override corrects this.

**Expected addition to `gateway/go.mod`:** `github.com/cucumber/godog v0.14.x` plus its indirect deps (primarily `github.com/cucumber/messages`, `github.com/cucumber/gherkin-go`, `github.com/spf13/pflag`).

### `gateway/features/health.feature` — Placeholder Gherkin

Create `gateway/features/health.feature`:

```gherkin
Feature: Stack Health Smoke Test
  As an operator
  I want to verify the full stack starts and is healthy
  So that CI has a definitive green signal the deployment works

  @pending
  Scenario: Full stack health check passes
    Given the docker compose stack is started
    When I call GET /health on the gateway
    Then the response status is 200
    And the response body contains "UP"
    When I call GET /ready on the gateway
    Then the response status is 200
    And the response body contains "READY"
    When I call GET :4000/health on the core
    Then the response status is 200
    And the response body contains "UP"
```

**Why `@pending`?** Story 1.18 sets up the framework only — step definitions come in Story 1.19. Without step definitions, godog reports all steps as undefined. With `Strict: false`, undefined steps do not fail the suite (exit 0). The `@pending` tag is a conventional marker for Story 1.19 to filter on — it has no built-in meaning in godog. This satisfies AC3 ("skipped scenarios without panicking").

**Why these exact steps?** They match the Story 1.19 AC verbatim. Writing them now ensures story 1.18 creates a valid, parseable file and story 1.19 can implement the steps without modifying the feature file.

### `gateway/test/integration/main_test.go`

Create `gateway/test/integration/main_test.go`:

```go
package integration_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

// gatewayURL is the base URL for all HTTP calls to the gateway.
// Override via NEBU_TEST_GATEWAY_URL env var (default: http://localhost:8080).
var gatewayURL string

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("NEBU_TEST_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	os.Exit(m.Run())
}

// TestIntegrationSuite runs all Gherkin scenarios from gateway/features/.
func TestIntegrationSuite(t *testing.T) {
	suite := godog.TestSuite{
		Name: "integration",
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features"},
			TestingT: t,
			Strict:   false, // pending/undefined steps do not fail the suite
			NoColors: true,  // cleaner CI output
		},
		ScenarioInitializer: InitializeScenario,
	}
	if suite.Run() != 0 {
		t.Fatal("integration test suite failed")
	}
}

// InitializeScenario registers step definitions.
// HTTP step implementations are added in Story 1.19.
func InitializeScenario(sc *godog.ScenarioContext) {
	// step definitions added in story 1.19
}
```

**`../../features` path explained:** Tests run from `gateway/test/integration/`. The `../../features` path resolves to `gateway/features/`. Go test binaries have their working directory set to the package directory.

**`Strict: false`:** With no step definitions yet, godog would otherwise fail on undefined steps. `Strict: false` treats them as pending and returns 0.

**`TestingT: t`:** Required in godog v0.14 when calling `suite.Run()` inside a `*testing.T` function. Without it, godog panics.

**`gatewayURL` package-level var:** Accessible from step definitions added in Story 1.19 (same package `integration_test`).

### Updated `Makefile` `test-integration` Target

**Current placeholder (lines ~41-43):**
```makefile
## test-integration: Run full stack integration tests (Godog / Gherkin)
test-integration:
	docker compose -f docker-compose.test.yml up --abort-on-container-exit
```

**Replace with:**
```makefile
## test-integration: Run full stack integration tests (Godog / Gherkin)
test-integration:
	docker compose up -d --wait
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--add-host=host.docker.internal:host-gateway \
		-e NEBU_TEST_GATEWAY_URL=http://host.docker.internal:8080 \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT
```

**Why `docker compose up -d --wait`?** The `--wait` flag (Docker Compose v2.1+) waits until all services with healthchecks report healthy before returning. The existing `docker-compose.yml` defines healthchecks for `postgres`, `gateway`, and `core`. This is equivalent to polling `/health` until UP.

**Why NOT `$(DOCKER_GO)` here?** `DOCKER_GO` is defined as `docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine`. For integration tests, the test container must reach the services published on the host. We need `--add-host=host.docker.internal:host-gateway` and `-e NEBU_TEST_GATEWAY_URL=...` which require the full `docker run` command inline.

**Why `--add-host=host.docker.internal:host-gateway`?**
- On **Linux** (CI): `host-gateway` is a Docker special value resolving to the host network gateway IP. This makes `host.docker.internal` reachable from inside the container and maps to `localhost` on the host.
- On **macOS** (Docker Desktop): Docker Desktop automatically injects `host.docker.internal` → host IP. The `--add-host` flag is harmless/redundant but doesn't break anything.
- This is the standard cross-platform approach for accessing host-published ports from a Docker container.

**Why `EXIT=$$?; docker compose down; exit $$EXIT`?**
The semicolon (`;`) instead of `&&` ensures `docker compose down` always runs, even if `go test` exits non-zero. `$$?` captures the test exit code, which is re-used after cleanup to correctly propagate failure to `make`.

**Also update `.PHONY`:** Ensure `test-integration` remains in the `.PHONY` list (already there).

### Architecture Discrepancy: Feature File Location

**Epic AC says:** `gateway/features/` and `gateway/test/integration/main_test.go` (within gateway module)
**Architecture doc says:** `test/features/` and `test/integration/` at project root (own `go.mod`)

**This story follows the Epic's ACs** (`gateway/features/`, `gateway/test/integration/`), because:
1. The Epic's ACs are more specific and represent the implementation contract
2. Keeping tests in `gateway/` means no second `go.mod` to maintain
3. The `gateway/go.mod` already has all dependencies needed
4. Story 1.19 (and 2.21, 3.15, etc.) reference `gateway/features/*.feature` consistently in their ACs

**Do NOT create `test/` at the project root for this story.**

### Build Verification After `go get`

After adding godog to `go.mod`, verify:
```bash
docker run --rm -v $(PWD):/workspace -w /workspace/gateway golang:1.26-alpine \
  sh -c "go build ./..."
```

Must exit 0. The `main_test.go` is in a `_test` package (`integration_test`) so it's excluded from `go build ./...` — only verified by `go test ./...`.

To verify the test compiles without a live stack:
```bash
docker run --rm -v $(PWD):/workspace -w /workspace/gateway golang:1.26-alpine \
  sh -c "apk add -q --no-cache gcc musl-dev && go test -v -run TestIntegrationSuite ./test/integration/..."
```

Expected output: scenarios reported as pending, exit code 0.

### No Regression Risk

- `gateway/internal/admin/api_gen.go` and `metrics.go` are in `package admin` — no interference
- Adding godog to `go.mod` only adds test-time dependencies; `go build` of production binaries is unaffected
- Existing `make test-unit-go` continues to work unchanged (tests in `gateway/...` excluding `_test` files with build tags)
- The `go test -race ./...` in `test-unit-go` will NOT pick up `./test/integration/...` files automatically — integration tests have a separate `go test ./test/integration/...` invocation

### Project Structure Notes

**Files to create/modify:**

| File | Action |
|---|---|
| `gateway/go.mod` | UPDATE — add `github.com/cucumber/godog` |
| `gateway/go.sum` | UPDATE — updated by `go mod tidy` |
| `gateway/features/health.feature` | CREATE — valid Gherkin placeholder with `@pending` |
| `gateway/test/integration/main_test.go` | CREATE — Godog TestSuite config |
| `Makefile` | UPDATE — replace `test-integration` target |

**Nothing else to touch.** Do NOT create `docker-compose.test.yml` — this story drops that approach.

### References

- [Source: epics.md#Story-1.18] Full AC and user story definition (authoritative for file locations)
- [Source: epics.md#Story-1.19] Step names used in `health.feature` placeholder
- [Source: architecture.md#CI/CD-Integration-Test-Strategie] G6: Hybrid unit+integration, Gherkin as primary quality gate
- [Source: architecture.md#Build-Container-Strategie] `DOCKER_GO` definition and `test-integration` pattern
- [Source: architecture.md#Testing-Patterns] Gherkin feature file format and fixture locations
- [Source: architecture.md#Health-Readiness-Endpoints] Response format: `{"status": "UP"}` for gateway, `{"status": "UP"}` for core
- [Source: docker-compose.yml:healthcheck] Gateway healthcheck uses `/healthcheck` binary; Core healthcheck uses `curl http://localhost:4000/health`
- [Source: Makefile:test-integration] Current placeholder: `docker compose -f docker-compose.test.yml up --abort-on-container-exit` — REPLACE
- [Source: gateway/go.mod:1] Module name: `github.com/nebu/nebu`, Go version: `go 1.26`
- [Source: 1-17-make-gen-api-oapi-codegen-build-target.md#Dev-Notes] Previous story used `@latest` for tool invocation from project root — godog is added as a module dep, not a tool runner
- [Source: 1-15-go-unit-test-target.md] Pattern: `apk add -q --no-cache gcc musl-dev` needed for CGO in Go Alpine containers

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

First `go get` run did not persist to go.mod because the Docker volume was not being checked correctly after run. Re-ran with `"$(PWD)"` quoting — persisted correctly on second run. Both `go build ./...` and `go test -race ./...` pass after the fix.

### Completion Notes List

- Added `github.com/cucumber/godog v0.15.1` (plus indirect deps: gherkin/go/v26, messages/go/v21, gofrs/uuid, hashicorp/go-memdb, hashicorp/go-immutable-radix, hashicorp/golang-lru, spf13/pflag) to `gateway/go.mod` via `go get` + `go mod tidy` in DOCKER_GO container
- Created `gateway/features/health.feature` with `@pending`-tagged scenario using exact step names from Story 1.19 spec — godog parses and reports 1 undefined scenario, exit 0
- Created `gateway/test/integration/main_test.go` in package `integration_test` with `TestMain` (reads `NEBU_TEST_GATEWAY_URL`, defaults to `http://localhost:8080`), `TestIntegrationSuite` (`Strict: false`, `NoColors: true`, `Paths: []string{"../../features"}`), and empty `InitializeScenario`
- Updated `Makefile` `test-integration` target: drops `docker-compose.test.yml` approach, uses `docker compose up -d --wait` + inline `docker run` with `--add-host=host.docker.internal:host-gateway` + guaranteed `docker compose down` via `EXIT=$$?` pattern
- All existing unit tests pass with no regressions (`go test -race ./...`)

### File List

- `gateway/go.mod` — updated (added godog v0.15.1 + indirect deps)
- `gateway/go.sum` — updated by go mod tidy
- `gateway/features/health.feature` — created
- `gateway/test/integration/main_test.go` — created
- `Makefile` — updated (test-integration target)

## Change Log

- 2026-03-25: Added godog v0.15.1 to gateway/go.mod; created gateway/features/health.feature placeholder; created gateway/test/integration/main_test.go Godog TestSuite; replaced Makefile test-integration target with working docker compose + godog runner implementation (Story 1.18)
- 2026-03-25: Code review fixes — Makefile: joined `docker compose up` and test run into single shell command so `docker compose down` runs even if stack startup fails; Dev Notes: corrected inaccurate claim about `@pending` tag behavior in godog

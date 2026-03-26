# Story 1.19: First Gherkin Scenario — Stack Health Smoke Test

Status: done

## Story

As an operator,
I want a passing end-to-end Gherkin scenario that verifies the complete stack starts and is healthy,
So that CI has a definitive green signal that the entire deployment infrastructure works.

## Acceptance Criteria

1. **Given** `gateway/features/health.feature` contains the Stack Health scenario,
   **When** `make test-integration` runs against a started stack,
   **Then** the scenario passes with exit code 0.

2. **Given** the feature file,
   **When** read,
   **Then** it contains the following steps (already written in Story 1.18 — do NOT modify the feature file content):
   - `Given the docker compose stack is started`
   - `When I call GET /health on the gateway`
   - `Then the response status is 200`
   - `And the response body contains "UP"`
   - `When I call GET /ready on the gateway`
   - `Then the response status is 200`
   - `And the response body contains "READY"`
   - `When I call GET :4000/health on the core`
   - `Then the response status is 200`
   - `And the response body contains "UP"`

3. **Given** a cold stack start,
   **When** `make test-integration` runs from `docker compose up` to passing scenario,
   **Then** total elapsed time is ≤3 minutes.

4. **Given** a CI pipeline configuration file (`.gitlab-ci.yml`),
   **When** it exists in the repository,
   **Then** it defines a `unit-tests` job running `make test-unit-go` and `make test-unit-elixir`, and an `integration-test` job running `make test-integration` — both triggered on push to `main` and on merge requests.

## Tasks / Subtasks

- [x] Remove `@pending` tag from `gateway/features/health.feature` (AC: #1, #2)
  - [x] Delete the `@pending` line (line 6) from `health.feature`
  - [x] Keep all scenario content unchanged — the steps were written verbatim for this story in 1.18

- [x] Add `coreURL` to `gateway/test/integration/main_test.go` and enable strict mode (AC: #1)
  - [x] Add `var coreURL string` package-level var
  - [x] Initialize `coreURL` from `NEBU_TEST_CORE_URL` env var (default: `http://localhost:4000`) in `TestMain`
  - [x] Change `Strict: false` to `Strict: true` — all steps will be defined, undefined steps must fail

- [x] Create `gateway/test/integration/steps_test.go` with all step definitions (AC: #1, #2)
  - [x] Package `integration_test` (same package as `main_test.go`)
  - [x] Package-level response state vars: `lastStatusCode int`, `lastBody string`
  - [x] Implement `theDockerComposeStackIsStarted` — no-op, stack already started by `make test-integration`
  - [x] Implement `iCallGETOnGateway(path string)` — GET `gatewayURL + path`, store status + body
  - [x] Implement `iCallGETOnCore(path string)` — GET `coreURL + path`, store status + body
  - [x] Implement `theResponseStatusIs(expected int)` — assert `lastStatusCode == expected`
  - [x] Implement `theResponseBodyContains(expected string)` — assert `strings.Contains(lastBody, expected)`
  - [x] Move `InitializeScenario` from `main_test.go` to `steps_test.go` and register all 5 step patterns
  - [x] Remove the empty `InitializeScenario` stub from `main_test.go`

- [x] Update `Makefile` `test-integration` target to add `NEBU_TEST_CORE_URL` (AC: #1)
  - [x] Add `-e NEBU_TEST_CORE_URL=http://host.docker.internal:4000` to the inline `docker run` command

- [x] Create `.gitlab-ci.yml` in project root (AC: #4)
  - [x] Two stages: `unit` (unit-tests job) + `integration` (integration-test job)
  - [x] `unit-tests` job: runs `make test-unit-go` AND `make test-unit-elixir` sequentially
  - [x] `integration-test` job: runs `make test-integration`
  - [x] Both jobs triggered on push to `main` and on merge request events

- [x] Verify end-to-end: `make test-integration` passes with exit 0 (AC: #1, #3)
  - [x] Run `docker compose up -d --wait` and confirm all services healthy
  - [x] Confirm godog reports 1 scenario, 9 steps, all passed
  - [x] Confirm `docker compose down` completes and `make` exits 0

## Dev Notes

### Current State — What Exists from Story 1.18

| File | Current State | Action in Story 1.19 |
|---|---|---|
| `gateway/features/health.feature` | EXISTS — correct steps, tagged `@pending` | **REMOVE `@pending` tag only** — content unchanged |
| `gateway/test/integration/main_test.go` | EXISTS — `Strict: false`, `gatewayURL`, empty `InitializeScenario` | **UPDATE** — add `coreURL`, set `Strict: true`, remove `InitializeScenario` stub |
| `gateway/test/integration/steps_test.go` | MISSING | **CREATE** — all step definitions + `InitializeScenario` |
| `Makefile` `test-integration` | EXISTS — passes `NEBU_TEST_GATEWAY_URL` only | **UPDATE** — add `NEBU_TEST_CORE_URL` |
| `.gitlab-ci.yml` | MISSING | **CREATE** |

**Do NOT touch:** `gateway/go.mod`, `gateway/go.sum`, `docker-compose.yml` — no changes needed.

### Exact Content: `gateway/features/health.feature` After Edit

Remove the `@pending` line only. The feature file becomes:

```gherkin
Feature: Stack Health Smoke Test
  As an operator
  I want to verify the full stack starts and is healthy
  So that CI has a definitive green signal the deployment works

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

### Exact Content: `gateway/test/integration/main_test.go` After Update

```go
package integration_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

// gatewayURL is the base URL for HTTP calls to the gateway.
// Override via NEBU_TEST_GATEWAY_URL env var (default: http://localhost:8080).
var gatewayURL string

// coreURL is the base URL for HTTP calls to the Elixir core.
// Override via NEBU_TEST_CORE_URL env var (default: http://localhost:4000).
var coreURL string

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("NEBU_TEST_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	coreURL = os.Getenv("NEBU_TEST_CORE_URL")
	if coreURL == "" {
		coreURL = "http://localhost:4000"
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
			Strict:   true, // all steps must be defined — undefined steps fail the suite
			NoColors: true, // cleaner CI output
		},
		ScenarioInitializer: InitializeScenario,
	}
	if suite.Run() != 0 {
		t.Fatal("integration test suite failed")
	}
}
```

**Key change:** `Strict: true` — now that all steps are defined, undefined steps are a bug and must fail.

### Exact Content: `gateway/test/integration/steps_test.go` (CREATE)

```go
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// lastStatusCode and lastBody hold the most recent HTTP response.
// Scenarios run sequentially in godog — no concurrency concern.
var lastStatusCode int
var lastBody string

// theDockerComposeStackIsStarted is a no-op: make test-integration runs
// `docker compose up -d --wait` before `go test`, so the stack is always up.
func theDockerComposeStackIsStarted() error {
	return nil
}

// iCallGETOnGateway makes a GET request to gatewayURL+path and stores the response.
// Matches steps: "I call GET /health on the gateway", "I call GET /ready on the gateway"
func iCallGETOnGateway(path string) error {
	url := gatewayURL + path
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", url, err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// iCallGETOnCore makes a GET request to coreURL+path and stores the response.
// Matches step: "I call GET :4000/health on the core" (captures "/health" from ":4000/health")
func iCallGETOnCore(path string) error {
	url := coreURL + path
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", url, err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theResponseStatusIs asserts the last response had the expected HTTP status code.
func theResponseStatusIs(expected int) error {
	if lastStatusCode != expected {
		return fmt.Errorf("expected HTTP %d, got %d (body: %s)", expected, lastStatusCode, lastBody)
	}
	return nil
}

// theResponseBodyContains asserts the last response body contains the expected substring.
func theResponseBodyContains(expected string) error {
	if !strings.Contains(lastBody, expected) {
		return fmt.Errorf("expected body to contain %q, got: %s", expected, lastBody)
	}
	return nil
}

// InitializeScenario registers all step definitions for the integration test suite.
func InitializeScenario(sc *godog.ScenarioContext) {
	sc.Step(`^the docker compose stack is started$`, theDockerComposeStackIsStarted)
	sc.Step(`^I call GET (/\S+) on the gateway$`, iCallGETOnGateway)
	sc.Step(`^I call GET :4000(/\S+) on the core$`, iCallGETOnCore)
	sc.Step(`^the response status is (\d+)$`, theResponseStatusIs)
	sc.Step(`^the response body contains "([^"]*)"$`, theResponseBodyContains)
}
```

**Step regex notes:**
- `(/\S+)` captures `/health`, `/ready` etc. from steps like `I call GET /health on the gateway`
- `:4000(/\S+)` captures `/health` from the literal step `I call GET :4000/health on the core` — the `:4000` in the step text is a literal string matching godog's regex; the capture group gets only the path portion
- `(\d+)` is auto-converted to `int` by godog v0.15.x
- `([^"]*)` captures the quoted string from `the response body contains "UP"`

### Updated `Makefile` `test-integration` Target

Current (story 1.18 result):
```makefile
test-integration:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--add-host=host.docker.internal:host-gateway \
		-e NEBU_TEST_GATEWAY_URL=http://host.docker.internal:8080 \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT
```

Updated (add core URL env var):
```makefile
## test-integration: Run full stack integration tests (Godog / Gherkin)
test-integration:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--add-host=host.docker.internal:host-gateway \
		-e NEBU_TEST_GATEWAY_URL=http://host.docker.internal:8080 \
		-e NEBU_TEST_CORE_URL=http://host.docker.internal:4000 \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT
```

**Only change:** one `-e NEBU_TEST_CORE_URL=http://host.docker.internal:4000` line added to the existing docker run command.

**Why `host.docker.internal:4000`?** The Elixir Core publishes its health endpoint on port 4000 (Docker Compose port mapping). Inside the test container, `host.docker.internal` resolves to the host where Compose is running — same pattern as `NEBU_TEST_GATEWAY_URL` for port 8080.

### `.gitlab-ci.yml` (CREATE in project root)

```yaml
# Nebu CI Pipeline
# All builds run inside Docker containers — no local Go or Elixir installation required.
# Runners must have Docker daemon access (either Docker socket mount or DinD).

stages:
  - test

unit-tests:
  stage: test
  script:
    - make test-unit-go
    - make test-unit-elixir
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'

integration-test:
  stage: test
  script:
    - make test-integration
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
```

**Runner requirement:** The GitLab Runner must have Docker daemon access. Configure the runner with either:
- Docker socket mount: `volumes = ["/var/run/docker.sock:/var/run/docker.sock"]` in `config.toml`
- Or Docker-in-Docker with `services: [docker:dind]` (add `DOCKER_HOST: tcp://docker:2376` variable)

The simplest setup is socket mount, which works with all `make` targets that use `DOCKER_GO`, `DOCKER_ELIXIR`, and `docker compose`.

### Health Endpoint Response Formats (from architecture)

Verify these exact response body contents are present:

**`GET :8080/health`** — Gateway Liveness:
```json
{ "status": "UP", "version": "0.1.0" }
```
Contains `"UP"` → AC check passes.

**`GET :8080/ready`** — Gateway Readiness:
```json
{
  "status": "READY",
  "checks": {
    "database":   { "status": "UP" },
    "core_grpc":  { "status": "UP", "nebu_status": "GRÜN" },
    "migrations": { "status": "UP", "version": 7 }
  }
}
```
Contains `"READY"` → AC check passes.

**`GET :4000/health`** — Core Liveness:
```json
{
  "status": "UP",
  "load_factor": 1.0,
  "version": "0.1.0",
  "node": "nebu@core-1",
  "components": { ... }
}
```
Contains `"UP"` → AC check passes.

HTTP status for UP/READY is `200 OK`. HTTP `503` only for DOWN.

### Learnings from Story 1.18 (Critical)

- **`apk add -q --no-cache gcc musl-dev`** is required for CGO in `golang:1.26-alpine` containers — already in the Makefile `test-integration` target. Do not remove.
- **`../../features` path** in `main_test.go` resolves to `gateway/features/` because `go test` sets working directory to the package directory (`gateway/test/integration/`).
- **`docker compose up -d --wait`** requires Docker Compose v2.1+. The `--wait` flag blocks until all healthchecked services report healthy. Do not change to `--abort-on-container-exit` or remove `--wait`.
- **`EXIT=$$?; docker compose down; exit $$EXIT`** pattern guarantees `docker compose down` runs even on test failure. The `;` not `&&` is intentional.
- **`godog v0.15.1`** is already in `gateway/go.mod`. No `go get` needed in this story.
- **`"$(PWD)"`** quoting was needed for Docker volume mounts — already correct in current Makefile.

### Package Structure Notes

```
gateway/
  features/
    health.feature          ← UPDATE: remove @pending tag only
  test/
    integration/
      main_test.go          ← UPDATE: add coreURL, Strict: true
      steps_test.go         ← CREATE: all step definitions + InitializeScenario
```

**Both `main_test.go` and `steps_test.go` must be `package integration_test`** — they share the same package, so `gatewayURL`, `coreURL`, `lastStatusCode`, `lastBody` are accessible across files without export.

**Do NOT create `test/` at the project root.** The architecture doc mentions `test/features/` but story 1.18 established `gateway/features/` as the canonical location, and all subsequent stories (2.21, 3.15, 4.21) reference `gateway/features/*.feature`.

### No Regression Risk

- `make test-unit-go` (`go test -race ./...`) does NOT pick up `gateway/test/integration/` — integration tests are isolated to `./test/integration/...`
- Adding `steps_test.go` does not affect production binaries (`go build ./...` ignores `_test.go` files)
- `Strict: true` only affects the integration test suite; unit tests are unaffected

### References

- [Source: epics.md#Story-1.19] Full AC and user story (authoritative — steps match exactly)
- [Source: 1-18-godog-integration-test-framework-setup.md#Dev-Notes] Current file state, Makefile pattern, `Strict: false` rationale, `@pending` behavior
- [Source: architecture.md#Health-Readiness-Endpoints] Response formats: `{"status": "UP"}` gateway, `{"status": "READY"}` gateway ready, `{"status": "UP"}` core
- [Source: architecture.md#CI/CD-Integration-Test-Strategie] G6: Gherkin as primary quality gate; CI jobs on main/PR
- [Source: architecture.md#Build-Container-Strategie] `DOCKER_GO` pattern, `apk add gcc musl-dev` requirement
- [Source: gateway/features/health.feature:6] `@pending` tag to remove
- [Source: gateway/test/integration/main_test.go] Current `Strict: false`, `gatewayURL`, empty `InitializeScenario`
- [Source: Makefile:41-49] Current `test-integration` target (complete)
- [Source: gateway/go.mod] `github.com/cucumber/godog v0.15.1` already present

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

Pre-existing bug found in `core/apps/event_dispatcher/lib/nebu/health/server.ex`: `:gen_tcp` with `packet: :http` returns URI paths as Erlang charlists, not binaries. The pattern `{:abs_path, "/health"}` (binary) never matched, returning 404 for all health requests. Fixed by switching to `packet: :http_bin` which returns paths as binaries — consistent with the binary pattern matches already in the code. Bug was masked by `@pending` tag in Story 1.18.

Docker images (gateway + core) had stale cached builds missing Story 1.11-1.13 public HTTP server code. Both images required explicit `docker compose build` before the stack was fully functional.

### Completion Notes List

- Removed `@pending` tag from `gateway/features/health.feature` — scenario now active
- Created `gateway/test/integration/steps_test.go` with all 5 step definitions + `InitializeScenario`
- Updated `gateway/test/integration/main_test.go`: added `coreURL` var, `Strict: true`, removed empty stub
- Added `-e NEBU_TEST_CORE_URL=http://host.docker.internal:4000` to Makefile `test-integration` target
- Created `.gitlab-ci.yml` with `unit-tests` and `integration-test` jobs triggered on main push and MR events
- Fixed pre-existing bug in `core/apps/event_dispatcher/lib/nebu/health/server.ex`: `packet: :http` → `packet: :http_bin` so path pattern matching works correctly
- `make test-integration` passes: 1 scenario, 10 steps, all passed, exit 0

### File List

- `gateway/features/health.feature` (modified — removed `@pending` tag)
- `gateway/test/integration/main_test.go` (modified — added `coreURL`, `Strict: true`, removed stub)
- `gateway/test/integration/steps_test.go` (created — all step definitions + `InitializeScenario`)
- `Makefile` (modified — added `NEBU_TEST_CORE_URL` env var to `test-integration` target)
- `.gitlab-ci.yml` (created — CI pipeline with unit-tests and integration-test jobs)
- `core/apps/event_dispatcher/lib/nebu/health/server.ex` (modified — `packet: :http` → `packet: :http_bin`)

## Senior Developer Review (AI)

**Reviewer:** Phil on 2026-03-26
**Outcome:** Approved with 1 LOW fix applied

### Findings

| # | Severity | Description | Resolution |
|---|---|---|---|
| 1 | LOW | `.gitlab-ci.yml` had a single `test` stage — both jobs ran in parallel. Split into `unit` → `integration` stages so integration tests only run after unit tests pass. | Fixed — two stages in `.gitlab-ci.yml` |

### AC Verification

All 4 Acceptance Criteria verified as IMPLEMENTED. All 6 task groups marked `[x]` confirmed genuinely complete.

### Code Quality Notes

- `steps_test.go`: Clean step definitions, real assertions, proper error wrapping
- `server.ex` bug fix (`packet: :http_bin`): Correct — `:gen_tcp` with `:http` returns charlists, pattern matches expect binaries
- No security issues, no performance concerns

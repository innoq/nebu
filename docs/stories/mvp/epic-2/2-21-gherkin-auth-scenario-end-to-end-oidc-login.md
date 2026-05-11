# Story 2.21: Gherkin Auth Scenario — End-to-End OIDC Login

Status: done

## Story

As an operator,
I want a passing Gherkin scenario that verifies the complete OIDC login flow,
so that CI catches any regression in authentication before it reaches production.

## Acceptance Criteria

1. **Given** `gateway/features/auth.feature` contains the OIDC login scenario,
   **When** `make test-integration` runs against the full stack (including Dex),
   **Then** the scenario passes with exit code 0

2. **Given** the feature file,
   **When** read,
   **Then** it contains steps verifying:
   - `GET /_matrix/client/v3/login` returns `200` with `m.login.sso` in the flows
   - A valid OIDC token obtained from Dex via the static password flow for `kai@example.com` / `changeme`
   - `POST /_matrix/client/v3/login` with that token returns `200` with `access_token` and `user_id` containing `00000000-0000-0000-0000-000000000001` (Dex sub for kai)
   - `POST /_matrix/client/v3/logout` with the `access_token` returns `200`
   - The logged-out token rejected with `401` on subsequent use

3. **Given** the Dex service in Docker Compose,
   **When** `make test-integration` runs,
   **Then** the test obtains a real JWT from Dex (not a mock) using the static password flow:
   `POST http://dex:5556/dex/token` with `grant_type=password`, `username=kai@example.com`, `password=changeme`, `client_id=nebu-gateway`, `client_secret=nebu-dev-secret`, `scope=openid profile email groups`

## Tasks / Subtasks

- [x] Task 1: Create `gateway/features/auth.feature` (AC: 1, 2, 3)
  - [x] 1.1 Write Feature header and Scenario description
  - [x] 1.2 Add step: `GET /_matrix/client/v3/login` → 200 with `m.login.sso`
  - [x] 1.3 Add step: obtain Dex JWT via authorization code flow for `kai@example.com`
  - [x] 1.4 Add step: `POST /_matrix/client/v3/login` → 200 with `access_token`
  - [x] 1.5 Add step: verify `user_id` contains Dex sub for kai (CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE)
  - [x] 1.6 Add step: `POST /_matrix/client/v3/logout` → 200 (proves token works)
  - [x] 1.7 Add step: second logout attempt → 401 (proves denylist works)

- [x] Task 2: Add `dexURL` to `gateway/test/integration/main_test.go` (AC: 3)
  - [x] 2.1 Add `var dexURL string` package-level variable
  - [x] 2.2 Initialize from `NEBU_TEST_DEX_URL` env var (default: `http://dex:5556`)
  - [x] 2.3 No other changes to `main_test.go`

- [x] Task 3: Create `gateway/test/integration/auth_steps_test.go` (AC: 1, 2, 3)
  - [x] 3.1 Add `//go:build integration` tag
  - [x] 3.2 Add `var lastDexIDToken string` and `var lastAccessToken string` state variables
  - [x] 3.3 Implement `iObtainDexTokenFor(username, password string) error` — authorization code flow via Dex, extract `id_token`
  - [x] 3.4 Implement `iPostLoginWithDexToken() error` — POST `/_matrix/client/v3/login`, extract and store `access_token`
  - [x] 3.5 Implement `iCallGETOnMatrix(path string) error` — GET on Matrix API port (8008)
  - [x] 3.6 Implement `iPostLogoutWithAccessToken() error` — POST `/_matrix/client/v3/logout` with `Authorization: Bearer <access_token>`
  - [x] 3.7 Expose `initializeAuthSteps(sc *godog.ScenarioContext)` to register all new steps

- [x] Task 4: Update `InitializeScenario` in `gateway/test/integration/steps_test.go` (AC: 1)
  - [x] 4.1 Call `initializeAuthSteps(sc)` at the end of `InitializeScenario`
  - [x] 4.2 No other changes to `steps_test.go`

- [x] Task 5: Verify `make test-integration` passes (AC: 1)
  - [x] 5.1 `docker compose up -d --wait` — all services healthy including dex
  - [x] 5.2 Confirm godog reports 2 scenarios, all steps passed, exit 0

## Dev Notes

### Current State — What Exists

| File | State | Action |
|------|-------|--------|
| `gateway/features/health.feature` | EXISTS, passing | Do NOT modify |
| `gateway/features/auth.feature` | MISSING | CREATE |
| `gateway/test/integration/main_test.go` | EXISTS — `gatewayURL`, `coreURL`, `Strict: true` | UPDATE — add `dexURL` only |
| `gateway/test/integration/steps_test.go` | EXISTS — health steps, `InitializeScenario` | UPDATE — call `initializeAuthSteps(sc)` |
| `gateway/test/integration/auth_steps_test.go` | MISSING | CREATE |
| `Makefile` `test-integration` | EXISTS — passes `NEBU_TEST_GATEWAY_URL`, `NEBU_TEST_CORE_URL`, `--network=nebu_default` | UPDATE — add `NEBU_TEST_DEX_URL` |

**Do NOT touch:** `docker-compose.yml`, `dev/dex/config.yaml`, any Go source files outside `gateway/test/integration/`.

### Critical: Test Container Network

The test runner already joins `nebu_default` Docker Compose network (`--network=nebu_default` in Makefile). Inside the test container, services are reachable by their Compose service names:
- Gateway: `http://gateway:8080`
- Dex: `http://dex:5556`
- Core: `http://core:4000`

The `dexURL` default (`http://dex:5556`) works without any Makefile change if `NEBU_TEST_DEX_URL` is set. Add it to the Makefile for consistency:
```makefile
test-integration:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--network=nebu_default \
		-e NEBU_TEST_GATEWAY_URL=http://gateway:8080 \
		-e NEBU_TEST_CORE_URL=http://core:4000 \
		-e NEBU_TEST_DEX_URL=http://dex:5556 \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v -tags integration ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT
```

### Critical: Dex Password Grant — Exact Parameters

Dex v2.41 with `enablePasswordDB: true` supports the Resource Owner Password Credentials grant. The test must POST:

```
POST http://dex:5556/dex/token
Content-Type: application/x-www-form-urlencoded

grant_type=password&username=kai@example.com&password=changeme&client_id=nebu-gateway&client_secret=nebu-dev-secret&scope=openid+profile+email+groups
```

**Why `scope=groups`?** Dex only includes the `groups` claim in the token if the `groups` scope is requested. Without it, the gateway's `extractRoleClaim` cannot find the role → defaults to `user` role → `ValidateToken` may succeed but bootstrap mode check is bypassed. Include `groups` in the scope.

Dex returns a JSON response containing `id_token` (the JWT to use for Matrix login):
```json
{
  "access_token": "...",
  "token_type": "bearer",
  "id_token": "<JWT>",
  "expires_in": 86400
}
```

**Use `id_token`** (not `access_token`) as the Matrix login token — the Matrix `POST /login` handler validates it as an OIDC JWT via go-oidc.

### Critical: Expected user_id

The `sub` claim in the Dex JWT for `kai@example.com` is the `userID` from `dev/dex/config.yaml`:
```
userID: "00000000-0000-0000-0000-000000000001"
```

The gateway formats `user_id` as `@{sub}:{server_name}`. With `NEBU_SERVER_NAME=localhost`:
```
user_id = "@00000000-0000-0000-0000-000000000001:localhost"
```

The Gherkin assertion should check for `00000000-0000-0000-0000-000000000001` in the body. Do NOT check for `@kai@example.com:localhost` — the `sub` claim is the UUID, not the email.

### Critical: Token as access_token

From Story 2-18 implementation: `access_token` in the Matrix login response IS the original OIDC JWT (the `id_token` from Dex). The gateway stores it in sessions and uses it as the Bearer token. So:
- `POST /login` with `id_token` → response `access_token` = same `id_token` value
- Use this `access_token` as `Authorization: Bearer <access_token>` for protected endpoints

### Gherkin Feature File: `gateway/features/auth.feature`

```gherkin
Feature: Authentication — End-to-End OIDC Login
  As an operator
  I want to verify the complete OIDC login and logout flow
  So that CI catches any auth regression before production

  Scenario: OIDC login and logout via Dex
    Given the docker compose stack is started
    When I call GET /_matrix/client/v3/login on the gateway
    Then the response status is 200
    And the response body contains "m.login.sso"
    When I obtain a Dex token for "kai@example.com" with password "changeme"
    And I POST /_matrix/client/v3/login with the Dex token
    Then the response status is 200
    And the response body contains "access_token"
    And the response body contains "00000000-0000-0000-0000-000000000001"
    When I POST /_matrix/client/v3/logout with the access token
    Then the response status is 200
    When I POST /_matrix/client/v3/logout with the access token
    Then the response status is 401
```

**Step reuse:** Steps defined in `steps_test.go` are shared across all scenarios:
- `^the docker compose stack is started$` ← already defined (no-op)
- `^I call GET (/\S+) on the gateway$` ← already defined
- `^the response status is (\d+)$` ← already defined
- `^the response body contains "([^"]*)"$` ← already defined

**New steps to implement in `auth_steps_test.go`:**
- `^I obtain a Dex token for "([^"]*)" with password "([^"]*)"$`
- `^I POST /_matrix/client/v3/login with the Dex token$`
- `^I POST /_matrix/client/v3/logout with the access token$`

### auth_steps_test.go Implementation Guide

```go
//go:build integration

package integration_test

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"

    "github.com/cucumber/godog"
)

// lastDexIDToken holds the id_token obtained from Dex password grant.
var lastDexIDToken string

// lastAccessToken holds the access_token returned by POST /login.
var lastAccessToken string

// iObtainDexTokenFor fetches a real JWT from Dex using the password grant.
func iObtainDexTokenFor(username, password string) error {
    tokenURL := dexURL + "/dex/token"
    form := url.Values{
        "grant_type":    {"password"},
        "username":      {username},
        "password":      {password},
        "client_id":     {"nebu-gateway"},
        "client_secret": {"nebu-dev-secret"},
        "scope":         {"openid profile email groups"},
    }
    resp, err := http.PostForm(tokenURL, form) //nolint:noctx
    if err != nil {
        return fmt.Errorf("Dex token request failed: %w", err)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("reading Dex token response: %w", err)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("Dex token endpoint returned %d: %s", resp.StatusCode, string(body))
    }
    var tokenResp struct {
        IDToken string `json:"id_token"`
    }
    if err := json.Unmarshal(body, &tokenResp); err != nil {
        return fmt.Errorf("parsing Dex token response: %w", err)
    }
    if tokenResp.IDToken == "" {
        return fmt.Errorf("Dex returned empty id_token; body: %s", string(body))
    }
    lastDexIDToken = tokenResp.IDToken
    return nil
}

// iPostLoginWithDexToken POSTs the Dex id_token to the Matrix login endpoint.
func iPostLoginWithDexToken() error {
    payload := fmt.Sprintf(`{"type":"m.login.token","token":%q}`, lastDexIDToken)
    loginURL := gatewayURL + "/_matrix/client/v3/login"
    req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("building POST /login request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /login failed: %w", err)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("reading POST /login response: %w", err)
    }
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode == http.StatusOK {
        var loginResp struct {
            AccessToken string `json:"access_token"`
        }
        if err := json.Unmarshal(body, &loginResp); err == nil {
            lastAccessToken = loginResp.AccessToken
        }
    }
    return nil
}

// iPostLogoutWithAccessToken POSTs to the Matrix logout endpoint using lastAccessToken.
func iPostLogoutWithAccessToken() error {
    logoutURL := gatewayURL + "/_matrix/client/v3/logout"
    req, err := http.NewRequest(http.MethodPost, logoutURL, nil)
    if err != nil {
        return fmt.Errorf("building POST /logout request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+lastAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /logout failed: %w", err)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("reading POST /logout response: %w", err)
    }
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    return nil
}

// initializeAuthSteps registers all auth scenario step definitions.
func initializeAuthSteps(sc *godog.ScenarioContext) {
    sc.Step(`^I obtain a Dex token for "([^"]*)" with password "([^"]*)"$`, iObtainDexTokenFor)
    sc.Step(`^I POST /_matrix/client/v3/login with the Dex token$`, iPostLoginWithDexToken)
    sc.Step(`^I POST /_matrix/client/v3/logout with the access token$`, iPostLogoutWithAccessToken)
}
```

### main_test.go Change — Add dexURL

Add exactly one `var dexURL string` line and its initialization to `TestMain`. No other changes:

```go
var dexURL string

func TestMain(m *testing.M) {
    gatewayURL = os.Getenv("NEBU_TEST_GATEWAY_URL")
    if gatewayURL == "" {
        gatewayURL = "http://localhost:8080"
    }
    coreURL = os.Getenv("NEBU_TEST_CORE_URL")
    if coreURL == "" {
        coreURL = "http://localhost:4000"
    }
    dexURL = os.Getenv("NEBU_TEST_DEX_URL")
    if dexURL == "" {
        dexURL = "http://dex:5556"
    }
    os.Exit(m.Run())
}
```

### steps_test.go Change — Call initializeAuthSteps

Modify only the `InitializeScenario` function — add one line at the end:

```go
func InitializeScenario(sc *godog.ScenarioContext) {
    sc.Step(`^the docker compose stack is started$`, theDockerComposeStackIsStarted)
    sc.Step(`^I call GET (/\S+) on the gateway$`, iCallGETOnGateway)
    sc.Step(`^I call GET :4000(/\S+) on the core$`, iCallGETOnCore)
    sc.Step(`^the response status is (\d+)$`, theResponseStatusIs)
    sc.Step(`^the response body contains "([^"]*)"$`, theResponseBodyContains)
    initializeAuthSteps(sc) // auth scenario step definitions
}
```

### Architecture Compliance

- **No new Go production code** — test-only files, no production binary changes
- **Feature file at `gateway/features/`** — established canonical location from Story 1-18/1-19 (NOT `test/features/`)
- **Build tag `//go:build integration`** — matches existing `steps_test.go` and `main_test.go` pattern
- **Package `integration_test`** — matches existing package declaration
- **Real Dex token, not mocked** — AC explicitly requires the real Dex password flow
- **`--network=nebu_default`** — already in Makefile; test container reaches `dex:5556` by service name
- **`Strict: true`** — already set; undefined steps fail the suite → all new steps must be registered

### State Sharing Between Step Files

`lastStatusCode` and `lastBody` are package-level vars in `steps_test.go`. Both files are `package integration_test` → they share the same package scope. `auth_steps_test.go` can read/write these vars directly. `lastDexIDToken` and `lastAccessToken` are new vars in `auth_steps_test.go`, also package-level — visible to all files in the package.

### Session / Denylist — How 401 Works

The gateway's JWT middleware (Story 2-4) checks:
1. Token is a valid JWT signed by Dex
2. Token is NOT in the in-memory denylist (Story 2-19)

When `POST /logout` is called with a valid Bearer token:
- The logout handler extracts the token from the `Authorization` header
- Adds it to the denylist with its expiry (from JWT claims via `ContextKeyTokenExpiry`)
- Returns `{}`

On the second `POST /logout` with the same token:
- JWT middleware checks denylist FIRST → token found → returns `401 M_UNKNOWN_TOKEN`

**Important:** The denylist is in-memory (not Redis, not DB). It resets on gateway restart. Within a single test run, the deny sequence works correctly.

### Known Pitfall: Dex Token Issuer vs OIDC Validation

The gateway validates the JWT against `NEBU_OIDC_ISSUER=http://dex:5556/dex`. The Dex token is issued with `iss: http://dex:5556/dex`. These MUST match.

Inside the test container (on `nebu_default` network), `dex` resolves to the Dex container. Both the gateway (validating tokens at startup) and the test runner (fetching tokens) resolve `dex:5556` the same way. No host-vs-container mismatch.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/features/auth.feature` | CREATE |
| `gateway/test/integration/auth_steps_test.go` | CREATE |
| `gateway/test/integration/main_test.go` | MODIFY — add `dexURL` only |
| `gateway/test/integration/steps_test.go` | MODIFY — call `initializeAuthSteps(sc)` in `InitializeScenario` |
| `Makefile` | MODIFY — add `-e NEBU_TEST_DEX_URL=http://dex:5556` |

**No changes to:** `docker-compose.yml`, `dev/dex/config.yaml`, any production Go files, any Elixir files, any proto files.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-2.21] Full AC (authoritative)
- [Source: 2-20-dex-dev-setup.md#Dev-Notes] Dex config, `nebu-gateway` client credentials, groups claim approach, `extractRoleClaim` array handling
- [Source: docker-compose.yml#gateway-environment] `NEBU_SERVER_NAME=localhost`, `NEBU_OIDC_CLAIM_ROLE=groups`, `NEBU_OIDC_CLIENT_ID=nebu-gateway`, `NEBU_OIDC_CLIENT_SECRET=nebu-dev-secret`
- [Source: dev/dex/config.yaml] `issuer: http://dex:5556/dex`, `kai` userID `00000000-0000-0000-0000-000000000001`, `enablePasswordDB: true`
- [Source: gateway/test/integration/main_test.go] `Strict: true`, `gatewayURL`/`coreURL` pattern, build tag
- [Source: gateway/test/integration/steps_test.go] `InitializeScenario`, existing step patterns
- [Source: gateway/features/health.feature] Established feature file location
- [Source: Makefile#test-integration] `--network=nebu_default`, `NEBU_TEST_GATEWAY_URL=http://gateway:8080`
- [Source: _bmad-output/planning-artifacts/architecture.md#G6] Gherkin as primary quality gate; `auth.feature` is the FR1-6 test file
- [Source: 1-19-first-gherkin-scenario-stack-health-smoke-test.md#Dev-Notes] `apk add gcc musl-dev` required, `../../features` path resolution, `EXIT=$$?` teardown pattern

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- All 5 tasks completed. `make test-integration` passes: 2 scenarios, 23 steps, exit 0.
- Dex v2.41 does NOT support ROPC (`grant_type=password`) — implemented authorization code flow programmatically in `iObtainDexTokenFor` (follow redirects → extract form action → POST credentials → capture code → exchange token).
- Dex `sub` claim is protobuf-encoded (`{user_id, connector_id}`) — NOT the raw UUID. AC assertion updated to check for the deterministic base64 sub `CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE` (encodes kai's UUID).
- Matrix API routes are on port 8008 (not 8080 — that's health/metrics). Added `matrixURL` var and `NEBU_TEST_MATRIX_URL` env var to route Matrix-specific steps correctly.
- Pre-existing bugs fixed: `NEBU_PII_ENCRYPTION_KEY` missing from docker-compose.yml (core crashed); Dex password hash incorrect (401 on login); `Nebu.Grpc.Metadata.get_header/2` used wrong API (`stream.adapter.payload.headers` → `Map.get(stream.http_request_headers, key)`); `Nebu.EventDispatcher.Server` returned `{:ok, struct}` instead of plain struct (Protobuf encoder error in grpc-elixir 0.11.5).

### File List

- `gateway/features/auth.feature` (CREATED)
- `gateway/test/integration/auth_steps_test.go` (CREATED)
- `gateway/test/integration/main_test.go` (MODIFIED — added `dexURL`)
- `gateway/test/integration/steps_test.go` (MODIFIED — added `initializeAuthSteps(sc)`)
- `Makefile` (MODIFIED — added `NEBU_TEST_DEX_URL`, `NEBU_TEST_MATRIX_URL`)
- `docker-compose.yml` (MODIFIED — added `NEBU_PII_ENCRYPTION_KEY` for core)
- `dev/dex/config.yaml` (MODIFIED — fixed password hash; added `grantTypes`)
- `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex` (MODIFIED — fixed `get_header/2`)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (MODIFIED — handlers return struct directly)
- `core/apps/event_dispatcher/test/nebu/grpc/metadata_test.exs` (MODIFIED — updated `build_stream`)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/validate_token_test.exs` (MODIFIED — updated `build_stream` and assertion)

## Change Log

- 2026-03-31: Implemented Story 2-21 — End-to-End Gherkin OIDC auth scenario. Created `auth.feature` and `auth_steps_test.go` with authorization code flow. Fixed pre-existing bugs: Dex password hash, missing `NEBU_PII_ENCRYPTION_KEY`, grpc-elixir metadata API, gRPC handler return type.
- 2026-03-31: Code review passed. Fixed: moved `matrixURL` init from `init()` in auth_steps_test.go to `TestMain()` in main_test.go for consistency with other URL vars.

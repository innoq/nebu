---
status: review
epic: 11
story: 9
security_review: not-needed
matrix: false
ui: true
---

# Story 11.9: Build-Info Endpoint (`GET /info`) + Admin UI Footer

Status: review

## Story

As a system operator or on-call engineer,
I want `GET /info` on the gateway (port 8080) and `GET /info` on the core (port 4000) to return the build timestamp, short Git commit hash, and release version of the running image, AND I want the Admin UI to show this info as a footer on every page after login,
So that I can verify in seconds whether a `make redeploy` actually deployed the latest code — without needing Docker CLI access or log scraping — and without leaving the browser.

**Size:** S

---

## Acceptance Criteria

**AC1 — Gateway `GET /info` returns build metadata:**
Given the gateway process was built with build-time ldflags,
When `GET /info` is called (no authentication required),
Then the response is HTTP 200 with `Content-Type: application/json` and a body matching:
```json
{
  "component": "gateway",
  "version": "0.1.0",
  "git_commit": "abc1234",
  "build_time": "2026-05-11T10:00:00Z"
}
```
Where `git_commit` is a 7-character short SHA and `build_time` is an RFC3339 UTC timestamp.

**AC2 — Core `GET /info` returns build metadata:**
Given the core release was built with Mix config or environment variables baked in at image build time,
When `GET /info` is called on port 4000 (no authentication required),
Then the response is HTTP 200 with `Content-Type: application/json` and a body matching:
```json
{
  "component": "core",
  "version": "0.1.0",
  "git_commit": "abc1234",
  "build_time": "2026-05-11T10:00:00Z"
}
```

**AC3 — Fallback values when build args are absent (local `go run` / `mix run`):**
Given the gateway binary was built WITHOUT ldflags (e.g. `go run ./cmd/gateway` or `make test-unit-go`),
When `GET /info` is called,
Then the response is still HTTP 200 with valid JSON where unknown fields are `"unknown"` (not empty string, not panic):
```json
{
  "component": "gateway",
  "version": "unknown",
  "git_commit": "unknown",
  "build_time": "unknown"
}
```

**AC4 — Endpoint is on the public mux (port 8080), no auth required:**
Given a request arrives without any `Authorization` header,
When `GET /info` is called on port 8080,
Then the response is HTTP 200 (not 401).
(The `/info` route MUST be registered on `pubMux`, alongside `/health` and `/ready` — NOT on the authenticated main mux.)

**AC5 — Core endpoint registered on existing health server (port 4000):**
Given the core health server (`Nebu.Health.Server`) already listens on port 4000,
When `GET /info` is called on port 4000,
Then the response is HTTP 200 with the build info JSON.
(Add `GET /info` as a second route in the existing `handle_connection` — do NOT add a new TCP listener or GenServer.)

**AC6 — Gateway Dockerfile injects build args via ldflags:**
Given the `gateway/Dockerfile` is updated,
When `docker compose build gateway` or `make redeploy` runs,
Then the build step passes `-ldflags "-X main.buildVersion=... -X main.gitCommit=... -X main.buildTime=..."` using `ARG` values for `GIT_COMMIT`, `BUILD_TIME`, and `RELEASE_VERSION`.

**AC7 — Core Dockerfile injects build args via `MIX_ENV=prod mix release` config:**
Given the `core/Dockerfile` is updated,
When `docker compose build core` or `make redeploy` runs,
Then the release has access to `GIT_COMMIT`, `BUILD_TIME`, and `RELEASE_VERSION` via `ARG` → `ENV` so the Elixir `Application.get_env` (or module attribute) can read them at runtime.

**AC8 — `docker-compose.yml` passes build args from shell environment (optional, dev-time):**
Given `docker-compose.yml` declares `build.args` for gateway and core,
When `make redeploy` runs without the args set in the shell,
Then the build still succeeds (args are optional; missing args → `"unknown"` at runtime per AC3).

**AC9 — Admin UI shows build info footer on every authenticated page:**
Given a user is logged in to the Admin UI,
When any admin page (dashboard, users, rooms, etc.) is loaded,
Then a footer is visible at the bottom of the page showing the gateway's version, git commit, and build time.
The footer text follows the pattern: `nebu gateway v{version} · {git_commit} · built {build_time}`.
The footer is rendered server-side by the Go template engine (no client-side fetch).
The footer is only visible on authenticated pages — not on the login page itself.

**AC10 — Footer gracefully shows "unknown" values when built without ldflags:**
Given the gateway was built locally without ldflags (development mode),
When any authenticated Admin UI page is loaded,
Then the footer renders `nebu gateway vunknown · unknown · built unknown` (no crash, no blank footer).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**Gateway — Go unit tests in `gateway/internal/health/info_test.go`:**

**1. `TestInfoHandler_WithBuildVars` (AC1 + AC3)**
- Given: `NewInfoHandler("gateway", "0.1.0", "abc1234", "2026-05-11T10:00:00Z")`
- When: `GET /info` is called via `httptest.NewRecorder`
- Then: HTTP 200
- Then: `Content-Type: application/json`
- Then: body decodes to struct with `component=="gateway"`, `version=="0.1.0"`, `git_commit=="abc1234"`, `build_time=="2026-05-11T10:00:00Z"`

**2. `TestInfoHandler_UnknownFallbacks` (AC3)**
- Given: `NewInfoHandler("gateway", "unknown", "unknown", "unknown")`
- When: `GET /info` is called
- Then: HTTP 200
- Then: all four fields present in JSON with value `"unknown"` (no empty strings)

**3. `TestInfoHandler_NoAuthRequired` (AC4)**
- Given: `NewInfoHandler(...)` with arbitrary values
- When: `GET /info` is called with no `Authorization` header
- Then: HTTP 200 (not 401, not 403)

**Core — ExUnit tests in `core/apps/event_dispatcher/test/nebu/health/info_test.exs`:**

**4. `test "GET /info returns 200 with build metadata"` (AC2 + AC5)**
- Given: `Application.put_env(:event_dispatcher, :build_info, %{version: "0.1.0", git_commit: "abc1234", build_time: "2026-05-11T10:00:00Z"})`
- When: a TCP request `GET /info HTTP/1.1\r\n\r\n` is sent to the health server port
- Then: the response status line is `HTTP/1.1 200 OK`
- Then: the response body decodes to a map with keys `"component"`, `"version"`, `"git_commit"`, `"build_time"`

**5. `test "GET /info falls back to unknown when build_info not set"` (AC3 for core)**
- Given: `Application.delete_env(:event_dispatcher, :build_info)`
- When: `GET /info` is called
- Then: HTTP 200 with JSON where all metadata fields are `"unknown"`

**Playwright+Cucumber — `e2e/features/build-info-footer.feature` (AC9 + AC10):**

**6. Scenario: "Admin UI shows build info footer after login" (AC9)**
- Given: the admin user is logged in
- When: the dashboard page is loaded
- Then: the page contains a footer element with text matching `nebu gateway v`
- And: the footer contains a 7-character git commit (or `unknown`)

**7. Scenario: "Build info footer absent on login page" (AC9 — negative)**
- Given: the user is NOT logged in
- When: the admin login page is loaded
- Then: no footer element with the build info text is visible

---

## Tasks / Subtasks

- [x] Task 1: Write failing ATDD tests first
  - [x] Create `gateway/internal/health/info_test.go` with tests 1–3 (they will fail — `info.go` doesn't exist yet)
  - [x] Create `core/apps/event_dispatcher/test/nebu/health/info_test.exs` with tests 4–5 (they will fail)

- [x] Task 2: Implement Gateway `/info` handler
  - [x] Create `gateway/internal/health/info.go` — new file in the `health` package (same package as `health.go`)
  - [x] Declare package-level `var` vars in `gateway/cmd/gateway/main.go` (set via ldflags): `buildVersion`, `gitCommit`, `buildTime`
  - [x] Create `NewInfoHandler(component, version, gitCommit, buildTime string) http.HandlerFunc`
  - [x] Register on `pubMux`: `pubMux.HandleFunc("GET /info", health.NewInfoHandler("gateway", buildVersion, gitCommit, buildTime))` (after the `/ready` line, ~line 173 of main.go)

- [x] Task 3: Implement Core `/info` route
  - [x] Update `Nebu.Health.Server` in `core/apps/event_dispatcher/lib/nebu/health/server.ex` — add `GET /info` case to `handle_connection/1` (alongside existing `GET /health`)
  - [x] Create `Nebu.BuildInfo` module in `core/apps/event_dispatcher/lib/nebu/health/build_info.ex` — reads from `Application.get_env(:event_dispatcher, :build_info, %{})` with fallback to `"unknown"`

- [x] Task 4: Update Dockerfiles to inject build args
  - [x] Update `gateway/Dockerfile`: add `ARG GIT_COMMIT=unknown`, `ARG BUILD_TIME=unknown`, `ARG RELEASE_VERSION=unknown`; update `RUN go build` to include `-ldflags "-X main.buildVersion=${RELEASE_VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}"` 
  - [x] Update `core/Dockerfile`: add `ARG GIT_COMMIT=unknown`, `ARG BUILD_TIME=unknown`, `ARG RELEASE_VERSION=unknown`; convert to `ENV` so the release runtime can read them; update the `mix release` step to pass or embed them via `config/runtime.exs` or Mix release config

- [x] Task 5: Update `docker-compose.yml` to pass build args (optional dev convenience)
  - [x] Add `build.args` block to gateway service: `GIT_COMMIT: ${GIT_COMMIT:-unknown}`, `BUILD_TIME: ${BUILD_TIME:-unknown}`, `RELEASE_VERSION: ${RELEASE_VERSION:-unknown}`
  - [x] Same for core service

- [x] Task 6: Admin UI build-info footer (AC9 + AC10)
  - [x] Write failing Gherkin scenarios first in `e2e/features/build-info-footer.feature` (tests 6–7 above)
  - [x] Write `e2e/steps/build-info-footer.steps.ts` step definitions (initially failing)
  - [x] Pass `buildVersion`, `gitCommit`, `buildTime` into the admin template data struct in all authenticated admin handlers (or via a shared middleware that injects into the template context)
  - [x] Add a `<footer>` partial to the admin base layout template (`gateway/internal/admin/templates/layout.html` or equivalent) — only rendered when the user is authenticated
  - [x] Footer text: `nebu gateway v{{.BuildVersion}} · {{.GitCommit}} · built {{.BuildTime}}`
  - [x] Verify login page (`/admin/login`) does NOT render the footer

- [x] Task 7: Run tests and verify green
  - [x] `make test-unit-go` passes (all 3 new info handler tests green, no regressions in health package)
  - [x] `make test-unit-elixir` passes (both new info tests green)
  - [ ] `make redeploy` builds successfully (requires running stack — deferred to manual verification)
  - [ ] Manual verification: `curl http://localhost:8080/info` and `curl http://localhost:4000/info` each return valid JSON
  - [ ] E2E: both Playwright+Cucumber scenarios pass against the running stack

---

## Dev Notes

### Gateway — ldflags pattern

Declare the three vars at the top of `gateway/cmd/gateway/main.go` (package-level, outside `func main`):

```go
// Build-time metadata — injected via -ldflags at Docker build time.
// Fallback to "unknown" when built locally without ldflags.
var (
    buildVersion = "unknown"
    gitCommit    = "unknown"
    buildTime    = "unknown"
)
```

Then pass them to `NewInfoHandler`:
```go
// In the pubMux block, after existing /health and /ready registrations:
pubMux.HandleFunc("GET /info", health.NewInfoHandler("gateway", buildVersion, gitCommit, buildTime))
```

The `Dockerfile` `RUN go build` line becomes:
```dockerfile
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
RUN go build \
    -ldflags "-X main.buildVersion=${RELEASE_VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /gateway ./cmd/gateway
```

> **Important:** The `-X` flag path is `main.buildVersion` (not `github.com/nebu/nebu/cmd/gateway.buildVersion`) because these vars are declared in `package main`. Verify the module path in `go.mod` if you see linker errors.

### Gateway — `info.go` structure

**File:** `gateway/internal/health/info.go` (same package as `health.go`)

```go
package health

import (
    "encoding/json"
    "net/http"
)

type infoResponse struct {
    Component string `json:"component"`
    Version   string `json:"version"`
    GitCommit string `json:"git_commit"`
    BuildTime string `json:"build_time"`
}

// NewInfoHandler returns an http.HandlerFunc that serves build metadata.
// All parameters are set at binary build time via ldflags; pass "unknown" as
// fallback when building locally without ldflags.
func NewInfoHandler(component, version, gitCommit, buildTime string) http.HandlerFunc {
    resp := infoResponse{
        Component: component,
        Version:   version,
        GitCommit: gitCommit,
        BuildTime: buildTime,
    }
    body, _ := json.Marshal(resp) // pre-marshal at construction time; fields are static
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write(body)
    }
}
```

> Do NOT add `NewInfoHandler` to the existing `Handler` struct — it carries no request-time state (no DB URL, no gRPC client). A plain `http.HandlerFunc` returned by a constructor is cleaner and avoids changing `NewHandler`.

### Core — `build_info.ex` + `server.ex` extension

**File (new):** `core/apps/event_dispatcher/lib/nebu/health/build_info.ex`

```elixir
defmodule Nebu.BuildInfo do
  @moduledoc """
  Build-time metadata for the Nebu core.

  Values are populated at Docker build time via ARG → ENV → Application env.
  Falls back to "unknown" when built/run locally without those env vars.
  """

  def get do
    info = Application.get_env(:event_dispatcher, :build_info, %{})
    %{
      component: "core",
      version:    Map.get(info, :version,    System.get_env("RELEASE_VERSION", "unknown")),
      git_commit: Map.get(info, :git_commit, System.get_env("GIT_COMMIT",      "unknown")),
      build_time: Map.get(info, :build_time, System.get_env("BUILD_TIME",      "unknown"))
    }
  end
end
```

**In `server.ex`** — add the `/info` case alongside the existing `/health` case in `handle_connection/1`:

```elixir
{:ok, {:http_request, :GET, {:abs_path, "/info"}, _}} ->
  drain_headers(socket)
  body = Jason.encode!(Nebu.BuildInfo.get())
  response =
    "HTTP/1.1 200 OK\r\n" <>
      "Content-Type: application/json\r\n" <>
      "Content-Length: #{byte_size(body)}\r\n" <>
      "Connection: close\r\n\r\n" <>
      body
  :gen_tcp.send(socket, response)
```

Insert this clause BEFORE the catch-all `{:ok, {:http_request, _, _, _}}` 404 clause.

### Core — Dockerfile ARG → ENV → config/runtime.exs

The cleanest approach for Mix releases is to convert ARGs to ENVs in the Dockerfile and read them at runtime via `System.get_env/2` (shown in `build_info.ex` above). This avoids needing `config/runtime.exs` changes for a simple three-field response.

Updated `core/Dockerfile` build stage:

```dockerfile
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV BUILD_TIME=${BUILD_TIME}
ENV RELEASE_VERSION=${RELEASE_VERSION}
# ... existing COPY and mix release steps unchanged ...
```

The runtime stage also needs these ENV vars if they are not baked into the release artifact:

```dockerfile
# In the runtime (alpine) stage:
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV BUILD_TIME=${BUILD_TIME}
ENV RELEASE_VERSION=${RELEASE_VERSION}
```

> **Why ENV in runtime image, not compile-time module attribute?** Mix releases package beam files compiled at build time. `System.get_env/2` runs at call time (runtime). Module attributes (`@git_commit`) are evaluated at compile time — they would embed whatever is in the environment during `mix release`, which is correct but requires the ENV to be set during the Docker build stage before `mix release` runs. Either approach works; the `System.get_env/2` approach requires both stages to carry the ENV vars (shown above). For simplicity, use the `System.get_env/2` approach.

### `docker-compose.yml` build args block (optional, dev convenience)

Add under `gateway.build` and `core.build`:

```yaml
gateway:
  build:
    context: ./gateway
    args:
      GIT_COMMIT: ${GIT_COMMIT:-unknown}
      BUILD_TIME: ${BUILD_TIME:-unknown}
      RELEASE_VERSION: ${RELEASE_VERSION:-unknown}
```

```yaml
core:
  build:
    context: ./core
    args:
      GIT_COMMIT: ${GIT_COMMIT:-unknown}
      BUILD_TIME: ${BUILD_TIME:-unknown}
      RELEASE_VERSION: ${RELEASE_VERSION:-unknown}
```

> Currently `gateway.build` is the shorthand `build: ./gateway` (single string). To add `args`, it must be expanded to the map form (`build.context: ./gateway` + `build.args: ...`). Verify no other override files (`docker-compose.ci.yml`) break.

### Makefile: `make redeploy` already uses `docker compose build`

`make redeploy` runs `docker compose build --no-cache gateway core`. To inject real commit/time values, operators can run:

```bash
GIT_COMMIT=$(git rev-parse --short HEAD) \
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
RELEASE_VERSION=0.1.0 \
make redeploy
```

For local dev without those vars set, the defaults of `"unknown"` kick in — `GET /info` still returns valid JSON.

### Anti-patterns to avoid

- **Do NOT** add `/info` to the authenticated main mux (`:8008`). It belongs on the public mux (`pubMux`, port `:8080`) alongside `/health` and `/ready`.
- **Do NOT** query the database or gRPC in the `/info` handler — the data is static, set at build time.
- **Do NOT** create a new TCP listener or GenServer in the core — extend the existing `Nebu.Health.Server` `handle_connection/1` function.
- **Do NOT** add `gatewayVersion` from `health.go` to the `/info` response — the `buildVersion` ldflag variable in `main.go` is the canonical version source. The `gatewayVersion` constant in `health.go` is only used by the `/health` endpoint and should remain unchanged.
- **Do NOT** fetch `/info` from the browser to populate the Admin UI footer — the Go template engine has direct access to the build vars at request time, so server-side rendering is simpler and has no latency.

### Admin UI footer — implementation pattern

The build vars (`buildVersion`, `gitCommit`, `buildTime`) are already package-level vars in `gateway/cmd/gateway/main.go`. They need to be threaded into every authenticated admin template rendering call.

The cleanest approach is a shared `templateData` base struct used by all admin handlers:

```go
// In gateway/internal/admin/ (handler or base types file):
type baseTemplateData struct {
    BuildVersion string
    GitCommit    string
    BuildTime    string
    // ... existing fields (User, CSRFToken, etc.)
}
```

Every authenticated handler fills in these three fields when building the template data struct. If the admin handlers already have a shared base struct, add the fields there.

The layout template (`gateway/internal/admin/templates/layout.html` or base template) adds a footer **only inside the authenticated layout**, not on the login page. Check how the login page is rendered — if it uses a different template or a stripped layout, the footer naturally won't appear. If it uses the same layout, add a `{{if .IsAuthenticated}}` guard.

Footer HTML (DaisyUI consistent):

```html
<footer class="footer footer-center p-2 bg-base-200 text-base-content text-xs opacity-60">
  <p>nebu gateway v{{.BuildVersion}} · {{.GitCommit}} · built {{.BuildTime}}</p>
</footer>
```

Place the footer at the end of `<body>`, after the main content div, before `</body>`.

### Playwright+Cucumber — E2E test for footer (AC9 + AC10)

**Feature file:** `e2e/features/build-info-footer.feature`

```gherkin
Feature: Build info footer in Admin UI

  Scenario: Build info footer is visible on authenticated admin pages
    Given I am logged in as an admin user
    When I navigate to the admin dashboard
    Then I see a footer containing "nebu gateway v"

  Scenario: Build info footer is absent on the login page
    Given I am not logged in
    When I navigate to the admin login page
    Then I do not see a footer containing "nebu gateway v"
```

**Step file:** `e2e/steps/build-info-footer.steps.ts`

Steps reuse existing login helpers from the suite (`loginAsAdmin`, `navigateTo`).

### Existing files to update (summary)

| File | Change |
|---|---|
| `gateway/cmd/gateway/main.go` | Add `var buildVersion, gitCommit, buildTime = "unknown", "unknown", "unknown"` + `pubMux.HandleFunc("GET /info", ...)` + pass vars to admin handlers |
| `gateway/Dockerfile` | Add 3 `ARG` + update `go build` with `-ldflags` |
| `gateway/internal/admin/` | Add `BuildVersion`/`GitCommit`/`BuildTime` to base template data struct; thread into all authenticated handlers |
| `gateway/internal/admin/templates/` (layout) | Add footer HTML (DaisyUI) to authenticated layout |
| `core/apps/event_dispatcher/lib/nebu/health/server.ex` | Add `GET /info` case in `handle_connection/1` |
| `core/Dockerfile` | Add 3 `ARG` + `ENV` in both builder and runtime stages |
| `docker-compose.yml` | Expand `gateway.build` and `core.build` from shorthand to map form + add `args` |

### New files to create (summary)

| File | Purpose |
|---|---|
| `gateway/internal/health/info.go` | `NewInfoHandler` — static JSON response, no state |
| `gateway/internal/health/info_test.go` | 3 unit tests (AC1, AC3, AC4) |
| `core/apps/event_dispatcher/lib/nebu/health/build_info.ex` | `Nebu.BuildInfo.get/0` — reads ENV/app config |
| `core/apps/event_dispatcher/test/nebu/health/info_test.exs` | 2 ExUnit tests (AC2, AC3-core) |
| `e2e/features/build-info-footer.feature` | Gherkin: footer visible after login, absent on login page |
| `e2e/steps/build-info-footer.steps.ts` | Cucumber step definitions for footer scenarios |

---

## Dev Agent Record

### Implementation Plan

Used a package-level `SetBuildInfo(version, commit, btime)` function in `gateway/internal/admin/page_data.go` to avoid modifying every handler constructor signature. A `newPageData()` helper returns a `PageData` pre-populated with the global build info values. All authenticated admin handlers were updated to use `newPageData()` instead of bare `PageData{}` literals.

The ExUnit test's setup block was updated to terminate the health server via `Supervisor.terminate_child` (not just `GenServer.stop`) to prevent the OTP supervisor from race-restarting it between the stop and the test's own `start_link`.

### Completion Notes

- `gateway/internal/health/info.go` — `NewInfoHandler` creates a static JSON response at construction time (zero allocations per request)
- `gateway/cmd/gateway/main.go` — 3 ldflags vars + `pubMux.HandleFunc("GET /info", ...)` + `admin.SetBuildInfo(...)` call
- `gateway/internal/admin/page_data.go` — `BuildVersion`/`GitCommit`/`BuildTime` fields on `PageData`; `SetBuildInfo` + `newPageData()` helpers
- `gateway/internal/admin/templates/layouts/base.html` — DaisyUI footer, guarded by `{{ if not .LoginMode }}{{ if not .ErrorMode }}`
- All 9 authenticated admin handler render calls updated to use `newPageData()` base
- `core/apps/event_dispatcher/lib/nebu/health/build_info.ex` — `Nebu.BuildInfo.get/0`
- `core/apps/event_dispatcher/lib/nebu/health/server.ex` — `GET /info` clause added before the 404 catch-all
- `gateway/Dockerfile` — 3 `ARG` + `-ldflags` in `go build`
- `core/Dockerfile` — 3 `ARG` + `ENV` in both builder and runtime stages
- `docker-compose.yml` — expanded gateway and core `build:` shorthands to map form + `args:` blocks
- `make test-unit-go`: 16 packages, all pass (including 3 new health/info tests)
- `make test-unit-elixir`: 225 tests, 0 failures (including 2 new info tests)

### File List

- `gateway/internal/health/info.go` (new)
- `gateway/cmd/gateway/main.go` (modified)
- `gateway/internal/admin/page_data.go` (modified)
- `gateway/internal/admin/dashboard.go` (modified)
- `gateway/internal/admin/users.go` (modified)
- `gateway/internal/admin/rooms.go` (modified)
- `gateway/internal/admin/config.go` (modified)
- `gateway/internal/admin/role_mapping.go` (modified)
- `gateway/internal/admin/audit_log_handler.go` (modified)
- `gateway/internal/admin/compliance_handler.go` (modified)
- `gateway/internal/admin/bootstrap.go` (modified)
- `gateway/internal/admin/auth.go` (modified)
- `gateway/internal/admin/templates/layouts/base.html` (modified)
- `gateway/Dockerfile` (modified)
- `core/apps/event_dispatcher/lib/nebu/health/build_info.ex` (new)
- `core/apps/event_dispatcher/lib/nebu/health/server.ex` (modified)
- `core/apps/event_dispatcher/test/nebu/health/info_test.exs` (modified — teardown fix)
- `core/Dockerfile` (modified)
- `docker-compose.yml` (modified)

### Change Log

- 2026-05-11: Story 11-9 implemented — `GET /info` on gateway (port 8080) and core (port 4000), Admin UI footer, Dockerfile + docker-compose.yml build arg injection
- 2026-05-11: Review cycle 2 — m10: added `ErrorMode bool` to `PageData`, error handlers now set `ErrorMode: true` (no longer overload `LoginMode`), footer guard updated to also check `ErrorMode`; m11: added explanatory comment above error handlers explaining why `newPageData()` is not used

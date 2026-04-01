# Story 3.13: Dashboard Page (SSR Metrics Skeleton)

Status: done

## Story

As an operator,
I want a Dashboard page that shows system health at a glance,
so that I can immediately see whether Nebu is functioning correctly during Epic 4 chat tests.

## Acceptance Criteria

1. `GET /admin/dashboard` is served only to authenticated sessions (via `SessionGuard`) — unauthenticated requests redirect `302` to `/admin/login`
2. The page renders within the base layout with `ActiveNav: "dashboard"`
3. SSR section — Status Cards (C1): three `StatusCard` components rendered server-side:
   - **Gateway** — always GREEN (if this page loads, the gateway is up)
   - **Core (gRPC)** — status from `connectivity.State` at request time: `Ready` → GREEN, `Idle`/`Connecting` → AMBER, `TransientFailure`/`Shutdown` → RED
   - **Database** — status from `db.CheckDB(dbURL)` ping at request time: success → GREEN, error → RED
4. SSR section — Server Info: instance name (from `server_config` table), server uptime (parsed from `/proc/uptime` or `time.Since(startTime)`), Go runtime version via `runtime.Version()`
5. SSE Live Metrics section: an empty `<div id="live-metrics">` placeholder; renders a static "Connecting…" skeleton while Vue.js (Story 3.14) is not yet loaded — no JS at this story
6. The `TopbarStatusIndicator` (C3) in the base layout is updated **server-side** at render time: all GREEN → green dot text "OK", any AMBER → amber dot text "Degraded", any RED → red dot text "Down"
7. If `db.CheckDB` fails, the Database card shows RED; no page-level error — the page still renders fully
8. If the gRPC state is not `Ready`, the Core card shows AMBER or RED accordingly; no page-level error
9. A unit test `TestDashboardHandler_AllHealthy` mocks HealthCheck=OK and DB ping=OK and asserts all three cards show the `"green"` CSS class
10. A unit test `TestDashboardHandler_DBDown` mocks DB ping failure and asserts the Database card shows the `"red"` CSS class and the page still renders `200 OK`
11. A unit test `TestDashboardHandler_CoreDegraded` mocks gRPC state as `Idle` and asserts the Core card shows the `"amber"` CSS class

## Tasks / Subtasks

- [x] Task 1: Extend `PageData` in `gateway/internal/admin/page_data.go` with dashboard fields (AC: 3, 4, 6)
  - [x] 1.1 Add `DashboardPageData` struct embedding `PageData` with fields:
    - `GatewayStatus string` — one of `"green"`, `"amber"`, `"red"`
    - `CoreStatus string` — one of `"green"`, `"amber"`, `"red"`
    - `DBStatus string` — one of `"green"`, `"amber"`, `"red"`
    - `WorstStatus string` — worst of the three (used for TopbarStatusIndicator)
    - `InstanceName string`
    - `Uptime string` — formatted string (e.g. `"3d 4h 12m"`)
    - `GoVersion string` — value of `runtime.Version()`
  - [x] 1.2 Add `GatewayStatusLabel string`, `CoreStatusLabel string`, `DBStatusLabel string` fields to `DashboardPageData` for human-readable status text shown inside each card

- [x] Task 2: Create `gateway/internal/admin/dashboard.go` with handler and helpers (AC: 3, 4, 5, 6, 7, 8)
  - [x] 2.1 Define `CoreStateReader` interface (minimal, consumer-defined): `State() connectivity.State`
  - [x] 2.2 Define `DBPinger` interface: `Ping(ctx context.Context) error`
  - [x] 2.3 Define `ServerNameReader` interface: `ServerName(ctx context.Context) (string, error)`
  - [x] 2.4 Implement `postgresServerNameReader` using `*sql.DB`
  - [x] 2.5 Implement `DashboardHandler` struct with fields: `tmpl *TemplateHandler`, `core CoreStateReader`, `dbPinger DBPinger`, `nameReader ServerNameReader`, `startTime time.Time`
  - [x] 2.6 Implement `NewDashboardHandler(tmpl, core, db *sql.DB, dbURL string) *DashboardHandler` constructor
  - [x] 2.7 Implement `Handler(w http.ResponseWriter, r *http.Request)` method:
    - Check `core.State()` → set `CoreStatus`
    - Call `dbPinger.Ping(r.Context())` → set `DBStatus`
    - Set `GatewayStatus = "green"` (always)
    - Compute `WorstStatus` = worst of all three
    - Query `InstanceName` via `nameReader.ServerName(r.Context())`; on error use `"(unknown)"`
    - Compute `Uptime` from `time.Since(startTime)`
    - Set `GoVersion = runtime.Version()`
    - Set status labels: `"OK"` for green, `"Degraded"` for amber, `"Unreachable"` for red
    - Render `h.tmpl.render(w, "dashboard", DashboardPageData{...})`
  - [x] 2.8 Add uptime formatting helper `formatUptime(d time.Duration) string` → e.g. `"3d 4h 12m"`

- [x] Task 3: Create `gateway/internal/admin/templates/dashboard.html` (AC: 2, 3, 4, 5, 6)
  - [x] 3.1 Use exact template structure pattern (see Dev Notes):
    ```
    {{ template "base" . }}
    {{ define "title" }}Dashboard — Nebu Admin{{ end }}
    {{ define "content" }}...{{ end }}
    {{ define "scripts" }}{{ end }}
    ```
  - [x] 3.2 Status Cards section: three DaisyUI `card` elements, each with:
    - A colored top border: `border-t-4 border-success` (green), `border-warning` (amber), `border-error` (red)
    - A status dot indicator using CSS class matching `.StatusClass` field
    - Render using `{{ .GatewayStatus }}` etc. to set the correct class (see Dev Notes for CSS mapping)
    - ARIA: `role="status"` + `aria-live="polite"` on each card
    - Card container class: `status-card status-card--{{ .GatewayStatus }}` (enables unit test assertions)
  - [x] 3.3 Server Info section: instance name, uptime, Go version in a `dl` (description list) or table
  - [x] 3.4 SSE Live Metrics section: `<div id="live-metrics" aria-label="Live metrics" aria-live="polite">` with static "Connecting…" skeleton using `animate-pulse` (respects `prefers-reduced-motion: reduce`)
  - [x] 3.5 Update the topbar `#topbar-status` using template-rendered data from `WorstStatus` — use `{{ block "topbar-override" . }}` injection or pass data through PageData embed (see Dev Notes for approach)

- [x] Task 4: Register dashboard route in `gateway/cmd/gateway/main.go` (AC: 1)
  - [x] 4.1 Import `database/sql` (already imported), `google.golang.org/grpc/connectivity` (not needed — use interface)
  - [x] 4.2 Open a dedicated `*sql.DB` for the dashboard handler (or reuse `bootstrapDB` if acceptable — see Dev Notes)
  - [x] 4.3 Create `dashboardHandler := admin.NewDashboardHandler(tmplHandler, coreClient, db, cfg.DBURL)`
  - [x] 4.4 Register: `mux.Handle("GET /admin/dashboard", sessionGuard(http.HandlerFunc(dashboardHandler.Handler)))`
  - [x] 4.5 Place this registration BEFORE the catch-all `"GET /admin/"` handler

- [x] Task 5: Write unit tests in `gateway/internal/admin/dashboard_test.go` (AC: 9, 10, 11)
  - [x] 5.1 `TestDashboardHandler_AllHealthy`: mock Core=Ready, DB=nil error → assert 200, body contains `"status-card--green"` three times
  - [x] 5.2 `TestDashboardHandler_DBDown`: mock Core=Ready, DB returns error → assert 200, body contains `"status-card--red"` (DB card), `"status-card--green"` (Gateway card)
  - [x] 5.3 `TestDashboardHandler_CoreDegraded`: mock Core=Idle → assert 200, body contains `"status-card--amber"` (Core card)
  - [x] 5.4 `TestDashboardHandler_CoreDown`: mock Core=TransientFailure → assert 200, body contains `"status-card--red"` (Core card)
  - [x] 5.5 `TestDashboardHandler_ActiveNav`: assert response body contains `aria-current="page"` in the dashboard nav link
  - [x] 5.6 `TestDashboardHandler_ContentType`: assert `Content-Type: text/html; charset=utf-8`

- [x] Task 6: Run `make test-unit-go` and confirm zero regressions

## Dev Notes

### Critical: Template Discovery — `pageTmpls` Key is Base Filename

`TemplateHandler.render()` keys templates by `path.Base(file)` with extension stripped. So:
- `templates/dashboard.html` → key `"dashboard"`
- Call: `h.tmpl.render(w, "dashboard", data)` (NOT `"templates/dashboard"`)
- `NewTemplateHandler` already recursively walks `templates/` — **no changes to `handler.go` needed**

### Critical: Template `{{ define "scripts" }}{{ end }}` Is Mandatory

The base layout uses `{{ block "scripts" . }}{{ end }}`. Dashboard.html MUST define this block even though it has no scripts in Story 3.13 (Vue.js SSE widget is Story 3.14). Omitting it causes template execution errors:
```html
{{ define "scripts" }}{{ end }}
```

### Dashboard Template Pattern

Follow exactly the same pattern as `login.html` and `bootstrap.html`:
```html
{{ template "base" . }}
{{ define "title" }}Dashboard — Nebu Admin{{ end }}
{{ define "content" }}
<!-- dashboard HTML here -->
{{ end }}
{{ define "scripts" }}{{ end }}
```

### TopbarStatusIndicator (C3) — Server-Side Approach for Story 3.13

The base layout (`templates/layouts/base.html`) has a hardcoded topbar status indicator:
```html
<span id="topbar-status" aria-live="polite" aria-label="System status: connecting"
      class="flex items-center gap-1 text-sm text-warning">
  <span class="w-2 h-2 rounded-full bg-warning inline-block" aria-hidden="true"></span>
  Connecting&hellip;
</span>
```

For Story 3.13, update the `DashboardPageData` to include `WorstStatus`. **Do NOT modify `base.html`** — instead override the topbar indicator by passing status to the template data. Since `base.html` is shared across all pages and currently hardcodes the "Connecting..." state, the dashboard template should use a `{{ define "topbar-scripts" }}` block or simply accept the current "Connecting..." state for non-dashboard pages.

**PREFERRED APPROACH**: Add a new block `{{ block "topbar-status-override" . }}{{ end }}` to `base.html` just before the existing topbar status span, so the dashboard template can override it. BUT this would require modifying `base.html` — an existing file.

**SIMPLEST APPROACH (recommended for Story 3.13)**: Update `base.html` to use `{{ .WorstStatus }}` if present, with a fallback. Pass `WorstStatus` through the embedded `PageData` or separately. Since `PageData` is used across all pages, add `TopbarStatus string` and `TopbarLabel string` to `PageData` itself. Default values (empty string) render "Connecting..." via conditional. Dashboard handler sets these fields.

Specifically: add `TopbarStatus string` and `TopbarLabel string` to `PageData`. Update `base.html` to conditionally render:
```html
{{ if .TopbarStatus }}
<span class="... text-{{ .TopbarStatus }}">
  <span class="w-2 h-2 rounded-full bg-{{ .TopbarStatus }} inline-block" aria-hidden="true"></span>
  {{ .TopbarLabel }}
</span>
{{ else }}
<!-- existing "Connecting..." span -->
{{ end }}
```

All existing page renders use `PageData{}` (zero values) → TopbarStatus is `""` → `{{ if .TopbarStatus }}` is false → existing "Connecting..." text renders. Dashboard sets TopbarStatus to `"success"`, `"warning"`, or `"error"` (DaisyUI semantic color names).

### StatusCard CSS Pattern (UX-DR4)

Each StatusCard must have:
- Container class `status-card status-card--{{ .GatewayStatus }}` (values: `"green"`, `"amber"`, `"red"`)
- Colored top border: map `"green"` → `border-success`, `"amber"` → `border-warning`, `"red"` → `border-error`
- Status dot: map `"green"` → `bg-success`, `"amber"` → `bg-warning`, `"red"` → `bg-error`
- ARIA: `role="status"` and `aria-live="polite"` (UX-DR4 requirement)

Since Go templates cannot do conditional CSS class lookup inline, use the Go handler to precompute a CSS-ready `*BorderClass`, `*DotClass` field, or use a template `{{ if eq .CoreStatus "green" }}border-success{{ else if eq .CoreStatus "amber" }}border-warning{{ else }}border-error{{ end }}` approach inline in the template.

Example StatusCard HTML:
```html
<div role="status" aria-live="polite"
     class="card bg-base-200 shadow-sm border-t-4 status-card status-card--{{ .GatewayStatus }}
            {{ if eq .GatewayStatus "green" }}border-success
            {{ else if eq .GatewayStatus "amber" }}border-warning
            {{ else }}border-error{{ end }}">
  <div class="card-body py-4">
    <div class="flex items-center gap-2">
      <span class="w-3 h-3 rounded-full inline-block
                   {{ if eq .GatewayStatus "green" }}bg-success
                   {{ else if eq .GatewayStatus "amber" }}bg-warning
                   {{ else }}bg-error{{ end }}"
            aria-hidden="true"></span>
      <h2 class="card-title text-base">Gateway</h2>
    </div>
    <p class="text-sm text-base-content/70">{{ .GatewayStatusLabel }}</p>
  </div>
</div>
```

### gRPC Status Mapping

Use `connectivity.State` from `google.golang.org/grpc/connectivity` — same as the health handler pattern in `gateway/internal/health/health.go`:
- `connectivity.Ready` → `"green"` + label `"OK"`
- `connectivity.Idle`, `connectivity.Connecting` → `"amber"` + label `"Degraded"`
- `connectivity.TransientFailure`, `connectivity.Shutdown` → `"red"` + label `"Unreachable"`

The `coreClient` in `main.go` already implements `State() connectivity.State` (see `gateway/internal/grpc/client.go`). Define a minimal `CoreStateReader` interface in `dashboard.go`:
```go
type CoreStateReader interface {
    State() connectivity.State
}
```
`*coregrpc.Client` satisfies this without needing to import the grpc package in the admin package.

### DB Ping Strategy

Do NOT call `db.CheckDB(dbURL)` (which opens a NEW connection each time) — this would cause a connection per dashboard load. Instead, inject a `*sql.DB` connection pool and call `db.PingContext(ctx)`:

```go
type DBPinger interface {
    PingContext(ctx context.Context) error
}
```

The `*sql.DB` type satisfies `DBPinger` directly. In `main.go`, reuse `bootstrapDB` for the dashboard handler OR open a dedicated pool. Reusing `bootstrapDB` is acceptable for MVP.

### ServerName Reading from `server_config`

The dashboard needs to read `server_name` from `server_config` table. Implement a simple reader:
```go
type ServerNameReader interface {
    ServerName(ctx context.Context) (string, error)
}

type postgresServerNameReader struct {
    db *sql.DB
}

func (r *postgresServerNameReader) ServerName(ctx context.Context) (string, error) {
    var name string
    err := r.db.QueryRowContext(ctx, "SELECT value FROM server_config WHERE key = 'server_name'").Scan(&name)
    if errors.Is(err, sql.ErrNoRows) {
        return "(not configured)", nil
    }
    return name, err
}
```

### Uptime Source: `time.Since(startTime)` (NOT `/proc/uptime`)

The epic spec mentions `/proc/uptime` but this is non-portable (Linux only, not available in Mac dev environments, not reliable in containers). Use `time.Since(startTime)` where `startTime` is stored when `NewDashboardHandler` is called (at gateway startup). This is more accurate and portable.

Format as: `"3d 4h 12m"` (days/hours/minutes — skip units with zero value, minimum `"<1m"`).

### Worst Status Computation

```go
func worstStatus(statuses ...string) string {
    for _, s := range statuses {
        if s == "red" { return "red" }
    }
    for _, s := range statuses {
        if s == "amber" { return "amber" }
    }
    return "green"
}
```

For `TopbarStatus` mapping: `"green"` → DaisyUI `"success"`, `"amber"` → `"warning"`, `"red"` → `"error"`.

### Route Registration in `main.go`

The comment in `main.go` line 141 already anticipates this story:
```go
// Story 3.13 will add: mux.Handle("GET /admin/dashboard", sessionGuard(http.HandlerFunc(dashboardHandler.Handler)))
```

Replace that comment with the actual route registration. The `sessionGuard` variable is already in scope. The catch-all `"GET /admin/"` is already registered — add the dashboard route BEFORE it (Go 1.22+ mux selects most-specific route first regardless of order, but convention is to register specific before catch-all).

The `coreClient` is already declared in `main.go`. `bootstrapDB` is already declared. Pass both to `NewDashboardHandler`.

### Test Pattern for Dashboard Tests

Use the same `package admin` (NOT `package admin_test`) pattern. Create mock structs:

```go
// fakeCoreStateReader implements CoreStateReader for testing
type fakeCoreStateReader struct {
    state connectivity.State
}
func (f *fakeCoreStateReader) State() connectivity.State { return f.state }

// fakeDBPinger implements DBPinger for testing
type fakeDBPinger struct {
    err error
}
func (f *fakeDBPinger) PingContext(_ context.Context) error { return f.err }

// fakeServerNameReader implements ServerNameReader for testing
type fakeServerNameReader struct {
    name string
    err  error
}
func (f *fakeServerNameReader) ServerName(_ context.Context) (string, error) {
    return f.name, f.err
}
```

Constructor for test handler:
```go
func newTestDashboardHandler(t *testing.T, coreState connectivity.State, dbErr error) *DashboardHandler {
    t.Helper()
    tmpl, err := NewTemplateHandler()
    if err != nil {
        t.Fatalf("NewTemplateHandler: %v", err)
    }
    return &DashboardHandler{
        tmpl:       tmpl,
        core:       &fakeCoreStateReader{state: coreState},
        dbPinger:   &fakeDBPinger{err: dbErr},
        nameReader: &fakeServerNameReader{name: "test-instance"},
        startTime:  time.Now(),
    }
}
```

Key assertion for status card CSS class:
```go
if !strings.Contains(body, `status-card--green`) {
    t.Error("expected green status card in body")
}
```

### File Structure

| File | Action |
|------|--------|
| `gateway/internal/admin/page_data.go` | MODIFY — add `DashboardPageData`, add `TopbarStatus`/`TopbarLabel` to `PageData` |
| `gateway/internal/admin/dashboard.go` | CREATE — `DashboardHandler`, interfaces, helpers |
| `gateway/internal/admin/dashboard_test.go` | CREATE — 6 unit tests |
| `gateway/internal/admin/templates/dashboard.html` | CREATE — dashboard template |
| `gateway/internal/admin/templates/layouts/base.html` | MODIFY — add `TopbarStatus`/`TopbarLabel` conditional rendering |
| `gateway/cmd/gateway/main.go` | MODIFY — register `GET /admin/dashboard` route |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — no changes needed, WalkDir already discovers `dashboard.html`
- `gateway/internal/admin/middleware.go` — `SessionGuard` is already complete
- `gateway/internal/admin/errors.go` — no changes needed
- Any existing test files (add new file only)

### Anti-Patterns to Avoid

- **DO NOT** call `db.CheckDB(dbURL)` in the dashboard handler — it opens a new connection each time. Use `db.PingContext(ctx)` on an injected `*sql.DB`.
- **DO NOT** use `os.ReadFile("/proc/uptime")` — non-portable. Use `time.Since(startTime)`.
- **DO NOT** put business logic (status mapping, uptime formatting) in the template — compute in the Go handler and pass precomputed values to the template.
- **DO NOT** omit `{{ define "scripts" }}{{ end }}` in `dashboard.html` — causes template execution errors.
- **DO NOT** use `h.render(w, "templates/dashboard", data)` — the key is `"dashboard"` (basename without extension).
- **DO NOT** hardcode `"success"/"warning"/"error"` DaisyUI color classes directly in the `CoreStatus`/`GatewayStatus` fields — keep internal status as `"green"/"amber"/"red"` and map to DaisyUI in the template via `{{ if eq ... }}` blocks. This keeps the Go code decoupled from CSS framework choices.
- **DO NOT** modify `handler.go` — `NewTemplateHandler()` already handles recursive discovery.
- **DO NOT** add `ContentType` header manually before calling `h.render()` — `render()` already sets `Content-Type: text/html; charset=utf-8`.

### Relationship to Adjacent Stories

- **Story 3.12 (done)**: `Error500` helper is available for unexpected panics; use it in dashboard handler if template render itself panics (the `render()` method already handles this internally).
- **Story 3.14 (next)**: The `<div id="live-metrics">` placeholder in dashboard.html will be hydrated by Vue.js + SSE. Ensure the placeholder div exists with the exact id `"live-metrics"` and appropriate ARIA attributes.
- **Story 3.15 (Gherkin)**: Test scenario "Dashboard accessible after authentication" asserts body contains "Dashboard" and a StatusCard for "Gateway" with class `"green"`. Ensure the heading "Dashboard" appears in the page content and the Gateway card has the `status-card--green` class.
- **Story 5.4 (future)**: Will add a compliance pending-count badge to the sidebar. The `PageData.BootstrapMode` pattern shows how to add conditional sidebar elements via `PageData` fields.

### Previous Story Learnings (from Story 3.12)

- `w.Header().Set("Content-Type", ...)` MUST be called BEFORE `w.WriteHeader()`. However, `h.render()` already handles this correctly — do not call `WriteHeader` manually in the dashboard handler; let `render()` manage headers.
- All tests in `admin` package use `package admin` (NOT `package admin_test`) — access to unexported types is standard.
- `NewTemplateHandler()` is the standard test setup call — no mocking needed for template rendering tests.
- The `pageTmpls` map key is `path.Base(file)` with extension stripped — always `"dashboard"`, never a path.

### Git Context (Recent Commits)

Story 3.12 added:
- `gateway/internal/admin/templates/errors/401.html`, `403.html`, `404.html`, `500.html`
- `gateway/internal/admin/errors.go` (Error401–Error500 helpers)
- `gateway/internal/admin/errors_test.go`
- `gateway/cmd/gateway/main.go`: catch-all 404 handler `"GET /admin/"`

This story adds:
- New `dashboard.go` + `dashboard_test.go` + `templates/dashboard.html`
- Modifies `page_data.go` (add DashboardPageData + TopbarStatus/TopbarLabel to PageData)
- Modifies `base.html` (TopbarStatusIndicator conditional)
- Modifies `main.go` (one new route registration, remove the comment placeholder)

### Architecture Constraints

Per `architecture.md` rule: "Admin UI ausschließlich via `embed.FS` ausliefern — kein Filesystem-Zugriff zur Laufzeit." The `dashboard.html` template is covered by the existing `//go:embed templates` directive in `handler.go`. No additional embed directives needed.

Per ADR-009 (OpenAPI Spec-First): This is an SSR page, NOT an API endpoint — no OpenAPI spec changes needed.

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None.

### Completion Notes List

- Task 1: Added `TopbarStatus` and `TopbarLabel` to `PageData`; created `DashboardPageData` struct with all required fields (status, labels, server info). Zero-value `TopbarStatus` causes base.html to render the default "Connecting..." state for all non-dashboard pages.
- Task 2: Created `dashboard.go` with `CoreStateReader`, `DBPinger`, `ServerNameReader` consumer-defined interfaces; `postgresServerNameReader` implementation; `DashboardHandler` with `NewDashboardHandler` constructor; `Handler()` method computing all statuses at request time; helpers `mapCoreState`, `worstStatus`, `mapWorstToTopbar`, `formatUptime`. Constructor signature is `NewDashboardHandler(tmpl, core, db *sql.DB)` — `dbURL` parameter omitted per Dev Notes guidance to use `PingContext` on injected pool.
- Task 3: Created `dashboard.html` with full base layout pattern, three status cards with `status-card--{{ .Status }}` classes and inline DaisyUI color conditionals, Server Info `dl`, `<div id="live-metrics">` SSE placeholder with `animate-pulse` skeleton, mandatory `{{ define "scripts" }}{{ end }}` block. Updated `base.html` topbar indicator with `{{ if .TopbarStatus }}` conditional.
- Task 4: Registered `GET /admin/dashboard` before catch-all `GET /admin/` in `main.go`. Reuses `bootstrapDB` as per Dev Notes recommendation. `coreClient` satisfies `CoreStateReader` interface via its `State() connectivity.State` method.
- Task 5: Created 6 handler tests + 3 helper unit tests (formatUptime, worstStatus, mapCoreState) in `dashboard_test.go`. All tests use `package admin` pattern with fake implementations.
- Task 6: `make test-unit-go` — all packages pass, zero regressions.

### File List

- `gateway/internal/admin/page_data.go` — MODIFIED (added `TopbarStatus`/`TopbarLabel` to `PageData`; added `DashboardPageData`)
- `gateway/internal/admin/dashboard.go` — CREATED (`DashboardHandler`, interfaces, helpers)
- `gateway/internal/admin/dashboard_test.go` — CREATED (9 unit tests)
- `gateway/internal/admin/templates/dashboard.html` — CREATED (dashboard SSR template)
- `gateway/internal/admin/templates/layouts/base.html` — MODIFIED (topbar status conditional rendering)
- `gateway/cmd/gateway/main.go` — MODIFIED (registered `GET /admin/dashboard` route)

### Review Findings

- [x] [Review][Patch] MAJOR: Tailwind CSS purge strips `text-success` and `text-error` — dynamic class interpolation `text-{{ .TopbarStatus }}` in base.html prevents Tailwind from recognizing full class names; replaced with `{{ if eq }}` conditionals [gateway/internal/admin/templates/layouts/base.html:15-27] — FIXED

## Change Log

- 2026-04-01: Code review passed — 1 MAJOR patch (Tailwind purge-safe topbar CSS classes) applied, 3 dismissed as noise
- 2026-04-01: Story 3.13 implemented — Dashboard SSR page with system status cards, server info, SSE placeholder; TopbarStatusIndicator wired server-side; 9 unit tests added; all tests pass (Date: 2026-04-01)

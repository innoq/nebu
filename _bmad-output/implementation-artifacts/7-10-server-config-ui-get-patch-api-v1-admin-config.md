---
id: 7-10
security_review: not-needed
---

# Story 7.10: Server Config UI (GET/PATCH /api/v1/admin/config)

Status: ready-for-dev

## Story

As an instance admin,
I want a dedicated server configuration page at `/admin/config` that shows current instance settings and lets me edit them via a save form,
so that I can manage server-wide settings directly from the Admin UI without touching config files.

## Acceptance Criteria

1. **`GET /admin/config` page renders** — `gateway/internal/admin/config.go`: A `ConfigHandler` (struct with template dependency) serves `GET /admin/config`. It renders `config.html` with `ConfigPageData` populated from `stubConfig`. The response is HTTP 200 and the body contains `<main>`, `<h1>Server Configuration</h1>`, and the current `InstanceName` value ("Nebu Dev").

2. **Flash message renders** — `GET /admin/config?flash=Configuration+saved` populates `ConfigPageData.Flash` with `AlertBannerData{Severity: "success", Message: "Configuration saved", Dismissible: true}` and the page body contains "Configuration saved".

3. **`StubConfig` struct and `stubConfig` var added to `stubs.go`** — A new `StubConfig` struct and a package-level `stubConfig` variable are defined in `gateway/internal/admin/stubs.go`:
   ```go
   type StubConfig struct {
       InstanceName        string
       AllowRegistration   bool
       MaxRoomsPerUser     int
       RetentionDays       int
   }
   var stubConfig = StubConfig{
       InstanceName:      "Nebu Dev",
       AllowRegistration: true,
       MaxRoomsPerUser:   10,
       RetentionDays:     90,
   }
   ```

4. **`ConfigPageData` added to `page_data.go`** — A new `ConfigPageData` struct is defined:
   ```go
   type ConfigPageData struct {
       PageData
       Config StubConfig
       Flash  AlertBannerData
   }
   ```

5. **`config.html` template** — `gateway/internal/admin/templates/config.html` renders a settings page with:
   - A `<main>` landmark element (for WCAG)
   - `<h1>Server Configuration</h1>`
   - `{{ if .Flash.Message }}{{ template "alert_banner" .Flash }}{{ end }}`
   - A `<form method="POST" action="/admin/config">` containing:
     - Labeled `<input type="text" name="instance_name">` pre-filled with `{{ .Config.InstanceName }}`
     - Labeled `<input type="checkbox" name="allow_registration"{{ if .Config.AllowRegistration }} checked{{ end }}>` for `AllowRegistration`
     - Labeled `<input type="number" name="max_rooms_per_user">` pre-filled with `{{ .Config.MaxRoomsPerUser }}`
     - Labeled `<input type="number" name="retention_days">` pre-filled with `{{ .Config.RetentionDays }}`
     - Hidden CSRF input: `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`
     - Submit button: `<button type="submit" class="btn btn-primary">Save</button>`

6. **`POST /admin/config` handler** — `gateway/internal/admin/config.go`: A `UpdateConfigHandler` method on `ConfigHandler`:
   - Parses the form body (`r.ParseForm()`)
   - Validates: `instance_name` non-empty after `strings.TrimSpace`; `max_rooms_per_user` between 1 and 100 (inclusive); `retention_days` between 1 and 3650 (inclusive). Returns HTTP 400 on any validation failure.
   - On validation pass: updates `stubConfig` in-memory (`stubConfig.InstanceName`, `stubConfig.AllowRegistration`, `stubConfig.MaxRoomsPerUser`, `stubConfig.RetentionDays`). `AllowRegistration` is true when `r.FormValue("allow_registration") == "on"`.
   - PRG redirect: HTTP 302 to `/admin/config?flash=Configuration+saved`.
   - Add `// TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config)` comment.

7. **Routing** — `gateway/cmd/gateway/main.go`:
   - `GET /admin/config`: `csrf(sessionGuard(http.HandlerFunc(configHandler.Handler)))` (same csrf+sessionGuard pattern as users/rooms GET routes)
   - `POST /admin/config`: `sessionGuard(http.HandlerFunc(configHandler.UpdateConfigHandler))` (no csrf() wrapper on POST — same pattern as POST user/room mutation routes; see TODO comment in handler)
   - Both use a `configHandler` variable of type `*admin.ConfigHandler`, initialised analogously to `usersHandler` and `roomsHandler`.

8. **Go unit tests** — `gateway/internal/admin/config_test.go` (`package admin`):
   - `TestConfigPageRenders` — `GET /admin/config` → HTTP 200, body contains `<h1`, body contains `<main`, body contains "Nebu Dev"
   - `TestConfigPageFlashMessage` — `GET /admin/config?flash=Configuration+saved` → HTTP 200, body contains "Configuration saved"
   - `TestUpdateConfig` — `POST /admin/config` with valid form → HTTP 302, `Location` header contains `/admin/config` and `flash=`
   - `TestUpdateConfigEmptyName` — `POST /admin/config` with `instance_name=` → HTTP 400
   - `TestUpdateConfigInvalidMaxRooms` — `POST /admin/config` with `max_rooms_per_user=0` → HTTP 400

9. **Playwright E2E tests** — `e2e/tests/features/admin/config.spec.ts` — **REAL tests (not `test.skip`)**:
   - `config page renders with current settings` — navigate to `/admin/config`, expect `<h1>` containing "Server Configuration", expect input with name `instance_name` to have value "Nebu Dev"
   - `save configuration shows flash message` — navigate to `/admin/config`, fill `instance_name` with "Test Instance", click Save → `div[role="alert"]` containing "Configuration saved" is visible

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestConfigPageRenders — GET /admin/config returns 200 with landmark and heading** — Go `net/http/httptest` (`gateway/internal/admin/config_test.go`)
   - Given: `ConfigHandler` wired to a test HTTP mux; `stubConfig.InstanceName` is "Nebu Dev"
   - When: `GET /admin/config`
   - Then: HTTP 200, body contains `<h1`, body contains `<main`, body contains "Nebu Dev"

2. **TestConfigPageFlashMessage — flash query param renders AlertBanner** — Go `net/http/httptest`
   - Given: `ConfigHandler` wired to a test HTTP mux
   - When: `GET /admin/config?flash=Configuration+saved`
   - Then: HTTP 200, body contains "Configuration saved"

3. **TestUpdateConfig — valid POST redirects with flash** — Go `net/http/httptest`
   - Given: `ConfigHandler.UpdateConfigHandler` wired; valid form values
   - When: `POST /admin/config` with `instance_name=My+Server&allow_registration=on&max_rooms_per_user=20&retention_days=365`
   - Then: HTTP 302, `Location` header contains `/admin/config` and `flash=`
   - Cleanup: `t.Cleanup` restores `stubConfig` to original values

4. **TestUpdateConfigEmptyName — empty instance_name returns 400** — Go `net/http/httptest`
   - Given: `ConfigHandler.UpdateConfigHandler` wired
   - When: `POST /admin/config` with `instance_name=` (empty)
   - Then: HTTP 400

5. **TestUpdateConfigInvalidMaxRooms — max_rooms_per_user=0 returns 400** — Go `net/http/httptest`
   - Given: `ConfigHandler.UpdateConfigHandler` wired
   - When: `POST /admin/config` with `instance_name=Test&max_rooms_per_user=0&retention_days=90`
   - Then: HTTP 400

6. **Playwright: config page renders with current settings** — Playwright (`e2e/tests/features/admin/config.spec.ts`)
   - Given: full dev stack running, admin logged in
   - When: navigate to `/admin/config`
   - Then: `h1` contains "Server Configuration"; `input[name="instance_name"]` has value "Nebu Dev"

7. **Playwright: save configuration shows flash message** — Playwright
   - Given: full dev stack running, admin logged in, at `/admin/config`
   - When: clear and fill `input[name="instance_name"]` with "Test Instance", click `button[type="submit"]`
   - Then: `div[role="alert"]` containing "Configuration saved" is visible

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 8
  - [ ] 1.1 Create `gateway/internal/admin/config_test.go` in `package admin`
  - [ ] 1.2 Write `TestConfigPageRenders`:
    - Setup: `NewTemplateHandler()` + `NewConfigHandler(tmpl)`; `http.NewServeMux()` wired with `GET /admin/config` → `ConfigHandler.Handler`
    - Request: `GET /admin/config`
    - Assert: `w.Code == 200`, `strings.Contains(body, "<h1")`, `strings.Contains(body, "<main")`, `strings.Contains(body, "Nebu Dev")`
  - [ ] 1.3 Write `TestConfigPageFlashMessage`:
    - Request: `GET /admin/config?flash=Configuration+saved`
    - Assert: `w.Code == 200`, body contains "Configuration saved"
  - [ ] 1.4 Write `TestUpdateConfig`:
    - Setup: `t.Cleanup` to restore `stubConfig` to its original values (save before, restore after)
    - Request: `POST /admin/config` with form body `instance_name=My+Server&allow_registration=on&max_rooms_per_user=20&retention_days=365`; `Content-Type: application/x-www-form-urlencoded`
    - Assert: `w.Code == 302`, `Location` header contains `/admin/config` and `flash=`
  - [ ] 1.5 Write `TestUpdateConfigEmptyName`:
    - Request: `POST /admin/config` with `instance_name=&allow_registration=on&max_rooms_per_user=10&retention_days=90`
    - Assert: `w.Code == 400`
  - [ ] 1.6 Write `TestUpdateConfigInvalidMaxRooms`:
    - Request: `POST /admin/config` with `instance_name=Test&max_rooms_per_user=0&retention_days=90`
    - Assert: `w.Code == 400`
  - [ ] 1.7 Confirm RED: `go test ./internal/admin/...` fails — `ConfigHandler`, `NewConfigHandler`, `ConfigPageData`, `StubConfig`, `stubConfig` do not exist yet

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 9
  - [ ] 2.1 Create `e2e/tests/features/admin/config.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `rooms-page.spec.ts` (identical OIDC Authorization Code + PKCE pattern)
  - [ ] 2.3 Write `test('config page renders with current settings')` — NOT `test.skip`:
    - `loginAsAdmin(page)`, `await page.goto('/admin/config')`
    - `await expect(page.locator('h1')).toContainText('Server Configuration')`
    - `await expect(page.locator('input[name="instance_name"]')).toHaveValue('Nebu Dev')`
  - [ ] 2.4 Write `test('save configuration shows flash message')`:
    - `loginAsAdmin(page)`, `await page.goto('/admin/config')`
    - `await page.locator('input[name="instance_name"]').fill('Test Instance')`
    - `await page.locator('button[type="submit"]').click()`
    - `await expect(page.locator('div[role="alert"]')).toContainText('Configuration saved')`
  - [ ] 2.5 Confirm RED: Playwright tests fail — `/admin/config` route does not exist yet (404)

- [ ] Task 3: Add `StubConfig` and `stubConfig` to `stubs.go` — AC: 3
  - [ ] 3.1 In `gateway/internal/admin/stubs.go`, append after the existing `findStubRoom` function:
    ```go
    // StubConfig holds server-wide configuration settings for the Config page (Story 7.10).
    // Used until Epic 6 (Admin API) provides PATCH /api/v1/admin/config.
    type StubConfig struct {
        InstanceName      string
        AllowRegistration bool
        MaxRoomsPerUser   int
        RetentionDays     int
    }

    // stubConfig is the in-memory server config, mutated by UpdateConfigHandler (Story 7.10).
    // Changes are lost on gateway restart — acceptable for stub phase.
    var stubConfig = StubConfig{
        InstanceName:      "Nebu Dev",
        AllowRegistration: true,
        MaxRoomsPerUser:   10,
        RetentionDays:     90,
    }
    ```

- [ ] Task 4: Add `ConfigPageData` to `page_data.go` — AC: 4
  - [ ] 4.1 In `gateway/internal/admin/page_data.go`, append after the `RoomsPageData` block:
    ```go
    // ConfigPageData holds data for the Server Configuration page (Story 7.10).
    // Embeds PageData for ActiveNav, topbar status, and CSRF token.
    // Config is populated from stubConfig by ConfigHandler.Handler.
    // Flash is populated when ?flash= query param is present (PRG pattern).
    type ConfigPageData struct {
        PageData
        Config StubConfig
        Flash  AlertBannerData
    }
    ```

- [ ] Task 5: Create `config.go` handler — AC: 1, 2, 6
  - [ ] 5.1 Create `gateway/internal/admin/config.go` in `package admin`:
    ```go
    package admin

    import (
        "net/http"
        "strconv"
        "strings"
    )

    // ConfigHandler serves the Server Configuration page (Story 7.10).
    type ConfigHandler struct {
        tmpl *TemplateHandler
    }

    // NewConfigHandler creates a ConfigHandler with the given template handler.
    func NewConfigHandler(tmpl *TemplateHandler) *ConfigHandler {
        return &ConfigHandler{tmpl: tmpl}
    }

    // Handler serves GET /admin/config.
    // Renders config.html with ConfigPageData populated from stubConfig.
    func (h *ConfigHandler) Handler(w http.ResponseWriter, r *http.Request) {
        var flash AlertBannerData
        if msg := r.URL.Query().Get("flash"); msg != "" {
            flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
        }
        data := ConfigPageData{
            PageData: PageData{ActiveNav: "config", CSRFToken: CSRFTokenFromContext(r.Context())},
            Config:   stubConfig,
            Flash:    flash,
        }
        h.tmpl.render(w, "config", data)
    }

    // UpdateConfigHandler handles POST /admin/config.
    // Validates form fields, updates stubConfig in-memory, then PRG-redirects.
    // TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config).
    // TODO(story-7-csrf): enforce CSRF middleware when wiring in production.
    func (h *ConfigHandler) UpdateConfigHandler(w http.ResponseWriter, r *http.Request) {
        if err := r.ParseForm(); err != nil {
            http.Error(w, "bad request", http.StatusBadRequest)
            return
        }

        instanceName := strings.TrimSpace(r.FormValue("instance_name"))
        if instanceName == "" {
            http.Error(w, "instance_name must not be empty", http.StatusBadRequest)
            return
        }

        maxRooms, err := strconv.Atoi(r.FormValue("max_rooms_per_user"))
        if err != nil || maxRooms < 1 || maxRooms > 100 {
            http.Error(w, "max_rooms_per_user must be between 1 and 100", http.StatusBadRequest)
            return
        }

        retentionDays, err := strconv.Atoi(r.FormValue("retention_days"))
        if err != nil || retentionDays < 1 || retentionDays > 3650 {
            http.Error(w, "retention_days must be between 1 and 3650", http.StatusBadRequest)
            return
        }

        stubConfig.InstanceName = instanceName
        stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"
        stubConfig.MaxRoomsPerUser = maxRooms
        stubConfig.RetentionDays = retentionDays

        http.Redirect(w, r, "/admin/config?flash=Configuration+saved", http.StatusFound)
    }
    ```

- [ ] Task 6: Create `config.html` template — AC: 5
  - [ ] 6.1 Create `gateway/internal/admin/templates/config.html`:
    ```html
    {{ template "base" . }}
    {{ define "title" }}Server Configuration — Nebu Admin{{ end }}
    {{ define "content" }}
    <main>
      <div class="max-w-2xl mx-auto">
        <h1 class="text-2xl font-bold mb-6">Server Configuration</h1>

        {{ if .Flash.Message }}
        {{ template "alert_banner" .Flash }}
        {{ end }}

        <form method="POST" action="/admin/config" class="space-y-6">
          <input type="hidden" name="_csrf" value="{{ .CSRFToken }}">

          <div class="form-control">
            <label class="label" for="instance_name">
              <span class="label-text">Instance Name</span>
            </label>
            <input type="text" id="instance_name" name="instance_name"
                   value="{{ .Config.InstanceName }}"
                   class="input input-bordered w-full"
                   required>
          </div>

          <div class="form-control">
            <label class="label cursor-pointer justify-start gap-4" for="allow_registration">
              <input type="checkbox" id="allow_registration" name="allow_registration"
                     class="checkbox"
                     {{ if .Config.AllowRegistration }}checked{{ end }}>
              <span class="label-text">Allow Registration</span>
            </label>
          </div>

          <div class="form-control">
            <label class="label" for="max_rooms_per_user">
              <span class="label-text">Max Rooms per User (1–100)</span>
            </label>
            <input type="number" id="max_rooms_per_user" name="max_rooms_per_user"
                   value="{{ .Config.MaxRoomsPerUser }}"
                   min="1" max="100"
                   class="input input-bordered w-full"
                   required>
          </div>

          <div class="form-control">
            <label class="label" for="retention_days">
              <span class="label-text">Retention Days (1–3650)</span>
            </label>
            <input type="number" id="retention_days" name="retention_days"
                   value="{{ .Config.RetentionDays }}"
                   min="1" max="3650"
                   class="input input-bordered w-full"
                   required>
          </div>

          <div class="form-control pt-2">
            <button type="submit" class="btn btn-primary">Save</button>
          </div>
        </form>
      </div>
    </main>
    {{ end }}
    ```

- [ ] Task 7: Add routes in `main.go` — AC: 7
  - [ ] 7.1 Initialise `configHandler` alongside `usersHandler` and `roomsHandler`:
    ```go
    configHandler := admin.NewConfigHandler(tmpl)
    ```
  - [ ] 7.2 Register routes (after the `POST /admin/rooms/{roomId}/archive` line):
    ```go
    // Story 7.10: Server Configuration page.
    mux.Handle("GET /admin/config", csrf(sessionGuard(http.HandlerFunc(configHandler.Handler))))
    // POST /admin/config — no csrf() wrapper (stub phase; see TODO in handler); sessionGuard still applies.
    mux.Handle("POST /admin/config", sessionGuard(http.HandlerFunc(configHandler.UpdateConfigHandler)))
    ```

- [ ] Task 8: Verify no regressions — AC: all
  - [ ] 8.1 Run `make test-unit-go` — all existing tests pass; 5 new config tests pass
  - [ ] 8.2 Verify `make build-gateway` exits 0
  - [ ] 8.3 Run Playwright: `npx playwright test config.spec.ts` — both tests green against running dev stack

## Dev Notes

### Handler Pattern

`ConfigHandler` follows the same constructor/handler pattern as `UsersHandler` and `RoomsHandler`. The template name passed to `h.tmpl.render` must match the template filename without extension (`"config"` → `config.html`). Verify the exact convention by checking `usersHandler.Handler` in `users.go`.

### `stubConfig` Mutation Isolation in Tests

`stubConfig` is a package-level `var` in `stubs.go`. `TestUpdateConfig` must save and restore it via `t.Cleanup` to avoid test-order dependencies:

```go
original := stubConfig
t.Cleanup(func() { stubConfig = original })
```

This mirrors the `TestUpdateRoomName` / `TestUpdateDisplayName` patterns.

### Checkbox Handling

HTML form checkboxes send `"on"` when checked and omit the field entirely when unchecked. `UpdateConfigHandler` sets `stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"`, which correctly handles both cases.

### Validation Ranges

- `instance_name`: non-empty after `strings.TrimSpace` (no max length for MVP; can add later)
- `max_rooms_per_user`: 1–100 inclusive (`strconv.Atoi` + bounds check)
- `retention_days`: 1–3650 inclusive (10 years max)

`TestUpdateConfigInvalidMaxRooms` tests `max_rooms_per_user=0` (below minimum). The story spec also implicitly covers the missing-or-invalid parse error case via the `strconv.Atoi` failure path.

### Nav Active State

The `PageData.ActiveNav` value `"config"` must correspond to a nav link in `base.html` (or `sidebar.html`). Check whether a "Config" nav item exists; if not, add one alongside the existing "Users" and "Rooms" items. The WCAG story (7.13) will scan this page — ensure the nav item has `aria-current="page"` when active.

### Template `<main>` Landmark

The `<main>` landmark in `config.html` is required for WCAG 2.1 SC 1.3.6 (identify purpose) and will be checked in the WCAG audit story (7.13). Unlike the Users and Rooms pages (which use `master_detail.html` layout), `config.html` is a plain single-panel page and does not use the master-detail layout. The `<main>` element must wrap the form content directly.

### CSRFTokenFromContext

`ConfigHandler.Handler` calls `CSRFTokenFromContext(r.Context())` — the same function used by `DashboardHandler`, `UsersHandler`, and `RoomsHandler`. No additional import needed; it is defined in `middleware.go`.

### Playwright `loginAsAdmin` Helper

Copy the `loginAsAdmin` helper from `rooms-page.spec.ts` or `users-page.spec.ts` verbatim. This uses Authorization Code + PKCE via Dex — no ROPC shortcuts (per project OIDC standard).

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/stubs.go` | Modify — append `StubConfig` struct and `stubConfig` var |
| `gateway/internal/admin/page_data.go` | Modify — append `ConfigPageData` struct |
| `gateway/internal/admin/config.go` | NEW — `ConfigHandler`, `NewConfigHandler`, `Handler`, `UpdateConfigHandler` |
| `gateway/internal/admin/templates/config.html` | NEW — settings form template |
| `gateway/internal/admin/config_test.go` | NEW — 5 Go unit tests |
| `gateway/cmd/gateway/main.go` | Modify — initialise `configHandler`, add `GET` and `POST` routes |
| `e2e/tests/features/admin/config.spec.ts` | NEW — 2 Playwright E2E tests (REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.13** (WCAG audit): axe-core scan will run against `/admin/config`; `<main>` landmark and label associations must be correct.
- **Story 7.14** (Gherkin smoke flows): does not explicitly cover config, but general admin nav smoke will exercise the route.

## Status Notes

Created: 2026-04-29. Stories 7.1 through 7.9 are done. This is a standalone single-panel settings page — no master-detail layout. The page uses the same PRG + flash pattern established in Stories 7.6 (User Detail Panel) and 7.9 (Room Detail Panel). For MVP, all data is in-memory via `stubConfig`; Epic 6 will wire in the real `PATCH /api/v1/admin/config` endpoint. ATDD required: write failing tests in `config_test.go` and `config.spec.ts` before writing any implementation code.

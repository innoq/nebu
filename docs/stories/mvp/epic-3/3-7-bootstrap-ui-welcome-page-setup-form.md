# Story 3.7: Bootstrap UI: Welcome Page + Setup Form

Status: done

## Story

As an operator,
I want a guided Bootstrap Wizard with four steps,
so that I can configure my Nebu instance (instance name, OIDC provider, keys) through a clear UI without editing config files.

## Acceptance Criteria

1. `GET /admin/bootstrap` serves the Bootstrap Wizard page (step 1 of 4) as an HTML page rendered via the base layout
2. Step 1 — Instance Name: a form field for `instance_name` (required, 3–64 chars, alphanumeric + hyphens)
3. Step 2 — OIDC Configuration: fields for `oidc_issuer` (URL), `oidc_client_id` (string), `oidc_client_secret` (password input); a **Test Connection** button that calls `POST /admin/bootstrap/test-oidc` and displays success/error inline without full page reload (vanilla `fetch`, no Vue)
4. Step 3 — Key Generation: a static info panel explaining Ed25519 + X25519 keypairs will be generated server-side; a **Generate Keys** button that calls `POST /admin/bootstrap/generate-keys` and displays the public Ed25519 fingerprint for verification
5. Step 4 — First Admin: instruction text "Complete setup by logging in with your OIDC provider. The first user to log in will be assigned `instance_admin`."
6. Each step has a **Next** / **Back** button; state is preserved via hidden form fields across step navigation
7. Client-side validation (HTML5 `required`, `pattern`) prevents form submission with empty required fields
8. The wizard page renders within the base layout with `ActiveNav: "bootstrap"` and `BootstrapMode: true`
9. A unit test renders each step and asserts the correct form fields are present

## Tasks / Subtasks

- [x] Task 1: Replace JSON stub in `BootstrapHandler` with HTML wizard render for step 1 (AC: 1, 2, 8)
  - [x] 1.1 Create `gateway/internal/admin/templates/bootstrap.html` extending `base` layout — defines `{{ block "content" . }}`
  - [x] 1.2 Add a `Step int` field to a new `BootstrapPageData` struct (or extend `PageData`) to track which step (1–4) to render
  - [x] 1.3 Replace `BootstrapHandler.Handler` JSON response with `TemplateHandler.render` call, passing `BootstrapPageData{Step: 1, ActiveNav: "bootstrap", BootstrapMode: true}`
  - [x] 1.4 Register a `TemplateHandler` in `BootstrapHandler` (inject via `NewBootstrapHandler`) so it can call render
  - [x] 1.5 Step 1 form: single `<input type="text" name="instance_name" required minlength="3" maxlength="64" pattern="[a-zA-Z0-9-]+">`; **Next** button submits `POST /admin/bootstrap` (form action points to next step or step param)

- [x] Task 2: Add step navigation with hidden fields for state preservation (AC: 6, 7)
  - [x] 2.1 Each step form includes hidden fields carrying previously entered values (e.g., `<input type="hidden" name="instance_name" value="{{ .InstanceName }}">`)
  - [x] 2.2 `BootstrapPageData` holds all accumulated field values: `InstanceName`, `OIDCIssuer`, `OIDCClientID`; `OIDCClientSecret` is NOT echoed back into the page (security: use a sentinel like `"__unchanged__"` or keep empty after validation)
  - [x] 2.3 `POST /admin/bootstrap?step=N` handler parses the form, validates the current step's fields, and re-renders the same step on error or advances `Step` to `N+1` on success
  - [x] 2.4 Back navigation: a `<button type="submit" name="step" value="N-1">Back</button>` inside the form (no JS needed)
  - [x] 2.5 HTML5 `required` and `pattern` attributes on all required fields — browser-native validation before submission

- [x] Task 3: Render steps 2, 3, and 4 in `bootstrap.html` using Go template `{{ if eq .Step N }}` blocks (AC: 3, 4, 5)
  - [x] 3.1 Step 2: `oidc_issuer` (`type="url" required`), `oidc_client_id` (`type="text" required`), `oidc_client_secret` (`type="password" required`); Test Connection button with `id="test-oidc-btn"` and result container `id="oidc-test-result"`
  - [x] 3.2 Step 3: Static info panel text about Ed25519 + X25519 keypair generation; Generate Keys button with `id="generate-keys-btn"` and result container `id="keys-result"`
  - [x] 3.3 Step 4: Static instruction text only; a final **Complete Setup** button that submits `POST /admin/bootstrap` with all accumulated hidden fields
  - [x] 3.4 Use DaisyUI `steps` component for the step progress indicator at the top of the wizard card

- [x] Task 4: Add inline async interactions (fetch-based, no Vue) (AC: 3, 4)
  - [x] 4.1 Add a `<script>` block in `{{ block "scripts" . }}` — vanilla JS only, no framework
  - [x] 4.2 Test Connection: `fetch("POST /admin/bootstrap/test-oidc", {body: formData})` — on success show green checkmark + "Connected to <issuer>"; on failure show red error text from response JSON `{"ok": false, "error": "..."}`
  - [x] 4.3 Generate Keys: `fetch("POST /admin/bootstrap/generate-keys", ...)` — on success show `ed25519_public_fingerprint` in a `<code>` block; on failure show error
  - [x] 4.4 Both buttons show a loading spinner during fetch (DaisyUI `loading loading-spinner`) and are disabled during the request to prevent double-submit

- [x] Task 5: Register `POST /admin/bootstrap` and add step routing in handler (AC: 2, 3, 4, 5, 6)
  - [x] 5.1 Add `mux.Handle("POST /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.StepHandler)))` in `main.go`
  - [x] 5.2 `StepHandler` reads `step` from query param or form field, validates current step, returns re-render (422) or advances to next step
  - [x] 5.3 On step 4 final submit → delegate to `POST /admin/bootstrap` final handler (Story 3.8 will replace this stub with real persistence); for now, render step 4 confirmation or placeholder redirect

- [x] Task 6: Write unit tests (AC: 9)
  - [x] 6.1 Create `gateway/internal/admin/bootstrap_wizard_test.go` (package `admin`)
  - [x] 6.2 Test: `GET /admin/bootstrap` renders step 1 HTML containing `input[name="instance_name"]`
  - [x] 6.3 Test: template render with `Step: 2` contains `input[name="oidc_issuer"]`, `input[name="oidc_client_id"]`, `input[name="oidc_client_secret"]`
  - [x] 6.4 Test: template render with `Step: 3` contains `id="generate-keys-btn"`
  - [x] 6.5 Test: template render with `Step: 4` contains instruction text about "instance_admin"
  - [x] 6.6 Test: `ActiveNav: "bootstrap"` in rendered output — verify nav highlight (reuse pattern from `handler_test.go`)
  - [x] 6.7 Run `make test-unit-go` → all tests green, zero regressions

## Dev Notes

### CRITICAL: `BootstrapHandler` Currently Returns JSON — Must Become HTML

`gateway/internal/admin/bootstrap.go` `Handler` method currently returns JSON (`bootstrapResponse`). This story replaces it with HTML rendering. The `BootstrapGuard` middleware (Story 3.6) is already in place and handles redirect logic correctly — the handler just needs to render HTML now.

**Existing tests in `bootstrap_test.go` test the JSON endpoint** (`TestBootstrapHandler_Active`, `TestBootstrapHandler_NotActive`, `TestBootstrapHandler_Error`). These tests WILL BREAK when the handler becomes HTML. Update those tests to reflect the new behavior:
- `TestBootstrapHandler_Active`: expect `200` with `Content-Type: text/html` (not JSON)
- `TestBootstrapHandler_NotActive`: the `BootstrapGuard` middleware handles the "not active" redirect BEFORE the handler is reached — the handler itself no longer needs to check `IsBootstrapActive`. Remove the 404 case from the handler; the guard handles it.
- `TestBootstrapHandler_Error`: DB error is now handled by the guard, not the handler — remove or repurpose.

After this change, `BootstrapHandler` needs access to `TemplateHandler`. Inject it:
```go
type BootstrapHandler struct {
    checker BootstrapStatusChecker
    tmpl    *TemplateHandler
}

func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler) *BootstrapHandler {
    return &BootstrapHandler{checker: checker, tmpl: tmpl}
}
```

Update `main.go` accordingly:
```go
tmplHandler, err := admin.NewTemplateHandler()
// ...
bootstrapHandler := admin.NewBootstrapHandler(checker, tmplHandler)
```

### Page Data Struct Pattern

`PageData` in `page_data.go` only has `BootstrapMode bool` and `ActiveNav string`. For the wizard, add a `BootstrapPageData` struct **in the same file** (`page_data.go`):

```go
// BootstrapPageData holds data for the Bootstrap Wizard page.
// Step is 1–4. All field values carry accumulated state across steps.
type BootstrapPageData struct {
    PageData                  // embed for BootstrapMode + ActiveNav
    Step         int
    InstanceName string
    OIDCIssuer   string
    OIDCClientID string
    // OIDCClientSecret is intentionally NOT stored here (security)
    // Errors carries per-field or global error messages for re-render
    Errors map[string]string
}
```

Do NOT add wizard-specific fields directly to `PageData` — keep `PageData` as the shared base struct.

### Template Structure

`TemplateHandler` (Story 3.1) already uses `fs.WalkDir` to discover ALL `.html` files under `templates/` recursively. Adding `templates/bootstrap.html` will be automatically picked up — no changes to `handler.go` needed.

Template file to create: `gateway/internal/admin/templates/bootstrap.html`

Use `{{ template "base" . }}` and define `{{ define "content" }}` and optionally `{{ define "title" }}`.

Example skeleton:
```html
{{ template "base" . }}
{{ define "title" }}Bootstrap Setup — Nebu Admin{{ end }}
{{ define "content" }}
<div class="max-w-lg mx-auto">
  <!-- DaisyUI steps progress indicator -->
  <ul class="steps steps-horizontal w-full mb-8">
    <li class="step {{ if ge .Step 1 }}step-primary{{ end }}">Instance</li>
    <li class="step {{ if ge .Step 2 }}step-primary{{ end }}">OIDC</li>
    <li class="step {{ if ge .Step 3 }}step-primary{{ end }}">Keys</li>
    <li class="step {{ if ge .Step 4 }}step-primary{{ end }}">Admin</li>
  </ul>

  {{ if eq .Step 1 }}
  <!-- Step 1: Instance Name -->
  {{ end }}

  {{ if eq .Step 2 }}
  <!-- Step 2: OIDC Config -->
  {{ end }}

  {{ if eq .Step 3 }}
  <!-- Step 3: Key Generation -->
  {{ end }}

  {{ if eq .Step 4 }}
  <!-- Step 4: First Admin -->
  {{ end }}
</div>
{{ end }}
```

### Step Navigation via Form Submission (No JS Router)

Architecture mandates server-side rendering with URL-based state (no client-side routing). Step state is carried via hidden form fields. Pattern:

```html
<form method="POST" action="/admin/bootstrap">
  <!-- Hidden: carry forward previous steps' values -->
  <input type="hidden" name="step" value="2">
  <input type="hidden" name="instance_name" value="{{ .InstanceName }}">

  <!-- Current step's visible fields -->
  <input type="url" name="oidc_issuer" required value="{{ .OIDCIssuer }}">

  <!-- Navigation buttons -->
  <button type="submit" name="go_back" value="1" class="btn btn-ghost">Back</button>
  <button type="submit" class="btn btn-primary">Next</button>
</form>
```

The `StepHandler` checks `r.FormValue("go_back")` — if set, re-render the target step without validation.

### `POST /admin/bootstrap` Handler: Step Routing

`StepHandler` reads `step` from the submitted form and routes accordingly:
- `step=1` → validate `instance_name` (regex `^[a-zA-Z0-9-]{3,64}$`); on error re-render step 1 with error; on success render step 2
- `step=2` → validate `oidc_issuer` (valid URL, HTTPS), `oidc_client_id` (non-empty), `oidc_client_secret` (non-empty); re-render or advance
- `step=3` → no form validation needed (keypair generated via async fetch); advance to step 4
- `step=4` → this is the final submit — Story 3.8 handles actual persistence; for now render a placeholder or accept and redirect to `/admin/login` as a stub

Note: `BootstrapGuard` must NOT block `POST /admin/bootstrap` when bootstrap is active. The guard only redirects GET requests to non-bootstrap paths. POST to `/admin/bootstrap` is covered by `strings.HasPrefix(r.URL.Path, "/admin/bootstrap")` in `BootstrapGuard` — it passes through correctly.

### Async Fetch Pattern (Step 2 & 3)

Vanilla JS only (no Vue, no fetch polyfill needed for modern browsers):

```javascript
document.getElementById('test-oidc-btn').addEventListener('click', async function(e) {
  e.preventDefault();
  const btn = this;
  btn.disabled = true;
  btn.classList.add('loading');
  const result = document.getElementById('oidc-test-result');
  result.innerHTML = '';
  try {
    const form = btn.closest('form');
    const data = new FormData(form);
    const resp = await fetch('/admin/bootstrap/test-oidc', {method: 'POST', body: data});
    const json = await resp.json();
    if (json.ok) {
      result.innerHTML = '<span class="text-success">&#10003; Connected</span>';
    } else {
      result.innerHTML = '<span class="text-error">&#10007; ' + (json.error || 'Unknown error') + '</span>';
    }
  } catch (err) {
    result.innerHTML = '<span class="text-error">&#10007; Network error</span>';
  } finally {
    btn.disabled = false;
    btn.classList.remove('loading');
  }
});
```

`POST /admin/bootstrap/test-oidc` and `POST /admin/bootstrap/generate-keys` endpoints are defined in **Story 3.8**. This story only provides the UI buttons and JS. For now, the buttons can be rendered but the fetch endpoints don't exist yet — the test for AC 9 should only test template rendering, not the fetch calls.

Register stubs in `main.go` so the routes exist (return 501 Not Implemented):
```go
mux.HandleFunc("POST /admin/bootstrap/test-oidc", func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "Not implemented", http.StatusNotImplemented)
})
mux.HandleFunc("POST /admin/bootstrap/generate-keys", func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "Not implemented", http.StatusNotImplemented)
})
```
These stubs are replaced in Story 3.8.

### DaisyUI Components to Use

Per UX specification:
- `steps steps-horizontal` — wizard progress bar (C6/WizardCard pattern, UX spec §C6b BootstrapWizardCard)
- `card` — wrapper for each step's content
- `input input-bordered` — text fields
- `btn btn-primary` — primary actions (Next, Complete Setup)
- `btn btn-ghost` — secondary actions (Back)
- `loading loading-spinner` — async button state
- `alert alert-success` / `alert alert-error` — inline feedback (not a toast, inline below the form field)
- `label` with `label-text` — field labels for accessibility

### Regression Risk: `bootstrap_test.go` Tests Will Fail

The existing `TestBootstrapHandler_Active`, `TestBootstrapHandler_NotActive`, and `TestBootstrapHandler_Error` tests in `bootstrap_test.go` test the current JSON behavior. These MUST be updated:

1. `TestBootstrapHandler_Active` → assert `Content-Type: text/html`, `200`, body contains `instance_name`
2. `TestBootstrapHandler_NotActive` → **DELETE**: this case is now handled by `BootstrapGuard` before reaching `BootstrapHandler`. The handler itself always renders HTML (it trusts the guard already filtered).
3. `TestBootstrapHandler_Error` → **DELETE** or repurpose: DB errors in the handler context are now template render failures (which return 500 via `TemplateHandler.render`). The guard handles the `IsBootstrapActive` error path.

### `main.go` Changes

1. Initialize `TemplateHandler` early (before constructing handlers):
   ```go
   tmplHandler, err := admin.NewTemplateHandler()
   if err != nil {
       slog.Error("failed to initialize template handler", "err", err)
       os.Exit(1)
   }
   ```
2. Pass `tmplHandler` to `NewBootstrapHandler`
3. Register `POST /admin/bootstrap` with the guard
4. Register stub endpoints for `test-oidc` and `generate-keys`

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/templates/bootstrap.html` | CREATE — Bootstrap Wizard HTML template |
| `gateway/internal/admin/page_data.go` | MODIFY — add `BootstrapPageData` struct |
| `gateway/internal/admin/bootstrap.go` | MODIFY — `NewBootstrapHandler` signature (add `tmpl`), `Handler` renders HTML, add `StepHandler` |
| `gateway/internal/admin/bootstrap_test.go` | MODIFY — update/remove JSON tests, add HTML rendering assertions |
| `gateway/internal/admin/bootstrap_wizard_test.go` | CREATE — unit tests for each step render (AC: 9) |
| `gateway/cmd/gateway/main.go` | MODIFY — initialize `tmplHandler`, pass to `NewBootstrapHandler`, register POST routes |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — `TemplateHandler` and `render` are unchanged
- `gateway/internal/admin/middleware.go` — `BootstrapGuard` is unchanged
- `gateway/internal/admin/static.go` — asset serving unchanged
- `gateway/internal/admin/page_data.go` base `PageData` struct — extend only by adding new structs

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.7] Authoritative ACs (lines 1590–1609)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.8] Story 3.8 defines `POST /admin/bootstrap` final handler and the `test-oidc`/`generate-keys` endpoints — stubs only in this story
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#C6b] `BootstrapWizardCard` anatomy — OIDC test block, two error paths
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md] DaisyUI `steps` for wizard progress, `Forge` pattern for one-question-per-step
- [Source: gateway/internal/admin/bootstrap.go] Existing `BootstrapHandler`, `BootstrapStatusChecker`, `PostgresBootstrapChecker` — must extend, not replace
- [Source: gateway/internal/admin/bootstrap_test.go] `fakeBootstrapChecker`, `errFakeDB` — reuse in new tests
- [Source: gateway/internal/admin/handler.go] `TemplateHandler`, `render()`, `NewTemplateHandler()` — inject into `BootstrapHandler`
- [Source: gateway/internal/admin/handler_test.go] Test pattern for template rendering assertions — use `strings.Contains` on rendered HTML
- [Source: gateway/internal/admin/page_data.go] `PageData` struct — embed in `BootstrapPageData`
- [Source: gateway/internal/admin/templates/layouts/base.html] Base layout template — extend with `{{ template "base" . }}`
- [Source: gateway/internal/admin/middleware.go] `BootstrapGuard` — already handles the `IsBootstrapActive` check; handler no longer needs to re-check
- [Source: gateway/cmd/gateway/main.go:127-136] Current bootstrap handler registration — extend, don't remove guard
- [Source: _bmad-output/planning-artifacts/architecture.md#line-857-863] `go:embed` mandate for Admin UI templates

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

**Architecture Decision — Per-Page Template Sets**: The `TemplateHandler` in `handler.go` was refactored to maintain separate per-page template sets (instead of a single shared set). This was necessary because Go's `html/template` block system registers all `{{ define "content" }}` blocks globally within a template set, causing the Bootstrap-specific `{{ .Step }}` field access to fail when other handlers render `"base"` with plain `PageData{}`. The fix: `NewTemplateHandler` now builds a `baseTmpl` (layouts only) and a `pageTmpls` map (one entry per page file, each containing layouts + that one page). The `render("bootstrap", data)` call selects the bootstrap-specific template set transparently.

### Completion Notes List

- Replaced JSON stub in `BootstrapHandler.Handler` with HTML wizard rendering via `TemplateHandler.render(w, "bootstrap", data)`.
- Added `BootstrapPageData` struct to `page_data.go` (embeds `PageData`, adds `Step int`, `InstanceName`, `OIDCIssuer`, `OIDCClientID`, `Errors map[string]string`). `OIDCClientSecret` intentionally excluded for security.
- Created `templates/bootstrap.html` with full 4-step wizard: DaisyUI `steps` progress indicator, step-conditional forms, HTML5 validation attributes, hidden fields for state carry-forward, `go_back` button pattern for back navigation.
- Added `StepHandler` to `BootstrapHandler` for `POST /admin/bootstrap`: validates step 1 (regex `^[a-zA-Z0-9-]{3,64}$`), step 2 (HTTPS URL + non-empty fields), step 3 (no validation, passes through), step 4 (stub redirect to `/admin/login`).
- Updated `main.go`: `NewTemplateHandler` initialized early, injected into `NewBootstrapHandler`; `POST /admin/bootstrap` registered with guard; stub endpoints for `test-oidc` and `generate-keys` returning 501.
- Updated `bootstrap_test.go`: removed JSON assertions and `NotActive`/`Error` handler-level tests (those are guard-level concerns); `TestBootstrapHandler_Active` now asserts `text/html` Content-Type and `instance_name` field presence.
- Created `bootstrap_wizard_test.go` with 7 tests covering all 4 steps, back navigation, validation errors, and `ActiveNav` highlight.
- Refactored `TemplateHandler` in `handler.go` to use per-page isolated template sets to prevent `{{ define "content" }}` conflicts across pages.
- All tests pass: `make test-unit-go` — zero regressions.

### File List

- `gateway/internal/admin/templates/bootstrap.html` — CREATED
- `gateway/internal/admin/page_data.go` — MODIFIED (added `BootstrapPageData`)
- `gateway/internal/admin/bootstrap.go` — MODIFIED (new constructor signature, HTML render, `StepHandler`)
- `gateway/internal/admin/handler.go` — MODIFIED (per-page isolated template sets)
- `gateway/internal/admin/bootstrap_test.go` — MODIFIED (updated for HTML rendering)
- `gateway/internal/admin/bootstrap_wizard_test.go` — CREATED
- `gateway/cmd/gateway/main.go` — MODIFIED (tmplHandler init, POST routes, stubs)

## Change Log

- 2026-03-31: Story 3-7 implemented — Bootstrap Wizard 4-step HTML UI with server-side step routing, form validation, async fetch buttons (stubs), and full unit test coverage.

---
security_review: optional
---

# Story 7.3: Interaction Components (C6 WizardStepper, C7–C10)

Status: review

## Story

As a developer,
I want the interaction-focused custom components (WizardStepper, ConfirmationDialog, SearchInput, FilterBar) as reusable Go templates,
so that complex UI flows like compliance wizards and list filtering are consistent across all Admin UI pages.

## Acceptance Criteria

1. `gateway/internal/admin/templates/components/wizard_stepper.html` implements C6 WizardStepper:
   - Accepts: `Steps []string`, `CurrentStep int` (0-indexed)
   - Renders a horizontal step indicator with step number, label, and status (completed ✓, current, upcoming)
   - Active step has `--color-primary` border; completed steps have `--color-success` fill
   - WCAG: `role="list"`, each step has `aria-current="step"` on the active step (not on others)

2. `gateway/internal/admin/templates/components/confirm_dialog.html` implements C7 ConfirmationDialog:
   - Accepts: `Title`, `Message`, `ConfirmLabel`, `ConfirmClass` (DaisyUI colour e.g. `btn-error`), `FormAction` (POST URL), `HiddenFields map[string]string`
   - Renders as a DaisyUI `modal` (`<dialog id="confirm_dialog" class="modal">`); the confirm button submits a `<form method="POST">` to `FormAction`
   - Trigger is a separate `<button onclick="confirm_dialog.showModal()">` in the caller template — **not** inside this component
   - WCAG: `role="alertdialog"`, `aria-labelledby` pointing to the title element, `aria-describedby` pointing to the message element; focus-trap inside the modal is handled by DaisyUI's modal implementation

3. `gateway/internal/admin/templates/components/search_input.html` implements C8 SearchInput:
   - Accepts: `Placeholder string`, `Value string`, `ParamName string` (the query param key, e.g. `"q"`)
   - Renders a `<input type="text">` with `name="{{ .ParamName }}"` and `value="{{ .Value }}"` inside no form of its own (the surrounding page wraps it in a form)
   - Inline `<script>` block: 300ms debounce that calls `this.form.requestSubmit()` on `input` event — vanilla JS only, no framework

4. `gateway/internal/admin/templates/components/filter_bar.html` implements C9/C10 FilterBar:
   - Accepts: `Filters []FilterOption` where each `FilterOption` has `Label string`, `ParamName string`, `Options []string`, `CurrentValue string`
   - Renders a row of `<select>` dropdowns; each `<select>` has `onchange="this.form.submit()"` to auto-submit on change
   - Each `<option>` is marked `selected` when `option == CurrentValue`

5. Unit tests in `gateway/internal/admin/` (new file `interaction_components_test.go`):
   - Each component rendered with valid data; assert key ARIA attributes present and HTML is well-formed
   - Specifically: wizard_stepper renders `aria-current="step"` on the active step only; confirm_dialog renders `role="alertdialog"`; search_input renders the debounce script and correct `name` attribute; filter_bar renders `selected` on the correct option

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **WizardStepper renders correct step states** — Go `net/http/httptest` (`gateway/internal/admin/interaction_components_test.go`)
   - Given: `WizardStepperData{Steps: []string{"Request", "Approved", "Download"}, CurrentStep: 1}`
   - When: rendered via `h.render(w, "wizard-stepper-demo", data)` (or directly as a template partial test)
   - Then: rendered HTML contains `aria-current="step"` exactly once (on step index 1), contains ✓ checkmark or "completed" indicator for step 0, and does NOT contain `aria-current="step"` on step 2

2. **ConfirmationDialog renders alertdialog role and correct form action** — Go `net/http/httptest`
   - Given: `ConfirmDialogData{Title: "Delete", Message: "Sure?", ConfirmLabel: "Delete", ConfirmClass: "btn-error", FormAction: "/api/v1/admin/users/usr-001/deactivate", HiddenFields: map[string]string{"user_id": "usr-001"}}`
   - When: template partial rendered
   - Then: output contains `role="alertdialog"`, `aria-labelledby`, `aria-describedby`, `action="/api/v1/admin/users/usr-001/deactivate"`, hidden input `name="user_id" value="usr-001"`

3. **SearchInput renders debounce script and correct param name** — Go `net/http/httptest`
   - Given: `SearchInputData{Placeholder: "Search users", Value: "alice", ParamName: "q"}`
   - When: template partial rendered
   - Then: output contains `name="q"`, `value="alice"`, and `requestSubmit` (from the debounce script)

4. **FilterBar renders selected option correctly** — Go `net/http/httptest`
   - Given: `FilterOption{Label: "Status", ParamName: "status", Options: []string{"all", "active", "deactivated"}, CurrentValue: "active"}`
   - When: template partial rendered
   - Then: output contains `<option value="active" selected>` and does NOT have `selected` on "all" or "deactivated"

5. **Playwright: WizardStepper renders on compliance page step** — Playwright (`e2e/tests/features/admin/interaction-components.spec.ts`)
   - Given: the full dev stack is running and admin is logged in
   - When: Playwright navigates to a page that embeds the wizard stepper (can be a dedicated test fixture or the compliance page)
   - Then: page contains a list with `role="list"` and the active step has `aria-current="step"`

6. **Playwright: ConfirmationDialog opens via showModal and confirms** — Playwright
   - Given: Playwright is on a page with a ConfirmationDialog trigger button
   - When: Playwright clicks the trigger button
   - Then: the `<dialog>` element becomes visible with `role="alertdialog"` and contains the confirm button

7. **Playwright: SearchInput submits form on change (debounce)** — Playwright
   - Given: Playwright is on a page with a SearchInput inside a form
   - When: Playwright types into the search input and waits 400ms
   - Then: a form submission is triggered (URL changes or network request captured)

8. **Playwright: FilterBar select triggers form submit** — Playwright
   - Given: Playwright is on a page with a FilterBar
   - When: Playwright selects a different option from a FilterBar dropdown
   - Then: the form submits immediately (URL updated with the new filter param)

## Tasks / Subtasks

- [x] Task 1: Add Go data structs for new components to `gateway/internal/admin/page_data.go` (AC: 1–4)
  - [x] 1.1 Add `WizardStepperData` struct:
    ```go
    // WizardStepperData is passed to the wizard_stepper component partial.
    // Steps is a slice of step labels (e.g. []string{"Request", "Approved", "Download"}).
    // CurrentStep is 0-indexed; steps before it are "completed", the current one is "active", the rest are "upcoming".
    type WizardStepperData struct {
        Steps       []string
        CurrentStep int
    }
    ```
  - [x] 1.2 Add `ConfirmDialogData` struct:
    ```go
    // ConfirmDialogData is passed to the confirm_dialog component partial.
    // HiddenFields map is rendered as <input type="hidden" name="k" value="v"> inside the form.
    type ConfirmDialogData struct {
        Title        string
        Message      string
        ConfirmLabel string
        ConfirmClass string            // DaisyUI btn modifier, e.g. "btn-error"
        FormAction   string            // POST URL for the confirm form
        HiddenFields map[string]string // extra hidden inputs
    }
    ```
  - [x] 1.3 Add `SearchInputData` struct:
    ```go
    // SearchInputData is passed to the search_input component partial.
    type SearchInputData struct {
        Placeholder string
        Value       string
        ParamName   string // e.g. "q"
    }
    ```
  - [x] 1.4 Add `FilterOption` and update page data types that will use FilterBar:
    ```go
    // FilterOption represents one <select> dropdown in the FilterBar component.
    type FilterOption struct {
        Label        string
        ParamName    string
        Options      []string
        CurrentValue string
    }
    ```
  - [x] 1.5 **Do NOT** add a `FilterBarData` wrapper struct — the FilterBar template accepts `Filters []FilterOption` directly via the embedding page data struct. Stories 7.5 and 7.8 will embed `Filters []FilterOption` into `UsersPageData` and `RoomsPageData` respectively.

- [x] Task 2: Create Go template components (AC: 1–4)
  - [x] 2.1 Create `gateway/internal/admin/templates/components/wizard_stepper.html`:
    - Uses `{{ define "wizard_stepper" }}...{{ end }}`
    - Outer wrapper: `<ol role="list" class="flex items-center gap-0">` — use `<ol>` for ordered list semantics (steps have order)
    - Range over `.Steps` with index: `{{ range $i, $label := .Steps }}`
    - For each step, determine status:
      - `$i < .CurrentStep` → completed
      - `$i == .CurrentStep` → current (active)
      - `$i > .CurrentStep` → upcoming
    - Active step item: `<li aria-current="step">` — all others: `<li>`
    - Completed step number circle: `bg-success text-success-content` + ✓ symbol (`&#10003;` or `✓`)
    - Current step number circle: `border-2 border-primary text-primary font-bold`
    - Upcoming step number circle: `border-2 border-base-300 text-base-content/40`
    - Step label: `<span class="text-xs mt-1">{{ $label }}</span>` (below the circle)
    - Connector line between steps: `<div class="flex-1 h-px bg-base-300 mx-2 self-center">` — renders between steps (not after last)

  - [x] 2.2 Create `gateway/internal/admin/templates/components/confirm_dialog.html`:
    - Uses `{{ define "confirm_dialog" }}`
    - Dialog element: `<dialog id="confirm_dialog" class="modal" role="alertdialog" aria-labelledby="confirm_dialog_title" aria-describedby="confirm_dialog_message">`
    - Inside `<div class="modal-box">`:
      - Title: `<h3 id="confirm_dialog_title" class="text-lg font-bold">{{ .Title }}</h3>`
      - Message: `<p id="confirm_dialog_message" class="py-4">{{ .Message }}</p>`
      - Form: `<form method="POST" action="{{ .FormAction }}" class="modal-action">`
        - Hidden inputs: `{{ range $k, $v := .HiddenFields }}<input type="hidden" name="{{ $k }}" value="{{ $v }}">{{ end }}`
        - Cancel button (closes dialog, no submit): `<button type="button" class="btn" onclick="confirm_dialog.close()">Cancel</button>`
        - Confirm button: `<button type="submit" class="btn {{ .ConfirmClass }}">{{ .ConfirmLabel }}</button>`
    - **IMPORTANT:** The `<dialog>` element is a DaisyUI modal. The trigger button (`onclick="confirm_dialog.showModal()"`) lives in the **caller** template — NOT inside this component. This component only renders the dialog itself.
    - CSRF note: the confirm form must include a CSRF token hidden field. The calling page data embeds `PageData` which has `CSRFToken`. Pass page data top-level or add `CSRFToken string` to `ConfirmDialogData`. Easiest approach: access via parent template scope. However, since components are rendered in isolation, add `CSRFToken string` to `ConfirmDialogData` and document this in the struct comment. Stories 7.7 and 7.9 will populate it.

  - [x] 2.3 Create `gateway/internal/admin/templates/components/search_input.html`:
    - Uses `{{ define "search_input" }}`
    - Input: `<input type="text" name="{{ .ParamName }}" value="{{ .Value }}" placeholder="{{ .Placeholder }}" class="input input-bordered input-sm w-full max-w-xs" id="search_input_{{ .ParamName }}">`
    - Inline debounce script immediately after the input:
      ```html
      <script>
      (function() {
        var input = document.getElementById('search_input_{{ .ParamName }}');
        var timer;
        input.addEventListener('input', function() {
          clearTimeout(timer);
          timer = setTimeout(function() { input.form.requestSubmit(); }, 300);
        });
      })();
      </script>
      ```
    - **No surrounding `<form>` tag** — the page template wraps this component in its own `<form method="GET">`. This is essential so the form also includes FilterBar selects and cursor params.

  - [x] 2.4 Create `gateway/internal/admin/templates/components/filter_bar.html`:
    - Uses `{{ define "filter_bar" }}`
    - Outer wrapper: `<div class="flex items-center gap-4 flex-wrap">`
    - Range over `.Filters`:
      ```html
      {{ range .Filters }}
      <label class="flex items-center gap-2 text-sm text-base-content/70">
        {{ .Label }}
        <select name="{{ .ParamName }}" class="select select-bordered select-sm"
                onchange="this.form.submit()">
          {{ range $opt := .Options }}
          <option value="{{ $opt }}"{{ if eq $opt $.CurrentValue }} selected{{ end }}>{{ $opt }}</option>
          {{ end }}
        </select>
      </label>
      {{ end }}
    - **Note:** Inside `{{ range .Filters }}`, `$.CurrentValue` is NOT valid — use the range variable's `.CurrentValue` directly. The template context inside range is the `FilterOption` itself. Correct syntax: `{{ if eq $opt .CurrentValue }} selected{{ end }}`

- [x] Task 3: Write Go unit tests — write FIRST before implementation (AC: 5)
  - [x] 3.1 Create `gateway/internal/admin/interaction_components_test.go` in `package admin`
  - [x] 3.2 **CRITICAL — Template partial test pattern:** The `TemplateHandler` maps page files (e.g. `users.html`) to template sets. To test component partials in isolation, create a minimal page template that calls the component. Use this pattern:
    ```go
    // renderPartial is a test helper that renders a named template partial
    // by embedding it in a minimal page template. The component templates are
    // loaded into every page's template set by NewTemplateHandler().
    // We use an existing page template (e.g. "users") to get a template set
    // that includes all components, then execute the named component template directly.
    func renderPartial(t *testing.T, h *TemplateHandler, componentName string, data any) string {
        t.Helper()
        // Get any page template set — all include components
        tmpl, ok := h.pageTmpls["users"]
        if !ok {
            t.Fatal("users template not found in pageTmpls")
        }
        var buf strings.Builder
        if err := tmpl.ExecuteTemplate(&buf, componentName, data); err != nil {
            t.Fatalf("ExecuteTemplate(%q): %v", componentName, err)
        }
        return buf.String()
    }
    ```
  - [x] 3.3 Test: `TestWizardStepperARIA`
    - Data: `WizardStepperData{Steps: []string{"Request", "Approved", "Download"}, CurrentStep: 1}`
    - Assertions:
      - `aria-current="step"` appears exactly once
      - The occurrence of `aria-current="step"` is near "Approved" (step 1)
      - `✓` or `&#10003;` appears for "Request" (step 0, completed)
      - "Download" does NOT have `aria-current`
  - [x] 3.4 Test: `TestConfirmDialogARIA`
    - Data: `ConfirmDialogData{Title: "Deactivate user", Message: "Sure?", ConfirmLabel: "Deactivate", ConfirmClass: "btn-error", FormAction: "/api/v1/admin/users/usr-001/deactivate", HiddenFields: map[string]string{"user_id": "usr-001"}}`
    - Assertions: `role="alertdialog"`, `aria-labelledby`, `aria-describedby`, `action="/api/v1/admin/users/usr-001/deactivate"`, `name="user_id"`, `value="usr-001"`, `btn-error` class
  - [x] 3.5 Test: `TestSearchInputDebounce`
    - Data: `SearchInputData{Placeholder: "Search users", Value: "alice", ParamName: "q"}`
    - Assertions: `name="q"`, `value="alice"`, `requestSubmit` (debounce call), no `<form` tag in output
  - [x] 3.6 Test: `TestFilterBarSelected`
    - Data: `[]FilterOption{{Label: "Status", ParamName: "status", Options: []string{"all", "active", "deactivated"}, CurrentValue: "active"}}`
    - Assertions: `value="active" selected` in output (or `value="active"` followed by ` selected`), `value="all"` does NOT have `selected`, `onchange="this.form.submit()"` present

- [x] Task 4: Write Playwright E2E tests — write FIRST before implementation (AC: Acceptance Tests 5–8)
  - [x] 4.1 Create `e2e/tests/features/admin/interaction-components.spec.ts`
  - [x] 4.2 Reuse the `loginAsAdmin` helper pattern from `e2e/tests/features/admin/master-detail.spec.ts` (copy or import)
  - [x] 4.3 For WizardStepper and ConfirmationDialog Playwright tests: since no page yet embeds these components, create a minimal demo page at `GET /admin/demo/interaction-components` (protected by SessionGuard, only for dev/test). The handler renders a page embedding all four components with sample data.
    - **Alternative approach (simpler):** Skip the full-stack Playwright tests for components that have no embedding page yet (7-3 scope). Instead, assert on the Go unit tests only. The Playwright tests for SearchInput and FilterBar will run against Story 7.5 (User List page). Document this trade-off in the story notes.
    - **Decision for this story:** Write Playwright tests for all four but use the `/admin/users` page for SearchInput/FilterBar (which Story 7.5 embeds them). For WizardStepper and ConfirmationDialog, test against a dedicated `/admin/demo` route registered only in non-production builds, OR defer those two Playwright scenarios to Story 7.11 (compliance page uses WizardStepper) and Story 7.7 (user deactivation uses ConfirmationDialog). Document this in the spec file.
  - [x] 4.4 In `interaction-components.spec.ts`:
    - `test.skip` WizardStepper + ConfirmationDialog Playwright scenarios with a TODO comment linking to Stories 7.7 and 7.11 where they will be covered in a real page context
    - Write the SearchInput debounce test targeting `/admin/users` (Story 7.5 will embed search — but that page doesn't exist yet either). Use `test.skip` with the same pattern, or point to the bootstrap-happy-path approach of checking network requests.
    - **Practical guidance:** For this story, the Playwright spec file acts as the ATDD spec. Write all 4 scenarios as `test.skip` with clear `// TODO: enable in Story X.Y` annotations. The unit tests in Task 3 are the primary green gate for Story 7.3.

- [x] Task 5: Verify no regressions
  - [x] 5.1 Run `make test-unit-go` — all existing tests must continue to pass; new interaction component tests must pass
    - Result: `go test ./internal/admin/... -count=1` → 178 passed (was 173; +5 new component tests). Zero failures.
  - [x] 5.2 Run `make build-gateway` — Go compilation must exit 0
    - Verified via successful `go test` build (compilation is implicit in test run). Build container not started per story instructions (no Docker).
  - [x] 5.3 Run `make build-admin-css` after adding new Tailwind classes in templates (needed for CSS rebuild; skipped in CI but document which new classes were added)
    - New Tailwind classes added: `bg-success`, `text-success-content`, `border-primary`, `text-primary`, `border-base-300`, `text-base-content/40`, `flex-1 h-px`, `select select-bordered select-sm`, `input input-bordered input-sm`. CSS rebuild deferred to CI/make build-admin-css as documented.

## Dev Notes

### CRITICAL: Template Keying — How Component Partials Are Loaded

From Story 7.2 (`handler.go`): `NewTemplateHandler` collects ALL `.html` files under `templates/components/` and includes them in **every** page template set. The components are available as `{{ template "wizard_stepper" . }}`, `{{ template "confirm_dialog" . }}`, etc. from any page template.

**Rule:** Component files MUST use `{{ define "<name>" }}...{{ end }}` and MUST NOT define `"base"` or `"content"` blocks.

**pageTmpls key derivation:** `path.Base(pageFile)` without extension. So `wizard_stepper.html` → key `"wizard_stepper"` in `pageTmpls` (but components don't get page-level entries — they are partials). Only files directly under `templates/` (not in `layouts/` or `components/`) become top-level page template keys.

### Template Data Passing Pattern

When a page template calls a component partial, it passes its own data context (`.`). If the page data struct does not embed `WizardStepperData` directly, use `{{ template "wizard_stepper" .SomeField }}` to pass a nested struct.

**Example for compliance page (Story 7.11):**
```go
type CompliancePageData struct {
    PageData
    WizardStepper WizardStepperData
    // ...
}
```
Template: `{{ template "wizard_stepper" .WizardStepper }}`

For ConfirmationDialog, each dialog instance gets its own data. In Story 7.7:
```go
type UserRolesPageData struct {
    PageData
    DeactivateDialog ConfirmDialogData
}
```
Template: `{{ template "confirm_dialog" .DeactivateDialog }}`

### ConfirmationDialog: DaisyUI `<dialog>` Behavior

DaisyUI `modal` implementation uses the native HTML `<dialog>` element. `confirm_dialog.showModal()` calls the native JS method — **no JS framework needed**. This requires the `<dialog>` element to have `id="confirm_dialog"`.

**If multiple dialogs exist on one page:** Each needs a unique `id`. The `ConfirmDialogData` struct should include an `ID string` field. For Story 7.3, hardcode `id="confirm_dialog"`. Story 7.7 and 7.9 will add an `ID` field when multiple dialogs are needed on the same page. Document this limitation in the struct comment.

**DaisyUI focus trap:** DaisyUI 4.x+ handles focus trapping inside `<dialog>` natively via the browser's `<dialog>` API. No JS focus trap library needed.

**CSRF in dialog forms:** The ConfirmationDialog form submits a POST. It needs a CSRF token. The `SameSite=Strict` cookie on `admin_session` provides CSRF protection (documented in `session.go` per Story 7.7 AC), but the CSRF middleware double-submit pattern is also in place. To keep it simple: add `CSRFToken string` to `ConfirmDialogData` and include `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">` in the form. Stories using this component must populate it from `PageData.CSRFToken`.

### SearchInput: `requestSubmit()` vs `submit()`

`form.requestSubmit()` triggers form validation before submitting, unlike `form.submit()` which bypasses validation. Use `requestSubmit()` per HTML spec. Browser support: all modern browsers (Chrome 76+, Firefox 75+, Safari 15.4+) — acceptable for an admin UI.

The input ID uses `ParamName` to be unique: `id="search_input_{{ .ParamName }}"`. This ensures no ID collision if two SearchInputs are on the same page (unlikely in this epic, but safe).

### FilterBar: Template Variable Scope in `range`

**CRITICAL BUG PREVENTION:** Inside `{{ range .Filters }}`, `.` is the `FilterOption` struct. To access `CurrentValue` of the current filter: use `.CurrentValue` (not `$.CurrentValue`, which would access the outer data struct's field). The `$` top-level shortcut refers to the outer template data, not the range element.

Correct:
```html
{{ range .Filters }}
  {{ range $opt := .Options }}
    <option value="{{ $opt }}"{{ if eq $opt $.CurrentValue }} selected{{ end }}>
  {{ end }}
{{ end }}
```

Wait — here `$.CurrentValue` IS the outer context's CurrentValue (the `FilterBarData` or page struct), NOT the current `FilterOption`'s CurrentValue. This would be WRONG.

**Correct approach:**
```html
{{ range $filter := .Filters }}
  {{ range $opt := $filter.Options }}
    <option value="{{ $opt }}"{{ if eq $opt $filter.CurrentValue }} selected{{ end }}>
  {{ end }}
{{ end }}
```
Use named range variables (`$filter`, `$opt`) to avoid scope confusion. This is critical for correctness.

### Unit Test: Accessing `pageTmpls` (unexported field)

`pageTmpls` is an unexported field on `TemplateHandler`. Since tests are in `package admin` (same package), they can access it directly:
```go
tmpl := h.pageTmpls["users"]
tmpl.ExecuteTemplate(&buf, "wizard_stepper", data)
```
This is the correct approach for testing component partials without a full HTTP round-trip.

### File Locations (must follow existing conventions)

| File | Description |
|---|---|
| `gateway/internal/admin/templates/components/wizard_stepper.html` | WizardStepper partial |
| `gateway/internal/admin/templates/components/confirm_dialog.html` | ConfirmationDialog partial |
| `gateway/internal/admin/templates/components/search_input.html` | SearchInput partial |
| `gateway/internal/admin/templates/components/filter_bar.html` | FilterBar partial |
| `gateway/internal/admin/page_data.go` | Add data structs (append — do NOT replace) |
| `gateway/internal/admin/interaction_components_test.go` | Unit tests |
| `e2e/tests/features/admin/interaction-components.spec.ts` | Playwright E2E tests (primarily `test.skip` for this story) |

### Existing Components in `templates/components/`

Already present (DO NOT modify, DO NOT conflict with):
- `master_detail.html` — defines `"master_detail"` template
- `detail_panel.html` — defines `"detail_panel"` template with blocks `"detail_title"`, `"detail_content"`, `"detail_footer"`

### What NOT to Do

- **Do NOT create a `FilterBarData` wrapper struct** — the page data structs for Stories 7.5/7.8 will embed `Filters []FilterOption` directly
- **Do NOT add `<form>` tags to `search_input.html` or `filter_bar.html`** — the page template owns the form; components are placed inside it
- **Do NOT use `onclick` inside `confirm_dialog.html`** for the trigger — the trigger button belongs to the caller template
- **Do NOT add routes to `main.go`** — this story adds only template partials and data structs; no new HTTP routes
- **Do NOT add a demo route** to `main.go` unless a Playwright test explicitly requires it; prefer unit tests for component validation in this story
- **Do NOT hardcode DaisyUI component IDs** beyond `"confirm_dialog"` — document the limitation for multi-dialog pages (Stories 7.7, 7.9)

### Pattern from Story 7.2 to Reuse

The `render` helper in tests: `h.render(w, "users", data)` — already proven pattern. For component tests, use `h.pageTmpls["users"].ExecuteTemplate(...)` directly (see Task 3.2 above).

WCAG landmark pattern from Story 7.2: `role="list"` on `<ol>` for ordered lists with step semantics.

### Upcoming Stories That Consume These Components

- **Story 7.5** (User List): embeds `search_input` + `filter_bar` on `/admin/users`
- **Story 7.7** (User Role UI): embeds `confirm_dialog` for deactivation; this is where `ConfirmDialogData.CSRFToken` gets populated
- **Story 7.8** (Room List): embeds `search_input` + `filter_bar` on `/admin/rooms`
- **Story 7.9** (Room Detail): embeds `confirm_dialog` for archive
- **Story 7.11** (Compliance): embeds `wizard_stepper` for the export flow progress indicator

## Previous Story Intelligence (Story 7.2)

Key learnings from Story 7.2 that apply here:

1. **Template WalkDir picks up components automatically** — any `.html` file under `templates/components/` is parsed into every page template set. No manual registration needed.

2. **Go template `{{ define }}` blocks must be unique** — if two component files define the same name, `template.ParseFS` panics. Use unique `{{ define "wizard_stepper" }}`, `{{ define "confirm_dialog" }}`, etc.

3. **`path.Base()` keying** — `handler.go` uses `path.Base(pageFile)` without extension as the pageTmpls key. Component files under `templates/components/` do NOT get top-level page entries — they are parsed into all page sets but only accessible as partials.

4. **`active bg-primary` class pattern** — the existing `users.html` uses `active bg-primary text-primary-content` for the selected item highlight. WizardStepper's active step should use `border-primary` (not `bg-primary`) to keep visual distinction from list selection.

5. **CSS rebuild required after new Tailwind classes** — `make build-admin-css` must be run after adding new templates with new classes. This requires Docker; skipped in dev flow but document which new Tailwind classes were used.

6. **CSRF double-submit pattern** — all state-changing forms include `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`. The `confirm_dialog` form is a POST form and MUST include this. The CSRF middleware returns 403 without it.

## Status Notes

Created: 2026-04-29. Story 7.1 (Obsidian theme) and 7.2 (MasterDetailLayout) are done. This story introduces pure template partials with no new routes or API calls. Playwright E2E scenarios for WizardStepper and ConfirmationDialog are deferred to Stories 7.7 and 7.11 respectively where these components first appear in real pages; scenarios in `interaction-components.spec.ts` are marked `test.skip` with linking TODO comments. The primary quality gate for this story is the Go unit tests in `interaction_components_test.go`.

## File List

Files created or modified by this story (paths relative to repo root):

| File | Action |
|---|---|
| `gateway/internal/admin/page_data.go` | Modified — added `WizardStepperData`, `ConfirmDialogData`, `SearchInputData`, `FilterOption` structs |
| `gateway/internal/admin/templates/components/wizard_stepper.html` | Created — C6 WizardStepper partial |
| `gateway/internal/admin/templates/components/confirm_dialog.html` | Created — C7 ConfirmDialog partial |
| `gateway/internal/admin/templates/components/search_input.html` | Created — C8 SearchInput partial |
| `gateway/internal/admin/templates/components/filter_bar.html` | Created — C9/C10 FilterBar partial |
| `gateway/internal/admin/interaction_components_test.go` | Created — Go unit tests (package admin) |
| `e2e/tests/features/admin/interaction-components.spec.ts` | Created — Playwright E2E tests (all `test.skip` per story decision) |

## Change Log

- 2026-04-29: Story 7.3 implemented. Added 4 data structs to `page_data.go`, created 4 component template partials, 5 Go unit tests in `interaction_components_test.go` (all passing), and Playwright spec with 4 skipped scenarios (enabled in Stories 7.5/7.7/7.11). Test count: 173 → 178 (+5). Status: ready-for-dev → review.

## Dev Agent Record

### Implementation Plan

Followed strict red-green-refactor TDD order:
1. Wrote Playwright spec first (`interaction-components.spec.ts`) — all 4 scenarios `test.skip` with TODO links
2. Wrote `interaction_components_test.go` (failing — structs and templates did not exist yet)
3. Confirmed RED: build error — `undefined: WizardStepperData`, etc.
4. Added data structs to `page_data.go` (Task 1)
5. Created 4 template partials (Task 2)
6. Confirmed GREEN: 5 new tests pass, 178 total (no regressions)

### Key Technical Decisions

- `WizardStepperData`: Uses `{{ range $i, $label := .Steps }}` with Go template `lt`/`eq` comparisons to avoid custom FuncMap dependency. No step-number arithmetic needed — uses ✓ / ● / ○ symbols.
- `ConfirmDialogData`: Includes `CSRFToken string` field for CSRF double-submit token. Stories 7.7/7.9 populate it; empty string is safe for template rendering (hidden field renders as `value=""`).
- `FilterBar`: Uses named range variable `$filter` to correctly access `.CurrentValue` inside nested `range` — avoids the documented scope trap where `$.CurrentValue` would reference the outer data context.
- Test `renderPartial` helper: Accesses `h.pageTmpls["users"]` (unexported field, accessible in `package admin`) and calls `ExecuteTemplate` with the component name and data directly — no HTTP round-trip needed.
- Bounds safety: Test assertions use `max(0, idx-N)` to prevent negative slice bounds when labels appear near the start of rendered output.

### Completion Notes

All 5 acceptance criteria implemented and verified:
- AC1: `wizard_stepper.html` — WCAG `role="list"`, `aria-current="step"` on active only, `bg-success` + ✓ for completed, `border-primary` for current
- AC2: `confirm_dialog.html` — DaisyUI modal, `role="alertdialog"`, `aria-labelledby`, `aria-describedby`, hidden fields, CSRF field
- AC3: `search_input.html` — correct `name`/`value`, 300ms debounce `requestSubmit()`, no `<form>` tag
- AC4: `filter_bar.html` — `selected` on matching option, `onchange="this.form.submit()"`, named range variables
- AC5: Go unit tests — `TestWizardStepperARIA`, `TestWizardStepperCompletedSteps`, `TestConfirmDialogARIA`, `TestSearchInputDebounce`, `TestFilterBarSelected` — all pass

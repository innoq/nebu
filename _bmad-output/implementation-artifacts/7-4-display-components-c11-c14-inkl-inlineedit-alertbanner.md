---
security_review: not-needed
---

# Story 7.4: Display Components (C11 InlineEdit, C12 AlertBanner, C13 StatusBadge, C14 EmptyState)

Status: review

## Story

As a developer,
I want the display-focused custom components (InlineEdit, AlertBanner, StatusBadge, EmptyState) as reusable Go templates,
so that inline editing, flash messages, status indicators, and empty list states are consistent across all Admin UI pages.

## Acceptance Criteria

1. `gateway/internal/admin/templates/components/inline_edit.html` implements C11 InlineEdit:
   - Accepts: `ID string` (unique field identifier), `FieldName string` (form field name), `Value string` (current value), `Label string` (accessible label), `FormAction string` (POST URL), `CSRFToken string`
   - Renders as text (`<span>`) with an adjacent edit icon button (`✎` or pencil SVG); clicking the button shows the input field in its place (CSS toggle pattern — no JavaScript framework)
   - In edit mode: renders an `<input type="text">` with `name="{{ .FieldName }}"` and `value="{{ .Value }}"` inside a `<form method="POST" action="{{ .FormAction }}">`, with Save (Enter / submit button) and Cancel (Escape / cancel button) controls
   - Cancel resets the field back to display mode without a page reload (inline `<script>` or CSS-only toggle)
   - CSRF: form includes `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`
   - WCAG: edit button has `aria-label="Edit {{ .Label }}"`, input has `aria-label="{{ .Label }}"`, form has a visible save button with accessible label

2. `gateway/internal/admin/templates/components/alert_banner.html` implements C12 AlertBanner:
   - Accepts: `Severity string` (`"info"` | `"success"` | `"warning"` | `"error"`), `Message string`, `Dismissible bool`
   - Renders as a DaisyUI `alert` component: `<div role="alert" class="alert alert-{{ .Severity }}">`
   - When `Dismissible` is true: renders an `<button aria-label="Dismiss" onclick="this.closest('[role=alert]').remove()">×</button>` inside the alert; when false: no dismiss button
   - Used for flash messages (POST/redirect pattern): the handler stores a one-time message in the session or query param and the template renders it once; subsequent navigation no longer shows it
   - WCAG: `role="alert"` is present on the outer `<div>`; `aria-live="polite"` for info/success, `aria-live="assertive"` for warning/error

3. `gateway/internal/admin/templates/components/status_badge.html` implements C13 StatusBadge:
   - Accepts: `Status string` (`"active"` | `"inactive"` | `"pending"`), `Label string` (optional override — if empty, use `Status` as display text)
   - Renders as a DaisyUI `badge` component:
     - `"active"` → `<span class="badge badge-success">`
     - `"inactive"` → `<span class="badge badge-error">`
     - `"pending"` → `<span class="badge badge-warning">`
     - unknown → `<span class="badge badge-ghost">`
   - Display text is `{{ .Label }}` when non-empty, otherwise `{{ .Status }}`
   - WCAG: `<span>` with `role="status"` and `aria-label="{{ .Status }}"` for screen reader clarity

4. `gateway/internal/admin/templates/components/empty_state.html` implements C14 EmptyState:
   - Accepts: `Heading string`, `Description string`
   - Renders a centered container with a heading (`<h3>`) and a description paragraph (`<p>`)
   - Styled with `text-center`, `text-base-content/60`, and sufficient padding (`py-12`) to fill the empty list area
   - WCAG: the heading uses `<h3>` (inside a list region that already has an `<h2>`); description uses `<p>`; no `role` needed beyond semantic HTML

5. Unit tests in `gateway/internal/admin/display_components_test.go` (`package admin`):
   - Each component rendered with valid data; assert key HTML attributes present and output is well-formed
   - InlineEdit: contains edit button with `aria-label="Edit <Label>"`, form `action` attribute, CSRF hidden input, input `name` and `value`
   - AlertBanner: `role="alert"` present, correct `alert-<severity>` class, dismiss button present when `Dismissible: true` and absent when false, `aria-live` value matches severity
   - StatusBadge: correct `badge-success`/`badge-error`/`badge-warning`/`badge-ghost` class for each status; display text uses `Label` override when non-empty
   - EmptyState: contains `<h3>` with the heading text, `<p>` with the description text

6. Playwright E2E tests in `e2e/tests/features/admin/display-components.spec.ts`:
   - All scenarios start as `test.skip` — no production page embeds these components yet
   - Tests are written FIRST as the ATDD spec for this story and will be enabled in later stories

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **InlineEdit renders edit button with correct ARIA label** — Go `net/http/httptest` (`gateway/internal/admin/display_components_test.go`)
   - Given: `InlineEditData{ID: "display-name", FieldName: "display_name", Value: "Alice Müller", Label: "Display Name", FormAction: "/admin/users/usr-001/display-name", CSRFToken: "tok123"}`
   - When: template partial `"inline_edit"` rendered via `renderPartial`
   - Then: output contains `aria-label="Edit Display Name"`, `name="display_name"`, `value="Alice Müller"`, `action="/admin/users/usr-001/display-name"`, `name="_csrf" value="tok123"`, well-formed HTML

2. **AlertBanner renders role=alert and correct severity class** — Go `net/http/httptest`
   - Given: `AlertBannerData{Severity: "success", Message: "User deactivated.", Dismissible: true}`
   - When: template partial `"alert_banner"` rendered
   - Then: output contains `role="alert"`, `class` includes `alert-success`, dismiss button present (`aria-label="Dismiss"`), `aria-live="polite"`

3. **AlertBanner warning uses assertive aria-live** — Go `net/http/httptest`
   - Given: `AlertBannerData{Severity: "warning", Message: "Quota nearly reached.", Dismissible: false}`
   - When: template partial `"alert_banner"` rendered
   - Then: `aria-live="assertive"` present, no dismiss button in output

4. **StatusBadge renders correct class for each status** — Go `net/http/httptest`
   - Given: three separate renders with `Status: "active"`, `Status: "inactive"`, `Status: "pending"`
   - When: template partial `"status_badge"` rendered each time
   - Then: each output contains `badge-success`, `badge-error`, and `badge-warning` respectively; `role="status"` present in each

5. **StatusBadge uses Label override when provided** — Go `net/http/httptest`
   - Given: `StatusBadgeData{Status: "active", Label: "Online"}`
   - When: rendered
   - Then: display text is "Online" (not "active")

6. **EmptyState renders heading and description** — Go `net/http/httptest`
   - Given: `EmptyStateData{Heading: "No users found", Description: "Adjust your search filters to find users."}`
   - When: template partial `"empty_state"` rendered
   - Then: output contains `<h3>` with text "No users found" and `<p>` with the description text; well-formed HTML

7. **Playwright: InlineEdit click-to-edit flow** — Playwright (`e2e/tests/features/admin/display-components.spec.ts`)
   - Given: the full dev stack running and admin logged in, navigated to a page embedding InlineEdit
   - When: Playwright clicks the edit icon button
   - Then: an `<input>` field appears with the current value pre-filled
   - Status: `test.skip` — TODO: enable in Story 7.6 (User Detail Panel embeds InlineEdit for display name)

8. **Playwright: AlertBanner dismiss button removes the alert** — Playwright
   - Given: admin navigated to a page with a dismissible AlertBanner
   - When: Playwright clicks the dismiss button
   - Then: the `[role=alert]` element is no longer visible
   - Status: `test.skip` — TODO: enable in Story 7.6 or 7.7 (first page to show flash messages)

9. **Playwright: StatusBadge visible in user list** — Playwright
   - Given: admin navigated to `/admin/users`
   - When: the page renders
   - Then: at least one `<span>` with `role="status"` and `badge-success` or `badge-error` class is visible
   - Status: `test.skip` — TODO: enable in Story 7.5 (User List page embeds StatusBadge per user)

10. **Playwright: EmptyState visible when list is empty** — Playwright
    - Given: admin navigated to `/admin/users?q=zzznoresultszzz`
    - When: the page renders with no matching users
    - Then: a `<h3>` with "No users found" text is visible
    - Status: `test.skip` — TODO: enable in Story 7.5 (User List page renders EmptyState when no results)

## Tasks / Subtasks

- [x] Task 1: Write failing Go unit tests FIRST (before implementation code) — AC: 5
  - [x] 1.1 Create `gateway/internal/admin/display_components_test.go` in `package admin`
  - [x] 1.2 Add `renderPartial` helper (already exists in `interaction_components_test.go` in the same package — do NOT redefine; it is available to all tests in `package admin`)
  - [x] 1.3 Write `TestInlineEditARIA`:
    - Data: `InlineEditData{ID: "display-name", FieldName: "display_name", Value: "Alice Müller", Label: "Display Name", FormAction: "/admin/users/usr-001/display-name", CSRFToken: "tok123"}`
    - Assert: `aria-label="Edit Display Name"`, `name="display_name"`, `value="Alice Müller"`, `action="/admin/users/usr-001/display-name"`, `value="tok123"` (CSRF), well-formed HTML via `assertWellFormed`
  - [x] 1.4 Write `TestAlertBannerDismissible`:
    - Data: `AlertBannerData{Severity: "success", Message: "User deactivated.", Dismissible: true}`
    - Assert: `role="alert"`, `alert-success` in class, `aria-label="Dismiss"` present, `aria-live="polite"`
  - [x] 1.5 Write `TestAlertBannerNonDismissible`:
    - Data: `AlertBannerData{Severity: "warning", Message: "Quota nearly reached.", Dismissible: false}`
    - Assert: `aria-live="assertive"`, no `aria-label="Dismiss"` in output
  - [x] 1.6 Write `TestStatusBadgeClasses`:
    - Three sub-tests (or table-driven): `"active"` → `badge-success`, `"inactive"` → `badge-error`, `"pending"` → `badge-warning`
    - Each assert: `role="status"` present
  - [x] 1.7 Write `TestStatusBadgeLabelOverride`:
    - Data: `StatusBadgeData{Status: "active", Label: "Online"}`
    - Assert: output contains "Online", does NOT contain `>active<` as display text (it may appear in aria-label)
  - [x] 1.8 Write `TestEmptyState`:
    - Data: `EmptyStateData{Heading: "No users found", Description: "Adjust your search filters."}`
    - Assert: `<h3>` present, "No users found" in output, "Adjust your search filters." in output, well-formed HTML
  - [x] 1.9 Confirm RED: `go test ./internal/admin/...` fails with undefined type errors (structs and templates do not exist yet)

- [x] Task 2: Write Playwright E2E spec FIRST (before implementation) — AC: 6
  - [x] 2.1 Create `e2e/tests/features/admin/display-components.spec.ts`
  - [x] 2.2 Reuse the `loginAsAdmin` helper (copy from `interaction-components.spec.ts` — same file pattern)
  - [x] 2.3 Write all 4 Playwright scenarios (Acceptance Tests 7–10) as `test.skip` with TODO comments linking to the story where each will be enabled:
    - InlineEdit: `// TODO: enable in Story 7.6`
    - AlertBanner: `// TODO: enable in Story 7.6 or 7.7`
    - StatusBadge: `// TODO: enable in Story 7.5`
    - EmptyState: `// TODO: enable in Story 7.5`

- [x] Task 3: Add Go data structs to `page_data.go` — AC: 1–4
  - [x] 3.1 Append `InlineEditData` struct:
    ```go
    // InlineEditData is passed to the inline_edit component partial (C11, Story 7.4).
    // The component renders a text display with an edit icon button that reveals an
    // inline form on click. Save submits the form via POST; Cancel restores display mode.
    // CSRFToken must be populated from the page's PageData.CSRFToken by the caller.
    // ID must be unique on the page (used for CSS toggle and ARIA correlation).
    type InlineEditData struct {
        ID         string // unique element identifier on the page (e.g. "display-name")
        FieldName  string // <input name="..."> value (e.g. "display_name")
        Value      string // current field value (pre-fills the input)
        Label      string // human-readable label for ARIA attributes (e.g. "Display Name")
        FormAction string // POST URL for the save form (e.g. "/admin/users/usr-001/display-name")
        CSRFToken  string // CSRF double-submit token (from PageData.CSRFToken)
    }
    ```
  - [x] 3.2 Append `AlertBannerData` struct:
    ```go
    // AlertBannerData is passed to the alert_banner component partial (C12, Story 7.4).
    // Severity must be one of: "info", "success", "warning", "error".
    // When Dismissible is true, an X button is rendered that removes the alert client-side.
    // aria-live is set to "assertive" for warning/error and "polite" for info/success.
    type AlertBannerData struct {
        Severity    string // "info" | "success" | "warning" | "error"
        Message     string
        Dismissible bool
    }
    ```
  - [x] 3.3 Append `StatusBadgeData` struct:
    ```go
    // StatusBadgeData is passed to the status_badge component partial (C13, Story 7.4).
    // Status drives the DaisyUI badge colour class (active→success, inactive→error, pending→warning).
    // Label overrides the display text; if empty, Status is used as the display text.
    type StatusBadgeData struct {
        Status string // "active" | "inactive" | "pending" (unknown → badge-ghost)
        Label  string // optional display text override; if empty, uses Status
    }
    ```
  - [x] 3.4 Append `EmptyStateData` struct:
    ```go
    // EmptyStateData is passed to the empty_state component partial (C14, Story 7.4).
    // Heading is rendered as <h3>; Description is rendered as <p>.
    // Both are required — empty values render an empty heading/description.
    type EmptyStateData struct {
        Heading     string
        Description string
    }
    ```

- [x] Task 4: Create Go template partials — AC: 1–4
  - [x] 4.1 Create `gateway/internal/admin/templates/components/inline_edit.html`:
    - Uses `{{ define "inline_edit" }}`
    - Outer wrapper: `<div class="inline-edit-field" id="inline-edit-{{ .ID }}">`
    - Display mode (default visible): `<span class="inline-edit-display">{{ .Value }}<button type="button" class="btn btn-ghost btn-xs ml-1" aria-label="Edit {{ .Label }}" onclick="document.getElementById('inline-edit-{{ .ID }}').classList.add('editing')">✎</button></span>`
    - Edit mode (hidden until `.editing` class added):
      ```html
      <span class="inline-edit-form hidden">
        <form method="POST" action="{{ .FormAction }}" class="flex items-center gap-2">
          <input type="hidden" name="_csrf" value="{{ .CSRFToken }}">
          <input type="text" name="{{ .FieldName }}" value="{{ .Value }}"
                 aria-label="{{ .Label }}"
                 class="input input-bordered input-sm"
                 id="inline-edit-input-{{ .ID }}">
          <button type="submit" class="btn btn-primary btn-sm">Save</button>
          <button type="button" class="btn btn-ghost btn-sm"
                  onclick="document.getElementById('inline-edit-{{ .ID }}').classList.remove('editing')">Cancel</button>
        </form>
      </span>
      ```
    - Inline style block:
      ```html
      <style>
        #inline-edit-{{ .ID }}.editing .inline-edit-display { display: none; }
        #inline-edit-{{ .ID }}.editing .inline-edit-form { display: inline-flex; }
        #inline-edit-{{ .ID }} .inline-edit-form { display: none; }
      </style>
      ```
    - **NOTE:** Using `hidden` class + CSS toggle via `.editing` parent class keeps the toggle pure CSS-driven with minimal inline JS only for the toggle itself. No framework required.

  - [x] 4.2 Create `gateway/internal/admin/templates/components/alert_banner.html`:
    - Uses `{{ define "alert_banner" }}`
    - Determine `aria-live` value via template conditional:
      - `"warning"` or `"error"` → `aria-live="assertive"`
      - `"info"` or `"success"` → `aria-live="polite"`
    - Outer div: `<div role="alert" aria-live="{{ $ariaLive }}" class="alert alert-{{ .Severity }} flex items-start gap-3">`
    - Message: `<span>{{ .Message }}</span>`
    - Dismiss button (conditional):
      ```html
      {{ if .Dismissible }}
      <button type="button" class="btn btn-ghost btn-xs ml-auto" aria-label="Dismiss"
              onclick="this.closest('[role=alert]').remove()">×</button>
      {{ end }}
      ```
    - **Go template tip:** Use `{{ $ariaLive := "polite" }}{{ if or (eq .Severity "warning") (eq .Severity "error") }}{{ $ariaLive = "assertive" }}{{ end }}` at the top of the define block to compute `$ariaLive` before the HTML.

  - [x] 4.3 Create `gateway/internal/admin/templates/components/status_badge.html`:
    - Uses `{{ define "status_badge" }}`
    - Compute badge class:
      ```
      {{ $class := "badge-ghost" }}
      {{ if eq .Status "active" }}{{ $class = "badge-success" }}{{ end }}
      {{ if eq .Status "inactive" }}{{ $class = "badge-error" }}{{ end }}
      {{ if eq .Status "pending" }}{{ $class = "badge-warning" }}{{ end }}
      ```
    - Compute display text: `{{ $text := .Status }}{{ if .Label }}{{ $text = .Label }}{{ end }}`
    - Render: `<span class="badge {{ $class }}" role="status" aria-label="{{ .Status }}">{{ $text }}</span>`

  - [x] 4.4 Create `gateway/internal/admin/templates/components/empty_state.html`:
    - Uses `{{ define "empty_state" }}`
    - Renders:
      ```html
      <div class="flex flex-col items-center justify-center py-12 text-center text-base-content/60">
        <h3 class="text-lg font-semibold mb-2">{{ .Heading }}</h3>
        <p class="text-sm max-w-sm">{{ .Description }}</p>
      </div>
      ```

- [x] Task 5: Verify no regressions
  - [x] 5.1 Run `make test-unit-go` — all existing 178 tests must pass; 6 new display component tests must pass (total ≥ 184)
  - [x] 5.2 Verify `make build-gateway` exits 0 (compilation check)
  - [x] 5.3 Document new Tailwind classes added: `inline-edit-field`, `inline-edit-display`, `inline-edit-form` (custom, not in DaisyUI — no CSS rebuild impact); `alert`, `alert-info`, `alert-success`, `alert-warning`, `alert-error` (DaisyUI built-in — likely in existing CSS); `badge`, `badge-success`, `badge-error`, `badge-warning`, `badge-ghost` (DaisyUI built-in); `flex flex-col items-center justify-center`, `text-base-content/60`, `max-w-sm` (Tailwind utilities — may need CSS rebuild if not already in existing pages)

## Dev Notes

### Template Variable Assignment in Go html/template

Go `html/template` supports variable assignment (`{{ $var := value }}`) but NOT variable reassignment via `{{ $var = newValue }}` in older Go versions. From Go 1.11+, `{{ $var = value }}` IS supported for reassignment inside the same template block. Verify via `go version` in the container — the project uses Go 1.26+, so reassignment is fully supported.

**For AlertBanner `$ariaLive`:**
```html
{{ define "alert_banner" }}
{{ $ariaLive := "polite" }}
{{ if or (eq .Severity "warning") (eq .Severity "error") }}{{ $ariaLive = "assertive" }}{{ end }}
<div role="alert" aria-live="{{ $ariaLive }}" class="alert alert-{{ .Severity }} ...">
```

**For StatusBadge `$class`:**
```html
{{ define "status_badge" }}
{{ $class := "badge-ghost" }}
{{ if eq .Status "active" }}{{ $class = "badge-success" }}{{ end }}
{{ if eq .Status "inactive" }}{{ $class = "badge-error" }}{{ end }}
{{ if eq .Status "pending" }}{{ $class = "badge-warning" }}{{ end }}
```

### InlineEdit: CSS Toggle Pattern (No Framework)

The inline_edit component uses a CSS class toggle (`editing`) on the outer wrapper `<div>` to show/hide the display span and edit form. This requires:
- `.inline-edit-display { display: inline; }` by default
- `.inline-edit-form { display: none; }` by default
- `#inline-edit-{{ .ID }}.editing .inline-edit-display { display: none; }`
- `#inline-edit-{{ .ID }}.editing .inline-edit-form { display: inline-flex; }`

The `{{ .ID }}` field makes the CSS selector unique per instance, allowing multiple InlineEdit components on the same page (e.g., Story 7.6 may have both display_name and email as InlineEdits on the user detail panel).

**Keyboard handling note:** After Save/Cancel, focus should ideally return to the edit button. This requires a small additional line in the Cancel onclick: `document.getElementById('inline-edit-{{ .ID }}').querySelector('button[aria-label^="Edit"]').focus()`. The Dev agent may choose to include this for WCAG 2.4.3 Focus Order compliance or defer to a follow-up.

### AlertBanner: Flash Message Pattern

The typical usage pattern in later stories will be:
1. Handler performs an action (e.g., deactivate user) via POST
2. Handler stores a flash message in the session cookie or redirects with a `?flash=success&msg=...` query param
3. GET handler reads the flash message, populates `AlertBannerData`, and passes it to the page template
4. Template conditionally renders `{{ if .FlashMessage }}{{ template "alert_banner" .FlashMessage }}{{ end }}`

For Story 7.4, no page embeds the component yet — it is tested in isolation only. The consuming page data structs in Stories 7.5–7.9 will embed an `*AlertBannerData` (pointer, nil means no banner) field named `Flash`.

### StatusBadge: Usage in StubUser and StubRoom

`StubUser.Status` is already `"active"` | `"deactivated"`. Note that `"deactivated"` does NOT match `"inactive"` exactly. When Story 7.5 embeds StatusBadge in the user list, it should pass `Status: "inactive"` (or adjust the badge condition) — the Dev agent must normalise the value before passing to `StatusBadgeData`. Document this mapping in the page handler.

**Recommendation:** Story 7.5 maps `StubUser.Status == "deactivated"` → `StatusBadgeData{Status: "inactive"}` when rendering the badge.

### EmptyState: Heading Level

The component uses `<h3>` because the list page's section heading is expected to be `<h2>` (e.g., "Users" in the page content area). If a page has a different heading hierarchy, the component can be adapted — but `<h3>` is the correct default for this context.

### renderPartial Helper — Already Defined

`renderPartial` and `assertWellFormed` are defined in `gateway/internal/admin/interaction_components_test.go` (package `admin`). Since all test files share `package admin`, the new `display_components_test.go` must NOT redefine them — they are already available.

### File Locations (must follow existing conventions)

| File | Description |
|---|---|
| `gateway/internal/admin/templates/components/inline_edit.html` | InlineEdit partial (C11) |
| `gateway/internal/admin/templates/components/alert_banner.html` | AlertBanner partial (C12) |
| `gateway/internal/admin/templates/components/status_badge.html` | StatusBadge partial (C13) |
| `gateway/internal/admin/templates/components/empty_state.html` | EmptyState partial (C14) |
| `gateway/internal/admin/page_data.go` | Modified — append 4 new structs (do NOT replace) |
| `gateway/internal/admin/display_components_test.go` | New Go unit tests (package admin) |
| `e2e/tests/features/admin/display-components.spec.ts` | New Playwright E2E tests (all `test.skip`) |

### Existing Component Templates — Do NOT Modify

| Template | Defines |
|---|---|
| `master_detail.html` | `"master_detail"` |
| `detail_panel.html` | `"detail_panel"` |
| `wizard_stepper.html` | `"wizard_stepper"` |
| `confirm_dialog.html` | `"confirm_dialog"` |
| `search_input.html` | `"search_input"` |
| `filter_bar.html` | `"filter_bar"` |

New components must use unique `{{ define "..." }}` names: `"inline_edit"`, `"alert_banner"`, `"status_badge"`, `"empty_state"`.

### Upcoming Stories That Consume These Components

- **Story 7.5** (User List): embeds `status_badge` per row, `empty_state` when list is empty
- **Story 7.6** (User Detail Panel): embeds `inline_edit` for display name, `alert_banner` for flash messages
- **Story 7.7** (User Role UI): embeds `alert_banner` for success/error after deactivation
- **Story 7.8** (Room List): embeds `status_badge` per row, `empty_state` when list is empty
- **Story 7.9** (Room Detail Panel): embeds `inline_edit` for room name

### What NOT to Do

- **Do NOT add HTTP routes** — this story adds only template partials and data structs; no new routes in `main.go`
- **Do NOT redefine `renderPartial` or `assertWellFormed`** — they are already in `package admin` via `interaction_components_test.go`
- **Do NOT add a `FlashAlertBannerData` wrapper struct** — stories that embed AlertBanner will use `*AlertBannerData` (pointer) directly on their page data struct
- **Do NOT implement the flash message session mechanism** — that is implemented in the consuming stories (7.5+)
- **Do NOT use a JavaScript UI framework** — all interactive behavior (inline edit toggle, dismiss) uses vanilla JS inline `onclick` handlers only

### Review Findings

- [x] [Review][Patch] Story status and task checkboxes not updated after implementation [7-4-display-components-c11-c14-inkl-inlineedit-alertbanner.md] — fixed inline: Status → review, all tasks checked, sprint-status.yaml synced
- [x] [Review][Defer] AlertBanner Playwright test body does not set up flash state before asserting [role="alert"] visibility — deferred, pre-existing; developer enabling the test in Story 7.6/7.7 must add flash-trigger step before the navigate call (acknowledged in TODO comment)

## Status Notes

Created: 2026-04-29. Stories 7.1 (Obsidian theme), 7.2 (MasterDetailLayout), and 7.3 (Interaction Components C6–C10) are done. This story introduces 4 display-only template partials with no new routes or database interactions. Playwright E2E scenarios are all `test.skip` with TODO links to the stories (7.5, 7.6, 7.7) where each component first appears in a real production page. The primary quality gate for this story is the Go unit test suite in `display_components_test.go` (6 tests expected).

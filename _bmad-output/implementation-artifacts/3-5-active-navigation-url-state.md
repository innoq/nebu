# Story 3.5: Active Navigation URL State

Status: done

## Story

As an operator,
I want the current page's nav item to be visually highlighted in the sidebar,
so that I always know where I am in the Admin UI.

## Acceptance Criteria

1. `PageData` struct in `gateway/internal/admin/page_data.go` includes `ActiveNav string` field
2. The base layout template (`templates/layouts/base.html`) compares each nav item's `data-navkey` to `.ActiveNav`; if they match, the DaisyUI `active` class is applied to the `<a>` tag
3. Each admin route handler that renders a page sets `ActiveNav` to the correct key before calling `render` (keys: `"bootstrap"`, `"dashboard"`, `"logout"`)
4. A unit test renders the base layout with `ActiveNav: "dashboard"` and asserts the dashboard `<a>` tag contains `active` while the logout `<a>` tag does not
5. A unit test renders the base layout with `ActiveNav: "bootstrap"` (and `BootstrapMode: true`) and asserts the bootstrap `<a>` tag contains `active`
6. The topbar title reflects the page title set via the `{{ block "title" . }}` slot (this is already built into the layout — no code change needed, just verify it works)
7. All existing tests continue to pass — zero regressions

## Tasks / Subtasks

- [x] Task 1: Extend `PageData` with `ActiveNav` (AC: 1)
  - [x] 1.1 In `gateway/internal/admin/page_data.go`, add `ActiveNav string` to the `PageData` struct
  - [x] 1.2 Update the struct comment to reflect the new field

- [x] Task 2: Update `templates/layouts/base.html` to apply `active` class conditionally (AC: 2)
  - [x] 2.1 For each nav `<a>` tag, add `{{ if eq .ActiveNav "navkey" }}active{{ end }}` to the class attribute
  - [x] 2.2 Bootstrap item: `{{ if eq .ActiveNav "bootstrap" }}active{{ end }}`
  - [x] 2.3 Dashboard item: `{{ if eq .ActiveNav "dashboard" }}active{{ end }}`
  - [x] 2.4 Logout item: `{{ if eq .ActiveNav "logout" }}active{{ end }}`
  - [x] 2.5 Run `make build-admin-css` after template change — the `active` class must be included in `admin.css`

- [x] Task 3: Update existing route handlers to pass `ActiveNav` (AC: 3)
  - [x] 3.1 Audit all current callers of `h.render(...)` in `gateway/internal/admin/`
  - [x] 3.2 Pass `PageData{ActiveNav: "correct-key", BootstrapMode: ...}` in each render call

- [x] Task 4: Write unit tests (AC: 4, 5, 7)
  - [x] 4.1 In `gateway/internal/admin/handler_test.go`, add `TestActiveNav` test
  - [x] 4.2 Test case: `ActiveNav: "dashboard"` → dashboard `<a>` contains `class="... active ..."`, logout `<a>` does not contain `active`
  - [x] 4.3 Test case: `ActiveNav: "bootstrap"` with `BootstrapMode: true` → bootstrap `<a>` contains `active`
  - [x] 4.4 Test case: `ActiveNav: ""` (empty) → no nav item has `active` class
  - [x] 4.5 Run `make test-unit-go` → all tests green, zero regressions

## Dev Notes

### CRITICAL: `PageData` Extension — Backward Compatibility

`PageData` in `gateway/internal/admin/page_data.go` currently only has `BootstrapMode bool`. Add `ActiveNav string` — Go zero value (`""`) is safe: when `ActiveNav` is empty, `eq .ActiveNav "dashboard"` evaluates false for all nav keys, so no item gets the `active` class. Existing render calls passing `PageData{BootstrapMode: true/false}` will continue to work with zero regressions.

```go
// gateway/internal/admin/page_data.go — UPDATED
package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// ActiveNav identifies the current page for nav highlight (keys: "bootstrap", "dashboard", "logout").
// Extended by Story 3.13 with status fields.
type PageData struct {
    BootstrapMode bool
    ActiveNav     string
}
```

### CRITICAL: DaisyUI `active` Class — How It Works

In DaisyUI `menu` component, the `active` class on an `<a>` element applies the active state styling (primary color background/text). Since the sidebar uses plain `<ul>`/`<li>`/`<a>` (not the DaisyUI `menu` component class), the `active` class from DaisyUI's menu context may not apply automatically.

**Correct approach for this project:** The sidebar nav items already have Tailwind classes. Add the `active` class as a DaisyUI utility that applies `bg-primary text-primary-content` on the `<a>`. Check if DaisyUI's `active` modifier works on plain `<a>` elements in this theme.

**Safe fallback:** Use explicit Tailwind classes for the active state instead of relying on DaisyUI's `active` class magic:

```html
<a href="/admin/dashboard"
   data-navkey="dashboard"
   class="flex items-center gap-2 px-3 py-3 rounded text-sm font-medium
          {{ if eq .ActiveNav "dashboard" }}bg-primary text-primary-content{{ else }}text-base-content hover:bg-base-300{{ end }}
          focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200">
  Dashboard
</a>
```

This approach is **explicit and safe** — it avoids relying on DaisyUI component internals while still applying the correct Obsidian theme colors (`bg-primary = #f97316` orange, `text-primary-content = #fff7ed`).

**However:** The AC says "the DaisyUI `active` class is applied". Use both — add `active` as a literal class AND the explicit Tailwind override:

```html
class="flex items-center gap-2 px-3 py-3 rounded text-sm font-medium
       {{ if eq .ActiveNav "dashboard" }}active bg-primary text-primary-content{{ else }}text-base-content hover:bg-base-300{{ end }}
       focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200"
```

This satisfies the AC (DaisyUI `active` class is present) and ensures correct colors via explicit Tailwind classes.

### CRITICAL: `make build-admin-css` Required After Template Change

The `admin.css` is compiled by Tailwind from a scan of `templates/**/*.html`. The classes `active`, `bg-primary`, `text-primary-content` must appear in the compiled output. Run `make build-admin-css` after modifying `base.html`. Commit the updated `admin.css`.

Architecture rule #9 applies: the `admin.css` binary artifact must be committed. Do NOT skip the rebuild.

### Template Pattern — Go `html/template` Conditional Class

In Go's `html/template`, inline conditionals inside `class="..."` attributes work correctly. Use this pattern:

```html
class="base-classes {{ if eq .ActiveNav "navkey" }}active bg-primary text-primary-content{{ else }}text-base-content hover:bg-base-300{{ end }} focus-classes"
```

Note: Go templates output the result directly — whitespace from the `{{ if }}` block is preserved. Multiple spaces in class lists are fine for browsers; Tailwind only needs the class names to appear in the source for purging.

### Audit of Current `render` Callers

After Story 3.4, the existing route handlers that call `render` are:

| File | Handler | Current render call | Required `ActiveNav` |
|------|---------|---------------------|----------------------|
| `gateway/internal/admin/bootstrap.go` | `Handler` | Returns JSON currently — check if it renders HTML | `"bootstrap"` if/when it renders HTML |

The bootstrap handler (`bootstrap.go`) from Story 2.16 currently returns JSON. It does NOT render templates. No render call exists yet. Therefore **no existing handler needs updating for AC #3** in this story — the story only sets up the mechanism. Future stories (3.7 Bootstrap UI, 3.13 Dashboard) will use `ActiveNav` when they add page renders.

**Exception:** The smoke test in `handler_test.go` line 26 calls `h.render(w, "base", nil)`. This passes `nil` as data — with the updated template using `.ActiveNav`, accessing `.ActiveNav` on a nil data value will cause a template error. Check if Go's `html/template` panics on nil data with field access.

**Fix for nil data regression:** The test calls `h.render(w, "base", nil)` which currently works because `base.html` only uses `.BootstrapMode` (accessed safely via `{{ if .BootstrapMode }}`). With `{{ if eq .ActiveNav "dashboard" }}`, a nil data pointer will cause `template: base:XX:XX: executing "base" at <.ActiveNav>: nil pointer evaluating interface {}.ActiveNav`.

**Required fix:** Update the existing test `{"valid template", "base", http.StatusOK, true}` to pass `PageData{}` instead of `nil`:

```go
// handler_test.go — fix for nil data
h.render(w, tc.tmplName, PageData{})
// instead of:
h.render(w, tc.tmplName, nil)
```

This is a **critical regression fix** required by this story.

### Unit Test Pattern

```go
func TestActiveNav(t *testing.T) {
    h, err := NewTemplateHandler()
    if err != nil {
        t.Fatalf("NewTemplateHandler: %v", err)
    }

    t.Run("dashboard active when ActiveNav=dashboard", func(t *testing.T) {
        w := httptest.NewRecorder()
        h.render(w, "base", PageData{ActiveNav: "dashboard"})
        body := w.Body.String()
        // Find dashboard anchor and check it has active class
        // Find logout anchor and check it does NOT have active class
        if !strings.Contains(body, `data-navkey="dashboard"`) {
            t.Fatal("dashboard nav item not found")
        }
        // Extract the dashboard anchor block and check for active
        dashIdx := strings.Index(body, `data-navkey="dashboard"`)
        // The active class should appear near the navkey attribute
        dashBlock := body[max(0, dashIdx-200):min(len(body), dashIdx+200)]
        if !strings.Contains(dashBlock, "active") {
            t.Error("dashboard nav item should have active class when ActiveNav=dashboard")
        }
        // Logout should not be active
        logoutIdx := strings.Index(body, `data-navkey="logout"`)
        logoutBlock := body[max(0, logoutIdx-200):min(len(body), logoutIdx+200)]
        if strings.Contains(logoutBlock, "active") {
            t.Error("logout nav item should NOT have active class when ActiveNav=dashboard")
        }
    })

    t.Run("bootstrap active when ActiveNav=bootstrap and BootstrapMode=true", func(t *testing.T) {
        w := httptest.NewRecorder()
        h.render(w, "base", PageData{ActiveNav: "bootstrap", BootstrapMode: true})
        body := w.Body.String()
        bootIdx := strings.Index(body, `data-navkey="bootstrap"`)
        if bootIdx < 0 {
            t.Fatal("bootstrap nav item not found")
        }
        bootBlock := body[max(0, bootIdx-200):min(len(body), bootIdx+200)]
        if !strings.Contains(bootBlock, "active") {
            t.Error("bootstrap nav item should have active class when ActiveNav=bootstrap")
        }
    })

    t.Run("no active when ActiveNav empty", func(t *testing.T) {
        w := httptest.NewRecorder()
        h.render(w, "base", PageData{ActiveNav: ""})
        body := w.Body.String()
        // active class should not appear in any nav item context
        // (it may appear elsewhere in page, so check near navkey attributes)
        for _, key := range []string{"dashboard", "logout"} {
            idx := strings.Index(body, `data-navkey="`+key+`"`)
            if idx < 0 {
                continue
            }
            block := body[max(0, idx-200):min(len(body), idx+200)]
            if strings.Contains(block, "active") {
                t.Errorf("nav item %q should NOT have active class when ActiveNav is empty", key)
            }
        }
    })
}

func max(a, b int) int {
    if a > b {
        return a
    }
    return b
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

Note: `min`/`max` are built-in since Go 1.21. If Go version is 1.21+, the helper functions above are not needed. Check `go.mod` — if Go >= 1.21, remove the helper functions.

### Check `go.mod` for Go Version

The project uses Go 1.26+ (per CLAUDE.md). Go 1.21 introduced built-in `min`/`max`. At Go 1.26 the helpers are not needed — just use `min(a, b)` and `max(a, b)` directly.

### Obsidian Color Mapping for Active State

From `tailwind.config.js`:
- `primary = #f97316` (orange) — active nav item background
- `primary-content = #fff7ed` — text on primary background
- `base-300 = #374151` — hover state for inactive items

Active nav item visual spec:
- Background: `bg-primary` (`#f97316`)
- Text: `text-primary-content` (`#fff7ed`)
- No hover effect on active item (hover state is irrelevant when active)

### Accessibility — Active Nav Item

Per WCAG 2.1 AA and UX-DR3:
- Active nav item must have visual distinction beyond color alone (pattern from UX spec: `left-border 3px primary` for C1 cards — consider also adding `font-semibold` for active nav items as secondary indicator)
- Screen readers: use `aria-current="page"` on the active `<a>` tag — this is the correct semantic indicator for "current page" in navigation

Add `{{ if eq .ActiveNav "dashboard" }}aria-current="page"{{ end }}` to each nav item.

```html
<a href="/admin/dashboard"
   data-navkey="dashboard"
   {{ if eq .ActiveNav "dashboard" }}aria-current="page"{{ end }}
   class="...{{ if eq .ActiveNav "dashboard" }}active bg-primary text-primary-content font-semibold{{ else }}text-base-content hover:bg-base-300{{ end }}...">
  Dashboard
</a>
```

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/page_data.go` | MODIFY — add `ActiveNav string` to `PageData` |
| `gateway/internal/admin/templates/layouts/base.html` | MODIFY — add conditional `active` class and `aria-current` to nav items |
| `gateway/internal/admin/handler_test.go` | MODIFY — fix nil→PageData{} regression + add `TestActiveNav` |
| `gateway/internal/admin/static/admin.css` | MODIFY — rebuild with `make build-admin-css` after template change |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — render signature and WalkDir parsing unchanged
- `gateway/internal/admin/static.go` — font/CSS serving unchanged
- `gateway/internal/admin/bootstrap.go` — returns JSON, no template render call yet
- `gateway/cmd/gateway/main.go` — no new routes, no changes
- `gateway/internal/admin/tailwind.config.js` — Obsidian theme config unchanged
- `gateway/internal/admin/tailwind.input.css` — `@font-face` unchanged

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.5] Authoritative ACs (lines 1553–1568)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.7] Story 3.7 sets `ActiveNav: "bootstrap"` — proof the mechanism must work
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.13] Story 3.13 sets `ActiveNav: "dashboard"`
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Typography-System] `color-primary = #f97316` for active nav items
- [Source: gateway/internal/admin/page_data.go] Current struct — add `ActiveNav string`
- [Source: gateway/internal/admin/templates/layouts/base.html] Current nav implementation — `data-navkey` attributes already in place
- [Source: gateway/internal/admin/handler_test.go:26] `h.render(w, tc.tmplName, nil)` → must be changed to `PageData{}` to prevent nil pointer error
- [Source: gateway/internal/admin/tailwind.config.js] `primary=#f97316`, `primary-content=#fff7ed`, `base-300=#374151`
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement] Rule #9: compiled `admin.css` must be committed

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None.

### Completion Notes List

- Task 1: `PageData` in `page_data.go` um `ActiveNav string` erweitert. Struct-Kommentar aktualisiert.
- Task 2: `base.html` aktualisiert — alle drei Nav-Items (bootstrap, dashboard, logout) erhalten bedingte `active bg-primary text-primary-content font-semibold`-Klassen und `aria-current="page"`-Attribut. `make build-admin-css` erfolgreich ausgeführt, `admin.css` wurde neu gebaut.
- Task 3: Audit ergab keine bestehenden HTML-render-Aufrufe in Handlern (bootstrap.go gibt JSON zurück). Kritischer Regression-Fix: Bestehender Test in `handler_test.go` nutzte `nil` als PageData — auf `PageData{}` geändert, um nil-pointer bei `{{ if eq .ActiveNav "..." }}`-Auswertung zu verhindern.
- Task 4: `TestActiveNav` mit drei Test-Cases hinzugefügt (dashboard active, bootstrap active mit BootstrapMode=true, leer = kein active). Alle 11 Tests in `internal/admin` grün. Keine Regressions. Go 1.26 built-in `min`/`max` genutzt (keine eigenen Hilfsfunktionen).
- AC 6 (topbar title via `{{ block "title" . }}`): Bereits implementiert in base.html — kein Code-Change nötig, verifiziert.

### File List

- `gateway/internal/admin/page_data.go` — MODIFIED: `ActiveNav string` zu `PageData` hinzugefügt
- `gateway/internal/admin/templates/layouts/base.html` — MODIFIED: Bedingte `active`-Klasse und `aria-current="page"` für alle drei Nav-Items
- `gateway/internal/admin/handler_test.go` — MODIFIED: nil→PageData{} Regression-Fix + `TestActiveNav` hinzugefügt
- `gateway/internal/admin/static/admin.css` — MODIFIED: Neu gebaut via `make build-admin-css` mit neuen Tailwind-Klassen

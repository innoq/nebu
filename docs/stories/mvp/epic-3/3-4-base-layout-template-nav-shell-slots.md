# Story 3.4: Base Layout Template (Nav, Shell, Slots)

Status: done

## Story

As an operator,
I want a consistent shell layout (topbar, sidebar nav, content area) for all Admin UI pages,
so that every page looks coherent without duplicating markup.

## Acceptance Criteria

1. `gateway/internal/admin/templates/layouts/base.html` defines blocks: `{{ block "title" . }}`, `{{ block "content" . }}`, `{{ block "scripts" . }}`
2. The layout includes a topbar (logo + `TopbarStatusIndicator` C3 placeholder: amber dot "Connecting..." on initial load), a left sidebar nav, and a main content area
3. Sidebar nav items: **Bootstrap** (shown only when server is in bootstrap mode via `.BootstrapMode` data field), **Dashboard**, **Logout**
4. Each nav item renders an `<a>` tag with `href` and a `data-navkey` attribute matching its route key (e.g., `data-navkey="dashboard"`)
5. The layout loads `/admin/static/admin.css` via `<link>` in `<head>`; fonts are delivered via `@font-face` in that CSS (no separate `<link>` needed for font files)
6. The `{{ block "scripts" . }}` slot is empty by default; pages that need Vue.js append their script tags here
7. All HTML is valid: no unclosed tags, no duplicate IDs, semantically correct (`<nav>`, `<main>`, `<header>` landmarks)
8. A smoke test renders the base layout with empty content and asserts the topbar and sidebar are present in the output
9. `TemplateHandler.NewTemplateHandler()` successfully parses the new template structure (including subdirectory `layouts/`)

## Tasks / Subtasks

- [x] Task 1: Create `gateway/internal/admin/templates/layouts/base.html` (AC: 1, 2, 3, 4, 5, 6, 7)
  - [x] 1.1 Create directory `gateway/internal/admin/templates/layouts/`
  - [x] 1.2 Write `base.html` with full shell: `<html data-theme="obsidian">`, `<head>` with CSS link, `<header>` topbar, `<nav>` sidebar, `<main>` content area
  - [x] 1.3 Add `{{ block "title" . }}Nebu Admin{{ end }}` in `<title>` tag
  - [x] 1.4 Add `{{ block "content" . }}{{ end }}` inside `<main>`
  - [x] 1.5 Add `{{ block "scripts" . }}{{ end }}` before closing `</body>`
  - [x] 1.6 Add topbar with Nebu logo text and `TopbarStatusIndicator` C3 placeholder (amber dot + "Connecting..." span with `aria-live="polite"`)
  - [x] 1.7 Add sidebar nav with Bootstrap item (conditional: `{{ if .BootstrapMode }}`), Dashboard item, and Logout item, each with `data-navkey` attribute and correct `href`
  - [x] 1.8 Apply DaisyUI + Obsidian Tailwind classes: sidebar `bg-base-200`, topbar `bg-base-300`, main `bg-base-100`

- [x] Task 2: Update `TemplateHandler` embed directive and template parsing (AC: 9)
  - [x] 2.1 In `gateway/internal/admin/handler.go`, change `//go:embed templates` to keep it as-is (Go embeds recursively without `**`); verify the embed also captures `layouts/`
  - [x] 2.2 Update `template.ParseFS` pattern from `"templates/*.html"` to `"templates/**/*.html"` — or use `fs.Glob` + `template.ParseFS` with the full recursive pattern so `layouts/base.html` is parsed
  - [x] 2.3 Verify `NewTemplateHandler()` succeeds with the updated pattern (add a test if needed)

- [x] Task 3: Define `PageData` struct (AC: 3, 8)
  - [x] 3.1 In `gateway/internal/admin/handler.go` (or a new `gateway/internal/admin/page_data.go`), define `PageData` struct with at minimum `BootstrapMode bool`
  - [x] 3.2 The struct must be exported for use in route handlers; keep it in `package admin`

- [x] Task 4: Update `base.html` placeholder to use the new layout (AC: 9)
  - [x] 4.1 Remove (or repurpose) the old `gateway/internal/admin/templates/base.html` placeholder from Story 3.1 — it conflicts with the existing test `{"valid template", "base", ...}` — see critical note below
  - [x] 4.2 Decide: either keep `templates/base.html` as a stub that wraps the layout, OR update `handler_test.go` to use the correct new template name

- [x] Task 5: Write unit tests (AC: 8)
  - [x] 5.1 In `gateway/internal/admin/handler_test.go`, add `TestBaseLayout` test
  - [x] 5.2 Test: render layout with `PageData{BootstrapMode: false}` → output contains `<header`, `<nav`, `<main`
  - [x] 5.3 Test: render layout with `PageData{BootstrapMode: true}` → output contains `data-navkey="bootstrap"`
  - [x] 5.4 Test: render layout with `PageData{BootstrapMode: false}` → output does NOT contain `data-navkey="bootstrap"`
  - [x] 5.5 Run `make test-unit-go` — all tests green, zero regressions

## Dev Notes

### CRITICAL: Template Parsing Must Include Subdirectory `layouts/`

The current `handler.go` uses `template.ParseFS(adminFS, "templates/*.html")`. The glob `templates/*.html` does NOT match `templates/layouts/base.html`. Change the pattern to use recursive glob or `fs.WalkDir`:

**Option A — Recursive glob pattern (recommended):**
```go
tmpl, err := template.ParseFS(adminFS, "templates/**/*.html", "templates/*.html")
```
Note: Go's `template.ParseFS` uses `path.Match` semantics. `**` is NOT a standard glob in Go's `path.Match` — it only matches one path segment. To match all `.html` files recursively, use `fs.Glob` from the `io/fs` package or enumerate patterns explicitly:

```go
// gateway/internal/admin/handler.go — MODIFIED
package admin

import (
    "embed"
    "html/template"
    "io/fs"
    "log/slog"
    "net/http"
)

//go:embed templates
var adminFS embed.FS

type TemplateHandler struct {
    tmpl *template.Template
}

func NewTemplateHandler() (*TemplateHandler, error) {
    // fs.Glob with pattern "templates/**/*.html" does not work in Go stdlib.
    // Use fs.WalkDir to collect all .html files recursively.
    var patterns []string
    err := fs.WalkDir(adminFS, "templates", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if !d.IsDir() && filepath.Ext(path) == ".html" {
            patterns = append(patterns, path)
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    tmpl, err := template.ParseFS(adminFS, patterns...)
    if err != nil {
        return nil, err
    }
    return &TemplateHandler{tmpl: tmpl}, nil
}
```

**Important:** Add `"io/fs"` and `"path/filepath"` imports. The `//go:embed templates` directive (no glob) recursively embeds the entire `templates/` directory tree including `layouts/` — no change needed to the embed directive itself.

**Option B — explicit subdirectory patterns:**
```go
tmpl, err := template.ParseFS(adminFS, "templates/*.html", "templates/layouts/*.html")
```
This is simpler but requires updating the pattern for every new subdirectory added in future stories. Option A (WalkDir) scales automatically.

### CRITICAL: Existing Test Uses Template Name `"base"`

`handler_test.go` line 21 has: `{"valid template", "base", http.StatusOK, true}`. The template name `"base"` matches `{{ define "base" }}` in the current `templates/base.html`. 

After Story 3.4 replaces the layout, there are two approaches:
1. **Keep `templates/base.html`** — rename the `{{ define ... }}` block to a different name (e.g., `{{ define "base" }}` remains as a thin wrapper that calls `templates/layouts/base.html`). But Go templates don't support this pattern cleanly.
2. **Replace `templates/base.html`** — change the test to use the new layout template name (e.g., `"layouts/base"`) and update the render call in the test.

**Recommended:** Rename `templates/base.html` to `templates/layouts/base.html`. Update `handler_test.go` to call `h.render(w, "layouts/base", ...)` instead of `h.render(w, "base", ...)`. The template name is derived from the `{{ define "name" }}` block, NOT the filename — so the new `layouts/base.html` should use `{{ define "layouts/base" }}` OR use the simpler name `{{ define "base" }}` and keep the test using `"base"`. The simplest path: keep `{{ define "base" }}` inside `layouts/base.html` and keep the test as-is; delete `templates/base.html`.

### CRITICAL: `data-theme="obsidian"` on `<html>`

The DaisyUI Obsidian theme is activated by `data-theme="obsidian"` on the `<html>` element. This was prepared in Story 3.2 (tailwind.config.js declares `themes: [{ obsidian: {...} }]`). Without this attribute, DaisyUI uses the default light theme and all colors will be wrong.

```html
<html lang="en" data-theme="obsidian">
```

This is not optional — the entire Obsidian color system depends on it.

### CRITICAL: `@font-face` is in CSS — No Separate Font `<link>` Needed

Story 3.3 embedded `@font-face` rules directly into `admin.css`. The layout template only needs:
```html
<link rel="stylesheet" href="/admin/static/admin.css">
```
Do NOT add `<link>` tags for individual font files — fonts load automatically via `@font-face` in the CSS. The epics file AC #5 says "loads `/admin/static/fonts/inter.woff2`" — this refers to the CSS loading the font via `@font-face`, not a direct `<link>` tag.

### Layout Structure (from UX-DR3)

```
┌─────────────────────────────────────────────────────┐
│  <header> Top Bar (48px) — bg-base-300              │
│  Logo "Nebu" + TopbarStatusIndicator C3             │
├──────────────┬──────────────────────────────────────┤
│  <nav>       │  <main>                               │
│  Sidebar     │  Content Area                         │
│  (240px)     │  {{ block "content" . }}              │
│  bg-base-200 │  bg-base-100                          │
│              │                                       │
└──────────────┴──────────────────────────────────────┘
```

- Sidebar: Fixed left, 240px, `bg-base-200`
- Topbar: Fixed top, 48px, `bg-base-300`
- Content: Fluid, `p-8`, `max-w-screen-xl`
- Nav item height: 48px

### Base Layout Template — Reference Implementation

```html
{{ define "base" }}<!DOCTYPE html>
<html lang="en" data-theme="obsidian">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ block "title" . }}Nebu Admin{{ end }}</title>
  <link rel="stylesheet" href="/admin/static/admin.css">
</head>
<body class="bg-base-100 text-base-content min-h-screen flex flex-col">

  <!-- Topbar -->
  <header class="h-12 bg-base-300 flex items-center justify-between px-4 shrink-0">
    <span class="font-semibold text-base-content">Nebu Admin</span>
    <!-- C3: TopbarStatusIndicator — placeholder; Story 3.13 wires real status -->
    <span id="topbar-status" aria-live="polite" aria-label="System status: connecting"
          class="flex items-center gap-1 text-sm text-warning">
      <span class="w-2 h-2 rounded-full bg-warning inline-block" aria-hidden="true"></span>
      Connecting&hellip;
    </span>
  </header>

  <!-- Shell: sidebar + content -->
  <div class="flex flex-1 overflow-hidden">

    <!-- Sidebar Nav -->
    <nav aria-label="Admin navigation"
         class="w-60 bg-base-200 flex flex-col shrink-0 border-r border-base-300">
      <ul role="list" class="flex flex-col gap-1 p-2 flex-1">
        {{ if .BootstrapMode }}
        <li>
          <a href="/admin/bootstrap"
             data-navkey="bootstrap"
             class="flex items-center gap-2 px-3 py-3 rounded text-sm font-medium
                    text-base-content hover:bg-base-300 focus:outline-none
                    focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200">
            Bootstrap Setup
          </a>
        </li>
        {{ end }}
        <li>
          <a href="/admin/dashboard"
             data-navkey="dashboard"
             class="flex items-center gap-2 px-3 py-3 rounded text-sm font-medium
                    text-base-content hover:bg-base-300 focus:outline-none
                    focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200">
            Dashboard
          </a>
        </li>
        <li>
          <a href="/admin/auth/logout"
             data-navkey="logout"
             class="flex items-center gap-2 px-3 py-3 rounded text-sm font-medium
                    text-base-content hover:bg-base-300 focus:outline-none
                    focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200">
            Logout
          </a>
        </li>
      </ul>
    </nav>

    <!-- Main Content -->
    <main class="flex-1 overflow-auto p-8 max-w-screen-xl">
      {{ block "content" . }}{{ end }}
    </main>

  </div>

  {{ block "scripts" . }}{{ end }}
</body>
</html>{{ end }}
```

**Class choices explained:**
- `data-theme="obsidian"` activates the DaisyUI Obsidian theme (non-negotiable — see critical note above)
- `bg-base-100` / `bg-base-200` / `bg-base-300` map to Obsidian palette (`#111827` / `#1f2937` / `#374151`)
- `text-base-content` → `#f9fafb` (white text on dark background)
- `text-warning` + `bg-warning` → amber `#f59e0b` for initial "Connecting..." state
- `focus:ring-2 focus:ring-primary focus:ring-offset-2` — WCAG 2.1 AA focus indicators (required)
- `aria-live="polite"` on status indicator — required for WCAG screen reader support
- `role="list"` on `<ul>` — required when list styling is removed (WCAG pattern)

### `PageData` Struct

Define a `PageData` struct for all template renders. This is the minimum for Story 3.4; Story 3.5 extends it with `ActiveNav string`:

```go
// gateway/internal/admin/page_data.go (NEW FILE)
package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// Extended by Story 3.5 with ActiveNav, Story 3.13 with status fields.
type PageData struct {
    BootstrapMode bool
}
```

Place this in its own file to keep `handler.go` focused on the template engine mechanics. This also allows Stories 3.5, 3.13 etc. to extend it without touching the render logic.

### `render` Method Signature — Stay Backward Compatible

The existing `render` method signature is:
```go
func (h *TemplateHandler) render(w http.ResponseWriter, name string, data any)
```

Do NOT change this signature. Pass `PageData` as the `data any` parameter when calling render:
```go
h.render(w, "base", PageData{BootstrapMode: false})
```

### Route — No New Route Needed

Story 3.4 is a template/data change, not a route change. The existing route `GET /admin/bootstrap` (registered in `main.go`) already calls `bootstrapHandler.Handler`. That handler currently returns JSON — it is NOT changed in this story. The base layout template is used by page handlers created in later stories (3.7, 3.13 etc.), not wired to routes in this story.

**What IS wired**: `main.go` already has `TemplateHandler` created via `NewTemplateHandler()` (from Story 3.1). Verify `NewTemplateHandler()` still constructs successfully after the template structure change (the unit test covers this).

### Template Name Convention

Go's `html/template` identifies templates by the name in `{{ define "name" }}`, not by filename. Use `{{ define "base" }}` inside `layouts/base.html` so the existing test `h.render(w, "base", ...)` continues to work without changes. This is the least-friction path and is consistent with how Story 3.1 structured the placeholder.

### Accessibility Requirements (WCAG 2.1 AA)

Per UX spec NFR-A1–A3 and UX-DR3:
- `<header>` → topbar landmark
- `<nav aria-label="Admin navigation">` → nav landmark with label (multiple navs on page need distinct `aria-label`)
- `<main>` → main content landmark
- `<ul role="list">` → required when Tailwind removes default `list-style` (VoiceOver on Safari)
- Tab order: sidebar nav → content area → scripts area (natural DOM order)
- Focus ring: `focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200` on all interactive elements
- Minimum font size: all nav items use `text-sm` (14px) — meets minimum

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/templates/layouts/base.html` | CREATE — full nav shell |
| `gateway/internal/admin/templates/base.html` | REPLACE/DELETE — old placeholder; content moved to `layouts/base.html` |
| `gateway/internal/admin/handler.go` | MODIFY — update `ParseFS` to parse subdirectories recursively |
| `gateway/internal/admin/page_data.go` | CREATE — `PageData` struct |
| `gateway/internal/admin/handler_test.go` | MODIFY — add `TestBaseLayout` test cases |

**Do NOT modify:**
- `gateway/internal/admin/static.go` — font/CSS serving unchanged
- `gateway/internal/admin/static_test.go` — no changes
- `gateway/internal/admin/bootstrap.go` — handler logic unchanged (still returns JSON)
- `gateway/cmd/gateway/main.go` — no new routes needed for this story
- `gateway/internal/admin/tailwind.config.js` — theme config unchanged
- `gateway/internal/admin/tailwind.input.css` — `@font-face` blocks unchanged
- `gateway/internal/admin/static/admin.css` — DO NOT manually edit; rebuild with `make build-admin-css` if new Tailwind classes are added

### Tailwind CSS Class Availability

The `admin.css` is compiled from template content scans. If the base layout uses DaisyUI or Tailwind classes that aren't in any currently scanned template file, `make build-admin-css` must be re-run to include them in the output CSS. The dev agent MUST run `make build-admin-css` after creating the template to regenerate `admin.css` with all new classes.

Architecture rule #9 is still in force: the updated `admin.css` must be committed (it is the binary artifact embedded at build time).

### Story 3.5 Awareness (Do NOT implement now)

Story 3.5 adds `ActiveNav string` to `PageData` and adds conditional `active` class to matching nav items in `base.html`. Design the nav items so adding `{{ if eq .ActiveNav .NavKey }}active{{ end }}` class is trivial. The `data-navkey` attribute is set now (this story) and read by Story 3.5.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.4] Authoritative ACs (lines 1532–1550)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.5] Story 3.5 `ActiveNav` field — design `PageData` to extend cleanly
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Layout-Struktur] 240px sidebar, 48px topbar, fluid main, Obsidian theme tokens
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#C3-TopbarStatusIndicator] Amber dot "Connecting..." placeholder
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Accessibility] WCAG 2.1 AA: landmarks, focus rings, aria-live
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement] Rule #9: go:embed mandatory, no runtime filesystem access
- [Source: gateway/internal/admin/handler.go] Current template parsing: `template.ParseFS(adminFS, "templates/*.html")` — MUST be updated
- [Source: gateway/internal/admin/handler_test.go] Existing test uses template name `"base"` — keep `{{ define "base" }}` in new layout
- [Source: gateway/internal/admin/tailwind.config.js] Obsidian theme: `base-100=#111827`, `base-200=#1f2937`, `base-300=#374151`, `primary=#f97316`, `warning=#f59e0b`
- [Source: gateway/internal/admin/static/admin.css] Compiled from Story 3.3; `@font-face` already embedded — no separate font `<link>` in HTML needed
- [Source: gateway/cmd/gateway/main.go:21–27] `NewTemplateHandler()` called at startup; must succeed after template restructure

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None.

### Completion Notes List

- Implementierung abgeschlossen am 2026-03-31 (claude-sonnet-4-6[1m])
- `templates/layouts/base.html` erstellt mit vollständigem Shell-Layout: `<html data-theme="obsidian">`, Topbar (48px, bg-base-300), Sidebar-Nav (240px, bg-base-200), Content-Area (bg-base-100)
- Alle drei Template-Blöcke implementiert: `{{ block "title" }}`, `{{ block "content" }}`, `{{ block "scripts" }}`
- TopbarStatusIndicator C3-Placeholder (Amber-Dot + "Connecting...") mit `aria-live="polite"` und WCAG-konformen Accessibility-Attributen
- Sidebar: Bootstrap-Nav-Item (bedingt via `.BootstrapMode`), Dashboard, Logout — alle mit `data-navkey`-Attributen
- Altes `templates/base.html`-Placeholder gelöscht; neues `layouts/base.html` behält `{{ define "base" }}` für Rückwärtskompatibilität mit bestehendem Test
- `handler.go` auf `fs.WalkDir`-basiertes rekursives Template-Parsing umgestellt (Option A aus Dev Notes)
- `page_data.go` mit exportiertem `PageData`-Struct angelegt
- `TestBaseLayout` mit 3 Test-Cases hinzugefügt (Landmarks, BootstrapMode=true, BootstrapMode=false)
- `make build-admin-css` ausgeführt — neue DaisyUI/Tailwind-Klassen (bg-base-200, bg-base-300, text-warning, bg-warning, w-60, shrink-0, overflow-hidden) sind in admin.css
- Alle Tests grün, keine Regressionen (11 Packages, race detector aktiv)

### File List

- `gateway/internal/admin/templates/layouts/base.html` (CREATED)
- `gateway/internal/admin/templates/base.html` (DELETED)
- `gateway/internal/admin/handler.go` (MODIFIED — rekursives Template-Parsing via fs.WalkDir)
- `gateway/internal/admin/page_data.go` (CREATED)
- `gateway/internal/admin/handler_test.go` (MODIFIED — TestBaseLayout hinzugefügt)
- `gateway/internal/admin/static/admin.css` (MODIFIED — via make build-admin-css neu generiert)

## Change Log

| Date | Change |
|------|--------|
| 2026-03-31 | Story implementiert: Base Layout Template mit Topbar, Sidebar-Nav, Content-Area; PageData-Struct; rekursives Template-Parsing; TestBaseLayout; admin.css neu generiert |
| 2026-03-31 | Code Review bestanden (claude-opus-4-6): 1 MINOR gefixt (filepath.Ext → path.Ext fuer embed.FS-Kompatibilitaet), 0 MAJOR. Alle ACs verifiziert, alle Tests gruen. |

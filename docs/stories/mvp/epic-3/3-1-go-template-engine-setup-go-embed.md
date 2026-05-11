# Story 3.1: Go Template Engine Setup + go:embed

Status: done

## Story

As an operator,
I want the Admin UI to be served as pre-rendered HTML from the Go gateway binary,
so that no separate frontend server is needed and the binary is fully self-contained.

## Acceptance Criteria

1. `gateway/internal/admin/` package exists with a `Handler` struct that embeds templates via `//go:embed templates/**/*`
2. Templates live under `gateway/internal/admin/templates/`; the embed directive compiles them into the binary at build time
3. A `render(w http.ResponseWriter, name string, data any)` helper executes the named template and writes the response; it sets `Content-Type: text/html; charset=utf-8`
4. If template execution fails, it writes `500 Internal Server Error` (no panic)
5. `go build ./...` succeeds with all embedded files present
6. A unit test verifies that `render()` with a valid template name produces non-empty HTML output

## Tasks / Subtasks

- [x] Task 1: Create `gateway/internal/admin/handler.go` (AC: 1, 2, 3, 4)
  - [x] 1.1 Declare `//go:embed templates` directive and `var adminFS embed.FS` at package level
  - [x] 1.2 Define `TemplateHandler` struct that parses all templates from `adminFS` on construction via `template.ParseFS`
  - [x] 1.3 Implement `render(w http.ResponseWriter, name string, data any)` method: set `Content-Type: text/html; charset=utf-8`, execute named template, write 500 on execution error (no panic, no stack trace)
  - [x] 1.4 Export `NewTemplateHandler() (*TemplateHandler, error)` constructor that parses all templates from the embedded FS

- [x] Task 2: Create placeholder template (AC: 2, 5)
  - [x] 2.1 Create `gateway/internal/admin/templates/base.html` — minimal skeleton (`<!DOCTYPE html><html>...`) with `{{ define "base" }}` block; will be replaced in Story 3.4
  - [x] 2.2 Verify `go build ./...` succeeds from `gateway/` directory

- [x] Task 3: Write unit tests (AC: 6)
  - [x] 3.1 Create `gateway/internal/admin/handler_test.go` in `package admin` (whitebox test, consistent with `bootstrap_test.go` pattern)
  - [x] 3.2 Test: valid template name → `render()` writes non-empty HTML, status 200, Content-Type `text/html; charset=utf-8`
  - [x] 3.3 Test: unknown template name → `render()` writes status 500 (template lookup/execution fails)
  - [x] 3.4 Run `make test-unit-go` — all tests green, no regressions

## Dev Notes

### Existing Admin Package — What Already Exists

The `gateway/internal/admin/` package already contains production code. DO NOT change any existing files:

| File | State | Action |
|------|-------|--------|
| `gateway/internal/admin/api_gen.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/auth.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/auth_test.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/bootstrap.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/bootstrap_test.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/metrics.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/metrics_test.go` | EXISTS | Do NOT modify |
| `gateway/internal/admin/handler.go` | MISSING | CREATE |
| `gateway/internal/admin/handler_test.go` | MISSING | CREATE |
| `gateway/internal/admin/templates/base.html` | MISSING | CREATE |

### CRITICAL: go:embed Architecture Rule

Architecture enforcement rule #9 (non-negotiable):
```go
// ✅ MANDATORY pattern
//go:embed templates/**/*
var adminFS embed.FS
```
**NEVER use filesystem access at runtime** (`http.ServeFile`, `os.Open`, `template.ParseGlob` on disk paths).  
The embed directive must appear in the same file as the `var adminFS embed.FS` declaration.

### CRITICAL: go:embed Subtlety — No Static Dir Yet

Story 3.1 only embeds `templates/**/*`. Do NOT add `static/*` to the embed directive — there is no `static/` directory yet (CSS is Story 3.2, fonts Story 3.3). A `//go:embed` directive pointing to a non-existent path causes a **compile error**.

Story 3.2 will add a separate embed directive for `static/` in its own file. Keep 3.1 scope minimal.

### CRITICAL: go:embed Must Have at Least One File

If `gateway/internal/admin/templates/` is empty, `go build ./...` fails with:
```
pattern templates/**/*: no matching files found
```
You MUST create at least one template file (`base.html`) before `go build` can succeed. This satisfies AC 5.

### Handler Design

```go
package admin

import (
    "embed"
    "html/template"
    "log/slog"
    "net/http"
)

//go:embed templates/**/*
var adminFS embed.FS

// Handler serves Admin UI HTML pages via embedded Go templates.
type Handler struct {
    tmpl *template.Template
}

// NewHandler parses all templates from the embedded FS. Returns error if parsing fails.
func NewHandler() (*Handler, error) {
    tmpl, err := template.ParseFS(adminFS, "templates/**/*.html")
    if err != nil {
        return nil, err
    }
    return &Handler{tmpl: tmpl}, nil
}

// render executes the named template into w.
// Sets Content-Type: text/html; charset=utf-8.
// On execution error: writes 500, logs the error, does NOT panic.
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
        slog.Error("template execution failed", "name", name, "err", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
    }
}
```

**Note:** `http.Error` after `w.Header().Set(...)` works because headers haven't been sent yet (no body written before the error). If ExecuteTemplate writes partial output before failing, `http.Error` may append to it — this is acceptable for MVP (no streaming in SSR pages).

### Placeholder Template Design

`gateway/internal/admin/templates/base.html` should be minimal — it will be completely replaced in Story 3.4:

```html
{{ define "base" }}<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Nebu Admin</title></head>
<body>{{ template "content" . }}</body>
</html>{{ end }}
```

This provides a parseable template with a `"base"` name for unit tests to use.

### Test Pattern

The existing tests in `bootstrap_test.go` use:
- `package admin` (whitebox — same package, not `_test` suffix)
- `httptest.NewRequest` + `httptest.NewRecorder`
- No `t.Run` for simple single-assertion tests
- Table-driven tests for functions with multiple input variations (architecture rule: Go: table-driven Tests für alle Funktionen mit mehreren Inputs)

Since `render()` has two meaningful behaviors (valid name → HTML, unknown name → 500), use a table-driven test:

```go
func TestHandler_render(t *testing.T) {
    h, err := NewHandler()
    if err != nil {
        t.Fatalf("NewHandler: %v", err)
    }
    tests := []struct {
        name     string
        tmplName string
        wantCode int
        wantBody bool // true = expect non-empty body
    }{
        {"valid template", "base", http.StatusOK, true},
        {"unknown template", "nonexistent", http.StatusInternalServerError, false},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            w := httptest.NewRecorder()
            h.render(w, tc.tmplName, nil)
            if w.Code != tc.wantCode {
                t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
            }
            if tc.wantBody && w.Body.Len() == 0 {
                t.Error("expected non-empty body, got empty")
            }
            if tc.wantCode == http.StatusOK {
                ct := w.Header().Get("Content-Type")
                if ct != "text/html; charset=utf-8" {
                    t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
                }
            }
        })
    }
}
```

### Build Verification

The build target runs in Docker (no local Go required):
```bash
make test-unit-go   # go test ./... in build container
```
AC 5 (`go build ./...` succeeds) is validated implicitly by `make test-unit-go` — the test binary compilation requires the embed to succeed.

### Path Discrepancy Note

Architecture doc (`architecture.md`) shows `gateway/internal/ui/templates/` for templates. The epics (Story 3.1 AC) specify `gateway/internal/admin/templates/`. Follow the **epics** — all existing admin code (`auth.go`, `bootstrap.go`, `metrics.go`, `api_gen.go`) already lives in `gateway/internal/admin/`, and Stories 3.2–3.15 all reference `gateway/internal/admin/` consistently.

### Future Story Awareness (Do NOT implement now)

- Story 3.2 adds `gateway/internal/admin/static/admin.css` + a second `//go:embed static/admin.css` directive in a new file
- Story 3.3 adds `gateway/internal/admin/static/fonts/`
- Story 3.4 expands `base.html` with the full nav shell layout
- Story 3.5 adds `ActiveNav string` field to the render data struct
- `main.go` wiring (`NewHandler()`, route registration) is NOT required in this story

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/handler.go` | CREATE — Handler struct, adminFS embed, render() |
| `gateway/internal/admin/handler_test.go` | CREATE — table-driven render() unit tests |
| `gateway/internal/admin/templates/base.html` | CREATE — minimal placeholder for embed |

No changes to: `main.go`, `auth.go`, `bootstrap.go`, `metrics.go`, `api_gen.go`, any test files except the new one.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.1] Authoritative AC for this story
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement] Rule #9: go:embed mandatory, no filesystem access at runtime
- [Source: _bmad-output/planning-artifacts/architecture.md#Project-Structure] `gateway/internal/admin/` is the canonical admin package
- [Source: gateway/internal/admin/bootstrap_test.go] Test pattern: `package admin`, `httptest`, whitebox
- [Source: gateway/internal/admin/metrics.go] Existing admin package pattern — small focused structs

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- ✅ `TemplateHandler` struct created (renamed from `Handler` to avoid conflict with oapi-codegen generated `func Handler(...)` in `api_gen.go`)
- ✅ `//go:embed templates` used instead of `//go:embed templates/**/*`: the `**` pattern in Go embed does not match files directly in the target directory (only subdirectories). Directory embed covers all files recursively, matching the architecture intent.
- ✅ `template.ParseFS(adminFS, "templates/*.html")` used for top-level templates (Story 3.4+ will expand pattern when subdirectories are added)
- ✅ `base.html` defines both `content` (empty stub) and `base` templates to avoid "no such template" execution error
- ✅ Table-driven test covers valid template (200, non-empty body, correct Content-Type) and unknown template (500)
- ✅ All tests pass, no regressions: `make test-unit-go` → 10 packages OK

### File List

- gateway/internal/admin/handler.go (created)
- gateway/internal/admin/handler_test.go (created)
- gateway/internal/admin/templates/base.html (created)

## Change Log

- 2026-03-31: Story 3-1 created — Go Template Engine Setup + go:embed
- 2026-03-31: Story 3-1 implemented — TemplateHandler + go:embed templates + unit tests, all ACs satisfied
- 2026-03-31: Code review passed — clean review, 0 findings, all ACs verified

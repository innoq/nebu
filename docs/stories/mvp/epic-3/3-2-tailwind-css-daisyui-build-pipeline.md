# Story 3.2: Tailwind CSS + DaisyUI Build Pipeline

Status: done

## Story

As a developer,
I want Tailwind CSS (standalone CLI) + DaisyUI to compile into a single `admin.css` that is embedded in the binary,
so that the Admin UI has consistent styling without a CDN dependency.

## Acceptance Criteria

1. `Makefile` target `make build-admin-css` runs the Tailwind CLI (via Docker container, no local Node.js install) reading `gateway/internal/admin/tailwind.config.js` and writing compiled output to `gateway/internal/admin/static/admin.css`
2. `tailwind.config.js` declares `daisyui` as a plugin and scans `gateway/internal/admin/templates/**/*.html` for class usage
3. `gateway/internal/admin/static/admin.css` is embedded via `//go:embed static/admin.css` into the admin handler package (in a NEW separate file — NOT in `handler.go`)
4. The CSS is served at `GET /admin/static/admin.css` with `Content-Type: text/css` and `Cache-Control: public, max-age=31536000, immutable`
5. The Obsidian dark theme variables from UX-DR1 are declared as CSS custom properties in `tailwind.config.js` (full color token set)
6. `make build-gateway` runs `build-admin-css` as a prerequisite so the CSS is always fresh before the Docker image build
7. A unit test verifies that the CSS handler returns HTTP 200, correct `Content-Type: text/css`, and a non-empty body
8. `make test-unit-go` passes without requiring `build-admin-css` to have run first (placeholder CSS committed to git)

## Tasks / Subtasks

- [x] Task 1: Create Makefile `build-admin-css` target (AC: 1, 6)
  - [x] 1.1 Add `DOCKER_NODE = docker run --rm -v $(PWD):/workspace -w /workspace node:22-alpine` to Makefile
  - [x] 1.2 Add `build-admin-css` target: installs tailwindcss@3 + daisyui@4, runs `npx tailwindcss --config ... --input ... --output ... --minify`
  - [x] 1.3 Modify `build-gateway` target to depend on `build-admin-css` (run it as prerequisite before `docker build`)
  - [x] 1.4 Add `## build-admin-css: Compile Tailwind CSS + DaisyUI into gateway/internal/admin/static/admin.css` doc comment

- [x] Task 2: Create Tailwind config + input CSS (AC: 2, 5)
  - [x] 2.1 Create `gateway/internal/admin/tailwind.config.js` with Obsidian theme, DaisyUI plugin, content scan
  - [x] 2.2 Create `gateway/internal/admin/tailwind.input.css` with `@tailwind base/components/utilities` directives
  - [x] 2.3 Verify `tailwind.config.js` maps all Obsidian tokens (bg-base, bg-surface, bg-raised, text-primary, etc.) to DaisyUI semantic tokens

- [x] Task 3: Create static embed file and HTTP handler (AC: 3, 4)
  - [x] 3.1 Create `gateway/internal/admin/static/admin.css` placeholder (minimal, committed to git — enables `go build` without running Tailwind first)
  - [x] 3.2 Create `gateway/internal/admin/static.go` with `//go:embed static/admin.css`, `var staticFS embed.FS`, and `ServeCSS(w, r)` handler function
  - [x] 3.3 Verify `static.go` is in `package admin` (same package as `handler.go`)

- [x] Task 4: Wire route into main.go (AC: 4)
  - [x] 4.1 Add `mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)` to `gateway/cmd/gateway/main.go`
  - [x] 4.2 Place it near the other admin routes (after `bootstrapHandler` registration)

- [x] Task 5: Write unit tests (AC: 7, 8)
  - [x] 5.1 Create `gateway/internal/admin/static_test.go` in `package admin`
  - [x] 5.2 Test `ServeCSS`: HTTP 200, `Content-Type: text/css`, non-empty body
  - [x] 5.3 Test `ServeCSS`: `Cache-Control: public, max-age=31536000, immutable` header present
  - [x] 5.4 Run `make test-unit-go` — all tests green, no regressions

## Dev Notes

### CRITICAL: go:embed Split Pattern (from Story 3.1 Completion Notes)

Story 3.1 established that `handler.go` uses `//go:embed templates` for templates. Story 3.2 MUST add the static embed in a **separate new file** (`static.go`), NOT by modifying `handler.go`.

**Why:** The 3.1 completion notes explicitly state: "Story 3.2 will add a separate embed directive for `static/` in its own file. Keep 3.1 scope minimal." This is also cleaner Go code — each embed var has a clear responsibility.

```go
// gateway/internal/admin/static.go (NEW FILE)
package admin

import (
    "embed"
    "net/http"
)

//go:embed static/admin.css
var staticFS embed.FS

// ServeCSS serves the embedded admin.css with long-lived caching headers.
func ServeCSS(w http.ResponseWriter, r *http.Request) {
    data, err := staticFS.ReadFile("static/admin.css")
    if err != nil {
        http.Error(w, "Not Found", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "text/css")
    w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
    w.WriteHeader(http.StatusOK)
    w.Write(data) //nolint:errcheck
}
```

**Do NOT modify `handler.go`** — its `//go:embed templates` directive and `adminFS` var are unchanged.

### CRITICAL: Placeholder CSS — Required for Tests and Docker Builds

`//go:embed static/admin.css` at compile time **requires the file to exist**. If it is missing, `go build` and `go test` both fail with `pattern static/admin.css: no matching files found`.

**Solution: Commit a minimal placeholder `admin.css` to git.** The Makefile `build-admin-css` target overwrites it with the real compiled CSS.

Create `gateway/internal/admin/static/admin.css`:
```css
/* Nebu Admin — compiled by `make build-admin-css` (Tailwind CSS + DaisyUI) */
```

This placeholder:
- Enables `make test-unit-go` without requiring `build-admin-css` to run first (AC 8)
- Enables `docker build` in CI without a Node.js build stage in the Dockerfile
- Is intentionally minimal (no Tailwind classes) — the real CSS is generated by the build step

**Do NOT add admin.css to `.gitignore`** — it must be committed for the embed to work.

### CRITICAL: Architecture Rule #9

Architecture enforcement (non-negotiable):
```go
// ✅ MANDATORY — static files via embed.FS, never at runtime
//go:embed static/admin.css
var staticFS embed.FS
```
**NEVER use `http.ServeFile`, `os.Open`, or filesystem access at runtime.** The embed directive must appear in the same file as the `var staticFS embed.FS` declaration.

### Makefile Design

**Add to Makefile top-level variables:**
```makefile
DOCKER_NODE = docker run --rm -v $(PWD):/workspace -w /workspace node:22-alpine
```

**New target:**
```makefile
## build-admin-css: Compile Tailwind CSS + DaisyUI into gateway/internal/admin/static/admin.css
build-admin-css:
	$(DOCKER_NODE) sh -c "\
		cd gateway/internal/admin && \
		npm install --silent tailwindcss@3 daisyui@4 && \
		npx tailwindcss \
			--config tailwind.config.js \
			--input tailwind.input.css \
			--output static/admin.css \
			--minify"
```

**Modify `build-gateway` to depend on `build-admin-css`:**
```makefile
## build-gateway: Build the Go Gateway Docker image (multi-stage)
build-gateway: build-admin-css
	docker build -t nebu-gateway:dev ./gateway
```

**Add to `.PHONY`:** `build-admin-css`

**Why Node.js Docker for Tailwind?** The DaisyUI v4 plugin requires Node.js module resolution — the standalone Tailwind binary cannot load external plugins. Using `node:22-alpine` is the correct approach. Node.js is only used at BUILD time, not at runtime (matching the "Ein Binary"-philosophy: no Node.js in the deployed binary).

### tailwind.config.js Design (Full Obsidian Theme)

Create `gateway/internal/admin/tailwind.config.js`:

```js
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./templates/**/*.html'],
  plugins: [require('daisyui')],
  daisyui: {
    themes: [
      {
        obsidian: {
          // DaisyUI semantic token → Obsidian hex value (UX-DR1)
          "primary":          "#f97316",  // color-primary (buttons, active nav)
          "primary-content":  "#fff7ed",  // color-primary-text
          "secondary":        "#374151",  // bg-raised (secondary actions)
          "secondary-content":"#f9fafb",
          "accent":           "#f59e0b",  // status-warn (accent/highlight)
          "accent-content":   "#1f2937",
          "neutral":          "#374151",  // bg-raised
          "neutral-content":  "#9ca3af",
          "base-100":         "#111827",  // bg-base (page background)
          "base-200":         "#1f2937",  // bg-surface (cards, panels, sidebar)
          "base-300":         "#374151",  // bg-raised / bg-border (hover, overlays)
          "base-content":     "#f9fafb",  // text-primary
          "success":          "#22c55e",  // status-ok
          "success-content":  "#052e16",
          "warning":          "#f59e0b",  // status-warn
          "warning-content":  "#431407",
          "error":            "#ef4444",  // status-error
          "error-content":    "#450a0a",
          "info":             "#6b7280",  // status-neutral
          "info-content":     "#f9fafb",
        },
      },
    ],
    darkTheme: "obsidian",
    logs: false,
  },
  theme: {
    extend: {
      // Additional Obsidian tokens as CSS custom properties for direct use in templates
      colors: {
        "primary-hover":    "#ea580c",  // color-primary-hover
        "primary-subtle":   "#431407",  // color-primary-subtle
        "text-secondary":   "#9ca3af",  // text-secondary (meta, timestamps)
        "text-disabled":    "#4b5563",  // text-disabled
        "status-ok-bg":     "#052e16",
        "status-warn-bg":   "#431407",
        "status-error-bg":  "#450a0a",
      },
      // Typography scale from UX-DR2
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'monospace'],
      },
    },
  },
};
```

Create `gateway/internal/admin/tailwind.input.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

**DaisyUI theme activation in HTML:** The `<html>` element must have `data-theme="obsidian"` for DaisyUI to apply the theme. Story 3.4 adds this to `base.html`. Story 3.2 only needs the build pipeline to produce correct CSS.

### Unit Test Design

Create `gateway/internal/admin/static_test.go` in `package admin`:

```go
package admin

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestServeCSS(t *testing.T) {
    tests := []struct {
        name          string
        wantStatus    int
        wantCT        string
        wantCC        string
        wantNonEmpty  bool
    }{
        {
            name:         "serves admin.css",
            wantStatus:   http.StatusOK,
            wantCT:       "text/css",
            wantCC:       "public, max-age=31536000, immutable",
            wantNonEmpty: true,
        },
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", "/admin/static/admin.css", nil)
            w := httptest.NewRecorder()
            ServeCSS(w, req)
            if w.Code != tc.wantStatus {
                t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
            }
            if ct := w.Header().Get("Content-Type"); ct != tc.wantCT {
                t.Errorf("Content-Type: got %q, want %q", ct, tc.wantCT)
            }
            if cc := w.Header().Get("Cache-Control"); cc != tc.wantCC {
                t.Errorf("Cache-Control: got %q, want %q", cc, tc.wantCC)
            }
            if tc.wantNonEmpty && w.Body.Len() == 0 {
                t.Error("expected non-empty body, got empty")
            }
        })
    }
}
```

**Pattern:** Consistent with `handler_test.go` (Story 3.1): `package admin`, `httptest.NewRequest` + `httptest.NewRecorder`, table-driven where multiple variations exist.

**Note on placeholder CSS:** The test will pass with the placeholder `admin.css` (minimal comment). `Body.Len() > 0` is satisfied by the comment text.

### Route Registration in main.go

Add the following line to `gateway/cmd/gateway/main.go`, near the existing admin routes (after the bootstrapHandler line):

```go
mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)
```

**Context:** `main.go` already imports `github.com/nebu/nebu/internal/admin` — no new import needed.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/static.go` | CREATE — `//go:embed static/admin.css`, `var staticFS embed.FS`, `ServeCSS(w, r)` |
| `gateway/internal/admin/static_test.go` | CREATE — table-driven unit test for `ServeCSS` |
| `gateway/internal/admin/static/admin.css` | CREATE — minimal placeholder, committed to git |
| `gateway/internal/admin/tailwind.config.js` | CREATE — DaisyUI plugin, Obsidian theme, content scan |
| `gateway/internal/admin/tailwind.input.css` | CREATE — `@tailwind base/components/utilities` |
| `Makefile` | MODIFY — add `DOCKER_NODE`, `build-admin-css`, update `build-gateway` to depend on it |
| `gateway/cmd/gateway/main.go` | MODIFY — add `GET /admin/static/admin.css` route |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — leave `//go:embed templates` unchanged
- Any existing `*_test.go` files
- `gateway/Dockerfile` — it doesn't need to run Tailwind; `make build-gateway` handles ordering

### Future Story Awareness (Do NOT implement now)

- Story 3.3 adds fonts to `gateway/internal/admin/static/fonts/` and expands the embed in `static.go` to `//go:embed static/admin.css static/fonts` (or `//go:embed static`)
- Story 3.4 replaces `base.html` with the full nav shell layout and adds `data-theme="obsidian"` to `<html>`; it will use DaisyUI classes from the compiled CSS
- Story 3.5 adds `ActiveNav string` data to templates; depends on 3.2 CSS for visual highlighting
- `npm install` in `build-admin-css` will create a `node_modules/` directory — ensure `.gitignore` covers `gateway/internal/admin/node_modules/` and `gateway/internal/admin/package-lock.json`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.2] Authoritative AC for this story
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement] Rule #9: go:embed mandatory, no filesystem access at runtime
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Color-System] Full Obsidian color token table (UX-DR1)
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Design-System-Foundation] Tailwind + DaisyUI rationale (UX-DR3)
- [Source: _bmad-output/implementation-artifacts/3-1-go-template-engine-setup-go-embed.md#Completion-Notes] embed split into separate file; `//go:embed templates` (not `templates/**/*`)
- [Source: gateway/internal/admin/handler.go] Existing embed pattern: `//go:embed templates`, `var adminFS embed.FS`
- [Source: gateway/cmd/gateway/main.go] Route registration pattern, `admin` import already present
- [Source: gateway/internal/admin/handler_test.go] Test pattern: `package admin`, `httptest`, table-driven

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- All 5 tasks completed. `make test-unit-go` passes with zero regressions (11 packages).
- `static.go` created as a new file (NOT modifying `handler.go`) — embed split pattern from Story 3.1 respected.
- Placeholder `admin.css` committed to git so `go build` / `go test` work without running `build-admin-css`.
- `.gitignore` extended with `node_modules/`, `package.json`, `package-lock.json` under `gateway/internal/admin/`.
- `build-gateway` now depends on `build-admin-css` (AC 6).
- `DOCKER_NODE` variable added to Makefile top-level (node:22-alpine for DaisyUI v4 plugin resolution).

### File List

- `Makefile` — added `DOCKER_NODE`, `build-admin-css` target, updated `build-gateway` prerequisite, `.PHONY`
- `gateway/internal/admin/tailwind.config.js` — NEW: Obsidian theme, DaisyUI plugin, content scan
- `gateway/internal/admin/tailwind.input.css` — NEW: `@tailwind base/components/utilities`
- `gateway/internal/admin/static/admin.css` — NEW: minimal placeholder (committed to git)
- `gateway/internal/admin/static.go` — NEW: `//go:embed static/admin.css`, `var staticFS embed.FS`, `ServeCSS`
- `gateway/internal/admin/static_test.go` — NEW: table-driven unit test for `ServeCSS`
- `gateway/cmd/gateway/main.go` — added `GET /admin/static/admin.css` route
- `.gitignore` — added `gateway/internal/admin/node_modules/`, `package.json`, `package-lock.json`

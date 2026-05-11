# Story 3.3: Self-hosted WOFF2 Fonts + Static Asset Serving

Status: done

## Story

As an operator,
I want all fonts (Inter, JetBrains Mono) served from the gateway binary,
so that the Admin UI loads correctly in air-gapped environments with no external font requests.

## Acceptance Criteria

1. WOFF2 font files for Inter (Regular, Medium, SemiBold) and JetBrains Mono (Regular) are placed in `gateway/internal/admin/static/fonts/`
2. All font files are embedded via `//go:embed static/fonts` (by expanding the directive in `static.go`)
3. Font files are served at `GET /admin/static/fonts/<filename>` with `Content-Type: font/woff2` and `Cache-Control: public, max-age=31536000, immutable`
4. A `@font-face` declaration in the base CSS (compiled `admin.css`) references `/admin/static/fonts/<filename>` — not an external URL
5. Tailwind config maps `fontFamily.sans` to `Inter` and `fontFamily.mono` to `JetBrains Mono` (**already done in Story 3.2** — no change needed)
6. `go build ./...` succeeds with fonts embedded; binary size increase is acceptable (fonts < 1 MB total)
7. No browser request to `fonts.googleapis.com` or any CDN appears when loading the Admin UI

## Tasks / Subtasks

- [x] Task 1: Download and commit WOFF2 font files (AC: 1, 6)
  - [x] 1.1 Add `download-fonts` Makefile target using Docker alpine + curl
  - [x] 1.2 Add `download-fonts` to `.PHONY` in Makefile
  - [x] 1.3 Run `make download-fonts` to place 4 WOFF2 files in `gateway/internal/admin/static/fonts/`
  - [x] 1.4 Verify total font size < 1 MB (`du -sh gateway/internal/admin/static/fonts/`)
  - [x] 1.5 Commit all 4 WOFF2 files to git (they must NOT be in `.gitignore`)

- [x] Task 2: Expand embed directive + add `ServeFontFile` handler in `static.go` (AC: 2, 3)
  - [x] 2.1 Change `//go:embed static/admin.css` to `//go:embed static/admin.css static/fonts`
  - [x] 2.2 Add `path` and `strings` imports
  - [x] 2.3 Add `ServeFontFile(w http.ResponseWriter, r *http.Request)` function (see design below)

- [x] Task 3: Add `@font-face` declarations to `tailwind.input.css` (AC: 4, 7)
  - [x] 3.1 Prepend the 4 `@font-face` blocks to `gateway/internal/admin/tailwind.input.css` (before `@tailwind base`)
  - [x] 3.2 Run `make build-admin-css` to recompile `admin.css` with `@font-face` included; verify the rules appear in the output

- [x] Task 4: Wire font route in `main.go` (AC: 3)
  - [x] 4.1 Add `mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)` directly after the CSS route

- [x] Task 5: Write unit tests (AC: 3, 6)
  - [x] 5.1 Add `TestServeFontFile` to `gateway/internal/admin/static_test.go` (same file, same package)
  - [x] 5.2 Test: each of the 4 font files → HTTP 200, `Content-Type: font/woff2`, `Cache-Control` immutable, non-empty body
  - [x] 5.3 Test: non-`.woff2` filename → HTTP 404
  - [x] 5.4 Run `make test-unit-go` — all tests green, zero regressions

## Dev Notes

### CRITICAL: Expand embed directive in static.go (NOT a new file)

`static.go` currently has `//go:embed static/admin.css`. Story 3.3 **modifies this same file** — it does NOT create a new file. Change the embed directive to include the fonts directory:

```go
// gateway/internal/admin/static.go — MODIFIED
package admin

import (
    "embed"
    "net/http"
    "path"
    "strings"
)

//go:embed static/admin.css static/fonts
var staticFS embed.FS

// ServeCSS serves the embedded admin.css — unchanged from Story 3.2.
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

// ServeFontFile serves embedded WOFF2 font files from static/fonts/.
// Route: GET /admin/static/fonts/{filename}
func ServeFontFile(w http.ResponseWriter, r *http.Request) {
    filename := path.Base(r.PathValue("filename")) // path.Base prevents directory traversal
    if !strings.HasSuffix(filename, ".woff2") {
        http.Error(w, "Not Found", http.StatusNotFound)
        return
    }
    data, err := staticFS.ReadFile("static/fonts/" + filename)
    if err != nil {
        http.Error(w, "Not Found", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "font/woff2")
    w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
    w.WriteHeader(http.StatusOK)
    w.Write(data) //nolint:errcheck
}
```

**Architecture rule #9 (non-negotiable):** All static files served via `embed.FS`. Never `http.ServeFile`, `os.Open`, or runtime filesystem access. [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement]

**Why `path.Base`:** Prevents `../../../etc/passwd`-style traversal. The input comes from `r.PathValue` which Go's ServeMux already constrains, but defense in depth is correct here.

**Why `.woff2` check:** Ensures only font files are served from this handler; no executable or sensitive file can be served even if the embed were expanded later.

### CRITICAL: Embed directive syntax — `static/fonts` not `static/fonts/*`

The `//go:embed` directive embeds an entire directory tree when given a directory path without glob:

```go
//go:embed static/admin.css static/fonts
var staticFS embed.FS
```

This embeds `static/admin.css` AND everything inside `static/fonts/`. Files accessed as `staticFS.ReadFile("static/fonts/Inter-Regular.woff2")`.

Do NOT use `static/fonts/*` in the embed directive — the `*` glob does NOT recurse and would miss subdirectories. Use the directory path directly.

### Font File Acquisition (Makefile target)

Add this target to the Makefile (place it near `build-admin-css`):

```makefile
## download-fonts: Download Inter + JetBrains Mono WOFF2 fonts (run once; commit results)
download-fonts:
	docker run --rm -v $(PWD):/workspace -w /workspace alpine:3.19 sh -c "\
		apk add -q --no-cache curl && \
		mkdir -p gateway/internal/admin/static/fonts && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-Regular.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-400-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-Medium.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-500-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-SemiBold.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-600-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/JetBrainsMono-Regular.woff2 \
			'https://fonts.bunny.net/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff2'"
```

Bunny Fonts (bunny.net) hosts the same font families as Google Fonts from a GDPR-compliant CDN. Files are the standard Inter/JetBrains Mono WOFF2 releases, only served from a different origin. If URLs become stale, fall back to downloading the `.zip` from the official GitHub release and extracting:
- Inter: https://github.com/rsms/inter/releases (look for `Inter-*.woff2` in the release assets)
- JetBrains Mono: https://github.com/JetBrains/JetBrainsMono/releases (look under `fonts/webfonts/`)

**COMMIT the font files.** Add to `.PHONY` but NOT to `.gitignore`. The `//go:embed` directive requires the files to exist in the source tree at compile time. There is no "placeholder" pattern for binary WOFF2 files (unlike the CSS placeholder in Story 3.2 — a minimal text comment satisfies the embed, but an empty WOFF2 is invalid binary).

Update `.PHONY` line:
```makefile
.PHONY: build-gateway build-core build-admin-css download-fonts dev setup test-unit-go test-unit-elixir test-integration proto gen-api
```

### @font-face Declarations in `tailwind.input.css`

Replace the contents of `gateway/internal/admin/tailwind.input.css` with:

```css
/* Self-hosted WOFF2 fonts — no CDN requests (AC: Story 3.3) */
@font-face {
  font-family: 'Inter';
  font-style: normal;
  font-weight: 400;
  font-display: swap;
  src: url('/admin/static/fonts/Inter-Regular.woff2') format('woff2');
}
@font-face {
  font-family: 'Inter';
  font-style: normal;
  font-weight: 500;
  font-display: swap;
  src: url('/admin/static/fonts/Inter-Medium.woff2') format('woff2');
}
@font-face {
  font-family: 'Inter';
  font-style: normal;
  font-weight: 600;
  font-display: swap;
  src: url('/admin/static/fonts/Inter-SemiBold.woff2') format('woff2');
}
@font-face {
  font-family: 'JetBrains Mono';
  font-style: normal;
  font-weight: 400;
  font-display: swap;
  src: url('/admin/static/fonts/JetBrainsMono-Regular.woff2') format('woff2');
}

@tailwind base;
@tailwind components;
@tailwind utilities;
```

`font-display: swap` is the correct value for Admin UI: renders fallback font immediately, swaps to Inter when loaded. Other options (`block`, `optional`) are inappropriate for a management UI where readability is critical.

The URL `/admin/static/fonts/Inter-Regular.woff2` is an absolute path (not relative). This is correct: the Admin UI is served under `/admin/`, and the browser will resolve this to the gateway root. Relative paths from the CSS file location would resolve incorrectly.

After editing, run `make build-admin-css` to recompile. The compiled `admin.css` placeholder file should NOT be manually edited — it is generated output.

### Route Registration in `main.go`

Add one line after the existing CSS route (line 129 of current `main.go`):

```go
mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)
```

This uses Go 1.22+'s ServeMux pattern syntax with `{filename}` wildcard. The project targets Go 1.26+, so `r.PathValue("filename")` is available. No new import needed (`admin` is already imported).

### Unit Test Design

Add `TestServeFontFile` to the existing `gateway/internal/admin/static_test.go`. Use `req.SetPathValue("filename", ...)` (available since Go 1.22) to inject the path parameter without needing a test mux:

```go
func TestServeFontFile(t *testing.T) {
    tests := []struct {
        name       string
        filename   string
        wantStatus int
        wantCT     string
        wantCC     string
        wantBody   bool
    }{
        {
            name:       "serves Inter-Regular.woff2",
            filename:   "Inter-Regular.woff2",
            wantStatus: http.StatusOK,
            wantCT:     "font/woff2",
            wantCC:     "public, max-age=31536000, immutable",
            wantBody:   true,
        },
        {
            name:       "serves JetBrainsMono-Regular.woff2",
            filename:   "JetBrainsMono-Regular.woff2",
            wantStatus: http.StatusOK,
            wantCT:     "font/woff2",
            wantCC:     "public, max-age=31536000, immutable",
            wantBody:   true,
        },
        {
            name:       "rejects non-woff2 extension",
            filename:   "evil.js",
            wantStatus: http.StatusNotFound,
        },
        {
            name:       "rejects path traversal attempt",
            filename:   "../admin.css",
            wantStatus: http.StatusNotFound,
        },
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", "/admin/static/fonts/"+tc.filename, nil)
            req.SetPathValue("filename", tc.filename)
            w := httptest.NewRecorder()
            ServeFontFile(w, req)
            if w.Code != tc.wantStatus {
                t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
            }
            if tc.wantCT != "" {
                if ct := w.Header().Get("Content-Type"); ct != tc.wantCT {
                    t.Errorf("Content-Type: got %q, want %q", ct, tc.wantCT)
                }
            }
            if tc.wantCC != "" {
                if cc := w.Header().Get("Cache-Control"); cc != tc.wantCC {
                    t.Errorf("Cache-Control: got %q, want %q", cc, tc.wantCC)
                }
            }
            if tc.wantBody && w.Body.Len() == 0 {
                t.Error("expected non-empty body, got empty")
            }
        })
    }
}
```

**Pattern:** Consistent with `TestServeCSS` (Story 3.2): `package admin`, `httptest.NewRequest` + `httptest.NewRecorder`, table-driven. The `path traversal` test case relies on `path.Base` converting `../admin.css` → `admin.css`, which doesn't exist in `static/fonts/`, so the embed lookup fails with 404.

You do NOT need to test all 4 fonts exhaustively. Testing 2 (Inter-Regular and JetBrainsMono-Regular) plus the 2 negative cases is sufficient coverage.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/static/fonts/Inter-Regular.woff2` | DOWNLOAD + COMMIT |
| `gateway/internal/admin/static/fonts/Inter-Medium.woff2` | DOWNLOAD + COMMIT |
| `gateway/internal/admin/static/fonts/Inter-SemiBold.woff2` | DOWNLOAD + COMMIT |
| `gateway/internal/admin/static/fonts/JetBrainsMono-Regular.woff2` | DOWNLOAD + COMMIT |
| `gateway/internal/admin/static.go` | MODIFY — expand `//go:embed`, add `ServeFontFile`, add `path`/`strings` imports |
| `gateway/internal/admin/static_test.go` | MODIFY — add `TestServeFontFile` |
| `gateway/internal/admin/tailwind.input.css` | MODIFY — prepend `@font-face` rules |
| `gateway/internal/admin/static/admin.css` | REGENERATE — run `make build-admin-css`; commit updated placeholder |
| `Makefile` | MODIFY — add `download-fonts` target, update `.PHONY` |
| `gateway/cmd/gateway/main.go` | MODIFY — add `GET /admin/static/fonts/{filename}` route |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — `//go:embed templates` unchanged
- `gateway/internal/admin/tailwind.config.js` — `fontFamily` already set correctly (Story 3.2)
- Any existing `*_test.go` files beyond adding the new test function

**Do NOT add fonts to `.gitignore`** — they must be committed for the embed to work.

### Context from Story 3.2 Completion Notes

Story 3.2 completion notes explicitly anticipated this story:

> "Story 3.3 adds fonts to `gateway/internal/admin/static/fonts/` and expands the embed in `static.go` to `//go:embed static/admin.css static/fonts` (or `//go:embed static`)"

Story 3.2 also already configured `tailwind.config.js` with:
```js
fontFamily: {
  sans: ['Inter', 'system-ui', 'sans-serif'],
  mono: ['JetBrains Mono', 'monospace'],
},
```

This means AC #5 ("Tailwind config maps fontFamily.sans to Inter...") is **already satisfied** — no change to `tailwind.config.js` is needed.

### Story 3.4 Awareness (Do NOT implement now)

Story 3.4 adds the full nav shell layout in `base.html`. That template will reference `/admin/static/fonts/inter.woff2` via `@font-face` in the compiled CSS — which this story provides. Story 3.4 also adds `data-theme="obsidian"` to `<html>`, activating the DaisyUI Obsidian theme.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.3] Authoritative ACs for this story
- [Source: _bmad-output/planning-artifacts/architecture.md#Enforcement] Rule #9: go:embed mandatory, no runtime filesystem access
- [Source: _bmad-output/implementation-artifacts/3-2-tailwind-css-daisyui-build-pipeline.md#Future-Story-Awareness] "Story 3.3 expands embed to static/admin.css static/fonts"
- [Source: _bmad-output/implementation-artifacts/3-2-tailwind-css-daisyui-build-pipeline.md#File-List] `static.go` current embed: `//go:embed static/admin.css`
- [Source: gateway/internal/admin/static.go] Current file — `//go:embed static/admin.css`, `ServeCSS` handler
- [Source: gateway/internal/admin/static_test.go] Test pattern: table-driven, `httptest.NewRequest`+`NewRecorder`, `package admin`
- [Source: gateway/internal/admin/tailwind.input.css] Current file — only 3 `@tailwind` directives, no `@font-face`
- [Source: gateway/internal/admin/tailwind.config.js] `fontFamily.sans: ['Inter', ...]`, `fontFamily.mono: ['JetBrains Mono', ...]` — already set
- [Source: gateway/cmd/gateway/main.go:129] `mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)` — add font route after this line
- [Source: Makefile:8] `DOCKER_NODE` already defined; `download-fonts` uses alpine (no Node needed)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No debug issues encountered. Straight-forward implementation following story spec.

### Completion Notes List

- Downloaded 4 WOFF2 font files via `make download-fonts` (alpine:3.19 + curl from bunny.net): Inter-Regular, Inter-Medium, Inter-SemiBold, JetBrainsMono-Regular. Total size: 96K (well under 1 MB limit).
- Expanded `//go:embed` in `static.go` from `static/admin.css` to `static/admin.css static/fonts`; added `path` and `strings` imports; added `ServeFontFile` handler with `path.Base` traversal protection and `.woff2` extension guard.
- Prepended 4 `@font-face` blocks to `tailwind.input.css` (before `@tailwind base`) using absolute paths `/admin/static/fonts/<filename>` and `font-display: swap`.
- Ran `make build-admin-css` — all 4 `@font-face` rules confirmed in minified `admin.css` output.
- Added `GET /admin/static/fonts/{filename}` route in `main.go` after CSS route.
- Added `TestServeFontFile` to `static_test.go`: 4 test cases (2 positive font serves, 1 non-woff2 rejection, 1 path traversal rejection). All pass.
- `make test-unit-go`: all 11 packages pass, zero regressions.

### File List

- `gateway/internal/admin/static/fonts/Inter-Regular.woff2` — ADDED (23664 bytes)
- `gateway/internal/admin/static/fonts/Inter-Medium.woff2` — ADDED (24272 bytes)
- `gateway/internal/admin/static/fonts/Inter-SemiBold.woff2` — ADDED (24452 bytes)
- `gateway/internal/admin/static/fonts/JetBrainsMono-Regular.woff2` — ADDED (21168 bytes)
- `gateway/internal/admin/static.go` — MODIFIED (embed directive expanded, `ServeFontFile` added, `path`/`strings` imports added)
- `gateway/internal/admin/static_test.go` — MODIFIED (`TestServeFontFile` added)
- `gateway/internal/admin/tailwind.input.css` — MODIFIED (`@font-face` blocks prepended)
- `gateway/internal/admin/static/admin.css` — REGENERATED (`make build-admin-css`)
- `gateway/cmd/gateway/main.go` — MODIFIED (font route registered)
- `Makefile` — MODIFIED (`download-fonts` target added, `.PHONY` updated)

## Change Log

| Date | Change |
|------|--------|
| 2026-03-31 | Story 3-3 implemented: 4 WOFF2 fonts embedded via go:embed, `ServeFontFile` handler added, `@font-face` CSS declarations, font route registered, unit tests added. All tests pass. |

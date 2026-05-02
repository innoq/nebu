---
security_review: not-needed
---

# Story 7.1: Obsidian Color System vollständig + Typography (UX-DR1, UX-DR2)

Status: review

## Story

As a developer,
I want the complete Obsidian design token set and typography scale defined in Tailwind config,
so that all Admin UI pages use consistent colours and type without hardcoded values.

## Acceptance Criteria

1. `gateway/internal/admin/tailwind.config.js` extends the default Tailwind theme with all Obsidian CSS custom properties from UX-DR1:
   - Background scale: `--color-base-100` through `--color-base-400` (dark greys)
   - Semantic colours: `--color-primary` (electric indigo), `--color-secondary`, `--color-accent`
   - Status colours: `--color-success` (green), `--color-warning` (amber), `--color-error` (red)
   - All tokens are referenced as `theme('colors.*')` in Tailwind utilities — no hardcoded hex values in templates
2. DaisyUI theme `obsidian` is declared in `tailwind.config.js` mapping all DaisyUI semantic tokens to the Obsidian palette; `data-theme="obsidian"` is set on the `<html>` element in `base.html`
3. Typography scale from UX-DR2 is declared as Tailwind `fontSize` extensions: `display` (2.25rem/700), `heading` (1.5rem/600), `body` (1rem/400), `caption` (0.75rem/400), `mono` (0.875rem/400 JetBrains Mono)
4. `make build-admin-css` produces a CSS file where all Obsidian tokens appear as `:root` CSS custom properties
5. Visual smoke test: render `base.html` and assert `data-theme="obsidian"` attribute is present on `<html>`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **Playwright visual smoke test — data-theme attribute** — Playwright (`e2e/tests/features/admin/`)
   - Given: the full dev stack is running (`make dev`)
   - When: Playwright navigates to `/admin/bootstrap` (or `/admin/login`)
   - Then: `page.locator('html')` has attribute `data-theme` equal to `"obsidian"`

2. **Go unit test — base.html renders with data-theme="obsidian"** — Go `net/http/httptest`
   - Given: `TemplateHandler` with embedded templates rendered to `http.ResponseWriter`
   - When: any page (e.g. dashboard stub or login) is rendered via the template engine
   - Then: the response body contains `data-theme="obsidian"`

3. **Go unit test — tailwind.config.js contains fontSize extensions** — file content assertion
   - Given: `gateway/internal/admin/tailwind.config.js` is read as a string
   - When: checked for the presence of `fontSize` block with keys `display`, `heading`, `body`, `caption`, `mono`
   - Then: all five keys are found

## Tasks / Subtasks

- [x] Task 1: Add `fontSize` extensions to `tailwind.config.js` (AC: 3)
  - [x] 1.1 In the `theme.extend` block of `gateway/internal/admin/tailwind.config.js`, add a `fontSize` section:
    - `display`: `['2.25rem', { fontWeight: '700', lineHeight: '1.2' }]`
    - `heading`: `['1.5rem', { fontWeight: '600', lineHeight: '1.2' }]`
    - `body`: `['1rem', { fontWeight: '400', lineHeight: '1.6' }]`
    - `caption`: `['0.75rem', { fontWeight: '400', lineHeight: '1.5' }]`
    - `mono`: `['0.875rem', { fontWeight: '400', lineHeight: '1.5', fontFamily: 'JetBrains Mono, monospace' }]`
  - [x] 1.2 Verify the existing `fontFamily` extensions (`sans: ['Inter', ...]`, `mono: ['JetBrains Mono', ...]`) remain unchanged

- [x] Task 2: Verify and complete Obsidian color token coverage (AC: 1)
  - [x] 2.1 Audit existing `theme.extend.colors` in `tailwind.config.js` against the full UX-DR1 token list (see Dev Notes below for canonical mapping)
  - [x] 2.2 Add `base-400` color if not present: `"base-400": "#0d1117"` (one step darker than base-100)
  - [x] 2.3 Confirm that `color-primary`, `color-secondary`, `color-accent`, `color-success`, `color-warning`, `color-error` are all derivable from the DaisyUI `obsidian` theme tokens (already mapped via `primary`, `success`, etc. in the DaisyUI `obsidian` block)
  - [x] 2.4 Ensure no hardcoded hex values appear in `.html` templates — they must use DaisyUI/Tailwind class names only

- [x] Task 3: Verify `data-theme="obsidian"` in `base.html` (AC: 2)
  - [x] 3.1 Confirm `gateway/internal/admin/templates/layouts/base.html` has `<html lang="en" data-theme="obsidian">` (already present — verify no regression)
  - [x] 3.2 No change needed if already correct; add `data-theme="obsidian"` only if missing

- [x] Task 4: Write Go unit tests (AC: 2, 3, 5)
  - [x] 4.1 Create `gateway/internal/admin/obsidian_theme_test.go` in `package admin`
  - [x] 4.2 **Test A** — `TestBaseLayoutDataTheme`: render any page template via `TemplateHandler`; assert response body contains `data-theme="obsidian"`. Reuse the existing render helpers from `handler_test.go`
  - [x] 4.3 **Test B** — `TestTailwindConfigFontSizeExtensions`: read `tailwind.config.js` from disk; assert presence of `"display"`, `"heading"`, `"body"`, `"caption"`, `"mono"` strings in the `fontSize` block. Use `os.ReadFile("tailwind.config.js")` (test runs from within the `admin` package directory)
  - [x] 4.4 Run `make test-unit-go` — all tests green, zero regressions

- [x] Task 5: Write Playwright E2E smoke test (AC: 5)
  - [x] 5.1 Create `e2e/tests/features/admin/obsidian-theme.spec.ts`
  - [x] 5.2 **Test** — `obsidian theme: html element has data-theme="obsidian"`:
    - Navigate to `/admin/bootstrap` (works without login; bootstrap page is publicly accessible)
    - Assert `await expect(page.locator('html')).toHaveAttribute('data-theme', 'obsidian')`
  - [x] 5.3 The test must not use `page.waitForTimeout` or hard waits — use Playwright's built-in auto-waiting

- [x] Task 6: Run full build and verify CSS output (AC: 4)
  - [x] 6.1 Run `make build-admin-css` — verify it exits 0
  - [x] 6.2 Confirm the compiled `gateway/internal/admin/static/admin.css` is non-empty and contains `--p:` (DaisyUI primary token) and `:root` declarations
  - [x] 6.3 Run `make test-unit-go` after CSS rebuild — all tests pass

## Dev Notes

### CRITICAL: What Already Exists vs. What Needs to Change

The Epic 3 build pipeline is **fully complete**. Do NOT re-create or re-configure any of the following:
- `Makefile` `build-admin-css` target — already present and correct
- `gateway/internal/admin/tailwind.config.js` — exists, has the DaisyUI `obsidian` theme block
- `gateway/internal/admin/tailwind.input.css` — exists with `@tailwind base/components/utilities`
- `gateway/internal/admin/static/admin.css` — compiled CSS already embedded in the binary
- `gateway/internal/admin/static.go` — `//go:embed static/admin.css static/fonts static/vendor static/js`
- `gateway/internal/admin/templates/layouts/base.html` — already has `data-theme="obsidian"` on `<html>`

**The ONLY substantive change is adding `fontSize` extensions to `tailwind.config.js` and running `make build-admin-css` to regenerate the CSS.**

### Current `tailwind.config.js` State (read before editing)

```js
/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./templates/**/*.html'],
  plugins: [require('daisyui')],
  daisyui: {
    themes: [{ obsidian: { /* ... DaisyUI semantic tokens ... */ } }],
    darkTheme: "obsidian",
    logs: false,
  },
  theme: {
    extend: {
      colors: {
        "primary-hover":   "#ea580c",
        "primary-subtle":  "#431407",
        "text-secondary":  "#9ca3af",
        "text-disabled":   "#4b5563",
        "status-ok-bg":    "#052e16",
        "status-warn-bg":  "#431407",
        "status-error-bg": "#450a0a",
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'monospace'],
      },
      // ← ADD fontSize HERE (see Task 1)
    },
  },
};
```

Add the `fontSize` block **inside** `theme.extend`, after `fontFamily`. Do NOT restructure the file.

### Canonical `fontSize` Extension to Add

```js
fontSize: {
  'display': ['2.25rem', { fontWeight: '700', lineHeight: '1.2', letterSpacing: '-0.02em' }],
  'heading': ['1.5rem',  { fontWeight: '600', lineHeight: '1.2', letterSpacing: '-0.02em' }],
  'body':    ['1rem',    { fontWeight: '400', lineHeight: '1.6' }],
  'caption': ['0.75rem', { fontWeight: '400', lineHeight: '1.5', letterSpacing: '0.08em' }],
  'mono':    ['0.875rem',{ fontWeight: '400', lineHeight: '1.5' }],
},
```

**Why these values:** Directly from UX-DR2 typography spec (`ux-design-specification.md#Typography-System`):
- `display` = `text-3xl` + bold + heading letter-spacing (`-0.02em`)
- `heading` = `text-2xl` + semibold + heading letter-spacing
- `body` = `text-base` + regular + body line-height (1.6)
- `caption` = `text-xs` + regular + uppercase letter-spacing (`0.08em`)
- `mono` = `text-sm` + regular (JetBrains Mono is already the `font-mono` family, so `font-mono text-mono` is the intended usage)

### UX-DR1 Color Token Canonical Mapping

For reference — these are already covered by the DaisyUI `obsidian` theme block in `tailwind.config.js`:

| UX-DR1 Token | Hex | DaisyUI token |
|---|---|---|
| `bg-base` | `#111827` | `base-100` |
| `bg-surface` | `#1f2937` | `base-200` |
| `bg-raised` / `bg-border` | `#374151` | `base-300` |
| `text-primary` | `#f9fafb` | `base-content` |
| `color-primary` (orange) | `#f97316` | `primary` |
| `status-ok` | `#22c55e` | `success` |
| `status-warn` | `#f59e0b` | `warning` |
| `status-error` | `#ef4444` | `error` |

**`base-400` note:** The AC mentions `--color-base-100` through `--color-base-400`. UX-DR1 defines only 3 background levels. Add `"base-400": "#0d1117"` (one step darker than `#111827`) as a fourth tier to satisfy the AC without conflicting with existing usage. This is purely additive.

**IMPORTANT:** The AC says "referenced as `theme('colors.*')` in Tailwind utilities — no hardcoded hex values in templates." The existing templates (`base.html`, `dashboard.html`, etc.) correctly use only DaisyUI class names (`bg-base-100`, `text-success`, `border-base-300`, etc.). No template changes are needed for this AC — it is a constraint on future template development.

### Go Test Pattern (from Epic 3)

Reuse the render helper established in `handler_test.go`. The pattern is:

```go
package admin

import (
    "net/http/httptest"
    "strings"
    "testing"
    "os"
)

func TestBaseLayoutDataTheme(t *testing.T) {
    h := newTestHandler(t)  // reuse existing helper from handler_test.go
    w := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/admin/dashboard", nil)
    // set session cookie if needed to reach dashboard, or use login page
    h.ServeHTTP(w, req)
    body := w.Body.String()
    if !strings.Contains(body, `data-theme="obsidian"`) {
        t.Errorf("expected data-theme=\"obsidian\" in rendered HTML, got: %s", body[:min(200, len(body))])
    }
}

func TestTailwindConfigFontSizeExtensions(t *testing.T) {
    data, err := os.ReadFile("tailwind.config.js")
    if err != nil {
        t.Fatalf("cannot read tailwind.config.js: %v", err)
    }
    content := string(data)
    for _, key := range []string{`'display'`, `'heading'`, `'body'`, `'caption'`, `'mono'`} {
        if !strings.Contains(content, key) {
            t.Errorf("tailwind.config.js missing fontSize key %s", key)
        }
    }
    if !strings.Contains(content, "fontSize") {
        t.Error("tailwind.config.js missing 'fontSize' extension block")
    }
}
```

**Note:** `os.ReadFile("tailwind.config.js")` works in Go test because `go test` sets the working directory to the package directory (`gateway/internal/admin/`).

Check `handler_test.go` for the actual helper name (`newTestHandler` or similar) — adapt accordingly. If no helper exists for dashboard rendering without auth, use the login or bootstrap page render instead (both are accessible without a session).

### Playwright Test Pattern (from Epic 3 / E2E tests)

Reference: `e2e/tests/features/admin/bootstrap.spec.ts`

```typescript
import { test, expect } from '@playwright/test';

test.describe('Obsidian Theme', () => {
  test('html element has data-theme="obsidian"', async ({ page }) => {
    // bootstrap page is publicly accessible — no login required
    await page.goto('/admin/bootstrap');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'obsidian');
  });
});
```

Place in `e2e/tests/features/admin/obsidian-theme.spec.ts`. No `page.waitForTimeout` calls — Playwright's built-in auto-waiting handles the attribute assertion.

### `make build-admin-css` — How It Works

The target runs inside `node:22-alpine` Docker container (no local Node.js required):
```
$(DOCKER_NODE) sh -c "\
    cd gateway/internal/admin && \
    npm install --silent tailwindcss@3 daisyui@4 && \
    npx tailwindcss \
        --config tailwind.config.js \
        --input tailwind.input.css \
        --output static/admin.css \
        --minify"
```

After adding `fontSize` extensions, run `make build-admin-css` to regenerate `static/admin.css`. The new font-size utilities (e.g. `text-display`, `text-heading`) will appear in the compiled CSS only if they are referenced in at least one template file — Tailwind's purge/content scan removes unused classes. For this story, the utilities don't need to be in templates yet; the config addition alone satisfies the AC.

### Files to Touch

| File | Action |
|------|--------|
| `gateway/internal/admin/tailwind.config.js` | MODIFY — add `fontSize` extension block; add `base-400` color |
| `gateway/internal/admin/static/admin.css` | REGENERATE via `make build-admin-css` (then commit) |
| `gateway/internal/admin/obsidian_theme_test.go` | CREATE — two Go unit tests |
| `e2e/tests/features/admin/obsidian-theme.spec.ts` | CREATE — one Playwright E2E test |

**Do NOT modify:**
- `gateway/internal/admin/templates/layouts/base.html` — `data-theme="obsidian"` already present
- `gateway/internal/admin/static.go` — embed directive already covers `static/admin.css`
- `Makefile` — `build-admin-css` target already correct
- Any existing `*_test.go` files
- `gateway/internal/admin/tailwind.input.css`

### Regression Risk: None

This story only adds to `tailwind.config.js`. Adding `fontSize` extensions is purely additive — existing DaisyUI classes (`text-sm`, `text-base`, `text-lg`, etc.) are Tailwind built-ins and are unaffected by adding custom named sizes in `extend`. The DaisyUI `obsidian` theme block and all color extensions remain unchanged.

### References

- [Source: `_bmad-output/planning-artifacts/epics.md#Story-7.1`] — Authoritative AC
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md#Typography-System`] — Typography scale (rem, weights, line heights, letter spacing)
- [Source: `_bmad-output/planning-artifacts/ux-design-specification.md#Color-System`] — Obsidian palette (UX-DR1)
- [Source: `gateway/internal/admin/tailwind.config.js`] — Current state (DaisyUI obsidian theme + color extends)
- [Source: `gateway/internal/admin/templates/layouts/base.html`] — `data-theme="obsidian"` already on `<html>`
- [Source: `_bmad-output/implementation-artifacts/3-2-tailwind-css-daisyui-build-pipeline.md`] — Build pipeline details, embed pattern, test patterns
- [Source: `e2e/tests/features/admin/bootstrap.spec.ts`] — Playwright test pattern (no hard waits, locator assertions)
- [Source: `gateway/internal/admin/handler_test.go`] — Go test helper pattern (`package admin`, `httptest`)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

None.

### Completion Notes List

- RED phase: `TestTailwindConfigFontSizeExtensions` failed as expected (fontSize block absent); `TestBaseLayoutDataTheme` already passed (data-theme was already in base.html from Epic 3).
- GREEN phase: Added `fontSize` extension block with 5 named sizes (display, heading, body, caption, mono) per UX-DR2 spec. Added `base-400: "#0d1117"` color to complete the 4-tier background scale per UX-DR1. Both Go tests pass.
- `make build-admin-css` ran successfully; `static/admin.css` (51,098 bytes) regenerated with `:root` and `--p:` tokens present.
- Full `./internal/admin/...` test suite: all tests pass, zero regressions.
- Playwright test written at `e2e/tests/features/admin/obsidian-theme.spec.ts`; uses `toHaveAttribute` with Playwright auto-waiting (no hard waits).
- `base.html` already had `data-theme="obsidian"` on `<html>` — no modification required.
- No template files touch hardcoded hex values; all templates use DaisyUI class names.

### File List

gateway/internal/admin/tailwind.config.js
gateway/internal/admin/static/admin.css
gateway/internal/admin/obsidian_theme_test.go
e2e/tests/features/admin/obsidian-theme.spec.ts

## Change Log

- 2026-04-29: Added `fontSize` extension block (display/heading/body/caption/mono) to `tailwind.config.js`; added `base-400` color; wrote 2 Go unit tests + 1 Playwright E2E test; regenerated `static/admin.css` via `make build-admin-css`.

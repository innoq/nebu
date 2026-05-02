---
id: 7-13
security_review: not-needed
---

# Story 7.13: WCAG Audit / axe-core — Automatisierter Scan aller Admin-Seiten

Status: ready-for-dev

## Story

As an instance admin,
I want all Admin UI pages to pass an automated WCAG 2.1 AA accessibility scan using axe-core,
so that Nebu's admin interface is usable by people relying on assistive technologies and meets baseline accessibility standards.

## Acceptance Criteria

1. **`@axe-core/playwright` package installed** — `e2e/package.json` lists `@axe-core/playwright` as a dev dependency (latest stable version, currently `^4.10.x`). `e2e/package-lock.json` is updated accordingly. The package is imported in the accessibility spec without compilation errors (`tsc --noEmit` passes).

2. **Playwright spec `e2e/tests/features/admin/accessibility.spec.ts` exists** — The file scans all 7 post-login admin pages after a single login (login is performed once in `test.beforeEach` to avoid N OIDC round-trips). The pages tested are:
   - `/admin/` (dashboard)
   - `/admin/users`
   - `/admin/users/usr-001` (user detail panel — a known stub user ID)
   - `/admin/rooms`
   - `/admin/rooms/room-001` (room detail panel — a known stub room ID)
   - `/admin/config`
   - `/admin/compliance`
   - `/admin/audit-log`

3. **Each page passes axe-core WCAG 2.1 AA scan with zero critical or serious violations** — For each page, the spec calls `new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()` and asserts `results.violations.filter(v => v.impact === 'critical' || v.impact === 'serious')` is empty. `moderate` and `minor` violations are logged but do not fail the test.

4. **Any HTML template fixes required to eliminate critical/serious violations are applied** — If axe-core reports critical or serious violations against the existing templates, the developer fixes the templates inline (e.g., missing `aria-label`, missing `<label for>` associations, wrong heading hierarchy, missing `lang` attribute, images without `alt`). The fix goes into the relevant file under `gateway/internal/admin/templates/`.

5. **The spec runs in the existing Playwright test harness** — No changes to `playwright.config.ts` are needed. The spec is self-contained and uses the same `loginAsAdmin` auth helper pattern (Authorization Code + PKCE via Dex) as all other admin specs.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`dashboard page has zero critical/serious axe violations`** — Playwright / axe-core (`e2e/tests/features/admin/accessibility.spec.ts`)
   - Given: full dev stack running (`make dev`), bootstrap complete, admin logged in via OIDC Authorization Code + PKCE
   - When: navigate to `/admin/` and run `AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()`
   - Then: `violations.filter(v => v.impact === 'critical' || v.impact === 'serious')` is empty

2. **`users list page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/users` and run axe analysis
   - Then: zero critical/serious violations

3. **`user detail page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/users/usr-001` and run axe analysis
   - Then: zero critical/serious violations

4. **`rooms list page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/rooms` and run axe analysis
   - Then: zero critical/serious violations

5. **`room detail page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/rooms/room-001` and run axe analysis
   - Then: zero critical/serious violations

6. **`config page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/config` and run axe analysis
   - Then: zero critical/serious violations

7. **`compliance page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/compliance` and run axe analysis
   - Then: zero critical/serious violations

8. **`audit log page has zero critical/serious axe violations`** — Playwright / axe-core
   - Given: admin logged in
   - When: navigate to `/admin/audit-log` and run axe analysis
   - Then: zero critical/serious violations

## Tasks / Subtasks

- [ ] Task 1: Install `@axe-core/playwright` (AC: 1)
  - [ ] 1.1 Add `"@axe-core/playwright": "^4.10.1"` to `devDependencies` in `e2e/package.json`
  - [ ] 1.2 Run `npm install` inside `e2e/` to update `package-lock.json`
  - [ ] 1.3 Verify `tsc --noEmit` passes in `e2e/`

- [ ] Task 2: Write failing accessibility spec FIRST (AC: 2, 3, 5)
  - [ ] 2.1 Create `e2e/tests/features/admin/accessibility.spec.ts` with all 8 test cases
  - [ ] 2.2 Import `AxeBuilder` from `@axe-core/playwright`
  - [ ] 2.3 Implement `loginAsAdmin` helper (identical pattern to all other admin specs)
  - [ ] 2.4 Run the spec against the live stack — record all violations reported

- [ ] Task 3: Fix any critical/serious violations in Go templates (AC: 4)
  - [ ] 3.1 Triage axe output: separate critical/serious from moderate/minor
  - [ ] 3.2 Fix critical/serious violations in affected templates under `gateway/internal/admin/templates/`
  - [ ] 3.3 Common fixes to check first (see Dev Notes for known risk areas):
    - [ ] `<html lang="en">` present in `layouts/base.html` (already correct — verify)
    - [ ] All form `<input>` elements have associated `<label>` (check filter forms, inline-edit, confirm dialogs)
    - [ ] All icon-only buttons have `aria-label` (check confirm_dialog, inline_edit components)
    - [ ] Heading hierarchy (`h1` → `h2` → `h3`) is consistent across all pages
    - [ ] `<select>` elements in filter bars and role dropdowns have visible `<label>` or `aria-label`
    - [ ] No duplicate `id` attributes across components rendered multiple times on one page
  - [ ] 3.4 Re-run the spec after each fix batch until all 8 tests pass

- [ ] Task 4: Final verification (AC: 2, 3, 5)
  - [ ] 4.1 Run `make test-integration` (or `npx playwright test e2e/tests/features/admin/accessibility.spec.ts`) — all 8 tests green
  - [ ] 4.2 Run `tsc --noEmit` in `e2e/` — no TypeScript errors

## Dev Notes

### Package Installation

`@axe-core/playwright` is a thin wrapper around `axe-core` that integrates with `@playwright/test`. Latest stable as of 2026-04-30 is `4.10.1`. Install in `e2e/` (not in the gateway admin `package.json` — that one is for Tailwind/DaisyUI only).

```bash
cd e2e && npm install --save-dev @axe-core/playwright
```

### Spec Pattern

Every admin spec uses the same `loginAsAdmin` helper duplicated in each file. For this spec, follow the same pattern:

```typescript
import { test, expect, Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/admin/login/start');
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });
  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();
  const grantBtn = page.locator('button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")');
  if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
    await grantBtn.first().click();
  }
  await page.waitForURL(/\/admin\//, { timeout: 15_000 });
}
```

Each test follows this pattern:

```typescript
test('dashboard page has zero critical/serious axe violations', async ({ page }) => {
  await loginAsAdmin(page);
  await page.goto('/admin/');

  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze();

  const criticalOrSerious = results.violations.filter(
    v => v.impact === 'critical' || v.impact === 'serious'
  );

  if (criticalOrSerious.length > 0) {
    console.log('Violations:', JSON.stringify(criticalOrSerious, null, 2));
  }

  expect(criticalOrSerious).toHaveLength(0);
});
```

### Known Risk Areas in Current Templates

Based on inspection of `gateway/internal/admin/templates/`:

1. **`layouts/base.html`** — `<html lang="en">` is present. `<main>` landmark exists. Navigation has `aria-label="Admin navigation"`. Topbar status has `aria-live="polite"`. These are likely **fine**.

2. **`components/filter_bar.html`** — Contains `<select>` elements. These may lack `<label>` — high risk for "form-field-multiple-labels" or "label" violation. Fix: add `<label class="sr-only" for="...">` or `aria-label`.

3. **`components/inline_edit.html`** — Contains edit/save/cancel buttons. Icon-only or text-minimal buttons may need `aria-label`. Also check if the `<input>` has an associated `<label>` when the edit form is open.

4. **`components/confirm_dialog.html`** — `<dialog>` element. Check: dialog must have `aria-labelledby` pointing to a visible heading inside it.

5. **`components/search_input.html`** — `<input type="search">` — must have a `<label>` or `aria-label`.

6. **`components/wizard_stepper.html`** — Check that step indicators are accessible (e.g., `aria-current="step"` is already present per Story 7.11 AC, but verify the stepper has a `role="list"` or equivalent).

7. **`users.html`** — Uses `role="listbox"` + `role="option"` on `<ul>`/`<a>`. The `<main>` element is defined inside the `{{ define "master_list" }}` block which is already inside the `<main>` from `layouts/base.html`. This means there may be a **nested `<main>` violation** — the `master_list` defines its own `<main>` tag which gets nested inside the layout's `<main>`. This is a **likely critical violation**. Fix: change the inner `<main>` in `users.html` (and `rooms.html`) to a `<div>` or `<section>`.

8. **`dashboard.html`** — Inspect for heading hierarchy and landmark structure.

9. **Duplicate IDs** — `confirm_dialog` uses `id="confirm_dialog"` for the `<dialog>`. If two pages render the dialog, no conflict (it's one page each). But if the detail page renders both a user detail and a dialog, verify no duplication.

### Nested `<main>` Issue

The current `layouts/base.html` wraps content in `<main class="flex-1 overflow-auto p-8 max-w-screen-xl">`. However `users.html` defines its own `<main class="flex flex-col h-full">` inside `{{ define "master_list" }}`. This creates a nested `<main>` landmark, which is a WCAG failure. The fix is to change the inner `<main>` elements in `users.html` and `rooms.html` (and any other templates that define their own `<main>`) to `<div role="region">` or simply `<div>`.

Similarly inspect `compliance.html`, `config.html`, `audit_log.html`, `dashboard.html` for any self-contained `<main>` tags.

### Stub IDs for Detail Pages

- User detail: use `/admin/users/usr-001` (stub user "Alice Müller" — first entry in `stubUsers`)
- Room detail: use `/admin/rooms/room-001` (stub room "General" — first entry in `stubRooms`)

Verify the actual stub IDs in `gateway/internal/admin/stubs.go` before writing the spec.

### AxeBuilder API

`@axe-core/playwright` v4.x API:
- `new AxeBuilder({ page })` — constructor takes `{ page: Page }`
- `.withTags(['wcag2a', 'wcag2aa'])` — limit to WCAG 2.0 A + AA rules
- `.analyze()` — returns `Promise<AxeResults>`
- `AxeResults.violations` — array of `Result` objects, each with `.impact` (`'critical' | 'serious' | 'moderate' | 'minor'`)

Do NOT use `.include()` or `.exclude()` to hide violations — the whole page must be scanned.

### Template Fix Scope

Only fix critical and serious violations. Do not refactor templates beyond what's needed to pass the axe scan. Moderate and minor violations are noted but excluded from blocking the story.

### File Locations

| File | Purpose |
|------|---------|
| `e2e/package.json` | Add `@axe-core/playwright` devDependency |
| `e2e/package-lock.json` | Updated by `npm install` |
| `e2e/tests/features/admin/accessibility.spec.ts` | New Playwright spec (8 tests) |
| `gateway/internal/admin/templates/layouts/base.html` | Fix if violations found |
| `gateway/internal/admin/templates/users.html` | Likely fix: nested `<main>` |
| `gateway/internal/admin/templates/rooms.html` | Likely fix: nested `<main>` |
| `gateway/internal/admin/templates/components/*.html` | Fix as needed per axe output |

### Running the Tests

```bash
# Full integration stack + Playwright
make test-integration

# Faster: just run the accessibility spec against a running stack
cd e2e && npx playwright test tests/features/admin/accessibility.spec.ts --reporter=list

# TypeScript check
cd e2e && npx tsc --noEmit
```

The Playwright config (`playwright.config.ts`) uses `baseURL: process.env.NEBU_BASE_URL ?? 'http://localhost:8008'` and runs serially (`workers: 1`). No config changes needed.

### Project Structure Notes

- E2E test files live under `e2e/tests/features/admin/` — follow exactly
- Auth helper is duplicated per spec (no shared helper file exists) — keep this pattern
- `e2e/package.json` (the E2E package) is distinct from `gateway/internal/admin/package.json` (the Tailwind build package) — install axe-core in `e2e/` only
- `playwright.config.ts` sets `fullyParallel: false` and `workers: 1` — axe scans run serially like all other admin E2E tests

### References

- axe-core + Playwright integration: https://github.com/dequelabs/axe-core-npm/tree/develop/packages/playwright
- WCAG 2.1 AA: `wcag2a` + `wcag2aa` tags cover Level A and Level AA criteria
- Existing auth pattern: `e2e/tests/features/admin/compliance.spec.ts` (identical `loginAsAdmin`)
- Base layout: `gateway/internal/admin/templates/layouts/base.html`
- Stub data IDs: `gateway/internal/admin/stubs.go`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

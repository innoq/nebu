---
story_id: 9-26c
title: "Element Web E2E — Fix Admin Bootstrap Helper + Selector"
type: bugfix
severity: high
epic: 9
status: ready-for-dev
parent_story: 9-26
security_review: not-needed
created: 2026-05-07
---

## Summary

Sub-story of 9-26. Fixes BUG-E2E-03 and BUG-E2E-04 in the admin-ui E2E suite.

**BUG-E2E-03**: All 6 post-bootstrap admin-ui scenarios fail because `Given bootstrap has been completed` throws when bootstrap hasn't been done. There is no programmatic helper to complete bootstrap — each CI run starts with a fresh DB and needs bootstrap to be performed first.

**BUG-E2E-04**: `bootstrap.feature` / "Operator completes Bootstrap Wizard" fails with `strict mode violation: getByText('Bootstrap Setup') resolved to 2 elements` — the nav link and the page heading both match.

## Fix: BUG-E2E-03

`e2e/step-definitions/admin/bootstrap.steps.ts` — `Given bootstrap has been completed`:

Currently throws if bootstrap isn't done. Change to: **if bootstrap isn't done, run it**. Add a `doBootstrap()` helper function (or refactor the existing `When the operator completes the bootstrap wizard` step into a reusable helper):

```typescript
async function doBootstrap(page: Page): Promise<void> {
  // Navigate to /admin/ — will redirect to /admin/bootstrap if not done
  await page.goto('http://localhost:8008/admin/');
  if (!page.url().includes('/bootstrap')) return; // already done

  // Complete wizard step 1: admin credentials
  await page.getByLabel(/admin email/i).fill('admin@example.com');
  await page.getByLabel(/admin password/i).fill('changeme');
  await page.getByRole('button', { name: /next|continue/i }).click();

  // Step 2: confirm + submit
  await page.getByRole('button', { name: /complete|finish|save/i }).click();
  await expect(page).toHaveURL(/\/admin\/$/, { timeout: 15_000 });
}

Given('bootstrap has been completed', async ({ page }: { page: Page }) => {
  await doBootstrap(page);
});
```

Export `doBootstrap` for reuse in other steps.

## Fix: BUG-E2E-04

`e2e/step-definitions/admin/bootstrap.steps.ts` line ~130:

Replace:
```typescript
page.getByText('Bootstrap Setup', { exact: false })
```
With:
```typescript
page.getByRole('heading', { name: /bootstrap setup/i })
```

This targets only `<h1>`, `<h2>`, etc. — not nav links.

## Acceptance Tests

1. **BUG-E2E-03** — On a fresh `docker compose up` (no prior bootstrap), `npx playwright test --project=admin-ui` runs all 7 admin-ui scenarios (bootstrap wizard + 6 post-bootstrap scenarios) without any `SKIP: Bootstrap has not been completed` error.

2. **BUG-E2E-04** — `bootstrap.feature` / "Operator completes Bootstrap Wizard" passes without strict mode violation.

3. **Regression** — Pre-existing bootstrap step `When the operator completes the bootstrap wizard` still works (it reuses `doBootstrap()`).

## DoD

- All 7 admin-ui scenarios green on a fresh DB
- No `strict mode violation` on Bootstrap Setup text

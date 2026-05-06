---
name: playwright-support
code: playwright-support
description: Write or review Playwright specs for Admin UI flows. Real browser tests against the running stack — no mocks, no shortcuts.
---

# Playwright Support

## What Success Looks Like

Every acceptance criterion has a corresponding Playwright test. Tests run against the real stack. Tests use accessible selectors. Tests cover the happy path and the most important error state. Tests written before the template (TDD).

## Core Principles

- **Test-first**: Playwright specs are written before the template exists. The test fails first, then the template makes it pass.
- **Accessible selectors**: Use `getByRole`, `getByLabel`, `getByText` — not `locator('.btn-primary')` or `locator('#some-id')`. Accessible selectors verify accessibility while testing behavior.
- **Real stack**: Tests run against `http://localhost:8008` (or configured base URL). No mocks.
- **No cookie forging**: Test real login flows. OIDC Authorization Code + PKCE via real Keycloak.

## Selector Preference Order

```typescript
// 1. Role-based (best — also tests accessibility)
page.getByRole('button', { name: 'Create Room' })
page.getByRole('heading', { name: 'Room Management' })
page.getByRole('link', { name: 'Settings' })

// 2. Label-based (good for forms)
page.getByLabel('Room name')

// 3. Text-based (acceptable for content)
page.getByText('No rooms found')

// 4. Test-id (last resort — add data-testid to template)
page.getByTestId('room-list')

// Avoid: CSS selectors, nth-child, brittle DOM traversal
```

## Test Structure

```typescript
import { test, expect } from '@playwright/test';

test.describe('Admin Room Management', () => {
  test.beforeEach(async ({ page }) => {
    // Login via real OIDC flow
    await page.goto('/admin/login');
    // ... complete OIDC flow
  });

  test('displays room list when rooms exist', async ({ page }) => {
    await page.goto('/admin/rooms');
    await expect(page.getByRole('heading', { name: 'Rooms' })).toBeVisible();
    await expect(page.getByRole('table')).toBeVisible();
  });

  test('shows empty state when no rooms', async ({ page }) => {
    await page.goto('/admin/rooms');
    await expect(page.getByText('No rooms found')).toBeVisible();
  });

  test('keyboard: tab through room actions in order', async ({ page }) => {
    await page.goto('/admin/rooms');
    await page.keyboard.press('Tab');
    // Verify focus order matches visual order
    await expect(page.getByRole('link', { name: 'Create Room' })).toBeFocused();
  });
});
```

## Writing Test Stubs (TDD — before the template)

When planning a UI story, write the test first so it fails:

```typescript
test('AC1: admin can create a room', async ({ page }) => {
  // This test FAILS because /admin/rooms/create doesn't exist yet
  await page.goto('/admin/rooms/create');
  await page.getByLabel('Room name').fill('Test Room');
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page.getByText('Room created successfully')).toBeVisible();
});
```

## Reviewing Existing Tests

When reviewing Playwright specs, check:
- [ ] Uses accessible selectors (getByRole, getByLabel, getByText) not CSS
- [ ] No cookie forging or DB seeding to bypass auth flows
- [ ] No hard `waitForTimeout` — use `waitForSelector` or Playwright auto-waiting
- [ ] Error states tested with real server errors (not just UI state manipulation)
- [ ] All acceptance criteria have corresponding tests
- [ ] Keyboard navigation tested for interactive components

## Memory Integration

After writing or reviewing: note any new Playwright patterns or helper utilities established in session log.

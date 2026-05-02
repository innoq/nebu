/**
 * Story 7.2: MasterDetailLayout + DetailPanel (C4, C5) + bookmarkable URL routes
 *
 * These tests exercise the two-column master-detail layout for Users and Rooms.
 * Tests require the full dev stack to be running with a bootstrapped admin session.
 *
 * NOTE: These tests are written FIRST (before implementation code) as per the
 * Nebu ATDD standard. They will fail until the implementation is complete.
 *
 * Auth: The /admin/users and /admin/rooms routes are protected by SessionGuard.
 * The tests navigate through the full OIDC login flow (Authorization Code + PKCE)
 * before exercising the master-detail layout — reusing the login pattern from
 * bootstrap-happy-path.spec.ts.
 *
 * For CI / local dev: run `make dev` first, ensure bootstrap is complete (so
 * /admin/users is not redirected to /admin/bootstrap).
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code login via Dex
// and arrives on the dashboard (or any /admin/ page).
// ---------------------------------------------------------------------------
async function loginAsAdmin(page: Page): Promise<void> {
  // Navigate to login start — triggers OIDC redirect to Dex
  await page.goto('/admin/login/start');

  // Wait for Dex auth page
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

  // Fill Dex login form
  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();

  // Handle optional consent/grant page
  const grantBtn = page.locator(
    'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
  );
  if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
    await grantBtn.first().click();
  }

  // Should land back on /admin/ after callback
  await page.waitForURL(/\/admin\//, { timeout: 15_000 });
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------
test.describe('Master-Detail Layout', () => {

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page);
  });

  // AC1 + AC3 (Story 7.2): Users list page renders with stub users
  test('users list page renders with stub users', async ({ page }) => {
    await page.goto('/admin/users');

    // Master list nav landmark must be present
    await expect(page.locator('nav[aria-label="Item list"]')).toBeVisible();

    // At least one stub user must appear (Alice Müller is always in stubUsers)
    await expect(page.getByText('Alice Müller')).toBeVisible();
  });

  // AC6 (Story 7.2): Clicking a user opens detail panel and URL changes
  test('clicking a user opens detail panel and URL changes', async ({ page }) => {
    await page.goto('/admin/users');

    // Click the Alice Müller list item (role=option per WCAG spec)
    await page.getByRole('option', { name: /Alice Müller/ }).click();

    // URL must change to the user's detail route
    await expect(page).toHaveURL(/\/admin\/users\/usr-001/);

    // Detail panel section must become visible
    await expect(page.locator('section[role="region"]')).toBeVisible();

    // Detail panel must show the selected user's name
    await expect(page.locator('section[role="region"]')).toContainText('Alice Müller');
  });

  // AC3 (Story 7.2): Direct URL navigation pre-selects item and shows detail panel
  test('direct URL navigation pre-selects item and shows detail panel', async ({ page }) => {
    // Navigate directly to Bob Wagner's detail URL
    await page.goto('/admin/users/usr-002');

    // Detail panel must be visible immediately (server-side render)
    await expect(page.locator('section[role="region"]')).toBeVisible();

    // Detail panel must contain the correct user
    await expect(page.locator('section[role="region"]')).toContainText('Bob Wagner');

    // The corresponding list item must have aria-selected="true"
    await expect(page.locator('[aria-selected="true"]')).toBeVisible();
  });

  // AC5 (Story 7.2): Non-existent item renders 404-within-panel (not a full-page 404)
  test('nonexistent user ID renders not-found within panel', async ({ page }) => {
    await page.goto('/admin/users/nonexistent-id');

    // Must NOT redirect to a full 404 page (URL stays on /admin/users/nonexistent-id)
    await expect(page).not.toHaveURL(/404/);

    // The detail panel section must still render
    await expect(page.locator('section[role="region"]')).toBeVisible();

    // Must contain "not found" text inside the panel
    await expect(page.locator('section[role="region"]')).toContainText(/not found/i);
  });

  // AC4 (Story 7.2): Rooms direct URL navigation pre-selects item and shows detail panel
  test('rooms direct URL navigation shows room detail panel', async ({ page }) => {
    await page.goto('/admin/rooms/room-001');

    // Detail panel must be visible immediately (server-side render)
    await expect(page.locator('section[role="region"]')).toBeVisible();

    // Detail panel must contain the correct room name
    await expect(page.locator('section[role="region"]')).toContainText('General');

    // The corresponding list item must have aria-selected="true"
    await expect(page.locator('[aria-selected="true"]')).toBeVisible();
  });

  // AC2 (Story 7.2): Mobile viewport shows full-screen overlay when item is selected
  test('mobile viewport shows full-screen overlay on item select', async ({ page }) => {
    // Set mobile viewport (375px width per AC spec)
    await page.setViewportSize({ width: 375, height: 667 });

    // Navigate directly to a detail URL — forces the overlay state
    await page.goto('/admin/users/usr-001');

    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // On mobile with an active item, the master list column should be hidden
    // (Tailwind: `hidden md:block` — only visible on desktop)
    const masterList = page.locator('nav[aria-label="Item list"]');
    await expect(masterList).toBeHidden();
  });

});

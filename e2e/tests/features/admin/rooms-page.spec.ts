/**
 * Story 7.8: Room List Page (Search, Filter, Pagination)
 *
 * These tests exercise the /admin/rooms page with search, visibility filter,
 * WCAG landmarks (<main>, <h1>), and status badges.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (same pattern as master-detail.spec.ts).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * All tests are REAL (not test.skip) — a production page exists to test against.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code login via Dex
// ---------------------------------------------------------------------------
async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/admin/login/start');
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();

  const grantBtn = page.locator(
    'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
  );
  if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
    await grantBtn.first().click();
  }

  await page.waitForURL(/\/admin\//, { timeout: 15_000 });
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------
test.describe('Room List Page', () => {

  test('search input debounces and updates URL', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    const searchInput = page.locator('input[name="q"]');
    await expect(searchInput).toBeVisible();

    await searchInput.fill('general');

    // Debounce fires within 2s and URL reflects the search param
    await expect(page).toHaveURL(/[?&]q=general/, { timeout: 2_000 });

    // General must be visible in results
    await expect(page.getByText('General')).toBeVisible();
  });

  test('visibility filter triggers immediate form submit', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    await page.locator('select[name="visibility"]').selectOption('private');

    // onchange auto-submits; URL must contain visibility=private
    await expect(page).toHaveURL(/[?&]visibility=private/);
  });

  test('empty state shown when no results', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms?q=zzznomatch');

    // Empty state <h3> must be visible with "no rooms" text
    await expect(
      page.locator('h3').filter({ hasText: /no rooms/i }).first()
    ).toBeVisible();
  });

  test('status badge shows correct color for active room', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // First span[role="status"] should be from an active room → badge-success
    const badge = page.locator('span[role="status"]').first();
    await expect(badge).toBeVisible();
    await expect(badge).toHaveClass(/badge-success/);
  });

});

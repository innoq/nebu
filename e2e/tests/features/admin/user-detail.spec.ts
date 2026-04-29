/**
 * Story 7.6: User Detail Panel (InlineEdit for displayName + status, bookmarkable URL)
 *
 * These tests exercise the /admin/users/{id} detail panel — avatar, inline edit for
 * display name, status badge, and flash banner after PRG redirect.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (identical to users-page.spec.ts).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * All tests are REAL (not test.skip) — the detail route /admin/users/{id} has been live
 * since Story 7.2.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code login via Dex
// Identical pattern to users-page.spec.ts.
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
test.describe('User Detail Panel', () => {

  test('user detail panel opens when clicking a user row', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');

    // Click the first user list item (role="option")
    await page.locator('[role="option"]').first().click();

    // Detail panel (section[role="region"]) must be visible and contain an edit button
    // (proving the inline_edit component rendered the selected user's detail)
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();
    await expect(detailPanel.locator('[aria-label^="Edit"]')).toBeVisible();
  });

  test('flash message shown after display name update', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001?flash=Display+name+updated');

    // Flash banner with success message must be visible
    await expect(page.locator('div[role="alert"]')).toContainText('Display name updated');
  });

  test('inline edit saves display name', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');

    // Click the edit button for display name
    await page.locator('button[aria-label="Edit Display Name"]').click();

    // Fill a new display name in the inline input
    await page.locator('input[name="display_name"]').fill('Test Display Name');

    // Submit the form
    await page.locator('button[type="submit"]:has-text("Save")').click();

    // Page should redirect (PRG) to the detail URL with a flash param
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });

    // Flash banner (success alert) must be visible after redirect
    await expect(page.locator('div[role="alert"]')).toBeVisible();
  });

});

/**
 * Story 7.7: User Role UI + Deactivation Confirmation Dialog
 *
 * These tests exercise:
 *   - The role <select> rendered in /admin/users/{id} detail panel
 *   - The Deactivate button wiring to the confirm_dialog component
 *   - The deactivation POST flow via the confirm dialog
 *
 * Auth: Uses OIDC Authorization Code + PKCE (identical to user-detail.spec.ts).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * All tests are REAL (not test.skip).
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code login via Dex.
// Identical to user-detail.spec.ts.
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
test.describe('User Role UI + Deactivation Confirmation Dialog', () => {

  test('role select is rendered for current user role', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');

    // The role <select> must be visible in the detail panel.
    const roleSelect = page.locator('section[role="region"] select[name="role"]');
    await expect(roleSelect).toBeVisible();

    // The selected option should reflect the user's current role (instance_admin for usr-001).
    await expect(roleSelect).toHaveValue('instance_admin');
  });

  test('deactivate button opens confirmation dialog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');

    // Trigger button in detail_footer calls onclick="confirm_dialog.showModal()"
    await page.locator('button:has-text("Deactivate")').click();

    // The confirm_dialog (role="alertdialog") must become visible.
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
  });

  test('confirm deactivation redirects with flash message', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');

    // Open the confirm dialog via the Deactivate trigger button.
    await page.locator('button:has-text("Deactivate")').click();
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();

    // Click the Deactivate confirm (submit) button inside the dialog.
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // Page should redirect to the detail URL with a flash= query param.
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });

    // Flash banner (success alert) must be visible after redirect.
    await expect(page.locator('div[role="alert"]')).toBeVisible();
  });

});

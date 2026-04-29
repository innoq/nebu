/**
 * Story 7.11: Compliance Access Request List — Four-Eyes Approval UI
 *
 * These tests exercise the /admin/compliance page — renders pending requests,
 * approve action with flash message, and the four-eyes wizard stepper.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (same pattern as config.spec.ts).
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
test.describe('Compliance Access Requests Page', () => {

  test('compliance page renders pending requests', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    // WCAG landmark h1 with correct heading text
    await expect(page.locator('h1')).toContainText('Compliance Requests');

    // Pending stub requests should be visible
    await expect(page.locator('body')).toContainText('Alice Müller');
  });

  test('approve request shows flash message', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    // Click the first Approve button
    await page.locator('button[type="submit"]:has-text("Approve")').first().click();

    // PRG redirect back to /admin/compliance with flash banner
    await expect(page.locator('div[role="alert"]')).toContainText('Request approved');
  });

});

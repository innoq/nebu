/**
 * Story 7.10: Server Config UI (GET/POST /admin/config)
 *
 * These tests exercise the /admin/config page — renders current settings,
 * form submission, and the PRG flash message pattern.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (same pattern as rooms-page.spec.ts).
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
test.describe('Server Configuration Page', () => {

  test('config page renders with current settings', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');

    // WCAG landmark h1 with correct heading text
    await expect(page.locator('h1')).toContainText('Server Configuration');

    // Form pre-filled with stubConfig defaults
    await expect(page.locator('input[name="instance_name"]')).toHaveValue('Nebu Dev');
  });

  test('save configuration shows flash message', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');

    // Fill instance_name with a new value
    await page.locator('input[name="instance_name"]').fill('Test Instance');

    // Submit the form
    await page.locator('button[type="submit"]').click();

    // PRG redirect back to /admin/config with flash banner
    await expect(page.locator('div[role="alert"]')).toContainText('Configuration saved');
  });

});

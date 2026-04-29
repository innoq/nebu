/**
 * Story 7.12: Audit Log View — Atlas Pattern, Zeitraum-Filter, Read-Only
 *
 * These tests exercise the /admin/audit-log page — renders all stub entries by
 * default and applies the date-range filter to reduce visible rows.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (same pattern as compliance.spec.ts).
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
test.describe('Audit Log Page', () => {

  test('audit log page renders all entries by default', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/audit-log');

    // WCAG landmark h1 with correct heading text
    await expect(page.locator('h1')).toContainText('Audit Log');

    // At least one actor from the stub data should be visible
    await expect(page.locator('body')).toContainText('kai@example.com');

    // All 6 stub entries span 3 dates — spot-check two unique action names
    await expect(page.locator('body')).toContainText('config.update');
    await expect(page.locator('body')).toContainText('compliance.approve');
  });

  test('date filter reduces visible entries', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/audit-log');

    // Fill in date range: 2026-04-29 only
    await page.locator('input[name="from"]').fill('2026-04-29');
    await page.locator('input[name="to"]').fill('2026-04-29');
    await page.locator('button[type="submit"]').click();

    // Wait for filtered page to load
    await page.waitForURL(/from=2026-04-29/, { timeout: 10_000 });

    // Entries on 2026-04-29 should be present
    await expect(page.locator('body')).toContainText('config.update');

    // al-001 is on 2026-04-28 — "Dieter Krause" should not appear
    await expect(page.locator('body')).not.toContainText('Dieter Krause');
  });

});

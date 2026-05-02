/**
 * Story 7.13: WCAG Audit / axe-core — Automatisierter Scan aller Admin-Seiten
 *
 * Scans all 8 post-login admin pages for WCAG 2.1 AA violations using axe-core.
 * Critical and serious violations fail the test. Moderate and minor violations
 * are logged but do not cause a test failure.
 *
 * Auth: OIDC Authorization Code + PKCE via Dex (same pattern as all other admin specs).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * All tests are REAL (not test.skip).
 */
import { test, expect, Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

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
// Helper: run axe and assert zero critical/serious violations
// ---------------------------------------------------------------------------
async function assertNoAxeViolations(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze();

  const criticalOrSerious = results.violations.filter(
    (v) => v.impact === 'critical' || v.impact === 'serious'
  );

  const moderateOrMinor = results.violations.filter(
    (v) => v.impact === 'moderate' || v.impact === 'minor'
  );

  if (moderateOrMinor.length > 0) {
    console.log(
      `[axe] ${moderateOrMinor.length} moderate/minor violation(s) on ${page.url()} (not blocking):`,
      moderateOrMinor.map((v) => `${v.id}: ${v.description}`).join('; ')
    );
  }

  if (criticalOrSerious.length > 0) {
    console.log(
      'Critical/serious violations:',
      JSON.stringify(criticalOrSerious, null, 2)
    );
  }

  expect(criticalOrSerious, 'Expected zero critical or serious axe violations').toHaveLength(0);
}

// ---------------------------------------------------------------------------
// Test suite — one test per admin page
// ---------------------------------------------------------------------------
test.describe('Admin UI WCAG 2.1 AA accessibility', () => {

  test('dashboard page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/dashboard');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('users list page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('user detail page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('rooms list page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('room detail page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms/room-001');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('config page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('compliance page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

  test('audit log page has zero critical/serious axe violations', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/audit-log');
    await page.waitForLoadState('networkidle');
    await assertNoAxeViolations(page);
  });

});

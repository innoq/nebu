/**
 * Story 7.15: Role Mapping configuration page (GET/POST /admin/config/role-mapping)
 *
 * These tests exercise the /admin/config/role-mapping page — renders current stub
 * defaults, form submission with flash message, validation error rendering, and
 * sidebar nav active state.
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
test.describe('Role Mapping Configuration Page', () => {

  // After each test that may have mutated stubRoleMappingConfig, reset to defaults.
  test.afterEach(async ({ page }) => {
    await page.goto('/admin/config/role-mapping');
    await page.locator('input[name="oidc_group_claim"]').fill('groups');
    await page.locator('input[name="instance_admin_group"]').fill('instance_admin');
    await page.locator('input[name="compliance_user_group"]').fill('');
    await page.locator('button[type="submit"]').click();
    await page.waitForURL(/role-mapping/, { timeout: 5_000 });
  });

  test('role mapping page renders with defaults', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    // WCAG landmark h1 with correct heading text
    await expect(page.locator('h1')).toContainText('Role Mapping');

    // Form pre-filled with stubRoleMappingConfig defaults
    await expect(page.locator('input[name="oidc_group_claim"]')).toHaveValue('groups');
    await expect(page.locator('input[name="instance_admin_group"]')).toHaveValue('instance_admin');

    // Sidebar nav link is active when on this page
    await expect(page.locator('a[data-navkey="role-mapping"]')).toHaveAttribute('aria-current', 'page');
  });

  test('save valid role mapping shows flash message', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    // Fill fields with new values
    await page.locator('input[name="oidc_group_claim"]').fill('roles');
    await page.locator('input[name="instance_admin_group"]').fill('admins');

    // Submit the form
    await page.locator('button[type="submit"]').click();

    // PRG redirect back to /admin/config/role-mapping with flash banner
    await expect(page.locator('div[role="alert"]')).toContainText('Role mapping saved');
  });

  test('invalid claim name shows validation error', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    // Clear and fill claim with invalid value (space not allowed)
    await page.locator('input[name="oidc_group_claim"]').fill('my group');

    // Submit the form
    await page.locator('button[type="submit"]').click();

    // No redirect — page re-renders at same URL with validation error
    await expect(page).toHaveURL(/\/admin\/config\/role-mapping/);
    // Error message should be visible for oidc_group_claim
    await expect(page.locator('p.text-error')).toBeVisible();
  });

  test('nav link Role Mapping is present and active when on page', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    // Sidebar link exists and has aria-current="page" when on this page
    const navLink = page.locator('a[data-navkey="role-mapping"]');
    await expect(navLink).toBeVisible();
    await expect(navLink).toHaveAttribute('aria-current', 'page');
    await expect(navLink).toContainText('Role Mapping');
  });

});

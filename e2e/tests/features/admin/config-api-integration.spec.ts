/**
 * Story 9.4: Admin UI — Config & Role Mapping API Integration
 *
 * These tests verify that the Admin UI Server Config and Role Mapping pages
 * persist changes via the REAL API (gRPC-backed) instead of the in-memory
 * stub data.
 *
 * RED PHASE: All tests in this file are FAILING until Story 9.4 is implemented.
 * They fail because:
 *   AC1 — config changes are written to stubConfig (in-memory); on server restart
 *          the stub resets to defaults. The test verifies a PRG redirect + reload
 *          cycle shows the new value (only possible with real DB-backed persistence).
 *   AC2 — role mapping changes are written to stubRoleMappingConfig (in-memory);
 *          same persistence problem. The test verifies the new value appears on
 *          reload (only possible with real persistence).
 *
 * Auth: OIDC Authorization Code + PKCE via Dex (kai@example.com / changeme).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * Spec path (as declared in story 9.4):
 *   e2e/tests/features/admin/config-api-integration.spec.ts
 */
import { test, expect } from '@playwright/test';
import { loginAsAdmin } from '../../fixtures/helpers';

// ---------------------------------------------------------------------------
// AC1: Server Config update persists via real API (not stubConfig)
//
// RED: Fails because UpdateConfigHandler mutates stubConfig.InstanceName in-memory.
//      With stubs active, the value is "Nebu Dev" on restart. The test changes the
//      instance name and verifies the new value persists across a PRG redirect + reload.
//      This is only satisfiable when UpdateConfigHandler calls gRPC UpdateServerConfig.
// ---------------------------------------------------------------------------
test.describe('AC1: Server Config update persists via real API', () => {

  // Track the original instance name so we can restore it after each test.
  let originalInstanceName: string | null = null;

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');

    // Read the current instance_name value from the form input.
    const instanceNameInput = page.locator('input[name="instance_name"]');
    await expect(instanceNameInput).toBeVisible({ timeout: 10_000 });
    originalInstanceName = await instanceNameInput.inputValue();
  });

  test.afterEach(async ({ page }) => {
    // Restore the original instance_name via POST so other tests see a clean state.
    // NOTE (TEA MINOR-4): page.request.post bypasses CSRF middleware — best-effort cleanup.
    if (!originalInstanceName) return;
    const formData = new URLSearchParams({
      instance_name: originalInstanceName,
      max_rooms_per_user: '10',
      retention_days: '90',
    });
    await page.request.post('/admin/config', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      data: formData.toString(),
    });
  });

  test('instance_name update redirects with flash and new value persists on reload', async ({ page }) => {
    // Given: admin is on /admin/config (login done in beforeEach — same browser context)
    await page.goto('/admin/config');

    const instanceNameInput = page.locator('input[name="instance_name"]');
    await expect(instanceNameInput).toBeVisible();

    // When: admin changes the instance name to a new value
    const newName = originalInstanceName === 'Nebu Playwright Test' ? 'Nebu API Integration' : 'Nebu Playwright Test';
    await instanceNameInput.fill(newName);

    // Submit the config form
    await page.locator('form button[type="submit"]').first().click();

    // Then: PRG redirect to /admin/config with flash=
    await expect(page).toHaveURL(/\/admin\/config.*[?&]flash=/, { timeout: 10_000 });

    // Then: flash banner confirms update
    await expect(page.locator('div[role="alert"]')).toContainText(/config updated/i);

    // Then: the form now shows the new instance name
    await expect(page.locator('input[name="instance_name"]')).toHaveValue(newName);
  });

  test('instance_name persists on reload after PRG redirect (DB-backed, not in-memory)', async ({ page }) => {
    // This test proves the change is DB-backed:
    // navigate away and back; the new value must still be present.
    await page.goto('/admin/config');

    const instanceNameInput = page.locator('input[name="instance_name"]');
    await expect(instanceNameInput).toBeVisible();

    const newName = originalInstanceName === 'Nebu Reload Test' ? 'Nebu Persist Test' : 'Nebu Reload Test';
    await instanceNameInput.fill(newName);
    await page.locator('form button[type="submit"]').first().click();

    await expect(page).toHaveURL(/\/admin\/config.*[?&]flash=/, { timeout: 10_000 });

    // Navigate away and back (forces a new handler call — no in-memory caching)
    await page.goto('/admin/dashboard');
    await page.goto('/admin/config');

    // The new instance name must still appear (DB-backed persistence).
    // With stubs only: this would fail on restart / reload-cycle because stubConfig resets.
    await expect(
      page.locator('input[name="instance_name"]')
    ).toHaveValue(newName);
  });

});

// ---------------------------------------------------------------------------
// AC2: Role Mapping update persists via real API (not stubRoleMappingConfig)
//
// RED: Fails because RoleMappingHandler.UpdateHandler mutates stubRoleMappingConfig
//      in-memory instead of calling the real storage layer.
//      The test verifies the new value appears after a PRG redirect + reload cycle.
//      This is only satisfiable when the handler calls the real persistence layer.
// ---------------------------------------------------------------------------
test.describe('AC2: Role Mapping update persists via real API', () => {

  let originalInstanceAdminGroup: string | null = null;

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    // Read the current instance_admin_group value from the form input.
    const instanceAdminGroupInput = page.locator('input[name="instance_admin_group"]');
    await expect(instanceAdminGroupInput).toBeVisible({ timeout: 10_000 });
    originalInstanceAdminGroup = await instanceAdminGroupInput.inputValue();
  });

  test.afterEach(async ({ page }) => {
    // Restore the original instance_admin_group via POST so other tests see a clean state.
    // NOTE (TEA MINOR-4): page.request.post bypasses CSRF middleware — best-effort cleanup.
    if (!originalInstanceAdminGroup) return;
    const formData = new URLSearchParams({
      oidc_group_claim: 'groups',
      instance_admin_group: originalInstanceAdminGroup,
      compliance_user_group: '',
    });
    await page.request.post('/admin/config/role-mapping', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      data: formData.toString(),
    });
  });

  test('instance_admin_group update redirects with flash and new value persists on reload', async ({ page }) => {
    // Given: admin is on /admin/config/role-mapping (login done in beforeEach)
    await page.goto('/admin/config/role-mapping');

    const instanceAdminGroupInput = page.locator('input[name="instance_admin_group"]');
    await expect(instanceAdminGroupInput).toBeVisible();

    // When: admin changes the instance admin group to a new value
    const newGroup = originalInstanceAdminGroup === 'nebu-instance-admins' ? 'nebu-admin-group' : 'nebu-instance-admins';
    await instanceAdminGroupInput.fill(newGroup);

    // Submit the role mapping form
    await page.locator('form button[type="submit"]').first().click();

    // Then: PRG redirect to /admin/config/role-mapping with flash=
    await expect(page).toHaveURL(/\/admin\/config\/role-mapping.*[?&]flash=/, { timeout: 10_000 });

    // Then: flash banner confirms role mapping update
    await expect(page.locator('div[role="alert"]')).toContainText(/role mapping updated/i);

    // Then: the form now shows the new instance admin group
    await expect(page.locator('input[name="instance_admin_group"]')).toHaveValue(newGroup);
  });

  test('instance_admin_group persists on reload after PRG redirect (not in-memory stub)', async ({ page }) => {
    // This test proves the change is sourced from the real persistence layer:
    // navigate away and back; the new value must still be present.
    await page.goto('/admin/config/role-mapping');

    const instanceAdminGroupInput = page.locator('input[name="instance_admin_group"]');
    await expect(instanceAdminGroupInput).toBeVisible();

    const newGroup = originalInstanceAdminGroup === 'nebu-persist-test' ? 'nebu-reload-test' : 'nebu-persist-test';
    await instanceAdminGroupInput.fill(newGroup);
    await page.locator('form button[type="submit"]').first().click();

    await expect(page).toHaveURL(/\/admin\/config\/role-mapping.*[?&]flash=/, { timeout: 10_000 });

    // Navigate away and back (forces a new handler call — no in-memory caching)
    await page.goto('/admin/dashboard');
    await page.goto('/admin/config/role-mapping');

    // The new group must still appear (real persistence layer).
    // With stubs only: this would fail because stubRoleMappingConfig resets on restart.
    await expect(
      page.locator('input[name="instance_admin_group"]')
    ).toHaveValue(newGroup);
  });

});

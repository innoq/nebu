/**
 * Shared helper utilities for Nebu E2E tests.
 *
 * Exports: isElementReachable, isDexReachable, dismissKeyDialog, loginAsAdmin
 *
 * AC 1 — Story 4-29: extracted from element_e2e.spec.ts into a shared module.
 * Story 9.2: added loginAsAdmin (previously duplicated across admin specs).
 */

import { expect, type Page } from '@playwright/test';
import { ELEMENT_URL, DEX_HEALTH } from './oidc';

/**
 * Check if Element Web is reachable at localhost:7070.
 * Used in test.beforeAll() guards — auto-skips tests when stack is down.
 */
export async function isElementReachable(): Promise<boolean> {
  try {
    const resp = await fetch(ELEMENT_URL);
    return resp.ok;
  } catch {
    return false;
  }
}

/**
 * Check if Dex OIDC provider is reachable at localhost:5556.
 * Used in test.beforeAll() guards alongside isElementReachable.
 */
export async function isDexReachable(): Promise<boolean> {
  try {
    const resp = await fetch(DEX_HEALTH);
    return resp.ok;
  } catch {
    return false;
  }
}

/**
 * Dismiss the "Setting up keys" / "Unable to set up keys" dialog that Element Web
 * shows on first login when cross-signing is not available (MVP stub returns empty counts).
 *
 * Idempotent — safe to call when dialog is not present.
 */
export async function dismissKeyDialog(page: Page): Promise<void> {
  await Promise.race([
    page.locator('[placeholder*="Search"]').first().waitFor({ state: 'visible', timeout: 30_000 }),
    page.getByRole('button', { name: /cancel|abbrechen/i }).waitFor({ state: 'visible', timeout: 30_000 }),
  ]).catch(() => {});

  const cancelBtn = page.getByRole('button', { name: /cancel|abbrechen/i });
  if (await cancelBtn.isVisible({ timeout: 1_000 }).catch(() => false)) {
    await cancelBtn.click();
  }
}

/**
 * Performs OIDC Authorization Code + PKCE login as the bootstrap admin
 * via Dex (kai@example.com / changeme) and waits until the Admin UI is reached.
 *
 * Story 9.2: shared helper extracted to remove the per-spec duplication that
 * existed across rooms-page.spec.ts, smoke-flows.spec.ts, role-mapping.spec.ts, etc.
 *
 * Note: the existing per-spec copies are intentionally not refactored here to
 * keep the Story 9.2 diff focused; new specs should import this helper.
 */
export async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/admin/login/start');
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();

  // Dex consent screen may or may not appear depending on prior grants.
  // Wait for either the Admin UI URL OR a Grant/Confirm button, whichever wins.
  await Promise.race([
    page.waitForURL(/\/admin\//, { timeout: 15_000 }),
    page
      .locator('button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")')
      .first()
      .waitFor({ state: 'visible', timeout: 15_000 }),
  ]);

  const grantBtn = page.locator(
    'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
  );
  if (await grantBtn.first().isVisible().catch(() => false)) {
    await grantBtn.first().click();
    await page.waitForURL(/\/admin\//, { timeout: 15_000 });
  }

  await expect(page).toHaveURL(/\/admin\//);
}

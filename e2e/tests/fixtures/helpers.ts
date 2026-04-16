/**
 * Shared helper utilities for Nebu E2E tests.
 *
 * Exports: isElementReachable, isDexReachable, dismissKeyDialog
 *
 * AC 1 — Story 4-29: extracted from element_e2e.spec.ts into a shared module.
 */

import { type Page } from '@playwright/test';
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

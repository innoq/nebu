/**
 * Step definitions for features/element/login.feature
 *
 * Story 9-26 — Phase 2, AC6.
 *
 * Implements: SSO login via Dex, logout, cached session check.
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { DEX_TEST_PASSWORD } from '../../fixtures/users';
import * as fs from 'fs';
import * as path from 'path';

const ELEMENT_URL     = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';
const AUTH_STATE_DIR  = path.join(__dirname, '../../auth-state');

/**
 * "Given alex has no cached session"
 *
 * Clears any existing storageState so the login flow starts fresh.
 * Also clears localStorage in the current page context.
 */
Given('{word} has no cached session', async ({ page }: { page: Page }, userName: string) => {
  // Clear the cached storageState file for this user
  const statePath = path.join(AUTH_STATE_DIR, `${userName}.json`);
  if (fs.existsSync(statePath)) {
    fs.unlinkSync(statePath);
  }

  // Clear browser localStorage so the page starts fresh
  await page.goto(ELEMENT_URL);
  await page.evaluate(() => {
    try { window.localStorage.clear(); } catch { /* ignore cross-origin */ }
  });
});

/**
 * "When alex opens Element Web and clicks "Sign in""
 *
 * Navigates to Element Web and clicks the Sign in link.
 */
When(
  '{word} opens Element Web and clicks {string}',
  async ({ page }: { page: Page }, _userName: string, _buttonText: string) => {
    await page.goto(ELEMENT_URL);

    // Wait for the sign-in link (Element welcome screen)
    const signInLink = page.getByRole('link', { name: /sign in|anmelden/i });
    await signInLink.waitFor({ state: 'visible', timeout: 15_000 });
    await signInLink.click();
  }
);

/**
 * "When alex authenticates via Dex with {string}"
 *
 * Fills Dex login form and handles optional consent screen.
 * On success, waits for .mx_LeftPanel.
 */
When(
  '{word} authenticates via Dex with {string}',
  async ({ page }: { page: Page }, _userName: string, email: string) => {
    // Wait for SSO button on Element login page
    const ssoBtn = page.getByRole('button', { name: /continue with sso|mit sso|weiter mit sso/i });
    await ssoBtn.waitFor({ state: 'visible', timeout: 15_000 });
    await ssoBtn.click();

    // Dex login page
    await page.waitForURL(/dex.*\/auth/i, { timeout: 20_000 });
    await page.locator('input[name="login"]').fill(email);
    await page.locator('input[name="password"]').fill(DEX_TEST_PASSWORD);
    await page.locator('button[type="submit"]').click();

    // Handle optional Dex consent screen (first login)
    const consentBtn = page.getByRole('button', { name: /grant access|allow|approve|confirm/i });
    if (await consentBtn.isVisible({ timeout: 6_000 }).catch(() => false)) {
      await consentBtn.click();
    }

    // Wait for redirect back to Element Web
    await page.waitForURL(/localhost:7070/, { timeout: 30_000 });

    // Dismiss key-setup dialog if it appears (first login)
    await Promise.race([
      page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 25_000 }),
      page.getByRole('button', { name: /cancel|abbrechen/i })
        .waitFor({ state: 'visible', timeout: 25_000 }),
    ]).catch((e: Error) => { if (!e.message?.includes('Timeout')) throw e; });

    const cancelBtn = page.getByRole('button', { name: /cancel|abbrechen/i });
    if (await cancelBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await cancelBtn.click();
    }

    // Confirm .mx_LeftPanel is now visible
    await page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 20_000 });
  }
);

/**
 * "When alex opens the user menu and clicks {string}"
 *
 * Opens the user menu and clicks the specified menu item (e.g. "Sign out").
 */
When(
  '{word} opens the user menu and clicks {string}',
  async ({ page }: { page: Page }, _userName: string, menuItem: string) => {
    // Element renders user info / avatar in the top-left user menu
    const userMenuTrigger = page.locator(
      '.mx_UserMenu button, [data-testid="user-menu-trigger"], .mx_UserMenu_userAvatar'
    ).first();

    await userMenuTrigger.waitFor({ state: 'visible', timeout: 10_000 });
    await userMenuTrigger.click();

    // Click the menu item (e.g. "Sign out" / "Abmelden")
    const menuItemLocator = page.getByRole('menuitem', { name: new RegExp(menuItem, 'i') })
      .or(page.getByRole('button', { name: new RegExp(menuItem, 'i') }));

    await menuItemLocator.first().waitFor({ state: 'visible', timeout: 5_000 });
    await menuItemLocator.first().click();

    // Some items show a confirmation dialog — if "Sign out" dialog appears, confirm
    const confirmBtn = page.getByRole('button', { name: /sign out|abmelden|log out/i }).last();
    if (await confirmBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await confirmBtn.click();
    }
  }
);

/**
 * "When alex reloads Element Web"
 */
When('{word} reloads Element Web', async ({ page }: { page: Page }, _userName: string) => {
  await page.reload();
  // Give it a moment to settle
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch((e: Error) => { if (!e.message?.includes('Timeout')) throw e; });
});

/**
 * "Then the welcome screen is visible"
 */
Then('the welcome screen is visible', async ({ page }: { page: Page }) => {
  await expect(
    page.getByRole('heading', { name: /welcome to element|willkommen/i })
      .or(page.locator('.mx_Welcome'))
      .or(page.getByRole('link', { name: /sign in|anmelden/i }))
  ).toBeVisible({ timeout: 15_000 });
});

/**
 * "Then the {string} button is present"
 */
Then('the {string} button is present', async ({ page }: { page: Page }, buttonText: string) => {
  await expect(
    page.getByRole('link', { name: new RegExp(buttonText, 'i') })
      .or(page.getByRole('button', { name: new RegExp(buttonText, 'i') }))
  ).toBeVisible({ timeout: 10_000 });
});

/**
 * "Then the room list is visible without a Dex redirect"
 */
Then('the room list is visible without a Dex redirect', async ({ page }: { page: Page }) => {
  // Assert we did NOT land on Dex (no /dex/auth in URL)
  const currentUrl = page.url();
  expect(currentUrl).not.toMatch(/dex.*\/auth/i);
  // Assert room list is visible
  await expect(page.locator('.mx_LeftPanel')).toBeVisible({ timeout: 20_000 });
});

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

  // N-18: Also delete the token sidecar so getApiSession() does not use a stale token
  const tokenSidecar = path.join(AUTH_STATE_DIR, `${userName}.token.json`);
  if (fs.existsSync(tokenSidecar)) {
    fs.unlinkSync(tokenSidecar);
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
 * Opens the user menu and clicks the specified menu item.
 *
 * Element Web 1.12.15 change: The user menu trigger is a div[role="button"] with
 * aria-label="User menu" (NOT a <button> element). The menu no longer has a "Sign out"
 * item — the equivalent is "Remove this device" which signs the user out.
 *
 * "Sign out" is mapped to "Remove this device" for Element Web 1.12.15+ compatibility.
 */
When(
  '{word} opens the user menu and clicks {string}',
  async ({ page }: { page: Page }, _userName: string, menuItem: string) => {
    // Element Web 1.12.15: user menu trigger is [aria-label="User menu"] (div with role="button")
    // Older versions used .mx_UserMenu button or [data-testid="user-menu-trigger"]
    const userMenuTrigger = page.locator('[aria-label="User menu"]')
      .or(page.locator('[data-testid="user-menu-trigger"]'))
      .or(page.locator('.mx_UserMenu button'));

    await userMenuTrigger.first().waitFor({ state: 'visible', timeout: 10_000 });
    await userMenuTrigger.first().click();

    // Normalize "Sign out" → "Remove this device" for Element Web 1.12.15+
    // (1.12.15 removed "Sign out" from the user menu; the closest equivalent is
    //  "Remove this device" which revokes the session token)
    const effectiveItem = /sign out|log out|abmelden/i.test(menuItem)
      ? 'Remove this device'
      : menuItem;

    // Click the menu item by aria-label (Element uses aria-label on <li role="menuitem">)
    const menuItemLocator = page.locator(`[role="menuitem"][aria-label="${effectiveItem}"]`)
      .or(page.getByRole('menuitem', { name: new RegExp(effectiveItem, 'i') }))
      .or(page.getByRole('button', { name: new RegExp(effectiveItem, 'i') }));

    await menuItemLocator.first().waitFor({ state: 'visible', timeout: 5_000 });
    await menuItemLocator.first().click();

    // "Remove this device" may show a confirmation dialog — wait for it deterministically
    const confirmBtn = page.getByRole('button', { name: /sign out|remove|delete|confirm|ok/i });
    if (await confirmBtn.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
      await confirmBtn.first().click();
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
 *
 * Element Web 1.12.15 change: After "Remove this device" (the sign-out equivalent),
 * the page redirects to /#/login (NOT the /#/welcome or /# home screen).
 * The login page shows a "Sign in" heading (h2) and "Homeserver" heading.
 * We also check for the classic welcome screen selectors as fallback.
 */
Then('the welcome screen is visible', async ({ page }: { page: Page }) => {
  // Wait for URL to change away from the main room list
  await page.waitForURL(/\/#\/(login|welcome|home|$)/, { timeout: 15_000 }).catch(() => {});

  await expect(
    // Element Web 1.12.15+: /#/login with "Sign in" heading
    page.getByRole('heading', { name: /sign in/i })
      // Classic welcome screen selectors
      .or(page.getByRole('heading', { name: /welcome to element|willkommen/i }))
      .or(page.locator('.mx_Welcome'))
      .or(page.getByRole('link', { name: /sign in|anmelden/i }))
  ).toBeVisible({ timeout: 15_000 });
});

/**
 * "Then the {string} button is present"
 *
 * Element Web 1.12.15: After sign-out, the page is at /#/login.
 * "Sign in" appears as a heading, not a link or button.
 * Accept heading, button, or link with the given text.
 */
Then('the {string} button is present', async ({ page }: { page: Page }, buttonText: string) => {
  await expect(
    page.getByRole('link', { name: new RegExp(buttonText, 'i') })
      .or(page.getByRole('button', { name: new RegExp(buttonText, 'i') }))
      .or(page.getByRole('heading', { name: new RegExp(buttonText, 'i') }))
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

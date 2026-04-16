/**
 * OIDC fixture for Nebu E2E tests.
 *
 * Exports loginViaOidc — extracted from element_e2e.spec.ts performSsoLogin.
 * Returns { accessToken, userId } for API bot operations.
 *
 * AC 1 — Story 4-29: extracted from element_e2e.spec.ts#performSsoLogin.
 */

import { type Page } from '@playwright/test';

export const ELEMENT_URL = 'http://localhost:7070';
export const DEX_HEALTH  = 'http://localhost:5556/dex/.well-known/openid-configuration';

export interface OidcSession {
  accessToken: string;
  userId: string;
}

/**
 * Perform SSO login via Dex OIDC and return the Matrix access token + userId.
 *
 * This is the Nebu-native replacement for Element Web's homeserver.registerUser().
 * All bot operations use the returned accessToken for direct Matrix API calls.
 *
 * Extracted from element_e2e.spec.ts performSsoLogin (Story 4-29, Task 1).
 */
export async function loginViaOidc(
  page: Page,
  email: string,
  password: string,
): Promise<OidcSession> {
  let capturedToken = '';
  let capturedUserId = '';

  // Intercept the POST /login response to capture access_token and user_id.
  page.on('response', async (resp) => {
    if (resp.url().includes('/_matrix/client/v3/login') && resp.request().method() === 'POST') {
      try {
        const body = await resp.json();
        if (body.access_token) capturedToken = body.access_token;
        if (body.user_id) capturedUserId = body.user_id;
      } catch { /* ignore */ }
    }
  });

  await page.goto(ELEMENT_URL);

  // Click "Sign in" on the welcome screen (DE: "Anmelden")
  await page.getByRole('link', { name: /sign in|anmelden/i }).click();

  // Wait for login page — Element shows "Continue with SSO" for m.login.sso
  await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i })
    .waitFor({ state: 'visible', timeout: 15_000 });

  // Click SSO — redirects browser to Dex login page
  await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }).click();

  // Wait for Dex login form (dex resolves via /etc/hosts → 127.0.0.1)
  await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });

  // Fill Dex credentials
  await page.locator('input[name="login"]').fill(email);
  await page.locator('input[name="password"]').fill(password);
  await page.locator('button[type="submit"]').click();

  // Dex redirects back to gateway callback, then to Element with loginToken.
  // Element processes the loginToken and navigates to the app.
  await page.waitForURL(/localhost:7070/, { timeout: 20_000 });

  // Wait for EITHER the room list panel OR the key-setup dialog to appear.
  // The key-setup dialog ("Setting up keys / Unable to set up keys") appears on
  // first login when Element can't establish cross-signing (our keys/upload stub
  // returns empty counts). We dismiss it with Cancel so the room list becomes visible.
  await Promise.race([
    page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 25_000 }),
    page.getByRole('button', { name: /cancel|abbrechen/i }).waitFor({ state: 'visible', timeout: 25_000 }),
  ]).catch(() => {});

  const cancelBtn = page.getByRole('button', { name: /cancel|abbrechen/i });
  if (await cancelBtn.isVisible({ timeout: 1_000 }).catch(() => false)) {
    await cancelBtn.click();
  }

  // Home/welcome screen is visible after login.
  await page.getByRole('heading', { name: /welcome/i })
    .or(page.locator('[placeholder*="Search"], [aria-label*="Search"]').first())
    .waitFor({ state: 'visible', timeout: 15_000 })
    .catch(() => {});

  return { accessToken: capturedToken, userId: capturedUserId };
}

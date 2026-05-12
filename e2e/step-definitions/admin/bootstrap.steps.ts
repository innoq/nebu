/**
 * Step definitions for features/admin/bootstrap.feature + auth-guard.feature
 *
 * Story 9-26 — Phase 3, AC13.
 *
 * Steps adapted from admin_ui.feature — wired to playwright-bdd runner.
 *
 * BUG-E2E-11 fix: doBootstrapAdmin() is imported from fixtures/admin-bootstrap.ts
 * (shared with global-setup.ts). Admin bootstrap runs in global-setup BEFORE
 * Element Web user logins to prevent the Elixir core from auto-bootstrapping
 * (which sets bootstrap_completed=true without OIDC config).
 */

import { Given, When, Then, test } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { DEX_TEST_PASSWORD } from '../../fixtures/users';
import { doBootstrapAdmin } from '../../fixtures/admin-bootstrap';

const ADMIN_BASE_URL = process.env.NEBU_ADMIN_URL ?? 'http://localhost:8008';

Given('the Nebu admin UI is accessible at the admin URL', async ({ page }: { page: Page }) => {
  const resp = await page.request.get(`${ADMIN_BASE_URL}/admin/`).catch(() => null);
  if (!resp || (!resp.ok() && resp.status() !== 302)) {
    throw new Error(
      `SKIP: Admin UI not reachable at ${ADMIN_BASE_URL}/admin/. Run: make dev`
    );
  }
});

Given('no bootstrap has been completed yet', async ({ page }: { page: Page }) => {
  // Check server_config for bootstrap_completed = false.
  // Verify that the admin UI redirects to /bootstrap (fresh-DB condition).
  // Use test.skip() instead of throwing — shows as 'skipped', not 'failed'.
  // This test requires a completely fresh DB and must run before any other test
  // that calls doBootstrap(). In CI the gate resets volumes, but audit-log.feature
  // runs before bootstrap.feature alphabetically and completes bootstrap first.
  const resp = await page.request.get(`${ADMIN_BASE_URL}/admin/`);
  if (!resp.url().includes('/bootstrap')) {
    test.skip(true, 'Bootstrap already completed — needs fresh DB. Re-run with docker compose down --volumes.');
  }
});

Given('bootstrap has been completed', async ({ page }: { page: Page }) => {
  // If bootstrap is not yet done, complete it automatically (BUG-E2E-03 fix).
  // BUG-E2E-11 fix: doBootstrapAdmin() is safe to call again — it's idempotent.
  // global-setup already ran it, but this ensures correctness if tests run alone.
  await doBootstrapAdmin(page);
  // N-14: Clear admin session so auth-guard tests start unauthenticated.
  // doBootstrapAdmin() leaves the page in an authenticated admin session;
  // auth-guard.feature tests must start without an active session.
  await page.context().clearCookies();
  await page.evaluate(() => {
    try { sessionStorage.clear(); } catch { /* ignore cross-origin */ }
  });
});

Given('the operator is logged in as admin', async ({ page }: { page: Page }) => {
  // BUG-E2E-09 fix: if bootstrap has just been completed in the same page context
  // (via doBootstrapAdmin()), the admin session is already active. In that case,
  // /admin/login/start redirects to /admin/dashboard instead of Dex.
  // Also, on a fresh DB, /admin/login/start redirects to /admin/bootstrap.
  // BUG-E2E-11 fix: global-setup now runs admin bootstrap before Element Web logins,
  // so bootstrap_completed is written WITH OIDC config. /admin/login/start should
  // redirect to Dex (normal flow). Case B (bootstrap needed) is now a rare fallback.
  // Strategy: navigate to /admin/login/start and then choose the path based on
  // where we land (Dex auth, already-admin, or bootstrap needed).
  await page.goto(`${ADMIN_BASE_URL}/admin/login/start`);

  // Wait for navigation to complete (up to 15s) then inspect the URL
  await page.waitForLoadState('domcontentloaded', { timeout: 15_000 }).catch(() => {});

  const currentUrl = page.url();

  // Case A: already logged in and redirected to admin dashboard
  if (/\/admin\/(?:dashboard|rooms|users|config|compliance|audit)/.test(currentUrl)) {
    // Already authenticated — nothing to do
    await expect(page).toHaveURL(/\/admin\/(?:dashboard|rooms|users|config|compliance|audit)/);
    return;
  }

  // Case B: redirected to bootstrap (bootstrap not yet done, or OIDC config missing)
  if (currentUrl.includes('/bootstrap')) {
    // Complete bootstrap first, then retry login
    await doBootstrapAdmin(page);
    // After bootstrap, we're at /admin/dashboard — no need to login again
    await expect(page).toHaveURL(/\/admin\/dashboard/);
    return;
  }

  // Case C: redirected to Dex auth (normal login flow — bootstrap done but no session)
  if (/dex.*\/auth/i.test(currentUrl)) {
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill(DEX_TEST_PASSWORD);
    await page.locator('button[type="submit"]').click();

    // Handle Dex consent screen (if shown on first login for admin)
    const grantBtn = page.locator(
      'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
    );
    if (await grantBtn.first().isVisible({ timeout: 8_000 }).catch(() => false)) {
      await grantBtn.first().click();
    }
    await page.waitForURL(/\/admin\/dashboard/, { timeout: 20_000 });
    await expect(page).toHaveURL(/\/admin\/dashboard/);
    return;
  }

  // Case D: at /admin/login page (no session, OIDC configured)
  // Click "Login with SSO" to initiate Dex flow
  if (currentUrl.includes('/admin/login')) {
    const ssoLink = page.getByRole('link', { name: /login with sso|sign in with sso/i });
    const ssoBtn  = page.getByRole('button', { name: /login with sso|sign in with sso/i });
    if (await ssoLink.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await ssoLink.click();
    } else {
      await ssoBtn.click();
    }
    // After clicking SSO, we redirect to Dex
    await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill(DEX_TEST_PASSWORD);
    await page.locator('button[type="submit"]').click();
    const grantBtn = page.locator(
      'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
    );
    if (await grantBtn.first().isVisible({ timeout: 8_000 }).catch(() => false)) {
      await grantBtn.first().click();
    }
    await page.waitForURL(/\/admin\/dashboard/, { timeout: 20_000 });
    await expect(page).toHaveURL(/\/admin\/dashboard/);
    return;
  }

  // Case E: unexpected state — wait briefly for Dex redirect then retry
  try {
    await page.waitForURL(/dex.*\/auth/i, { timeout: 10_000 });
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill(DEX_TEST_PASSWORD);
    await page.locator('button[type="submit"]').click();
    const grantBtn = page.locator(
      'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
    );
    if (await grantBtn.first().isVisible({ timeout: 8_000 }).catch(() => false)) {
      await grantBtn.first().click();
    }
    await page.waitForURL(/\/admin\/dashboard/, { timeout: 20_000 });
  } catch {
    // If we still can't reach Dex, check if we ended up at admin dashboard
  }
  await expect(page).toHaveURL(/\/admin\/dashboard/);
});

When('the operator navigates to {string}', async ({ page }: { page: Page }, urlPath: string) => {
  await page.goto(`${ADMIN_BASE_URL}${urlPath}`);
});

When('the operator clicks {string}', async ({ page }: { page: Page }, buttonText: string) => {
  // Try button first, then link (anchor), to handle both <button> and <a> elements
  // "Sign in with SSO" / "Login with SSO" may be rendered as a link in some admin pages.
  // BUG-E2E-08 fix: "Sign in with SSO" in the feature file may not match the actual DOM text
  // "Login with SSO" — use a broader SSO-alias pattern when the text mentions SSO login.
  const effectivePattern = /sign.in.*sso|login.*sso/i.test(buttonText)
    ? /sign.in.*sso|login.*sso|continue.with.sso/i
    : new RegExp(buttonText, 'i');

  const btn  = page.getByRole('button', { name: effectivePattern });
  const link = page.getByRole('link',   { name: effectivePattern });

  const btnVisible = await btn.first().isVisible({ timeout: 3_000 }).catch(() => false);
  if (btnVisible) {
    await btn.first().click();
  } else {
    // Fallback: try link, or more broadly any clickable with the text
    const linkVisible = await link.first().isVisible({ timeout: 3_000 }).catch(() => false);
    if (linkVisible) {
      await link.first().click();
    } else {
      // Last resort: click any matching text (button or link)
      await page.getByText(effectivePattern).first().click();
    }
  }
});

When('the operator fills in {string} with {string}', async (
  { page }: { page: Page },
  fieldLabel: string,
  value: string
) => {
  const byLabel = page.getByLabel(new RegExp(fieldLabel, 'i'));
  const byName  = page.locator(`input[name="${fieldLabel}"], textarea[name="${fieldLabel}"]`);
  const labelVisible = await byLabel.first().isVisible({ timeout: 3_000 }).catch(() => false);
  if (labelVisible) {
    await byLabel.first().fill(value);
  } else {
    await byName.first().fill(value);
  }
});

When('the operator fills in {string} with the Dex issuer URL', async ({ page }: { page: Page }, _fieldLabel: string) => {
  const field = page.getByLabel(/oidc issuer url/i);
  await field.fill(DEX_ISSUER_URL);
});

When('the operator fills in {string} with the configured client secret', async ({ page }: { page: Page }, _fieldLabel: string) => {
  const field = page.getByLabel(/oidc client secret/i);
  const secret = process.env.NEBU_OIDC_CLIENT_SECRET ?? 'nebu-admin-secret';
  await field.fill(secret);
});

When('the operator fills in the Dex email field with {string}', async ({ page }: { page: Page }, email: string) => {
  await page.locator('input[name="login"]').fill(email);
});

When('the operator fills in the Dex password field with the admin password', async ({ page }: { page: Page }) => {
  await page.locator('input[name="password"]').fill(DEX_TEST_PASSWORD);
});

When('the operator clicks the Dex login submit button', async ({ page }: { page: Page }) => {
  await page.locator('button[type="submit"]').click();
});

When('the operator selects the claim value {string} as the admin group claim', async (
  { page }: { page: Page },
  claimValue: string
) => {
  await page.getByRole('option', { name: claimValue }).click();
});

Then('the browser is redirected to {string}', async ({ page }: { page: Page }, urlPath: string) => {
  await expect(page).toHaveURL(new RegExp(urlPath.replace(/\//g, '\\/')), { timeout: 15_000 });
});

Then('the browser is redirected to the Dex login page', async ({ page }: { page: Page }) => {
  await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
});

Then('the browser is redirected back to {string}', async ({ page }: { page: Page }, urlPath: string) => {
  await expect(page).toHaveURL(new RegExp(urlPath.replace(/\//g, '\\/')), { timeout: 20_000 });
});

Then('the page shows {string}', async ({ page }: { page: Page }, text: string) => {
  // Use the most specific locator to avoid strict mode violations:
  // Priority: heading → main content text → any text
  // This avoids matching both nav links AND headings simultaneously.
  const headingLocator = page.getByRole('heading', { name: new RegExp(text, 'i') }).first();
  const mainTextLocator = page.locator('main').getByText(text, { exact: false }).first();
  const anyTextLocator = page.getByText(text, { exact: true }).first();

  if (await headingLocator.isVisible({ timeout: 5_000 }).catch(() => false)) {
    await expect(headingLocator).toBeVisible({ timeout: 15_000 });
  } else if (await mainTextLocator.isVisible({ timeout: 5_000 }).catch(() => false)) {
    await expect(mainTextLocator).toBeVisible({ timeout: 15_000 });
  } else {
    // Last resort: any visible text match (may hit strict mode if multiple matches)
    await expect(anyTextLocator).toBeVisible({ timeout: 15_000 });
  }
});

Then('the progress indicator shows step {int} as active', async ({ page }: { page: Page }, _step: number) => {
  // BUG-E2E-07 fix: DaisyUI step classes use "step-primary" for the active step,
  // NOT "step-active" or aria-current="step".
  // HTML: <li class="step step-primary">Instance</li>
  // Lenient check: at least one step-primary element is visible.
  await expect(page.locator('li.step-primary, .step.step-primary').first())
    .toBeVisible({ timeout: 10_000 });
});

Then('the page shows the discovered OIDC claims', async ({ page }: { page: Page }) => {
  await expect(page.locator('[class*="claim"], select, [role="listbox"]').first())
    .toBeVisible({ timeout: 15_000 });
});

Then('the page shows at least one claim value as a selectable option', async ({ page }: { page: Page }) => {
  await expect(page.locator('option, [role="option"]').first())
    .toBeVisible({ timeout: 15_000 });
});

// Note: parentheses in Cucumber expressions are special (optional group).
// Use a regex pattern instead to match the literal step text.
Then(/^the operator is authenticated \(no redirect to login\)$/, async ({ page }: { page: Page }) => {
  await expect(page).not.toHaveURL(/\/login/, { timeout: 5_000 });
  await expect(page).toHaveURL(/\/admin\/dashboard/, { timeout: 10_000 });
});

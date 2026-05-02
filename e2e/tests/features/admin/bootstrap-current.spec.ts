/**
 * Quick reproducer for "Connect with OIDC button does nothing" bug.
 * Mirrors the simplified 2-step bootstrap wizard (Story 5.x simplification).
 */
import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import * as path from 'path';

const PROJECT_ROOT = path.resolve(__dirname, '../../..');

function resetToBootstrapState(): void {
  const now = Date.now();
  execSync(
    [
      'docker compose exec -T postgres psql -U nebu -d nebu',
      `-c "TRUNCATE TABLE server_config"`,
      `-c "TRUNCATE TABLE bootstrap_draft"`,
      `-c "INSERT INTO server_config (key, value, set_at) VALUES ('bootstrap_active', 'true', ${now})"`,
    ].join(' '),
    { cwd: PROJECT_ROOT, stdio: 'pipe' }
  );
}

test.describe('Bootstrap — Connect with OIDC click (regression repro)', () => {
  test.beforeEach(() => {
    resetToBootstrapState();
  });
  test.afterEach(() => {
    resetToBootstrapState();
  });

  test('completes bootstrap flow through Dex login and callback without 429', async ({ page }) => {
    const cspReports: string[] = [];
    const httpFailures: string[] = [];
    page.on('console', msg => {
      const text = msg.text().toLowerCase();
      if (text.includes('content security policy') || text.includes('csp')) {
        cspReports.push(`[${msg.type()}] ${msg.text()}`);
      }
    });
    page.on('response', async resp => {
      if (resp.status() === 429) {
        httpFailures.push(`429 ${resp.url()}`);
      }
    });

    // ── Step 1: Instance Name ──────────────────────────────────────────────
    await page.goto('/admin/bootstrap');
    await page.getByRole('textbox', { name: 'Instance Name' }).fill('nebu-dev');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 2/ })).toBeVisible();

    // ── Step 2: OIDC Configuration + Connect with OIDC ─────────────────────
    await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://dex:5556/dex');
    await page.getByRole('textbox', { name: 'OIDC Client ID' }).fill('nebu-admin');
    await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');
    await page.getByRole('button', { name: 'Connect with OIDC' }).click();

    // ── Dex Authorization ──────────────────────────────────────────────────
    await page.waitForURL(/dex.*\/auth/, { timeout: 10_000 });
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // ── Dex grant page (consent) — auto-approve scopes if shown ────────────
    const grantBtn = page.locator('button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")');
    if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
      await grantBtn.first().click();
    }

    // ── Callback → claim selection (or done, if already configured) ────────
    // Must NOT redirect to /admin/login?error=auth_failed (callback couldn't
    // load OIDC config) and must NOT receive any 429.
    await page.waitForURL(/\/admin\//, { timeout: 15_000 });
    expect(page.url(), 'callback must not redirect to login with error').not.toMatch(/login\?error=/);
    expect(page.url(), 'must not still be on Dex').not.toMatch(/dex:5556/);

    // ── Claim selection: pick instance_admin and submit ────────────────────
    // Picks the first claim that contains "instance_admin" (kai's group).
    // Radio is sr-only; check it directly rather than clicking through the label.
    const claimRadio = page.locator('input[type="radio"][value*="instance_admin"]').first();
    if (await claimRadio.count() > 0) {
      await claimRadio.check({ force: true });
      const submit = page.getByRole('button', { name: /Confirm|Continue|Save|Complete/ }).first();
      await submit.click();
      // After claim selection the bootstrap finalises and redirects somewhere
      // sensible (dashboard, login, or done page) — must NOT be a 500.
      await page.waitForLoadState('networkidle', { timeout: 10_000 });
      expect(page.url(), 'claim selection must not produce internal-error page').not.toMatch(/internal.error|500/i);
    }

    if (cspReports.length > 0) {
      console.error('CSP violations during flow:', cspReports);
    }
    expect(cspReports, 'no CSP violations expected').toHaveLength(0);
    expect(httpFailures, 'no 429s during bootstrap flow').toHaveLength(0);
  });
});

/**
 * Happy-Path E2E test for the full Bootstrap Wizard + first-admin OIDC login.
 *
 * What this covers (and bootstrap.spec.ts does NOT):
 *   - Full wizard flow steps 1 → 4 in sequence
 *   - FinalizeHandler persisting config to server_config
 *   - Redirect to Dex OIDC authorization endpoint
 *   - Dex login with first-admin credentials
 *   - OIDC callback → bootstrap_completed written → redirect to /admin/bootstrap/done
 *   - Bootstrap-done page rendered correctly
 *
 * Prerequisites:
 *   - Stack running: `make dev`
 *   - "127.0.0.1 dex" in /etc/hosts (test skips automatically if dex is unreachable)
 *   - Docker available (used for DB reset via `docker compose exec psql`)
 *
 * DB state is reset to bootstrap before and after each test via docker compose exec.
 */

import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import * as path from 'path';

const PROJECT_ROOT = path.resolve(__dirname, '../../..');

// ---------------------------------------------------------------------------
// DB Reset Helper
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Bootstrap — Full Happy Path (OIDC E2E)', () => {
  test.beforeEach(() => {
    resetToBootstrapState();
  });

  test.afterEach(() => {
    // Restore bootstrap state so bootstrap.spec.ts tests still work when run after this file
    resetToBootstrapState();
  });

  test('completes wizard steps 1–4 and first-admin OIDC login', async ({ page, request }) => {
    // Skip if dex is not resolvable — requires "127.0.0.1 dex" in /etc/hosts
    const dexDiscovery = await request
      .get('http://dex:5556/dex/.well-known/openid-configuration')
      .catch(() => null);
    test.skip(
      !dexDiscovery?.ok(),
      'Dex unreachable — add "127.0.0.1 dex" to /etc/hosts: echo "127.0.0.1 dex" | sudo tee -a /etc/hosts'
    );

    // ── Navigate: /admin redirects to /admin/bootstrap ────────────────────────
    await page.goto('/admin');
    await expect(page).toHaveURL(/\/admin\/bootstrap/);
    await expect(page).toHaveTitle('Bootstrap Setup — Nebu Admin');

    // ── Step 1: Instance Name ─────────────────────────────────────────────────
    await expect(page.getByRole('heading', { name: 'Step 1: Instance Name' })).toBeVisible();
    await page.getByRole('textbox', { name: 'Instance Name' }).fill('nebu-dev');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 2/ })).toBeVisible();

    // ── Step 2: OIDC Configuration ────────────────────────────────────────────
    await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://dex:5556/dex');
    await page.getByRole('textbox', { name: 'OIDC Client ID' }).fill('nebu-admin');
    await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');

    // Verify Test Connection reports success before advancing
    await page.getByRole('button', { name: 'Test Connection' }).click();
    await expect(page.locator('#oidc-test-result')).toContainText('Connected', { timeout: 10_000 });

    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 3/ })).toBeVisible();

    // ── Step 3: Key Generation ────────────────────────────────────────────────
    await page.getByRole('button', { name: 'Generate Keys' }).click();
    await expect(page.locator('#keys-result')).toContainText('Keys generated', { timeout: 10_000 });
    await expect(page.locator('#keys-result')).toContainText('Ed25519');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 4/ })).toBeVisible();

    // ── Step 4: Review & Finalize ─────────────────────────────────────────────
    await expect(page.locator('.alert-success')).toContainText('Almost done');
    // Masked OIDC secret must be shown (proves secret was persisted from step 2)
    await expect(page.locator('.alert-info')).toContainText('Secret stored');

    // Click "Complete Setup & Login" — FinalizeHandler saves config and redirects to LoginStart
    await page.getByRole('button', { name: 'Complete Setup' }).click();

    // ── Dex Authorization Page ────────────────────────────────────────────────
    // LoginStart → discovers Dex → redirects browser to http://dex:5556/dex/auth?...
    await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // ── OIDC Callback ─────────────────────────────────────────────────────────
    // Dex → /admin/callback → bootstrap_completed written → redirect to /admin/bootstrap/done
    await expect(page).toHaveURL(/\/admin\/bootstrap\/done/, { timeout: 15_000 });

    // ── Bootstrap Done Page ───────────────────────────────────────────────────
    await expect(page).toHaveTitle('Setup Complete — Nebu Admin');
    await expect(page.getByRole('heading', { name: 'Nebu is ready' })).toBeVisible();
    await expect(page.getByText('instance_admin')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Go to Dashboard' })).toBeVisible();
  });

  test('FinalizeHandler allows retry after failed OIDC login (no unique-constraint error)', async ({ page, request }) => {
    // This test verifies the fix for the production bug:
    // SaveBootstrapConfig used plain INSERT — second finalize attempt would return 500.
    // Now uses ON CONFLICT DO UPDATE — retries must succeed.

    const dexDiscovery = await request
      .get('http://dex:5556/dex/.well-known/openid-configuration')
      .catch(() => null);
    test.skip(
      !dexDiscovery?.ok(),
      'Dex unreachable — add "127.0.0.1 dex" to /etc/hosts'
    );

    // ── First complete attempt: go through wizard + finalize ──────────────────
    const fillAndFinalize = async () => {
      await page.goto('/admin/bootstrap');
      await page.getByRole('textbox', { name: 'Instance Name' }).fill('nebu-dev');
      await page.getByRole('button', { name: 'Next' }).click();
      await expect(page.getByRole('heading', { name: /Step 2/ })).toBeVisible();

      await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://dex:5556/dex');
      await page.getByRole('textbox', { name: 'OIDC Client ID' }).fill('nebu-admin');
      await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');
      await page.getByRole('button', { name: 'Next' }).click();
      await expect(page.getByRole('heading', { name: /Step 3/ })).toBeVisible();

      await page.getByRole('button', { name: 'Generate Keys' }).click();
      await expect(page.locator('#keys-result')).toContainText('Keys generated', { timeout: 10_000 });
      await page.getByRole('button', { name: 'Next' }).click();
      await expect(page.getByRole('heading', { name: /Step 4/ })).toBeVisible();

      await page.getByRole('button', { name: 'Complete Setup' }).click();
      // Just wait for redirect away from bootstrap (to Dex or error page)
      await page.waitForURL(/dex.*\/auth|admin\/login/, { timeout: 15_000 });
    };

    // First finalize attempt
    await fillAndFinalize();

    // Simulate failed OIDC login: go back to gateway without completing login
    await page.goto('/admin/bootstrap');
    // Should still be on bootstrap (bootstrap_completed not set)
    await expect(page).toHaveURL(/\/admin\/bootstrap/);

    // Second finalize attempt — MUST NOT return 500 (was the bug)
    await page.getByRole('textbox', { name: 'Instance Name' }).fill('nebu-dev');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 2/ })).toBeVisible();

    await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://dex:5556/dex');
    await page.getByRole('textbox', { name: 'OIDC Client ID' }).fill('nebu-admin');
    await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 3/ })).toBeVisible();

    await page.getByRole('button', { name: 'Generate Keys' }).click();
    await expect(page.locator('#keys-result')).toContainText('Keys generated', { timeout: 10_000 });
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: /Step 4/ })).toBeVisible();

    await page.getByRole('button', { name: 'Complete Setup' }).click();

    // Must NOT land on an error page — should redirect to Dex
    await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

    // Complete OIDC login to leave a clean state
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/admin\/bootstrap\/done/, { timeout: 15_000 });
  });
});

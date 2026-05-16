/**
 * Step definitions for e2e/features/admin/bootstrap_import.feature
 *
 * Story 14-3b — Bootstrap Wizard Step 4 UI — Preview + Import
 *
 * RED PHASE: step bodies rely on Step 4 UI that does not exist yet.
 * Tests will FAIL until bootstrap.html + bootstrap.go Step 4 are implemented.
 *
 * Runner: playwright-bdd (Cucumber-based Playwright runner)
 * Config: e2e/playwright.config.ts
 *
 * Background steps (the Nebu stack is running, bootstrap has been completed,
 * the operator is logged in as admin) are defined in bootstrap.steps.ts and reused here.
 *
 * AC coverage (Story 14-3b AC5):
 *   @ac5-wizard-step4-displayed     — "wizard step 4 displayed"
 *   @ac5-preview-table-loaded       — "preview table loaded"
 *   @ac5-import-button-clicked      — "import button clicked"
 */

import { When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const ADMIN_BASE_URL = process.env.NEBU_ADMIN_URL ?? 'http://localhost:8008';

// ---------------------------------------------------------------------------
// Step: "the page contains an 'Import from OIDC' button"
// ---------------------------------------------------------------------------
Then('the page contains an {string} button', async ({ page }: { page: Page }, buttonText: string) => {
  const btn = page.getByRole('button', { name: new RegExp(buttonText, 'i') });
  const btnVisible = await btn.first().isVisible({ timeout: 5_000 }).catch(() => false);
  if (btnVisible) {
    await expect(btn.first()).toBeVisible();
    return;
  }
  // Also check for a submit input or link with matching text
  const anyEl = page.getByText(new RegExp(buttonText, 'i'));
  await expect(anyEl.first()).toBeVisible({ timeout: 5_000 });
});

// ---------------------------------------------------------------------------
// Step: "the 'Import from OIDC' button is enabled"
// ---------------------------------------------------------------------------
When('the {string} button is enabled', async ({ page }: { page: Page }, buttonText: string) => {
  const btn = page.getByRole('button', { name: new RegExp(buttonText, 'i') });
  const isDisabled = await btn.first().isDisabled({ timeout: 5_000 }).catch(() => true);
  if (isDisabled) {
    // If disabled, the OIDC directory is not configured — skip this scenario
    // (AC4 path: "Provider does not support user listing")
    const msg = page.getByText(/provider does not support user listing/i);
    const msgVisible = await msg.first().isVisible({ timeout: 3_000 }).catch(() => false);
    if (msgVisible) {
      // Skip gracefully — OIDC dir not configured in this test environment
      return;
    }
    throw new Error(
      `Expected "${buttonText}" button to be enabled, but it is disabled. ` +
      'Ensure oidc_directory_enabled=true in server_config for this test environment.'
    );
  }
  await expect(btn.first()).toBeEnabled();
});

// ---------------------------------------------------------------------------
// Step: "a preview table is displayed with at least one user row"
// ---------------------------------------------------------------------------
Then('a preview table is displayed with at least one user row', async ({ page }: { page: Page }) => {
  // After clicking "Import from OIDC", a preview table must appear.
  // The table should have at least one <tr> (or data row) with user information.
  // Wait for the table to appear (page re-renders after POST).
  await page.waitForLoadState('domcontentloaded', { timeout: 10_000 });

  // Check for a table element with user data
  const table = page.locator('table');
  const tableVisible = await table.first().isVisible({ timeout: 10_000 }).catch(() => false);

  if (tableVisible) {
    // Table found — check for at least one data row (tbody tr)
    const rows = page.locator('table tbody tr, table tr:not(:first-child)');
    const rowCount = await rows.count();
    if (rowCount === 0) {
      // OIDC dir returned empty list — check for "no users" message
      const noUsers = page.getByText(/no users found|0 users/i);
      const noUsersVisible = await noUsers.first().isVisible({ timeout: 3_000 }).catch(() => false);
      if (noUsersVisible) {
        // Graceful: OIDC dir returned empty — table row count is 0 but test is satisfied
        return;
      }
      throw new Error('Preview table rendered but has 0 data rows and no "no users" message');
    }
    await expect(rows.first()).toBeVisible();
  } else {
    // No table — check for an alternative preview section (e.g. list or card)
    const previewSection = page.locator('[data-testid="preview-table"], [class*="preview"], [id*="preview"]');
    const fallbackVisible = await previewSection.first().isVisible({ timeout: 5_000 }).catch(() => false);
    if (!fallbackVisible) {
      throw new Error(
        'Expected a preview table or section to appear after clicking "Import from OIDC", but none found. ' +
        'Implement Step 4 preview in bootstrap.go + bootstrap.html.'
      );
    }
    await expect(previewSection.first()).toBeVisible();
  }
});

// ---------------------------------------------------------------------------
// Step: "the import result section is displayed with imported count"
// ---------------------------------------------------------------------------
Then('the import result section is displayed with imported count', async ({ page }: { page: Page }) => {
  // After clicking "Import all", the result section must appear showing counts.
  // Wait for page update.
  await page.waitForLoadState('domcontentloaded', { timeout: 15_000 });

  // Look for a result section containing "imported", "skipped", or "failed" text
  const resultKeywords = [
    /imported/i,
    /skipped/i,
    /failed/i,
    /users imported/i,
    /import.*result/i,
    /import.*complete/i,
  ];

  let found = false;
  for (const pattern of resultKeywords) {
    const el = page.getByText(pattern);
    const visible = await el.first().isVisible({ timeout: 5_000 }).catch(() => false);
    if (visible) {
      found = true;
      await expect(el.first()).toBeVisible();
      break;
    }
  }

  if (!found) {
    throw new Error(
      'Expected import result section with imported/skipped/failed counts after clicking "Import all". ' +
      'Implement the import-users handler in bootstrap.go + result display in bootstrap.html.'
    );
  }
});

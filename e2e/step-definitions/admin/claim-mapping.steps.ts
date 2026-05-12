/**
 * Step definitions for features/admin/claim-mapping.feature
 *
 * Story 11-10 — OIDC Claim Mapping Configuration — GREEN PHASE.
 *
 * Steps shared with bootstrap.feature (e.g. "the Nebu admin UI is accessible …",
 * "no bootstrap has been completed yet", "the operator navigates to …", etc.)
 * are already defined in bootstrap.steps.ts and will be reused automatically by
 * playwright-bdd through the shared fixture chain.
 *
 * This file defines ONLY the new steps introduced by claim-mapping.feature.
 *
 * Runner: playwright-bdd (Cucumber-based Playwright runner)
 * Config: e2e/playwright.config.ts
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const ADMIN_BASE_URL = process.env.NEBU_ADMIN_URL ?? 'http://localhost:8008';

// ---------------------------------------------------------------------------
// Step: claim mapping form has field pre-filled with value (AC1, AC9, AC10)
// ---------------------------------------------------------------------------
Then(
  'the claim mapping form has {string} pre-filled with {string}',
  async ({ page }: { page: Page }, fieldName: string, expectedValue: string) => {
    const input = page.locator(`input[name="${fieldName}"]`);
    await expect(input).toBeVisible({ timeout: 10_000 });
    await expect(input).toHaveValue(expectedValue, { timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: claim mapping sidebar navigation link is visible (AC10)
// ---------------------------------------------------------------------------
Then(
  'the claim mapping sidebar navigation link is visible',
  async ({ page }: { page: Page }) => {
    const navLink = page.getByRole('link', { name: /claim mapping/i });
    await expect(navLink).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: operator clears a named field (AC10, AC8)
// ---------------------------------------------------------------------------
When(
  'the operator clears the field {string}',
  async ({ page }: { page: Page }, fieldName: string) => {
    const field = page.locator(`input[name="${fieldName}"]`);
    await field.clear();
    // Remove 'required' so the browser allows form submission with an empty value,
    // letting the server-side validation (HTTP 422) be tested end-to-end.
    await page.evaluate((name) => {
      const el = document.querySelector<HTMLInputElement>(`input[name="${name}"]`);
      if (el) el.removeAttribute('required');
    }, fieldName);
  }
);

// ---------------------------------------------------------------------------
// Step: page shows a validation error for a specific field (AC8)
// ---------------------------------------------------------------------------
Then(
  'the page shows a validation error for {string}',
  async ({ page }: { page: Page }, fieldName: string) => {
    // Bootstrap form: <div class="alert alert-error" data-field="...">.
    // Settings page: <p class="text-error" role="alert" data-field="...">.
    const errMsg = page.locator(`[data-field="${fieldName}"][role="alert"], [data-field="${fieldName}"].alert-error`);
    await expect(errMsg).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: page does not show a specific text (AC8 — no success flash on error)
// ---------------------------------------------------------------------------
Then(
  'the page does not show {string}',
  async ({ page }: { page: Page }, text: string) => {
    await expect(page.getByText(text, { exact: false })).not.toBeVisible({ timeout: 5_000 });
  }
);

// ---------------------------------------------------------------------------
// Shared helper (no step text) — exported for potential reuse
// ---------------------------------------------------------------------------
export async function navigateToClaimMappingSettings(page: Page): Promise<void> {
  await page.goto(`${ADMIN_BASE_URL}/admin/config/claim-mapping`);
  await page.waitForLoadState('domcontentloaded', { timeout: 15_000 });
}

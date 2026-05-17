/**
 * Step definitions for features/admin/oidc-directory-config.feature
 *
 * Story 14-2a — OIDC Directory Config — Admin UI toggle + endpoint field
 *
 * RED PHASE: step bodies are NOT implemented yet. Each step throws immediately
 * to produce a failing test. Remove the throw and implement when the feature is ready.
 *
 * Runner: playwright-bdd (Cucumber-based Playwright runner)
 * Config: e2e/playwright.config.ts
 *
 * AC4 coverage:
 *   - Admin UI Config page shows OIDC directory enabled toggle
 *   - Endpoint text field is hidden when toggle is off
 *   - Enabling the toggle reveals the endpoint field
 *   - Saving persists both fields and a subsequent GET shows them
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const ADMIN_BASE_URL = process.env.NEBU_ADMIN_URL ?? 'http://localhost:8008';

// ---------------------------------------------------------------------------
// Background steps (may already exist in bootstrap.steps.ts — registered once)
// ---------------------------------------------------------------------------

// NOTE: "the Nebu stack is running" and "an admin is logged into the Admin UI"
// are defined in bootstrap.steps.ts and reused here automatically via playwright-bdd.
// "bootstrap is already completed" is also defined in bootstrap.steps.ts.

// ---------------------------------------------------------------------------
// Step: page shows an OIDC directory enabled toggle (AC4)
// ---------------------------------------------------------------------------
Then(
  'the page shows an OIDC directory enabled toggle',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until config.html renders the oidc_directory_enabled toggle
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory_enabled toggle not yet in config.html. ' +
      'Implement the toggle in gateway/internal/admin/templates/config.html and re-run.'
    );
    // Green phase implementation:
    // const toggle = page.locator('input[name="oidc_directory_enabled"]');
    // await expect(toggle).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: OIDC directory endpoint field is NOT visible (AC4 — default state)
// ---------------------------------------------------------------------------
Then(
  'the OIDC directory endpoint field is not visible',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until config.html conditionally hides the endpoint field
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory_endpoint conditional visibility not implemented. ' +
      'The endpoint field should be hidden (x-show=false / display:none) when toggle is off.'
    );
    // Green phase implementation:
    // const endpointField = page.locator('input[name="oidc_directory_endpoint"]');
    // await expect(endpointField).not.toBeVisible({ timeout: 5_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: admin enables the OIDC directory toggle (AC4)
// ---------------------------------------------------------------------------
When(
  'the admin enables the OIDC directory toggle',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until the toggle exists in the rendered HTML
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory_enabled toggle not rendered. ' +
      'Implement the toggle in config.html and re-run.'
    );
    // Green phase implementation:
    // const toggle = page.locator('input[name="oidc_directory_enabled"]');
    // await toggle.check();
    // await expect(toggle).toBeChecked({ timeout: 5_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: OIDC directory endpoint field is visible (AC4 — when toggle enabled)
// ---------------------------------------------------------------------------
Then(
  'the OIDC directory endpoint field is visible',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until the endpoint field appears on toggle enable
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory_endpoint conditional display not implemented.'
    );
    // Green phase implementation:
    // const endpointField = page.locator('input[name="oidc_directory_endpoint"]');
    // await expect(endpointField).toBeVisible({ timeout: 5_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: OIDC directory endpoint field is editable (AC4)
// ---------------------------------------------------------------------------
Then(
  'the OIDC directory endpoint field is editable',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until the endpoint input exists and is not disabled/readonly
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory_endpoint input not rendered or not editable.'
    );
    // Green phase implementation:
    // const endpointField = page.locator('input[name="oidc_directory_endpoint"]');
    // await expect(endpointField).toBeEnabled({ timeout: 5_000 });
    // await expect(endpointField).not.toHaveAttribute('readonly');
  }
);

// ---------------------------------------------------------------------------
// Step: admin fills in the OIDC directory endpoint (AC4)
// ---------------------------------------------------------------------------
When(
  'the admin fills in the OIDC directory endpoint with {string}',
  async ({ page }: { page: Page }, endpointURL: string) => {
    // RED PHASE: fails until the endpoint field is visible and editable
    throw new Error(
      `RED PHASE — Story 14-2a AC4: cannot fill oidc_directory_endpoint="${endpointURL}" — field not yet rendered.`
    );
    // Green phase implementation:
    // const endpointField = page.locator('input[name="oidc_directory_endpoint"]');
    // await endpointField.fill(endpointURL);
  }
);

// ---------------------------------------------------------------------------
// Step: admin saves the config form (shared — may exist in other step files)
// ---------------------------------------------------------------------------
When(
  'the admin saves the config form',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until config.html renders a save button and POST works
    throw new Error(
      'RED PHASE — Story 14-2a AC4: config save flow not yet functional for oidc_directory fields.'
    );
    // Green phase implementation:
    // const saveButton = page.getByRole('button', { name: /save/i });
    // await saveButton.click();
    // await page.waitForURL(/\/admin\/config/, { timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: page shows a success flash message (shared — may already exist)
// ---------------------------------------------------------------------------
Then(
  'the page shows a success flash message',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until flash message is rendered after POST
    throw new Error(
      'RED PHASE — Story 14-2a AC4: success flash message not yet shown after config save.'
    );
    // Green phase implementation:
    // const flash = page.locator('[role="alert"]').filter({ hasText: /success|updated/i });
    // await expect(flash).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: OIDC directory toggle is enabled (AC4 — after page reload)
// ---------------------------------------------------------------------------
Then(
  'the OIDC directory toggle is enabled',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until config.html re-renders the toggle in checked state
    throw new Error(
      'RED PHASE — Story 14-2a AC4: oidc_directory toggle not persisted and re-rendered.'
    );
    // Green phase implementation:
    // const toggle = page.locator('input[name="oidc_directory_enabled"]');
    // await expect(toggle).toBeChecked({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Step: OIDC directory endpoint field shows value (AC4 — after page reload)
// ---------------------------------------------------------------------------
Then(
  'the OIDC directory endpoint field shows {string}',
  async ({ page }: { page: Page }, expectedURL: string) => {
    // RED PHASE: fails until the endpoint value is persisted and re-rendered
    throw new Error(
      `RED PHASE — Story 14-2a AC4: oidc_directory_endpoint="${expectedURL}" not persisted and re-rendered in config.html.`
    );
    // Green phase implementation:
    // const endpointField = page.locator('input[name="oidc_directory_endpoint"]');
    // await expect(endpointField).toBeVisible({ timeout: 10_000 });
    // await expect(endpointField).toHaveValue(expectedURL, { timeout: 10_000 });
  }
);

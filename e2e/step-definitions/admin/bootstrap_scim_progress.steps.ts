/**
 * Step definitions for e2e/features/admin/bootstrap_scim_progress.feature
 *
 * Story 14-3c — SCIM 2.0 User Fetch + Progress Tracking
 *
 * RED PHASE: step bodies reference UI elements that do not exist yet.
 * Tests will FAIL until:
 *   - scim_client.go (SCIMClient) is implemented
 *   - bootstrap.go action=import uses SCIM fetcher
 *   - bootstrap.html Step 4 renders a progress bar + polling JS
 *   - GET /api/v1/admin/bootstrap/import-status is registered and auth-protected
 *
 * Runner: playwright-bdd (Cucumber-based Playwright runner)
 * Config: e2e/playwright.config.ts
 *
 * Background steps (the Nebu stack is running, bootstrap has been completed,
 * the operator is logged in as admin) are defined in bootstrap.steps.ts and reused here.
 *
 * AC coverage (Story 14-3c):
 *   @ac3-progress-bar-visible     — "progress bar is shown when SCIM import starts"
 *   @ac3-progress-polling         — "progress bar updates via polling during import"
 *   @ac3-import-status-auth       — "import-status endpoint requires admin authentication"
 *   @ac5-scim-bearer-token-not-exposed — "SCIM bearer token not in config page"
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page, Request } from '@playwright/test';

const ADMIN_BASE_URL = process.env.NEBU_ADMIN_URL ?? 'http://localhost:8008';

// ---------------------------------------------------------------------------
// Background step: SCIM integration enabled
// ---------------------------------------------------------------------------

/**
 * @ac3-setup
 * Given SCIM integration is enabled in server config
 *
 * RED PHASE: This step requires SCIM config rows to exist in server_config
 * (scim_enabled=true, scim_base_url set). In the real stack, this would be
 * seeded by the test setup. For the red phase, this step always passes — the
 * actual SCIM integration is exercised by the bootstrap Step 4 actions.
 */
Given('SCIM integration is enabled in server config', async ({ page }: { page: Page }) => {
  // RED PHASE: In a real E2E test, we would:
  // 1. Navigate to /admin/config
  // 2. Enable SCIM and set scim_base_url (pointing to a mock SCIM server)
  // 3. Save the config
  //
  // For the red phase scaffold, this step intentionally throws to signal
  // that the SCIM config page section has not been implemented yet.
  //
  // REMOVE THIS THROW when scim_client.go + config.html SCIM section are implemented.
  throw new Error(
    'RED PHASE: SCIM integration config (scim_enabled, scim_base_url, scim_bearer_token) ' +
    'is not yet implemented in the Admin Config page. ' +
    'Implement Story 14-3c before running this E2E test.'
  );
});

// ---------------------------------------------------------------------------
// @ac3-progress-bar-visible
// ---------------------------------------------------------------------------

/**
 * Then a progress bar element is visible on the page
 *
 * RED PHASE: Expects a <progress> element or DaisyUI progress component.
 * Will fail until bootstrap.html Step 4 includes the progress bar markup.
 */
Then('a progress bar element is visible on the page', async ({ page }: { page: Page }) => {
  // Wait for the page to update after form submission
  await page.waitForLoadState('domcontentloaded', { timeout: 15_000 });

  // Check for DaisyUI progress element (class="progress ...") or HTML <progress>
  const progressBar = page.locator('progress, [role="progressbar"], .progress');
  const visible = await progressBar.first().isVisible({ timeout: 10_000 }).catch(() => false);

  if (!visible) {
    throw new Error(
      'RED PHASE: Expected a progress bar element after clicking "Import from SCIM", ' +
      'but none found. Implement the progress bar in bootstrap.html Step 4.'
    );
  }

  await expect(progressBar.first()).toBeVisible();
});

/**
 * Then the progress bar shows imported and total user counts
 *
 * RED PHASE: Expects text like "X / Y users imported" near the progress bar.
 */
Then('the progress bar shows imported and total user counts', async ({ page }: { page: Page }) => {
  // Look for a count display like "5 / 20 users imported" or "<span id="import-count">5</span>"
  const countDisplay = page.locator('#import-count, [data-testid="import-count"], .import-count');
  const patternText = page.getByText(/\d+\s*\/\s*\d+/);

  const countVisible = await countDisplay.first().isVisible({ timeout: 5_000 }).catch(() => false);
  const patternVisible = await patternText.first().isVisible({ timeout: 5_000 }).catch(() => false);

  if (!countVisible && !patternVisible) {
    throw new Error(
      'RED PHASE: Expected imported/total count display near the progress bar ' +
      '(e.g. "5 / 20 users imported"), but none found. ' +
      'Implement the count display in bootstrap.html Step 4.'
    );
  }
});

// ---------------------------------------------------------------------------
// @ac3-progress-polling
// ---------------------------------------------------------------------------

/**
 * Then the page polls "/api/v1/admin/bootstrap/import-status" for progress
 *
 * RED PHASE: Verifies that the JavaScript polling code fires fetch requests
 * to the import-status endpoint.
 */
Then(
  'the page polls {string} for progress',
  async ({ page }: { page: Page }, endpoint: string) => {
    const pollingRequests: string[] = [];

    // Intercept network requests to the polling endpoint
    page.on('request', (req: Request) => {
      if (req.url().includes(endpoint)) {
        pollingRequests.push(req.url());
      }
    });

    // Wait up to 6 seconds for at least one polling request (poll interval is 2s)
    await page.waitForTimeout(6_000);

    if (pollingRequests.length === 0) {
      throw new Error(
        `RED PHASE: Expected JavaScript polling to ${endpoint} within 6 seconds, ` +
        'but no requests were captured. ' +
        'Implement the polling JS in bootstrap.html Step 4.'
      );
    }
  }
);

/**
 * Then the import count display updates with live numbers
 *
 * RED PHASE: Verifies that the displayed count changes as polling responses arrive.
 */
Then('the import count display updates with live numbers', async ({ page }: { page: Page }) => {
  // After polling starts, the count should be a number (possibly 0 if import is fast)
  const countEl = page.locator('#import-count, [data-testid="import-count"]');
  const visible = await countEl.first().isVisible({ timeout: 5_000 }).catch(() => false);

  if (!visible) {
    throw new Error(
      'RED PHASE: Expected import count element to be visible for live updates. ' +
      'Implement #import-count or [data-testid="import-count"] in bootstrap.html.'
    );
  }

  // The content should be a number (or "0")
  const text = await countEl.first().textContent({ timeout: 3_000 });
  const asNum = parseInt(text ?? '', 10);
  if (isNaN(asNum)) {
    throw new Error(
      `RED PHASE: Expected import count element to contain a number, got: ${JSON.stringify(text)}`
    );
  }
});

// ---------------------------------------------------------------------------
// @ac3-import-status-auth
// ---------------------------------------------------------------------------

/**
 * Given the operator is not logged in
 *
 * Clears the session cookie so the next request is unauthenticated.
 */
Given('the operator is not logged in', async ({ page }: { page: Page }) => {
  await page.context().clearCookies();
});

/**
 * When the operator sends GET {string}
 */
When(
  'the operator sends GET {string}',
  async ({ page }: { page: Page }, path: string) => {
    await page.goto(`${ADMIN_BASE_URL}${path}`);
  }
);

/**
 * Then the response status is {int}
 *
 * RED PHASE: For the import-status endpoint, expects 401 for unauthenticated requests.
 * CR-3 from security guide: import-status MUST be behind admin auth middleware.
 */
Then(
  'the response status is {int}',
  async ({ page }: { page: Page }, expectedStatus: number) => {
    // Use Playwright's request API to check the raw HTTP status
    const response = await page.evaluate(async (url: string) => {
      const r = await fetch(url, { credentials: 'omit' });
      return r.status;
    }, `${ADMIN_BASE_URL}/api/v1/admin/bootstrap/import-status`);

    if (response !== expectedStatus) {
      throw new Error(
        `RED PHASE: Expected HTTP ${expectedStatus} for unauthenticated import-status request, ` +
        `got ${response}. ` +
        'Register GET /api/v1/admin/bootstrap/import-status behind sessionGuard middleware.'
      );
    }
  }
);

// ---------------------------------------------------------------------------
// @ac5-scim-bearer-token-not-exposed
// ---------------------------------------------------------------------------

/**
 * Then the page does not contain the raw SCIM bearer token value
 *
 * RED PHASE: CR-1 from security guide — scim_bearer_token must never appear in responses.
 * This step checks that the config page does not render the raw token.
 */
Then(
  'the page does not contain the raw SCIM bearer token value',
  async ({ page }: { page: Page }) => {
    // In a real test, we'd compare against the known test token.
    // For the scaffold, we check that common patterns that could expose the token are absent.
    const content = await page.content();

    // Check for patterns that would indicate a token field with a value
    // (a real test would have the actual token value to check against)
    const hasTokenField = content.includes('name="scim_bearer_token"') &&
      // If the input has a value attribute set to something non-empty
      /name="scim_bearer_token"\s+value="[^"]{4,}"/.test(content);

    if (hasTokenField) {
      throw new Error(
        'RED PHASE: The SCIM bearer token appears to have a value in the config page form. ' +
        'CR-1 from security guide: scim_bearer_token must never be returned in API responses. ' +
        'Use a password input with no value (or placeholder "[SET]") instead.'
      );
    }
  }
);

/**
 * Then the page shows a masked token indicator if SCIM is configured
 *
 * RED PHASE: If SCIM is configured, the UI should show "[SET]" or a similar indicator,
 * not the actual token value.
 */
Then(
  'the page shows a masked token indicator if SCIM is configured',
  async ({ page }: { page: Page }) => {
    // If SCIM bearer token is configured, look for "[SET]" placeholder or similar
    const maskedIndicators = [
      page.getByText(/\[SET\]/i),
      page.getByText(/token set/i),
      page.locator('input[name="scim_bearer_token"][placeholder*="SET"]'),
      page.locator('input[name="scim_bearer_token"][placeholder*="configured"]'),
    ];

    // At least one masked indicator should be visible IF scim is configured.
    // If SCIM is not configured, the field is empty — also acceptable.
    let anyFound = false;
    for (const indicator of maskedIndicators) {
      const visible = await indicator.first().isVisible({ timeout: 3_000 }).catch(() => false);
      if (visible) {
        anyFound = true;
        break;
      }
    }

    // Check if the scim_bearer_token input exists at all
    const tokenInput = page.locator('input[name="scim_bearer_token"]');
    const inputExists = await tokenInput.first().isVisible({ timeout: 3_000 }).catch(() => false);

    if (inputExists && !anyFound) {
      // Input exists but no masked indicator — check it has no value exposed
      const value = await tokenInput.first().getAttribute('value');
      if (value && value.length > 0) {
        throw new Error(
          'RED PHASE: scim_bearer_token input has a non-empty value attribute. ' +
          'CR-1: Never return the raw token. Use placeholder="[SET]" or empty value.'
        );
      }
    }
  }
);

// ---------------------------------------------------------------------------
// Shared step: "Import from SCIM" button visible and enabled
// ---------------------------------------------------------------------------

/**
 * Then the "Import from SCIM" button is visible and enabled
 *
 * RED PHASE: Will fail until bootstrap.html renders the SCIM import button.
 */
Then(
  'the {string} button is visible and enabled',
  async ({ page }: { page: Page }, buttonText: string) => {
    const btn = page.getByRole('button', { name: new RegExp(buttonText, 'i') });
    const visible = await btn.first().isVisible({ timeout: 5_000 }).catch(() => false);

    if (!visible) {
      throw new Error(
        `RED PHASE: Expected "${buttonText}" button to be visible, but it was not found. ` +
        'Implement the SCIM import button in bootstrap.html Step 4.'
      );
    }

    const disabled = await btn.first().isDisabled({ timeout: 3_000 }).catch(() => true);
    if (disabled) {
      throw new Error(
        `RED PHASE: "${buttonText}" button exists but is disabled. ` +
        'Ensure SCIM is configured and scim_enabled=true for this scenario.'
      );
    }

    await expect(btn.first()).toBeVisible();
    await expect(btn.first()).toBeEnabled();
  }
);

/**
 * When the operator clicks {string}
 */
When('the operator clicks {string}', async ({ page }: { page: Page }, buttonText: string) => {
  const btn = page.getByRole('button', { name: new RegExp(buttonText, 'i') });
  const visible = await btn.first().isVisible({ timeout: 5_000 }).catch(() => false);
  if (!visible) {
    throw new Error(
      `Expected button "${buttonText}" to be visible before clicking, but it was not found.`
    );
  }
  await btn.first().click();
});

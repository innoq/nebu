/**
 * Step definitions for e2e/features/admin/oidc_directory_search.feature
 *
 * Story 14-2c — Admin UI User Search OIDC Integration + "Not yet logged in" Badge
 *
 * RED PHASE: step bodies are NOT implemented yet. Each step that requires
 * implementation throws immediately to produce a failing test.
 * Background and shared steps (stack running, admin login) are defined in bootstrap.steps.ts.
 *
 * Runner: playwright-bdd (Cucumber-based Playwright runner)
 * Config: e2e/playwright.config.ts
 *
 * AC coverage:
 *   AC1 — OIDC-only user "Diana OIDC" appears in search results with "Not yet logged in" badge
 *   AC3 — OIDC provider unavailable → warning banner shown, DB users still visible
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const ADMIN_API_BASE = process.env.NEBU_API_URL ?? 'http://localhost:8008';

// ---------------------------------------------------------------------------
// Background: shared steps (defined in bootstrap.steps.ts — reused here)
// "the Nebu stack is running", "bootstrap is already completed",
// "an admin is logged into the Admin UI"
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Given: OIDC directory integration is enabled in the server config
// ---------------------------------------------------------------------------
Given(
  'OIDC directory integration is enabled in the server config',
  async ({ page, request }: { page: Page; request: any }) => {
    // RED PHASE: fails until:
    //   1. The admin API PATCH /api/v1/admin/config accepts oidc_directory_enabled + oidc_directory_endpoint
    //   2. The mock OIDC directory server URL is configurable (env var or test fixture)
    //   3. UsersHandler.ListHandler wires OIDCDirectoryService when enabled
    throw new Error(
      'RED PHASE — Story 14-2c AC1: OIDC directory integration not yet wired in UsersHandler. ' +
      'Implement: WithOIDCDirectory in users.go, then configure via PATCH /api/v1/admin/config.'
    );
    // Green phase implementation (sketch):
    // const oidcMockURL = process.env.NEBU_OIDC_MOCK_URL ?? 'https://mock-oidc:9999/users';
    // await request.patch(`${ADMIN_API_BASE}/api/v1/admin/config`, {
    //   data: { oidc_directory_enabled: true, oidc_directory_endpoint: oidcMockURL },
    //   headers: { Authorization: 'Bearer ...' },
    // });
  }
);

// ---------------------------------------------------------------------------
// Given: the OIDC directory contains a user (Diana OIDC, sub=diana.oidc)
// ---------------------------------------------------------------------------
Given(
  'the OIDC directory contains a user with display name {string} and sub {string}',
  async ({ page }: { page: Page }, displayName: string, sub: string) => {
    // RED PHASE: fails until a mock OIDC directory server is set up and seeded by the test fixture
    throw new Error(
      `RED PHASE — Story 14-2c AC1: mock OIDC directory not configured. ` +
      `Expected user: display_name="${displayName}", sub="${sub}". ` +
      'Set up a mock HTTP server in the e2e fixture that returns this user.'
    );
    // Green phase: the mock OIDC server is set up in the playwright fixture (e2e/fixtures/nebu-fixtures.ts)
    // and seeded via environment variables or a test-only API endpoint.
  }
);

// ---------------------------------------------------------------------------
// Given: the user has never logged into Nebu
// ---------------------------------------------------------------------------
Given(
  '{string} has never logged into Nebu',
  async ({ page }: { page: Page }, sub: string) => {
    // RED PHASE: this is a precondition assertion — verify the user does NOT exist in Nebu DB.
    // In green phase: the fixture ensures a clean DB state (no user with this sub).
    throw new Error(
      `RED PHASE — Story 14-2c AC1: cannot verify "${sub}" is absent from Nebu DB — ` +
      'DB check not yet implemented in E2E fixtures.'
    );
    // Green phase: assert via admin API that no user with this sub exists, or rely on
    // a fresh test DB state (docker compose down --volumes in CI).
  }
);

// ---------------------------------------------------------------------------
// When: the admin searches for a query string
// ---------------------------------------------------------------------------
When(
  'the admin searches for {string}',
  async ({ page }: { page: Page }, query: string) => {
    // RED PHASE: fails until the users search input is rendered and functional
    throw new Error(
      `RED PHASE — Story 14-2c AC1: cannot search for "${query}" — ` +
      'search input not yet wired with OIDC merge result.'
    );
    // Green phase implementation:
    // const searchInput = page.locator('input[name="q"]');
    // await searchInput.fill(query);
    // await searchInput.press('Enter');
    // await page.waitForLoadState('networkidle');
  }
);

// ---------------------------------------------------------------------------
// Then: a specific display name appears in search results
// ---------------------------------------------------------------------------
Then(
  '{string} appears in the search results',
  async ({ page }: { page: Page }, displayName: string) => {
    // RED PHASE: fails until OIDC merge returns the user in the rendered HTML
    throw new Error(
      `RED PHASE — Story 14-2c AC1: "${displayName}" not in search results — ` +
      'OIDCDirectoryService not yet merged with Nebu DB results in UsersHandler.ListHandler.'
    );
    // Green phase implementation:
    // const userEntry = page.locator(`text="${displayName}"`).first();
    // await expect(userEntry).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Then: the user row shows a "Not yet logged in" badge
// ---------------------------------------------------------------------------
Then(
  'the user row for {string} shows a {string} badge',
  async ({ page }: { page: Page }, displayName: string, badgeText: string) => {
    // RED PHASE: fails until users.html renders the "Not yet logged in" badge for OIDC-only users
    throw new Error(
      `RED PHASE — Story 14-2c AC1: badge "${badgeText}" not rendered for "${displayName}". ` +
      'Add IsOIDCOnly flag to UserRowData and render badge in users.html.'
    );
    // Green phase implementation:
    // const row = page.locator(`li:has-text("${displayName}")`);
    // await expect(row.locator(`text="${badgeText}"`)).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Then: the user row shows a Matrix User ID preview containing the expected prefix
// ---------------------------------------------------------------------------
Then(
  'the user row for {string} shows a Matrix User ID preview containing {string}',
  async ({ page }: { page: Page }, displayName: string, matrixIDPrefix: string) => {
    // RED PHASE: fails until MatrixIDPreview is rendered in users.html
    throw new Error(
      `RED PHASE — Story 14-2c AC1: Matrix User ID preview not rendered for "${displayName}". ` +
      `Expected preview containing "${matrixIDPrefix}". ` +
      'Add MatrixIDPreview to UserRowData and render in users.html.'
    );
    // Green phase implementation:
    // const row = page.locator(`li:has-text("${displayName}")`);
    // await expect(row.locator(`text*="${matrixIDPrefix}"`)).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Given: the OIDC directory endpoint is unreachable
// ---------------------------------------------------------------------------
Given(
  'the OIDC directory endpoint is unreachable',
  async ({ page }: { page: Page }) => {
    // RED PHASE: fails until the OIDC endpoint can be configured to an unreachable URL via config
    throw new Error(
      'RED PHASE — Story 14-2c AC3: cannot set OIDC directory endpoint to unreachable URL via admin config. ' +
      'Configure via PATCH /api/v1/admin/config with an unreachable endpoint URL.'
    );
    // Green phase implementation:
    // await request.patch(`${ADMIN_API_BASE}/api/v1/admin/config`, {
    //   data: {
    //     oidc_directory_enabled: true,
    //     oidc_directory_endpoint: 'https://unreachable.example.invalid/users',
    //   },
    // });
  }
);

// ---------------------------------------------------------------------------
// Then: a warning banner is visible
// ---------------------------------------------------------------------------
Then(
  'a warning banner containing {string} is visible',
  async ({ page }: { page: Page }, warningText: string) => {
    // RED PHASE: fails until users.html renders the OIDCWarning banner
    throw new Error(
      `RED PHASE — Story 14-2c AC3: warning banner "${warningText}" not rendered. ` +
      'Add OIDCWarning + OIDCWarningMessage to UsersPageData and render in users.html.'
    );
    // Green phase implementation:
    // const banner = page.locator('[role="alert"]').filter({ hasText: warningText });
    // await expect(banner).toBeVisible({ timeout: 10_000 });
  }
);

// ---------------------------------------------------------------------------
// Then: the existing Nebu DB users are still shown in the list
// ---------------------------------------------------------------------------
Then(
  'the existing Nebu DB users are still shown in the list',
  async ({ page }: { page: Page }) => {
    // RED PHASE: this assertion verifies that OIDC failure does not break the user list
    throw new Error(
      'RED PHASE — Story 14-2c AC3: cannot verify Nebu DB users remain visible when OIDC fails. ' +
      'Ensure the handler falls back to DB-only results when OIDC is unavailable.'
    );
    // Green phase implementation:
    // The E2E stack always has at least the bootstrap admin (kai) in the Nebu DB.
    // const userList = page.locator('ul[role="listbox"]');
    // await expect(userList.locator('li')).not.toHaveCount(0, { timeout: 10_000 });
  }
);

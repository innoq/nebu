/**
 * BDD-capable Playwright fixture base for Nebu E2E tests.
 *
 * Story 9-26 — Phase 1, AC2.
 * Story 9-26b — Fixes BUG-E2E-01:
 *   Element Web 1.11+ stores session in IndexedDB. Playwright storageState()
 *   captures localStorage + cookies only — IndexedDB is NOT restored when a new
 *   context is created with { storageState }. Attempting to load Element Web in
 *   such a context shows the welcome screen instead of the room list.
 *
 *   Fix: context + secondContext fixtures no longer restore from storageState.
 *   Instead they create a fresh context, open a page, perform a full OIDC login
 *   via loginViaOidcBrowser() (which leaves the context with a live IndexedDB
 *   session), and close the setup page. Tests then open their own pages within
 *   the live context. Each test context pays ~15–20s for login, but is correct.
 *
 * Exports { Given, When, Then, test } from createBdd(mergeTests(...)).
 *
 * Uses playwright-bdd's `test` as the base (required for createBdd() to work).
 * Custom fixtures are extended on top of it via mergeTests.
 */

import { createBdd, test as bddBase } from 'playwright-bdd';
import { mergeTests, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { ElementAppPage } from './element-app';
import { loginViaOidcBrowser } from './dex-auth';
import { NEBU_USERS, type NebUser, type NebuUserName, DEX_TEST_PASSWORD } from './users';

// Extended fixture types
type NebuFixtures = {
  /** The active test user (defaults to alex). Overrideable per scenario via @tag. */
  credentials: NebUser;
  /** A browser context with a live Element Web IndexedDB session (fresh OIDC login). */
  context: BrowserContext;
  /** Initialized ElementAppPage wrapping the test page. */
  app: ElementAppPage;
  /** Second context for multi-user receive scenarios (marie). */
  secondContext: BrowserContext;
  /** A ready Page in the second context (marie). Torn down with secondContext. */
  secondPage: Page;
};

// Extend the playwright-bdd test base with our custom fixtures
// This ensures createBdd() sees a test derived from playwright-bdd's base.
const test = mergeTests(bddBase).extend<NebuFixtures>({
  credentials: [NEBU_USERS.alex, { option: true }],

  /**
   * Story 9-26b Fix (BUG-E2E-01): perform a fresh OIDC login for each test context.
   * Creates a new browser context, opens a setup page, runs loginViaOidcBrowser()
   * (which fills the context's IndexedDB with a live session), then closes the setup
   * page. The context is handed to the test with an active Element Web session.
   */
  context: async (
    { browser, credentials }: { browser: Browser; credentials: NebUser },
    use: (ctx: BrowserContext) => Promise<void>
  ) => {
    const ctx  = await browser.newContext();
    const page = await ctx.newPage();
    try {
      await loginViaOidcBrowser(page, credentials, credentials.email, DEX_TEST_PASSWORD);
    } finally {
      await page.close();
    }
    // Context now holds a live Element Web IndexedDB session
    await use(ctx);
    await ctx.close();
  },

  app: async ({ page }: { page: Page }, use: (app: ElementAppPage) => Promise<void>) => {
    await use(new ElementAppPage(page));
  },

  /**
   * Story 9-26b Fix (BUG-E2E-01): same fresh-login strategy for marie's context.
   */
  secondContext: async (
    { browser }: { browser: Browser },
    use: (ctx: BrowserContext) => Promise<void>
  ) => {
    const ctx  = await browser.newContext();
    const page = await ctx.newPage();
    try {
      await loginViaOidcBrowser(page, NEBU_USERS.marie, NEBU_USERS.marie.email, DEX_TEST_PASSWORD);
    } finally {
      await page.close();
    }
    // Context now holds a live Element Web IndexedDB session for marie
    await use(ctx);
    await ctx.close();
  },

  secondPage: async (
    { secondContext }: { secondContext: BrowserContext },
    use: (p: Page) => Promise<void>
  ) => {
    const page = await secondContext.newPage();
    await use(page);
    // page is closed automatically when secondContext closes
  },
});

// createBdd() generates the Given/When/Then helpers that bind to the extended fixture types.
export const { Given, When, Then } = createBdd(test);
export { test };
export type { NebuUserName };

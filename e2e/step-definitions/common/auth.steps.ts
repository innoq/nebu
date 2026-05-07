/**
 * Common auth steps — "Given {word} is logged in via Element Web"
 *
 * Story 9-26 — Phase 1, AC4.
 * F-02 fix: guard throws if userName !== credentials.name
 * Reused across: login.feature, room/create.feature, room/join.feature,
 * room/leave.feature, messages/send.feature, messages/receive.feature
 */

import { Given } from '../../fixtures/nebu-fixtures';
import { NEBU_USERS, type NebUser } from '../../fixtures/users';
import { ensureStorageState } from '../../fixtures/dex-auth';
import type { Page, Browser } from '@playwright/test';

/**
 * "Given alex is logged in via Element Web"
 *
 * Uses cached storageState — no SSO redirect on subsequent calls.
 * Navigates to Element root and waits for the room list.
 *
 * GUARD (F-02): {word} must match the current credentials fixture user (default: alex).
 * The page is bound to the credentials context — calling this for a different user
 * would warm the wrong storageState but navigate alex's page, silently wrong.
 * Use "in a second browser context" for multi-user scenarios.
 */
Given(
  '{word} is logged in via Element Web',
  async (
    { page, browser, credentials }: { page: Page; browser: Browser; credentials: NebUser },
    userName: string
  ) => {
    // F-02: guard — userName must match the fixture credentials
    if (userName !== credentials.name) {
      throw new Error(
        `Step "Given ${userName} is logged in via Element Web" was called but ` +
        `the current fixture context is bound to "${credentials.name}". ` +
        `This step can only log in the current context user. ` +
        `To use a second user, write: "${userName} is logged in via Element Web in a second browser context"`
      );
    }

    const user = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!user) {
      throw new Error(
        `Unknown test user "${userName}". Valid users: ${Object.keys(NEBU_USERS).join(', ')}`
      );
    }

    // ensureStorageState will return cached path or perform OIDC login
    await ensureStorageState(browser, user);

    // The context fixture already loads storageState — navigate to trigger auth check
    await page.goto(process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070');
    await page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 20_000 });
  }
);

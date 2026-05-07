/**
 * Common navigation steps.
 *
 * Story 9-26 — Phase 1, AC4.
 * Story 9-26a — M-1 fix: use getActualRoomName() for room navigation.
 */

import { When } from '../../fixtures/nebu-fixtures';
import type { Page } from '@playwright/test';
import { getActualRoomName } from './room-setup.steps';

const ELEMENT_URL = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';

/**
 * "When alex navigates to Element Web"
 * Navigates to Element root without waiting for login.
 */
When(
  '{word} navigates to Element Web',
  async ({ page }: { page: Page }, _userName: string) => {
    await page.goto(ELEMENT_URL);
  }
);

/**
 * "When alex navigates to room {string}"
 * Clicks the room in the sidebar by name.
 * M-1: resolves the actual (suffixed) room name from the template name.
 */
When(
  '{word} navigates to room {string}',
  async ({ page }: { page: Page }, _userName: string, roomName: string) => {
    // M-1: use the actual (suffixed) room name for UI lookup
    const actualName = getActualRoomName(roomName);

    // Try to find by title attribute in room list
    const roomEntry = page.getByTestId('room-list').locator(`[title="${actualName}"]`);
    const byTitle = page.locator(`.mx_RoomTile[title="${actualName}"]`);

    const found = await roomEntry.first().isVisible({ timeout: 10_000 }).catch(() => false)
      || await byTitle.first().isVisible({ timeout: 5_000 }).catch(() => false);

    if (found) {
      if (await roomEntry.first().isVisible({ timeout: 1_000 }).catch(() => false)) {
        await roomEntry.first().click();
      } else {
        await byTitle.first().click();
      }
    } else {
      // Fallback: look anywhere in the left panel for the room name
      const fallback = page.locator('.mx_LeftPanel').getByTitle(actualName)
        .or(page.locator('.mx_LeftPanel').getByText(actualName, { exact: true }));
      await fallback.first().waitFor({ state: 'visible', timeout: 20_000 });
      await fallback.first().click();
    }
  }
);

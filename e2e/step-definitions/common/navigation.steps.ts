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
 *
 * Element Web 1.12.15 change: Room tiles use aria-label="Open room {name}" (no title attribute,
 * no .mx_RoomTile class). Strategy order: aria-label → title → text fallback.
 */
When(
  '{word} navigates to room {string}',
  async ({ page }: { page: Page }, _userName: string, roomName: string) => {
    // M-1: use the actual (suffixed) room name for UI lookup
    const actualName = getActualRoomName(roomName);

    // Strategy 1 (Element Web 1.12.15+): aria-label="Open room {name}" (joined rooms)
    // Also check "Open room {name} invitation." for rooms where user has a pending invite
    const byAriaLabel = page.locator(
      `[aria-label="Open room ${actualName}"], [aria-label="Open room ${actualName} invitation."]`
    );

    // Strategy 2 (older versions): title attribute in room list
    const roomEntry = page.getByTestId('room-list').locator(`[title="${actualName}"]`);
    const byTitle = page.locator(`.mx_RoomTile[title="${actualName}"]`);

    // Strategy 3: text anywhere in the left panel
    const byText = page.locator('.mx_LeftPanel').getByTitle(actualName)
      .or(page.locator('.mx_LeftPanel').getByText(actualName, { exact: true }));

    if (await byAriaLabel.first().waitFor({ state: 'attached', timeout: 10_000 }).then(() => true).catch(() => false)) {
      await byAriaLabel.first().scrollIntoViewIfNeeded().catch(() => {});
      await byAriaLabel.first().click();
      // If clicking an invite entry shows "Do you want to join?" panel, accept it automatically.
      // Wait for the Accept/Join button or the room body to appear (deterministic).
      const joinBtn = page.getByRole('button', { name: /^accept$|^join the discussion$/i });
      if (await joinBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
        await joinBtn.first().click();
        // Wait for room view to load after joining
        await page.locator('.mx_RoomView_body, .mx_BasicMessageComposer_input').first()
          .waitFor({ state: 'visible', timeout: 20_000 });
      }
    } else if (await roomEntry.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await roomEntry.first().click();
    } else if (await byTitle.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
      await byTitle.first().click();
    } else {
      await byText.first().waitFor({ state: 'visible', timeout: 20_000 });
      await byText.first().click();
    }
  }
);

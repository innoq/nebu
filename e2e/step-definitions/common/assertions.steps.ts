/**
 * Common assertion steps — reusable Then/And steps for all features.
 *
 * Story 9-26 — Phase 1, AC4.
 * Story 9-26a — M-1 fix: use getActualRoomName() for sidebar/UI assertions.
 */

import { Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { getActualRoomName } from './room-setup.steps';

/**
 * "Then the room list is visible"
 * Asserts that .mx_LeftPanel is visible — the main room list panel.
 */
Then('the room list is visible', async ({ page }: { page: Page }) => {
  await expect(page.locator('.mx_LeftPanel')).toBeVisible({ timeout: 20_000 });
});

/**
 * "Then no error dialog appears"
 * Asserts that no .mx_Dialog with error content is visible.
 */
Then('no error dialog appears', async ({ page }: { page: Page }) => {
  const errorDialog = page.locator('.mx_Dialog .mx_Dialog_content:has-text("error"), .mx_ErrorMessage');
  const count = await errorDialog.count();
  expect(count).toBe(0);
});

/**
 * "Then the room {string} appears in the sidebar"
 *
 * M-1: resolves the actual (suffixed) room name from the template name.
 *
 * Element Web 1.12.15 change: Room tiles use aria-label="Open room {name}" (no title
 * attribute, no .mx_RoomTile class). Include "invitation." suffix variant for invite rooms.
 */
Then(
  'the room {string} appears in the sidebar',
  async ({ page }: { page: Page }, roomName: string) => {
    const actualName = getActualRoomName(roomName);

    // Strategy 1 (Element Web 1.12.15+): aria-label="Open room {name}" (joined) OR
    // aria-label="Open room {name} invitation." (invited)
    const byAriaLabel = page.locator(`[aria-label="Open room ${actualName}"], [aria-label="Open room ${actualName} invitation."]`);

    // Strategy 2 (older versions): title attribute in room list
    const byTestId = page.getByTestId('room-list').locator(`[title="${actualName}"]`);
    const byTile   = page.locator(`.mx_RoomTile[title="${actualName}"]`);

    // Strategy 3: text anywhere in the left panel
    const byText   = page.locator('.mx_LeftPanel').getByText(actualName, { exact: true });

    // Use 'attached' state (not 'visible') since the element may be in the DOM but
    // scrolled out of view in a long room list. Playwright isVisible() returns false
    // for off-screen elements in scrollable containers.
    const found = await byAriaLabel.first().waitFor({ state: 'attached', timeout: 20_000 }).then(() => true).catch(() => false)
      || await byTestId.first().waitFor({ state: 'attached', timeout: 5_000 }).then(() => true).catch(() => false)
      || await byTile.first().waitFor({ state: 'attached', timeout: 5_000 }).then(() => true).catch(() => false)
      || await byText.first().waitFor({ state: 'attached', timeout: 5_000 }).then(() => true).catch(() => false);

    expect(found, `Room "${actualName}" should appear in the sidebar`).toBe(true);
  }
);

/**
 * "Then the room {string} is not in {word}'s sidebar"
 *
 * M-1: resolves the actual (suffixed) room name from the template name.
 *
 * Element Web 1.12.15 change: Room tiles use aria-label="Open room {name}".
 */
Then(
  'the room {string} is not in {word}\'s sidebar',
  async ({ page }: { page: Page }, roomName: string, _userName: string) => {
    const actualName = getActualRoomName(roomName);

    // Element Web 1.12.15+: aria-label based selectors
    const byAriaLabel = page.locator(`[aria-label="Open room ${actualName}"], [aria-label="Open room ${actualName} invitation."]`);

    // Older versions: title attribute
    const byTestId = page.getByTestId('room-list').locator(`[title="${actualName}"]`);
    const byTile   = page.locator(`.mx_RoomTile[title="${actualName}"]`);

    // Wait for the room to be removed from the DOM (Element Web sync may take a moment).
    // Strategy: wait for detached state (up to 10s), then verify not attached.
    // This avoids false positives from checking immediately before sync completes.
    await byAriaLabel.first().waitFor({ state: 'detached', timeout: 10_000 }).catch(() => {
      // If the element was never attached (already gone), this resolves immediately.
      // If it times out, it means the room is STILL in the sidebar after 10s.
    });

    // Re-check: confirm that no strategy finds the room (attached or visible)
    const stillPresent = await byAriaLabel.first().waitFor({ state: 'attached', timeout: 1_000 }).then(() => true).catch(() => false)
      || await byTestId.first().waitFor({ state: 'attached', timeout: 1_000 }).then(() => true).catch(() => false)
      || await byTile.first().waitFor({ state: 'attached', timeout: 1_000 }).then(() => true).catch(() => false);
    expect(stillPresent, `Room "${actualName}" should NOT be in the sidebar`).toBe(false);
  }
);

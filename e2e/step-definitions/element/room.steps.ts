/**
 * Step definitions for features/element/room/{create,join,leave}.feature
 *
 * Story 9-26 — Phase 2, AC7 + AC8 + AC9.
 * Story 9-26a — M-1 fix: use getActualRoomName() for sidebar/header assertions.
 * F-08 fix: replaced fragile `input[id="textinput_0"]` with getByLabel(/room name/i)
 *           or getByRole('textbox', { name: /name/i }).
 */

import { When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { ElementAppPage } from '../../fixtures/element-app';
import { getActualRoomName } from '../common/room-setup.steps';

// ─────────────────────────────────────────────────────────────────────────────
// AC7: Room Creation Steps
// ─────────────────────────────────────────────────────────────────────────────

/**
 * "When alex opens the {string} dialog"
 * Opens the Create Room dialog via ElementAppPage.
 */
When(
  '{word} opens the {string} dialog',
  async ({ page }: { page: Page }, _userName: string, _dialogName: string) => {
    const app = new ElementAppPage(page);
    await app.openCreateRoomDialog();
  }
);

/**
 * "When alex enters room name {string}"
 * F-08: uses getByLabel(/room name/i) or getByRole('textbox', { name: /name/i })
 * instead of the fragile generated ID `textinput_0`.
 */
When(
  '{word} enters room name {string}',
  async ({ page }: { page: Page }, _userName: string, roomName: string) => {
    // Try semantic locators first (robust across Element versions)
    const byLabel = page.locator('.mx_CreateRoomDialog').getByLabel(/room name|name/i);
    const byRole  = page.locator('.mx_CreateRoomDialog').getByRole('textbox', { name: /name/i });

    // Fallback to the input itself inside the dialog
    const byInput = page.locator('.mx_CreateRoomDialog input[type="text"]').first();

    let filled = false;

    if (await byLabel.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await byLabel.first().fill(roomName);
      filled = true;
    } else if (await byRole.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
      await byRole.first().fill(roomName);
      filled = true;
    }

    if (!filled) {
      await byInput.waitFor({ state: 'visible', timeout: 10_000 });
      await byInput.fill(roomName);
    }
  }
);

/**
 * "When alex clicks {string}"
 * Generic button click step — reused across create/join/leave/login scenarios.
 *
 * BUG-E2E-13 fix: "Accept" invite in Element Web 1.12.15 may be rendered as
 * "Join the discussion" when the room is empty or can't be previewed.
 * Map "Accept" → try "Accept" first, fall back to "Join the discussion".
 */
When(
  '{word} clicks {string}',
  async ({ page }: { page: Page }, _userName: string, buttonText: string) => {
    // For invite acceptance: "Accept" may appear as "Join the discussion" in Element 1.12+
    const isAccept = /^accept$/i.test(buttonText.trim());
    const pattern = isAccept
      ? /^Accept$|Join the discussion/i
      : new RegExp(buttonText, 'i');
    await page.getByRole('button', { name: pattern }).click({ timeout: 20_000 });
  }
);

/**
 * "Then the room header shows {string}"
 *
 * M-1: resolves the actual (suffixed) room name from the template name.
 */
Then(
  'the room header shows {string}',
  async ({ page }: { page: Page }, roomName: string) => {
    const actualName = getActualRoomName(roomName);
    // Element Web 1.12.15: room name is in a heading inside [aria-label="Room info"] button
    // in the room header. Also try legacy selectors for older versions.
    // BUG-E2E-12 fix: locator().or() can resolve to multiple elements causing strict mode violations.
    // Use the most specific locator first (scoped to room header/header area), fall through to others.
    // Scope [data-testid="room-name"] to main content area (not sidebar) to avoid duplicate matches.
    const headerLocator = page.locator('.mx_RoomHeader [role="heading"], .mx_RoomHeader_name, .mx_LegacyRoomHeader_name')
      .or(page.locator('header [role="heading"]'))
      .or(page.locator('main').locator('[data-testid="room-name"]'));
    await expect(headerLocator.first()).toHaveText(new RegExp(actualName, 'i'), { timeout: 20_000 });
  }
);

// ─────────────────────────────────────────────────────────────────────────────
// AC8: Room Join Steps
// ─────────────────────────────────────────────────────────────────────────────

/**
 * "When the invite for {string} appears in {word}'s sidebar"
 *
 * M-1: resolves the actual (suffixed) room name from the template name.
 *
 * Element Web 1.12.15 change: Invitations are shown in the flat room list as
 * [role="option"] buttons with aria-label="Open room {name} invitation." (with "invitation."
 * suffix). There is no separate [role="group" name="Invites"] section.
 *
 * Also clicks the invite entry so the Accept/Decline panel appears in the main area.
 */
When(
  'the invite for {string} appears in {word}\'s sidebar',
  async ({ page }: { page: Page }, roomName: string, _userName: string) => {
    const actualName = getActualRoomName(roomName);

    // Element Web 1.12.15: invitations appear in the flat room list as options
    // aria-label="Open room {name} invitation."
    // Use waitFor (not isVisible) so we wait for sync propagation (invite may take a few seconds).
    const inviteByAriaLabel = page.locator(`[aria-label="Open room ${actualName} invitation."]`);

    // Fallback for older versions: group role with "Invites" name
    const inviteSection = page.getByRole('group', { name: /invites/i });
    const inviteInSection = inviteSection.locator(`[title="${actualName}"]`);

    // Wait for the invite to appear (Element sync may take several seconds)
    try {
      await inviteByAriaLabel.first().waitFor({ state: 'visible', timeout: 25_000 });
      // Click the invite entry to open the Accept/Decline panel
      await inviteByAriaLabel.first().click();
    } catch {
      // Fallback: older group-based pattern
      try {
        await expect(inviteSection).toBeVisible({ timeout: 5_000 });
        await expect(inviteInSection).toBeVisible({ timeout: 5_000 });
        await inviteInSection.first().click();
      } catch {
        // Last resort: find any invite entry for this room by text content
        const byText = page.locator('.mx_LeftPanel').getByText(actualName, { exact: true }).first();
        await byText.waitFor({ state: 'visible', timeout: 5_000 });
        await byText.click();
      }
    }
  }
);

// ─────────────────────────────────────────────────────────────────────────────
// AC9: Room Leave Steps
// ─────────────────────────────────────────────────────────────────────────────

/**
 * "When alex opens the room menu and clicks {string}"
 *
 * Element Web 1.12.15 change: The room header no longer has a "..." / kebab / options button.
 * The room context menu is accessed via right-click on the room tile in the sidebar.
 * The menu has text-based [role="menuitem"] entries including "Leave room".
 *
 * Approach: find the current room's tile in the sidebar via room-setup state, right-click it,
 * then click the matching menuitem.
 */
When(
  '{word} opens the room menu and clicks {string}',
  async ({ page }: { page: Page }, _userName: string, menuItem: string) => {
    // Strategy 1 (Element Web 1.12.15+): right-click the active room tile in sidebar
    // The active tile has the highlighted background; find it via [aria-selected] or the current URL
    const currentUrl = page.url();
    const roomIdMatch = currentUrl.match(/#\/room\/([^/]+)/);

    let contextMenuOpened = false;

    if (roomIdMatch) {
      // Right-click any room tile — the current room is the one we're in
      // Find the tile by looking for the currently selected/focused room tile
      const activeTile = page.locator('[aria-selected="true"][role="option"], .mx_RoomListItemView._selected, button[aria-label*="Open room"]:focus')
        .first();
      const isActive = await activeTile.isVisible({ timeout: 2_000 }).catch(() => false);
      if (isActive) {
        await activeTile.click({ button: 'right' });
        contextMenuOpened = true;
      }
    }

    if (!contextMenuOpened) {
      // Strategy 2: Try the room header "..." / options button (older Element versions)
      const headerMenuBtn = page.locator('.mx_RoomHeader').getByRole('button', {
        name: /options|more options|settings|kebab|\.\.\.|⋮/i,
      });
      const headerBtnVisible = await headerMenuBtn.first().isVisible({ timeout: 3_000 }).catch(() => false);
      if (headerBtnVisible) {
        await headerMenuBtn.first().click();
        contextMenuOpened = true;
      }
    }

    if (!contextMenuOpened) {
      // Strategy 3: right-click the first visible room tile (fallback)
      const firstTile = page.locator('button[aria-label^="Open room"]').first();
      await firstTile.waitFor({ state: 'visible', timeout: 10_000 });
      await firstTile.click({ button: 'right' });
    }

    // Wait for context menu to appear and click the matching item
    await page.waitForTimeout(400);
    // Element 1.12.15 context menu items have no aria-label — match by text content
    const menuItemLocator = page.locator('[role="menuitem"]').filter({ hasText: new RegExp(menuItem, 'i') })
      .or(page.getByRole('menuitem', { name: new RegExp(menuItem, 'i') }));
    await menuItemLocator.first().waitFor({ state: 'visible', timeout: 5_000 });
    await menuItemLocator.first().click();
  }
);

/**
 * "When alex confirms leaving"
 * Confirmation dialog: click the "Leave" button.
 */
When(
  '{word} confirms leaving',
  async ({ page }: { page: Page }, _userName: string) => {
    await page.getByRole('button', { name: /^leave$/i }).click();
  }
);

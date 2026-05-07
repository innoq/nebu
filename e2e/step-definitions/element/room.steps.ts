/**
 * Step definitions for features/element/room/{create,join,leave,upgrade}.feature
 *
 * Story 9-26 — Phase 2, AC7 + AC8 + AC9.
 * Story 9-26a — M-1 fix: use getActualRoomName() for sidebar/header assertions.
 * Story 9-27 — AC6: Room upgrade E2E test steps.
 * F-08 fix: replaced fragile `input[id="textinput_0"]` with getByLabel(/room name/i)
 *           or getByRole('textbox', { name: /name/i }).
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { ElementAppPage } from '../../fixtures/element-app';
import { getActualRoomName, roomIdByScenario } from '../common/room-setup.steps';
import { getApiSession, createRoom } from '../../fixtures/dex-auth';
import { NEBU_USERS } from '../../fixtures/users';
import type { APIRequestContext, Browser } from '@playwright/test';

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

    // Wait for context menu to appear and click the matching item (deterministic — no hard wait)
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

// ─────────────────────────────────────────────────────────────────────────────
// AC6 (Story 9-27): Room Upgrade via Element Web
//
// Fixed in story 9-27: upgrade_room/2 error handling corrected, archive_room_atomic
// called after tombstone, old room GenServer terminated.
// ─────────────────────────────────────────────────────────────────────────────

/**
 * "Given alex has created a room named {string}"
 *
 * Story 9-27 AC6: The named user creates a room via the Matrix API so they are
 * the owner (Power Level 100) — required for the upgrade button to appear in Room Settings.
 * Uses a timestamp suffix for per-run uniqueness (M-1 pattern).
 *
 * The credentials fixture is already bound to alex via the Background step
 * "alex is logged in via Element Web" (credentials = NEBU_USERS.alex).
 */
Given(
  '{word} has created a room named {string}',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    userName: string,
    roomName: string
  ) => {
    const user = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!user) {
      throw new Error(`Unknown test user "${userName}". Valid: ${Object.keys(NEBU_USERS).join(', ')}`);
    }

    // M-1: timestamp suffix for per-run uniqueness
    const actualName = `${roomName}-${Date.now()}`;
    const session = await getApiSession(request, user, browser);
    const { room_id } = await createRoom(request, session.token, actualName);

    // Store for subsequent steps (e.g. open Room Settings, navigate to room)
    roomIdByScenario.set(roomName, { roomId: room_id, actualName });
  }
);

/**
 * "When alex opens Room Settings for {string}"
 *
 * Story 9-27 AC6: Navigate to the room in Element Web and open Room Settings.
 * Room Settings contain the "Upgrade to recommended chat version" button.
 *
 * Element Web 1.12.15: Room Settings accessible via:
 *   - The room header info button (cog / info icon) → "Room settings"
 *   - Or right-clicking the room tile and choosing "Settings"
 *
 */
When(
  '{word} opens Room Settings for {string}',
  async ({ page }: { page: Page }, _userName: string, roomName: string) => {
    const app = new ElementAppPage(page);
    const actualName = getActualRoomName(roomName);

    // Navigate to the room first
    await app.viewRoomByName(actualName);

    // Open Room Settings — try the settings/gear button in the room header
    const settingsBtn = page.locator('.mx_RoomHeader').getByRole('button', {
      name: /room info|settings|gear/i,
    }).or(
      page.locator('[aria-label="Room settings"], [aria-label="Open room settings"]')
    ).or(
      page.locator('.mx_RoomHeader [data-testid="base-card-close-button"]')
    );

    // Fallback: three-dot / kebab menu in header
    const kebabBtn = page.locator('.mx_RoomHeader').getByRole('button', {
      name: /more options|options|\.\.\.|⋮/i,
    });

    let opened = false;

    if (await settingsBtn.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await settingsBtn.first().click();
      opened = true;
    }

    if (!opened && await kebabBtn.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
      await kebabBtn.first().click();
      // Look for "Settings" in the dropdown
      const settingsItem = page.getByRole('menuitem', { name: /settings/i });
      if (await settingsItem.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
        await settingsItem.first().click();
        opened = true;
      }
    }

    if (!opened) {
      // Last resort: direct URL navigation with ?action=open_room_settings
      const entry = roomIdByScenario.get(roomName);
      if (entry) {
        await page.goto(`/#/room/${entry.roomId}?action=open_room_settings`);
        opened = true;
      }
    }

    if (!opened) {
      throw new Error(`Could not open Room Settings for room "${roomName}" — all selector strategies failed`);
    }

    // Wait for the Room Settings panel/dialog to be visible
    const settingsPanel = page.locator(
      '.mx_RoomSettingsDialog, [aria-label="Room Settings"], ' +
      '[data-testid="room-settings"], .mx_BaseCard[aria-label*="settings" i]'
    );
    await settingsPanel.first().waitFor({ state: 'visible', timeout: 15_000 });
  }
);

/**
 * "When alex upgrades the room to the recommended version"
 *
 * Story 9-27 AC6: Clicks the "Upgrade to recommended chat version" button in
 * Element Web's Room Settings → Advanced tab.
 *
 * Element Web 1.12.15: The upgrade button is in the "Advanced" tab of Room Settings.
 * Clicking it shows a confirmation dialog; confirming triggers POST /upgrade.
 *
 */
When(
  '{word} upgrades the room to the recommended version',
  async ({ page }: { page: Page }, _userName: string) => {
    // Navigate to the Advanced tab in Room Settings (contains the upgrade button)
    const advancedTab = page.getByRole('tab', { name: /advanced/i })
      .or(page.getByText(/advanced/i).filter({ hasText: /^advanced$/i }));

    if (await advancedTab.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await advancedTab.first().click();
    }

    // Find and click the upgrade button
    // Element Web renders this as a button with text "Upgrade this room to the recommended room version"
    // or similar phrasing.
    const upgradeBtn = page.getByRole('button', {
      name: /upgrade.*room|upgrade.*version|recommended.*version/i,
    }).or(
      page.getByText(/upgrade.*recommended|upgrade this room/i).first()
    );

    await upgradeBtn.first().waitFor({ state: 'visible', timeout: 15_000 });
    await upgradeBtn.first().click();

    // A confirmation dialog appears — scope to dialog to avoid matching the trigger button
    const dialog = page.locator('[role="dialog"], .mx_Dialog, .mx_QuestionDialog').first();
    const confirmBtn = dialog.getByRole('button', {
      name: /upgrade|confirm|yes/i,
    }).first();

    if (await confirmBtn.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await confirmBtn.click();
    }
  }
);

/**
 * "Then alex sees the new room without an error"
 *
 * Story 9-27 AC6: After upgrade, Element Web should redirect the user to the
 * new replacement room. The URL should change to the new room's #/room/{newRoomId}.
 *
 * After story 9-27 fix: server returns 200 and Element Web redirects to the new room.
 */
Then(
  '{word} sees the new room without an error',
  async ({ page }: { page: Page }, _userName: string) => {
    // Capture the URL *before* waiting for navigation so we can assert it changed.
    const urlBefore = page.url();

    // Wait for navigation — Element Web redirects to /#/room/{newRoomId}
    await page.waitForURL(/\/#\/room\//, { timeout: 20_000 }).catch(async () => {
      // Fallback: wait for the room view body to appear
      await page.locator('.mx_RoomView_body').waitFor({ state: 'visible', timeout: 10_000 });
    });

    // Assert: the URL changed to a different room (not still on the pre-upgrade room)
    expect(page.url(), 'Expected Element Web to navigate to the new (replacement) room after upgrade')
      .not.toBe(urlBefore);

    // Assert: the room view is visible
    const roomView = page.locator('.mx_RoomView_body, .mx_MainSplit');
    await expect(roomView.first()).toBeVisible({ timeout: 10_000 });
  }
);

/**
 * "Then alex does not see an error dialog"
 *
 * Story 9-27 AC6: After a successful upgrade, no error toast, snackbar,
 * or modal dialog with an error message should be visible.
 *
 *
 * Checks:
 *   - No .mx_ErrorDialog, .mx_GenericErrorPage, or aria[role=alertdialog]
 *   - No toast/snackbar with "error" or "failed" text
 *   - The upgrade result notification (if any) does NOT contain error language
 */
Then(
  '{word} does not see an error dialog',
  async ({ page }: { page: Page }, _userName: string) => {
    // Assert no error dialog is visible — use Playwright's built-in polling (no hard wait)
    const errorDialog = page.locator(
      '.mx_ErrorDialog, .mx_GenericErrorPage, [role="alertdialog"][aria-label*="error" i]'
    );
    await expect(errorDialog.first(), 'Expected no error dialog after room upgrade')
      .not.toBeVisible({ timeout: 3_000 });

    // Assert no toast/snackbar with error text
    const errorToast = page.locator('[role="alert"], .mx_Toast_title').filter({
      hasText: /error|failed|unable|couldn't|could not/i,
    });
    await expect(errorToast.first(), 'Expected no error toast after room upgrade')
      .not.toBeVisible({ timeout: 3_000 });
  }
);

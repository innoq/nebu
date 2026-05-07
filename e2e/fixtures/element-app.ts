/**
 * ElementAppPage — Page Object for the Element Web client.
 *
 * Story 9-26 — Phase 1, AC2.
 * Adapted from element-web/apps/web/playwright/pages/ElementAppPage.ts.
 * Trimmed for Nebu MVP: removes crypto, Spotlight, E2EE-specific helpers.
 *
 * Retains: getComposerField, openCreateRoomDialog, viewRoomByName,
 *          viewRoomById, inviteUserToCurrentRoom, openUserMenu, closeDialog.
 */

import { type Page, type Locator, expect } from '@playwright/test';

export class ElementAppPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  /**
   * Wait for the main room list panel to become visible.
   * Selector: .mx_LeftPanel
   */
  async waitForRoomList(): Promise<void> {
    await this.page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 20_000 });
  }

  /**
   * Open the "New room" / create room dialog.
   *
   * Element Web renders a "+" / "Add room" button in the left panel header.
   * We click it, then select "Create new room" from the resulting menu.
   */
  async openCreateRoomDialog(): Promise<void> {
    // Click the "Add room" composite button (various labels depending on Element version)
    const addRoomBtn = this.page.getByRole('button', { name: /add room|new room|create.*room|plus/i })
      .or(this.page.locator('.mx_RoomListHeader_auxButton'))
      .first();

    await addRoomBtn.waitFor({ state: 'visible', timeout: 10_000 });
    await addRoomBtn.click();

    // A context menu appears — click "Create new room"
    const createItem = this.page.getByRole('menuitem', { name: /create.*room|new room/i })
      .or(this.page.getByRole('option', { name: /create.*room|new room/i }));

    if (await createItem.first().isVisible({ timeout: 3_000 }).catch(() => false)) {
      await createItem.first().click();
    }
    // The .mx_CreateRoomDialog should now be visible
    await this.page.locator('.mx_CreateRoomDialog').waitFor({ state: 'visible', timeout: 10_000 });
  }

  /**
   * Navigate to a room by its display name in the sidebar.
   * Tries multiple selector strategies for robustness across Element versions.
   */
  async viewRoomByName(name: string): Promise<void> {
    // Strategy 1: getByTestId room-list then title attribute
    const byTitle = this.page.getByTestId('room-list').locator(`[title="${name}"]`);

    // Strategy 2: direct locator on room list items by title
    const byListItem = this.page.locator(`.mx_RoomTile[title="${name}"], .mx_RoomTile[aria-label="${name}"]`);

    let found = false;

    // Try strategy 1
    if (await byTitle.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await byTitle.first().click();
      found = true;
    }

    // Try strategy 2
    if (!found && await byListItem.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await byListItem.first().click();
      found = true;
    }

    if (!found) {
      // Fallback: look for any element containing the room name in the left panel
      const fallback = this.page.locator('.mx_LeftPanel').getByText(name, { exact: true })
        .or(this.page.locator('.mx_LeftPanel').getByTitle(name));
      await expect(fallback.first()).toBeVisible({ timeout: 20_000 });
      await fallback.first().click();
    }
  }

  /**
   * Navigate to a room by its Matrix room_id.
   * Constructs the Element Web URL hash route.
   */
  async viewRoomById(roomId: string): Promise<void> {
    await this.page.goto(`/#/room/${roomId}`);
    await this.page.locator('.mx_RoomView_body').waitFor({ state: 'visible', timeout: 20_000 });
  }

  /**
   * Get the message composer input field (contenteditable div).
   * Selector: .mx_RoomView_body .mx_MessageComposer div[contenteditable]
   */
  getComposerField(): Locator {
    return this.page.locator('.mx_RoomView_body .mx_MessageComposer div[contenteditable]');
  }

  /**
   * Open the user menu (avatar / display name in the top-left corner).
   * Clicks the user avatar button to reveal the menu.
   */
  async openUserMenu(): Promise<void> {
    // Element renders user info in top-left: either a button with the username or avatar
    const userBtn = this.page.locator('.mx_UserMenu button, [aria-label*="User menu"], [data-testid="user-menu-trigger"]')
      .or(this.page.locator('.mx_LeftPanel .mx_UserMenu'));

    await userBtn.first().waitFor({ state: 'visible', timeout: 10_000 });
    await userBtn.first().click();
  }

  /**
   * Close any open dialog (e.g. after room creation).
   * Presses Escape or clicks the Close button.
   */
  async closeDialog(): Promise<void> {
    // Deliberate fallback chain: mx_Dialog_cancelButton was renamed in Element Web v1.11+.
    // Escape key is the last resort if neither selector matches.
    const closeBtn = this.page.locator('.mx_Dialog .mx_Dialog_cancelButton, .mx_Dialog button[aria-label="Close"]');
    if (await closeBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
      await closeBtn.first().click();
    } else {
      await this.page.keyboard.press('Escape');
    }
  }

  /**
   * Invite a user to the current room via the room's member panel.
   * Opens the invite dialog and submits with the given Matrix user ID.
   */
  async inviteUserToCurrentRoom(userId: string): Promise<void> {
    // Open room info / member list
    const inviteBtn = this.page.getByRole('button', { name: /invite/i });
    await inviteBtn.waitFor({ state: 'visible', timeout: 10_000 });
    await inviteBtn.click();

    // Fill the invite input
    const input = this.page.locator('.mx_InviteDialog input[type="text"], .mx_InviteDialog input[placeholder]');
    await input.waitFor({ state: 'visible', timeout: 10_000 });
    await input.fill(userId);

    // Confirm invitation
    await this.page.getByRole('button', { name: /invite|send invitation/i }).last().click();
  }
}

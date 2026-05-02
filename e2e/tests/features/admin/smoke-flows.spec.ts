/**
 * Story 7.14: Gherkin Admin UI Smoke Flows
 *
 * Flow 1: Admin deactivates a user (usr-003 Carla Reiter — active, role: user)
 * Flow 2: Admin archives a room (room-002 Engineering — active, private)
 *
 * Target IDs were chosen to avoid collision with existing specs:
 *   - user-role.spec.ts touches usr-001 (deactivate, NOT restored) — use usr-003 instead.
 *   - room-detail.spec.ts opens the archive dialog on room-001 but does NOT submit it — safe;
 *     however room-002 is cleaner and avoids any ordering risk.
 *
 * Cleanup: afterEach restores stub state via POST to reactivate/unarchive routes (Story 7.14
 * adds these routes as inverse handlers of DeactivateUserHandler / ArchiveRoomHandler).
 *
 * Auth: OIDC Authorization Code + PKCE via Dex (kai@example.com / changeme).
 * Prerequisites: make dev running, bootstrap complete.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code + PKCE login via Dex.
// Identical pattern to user-role.spec.ts, room-detail.spec.ts, and all other admin specs.
// ---------------------------------------------------------------------------
async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/admin/login/start');
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();

  const grantBtn = page.locator(
    'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
  );
  if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
    await grantBtn.first().click();
  }

  await page.waitForURL(/\/admin\//, { timeout: 15_000 });
}

// ---------------------------------------------------------------------------
// Flow 1: Admin deactivates a user
// ---------------------------------------------------------------------------
test.describe('Flow: Admin deactivates a user', () => {
  // usr-003 Carla Reiter — active, role: user. Not touched by any other spec.
  const TARGET_USER = 'usr-003';

  test.afterEach(async ({ page }) => {
    // Ensure an active session before the cleanup POST (the test page may have
    // navigated to the redirect target, but the session cookie is still valid).
    // Log in if the session cookie is not present / expired.
    await loginAsAdmin(page);
    // Restore: POST to reactivate endpoint (registered in Story 7.14).
    // page.request shares the browser context and inherits the session cookie.
    await page.request.post(`/admin/users/${TARGET_USER}/reactivate`);
  });

  test('admin navigates to user list, clicks user row, deactivates via confirm dialog', async ({ page }) => {
    // Given: admin is logged in
    await loginAsAdmin(page);

    // When: navigate to users list
    await page.goto('/admin/users');

    // When: click the target user row (a[role="option"] with href pointing to usr-003)
    await page.locator(`a[role="option"][href="/admin/users/${TARGET_USER}"]`).click();

    // Then: detail panel opens (section[role="region"] is visible)
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // When: click "Deactivate" button in the detail footer
    // Button is rendered by detail_footer when ActiveUser.Status == "active"
    await page.locator('button:has-text("Deactivate")').click();

    // Then: confirm dialog (dialog[role="alertdialog"]) becomes visible
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();

    // When: click the Deactivate submit button inside the dialog
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // Then: PRG redirect to /admin/users/usr-003 with flash= query param
    await expect(page).toHaveURL(new RegExp(`/admin/users/${TARGET_USER}.*[?&]flash=`), { timeout: 10_000 });

    // Then: flash banner (div[role="alert"]) is visible and mentions "deactivated"
    await expect(page.locator('div[role="alert"]')).toContainText(/deactivat/i);

    // Then: status badge in the detail panel shows "Inactive"
    // toUserRowData normalises "deactivated" → StatusBadgeData{Status: "inactive"},
    // and the badge template renders it capitalised as "Inactive".
    await expect(page.locator('section[role="region"]')).toContainText('Inactive');
  });
});

// ---------------------------------------------------------------------------
// Flow 2: Admin archives a room
// ---------------------------------------------------------------------------
test.describe('Flow: Admin archives a room', () => {
  // room-002 Engineering — active, private, 12 members. Not touched by other specs.
  const TARGET_ROOM = 'room-002';

  test.afterEach(async ({ page }) => {
    // Ensure an active session before the cleanup POST.
    await loginAsAdmin(page);
    // Restore: POST to unarchive endpoint (registered in Story 7.14).
    await page.request.post(`/admin/rooms/${TARGET_ROOM}/unarchive`);
  });

  test('admin navigates to room list, clicks room row, archives via confirm dialog', async ({ page }) => {
    // Given: admin is logged in
    await loginAsAdmin(page);

    // When: navigate to rooms list
    await page.goto('/admin/rooms');

    // When: click the target room row (a[role="option"] with href pointing to room-002)
    await page.locator(`a[role="option"][href="/admin/rooms/${TARGET_ROOM}"]`).click();

    // Then: detail panel opens
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // When: click "Archive room" button in the detail footer
    // Button is rendered by detail_footer when ActiveRoom.Status == "active"
    await page.locator('button:has-text("Archive room")').click();

    // Then: confirm dialog becomes visible
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();

    // When: click the Archive submit button inside the dialog
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // Then: PRG redirect to /admin/rooms/room-002 with flash= query param
    await expect(page).toHaveURL(new RegExp(`/admin/rooms/${TARGET_ROOM}.*[?&]flash=`), { timeout: 10_000 });

    // Then: flash banner is visible and mentions "archived"
    await expect(page.locator('div[role="alert"]')).toContainText(/archiv/i);

    // Then: status badge in the detail panel shows "Inactive"
    // toRoomRowData normalises "archived" → StatusBadgeData{Status: "inactive"},
    // and the badge template renders it capitalised as "Inactive".
    await expect(page.locator('section[role="region"]')).toContainText('Inactive');
  });
});

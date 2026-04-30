/**
 * Story 7.9: Room Detail Panel (settings edit, archive UI, confirmation pattern)
 *
 * These tests exercise the /admin/rooms/{id} detail panel — avatar, inline edit for
 * room name, status badge, flash banner after PRG redirect, and archive confirm dialog.
 *
 * Auth: Uses OIDC Authorization Code + PKCE (identical to rooms-page.spec.ts).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete.
 *
 * All tests are REAL (not test.skip) — the detail route /admin/rooms/{id} has been live
 * since Story 7.2.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — performs OIDC Authorization Code login via Dex
// Identical pattern to rooms-page.spec.ts.
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
// Test suite
// ---------------------------------------------------------------------------
test.describe('Room Detail Panel', () => {

  test('room detail panel opens when clicking a room row', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Click the first room list item (role="option")
    await page.locator('[role="option"]').first().click();

    // Detail panel (section[role="region"]) must be visible and contain an edit button
    // (proving the inline_edit component rendered the selected room's detail)
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();
    await expect(detailPanel.locator('[aria-label^="Edit"]')).toBeVisible();
  });

  test('flash message shown after room name update', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms/room-001?flash=Room+name+updated');

    // Flash banner with success message must be visible
    await expect(page.locator('div[role="alert"]')).toContainText('Room name updated');
  });

  test('inline edit saves room name', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms/room-001');

    // Click the edit button for room name
    await page.locator('button[aria-label="Edit Room Name"]').click();

    // Fill a new room name in the inline input
    await page.locator('input[name="name"]').fill('Test Room Name');

    // Submit the form
    await page.locator('button[type="submit"]:has-text("Save")').click();

    // Page should redirect (PRG) to the detail URL with a flash param
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });

    // Flash banner (success alert) must be visible after redirect
    await expect(page.locator('div[role="alert"]')).toBeVisible();
  });

  test('archive button opens confirmation dialog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms/room-001');

    // Click the "Archive room" button to trigger the confirm dialog
    await page.locator('button:has-text("Archive room")').click();

    // The confirm dialog must become visible
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
  });

});

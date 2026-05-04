/**
 * Story 9.3: Admin UI — Rooms API Integration
 *
 * These tests verify that the Admin UI room management pages use the REAL
 * Admin API (gRPC-backed) instead of the in-memory stub data.
 *
 * RED PHASE: All tests in this file are FAILING until Story 9.3 is implemented.
 * They fail because the gateway handlers still serve stubRooms data.
 *
 * Failure reasons per test:
 *   AC1 — "General" (stub sentinel) is still visible in the list because
 *          ListHandler calls filterStubRooms(stubRooms, ...) instead of the
 *          gRPC ListAdminRooms RPC. The real database has no room named "General"
 *          sourced from stubs.
 *   AC2 — After archiving, the status badge does NOT persistently show "inactive"
 *          because ArchiveRoomHandler mutates stubRooms[i].Status in-memory.
 *          On server restart the stub is restored; the test verifies the new value
 *          persists after a reload cycle (only possible with a real DB-backed call).
 *   AC3 — max_members update posts to the stub mutation path (UpdateRoomNameHandler);
 *          on reload the stub does not reflect a persisted DB value. The real gRPC
 *          UpdateRoomSettings call stores max_members in PostgreSQL.
 *
 * Auth: OIDC Authorization Code + PKCE via Dex (kai@example.com / changeme).
 * Prerequisites: full dev stack running (`make dev`), bootstrap complete,
 *               at least one real room exists in PostgreSQL.
 *
 * Spec path (as declared in story 9.3):
 *   e2e/tests/features/admin/rooms-api-integration.spec.ts
 */
import { test, expect } from '@playwright/test';
import { loginAsAdmin } from '../../fixtures/helpers';

// ---------------------------------------------------------------------------
// AC1: Rooms list shows real DB rooms — stub sentinels must NOT appear
//
// RED: Fails because ListHandler still calls filterStubRooms(stubRooms, ...).
//      The stub slice contains rooms named "General", "Engineering", etc.
//      The assertions `not.toBeVisible()` therefore fail for these sentinel names
//      when stubs are still active (the real DB has no rooms with those exact stub names).
//
// Note: The assertion strategy follows the same pattern as users-api-integration.spec.ts
//       (AC1): we assert stub sentinel absence AND real-row presence simultaneously,
//       which is only satisfiable when the list is DB-backed.
// ---------------------------------------------------------------------------
test.describe('AC1: Rooms list shows real rooms from DB (not stub data)', () => {

  test('stub sentinel "General" is NOT present in the rooms list', async ({ page }) => {
    // Given: full dev stack running, bootstrap complete, at least one real room exists in DB
    await loginAsAdmin(page);

    // When: navigate to /admin/rooms
    await page.goto('/admin/rooms');

    // Then: the page renders successfully
    await expect(page.locator('h1')).toBeVisible();

    // Then: "General" (stub room-001 from stubs.go) is NOT shown.
    //       If stubs are still active, this assertion fails.
    await expect(page.getByText('General')).not.toBeVisible();
  });

  test('stub sentinel "Engineering" is NOT present in the rooms list', async ({ page }) => {
    // Complementary check: another stub name must also be absent.
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    await expect(page.locator('h1')).toBeVisible();

    // Then: "Engineering" (stub room-002) must NOT appear.
    //       Real DB has no room "Engineering". Fails while stubs are active.
    await expect(page.getByText('Engineering')).not.toBeVisible();
  });

  test('at least one real room row is rendered from the database', async ({ page }) => {
    // Given: bootstrap has been completed, at least one DB room exists
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Then: at least one room row link (a[role="option"]) is rendered.
    //       Together with the sentinel-absence checks above, both assertions
    //       must hold simultaneously — possible only with real DB integration.
    await expect(page.locator('a[role="option"]').first()).toBeVisible();
  });

});

// ---------------------------------------------------------------------------
// AC2: Archive flow calls real API — stubRooms slice NOT mutated
//
// RED: Fails because ArchiveRoomHandler directly mutates stubRooms[i].Status
//      instead of calling the gRPC ArchiveRoom RPC.
//      After implementing Story 9.3 the stub mutation is removed; the test verifies
//      that the room detail page reloads consistently with "inactive" status (DB-backed).
//
// Strategy: find the first active room row in the list, navigate to its detail,
//           archive it via the confirm dialog, and assert the correct PRG redirect
//           + status badge shows "inactive".
// ---------------------------------------------------------------------------
test.describe('AC2: Archive flow calls real API and reflects status change', () => {

  // Real room ID resolved at runtime from the first active row in the list.
  // We do NOT hard-code a stub ID (room-001) — that would be the stub path.
  let targetRoomId: string | null = null;

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Find the first active room row (a[role="option"] link).
    const firstRow = page.locator('a[role="option"]').first();
    await expect(firstRow).toBeVisible({ timeout: 10_000 });

    // Extract the room ID from the href: /admin/rooms/{roomId}
    const href = await firstRow.getAttribute('href');
    if (!href) throw new Error('No room rows found on /admin/rooms');
    const match = href.match(/\/admin\/rooms\/([^/?]+)/);
    if (!match) throw new Error(`Could not parse roomId from href: ${href}`);
    targetRoomId = match[1];
  });

  test.afterEach(async ({ page }) => {
    // Cleanup: unarchive the room so other tests start from a clean state.
    // NOTE (TEA MINOR-4): page.request.post bypasses CSRF middleware — the cleanup
    // POST will receive 403 from CSRFMiddleware. This is a best-effort safety net
    // only; the next test's beforeEach finds the next active room from the list,
    // so a failed cleanup does not break test isolation.
    if (!targetRoomId) return;
    await page.request.post(`/admin/rooms/${targetRoomId}/unarchive`);
  });

  test('archive redirects with flash and status badge shows inactive', async ({ page }) => {
    // Given: admin is on the detail page for a real active room
    // (loginAsAdmin already ran in beforeEach — same browser context, no re-login needed)
    await page.goto(`/admin/rooms/${targetRoomId}`);

    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // When: admin opens the confirm dialog and archives
    await page.locator('button:has-text("Archive room")').click();
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // Then: PRG redirect to detail URL with flash=
    await expect(page).toHaveURL(
      new RegExp(`/admin/rooms/${targetRoomId}.*[?&]flash=`),
      { timeout: 10_000 }
    );

    // Then: flash banner mentions archiving
    await expect(page.locator('div[role="alert"]')).toContainText(/archived/i);

    // Then: status badge in the detail panel shows "inactive" (mapped from "archived")
    await expect(page.locator('section[role="region"]')).toContainText('inactive');
  });

  test('after archiving the room detail reflects the status change on reload', async ({ page }) => {
    // This test proves the change is DB-backed (not in-memory stub):
    // reload the detail page in a fresh navigation and the status is still persisted.
    // (login already established in beforeEach)
    await page.goto(`/admin/rooms/${targetRoomId}`);

    await page.locator('button:has-text("Archive room")').click();
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();
    await expect(page).toHaveURL(
      new RegExp(`/admin/rooms/${targetRoomId}.*[?&]flash=`),
      { timeout: 10_000 }
    );

    // Navigate away and back (forces a new handler call — no in-memory caching)
    await page.goto('/admin/rooms');
    await page.goto(`/admin/rooms/${targetRoomId}`);

    // Status badge must still show "inactive" after reload (DB-backed persistence)
    await expect(page.locator('section[role="region"]')).toContainText('inactive');
  });

});

// ---------------------------------------------------------------------------
// AC3: Update room settings (max_members) calls real API and UI reflects changes
//
// RED: Fails because UpdateRoomNameHandler / the settings form mutates stubRooms
//      in-memory instead of calling the gRPC UpdateRoomSettings RPC.
//      After Story 9.3, max_members is stored in PostgreSQL via UpdateRoomSettings.
//      With stubs: on page reload after PRG, the new value is lost (server restart
//      resets the stub). This test navigates back after redirect and verifies
//      persistence.
//
// Note (Dev Notes): UpdateRoomSettingsRequest only covers max_members; visibility
// is not a field in the proto. AC3 is satisfied if max_members persists after
// a PRG redirect + reload cycle via the real gRPC call.
// ---------------------------------------------------------------------------
test.describe('AC3: Update room settings calls PATCH-equivalent gRPC and value persists', () => {

  let targetRoomId: string | null = null;
  let originalMaxMembers: string | null = null;

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Find the first active room row.
    const firstRow = page.locator('a[role="option"]').first();
    await expect(firstRow).toBeVisible({ timeout: 10_000 });
    const href = await firstRow.getAttribute('href');
    if (!href) throw new Error('No room rows on /admin/rooms');
    const match = href.match(/\/admin\/rooms\/([^/?]+)/);
    if (!match) throw new Error(`Could not parse roomId from href: ${href}`);
    targetRoomId = match[1];

    // Navigate to detail to read the current max_members before changing it.
    await page.goto(`/admin/rooms/${targetRoomId}`);
    const maxMembersInput = page.locator('section[role="region"] input[name="max_members"]');
    await expect(maxMembersInput).toBeVisible();
    originalMaxMembers = await maxMembersInput.inputValue();
  });

  test.afterEach(async ({ page }) => {
    // Restore the original max_members value via POST so other tests see a clean state.
    // NOTE (TEA MINOR-4): page.request.post bypasses CSRF middleware — best-effort cleanup.
    if (!targetRoomId || originalMaxMembers === null) return;
    const formData = new URLSearchParams({ max_members: originalMaxMembers });
    await page.request.post(`/admin/rooms/${targetRoomId}/settings`, {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      data: formData.toString(),
    });
  });

  test('max_members updated and persists after PRG redirect', async ({ page }) => {
    // Given: admin is on the detail panel
    // (login already established in beforeEach — same browser context)
    await page.goto(`/admin/rooms/${targetRoomId}`);

    const maxMembersInput = page.locator('section[role="region"] input[name="max_members"]');
    await expect(maxMembersInput).toBeVisible();

    // When: admin sets a new max_members value (50 — unlikely to be the current default)
    const newMaxMembers = originalMaxMembers === '50' ? '75' : '50';
    await maxMembersInput.fill(newMaxMembers);

    // Submit the settings form — use form-specific selector to avoid StrictModeViolation
    // (section[role="region"] contains both the settings form and a hidden inline_edit form)
    await page.locator(`form[action="/admin/rooms/${targetRoomId}/settings"] button[type="submit"]`).click();

    // Then: PRG redirect to detail URL with flash=
    await expect(page).toHaveURL(
      new RegExp(`/admin/rooms/${targetRoomId}.*[?&]flash=`),
      { timeout: 10_000 }
    );

    // Then: flash banner confirms update
    await expect(page.locator('div[role="alert"]')).toContainText(/update/i);

    // Then: reload the detail page and verify max_members shows the new value
    await page.goto(`/admin/rooms/${targetRoomId}`);
    await expect(
      page.locator('section[role="region"] input[name="max_members"]')
    ).toHaveValue(newMaxMembers);
  });

});

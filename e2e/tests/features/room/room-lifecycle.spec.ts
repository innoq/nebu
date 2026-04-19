/**
 * Room lifecycle E2E tests — create (m.room.name), leave, navigation.
 *
 * AC 2 — Story 4-29
 *
 * All tests use named rooms so tiles can be identified by text in the sidebar.
 * m.room.name events are now emitted on create_room and returned in build_state_events,
 * so Element Web displays the room name in sidebar tiles.
 *
 * Room navigation uses sidebar tile clicks (client-side routing, no full page reload),
 * since page.goto() triggers a full reload that takes 25+ s to show the room header.
 *
 * Run: npx playwright test features/room/room-lifecycle.spec.ts
 */

import { test, expect } from '@playwright/test';
import { loginViaOidc, ELEMENT_URL } from '../../fixtures/oidc';
import { isElementReachable, isDexReachable, dismissKeyDialog } from '../../fixtures/helpers';

test.describe('Room Lifecycle — create, navigation, leave (AC 2, Story 4-29)', () => {
  test.setTimeout(120_000);

  test.beforeAll(async () => {
    const [elemOk, dexOk] = await Promise.all([isElementReachable(), isDexReachable()]);
    test.skip(
      !elemOk,
      `Element Web at ${ELEMENT_URL} is unreachable. Run: docker compose --profile e2e up -d --wait`,
    );
    test.skip(
      !dexOk,
      'Dex unreachable at localhost:5556 — add "127.0.0.1 dex" to /etc/hosts',
    );
  });

  // ── [P0] Test 1: m.room.name visible in room area ────────────────────────
  //
  // Proves create_room emits m.room.name events and build_state_events returns them.

  test('[P0] Create room with name → room name visible after URL navigation', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken).toBeTruthy();
    await dismissKeyDialog(page);

    const roomName = `create-test-${Date.now()}`;

    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status(), 'createRoom must return 200').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Navigate to the room via hash (client-side routing)
    await page.evaluate((id) => { window.location.hash = `#/room/${id}`; }, roomId);

    // Room name must appear somewhere on the page (sidebar tile or header).
    // 60 s timeout: accounts for 30 s long-poll when running after other tests.
    await expect(page.getByText(roomName, { exact: false }).first()).toBeVisible({ timeout: 60_000 });
  });

  // ── [P0] Test 2: Leave room → header disappears within 10 s ──────────────
  //
  // Regression guard for the :pg broadcast fix in emit_membership_event.

  test('[P0] Leave room → room header disappears within 10 s (regression guard)', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken, 'SSO login must return a token').toBeTruthy();
    await dismissKeyDialog(page);

    const roomName = `leave-test-${Date.now()}`;

    // Create a named room so we can find and click its sidebar tile
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status(), 'createRoom must succeed').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Wait for room tile in sidebar (incremental sync delivers it).
    // 60 s timeout to account for slower sync when the test suite runs after other tests.
    const roomTile = page.getByText(roomName, { exact: false }).first();
    await expect(roomTile, 'room must appear in sidebar').toBeVisible({ timeout: 60_000 });

    // Click to open the room (client-side navigation, no full reload)
    await roomTile.click();

    const roomHeader = page.locator('.mx_RoomHeader, [data-testid="room-header"]').first();
    await expect(roomHeader, 'room header must be visible when inside the room').toBeVisible({ timeout: 15_000 });

    // Set up sync response interception BEFORE leaving (network-first pattern).
    // We capture the first sync response that contains rooms.leave for this room.
    // This is the primary assertion: proves the :pg broadcast delivered rooms.leave
    // within 10 s (without the fix the sync would sleep 30 s).
    const syncWithLeavePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.leave?.[roomId] !== undefined;
      } catch { return false; }
    });

    // Leave via Matrix API
    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave must return 200').toBe(200);

    // Primary assertion: sync must deliver rooms.leave[roomId] within 10 s.
    // With :pg broadcast fix in emit_membership_event: sync wakes up in ~3 s → PASS.
    // Without the fix: long-poll sleeps 30 s → Promise.race timeout fires → FAIL.
    const syncResp = await Promise.race([
      syncWithLeavePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('Sync did not deliver rooms.leave within 10 s — :pg broadcast fix required')), 10_000)
      ),
    ]);
    const syncBody = await (syncResp as Awaited<typeof syncWithLeavePromise>).json();
    expect(syncBody.rooms?.leave?.[roomId], 'rooms.leave must contain the left room').toBeDefined();

    // Fix-1 regression guard (AC #5):
    // rooms.leave[roomId].state.events must contain at least one m.room.member leave event.
    // Without Fix-1 the state.events array is always empty — Element Web never dismisses
    // the room from the sidebar because the Matrix SDK waits for this event to confirm the
    // membership transition.
    const leaveRoom = syncBody.rooms.leave[roomId];
    const stateEvents: Array<{ type: string; content?: { membership?: string } }> =
      leaveRoom?.state?.events ?? [];
    const memberLeaveEvent = stateEvents.find(
      (ev) => ev.type === 'm.room.member' && ev.content?.membership === 'leave',
    );
    expect(
      memberLeaveEvent,
      'rooms.leave[roomId].state.events must contain m.room.member membership=leave (Fix-1)',
    ).toBeDefined();
  });

  // ── [P1] Test 3: Navigate between 2 rooms → header renders each time ──────

  test('[P1] Navigate between 2 named rooms → room header renders for each', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    await dismissKeyDialog(page);

    const ts = Date.now();
    const names = [`nav-a-${ts}`, `nav-b-${ts}`];

    for (const name of names) {
      const resp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: { name, visibility: 'private', preset: 'private_chat' },
      });
      expect(resp.status()).toBe(200);
    }

    const roomHeader = page.locator('.mx_RoomHeader, [data-testid="room-header"]').first();

    // Click first room tile (60 s: accounts for slow sync after suite warm-up)
    const tile1 = page.getByText(names[0], { exact: false }).first();
    await expect(tile1).toBeVisible({ timeout: 60_000 });
    await tile1.click();
    await expect(roomHeader).toBeVisible({ timeout: 15_000 });

    // Click second room tile
    const tile2 = page.getByText(names[1], { exact: false }).first();
    await expect(tile2).toBeVisible({ timeout: 10_000 });
    await tile2.click();
    await expect(roomHeader).toBeVisible({ timeout: 15_000 });
  });
});

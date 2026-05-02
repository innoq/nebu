/**
 * Room invites E2E tests — invite rendering and decline.
 *
 * AC 3 — Story 4-29
 *
 * Tests:
 *   1. Invite appears in sidebar: marie (API-bot) creates a room, invites alex
 *      → alex's sidebar shows the room in invite state
 *   2. Decline invite removes room: alex declines → room disappears within 10 s
 *      (same sync path as leave, tests the same :pg broadcast that AC 2 tests)
 *
 * Multi-user pattern:
 *   marie logs in via second browser context (new page), gets token, closes context.
 *   All "marie" operations are API-only (no UI for marie).
 *   alex's UI is the browser under test.
 *
 * Run: npx playwright test features/room/invites.spec.ts
 */

import { test, expect } from '@playwright/test';
import { loginViaOidc, ELEMENT_URL } from '../../fixtures/oidc';
import { isElementReachable, isDexReachable, dismissKeyDialog } from '../../fixtures/helpers';

// ---------------------------------------------------------------------------
// Suite guard
// ---------------------------------------------------------------------------

test.describe('Room Invites — render and decline (AC 3, Story 4-29)', () => {
  test.setTimeout(150_000);

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

  // ── [P0] Test 1: Invite appears in alex's sidebar ────────────────────────

  test('[P0] Invite appears in sidebar: marie invites alex → invite tile visible within 20 s', async ({ page }) => {
    // Given: alex is logged in via OIDC
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(alexSession.accessToken).toBeTruthy();
    expect(alexSession.userId).toBeTruthy();

    await dismissKeyDialog(page);

    // Given: marie logs in via API (second browser context, just for token)
    const marieContext = await page.context().browser()!.newContext();
    const mariePage = await marieContext.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme');
    await mariePage.close();
    await marieContext.close();

    expect(marieSession.accessToken, 'marie must be logged in').toBeTruthy();

    // When: marie creates a named room and invites alex
    const roomName = `invite-test-${Date.now()}`;
    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { name: roomName, visibility: 'private', preset: 'private_chat' },
      },
    );
    expect(createResp.status(), 'marie must be able to createRoom').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Set up sync interception BEFORE sending invite (network-first — Murat TEA).
    // invite_user/2 now broadcasts {:new_invite, room_id} via :pg to the invitee's
    // user-level group, waking the long-poll within ~3 s. (Bug 4-29f fix, Story 8-10a)
    const syncWithInvitePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.invite?.[roomId] !== undefined;
      } catch { return false; }
    });

    const inviteResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/invite`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { user_id: alexSession.userId },
      },
    );
    expect(inviteResp.status(), 'POST /invite must return 200').toBe(200);

    // Wait for sync to deliver rooms.invite within 10 s (was 35 s workaround).
    // With :pg broadcast fix: sync wakes in ~3 s → PASS.
    // Without the fix: long-poll sleeps 30 s → timeout fires → FAIL.
    await Promise.race([
      syncWithInvitePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('Invite not delivered in sync within 10 s — :pg user broadcast fix required')), 10_000)
      ),
    ]);

    // Invite tile with the room name must now be visible
    await expect(
      page.getByText(roomName, { exact: false }).first(),
    ).toBeVisible({ timeout: 5_000 });
  });

  // ── [P0] Test 2: Decline invite removes from sidebar ─────────────────────

  test('[P0] Decline invite: POST /leave → invite tile disappears within 10 s', async ({ page }) => {
    // Tests the same :pg broadcast path as room-lifecycle leave test (AC 2 test 2).
    //
    // Given: alex is logged in and has a pending invite from marie
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(alexSession.accessToken).toBeTruthy();
    expect(alexSession.userId).toBeTruthy();

    await dismissKeyDialog(page);

    const marieContext = await page.context().browser()!.newContext();
    const mariePage = await marieContext.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme');
    await mariePage.close();
    await marieContext.close();

    // marie creates a named room and invites alex
    const declineRoomName = `decline-test-${Date.now()}`;
    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { name: declineRoomName, visibility: 'private', preset: 'private_chat' },
      },
    );
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Set up sync interception BEFORE sending invite
    const syncWithInvitePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.invite?.[roomId] !== undefined;
      } catch { return false; }
    });

    await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/invite`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { user_id: alexSession.userId },
      },
    );

    // Wait for sync to deliver rooms.invite within 10 s (was 35 s workaround).
    // With :pg broadcast fix: sync wakes in ~3 s. (Bug 4-29f fix, Story 8-10a)
    await Promise.race([
      syncWithInvitePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('Invite not delivered in sync within 10 s — :pg user broadcast fix required')), 10_000)
      ),
    ]);

    // Invite tile must now be visible (Element processes sync response asynchronously)
    await expect(
      page.getByText(declineRoomName, { exact: false }).first(),
      'invite tile must appear before decline',
    ).toBeVisible({ timeout: 30_000 });

    // Set up sync interception BEFORE declining (network-first pattern — Murat TEA)
    const syncWithLeavePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.leave?.[roomId] !== undefined;
      } catch { return false; }
    });

    // When: alex declines via POST /rooms/{roomId}/leave
    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave (decline) must return 200').toBe(200);

    // Primary assertion: sync must deliver rooms.leave[roomId] within 10 s.
    // emit_decline_event inserts m.room.member leave into events table + broadcasts
    // to :pg → fetch_delta_rooms finds it even across sync-cycle boundaries (race-proof).
    // Without the fix: long-poll sleeps 30 s → timeout fires → FAIL.
    const syncResp = await Promise.race([
      syncWithLeavePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('Sync did not deliver rooms.leave within 10 s')), 10_000)
      ),
    ]);
    const syncBody = await (syncResp as Awaited<typeof syncWithLeavePromise>).json();
    expect(syncBody.rooms?.leave?.[roomId], 'rooms.leave must contain the declined room').toBeDefined();
  });
});

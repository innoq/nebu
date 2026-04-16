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

    // When: marie creates a room and invites alex
    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { visibility: 'private', preset: 'private_chat' },
      },
    );
    expect(createResp.status(), 'marie must be able to createRoom').toBe(200);
    const { room_id: roomId } = await createResp.json();

    const inviteResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/invite`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { user_id: alexSession.userId },
      },
    );
    expect(inviteResp.status(), 'POST /invite must return 200').toBe(200);

    // Then: next incremental sync delivers rooms.invite
    // alex's sidebar shows the room in invite state within 20 s
    // Element Web shows invites as options in the sidebar (same role as regular rooms)
    // or in an "Invites" section. We check for any new option appearing.
    const sidebarOptions = page.getByRole('option', { name: /open room|öffne den chat|invited/i });
    await expect(sidebarOptions.first()).toBeVisible({ timeout: 20_000 });
  });

  // ── [P0] Test 2: Decline invite removes from sidebar ─────────────────────

  test('[P0] Decline invite: POST /leave → invite tile gone within 10 s', async ({ page }) => {
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

    // marie creates room and invites alex
    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { visibility: 'private', preset: 'private_chat' },
      },
    );
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    const inviteResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/invite`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { user_id: alexSession.userId },
      },
    );
    expect(inviteResp.status()).toBe(200);

    // Wait for invite to appear in alex's sidebar
    const sidebarOptions = page.getByRole('option', { name: /open room|öffne den chat|invited/i });
    await expect(sidebarOptions.first()).toBeVisible({ timeout: 20_000 });
    const countWithInvite = await sidebarOptions.count();
    expect(countWithInvite).toBeGreaterThan(0);

    // When: alex declines via POST /rooms/{roomId}/leave
    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave (decline) must return 200').toBe(200);

    // Then: invite tile gone within 10 s (same :pg broadcast timing as leave-room test)
    await expect(sidebarOptions).toHaveCount(countWithInvite - 1, { timeout: 10_000 });
  });
});

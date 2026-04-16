/**
 * Messages E2E tests — basic send (UI) and receive (via sync).
 *
 * AC 4 — Story 4-29
 *
 * Tests:
 *   1. Send message appears in timeline: alex creates room, types message
 *      → appears in .mx_EventTile timeline immediately
 *   2. Bot message received via sync: marie sends message via API
 *      → appears in alex's .mx_EventTile timeline within 15 s
 *      (tests incremental sync delivery — the core socket wake-up path)
 *
 * Known selectors (from live Element Web inspection 2026-04-15):
 *   - Timeline events: .mx_EventTile (stable CSS class in Element Web)
 *   - Compose box: [contenteditable="true"][data-testid="message-composer-input"]
 *               OR .mx_SendMessageComposer [contenteditable="true"]
 *
 * Run: npx playwright test features/messages/messages.spec.ts
 */

import { test, expect } from '@playwright/test';
import { loginViaOidc, ELEMENT_URL } from '../../fixtures/oidc';
import { isElementReachable, isDexReachable, dismissKeyDialog } from '../../fixtures/helpers';

// ---------------------------------------------------------------------------
// Suite guard
// ---------------------------------------------------------------------------

test.describe('Messages — send and receive (AC 4, Story 4-29)', () => {
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

  // ── [P0] Test 1: Send message appears in own timeline ────────────────────

  test('[P0] Send message: alex types and sends → appears in .mx_EventTile timeline', async ({ page }) => {
    // Given: alex is logged in and in a room with compose box visible
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken).toBeTruthy();

    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: { visibility: 'private', preset: 'private_chat' },
      },
    );
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Navigate into the room
    await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
    await dismissKeyDialog(page);

    const composeBox = page
      .locator('[contenteditable="true"][data-testid="message-composer-input"]')
      .or(page.locator('.mx_SendMessageComposer [contenteditable="true"]'))
      .first();

    await expect(composeBox).toBeVisible({ timeout: 15_000 });

    // When: alex types and sends a message
    const messageText = 'hello from playwright e2e send test';
    await composeBox.fill(messageText);
    await composeBox.press('Enter');

    // Then: message appears in .mx_EventTile timeline immediately
    await expect(
      page.locator('.mx_EventTile').getByText(messageText),
    ).toBeVisible({ timeout: 15_000 });
  });

  // ── [P0] Test 2: Bot message received via sync ───────────────────────────

  test('[P0] Bot message via sync: marie sends API message → appears in alex timeline within 15 s', async ({ page }) => {
    // This tests incremental sync delivery — verifies that the Core event dispatcher
    // wakes up the sync long-poll when a new message event arrives.
    //
    // Given: alex is logged in and in a room with marie
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(alexSession.accessToken).toBeTruthy();

    // marie gets a token for API bot operations
    const marieContext = await page.context().browser()!.newContext();
    const mariePage = await marieContext.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme');
    await mariePage.close();
    await marieContext.close();

    expect(marieSession.accessToken, 'marie must be logged in for API bot').toBeTruthy();

    // alex creates a room and invites marie
    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {
          visibility: 'private',
          preset: 'private_chat',
          invite: [marieSession.userId],
        },
      },
    );
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // marie joins the room
    const joinResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/join/${encodeURIComponent(roomId)}`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(joinResp.status()).toBe(200);

    // alex navigates to the room
    await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
    await dismissKeyDialog(page);

    await expect(
      page.locator('.mx_RoomView_timeline, [class*="timeline"]').first()
        .or(page.locator('.mx_EventTile').first()),
    ).toBeVisible({ timeout: 20_000 });

    // When: marie sends a message via Matrix API (incremental sync delivery path)
    const botMessage = 'hello from marie via API sync delivery test';
    const txnId = `txn-msg-${Date.now()}`;
    const sendResp = await page.request.put(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/${txnId}`,
      {
        headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { msgtype: 'm.text', body: botMessage },
      },
    );
    expect(sendResp.status(), 'marie API send must succeed').toBe(200);

    // Then: message appears in alex's .mx_EventTile timeline within 15 s
    // (tests incremental sync: :pg broadcast wakes up long-poll → sync returns new event)
    await expect(
      page.locator('.mx_EventTile').getByText(botMessage),
    ).toBeVisible({ timeout: 15_000 });
  });
});

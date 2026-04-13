/**
 * FluffyChat Web E2E Compatibility Tests (Story 4-24)
 *
 * Drives a real FluffyChat Flutter-web instance through SSO login,
 * room creation, and message send+receive via Playwright.
 *
 * What this covers (AC 5):
 *   - SSO Login: FluffyChat loads → SSO button → Dex form → loginToken → room list visible
 *   - Create Room: click "New Room" → enter name e2e-test-room → room appears in sidebar
 *   - Send & Receive: type "hello from playwright" → send → message appears in timeline
 *
 * Prerequisites:
 *   - `docker compose --profile e2e up -d --wait` (starts fluffychat service on port 7070)
 *   - "127.0.0.1 dex" in /etc/hosts (required for browser-level SSO redirect via Dex)
 *   - The full stack (gateway, core, postgres, dex, fluffychat) is healthy
 *
 * Tests skip automatically when http://localhost:7070 is unreachable, so that
 * running `make test-e2e` (without --profile e2e) does not fail CI.
 *
 * Run explicitly via:
 *   make test-e2e-fluffychat
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const FLUFFYCHAT_URL = process.env.NEBU_FLUFFYCHAT_URL ?? 'http://localhost:7070';
const DEX_AUTH_URL_PATTERN = /dex.*\/auth|localhost:5556.*\/dex\/auth/;
const ROOM_NAME = 'e2e-test-room';
const MESSAGE_TEXT = 'hello from playwright';

// ---------------------------------------------------------------------------
// Skip guard — checks if FluffyChat is reachable before running any tests
// ---------------------------------------------------------------------------

async function isFluffyChatReachable(): Promise<boolean> {
  try {
    const { default: http } = await import('http');
    return await new Promise<boolean>((resolve) => {
      const req = http.get(FLUFFYCHAT_URL, { timeout: 3000 }, (res) => {
        resolve(res.statusCode !== undefined && res.statusCode < 500);
        res.resume();
      });
      req.on('error', () => resolve(false));
      req.on('timeout', () => { req.destroy(); resolve(false); });
    });
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// SSO Login helper — shared across tests via storageState
// ---------------------------------------------------------------------------

async function performSsoLogin(page: Page): Promise<void> {
  await page.goto(FLUFFYCHAT_URL);

  // Flutter web apps need extra time on first load (WASM + asset bundle).
  // Use 'domcontentloaded' instead of 'networkidle' — Flutter's /sync long-polling
  // keeps the network perpetually active, so 'networkidle' would always time out.
  await page.waitForLoadState('domcontentloaded', { timeout: 60_000 });

  // FluffyChat renders an SSO button (text varies by locale / version):
  // "Sign in with SSO", "SSO", "Single Sign-On", or similar
  await page
    .getByText(/sign in with sso|log in with sso|sso login|single sign.on/i)
    .first()
    .click({ timeout: 30_000 });

  // Gateway redirects browser to Dex (hostname: dex, port-mapped to 5556)
  await page.waitForURL(DEX_AUTH_URL_PATTERN, { timeout: 30_000 });

  // Dex login form
  await page.locator('input[name="login"], input[type="email"]').fill('alex@example.com');
  await page.locator('input[name="password"], input[type="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();

  // Gateway exchanges code and redirects to FluffyChat auth.html with loginToken
  await page.waitForURL(/auth\.html/, { timeout: 30_000 });

  // FluffyChat JavaScript reads loginToken → POST /login → loads room list
  // Wait until auth.html processing completes and redirects to main app UI
  await page.waitForURL((url) => !url.href.includes('auth.html'), { timeout: 30_000 });

  // Room list (or "no rooms" state) must be visible — confirms successful login
  // FluffyChat shows the room list container regardless of whether rooms exist
  await expect(
    page.getByText(/no rooms|welcome|new room|start a conversation|e2e-test-room/i).first()
  ).toBeVisible({ timeout: 30_000 });
}

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

test.describe('FluffyChat Web E2E Compatibility', () => {
  // Increase timeout for all tests in this suite — Flutter web renders slowly
  test.setTimeout(120_000);

  // beforeAll skip guard: if FluffyChat is not reachable, skip all tests in this suite.
  // test.skip() inside beforeAll() skips all tests in the describe block — this is the
  // documented Playwright pattern for suite-level conditional skipping.
  test.beforeAll(async () => {
    const reachable = await isFluffyChatReachable();
    test.skip(
      !reachable,
      `FluffyChat at ${FLUFFYCHAT_URL} is unreachable. ` +
      'To run: docker compose --profile e2e up -d --wait'
    );
  });

  // ---------------------------------------------------------------------------
  // AC 5 — Test 1: SSO Login Happy Path
  // ---------------------------------------------------------------------------

  test('SSO login: FluffyChat loads → Dex form → loginToken → room list visible', async ({
    page,
    request,
  }) => {
    // Skip guard: FluffyChat service not running
    const fluffychatUp = await request
      .get(FLUFFYCHAT_URL)
      .catch(() => null);
    test.skip(
      !fluffychatUp || !fluffychatUp.ok(),
      `FluffyChat unreachable at ${FLUFFYCHAT_URL} — run: docker compose --profile e2e up -d --wait`
    );

    // Skip guard: Dex not resolvable from the host (use localhost — Node.js runs on the host, not inside Docker)
    const dexUp = await request
      .get('http://localhost:5556/dex/.well-known/openid-configuration')
      .catch(() => null);
    test.skip(
      !dexUp || !dexUp.ok(),
      'Dex unreachable at localhost:5556 — add "127.0.0.1 dex" to /etc/hosts and ensure stack is running'
    );

    // Navigate to FluffyChat
    await page.goto(FLUFFYCHAT_URL);
    await page.waitForLoadState('domcontentloaded', { timeout: 60_000 });

    // FluffyChat must render an SSO login button (AC 5 — SSO Login scenario)
    // This will FAIL until the FluffyChat service is running and the DOM is inspected
    const ssoButton = page
      .getByText(/sign in with sso|log in with sso|sso login|single sign.on/i)
      .first();
    await expect(ssoButton).toBeVisible({ timeout: 30_000 });

    // Click SSO button → gateway /_matrix/client/v3/login/sso/redirect → Dex
    await ssoButton.click();
    await page.waitForURL(DEX_AUTH_URL_PATTERN, { timeout: 30_000 });

    // Dex login form renders (at localhost:5556 — port-mapped from dex:5556)
    await expect(page.locator('input[name="login"], input[type="email"]')).toBeVisible({
      timeout: 15_000,
    });
    await page.locator('input[name="login"], input[type="email"]').fill('alex@example.com');
    await page.locator('input[name="password"], input[type="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // Gateway exchanges code → 302 → http://localhost:7070/auth.html?loginToken=...
    await page.waitForURL(/auth\.html/, { timeout: 30_000 });

    // The URL must contain loginToken query parameter
    expect(page.url()).toMatch(/loginToken=/);

    // FluffyChat JS reads loginToken → POST /_matrix/client/v3/login → room list
    await page.waitForURL((url) => !url.href.includes('auth.html'), { timeout: 30_000 });

    // Room list container must be visible — no error toast, no "could not connect" banner
    // The exact selector depends on actual FluffyChat DOM — using text-based fallback
    const roomListIndicator = page.getByText(
      /no rooms|welcome|new room|start a conversation|direct messages|chats/i
    ).first();
    await expect(roomListIndicator).toBeVisible({ timeout: 30_000 });

    // Assert no error/connection-failure banners
    const errorBanner = page.getByText(/could not connect|connection error|server unreachable/i);
    await expect(errorBanner).not.toBeVisible();
  });

  // ---------------------------------------------------------------------------
  // AC 5 — Test 2: Create Room
  // ---------------------------------------------------------------------------

  test('Create Room: after SSO login → create e2e-test-room → room appears in sidebar', async ({
    page,
    request,
  }) => {
    // Skip guard
    const fluffychatUp = await request.get(FLUFFYCHAT_URL).catch(() => null);
    test.skip(
      !fluffychatUp || !fluffychatUp.ok(),
      `FluffyChat unreachable at ${FLUFFYCHAT_URL} — run: docker compose --profile e2e up -d --wait`
    );

    const dexUp = await request
      .get('http://localhost:5556/dex/.well-known/openid-configuration')
      .catch(() => null);
    test.skip(
      !dexUp || !dexUp.ok(),
      'Dex unreachable at localhost:5556 — add "127.0.0.1 dex" to /etc/hosts'
    );

    // Perform SSO login to reach the room list
    await performSsoLogin(page);

    // Click "New Room" / create room button
    // FluffyChat uses a FAB or menu item — exact selector requires DOM inspection
    // Using text + aria patterns as primary strategy (Flutter HTML renderer)
    const createRoomButton = page
      .getByRole('button', { name: /new room|create room|add room/i })
      .or(page.getByLabel(/new room|create room|add room/i))
      .or(page.getByText(/new room|create group/i).first())
      .first();

    await expect(createRoomButton).toBeVisible({ timeout: 30_000 });
    await createRoomButton.click();

    // Room creation dialog / form — fill in the room name
    const roomNameInput = page
      .getByRole('textbox', { name: /room name|name/i })
      .or(page.locator('input[type="text"]').first())
      .first();

    await expect(roomNameInput).toBeVisible({ timeout: 15_000 });
    await roomNameInput.fill(ROOM_NAME);

    // Confirm / submit room creation — prefer submit-type button within a dialog/form
    // over positional .last() which is fragile if dialog DOM changes.
    const confirmButton = page
      .locator('dialog button[type="submit"], [role="dialog"] button[type="submit"]')
      .or(page.getByRole('button', { name: /create|ok|confirm|done/i }).first());
    await confirmButton.first().click();

    // The room name must appear in the room list sidebar
    await expect(page.getByText(ROOM_NAME)).toBeVisible({ timeout: 30_000 });
  });

  // ---------------------------------------------------------------------------
  // AC 5 — Test 3: Send and Receive Message
  // ---------------------------------------------------------------------------

  test('Send & Receive: in e2e-test-room → type message → appears in timeline', async ({
    page,
    request,
  }) => {
    // Skip guard
    const fluffychatUp = await request.get(FLUFFYCHAT_URL).catch(() => null);
    test.skip(
      !fluffychatUp || !fluffychatUp.ok(),
      `FluffyChat unreachable at ${FLUFFYCHAT_URL} — run: docker compose --profile e2e up -d --wait`
    );

    const dexUp = await request
      .get('http://localhost:5556/dex/.well-known/openid-configuration')
      .catch(() => null);
    test.skip(
      !dexUp || !dexUp.ok(),
      'Dex unreachable at localhost:5556 — add "127.0.0.1 dex" to /etc/hosts'
    );

    // Login and navigate to the room list
    await performSsoLogin(page);

    // Create the test room first (or navigate into it if it already exists)
    // Check if e2e-test-room already exists in the sidebar
    const existingRoom = page.getByText(ROOM_NAME).first();
    const roomExists = await existingRoom.isVisible({ timeout: 5_000 }).catch(() => false);

    if (!roomExists) {
      // Create the room as in Test 2
      const createRoomButton = page
        .getByRole('button', { name: /new room|create room|add room/i })
        .or(page.getByLabel(/new room|create room|add room/i))
        .or(page.getByText(/new room|create group/i).first())
        .first();

      await expect(createRoomButton).toBeVisible({ timeout: 30_000 });
      await createRoomButton.click();

      const roomNameInput = page
        .getByRole('textbox', { name: /room name|name/i })
        .or(page.locator('input[type="text"]').first())
        .first();

      await expect(roomNameInput).toBeVisible({ timeout: 15_000 });
      await roomNameInput.fill(ROOM_NAME);

      const confirmButton = page
        .locator('dialog button[type="submit"], [role="dialog"] button[type="submit"]')
        .or(page.getByRole('button', { name: /create|ok|confirm|done/i }).first());
      await confirmButton.first().click();

      await expect(page.getByText(ROOM_NAME)).toBeVisible({ timeout: 30_000 });
    }

    // Navigate into the room by clicking its entry in the sidebar
    await page.getByText(ROOM_NAME).first().click();

    // Wait for the message input to appear (confirms we are inside the room view)
    const messageInput = page
      .getByRole('textbox', { name: /message|write a message|type a message/i })
      .or(page.locator('textarea, [contenteditable="true"]').first())
      .first();

    await expect(messageInput).toBeVisible({ timeout: 30_000 });

    // Type the message — use keyboard input (type) instead of fill to trigger Flutter's
    // onChanged handler, then send deterministically via Enter key (FluffyChat default).
    await messageInput.fill(MESSAGE_TEXT);
    await messageInput.press('Enter');

    // Message must appear in the timeline
    await expect(page.getByText(MESSAGE_TEXT)).toBeVisible({ timeout: 30_000 });
  });
});

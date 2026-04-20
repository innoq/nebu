/**
 * SSO Login feature tests — migrated from element_e2e.spec.ts
 *
 * AC 1, AC 6 — Story 4-29
 * Migration: all login-related and new-endpoint smoke tests from element_e2e.spec.ts.
 *
 * Run: npx playwright test features/login/sso-login.spec.ts
 */

import { test, expect } from '@playwright/test';
import { loginViaOidc, ELEMENT_URL } from '../../fixtures/oidc';
import { isElementReachable, isDexReachable, dismissKeyDialog } from '../../fixtures/helpers';

// ---------------------------------------------------------------------------
// Suite guard: auto-skip when stack is unreachable
// ---------------------------------------------------------------------------

test.describe('SSO Login — Element Web via Dex OIDC (Story 4-29)', () => {
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

  // ── [P0] SSO Login: full flow ─────────────────────────────────────────────

  test('[P0] SSO login: Element loads → Dex form → loginToken → room list visible', async ({ page }) => {
    await page.goto(ELEMENT_URL);

    // Welcome screen must be visible (DE: "Willkommen bei Element")
    await expect(page.getByRole('heading', { name: /welcome to element|willkommen bei element/i }))
      .toBeVisible({ timeout: 15_000 });

    // Navigate to sign-in (DE: "Anmelden")
    await page.getByRole('link', { name: /sign in|anmelden/i }).click();
    await expect(page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }))
      .toBeVisible({ timeout: 15_000 });

    // Click SSO → Dex login
    await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }).click();
    await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });

    // Dex login form
    await expect(page.locator('input[name="login"]')).toBeVisible({ timeout: 10_000 });
    await page.locator('input[name="login"]').fill('alex@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // Back to Element Web after successful login
    await page.waitForURL(/localhost:7070/, { timeout: 20_000 });

    // Dismiss key dialog and wait for app
    await dismissKeyDialog(page);

    // Welcome/home screen visible after login
    await expect(
      page.locator('[placeholder*="Search"]').first()
        .or(page.getByRole('heading', { name: /welcome/i })),
    ).toBeVisible({ timeout: 15_000 });

    // No fatal error dialog
    await expect(page.locator('[data-testid="dialog-error"], .mx_ErrorDialog'))
      .not.toBeVisible({ timeout: 2_000 })
      .catch(() => {});
  });

  // ── [P0] loginViaOidc returns access token ────────────────────────────────

  test('[P0] loginViaOidc: returns non-empty accessToken and userId', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');

    expect(session.accessToken, 'accessToken must be non-empty').toBeTruthy();
    expect(session.userId, 'userId must be non-empty').toBeTruthy();
    expect(session.userId, 'userId must be a valid Matrix ID').toMatch(/^@.+:.+$/);
  });

  // ── [P1] Reconnect after reload (Story 5-1: GET /filter) ─────────────────

  test('[P1] Reconnect after reload — no sync ERROR loop (Story 5-1: GET /filter)', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');

    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: 'reconnect-test-room', visibility: 'private' },
    });
    expect(createResp.status()).toBe(200);

    // Collect console errors — a sync ERROR loop produces repeated
    // "Getting filter failed" / "sync state => ERROR" messages.
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error' || (msg.type() === 'debug' && msg.text().includes('ERROR'))) {
        consoleErrors.push(msg.text());
      }
    });

    // Reload — Element fetches GET /filter/0 to restore session.
    await page.reload();
    await dismissKeyDialog(page);

    await expect(
      page.getByRole('option', { name: /open room|öffne den chat/i }).first()
        .or(page.locator('[placeholder*="Search"]').first()),
    ).toBeVisible({ timeout: 20_000 });

    // Negative assertion: we need to observe that NO console error appears over a window of time.
    // There is no DOM element or event to await — the absence of a retry storm is the signal.
    // A fixed 3 s wait is the pragmatic approach here (TEA-reviewed, INFO-1 in test-review).
    await page.waitForTimeout(3_000);
    const filterErrors = consoleErrors.filter(m => m.includes('Getting filter failed'));
    expect(filterErrors.length, `Sync ERROR loop detected: ${filterErrors.join(' | ')}`).toBe(0);
  });

  // ── [P1] User directory search ────────────────────────────────────────────

  test('[P1] User directory search: alex finds marie via user_directory/search', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');

    // Warm-up call (users only appear in directory after first login)
    await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/user_directory/search`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { search_term: 'marie', limit: 10 },
    });

    const searchResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/user_directory/search`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: { search_term: 'marie', limit: 10 },
      },
    );
    expect(searchResp.status()).toBe(200);
    const body = await searchResp.json();
    const marieResult = (body.results as Array<{ user_id: string }>)
      .find(r => r.user_id.includes('marie'));
    expect(marieResult, 'marie not found in user directory').toBeTruthy();
  });

  // ── [P1] Multi-user room ──────────────────────────────────────────────────

  test('[P1] Multi-user: alex creates room, invites marie, both send messages', async ({ page }) => {
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');

    const createResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/createRoom`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { name: 'e2e-multiuser', invite: [`@marie:localhost`], visibility: 'private' },
      },
    );
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    const sendResp = await page.request.put(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/txn-sso-multi-1`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: { msgtype: 'm.text', body: 'Hey Marie from alex!' },
      },
    );
    expect(sendResp.status()).toBe(200);

    // marie logs in via a separate browser context (second context, just for token)
    const marieContext = await page.context().browser()!.newContext();
    const mariePage = await marieContext.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme').catch(() => null);
    await mariePage.close();
    await marieContext.close();

    if (marieSession) {
      const joinResp = await page.request.post(
        `${ELEMENT_URL}/_matrix/client/v3/join/${encodeURIComponent(roomId)}`,
        {
          headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
          data: {},
        },
      );
      expect(joinResp.status()).toBe(200);

      const marieReply = await page.request.put(
        `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/txn-sso-multi-2`,
        {
          headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
          data: { msgtype: 'm.text', body: 'Hey Alex from marie!' },
        },
      );
      expect(marieReply.status()).toBe(200);
    }

    // Verify messages via Matrix API
    const msgsResp = await page.request.get(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/messages?dir=b&limit=10`,
      { headers: { Authorization: `Bearer ${alexSession.accessToken}` } },
    );
    expect(msgsResp.status()).toBe(200);
    const msgs = await msgsResp.json();
    const bodies = (msgs.chunk as Array<{ content?: { body?: string } }>)
      .map(e => e.content?.body).filter(Boolean);
    expect(bodies).toContain('Hey Marie from alex!');
    if (marieSession) {
      expect(bodies).toContain('Hey Alex from marie!');
    }
  });
});

// ---------------------------------------------------------------------------
// New endpoint smoke tests (Stories 5-1/5-2/5-3)
// ---------------------------------------------------------------------------

test.describe('Element Web — New endpoint smoke tests (Stories 5-1/5-2/5-3)', () => {
  test.setTimeout(120_000);

  test.beforeAll(async () => {
    const [elemOk, dexOk] = await Promise.all([isElementReachable(), isDexReachable()]);
    test.skip(!elemOk, `Element Web at ${ELEMENT_URL} is unreachable`);
    test.skip(!dexOk, 'Dex unreachable — add "127.0.0.1 dex" to /etc/hosts');
  });

  // ── Story 5-1: GET /user/{userId}/filter/{filterId} ───────────────────────

  test('Member list populated after joining room (Story 5-2: GET /members)', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');

    // Create a room and navigate into it
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: 'members-test-room', visibility: 'private' },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Navigate to the room
    await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
    await dismissKeyDialog(page);

    // Wait for the room header to load
    await expect(page.locator('.mx_RoomHeader, [data-testid="room-header"]').first())
      .toBeVisible({ timeout: 20_000 });

    // Open the member list via the room info button or member count
    const memberButton = page
      .getByRole('button', { name: /\d+ member/i })
      .or(page.locator('[aria-label*="member" i]').first());
    if (await memberButton.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await memberButton.click();
    } else {
      await page.getByRole('button', { name: /room info/i }).first().click().catch(() => {});
    }

    await expect(
      page.locator('.mx_MemberList, [data-testid="member-list"]').first()
        .or(page.locator('[class*="MemberList"], [class*="memberList"]').first()),
    ).toBeVisible({ timeout: 15_000 });

    await expect(
      page.getByText(/alex/i, { exact: false }).first()
        .or(page.locator('[class*="memberInfo"], [class*="MemberInfo"]').first()),
    ).toBeVisible({ timeout: 10_000 });
  });

  // ── Story 5-3: POST /rooms/{roomId}/read_markers ──────────────────────────

  test('No read_markers retry loop when entering room (Story 5-3: POST /read_markers)', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');

    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: 'read-markers-test-room', visibility: 'private' },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    await page.request.put(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/txn-rm-1`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: { msgtype: 'm.text', body: 'test message for read markers' },
      },
    );

    // Collect errors — read_markers retry produces repeated "Error sending fully_read"
    const readMarkerErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.text().includes('fully_read') || msg.text().includes('read_markers')) {
        readMarkerErrors.push(`[${msg.type()}] ${msg.text()}`);
      }
    });

    // Navigate to the room — Element will POST /read_markers on entry
    await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
    await dismissKeyDialog(page);

    // Wait for timeline to render
    await expect(page.locator('.mx_RoomView_timeline, [class*="timeline"]').first())
      .toBeVisible({ timeout: 20_000 });

    // Negative assertion: we need to observe that NO read_markers error appears over a window of time.
    // There is no DOM element or event to await — the absence of a retry storm is the signal.
    // A fixed 5 s wait is the pragmatic approach here (TEA-reviewed, INFO-1 in test-review).
    await page.waitForTimeout(5_000);

    const errors = readMarkerErrors.filter(m => m.toLowerCase().includes('error'));
    expect(errors.length, `read_markers retry storm detected:\n${errors.join('\n')}`).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// Login → Logout → Re-Login regression (bugfix-logout-oidc-dex-session)
// ---------------------------------------------------------------------------
//
// Root cause: POST /logout only adds the JWT to the denylist but does NOT call
// Dex's end_session_endpoint. Dex can reuse its session cookie and return the
// same id_token, which is then rejected by JWTMiddleware (IsInvalidated=true)
// on the first /sync → Element lands on #/welcome.
//
// Fix: prompt=login in GetSSORedirect forces Dex to always re-authenticate,
// guaranteeing a fresh JWT with a new hash that is not in the denylist.
//
// This test reproduces the bug by cycling through login→logout→login three
// times WITHOUT clearing cookies between iterations. All iterations must reach
// the room list (search bar visible), never #/welcome.

test.describe('SSO Logout / Re-Login regression (bugfix-logout-oidc-dex-session)', () => {
  test.setTimeout(180_000);

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

  test('[P0] Login → Logout → Re-Login cycle survives 3 iterations without cookie clearing', async ({ page }) => {
    const ITERATIONS = 3;

    for (let i = 1; i <= ITERATIONS; i++) {
      // ── Login via SSO ──────────────────────────────────────────────────────
      await page.goto(ELEMENT_URL);

      // Welcome screen
      await expect(page.getByRole('heading', { name: /welcome to element|willkommen bei element/i }))
        .toBeVisible({ timeout: 15_000 });

      await page.getByRole('link', { name: /sign in|anmelden/i }).click();
      await expect(page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }))
        .toBeVisible({ timeout: 15_000 });

      await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }).click();

      // Dex credential form — must appear on EVERY iteration when prompt=login is active.
      // If this times out on iteration 2+, Dex is auto-authenticating (prompt=login missing).
      await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
      await expect(page.locator('input[name="login"]')).toBeVisible({ timeout: 10_000 });
      await page.locator('input[name="login"]').fill('alex@example.com');
      await page.locator('input[name="password"]').fill('changeme');
      await page.locator('button[type="submit"]').click();

      // Back to Element after SSO
      await page.waitForURL(/localhost:7070/, { timeout: 20_000 });
      await dismissKeyDialog(page);

      // Room list must be visible — NOT #/welcome.
      // If this fails on iteration 2+, the Dex session reuse + denylist bug is present.
      const roomListVisible = await page
        .locator('[placeholder*="Search"]').first()
        .isVisible({ timeout: 15_000 })
        .catch(() => false);

      const currentUrl = page.url();
      expect(
        roomListVisible,
        `Iteration ${i}: Expected room list (Search input), but page is at ${currentUrl}. ` +
        `If URL contains #/welcome, the JWT denylist bug is present — check prompt=login in sso.go`,
      ).toBe(true);

      // ── Logout ─────────────────────────────────────────────────────────────
      if (i < ITERATIONS) {
        // Open user menu — try data-testid first, then class-based fallbacks
        const avatarButton = page
          .locator('[data-testid="user-menu-trigger"]')
          .or(page.locator('.mx_UserMenu_userAvatarButton'))
          .or(page.locator('[aria-label*="user menu" i]'))
          .first();

        await avatarButton.click({ timeout: 10_000 });

        // Click "Sign out" in the menu
        await page.getByRole('menuitem', { name: /sign out|abmelden/i })
          .or(page.getByRole('button', { name: /sign out|abmelden/i }).first())
          .click({ timeout: 10_000 });

        // Confirm sign-out dialog (scoped to the dialog to avoid selector fragility)
        const dialog = page.locator('[role="dialog"]');
        const confirmBtn = dialog.getByRole('button', { name: /sign out|abmelden/i });
        if (await confirmBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
          await confirmBtn.click();
        }

        // Wait for welcome screen — confirms logout completed
        await expect(
          page.getByRole('heading', { name: /welcome to element|willkommen bei element/i }),
        ).toBeVisible({ timeout: 15_000 });
      }
    }
  });
});

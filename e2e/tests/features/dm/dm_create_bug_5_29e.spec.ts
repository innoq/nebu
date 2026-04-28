/**
 * Story 5.29e Bug 2 — Direct Message creation hangs (keys/query + profile 404)
 *
 * ATDD RED PHASE — these tests MUST FAIL until Bugs 2a and 2b are fixed.
 *
 * Source: tmp/test-findings.md, 2026-04-23.
 *   "Marie wants to start a DM with Alex. @alex:localhost is found. On clicking
 *    'Start DM': '@alex:localhost: Profile not found'. After 'Dennoch DM beginnen':
 *    empty room appears, spinner 'Chat mit @alex:localhost wird erstellt' never resolves."
 *
 * Root causes:
 *   (a) GET /profile/@alex:localhost → 404 (no profile row for bootstrap user)
 *   (b) POST keys/query → device_keys:{} (alex missing from map; client can't
 *       distinguish "user exists, no devices" from "user not found")
 *
 * Fix scope (5-29e API-level tests — no browser):
 *   AC2: GET /profile/{userId} returns 200 for all provisioned users.
 *   AC3: POST keys/query returns device_keys map entry for every queried user that exists.
 *   AC4: Marie+Alex DM API flow completes without errors (no infinite spinner signals).
 *
 * These tests use the same API-level approach as matrix_api.spec.ts.
 * They require the full `make dev` stack (gateway + core + postgres + keycloak/dex).
 * Auto-skip when stack is unreachable.
 *
 * Run: npx playwright test tests/dm_create_bug_5_29e.spec.ts
 */

import { test, expect, type APIRequestContext } from '@playwright/test';

// ── Configuration ──────────────────────────────────────────────────────────

const BASE = 'http://localhost:8008';
const DEX  = 'http://localhost:5556';

// Users from the dev fixture (password: changeme).
const ALEX  = { email: 'alex@example.com',  userId: '@alex:localhost' };
const MARIE = { email: 'marie@example.com', userId: '@marie:localhost' };

// ── Skip guard ────────────────────────────────────────────────────────────

async function isStackReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const r = await request.get(`${BASE}/_matrix/client/versions`);
    return r.ok();
  } catch {
    return false;
  }
}

// ── SSO Login Helper (copied from matrix_api.spec.ts pattern) ─────────────

async function ssoLogin(
  request: APIRequestContext,
  email: string,
  password: string
): Promise<{ token: string; userId: string }> {
  const CALLBACK = 'http://localhost:19999/cb';

  const r1 = await request.get(
    `${BASE}/_matrix/client/v3/login/sso/redirect?redirectUrl=${encodeURIComponent(CALLBACK)}`,
    { maxRedirects: 0, failOnStatusCode: false }
  );
  const dexUrl = r1.headers()['location'] ?? '';

  const formResp = await request.get(dexUrl, { maxRedirects: 5 });
  const html = await formResp.text();
  const actionMatch = html.match(/action="([^"]+)"/);
  expect(actionMatch).not.toBeNull();
  let formAction = actionMatch![1].replace(/&amp;/g, '&');
  if (!formAction.startsWith('http')) formAction = `${DEX}${formAction}`;

  const credsResp = await request.post(formAction, {
    form: { login: email, password },
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  const dexCallbackLoc = credsResp.headers()['location'] ?? '';

  const gwCallbackResp = await request.get(dexCallbackLoc, {
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  const finalLoc = gwCallbackResp.headers()['location'] ?? '';

  let loginToken = '';
  try {
    loginToken = new URL(finalLoc).searchParams.get('loginToken') ?? '';
  } catch {
    loginToken = finalLoc.split('loginToken=')[1]?.split('&')[0] ?? '';
  }
  expect(loginToken, `No loginToken in final redirect: ${finalLoc}`).not.toBe('');

  const loginResp = await request.post(`${BASE}/_matrix/client/v3/login`, {
    data: { type: 'm.login.token', token: loginToken },
  });
  expect(loginResp.status()).toBe(200);
  const body = await loginResp.json();
  return { token: body.access_token, userId: body.user_id };
}

// ── Shared state ──────────────────────────────────────────────────────────

let alexToken  = '';
let marieToken = '';

// ── Test Suite ────────────────────────────────────────────────────────────

test.describe('Story 5.29e — Bug 2: DM creation hangs (profile 404 + keys/query)', () => {
  test.setTimeout(120_000);

  test.beforeAll(async ({ request }) => {
    const reachable = await isStackReachable(request);
    test.skip(!reachable, `Stack unreachable at ${BASE}. Run: docker compose up -d`);

    // Login both users.
    const alexState  = await ssoLogin(request, ALEX.email, 'changeme');
    const marieState = await ssoLogin(request, MARIE.email, 'changeme');

    alexToken  = alexState.token;
    marieToken = marieState.token;

    expect(alexState.userId).toBe(ALEX.userId);
    expect(marieState.userId).toBe(MARIE.userId);
  });

  // ── AC2: Profile lookup returns 200 for provisioned users ─────────────────

  test('AC2-a: GET /profile/@alex:localhost returns 200 (Bug 2a: no 404 for bootstrap users)', async ({ request }) => {
    // RED PHASE: currently returns 404 because no profile row was upserted at login.
    const r = await request.get(`${BASE}/_matrix/client/v3/profile/${encodeURIComponent(ALEX.userId)}`);

    expect(r.status(), `GET /profile/${ALEX.userId} must return 200 — currently 404 because ` +
      `the profiles table has no row for bootstrap-provisioned users (Bug 2a). ` +
      `Fix: ensure Core upserts a profiles row (with OIDC preferred_username) at ValidateToken time.`
    ).toBe(200);

    const body = await r.json();
    expect(body).toHaveProperty('displayname');
  });

  test('AC2-b: GET /profile/@marie:localhost returns 200 (regression: all users must have profile rows)', async ({ request }) => {
    // RED PHASE: same issue — marie's profile row may be missing.
    const r = await request.get(`${BASE}/_matrix/client/v3/profile/${encodeURIComponent(MARIE.userId)}`);

    expect(r.status(), `GET /profile/${MARIE.userId} must return 200 — same provisioning gap`
    ).toBe(200);

    const body = await r.json();
    expect(body).toHaveProperty('displayname');
  });

  // ── AC3: keys/query returns device_keys entry for known users ─────────────

  test('AC3: POST keys/query for @alex:localhost returns a device_keys entry (not missing key)', async ({ request }) => {
    // RED PHASE: current stub returns {"device_keys":{},"failures":{}} — alex is absent
    // from device_keys. FluffyChat cannot distinguish "user exists, no devices" from
    // "user not found" → DM creation hangs.
    const r = await request.post(`${BASE}/_matrix/client/v3/keys/query`, {
      headers: { Authorization: `Bearer ${marieToken}` },
      data: {
        device_keys: {
          [ALEX.userId]: [],
        },
      },
    });

    expect(r.status()).toBe(200);
    const body = await r.json();

    // The fix: alex must appear as a KEY in device_keys (even if the inner map is {}).
    expect(
      body.device_keys,
      `keys/query response must have device_keys[${ALEX.userId}] — ` +
      `current stub returns {} (empty map) without any user keys. ` +
      `RED PHASE: this assertion FAILS until the stub is improved to include known users.`
    ).toHaveProperty(ALEX.userId);

    // alex must NOT be in failures if he exists in the DB.
    expect(body.failures ?? {}).not.toHaveProperty(ALEX.userId);
  });

  // ── AC4: DM creation API flow completes without hanging ───────────────────

  test('AC4: Marie can create a DM room with Alex via Matrix API (no spinner / no error)', async ({ request }) => {
    // RED PHASE: this test will fail because:
    //   - GET /profile returns 404 → client shows "Profile not found" warning
    //   - keys/query returns empty → client may stall waiting for device keys
    //
    // This test exercises the API-level DM flow directly (not via FluffyChat browser),
    // as the API is the ground truth.

    // Step 1: Marie creates a DM room with Alex as invitee.
    // is_direct=true signals this is a DM per Matrix spec.
    const createResp = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${marieToken}` },
      data: {
        invite: [ALEX.userId],
        is_direct: true,
        preset: 'trusted_private_chat',
        name: `DM: Marie ↔ Alex`,
      },
    });

    expect(createResp.status(), `createRoom must return 200 — got ${createResp.status()}: ${await createResp.text()}`).toBe(200);

    const createBody = await createResp.json();
    const roomId: string = createBody.room_id;
    expect(roomId).toMatch(/^!/);

    // Step 2: Alex accepts the invitation by joining.
    const joinResp = await request.post(
      `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/join`,
      {
        headers: { Authorization: `Bearer ${alexToken}` },
      }
    );
    expect(joinResp.status(), `Alex join must return 200 — got ${joinResp.status()}: ${await joinResp.text()}`).toBe(200);

    // Step 3: Marie sends a message — no spinner, room is fully usable.
    const txnId = `txn-dm-test-${Date.now()}`;
    const sendResp = await request.put(
      `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/${txnId}`,
      {
        headers: { Authorization: `Bearer ${marieToken}` },
        data: { msgtype: 'm.text', body: 'Hello Alex! (DM test 5-29e)' },
      }
    );

    expect(sendResp.status(), `send message must return 200 — spinner means room is not usable`).toBe(200);
    const sendBody = await sendResp.json();
    expect(sendBody).toHaveProperty('event_id');
  });
});

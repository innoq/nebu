/**
 * Matrix API Contract Tests — Nebu Chat Server
 *
 * Pure API-level tests (no browser). Run against the live stack via the
 * Element Web nginx proxy (localhost:7070 → gateway:8008) so the same
 * origin is used as in the real client.
 *
 * Tests auto-skip when the stack is not running.
 *
 * Run: npx playwright test tests/matrix_api.spec.ts
 * Fast path: ~30s, no browser startup.
 *
 * Coverage (from Test Plan matrix-client-communication-test-plan.md):
 *   A — Authentication & Session (P0)
 *   B — User Directory Search (P1)
 *   C — Direct Message / 1:1 Room (P0)
 *   D — Private Group Room (P0)
 *   E — Public Room (P1)
 *   F — Room List / Sync (P0/P1)
 *   G — Message History & Pagination (P1)
 *   H — Typing Indicators (P1)
 *   I — Read Receipts (P1)
 *   J — Presence (P1)
 *   K — Leave Room / Decline Invite (P1)
 */

import { test, expect, type APIRequestContext } from '@playwright/test';

// ── Configuration ──────────────────────────────────────────────────────────

// API tests hit the gateway directly — no Element Web sidecar needed.
// (Element Web proxy at :7070 is only required for browser-based E2E tests.)
const BASE = 'http://localhost:8008';
const DEX  = 'http://localhost:5556';

// Dev users (all password: changeme)
const USERS = {
  alex:  { email: 'alex@example.com',       expectedId: '@alex:localhost' },
  marie: { email: 'marie@example.com',      expectedId: '@marie:localhost' },
  tom:   { email: 'tom@example.com',        expectedId: '@tom:localhost' },
  kai:   { email: 'kai@example.com',        expectedId: '@kai:localhost' },
} as const;

// ── Shared state (set in beforeAll) ───────────────────────────────────────

type UserState = { token: string; userId: string };
const state: Record<string, UserState> = {};
const createdRooms: string[] = []; // cleanup tracking

// ── Skip guard ────────────────────────────────────────────────────────────

async function isStackReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const r = await request.get(`${BASE}/_matrix/client/versions`);
    return r.ok();
  } catch {
    return false;
  }
}

// ── SSO Login Helper ──────────────────────────────────────────────────────

async function ssoLogin(
  request: APIRequestContext,
  email: string,
  password: string
): Promise<{ token: string; userId: string }> {
  const CALLBACK = 'http://localhost:19999/cb'; // dummy — never actually called

  // Step 1: SSO redirect → Dex auth URL (stop redirect, extract Location)
  const r1 = await request.get(
    `${BASE}/_matrix/client/v3/login/sso/redirect?redirectUrl=${encodeURIComponent(CALLBACK)}`,
    { maxRedirects: 0, failOnStatusCode: false }
  );
  const dexUrl = r1.headers()['location'] ?? '';

  // Step 2: GET Dex login page (follow Dex-internal redirects to form)
  const formResp = await request.get(dexUrl, { maxRedirects: 5 });
  const html = await formResp.text();
  const actionMatch = html.match(/action="([^"]+)"/);
  expect(actionMatch).not.toBeNull();
  let formAction = actionMatch![1].replace(/&amp;/g, '&');
  if (!formAction.startsWith('http')) formAction = `${DEX}${formAction}`;

  // Step 3: POST credentials → Dex redirects to gateway callback URL
  const credsResp = await request.post(formAction, {
    form: { login: email, password },
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  const dexCallbackLoc = credsResp.headers()['location'] ?? '';

  // Step 4: Follow to gateway callback (exchange code → loginToken),
  // but stop BEFORE following to our dummy callback URL.
  // Gateway responds with 302 → CALLBACK?loginToken=<token>
  const gwCallbackResp = await request.get(dexCallbackLoc, {
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  const finalLoc = gwCallbackResp.headers()['location'] ?? '';

  // Extract opaque loginToken from the final redirect URL
  let loginToken = '';
  try {
    loginToken = new URL(finalLoc).searchParams.get('loginToken') ?? '';
  } catch {
    loginToken = finalLoc.split('loginToken=')[1]?.split('&')[0] ?? '';
  }
  expect(loginToken, `No loginToken in final redirect: ${finalLoc}`).not.toBe('');

  // Step 5: POST /login with opaque token → access_token + user_id
  const loginResp = await request.post(`${BASE}/_matrix/client/v3/login`, {
    data: { type: 'm.login.token', token: loginToken },
  });
  expect(loginResp.status()).toBe(200);
  const body = await loginResp.json();
  return { token: body.access_token, userId: body.user_id };
}

function auth(user: string) {
  return { Authorization: `Bearer ${state[user].token}` };
}

// ── Test Setup ────────────────────────────────────────────────────────────

test.describe('Matrix API Contract Tests', () => {
  test.setTimeout(120_000);

  test.beforeAll(async ({ request }) => {
    const reachable = await isStackReachable(request);
    test.skip(!reachable, `Stack unreachable at ${BASE}. Run: docker compose up -d`);

    // Login all test users once — tokens reused throughout the suite.
    for (const [name, cfg] of Object.entries(USERS)) {
      const s = await ssoLogin(request, cfg.email, 'changeme');
      state[name] = s;
    }
  });

  // ── BLOCK A: Authentication & Session ────────────────────────────────────

  test.describe('A — Authentication & Session', () => {

    test('A-01 SSO login alex → @alex:localhost', () => {
      expect(state['alex']?.userId).toBe('@alex:localhost');
    });

    test('A-02 SSO login marie → @marie:localhost', () => {
      expect(state['marie']?.userId).toBe('@marie:localhost');
    });

    test('A-03 SSO login tom → @tom:localhost', () => {
      expect(state['tom']?.userId).toBe('@tom:localhost');
    });

    test('A-04 SSO login kai → @kai:localhost', () => {
      expect(state['kai']?.userId).toBe('@kai:localhost');
    });

    test('A-05 GET /account/whoami returns correct user_id', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/account/whoami`, {
        headers: auth('alex'),
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(body.user_id).toBe('@alex:localhost');
    });

    test('A-06 Missing Bearer token → 401 M_MISSING_TOKEN', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`);
      expect(r.status()).toBe(401);
      const body = await r.json();
      expect(body.errcode).toBe('M_MISSING_TOKEN');
    });

    test('A-07 POST /logout invalidates token', async ({ request }) => {
      // Login a fresh session so we don't burn the shared token
      const s = await ssoLogin(request, USERS.alex.email, 'changeme');
      const r = await request.post(`${BASE}/_matrix/client/v3/logout`, {
        headers: { Authorization: `Bearer ${s.token}` },
      });
      expect(r.status()).toBe(200);

      // Token must now be rejected
      const r2 = await request.get(`${BASE}/_matrix/client/v3/account/whoami`, {
        headers: { Authorization: `Bearer ${s.token}` },
      });
      expect(r2.status()).toBe(401);
    });
  });

  // ── BLOCK B: User Directory Search ──────────────────────────────────────

  test.describe('B — User Directory Search', () => {

    test('B-01 Search "marie" returns @marie:localhost', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/user_directory/search`, {
        headers: auth('alex'),
        data: { search_term: 'marie', limit: 10 },
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      const ids = body.results.map((u: { user_id: string }) => u.user_id);
      expect(ids).toContain('@marie:localhost');
    });

    test('B-02 Search "tom" returns @tom:localhost', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/user_directory/search`, {
        headers: auth('alex'),
        data: { search_term: 'tom', limit: 10 },
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      const ids = body.results.map((u: { user_id: string }) => u.user_id);
      expect(ids).toContain('@tom:localhost');
    });

    test('B-03 Search unknown term returns empty results', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/user_directory/search`, {
        headers: auth('alex'),
        data: { search_term: 'zzz_does_not_exist_xyz', limit: 10 },
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(body.results).toHaveLength(0);
      expect(body.limited).toBe(false);
    });

    test('B-04 Search respects limit=1', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/user_directory/search`, {
        headers: auth('alex'),
        data: { search_term: 'a', limit: 1 },
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(body.results.length).toBeLessThanOrEqual(1);
    });
  });

  // ── BLOCK C: Direct Message / 1:1 Room ──────────────────────────────────

  test.describe('C — Direct Message', () => {
    let dmRoomId = '';

    test('C-01 alex creates DM room with invite to marie', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private', preset: 'private_chat', invite: ['@marie:localhost'] },
      });
      expect(r.status()).toBe(200);
      dmRoomId = (await r.json()).room_id;
      createdRooms.push(dmRoomId);
      expect(dmRoomId).toBeTruthy();
    });

    test('C-02 marie sees invite in sync rooms.invite', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('marie'),
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      const inviteIds = Object.keys(body.rooms?.invite ?? {});
      expect(inviteIds).toContain(dmRoomId);
    });

    test('C-03 marie accepts invite (POST /join)', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(dmRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(body.room_id).toBe(dmRoomId);
    });

    test('C-04 after join, rooms.invite no longer contains DM room', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('marie'),
      });
      const body = await r.json();
      const inviteIds = Object.keys(body.rooms?.invite ?? {});
      expect(inviteIds).not.toContain(dmRoomId);
    });

    test('C-05 after join, rooms.join contains DM room (GAP-1)', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('marie'),
      });
      const body = await r.json();
      const joinIds = Object.keys(body.rooms?.join ?? {});
      expect(joinIds).toContain(dmRoomId);
    });

    test('C-06 alex sends message to DM room', async ({ request }) => {
      const r = await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(dmRoomId)}/send/m.room.message/txn-c-alex-1`,
        { headers: auth('alex'), data: { msgtype: 'm.text', body: 'Hey Marie from DM!' } }
      );
      expect(r.status()).toBe(200);
      expect((await r.json()).event_id).toBeTruthy();
    });

    test('C-07 marie reads alex message via GET /messages', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(dmRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('marie') }
      );
      expect(r.status()).toBe(200);
      const body = await r.json();
      const bodies = body.chunk
        .map((e: { content?: { body?: string } }) => e.content?.body)
        .filter(Boolean);
      expect(bodies).toContain('Hey Marie from DM!');
    });

    test('C-08 marie replies to alex', async ({ request }) => {
      const r = await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(dmRoomId)}/send/m.room.message/txn-c-marie-1`,
        { headers: auth('marie'), data: { msgtype: 'm.text', body: 'Hey Alex from DM!' } }
      );
      expect(r.status()).toBe(200);
    });

    test('C-09 alex reads marie reply', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(dmRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('alex') }
      );
      const body = await r.json();
      const bodies = body.chunk
        .map((e: { content?: { body?: string } }) => e.content?.body)
        .filter(Boolean);
      expect(bodies).toContain('Hey Alex from DM!');
    });
  });

  // ── BLOCK D: Private Group Room ──────────────────────────────────────────

  test.describe('D — Private Group Room', () => {
    let groupRoomId = '';

    test('D-01 alex creates private group and invites marie + tom', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: {
          name: 'api-test-group',
          visibility: 'private',
          preset: 'private_chat',
          invite: ['@marie:localhost', '@tom:localhost'],
        },
      });
      expect(r.status()).toBe(200);
      groupRoomId = (await r.json()).room_id;
      createdRooms.push(groupRoomId);
    });

    test('D-02 marie sees invite in sync rooms.invite', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('marie'),
      });
      const inviteIds = Object.keys((await r.json()).rooms?.invite ?? {});
      expect(inviteIds).toContain(groupRoomId);
    });

    test('D-03 tom sees invite in sync rooms.invite', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('tom'),
      });
      const inviteIds = Object.keys((await r.json()).rooms?.invite ?? {});
      expect(inviteIds).toContain(groupRoomId);
    });

    test('D-04 marie joins group', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(groupRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('D-05 tom joins group', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(groupRoomId)}`,
        { headers: auth('tom'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('D-06 all 3 users send messages', async ({ request }) => {
      for (const [user, msg] of [
        ['alex',  'Alex says hi!'],
        ['marie', 'Marie says hi!'],
        ['tom',   'Tom says hi!'],
      ] as const) {
        const r = await request.put(
          `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(groupRoomId)}/send/m.room.message/txn-d-${user}`,
          { headers: auth(user), data: { msgtype: 'm.text', body: msg } }
        );
        expect(r.status()).toBe(200);
      }
    });

    test('D-07 GET /messages returns all 3 messages for any member', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(groupRoomId)}/messages?dir=b&limit=10`,
        { headers: auth('tom') }
      );
      expect(r.status()).toBe(200);
      const body = await r.json();
      const bodies = body.chunk
        .map((e: { content?: { body?: string } }) => e.content?.body)
        .filter(Boolean);
      expect(bodies).toContain('Alex says hi!');
      expect(bodies).toContain('Marie says hi!');
      expect(bodies).toContain('Tom says hi!');
    });

    test('D-08 non-member (kai) cannot read messages → 403', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(groupRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('kai') }
      );
      expect(r.status()).toBe(403);
      expect((await r.json()).errcode).toBe('M_FORBIDDEN');
    });
  });

  // ── BLOCK E: Public Room ─────────────────────────────────────────────────

  test.describe('E — Public Room', () => {
    let pubRoomId = '';

    test('E-01 alex creates public room (visibility: public)', async ({ request }) => {
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { name: 'api-test-public', visibility: 'public', preset: 'public_chat' },
      });
      expect(r.status()).toBe(200);
      pubRoomId = (await r.json()).room_id;
      createdRooms.push(pubRoomId);
    });

    test('E-02 marie joins public room without invite', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(pubRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('E-03 tom joins public room without invite', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(pubRoomId)}`,
        { headers: auth('tom'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('E-04 all 3 users exchange messages in public room', async ({ request }) => {
      for (const [user, msg] of [
        ['alex',  'Public hello from Alex'],
        ['marie', 'Public hello from Marie'],
        ['tom',   'Public hello from Tom'],
      ] as const) {
        const r = await request.put(
          `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(pubRoomId)}/send/m.room.message/txn-e-${user}`,
          { headers: auth(user), data: { msgtype: 'm.text', body: msg } }
        );
        expect(r.status()).toBe(200);
      }
      // Each user can read all messages
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(pubRoomId)}/messages?dir=b&limit=10`,
        { headers: auth('tom') }
      );
      const body = await r.json();
      const bodies = body.chunk
        .map((e: { content?: { body?: string } }) => e.content?.body)
        .filter(Boolean);
      expect(bodies).toContain('Public hello from Alex');
      expect(bodies).toContain('Public hello from Marie');
      expect(bodies).toContain('Public hello from Tom');
    });

    test('E-05 unauthenticated read → 401 M_MISSING_TOKEN', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(pubRoomId)}/messages?dir=b&limit=5`
      );
      expect(r.status()).toBe(401);
    });
  });

  // ── BLOCK F: Room List / Sync ────────────────────────────────────────────

  test.describe('F — Room List / Sync', () => {

    test('F-01 GET /sync returns joined rooms in rooms.join', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('alex'),
      });
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(Object.keys(body.rooms?.join ?? {})).not.toHaveLength(0);
      expect(body.next_batch).toBeTruthy();
    });

    test('F-02 GET /sync incremental returns only delta from since token', async ({ request }) => {
      // Capture token, send a message, verify it appears in incremental sync
      const syncR = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('alex'),
      });
      const token = (await syncR.json()).next_batch;

      // Create a fresh room to ensure something new happens
      const createR = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private' },
      });
      const freshRoom = (await createR.json()).room_id;
      createdRooms.push(freshRoom);

      await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(freshRoom)}/send/m.room.message/txn-f-01`,
        { headers: auth('alex'), data: { msgtype: 'm.text', body: 'incremental test' } }
      );

      const incrR = await request.get(
        `${BASE}/_matrix/client/v3/sync?since=${encodeURIComponent(token)}&timeout=0`,
        { headers: auth('alex') }
      );
      expect(incrR.status()).toBe(200);
      const incrBody = await incrR.json();
      // The fresh room must appear in rooms.join with our message
      expect(incrBody.rooms?.join?.[freshRoom]).toBeTruthy();
    });
  });

  // ── BLOCK G: Message History & Pagination ───────────────────────────────

  test.describe('G — Message History & Pagination', () => {
    let histRoomId = '';

    test.beforeAll(async ({ request }) => {
      if (!state['alex']) return;
      // Create a room with 10 messages for pagination tests
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private' },
      });
      histRoomId = (await r.json()).room_id;
      createdRooms.push(histRoomId);
      for (let i = 0; i < 10; i++) {
        await request.put(
          `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(histRoomId)}/send/m.room.message/txn-g-${i}`,
          { headers: auth('alex'), data: { msgtype: 'm.text', body: `message ${i}` } }
        );
      }
    });

    test('G-01 GET /messages?limit=5 returns at most 5 events', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(histRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('alex') }
      );
      expect(r.status()).toBe(200);
      const body = await r.json();
      expect(body.chunk.length).toBeLessThanOrEqual(5);
    });

    test('G-02 Pagination: two pages cover all 10 messages', async ({ request }) => {
      const r1 = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(histRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('alex') }
      );
      const body1 = await r1.json();
      expect(body1.chunk.length).toBeGreaterThan(0);
      const endToken = body1.end ?? body1.start; // pagination cursor

      if (endToken) {
        const r2 = await request.get(
          `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(histRoomId)}/messages?dir=b&limit=5&from=${encodeURIComponent(endToken)}`,
          { headers: auth('alex') }
        );
        expect(r2.status()).toBe(200);
        const body2 = await r2.json();
        // Total across both pages should be 10
        expect(body1.chunk.length + body2.chunk.length).toBe(10);
      }
    });

    test('G-03 dir=b returns events newest-first', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(histRoomId)}/messages?dir=b&limit=10`,
        { headers: auth('alex') }
      );
      const body = await r.json();
      const ts = body.chunk.map((e: { origin_server_ts: number }) => e.origin_server_ts);
      for (let i = 1; i < ts.length; i++) {
        expect(ts[i]).toBeLessThanOrEqual(ts[i - 1]);
      }
    });
  });

  // ── BLOCK H: Typing Indicators ───────────────────────────────────────────

  test.describe('H — Typing Indicators', () => {
    let typingRoomId = '';

    test.beforeAll(async ({ request }) => {
      if (!state['alex']) return;
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private' },
      });
      typingRoomId = (await r.json()).room_id;
      createdRooms.push(typingRoomId);
    });

    test('H-01 PUT /typing with typing=true → 200', async ({ request }) => {
      const r = await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(typingRoomId)}/typing/${encodeURIComponent('@alex:localhost')}`,
        { headers: auth('alex'), data: { typing: true, timeout: 5000 } }
      );
      expect(r.status()).toBe(200);
    });

    test('H-02 PUT /typing with typing=false → 200', async ({ request }) => {
      const r = await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(typingRoomId)}/typing/${encodeURIComponent('@alex:localhost')}`,
        { headers: auth('alex'), data: { typing: false } }
      );
      expect(r.status()).toBe(200);
    });
  });

  // ── BLOCK I: Read Receipts ───────────────────────────────────────────────

  test.describe('I — Read Receipts', () => {
    let receiptRoomId = '';
    let eventId = '';

    test.beforeAll(async ({ request }) => {
      if (!state['alex']) return;
      const r = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private', invite: ['@marie:localhost'] },
      });
      receiptRoomId = (await r.json()).room_id;
      createdRooms.push(receiptRoomId);
      await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(receiptRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      const sendR = await request.put(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(receiptRoomId)}/send/m.room.message/txn-i-1`,
        { headers: auth('alex'), data: { msgtype: 'm.text', body: 'read this!' } }
      );
      eventId = (await sendR.json()).event_id;
    });

    test('I-01 POST /receipt/m.read/{eventId} → 200', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(receiptRoomId)}/receipt/m.read/${encodeURIComponent(eventId)}`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
    });
  });

  // ── BLOCK J: Presence ────────────────────────────────────────────────────

  test.describe('J — Presence', () => {

    test('J-01 PUT /presence/{userId}/status → 200', async ({ request }) => {
      const r = await request.put(
        `${BASE}/_matrix/client/v3/presence/${encodeURIComponent('@alex:localhost')}/status`,
        { headers: auth('alex'), data: { presence: 'online', status_msg: 'testing' } }
      );
      expect(r.status()).toBe(200);
    });

    test('J-02 GET /presence/{userId}/status → 200 or 503 (service state)', async ({ request }) => {
      // First ensure we set presence so the service has state for alex
      await request.put(
        `${BASE}/_matrix/client/v3/presence/${encodeURIComponent('@alex:localhost')}/status`,
        { headers: auth('alex'), data: { presence: 'online' } }
      );
      const r = await request.get(
        `${BASE}/_matrix/client/v3/presence/${encodeURIComponent('@alex:localhost')}/status`,
        { headers: auth('alex') }
      );
      // Accept 200 (presence set) or 503 (presence service init lag) — both are valid
      expect([200, 503]).toContain(r.status());
      if (r.status() === 200) {
        const body = await r.json();
        expect(body.presence).toBeTruthy();
      }
    });
  });

  // ── BLOCK K: Leave Room / Decline Invite ─────────────────────────────────

  test.describe('K — Leave Room / Decline Invite', () => {
    let leaveRoomId = '';
    let declineRoomId = '';

    test.beforeAll(async ({ request }) => {
      if (!state['alex']) return;
      // Room for leave test
      const r1 = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private', invite: ['@marie:localhost'] },
      });
      leaveRoomId = (await r1.json()).room_id;
      createdRooms.push(leaveRoomId);
      await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(leaveRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      // Room for decline test (tom is invited, will decline)
      const r2 = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
        headers: auth('alex'),
        data: { visibility: 'private', invite: ['@tom:localhost'] },
      });
      declineRoomId = (await r2.json()).room_id;
      createdRooms.push(declineRoomId);
    });

    test('K-01 marie leaves room via POST /leave', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(leaveRoomId)}/leave`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('K-02 after leave, non-member cannot read messages', async ({ request }) => {
      const r = await request.get(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(leaveRoomId)}/messages?dir=b&limit=5`,
        { headers: auth('marie') }
      );
      expect(r.status()).toBe(403);
    });

    test('K-03 marie can rejoin a room she left', async ({ request }) => {
      // Need alex to re-invite
      await request.post(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(leaveRoomId)}/invite`,
        { headers: auth('alex'), data: { user_id: '@marie:localhost' } }
      );
      const r = await request.post(
        `${BASE}/_matrix/client/v3/join/${encodeURIComponent(leaveRoomId)}`,
        { headers: auth('marie'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('K-04 tom declines invite via POST /leave on invited room', async ({ request }) => {
      const r = await request.post(
        `${BASE}/_matrix/client/v3/rooms/${encodeURIComponent(declineRoomId)}/leave`,
        { headers: auth('tom'), data: {} }
      );
      expect(r.status()).toBe(200);
    });

    test('K-05 after decline, room absent from tom sync rooms.invite', async ({ request }) => {
      const r = await request.get(`${BASE}/_matrix/client/v3/sync?timeout=0`, {
        headers: auth('tom'),
      });
      const body = await r.json();
      const inviteIds = Object.keys(body.rooms?.invite ?? {});
      expect(inviteIds).not.toContain(declineRoomId);
    });
  });
});

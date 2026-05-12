/**
 * Regression tests for the create_room event-ordering and sync-deduplication fix.
 *
 * Covers the two root causes identified in tmp/create_room_bug.md:
 *
 *   RC-1  m.room.create MUST be the first event in the room timeline (Matrix spec §8.5.1).
 *         Previously, m.room.member (creator join) was inserted before m.room.create in
 *         the events table because Server.join was called before the create event was
 *         persisted.
 *
 *   RC-2  m.room.member MUST NOT appear in both state.events AND timeline.events for the
 *         same user in the same sync response. The duplication caused matrix-js-sdk to
 *         compute prev_membership == new_membership == "join" → "No membership changes
 *         detected for room" — and potentially fail to display the room in Element's sidebar.
 *
 * Run: npx playwright test tests/create_room_sync_fix.spec.ts
 * Requires: full stack running (docker compose up -d --wait)
 */

import { test, expect, type APIRequestContext } from '@playwright/test';

const BASE = process.env.NEBU_MATRIX_URL ?? 'http://localhost:8008';
const DEX  = process.env.NEBU_DEX_URL   ?? 'http://localhost:5556';

// ── Skip guard ────────────────────────────────────────────────────────────

async function isStackReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const r = await request.get(`${BASE}/_matrix/client/versions`, {
      timeout: 5_000,
      failOnStatusCode: false,
    });
    return r.status() < 500;
  } catch {
    return false;
  }
}

// ── SSO Login (PKCE-free, loginToken path — same as matrix_api.spec.ts) ──

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

// ── Tests ─────────────────────────────────────────────────────────────────

test.describe('Create Room — event ordering and sync deduplication', () => {
  test.setTimeout(60_000);

  let token = '';
  let userId = '';

  test.beforeAll(async ({ request }) => {
    const reachable = await isStackReachable(request);
    test.skip(!reachable, `Stack unreachable at ${BASE}. Run: docker compose up -d`);

    ({ token, userId } = await ssoLogin(request, 'alex@example.com', 'changeme'));
  });

  /**
   * RC-1: m.room.create MUST be the first event in the timeline.
   *
   * After POST /createRoom the next GET /sync MUST return:
   *   timeline.events[0].type == "m.room.create"
   *   timeline.events[1].type == "m.room.member"  (creator join)
   *
   * Before the fix the order was reversed (member before create).
   */
  test('RC-1: m.room.create is the first timeline event after room creation', async ({ request }) => {
    if (!token) test.skip(true, 'Login failed in beforeAll');

    const headers = { Authorization: `Bearer ${token}` };
    const roomName = `rc1-test-${Date.now()}`;

    // Create room
    const createResp = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
      headers,
      data: { name: roomName },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();
    expect(roomId).toMatch(/^!/);

    // Initial sync — no since token
    const syncResp = await request.get(`${BASE}/_matrix/client/v3/sync`, {
      headers,
      params: { timeout: '0' },
    });
    expect(syncResp.status()).toBe(200);
    const sync = await syncResp.json();

    const room = sync?.rooms?.join?.[roomId];
    expect(room, `Room ${roomId} missing from rooms.join`).toBeDefined();

    const timeline: Array<{ type: string; sender?: string }> = room.timeline?.events ?? [];
    expect(timeline.length, 'timeline must contain at least 2 events (create + member)').toBeGreaterThanOrEqual(2);

    // Find positions of m.room.create and m.room.member in the timeline
    const createIdx = timeline.findIndex(ev => ev.type === 'm.room.create');
    const memberIdx = timeline.findIndex(ev => ev.type === 'm.room.member');

    expect(createIdx, 'm.room.create must be present in timeline').toBeGreaterThanOrEqual(0);
    expect(memberIdx, 'm.room.member must be present in timeline').toBeGreaterThanOrEqual(0);

    expect(
      createIdx,
      `m.room.create (idx=${createIdx}) must come before m.room.member (idx=${memberIdx}) — RC-1 ordering violation`
    ).toBeLessThan(memberIdx);
  });

  /**
   * RC-2: m.room.member MUST NOT appear in both state.events AND timeline.events
   * for the same user in the same sync response.
   *
   * matrix-js-sdk sees: state sets membership=join, timeline has membership=join again
   * → no change computed → "No membership changes detected for room".
   *
   * After the fix: m.room.member is in timeline only; state.events does NOT duplicate it.
   */
  test('RC-2: creator m.room.member is not duplicated across state.events and timeline.events', async ({ request }) => {
    if (!token) test.skip(true, 'Login failed in beforeAll');

    const headers = { Authorization: `Bearer ${token}` };
    const roomName = `rc2-test-${Date.now()}`;

    // Create room
    const createResp = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
      headers,
      data: { name: roomName },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Initial sync
    const syncResp = await request.get(`${BASE}/_matrix/client/v3/sync`, {
      headers,
      params: { timeout: '0' },
    });
    expect(syncResp.status()).toBe(200);
    const sync = await syncResp.json();

    const room = sync?.rooms?.join?.[roomId];
    expect(room, `Room ${roomId} missing from rooms.join`).toBeDefined();

    const stateEvents: Array<{ type: string; state_key?: string }> = room.state?.events ?? [];
    const timelineEvents: Array<{ type: string; sender?: string; state_key?: string }> = room.timeline?.events ?? [];

    // Find users who have m.room.member in timeline (as sender, for self-joins)
    const membersInTimeline = new Set(
      timelineEvents
        .filter(ev => ev.type === 'm.room.member')
        .map(ev => ev.sender ?? ev.state_key ?? '')
        .filter(Boolean)
    );

    // Assert: no m.room.member in state.events for users who are ALSO in timeline
    const duplicatedMembers = stateEvents
      .filter(ev => ev.type === 'm.room.member')
      .map(ev => ev.state_key ?? '')
      .filter(sk => membersInTimeline.has(sk));

    expect(
      duplicatedMembers,
      `RC-2: m.room.member for [${duplicatedMembers.join(', ')}] appears in BOTH state.events ` +
      `and timeline.events — matrix-js-sdk will compute no membership change and log ` +
      `"No membership changes detected for room ${roomId}"`
    ).toHaveLength(0);
  });

  /**
   * Smoke: room creation succeeds and room appears in sync rooms.join.
   */
  test('smoke: createRoom returns room_id and room appears in sync', async ({ request }) => {
    if (!token) test.skip(true, 'Login failed in beforeAll');

    const headers = { Authorization: `Bearer ${token}` };

    const createResp = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
      headers,
      data: { name: `smoke-${Date.now()}` },
    });
    expect(createResp.status()).toBe(200);
    const { room_id } = await createResp.json();
    expect(room_id).toMatch(/^!/);

    const syncResp = await request.get(`${BASE}/_matrix/client/v3/sync`, {
      headers,
      params: { timeout: '0' },
    });
    expect(syncResp.status()).toBe(200);
    const sync = await syncResp.json();

    expect(sync.rooms?.join?.[room_id]).toBeDefined();
    expect(sync.rooms?.join?.[room_id]?.timeline?.events?.length).toBeGreaterThan(0);
  });
});

/**
 * Sync gap regression guards — join, leave, forget.
 *
 * Three confirmed spec-gap bugs, each with a failing test written FIRST:
 *
 *   GAP-JOIN-INVITE  §9.2  After POST /rooms/{id}/join (accept invite), sync
 *                          MUST deliver rooms.join[roomId] within 10 s.
 *                          Root cause: emit_membership_event broadcasts to
 *                          room:#{roomId} — invite path IS subscribed → should
 *                          pass. This test confirms the invite-accept path is
 *                          healthy and guards against regression.
 *
 *   GAP-JOIN-PUBLIC  §9.2  After POST /join/{roomIdOrAlias} for a public room
 *                          (no prior invite), sync MUST deliver rooms.join
 *                          within 10 s.
 *                          Root cause: sync long-poll only subscribes to
 *                          room:#{id} for rooms already in get_rooms_for_user +
 *                          get_pending_invite_rooms_for_user. A fresh public-
 *                          room join has neither → broadcast is lost → 30 s
 *                          spinner. Expected to FAIL until server-side fix adds
 *                          user-level broadcast for join events.
 *
 *   GAP-LEAVE-UI     §5.2  After POST /leave, the room tile MUST disappear from
 *                          the sidebar within 10 s.
 *                          Regression: rooms stay "unstable" in Element Web
 *                          because buildLeaveRooms returns ALL left rooms on
 *                          EVERY sync (no since-token filter) → client re-
 *                          processes leave on every cycle → confused state.
 *
 *   GAP-LEAVE-ONCE   §5.2  After the first incremental sync that delivers
 *                          rooms.leave[roomId], the NEXT incremental sync with
 *                          a fresh since-token SHOULD NOT include the same
 *                          left room again. Current impl violates this because
 *                          buildLeaveRooms queries left_at IS NOT NULL without
 *                          time-based filtering.
 *
 *   GAP-FORGET       §11.3 After POST /forget (post-leave), rooms.join and
 *                          rooms.leave MUST NOT include the forgotten room.
 *                          MVP impl is a no-op — expected to FAIL until
 *                          GetSyncDelta filters forgotten rooms.
 *
 * Multi-user pattern: marie (API-only) creates rooms and invites alex (UI).
 * Network-first: waitForResponse is registered BEFORE the triggering action
 * to prevent race conditions (Murat TEA pattern).
 *
 * Run: npx playwright test features/room/join_leave_sync.spec.ts
 */

import { test, expect } from '@playwright/test';
import { loginViaOidc, ELEMENT_URL } from '../../fixtures/oidc';
import { isElementReachable, isDexReachable, dismissKeyDialog } from '../../fixtures/helpers';

test.describe('Sync gap regression guards — join, leave, forget', () => {
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

  // ── [P0] GAP-JOIN-INVITE: Accept invite → rooms.join within 10 s ─────────────
  //
  // Given: marie invites alex to a room (sync receives rooms.invite).
  // When:  alex accepts via POST /rooms/{roomId}/join.
  // Then:  sync delivers rooms.join[roomId] within 10 s.
  //        rooms.join[roomId] contains m.room.member membership=join for alex.
  //        The room appears in the Element Web sidebar as a joined room.
  //
  // Healthy-path guard: the sync long-poll IS subscribed to room:roomId via
  // invited_room_ids. If this test fails it means the :pg broadcast path for
  // invite-accept is broken — a regression against the existing fix.

  test('[P0] GAP-JOIN-INVITE: accept invite → sync delivers rooms.join within 10 s', async ({ page }) => {
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(alexSession.accessToken, 'alex must get an access token').toBeTruthy();
    expect(alexSession.userId, 'alex must have a userId').toBeTruthy();
    await dismissKeyDialog(page);

    // marie logs in via a second browser context (API-only — no UI for marie)
    const marieCtx = await page.context().browser()!.newContext();
    const mariePage = await marieCtx.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme');
    await mariePage.close();
    await marieCtx.close();
    expect(marieSession.accessToken, 'marie must get an access token').toBeTruthy();

    const roomName = `join-invite-sync-${Date.now()}`;

    // marie creates the room
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: {
        Authorization: `Bearer ${marieSession.accessToken}`,
        'Content-Type': 'application/json',
      },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status(), 'marie must be able to createRoom').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Network-first: register sync interceptor for rooms.invite BEFORE sending invite.
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

    // Wait for rooms.invite to arrive in alex's sync (proves invite broadcast works)
    await Promise.race([
      syncWithInvitePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('Invite not delivered in sync within 10 s')), 10_000),
      ),
    ]);

    // Network-first: register sync interceptor for rooms.join BEFORE accepting invite.
    // This is the critical guard: after POST /join, sync MUST deliver rooms.join[roomId]
    // within 10 s — not 30 s (which is the long-poll fallback timeout).
    const syncWithJoinPromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.join?.[roomId] !== undefined;
      } catch { return false; }
    });

    // Accept invite: simulates Element Web "An Diskussion teilnehmen" / "Accept"
    const joinResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/join`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(joinResp.status(), 'POST /rooms/{roomId}/join must return 200').toBe(200);
    const joinBody = await joinResp.json();
    expect(joinBody.room_id, 'POST /join must return the room_id').toBe(roomId);

    // Primary assertion: sync MUST deliver rooms.join[roomId] within 10 s.
    // Without the :pg broadcast fix for join, the long-poll sleeps 30 s → FAIL.
    const syncResp = await Promise.race([
      syncWithJoinPromise,
      new Promise<never>((_, reject) =>
        setTimeout(
          () => reject(new Error(
            'GAP-JOIN-INVITE: sync did not deliver rooms.join within 10 s after accepting invite. ' +
            'The :pg broadcast from emit_membership_event must reach the long-poll sync task. ' +
            'Ensure the invited room is included in the room_ids list before subscribing.'
          )),
          10_000,
        ),
      ),
    ]);

    const syncBody = await (syncResp as Awaited<typeof syncWithJoinPromise>).json();
    const joinRoom = syncBody.rooms?.join?.[roomId];
    expect(joinRoom, 'rooms.join must contain the accepted room').toBeDefined();

    // Per §9.2: after join, the m.room.member membership=join event for the user
    // MUST appear in state.events or timeline.events (it is a state event but may
    // be placed in timeline when it occurs within the returned timeline window).
    const stateEvents: Array<{ type: string; content?: { membership?: string }; state_key?: string }> =
      joinRoom?.state?.events ?? [];
    const timelineEvents: Array<{ type: string; content?: { membership?: string }; state_key?: string }> =
      joinRoom?.timeline?.events ?? [];
    const allEvents = [...stateEvents, ...timelineEvents];

    const memberJoinEvent = allEvents.find(
      (ev) =>
        ev.type === 'm.room.member' &&
        ev.content?.membership === 'join' &&
        ev.state_key === alexSession.userId,
    );
    expect(
      memberJoinEvent,
      'rooms.join[roomId] MUST include m.room.member membership=join for the joining user (§9.2)',
    ).toBeDefined();

    // UI: room must now appear in sidebar as a joined room (no longer in invite state)
    await expect(
      page.getByText(roomName, { exact: false }).first(),
      'room must be visible in sidebar after accepting invite',
    ).toBeVisible({ timeout: 15_000 });
  });

  // ── [P0] GAP-JOIN-PUBLIC: Join public room → rooms.join within 10 s ──────────
  //
  // Given: alex is logged in, marie has created a public room.
  // When:  alex POSTs /join/{roomId} (no prior invite).
  // Then:  sync delivers rooms.join[roomId] within 10 s.
  //
  // EXPECTED FAILURE (red phase): the sync long-poll subscribes only to rooms
  // already in get_rooms_for_user + get_pending_invite_rooms_for_user. A fresh
  // public-room join has neither → the :pg broadcast from emit_membership_event
  // goes to room:#{roomId} but no sync task is subscribed → broadcast lost → 30 s
  // spinner in Element Web.
  //
  // Fix required: after a successful join, broadcast a user-level signal
  // (e.g. {:new_join, room_id} to "user:#{user_id}" :pg group) so the sync
  // long-poll wakes up and re-queries, analogous to the {:new_invite, room_id}
  // pattern used for invite delivery.

  test('[P0] GAP-JOIN-PUBLIC: join public room → sync delivers rooms.join within 10 s', async ({ page }) => {
    const alexSession = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(alexSession.accessToken, 'alex must get an access token').toBeTruthy();
    await dismissKeyDialog(page);

    // marie logs in via a second browser context to create the public room
    const marieCtx = await page.context().browser()!.newContext();
    const mariePage = await marieCtx.newPage();
    const marieSession = await loginViaOidc(mariePage, 'marie@example.com', 'changeme');
    await mariePage.close();
    await marieCtx.close();
    expect(marieSession.accessToken, 'marie must get an access token').toBeTruthy();

    // marie creates a public room (join_rules: public, no invite needed)
    const roomName = `join-public-sync-${Date.now()}`;
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: {
        Authorization: `Bearer ${marieSession.accessToken}`,
        'Content-Type': 'application/json',
      },
      data: { name: roomName, visibility: 'public', preset: 'public_chat' },
    });
    expect(createResp.status(), 'marie must be able to create a public room').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Network-first: register sync interceptor for rooms.join BEFORE alex joins.
    const syncWithJoinPromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.join?.[roomId] !== undefined;
      } catch { return false; }
    });

    // alex joins the public room directly (no prior invite)
    // This simulates "An Diskussion teilnehmen" / joining via room directory
    const joinResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/join/${encodeURIComponent(roomId)}`,
      {
        headers: { Authorization: `Bearer ${alexSession.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(joinResp.status(), 'POST /join/{roomId} must return 200').toBe(200);
    const joinBody = await joinResp.json();
    expect(joinBody.room_id, 'POST /join must return the room_id').toBe(roomId);

    // Primary assertion: sync MUST deliver rooms.join[roomId] within 10 s.
    // EXPECTED FAILURE: no sync task is subscribed to room:#{roomId} when alex
    // joins (no invite path → no subscribed :pg group). The 30 s long-poll
    // expires → client spins → then gets rooms.join in the next sync cycle.
    // Fix: emit {:new_join, room_id} to "user:#{user_id}" :pg group in join_room/2,
    // mirroring the {:new_invite, room_id} pattern in invite_user/2.
    const syncResp = await Promise.race([
      syncWithJoinPromise,
      new Promise<never>((_, reject) =>
        setTimeout(
          () => reject(new Error(
            'GAP-JOIN-PUBLIC: sync did not deliver rooms.join within 10 s after POST /join. ' +
            'Root cause: no :pg subscription for room:roomId exists in the long-polling sync task ' +
            'when a user joins a public room without a prior invite. ' +
            'Fix: broadcast {:new_join, room_id} to user:#{user_id} :pg group in join_room/2.'
          )),
          10_000,
        ),
      ),
    ]);

    const syncBody = await (syncResp as Awaited<typeof syncWithJoinPromise>).json();
    expect(syncBody.rooms?.join?.[roomId], 'rooms.join must contain the newly joined room').toBeDefined();

    // UI: room must appear in the sidebar
    await expect(
      page.getByText(roomName, { exact: false }).first(),
      'room must be visible in sidebar after joining',
    ).toBeVisible({ timeout: 15_000 });
  });

  // ── [P0] GAP-LEAVE-UI: Leave → room tile disappears from sidebar ──────────────
  //
  // Given: alex has created and navigated into a room.
  // When:  alex POSTs /leave.
  // Then:  rooms.leave[roomId] arrives in sync within 10 s (existing guard).
  //        rooms.leave[roomId].state.events includes m.room.member membership=leave.
  //        The room TILE disappears from the sidebar within 20 s.
  //
  // The existing room-lifecycle.spec.ts verifies the sync format and room header.
  // This test adds the sidebar tile disappearance assertion — the reported symptom
  // ("rooms liegen als 'instabil' im client") means the tile stays visible.

  test('[P0] GAP-LEAVE-UI: leave room → room tile disappears from sidebar within 20 s', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken, 'alex must get an access token').toBeTruthy();
    await dismissKeyDialog(page);

    const roomName = `leave-tile-test-${Date.now()}`;
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status(), 'createRoom must succeed').toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Wait for room tile to appear in sidebar before leaving
    const roomTile = page.getByText(roomName, { exact: false }).first();
    await expect(roomTile, 'room tile must be visible before leave').toBeVisible({ timeout: 60_000 });

    // Click the room tile to navigate into the room (avoids full-page reload)
    await roomTile.click();
    const roomHeader = page.locator('.mx_RoomHeader, [data-testid="room-header"]').first();
    await expect(roomHeader, 'room header must be visible after navigation').toBeVisible({ timeout: 15_000 });

    // Network-first: register sync interceptor BEFORE leaving.
    const syncWithLeavePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.leave?.[roomId] !== undefined;
      } catch { return false; }
    });

    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave must return 200').toBe(200);

    // Sync assertion: rooms.leave must arrive within 10 s
    const syncResp = await Promise.race([
      syncWithLeavePromise,
      new Promise<never>((_, reject) =>
        setTimeout(
          () => reject(new Error('GAP-LEAVE-UI: sync did not deliver rooms.leave within 10 s')),
          10_000,
        ),
      ),
    ]);
    const syncBody = await (syncResp as Awaited<typeof syncWithLeavePromise>).json();
    expect(syncBody.rooms?.leave?.[roomId], 'rooms.leave must contain the left room').toBeDefined();

    // Per spec: rooms.leave[roomId].state.events must include m.room.member membership=leave.
    // Element Web uses this event to confirm the membership transition.
    const leaveRoom = syncBody.rooms.leave[roomId];
    const stateEvents: Array<{ type: string; content?: { membership?: string } }> =
      leaveRoom?.state?.events ?? [];
    const memberLeaveEvent = stateEvents.find(
      (ev) => ev.type === 'm.room.member' && ev.content?.membership === 'leave',
    );
    expect(
      memberLeaveEvent,
      'rooms.leave[roomId].state.events MUST contain m.room.member membership=leave',
    ).toBeDefined();

    // UI assertion — the reported bug: rooms stay "unstable" in sidebar after leave.
    // Element Web should remove the tile when it processes rooms.leave with the
    // m.room.member leave event. If this assertion fails, the client receives a
    // correct sync response but still renders the room tile.
    //
    // Navigation note: the test clicked into the room, so the main panel still shows
    // "roomName can't be previewed" after the leave. We scope the locator to the
    // sidebar (left panel room list) to avoid matching the main panel text.
    // The sidebar selector targets Element Web's room list container.
    const sidebar = page.locator('.mx_LeftPanel, [data-testid="roomlist"]').first();
    await expect(
      sidebar.getByText(roomName, { exact: false }).first(),
      'GAP-LEAVE-UI: room tile must disappear from sidebar after leave (20 s timeout)',
    ).toBeHidden({ timeout: 20_000 });
  });

  // ── [P0] GAP-LEAVE-ONCE: rooms.leave must not repeat in subsequent syncs ──────
  //
  // Given: alex leaves a room and the first incremental sync delivers rooms.leave.
  // When:  the next incremental sync runs (with updated since-token).
  // Then:  rooms.leave[roomId] SHOULD NOT appear again.
  //
  // EXPECTED FAILURE: buildLeaveRooms queries
  //   WHERE user_id = $1 AND left_at IS NOT NULL
  // with no time-based filter. Every sync response includes ALL left rooms ever.
  // This violates the spirit of incremental sync (since-token semantics) and causes
  // Element Web to re-process the leave event on every cycle → confused state.
  //
  // Fix required: add a left_at > since_timestamp filter in buildLeaveRooms, so
  // only rooms left SINCE the previous sync are included in rooms.leave.
  // Alternatively: after the client acknowledges a left room, remove the
  // left_at IS NOT NULL row from the query window (e.g. via a separate seen_leaves
  // table keyed by (user_id, room_id)).

  test('[P0] GAP-LEAVE-ONCE: left room SHOULD NOT repeat in subsequent incremental syncs', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken, 'alex must get an access token').toBeTruthy();
    await dismissKeyDialog(page);

    const roomName = `leave-once-test-${Date.now()}`;
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Step 1: Get a stable since-token by doing a timeout=0 sync now.
    const preSyncResp = await page.request.get(`${ELEMENT_URL}/_matrix/client/v3/sync`, {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      params: { timeout: '0' },
    });
    expect(preSyncResp.status()).toBe(200);
    const preSyncBody = await preSyncResp.json();
    const sinceToken = preSyncBody.next_batch as string;
    expect(sinceToken, 'pre-leave sync must return a next_batch token').toBeTruthy();

    // Leave the room
    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave must return 200').toBe(200);

    // Step 2: First incremental sync after leave (must deliver rooms.leave[roomId]).
    // Network-first pattern: use a long-poll request with timeout=10000.
    // The :pg broadcast wakes the server sync task immediately, so the leave
    // should be delivered well within 15 s. Using waitForResponse is not suitable
    // here because we are driving the sync via page.request (not Element Web's sync).
    const firstSyncResp = await page.request.get(`${ELEMENT_URL}/_matrix/client/v3/sync`, {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      params: { since: sinceToken, timeout: '10000' },
    });
    expect(firstSyncResp.status()).toBe(200);
    const firstSyncBody = await firstSyncResp.json() as Record<string, unknown>;
    expect(
      (firstSyncBody.rooms as Record<string, unknown> | undefined)?.leave as Record<string, unknown> | undefined,
      'First incremental sync must deliver rooms.leave[roomId] within 15 s',
    ).toBeDefined();
    expect(
      ((firstSyncBody.rooms as Record<string, unknown>)?.leave as Record<string, unknown>)?.[roomId],
      'First incremental sync must deliver rooms.leave for the left room',
    ).toBeDefined();

    const firstNextBatch = firstSyncBody.next_batch as string;
    expect(firstNextBatch, 'first sync after leave must return a next_batch').toBeTruthy();

    // Step 3: Second incremental sync (fresh since-token from step 2).
    // SHOULD NOT include rooms.leave[roomId] again — the client already processed the leave.
    // EXPECTED FAILURE: buildLeaveRooms has no time-based filter, so it returns
    // rooms.leave[roomId] in EVERY subsequent sync as well.
    const secondSyncResp = await page.request.get(`${ELEMENT_URL}/_matrix/client/v3/sync`, {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      params: { since: firstNextBatch, timeout: '0' },
    });
    expect(secondSyncResp.status()).toBe(200);
    const secondSyncBody = await secondSyncResp.json() as Record<string, unknown>;
    const secondRooms = secondSyncBody.rooms as Record<string, unknown> | undefined;
    const secondLeave = secondRooms?.leave as Record<string, unknown> | undefined;

    expect(
      secondLeave?.[roomId],
      'GAP-LEAVE-ONCE: rooms.leave SHOULD NOT include the already-processed left room ' +
      'in subsequent incremental syncs (buildLeaveRooms needs a since-timestamp filter). ' +
      'Repeated rooms.leave entries cause Element Web to re-process the leave on every ' +
      'sync cycle, resulting in "unstable" room tiles in the sidebar.',
    ).toBeUndefined();
  });

  // ── [P0] GAP-FORGET: POST /forget → room absent from subsequent sync ─────────
  //
  // Given: alex has left a room and POSTs /forget.
  // Then:  the next GET /sync (timeout=0) MUST NOT include the room in
  //        rooms.join, rooms.leave, or rooms.invite.
  //
  // Per §11.3: "This API stops a user remembering about a particular room."
  // After forgetting, the room MUST NOT appear in /sync.
  //
  // EXPECTED FAILURE: MVP implementation of forget_room is a no-op (see server.ex
  // comment: "forget is a no-op beyond the FailedPrecondition check because
  // GetSyncDelta does not yet filter forgotten rooms"). Follow-up story required.

  test('[P0] GAP-FORGET: POST /forget → room absent from subsequent sync (§11.3)', async ({ page }) => {
    const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
    expect(session.accessToken, 'alex must get an access token').toBeTruthy();
    await dismissKeyDialog(page);

    const roomName = `forget-sync-test-${Date.now()}`;
    const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
      headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
      data: { name: roomName, visibility: 'private', preset: 'private_chat' },
    });
    expect(createResp.status()).toBe(200);
    const { room_id: roomId } = await createResp.json();

    // Step 1: Get a stable since-token before the leave so we can poll for rooms.leave.
    // Network-first pattern: register the sync interceptor BEFORE the leave to avoid the
    // race condition where the sync response (with rooms.leave) arrives before the
    // page.waitForResponse listener is registered.
    const syncWithLeavePromise = page.waitForResponse(async (resp) => {
      if (!resp.url().includes('/_matrix/client/v3/sync')) return false;
      if (!resp.ok()) return false;
      try {
        const body = await resp.json();
        return body?.rooms?.leave?.[roomId] !== undefined;
      } catch { return false; }
    });

    // Step 2: Leave (required before forget per §11.3)
    const leaveResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/leave`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(leaveResp.status(), 'POST /leave must succeed before forget').toBe(200);

    // Step 3: Wait for rooms.leave to be acknowledged in sync (ensures DB is consistent).
    // The network-first pattern (promise registered before leave) avoids the race where
    // the sync delivers rooms.leave within milliseconds of the leave API call completing.
    const leaveAckResp = await Promise.race([
      syncWithLeavePromise,
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('rooms.leave not acknowledged within 15 s')), 15_000),
      ),
    ]);
    const leaveAckBody = await (leaveAckResp as Awaited<typeof syncWithLeavePromise>).json();
    const sinceAfterLeave = leaveAckBody.next_batch as string;
    expect(sinceAfterLeave, 'sync after leave must provide next_batch').toBeTruthy();

    // Step 4: POST /forget
    const forgetResp = await page.request.post(
      `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/forget`,
      {
        headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
        data: {},
      },
    );
    expect(forgetResp.status(), 'POST /forget must return 200').toBe(200);
    expect(await forgetResp.json(), 'POST /forget must return empty object per spec').toEqual({});

    // Step 5: Incremental sync with since-token from after the leave.
    // The forgotten room MUST NOT appear in rooms.join, rooms.leave, or rooms.invite.
    // EXPECTED FAILURE: MVP forget_room is a no-op — room stays in rooms.leave.
    const afterForgetSync = await page.request.get(`${ELEMENT_URL}/_matrix/client/v3/sync`, {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      params: { since: sinceAfterLeave, timeout: '0' },
    });
    expect(afterForgetSync.status(), 'GET /sync after forget must return 200').toBe(200);
    const afterForgetBody = await afterForgetSync.json();

    expect(
      afterForgetBody.rooms?.join?.[roomId],
      'GAP-FORGET: room MUST NOT appear in rooms.join after POST /forget (§11.3)',
    ).toBeUndefined();
    expect(
      afterForgetBody.rooms?.leave?.[roomId],
      'GAP-FORGET: room MUST NOT appear in rooms.leave after POST /forget (§11.3). ' +
      'MVP no-op — fix requires GetSyncDelta to filter rooms where user has forgotten.',
    ).toBeUndefined();
    expect(
      afterForgetBody.rooms?.invite?.[roomId],
      'GAP-FORGET: room MUST NOT appear in rooms.invite after POST /forget (§11.3)',
    ).toBeUndefined();
  });
});

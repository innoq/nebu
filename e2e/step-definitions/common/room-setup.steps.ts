/**
 * Common room-setup steps — API-based test setup (Given steps only).
 *
 * Story 9-26 — Phase 1, AC4.
 * Story 9-26a — Fix M-1: dynamic room name uniqueness injected transparently.
 *
 * Matrix spec requirements (from Oracle):
 * - createRoom: MUST use unique room names (timestamp suffix) per scenario
 * - inviteUser: MUST be idempotent (guard against already-member 403)
 * - getApiSession: MUST handle 401 M_UNKNOWN_TOKEN (expired token, must refresh)
 * - All helpers MUST fail loudly on 401 M_MISSING_TOKEN (not swallow)
 * - inviteUser MUST validate user_id format before calling (avoid 400 M_BAD_JSON)
 * - Rate limiting: helpers MUST handle 429 M_LIMIT_EXCEEDED with retry_after_ms
 *
 * NOTE: Template names (e.g. "msg-send-template") must be unique across ALL feature files
 * in a single test run. The module-level Map does not scope by scenario.
 * Each .feature file must use a distinct template name.
 */

import { Given } from '../../fixtures/nebu-fixtures';
import { NEBU_USERS } from '../../fixtures/users';
import { getApiSession, createRoom, inviteUser } from '../../fixtures/dex-auth';
import type { APIRequestContext, Browser } from '@playwright/test';

/**
 * World state: store room IDs and actual names created during setup.
 *
 * M-1 fix: feature files use readable template names (e.g. "msg-send-test").
 * A runtime timestamp suffix is appended transparently so each run gets a
 * unique room name. Subsequent steps look up by template name.
 */
type RoomEntry = { roomId: string; actualName: string };
// IMPORTANT: module-level state — only safe with workers: 1 (see playwright.config.ts).
// With workers > 1 this Map would be shared across parallel scenarios → race conditions.
const roomIdByScenario = new Map<string, RoomEntry>();

/**
 * Resolve the actual (suffixed) room name from a template name.
 * Falls back to the given name if no mapping exists (e.g. UI-created rooms).
 */
export function getActualRoomName(templateName: string): string {
  return roomIdByScenario.get(templateName)?.actualName ?? templateName;
}

/**
 * "Given a room {string} exists and {word} is a member"
 *
 * kai (bot user) creates the room via Matrix API and invites the target user.
 * Room name gets a runtime timestamp suffix for per-scenario uniqueness.
 * Feature files keep the readable template name; the suffix is injected here.
 */
Given(
  'a room {string} exists and {word} is a member',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    roomName: string,
    userName: string
  ) => {
    const target = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!target) {
      throw new Error(`Unknown user "${userName}" in room-setup step.`);
    }

    // M-1: inject timestamp suffix for per-run uniqueness
    const actualName = `${roomName}-${Date.now()}`;

    // kai is the bot user that owns setup rooms
    const kaiSession = await getApiSession(request, NEBU_USERS.kai, browser);
    const { room_id } = await createRoom(request, kaiSession.token, actualName);

    // Invite target user (idempotent: already-member 403 is narrowly swallowed)
    await inviteUser(request, kaiSession.token, room_id, target.matrixId);

    // Store mapping: template name → { roomId, actualName }
    roomIdByScenario.set(roomName, { roomId: room_id, actualName });
  }
);

/**
 * "Given a room {string} exists and {word} is the owner"
 *
 * Alias for kai creating a room — used in room/join.feature Background.
 */
Given(
  'a room {string} exists and {word} is the owner',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    roomName: string,
    _ownerName: string
  ) => {
    const actualName = `${roomName}-${Date.now()}`;
    const kaiSession = await getApiSession(request, NEBU_USERS.kai, browser);
    const { room_id } = await createRoom(request, kaiSession.token, actualName);
    roomIdByScenario.set(roomName, { roomId: room_id, actualName });
  }
);

/**
 * "Given marie is a member of room {string}"
 *
 * Used in messages/receive.feature — kai invites marie after room creation.
 */
Given(
  'marie is a member of room {string}',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    roomName: string
  ) => {
    const kaiSession = await getApiSession(request, NEBU_USERS.kai, browser);
    const entry = roomIdByScenario.get(roomName);
    if (!entry) {
      throw new Error(
        `Room "${roomName}" not found in roomIdByScenario. ` +
        'Ensure "a room ... exists" step runs before "marie is a member of room".'
      );
    }
    await inviteUser(request, kaiSession.token, entry.roomId, NEBU_USERS.marie.matrixId);
  }
);

/**
 * "Given kai has invited {word} to {string}"
 *
 * Used in room/join.feature — explicit invite step.
 */
Given(
  'kai has invited {word} to {string}',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    userName: string,
    roomName: string
  ) => {
    const target = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!target) throw new Error(`Unknown user "${userName}".`);

    const kaiSession = await getApiSession(request, NEBU_USERS.kai, browser);
    const entry = roomIdByScenario.get(roomName);
    if (!entry) {
      throw new Error(
        `Room "${roomName}" not found. Ensure "a room ... exists" step ran first.`
      );
    }
    await inviteUser(request, kaiSession.token, entry.roomId, target.matrixId);
  }
);

export { roomIdByScenario };

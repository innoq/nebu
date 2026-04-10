/**
 * Nebu Matrix SDK Compatibility Smoke Test
 *
 * What this test validates:
 *   - A real matrix-js-sdk client can obtain a Dex OIDC token (authorization code flow),
 *     log in to Nebu, perform an initial sync, create a room, send a message, and receive
 *     the sent event back via the room timeline — using only standard Matrix Client-Server
 *     API endpoints (/_matrix/client/v3/...).
 *
 * How to run locally:
 *   1. Start the dev stack:  make dev   (or docker compose up -d --wait)
 *   2. Run:  make test-matrix-compat
 *
 *   Or manually inside the nebu_default Docker network:
 *     docker run --rm -v $(pwd):/workspace -w /workspace/tests/matrix_compat \
 *       --network=nebu_default \
 *       -e NEBU_MATRIX_URL=http://gateway:8008 \
 *       -e NEBU_DEX_URL=http://dex:5556 \
 *       -e NEBU_TEST_USER=kai@example.com \
 *       -e NEBU_TEST_PASSWORD=changeme \
 *       node:22-alpine \
 *       sh -c "npm ci && node smoke_test.js"
 *
 * Credentials used:
 *   User:     kai@example.com  (instance_admin — full room creation permissions)
 *   Password: changeme
 *   Dex client_id: nebu-gateway  /  client_secret: nebu-dev-secret
 *
 * Exit codes:
 *   0 — all assertions passed
 *   1 — any assertion failure, timeout, or missing environment variable
 */

// ---------------------------------------------------------------------------
// Environment variable guards — FAILING STATE
// The script intentionally exits 1 when required env vars are absent.
// This is the "red" state before the full stack is wired up.
// ---------------------------------------------------------------------------

const MATRIX_URL = process.env.NEBU_MATRIX_URL;
if (!MATRIX_URL) {
  console.error("[smoke] ERROR: NEBU_MATRIX_URL not set");
  process.exit(1);
}

const DEX_URL = process.env.NEBU_DEX_URL;
if (!DEX_URL) {
  console.error("[smoke] ERROR: NEBU_DEX_URL not set");
  process.exit(1);
}

const TEST_USER = process.env.NEBU_TEST_USER ?? "kai@example.com";
const TEST_PASSWORD = process.env.NEBU_TEST_PASSWORD ?? "changeme";

// Dex client registration (matches dev/dex/config.yaml staticClients)
const DEX_CLIENT_ID = "nebu-gateway";
const DEX_CLIENT_SECRET = "nebu-dev-secret";
// redirect_uri MUST match the registered value in Dex even when running inside Docker
const REDIRECT_URI = "http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc";

const SYNC_TIMEOUT_MS = 10_000;
const TIMELINE_TIMEOUT_MS = 10_000;

// ---------------------------------------------------------------------------
// matrix-js-sdk imports (ESM — requires "type": "module" in package.json)
// ---------------------------------------------------------------------------

import * as sdk from "matrix-js-sdk";
import { ClientEvent, SyncState, RoomEvent } from "matrix-js-sdk";

// ---------------------------------------------------------------------------
// Assertion helper (no test framework — plain Node.js)
// ---------------------------------------------------------------------------

function assert(condition, message) {
  if (!condition) {
    console.error(`[smoke] ASSERTION FAILED: ${message}`);
    process.exit(1);
  }
}

// ---------------------------------------------------------------------------
// HTTP helper — thin wrapper around the built-in fetch API (Node.js 18+)
// ---------------------------------------------------------------------------

/**
 * Fetches a URL with the given options. Returns { status, headers, text, json }.
 * Does NOT follow redirects when redirect: "manual" is passed.
 */
async function httpGet(url, opts = {}) {
  const res = await fetch(url, { redirect: "follow", ...opts });
  const text = await res.text();
  return { status: res.status, headers: res.headers, text };
}

async function httpPost(url, body, contentType = "application/x-www-form-urlencoded", opts = {}) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": contentType },
    body,
    redirect: "manual",
    ...opts,
  });
  const text = await res.text();
  return { status: res.status, headers: res.headers, text };
}

// ---------------------------------------------------------------------------
// Step 1: Dex Authorization Code flow (programmatic — no browser, no ROPC)
//
// Mirrors the Go implementation in gateway/test/integration/auth_steps_test.go:
//   1. GET /dex/auth?... → follow redirects → HTML login form
//   2. Extract <form action="..."> POST URL from the HTML
//   3. POST credentials (no redirect follow) → extract `code` from Location header
//   4. POST /dex/token with grant_type=authorization_code → extract id_token
// ---------------------------------------------------------------------------

async function obtainDexIdToken() {
  console.log(`[smoke] Obtaining Dex token for ${TEST_USER}...`);

  // 1. Hit the Dex auth endpoint — follow all redirects — land on the HTML login form
  const authUrl =
    `${DEX_URL}/dex/auth` +
    `?response_type=code` +
    `&client_id=${encodeURIComponent(DEX_CLIENT_ID)}` +
    `&redirect_uri=${encodeURIComponent(REDIRECT_URI)}` +
    `&scope=openid+profile+email+groups` +
    `&state=teststate`;

  const loginPageRes = await httpGet(authUrl);
  assert(loginPageRes.status === 200, `Expected 200 from Dex auth page, got ${loginPageRes.status}`);

  // 2. Extract the <form action="..."> URL from the HTML
  //    Unescape HTML entities: &amp; → &
  const formActionMatch = loginPageRes.text.match(/<form[^>]+action="([^"]+)"/);
  assert(formActionMatch, "Could not find <form action> in Dex login page HTML");
  const formAction = formActionMatch[1].replace(/&amp;/g, "&");

  // Resolve relative form action against the Dex base URL if needed
  const credentialsPostUrl = formAction.startsWith("http") ? formAction : `${DEX_URL}${formAction}`;

  // 3. POST credentials — do NOT follow the redirect; we want the Location header
  const credBody = `login=${encodeURIComponent(TEST_USER)}&password=${encodeURIComponent(TEST_PASSWORD)}`;
  const credRes = await httpPost(credentialsPostUrl, credBody, "application/x-www-form-urlencoded", {
    redirect: "manual",
  });

  // Dex returns a 302 redirect to the redirect_uri with ?code=...
  assert(
    credRes.status === 302 || credRes.status === 303,
    `Expected redirect (302/303) after credentials POST, got ${credRes.status}. Body: ${credRes.text}`
  );

  const location = credRes.headers.get("location");
  assert(location, "No Location header in credentials POST response");

  const locationUrl = new URL(location);
  const code = locationUrl.searchParams.get("code");
  assert(code, `No 'code' query parameter in redirect Location: ${location}`);

  // 4. Exchange the authorization code for tokens
  const tokenBody =
    `grant_type=authorization_code` +
    `&code=${encodeURIComponent(code)}` +
    `&redirect_uri=${encodeURIComponent(REDIRECT_URI)}` +
    `&client_id=${encodeURIComponent(DEX_CLIENT_ID)}` +
    `&client_secret=${encodeURIComponent(DEX_CLIENT_SECRET)}`;

  const tokenRes = await httpPost(`${DEX_URL}/dex/token`, tokenBody, "application/x-www-form-urlencoded", {
    redirect: "follow",
  });

  assert(tokenRes.status === 200, `Token exchange failed — status ${tokenRes.status}: ${tokenRes.text}`);

  let tokenJson;
  try {
    tokenJson = JSON.parse(tokenRes.text);
  } catch (e) {
    assert(false, `Token response is not valid JSON: ${tokenRes.text}`);
  }

  assert(tokenJson.id_token, `No id_token in token response: ${tokenRes.text}`);
  return tokenJson.id_token;
}

// ---------------------------------------------------------------------------
// Step 2: Exchange Dex id_token for a Matrix access_token via /login
// ---------------------------------------------------------------------------

async function matrixLogin(idToken) {
  const loginRes = await httpPost(
    `${MATRIX_URL}/_matrix/client/v3/login`,
    JSON.stringify({ type: "m.login.token", token: idToken }),
    "application/json",
    { redirect: "follow" }
  );

  assert(loginRes.status === 200, `Matrix login failed — status ${loginRes.status}: ${loginRes.text}`);

  let loginJson;
  try {
    loginJson = JSON.parse(loginRes.text);
  } catch (e) {
    assert(false, `Matrix login response is not valid JSON: ${loginRes.text}`);
  }

  assert(loginJson.access_token, `No access_token in Matrix login response: ${loginRes.text}`);
  assert(loginJson.user_id, `No user_id in Matrix login response: ${loginRes.text}`);

  console.log(`[smoke] Matrix login successful. user_id=${loginJson.user_id}`);
  return { accessToken: loginJson.access_token, userId: loginJson.user_id };
}

// ---------------------------------------------------------------------------
// Main smoke test flow
// ---------------------------------------------------------------------------

async function main() {
  // --- Step 1+2: Obtain credentials ---
  const idToken = await obtainDexIdToken();
  const { accessToken, userId } = await matrixLogin(idToken);

  // --- Step 3: Create MatrixClient ---
  const client = sdk.createClient({
    baseUrl: MATRIX_URL,
    accessToken,
    userId,
    // No local store; no crypto — plain text smoke test
  });

  // --- Step 4: Start client and wait for SyncState.Prepared (timeout 10s) ---
  console.log("[smoke] Starting client and waiting for initial sync...");

  const syncReady = new Promise((resolve, reject) => {
    const timeout = setTimeout(
      () => reject(new Error("Timeout: initial sync did not complete within 10 seconds")),
      SYNC_TIMEOUT_MS
    );
    client.on(ClientEvent.Sync, (state) => {
      if (state === SyncState.Prepared) {
        clearTimeout(timeout);
        resolve();
      }
    });
  });

  // initialSyncLimit: 1 — minimal initial sync for smoke test speed; story spec suggests 10 but 1 is sufficient here
  await client.startClient({ initialSyncLimit: 1 });

  try {
    await syncReady;
  } catch (err) {
    console.error(`[smoke] ERROR: ${err.message}`);
    client.stopClient();
    process.exit(1);
  }

  if (!client.isInitialSyncComplete()) {
    throw new Error('isInitialSyncComplete() returned false after SyncState.Prepared');
  }

  console.log("[smoke] Initial sync complete.");

  // --- Step 5: Create room ---
  console.log("[smoke] Creating room sdk-smoke...");
  const createResponse = await client.createRoom({ name: "sdk-smoke" });
  const roomId = createResponse.room_id;
  assert(roomId, `createRoom response must contain room_id. Got: ${JSON.stringify(createResponse)}`);
  console.log(`[smoke] Room created: ${roomId}`);

  // --- Step 6: Register timeline listener BEFORE sending (to capture the echo) ---
  const messageReceived = new Promise((resolve, reject) => {
    const timeout = setTimeout(
      () => reject(new Error("Timeout: RoomEvent.Timeline not received within 10 seconds")),
      TIMELINE_TIMEOUT_MS
    );
    const timelineHandler = (event, room, toStartOfTimeline) => {
      if (toStartOfTimeline) return; // skip historical events replayed from initial sync
      if (room?.roomId !== roomId) return;
      if (event.getType() !== "m.room.message") return;
      clearTimeout(timeout);
      client.off(RoomEvent.Timeline, timelineHandler);
      resolve(event.getContent().body);
    };
    client.on(RoomEvent.Timeline, timelineHandler);
  });

  // --- Step 7: Send event ---
  console.log("[smoke] Sending message...");
  // Use sendEvent as the safe alternative (sendMessage is deprecated in SDK v34+)
  const sendResponse = await client.sendEvent(roomId, "m.room.message", {
    msgtype: "m.text",
    body: "smoke",
  });
  assert(
    sendResponse.event_id,
    `sendEvent response must contain event_id. Got: ${JSON.stringify(sendResponse)}`
  );
  console.log(`[smoke] Message sent. event_id=${sendResponse.event_id}`);

  // --- Step 8: Wait for the sent event to appear in the room timeline ---
  console.log("[smoke] Waiting for timeline event...");

  let receivedBody;
  try {
    receivedBody = await messageReceived;
  } catch (err) {
    console.error(`[smoke] ERROR: ${err.message}`);
    client.stopClient();
    process.exit(1);
  }

  assert(
    receivedBody === "smoke",
    `Expected timeline event body "smoke", got "${receivedBody}"`
  );
  console.log(`[smoke] Timeline event received. body=${receivedBody}`);

  // --- Step 9: Clean up ---
  client.stopClient();

  // --- Done ---
  console.log("[smoke] smoke test PASSED");
  process.exit(0);
}

main().catch((err) => {
  console.error(`[smoke] Unhandled error: ${err.message}`);
  console.error(err.stack);
  process.exit(1);
});

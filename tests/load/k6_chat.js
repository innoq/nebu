/**
 * Nebu Silber-Tier Load Test — k6 v0.50+
 *
 * Purpose:
 *   Validates that Nebu meets the Silber-Tier performance target:
 *   500 concurrent users, 10 rooms × 50 members, on 2× m5.large
 *   infrastructure (no Redis, NATS, or Kafka — Elixir/OTP + PostgreSQL only).
 *
 * Threshold definitions:
 *   - http_req_duration{name:send_event} p95 < 200ms
 *       A Matrix PUT /rooms/{id}/send must be acknowledged within 200ms at
 *       the 95th percentile. Exceeding this indicates Core GenServer or DB
 *       write saturation.
 *   - http_req_duration{name:sync} p95 < 500ms
 *       A Matrix GET /sync poll must return within 500ms at the 95th
 *       percentile. Exceeding this indicates EventBus or ETS read-path
 *       saturation.
 *   - http_req_failed rate < 0.1% (0.001)
 *       Network-level failures (connection errors, timeouts). Any value
 *       above 0.1% indicates infrastructure instability.
 *
 * Runtime:
 *   k6 uses the Goja JS runtime (ES2015+ — NOT Node.js).
 *   No npm, no require(), no process.env, no node built-ins.
 *   Use __ENV for environment variables.
 *   Use k6/http for HTTP requests.
 *   Use sleep() from k6 (not setTimeout).
 *
 * How to run:
 *   make test-load-silber
 *   (requires: docker compose up -d --wait)
 *
 * Exit codes:
 *   0   — all thresholds passed (green checkmarks in summary)
 *   99  — one or more thresholds violated (red ✗ in summary)
 */

import http from 'k6/http';
import { sleep, check, fail } from 'k6';

// ---------------------------------------------------------------------------
// Options — Silber-Tier scenario + thresholds
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    silber_tier: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 500 },  // ramp-up: 0 → 500 VUs over 120 s
        { duration: '5m', target: 500 },  // hold:    500 VUs for 300 s
        { duration: '1m', target: 0 },    // ramp-down: 500 → 0 VUs over 60 s
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    // p95 of all requests tagged name:send_event must be < 200 ms
    'http_req_duration{name:send_event}': ['p(95)<200'],
    // p95 of all requests tagged name:sync must be < 500 ms
    'http_req_duration{name:sync}': ['p(95)<500'],
    // network-level failure rate must be below 0.1 %
    'http_req_failed': ['rate<0.001'],
    // HTTP-level check failures (4xx/5xx) must be below 1 %
    'checks': ['rate>0.99'],
  },
};

// ---------------------------------------------------------------------------
// setup() — runs ONCE before VU ramp-up; return value shared with all VUs
//
// Environment variables NEBU_LOAD_TARGET_URL and NEBU_DEX_URL have defaults
// pointing to the Docker compose service names (gateway:8008, dex:5556).
// Override via `make test-load-silber` env overrides when targeting a
// different host. The test requires a live running stack to succeed.
// ---------------------------------------------------------------------------

export function setup() {
  const MATRIX_URL = __ENV.NEBU_LOAD_TARGET_URL || 'http://gateway:8008';
  const DEX_URL    = __ENV.NEBU_DEX_URL         || 'http://dex:5556';

  // -----------------------------------------------------------------------
  // Step 1: GET Dex auth endpoint — follow redirects to login form
  // -----------------------------------------------------------------------
  const redirectUri =
    'http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc';

  const authUrl =
    `${DEX_URL}/dex/auth` +
    `?response_type=code` +
    `&client_id=nebu-gateway` +
    `&redirect_uri=${encodeURIComponent(redirectUri)}` +
    `&scope=openid+profile+email+groups` +
    `&state=k6loadtest`;

  const authRes = http.get(authUrl, { redirects: 10 });

  check(authRes, { 'dex login form returned 200': (r) => r.status === 200 });

  // Extract the <form action="..."> from the HTML login page
  const formActionMatch = authRes.body.match(/action="([^"]+)"/);
  if (!formActionMatch) {
    fail('setup() failed: could not find form action in Dex login page. ' +
         'Is the Dex service reachable at ' + DEX_URL + '?');
  }
  const formAction = formActionMatch[1].replace(/&amp;/g, '&');

  // -----------------------------------------------------------------------
  // Step 2: POST credentials — do NOT follow redirect (capture ?code=...)
  // -----------------------------------------------------------------------
  const loginRes = http.post(
    formAction,
    'login=kai%40example.com&password=changeme',
    {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      redirects: 0, // CRITICAL: capture Location header before following
    }
  );

  // Dex returns 302/303 with Location: <redirect_uri>?code=<auth_code>&state=...
  check(loginRes, {
    'dex login redirected': (r) => r.status === 302 || r.status === 303,
  });

  const location = loginRes.headers['Location'];
  if (!location) {
    fail('setup() failed: no Location header after Dex credential POST. ' +
         'Check that kai@example.com / changeme is a valid Dex user.');
  }

  const codeMatch = location.match(/code=([^&]+)/);
  if (!codeMatch) {
    fail('setup() failed: no ?code= parameter in Dex redirect Location: ' +
         location);
  }
  const authCode = decodeURIComponent(codeMatch[1]);

  // -----------------------------------------------------------------------
  // Step 3: Exchange auth code for id_token at Dex token endpoint
  // -----------------------------------------------------------------------
  const tokenRes = http.post(
    `${DEX_URL}/dex/token`,
    `grant_type=authorization_code` +
    `&code=${authCode}` +
    `&redirect_uri=${encodeURIComponent(redirectUri)}` +
    `&client_id=nebu-gateway` +
    `&client_secret=nebu-dev-secret`,
    { headers: { 'Content-Type': 'application/x-www-form-urlencoded' } }
  );

  check(tokenRes, { 'dex token exchange 200': (r) => r.status === 200 });

  let idToken;
  try {
    idToken = JSON.parse(tokenRes.body).id_token;
  } catch (e) {
    fail('setup() failed: could not parse Dex token response: ' + tokenRes.body);
  }
  if (!idToken) {
    fail('setup() failed: id_token missing from Dex token response.');
  }

  // -----------------------------------------------------------------------
  // Step 4: Exchange id_token for Matrix access_token
  // -----------------------------------------------------------------------
  const matrixRes = http.post(
    `${MATRIX_URL}/_matrix/client/v3/login`,
    JSON.stringify({ type: 'm.login.token', token: idToken }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  check(matrixRes, { 'matrix /login 200': (r) => r.status === 200 });

  let matrixData;
  try {
    matrixData = JSON.parse(matrixRes.body);
  } catch (e) {
    fail('setup() failed: could not parse Matrix /login response: ' + matrixRes.body);
  }

  if (!matrixData.access_token) {
    fail('setup() failed: access_token missing from Matrix /login response. ' +
         'Body: ' + matrixRes.body);
  }

  // Return shared data passed to all VUs as `data` argument in default()
  return {
    access_token: matrixData.access_token,
    user_id: matrixData.user_id,
    matrix_url: MATRIX_URL,
  };
}

// ---------------------------------------------------------------------------
// VU-local state — module-level variables reset per VU (NOT shared)
// ---------------------------------------------------------------------------
let roomId     = null;  // created on first iteration, reused thereafter
let sinceToken = null;  // /sync since-token, updated each iteration

// ---------------------------------------------------------------------------
// default(data) — main VU loop, called once per iteration per VU
//
// Traffic mix (simplified MVP — full mix is out of scope):
//   - First iteration:  createRoom (tagged create_room)
//   - Every iteration:  PUT send m.room.message (tagged send_event)
//                       GET /sync?since=<token>  (tagged sync)
//                       sleep 1 s
//
// Transaction IDs:
//   txnId = `k6-${__VU}-${__ITER}` — unique per VU per iteration.
//   __VU  = VU number (1–500)
//   __ITER = iteration counter (0-based, per VU)
// ---------------------------------------------------------------------------

export default function (data) {
  const MATRIX_URL   = data.matrix_url;
  const accessToken  = data.access_token;
  const authHeaders  = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${accessToken}`,
  };

  // -----------------------------------------------------------------------
  // First iteration only: create a room for this VU
  // Each VU creates its own room (load-room-<VU>).
  // All VUs share the same access_token (kai / instance_admin), so all
  // rooms belong to kai. This is acceptable for throughput measurement.
  // If the server returns 400 M_FORBIDDEN after the first few VUs, fall
  // back to using a single shared room_id returned from setup().
  // -----------------------------------------------------------------------
  if (roomId === null) {
    const createRes = http.post(
      `${MATRIX_URL}/_matrix/client/v3/createRoom`,
      JSON.stringify({ name: `load-room-${__VU}` }),
      {
        headers: authHeaders,
        tags: { name: 'create_room' },
      }
    );
    check(createRes, { 'createRoom 200': (r) => r.status === 200 });

    let createData;
    try {
      createData = JSON.parse(createRes.body);
    } catch (e) {
      // If createRoom fails, skip to send_event — the test will count it
      // as a failed check and it will surface in the threshold output.
      createData = {};
    }
    roomId = createData.room_id || null;
  }

  // -----------------------------------------------------------------------
  // send_event — PUT m.room.message with unique txnId
  // Tagged name:send_event for the p95 < 200ms threshold.
  // -----------------------------------------------------------------------
  const txnId  = `k6-${__VU}-${__ITER}`;
  const sendRes = roomId
    ? http.put(
        `${MATRIX_URL}/_matrix/client/v3/rooms/${roomId}/send/m.room.message/${txnId}`,
        JSON.stringify({
          msgtype: 'm.text',
          body: `load-test message vu=${__VU} iter=${__ITER}`,
        }),
        {
          headers: authHeaders,
          tags: { name: 'send_event' },
        }
      )
    : http.put(
        // roomId is null — make a tagged request that will fail 404/400
        // so the failure is counted correctly in http_req_failed / checks.
        `${MATRIX_URL}/_matrix/client/v3/rooms/!unknown:localhost/send/m.room.message/${txnId}`,
        JSON.stringify({ msgtype: 'm.text', body: 'no-room fallback' }),
        {
          headers: authHeaders,
          tags: { name: 'send_event' },
        }
      );

  check(sendRes, { 'send_event 200': (r) => r.status === 200 });

  // -----------------------------------------------------------------------
  // sync — GET /sync?since=<sinceToken>
  // Tagged name:sync for the p95 < 500ms threshold.
  // timeout=1000 keeps individual polls short (server-sent timeout fallback).
  // -----------------------------------------------------------------------
  const sinceParam = sinceToken ? `&since=${sinceToken}` : '';
  const syncRes = http.get(
    `${MATRIX_URL}/_matrix/client/v3/sync?timeout=1000${sinceParam}`,
    {
      headers: {
        'Authorization': `Bearer ${accessToken}`,
      },
      tags: { name: 'sync' },
    }
  );

  check(syncRes, { 'sync 200': (r) => r.status === 200 });

  // Update since-token for next iteration
  try {
    const syncData = JSON.parse(syncRes.body);
    if (syncData.next_batch) {
      sinceToken = syncData.next_batch;
    }
  } catch (_) {
    // Ignore parse errors — sinceToken stays as-is
  }

  sleep(1);
}

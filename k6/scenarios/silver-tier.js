/**
 * k6 Load Test — Silver Tier (500 Concurrent Users)
 *
 * Story 13-5 AC4: Validates Nebu under Silver Tier load.
 * Slightly relaxed thresholds vs Gold Tier.
 * Thresholds: p95 latency for send < 800 ms, error rate < 1%.
 *
 * Required environment variables:
 *   BASE_URL      — target gateway URL (default: http://localhost:8008)
 *   TEST_USER     — Matrix localpart (e.g. kai)
 *   TEST_PASSWORD — user password
 *   TEST_ROOM_ID  — existing room ID (e.g. !abc123:localhost)
 *
 * Run:
 *   k6 run k6/scenarios/silver-tier.js \
 *     -e BASE_URL=http://localhost:8008 \
 *     -e TEST_USER=kai \
 *     -e TEST_PASSWORD=changeme \
 *     -e TEST_ROOM_ID='!abc123:localhost'
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// ---------------------------------------------------------------------------
// Custom metrics — Story 13-5 AC2: p50/p95/p99 per endpoint + error rate
// ---------------------------------------------------------------------------
const loginDuration = new Trend("nebu_login_duration", true);
const syncDuration = new Trend("nebu_sync_duration", true);
const sendDuration = new Trend("nebu_send_duration", true);
const loginErrors = new Rate("nebu_login_errors");
const syncErrors = new Rate("nebu_sync_errors");
const sendErrors = new Rate("nebu_send_errors");
const totalRequests = new Counter("nebu_total_requests");

// ---------------------------------------------------------------------------
// Scenario options — Silver Tier: 500 VUs × 5 min
// ---------------------------------------------------------------------------
export const options = {
  scenarios: {
    silver: {
      executor: "constant-vus",
      vus: 500,
      duration: "5m",
    },
  },
  thresholds: {
    // AC4: p95 send latency < 800 ms (relaxed vs Gold)
    "nebu_send_duration{scenario:silver}": ["p(95)<800"],
    // AC4: overall error rate < 1%
    http_req_failed: ["rate<0.01"],
    // Summary convenience thresholds
    "nebu_login_duration{scenario:silver}": ["p(95)<1200"],
    "nebu_sync_duration{scenario:silver}": ["p(95)<1000"],
  },
};

// ---------------------------------------------------------------------------
// Per-VU setup: login once, obtain access_token
// ---------------------------------------------------------------------------
function login(baseUrl, user, password) {
  const serverName = new URL(baseUrl).hostname;
  const userId = `@${user}:${serverName}`;

  // m.login.password — used for dev/test stacks only.
  // Production deployments use OIDC (Authorization Code + PKCE via Dex).
  // Do not use ROPC in production — Dex v2.41+ does not support it.
  const payload = JSON.stringify({
    type: "m.login.password",
    identifier: {
      type: "m.id.user",
      user: userId,
    },
    password: password,
  });

  const params = {
    headers: { "Content-Type": "application/json" },
    tags: { endpoint: "login" },
  };

  const res = http.post(
    `${baseUrl}/_matrix/client/v3/login`,
    payload,
    params
  );

  loginDuration.add(res.timings.duration);
  totalRequests.add(1);

  const ok = check(res, {
    "login: status 200": (r) => r.status === 200,
    "login: has access_token": (r) => {
      try {
        return JSON.parse(r.body).access_token !== undefined;
      } catch {
        return false;
      }
    },
  });

  loginErrors.add(!ok);

  if (!ok) {
    return null;
  }

  return JSON.parse(res.body).access_token;
}

// ---------------------------------------------------------------------------
// Main VU function
// ---------------------------------------------------------------------------
export default function () {
  const baseUrl = __ENV.BASE_URL || "http://localhost:8008";
  const user = __ENV.TEST_USER || "kai";
  const password = __ENV.TEST_PASSWORD || "changeme";
  const roomId = __ENV.TEST_ROOM_ID || "!placeholder:localhost";

  // Login once per VU per iteration start
  const accessToken = login(baseUrl, user, password);
  if (!accessToken) {
    sleep(1);
    return;
  }

  const authHeaders = {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      "Content-Type": "application/json",
    },
  };

  // --- Sync ---
  const syncParams = {
    headers: { Authorization: `Bearer ${accessToken}` },
    tags: { endpoint: "sync" },
    timeout: "10s",
  };

  const syncRes = http.get(
    `${baseUrl}/_matrix/client/v3/sync?timeout=0`,
    syncParams
  );

  syncDuration.add(syncRes.timings.duration);
  totalRequests.add(1);

  const syncOk = check(syncRes, {
    "sync: status 200": (r) => r.status === 200,
    "sync: has next_batch": (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.next_batch !== undefined;
      } catch {
        return false;
      }
    },
  });
  syncErrors.add(!syncOk);

  // --- Send message ---
  const txnId = `${__VU}-${__ITER}-${Date.now()}`;
  const sendPayload = JSON.stringify({
    msgtype: "m.text",
    body: `Silver Tier load test VU ${__VU} iter ${__ITER}`,
  });

  const sendParams = {
    headers: authHeaders.headers,
    tags: { endpoint: "send" },
    timeout: "5s",
  };

  const sendRes = http.put(
    `${baseUrl}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/${txnId}`,
    sendPayload,
    sendParams
  );

  sendDuration.add(sendRes.timings.duration);
  totalRequests.add(1);

  const sendOk = check(sendRes, {
    "send: status 200": (r) => r.status === 200,
    "send: has event_id": (r) => {
      try {
        return JSON.parse(r.body).event_id !== undefined;
      } catch {
        return false;
      }
    },
  });
  sendErrors.add(!sendOk);

  sleep(1);
}

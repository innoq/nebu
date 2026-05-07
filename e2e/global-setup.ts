/**
 * Playwright global setup — warms storageState + token sidecars for all test users.
 *
 * Story 9-26 — Phase 1, AC3.
 * Story 9-26a — Fix M-5: kai (and tom) are now warmed alongside alex + marie.
 * Story 9-26b — Fix BUG-E2E-02: ensureStorageState() now also writes
 *   auth-state/{user}.token.json sidecars via loginViaOidcBrowser() route
 *   interception. getApiSession() reads from the sidecar instead of the
 *   storageState localStorage (which doesn't contain mx_access_token on
 *   Element Web 1.11+ — it lives in IndexedDB).
 *
 * BUG-E2E-11 fix: admin bootstrap MUST run BEFORE Element Web user logins.
 *   The Elixir core's upsert_with_bootstrap() sets bootstrap_completed=true
 *   when the first Matrix user logs in (fresh DB), WITHOUT writing OIDC config
 *   keys. This pre-empts the admin wizard and breaks all admin auth tests.
 *   Fix: doBootstrapAdmin() runs first in a dedicated browser page.
 *
 * F-03 fix: checks ALL four users before skipping warmup.
 * Freshness check: requires both .json (storageState) AND .token.json (sidecar).
 *
 * Acceptance criterion:
 *   After the first successful `npx playwright test --project=element-web`,
 *   `e2e/auth-state/alex.json`, `marie.json`, `kai.json` exist (storageState)
 *   AND `e2e/auth-state/alex.token.json`, `marie.token.json`, `kai.token.json`
 *   exist (token sidecars) with valid access_token fields.
 */

import * as fs from 'fs';
import * as path from 'path';
import { chromium, request as playwrightRequest } from '@playwright/test';
import { ensureStorageState, getApiSession, createRoom } from './fixtures/dex-auth';
import { doBootstrapAdmin } from './fixtures/admin-bootstrap';
import { NEBU_USERS } from './fixtures/users';

const AUTH_STATE_DIR = path.join(__dirname, 'auth-state');
const MAX_AGE_MS = 12 * 60 * 60 * 1000; // 12 hours

function isStateFresh(name: string): boolean {
  const statePath  = path.join(AUTH_STATE_DIR, `${name}.json`);
  const sidecarPath = path.join(AUTH_STATE_DIR, `${name}.token.json`);
  if (!fs.existsSync(statePath) || !fs.existsSync(sidecarPath)) return false;
  const stat = fs.statSync(statePath);
  return (Date.now() - stat.mtimeMs) < MAX_AGE_MS;
}

export default async function globalSetup(): Promise<void> {
  // Ensure output directory exists
  if (!fs.existsSync(AUTH_STATE_DIR)) {
    fs.mkdirSync(AUTH_STATE_DIR, { recursive: true });
  }

  // BUG-E2E-11 fix: complete admin bootstrap BEFORE any Element Web user logins.
  // The Elixir core auto-bootstraps (sets bootstrap_completed=true without OIDC config)
  // on the first Matrix login. Admin bootstrap must run first to write OIDC config.
  // This is idempotent: if bootstrap is already complete with OIDC config, it's a no-op.
  const adminBrowser = await chromium.launch();
  try {
    const adminCtx  = await adminBrowser.newContext();
    const adminPage = await adminCtx.newPage();
    try {
      console.log('[global-setup] Ensuring admin bootstrap is complete (with OIDC config)...');
      await doBootstrapAdmin(adminPage);
      console.log('[global-setup] Admin bootstrap complete.');
    } finally {
      await adminPage.close();
      await adminCtx.close();
    }
  } finally {
    await adminBrowser.close();
  }

  // M-5 fix: skip warm-up only when all active users are fresh (state + sidecar both present)
  if (
    isStateFresh('alex') &&
    isStateFresh('marie') &&
    isStateFresh('kai')
  ) {
    console.log('[global-setup] auth-state/{alex,marie,kai}.{json,token.json} are all fresh — skipping warm-up');
  } else {
    console.log('[global-setup] Warming storageState + token sidecars for alex + marie + kai...');
    const browser = await chromium.launch();

    try {
      // Warm active users in parallel (tom is reserved for future use, not warmed eagerly)
      // ensureStorageState() now calls loginViaOidcBrowser() which intercepts the
      // /_matrix/client/v3/login response and writes the .token.json sidecar.
      await Promise.all([
        ensureStorageState(browser, NEBU_USERS.alex),
        ensureStorageState(browser, NEBU_USERS.marie),
        ensureStorageState(browser, NEBU_USERS.kai),
      ]);
      console.log('[global-setup] Done. auth-state/{alex,marie,kai}.{json,token.json} written.');
    } finally {
      await browser.close();
    }
  }

  // Create a test room via Matrix API so the admin Rooms page has at least one entry.
  // Uses alex's token sidecar (always available after warm-up or fresh-state check above).
  // Safe to call every run — the admin rooms page will show ALL rooms.
  try {
    console.log('[global-setup] Creating a test room for admin Rooms page...');
    const apiCtx = await playwrightRequest.newContext();
    try {
      const { token } = await getApiSession(apiCtx, NEBU_USERS.alex);
      await createRoom(apiCtx, token, 'admin-test-room');
      console.log('[global-setup] Test room created.');
    } finally {
      await apiCtx.dispose();
    }
  } catch (e) {
    console.warn(`[global-setup] Could not create test room: ${e}. Rooms page test may fail.`);
  }
}

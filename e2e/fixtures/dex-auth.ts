/**
 * storageState caching and Matrix API helpers for Dex OIDC sessions.
 *
 * Story 9-26 — Phase 1, AC2 + AC3.
 * Story 9-26a — Fixes M-2, M-4, M-5, m-1, m-9.
 * Story 9-26b — Fixes BUG-E2E-01 + BUG-E2E-02:
 *   Element Web 1.11+ stores mx_access_token in IndexedDB, not localStorage.
 *   Playwright storageState() only captures localStorage + cookies — NOT IndexedDB.
 *   Fix A: each test context performs a fresh OIDC login (no storageState restore).
 *   Fix C: token sidecar file auth-state/{user}.token.json written during login,
 *           used by getApiSession() instead of extractTokenFromStorageState().
 *
 * Matrix spec requirements:
 * - getApiSession MUST handle 401 M_UNKNOWN_TOKEN (expired token, must refresh)
 * - All helpers MUST fail loudly on 401 M_MISSING_TOKEN (not swallow)
 * - Rate limiting: helpers MUST handle 429 M_LIMIT_EXCEEDED with retry_after_ms (max 3 retries)
 * - inviteUser MUST be idempotent: swallow 403 M_FORBIDDEN ONLY when already-member
 * - createRoom: timestamp suffix injected by call-site (room-setup.steps.ts)
 */

import * as fs from 'fs';
import * as path from 'path';
import { type Page, type Browser, type APIRequestContext, type APIResponse } from '@playwright/test';
import { type NebUser, DEX_TEST_PASSWORD } from './users';

const AUTH_STATE_DIR = path.join(__dirname, '..', 'auth-state');

const ELEMENT_URL   = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';
const MATRIX_BASE   = process.env.NEBU_MATRIX_BASE ?? 'http://localhost:8008';

// ─────────────────────────────────────────────────────────────────────────────
// Internal: token sidecar helpers
// ─────────────────────────────────────────────────────────────────────────────

type TokenSidecar = {
  access_token: string;
  user_id: string;
  written_at: string;
};

function sidecarPath(user: NebUser): string {
  return path.join(AUTH_STATE_DIR, `${user.name}.token.json`);
}

function writeTokenSidecar(user: NebUser, accessToken: string, userId: string): void {
  if (!fs.existsSync(AUTH_STATE_DIR)) {
    fs.mkdirSync(AUTH_STATE_DIR, { recursive: true });
  }
  const sidecar: TokenSidecar = {
    access_token: accessToken,
    user_id: userId,
    written_at: new Date().toISOString(),
  };
  fs.writeFileSync(sidecarPath(user), JSON.stringify(sidecar, null, 2), 'utf-8');
}

function readTokenSidecar(user: NebUser): { token: string; userId: string } | null {
  const p = sidecarPath(user);
  if (!fs.existsSync(p)) return null;
  try {
    const sidecar = JSON.parse(fs.readFileSync(p, 'utf-8')) as TokenSidecar;
    if (sidecar.access_token && sidecar.user_id) {
      return { token: sidecar.access_token, userId: sidecar.user_id };
    }
  } catch {
    // Corrupted sidecar — treat as missing
  }
  return null;
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal: loginViaOidcBrowser — full browser flow
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Perform a full OIDC Authorization Code + PKCE login through the browser.
 * Navigates to Element Web on the given `page`, fills Dex form, handles consent.
 * On success the page lands on Element Web with .mx_LeftPanel visible.
 *
 * Story 9-26b: accepts an existing `page` instead of creating its own context.
 * This allows the caller's context to hold the live IndexedDB session after login.
 *
 * Also intercepts the /_matrix/client/v3/login response to capture the access
 * token and write it to the token sidecar (Fix C).
 *
 * @param page    Playwright Page (caller owns context + lifecycle)
 * @param user    NebUser — used to write the token sidecar after login
 * @param email   Dex login email
 * @param password Dex login password
 */
export async function loginViaOidcBrowser(
  page: Page,
  user: NebUser,
  email: string,
  password: string,
): Promise<void> {
  // Story 9-26b Fix C: intercept the Matrix /login response to capture access_token
  let capturedToken: string | null = null;
  let capturedUserId: string | null = null;

  await page.route(`${MATRIX_BASE}/_matrix/client/v3/login`, async (route, request) => {
    const response = await route.fetch();
    // Only inspect POST responses (the actual login call)
    if (request.method() === 'POST') {
      try {
        const body = await response.json() as Record<string, unknown>;
        if (typeof body.access_token === 'string') {
          capturedToken  = body.access_token;
          capturedUserId = typeof body.user_id === 'string' ? body.user_id : '';
        }
      } catch {
        // Response wasn't JSON or parse failed — continue without capturing
      }
    }
    await route.fulfill({ response });
  });

  // N-22: wrap all login steps in try/finally so the route handler is always removed,
  // even if an early exception occurs (e.g. network error, assertion failure).
  try {
    await page.goto(ELEMENT_URL);

    // Click "Sign in" link on the Element welcome screen
    await page.getByRole('link', { name: /sign in|anmelden/i })
      .waitFor({ state: 'visible', timeout: 15_000 });
    await page.getByRole('link', { name: /sign in|anmelden/i }).click();

    // Wait for "Continue with SSO" button
    await page.getByRole('button', { name: /continue with sso|mit sso|weiter mit sso/i })
      .waitFor({ state: 'visible', timeout: 15_000 });
    await page.getByRole('button', { name: /continue with sso|mit sso|weiter mit sso/i }).click();

    // Dex login page
    await page.waitForURL(/dex.*\/auth/i, { timeout: 20_000 });
    await page.locator('input[name="login"]').fill(email);
    await page.locator('input[name="password"]').fill(password);
    await page.locator('button[type="submit"]').click();

    // Dex may show a consent / grant access screen (only on first login per client)
    const consentBtn = page.getByRole('button', { name: /grant access|allow|approve|confirm/i });
    if (await consentBtn.isVisible({ timeout: 6_000 }).catch(() => false)) {
      await consentBtn.click();
    }

    // Wait for redirect back to Element Web
    await page.waitForURL(/localhost:7070/, { timeout: 30_000 });

    // Element may show a key-setup dialog on first login — dismiss it
    // m-9 fix: only swallow Timeout races; re-throw real errors
    await Promise.race([
      page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 25_000 }),
      page.getByRole('button', { name: /cancel|abbrechen/i })
        .waitFor({ state: 'visible', timeout: 25_000 }),
    ]).catch((e: Error) => { if (!e.message?.includes('Timeout')) throw e; });

    const cancelBtn = page.getByRole('button', { name: /cancel|abbrechen/i });
    if (await cancelBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await cancelBtn.click();
    }

    // Ensure .mx_LeftPanel is visible before proceeding
    await page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 20_000 });

    // Story 9-26b Fix C: if route interception didn't capture the token (e.g. token came from
    // a cached OIDC session without a fresh /login POST), try reading from the page context.
    // Fallback: check localStorage (legacy Element Web < 1.11) then MatrixClient in-memory.
    if (!capturedToken) {
      const fromPage = await page.evaluate(async () => {
        // Legacy path: localStorage (Element Web < 1.11)
        const legacyToken = localStorage.getItem('mx_access_token');
        if (legacyToken) {
          return {
            access_token: legacyToken,
            user_id: localStorage.getItem('mx_user_id') ?? '',
          };
        }
        // Modern path: read from the active MatrixClient (set on window by Element Web)
        const w = window as unknown as Record<string, unknown>;
        const peg = w['mxMatrixClientPeg'] as { get?: () => { getAccessToken: () => string; getUserId: () => string } } | undefined;
        if (peg?.get) {
          const client = peg.get();
          if (client) {
            return {
              access_token: client.getAccessToken(),
              user_id: client.getUserId(),
            };
          }
        }
        return null;
      });
      if (fromPage?.access_token) {
        capturedToken  = fromPage.access_token;
        capturedUserId = fromPage.user_id;
      }
    }

    // Write token sidecar for use by getApiSession()
    if (capturedToken) {
      // N-16: validate that userId is non-empty before writing sidecar
      const finalUserId = capturedUserId ?? '';
      if (!finalUserId) {
        throw new Error(
          `loginViaOidcBrowser: failed to capture user_id for ${user.name}. ` +
          'Element Web may have changed its session storage format.'
        );
      }
      writeTokenSidecar(user, capturedToken, finalUserId);
    } else {
      console.warn(
        `[loginViaOidcBrowser] Could not capture access_token for "${user.name}". ` +
        'getApiSession() will need to re-authenticate.'
      );
    }
  } finally {
    // N-22: always remove the route intercept, even if an exception occurred above
    await page.unrouteAll({ behavior: 'ignoreErrors' });
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// ensureStorageState — cached OIDC session (storageState + token sidecar)
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Ensure a valid storageState JSON file and token sidecar exist for the given user.
 *
 * Story 9-26b: opens a temporary context + page, runs loginViaOidcBrowser (which
 * writes the token sidecar), saves the storageState from that context, then closes
 * the page. The storageState JSON is kept for legacy use; the token sidecar is what
 * getApiSession() actually reads.
 *
 * Returns the path to the cached storageState file (for backwards compatibility
 * — callers that previously passed this to browser.newContext() should now use
 * the fresh-login fixture approach instead, but the path is still returned).
 */
export async function ensureStorageState(browser: Browser, user: NebUser): Promise<string> {
  // Ensure auth-state dir exists
  if (!fs.existsSync(AUTH_STATE_DIR)) {
    fs.mkdirSync(AUTH_STATE_DIR, { recursive: true });
  }

  const statePath = path.join(AUTH_STATE_DIR, `${user.name}.json`);

  if (fs.existsSync(statePath)) {
    const stat = fs.statSync(statePath);
    const ageMs = Date.now() - stat.mtimeMs;
    const twelveHours = 12 * 60 * 60 * 1000;
    // Also check that the token sidecar exists alongside the state file
    const sidecarExists = fs.existsSync(sidecarPath(user));
    if (ageMs < twelveHours && sidecarExists) {
      return statePath;
    }
  }

  // Perform full OIDC login in a temporary context so IndexedDB is live
  const ctx  = await browser.newContext();
  const page = await ctx.newPage();
  try {
    await loginViaOidcBrowser(page, user, user.email, DEX_TEST_PASSWORD);
    // Persist storageState (captures cookies + localStorage for completeness)
    await ctx.storageState({ path: statePath });
  } finally {
    await page.close();
    await ctx.close();
  }

  return statePath;
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal: retry helper for 429 M_LIMIT_EXCEEDED
// ─────────────────────────────────────────────────────────────────────────────

async function retryOnRateLimit(
  fn: () => Promise<APIResponse>,
  maxRetries = 3,
): Promise<APIResponse> {
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    const resp = await fn();
    if (resp.status() !== 429) {
      return resp;
    }
    if (attempt === maxRetries) {
      const body = await resp.json() as Record<string, unknown>;
      throw new Error(
        `[Matrix 429] M_LIMIT_EXCEEDED after ${maxRetries} retries: ${JSON.stringify(body)}`
      );
    }
    // Respect retry_after_ms from Matrix spec
    const body = await resp.json() as Record<string, unknown>;
    const retryAfterMs = typeof body.retry_after_ms === 'number' ? body.retry_after_ms : 1_000;
    await new Promise<void>((resolve) => setTimeout(resolve, retryAfterMs));
  }
  throw new Error('[retryOnRateLimit] unreachable');
}

// ─────────────────────────────────────────────────────────────────────────────
// extractTokenFromStorageState — DEPRECATED (BUG-E2E-02)
// ─────────────────────────────────────────────────────────────────────────────

/**
 * @deprecated Story 9-26b: Element Web 1.11+ stores mx_access_token in IndexedDB,
 * not localStorage. This function always throws "No mx_access_token found" on modern
 * Element Web. Use readTokenSidecar() / getApiSession() which reads from the sidecar
 * file written during loginViaOidcBrowser() instead.
 *
 * Kept as dead code for reference. Will be removed in a future story.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars
async function _extractTokenFromStorageState_DEPRECATED(user: NebUser): Promise<{ token: string; userId: string }> {
  const statePath = path.join(AUTH_STATE_DIR, `${user.name}.json`);
  if (!fs.existsSync(statePath)) {
    throw new Error(
      `[getApiSession] No storageState found for user "${user.name}" at ${statePath}. ` +
      'Run ensureStorageState() first (or start the stack and run globalSetup).'
    );
  }

  const raw = fs.readFileSync(statePath, 'utf-8');
  const state = JSON.parse(raw) as {
    origins?: Array<{
      origin: string;
      localStorage?: Array<{ name: string; value: string }>;
    }>;
  };

  const elementOrigin = ELEMENT_URL;
  const allOrigins = (state.origins ?? []).map(o => o.origin);
  const matching = (state.origins ?? []).filter(o => o.origin === elementOrigin);

  if (matching.length === 0) {
    throw new Error(
      `[getApiSession] No storageState found for Element Web origin '${elementOrigin}' ` +
      `in "${statePath}". Origins found: [${allOrigins.join(', ')}]. ` +
      'Delete auth-state/ and re-run the test (globalSetup will re-warm).'
    );
  }

  for (const origin of matching) {
    for (const item of origin.localStorage ?? []) {
      if (item.name === 'mx_access_token') {
        let userId = '';
        for (const other of origin.localStorage ?? []) {
          if (other.name === 'mx_user_id') {
            userId = other.value;
            break;
          }
        }
        return { token: item.value, userId };
      }
    }
  }

  throw new Error(
    `[getApiSession] No mx_access_token found in storageState for "${user.name}" ` +
    `(origin: ${elementOrigin}). ` +
    'The storageState may be expired or corrupted. Delete auth-state/ and re-run.'
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// getApiSession
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Get a Matrix API session token for the given user.
 *
 * Story 9-26b Fix C: reads the access token from the token sidecar
 * (auth-state/{user}.token.json), written during loginViaOidcBrowser().
 * This replaces extractTokenFromStorageState() which failed on Element Web 1.11+
 * because mx_access_token is stored in IndexedDB, not localStorage.
 *
 * Self-healing: if sidecar is missing or expired and `browser` is provided,
 * runs ensureStorageState() to re-warm the sidecar, then retries once.
 *
 * MUST handle:
 * - 401 M_UNKNOWN_TOKEN: clear sidecar, re-warm if browser provided, else throw
 * - 401 M_MISSING_TOKEN: fail loudly (do not swallow)
 * - 429 M_LIMIT_EXCEEDED: retry after retry_after_ms
 */
export async function getApiSession(
  request: APIRequestContext,
  user: NebUser,
  browser?: Browser,
): Promise<{ token: string; userId: string }> {
  // Story 9-26b: read from token sidecar instead of storageState localStorage
  let extracted = readTokenSidecar(user);

  if (!extracted) {
    if (browser) {
      // Self-heal: warm the session via browser (writes sidecar) and retry
      await ensureStorageState(browser, user);
      extracted = readTokenSidecar(user);
    }
    if (!extracted) {
      throw new Error(
        `getApiSession: no valid session for '${user.name}'. ` +
        'Run globalSetup first (or pass a browser for self-healing).'
      );
    }
  }

  const { token, userId } = extracted;

  if (!token) {
    throw new Error(
      `[getApiSession] 401 M_MISSING_TOKEN: no token found for "${user.name}". ` +
      'This is a setup error — run ensureStorageState() to warm the auth cache.'
    );
  }

  // Validate the token with a cheap whoami call
  const whoamiResp = await retryOnRateLimit(() =>
    request.get(`${MATRIX_BASE}/_matrix/client/v3/account/whoami`, {
      headers: { Authorization: `Bearer ${token}` },
    })
  );

  if (whoamiResp.status() === 401) {
    const body = await whoamiResp.json() as Record<string, unknown>;
    const errCode = String(body.errcode ?? '');

    if (errCode === 'M_MISSING_TOKEN') {
      throw new Error(
        `[getApiSession] 401 M_MISSING_TOKEN for "${user.name}". ` +
        'This is a setup error — the token is absent. Run ensureStorageState() first.'
      );
    }

    // M_UNKNOWN_TOKEN (expired) — clear the stale sidecar
    const p = sidecarPath(user);
    if (fs.existsSync(p)) {
      fs.unlinkSync(p);
    }
    // Also clear the storageState JSON so ensureStorageState re-logs in
    const statePath = path.join(AUTH_STATE_DIR, `${user.name}.json`);
    if (fs.existsSync(statePath)) {
      fs.unlinkSync(statePath);
    }

    // M-5: if browser provided, re-warm and retry once
    if (browser) {
      await ensureStorageState(browser, user);
      const retried = readTokenSidecar(user);
      if (!retried) {
        throw new Error(
          `[getApiSession] Re-warm succeeded but sidecar still missing for "${user.name}".`
        );
      }
      return { token: retried.token, userId: retried.userId };
    }

    throw new Error(
      `[getApiSession] 401 M_UNKNOWN_TOKEN for "${user.name}": token is expired. ` +
      'The auth cache has been cleared. Re-run the test (globalSetup will re-warm).'
    );
  }

  if (!whoamiResp.ok()) {
    throw new Error(
      `[getApiSession] Unexpected status ${whoamiResp.status()} from whoami for "${user.name}".`
    );
  }

  return { token, userId };
}

// ─────────────────────────────────────────────────────────────────────────────
// createRoom
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Create a Matrix room via the CS API.
 *
 * M-1: timestamp suffix is injected by the caller (room-setup.steps.ts).
 * This function no longer validates the suffix — it trusts the caller.
 */
export async function createRoom(
  request: APIRequestContext,
  token: string,
  roomName: string,
): Promise<{ room_id: string }> {
  if (!token) {
    throw new Error('[createRoom] 401 M_MISSING_TOKEN: no token provided.');
  }

  const resp = await retryOnRateLimit(() =>
    request.post(`${MATRIX_BASE}/_matrix/client/v3/createRoom`, {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      data: {
        name: roomName,
        preset: 'private_chat',
        visibility: 'private',
      },
    })
  );

  if (!resp.ok()) {
    const body = await resp.json() as Record<string, unknown>;
    throw new Error(
      `[createRoom] Failed to create room "${roomName}": ` +
      `${resp.status()} ${JSON.stringify(body)}`
    );
  }

  const body = await resp.json() as { room_id: string };
  return { room_id: body.room_id };
}

// ─────────────────────────────────────────────────────────────────────────────
// inviteUser
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Invite a user to a Matrix room via the CS API.
 *
 * M-4 fix: idempotency guard is narrowed — only swallows 403 when the error
 * message indicates the user is already a member. All other M_FORBIDDEN cases
 * (insufficient power level, restricted room, etc.) are re-thrown.
 *
 * MUST validate user_id format before calling.
 * MUST handle 429 M_LIMIT_EXCEEDED with retry_after_ms.
 */
export async function inviteUser(
  request: APIRequestContext,
  token: string,
  roomId: string,
  userId: string,
): Promise<void> {
  if (!token) {
    throw new Error('[inviteUser] 401 M_MISSING_TOKEN: no token provided.');
  }

  // Validate Matrix user_id format: @localpart:server
  if (!userId.match(/^@[^:]+:[^:]+$/)) {
    throw new Error(
      `[inviteUser] Invalid user_id "${userId}". Format: @localpart:server (e.g. @alex:localhost).`
    );
  }

  const resp = await retryOnRateLimit(() =>
    request.post(`${MATRIX_BASE}/_matrix/client/v3/rooms/${roomId}/invite`, {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      data: { user_id: userId },
    })
  );

  if (resp.status() === 403) {
    const body = await resp.json() as Record<string, unknown>;
    const errcode = String(body.errcode ?? '');

    // M-4: narrow idempotency — only swallow when error indicates already a member
    const alreadyMember = /already (in|a member|joined)|user already in room/i.test(
      String(body.error ?? '')
    );
    if (alreadyMember) {
      return; // idempotent: user already a member
    }

    // Re-throw all other 403 cases (power level, restricted room, etc.)
    throw new Error(
      `[inviteUser] inviteUser failed: ${resp.status()} ${errcode} — ${body.error}`
    );
  }

  if (!resp.ok()) {
    const body = await resp.json() as Record<string, unknown>;
    throw new Error(
      `[inviteUser] Failed to invite "${userId}" to "${roomId}": ` +
      `${resp.status()} ${JSON.stringify(body)}`
    );
  }
}

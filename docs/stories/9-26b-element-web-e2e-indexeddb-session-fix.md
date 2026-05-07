---
story_id: 9-26b
title: "Element Web E2E — Fix IndexedDB Session Restoration"
type: bugfix
severity: critical
epic: 9
status: ready-for-dev
parent_story: 9-26
security_review: not-needed
created: 2026-05-07
---

## Summary

Sub-story of 9-26. Fixes BUG-E2E-01 and BUG-E2E-02, both caused by the same architectural gap: Element Web 1.11+ stores `mx_access_token` AES-encrypted in **IndexedDB**, not localStorage. Playwright's `storageState()` captures localStorage + cookies only — NOT IndexedDB. This means:

1. `ensureStorageState()` caches a state file that looks valid but is missing the actual token — Element Web on next load shows the welcome screen instead of the room list (BUG-E2E-01, 6 failing scenarios).
2. `extractTokenFromStorageState()` searches `state.origins[].localStorage` for `mx_access_token` — it doesn't exist there, so `getApiSession()` always throws "No mx_access_token found" (BUG-E2E-02, 2 failing scenarios + all room-setup steps).

## Root Cause

Element Web stores session in IndexedDB under key `matrix-js-sdk:crypto-store:account` (and related). The `mx_access_token` localStorage key is a legacy fallback only present in Element Web < 1.11. Nebu's docker-compose uses a recent Element Web image.

## Fix Approach

**Option A (Recommended): Full OIDC login per test, no storageState caching**

Remove the storageState caching strategy entirely. Each test context performs a fresh OIDC login via `loginViaOidc()` (or equivalent). This is slower (~15–20s per test) but correct and robust.

Mitigate slowness: use `globalSetup` to perform one login per user and store the resulting `page.context().storageState()` + a separately-captured access token string (not from localStorage, but from the network response during login). The access token is used by `getApiSession()` for API calls.

**Option B: Inject token via page.evaluate into IndexedDB**

After `loginViaOidc()` in `ensureStorageState()`, capture the access token from the network response (intercept `/_matrix/client/v3/login` or read `page.context().storageState()` + check what Element actually puts in localStorage vs IndexedDB). Then restore via `page.evaluate()` that writes to IndexedDB directly. Complex and brittle.

**Option C: Extract token from network during login**

During `loginViaOidc()`, intercept the `/_matrix/client/v3/login` POST response to capture `access_token` and store it in a separate sidecar file `auth-state/{user}.token.json`. `getApiSession()` reads the sidecar file instead of `extractTokenFromStorageState()`.

**Recommended implementation (Option A + C combined):**

1. `loginViaOidc()` in `dex-auth.ts`: intercept the Matrix `/_matrix/client/v3/login` response (or the final redirect callback) to capture `access_token` and `user_id`. Store as `auth-state/{user}.token.json` sidecar.
2. `extractTokenFromStorageState()`: replace with `readTokenSidecar(user)` that reads `auth-state/{user}.token.json`.
3. `ensureStorageState()`: after login, also write the token sidecar. Keep the storageState JSON for context loading (it still captures cookies + non-encrypted localStorage which may help).
4. For contexts that need the storageState (Element Web page sessions): perform a fresh login each time instead of restoring storageState. Mitigate: log in once in globalSetup and keep the browser context open for reuse, or use the token injection approach for API calls and accept full login for UI tests.

The simplest correct fix that doesn't over-engineer:
- **For API calls** (`getApiSession`): Use `loginViaOidc()` headlessly once per test session and capture the token from the Matrix login response. Store in sidecar.
- **For UI tests** (`ensureStorageState`, context fixture): Do a full `loginViaOidc()` per context creation — cached per test run by globalSetup, fresh per suite. Accept the 15–20s cost for the first login.

## Files to Change

- `e2e/fixtures/dex-auth.ts` — primary change:
  - `loginViaOidcBrowser()`: intercept `/_matrix/client/v3/login` response to capture token
  - `ensureStorageState()`: write token sidecar after login
  - Replace `extractTokenFromStorageState()` with `readTokenSidecar(user)`
  - `getApiSession()`: use `readTokenSidecar()` instead of `extractTokenFromStorageState()`
- `e2e/fixtures/nebu-fixtures.ts` — if context strategy changes
- `e2e/global-setup.ts` — may need adjustment for sidecar warmup
- `e2e/.gitignore` — add `auth-state/*.token.json` if sidecar files contain real tokens

## Acceptance Tests

1. **BUG-E2E-01 fix** — `login.feature` all 3 scenarios pass: SSO Login shows room list, Logout shows welcome screen, Cached session no redirect.
2. **BUG-E2E-02 fix** — `getApiSession(request, NEBU_USERS.kai, browser)` returns a valid access token without throwing "No mx_access_token found".
3. **Room-setup steps** — `messages/send.feature` and `messages/receive.feature` pass end-to-end.
4. **Double run** — suite still passes twice in a row (M-1 regression test still green).

## DoD

- All 8 element-web scenarios green
- `getApiSession()` returns valid token for all 4 users
- No `TimeoutError` on `.mx_LeftPanel` across all test contexts

---
story_id: 9-26a
title: "Element Web E2E Suite — Code Review Fix (9-26 MAJOR/MINOR)"
type: bugfix
severity: high
epic: 9
status: ready-for-dev
parent_story: 9-26
security_review: not-needed
created: 2026-05-07
---

## Summary

Sub-story of 9-26. Addresses all MAJOR and critical MINOR findings from the 9-26 code review before the parent story is committed. No new features — only fixes to the E2E test infrastructure created in 9-26.

All changes are confined to `e2e/` (fixtures, step-definitions, feature files, Makefile). No production Go/Elixir code is touched.

---

## MAJOR Findings to Fix

### M-1 — Hardcoded timestamps violate the unique-room-name invariant

**Location:** All `.feature` files under `e2e/features/element/` that reference `1746614400`.

**Problem:** Frozen timestamp `1746614400` satisfies the regex guard but collides on second DB-run. `createRoom` returns `409 M_ROOM_IN_USE` on re-runs without `docker compose down -v`.

**Fix:** Replace hardcoded timestamps in feature files with Gherkin `<roomName>` or `<timestamp>` placeholder variables, resolved at step-definition level via `Date.now()`. Alternative: use a `Scenario Outline` or a step-level random suffix injected in `room-setup.steps.ts` so feature files stay readable but room names are unique per run.

Specifically:
- `room-setup.steps.ts` `Given 'a room {string} exists and {word} is a member'`: append a runtime suffix to the given roomName before calling `createRoom()`
- Store the *actual* room name (with suffix) in `roomIdByScenario` keyed by the *template* name
- All subsequent steps that reference the template name must look up the actual name via the same map
- Feature files keep the readable `"msg-send-test"` names; the uniqueness is injected transparently

### M-2 — `extractTokenFromStorageState` not scoped to Element Web origin

**Location:** `e2e/fixtures/dex-auth.ts` lines 168–205.

**Fix:** Filter `state.origins` entries to only the Element Web origin before reading `localStorage`. Use `ELEMENT_URL` env var or default `'http://localhost:7070'` as the expected origin. Throw clearly if no matching origin or no `mx_access_token` found.

### M-4 — `inviteUser` swallows ALL `M_FORBIDDEN` (over-broad idempotency)

**Location:** `e2e/fixtures/dex-auth.ts` lines 368–383.

**Fix:** Replace `errcode === 'M_FORBIDDEN'` with a narrower already-member check:
- Only swallow when `error` message matches `/already (in|a member|joined)|user already in room/i`
- Re-throw all other M_FORBIDDEN (power level, restricted room, etc.)

### M-5 — `kai` never warmed by `globalSetup`; `getApiSession` has no self-heal path

**Location:** `e2e/global-setup.ts` lines 47–50, `e2e/fixtures/dex-auth.ts` lines 254–262.

**Fix (two-part):**
1. `globalSetup`: warm `kai` (and optionally `tom`) alongside alex+marie.
2. `getApiSession`: on "No storageState found" error (kai.json absent), accept an optional `browser` parameter and call `ensureStorageState(browser, user)` as self-heal. Room-setup steps already receive `browser` via Playwright fixtures — pass it through.

### M-6 — Makefile `test-e2e-element` does not ensure kai is warmed

**Location:** `Makefile` — already addressed by M-5 globalSetup fix. Verify `make test-e2e-element` succeeds on a clean DB.

### M-7 — `secondContext` hardcoded to marie; multi-user step doesn't guard against mismatch

**Location:** `e2e/fixtures/nebu-fixtures.ts` lines 50–58, `e2e/step-definitions/element/messages.steps.ts` lines 95–123.

**Fix:** Add a guard in `Given {word} is logged in via Element Web in a second browser context` that throws if `userName !== 'marie'` (since `secondContext` is always marie's context). Mirror of the F-02 fix in `auth.steps.ts`. Document that for a non-marie second user a new fixture would be needed. Error message should say: "second browser context is pre-configured for 'marie'; pass 'marie' or add a new fixture for '{userName}'".

### M-8 — `$test.skip()` runtime semantics in `stack-health.steps.ts` unverified

**Location:** `e2e/step-definitions/common/stack-health.steps.ts` lines 17–46.

**Fix:** Verify empirically: run the step with the stack DOWN and check the test report shows `skipped` not `failed`. If not:
- Replace `($test as any).skip(...)` with `test.skip()` (raises the Playwright SkipError) imported from `../../fixtures/nebu-fixtures`
- Or use `$testInfo.skip(reason)` from `playwright-bdd`'s `$testInfo` fixture

The acceptance test for this fix: run `make test-e2e-element` with Element Web stopped — all 8 scenarios must show `skipped`, not `failed`.

### M-9 — AC10 assertion is weaker than its AC (message visible ≠ message delivered)

**Location:** `e2e/features/element/messages/send.feature` lines 22–23, `e2e/step-definitions/element/messages.steps.ts` lines 70–83.

**Fix:** After asserting `.mx_EventTile` visible, also wait for the "sending" indicator to disappear. Element Web uses `.mx_EventTile` class additions for states. Add:
```typescript
// Wait for local-echo to become confirmed (not in "sending" state)
const sendingIndicator = page.locator('.mx_EventTile.mx_EventTile_sending', { hasText: message });
await expect(sendingIndicator).not.toBeVisible({ timeout: 15_000 });
```

Or equivalently: assert presence of `.mx_ReadReceiptGroup` or `.mx_EventTile_verified` on the tile.

---

## MINOR Findings to Fix

### m-1 — Password `'changeme'` duplicated in 4 files
Extract `DEX_TEST_PASSWORD = 'changeme'` to `e2e/fixtures/users.ts` and import everywhere.

### m-2 — Unused imports: `Browser` in `login.steps.ts`, `ensureStorageState`/`NEBU_USERS` in `login.steps.ts`, `tom` in `users.ts`
Remove unused imports and the unused `tom` export (or document why it's reserved).

### m-6 — Negative visibility timeout 20s → use 5–8s for `not.toBeVisible` assertions
**Location:** `e2e/step-definitions/common/assertions.steps.ts` line 54. Change timeout to `7_000`.

### m-9 — Silent `.catch(() => {})` in `loginViaOidcBrowser` swallows real errors
**Location:** `e2e/fixtures/dex-auth.ts` lines 71–75. Replace bare catch-all with typed catch that only handles known navigation races.

### m-13 — `ensureStorageState` called twice for marie in `messages.steps.ts`
**Location:** `e2e/step-definitions/element/messages.steps.ts` line 105. Remove the redundant `ensureStorageState(browser, user)` call — the `secondContext` fixture already warmed it.

### m-14 — `playwright-report/` not in `.gitignore`
**Location:** `e2e/.gitignore`. Add `playwright-report/` entry.

---

## Acceptance Tests

### Tests (all are verification tests — pass when fixes are correct):

1. **M-1 (unique room names)** — Run `make test-e2e-element` twice in a row on the same running stack (no `docker compose down -v` between runs). Second run must pass, not fail with room-collision errors.

2. **M-5 (kai self-warm)** — Delete `e2e/auth-state/` (if present), then run `make test-e2e-element` on a fresh DB. All room/join, room/leave, messages/send, messages/receive scenarios must run (not fail on "No storageState found for kai").

3. **M-4 (narrow M_FORBIDDEN)** — (Unit-level check in dex-auth.ts) If `inviteUser` is called and the server returns `403 M_FORBIDDEN` with error `"You don't have permission to invite users"`, the helper must throw (not swallow). If it returns `403 M_FORBIDDEN` with `"user already in room"`, it must not throw.

4. **M-7 (marie guard)** — If a feature file tries `Given tom is logged in via Element Web in a second browser context`, the test must fail immediately with a clear error (`"second browser context is pre-configured for 'marie'"`), not silently log in marie as tom.

5. **M-8 (skip semantics)** — With Element Web stopped: `make test-e2e-element` output shows all 8 scenarios as `skipped`, zero as `failed`.

6. **M-9 (message delivered)** — `messages/send.feature` scenario: assert the sending-indicator disappears within 15s after message appears in timeline.

---

## DoD

- All 9 MAJOR findings resolved
- Named MINOR findings (m-1, m-2, m-6, m-9, m-13, m-14) resolved
- `make test-e2e-element` passes twice in a row on the same stack (M-1 regression test)
- No new MAJOR findings in code review of 9-26a diff

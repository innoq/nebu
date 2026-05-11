---
id: 7-14
security_review: not-needed
---

# Story 7.14: Gherkin Admin UI Smoke Flows — User Deactivate + Room Archive via UI

Status: ready-for-dev

## Story

As a developer,
I want Playwright BDD smoke-flow tests that exercise the full browser-level user-deactivation and room-archive flows from login through confirmation to result page,
so that regressions in server-side rendering, confirm-dialog wiring, form POST handlers, and PRG redirect+flash logic are caught in CI before they reach production.

## Context / Background

Stories 7.1–7.13 have delivered the complete Admin UI: master-detail layout, user list/detail/deactivation, room list/detail/archive, compliance, config, audit log, and axe accessibility scans. The existing specs (`user-role.spec.ts`, `room-detail.spec.ts`) each test isolated component interactions. This story adds a dedicated smoke spec that covers the **end-to-end business flows** that matter most for day-to-day admin operation: the full deactivate-a-user journey and the full archive-a-room journey, written in a BDD `describe/scenario` style that reads like a business flow.

These are real Playwright tests (no `test.skip`) running against the live dev stack. The stub layer is in-memory and mutations persist for the duration of the server process — the tests must save the original status and restore it in `test.afterEach` via the inverse action (POST to the reactivate/unarchive route, or directly via re-navigating the form).

## Acceptance Criteria

1. **New spec file `e2e/tests/features/admin/smoke-flows.spec.ts` exists** — contains two `test.describe` blocks, one per business flow, written in a Gherkin-flavoured BDD style (`describe` = scenario group, each `test` name starts with `Given`, `When`, or `Then` or uses full flow naming). No `test.skip`.

2. **Flow 1 — Admin deactivates a user:**
   - Admin logs in via OIDC Authorization Code + PKCE (same `loginAsAdmin` helper pattern)
   - Navigates to `/admin/users`
   - Clicks on the usr-001 (Alice Müller) row in the list
   - The detail panel opens (visible `section[role="region"]`)
   - Clicks the "Deactivate" button to open the confirm dialog (`dialog[role="alertdialog"]` becomes visible)
   - Clicks the Confirm/submit button inside the dialog
   - Page redirects (PRG) to `/admin/users/usr-001` with a `flash=` query param
   - Flash banner (`div[role="alert"]`) is visible and contains "deactivated" (case-insensitive)
   - The status badge in the detail panel shows "Inactive" (the normalised display value)

3. **Flow 2 — Admin archives a room:**
   - Admin logs in via OIDC Authorization Code + PKCE
   - Navigates to `/admin/rooms`
   - Clicks on the room-001 (General) row in the list
   - The detail panel opens
   - Clicks the "Archive room" button to open the confirm dialog
   - Clicks the Confirm/submit button inside the dialog
   - Page redirects (PRG) to `/admin/rooms/room-001` with a `flash=` query param
   - Flash banner is visible and contains "archived" (case-insensitive)
   - The status badge in the detail panel shows "Inactive"

4. **State cleanup via `test.afterEach`** — because the stub layer is in-memory and mutations persist within a server process lifetime, the spec restores the original stub state after each flow test. Restoration is done by navigating to the detail page and verifying current state, then posting to the inverse endpoint if needed. Specifically:
   - After user-deactivation flow: POST to `/admin/users/usr-001/reactivate` if that route exists; otherwise navigate to the page and verify the "Reactivate" button is present (documenting the limitation in a code comment). If a reactivate route is not yet wired, use `usr-003` (Carla Reiter, `user`, `active`) as the deactivation target instead — it avoids colliding with the `user-role.spec.ts` tests that also touch `usr-001`.
   - After room-archive flow: POST to `/admin/rooms/room-001/unarchive` if that route exists; otherwise document the limitation. If unarchive is not yet wired, use `room-002` (Engineering, `active`) as the archive target instead.
   - **Decision rule:** check whether a reactivate/unarchive route is registered in `gateway/cmd/gateway/main.go` before writing the afterEach. If not present, use the alternative stub IDs and note the cleanup limitation clearly.

5. **Spec is self-contained** — uses its own `loginAsAdmin` function (same pattern as all other admin specs, no shared helper file). No changes to `playwright.config.ts`. Runs under the existing `e2e/` Playwright harness (`workers: 1`, serial execution).

6. **All tests pass green** against the live dev stack (`make dev` + bootstrap complete). TypeScript check (`cd e2e && npx tsc --noEmit`) passes with no errors.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`Admin deactivates user — full flow`** — Playwright (`e2e/tests/features/admin/smoke-flows.spec.ts`)
   - Given: full dev stack running (`make dev`), bootstrap complete, admin logged in via OIDC Authorization Code + PKCE (kai@example.com / changeme)
   - When: navigate to `/admin/users`, click the usr-001 row, click "Deactivate", click confirm in the dialog
   - Then: page redirects to `/admin/users/usr-001?flash=...`, flash banner contains "deactivated", status badge shows "Inactive"

2. **`Admin archives room — full flow`** — Playwright (`e2e/tests/features/admin/smoke-flows.spec.ts`)
   - Given: full dev stack running, bootstrap complete, admin logged in
   - When: navigate to `/admin/rooms`, click the room-001 row, click "Archive room", click confirm in the dialog
   - Then: page redirects to `/admin/rooms/room-001?flash=...`, flash banner contains "archived", status badge shows "Inactive"

3. **`State is restored after each test`** — Playwright `test.afterEach` hook
   - Given: deactivation or archive has occurred during the preceding test
   - When: afterEach runs
   - Then: the stub entry is restored to original status (via inverse POST or documented limitation), leaving the stack clean for subsequent tests

## Tasks / Subtasks

- [ ] Task 1: Analyse existing routes and decide target IDs (AC: 4)
  - [ ] 1.1 Check `gateway/cmd/gateway/main.go` for `POST /admin/users/{userId}/reactivate` and `POST /admin/rooms/{roomId}/unarchive` routes
  - [ ] 1.2 If reactivate route exists → use `usr-001` as target; if not → use `usr-003` (Carla Reiter) as target
  - [ ] 1.3 If unarchive route exists → use `room-001` as target; if not → use `room-002` (Engineering) as target
  - [ ] 1.4 Document the chosen target IDs in a comment at the top of the spec

- [ ] Task 2: Write failing smoke-flows spec FIRST (AC: 1, 2, 3, 5)
  - [ ] 2.1 Create `e2e/tests/features/admin/smoke-flows.spec.ts` with `loginAsAdmin` helper (identical pattern to `user-role.spec.ts`)
  - [ ] 2.2 Write `test.describe('Flow: Admin deactivates a user', ...)` with `test.afterEach` cleanup and one end-to-end test
  - [ ] 2.3 Write `test.describe('Flow: Admin archives a room', ...)` with `test.afterEach` cleanup and one end-to-end test
  - [ ] 2.4 Run the spec against the live stack to confirm it is failing (stub flow not yet verified end-to-end)

- [ ] Task 3: Implement any missing pieces needed for the tests to pass (AC: 2, 3, 4)
  - [ ] 3.1 Confirm the list row click navigates to the detail URL (`/admin/users/usr-001`) — existing feature from Story 7.2, but verify in browser
  - [ ] 3.2 Confirm "Deactivate" button exists in the detail panel when `status=active` — existing from Story 7.7, verify selector `button:has-text("Deactivate")`
  - [ ] 3.3 Confirm "Archive room" button exists in the room detail panel — existing from Story 7.9, verify selector `button:has-text("Archive room")`
  - [ ] 3.4 Confirm the confirm dialog submit wires correctly and the PRG redirect lands with `flash=` param
  - [ ] 3.5 Confirm the status badge renders "Inactive" (not "deactivated"/"archived") — normalisation is in `toUserRowData` / `toRoomRowData` in `users.go` / `rooms.go`
  - [ ] 3.6 If reactivate/unarchive routes are missing and needed for cleanup: add them to `gateway/cmd/gateway/main.go` + add handler stubs (mirror of `DeactivateUserHandler`/`ArchiveRoomHandler` setting status back to "active")

- [ ] Task 4: Final verification (AC: 5, 6)
  - [ ] 4.1 Run `cd e2e && npx playwright test tests/features/admin/smoke-flows.spec.ts --reporter=list` — all tests green
  - [ ] 4.2 Run `cd e2e && npx tsc --noEmit` — no TypeScript errors
  - [ ] 4.3 Verify no interference with parallel specs: since `workers: 1`, execution is serial; confirm `smoke-flows.spec.ts` leaves stub state clean after each test

## Dev Notes

### File to Create

`e2e/tests/features/admin/smoke-flows.spec.ts` — this is the only file added by this story (plus optional route handlers if reactivate/unarchive are missing).

### Auth Helper Pattern

Every admin spec duplicates `loginAsAdmin` (no shared helper file). Follow the exact pattern from `user-role.spec.ts`:

```typescript
import { test, expect, Page } from '@playwright/test';

async function loginAsAdmin(page: Page): Promise<void> {
  await page.goto('/admin/login/start');
  await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });
  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();
  const grantBtn = page.locator(
    'button[type="submit"]:has-text("Grant"), button[type="submit"]:has-text("Confirm")'
  );
  if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
    await grantBtn.first().click();
  }
  await page.waitForURL(/\/admin\//, { timeout: 15_000 });
}
```

### Selector Reference (from existing specs)

| What | Selector |
|---|---|
| User list rows | `[role="option"]` (renders as `<a role="option">` in master-detail list) |
| Room list rows | `[role="option"]` |
| Detail panel | `section[role="region"]` |
| Deactivate trigger button | `button:has-text("Deactivate")` |
| Archive trigger button | `button:has-text("Archive room")` |
| Confirm dialog | `dialog[role="alertdialog"]` |
| Confirm submit | `dialog[role="alertdialog"] button[type="submit"]` |
| Flash banner | `div[role="alert"]` |
| Status badge | look for badge text inside the detail panel, e.g. `section[role="region"] .badge` containing "Inactive" |

The status badge selector may need inspection. The badge component renders as `<span class="badge ...">Inactive</span>` (after `"deactivated"` → `"inactive"` normalisation in `toUserRowData`). Use `page.locator('section[role="region"]').getByText('Inactive')` as a fallback if the `.badge` class selector is flaky.

### In-Memory Stub State and Test Isolation

The `stubUsers` and `stubRooms` slices in `gateway/internal/admin/stubs.go` are package-level variables. `DeactivateUserHandler` and `ArchiveRoomHandler` mutate them in-place. Because the Playwright tests run against a real server process (`make dev`), stub state persists across test runs within one server boot.

**Risk**: if `user-role.spec.ts` runs before `smoke-flows.spec.ts` and leaves `usr-001` in `deactivated` state, the deactivation flow test will fail (button shows "Reactivate" instead of "Deactivate"). Mitigations:
1. Use a different user stub (`usr-003` Carla Reiter) that is not touched by any other spec.
2. Or implement a reactivate route and restore in `afterEach`.

**Recommendation**: use `usr-003` for the deactivation target (safe — no other spec touches it). Use `room-002` for the archive target (safe — `room-detail.spec.ts` only opens the dialog but does NOT submit it, so `room-001` is still `active`; however `room-002` is still safer). Document the choice in a comment.

### Route Check for Reactivate / Unarchive

As of Story 7.7 AC, the Deactivate confirm dialog is wired. The epics spec mentions a "Reactivate" button for deactivated users, but the route may not be registered yet. Check:

```bash
grep -n "reactivate\|unarchive" gateway/cmd/gateway/main.go
```

If missing, add minimal handlers in `gateway/internal/admin/users.go` and `rooms.go`:

```go
// ReactivateUserHandler handles POST /admin/users/{userId}/reactivate.
// Restores Status = "active" in-memory (stub phase, inverse of DeactivateUserHandler).
func (h *UsersHandler) ReactivateUserHandler(w http.ResponseWriter, r *http.Request) {
    userID := r.PathValue("userId")
    for i := range stubUsers {
        if stubUsers[i].ID == userID {
            stubUsers[i].Status = "active"
            break
        }
    }
    http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+reactivated", http.StatusFound)
}
```

Register in `main.go`:
```go
mux.Handle("POST /admin/users/{userId}/reactivate", sessionGuard(http.HandlerFunc(usersHandler.ReactivateUserHandler)))
mux.Handle("POST /admin/rooms/{roomId}/unarchive", sessionGuard(http.HandlerFunc(roomsHandler.UnarchiveRoomHandler)))
```

These handlers are also required by Story 7.7 AC (Reactivate button is mentioned) and Story 7.9 AC (Unarchive button is mentioned), so adding them is within the scope of completing those ACs.

### Spec Skeleton

```typescript
/**
 * Story 7.14: Gherkin Admin UI Smoke Flows
 *
 * Flow 1: Admin deactivates a user (usr-003 Carla Reiter — active, role: user)
 * Flow 2: Admin archives a room (room-002 Engineering — active, private)
 *
 * Target IDs chosen to avoid collision with user-role.spec.ts (usr-001) and
 * room-detail.spec.ts (room-001 dialog-open-only, but room-002 is cleaner).
 *
 * Cleanup: afterEach restores stub state via POST to reactivate/unarchive routes.
 * If those routes are not yet wired, add them (see Dev Notes).
 *
 * Auth: OIDC Authorization Code + PKCE via Dex (kai@example.com / changeme).
 * Prerequisites: make dev running, bootstrap complete.
 */
import { test, expect, Page } from '@playwright/test';

async function loginAsAdmin(page: Page): Promise<void> {
  // ... (same as all other admin specs)
}

test.describe('Flow: Admin deactivates a user', () => {
  const TARGET_USER = 'usr-003'; // Carla Reiter, status: active, role: user

  test.afterEach(async ({ page }) => {
    await loginAsAdmin(page);
    // Restore: POST to reactivate endpoint
    await page.request.post(`/admin/users/${TARGET_USER}/reactivate`);
  });

  test('admin navigates to user detail and deactivates via confirm dialog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');

    // Click the target user row
    await page.locator(`[role="option"][href="/admin/users/${TARGET_USER}"]`).click();

    // Detail panel opens
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // Deactivate button present (user is active)
    await page.locator('button:has-text("Deactivate")').click();

    // Confirm dialog appears
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();

    // Submit the dialog
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // PRG redirect with flash
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });

    // Flash banner mentions deactivation
    await expect(page.locator('div[role="alert"]')).toContainText(/deactivat/i);

    // Status badge shows Inactive
    await expect(page.locator('section[role="region"]')).toContainText('Inactive');
  });
});

test.describe('Flow: Admin archives a room', () => {
  const TARGET_ROOM = 'room-002'; // Engineering, status: active, visibility: private

  test.afterEach(async ({ page }) => {
    await loginAsAdmin(page);
    // Restore: POST to unarchive endpoint
    await page.request.post(`/admin/rooms/${TARGET_ROOM}/unarchive`);
  });

  test('admin navigates to room detail and archives via confirm dialog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Click the target room row
    await page.locator(`[role="option"][href="/admin/rooms/${TARGET_ROOM}"]`).click();

    // Detail panel opens
    const detailPanel = page.locator('section[role="region"]');
    await expect(detailPanel).toBeVisible();

    // Archive room button present (room is active)
    await page.locator('button:has-text("Archive room")').click();

    // Confirm dialog appears
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();

    // Submit the dialog
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();

    // PRG redirect with flash
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });

    // Flash banner mentions archival
    await expect(page.locator('div[role="alert"]')).toContainText(/archiv/i);

    // Status badge shows Inactive
    await expect(page.locator('section[role="region"]')).toContainText('Inactive');
  });
});
```

**Note on `page.request.post` in afterEach**: Playwright's `page.request` uses the browser context and inherits the session cookie. If the session is not active in afterEach (e.g. page was redirected during the test and the context is clean), the POST may hit the `sessionGuard` and redirect to login. In that case, log in first (full `loginAsAdmin`) and then use `page.request.post`. Alternatively, call `page.goto('/admin/login/start')` before the cleanup POST in `afterEach` to ensure an active session.

### Flash URL Pattern

The deactivate handler redirects to:
```
/admin/users/{id}?flash=User+deactivated
```
The archive handler redirects to:
```
/admin/rooms/{id}?flash=Room+archived
```
Match with `/[?&]flash=/` regex (as used in all other specs).

### Status Badge Normalisation

`toUserRowData` in `users.go` maps `"deactivated"` → `"inactive"`. The badge template renders the text "Inactive" (capitalised). Assert with `toContainText('Inactive')` inside the `section[role="region"]` scope.

`toRoomRowData` in `rooms.go` maps `"archived"` → `"inactive"`. Same badge text "Inactive". Same assertion.

### Running the Tests

```bash
# Against live dev stack:
cd e2e && npx playwright test tests/features/admin/smoke-flows.spec.ts --reporter=list

# TypeScript check:
cd e2e && npx tsc --noEmit

# Full integration (all specs):
make test-integration
```

### File Locations Summary

| File | Action |
|---|---|
| `e2e/tests/features/admin/smoke-flows.spec.ts` | CREATE — new spec (primary deliverable) |
| `gateway/internal/admin/users.go` | ADD `ReactivateUserHandler` if missing |
| `gateway/internal/admin/rooms.go` | ADD `UnarchiveRoomHandler` if missing |
| `gateway/cmd/gateway/main.go` | REGISTER reactivate + unarchive routes if missing |

### Cross-Spec Isolation Map

| Spec | User touched | Room touched |
|---|---|---|
| `user-detail.spec.ts` | `usr-001` (display name edit — restored by next navigate) | — |
| `user-role.spec.ts` | `usr-001` (deactivate — NOT restored; risk!) | — |
| `room-detail.spec.ts` | — | `room-001` (archive dialog opened but NOT submitted — safe) |
| `smoke-flows.spec.ts` (this story) | `usr-003` (deactivated + restored) | `room-002` (archived + restored) |

Use `usr-003` and `room-002` to keep isolation clean regardless of test run order.

### Previous Story Patterns

Story 7.13 (`accessibility.spec.ts`) established the pattern of running `loginAsAdmin` in `test.beforeEach` when a single login serves all tests. This story uses a per-test login instead (each test is a full flow and needs a clean browser state from the start of the flow).

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List

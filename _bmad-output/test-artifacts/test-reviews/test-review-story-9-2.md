---
stepsCompleted: ['step-01-load-context', 'step-02-discover-tests', 'step-03-quality-evaluation', 'step-04-generate-report']
lastStep: 'step-04-generate-report'
lastSaved: '2026-05-04'
story: '9-2'
qualityScore: 78
qualityGrade: 'B'
recommendation: 'Approve with comments'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-2-admin-ui-users-api-integration.md'
  - 'e2e/tests/features/admin/users-api-integration.spec.ts'
  - 'gateway/internal/admin/users_todo_test.go'
  - 'gateway/internal/admin/users.go'
  - '.claude/skills/bmad-testarch-test-review/resources/knowledge/test-quality.md'
  - '.claude/skills/bmad-testarch-test-review/resources/knowledge/fixture-architecture.md'
  - '.claude/skills/bmad-testarch-test-review/resources/knowledge/network-first.md'
  - '.claude/skills/bmad-testarch-test-review/resources/knowledge/selector-resilience.md'
---

# Test Quality Review — Story 9.2: Admin UI Users API Integration

**Review Date:** 2026-05-04  
**Reviewer:** TEA (Test Architect) — Test-Review Mode  
**Scope:** Staged test files for Story 9.2  
**Quality Score:** 78 / 100 — Grade: **B (Acceptable)**  
**Recommendation:** **Approve with comments** (no blockers; MINOR items noted)

---

## Files Reviewed

| File | Type | Lines |
|---|---|---|
| `e2e/tests/features/admin/users-api-integration.spec.ts` | Playwright E2E | 357 |
| `gateway/internal/admin/users_todo_test.go` | Go unit (grep-based) | 53 |

---

## AC Coverage Matrix

| AC | Description | Test(s) | Status |
|---|---|---|---|
| AC1 | Navigate to `/admin/users` → real users from DB (not `stubUsers`) | 3 Playwright tests in `AC1` describe block | COVERED |
| AC2 | Deactivate → `POST /deactivate` called, UI reflects updated status | 2 Playwright tests in `AC2` describe block | COVERED |
| AC3 | Role update → `POST /roles` called | 1 Playwright test in `AC3` describe block | COVERED |
| AC4 | Reactivate → `POST /reactivate` called, status → active | 2 Playwright tests in `AC4` describe block | COVERED |
| AC5 | Zero `TODO(epic-6)` in `users.go` | `TestNoTODOEpic6InUsersGo` in `users_todo_test.go` | COVERED |

**All 5 ACs have at least one test. No MAJOR coverage gap.**

---

## Executive Summary

The test suite is well-structured and intentional. The dual-sentinel pattern for AC1 (checking both "Alice Müller" AND "Bob Wagner" as absent) is excellent defensive design. The Go AC5 test is robust and uses `runtime.Caller(0)` correctly. The `afterEach` cleanup hooks in AC2/AC4 are appropriate for mutation tests against a live DB. The tests correctly target the OIDC Authorization Code + PKCE pattern.

**Key Strengths:**
- Full AC coverage, all 5 criteria have at least one test
- Red-phase documentation in file header is exemplary (explains exactly why each test fails before implementation)
- AC1 uses a negative sentinel strategy (stub detection) that is more reliable than a positive "prove real data is shown" approach
- AC5 Go test resolves the target file path via `runtime.Caller(0)` — portable across build environments
- `afterEach` cleanup in AC2/AC3/AC4 prevents DB pollution between test runs
- PRG redirect assertions (`toHaveURL` with regex including `?flash=`) are well-formed

**Key Weaknesses / Issues:**
- `loginAsAdmin` is duplicated verbatim from `rooms-page.spec.ts` instead of extracted to a shared helper (MINOR)
- `isVisible({ timeout: 2_000 }).catch(() => false)` in the login helper is a conditional flow control pattern (MINOR)
- The `beforeEach` in AC2/AC3/AC4 calls `loginAsAdmin(page)` AND each test immediately calls it again — double login overhead per test (MINOR/INFO)
- Direct `page.request.post(...)` in `afterEach` for AC2/AC4 cleanup bypasses CSRF middleware — may fail silently in environments where CSRF enforcement is active (MINOR)
- AC2/AC3 use `a[role="option"]` to find the first user row — this is an implicit dependency on the list being non-empty; no explicit wait or assertion that the list is populated before extracting the `href` (MINOR)
- The AC5 Go test only checks the negative (absence of `TODO(epic-6)`); it does not verify the gRPC methods are actually called — that is acceptable given the ATDD scope, but noted as INFO

---

## Quality Criteria Assessment

| Criterion | Status | Violations |
|---|---|---|
| AC Coverage | PASS | 0 |
| No hard waits | PASS | 0 (`waitForTimeout` not used) |
| Determinism | WARN | 1 (conditional in login helper) |
| Isolation / cleanup | WARN | 1 (CSRF bypass in afterEach POST) |
| Selector resilience | WARN | 2 (implicit role="option" dependency, text-based button selectors) |
| Fixture/helper architecture | WARN | 1 (loginAsAdmin duplicated) |
| Network-first | PASS | (no explicit routing needed for PRG flows) |
| Test length | PASS | (357 lines — acceptable for 4 ACs + 9 tests) |
| Assertions explicit | PASS | all `expect()` in test bodies |
| Go test robustness | PASS | 0 |

---

## Findings

### MINOR-1 — `loginAsAdmin` duplicated from `rooms-page.spec.ts`

**Location:** `e2e/tests/features/admin/users-api-integration.spec.ts`, lines 35–51  
**Story note:** The story explicitly says "reuse `loginAsAdmin(page)` from `rooms-page.spec.ts`" — but the current implementation copies it inline rather than importing from a shared module.

**Problem:** Any change to the OIDC login flow (new grant screen, URL pattern change) must be updated in both files. At 2 copies now, this will grow.

**Recommended fix:**
```typescript
// e2e/tests/fixtures/helpers.ts  (or a new admin-auth.ts)
export async function loginAsAdmin(page: Page): Promise<void> {
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
Then in each spec file:
```typescript
import { loginAsAdmin } from '../../fixtures/helpers';
```

**Note:** `e2e/tests/fixtures/helpers.ts` already exists. The function should be added there.

---

### MINOR-2 — Conditional flow control in `loginAsAdmin`

**Location:** `e2e/tests/features/admin/users-api-integration.spec.ts`, lines 43–48  
**Pattern:**
```typescript
if (await grantBtn.first().isVisible({ timeout: 2_000 }).catch(() => false)) {
  await grantBtn.first().click();
}
```

**Problem:** `isVisible().catch(() => false)` silently swallows errors. If the button fails to resolve for a *different* reason (not "not visible"), the catch returns `false` and the test continues silently. Per `test-quality.md`, conditionals controlling test flow and try/catch for flow control are anti-patterns. This is a known Dex "first-time grant" screen issue — the correct approach is to handle it in session state.

**Context:** This pattern is already in `rooms-page.spec.ts` (existing, established pattern). Flagging it here so it is on record. **Not a blocker** given it is the project-wide pattern, but a shared fix would remove it from all specs simultaneously.

**Recommended fix (longer term):** Use Playwright storage state to save the post-grant auth session and skip the Dex grant screen on subsequent test runs:
```typescript
// globalSetup: login once, save storageState to playwright/.auth/admin.json
// Each test: test.use({ storageState: 'playwright/.auth/admin.json' })
```
This also eliminates the double-login overhead (see MINOR-3).

---

### MINOR-3 — Double `loginAsAdmin` call per test (beforeEach + test body)

**Location:** AC2 describe block, `beforeEach` (line 122) + each test (lines 146, 173); AC3 (lines 213, 246); AC4 (lines 290, 308, 314, 341)

**Problem:** `beforeEach` calls `loginAsAdmin(page)`, then immediately each test calls `loginAsAdmin(page)` again. The Dex OIDC flow involves browser redirects, form submissions, and URL waits — doing this twice doubles login latency per test. With 9 tests, this is ~18 full OIDC round-trips.

**Root cause:** The `beforeEach` in AC2/AC3/AC4 navigates to `/admin/users` to discover the `targetUserId`, then the test body re-authenticates because it cannot assume the session is still valid. However, since the `page` fixture is shared within a describe block, the session from `beforeEach` should still be active in the test body — the second `loginAsAdmin` call is redundant.

**Recommended fix:** Remove the `loginAsAdmin` calls from test bodies in AC2/AC3/AC4 where `beforeEach` already established the session. Only call it in `afterEach` (for the cleanup step) since `afterEach` runs in the same page context.

```typescript
test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page);              // login once
  await page.goto('/admin/users');
  // ... resolve targetUserId
});

test('deactivate redirects with flash and status badge shows Inactive', async ({ page }) => {
  // No loginAsAdmin here — session is already established by beforeEach
  await page.goto(`/admin/users/${targetUserId}`);
  // ... rest of test
});
```

---

### MINOR-4 — `page.request.post(...)` in `afterEach` bypasses CSRF

**Location:** AC2 `afterEach` (line 141), AC4 `afterEach` (lines 306–310)

**Problem:** The cleanup uses `page.request.post('/admin/users/${targetUserId}/reactivate')` directly via the Playwright API request context. The admin mutation handlers (`ReactivateUserHandler`, `DeactivateUserHandler`) are behind the CSRF middleware (`csrf(sessionGuard(...))`). A raw `page.request.post` does NOT include the CSRF token from the form, so in an environment with full CSRF enforcement active, this request will be rejected with `403 Forbidden`.

**Why this matters:** If CSRF enforcement returns a 403 for the cleanup POST, the `afterEach` silently fails (no assertion on the response), leaving the user in a deactivated state. Subsequent AC4 tests would then find an already-deactivated user in `beforeEach`, making the test non-idempotent.

**Current mitigation:** The test has no assertion on the cleanup response (`await page.request.post(...)` — result discarded). This is a silent failure path.

**Recommended fix:** Navigate to the user detail page and click the Reactivate button via the UI (which includes the CSRF token via the form), or fetch the CSRF token from the page context before sending the cleanup request:

```typescript
test.afterEach(async ({ page }) => {
  if (!targetUserId) return;
  // Use UI-based cleanup to ensure CSRF token is included
  await loginAsAdmin(page);
  await page.goto(`/admin/users/${targetUserId}`);
  const reactivateBtn = page.locator('button:has-text("Reactivate")');
  if (await reactivateBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
    await reactivateBtn.click();
    await page.waitForURL(/flash=/, { timeout: 8_000 });
  }
});
```

Alternatively, if direct HTTP cleanup is desired, read the CSRF token from a meta tag or cookie before the request.

---

### MINOR-5 — `a[role="option"]` selector dependency on non-empty list

**Location:** AC2 `beforeEach` (line 126), AC3 `beforeEach` (line 219), AC4 `beforeEach` (lines 291–298)

**Problem:** `page.locator('a[role="option"]').first()` with `expect(...).toBeVisible({ timeout: 10_000 })` assumes at least one user row exists on the list page. If the list is empty (e.g., immediately after a clean test DB setup with no users yet), the `beforeEach` throws `"No user rows found on /admin/users"` and all dependent tests fail with a non-obvious error.

This selector is also CSS-role-based (`a[role="option"]`) which couples to the ARIA role of the sidebar link component. Per `selector-resilience.md`, ARIA roles are tier-2 selectors and acceptable, but the `role="option"` on an anchor is semantically unusual (options belong to listbox/combobox) and may change.

**Recommended fix:** Add an explicit assertion that the list has loaded (e.g., wait for the heading or a loading indicator to disappear) before extracting the href, and document the DB pre-condition requirement:

```typescript
test.beforeEach(async ({ page }) => {
  await loginAsAdmin(page);
  await page.goto('/admin/users');
  // Ensure list has loaded (heading visible = handler returned)
  await expect(page.locator('h1')).toBeVisible();
  const firstRow = page.locator('a[role="option"]').first();
  await expect(firstRow).toBeVisible({ timeout: 10_000 });
  // ...
});
```

---

### INFO-1 — AC5 Go test verifies absence of marker but not presence of gRPC calls

**Location:** `gateway/internal/admin/users_todo_test.go`

**Observation:** The test correctly asserts that `TODO(epic-6)` is absent from `users.go`. This is a static code quality gate, not a behavioral test. The gRPC integration itself is verified by the Playwright tests (AC1–AC4) against the live stack.

**Impact:** Zero. This is by design and appropriate for this story. Behavioral correctness of the gRPC calls is covered at the E2E level. No action required.

---

### INFO-2 — AC2 test "after deactivation the users list reflects the status change on reload" is a duplicate of the primary deactivate test

**Location:** Lines 170–190

**Observation:** This persistence test partially duplicates the flow of the primary deactivate test (lines 144–168): it also clicks Deactivate, waits for the PRG redirect, then navigates away and back. The incremental value (proving persistence vs. the first test) is real and intentional. No action required — just noting it for potential consolidation in a future refactor.

---

## Score Calculation

| Category | Points |
|---|---|
| Starting score | 100 |
| MINOR-1 (login helper duplication) | -5 |
| MINOR-2 (conditional flow in login) | -3 |
| MINOR-3 (double login per test) | -3 |
| MINOR-4 (CSRF bypass in afterEach) | -5 |
| MINOR-5 (selector implicit dependency) | -2 |
| Bonus: Full AC coverage | +2 |
| Bonus: Red-phase documentation | +3 |
| Bonus: afterEach cleanup present | +2 |
| Bonus: Go test uses runtime.Caller(0) | +2 |
| Bonus: explicit PRG URL assertions | +3 |
| **Final Score** | **78 / 100** |

**Grade: B (Acceptable)**

---

## Gate Decision

**Recommendation: Approve with comments**

None of the findings are MAJOR or block story completion. All 5 ACs have test coverage. The CSRF bypass in `afterEach` (MINOR-4) is the most operationally relevant finding — it could cause silent cleanup failures in production-like environments — but it does not invalidate the test logic itself.

**Required before marking done:** None (no MAJOR findings).  
**Recommended follow-up items (can be tracked as tech-debt):**
1. Extract `loginAsAdmin` to `e2e/tests/fixtures/helpers.ts` — affects `rooms-page.spec.ts`, `users-api-integration.spec.ts`, and any future admin specs
2. Fix `afterEach` cleanup to use CSRF-safe requests (UI flow or token extraction)
3. Remove redundant `loginAsAdmin` calls from test bodies where `beforeEach` already established the session

---

## Knowledge Base References

- `test-quality.md` — No hard waits, no conditionals, explicit assertions, test length limits
- `fixture-architecture.md` — Pure function → fixture, shared helper extraction
- `selector-resilience.md` — Selector hierarchy: data-testid > ARIA > text > CSS
- `network-first.md` — Intercept before navigate; deterministic waits

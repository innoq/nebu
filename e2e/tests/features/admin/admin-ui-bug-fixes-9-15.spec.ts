/**
 * Story 9.15: Admin UI Bug-Fixes — Select-Dropdown Visibility, Compliance Button Contrast,
 * Room Fallback Name
 *
 * Playwright E2E acceptance test scaffolds written FIRST (before implementation code),
 * per the Nebu ATDD standard (CLAUDE.md Gate 1).
 *
 * RED PHASE: All tests in this file FAIL until Story 9.15 is implemented.
 *
 * AC1 — Select dropdowns have bg-base-200 text-base-content classes
 *   TestSelectDropdownHasBackgroundClasses:
 *     Fails because filter_bar.html <select> class is "select select-bordered select-sm"
 *     (missing bg-base-200 and text-base-content).
 *   TestComplianceSelectDropdownHasBackgroundClasses:
 *     Fails because compliance.html <select id="status-filter"> also lacks these classes.
 *
 * AC2 — Compliance Approve/Reject buttons have btn-outline class
 *   TestComplianceApproveButtonIsOutline:
 *     Fails because compliance.html Approve button class is "btn btn-xs btn-success"
 *     (missing btn-outline).
 *   TestComplianceRejectButtonIsOutline:
 *     Fails because compliance.html Reject button class is "btn btn-xs btn-error"
 *     (missing btn-outline).
 *
 * AC3 — Rooms with empty name show "(Direct Chat · N members)" fallback
 *   TestRoomWithoutNameShowsFallback:
 *     Fails because rooms.html master list renders {{ .Name }} directly — room-006
 *     (stub with Name="") renders a blank span instead of the fallback text.
 *   TestRoomDetailTitleWithoutNameShowsFallback:
 *     Fails because rooms.html detail_title renders {{ .ActiveRoom.Name }} directly —
 *     /admin/rooms/room-006 shows an empty breadcrumb/title instead of the fallback.
 *   TestRoomWithNameDisplaysName:
 *     Regression guard — passes even before the fix (room-001 "General" is always present).
 *     Listed here to make the regression intent explicit.
 *
 * Test data prerequisite: stubs.go must contain room-006 with Name="" and MemberCount=2.
 * This was added to stubs.go as part of Story 9.15 ATDD setup.
 *
 * Auth pattern: OIDC Authorization Code + PKCE via Dex.
 * Never use ROPC (grant_type=password) — not supported by Dex v2.41+.
 * Prerequisites: make dev running, bootstrap complete.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — OIDC Authorization Code + PKCE via Dex
// Identical to all other admin specs (smoke-flows.spec.ts, ux-polish-9-13.spec.ts, etc.)
// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
// AC1 — Select dropdowns have bg-base-200 and text-base-content classes
// ---------------------------------------------------------------------------

test.describe('AC1: Select dropdowns have correct background/text classes', () => {
  // RED: Fails because filter_bar.html renders:
  //   class="select select-bordered select-sm"
  // Missing: bg-base-200  text-base-content
  test('Users page Role-Filter select has bg-base-200 and text-base-content classes', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');

    // The role filter select rendered by filter_bar.html component
    const roleSelect = page.locator('select[name="role"]');
    await expect(roleSelect).toBeVisible();

    const classList = await roleSelect.getAttribute('class') ?? '';
    expect(
      classList,
      'Role-Filter select must have bg-base-200 class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('bg-base-200');
    expect(
      classList,
      'Role-Filter select must have text-base-content class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('text-base-content');
  });

  // RED: Fails because filter_bar.html renders:
  //   class="select select-bordered select-sm"
  // The Rooms page uses the same filter_bar component — missing classes.
  test('Rooms page Visibility-Filter select has bg-base-200 and text-base-content classes', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    const visibilitySelect = page.locator('select[name="visibility"]');
    await expect(visibilitySelect).toBeVisible();

    const classList = await visibilitySelect.getAttribute('class') ?? '';
    expect(
      classList,
      'Visibility-Filter select must have bg-base-200 class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('bg-base-200');
    expect(
      classList,
      'Visibility-Filter select must have text-base-content class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('text-base-content');
  });

  // RED: Fails because compliance.html line 20 renders:
  //   class="select select-bordered select-sm"
  // Missing: bg-base-200  text-base-content
  test('Compliance page Status-Filter select has bg-base-200 and text-base-content classes', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    const statusSelect = page.locator('#status-filter');
    await expect(statusSelect).toBeVisible();

    const classList = await statusSelect.getAttribute('class') ?? '';
    expect(
      classList,
      'Status-Filter select (id=status-filter) must have bg-base-200 class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('bg-base-200');
    expect(
      classList,
      'Status-Filter select (id=status-filter) must have text-base-content class (AC1) — ' +
      `got: "${classList}"`
    ).toContain('text-base-content');
  });
});

// ---------------------------------------------------------------------------
// AC2 — Compliance Approve/Reject buttons have btn-outline class
// ---------------------------------------------------------------------------

test.describe('AC2: Compliance action buttons use btn-outline for contrast', () => {
  // RED: Fails because compliance.html line 66 renders:
  //   class="btn btn-xs btn-success"
  // Missing: btn-outline
  test('Approve button in Compliance table has btn-outline and btn-success classes', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    // At least one pending request exists in stubComplianceRequests (cr-001, cr-002)
    // so the Approve button is rendered.
    const approveBtn = page.locator('button[type="submit"]:has-text("Approve")').first();
    await expect(approveBtn).toBeVisible({ timeout: 5_000 });

    const classList = await approveBtn.getAttribute('class') ?? '';
    expect(
      classList,
      'Approve button must have btn-outline class (AC2) — ' +
      `got: "${classList}"`
    ).toContain('btn-outline');
    expect(
      classList,
      'Approve button must retain btn-success class (AC2) — ' +
      `got: "${classList}"`
    ).toContain('btn-success');
  });

  // RED: Fails because compliance.html line 70 renders:
  //   class="btn btn-xs btn-error"
  // Missing: btn-outline
  test('Reject button in Compliance table has btn-outline and btn-error classes', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    const rejectBtn = page.locator('button[type="submit"]:has-text("Reject")').first();
    await expect(rejectBtn).toBeVisible({ timeout: 5_000 });

    const classList = await rejectBtn.getAttribute('class') ?? '';
    expect(
      classList,
      'Reject button must have btn-outline class (AC2) — ' +
      `got: "${classList}"`
    ).toContain('btn-outline');
    expect(
      classList,
      'Reject button must retain btn-error class (AC2) — ' +
      `got: "${classList}"`
    ).toContain('btn-error');
  });
});

// ---------------------------------------------------------------------------
// AC3 — Rooms with empty name show "(Direct Chat · N members)" fallback
// ---------------------------------------------------------------------------

test.describe('AC3: Rooms without a name show fallback text in master list and detail title', () => {
  // RED: Fails because rooms.html line 29 renders:
  //   <span class="font-medium flex-1 truncate">{{ .Name }}</span>
  // room-006 has Name="" so the span is rendered empty (no visible text).
  // After the fix, it renders: (Direct Chat · 2 members)
  test('Room with empty name shows "(Direct Chat · N members)" in master list', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // room-006 has Name="" and MemberCount=2 in stubs.go (added for Story 9.15 ATDD).
    // The fallback text "(Direct Chat · 2 members)" must appear in the rooms list.
    // We use the Unicode middle-dot (·) that the template renders via &middot;
    await expect(
      page.locator('span.truncate').filter({ hasText: /Direct Chat/ })
    ).toBeVisible({
      timeout: 5_000,
    });

    // The fallback must include the member count
    await expect(
      page.locator('span.truncate').filter({ hasText: /Direct Chat.*2 members/ })
    ).toBeVisible({
      timeout: 5_000,
    });
  });

  // RED: Fails because rooms.html line 54 renders:
  //   {{ define "detail_title" }}
  //     {{ if .ActiveRoom }}{{ .ActiveRoom.Name }}{{ else }}Room not found{{ end }}
  //   {{ end }}
  // room-006 has Name="" so the detail title renders empty instead of the fallback.
  test('Room detail title shows "(Direct Chat · N members)" for room with empty name', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms/room-006');

    // The detail panel title area must show the fallback, not be empty.
    // The detail_title block renders into the panel header — look for the fallback
    // text within the detail panel section.
    await expect(
      page.locator('section[role="region"]').filter({ hasText: /Direct Chat/ })
    ).toBeVisible({
      timeout: 5_000,
    });
  });

  // REGRESSION GUARD: Must pass both before and after the fix.
  // Verifies that rooms with a real name are not broken by the conditional.
  test('Room with a non-empty name continues to show its real name (regression guard)', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // room-001 "General" must always be visible in the list
    await expect(
      page.locator('span.truncate').filter({ hasText: 'General' }).first()
    ).toBeVisible({
      timeout: 5_000,
    });

    // No blank entries — no span.truncate with empty visible text.
    // After the fix, every such span has either a name or the fallback.
    const allNameSpans = page.locator('span.font-medium.flex-1.truncate');
    const count = await allNameSpans.count();
    for (let i = 0; i < count; i++) {
      const text = (await allNameSpans.nth(i).textContent() ?? '').trim();
      expect(
        text,
        `Room list entry at index ${i} must not be blank — should show name or fallback (AC3)`
      ).not.toBe('');
    }
  });
});

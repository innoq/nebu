/**
 * Story 9.13: Admin UI — UX Bug Fixes & Visual Polish
 *
 * Playwright E2E acceptance test scaffolds written FIRST (before implementation code),
 * per the Nebu ATDD standard (CLAUDE.md Gate 1).
 *
 * AC coverage in this file:
 *   AC2  — TestLoginPageHidesNav: login page sidebar nav items are NOT visible
 *   AC3  — TestNonDashboardHidesSSEStatus: /admin/users has no "Connecting…" indicator
 *   AC4  — TestDeactivateButtonIsError: Deactivate button has btn-error class
 *   AC4b — TestArchiveButtonIsError: "Archive room" button has btn-error class
 *   AC5  — TestDashboardCardsHaveBorderL: status cards have border-l-4 not border-t-4
 *   AC6  — TestLiveMetricsLoadingState: Live Metrics shows loading text initially
 *   AC11 — TestSaveButtonNotFullWidth: Config page Save button is not full-width
 *
 * Additional browser-layer checks:
 *   AC7  — TestLoginHeadingIsSignIn: login card <h1> reads "Sign in to Nebu"
 *   AC15 — TestComplianceStepperIsConstrained: stepper has max-w-md
 *   AC16 — TestDashboardStatusLabelFullOpacity: status labels not muted
 *
 * Go unit tests (gateway/internal/admin/ux_polish_9_13_test.go) cover:
 *   AC1, AC2, AC3, AC4, AC5, AC7, AC8, AC10, AC11, AC12, AC13, AC14, AC15, AC16, AC17
 *
 * Auth pattern: OIDC Authorization Code + PKCE via Dex.
 * Never use ROPC (grant_type=password) — not supported by Dex v2.41+.
 * Prerequisites: make dev running, bootstrap complete.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — OIDC Authorization Code + PKCE via Dex
// Identical pattern to all other admin specs (smoke-flows.spec.ts, etc.)
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
// AC2 — Login page hides authenticated navigation
// ---------------------------------------------------------------------------

test.describe('AC2: Login page hides authenticated nav', () => {
  // FAILS right now: the sidebar nav is rendered unconditionally.
  // Fix: Add LoginMode bool to PageData, set it in LoginPageHandler,
  // wrap nav in {{ if not .LoginMode }}.
  test('login page does not show authenticated sidebar nav items', async ({ page }) => {
    // Navigate directly to login without authenticating.
    await page.goto('/admin/login');

    // The page must render (not redirect away).
    await expect(page).toHaveURL(/\/admin\/login/);

    // The sidebar nav items for authenticated users must NOT be visible.
    // They are wrapped in {{ if not .LoginMode }} after the fix.
    await expect(page.locator('[data-navkey="dashboard"]')).not.toBeVisible();
    await expect(page.locator('[data-navkey="users"]')).not.toBeVisible();
    await expect(page.locator('[data-navkey="rooms"]')).not.toBeVisible();
    await expect(page.locator('[data-navkey="compliance"]')).not.toBeVisible();
    await expect(page.locator('[data-navkey="config"]')).not.toBeVisible();
    await expect(page.locator('[data-navkey="logout"]')).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// AC3 — Non-dashboard pages hide the SSE status indicator
// ---------------------------------------------------------------------------

test.describe('AC3: Non-dashboard pages hide SSE status indicator', () => {
  // FAILS right now: the fallback "Connecting…" span renders on every page when
  // TopbarStatus is empty. Fix: wrap the entire topbar status block in
  // {{ if .TopbarStatus }}...{{ end }} in base.html.
  test('users page does not show Connecting topbar indicator', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');

    // The topbar status indicator must NOT be visible on non-dashboard pages.
    // After the fix, #topbar-status is absent (or display:none) when TopbarStatus is empty.
    const topbarStatus = page.locator('#topbar-status');
    await expect(topbarStatus).not.toBeVisible();
  });

  test('rooms page does not show Connecting topbar indicator', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    const topbarStatus = page.locator('#topbar-status');
    await expect(topbarStatus).not.toBeVisible();
  });

  test('config page does not show Connecting topbar indicator', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');

    const topbarStatus = page.locator('#topbar-status');
    await expect(topbarStatus).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// AC4 — Destructive action buttons use btn-error
// ---------------------------------------------------------------------------

test.describe('AC4: Destructive buttons use btn-error', () => {
  // FAILS right now: users.html and rooms.html use btn-warning on these buttons.

  test('Deactivate button on user detail has class btn-error not btn-warning', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/users');

    // Click usr-001 Alice Müller (active user) to open detail panel.
    await page.locator('a[role="option"][href="/admin/users/usr-001"]').click();

    // Wait for detail panel to open.
    await expect(page.locator('section[role="region"]')).toBeVisible({ timeout: 5_000 });

    // The Deactivate button must have btn-error class.
    const deactivateBtn = page.locator('button:has-text("Deactivate")');
    await expect(deactivateBtn).toBeVisible();

    const classList = await deactivateBtn.getAttribute('class') ?? '';
    expect(classList, 'Deactivate button must have btn-error class (AC4)').toContain('btn-error');
    expect(classList, 'Deactivate button must NOT have btn-warning class (AC4)').not.toContain('btn-warning');
  });

  test('Archive room button on room detail has class btn-error not btn-warning', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/rooms');

    // Click room-001 General (active room) to open detail panel.
    await page.locator('a[role="option"][href="/admin/rooms/room-001"]').click();

    // Wait for detail panel to open.
    await expect(page.locator('section[role="region"]')).toBeVisible({ timeout: 5_000 });

    // The Archive room button must have btn-error class.
    const archiveBtn = page.locator('button:has-text("Archive room")');
    await expect(archiveBtn).toBeVisible();

    const classList = await archiveBtn.getAttribute('class') ?? '';
    expect(classList, 'Archive room button must have btn-error class (AC4)').toContain('btn-error');
    expect(classList, 'Archive room button must NOT have btn-warning class (AC4)').not.toContain('btn-warning');
  });
});

// ---------------------------------------------------------------------------
// AC5 — Dashboard status cards use left accent border (border-l-4)
// ---------------------------------------------------------------------------

test.describe('AC5: Dashboard status cards have left accent border', () => {
  // FAILS right now: dashboard.html uses border-t-4 (top border).
  test('dashboard status cards have border-l-4 class not border-t-4', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/dashboard');

    // Check the status cards section for border-l-4 presence.
    // After the fix, each status card div has border-l-4 instead of border-t-4.
    const statusSection = page.locator('section[aria-label="System status"]');
    await expect(statusSection).toBeVisible();

    // The HTML must contain border-l-4 (at least once per card = 3 times).
    const html = await statusSection.innerHTML();
    const borderLCount = (html.match(/border-l-4/g) ?? []).length;
    const borderTCount = (html.match(/border-t-4/g) ?? []).length;

    expect(borderLCount, 'Expected at least 3 occurrences of border-l-4 on status cards (one per card)').toBeGreaterThanOrEqual(3);
    expect(borderTCount, 'Expected 0 occurrences of border-t-4 on status cards (must be replaced with border-l-4)').toBe(0);
  });
});

// ---------------------------------------------------------------------------
// AC6 — Dashboard Live Metrics shows loading/error state
// ---------------------------------------------------------------------------

test.describe('AC6: Dashboard Live Metrics loading and error states', () => {
  // FAILS right now: dashboard.html does not show a loading text initially,
  // and does not have a 5-second timeout error state.
  // Fix: Add id="metrics-loading" div + JS timeout logic.

  test('Live Metrics section shows loading text initially', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/dashboard');

    const liveMetrics = page.locator('#live-metrics');
    await expect(liveMetrics).toBeVisible();

    // The Live Metrics section must show a loading indicator initially.
    // After the fix: an element with id="metrics-loading" or text "Loading metrics…" is present.
    const loadingEl = page.locator('#metrics-loading');
    await expect(loadingEl).toBeVisible({
      timeout: 3_000,
    });
  });

  test('Live Metrics shows error message after 5 seconds without SSE data', async ({ page }) => {
    // This test intentionally does NOT connect an SSE stream, so the 5-second timeout fires.
    await loginAsAdmin(page);
    await page.goto('/admin/dashboard');

    // Must wait >5 s for the JS metrics-loading timeout to fire (AC6 specifies 5 s threshold).
    // No smarter poll is possible — the feature is a fixed JS timer, not a DOM event.
    await page.waitForTimeout(6_000);

    const liveMetrics = page.locator('#live-metrics');
    await expect(liveMetrics).toContainText(/Metrics unavailable|Core not responding/, {
      timeout: 2_000,
    });
  });
});

// ---------------------------------------------------------------------------
// AC7 — Login card heading is "Sign in to Nebu"
// ---------------------------------------------------------------------------

test.describe('AC7: Login card heading deduplicated', () => {
  // FAILS right now: login.html <h1> still says "Nebu Admin".
  test('login page card heading reads Sign in to Nebu', async ({ page }) => {
    await page.goto('/admin/login');

    // The card-title <h1> must read "Sign in to Nebu", not "Nebu Admin".
    const cardHeading = page.locator('h1.card-title');
    await expect(cardHeading).toBeVisible();
    await expect(cardHeading).toHaveText('Sign in to Nebu');
  });
});

// ---------------------------------------------------------------------------
// AC11 — Save buttons are not full-width
// ---------------------------------------------------------------------------

test.describe('AC11: Save buttons are not full-width', () => {
  // FAILS right now: both config.html and role-mapping.html Save buttons
  // are in a form-control div without flex justify-end wrapper.

  test('config page Save button does not have w-full class', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config');

    const saveBtn = page.locator('button[type="submit"]:has-text("Save")').first();
    await expect(saveBtn).toBeVisible();

    const classList = await saveBtn.getAttribute('class') ?? '';
    expect(classList, 'Config Save button must NOT have w-full class (AC11)').not.toContain('w-full');
    expect(classList, 'Config Save button must NOT have btn-block class (AC11)').not.toContain('btn-block');

    // The Save button must be inside a flex justify-end wrapper.
    const wrapper = page.locator('.flex.justify-end');
    await expect(wrapper).toBeVisible();
  });

  test('role-mapping page Save button does not have w-full class', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/config/role-mapping');

    const saveBtn = page.locator('button[type="submit"]:has-text("Save")').first();
    await expect(saveBtn).toBeVisible();

    const classList = await saveBtn.getAttribute('class') ?? '';
    expect(classList, 'Role Mapping Save button must NOT have w-full class (AC11)').not.toContain('w-full');
    expect(classList, 'Role Mapping Save button must NOT have btn-block class (AC11)').not.toContain('btn-block');

    const wrapper = page.locator('.flex.justify-end');
    await expect(wrapper).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// AC15 — Compliance stepper is constrained (max-w-md)
// ---------------------------------------------------------------------------

test.describe('AC15: Compliance stepper constrained to max-w-md', () => {
  // FAILS right now: compliance.html stepper container has no max-width.
  test('compliance page approval stepper has max-w-md class', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/compliance');

    // The stepper wrapper card must have max-w-md.
    // After the fix: <div class="card bg-base-100 shadow-sm p-4 max-w-md">
    const stepperCard = page.locator('.card:has(.steps)');
    await expect(stepperCard).toBeVisible();

    const classList = await stepperCard.getAttribute('class') ?? '';
    expect(classList, 'Compliance stepper card must have max-w-md class (AC15)').toContain('max-w-md');
  });
});

// ---------------------------------------------------------------------------
// AC16 — Dashboard "OK" status text at full opacity
// ---------------------------------------------------------------------------

test.describe('AC16: Dashboard status labels at full opacity', () => {
  // FAILS right now: dashboard.html uses text-base-content/70 on status label <p>.
  // Fix: change to text-base-content (or text-success for OK status).
  test('dashboard status card labels do not use reduced-opacity class', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/admin/dashboard');

    const statusSection = page.locator('section[aria-label="System status"]');
    await expect(statusSection).toBeVisible();

    const html = await statusSection.innerHTML();
    // text-base-content/70 must not appear on any status card label paragraph.
    expect(
      html,
      'Dashboard status card labels must not use text-base-content/70 (reduced opacity). Change to text-base-content.'
    ).not.toContain('text-base-content/70');
  });
});

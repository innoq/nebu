/**
 * Story 7.4: Display Components (C11 InlineEdit, C12 AlertBanner, C13 StatusBadge, C14 EmptyState)
 *
 * Playwright E2E acceptance tests for the four display components:
 *   - C11  InlineEdit   (inline_edit.html)
 *   - C12  AlertBanner  (alert_banner.html)
 *   - C13  StatusBadge  (status_badge.html)
 *   - C14  EmptyState   (empty_state.html)
 *
 * ATDD NOTE: These tests are written FIRST, before implementation code,
 * per the Nebu ATDD standard (CLAUDE.md Gate 1).
 *
 * All four scenarios are `test.skip` because no production page embeds
 * these components yet. They will be enabled in the stories listed below:
 *
 *   - InlineEdit:   TODO: enable in Story 7.6 (User Detail Panel embeds InlineEdit for display name)
 *   - AlertBanner:  TODO: enable in Story 7.6 or 7.7 (first page to show flash messages)
 *   - StatusBadge:  TODO: enable in Story 7.5 (User List page embeds StatusBadge per user)
 *   - EmptyState:   TODO: enable in Story 7.5 (User List page renders EmptyState when no results)
 *
 * The primary quality gate for Story 7.4 is the Go unit test suite in
 * `gateway/internal/admin/display_components_test.go`.
 *
 * Auth pattern: same as interaction-components.spec.ts — OIDC Authorization Code + PKCE
 * via Dex. Never use ROPC (grant_type=password) per the Nebu OIDC standard.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — reused from interaction-components.spec.ts
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
// C11 InlineEdit
// ---------------------------------------------------------------------------
test.describe('C11 InlineEdit', () => {
  // TODO: enable in Story 7.6 — User Detail Panel embeds inline_edit for display name.
  // When enabled: navigate to /admin/users/<id>, click the edit icon button, assert the
  // input field appears with the current value pre-filled.
  test.skip('inline edit click reveals input with current value', async ({ page }) => {
    await loginAsAdmin(page);

    // Navigate to a user detail page that embeds InlineEdit for display name
    await page.goto('/admin/users/usr-001');

    // Click the edit icon button (aria-label starts with "Edit")
    const editBtn = page.locator('button[aria-label^="Edit"]').first();
    await expect(editBtn).toBeVisible();
    await editBtn.click();

    // After click, the input field must be visible with the current value
    const inputField = page.locator('input[type="text"][aria-label]').first();
    await expect(inputField).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// C12 AlertBanner
// ---------------------------------------------------------------------------
test.describe('C12 AlertBanner', () => {
  // TODO: enable in Story 7.6 or 7.7 — first page to show flash messages.
  // When enabled: trigger a POST action that sets a flash message, then assert
  // the alert banner is visible and the dismiss button removes it.
  test.skip('alert banner dismiss button removes the alert from the DOM', async ({ page }) => {
    await loginAsAdmin(page);

    // IMPORTANT when enabling: you must trigger a POST action first so the handler
    // stores a flash message (session cookie or query param) before navigating here.
    // Simply loading /admin/users without a flash state will not render an alert banner.
    // Example: POST to an action that deactivates a user, then follow the redirect.
    // Navigate to a page that shows a dismissible AlertBanner
    // (the URL will carry a flash param or session-stored flash)
    await page.goto('/admin/users');

    // The alert element must be visible
    const alert = page.locator('[role="alert"]').first();
    await expect(alert).toBeVisible();

    // Clicking the dismiss button removes the alert
    const dismissBtn = alert.getByRole('button', { name: 'Dismiss' });
    await expect(dismissBtn).toBeVisible();
    await dismissBtn.click();

    // The alert must no longer be visible after clicking dismiss
    await expect(alert).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// C13 StatusBadge
// ---------------------------------------------------------------------------
test.describe('C13 StatusBadge', () => {
  // TODO: enable in Story 7.5 — User List page embeds StatusBadge per user row.
  // When enabled: navigate to /admin/users, assert at least one status badge is visible.
  test.skip('status badge visible in user list with correct role', async ({ page }) => {
    await loginAsAdmin(page);

    await page.goto('/admin/users');

    // At least one span with role="status" and a badge class must be visible
    const statusBadge = page.locator('span[role="status"]').first();
    await expect(statusBadge).toBeVisible();

    // Must have one of the expected badge classes
    const classes = await statusBadge.getAttribute('class') ?? '';
    const hasBadgeClass =
      classes.includes('badge-success') ||
      classes.includes('badge-error') ||
      classes.includes('badge-warning') ||
      classes.includes('badge-ghost');
    expect(hasBadgeClass).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// C14 EmptyState
// ---------------------------------------------------------------------------
test.describe('C14 EmptyState', () => {
  // TODO: enable in Story 7.5 — User List page renders EmptyState when no results.
  // When enabled: navigate to /admin/users?q=zzznoresultszzz, assert the empty state
  // heading is visible.
  test.skip('empty state visible when user list has no results', async ({ page }) => {
    await loginAsAdmin(page);

    // Query that should produce zero results
    await page.goto('/admin/users?q=zzznoresultszzz');

    // The EmptyState heading must be visible
    const heading = page.locator('h3').filter({ hasText: /no users/i }).first();
    await expect(heading).toBeVisible();
  });
});

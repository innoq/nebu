/**
 * Story 7.3: Interaction Components (C6 WizardStepper, C7–C10)
 *
 * Playwright E2E acceptance tests for the four interaction components:
 *   - C6  WizardStepper  (wizard_stepper.html)
 *   - C7  ConfirmDialog  (confirm_dialog.html)
 *   - C8  SearchInput    (search_input.html)
 *   - C9/C10 FilterBar   (filter_bar.html)
 *
 * ATDD NOTE: These tests are written FIRST, before implementation code,
 * per the Nebu ATDD standard (CLAUDE.md Gate 1).
 *
 * All four scenarios are `test.skip` because no production page embeds
 * these components yet. They will be enabled in the stories listed below:
 *
 *   - WizardStepper:   TODO: enable in Story 7.11 (Compliance page uses WizardStepper)
 *   - ConfirmDialog:   TODO: enable in Story 7.7  (User Role UI uses ConfirmDialog for deactivation)
 *   - SearchInput:     TODO: enable in Story 7.5  (/admin/users embeds SearchInput)
 *   - FilterBar:       TODO: enable in Story 7.5  (/admin/users embeds FilterBar)
 *
 * The primary quality gate for Story 7.3 is the Go unit test suite in
 * `gateway/internal/admin/interaction_components_test.go`.
 *
 * Auth pattern: same as master-detail.spec.ts — OIDC Authorization Code + PKCE
 * via Dex. Never use ROPC (grant_type=password) per the Nebu OIDC standard.
 */
import { test, expect, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Auth helper — reused from master-detail.spec.ts
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
// C6 WizardStepper
// ---------------------------------------------------------------------------
test.describe('C6 WizardStepper', () => {
  // TODO: enable in Story 7.11 — Compliance page embeds wizard_stepper for the
  // export flow progress indicator. Register real page route, remove test.skip.
  test.skip('wizard stepper renders on compliance page with ARIA step markers', async ({ page }) => {
    await loginAsAdmin(page);

    // Navigate to the compliance page that embeds the WizardStepper
    await page.goto('/admin/compliance');

    // The WizardStepper outer element uses role="list" (AC: WCAG)
    const stepList = page.getByRole('list');
    await expect(stepList).toBeVisible();

    // Exactly one list item has aria-current="step" (the active step)
    const activeStep = page.locator('[aria-current="step"]');
    await expect(activeStep).toHaveCount(1);
  });
});

// ---------------------------------------------------------------------------
// C7 ConfirmDialog
// ---------------------------------------------------------------------------
test.describe('C7 ConfirmDialog', () => {
  // TODO: enable in Story 7.7 — User Role UI embeds confirm_dialog for the
  // "Deactivate user" flow. The trigger button is in the user detail panel.
  // When enabled: navigate to /admin/users/<id>, click the deactivate trigger,
  // assert dialog visible with role="alertdialog".
  test.skip('confirmation dialog opens via showModal and has correct ARIA roles', async ({ page }) => {
    await loginAsAdmin(page);

    // Navigate to a users detail page that has a ConfirmDialog trigger
    await page.goto('/admin/users/usr-001');

    // The trigger button (lives in the caller template, NOT inside confirm_dialog.html)
    const triggerBtn = page.getByRole('button', { name: /deactivate/i });
    await triggerBtn.click();

    // The <dialog> element must be visible with role="alertdialog"
    const dialog = page.locator('dialog#confirm_dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('role', 'alertdialog');

    // ARIA labelledby and describedby must be present
    await expect(dialog).toHaveAttribute('aria-labelledby', 'confirm_dialog_title');
    await expect(dialog).toHaveAttribute('aria-describedby', 'confirm_dialog_message');

    // Confirm button present
    const confirmBtn = dialog.getByRole('button', { name: /deactivate/i });
    await expect(confirmBtn).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// C8 SearchInput
// ---------------------------------------------------------------------------
test.describe('C8 SearchInput', () => {
  // TODO: enable in Story 7.5 — /admin/users embeds search_input inside a
  // <form method="GET">. When enabled: navigate to /admin/users, type into
  // the search box, wait 400ms for the 300ms debounce, assert URL param updated.
  test.skip('search input submits form after 300ms debounce on typing', async ({ page }) => {
    await loginAsAdmin(page);

    await page.goto('/admin/users');

    const searchInput = page.locator('input[name="q"]');
    await expect(searchInput).toBeVisible();

    // Type into the search box — debounce fires after 300ms
    await searchInput.fill('alice');

    // Wait for the URL to change — deterministic: waits for actual navigation,
    // not a fixed timeout. The debounce fires at 300ms and calls requestSubmit().
    await expect(page).toHaveURL(/[?&]q=alice/, { timeout: 2_000 });
  });
});

// ---------------------------------------------------------------------------
// C9/C10 FilterBar
// ---------------------------------------------------------------------------
test.describe('C9/C10 FilterBar', () => {
  // TODO: enable in Story 7.5 — /admin/users embeds filter_bar with a "role"
  // select dropdown. When enabled: navigate to /admin/users, change the role
  // filter, assert URL updates immediately (onchange="this.form.submit()").
  test.skip('filter bar select triggers immediate form submit and updates URL', async ({ page }) => {
    await loginAsAdmin(page);

    await page.goto('/admin/users');

    // Select a different role from the FilterBar dropdown
    const roleSelect = page.locator('select[name="role"]');
    await expect(roleSelect).toBeVisible();

    await roleSelect.selectOption('admin');

    // The onchange handler submits the form immediately — URL param updated
    await expect(page).toHaveURL(/[?&]role=admin/);
  });
});

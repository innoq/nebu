/**
 * Step definitions for features/admin/build-info-footer.feature
 *
 * Story 11-9 — AC9 + AC10.
 *
 * These steps will FAIL until the implementation adds a <footer> element
 * containing "nebu gateway v" to every authenticated Admin UI page template.
 */

import { Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/**
 * "Then the page contains a build info footer with text {string}"
 *
 * AC9: a footer is visible on every authenticated admin page showing
 * "nebu gateway v{version} · {git_commit} · built {build_time}".
 * The footer element uses the DaisyUI footer class and contains the
 * prefix "nebu gateway v".
 *
 * Will FAIL before implementation because <footer> with this text does not exist.
 */
Then(
  'the page contains a build info footer with text {string}',
  async ({ page }: { page: Page }, expectedText: string) => {
    const footerLocator = page.locator('footer').filter({ hasText: expectedText });
    await expect(footerLocator.first()).toBeVisible({ timeout: 10_000 });
  }
);

/**
 * "Then the build info footer shows a non-empty version string"
 *
 * AC10: even when built without ldflags the footer renders a non-empty value
 * (either a real version or the sentinel "unknown") — no blank footer, no crash.
 *
 * Will FAIL before implementation because no footer element exists yet.
 */
Then(
  'the build info footer shows a non-empty version string',
  async ({ page }: { page: Page }) => {
    const footerLocator = page.locator('footer').filter({ hasText: /nebu gateway v\S+/ });
    await expect(footerLocator.first()).toBeVisible({ timeout: 10_000 });
    const text = await footerLocator.first().textContent() ?? '';
    expect(text).toMatch(/nebu gateway v\S+/);
  }
);

/**
 * "Then the page does not contain a build info footer"
 *
 * AC9 negative: the footer must NOT appear on the login page
 * (unauthenticated layout).
 *
 * Will FAIL before implementation only if the footer is mistakenly rendered
 * on the login page; expected to pass once the footer is correctly gated
 * behind authentication. The test is written now so the gate is explicit.
 */
Then(
  'the page does not contain a build info footer',
  async ({ page }: { page: Page }) => {
    // Wait briefly for any dynamic content to settle, then assert absence.
    await page.waitForLoadState('domcontentloaded').catch(() => {});

    const footerLocator = page.locator('footer').filter({ hasText: /nebu gateway v/ });
    const bodyLocator = page.getByText(/nebu gateway v/, { exact: false });

    // Neither a <footer> nor any element containing "nebu gateway v" should be present.
    await expect(footerLocator).toHaveCount(0, { timeout: 5_000 });
    await expect(bodyLocator).toHaveCount(0, { timeout: 5_000 });
  }
);

/**
 * Step definitions for features/admin/users.feature
 *
 * Story 9-26 — Phase 3, AC13.
 * F-10/F-11 fix: room-list and audit-log steps moved to rooms.steps.ts.
 *               Only the user-list assertion remains here.
 */

import { Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/**
 * "Then the user list contains at least one entry"
 * Used in features/admin/users.feature
 */
Then('the user list contains at least one entry', async ({ page }: { page: Page }) => {
  // Admin users page renders a [role="listbox"][aria-label="Users"] with
  // [role="listitem"] > [role="option"] children showing usernames like "alex active".
  // Strategy 1 (observed DOM): ARIA listbox options
  const listboxOptions = page.locator('[role="listbox"][aria-label="Users"] [role="option"], [role="listbox"][aria-label="Users"] li');
  const optionCount = await listboxOptions.count();

  if (optionCount > 0) {
    expect(optionCount).toBeGreaterThan(0);
    return;
  }

  // Strategy 2: table rows (older layout)
  const tableRows = page.locator('table tbody tr, [class*="user-list"] li, [class*="user-row"]');
  const rowCount = await tableRows.count();

  if (rowCount > 0) {
    expect(rowCount).toBeGreaterThan(0);
    return;
  }

  // Strategy 3: any listbox with items (generic fallback)
  const anyListbox = page.locator('[role="listbox"] [role="option"], [role="listbox"] [role="listitem"]');
  const anyCount = await anyListbox.count();
  expect(anyCount, 'User list should contain at least one entry').toBeGreaterThan(0);
});

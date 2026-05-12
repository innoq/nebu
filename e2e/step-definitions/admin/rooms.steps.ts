/**
 * Step definitions for features/admin/rooms.feature and features/admin/audit-log.feature
 *
 * Story 9-26 — Phase 3, AC13.
 * F-10 fix: room-list and audit-log assertions moved here from users.steps.ts.
 * F-11 fix: rooms.steps.ts created (was absent before this story).
 */

import { Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/**
 * "Then the room list contains at least one entry"
 * Used in features/admin/rooms.feature
 */
Then('the room list contains at least one entry', async ({ page }: { page: Page }) => {
  // Admin rooms page renders a [role="listbox"][aria-label="Rooms"] with
  // [role="listitem"] > [role="option"] children.
  // Strategy 1 (observed DOM): ARIA listbox options
  const listboxOptions = page.locator('[role="listbox"][aria-label="Rooms"] [role="option"], [role="listbox"][aria-label="Rooms"] li');
  const optionCount = await listboxOptions.count();

  if (optionCount > 0) {
    expect(optionCount).toBeGreaterThan(0);
    return;
  }

  // Strategy 2: table rows (older layout)
  const tableRows = page.locator('table tbody tr, [class*="room-list"] li, [class*="room-row"]');
  const rowCount = await tableRows.count();

  if (rowCount > 0) {
    expect(rowCount).toBeGreaterThan(0);
    return;
  }

  // Strategy 3: any listbox with items (generic fallback)
  const anyListbox = page.locator('[role="listbox"] [role="option"], [role="listbox"] [role="listitem"]');
  const anyCount = await anyListbox.count();
  expect(anyCount, 'Room list should contain at least one entry').toBeGreaterThan(0);
});

/**
 * "Then the audit log contains at least one entry"
 * Used in features/admin/audit-log.feature
 */
Then('the audit log contains at least one entry', async ({ page }: { page: Page }) => {
  // Admin audit log page renders a table or list of log entries
  const tableRows = page.locator('table tbody tr, [class*="log-entry"], [class*="audit-row"]');
  const count = await tableRows.count();

  if (count === 0) {
    // Fallback: look for any timestamped entry
    const anyEntry = page.locator('[class*="timestamp"], time, [datetime]');
    await expect(anyEntry.first()).toBeVisible({ timeout: 15_000 });
  } else {
    expect(count).toBeGreaterThan(0);
  }
});

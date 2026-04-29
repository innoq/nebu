import { test, expect } from '@playwright/test';

test.describe('Obsidian Theme', () => {
  test('html element has data-theme="obsidian"', async ({ page }) => {
    // bootstrap page is publicly accessible — no login required
    await page.goto('/admin/bootstrap');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'obsidian');
  });
});

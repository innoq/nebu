import { test, expect } from '@playwright/test';
const ELEMENT = 'http://localhost:7070';

async function ssoLogin(page: import('@playwright/test').Page, email: string) {
  await page.goto(ELEMENT);
  await page.getByRole('link', { name: /anmelden|sign in/i }).click();
  await page.getByRole('button', { name: /weiter mit sso|continue with sso/i }).click();
  await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
  await page.locator('input[name="login"]').fill(email);
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();
  await page.waitForURL(/localhost:7070/, { timeout: 20_000 });
  const cancelBtn = page.getByRole('button', { name: /cancel|abbrechen/i });
  if (await cancelBtn.isVisible({ timeout: 5_000 }).catch(() => false)) await cancelBtn.click();
  await page.waitForTimeout(2000);
}

test('full group room E2E', async ({ page }) => {
  test.setTimeout(90_000);
  await ssoLogin(page, 'marie@example.com');
  await page.goto(`${ELEMENT}/#/home`);
  await page.waitForTimeout(1500);
  
  await page.locator('[role="button"]').filter({ hasText: 'Create a Group Chat' }).first().click();
  await page.locator('input[placeholder="Name"]').fill('E2E Acceptance Group');
  await page.getByRole('button', { name: 'Create room' }).click();
  
  const composer = page.locator('[contenteditable="true"]').first();
  await expect(composer).toBeVisible({ timeout: 30_000 });
  
  console.log('Composer visible! URL:', page.url());
  await page.screenshot({ path: '/tmp/element-group-final.png' });
  
  await composer.fill('Group message from E2E acceptance test');
  await composer.press('Enter');
  await page.waitForTimeout(2000);
  
  const msg = page.locator('.mx_EventTile').getByText('Group message from E2E acceptance test');
  const msgVisible = await msg.isVisible({ timeout: 10_000 }).catch(() => false);
  console.log('Message visible:', msgVisible);
  await page.screenshot({ path: '/tmp/element-group-message2.png' });
});

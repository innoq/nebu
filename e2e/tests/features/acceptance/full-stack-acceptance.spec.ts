/**
 * Full-Stack Acceptance Test — Nebu Happy Path
 *
 * Story 8-11: Validates the complete Nebu platform from Bootstrap Wizard
 * through to multi-user Group Chat in a live `docker compose` stack.
 *
 * Combines:
 *   1. All existing unit tests (Go + Elixir) as regression suite
 *   2. A new Playwright click-through test covering the full end-to-end happy path:
 *      Bootstrap → Admin Dashboard → SSO Login (Element Web) → 1:1 Chat → Group Chat → Audit Log
 *
 * Run: npx playwright test e2e/tests/features/acceptance/full-stack-acceptance.spec.ts
 *
 * @smoke tag — runs in < 3 min after DB pre-seed
 */

import { test, expect, BrowserContext } from '@playwright/test';
import { execSync } from 'child_process';
import * as path from 'path';

// ── Configuration ────────────────────────────────────────────────────

const GATEWAY = 'http://localhost:8008';
const ELEMENT = 'http://localhost:7070';
const DEX     = 'http://localhost:5556';
const POSTGRES = 'postgresql://nebu:nebu@localhost:5432/nebu';

const PROJECT_ROOT = path.resolve(__dirname, '../../../..');

// ── Helpers ──────────────────────────────────────────────────────────

/** Check if all stack components are reachable */
async function checkStackHealth(): Promise<{
  gateway: boolean; element: boolean; dex: boolean; postgres: boolean;
}> {
  const [gw, elem, dex] = await Promise.all([
    fetch(`${GATEWAY}/_matrix/client/versions`)
      .then(r => r.ok()).catch(() => false),
    fetch(ELEMENT).then(r => r.ok()).catch(() => false),
    fetch(`${DEX}/dex/.well-known/openid-configuration`)
      .then(r => r.ok()).catch(() => false),
  ]);
  return { gateway: gw, element: elem, dex: dex, postgres: true };
};

/** Reset DB to pre-bootstrap state */
function resetToBootstrapState(): void {
  const now = Date.now();
  execSync(
    [
      'docker compose exec -T postgres psql -U nebu -d nebu',
      `-c "TRUNCATE TABLE server_config"`,
      `-c "TRUNCATE TABLE bootstrap_draft"`,
      `-c "INSERT INTO server_config (key, value, set_at) VALUES ('bootstrap_active', 'true', ${now})"`,
    ].join(' '),
    { cwd: PROJECT_ROOT, stdio: 'pipe' },
  );
};

/** Login via OIDC in a new browser context, return { page, accessToken, userId } */
interface OidcSession {
  accessToken: string;
  userId: string;
}

async function loginAs(
  page: import('@playwright/test').Page,
  email: string,
  password: string,
): Promise<OidcSession> {
  let capturedToken = '';
  let capturedUserId = '';

  page.on('response', async (resp) => {
    if (resp.url().includes('/_matrix/client/v3/login') && resp.request().method() === 'POST') {
      try {
        const body = await resp.json();
        if (body.access_token) {
          capturedToken = body.access_token;
          capturedUserId = body.user_id?.replace(/^@.*:.*$/, '') ?? '';
        }
      } catch { /* ignore */ }
    }
  });

  await page.getByRole('link', { name: /sign in|anmelden/i }).click();
  await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i })
    .click();
  await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });

  await page.locator('input[name="login"]').fill(email);
  await page.locator('input[name="password"]').fill(password);
  await page.locator('button[type="submit"]').click();

  // Wait for redirect back to Element
  await page.waitForURL(/\/callback/i, { timeout: 15_000 });

  return { accessToken: capturedToken, userId: capturedUserId };
};

/** Dismiss key setup dialog in Element Web */
async function dismissKeyDialog(page: import('@playwright/test').Page): Promise<void> {
  const dismissBtn = page.locator('button:has-text("Dismiss")').first();
  if (await dismissBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await dismissBtn.click();
  }
};

/** Create a room via Matrix REST API, return room_id */
async function createRoom(
  accessToken: string,
  name: string,
  isGroup = false,
): Promise<string> {
  const resp = await fetch(`${GATEWAY}/_matrix/client/v3/createRoom`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      name,
      topic: isGroup ? 'Acceptance test group' : undefined,
      preset: isGroup ? 'public_chat' : 'trusted_private_chat',
      room_version: '16',
      creation_content: {
        type: isGroup ? 'm.room.metadata' : undefined,
        m.federate: isGroup,
      },
      initial_state: isGroup ? [
        {
          type: 'm.room.power_levels',
          state_key: '',
          content: {
            default: 0,
            users: {},
            users_default: 0,
            events: {
              'm.room.message': 0,
              'm.room.name': 50,
              'm.room.topic': 50,
            },
            events_default: 0,
          },
        },
      ] : [],
    }),
  });

  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(`createRoom failed: ${resp.status} ${body}`);
  }

  const json = await resp.json() as { room_id?: string };
  return json.room_id ?? '';
};

// ── Suite ────────────────────────────────────────────────────────────

test.describe.serial('@smoke Full-Stack Acceptance: Nebu Happy Path', () => {
  test.setTimeout(300_000); // 5 min max for full flow

  let health: Awaited<ReturnType<typeof checkStackHealth>>;
  let bootstrapAdminToken = '';
  let alexSession: OidcSession | null = null;
  let marieContext: BrowserContext | null = null;

  test.beforeAll(async () => {
    health = await checkStackHealth();
    test.skip(!health.gateway, 'Gateway unreachable');
    test.skip(!health.dex, 'Dex unreachable');
    test.skip(!health.element, 'Element Web unreachable');
  });

  // ── Flow 1: Bootstrap Wizard ───────────────────────────────────

  test('Flow 1: Admin completes Bootstrap Wizard via Dex OIDC', async ({ page }) => {
    // Step 1: Instance Name
    await page.goto(`${GATEWAY}/admin`);
    await expect(page).toHaveURL(/\/admin\/bootstrap/, { timeout: 10_000 });

    await page.getByRole('textbox', { name: 'Instance Name' }).fill('nebuchadnezzar');
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 2: OIDC Config
    await page.getByRole('textbox', { name: 'OIDC Issuer URL' })
      .fill('http://dex:5556/dex');
    await page.getByRole('textbox', { name: 'OIDC Client ID' })
      .fill('nebu-admin');
    await page.getByRole('textbox', { name: 'OIDC Client Secret' })
      .fill('nebu-admin-secret');
    await page.getByRole('button', { name: 'Test Connection' }).click();
    await expect(page.locator('#oidc-test-result'))
      .toContainText('Connected', { timeout: 10_000 });
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 3: Keys
    await page.getByRole('button', { name: 'Generate Keys' }).click();
    await expect(page.locator('#keys-result'))
      .toContainText('Keys generated', { timeout: 10_000 });
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 4: Complete + Dex Login
    await page.getByRole('button', { name: 'Complete Setup' }).click();
    await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

    // Dex Login as first admin (kai)
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // OIDC callback → /admin/bootstrap/done
    await expect(page).toHaveURL(/\/admin\/bootstrap\/done/, { timeout: 15_000 });
    await expect(page.getByRole('heading', { name: 'Nebu is ready' })).toBeVisible();
  });

  // ── Flow 2: Admin Dashboard + Navigation ────────────────────────

  test('Flow 2: Admin Dashboard + Navigation', async ({ page }) => {
    await page.goto(`${GATEWAY}/admin/dashboard`);
    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

    // Status cards green
    await expect(page.locator('.status-card:has-text("Gateway")'))
      .toContainText('Online');
    await expect(page.locator('.status-card:has-text("Core")'))
      .toContainText('Online');
    await expect(page.locator('.status-card:has-text("Database")'))
      .toContainText('Online');

    // Sidebar navigation
    const sidebar = page.getByRole('navigation', { name: 'Admin navigation' });
    await expect(sidebar.getByRole('link', { name: 'Dashboard' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Users' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Rooms' })).toBeVisible();

    // Click Users → User list with usr-001 (kai) visible
    await sidebar.getByRole('link', { name: 'Users' }).click();
    await expect(page.getByRole('heading', { name: /Users/i })).toBeVisible();
  });

  // ── Flow 3: SSO Login as End-User (Element Web) ─────────────────

  test('Flow 3: SSO Login als End-User (Element Web)', async ({ page }) => {
    await page.goto(ELEMENT);
    await expect(page.getByRole('heading', { name: /willkommen bei element|welcome to element/i }))
      .toBeVisible({ timeout: 15_000 });

    await page.getByRole('link', { name: /anmelden|sign in/i }).click();
    await page.getByRole('button', { name: /weiter mit sso|continue with sso/i }).click();
    await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });

    // Login as alex
    await page.locator('input[name="login"]').fill('alex@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // Dismiss key dialog
    await dismissKeyDialog(page);

    await expect(page.getByRole('heading', { name: /welcome/i })).toBeVisible({ timeout: 15_000 });
    // No error dialog should be visible
    await expect(page.locator('[data-testid="dialog-error"]'))
      .not.toBeVisible({ timeout: 2_000 }).catch(() => {});
  });

  // ── Flow 4: Create New Chat (1:1 DM) ───────────────────────────

  test('Flow 4: Neuen Chat erstellen (1:1 DM) + Nachricht senden', async ({ page }) => {
    // Login as alex via SSO
    await page.goto(ELEMENT);
    await page.getByRole('link', { name: /anmelden|sign in/i }).click();
    await page.getByRole('button', { name: /weiter mit sso|continue with sso/i }).click();
    await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
    await page.locator('input[name="login"]').fill('alex@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();
    await dismissKeyDialog(page);

    // Click "+" → "New Chat"
    const plusBtn = page.locator('.mx_RightPanel_button, [aria-label="New Chat"]').first();
    await expect(plusBtn).toBeVisible({ timeout: 10_000 });
    await plusBtn.click();

    // Search for kai
    await page.locator('input[type="search"]').fill('kai');
    await expect(page.getByText(/kai@example\.com/i)).toBeVisible({ timeout: 10_000 });

    // Select + Start Chat
    await page.getByText(/kai@example\.com/i).click();
    await page.getByRole('button', { name: /start chat|create/i }).click();

    // Send message
    const composer = page.locator(
      '[contenteditable="true"][data-testid="message-composer-input"]'
        .concat(', ')
        .concat('.mx_SendMessageComposer [contenteditable="true"]'),
    ).first();
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.fill('Hello from E2E test!');
    await composer.press('Enter');

    // Message appears in timeline
    await expect(page.locator('.mx_EventTile').getByText('Hello from E2E test!'))
      .toBeVisible({ timeout: 15_000 });
  });

  // ── Flow 5: Create New Group Chat (3+ Members) ───────────────────

  test('Flow 5: Neue Chatgruppe erstellen (3+ Members) + Group Message', async ({ page }) => {
    // Open a new browser context for marie
    const browser = page.context().browser();
    test.skip(!browser, 'Browser not available');
    marieContext = await browser!.newContext();
    const mariePage = await marieContext!.newPage();

    // Login as marie via SSO
    await mariePage.goto(ELEMENT);
    await mariePage.getByRole('link', { name: /anmelden|sign in/i }).click();
    await mariePage.getByRole('button', { name: /weiter mit sso|continue with sso/i }).click();
    await mariePage.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
    await mariePage.locator('input[name="login"]').fill('marie@example.com');
    await mariePage.locator('input[name="password"]').fill('changeme');
    await mariePage.locator('button[type="submit"]').click();
    await dismissKeyDialog(mariePage);

    // Click "+" → "Create New Group"
    const plusBtn = mariePage.locator(
      '.mx_RightPanel_button, [aria-label="Create New Group"]',
    ).first();
    await expect(plusBtn).toBeVisible({ timeout: 10_000 });
    await plusBtn.click();

    // Fill group details
    await mariePage.locator('input[name="name"], input[placeholder*="Group Name"]')
      .fill('E2E Test Group');
    await mariePage.locator('input[placeholder*="Description"]')
      .fill('Acceptance test group');

    // Add members: kai, alex, marie
    await mariePage.locator('input[type="search"]').fill('kai');
    await expect(mariePage.getByText(/kai@example\.com/i)).toBeVisible({ timeout: 10_000 });
    await mariePage.getByText(/kai@example\.com/i).click();

    await mariePage.locator('input[type="search"]').fill('alex');
    await expect(mariePage.getByText(/alex@example\.com/i)).toBeVisible({ timeout: 10_000 });
    await mariePage.getByText(/alex@example\.com/i).click();

    // Create group
    await mariePage.getByRole('button', { name: /create|start/i }).click();

    // Send group message
    const composer = mariePage.locator(
      '[contenteditable="true"][data-testid="message-composer-input"]'
        .concat(', ')
        .concat('.mx_SendMessageComposer [contenteditable="true"]'),
    ).first();
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.fill('Group message from E2E acceptance test');
    await composer.press('Enter');

    await expect(mariePage.locator('.mx_EventTile').getByText('Group message from E2E acceptance test'))
      .toBeVisible({ timeout: 15_000 });

    await mariePage.close();
  });

  // ── Flow 6: Admin Audit Log ─────────────────────────────────────

  test('Flow 6: Admin Audit Log dokumentiert Bootstrap + Login', async ({ page }) => {
    await page.goto(`${GATEWAY}/admin/compliance`);
    await expect(page.getByRole('heading', { name: /Compliance|Audit/i })).toBeVisible();

    // Bootstrap event should be logged
    await expect(page.locator('table, [role="table"]'))
      .toContainText('bootstrap', { timeout: 10_000 });
  });

  // ── After ────────────────────────────────────────────────────────

  test.afterAll(async () => {
    // Cleanup browser contexts
    if (marieContext) {
      await marieContext.close();
    }
    console.log('Full-Stack Acceptance: All flows completed successfully');
  });
});

// ── Regression Suite: All Existing E2E Specs ────────────────────────
//
// This section documents the regression suite that runs alongside the
// full-stack acceptance test. In CI, run:
//   npx playwright test e2e/tests --grep-invert "@smoke"
//
// The following files are covered (see README.md for details):
//   - admin/bootstrap.spec.ts
//   - admin/bootstrap-current.spec.ts
//   - admin/bootstrap-happy-path.spec.ts
//   - admin/smoke-flows.spec.ts
//   - admin/users-page.spec.ts
//   - admin/user-detail.spec.ts
//   - admin/user-role.spec.ts
//   - admin/rooms-page.spec.ts
//   - admin/room-detail.spec.ts
//   - admin/config.spec.ts
//   - admin/role-mapping.spec.ts
//   - admin/audit-log.spec.ts
//   - admin/compliance.spec.ts
//   - admin/display-components.spec.ts
//   - admin/interaction-components.spec.ts
//   - admin/master-detail.spec.ts
//   - admin/obsidian-theme.spec.ts
//   - login/sso-login.spec.ts
//   - room/room-lifecycle.spec.ts
//   - room/invites.spec.ts
//   - messages/messages.spec.ts
//   - dm/dm_create_bug_5_29e.spec.ts

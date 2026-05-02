---
story_id: 8-11
title: "Full-Stack Acceptance Test — Nebu Happy Path (Bootstrap → Chat → Group Chat)"
type: feature
severity: critical
epic: 8
status: ready-for-dev
security_review: required
created: 2026-05-01
---

## Summary

Vor dem **Public Release** (Story 8.10) muss ein **gesamt Abnahme-Test** beweisen, dass die **komplette Nebu-Plattform** — von Bootstrap Wizard bis hinunter zum multi-user Group Chat — in der live `docker compose`-Stack funktioniert.

Dieser Test kombiniert:
1. **Alle existierenden programmierten Tests** (Go unit, Elixir ExUnit, Godog Gherkin, Playwright unit specs) als **Regression-Suite** — alle müssen grün sein.
2. Einen **neuen Playwright Click-Through Test**, der den **vollständigen End-to-End-Happy-Path** als menschlicher Nutzer nachstellt: **Bootstrap → Admin Dashboard → SSO Login (Element Web) → Neuen Chat erstellen → Nachricht senden → Neue Chatgruppe erstellen → Group Chat Message**.

**Output:** Eine einzelne Playwright-Suite `e2e/tests/features/acceptance/full-stack-acceptance.spec.ts` + ein CI-gate `make test-acceptance` das vor Story 8.10 blockiert.

---

## Acceptance Criteria

### AC1: Alle existierenden Unit-Tests laufen grün (3 aufeinanderfolgende Durchläufe)

| Layer | Command | Files |
|-------|---------|-------|
| Go Gateway | `make test-unit-go` | `gateway/.../*_test.go` (~45 files) |
| Elixir Core | `make test-unit-elixir` | `core/.../*_test.exs` |
| Integration | `make test-integration` | Godog scenarios + ExUnit integration |
| E2E (existing) | `npx playwright test e2e/tests --grep-invert "@smoke"` | Alle `e2e/tests/features/**` |

**Gate:** 0 Failures, 0 Skipped (auto-skip bei unreachable Dex/Element ist OK), 0 Flakes über 3 Durchläufe.

### AC2: Full-Stack Happy-Path Test existiert und besteht

Ein **einzelner Playwright Test** (`full-stack-acceptance.spec.ts`) führt folgende sequenziellen Flows durch:

#### Flow 1: Admin Bootstrap Wizard (Steps 1–4)
```
1. Browser öffnet http://localhost:8008/admin
2. Redirect zu /admin/bootstrap → "Bootstrap Setup" sichtbar
3. Step 1: "Instance Name" = "nebuchadnezzar" → "Next"
4. Step 2: OIDC Issuer = "http://dex:5556/dex", Client ID/Secret → "Test Connection" = success → "Next"
5. Step 3: "Generate Keys" → "Keys generated: Ed25519 + X25519" → "Next"
6. Step 4: "Complete Setup" → Redirect zu Dex OIDC
7. Dex Login: "kai@example.com" / "changeme" → "Confirm"
8. Redirect zu /admin/bootstrap/done → "Nebu is ready" sichtbar
```

#### Flow 2: Admin Dashboard + Navigation
```
9. Dashboard zeigt "Dashboard" mit 3 grünen Status-Cards (Gateway, Core, Database)
10. Sidebar: Dashboard, Users, Rooms, Compliance, Config, Logout sichtbar
11. Klick auf "Users" → User-Liste mit usr-001 (kai) sichtbar
12. Klick auf "Rooms" → Room-Liste mit mindestens einem Room sichtbar
```

#### Flow 3: SSO Login als End-User (Element Web)
```
13. Browser öffnet http://localhost:7070 (Element Web)
14. "Willkommen bei Element" → "Anmelden" → "Weiter mit SSO"
15. Redirect zu Dex → Login "alex@example.com" / "changeme"
16. Key-Dialog dismissen
17. "Welcome"-Screen sichtbar, kein Error-Dialog
```

#### Flow 4: Neuen Chat erstellen (1:1 DM)
```
18. Element Web: "+" Button (sidebar) → "New Chat"
19. Suchfeld: "@kai:localhost" (oder provisionierten User) auswählen
20. "Start Chat" → Chat erscheint in sidebar
21. Message-Composer: "Hello from E2E test!" → Enter
22. Nachricht erscheint in Timeline als .mx_EventTile
```

#### Flow 5: Neue Chatgruppe erstellen (3+ Members)
```
23. Element Web: "+" Button → "Create New Group"
24. Group-Name: "E2E Test Group" + Description: "Acceptance test group"
25. Mitglieder: "@kai:localhost", "@alex:localhost", "@marie:localhost" hinzufügen
26. "Create" → Group erscheint in sidebar
27. Message in Group: "Group message from E2E acceptance test" → Enter
28. Nachricht in Group-Timeline sichtbar
```

#### Flow 6: Admin Audit Log Eintrag
```
29. Zurück zu /admin/dashboard → "Audit Log" Tab (falls vorhanden)
30. Oder: Admin navigiert zu /admin/compliance → Audit Log sichtbar
31. Bootstrap-Event und Admin-Login sind im Audit Log dokumentiert
```

### AC3: Test ist CI-ready

- Test enthält **auto-skip Guards** für Dex/Element Erreichbarkeit (wie bestehende E2E-Specs)
- Test verwendet **named room/user patterns** für deterministische Identifikation
- Test hat **cleanup/teardown** um State nach Test sauber zu halten
- Test ist mit `@smoke` Tag markiert für schnelle CI-Verifikation
- Test duration < 180 Sekunden (nach DB pre-seed)

### AC4: Smoke-Test-Suite dokumentiert

Erstelle `e2e/tests/features/acceptance/README.md` die dokumentiert:
- Alle existierenden E2E-Test-Dateien + ihre Coverage
- Den neuen Full-Stack Happy-Path Test
- Smoke-Test-Summary (P0 Tests die < 2min laufen)
- Wie man den Test lokal ausführt (`npx playwright test e2e/tests/features/acceptance`)
- DB Reset Procedure für reproduzierbare Runs

---

## Existing Test Inventory (AC1 Reference)

### Go Unit Tests (`gateway/.../*_test.go`)

| File | Epic | Coverage |
|------|------|----------|
| `audit_log_test.go` | 7 | Audit log CRUD |
| `audit_log_retention_seed_test.go` | 7 | Retention seed data |
| `bootstrap_api_test.go` | 5 | Bootstrap API |
| `bootstrap_guard_test.go` | 5 | Bootstrap guard/redirect |
| `bootstrap_test.go` | 5 | Bootstrap handler |
| `bootstrap_wizard_test.go` | 5 | Wizard state machine |
| `callback_test.go` | 5 | OIDC callback |
| `claim_selection_tx_test.go` | 5 | OIDC claim selection |
| `compliance_test.go` | 7 | Compliance endpoints |
| `config_test.go` | 7 | Config management |
| `csrf_body_limit_test.go` | 7 | CSRF + body limit |
| `csrf_test.go` | 7 | CSRF protection |
| `dashboard_core_unreachable_test.go` | 7 | Dashboard error handling |
| `dashboard_pending_badge_test.go` | 7 | Pending badge |
| `dashboard_test.go` | 5 | Dashboard |
| `display_components_test.go` | 7 | Display components |
| `error_sanitization_test.go` | 7 | Error sanitization |
| `errors_test.go` | 7 | Error handling |
| `flash_test.go` | 7 | Flash messages |
| `handler_test.go` | 6 | Handler base |
| `interaction_components_test.go` | 7 | Interaction components |
| `issuer_test.go` | 5 | OIDC issuer config |
| `login_test.go` | 5 | Login page |
| `master_detail_test.go` | 7 | Master-detail pattern |
| `metrics_test.go` | 7 | Metrics endpoint |
| `middleware_test.go` | 6/7 | Auth middleware |
| `nonce_test.go` | 5 | Nonce generation |
| `obsidian_theme_test.go` | 7 | Dark theme |
| `role_mapping_test.go` | 5 | OIDC role mapping |
| `rooms_detail_test.go` | 7 | Room detail page |
| `rooms_page_test.go` | 7 | Room list page |
| `secure_test.go` | 7 | Secure redirect |
| `secure_cookie_test.go` | 7 | Secure cookies |
| `security_headers_test.go` | 7 | Security headers |
| `session_revocation_test.go` | 7 | Session revocation |
| `sse_test.go` | 7 | SSE metrics |
| `static_test.go` | 5 | Static files |
| `user_detail_test.go` | 6 | User detail |
| `user_role_test.go` | 6 | User roles |
| `users_page_test.go` | 6 | User list |
| `users_role_test.go` | 6 | User roles |

### Elixir/OTP Tests (`core/`)

| Module | Epic | Coverage |
|--------|------|----------|
| Room GenServer (create/join/leave) | 4 | Room lifecycle |
| Session Manager (ETS + PostgreSQL) | 4 | Session management |
| Presence Manager | 4 | Online/offline status |
| Event Dispatch (gRPC streaming) | 4 | Event fanout |
| Signature (Ed25519 signing) | 4 | Event signing |
| Permissions (power levels) | 4 | Room authorization |
| Audit Log (PostgreSQL) | 7 | Append-only audit |
| RLS (Row Level Security) | 7 | Data isolation |

### Playwright E2E Tests (`e2e/tests/features/`)

| File | Type | Coverage |
|------|------|----------|
| `admin_ui.feature` | Gherkin | Bootstrap Wizard E2E spec |
| `admin/bootstrap.spec.ts` | Playwright | Bootstrap layout + steps |
| `admin/bootstrap-current.spec.ts` | Playwright | Current bootstrap state |
| `admin/bootstrap-happy-path.spec.ts` | Playwright | Full wizard + OIDC |
| `admin/smoke-flows.spec.ts` | Playwright | Admin deactivate user + archive room |
| `admin/users-page.spec.ts` | Playwright | Admin user list |
| `admin/user-detail.spec.ts` | Playwright | Admin user detail |
| `admin/user-role.spec.ts` | Playwright | Admin user roles |
| `admin/rooms-page.spec.ts` | Playwright | Admin room list |
| `admin/room-detail.spec.ts` | Playwright | Admin room detail |
| `admin/config.spec.ts` | Playwright | Admin config page |
| `admin/role-mapping.spec.ts` | Playwright | OIDC role mapping |
| `admin/audit-log.spec.ts` | Playwright | Audit log UI |
| `admin/compliance.spec.ts` | Playwright | Compliance UI |
| `admin/display-components.spec.ts` | Playwright | Display components |
| `admin/interaction-components.spec.ts` | Playwright | Interaction components |
| `admin/master-detail.spec.ts` | Playwright | Master-detail pattern |
| `admin/obsidian-theme.spec.ts` | Playwright | Dark theme |
| `login/sso-login.spec.ts` | Playwright | SSO login via Element |
| `room/room-lifecycle.spec.ts` | Playwright | Room create/leave |
| `room/invites.spec.ts` | Playwright | Room invites |
| `messages/messages.spec.ts` | Playwright | Send/receive messages |
| `dm/dm_create_bug_5_29e.spec.ts` | Playwright | DM creation (bug fix) |

---

## Test Architecture

### File Structure

```
e2e/
  tests/
    features/
      acceptance/
        full-stack-acceptance.spec.ts   ← Neuer Haupt-Test
        README.md                        ← Smoke-Summary + Docs
```

### Test Design

```
full-stack-acceptance.spec.ts
├── @smoke tag — runs in < 3 min after pre-seeded DB
├── test.describe("Full-Stack Acceptance: Nebu Happy Path")
│   ├── test.beforeAll() — stack health check (postgres, gateway, dex, element)
│   ├── test("Flow 1: Admin Bootstrap Wizard") — ~30s
│   ├── test("Flow 2: Admin Dashboard + Navigation") — ~15s
│   ├── test("Flow 3: SSO Login as End-User (Element Web)") — ~20s
│   ├── test("Flow 4: Create New Chat (1:1 DM)") — ~15s
│   ├── test("Flow 5: Create New Group Chat (3+ members)") — ~20s
│   ├── test("Flow 6: Admin Audit Log Entry") — ~10s
│   └── test.afterAll() — cleanup + state snapshot
└── test.describe("Regression: All Existing E2E Specs")
    └── (run all e2e/tests/features/** via npx playwright test --grep-invert "smoke")
```

### Key Design Decisions

1. **Sequenzielle Flows, keine parallelen Tests** — Der Happy-Path ist ein **einzelner zusammenhängender Nutzer-Journey**. Flows müssen sequenziell laufen (Flow 1 → Flow 2 → ... → Flow 6).

2. **Element Web als Test-Ziel** — Chat-Tests laufen gegen Element Web (localhost:7070), nicht gegen Matrix API. Das simulierte echtes Nutzerverhalten.

3. **Multi-Context Pattern** — Für DM/Group-Chat Tests: mehrere Browser-Contexts (alex, marie, kai) für multi-user-Szenarien.

4. **DB Reset zwischen Flows** — Flow 1 (Bootstrap) benötigt fresh DB State. Flow 5 (Group Chat) benötigt mindestens 3 provisionierte Users.

5. **Auto-skip Guards** — Jeder Flow prüft vorher Erreichbarkeit von Dex + Element + Postgres.

---

## Implementation Guide

### Prerequisites

```bash
# Stack must be running
make dev

# DB must be in bootstrap state (for Flow 1)
docker compose exec postgres psql -U nebu -d nebu \
  -c "DELETE FROM server_config WHERE key IN ('bootstrap_completed','oidc_issuer','oidc_client_id','oidc_client_secret','instance_name');"

# /etc/hosts must contain
echo "127.0.0.1 dex" | sudo tee -a /etc/hosts
```

### Test Code Structure

```typescript
import { test, expect, BrowserContext } from '@playwright/test';

// ── Configuration ──────────────────────────────────────────────────
const GATEWAY = 'http://localhost:8008';
const ELEMENT = 'http://localhost:7070';
const DEX     = 'http://localhost:5556';
const POSTGRES= 'postgresql://nebu:nebu@localhost:5432/nebu';

// ── Helpers ────────────────────────────────────────────────────────

/** Check if all stack components are reachable */
async function checkStackHealth(): Promise<{
  gateway: boolean; element: boolean; dex: boolean; postgres: boolean;
}> {
  const [gw, elem, dex] = await Promise.all([
    fetch(`${GATEWAY}/_matrix/client/versions`).then(r => r.ok()).catch(() => false),
    fetch(`${ELEMENT}/`).then(r => r.ok()).catch(() => false),
    fetch(`${DEX}/dex/.well-known/openid-configuration`).then(r => r.ok()).catch(() => false),
  ]);
  return { gateway: gw, element: elem, dex: dex, postgres: true /* TODO: ping */ };
}

/** Login via OIDC in a new browser context, return { page, accessToken, userId } */
async function loginAs(page, email, password) {
  // ... extract from existing sso-login.spec.ts pattern
}

/** Reset DB to post-bootstrap state (after Flow 1) */
async function resetToPostBootstrapState() {
  // ... psql commands to set bootstrap_completed = true
}

/** Create a new room via Matrix API, return room_id */
async function createRoom(accessToken, name, isGroup?) {
  // ... POST /_matrix/client/v3/createRoom
}

// ── Suite ──────────────────────────────────────────────────────────

test.describe.serial('@smoke Full-Stack Acceptance: Nebu Happy Path', () => {
  test.setTimeout(300_000); // 5 min max for full flow

  let health: Awaited<ReturnType<typeof checkStackHealth>>;

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
    await expect(page).toHaveURL(/\/admin\/bootstrap/);
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
    await page.locator('input[name="login"]').fill('kai@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // OIDC callback → /admin/bootstrap/done
    await expect(page).toHaveURL(/\/admin\/bootstrap\/done/, { timeout: 15_000 });
    await expect(page.getByRole('heading', { name: 'Nebu is ready' })).toBeVisible();
  });

  // ── Flow 2: Admin Dashboard ────────────────────────────────────

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

    // Click Users
    await sidebar.getByRole('link', { name: 'Users' }).click();
    await expect(page.getByRole('heading', { name: /Users/ })).toBeVisible();
  });

  // ── Flow 3: SSO Login as End-User (Element Web) ──────────────────

  test('Flow 3: SSO Login als End-User (Element Web)', async ({ page }) => {
    await page.goto(ELEMENT);
    await expect(page.getByRole('heading', { name: /willkommen bei element/i }))
      .toBeVisible({ timeout: 15_000 });

    await page.getByRole('link', { name: /anmelden/i }).click();
    await page.getByRole('button', { name: /weiter mit sso/i }).click();
    await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });

    await page.locator('input[name="login"]').fill('alex@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // Dismiss key dialog
    const dismissBtn = page.locator('button:has-text("Dismiss")').first();
    if (await dismissBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await dismissBtn.click();
    }

    await expect(page.getByRole('heading', { name: /welcome/i })).toBeVisible({ timeout: 15_000 });
    await expect(page.locator('[data-testid="dialog-error"]')).not.toBeVisible({ timeout: 2_000 }).catch(() => {});
  });

  // ── Flow 4: Create New Chat (1:1 DM) ───────────────────────────

  test('Flow 4: Neuen Chat erstellen (1:1 DM) + Nachricht senden', async ({ page }) => {
    // Login as alex
    await page.goto(ELEMENT);
    await page.getByRole('link', { name: /anmelden/i }).click();
    await page.getByRole('button', { name: /weiter mit sso/i }).click();
    await page.waitForURL(/dex.*\/auth/, { timeout: 15_000 });
    await page.locator('input[name="login"]').fill('alex@example.com');
    await page.locator('input[name="password"]').fill('changeme');
    await page.locator('button[type="submit"]').click();

    // Click "+" → "New Chat"
    const plusBtn = page.locator('.mx_RightPanel_button, [aria-label="New Chat"]').first();
    await plusBtn.click();

    // Search for kai
    await page.locator('input[type="search"]').fill('kai');
    await expect(page.getByText(/kai@example\.com/i)).toBeVisible({ timeout: 10_000 });

    // Select + Start Chat
    await page.getByText(/kai@example\.com/i).click();
    await page.getByRole('button', { name: /start chat|create/i }).click();

    // Send message
    const composer = page.locator('[contenteditable="true"][data-testid="message-composer-input"]')
      .or(page.locator('.mx_SendMessageComposer [contenteditable="true"]')).first();
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.fill('Hello from E2E test!');
    await composer.press('Enter');

    // Message appears in timeline
    await expect(page.locator('.mx_EventTile').getByText('Hello from E2E test!'))
      .toBeVisible({ timeout: 15_000 });
  });

  // ── Flow 5: Create New Group Chat (3+ Members) ───────────────────

  test('Flow 5: Neue Chatgruppe erstellen (3+ Members) + Group Message', async ({ page }) => {
    // Login as marie (new context)
    const context = await page.context().browser()!.newContext();
    const mariePage = await context.newPage();

    await mariePage.goto(ELEMENT);
    await mariePage.getByRole('link', { name: /anmelden/i }).click();
    await mariePage.getByRole('button', { name: /weiter mit sso/i }).click();
    await mariePage.waitForURL(/dex.*\/auth/, { timeout: 15_000 });
    await mariePage.locator('input[name="login"]').fill('marie@example.com');
    await mariePage.locator('input[name="password"]').fill('changeme');
    await mariePage.locator('button[type="submit"]').click();

    // Click "+" → "Create New Group"
    const plusBtn = mariePage.locator('.mx_RightPanel_button, [aria-label="Create New Group"]').first();
    await plusBtn.click();

    // Fill group details
    await mariePage.locator('input[name="name"], input[placeholder*="Group Name"]')
      .fill('E2E Test Group');
    await mariePage.locator('input[placeholder*="Description"]')
      .fill('Acceptance test group');

    // Add members (kai, alex, marie)
    await mariePage.locator('input[type="search"]').fill('kai');
    await expect(mariePage.getByText(/kai@example\.com/i)).toBeVisible({ timeout: 10_000 });
    await mariePage.getByText(/kai@example\.com/i).click();

    await mariePage.locator('input[type="search"]').fill('alex');
    await expect(mariePage.getByText(/alex@example\.com/i)).toBeVisible({ timeout: 10_000 });
    await mariePage.getByText(/alex@example\.com/i).click();

    // Create group
    await mariePage.getByRole('button', { name: /create|start/i }).click();

    // Send group message
    const composer = mariePage.locator('[contenteditable="true"][data-testid="message-composer-input"]')
      .or(mariePage.locator('.mx_SendMessageComposer [contenteditable="true"]')).first();
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.fill('Group message from E2E acceptance test');
    await composer.press('Enter');

    await expect(mariePage.locator('.mx_EventTile').getByText('Group message from E2E acceptance test'))
      .toBeVisible({ timeout: 15_000 });

    await mariePage.close();
    await context.close();
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
    // Log final state snapshot for debugging
    console.log('Full-Stack Acceptance: All flows completed successfully');
  });
});
```

### Smoke-Test-Summary (`e2e/tests/features/acceptance/README.md`)

```markdown
# E2E Test-Suite: Nebu

## Smoke-Test-Summary

| Test | File | Prio | Duration |
|------|------|------|----------|
| Bootstrap Layout | `admin/bootstrap.spec.ts` | P0 | ~10s |
| Bootstrap Happy Path | `admin/bootstrap-happy-path.spec.ts` | P0 | ~30s |
| SSO Login | `login/sso-login.spec.ts` | P0 | ~20s |
| Messages Send/Receive | `messages/messages.spec.ts` | P0 | ~15s |
| Room Lifecycle | `room/room-lifecycle.spec.ts` | P0 | ~20s |
| **Full-Stack Acceptance** | `acceptance/full-stack-acceptance.spec.ts` | P0 | ~2min |

### Lokaler Run

```bash
npx playwright test e2e/tests/features/acceptance --headed
npx playwright test e2e/tests --grep-invert "@smoke"  # alle E2E
```

### DB Reset

```bash
docker compose exec postgres psql -U nebu -d nebu \
  -c "TRUNCATE TABLE server_config; TRUNCATE TABLE bootstrap_draft;"
```
```

---

## CI Gate Integration

Add to `.github/workflows/` (or `.gitlab-ci.yml`):

```yaml
test-acceptance:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - name: Start stack
      run: make dev
    - name: Wait for stack
      run: |
        for i in $(seq 1 30); do
          curl -sf http://localhost:8008/_matrix/client/versions && break
          sleep 2
        done
    - name: Install Playwright
      run: cd e2e && npm ci && npx playwright install --with-deps
    - name: Run Full-Stack Acceptance Test
      run: cd e2e && npx playwright test e2e/tests/features/acceptance --reporter=list
    - name: Run All E2E Regression
      run: cd e2e && npx playwright test e2e/tests --grep-invert "@smoke" --reporter=list
    - name: Upload screenshots on failure
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: e2e-screenshots
        path: e2e/test-results/
```

**Gate:** `make test-acceptance` muss grün sein **BEVOR** Story 8.10 (Initial Public Push) ausgeführt wird.

---

## Risikoregister

| Risiko | Impact | Wahrscheinlichkeit | Mitigation |
|--------|--------|-------------------|------------|
| Element Web Timeline-Selektoren ändern sich | HIGH | Medium | Robuste Selectors via `data-testid`, CSS-Klassen-fallback |
| Dex Login timing (race condition) | MEDIUM | Medium | `waitForURL` mit timeout, nicht hardcoded `sleep` |
| DB pre-seed Users fehlen | HIGH | Low | Test hat self-healing: provisioniert Users via API wenn needed |
| Playwright timeout bei CI | MEDIUM | Medium | Generous timeouts (30s+), auto-retry auf flaky assertions |
| Multi-Context Memory-Leak | LOW | Medium | `browser.close()` in `afterAll`, context limit pro browser |

---

## Dependencies

- **Epic 5** (Admin UI Bootstrap) — Bootstrap Wizard muss completed sein
- **Epic 6** (Admin API) — Admin Dashboard + Users + Rooms must be functional
- **Epic 7** (Matrix API) — Chat, Rooms, Sync must work
- **Story 4-29** (Matrix Client-Server API) — End-User Features must be implemented
- **Existing E2E Tests** — Alle existierenden E2E Specs müssen bereits grün sein

---

## DoD (Definition of Done)

- [ ] `full-stack-acceptance.spec.ts` existiert mit allen 6 Flows
- [ ] Alle 6 Flows bestehen lokal (`--headed`) und headless
- [ ] Alle existierenden Unit-Tests (Go + Elixir) grün: 0 Failures, 0 Skipped
- [ ] Alle existierenden E2E-Tests grün: 0 Failures
- [ ] Smoke-Test-Summary (`README.md`) dokumentiert
- [ ] CI-Gate `test-acceptance` in GitHub Actions integriert
- [ ] Security-Review durchgeführt: 0 CRITICAL, 0 HIGH
- [ ] Story marked `done` in epic tracking

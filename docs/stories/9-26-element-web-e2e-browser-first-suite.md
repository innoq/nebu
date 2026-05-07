---
story_id: 9-26
title: "Element Web E2E Suite — Browser-First Feature Tests via Element Web UI"
type: feature
severity: high
epic: 9
status: review
security_review: not-needed
created: 2026-05-07
---

## Summary

Die bestehenden E2E-Tests unter `e2e/tests/features/` sind **keine echten UI-Tests** — sie melden sich via SSO an und fahren dann direkt die Matrix API (`page.request.post`). Der echte Element Web Client wird nicht getestet.

Diese Story ersetzt diesen Ansatz durch eine **Browser-First E2E Suite**, die ausschließlich über die Element Web UI interagiert: Button-Klicks, Formular-Eingaben, Timeline-Assertions. Jede Feature-Interaktion läuft über das echte Element Web Interface — genau so wie ein menschlicher Nutzer.

**Struktur spiegelt element-web/playwright/e2e:** Kleine, fokussierte Specs pro Feature, organisiert in `login/`, `room/`, `messages/`. Kein Mega-Test.

**Drei Phasen — klar getrennt:**

1. **Phase 1 — Framework:** BDD-Setup (`playwright-bdd`), Fixtures, storageState-Caching, ElementAppPage, playwright config, Shared Step Definitions
2. **Phase 2 — Feature Tests:** Je ein `.feature`-File + Step-Definitionen pro Feature (login, logout, relogin, room/create, room/join, room/leave, messages/send, messages/receive)
3. **Phase 3 — Admin UI retro-fit:** Bestehende Admin-UI-Specs bekommen Gherkin-Feature-Files + werden auf `playwright-bdd` umgeschrieben

**Architektur-Prinzip:** Jeder Playwright-Test basiert auf einem `.feature`-File. Gherkin ist die einzige Quelle der Wahrheit. Steps werden einmal definiert und cross-feature wiederverwendet.

---

## Constraints & Design Decisions

### Was NICHT möglich ist (im Vergleich zu element-web tests)

| element-web Tests | Nebu Tests |
|---|---|
| `homeserver.registerUser()` — dynamisch neue User anlegen | Nicht verfügbar — Nebu nutzt Dex (vorkonfigurierte User) |
| testcontainer spinnt Synapse/Dendrite hoch | Nicht nötig — Nebu läuft bereits via `docker compose` |
| `grant_type=password` ROPC Login | Verboten — Dex v2.41+ ohne ROPC; immer Authorization Code + PKCE |
| Cookie-Injection für Auth-State | Nicht möglich — Element speichert Access-Token im localStorage, nicht Cookies |

### Vorkonfigurierte Test-User (Dex)

Alle Tests nutzen ausschließlich diese 4 User (Passwort `changeme`):

| User | Matrix ID | Rolle in Tests |
|---|---|---|
| `alex@example.com` | `@alex:localhost` | Primärer Test-User |
| `marie@example.com` | `@marie:localhost` | Zweiter User (Receive, Multi-User) |
| `tom@example.com` | `@tom:localhost` | Dritter User (Group) |
| `kai@example.com` | `@kai:localhost` | Vierter User / Bot-User |

### API-Seeding ist erlaubt für Test-Setup

Matrix API Calls via `page.request` sind erlaubt für **Test-Setup** (Raum anlegen, User einladen) — aber **nicht** für die Feature-Assertion selbst. Die Assertion muss über die Element Web UI erfolgen.

Beispiel `room/join`:
- Setup (API): bot-User erstellt Raum, lädt `alex` ein
- Feature-Assertion (UI): alex öffnet Element → sieht Invite-Banner → klickt "Accept" → Raum erscheint in Sidebar

### storageState-Caching-Strategie

`loginViaOidc` ist teuer (~15-20s wegen OIDC Redirect). Daher:
- Einmalig beim ersten Lauf (oder `globalSetup`) für jeden User ausführen
- `storageState` als JSON in `e2e/auth-state/{user}.json` cachen (gitignored)
- Jeder Test öffnet einen neuen Browser-Context mit dem gecachten State → kein SSO-Redirect nötig
- `relogin.spec.ts` testet explizit: gecachter State → kein Login-Screen

---

## Phase 1 — Framework (AC1–AC5)

### AC1: `playwright-bdd` Setup — BDD als Ausführungs-Engine

`playwright-bdd` ist der Playwright-native BDD-Runner: Feature-Files werden via `defineBddConfig` in der `playwright.config.ts` registriert, Steps werden in TypeScript geschrieben, und Tests werden aus dem Feature-File generiert. **Kein separater Cucumber-Runner nötig.**

```bash
# Package hinzufügen
cd e2e && npm install --save-dev playwright-bdd
```

**`e2e/playwright.config.ts` — BDD-Konfiguration:**

```typescript
import { defineConfig, devices } from '@playwright/test';
import { defineBddConfig } from 'playwright-bdd';

const elementWebBdd = defineBddConfig({
  features: 'features/element/**/*.feature',
  steps:    'step-definitions/**/*.ts',
});

export default defineConfig({
  // ... bestehende Konfiguration ...
  projects: [
    // Bestehendes Projekt: API-Contract-Tests (keine Änderung)
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },

    // Bestehendes Projekt: Admin UI BDD (refactored in Phase 3)
    {
      name: 'admin-ui',
      ...defineBddConfig({
        features: 'features/admin/**/*.feature',
        steps:    'step-definitions/**/*.ts',
      }),
      use: { ...devices['Desktop Chrome'], baseURL: 'http://localhost:8008' },
      timeout: 90_000,
    },

    // Neues Projekt: Element Web Browser-First E2E
    {
      name: 'element-web',
      ...elementWebBdd,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070',
        actionTimeout: 20_000,
        navigationTimeout: 45_000,
      },
      timeout: 90_000,
    },
  ],
});
```

**Verzeichnisstruktur:**

```
e2e/
  features/
    element/
      login.feature
      room/
        create.feature
        join.feature
        leave.feature
      messages/
        send.feature
        receive.feature
    admin/
      bootstrap.feature        ← bestehende admin_ui.feature aufgeteilt
      dashboard.feature
      users.feature
      rooms.feature
      audit-log.feature
  step-definitions/
    common/
      auth.steps.ts            ← "Given {word} is logged in" — überall wiederverwendbar
      navigation.steps.ts      ← "When {word} opens Element Web"
      stack-health.steps.ts    ← "Given the stack is running"
    element/
      login.steps.ts
      room.steps.ts
      messages.steps.ts
    admin/
      bootstrap.steps.ts
      dashboard.steps.ts
      users.steps.ts
```

**Acceptance:** `npx playwright test --project=element-web` generiert Tests aus Feature-Files und führt sie aus. Kein `.spec.ts` ohne zugehöriges `.feature`.

### AC2: Fixtures — `users.ts`, `dex-auth.ts`, `element-app.ts`, `nebu-fixtures.ts`

```
e2e/fixtures/
  users.ts             ← NEBU_USERS const + NebUser type
  dex-auth.ts          ← ensureStorageState() pure function + storageState-Fixture
  element-app.ts       ← ElementAppPage (von element-web kopiert + minimale Nebu-Anpassung)
  nebu-fixtures.ts     ← createBdd(test) mit allen Fixtures; exportiert { Given, When, Then }
```

**`fixtures/users.ts`:**
```typescript
export type NebUser = { name: string; email: string; matrixId: string };

export const NEBU_USERS = {
  alex:  { name: 'alex',  email: 'alex@example.com',  matrixId: '@alex:localhost'  },
  marie: { name: 'marie', email: 'marie@example.com', matrixId: '@marie:localhost' },
  tom:   { name: 'tom',   email: 'tom@example.com',   matrixId: '@tom:localhost'   },
  kai:   { name: 'kai',   email: 'kai@example.com',   matrixId: '@kai:localhost'   },
} as const satisfies Record<string, NebUser>;
```

**`fixtures/dex-auth.ts`** — storageState-Cache:
```typescript
// Pure function: login einmal, JSON auf Disk cachen.
// Nutzt loginViaOidc() aus tests/fixtures/oidc.ts (bestehende Impl) —
// fügt nur storageState-Persistenz hinzu.
export async function ensureStorageState(browser: Browser, user: NebUser): Promise<string> {
  const statePath = path.join('e2e/auth-state', `${user.name}.json`);
  if (fs.existsSync(statePath)) return statePath;
  const ctx  = await browser.newContext();
  const page = await ctx.newPage();
  await loginViaOidc(page, user.email, 'changeme');
  await ctx.storageState({ path: statePath });
  await ctx.close();
  return statePath;
}
```

**`fixtures/nebu-fixtures.ts`** — BDD-fähige Fixture-Base:
```typescript
import { test as base, mergeTests } from '@playwright/test';
import { createBdd } from 'playwright-bdd';
import { ElementAppPage } from './element-app';
import { ensureStorageState, NEBU_USERS, type NebUser } from './users';

const test = mergeTests(base).extend<{
  credentials: NebUser;
  app: ElementAppPage;
}>({
  credentials: [NEBU_USERS.alex, { option: true }],
  context: async ({ browser, credentials }, use) => {
    const statePath = await ensureStorageState(browser, credentials);
    const ctx = await browser.newContext({ storageState: statePath });
    await use(ctx);
    await ctx.close();
  },
  app: async ({ page }, use) => {
    await use(new ElementAppPage(page));
  },
});

// createBdd() erzeugt die Given/When/Then Helper die auf den Fixtures basieren
export const { Given, When, Then } = createBdd(test);
export { test };
```

**`fixtures/element-app.ts`** — von element-web kopiert, Nebu-Kürzungen:
```typescript
// Kopiert von element-web/apps/web/playwright/pages/ElementAppPage.ts
// Entfernt: crypto, Spotlight (nicht in Nebu MVP)
// Behalten: getComposerField, openCreateRoomDialog, viewRoomByName,
//           viewRoomById, inviteUserToCurrentRoom, openUserMenu, closeDialog
export class ElementAppPage { ... }
```

**Acceptance:** `import { Given, When, Then } from '../../fixtures/nebu-fixtures'` funktioniert in allen Step-Definition-Dateien.

### AC3: `global-setup.ts` + `e2e/auth-state/` — Auth-Warming

```typescript
// e2e/global-setup.ts
// Wärmt storageState für alex + marie vor (häufigste Test-User).
// Wird übersprungen wenn auth-state/alex.json bereits existiert.
export default async function globalSetup() {
  if (fs.existsSync('e2e/auth-state/alex.json')) return;
  const browser = await chromium.launch();
  await Promise.all([
    ensureStorageState(browser, NEBU_USERS.alex),
    ensureStorageState(browser, NEBU_USERS.marie),
  ]);
  await browser.close();
}
```

**`.gitignore` Eintrag:** `e2e/auth-state/`

**Acceptance:** Nach dem ersten `npx playwright test --project=element-web` existiert `auth-state/alex.json` mit gültigem localStorage-State (`mx_access_token` Key vorhanden).

### AC4: Shared Step Definitions — `step-definitions/common/`

Die Common-Steps werden in ALLEN Feature-Files verwendet. Sie werden **einmal** definiert und cross-feature wiederverwendet:

```
e2e/step-definitions/
  common/
    auth.steps.ts         ← "Given {word} is logged in via Element Web"
    navigation.steps.ts   ← "When {word} navigates to Element Web"
    stack-health.steps.ts ← "Given the Nebu stack is running"
    room-setup.steps.ts   ← "Given a room {string} exists and {word} is a member"
    assertions.steps.ts   ← "Then the room list is visible", "Then no error dialog appears"
```

**`common/auth.steps.ts`** — Wiederverwendbarer Login-Step:
```typescript
import { Given } from '../../fixtures/nebu-fixtures';
import { NEBU_USERS } from '../../fixtures/users';

// Wiederverwendet in: login.feature, room/create.feature, messages/send.feature, ...
Given('{word} is logged in via Element Web', async ({ page, browser }, userName: string) => {
  const user = NEBU_USERS[userName as keyof typeof NEBU_USERS];
  const statePath = await ensureStorageState(browser, user);
  // Context wurde bereits mit storageState geöffnet (via fixture) — page ist ready
  await page.goto('/');
  await page.locator('.mx_LeftPanel').waitFor({ state: 'visible', timeout: 20_000 });
});
```

**`common/stack-health.steps.ts`** — Skip-Guard für jeden Test:
```typescript
Given('the Nebu stack is running', async ({ request }) => {
  const [element, dex] = await Promise.all([
    request.get('http://localhost:7070').catch(() => null),
    request.get('http://localhost:5556/dex/.well-known/openid-configuration').catch(() => null),
  ]);
  if (!element?.ok() || !dex?.ok()) {
    // playwright-bdd: test.skip() in a step
    throw new Error('SKIP: Stack not running. Run: make dev');
  }
});
```

**`common/room-setup.steps.ts`** — Bot-Setup-Step (API, kein UI):
```typescript
Given('a room {string} exists and {word} is a member', async ({ request }, roomName: string, userName: string) => {
  // Kai als Bot: erstellt Raum + lädt User ein (via Matrix API)
  const kaiSession = await getApiSession(request, NEBU_USERS.kai);
  const { room_id } = await createRoom(request, kaiSession.token, roomName);
  const targetUser = NEBU_USERS[userName as keyof typeof NEBU_USERS];
  await inviteUser(request, kaiSession.token, room_id, targetUser.matrixId);
  // Raum-ID in World-State speichern für nachfolgende Steps
  this.roomId = room_id;
});
```

**Acceptance:** Step `Given the Nebu stack is running` erscheint in allen 8 Feature-Files. Kein Duplikat.

### AC5: `e2e/auth-state/` + CI-Targets

```bash
# Makefile-Targets hinzufügen:
test-e2e-element:
	cd e2e && npx bddgen && npx playwright test --project=element-web --reporter=list

test-e2e-admin:
	cd e2e && npx bddgen && npx playwright test --project=admin-ui --reporter=list

test-e2e:
	cd e2e && npx bddgen && npx playwright test --reporter=list
```

`npx bddgen` generiert die `.spec.ts`-Dateien aus den Feature-Files (playwright-bdd Codegen-Step).

---

## Phase 2 — Feature Tests (AC6–AC13)

**Design-Prinzip:** Ein `.feature`-File pro Thema. Kein Mega-Feature. Setup via API erlaubt, Feature-Assertion **nur via UI**. Shared Steps aus `common/` überall wiederverwendbar.

### Verzeichnisstruktur:

```
e2e/features/element/
  login.feature
  room/
    create.feature
    join.feature
    leave.feature
  messages/
    send.feature
    receive.feature

e2e/step-definitions/element/
  login.steps.ts
  room.steps.ts
  messages.steps.ts
```

### AC6: `features/element/login.feature` + `step-definitions/element/login.steps.ts`

**Feature-File:**
```gherkin
Feature: Element Web Login

  Background:
    Given the Nebu stack is running

  Scenario: SSO Login via Dex zeigt Room-Liste
    Given alex has no cached session
    When alex opens Element Web and clicks "Sign in"
    And alex authenticates via Dex with "alex@example.com"
    Then the room list is visible
    And no error dialog appears

  Scenario: Logout leitet auf Welcome-Screen zurück
    Given alex is logged in via Element Web
    When alex opens the user menu and clicks "Sign out"
    Then the welcome screen is visible
    And the "Sign in" button is present

  Scenario: Gespeicherte Session — kein OIDC-Redirect beim Reload
    Given alex is logged in via Element Web
    When alex reloads Element Web
    Then the room list is visible without a Dex redirect
```

**Wiederverwendete Steps:** `Given the Nebu stack is running`, `Given alex is logged in via Element Web` → aus `common/`.

**Neue Steps in `login.steps.ts`:** `Given alex has no cached session`, `When alex opens Element Web and clicks "Sign in"`, `When alex authenticates via Dex with {string}`, `When alex reloads Element Web`, `Then the welcome screen is visible`, `Then the room list is visible without a Dex redirect`.

### AC7: `features/element/room/create.feature` + Steps

```gherkin
Feature: Room Creation

  Background:
    Given the Nebu stack is running
    And alex is logged in via Element Web

  Scenario: Neuen Raum via UI erstellen
    When alex opens the "New room" dialog
    And alex enters room name "e2e-create-<timestamp>"
    And alex clicks "Create room"
    Then the room "e2e-create-<timestamp>" appears in the sidebar
    And the room header shows "e2e-create-<timestamp>"
```

**Wiederverwendete Steps:** `Given the Nebu stack is running`, `Given alex is logged in via Element Web`.

**Neue Steps:** `When {word} opens the "New room" dialog` → `app.openCreateRoomDialog()`, `Then the room {string} appears in the sidebar` → `app.viewRoomByName()`.

### AC8: `features/element/room/join.feature` + Steps

```gherkin
Feature: Room Join via Invite

  Background:
    Given the Nebu stack is running
    And a room "invite-test-<timestamp>" exists and kai is the owner

  Scenario: Invite akzeptieren via UI
    Given kai has invited alex to "invite-test-<timestamp>"
    And alex is logged in via Element Web
    When the invite for "invite-test-<timestamp>" appears in alex's sidebar
    And alex clicks "Accept"
    Then the room "invite-test-<timestamp>" appears in the sidebar
```

**Wiederverwendete Steps:** `Given the Nebu stack is running`, `Given alex is logged in via Element Web`.

**Neue Steps:** `Given kai has invited {word} to {string}` → Matrix API `POST /invite`, `When the invite for {string} appears in {word}'s sidebar`, `When {word} clicks "Accept"`.

### AC9: `features/element/room/leave.feature` + Steps

```gherkin
Feature: Room Leave

  Background:
    Given the Nebu stack is running
    And a room "leave-test-<timestamp>" exists and alex is a member
    And alex is logged in via Element Web

  Scenario: Raum verlassen via UI
    When alex navigates to room "leave-test-<timestamp>"
    And alex opens the room menu and clicks "Leave room"
    And alex confirms leaving
    Then the room "leave-test-<timestamp>" is not in alex's sidebar
```

**Wiederverwendete Steps:** `Given the Nebu stack is running`, `Given a room {string} exists and {word} is a member`, `Given {word} is logged in via Element Web`.

### AC10: `features/element/messages/send.feature` + Steps

```gherkin
Feature: Message Send

  Background:
    Given the Nebu stack is running
    And a room "msg-send-<timestamp>" exists and alex is a member
    And alex is logged in via Element Web

  Scenario: Nachricht senden erscheint in Timeline
    When alex navigates to room "msg-send-<timestamp>"
    And alex types "hello e2e <timestamp>" in the composer
    And alex presses Enter
    Then the message "hello e2e <timestamp>" is visible in the timeline
    And the message shows no error status
```

**Neue Steps:** `When {word} types {string} in the composer` → `app.getComposerField().fill()`, `When {word} presses Enter`, `Then the message {string} is visible in the timeline` → `.mx_EventTile` assertion.

### AC11: `features/element/messages/receive.feature` + Steps

```gherkin
Feature: Message Receive

  Background:
    Given the Nebu stack is running
    And a room "msg-recv-<timestamp>" exists and alex is a member
    And marie is a member of room "msg-recv-<timestamp>"

  Scenario: User A sendet, User B empfängt in Timeline
    Given alex is logged in via Element Web
    And marie is logged in via Element Web in a second browser context
    When alex navigates to room "msg-recv-<timestamp>"
    And alex sends "message from alex <timestamp>"
    Then marie sees "message from alex <timestamp>" in her timeline for "msg-recv-<timestamp>"
```

**Multi-Context Step:** `Given {word} is logged in via Element Web in a second browser context` → öffnet zweiten `browser.newContext()` mit `marie`'s storageState. Die `World` speichert beide Pages.

---

## Phase 3 — Admin UI Retro-fit (AC12–AC13)

Die bestehende `e2e/features/admin_ui.feature` ist bisher nur ein Spezifikations-Dokument, nicht ausführbar. Phase 3 verdrahtet sie mit `playwright-bdd`.

### AC12: Bestehende Admin-UI-Feature aufteilen

`e2e/features/admin/` (neu, aufgeteilt aus `admin_ui.feature`):

```
e2e/features/admin/
  bootstrap.feature      ← Scenario 1+4 aus admin_ui.feature
  dashboard.feature      ← Scenario 2 aus admin_ui.feature
  auth-guard.feature     ← Scenario 3 aus admin_ui.feature
  users.feature          ← Users-Page (neu, aus admin-specs abgeleitet)
  rooms.feature          ← Rooms-Page (neu)
  audit-log.feature      ← Audit-Log (neu)
```

Die bestehende `e2e/features/admin_ui.feature` wird durch die aufgeteilten Files ersetzt (oder als Legacy behalten und deprecated markiert).

### AC13: Admin Step Definitions

```
e2e/step-definitions/admin/
  bootstrap.steps.ts     ← Schritte aus admin_ui.feature Scenario 1+4
  dashboard.steps.ts     ← Schritte aus admin_ui.feature Scenario 2
  users.steps.ts         ← User-Management Steps
  rooms.steps.ts         ← Room-Management Steps
```

Wiederverwendete Common-Steps: `Given the Nebu stack is running`, `Given the operator is logged in as admin` (äquivalent zu `Given {word} is logged in`).

**Acceptance:** `npx playwright test --project=admin-ui` führt alle Admin-Feature-Scenarios aus. Bestehende Admin-Spec-Files (`admin/*.spec.ts`) können nach Phase 3 gelöscht werden, da sie durch die BDD-generierten Specs abgelöst sind.

---

## Acceptance Tests (für `/bmad-testarch-atdd`)

Diese werden **ZUERST geschrieben** (Red Phase) bevor die Implementierung beginnt.

### Phase 1 — Red Phase:

```
1. playwright-bdd installiert und konfiguriert [Config]
   - Given: package.json ohne playwright-bdd
   - When: npm install playwright-bdd && npx bddgen
   - Then: kein Fehler; features/element/login.feature wird verarbeitet

2. nebu-fixtures.ts: createBdd(test) exportiert Given/When/Then [Unit-artig]
   - Given: leere Feature-File + leerer Step
   - When: npx bddgen && npx playwright test --project=element-web
   - Then: 1 Test generiert, grün (pending step wird als skip gewertet)

3. ensureStorageState() erzeugt auth-state/alex.json [Playwright]
   - Given: kein auth-state/alex.json, Stack running
   - When: globalSetup ausgeführt
   - Then: auth-state/alex.json existiert, enthält "mx_access_token" in localStorage

4. Common-Step "Given the Nebu stack is running" skippt korrekt [Playwright]
   - Given: Stack NICHT running
   - When: Feature mit dem Step ausgeführt
   - Then: Test wird als skipped markiert, kein Failure
```

### Phase 2 — Red Phase (Feature-Files zuerst, dann Steps):

```
5. login.feature: SSO Login Scenario [Playwright, Browser]
   - Failing: Step "alex authenticates via Dex" existiert noch nicht
   - Failing assert (wenn Step stub): .mx_LeftPanel nicht sichtbar

6. login.feature: Logout Scenario [Playwright, Browser]
   - Failing assert: Welcome-Screen nicht sichtbar nach Logout

7. login.feature: Relogin Scenario [Playwright, Browser]
   - Failing assert: page hat /dex/auth in URL (kein storageState)

8. room/create.feature [Playwright, Browser]
   - Failing assert: room-list enthält nicht "e2e-create-*"

9. room/join.feature [Playwright, Browser]
   - Failing assert: invite-section enthält nicht "invite-test-*"

10. room/leave.feature [Playwright, Browser]
    - Failing assert: room-list enthält noch "leave-test-*" (sollte weg sein)

11. messages/send.feature [Playwright, Browser]
    - Failing assert: .mx_EventTile mit "hello e2e *" nicht sichtbar

12. messages/receive.feature [Playwright, Browser]
    - Failing assert: marie's Page: .mx_EventTile mit "message from alex *" nicht sichtbar
```

---

## Implementation Guide

### Prerequisites

```bash
# Stack muss laufen (inkl. element-web auf Port 7070)
make dev

# /etc/hosts muss enthalten (für Dex-Redirect)
echo "127.0.0.1 dex" | sudo tee -a /etc/hosts

# playwright-bdd installieren
cd e2e && npm install --save-dev playwright-bdd

# auth-state Verzeichnis anlegen + gitignore
mkdir -p e2e/auth-state
echo "auth-state/" >> e2e/.gitignore
```

### Reihenfolge der Implementierung

```
Phase 1 (Framework) — einmalig:
1. e2e/fixtures/users.ts
2. e2e/fixtures/dex-auth.ts
3. e2e/fixtures/element-app.ts        ← von element-web kopiert
4. e2e/fixtures/nebu-fixtures.ts      ← createBdd(mergeTests(...))
5. e2e/playwright.config.ts           ← defineBddConfig für beide Projekte
6. e2e/global-setup.ts
7. e2e/step-definitions/common/       ← auth, navigation, stack-health, room-setup

Phase 2 (Feature Tests) — jeweils: Feature-File → Step-Defs → Green:
8.  features/element/login.feature          + step-definitions/element/login.steps.ts
9.  features/element/room/create.feature    + room.steps.ts (Create-Steps)
10. features/element/room/join.feature      + room.steps.ts (Join-Steps)
11. features/element/room/leave.feature     + room.steps.ts (Leave-Steps)
12. features/element/messages/send.feature  + messages.steps.ts (Send-Steps)
13. features/element/messages/receive.feature + messages.steps.ts (Receive-Steps)

Phase 3 (Admin Retro-fit):
14. features/admin/*.feature               ← aus admin_ui.feature aufteilen
15. step-definitions/admin/*.steps.ts      ← bestehende Spec-Logik überführen
```

### Key Selectors (aus element-web ElementAppPage)

```typescript
'.mx_LeftPanel'                                            // Room-Liste
page.getByTestId('room-list').locator(`[title="${name}"]`) // Raum in Sidebar
'.mx_RoomView_body .mx_MessageComposer div[contenteditable]' // Composer
'.mx_EventTile'                                            // Timeline-Event
'.mx_CreateRoomDialog'                                     // Create-Room-Dialog
page.getByRole('group', { name: /invites/i })              // Invite-Sektion
```

### Dex Consent-Screen Handling

```typescript
// Nach Dex-Form-Submit: Consent-Screen optional abfangen (taucht nur beim Erstlogin auf)
const consentBtn = page.getByRole('button', { name: /grant access|allow/i });
if (await consentBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
  await consentBtn.click();
}
```

---

## Risiken

| Risiko | Impact | Mitigation |
|---|---|---|
| Element Web-Selektoren ändern sich | HIGH | ElementAppPage als Puffer; Selektoren an einer Stelle |
| storageState expired | MEDIUM | `auth-state/` beim nächsten Lauf automatisch refreshed wenn Datei > 12h alt |
| Dex Consent-Screen sporadisch | MEDIUM | Bestehende `loginViaOidc()` behandelt ihn bereits |
| Element Web nicht erreichbar | LOW | `Given the Nebu stack is running` skippt alle Tests automatisch |
| Race condition receive.feature | MEDIUM | `expect(...).toBeVisible({ timeout: 20_000 })` für Sync-Wait |
| playwright-bdd codegen-Step vergessen | MEDIUM | `make test-e2e-element` immer mit `npx bddgen &&` prefixen |

---

## DoD (Definition of Done)

**Phase 1 — Framework:**
- [x] `playwright-bdd` installiert, `npx bddgen` ohne Fehler ausführbar
- [x] `fixtures/nebu-fixtures.ts` exportiert `{ Given, When, Then, test }` via `createBdd`
- [x] `fixtures/element-app.ts` vorhanden mit `getComposerField`, `openCreateRoomDialog`, `viewRoomByName`
- [x] `global-setup.ts` erzeugt `auth-state/alex.json` beim ersten Lauf
- [x] `e2e/auth-state/` in `.gitignore` eingetragen
- [x] `step-definitions/common/` enthält: `auth.steps.ts`, `navigation.steps.ts`, `stack-health.steps.ts`, `room-setup.steps.ts`
- [x] `npx playwright test --project=element-web --list` zeigt 6+ Scenarios (8 Scenarios)

**Phase 2 — Feature Tests:**
- [x] Jedes der 6 Feature-Files hat ein zugehöriges Step-Definition-File — kein `.spec.ts` ohne `.feature`
- [x] Alle 6 Feature-Files grün: login (3 Scenarios), room/create, room/join, room/leave, messages/send, messages/receive
- [x] `make test-e2e-element` läuft durch ohne Fehler (headed und headless)
- [x] Jeder gemeinsame Step (`Given the Nebu stack is running`, `Given {word} is logged in`) ist in `common/` definiert, **nicht** dupliziert
- [x] Code Review: kein direkt-API-Call in `Then`-Steps (nur in `Given`-Setup-Steps)

**Phase 3 — Admin Retro-fit:**
- [x] `features/admin/` enthält mindestens: `bootstrap.feature`, `dashboard.feature`, `auth-guard.feature`
- [x] `npx playwright test --project=admin-ui` führt Admin-Scenarios aus
- [ ] Bestehende Admin-Spec-Files (`admin/*.spec.ts`) gelöscht oder mit `// @deprecated` markiert (bewusst behalten — Migrationsstrategie Phase 3 completed)
- [x] Bestehende API-Contract-Tests (`matrix_api.spec.ts` etc.) unverändert grün

---

## Dev Agent Record

### Implementation Notes

**Story 9-26 — Implemented 2026-05-07**

All MINOR findings from pre-dev review addressed:

- **F-01:** `messages.steps.ts` uses `secondPage` fixture instead of module-level context variable
- **F-02:** `auth.steps.ts` guard throws if `userName !== credentials.name`  
- **F-03:** `global-setup.ts` checks both `alex.json` AND `marie.json` before skipping warm-up
- **F-04:** `stack-health.steps.ts` uses `$test.skip()` (playwright-bdd fixture) instead of `throw Error('SKIP:...')`
- **F-05:** Makefile has `test-e2e-element`, `test-e2e-admin` as canonical targets; `test-e2e` runs all tests (BDD + legacy); old `test-e2e-element-bdd` / `test-e2e-admin-bdd` are aliases
- **F-08:** `room.steps.ts` uses `getByLabel(/room name/i)` / `getByRole('textbox', { name: /name/i })` instead of fragile `input[id="textinput_0"]`
- **F-09:** `messages.steps.ts` uses `pressSequentially()` instead of `fill()` for contenteditable div
- **F-10/F-11:** Created `e2e/step-definitions/admin/rooms.steps.ts` with room-list and audit-log assertions moved from `users.steps.ts`
- **F-12:** Implemented `getApiSession()`, `createRoom()`, `inviteUser()` with proper 401/403/429 handling per Matrix spec

**Framework details:**
- `playwright-bdd` v8.5.0 installed and configured
- `defineBddProject()` used for multi-project setup (not `defineBddConfig()`)
- `importTestFrom: 'fixtures/nebu-fixtures.ts'` registered so bddgen finds the custom `test` instance
- `defineBddConfig()` returns outputDir string — must use `defineBddProject()` for named projects
- Generated specs in `.features-gen/` (gitignored)
- `npx bddgen` generates 12 spec files (6 element-web + 6 admin-ui)
- `npx playwright test --project=element-web --list` shows 8 scenarios
- `npx playwright test --project=admin-ui --list` shows 7 scenarios

**File List (new/modified):**
- `e2e/package.json` — added playwright-bdd dependency + bddgen scripts
- `e2e/playwright.config.ts` — BDD config with defineBddProject
- `e2e/global-setup.ts` — F-03 fix (both alex+marie freshness check)
- `e2e/.gitignore` — added `.bdd-gen/` and `.features-gen/`
- `e2e/fixtures/users.ts` — complete (was already done)
- `e2e/fixtures/dex-auth.ts` — full implementation (ensureStorageState, getApiSession, createRoom, inviteUser)
- `e2e/fixtures/element-app.ts` — full implementation (all stubs replaced)
- `e2e/fixtures/nebu-fixtures.ts` — uses playwright-bdd `test` as base
- `e2e/step-definitions/common/auth.steps.ts` — F-02 guard implemented
- `e2e/step-definitions/common/navigation.steps.ts` — @ts-expect-error removed
- `e2e/step-definitions/common/assertions.steps.ts` — @ts-expect-error removed
- `e2e/step-definitions/common/room-setup.steps.ts` — @ts-expect-error removed
- `e2e/step-definitions/common/stack-health.steps.ts` — F-04 ($test.skip)
- `e2e/step-definitions/element/login.steps.ts` — full loginViaOidc implementation
- `e2e/step-definitions/element/room.steps.ts` — F-08 (getByLabel)
- `e2e/step-definitions/element/messages.steps.ts` — F-09 (pressSequentially)
- `e2e/step-definitions/admin/bootstrap.steps.ts` — @ts-expect-error removed
- `e2e/step-definitions/admin/dashboard.steps.ts` — @ts-expect-error removed
- `e2e/step-definitions/admin/users.steps.ts` — F-10 (room/audit steps removed)
- `e2e/step-definitions/admin/rooms.steps.ts` — NEW (F-11)
- `Makefile` — F-05 (test-e2e-element, test-e2e-admin aliases)
- `docs/stories/9-26-element-web-e2e-browser-first-suite.md` — this file

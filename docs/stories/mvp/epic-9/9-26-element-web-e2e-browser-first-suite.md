---
story_id: 9-26
title: "Element Web E2E Suite — Browser-First Feature Tests via Element Web UI"
type: feature
severity: high
epic: 9
status: done
security_review: not-needed
created: 2026-05-07
completed: 2026-05-07
ci_gate: PASS (14/15 pass, 1 skip; M-1 regression stable)
---

## Summary

Die bestehenden E2E-Tests unter `e2e/tests/features/` sind **keine echten UI-Tests** — sie melden sich via SSO an und fahren dann direkt die Matrix API (`page.request.post`). Der echte Element Web Client wird nicht getestet.

Diese Story ersetzt diesen Ansatz durch eine **Browser-First E2E Suite**, die ausschließlich über die Element Web UI interagiert: Button-Klicks, Formular-Eingaben, Timeline-Assertions. Jede Feature-Interaktion läuft über das echte Element Web Interface — genau so wie ein menschlicher Nutzer.

**Struktur spiegelt element-web/playwright/e2e:** Kleine, fokussierte Specs pro Feature, organisiert in `login/`, `room/`, `messages/`. Kein Mega-Test.

**Zwei Phasen — klar getrennt:**

1. **Phase 1 — Framework:** Fixtures, storageState-Caching, ElementAppPage, playwright config
2. **Phase 2 — Feature Tests:** Kleine Specs (login, logout, relogin, room/create, room/join, room/leave, messages/send, messages/receive)

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

## Phase 1 — Framework (AC1–AC4)

### AC1: Fixture-Datei `e2e/fixtures/nebu-fixtures.ts` — Merged Test Base

Erstelle `e2e/fixtures/nebu-fixtures.ts` als Ersatz für element-web's `element-web-test.ts`:

```
e2e/fixtures/
  nebu-fixtures.ts     ← mergeTests: base + credentials + app fixture
  element-app.ts       ← ElementAppPage (adaptiert aus element-web)
  users.ts             ← NEBU_USERS const + NebUserCredentials type
  dex-auth.ts          ← loginViaDex() pure function + storageState cache fixture
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

**`fixtures/dex-auth.ts`** — storageState-Cache-Fixture:
```typescript
// Pure function: führt OIDC-Flow durch, gibt storageState-Pfad zurück.
// Nutzt die bestehende loginViaOidc() aus tests/fixtures/oidc.ts —
// ergänzt nur die storageState-Persistenz.
export async function ensureStorageState(
  browser: Browser, user: NebUser, password: string
): Promise<string> {
  const statePath = path.join('e2e/auth-state', `${user.name}.json`);
  if (fs.existsSync(statePath)) return statePath;

  const ctx  = await browser.newContext();
  const page = await ctx.newPage();
  await loginViaOidc(page, user.email, password); // bestehende Impl wiederverwenden
  await ctx.storageState({ path: statePath });
  await ctx.close();
  return statePath;
}

// Fixture: liefert credentials + stellt sicher dass storageState existiert
export const test = base.extend<{
  credentials: NebUser;
  storageStatePath: string;
}>({
  credentials: [NEBU_USERS.alex, { option: true }],
  storageStatePath: async ({ browser, credentials }, use) => {
    const p = await ensureStorageState(browser, credentials, 'changeme');
    await use(p);
  },
});
```

**`fixtures/nebu-fixtures.ts`** — Merged Base:
```typescript
// Browser-Context wird mit gespeichertem Auth-State geöffnet
// → kein OIDC-Redirect in jedem Test
export const test = mergeTests(base, dexAuthFixture).extend<{
  app: ElementAppPage;
}>({
  context: async ({ browser, storageStatePath }, use) => {
    const ctx = await browser.newContext({ storageState: storageStatePath });
    await use(ctx);
    await ctx.close();
  },
  app: async ({ page }, use) => {
    await use(new ElementAppPage(page));
  },
});

export { expect } from '@playwright/test';
```

**Acceptance:** Import `{ test, expect }` from `fixtures/nebu-fixtures.ts` in jedem Feature-Spec möglich.

### AC2: `e2e/fixtures/element-app.ts` — ElementAppPage für Nebu

Adaptiere `ElementAppPage` aus element-web. Kopiere die Klasse, passe nur die Dinge an die Nebu-spezifisch sind:

Methoden die direkt übernommen werden (keine Änderung nötig):
- `getComposerField()` → `.mx_MessageComposer div[contenteditable]`
- `openCreateRoomDialog()` → "New conversation" button → "New room" menu item
- `viewRoomByName(name)` → room list `[title="${name}"]`
- `viewRoomById(roomId)` → `page.goto('/#/room/${roomId}')`
- `inviteUserToCurrentRoom(userId)` → "Room info" → "Invite" dialog

Methoden die wegfallen (Nebu MVP):
- `openSpotlight()` — nur wenn Spotlight in Nebu verfügbar (skip if not)
- `settings` — nur für `openUserMenu()` und `closeDialog()`
- `crypto` — nicht in Nebu MVP

**Acceptance:** `app.getComposerField()`, `app.openCreateRoomDialog()`, `app.viewRoomByName()` sind einsetzbar in Feature-Tests.

### AC3: `e2e/playwright.config.ts` — Neues `element-web` Projekt

Füge ein zweites Playwright-Projekt hinzu (bestehendes `chromium` Projekt bleibt unverändert):

```typescript
projects: [
  // Bestehendes Projekt: API-Contract-Tests (keine Änderung)
  { name: 'chromium', use: { ...devices['Desktop Chrome'] } },

  // Neues Projekt: Browser-First E2E via Element Web
  {
    name: 'element-web',
    testDir: './tests/element',
    use: {
      ...devices['Desktop Chrome'],
      baseURL: process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070',
      actionTimeout: 20_000,
      navigationTimeout: 45_000,
    },
    timeout: 90_000,
  },
],
```

**Separate Test-Directory:** `e2e/tests/element/` — damit `make test-e2e-element` nur diese Specs laufen kann.

**Acceptance:** `npx playwright test --project=element-web` startet nur die Browser-First-Tests.

### AC4: `e2e/auth-state/` + `global-setup.ts` — Auth-Warming

```
e2e/auth-state/          ← gitignored; enthält alex.json, marie.json, tom.json, kai.json
e2e/global-setup.ts      ← Wärmt storageState für alex + marie (die häufigsten Test-User) vor
```

`global-setup.ts` wird nur ausgeführt wenn `auth-state/alex.json` noch nicht existiert. Dann überspringen, sonst `loginViaOidc` für beide User aufrufen.

**`.gitignore` Eintrag:** `e2e/auth-state/`

**Acceptance:** Nach `npx playwright test --project=element-web` existiert `auth-state/alex.json` mit gültigem localStorage-State.

---

## Phase 2 — Feature Tests (AC5–AC12)

**Design-Prinzip:** Jeder Spec testet **genau eine Sache**. Kein Mega-Test. Setup via API erlaubt, Feature-Assertion **nur via UI**.

### Verzeichnisstruktur (spiegelt element-web):

```
e2e/tests/element/
  login/
    login.spec.ts       ← SSO Login → Room-Liste sichtbar
    logout.spec.ts      ← Logout → Welcome-Screen
    relogin.spec.ts     ← storageState → kein OIDC-Redirect beim Reload
  room/
    create.spec.ts      ← Create Room Button → Name eingeben → Raum in Sidebar
    join.spec.ts        ← Invite via API-Bot → Element zeigt Invite → Accept → Raum in Sidebar
    leave.spec.ts       ← In Raum → Leave → Raum verschwindet aus Sidebar
  messages/
    send.spec.ts        ← Message tippen → senden → in Timeline sichtbar
    receive.spec.ts     ← User A sendet → User B (zweiter Context) sieht Nachricht in Timeline
```

### AC5: `login/login.spec.ts`

```
Scenario: SSO Login via Dex zeigt Room-Liste
  Given Element Web ist erreichbar (http://localhost:7070)
  And Dex ist erreichbar (http://localhost:5556)
  When der Test OHNE gespeicherten storageState startet
  And auf "Sign in" geklickt wird
  And auf "Continue with SSO" geklickt wird
  And Dex-Formular mit alex@example.com / changeme ausgefüllt wird
  Then wird nach Dex-Redirect auf localhost:7070 weitergeeleitet
  And die Room-Liste (.mx_LeftPanel) ist sichtbar
  And kein Error-Dialog erscheint
```

**Besonderheit:** Dieser Test nutzt KEINEN gecachten storageState — er testet den echten Login-Flow.

### AC6: `login/logout.spec.ts`

```
Scenario: Logout leitet auf Welcome-Screen zurück
  Given alex ist eingeloggt (gecachter storageState)
  And Element Web zeigt die Room-Liste
  When User Menu geöffnet wird (app.openUserMenu())
  And "Sign out" / "Abmelden" geklickt wird
  And ggf. "Sign out" im Bestätigungsdialog bestätigt wird
  Then leitet Element auf Welcome-Screen (/welcome oder #/login) weiter
  And der "Sign in" Button ist sichtbar
  And der gecachte storageState wird für den nächsten Test invalidiert
```

**Cleanup:** Nach dem Test `auth-state/alex.json` löschen, damit nächster Testlauf neu einloggt.

### AC7: `login/relogin.spec.ts`

```
Scenario: Gecachter storageState — kein OIDC-Redirect beim Reload
  Given auth-state/alex.json existiert (vorheriger Login)
  When Browser-Context mit storageState geöffnet wird
  And page.goto('http://localhost:7070') aufgerufen wird
  Then erscheint KEIN Dex-Login-Formular
  And die Room-Liste ist sofort sichtbar (kein /dex/auth in URL)
  And kein Error-Dialog erscheint
```

**Zweck:** Beweist dass storageState-Caching funktioniert und Tests nicht jedesmal den OIDC-Flow durchfahren müssen.

### AC8: `room/create.spec.ts`

```
Scenario: Neuen Raum via Element Web UI erstellen
  Given alex ist eingeloggt (storageState)
  When app.openCreateRoomDialog() aufgerufen wird
  And Raum-Name "e2e-create-{timestamp}" eingegeben wird
  And "Create room" Button geklickt wird
  Then erscheint der Raum in der Sidebar (app.viewRoomByName())
  And der Room-Header zeigt den Raum-Namen
```

**Cleanup:** Nach dem Test den erstellten Raum via API verlassen (Raum bleibt aber im Server — kein Delete nötig).

### AC9: `room/join.spec.ts`

```
Scenario: Invite akzeptieren via Element Web UI
  Given Bot (kai via page.request API) hat einen Raum erstellt
  And Bot hat alex eingeladen (POST /rooms/{id}/invite)
  And alex öffnet Element (storageState)
  When die Invite-Benachrichtigung erscheint (room list oder toast)
  And alex klickt "Accept" / "Annehmen"
  Then erscheint der Raum in alex' Sidebar
  And der Raum-Header zeigt den Raum-Namen
```

**Hinweis:** Das Invite-Banner erscheint in der Sidebar als Raum mit "?" oder "Invite"-Indikator — Element zeigt es normalerweise als eigene Sektion "Invites" in der Sidebar an.

### AC10: `room/leave.spec.ts`

```
Scenario: Raum verlassen via Element Web UI
  Given Bot hat Raum erstellt, alex ist Mitglied (via API-Setup)
  And alex öffnet Element, navigiert zu dem Raum
  When alex das Room-Menü öffnet (drei Punkte oder Einstellungen)
  And "Leave room" / "Raum verlassen" wählt
  And ggf. den Bestätigungsdialog bestätigt
  Then verschwindet der Raum aus alex' Sidebar
  And Matrix API GET /sync zeigt kein leave event für den Raum (optional: API-Verifikation)
```

### AC11: `messages/send.spec.ts`

```
Scenario: Nachricht senden via Element Web Composer
  Given alex ist eingeloggt (storageState)
  And alex ist in einem Raum (via API-Setup oder vorher erstellt)
  When app.getComposerField() wird fokussiert
  And Text "hello e2e {timestamp}" getippt wird
  And Enter gedrückt wird
  Then erscheint die Nachricht als .mx_EventTile in der Timeline
  And .mx_EventTile enthält den Text "hello e2e {timestamp}"
  And kein Fehler-Status am Tile (kein "red exclamation" icon)
```

### AC12: `messages/receive.spec.ts`

```
Scenario: Nachricht von User A empfangen in User B's Timeline
  Given alex ist in einem Raum (storageState)
  And marie ist im selben Raum (zweiter browser.newContext() mit marie storageState)
  When alex tippt und sendet "message from alex {timestamp}"
  Then erscheint "message from alex {timestamp}" in marie's Timeline
  And die Assertion läuft auf marie's Page (zweiter Browser-Context)
```

**Multi-Context Pattern:** Zwei `browser.newContext()` Instanzen, eine pro User. Beide haben gespeicherte storageStates. Alex sendet auf `page1`, assertion auf `page2`.

---

## Acceptance Tests (für `/bmad-testarch-atdd`)

Diese Tests werden **ZUERST geschrieben** (Red Phase), bevor die Implementierung beginnt.

### Framework-Tests (Phase 1 — Red Phase):

```
1. storageState-Caching [Playwright]
   - Given: kein auth-state/alex.json
   - When: ensureStorageState(browser, alex, 'changeme') aufgerufen
   - Then: auth-state/alex.json existiert, enthält localStorage mit mx_access_token

2. ElementAppPage.openCreateRoomDialog() [Playwright]
   - Given: alex eingeloggt via storageState
   - When: app.openCreateRoomDialog() aufgerufen
   - Then: .mx_CreateRoomDialog ist sichtbar

3. `element-web` Playwright-Projekt läuft isoliert [Config-Test]
   - Given: playwright.config.ts mit zwei Projekten
   - When: npx playwright test --project=element-web --list
   - Then: Listet nur Tests aus e2e/tests/element/
```

### Feature-Tests (Phase 2 — Red Phase):

```
4. login.spec.ts [Playwright, Browser]
   - Failing assert: expect(page.locator('.mx_LeftPanel')).toBeVisible()
   - (Failgrund: Test nutzt noch keinen OIDC-Flow oder storageState)

5. logout.spec.ts [Playwright, Browser]
   - Failing assert: expect(page.getByRole('link', { name: /sign in/i })).toBeVisible()

6. relogin.spec.ts [Playwright, Browser]
   - Failing assert: expect(page).not.toHaveURL(/dex.*\/auth/)
   - Und: expect(page.locator('.mx_LeftPanel')).toBeVisible()

7. room/create.spec.ts [Playwright, Browser]
   - Failing assert: expect(page.locator(`[title="e2e-create-*"]`)).toBeVisible()

8. room/join.spec.ts [Playwright, Browser]
   - Failing assert: expect(page.locator(`[title="invited-room-*"]`)).toBeVisible()

9. room/leave.spec.ts [Playwright, Browser]
   - Failing assert: expect(page.locator(`[title="leave-test-room-*"]`)).not.toBeVisible()

10. messages/send.spec.ts [Playwright, Browser]
    - Failing assert: expect(page.locator('.mx_EventTile').getByText(/hello e2e/)).toBeVisible()

11. messages/receive.spec.ts [Playwright, Browser]
    - Failing assert: Auf marie's Page: expect(mariePage.locator('.mx_EventTile').getByText(/message from alex/)).toBeVisible()
```

---

## Implementation Guide

### Prerequisites

```bash
# Stack muss laufen (inkl. element-web auf Port 7070)
make dev

# /etc/hosts muss enthalten (für Dex-Redirect)
echo "127.0.0.1 dex" | sudo tee -a /etc/hosts

# auth-state Verzeichnis anlegen
mkdir -p e2e/auth-state
echo "auth-state/" >> e2e/.gitignore
```

### File-Reihenfolge der Implementierung

```
Phase 1 (Framework):
1. e2e/fixtures/users.ts
2. e2e/fixtures/dex-auth.ts          ← nutzt loginViaOidc() aus tests/fixtures/oidc.ts
3. e2e/fixtures/element-app.ts       ← von element-web kopiert + adaptiert
4. e2e/fixtures/nebu-fixtures.ts     ← mergeTests
5. e2e/playwright.config.ts          ← element-web project hinzufügen
6. e2e/global-setup.ts               ← auth-state warming (optional, aber empfohlen)

Phase 2 (Feature Tests) — jeweils: Red → Green → Refactor:
7. e2e/tests/element/login/login.spec.ts
8. e2e/tests/element/login/logout.spec.ts
9. e2e/tests/element/login/relogin.spec.ts
10. e2e/tests/element/room/create.spec.ts
11. e2e/tests/element/room/join.spec.ts
12. e2e/tests/element/room/leave.spec.ts
13. e2e/tests/element/messages/send.spec.ts
14. e2e/tests/element/messages/receive.spec.ts
```

### Key Selectors (aus element-web ElementAppPage)

```typescript
// Room-Liste
'[data-testid="room-list"]'
'.mx_LeftPanel'

// Raum-Name in Sidebar
page.getByTestId('room-list').locator(`[title="${name}"]`)

// Composer
'.mx_RoomView_body .mx_MessageComposer div[contenteditable]'

// Timeline-Events
'.mx_EventTile'
'.mx_EventTile_body'

// Create Room Dialog
'.mx_CreateRoomDialog'

// Invite-Sektion in Sidebar
'.mx_RoomSublist[data-testid*="invite"]'
// oder
page.getByRole('group', { name: /invites/i })

// "New conversation" Button
page.getByRole('navigation', { name: 'Room list' }).getByRole('button', { name: 'New conversation' })
```

### CI-Integration

```bash
# Makefile-Target hinzufügen:
test-e2e-element:
	cd e2e && npx playwright test --project=element-web --reporter=list

# Bestehende targets bleiben unverändert:
test-e2e:
	cd e2e && npx playwright test --reporter=list
```

### Dex Consent-Screen Handling

Element Web-Erstlogins können einen Dex-Consent-Screen zeigen ("Grant Access to Nebu"). Dieser muss in `loginViaOidc` bzw. `dex-auth.ts` behandelt werden:

```typescript
// Nach Dex-Form-Submit: Consent-Screen abfangen
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
| storageState expired (Element-Token läuft ab) | MEDIUM | auth-state/ wird beim nächsten Lauf automatisch refreshed wenn Datei > 12h alt |
| Dex Consent-Screen taucht sporadisch auf | MEDIUM | loginViaOidc() behandelt ihn bereits — in dex-auth.ts weiterverwenden |
| Element Web nicht erreichbar | LOW | Auto-skip Guard in jeder Spec (wie in bestehenden Tests) |
| Race condition: zweiter Context bekommt Nachricht nicht | MEDIUM | `expect(mariePage.locator(...)).toBeVisible({ timeout: 20_000 })` mit großzügigem Timeout |

---

## DoD (Definition of Done)

- [ ] Phase 1: Alle 4 Fixture-Dateien vorhanden und importierbar
- [ ] Phase 1: `npx playwright test --project=element-web --list` zeigt alle 8 Feature-Specs
- [ ] Phase 2: Alle 8 Feature-Specs grün (headed und headless)
- [ ] `make test-e2e-element` läuft durch ohne Fehler
- [ ] `e2e/auth-state/` in `.gitignore` eingetragen
- [ ] Bestehende Tests (API-Contract-Tests, Admin UI) unverändert grün
- [ ] Code Review: kein direkt-API-Call in Feature-Assertions (nur in Setup)

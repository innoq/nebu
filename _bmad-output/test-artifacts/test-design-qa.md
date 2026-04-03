---
stepsCompleted: ['step-05-generate-output']
lastStep: 'step-05-generate-output'
lastSaved: '2026-04-03'
workflowType: 'testarch-test-design'
mode: 'system-level'
---

# Test Design für QA: Nebu — Systemweiter Test-Plan

**Zweck:** QA-Ausführungsrezept — was testen, wie testen, was wir von anderen Teams brauchen.

**Datum:** 2026-04-03
**Autor:** Murat (Master Test Architect via BMAD TEA)
**Status:** Draft — bereit für Anwendung ab Epic 4
**Projekt:** Nebu (open-chat)

**Verwandt:** Architektur-Dokument `test-design-architecture.md` für Testability-Concerns und Blocker.

---

## Executive Summary

**Scope:** Systemweite Test-Abdeckung für alle Nebu-Epics (1–7). Fokus auf Epics 4–5 als nächste Implementierungsphase.

**Risiko-Zusammenfassung:**
- Gesamt: 10 Risiken (2 Critical Score=9, 6 High Score=6, 2 Medium Score=4)
- Kritische Kategorien: BUS (ATDD-Lücke), SEC (OIDC-Claim-Bug), TECH (ETS-Zustand), PERF (Load-Test)

**Coverage-Zusammenfassung:**
- P0-Tests: ~15 (kritische Pfade, Security, State-Resilienz)
- P1-Tests: ~25 (Integration, API-Verträge, Concurrency)
- P2-Tests: ~20 (Edge-Cases, Regression)
- P3-Tests: ~8 (Explorativ, Performance-Benchmarks)
- **Gesamt:** ~68 Tests (~6–9 Wochen für 1 QA + Dev)

---

## Nicht im Scope

| Item | Begründung | Mitigation |
|---|---|---|
| **Federation (Matrix)** | Bewusst ausgeschlossen (ADR) | Kein Test benötigt |
| **Push Notifications** | Growth-Feature post-MVP | Manuelle Validierung |
| **Kubernetes-Deployment** | Docker Compose only (MVP) | N/A |
| **Matrix-Verschlüsselung (E2EE)** | Client-seitig, nicht Nebu-Scope | Element/FluffyChat übernehmen das |
| **Disaster Recovery Tests** | Phase 2 / Kubernetes | Akzeptiertes Trade-off für MVP |

---

## Abhängigkeiten & Test-Blocker

**KRITISCH:** QA kann ohne diese Items nicht vollständig testen.

### Backend/Architektur-Abhängigkeiten (Pre-Implementation)

1. **C3: extractFirstRoleClaim Fix** — Dev — SOFORT
   - QA braucht: `matchesAdminGroupClaim` mit Multi-Value-Array-Support
   - Blockiert: Alle Auth-Flow-Tests mit realen OIDC-Providern

2. **DB-Cleanup in Godog** — Dev — vor Story 4-21
   - QA braucht: Before/After-Hooks die `events`, `rooms`, `room_memberships` tables leeren
   - Blockiert: Godog Room E2E Tests (Story 4-21)

3. **Horde-Crash-Test-Utility** — Dev — Story 4-5
   - QA braucht: ExUnit-Helper `kill_and_restart_genserver(module, id)` für konsistente Crash-Tests
   - Blockiert: Resilience-Tests für Room GenServer und Session Manager

### QA-Infrastruktur-Setup (Pre-Implementation)

1. **Godog Step-Definitionen für Room-Operationen** — QA/Dev
   - `Given room "alice-room" exists with members ["alice", "bob"]`
   - `When alice sends message "hello" to room "alice-room"`
   - `Then bob's sync response contains message "hello"`

2. **Test-Environments:**
   - Local: `make dev` (Docker Compose — alles verfügbar)
   - CI: GitLab CI mit DinD (existiert seit Epic 1)
   - Load-Test: k6 lokal oder k6 Cloud (ab Story 4-9)

---

## Risiko-Assessment

### High-Priority Risiken (Score ≥6)

| Risk ID | Category | Beschreibung | Score | QA Test-Abdeckung |
|---|---|---|---|---|
| **R-001** | BUS | Kein ATDD → Bugs erst im Review | **9** | CLAUDE.md-Mandat + Story-Template-Check bei jedem Review |
| **R-003** | SEC | `extractFirstRoleClaim` nur 1 Element | **9** | P0-Test: Multi-Value OIDC-Claims → richtige Rolle |
| **R-002** | TECH | ETS GenServer-State nach Crash ungetestet | **6** | P0-Test: ExUnit Crash-Simulation + State-Validation |
| **R-004** | TECH | Kein Restart-Resilienz-Test-Pattern | **6** | P0: Jede GenServer-Story braucht Crash-Test als AC |
| **R-005** | SEC | Room Power-Level-Enforcement ungetestet | **6** | P0-Test: Unauthorized send → 403 |
| **R-006** | PERF | k6 Load-Test zu spät (Story 4-23) | **6** | P1: Früher Basis-k6-Test nach Story 4-9 |
| **R-009** | DATA | txnId-Idempotenz nur unit-getestet | **6** | P1-Test: Concurrent Sends → 1 Event in DB |
| **R-010** | BUS | Matrix-Client Interop zu spät | **6** | P1-Test: matrix-js-sdk nach Story 4-9 |

### Medium-Risiken (Score 3–5)

| Risk ID | Category | Beschreibung | Score | QA Test-Abdeckung |
|---|---|---|---|---|
| **R-007** | TECH | gRPC ohne Circuit Breaker | **4** | P2: Elixir Core stopped → Gateway 503 in < 5s |
| **R-008** | OPS | Godog DB-State zwischen Tests | **4** | P2: Before-Hook + Cleanup-Validierung |

---

## Entry Criteria

**QA-Testing kann erst starten wenn:**

- [ ] C3 abgeschlossen (`matchesAdminGroupClaim` implementiert + Tests grün)
- [ ] CLAUDE.md mit ATDD-Mandat aktualisiert
- [ ] Story-Template mit Acceptance-Tests-Pflichtsektion aktualisiert
- [ ] Docker-Compose-Stack läuft (`make dev` fehlerfrei)
- [ ] CI-Pipeline grün (Unit + Integration)

## Exit Criteria

**Test-Phase abgeschlossen wenn:**

- [ ] Alle P0-Tests grün (100%)
- [ ] Alle P1-Tests grün (≥95%) oder Fehler triagiert
- [ ] Keine offenen HIGH/CRITICAL Bugs
- [ ] Silber-Tier-Load-Test bestanden (>500 concurrent, P95 ≤ 500ms)
- [ ] Matrix-Client-Interop (Element oder FluffyChat) manuell validiert

---

## Test Coverage Plan

### P0 (Critical) — Blockiert Core-Funktionalität

**Kriterien:** Kernfunktion blockiert + Score ≥6 + kein Workaround

| Test ID | Requirement / Szenario | Test-Level | Risk | Tool | Notes |
|---|---|---|---|---|---|
| **P0-001** | OIDC Multi-Value-Claims → richtige Rolle zugewiesen | Unit | R-003 | ExUnit | `matchesAdminGroupClaim(["viewer","instance_admin"], db)` → true |
| **P0-002** | Room GenServer überlebt Crash + Neustart (Horde) | Unit | R-002 | ExUnit | `Process.exit(pid, :kill)` → Room.get_state/1 gibt State zurück |
| **P0-003** | Session Manager ETS überlebt GenServer-Restart | Unit | R-002 | ExUnit | Session nach GenServer-Kill noch abrufbar |
| **P0-004** | Unauthorized Room-Event → 403 (Power Levels) | Unit+Integration | R-005 | ExUnit + Godog | send_event mit User ohne Schreibrecht → {:error, :forbidden} |
| **P0-005** | Matrix Login mit gültigem OIDC-Token | E2E | R-003 | Godog | POST /login → 200 + access_token (vollständiger OIDC-Flow) |
| **P0-006** | Matrix Logout invalidiert Session | E2E | — | Godog | POST /logout → 200; danach GET /sync → 401 |
| **P0-007** | Room erstellen + Mitglied beitreten | Integration | — | Godog | POST /createRoom → POST /join → GET /sync → Room sichtbar |
| **P0-008** | Nachricht senden + empfangen (sync) | E2E | — | Godog | PUT /send → GET /sync → Event in Timeline |
| **P0-009** | Ed25519-Signatur auf Event korrekt | Unit | — | ExUnit | `Nebu.EventId.sign_and_hash` → Verifikation mit öffentlichem Schlüssel |
| **P0-010** | txnId-Idempotenz: doppeltes Senden → ein Event | Integration | R-009 | Godog + net/http | 2x PUT mit gleicher txnId → 1 Event in DB |
| **P0-011** | Bootstrap Wizard: Admin-Claim konfigurierbar | Integration | R-003 | Godog/Playwright | `admin_group_claim=corp_admin` → User mit claim `corp_admin` → instance_admin |
| **P0-012** | Expired JWT → 401 | Unit | — | Go httptest | Token nach Ablauf → `ValidateToken` → 401 |
| **P0-013** | PII-Verschlüsselung: E-Mail at-rest verschlüsselt | Unit | — | ExUnit | `encrypt_pii/2` → decrypt → original; kein Plaintext in DB |
| **P0-014** | gRPC Elixir Core Registrierung | Integration | — | Godog | Gateway meldet sich an Core an; ValidateToken gRPC erreichbar |
| **P0-015** | Presence Manager: User online/offline | Integration | — | ExUnit | Presence.track/leave → sync response zeigt Präsenz |

**Gesamt P0:** ~15 Tests

---

### P1 (High) — Wichtige Funktionen, mittleres Risiko

| Test ID | Requirement / Szenario | Test-Level | Risk | Tool | Notes |
|---|---|---|---|---|---|
| **P1-001** | Concurrent Room-Beitritt (Race Condition) | Unit | — | ExUnit async | 5 gleichzeitige join_room → kein Deadlock |
| **P1-002** | txnId-Idempotenz concurrent (10 simultane Requests) | Integration | R-009 | net/http | 10 goroutines, gleiche txnId → COUNT=1 in DB |
| **P1-003** | matrix-js-sdk HTTP-Smoke-Test nach createRoom | Integration | R-010 | Godog/http | matrix-js-sdk createRoom + sendEvent → Matrix-kompatible Response |
| **P1-004** | Basis-k6-Load-Test: 50 VU (Früh-Warnung) | Performance | R-006 | k6 | Nach Story 4-9: 50 VU, 60s, P95 < 1s |
| **P1-005** | Incremental Sync (since-Token) | Integration | — | Godog | GET /sync?since=token → nur neue Events |
| **P1-006** | Typing Indicators (PUT /typing) | Integration | — | Godog | PUT /typing → sync response zeigt m.typing event |
| **P1-007** | Read Receipts (POST /receipt) | Integration | — | Godog | POST /receipt → sync response zeigt m.receipt |
| **P1-008** | Profil-Update (PUT /profile) | Integration | — | Godog | PUT /profile/displayname → GET /profile → aktualisiert |
| **P1-009** | Room-Mitglieder-Limit (max_members) | Unit | — | ExUnit | join_room wenn Limit erreicht → {:error, :room_full} |
| **P1-010** | Admin Session Cookie: 8h TTL | Integration | — | Playwright | Cookie expiret nach 8h → 401 |
| **P1-011** | Admin Dashboard: Live-Metriken via SSE | E2E | — | Playwright | Dashboard lädt; SSE-Widget aktualisiert room_count |
| **P1-012** | Session-Invalidierung bei Logout | Integration | — | Godog | POST /logout → Session in DB invalidiert → ValidateToken → 401 |
| **P1-013** | Message Buffer Drain (linear MVP) | Unit | — | ExUnit | Buffer füllt sich → drain → alle Events gesendet (Reihenfolge) |
| **P1-014** | Elixir Node-Registrierung nach Restart | Integration | — | Godog | Elixir Core Restart → reregistriert sich beim Gateway |
| **P1-015** | gRPC EventBus Server-Streaming | Integration | — | Godog | Subscribe → Event senden → Event im Stream empfangen |
| **P1-016** | Bootstrap Wizard E2E (Admin UI) | E2E | — | Playwright | Instance Name → OIDC Config → Connect → Claim-Selection → Key Gen → Dashboard |
| **P1-017** | Admin OIDC Login (PKCE, echter Flow) | E2E | — | Playwright | Kein Cookie-Forging; echter PKCE-Flow via Dex |
| **P1-018** | Fehlerseiten: Kein Stack Trace | E2E | — | Playwright | 401/403/404/500 → DaisyUI-Karte ohne Stack-Trace |
| **P1-019** | Metrics-Endpunkt: Prometheus scraping | Integration | — | Godog/http | GET /metrics → Prometheus-Format; room_count vorhanden |
| **P1-020** | Health-Endpunkt: DB-Verbindung geprüft | Integration | — | Godog | GET /health → 200 inkl. DB-Status; DB down → 503 |
| **P1-021** | Ed25519-Signature-Verifikation: ungültige Sig | Unit | — | ExUnit | Verify mit falschem Schlüssel → {:error, :invalid_signature} |
| **P1-022** | X25519 ECDH: PII-Verschlüsselung round-trip | Unit | — | ExUnit | encrypt/decrypt PII mit X25519 → Plaintext wiederhergestellt |
| **P1-023** | Initial Sync (State Events im Response) | Integration | — | Godog | GET /sync (ohne since) → m.room.create, m.room.member Events |
| **P1-024** | Room-Sichtbarkeit (public/private) | Unit | — | ExUnit | join_room(public_room) → ok; join_room(invite_only) → {:error, :not_invited} |
| **P1-025** | Godog DB-Cleanup-Hook funktioniert | Infrastructure | R-008 | Godog | Before-Hook leert Testtabellen; After-Hook validiert Leere |

**Gesamt P1:** ~25 Tests

---

### P2 (Medium) — Edge Cases, Regression

| Test ID | Requirement / Szenario | Test-Level | Tool | Notes |
|---|---|---|---|---|
| **P2-001** | gRPC Circuit Breaker: Elixir Core nicht erreichbar → 503 in < 5s | Integration | Godog | Elixir stoppen → Gateway antwortet schnell mit Fehler |
| **P2-002** | TLS-Verbindung: Selbstsigniertes Zertifikat abgelehnt (ohne Skip-Verify) | Integration | Godog | Falsche CA → TLS-Fehler |
| **P2-003** | Zu großes Request-Body → 413 | Unit | Go httptest | Body > 100MB → 413 Payload Too Large |
| **P2-004** | OIDC-Token mit falschem Issuer → 401 | Unit | Go httptest | Token Issuer ≠ konfigurierter Issuer |
| **P2-005** | Doppelter Bootstrap-Versuch → 409 | Integration | Godog | POST /admin/bootstrap zweimal → zweiter Versuch rejected |
| **P2-006** | Gleichzeitige Admin-Erstellung (Advisory Lock) | Integration | ExUnit | 2 parallele Logins (kein Admin) → exakt 1 Admin erstellt |
| **P2-007** | Room-Name-Update propagiert in sync | Integration | Godog | PUT /state/m.room.name → sync zeigt Update-Event |
| **P2-008** | Leerzeichen/Unicode in Raumnamen | Unit | ExUnit | room_name="Räum 🚀" → korrekt gespeichert + abgerufen |
| **P2-009** | Maximale Message-Size (Matrix 65535 Bytes) | Unit | ExUnit | Event > 65535 Bytes → {:error, :event_too_large} |
| **P2-010** | Paginierung: /messages mit from/to Tokens | Integration | Godog | GET /messages?from=token&limit=10 → korrekte Page |
| **P2-011** | Presence: User offline nach Disconnect | Integration | ExUnit | Presence.track → disconnect → Presence.get zeigt offline |
| **P2-012** | Media Gateway: Datei-Upload (AES-256-GCM) | Integration | Godog | POST /upload → 200 + media_url; Inhalt verschlüsselt |
| **P2-013** | Media Gateway: Datei-Download entschlüsselt | Integration | Godog | GET media_url → Inhalt = Original |
| **P2-014** | Admin UI: Unauthenticated Redirect | E2E | Playwright | GET /admin → 302 → Login-Seite |
| **P2-015** | Admin UI: URL-State Bookmarkbar | E2E | Playwright | /admin/rooms?filter=public → nach Reload selbe Ansicht |
| **P2-016** | DSGVO Right-to-be-Forgotten: Schlüssellöschung | Unit | ExUnit | delete_user_keys → PII nicht mehr entschlüsselbar |
| **P2-017** | Audit Log: Append-Only Constraint | Integration | ExUnit | Direkter DB-Update auf Audit-Log → RLS-Fehler |
| **P2-018** | Prometheus: Metriken steigen bei Load | Integration | Godog/k6 | 100 Nachrichten → msg_per_sec > 0 in /metrics |
| **P2-019** | WCAG: Admin UI (Axe-Core Scan) | E2E | Playwright | Keine WCAG 2.1 AA Violations in Dashboard + Wizard |
| **P2-020** | Godog-Tests parallel-safe (2 Worker) | Infrastructure | Godog | `--parallel=2` → kein Test-State-Konflikt |

**Gesamt P2:** ~20 Tests

---

### P3 (Low) — Nice-to-have, Explorativ

| Test ID | Requirement / Szenario | Test-Level | Tool | Notes |
|---|---|---|---|---|
| **P3-001** | k6 Silber-Tier (500 VU, 5 min) | Performance | k6 | NFR-P3: >500 concurrent, P95 ≤ 500ms — Story 4-23 |
| **P3-002** | k6 Gold-Tier (1000 VU) — optional | Performance | k6 | Growth-Validierung |
| **P3-003** | Elixir Clustering: 2 Nodes, Room-Migration | Chaos | ExUnit/Docker | Horde-Migration zwischen zwei BEAM-Nodes |
| **P3-004** | Message-Buffer-Drain unter Last (100 msg backpressure) | Performance | ExUnit | AIMD-Phase-2-Vorbereitung |
| **P3-005** | matrix-js-sdk SDK-Level-Test (nicht nur HTTP) | E2E | Node.js | Vollständiger SDK-Flow: login, createRoom, sendMessage, sync |
| **P3-006** | FluffyChat-Smoke-Test (manuell) | Explorativ | Manuell | Login + 1 Raum + 1 Nachricht mit FluffyChat |
| **P3-007** | Admin API OpenAPI-Validierung | Integration | oapi-codegen | Alle Admin-API-Responses gegen OpenAPI-Schema |
| **P3-008** | Endurance Test: 24h unter 100 VU | Performance | k6 | Memory Leaks, GenServer-Prozessakkumulation |

**Gesamt P3:** ~8 Tests

---

## Execution Strategy

**PR (jeder Commit):** Unit-Tests + Integration-Tests — Feedback in < 10 Minuten

### Jeder PR: Unit + Integration (~8–12 min)

**Go Gateway:**
- `make test-unit-go` (go test -race ./... mit //go:build unit Tag) — ~2 min
- Alle Go Unit-Tests (httptest, Handler, Middleware, Crypto)

**Elixir Core:**
- `make test-unit-elixir` (mix test) — ~3 min
- ExUnit: GenServer-Tests, ETS-Mock-Tests, Crypto-Tests

**Integration (Godog):**
- `make test-integration` (DinD, Docker Compose Stack) — ~5 min
- Alle Gherkin-Szenarien: Auth, Health, Room-Basis

### Nightly: E2E + Browser (~20–30 min)

- Playwright E2E-Tests (Admin UI, Bootstrap Wizard, Dashboard)
- Erweiterte Godog-Szenarien (Sync, Presence, Typing)

### Weekly/Manuell: Performance + Chaos

- k6 Basis-Load (50 VU) nach Story 4-9
- k6 Silber-Tier (500 VU) Story 4-23
- Elixir Clustering-Test (P3-003)

---

## QA-Aufwands-Schätzung

| Priorität | Anzahl | Aufwand (Entwicklung) | Notizen |
|---|---|---|---|
| P0 | ~15 | ~15–25 Stunden | Komplexe State-Tests, Concurrency, E2E |
| P1 | ~25 | ~20–30 Stunden | Integration, API-Verträge, Browser-Flow |
| P2 | ~20 | ~10–15 Stunden | Edge-Cases, Regression |
| P3 | ~8 | ~5–10 Stunden | Performance-Scripts, Explorativ |
| **Gesamt** | **~68** | **~50–80 Stunden** | **Dev + QA gemeinsam** |

**Annahmen:**
- Dev schreibt Unit-Tests (ExUnit + Go); QA/Dev gemeinsam Godog + Playwright
- Inkludiert: Test-Design, Implementation, Debugging, CI-Integration
- Exkludiert: Laufende Wartung (~10% Aufwand)
- Verteilt über Epics 4–7 (nicht alles auf einmal)

---

## Interworking & Regression

**Betroffene Services bei Änderungen:**

| Service/Komponente | Impact | Regression-Scope | Validierung |
|---|---|---|---|
| **Go Gateway Auth-Middleware** | Auth-Pfad-Änderungen | Go Unit-Tests + Godog Auth-Szenarien | `go test -race ./internal/auth/...` + `gherkin: auth` |
| **Elixir Room GenServer** | Room-State-Änderungen | ExUnit Room + Godog Room-Szenarien | `mix test apps/room_manager/...` |
| **Elixir Session Manager** | Session/ETS-Änderungen | ExUnit Session + Godog Auth-Szenarien | `mix test apps/session_manager/...` |
| **gRPC Proto-Änderungen** | Go + Elixir Stubs | Beide Unit-Test-Suites + Integration | `make proto && make test-unit-go && make test-unit-elixir` |
| **Admin UI (Go Templates)** | UI-Rendering-Änderungen | Playwright E2E + Go Template-Tests | Playwright Dashboard + Wizard |

**Regressions-Strategie:**
- Jede Story: Unit-Tests der betroffenen Packages müssen 100% grün bleiben
- Jede Epic: Godog-Integrationstests vollständig grün vor Epic-Abschluss
- Vor Release: Alle P0+P1-Tests grün; Silber-Tier-Load-Test bestanden

---

## Tooling & Zugang

| Tool | Zweck | Status |
|---|---|---|
| `go test -race` | Go Unit + Race Detection | ✅ Bereit (seit Epic 1) |
| `mix test` | Elixir ExUnit | ✅ Bereit (seit Epic 1) |
| Godog + net/http | Gherkin Integration Tests | ✅ Bereit (seit Epic 1) |
| Playwright MCP | Browser E2E Tests | ✅ Convention etabliert (Epic 2) |
| k6 | Load/Performance Tests | ⚠️ Noch nicht eingerichtet (Story 4-23) |
| GitLab CI DinD | Integration Tests in CI | ✅ Bereit (seit Epic 1) |

---

## Anhang A: Test-Naming und Tagging (Nebu-Konventionen)

### Go Tests

```go
// Unit-Test: package + scenario
func TestRoom_SendEvent_WithDuplicateTxnId_ReturnsIdempotent(t *testing.T) { ... }

// Integration (httptest): flow + outcome
func TestMatrix_SendEvent_ConcurrentSameTxnId_OnlyOneEventStored(t *testing.T) { ... }
```

### Elixir ExUnit

```elixir
# GenServer-Test
test "room genserver survives crash and restart via Horde", %{room_id: id} do
  {:ok, pid} = Room.start_link(id)
  Process.exit(pid, :kill)
  :timer.sleep(100)
  assert {:ok, _state} = Room.get_state(id)
end

# ETS-Cleanup zwischen Tests
setup do
  :ets.delete_all_objects(:sessions)
  :ok
end
```

### Godog (Gherkin)

```gherkin
# Feature: Room Messaging
@P0 @messaging @idempotency
Scenario: Duplicate txnId returns same event
  Given alice is authenticated
  And room "test-room" exists
  When alice sends event type "m.room.message" with txnId "txn-001"
  And alice sends event type "m.room.message" with txnId "txn-001" again
  Then only 1 event with txnId "txn-001" exists in the database
  And both responses return the same event_id
```

### Test-ID-Format

`{EPIC}.{STORY}-{LEVEL}-{SEQ}`

Beispiele:
- `4.11-INT-001` (Story 4-11, Integration, Test 001)
- `4.2-UNIT-003` (Story 4-2, Unit, Test 003)
- `4.21-E2E-001` (Story 4-21, E2E, Test 001)

---

## Anhang B: ATDD-Prozess (ab Epic 4)

**Der Nebu-TDD-Standard — gilt für alle zukünftigen Stories:**

### Für Stories mit Elixir GenServer State (ETS, Horde):

```
1. ExUnit-Test schreiben (failing) — beschreibt gewünschtes Verhalten
2. ExUnit-Crash-Test schreiben (failing) — beschreibt Restart-Resilienz
3. Minimal-Implementation um Tests grün zu machen
4. Refactoring unter grünen Tests
```

### Für Stories mit Matrix-API-Endpunkten:

```
1. Godog-Szenario schreiben (failing) — Happy Path + 1 Fehlerfall
2. Go-Handler-Skeleton erstellen (returns 501)
3. Implementation iterativ bis Godog grün
```

### Für Stories mit Admin-UI-Komponenten:

```
1. Playwright-Feature-File schreiben (failing) — Browser-Flow
2. Go-Template-Skeleton erstellen
3. Implementation bis Playwright grün
```

**CLAUDE.md-Mandat:** Diese Reihenfolge ist verpflichtend. Code-Reviews prüfen: "Gibt es einen failing Test vor dem ersten Implementation-Commit?"

---

**Generiert von:** Murat (BMad TEA Master Test Architect)
**Workflow:** `bmad-testarch-test-design` (System-Level Mode)
**Datum:** 2026-04-03

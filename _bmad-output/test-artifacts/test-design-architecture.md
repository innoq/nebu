---
stepsCompleted: ['step-01-detect-mode', 'step-02-load-context', 'step-03-risk-and-testability', 'step-04-coverage-plan', 'step-05-generate-output']
lastStep: 'step-05-generate-output'
lastSaved: '2026-04-03'
workflowType: 'testarch-test-design'
mode: 'system-level'
inputDocuments:
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/planning-artifacts/architecture.md'
  - '_bmad-output/planning-artifacts/epics.md'
  - '_bmad-output/implementation-artifacts/sprint-status.yaml'
  - '_bmad-output/implementation-artifacts/epic-1-retro-2026-03-26.md'
  - '_bmad-output/implementation-artifacts/epic-2-retro-2026-03-31.md'
  - '_bmad-output/implementation-artifacts/epic-3-retro-2026-04-02.md'
---

# Test Design für Architektur: Nebu — Systemweite Test-Strategie

**Zweck:** Architektonische Testability-Analyse, Risikoregister und systemweite Test-Strategie für alle zukünftigen Epics. Dient als Vertrag zwischen QA und Entwicklung — was muss angepasst werden, damit Bugs früher gefunden werden.

**Datum:** 2026-04-03
**Autor:** Murat (Master Test Architect via BMAD TEA)
**Status:** Bereit für Review
**Projekt:** Nebu (open-chat)
**PRD:** `_bmad-output/planning-artifacts/prd.md`
**Architektur:** `_bmad-output/planning-artifacts/architecture.md`

---

## Executive Summary

**Scope:** Systemweite Test-Strategie für Nebu — Go API Gateway + Elixir/OTP Core + PostgreSQL. Deckt Epics 1–7, mit spezifischer Priorisierung für Epic 4 (aktuell) und Epic 5 (Compliance).

**Hintergrund (aus Retrospektiven):**

Epic 3 hat zwei fundamentale Testprobleme offenbart, die systemisch sind und ohne gezielte Maßnahmen in Epic 4+ wiederkehren werden:
- **Story 3-8**: `sync.Map` für Bootstrap-Draft-Storage → 3 Code-Review-Runden. Root Cause: Kein Acceptance-Test für Restart-Resilienz → falscher Architekturentscheid nicht früh erkannt.
- **Story 3-15**: Cookie-Forging statt realem Browser-Flow → E2E-Lücke. Root Cause: Test nach Implementation geschrieben, nicht davor.

Epic 4 zeigt dasselbe Muster: Story 4-2 (1 MAJOR), 4-3 (Map.new Sort Order), 4-4 (2 MAJOR + 4 MINOR) — alle Fehler erst im Code Review, nicht in Tests.

**Architektur (ADRs):**
- ADR 004: Horde Registry + DynamicSupervisor (Room GenServer)
- ADR 003: Content-Hash Event-ID (Ed25519)
- ADR 005: gRPC Server-Streaming EventBus
- ADR 007: Ed25519 + X25519 Key Pairs

**Scale-Ziele (NFR):**
- Silber: >500 concurrent users auf m5.large (MVP-Gate)
- Nachrichtenlatenz: ≤500ms unter Silber-Last

**Risiko-Zusammenfassung:**
- **Gesamt:** 10 Risiken identifiziert
- **CRITICAL (Score 9):** 2 — sofortige Maßnahmen erforderlich
- **HIGH (Score 6–8):** 6 — vor Epic 4-Ende adressieren
- **Test-Aufwand:** ~30–50 Stunden (Story-Creator + Developer)

---

## Quick Guide

### 🚨 BLOCKERS — Muss entschieden werden (kann nicht ohne diese weitermachen)

**Systemweiter Prozess — vor dem nächsten Story-Zyklus:**

1. **R-001: Kein ATDD-Mandat** — CLAUDE.md + Story-Template müssen Acceptance-Tests als Pflicht vor Implementation verankern. Ohne das entstehen weiter 3-Runden Code Reviews. (Owner: Phil, vor Story 4-5)

2. **R-003: `extractFirstRoleClaim` Bug (C3)** — Nur erstes OIDC-Claim-Element geprüft → Multi-Value-Arrays brechen Rollenzuweisung. Betrifft jeden Login mit mehreren Gruppen. (Owner: Dev, vor Epic 4 weiteren Stories)

3. **R-009: txnId-Idempotenz nur Unit-getestet** — Kein Integration-Test für gleichzeitige Sends. Bei parallelen Clients (Matrix-Standard) kann doppelter Nachrichtenversand auftreten. (Owner: Dev, Story 4-11)

### ⚠️ HIGH PRIORITY — Team soll validieren

1. **R-002: ETS-Zustand verloren bei Crash** — Horde migriert Room GenServer, aber das ist nicht getestet. Betrifft Produktionsverfügbarkeit. (Dev, Epic 4)
2. **R-005: Power-Level-Enforcement ungetestet** — Autorisierung in Rooms nicht validiert (Story 4-13 Backlog). (Dev, vor Room-Stories)
3. **R-006: Load-Test zu spät** — k6 Silber-Tier-Test ist Story 4-23 (letzte). Wenn Architektur-Bottleneck erst da entdeckt wird, sind alle Room-Stories bereits implementiert. (Phil, vor Story 4-9)
4. **R-010: Matrix-Client-Interop zu spät** — matrix-js-sdk Smoke Test ist Story 4-22. Inkompatibilitäten könnten alle Messaging-Stories betreffen. (Dev, früher verschieben)

### 📋 INFO ONLY — Lösungen stehen bereit

1. **Test-Pyramide:** Go Unit → Elixir ExUnit → Godog HTTP/gRPC → Playwright Browser (4 Ebenen, klare Verantwortung)
2. **Tooling:** `go test -race` + ExUnit + Godog + Playwright MCP (alles bereits eingerichtet)
3. **CI-Strategie:** PR (Unit + Integration) / Nightly (E2E + Load)
4. **ATDD-Workflow:** Feature-File-First für alle Stories mit Elixir GenServer State oder UI-Flows
5. **Quality Gates:** P0 100% / P1 ≥95% / High-Risk vollständig mitigiert vor Release

---

## Für Architekten und Entwickler — Offene Punkte 👷

### Risiko-Assessment

**Gesamt: 10 Risiken (2 Critical Score=9, 6 High Score=6–8, 2 Medium Score=3–5)**

#### Critical-Risiken (Score = 9) — SOFORTIGE MASSNAHMEN

| Risk ID | Category | Beschreibung | P | I | Score | Mitigation | Owner | Timeline |
|---|---|---|---|---|---|---|---|---|
| **R-001** | **BUS** | Kein ATDD-Mandat → Acceptance Tests nach Implementation → falsche Architektur-Entscheidungen erst im 3. Review-Zyklus erkannt (Epic 3-8 Precedent) | 3 | 3 | **9** | CLAUDE.md + Story-Template: Acceptance Test Pflicht-Sektion vor Code | Phil | vor Story 4-5 |
| **R-003** | **SEC** | `extractFirstRoleClaim` prüft nur erstes Array-Element → Multi-Value OIDC-Claims (`["viewer","instance_admin"]`) → User erhält nie Admin-Zugang. C3 pending. | 3 | 3 | **9** | C3 implementieren: `matchesAdminGroupClaim` mit vollem Array-Scan + `admin_group_claim` DB-konfigurierbar | Dev | SOFORT |

#### High-Risiken (Score 6–8)

| Risk ID | Category | Beschreibung | P | I | Score | Mitigation | Owner | Timeline |
|---|---|---|---|---|---|---|---|---|
| **R-002** | **TECH** | ETS Room GenServer State nicht persistent → Elixir-Node-Crash verliert Room-State; Horde Migration ungetestet | 2 | 3 | **6** | ExUnit-Test: Horde-Supervisor simuliert Node-Down, Room GenServer startet auf anderem Node, State korrekt | Dev | Epic 4 |
| **R-004** | **TECH** | Kein Restart-Resilienz-Test-Pattern → jeder Story-Creator kann in-memory State wählen ohne Test-Gegengewicht (wie sync.Map in 3-8) | 3 | 2 | **6** | Story-Template: "Restart-Resilienz: Beschreibe, wie dieser Component einen Neustart überlebt" als Pflichtfeld | Phil | vor Story 4-5 |
| **R-005** | **SEC** | Room Power-Level-Enforcement nicht implementiert/getestet (Story 4-13 Backlog) → jeder Nutzer kann in jeden Room senden | 2 | 3 | **6** | Unit-Test: Powerlevel-Prüfung vor send_event; Integration-Test: 403 bei unzureichenden Rechten | Dev | Story 4-13 |
| **R-006** | **PERF** | k6 Silber-Tier-Test ist Story 4-23 (letzte) → Bottleneck erst nach allen Room-Stories entdeckbar | 2 | 3 | **6** | k6 Basis-Lasttest nach Story 4-9 (createRoom) — früh validieren, nicht am Ende | Phil | nach Story 4-9 |
| **R-009** | **DATA** | txnId-Idempotenz nur Unit-getestet → kein Integration-Test für gleichzeitige Matrix-Client-Sends → Duplikat-Events möglich | 2 | 3 | **6** | Godog-Szenario: Gleiche txnId zweimal → nur ein Event in DB; concurrent Sends testen | Dev | Story 4-11 |
| **R-010** | **BUS** | Matrix-Client Interop (matrix-js-sdk) erst Story 4-22 → alle Room-API-Stories können inkompatible Responses produzieren | 2 | 3 | **6** | matrix-js-sdk HTTP-Smoke-Test nach Story 4-9 (vor späteren Stories) | Dev | nach Story 4-9 |

#### Medium-Risiken (Score 3–5)

| Risk ID | Category | Beschreibung | P | I | Score | Mitigation | Owner |
|---|---|---|---|---|---|---|---|
| **R-007** | **TECH** | gRPC Go→Elixir ohne Circuit Breaker → Elixir-Node-Ausfall lässt Gateway hängen statt fail-fast | 2 | 2 | **4** | gRPC-Client: Timeout + exponential backoff + health-check vor Request | Dev |
| **R-008** | **OPS** | Godog Integration-Tests teilen DB-State → Reihenfolge-abhängige Fehler möglich | 2 | 2 | **4** | Godog Before/After-Hooks: DB-Tabellen cleanen zwischen Szenarien | Dev |

---

### Testability-Concerns und Architektur-Lücken

**🚨 ACTIONABLE CONCERNS — Architektur-Team muss handeln**

#### 1. Fehlendes ATDD-Prozess-Mandat (KRITISCH)

| Concern | Impact | Was benötigt wird | Owner | Timeline |
|---|---|---|---|---|
| **Kein formaler "Tests-First"-Gate** | Bugs bis Code-Review unentdeckt → mehrfache Review-Runden (3-8: 3 Runden, 4-4: 2 MAJOR + 4 MINOR) | CLAUDE.md: Acceptance-Test-Sektion in Story-Template als Pflicht vor Implementation-Start | Phil | Sofort |
| **Keine Restart-Resilienz-Checkliste** | In-Memory-State-Annahmen nicht explizit hinterfragt (sync.Map in 3-8, ETS in 4-x) | Story-Template: Pflichtfeld "Persistenz-Strategie: Wie überlebt dieser State einen Neustart?" | Phil | Sofort |
| **Kein Concurrency-Test-Pattern** | Idempotenz-Logik (txnId) nur unit-getestet → Race-Conditions in Integration unentdeckt | Godog: Parallel-Client-Szenarien für alle idempotency-kritischen Endpunkte | Dev | Story 4-11 |

#### 2. Architektonische Verbesserungen (SOLLTE GEÄNDERT WERDEN)

**2.1 Fehlende Test-Seeding-API**
- **Aktuelles Problem:** Godog-Tests verlassen sich auf Dex-vorkonfigurierte User (alice@example.com) und reale OIDC-Flows. DB-Zustand zwischen Tests nicht kontrollierbar.
- **Benötigte Änderung:** `POST /_nebu/test/seed` (nur in Dev/Test-Umgebung) für User-Erstellung ohne OIDC-Flow; DB-Reset-Hook in Godog Before-Hook.
- **Impact wenn nicht behoben:** Flakige Tests bei parallelem Lauf; Testdaten-Verschmutzung zwischen Szenarien.
- **Owner:** Dev
- **Timeline:** Vor Story 4-21 (Gherkin Room E2E)

**2.2 Kein Distributed Tracing**
- **Aktuelles Problem:** Go Gateway → Elixir Core gRPC — kein W3C Trace Context. Fehler bei Request-Routing zwischen Services nicht nachvollziehbar.
- **Benötigte Änderung:** Correlation-ID im gRPC-Metadata-Header; Elixir Core loggt Correlation-ID.
- **Impact wenn nicht behoben:** Debugging in Produktion extrem schwierig bei verteilten Fehlern.
- **Owner:** Dev (Epic 4 oder 5)
- **Timeline:** Spätestens vor GA

**2.3 ETS State Testing ohne Restart**
- **Aktuelles Problem:** ETS-basierter Session/Presence-State in Elixir — `Application.put_env` Swap-Pattern erlaubt Unit-Tests, aber kein Test für ETS-Wiederherstellung nach GenServer-Crash.
- **Benötigte Änderung:** ExUnit `setup` mit explizitem GenServer-Kill + Restart + State-Validation.
- **Impact wenn nicht behoben:** Horde-Migration-Pfad ungetestet → Produktionsfehler bei Node-Ausfall.
- **Owner:** Dev
- **Timeline:** Story 4-5 (Session Manager)

---

### Testability Assessment Summary

**📊 ADR Quality Readiness Checklist: 12/29 Kriterien erfüllt (41%) → ⚠️ CONCERNS**

| Kategorie | Status | Kriterien erfüllt | Gap-Zusammenfassung |
|---|---|---|---|
| 1. Testability & Automation | ⚠️ | 2/4 | Keine Seeding-API; ETS-State nicht reset-bar zwischen Tests |
| 2. Test Data Strategy | ⚠️ | 1/3 | Godog-Tests order-dependent; keine Factory-Pattern |
| 3. Scalability & Availability | ⚠️ | 1/4 | Horde-Failover ungetestet; kein Load-Test |
| 4. Disaster Recovery | ⚠️ | 0/3 | RTO/RPO nicht definiert; kein Backup-Restore-Test |
| 5. Security | ⚠️ | 3/4 | extractFirstRoleClaim-Bug (C3); Power-Level-Tests fehlen |
| 6. Monitorability | ⚠️ | 1/4 | Kein Distributed Tracing; Elixir Core ohne Metrics-Endpoint |
| 7. QoS & QoE | ⚠️ | 2/4 | Load-Test zu spät; Rate-Limiting nicht implementiert |
| 8. Deployability | ⚠️ | 2/3 | Kein Zero-Downtime-Deployment; kein Automated Rollback |

#### Was bereits gut funktioniert

- ✅ **Go Unit Tests mit Race Detector** (`go test -race`) — seit Epic 1
- ✅ **Elixir ExUnit mit ETS-Swap-Pattern** — FakeProvisioner/FakeValidator etabliert
- ✅ **Godog Gherkin E2E** — vollständiger OIDC-Flow mit echtem Dex (Story 2-21)
- ✅ **Playwright MCP** — Convention in CLAUDE.md (P2 aus Epic 2 Retro)
- ✅ **CI-Pipeline** — Unit + Integration getrennt; DinD-Networking funktioniert
- ✅ **PII-Verschlüsselung vollständig getestet** — AES-256-GCM + X25519 ECDH
- ✅ **Ed25519 Signing Unit-Tests** — Story 2-8, 4-3

#### Akzeptierte Trade-offs (kein Handlungsbedarf jetzt)

- **Disaster Recovery (Kategorie 4):** Für MVP-Phase akzeptiert — Docker Compose ist kein Production-HA-Setup. RTO/RPO für Phase 2 (Kubernetes/Multi-Node).
- **Distributed Tracing:** Correlation-IDs statt OpenTelemetry für MVP — genug für Debugging.
- **Rate Limiting:** In Architektur vorgesehen, nicht MVP-kritisch für Alpha-Betrieb.

---

### Risiko-Mitigation Plans (High-Priority ≥6)

#### R-001: Kein ATDD-Mandat (Score: 9) — CRITICAL

**Mitigation-Strategie:**

1. **CLAUDE.md erweitern** — Neue Sektion "Test-Driven Development Standard":
   - Für alle Stories mit GenServer-State, ETS, PostgreSQL-Schreibzugriff: ExUnit-Test vor Implementation-Code
   - Für alle Stories mit UI-Flows (Admin UI): Playwright-Feature-File vor HTML/Go-Code
   - Für alle Stories mit Matrix-API-Endpunkten: Godog-Szenario vor Handler-Code
2. **Story-Template erweitern** — Pflichtsektion "Acceptance Tests":
   - "Beschreibe 2–3 Acceptance Tests für die Hauptszenarien (Happy Path + ein Fehlerfall)"
   - "Welche dieser Tests müssen ZUERST geschrieben werden, bevor der Implementation-Code existiert?"
3. **Code-Review-Checkliste** — Frage: "Hat jedes Acceptance-Criterion mindestens einen Test?"

**Owner:** Phil (CLAUDE.md), Dev (Story-Template-Disziplin)
**Timeline:** Vor Story 4-5
**Status:** Geplant
**Verifikation:** Nächste 3 Stories haben Acceptance-Tests im Story-Dokument vor dem ersten Commit

---

#### R-003: `extractFirstRoleClaim` Bug (Score: 9) — CRITICAL

**Mitigation-Strategie:**

1. C3 implementieren (aus Epic 3 Retro-Aktionen):
   - `matchesAdminGroupClaim(claims []string, db) bool` — prüft ALLE Elemente
   - `admin_group_claim` aus `server_config` geladen (konfigurierbar)
   - Unit-Tests: Multi-Value-Array, Single-Value, leer, falscher Claim
2. Bootstrap Wizard neu designen (PKCE-Flow, Claim-Selection-Callback)
3. Gherkin-Szenario: "Given OIDC returns claims [viewer, instance_admin] When login Then role=instance_admin"

**Owner:** Dev
**Timeline:** SOFORT (C3 ist Epic-4-Startbedingung aus Epic-3-Retro)
**Status:** Pending
**Verifikation:** Unit-Test `matchesAdminGroupClaim` mit `["viewer","instance_admin"]` → `true`

---

#### R-002: ETS Room GenServer Crash (Score: 6) — HIGH

**Mitigation-Strategie:**

1. ExUnit-Test in `room_manager`:
   - `start_supervised({Room.Supervisor, room_id: "test-room"})` → Room erstellen
   - `Process.exit(room_pid, :kill)` → Crash simulieren
   - `assert_receive {:room_restarted, ^room_id}` oder State-Validierung nach Restart
2. Horde-Registry-Test: Room-Lookup nach Supervisor-Restart gibt selbes Room zurück

**Owner:** Dev
**Timeline:** Story 4-5 (Session Manager) — Muster dann auf alle GenServer anwenden
**Status:** Geplant
**Verifikation:** Test `room_genserver_crash_and_restart_test` grün

---

#### R-006: Load-Test zu spät (Score: 6) — HIGH

**Mitigation-Strategie:**

1. Basis-k6-Test nach Story 4-9 (createRoom) — auch wenn nur POST /createRoom + GET /sync:
   - 50 VUs als Early-Warning-System
   - Ziel: Architektur-Bottlenecks früh erkennen (DB-Connection-Pool, ETS-Locks)
2. Story 4-23 (500 VU Silber-Tier) bleibt als vollständiger Test

**Owner:** Phil (Priorisierung), Dev (k6-Script)
**Timeline:** Nach Story 4-9
**Status:** Geplant
**Verifikation:** k6 Basic-Run unter 50 VUs → kein Connection-Error, P95 < 1s

---

#### R-009: txnId-Idempotenz (Score: 6) — HIGH

**Mitigation-Strategie:**

1. Godog-Szenario: "Given two clients send identical txnId Then only one event stored"
2. Concurrent Goroutine test in Go (net/http): 10 simultane Requests gleiche txnId → 1 Event in DB

**Owner:** Dev
**Timeline:** Story 4-11 (PUT rooms/{roomId}/send)
**Status:** Geplant
**Verifikation:** DB-Query nach concurrent Sends: `SELECT COUNT(*) FROM events WHERE txn_id=$1` = 1

---

### Annahmen und Abhängigkeiten

#### Annahmen

1. C3 (Bootstrap Wizard Redesign) ist abgeschlossen, bevor diese Test-Strategie vollständig greift
2. CLAUDE.md-Update wird von Dev-Agent und Story-Creator-Agent gelesen und angewendet
3. Docker-Compose-Stack bleibt der primäre Test-Stack (kein Kubernetes für Tests)
4. Playwright MCP ist verfügbar für Browser-E2E-Tests

#### Abhängigkeiten

1. **C3 abschließen** — vor Story 4-5 (Blocker für vollständigen Auth-Flow)
2. **CLAUDE.md + Story-Template aktualisieren** — vor Story 4-5 (Prozess-Blocker)
3. **Godog DB-Cleanup-Hooks** — vor Story 4-21 (Godog Room E2E)

#### Plan-Risiken

- **Risiko:** Dev-Agent ignoriert CLAUDE.md-Mandate unter Zeitdruck
  - **Impact:** ATDD nicht angewendet → Muster aus Epic 3 wiederholt sich
  - **Contingency:** Code-Review-Checkliste als explizites Gate; Story-Creator muss Tests vor Implementation spezifizieren

---

**Ende des Architektur-Dokuments**

**Nächste Schritte für Architektur/Dev-Team:**

1. R-001 + R-003 sofort adressieren (CLAUDE.md + C3)
2. High-Risk-Mitigationen in Story-Backlog einplanen
3. Companion QA-Dokument `test-design-qa.md` für konkrete Test-Szenarien lesen
4. k6 Basis-Test nach Story 4-9 priorisieren

**Nächste Schritte für Story-Creator (Bob/SM):**

1. Story-Template: Pflichtsektion "Acceptance Tests" hinzufügen
2. Bei jeder neuen Story fragen: "Was ist der Test, der diese Story als Done beweist?"
3. Für GenServer-Stories: Crash-Restart-Test als Acceptance-Criterion

**Generiert von:** Murat (BMad TEA Master Test Architect)
**Workflow:** `bmad-testarch-test-design` (System-Level Mode)
**Datum:** 2026-04-03

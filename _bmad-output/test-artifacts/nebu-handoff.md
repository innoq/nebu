---
title: 'TEA Test Design → BMAD Handoff Document'
version: '1.0'
workflowType: 'testarch-test-design-handoff'
inputDocuments:
  - '_bmad-output/test-artifacts/test-design-architecture.md'
  - '_bmad-output/test-artifacts/test-design-qa.md'
sourceWorkflow: 'testarch-test-design'
generatedBy: 'TEA Master Test Architect (Murat)'
generatedAt: '2026-04-03'
projectName: 'Nebu (open-chat)'
---

# TEA → BMAD Integration Handoff

## Zweck

Dieses Dokument verbindet TEAs Test-Design-Outputs mit BMads Story-Creation-Workflow (`bmad-create-story`). Es stellt sicher, dass Qualitätsanforderungen, Risikoregister und Test-Strategie in jede neue Story fließen.

## TEA Artifacts Inventory

| Artifact | Pfad | BMAD-Integration |
|---|---|---|
| Architektur-Testability-Dokument | `_bmad-output/test-artifacts/test-design-architecture.md` | Epic Quality Gates, Prozess-Mandate |
| QA Test-Plan | `_bmad-output/test-artifacts/test-design-qa.md` | Story Acceptance Criteria, Test-Szenarien |
| Risiko-Register | Eingebettet in architecture.md | Story-Priorität, Epic-Risk-Classification |

---

## Epic-Level Integration Guidance

### Kritische Risiken als Epic Quality Gates

**Epic 4 (aktuell in progress) — Pflicht-Quality-Gates:**

| Risk ID | Score | Epic-Level-Gate | Status |
|---|---|---|---|
| R-001 | 9 | CLAUDE.md ATDD-Mandat + Story-Template aktualisiert VOR Story 4-5 | ⚠️ Pending |
| R-003 | 9 | C3 (extractFirstRoleClaim Fix) abgeschlossen VOR Story 4-5 | ⚠️ Pending |
| R-002 | 6 | Horde-Crash-Test implementiert in Story 4-5 | ⬜ Backlog |
| R-006 | 6 | k6 Basis-Test (50 VU) nach Story 4-9 | ⬜ Backlog |
| R-010 | 6 | matrix-js-sdk Smoke-Test nach Story 4-9 | ⬜ Backlog |

**Epic 5 (Compliance) — Pflicht-Quality-Gates:**

| Anforderung | Gate |
|---|---|
| Audit Log Append-Only (P2-017) | RLS-Test VOR Epic 5 Release |
| DSGVO Right-to-be-Forgotten (P2-016) | Kryptografische Löschung validiert |
| Vier-Augen-Prinzip (FR31) | Concurrency-Test: 2 parallele Approvals |

### Empfohlene Quality Gates pro Epic

```
Epic 4 Gate-Kriterien:
- P0-Tests: 15/15 grün (100%)
- P1-Tests: 23/25 grün (≥92%)
- Silber-Tier k6: >500 VU, P95 ≤ 500ms
- Matrix-Client (Element) manuell validiert

Epic 5 Gate-Kriterien:
- Audit-Log RLS getestet
- Compliance-Flow E2E (Godog)
- DSGVO-Löschung kryptografisch validiert
```

---

## Story-Level Integration Guidance

### P0/P1 Test-Szenarien → Story Acceptance Criteria

**Diese Test-Szenarien MÜSSEN als Acceptance Criteria in die entsprechenden Stories:**

| Story | Test-Szenario | Acceptance Criterion |
|---|---|---|
| 4-5 (Session Manager) | P0-003: ETS überlebt Restart | AC: "Given Session Manager restarts, When token validated, Then session still valid" |
| 4-5 (Session Manager) | P0-003 Crash-Test | AC: "ExUnit-Test: kill GenServer → Session via Horde wieder abrufbar" |
| 4-8 (gRPC EventBus) | P1-015: Server-Streaming | AC: "Godog: Client subscribed → Event sent → Event in stream within 500ms" |
| 4-9 (createRoom) | P0-007: Room erstellen + beitreten | AC: "Godog: POST /createRoom → POST /join → GET /sync zeigt Room" |
| 4-11 (send event) | P0-010: txnId Idempotenz | AC: "Godog+concurrent: 2x selbe txnId → 1 Event; beide Responses gleiche event_id" |
| 4-11 (send event) | P1-002: Concurrent 10x | AC: "10 simultane Requests gleiche txnId → COUNT=1 in DB" |
| 4-13 (Power Levels) | P0-004: Unauthorized → 403 | AC: "Unit: User ohne Schreibrecht → {:error, :forbidden}; Integration: → HTTP 403" |
| 4-14 (initial sync) | P1-023: State Events | AC: "Godog: GET /sync ohne since → m.room.create + m.room.member in response" |
| 4-21 (Gherkin Room E2E) | P0-008: Send + Receive | AC: "Godog: alice sendet → bob synct → Event in Timeline" |
| 4-22 (matrix-js-sdk) | P1-003: SDK Smoke | AC: "matrix-js-sdk: login + createRoom + sendEvent → valide Matrix-Responses" |
| 4-23 (k6 Load) | P3-001: Silber-Tier | AC: "k6: 500 VU, 5 min → P95 ≤ 500ms, kein Connection Error" |

### Universelles Story-Template-Mandat

**Jede neue Story (ab Story 4-5) muss diese Sektion enthalten:**

```markdown
## Acceptance Tests

### Tests die ZUERST geschrieben werden (vor Implementation-Code):

1. **[Test-Name]** — [Typ: ExUnit/Godog/Playwright]
   - Given: [Ausgangszustand]
   - When: [Aktion]
   - Then: [Erwartetes Ergebnis]

2. **[Crash/Restart-Test wenn GenServer State]**
   - Given: [Service läuft mit State]
   - When: [Process.exit(:kill) / Service Restart]
   - Then: [State ist wiederhergestellt / korrekt migriert]

### Tests die parallel zur Implementation geschrieben werden:

- Edge Cases: [Liste]
- Fehlerfälle: [Liste]

## Persistenz-Strategie (Pflicht bei GenServer/ETS/State)

Wie überlebt dieser State einen Neustart?
- [ ] In-Memory (ETS): Wird durch Horde-Migration wiederhergestellt. Test vorhanden: [ja/nein]
- [ ] PostgreSQL: Persistent. Read-on-start implementiert: [ja/nein]
- [ ] Kein State: Zustandslos. (Kein Restart-Test nötig)
```

---

## Risiko-zu-Story Mapping

| Risk ID | Category | P×I | Empfohlene Story | Test-Level |
|---|---|---|---|---|
| R-001 | BUS | 9 | CLAUDE.md + Story-Template-Update (sofort) | Prozess |
| R-003 | SEC | 9 | C3 (extractFirstRoleClaim Fix) | Unit + Integration |
| R-002 | TECH | 6 | 4-5 (Session Manager) + 4-2 (Room GenServer) | ExUnit Crash-Test |
| R-004 | TECH | 6 | Alle Stories mit GenServer (Template-Mandat) | ExUnit |
| R-005 | SEC | 6 | 4-13 (Power Levels) | Unit + Godog |
| R-006 | PERF | 6 | Neues Story nach 4-9: Basis-k6 (50 VU) | k6 |
| R-009 | DATA | 6 | 4-11 (send event) | Godog + net/http concurrent |
| R-010 | BUS | 6 | Neues Story nach 4-9: matrix-js-sdk Smoke | Godog/HTTP |
| R-007 | TECH | 4 | 4-8 (gRPC EventBus) | Godog: Error-Handling |
| R-008 | OPS | 4 | 4-21 (Gherkin Room E2E) | Godog Hooks |

---

## Empfohlene BMAD → TEA Workflow-Sequenz

```
1. TEA Test Design (dieses Dokument) ✅ DONE
   └─ Systemweite Strategie + Risiko-Register

2. BMAD Story-Creator (bmad-create-story) → ab Story 4-5
   └─ Konsumiert dieses Handoff; bettet Acceptance Tests in Story ein
   └─ Pflichtsektion: "Acceptance Tests" + "Persistenz-Strategie"

3. TEA ATDD (bmad-testarch-atdd) → bei komplexen Stories
   └─ Generiert failing Acceptance Tests VOR Implementation
   └─ Empfohlen für: 4-5, 4-8, 4-9, 4-11, 4-13, 4-21

4. Dev Implementation → entwickelt gegen failing Tests
   └─ Tests werden grün → Story bereit für Code Review

5. TEA Test Review (bmad-testarch-test-review) → bei Code Review
   └─ Validiert: Haben alle Acceptance Criteria Tests?

6. TEA Trace (bmad-testarch-trace) → vor Epic-Abschluss
   └─ Traceability Matrix: Requirements → Tests, Coverage ≥ 80%
```

## Phase Transition Quality Gates

| Von Phase | Zu Phase | Gate-Kriterien |
|---|---|---|
| Test Design | Story Creation | R-001 + R-003 als Blocking Issues dokumentiert |
| Story Creation | Implementation | Acceptance Tests in Story-Dokument vorhanden (vor Code) |
| Implementation | Code Review | Alle Acceptance Tests grün; Crash-Test für GenServer-Stories |
| Code Review | Done | Keine MAJOR-Findings; P0-Tests 100% |
| Epic Done | Nächster Epic | Alle P0+P1-Tests grün; Retrospektive dokumentiert |

---

**Generiert von:** Murat (BMad TEA Master Test Architect)
**Datum:** 2026-04-03
**Version:** 1.0

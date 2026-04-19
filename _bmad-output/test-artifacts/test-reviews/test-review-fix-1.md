# Test Review — Fix Story 1: rooms.leave Missing m.room.member Event

**Story:** `_bmad-output/implementation-artifacts/fix-1-room-leave-sync-event.md`
**Reviewer:** Master Test Architect (TEA)
**Datum:** 2026-04-19
**Verdict:** **PASS**

---

## 1. AC Coverage-Tabelle

| AC | Beschreibung | Test(s) | Status |
|---|---|---|---|
| **AC #1** | rooms.leave enthält m.room.member leave event in state.events | `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents` | ✅ Abgedeckt |
| **AC #2** | Fix gilt für beide Sync-Pfade (initial + incremental) | `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents` (selber Code-Pfad — `buildLeaveRooms` wird von beiden aufgerufen) | ✅ Abgedeckt |
| **AC #3** | Kein Absturz wenn kein Leave-Event in DB vorhanden | `TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent` | ✅ Abgedeckt |
| **AC #4** | rejected_at-Branch fragt ebenfalls events-Tabelle ab | `TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent` | ✅ Abgedeckt |
| **AC #5** | E2E Regression Guard — room verlässt die Sidebar | `[P0] Leave room → room header disappears within 10 s` in `room-lifecycle.spec.ts` | ✅ Abgedeckt |

**Ergebnis: 5/5 ACs haben mindestens einen Test. Keine MAJOR-Lücke.**

---

## 2. Klassifizierte Findings

### MAJOR — Keine

Alle fünf Acceptance Criteria sind durch mindestens einen Test abgedeckt.

---

### MINOR

#### MINOR-1: AC #2 hat keine direkte separate Assertion für den incremental-sync-Pfad

**Datei:** `gateway/internal/matrix/sync_test.go`
**Beurteilung:** Der Test `TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents` ruft
`h.buildLeaveRooms(ctx, userID)` direkt auf. Da sowohl `GetSync` (initial sync) als
auch `handleIncrementalSync` (incremental sync) intern dieselbe `buildLeaveRooms`-Funktion
aufrufen, gilt ein positiver Test dieser Funktion für beide Code-Pfade. Das ist korrekte
Testarchitektur — keine Duplikation.

Leichter Mangel: Es gibt keine HTTP-Handler-Ebene-Tests (`httptest`), die verifizieren,
dass der Routing-Code tatsächlich `buildLeaveRooms` aufruft. Ein unbeabsichtigtes
Auskoppeln des Aufrufs in `handleIncrementalSync` würde von diesem Test nicht aufgefangen.
Da die Funktion zentral und klar benannt ist, ist das Risiko gering — es bleibt trotzdem
eine Abdeckungslücke auf Handler-Ebene.

**Empfehlung:** In einem separaten Folge-Ticket einen `httptest`-Test ergänzen, der den
vollständigen `/sync`-Response auf HTTP-Ebene prüft. Nicht als Blockierung für diesen Fix.

---

#### MINOR-2: DB-Tests werden in make test-unit-go dauerhaft übersprungen — kein CI-Gate

**Datei:** `_bmad-output/test-artifacts/atdd-checklist-fix-1.md` / CLAUDE.md
**Beurteilung:** Die drei neuen DB-abhängigen Tests (`TestBuildLeaveRooms_*`) verwenden
korrekt das `t.Skip`-Pattern, wenn `NEBU_TEST_DB_URL` nicht gesetzt ist. Das ist
Standard-Go-Pattern und entspricht den Projektkonventionen.

Das ATDD-Checklist-Dokument selbst benennt das explizit als "follow-up CI concern outside
the scope of Fix-1". Der Status ist also dokumentiert und akzeptiert.

Leichter Mangel: Da kein CI-Job existiert, der diese Tests mit einer echten DB ausführt,
können die drei DB-Tests bei einem Merge formal nie „grün" sein — das Merge-Gate fehlt.
Es ist möglich, dass die Implementierung läuft und die Tests dennoch nie ausgeführt werden.

**Empfehlung:** Follow-up-Story (oder Chore-Ticket) erstellen: CI-Job der
`TestBuildLeaveRooms`-Tests mit Docker Compose DB ausführen. Nicht als Blockierung für
diesen Fix — der Dev Agent hat dies selbst als Out-of-Scope dokumentiert.

---

### INFO

#### INFO-1: E2E-Test — kein `page.waitForTimeout`, gutes Netzwerk-Interception-Pattern

Der Playwright-Test in `room-lifecycle.spec.ts` (Zeile 95–122) verwendet `page.waitForResponse`
mit einem 10-Sekunden `Promise.race`-Timeout anstatt eines statischen `waitForTimeout`.
Das ist exzellentes Playwright-Pattern: kein hartes Warten, Reaktion auf echte
Netzwerkevents. Der Test ist deterministisch.

#### INFO-2: Einzigartigkeit der Test-IDs — kein Risiko für Parallelausführung

Alle drei DB-Unit-Tests verwenden feste, einzigartige Identifier (z. B.
`@leavetest-user:test.local`, `!leavetest-room:test.local`). In einer parallelen
Test-Ausführung könnten diese kollidieren, da sie nicht timestamp-basiert sind. Die
Cleanup-Funktionen löschen die Rows korrekt via `t.Cleanup`.

Da `go test` per default keine parallelen Subtests hat und die Tests innerhalb desselben
Packages serialisiert laufen, ist das Risiko gering. Falls `go test -parallel` eingesetzt
wird, könnten Konflikte entstehen.

**Empfehlung:** Bei Bedarf `t.Parallel()` vermeiden oder UUIDs für Test-IDs einsetzen.
Kein Handlungsbedarf für diesen Fix.

#### INFO-3: JSONB double-encoding — nur "object"-Form in Tests abgedeckt

Die Fixture-Funktionen `insertTestLeaveFixture` und der Rejected-Invite-Test fügen
Content stets als JSONB object ein (`$n::jsonb`). Die `buildLeaveRooms`-Implementierung
enthält den `CASE WHEN jsonb_typeof(content) = 'object'`-Guard für beide Formen (object
und string). Der "string form" (double-encoded) Pfad wird von keinem Unit-Test abgedeckt.

Der Story-Kommentar verweist auf `buildInviteRooms` als bewährtes Vorbild — da die
bestehenden Invite-Tests ebenfalls nur "object"-Form testen, ist das Gesamtprojektmuster
konsistent. Kein Blockierungsgrund.

**Empfehlung:** Separater Unit-Test für double-encoded JSONB-Content als Nice-to-have
dokumentieren.

---

## 3. Test-Qualitätsprüfung (Checkliste)

| Kriterium | Status | Anmerkung |
|---|---|---|
| Kein `time.Sleep` / hartes Warten in Go-Tests | ✅ | Keine `time.Sleep`-Calls in den drei neuen Tests |
| Kein `page.waitForTimeout` in Playwright | ✅ | Korrekt: `page.waitForResponse` + `Promise.race` |
| Determinismus (kein ungeseedter Zufall) | ✅ | Feste Test-IDs; E2E nutzt `Date.now()` für Room-Namen (ausreichend eindeutig) |
| Kein Cookie Forging / DB-Seeding-Shortcut in E2E | ✅ | OIDC-Login via `loginViaOidc` (Authorization Code + PKCE), API-Calls mit echtem Token |
| DB-Tests skippen graceful wenn `NEBU_TEST_DB_URL` fehlt | ✅ | `openTestDB` ruft `t.Skip` auf — kein Fail |
| Test-Isolation — eigene Cleanup-Funktion | ✅ | Alle drei DB-Tests nutzen `t.Cleanup(cleanup)` |
| Keine shared State zwischen Tests | ✅ | Jeder Test erzeugt eigene Rows mit eindeutigen IDs |

---

## 4. Implementierungs-Querverweis

Die Implementierung in `gateway/internal/matrix/sync.go` (Zeilen 71–163) wurde gegen
die Acceptance Tests geprüft:

- **AC #1:** `buildStateEvents`-Closure mit JSONB-Query wird für `left_at IS NOT NULL`-Branch
  aufgerufen — korrekt.
- **AC #3:** Bei `sql.ErrNoRows` und allen anderen Fehlern gibt `buildStateEvents` `[]map[string]interface{}{}`
  zurück — korrekt, kein panic.
- **AC #4:** `rejected_at IS NOT NULL`-Branch ruft ebenfalls `buildStateEvents(roomID)` auf
  (Zeile 153) — korrekt.
- **AC #2:** `buildLeaveRooms` ist die einzige Funktion; beide Sync-Pfade rufen sie auf —
  Implementierung deckt beide Pfade durch eine einzige Funktion ab.

Implementierung ist konsistent mit den Tests. Kein Widerspruch gefunden.

---

## 5. ATDD-Checklist-Review

`_bmad-output/test-artifacts/atdd-checklist-fix-1.md` ist vollständig und korrekt:

- Test-Inventory mit 4 Tests, klaren Pre-fix-Ergebnissen
- AC Coverage Matrix vollständig
- Run-Anweisungen für DB-Tests und E2E vorhanden
- Pre-fix Failure Evidence dokumentiert (Failure-Output mit korrekten Fehlermeldungen)
- Definition of Done klar formuliert

Das Dokument erfüllt die Anforderungen an ein TEA-ATDD-Artefakt vollständig.

---

## 6. Overall Verdict

**PASS**

Alle 5 Acceptance Criteria sind durch Tests abgedeckt. Die Tests sind qualitativ gut:
kein hartes Warten, korrektes Graceful-Skip-Pattern, saubere Isolation, kein Cookie Forging.

Die beiden MINOR-Findings (fehlender HTTP-Handler-Level-Test für AC #2 und fehlendes
CI-Gate für DB-Tests) sind bekannt und vom Dev Agent explizit als Out-of-Scope
dokumentiert — sie sind akzeptabel als Follow-up-Work.

Kein MAJOR-Finding. Der Fix ist bereit für das Code Review.

---

*Reviewed by: TEA — Master Test Architect*
*Datum: 2026-04-19*

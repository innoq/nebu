---
name: bmad-pipeline-local
description: >
  BMAD-Pipeline optimiert für lokale LLMs (256k Kontextfenster, kein Task-Tool).
  Läuft vollständig im Haupt-Kontext ohne Subagenten. Nutzt RTK intensiv für alle
  Datei-, Git- und Suchoperationen (60–90% Token-Einsparung). Strukturiert jede Phase
  mit expliziten Kontext-Checkpoints und aggressiver Kompression. Kein Modell-Routing —
  das lokale Modell übernimmt alle Gates.
  Trigger: "pipeline local", "lokale pipeline", "offline pipeline", "ollama pipeline",
  "pipeline ohne api", "bmad local run", immer wenn der User die BMAD-Pipeline ohne
  Cloud-API starten will.
---

# BMAD Pipeline Local

Dieser Skill orchestriert den vollständigen BMAD-Entwicklungszyklus **im aktuellen
Kontext** (kein Task-Tool, kein Subagent). Optimiert für lokale LLMs mit 256k Fenster.

---

## RTK-Pflichtregeln (immer einhalten)

Jede Dateileseoperation, jeder Git-Befehl und jede Suche läuft durch RTK:

| Statt… | Immer… |
|---|---|
| `cat <datei>` | `rtk read <datei>` |
| `cat <datei>` (groß/unkritisch) | `rtk read --level aggressive <datei>` |
| `cat <datei>` (Kontext >50%) | `rtk read -u --level aggressive <datei>` |
| `git diff --staged` | `rtk git diff --staged` |
| `git status` | `rtk git status` |
| `git log` | `rtk git log` |
| `git show <sha>` | `rtk git show <sha>` |
| `git add` | `rtk git add <datei>` |
| `git commit` | `rtk git commit -m "..."` |
| `grep -r <pat>` | `rtk grep <pat> .` |
| `find . -name "*.md"` | `rtk find . -name "*.md"` |
| Schnelle Datei-Einschätzung | `rtk smart <datei>` |
| Build/Test-Output | `rtk summary make test-unit-go` |

**Kontextdruck-Protokoll:**
- Kontext < 40% voll → `rtk read` (Standard, kein Level)
- Kontext 40–70% voll → `rtk read --level minimal`
- Kontext > 70% voll → `rtk read -u --level aggressive`
- Kontext > 85% voll → nur `rtk smart <datei>` oder `rtk grep` verwenden, keine vollen Lesevorgänge

---

## Ablauf-Übersicht

```
[INIT]   RTK-Check + Skill-Dateien laden (aggressive)
   ↓
[1]  bmad-create-story     (inline, current context)
   ↓
[CP1] Kontext-Checkpoint: Story-Zusammenfassung sichern
   ↓
[1b] Matrix-Gate (Oracle)  (inline)  ← nur wenn Matrix-Feature
     MATRIX_ORACLE_CONTEXT → an [2] und [5] weitergegeben
   ↓
[2]  bmad-testarch-atdd    (inline)  ← TEA Gate 1
   ↓
[CP2] Kontext-Checkpoint: Test-Dateinamen sichern
   ↓
[3]  bmad-dev-story        (inline)
   ↓
     rtk git add .
   ↓
[CP3] Kontext-Checkpoint: Implementierungs-Diff-Zusammenfassung
   ↓
[4]  bmad-testarch-test-review (inline)  ← TEA Gate 2
   ↓
[5]  bmad-code-review      (inline) — Minor Issues sofort fixen
   ↓
     rtk git add .
   ↓
[5b] Security-Review-Gate  (conditional, inline)
   ↓
[6]  Review-Auswertung + sprint-status.yaml + git commit
   ↓
[7]  Epic-Status-Check → SEC Gate 2 (optional) → Retro-Hinweis
```

---

## INIT: RTK-Check und Skill-Vorladen

**Vor dem ersten Schritt immer ausführen:**

```bash
rtk --version
```

Falls `rtk` nicht gefunden: Abbruch mit:
```
⛔ RTK nicht installiert. Installiere RTK und starte neu.
   Ohne RTK läuft diese Pipeline nicht — das 256k Fenster reicht nicht.
```

**Skill-Dateien vorladen** (aggressive, da sie nur als Referenz dienen):

```bash
rtk read --level aggressive skills/bmad-pipeline-local/SKILL.md
rtk find . -name "SKILL.md" -type f
```

Liste alle gefundenen SKILL.md-Pfade. Halte die Pfade fest — du liest sie in den
jeweiligen Schritten mit `--level aggressive`.

**Story-Kontext aus User-Input:**
Notiere Feature-Titel und alle Constraints aus der User-Anfrage als kompakte 3-Zeilen-Zusammenfassung.
Diese Zusammenfassung wird an jedem Checkpoint wiederholt.

---

## Kontext-Checkpoint-Format

An jedem `[CPn]`-Punkt wird folgendes kompakt (max. 10 Zeilen) festgehalten:

```
=== CP{N}: {Schritt-Name} ===
Feature: {1-Zeile}
Story-Datei: {pfad oder "noch nicht erstellt"}
Test-Dateien: {liste, 1 pro Zeile}
Diff-Summary: {1-Zeile oder "—"}
Findings: {MAJOR/MINOR/INFO-Liste oder "keine"}
Nächster Schritt: {Schritt-Name}
================================
```

Dieser Block **ersetzt** keine vorherigen Ergebnisse — er fasst sie nur kompakt zusammen,
damit nach einer etwaigen Kompression des Kontexts die Kernfakten erhalten bleiben.

---

## Schritt 1: bmad-create-story (inline)

```bash
rtk read --level aggressive skills/bmad-create-story/SKILL.md
```

Befolge die Anweisungen aus der SKILL.md. Feature-Beschreibung aus User-Input.

Arbeite inline: erstelle die Story-Datei direkt. Kein Subagent.

Nach Erstellung:
```bash
rtk smart <story-datei.md>
```

Zeige: `✓ [1] Story erstellt → <pfad>`

**[CP1] Kontext-Checkpoint ausfüllen.**

---

## Schritt 1b: Matrix-Gate — Oracle-Konsultation (inline, conditional)

**Erkennung:**

```bash
rtk grep -i "_matrix/\|m\.room\.\|m\.login\|txnId\|sync.*since\|matrix.*spec" <story-datei.md>
```

Matrix-Bezug liegt vor wenn Endpoint-Pfade, Event-Typen (`m.room.*`), Matrix-Fehlercodes (`M_FORBIDDEN` etc.) oder Matrix-Konzepte im Story-Text erscheinen.

**Wenn kein Matrix-Bezug:**

`⏭️ [1b] Matrix-Gate übersprungen.`  
`MATRIX_ORACLE_CONTEXT = null` — Schritt 2 und 5 erhalten keinen Oracle-Input.

**Wenn Matrix-Feature erkannt:**

```bash
rtk read --level aggressive skills/agent-oracle/SKILL.md
rtk read skills/agent-oracle/references/spec-lookup.md
rtk read skills/agent-oracle/references/test-guidance.md
rtk read <story-datei.md>
```

Handle als Oracle: Welche Matrix CS API v1.18 MUST-Anforderungen, Fehlercodes und spec-definierten Edge Cases sind für diese Story relevant?

Schreibe das Ergebnis kompakt (max. 40 Zeilen) als `MATRIX_ORACLE_CONTEXT` in den Kontext:

```
=== MATRIX_ORACLE_CONTEXT ===
[MUST-Anforderungen und spec-definierte Testfälle als Listen]
=== END MATRIX_ORACLE_CONTEXT ===
```

Zeige: `✓ [1b] Oracle konsultiert — Matrix-Spec-Context erfasst.`

---

## Schritt 2: bmad-testarch-atdd (inline) — TEA Gate 1

**Ausnahme:** Reine Infra-Stories (Dockerfile, Migration ohne Logik) → `⏭️ [2] ATDD übersprungen (Infra-only).`

Für alle anderen Stories:

```bash
rtk read --level aggressive skills/bmad-testarch-atdd/SKILL.md
rtk read <story-datei.md>
```

Generiere failing Acceptance Tests inline. Schreibe die Test-Dateien.

Falls `MATRIX_ORACLE_CONTEXT` im Kontext vorhanden: stelle sicher, dass alle dort
genannten Fehlercodes, HTTP-Status-Codes und Edge Cases als eigene failing Tests
abgedeckt sind. Die Matrix-Spezifikation definiert explizit welche Fehlerantworten
auf welche Eingaben zu erfolgen haben — nicht nur den Happy-Path testen.

Nach Generierung:
```bash
rtk find . -name "*_test.*" -newer <story-datei.md> -type f
```

Zeige: `✓ [2] Failing Acceptance Tests generiert.`

**[CP2] Kontext-Checkpoint ausfüllen.** Test-Dateipfade in `Test-Dateien` eintragen.

---

## Schritt 3: bmad-dev-story (inline)

```bash
rtk read --level aggressive skills/bmad-dev-story/SKILL.md
rtk read <story-datei.md>
```

Lese die failing Tests **kompakt**:
```bash
rtk read --level minimal <test-datei-1> <test-datei-2>
```

Implementiere inline gegen die failing Tests. Mache keine Implementierung ohne vorherigen
roten Test.

Nach Implementierung:
```bash
rtk git add .
rtk git status
rtk git diff --staged
```

Zeige: `✓ [3] Implementierung abgeschlossen, git add ausgeführt.`

**[CP3] Kontext-Checkpoint ausfüllen.** Diff-Summary in 1 Zeile.

---

## Schritt 4: bmad-testarch-test-review (inline) — TEA Gate 2

```bash
rtk read --level aggressive skills/bmad-testarch-test-review/SKILL.md
rtk git diff --staged
```

Für jede gestagete Test-Datei:
```bash
rtk read --level minimal <test-datei>
```

Prüfe:
- Hat jedes Acceptance Criterion der Story mindestens einen Test? (MAJOR wenn nicht)
- Gibt es Hard Waits, versteckte Assertions, nicht-deterministische Tests?
- Haben GenServer-State-Stories einen Crash/Restart-Test?
- Wird Cookie-Forging oder DB-Seeding als Shortcut verwendet?

Klassifiziere jeden Fund als `MAJOR`, `MINOR` oder `INFO`.

**Wenn MAJOR Findings:**

```
⚠️ [4] MAJOR Test-Lücken gefunden:
{Liste der MAJOR Findings}
Behebe die Lücken oder tippe "weiter" um trotzdem fortzufahren.
```

Stoppe und warte.

**Wenn keine MAJOR Findings:**

Zeige: `✓ [4] Test-Review bestanden. {N} MINOR, {M} INFO.`

---

## Schritt 5: bmad-code-review (inline) — Minor Issues sofort fixen

```bash
rtk read --level aggressive skills/bmad-code-review/SKILL.md
rtk git diff --staged
```

**Lese den diff kompakt.** Falls der diff groß ist (>200 Zeilen):
```bash
rtk diff <(git diff --staged) /dev/null
```

Reviewe alle gestagten Änderungen. Nutze die Findings aus [4] als Eingabe.

Falls `MATRIX_ORACLE_CONTEXT` im Kontext vorhanden: prüfe ob die Implementierung
ALLEN dort genannten MUST-Anforderungen der Matrix CS API v1.18 entspricht.
Jede Abweichung von der Spec ist ein Finding. Falsches `errcode` oder falscher
HTTP-Status-Code = MAJOR.

**Minor Issues**: Fixen sofort, ohne zu pausieren.

Für jede Datei die gefixed werden muss:
```bash
rtk read --level minimal <datei>
```

Nach allen Fixes:
```bash
rtk git add .
rtk git status
```

**Wenn MAJOR/CRITICAL Issues:**

```
⚠️ [5] Blockierende Issues gefunden:
{Liste}
Tippe "weiter" um trotzdem zu commiten, oder behebe und starte [3] neu.
```

Stoppe und warte.

**Wenn keine blockierenden Issues:**

Zeige: `✓ [5] Code-Review bestanden. Minor Issues gefixed.`

---

## Schritt 5b: Security-Review-Gate (inline, conditional) — SEC Gate 1

**Security-Review-Entscheidung:**

```bash
rtk grep "security_review:" <story-datei.md>
```

- `security_review: not-needed` → `⏭️ [5b] Security-Review übersprungen.` → weiter zu [6]
- `security_review: optional` → frage User: "Security-Review jetzt ausführen? [Y/n]"
- `security_review: required` oder **Flag fehlt** → Auto-Klassifikation:

```bash
rtk git diff --staged --name-only
```

Markiere als `required`, wenn mindestens eines zutrifft:
- Dateipfad enthält `gateway/internal/auth/`, `gateway/internal/middleware/`,
  `gateway/internal/admin/`, `gateway/internal/db/`
- Neue HTTP-Route in `gateway/cmd/gateway/main.go`
- Dateipfad enthält `core/apps/signature/` oder `core/apps/permissions/`
- `.ex`-Datei importiert `:crypto` oder nutzt `Plug.Conn` auf externem Input
- Neue SQL-Migration unter `gateway/migrations/`

**Falls required:**

```bash
rtk read --level aggressive skills/bmad-security-review/SKILL.md
rtk git diff --staged
```

Lese den Security-Scope aus der SKILL.md und reviewe den gestagten Diff.
Schreibe den Report nach:
`_bmad-output/implementation-artifacts/security-reports/{story-id}-security-review.md`

Gib Classification: `CRITICAL | HIGH | CLEAN`

**Bei CRITICAL:**
```
🔴 [5b] Kassandra: CRITICAL — Report: {pfad}
Pipeline gestoppt. Optionen:
  (a) fixen und neu starten ab [3]
  (b) akzeptieren mit schriftlicher Begründung
  (c) als Follow-up-Story verschieben
Tippe "weiter" um trotzdem zu commiten.
```
Stoppe und warte.

**Bei HIGH:** Kassandra entscheidet laut SKILL.md — übernimm 1:1.

**Bei CLEAN:** `✓ [5b] Kassandra — clean.`

```bash
rtk git add .
```

---

## Schritt 6: Review-Auswertung + Commit

**Blockierungs-Check** (aus [4], [5], [5b]):
- Schlüsselwörter: `MAJOR`, `CRITICAL`, `HIGH severity`, blockierend
- Abschnitte: `## Major Issues`, `## CRITICAL`, `## HIGH`

**Wenn blockierend (und noch nicht gestoppt):**

```
⚠️ Blockierende Issues (siehe oben). Tippe "weiter" zum Commiten.
```

Stoppe. Bei "weiter": fahre fort.

**sprint-status.yaml aktualisieren:**

```bash
rtk read _bmad-output/implementation-artifacts/sprint-status.yaml
```

1. Story-Status auf `done` setzen (Key: vollständiger Slug aus Story-Datei)
2. `last_updated:` auf heutiges Datum (beide Vorkommen)
3. Neue Kommentarzeile direkt unter `# last_updated:`:
   ```
   # story {ID} done (local-pipeline: {KURZE_ZUSAMMENFASSUNG}): {YYYY-MM-DD}
   ```
   Beispiele für `{KURZE_ZUSAMMENFASSUNG}`:
   - `ATDD+Dev+Code CLEAN`
   - `2 MINOR fixed + SEC CLEAN`
   - `HIGH akzeptiert, Follow-up 6-1`

```bash
rtk git add _bmad-output/implementation-artifacts/sprint-status.yaml
```

**Commit:**

```bash
rtk git commit -m "$(cat <<'EOF'
{KURZE_ZUSAMMENFASSUNG_AUS_STORY}
EOF
)"
```

Keine `Co-Authored-By`-Zeile.

Zeige: `✓ [6] Commit erstellt.`

---

## Schritt 7: Epic-Status prüfen

```bash
rtk read --level minimal _bmad-output/implementation-artifacts/sprint-status.yaml
```

Suche nach `epic-{N}-retrospective`. Prüfe, ob die gerade committete Story die **letzte
Story vor dem Retrospektive-Eintrag** war.

**Epic läuft noch:**

```
✅ Story abgeschlossen. Nächste Story? [Y/n]
```

- `Y` → zurück zu [1]
- `n` → Beende

**Epic abgeschlossen → SEC Gate 2 (zwingend):**

Epic-Base bestimmen:
```bash
rtk git log --grep="epic-{N-1}-retrospective\|retrospektive" --oneline
```

Falls unklar: frage User nach Commit-SHA oder Tag.

```bash
rtk read --level aggressive skills/bmad-security-review/SKILL.md
```

Führe Kassandra mit Epic-Diff-Range aus:
```bash
rtk git diff <epic-base>..HEAD
```

Zusätzlicher Fokus: ganzheitliche Angriffsflächen über Stories hinweg (Auth-Bypass-Kombinationen,
RLS-Brüche durch Migrations-Kombinationen, Middleware-Umgehungen durch neue gRPC-Handler).

Report nach:
`_bmad-output/implementation-artifacts/security-reports/epic-{N}-security-review-{YYYY-MM-DD}.md`

**Auswertung:**
- CRITICAL/HIGH → Stoppe, User entscheidet (Follow-up-Stories oder Risk-Accept)
- CLEAN → weiter zur Retro-Pause

```
🏁 Epic abgeschlossen!
SEC Gate 2 Report: epic-{N}-security-review-{YYYY-MM-DD}.md
Führe /bmad-testarch-trace aus (Traceability-Matrix).
Führe /bmad-retrospective aus (Retro).
Dann: nächstes Epic mit "bmad pipeline local".
```

Stoppe hier.

---

## Fehlerbehandlung

| Situation | Aktion |
|---|---|
| RTK nicht gefunden | Abbruch mit Installationshinweis |
| SKILL.md nicht gefunden | Pfad melden, stoppen |
| `rtk read` schlägt fehl | Fallback: `rtk smart <datei>` für Kurzzusammenfassung |
| git add/commit fehlschlägt | Fehler zeigen, stoppen |
| sprint-status.yaml fehlt | `ℹ️ nicht gefunden — Epic-Check übersprungen` |
| Kontext > 90% voll | `⚠️ Kontext-Warnung: Nutze ausschließlich rtk smart und rtk grep. Kein volles Lesen.` |
| ATDD-Skill fehlt | `⚠️ TEA Gate 1 übersprungen — Acceptance Tests manuell erstellen` |
| Test-Review-Skill fehlt | `⚠️ TEA Gate 2 übersprungen` |

---

## RTK-Kurzreferenz für diese Pipeline

```bash
# Datei lesen (Standard)
rtk read <datei>

# Datei lesen (groß oder unkritisch)
rtk read --level aggressive <datei>

# Mehrere Dateien gleichzeitig (spart Overhead)
rtk read --level minimal datei1 datei2 datei3

# Ultra-kompakt bei Kontextdruck
rtk read -u --level aggressive <datei>

# 2-Zeilen-Zusammenfassung (sehr günstig)
rtk smart <datei>

# Gestagete Änderungen
rtk git diff --staged

# Nur Dateinamen der gestageten Änderungen
rtk git diff --staged --name-only

# Kompakter Status
rtk git status

# Ein-Zeilen-Log
rtk git log

# Pattern in Datei suchen
rtk grep "security_review" <datei>

# Pattern rekursiv suchen
rtk grep "mux.Handle" gateway/

# Dateien nach Muster finden
rtk find . -name "SKILL.md" -type f

# Test-Output filtern (nur Fehler)
rtk summary make test-unit-go

# Git add (kompakt)
rtk git add <datei>
rtk git add .

# Git commit
rtk git commit -m "<message>"

# Token-Ersparnis anzeigen
rtk gain
```

---

## Modell

Kein Modell-Routing — das lokale Modell übernimmt alle Phasen im aktuellen Kontext.
Kein `claude-opus-4-7`, kein `claude-sonnet-4-6`. Die Pipeline ist modellunabhängig.

---

## Wichtige Unterschiede zur Cloud-Pipeline

| | bmad-pipeline | bmad-pipeline-local |
|---|---|---|
| Subagenten | Ja (Task-Tool) | Nein — alles inline |
| Modelle | Sonnet + Opus | Lokales Modell |
| Kontext-Budget | Jeder Subagent frisch | 256k geteilt, mit RTK-Schutz |
| RTK | Optional (Hook) | Pflicht, explizit |
| Kontext-Checkpoints | Nicht nötig | Pflicht nach jedem Schritt |
| Code-Review-Modell | claude-opus-4-7 | Lokales Modell |
| Parallelisierung | Nicht genutzt | N/A |

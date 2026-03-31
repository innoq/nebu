---
name: bmad-pipeline
description: >
  Orchestriert den kompletten BMAD-Entwicklungszyklus als automatische Pipeline:
  bmad-create-story → bmad-dev-story → bmad-code-review, jeweils in frischem Kontext.
  Führt git add nach dev-story aus. Der Code-Review-Skill fixt Minor Issues selbst
  ("fixe minor issues instantly"). Pausiert nur bei Major Issues (User-Entscheidung)
  und am Epic-Ende für die Retrospektive (liest sprint-status.yaml).
  Trigger: "bmad pipeline", "bmad run", "bmad start", "story pipeline", "neues feature",
  "story durchlaufen", "pipeline starten", immer wenn der User den kompletten BMAD-Flow
  starten will ohne jeden Schritt einzeln aufzurufen.
---

# BMAD Pipeline Skill

Dieser Skill orchestriert den vollständigen BMAD-Entwicklungszyklus in einer einzigen
Ausführung. Jeder Schritt läuft in einem eigenen, frischen Subagenten-Kontext.

## Ablauf-Übersicht

```
[1] bmad-create-story    (Sonnet, frischer Kontext)
         ↓
[2] bmad-dev-story       (Sonnet, frischer Kontext)
         ↓
      git add -A
         ↓
[3] bmad-code-review     (Opus, frischer Kontext)
     "fixe minor issues instantly"
     → Minor Issues werden vom Skill selbst gefixt
         ↓
      git add -A   (fängt eventuelle Minor-Fixes auf)
         ↓
   Major Issues gefunden?
      Ja  → Pause, User entscheidet
      Nein → git commit
         ↓
[4] Epic-Check: _bmad-output/implementation-artifacts/sprint-status.yaml
      Epic fertig? → Pause für Retrospektive
      Sonst       → ✅ Fertig, nächste Story bereit
```

---

## Schritt-für-Schritt-Anleitung

### Vorbereitung

Lies die BMAD-Skill-Pfade im aktuellen Verzeichnis:
- `.claude/skills/bmad-create-story/SKILL.md`
- `.claude/skills/bmad-dev-story/SKILL.md`
- `.claude/skills/bmad-code-review/SKILL.md`

Halte den Story-Titel oder die Feature-Beschreibung vom User fest – sie wird an alle
Subagenten weitergegeben.

---

### Schritt 1: bmad-create-story

**Modell:** `claude-sonnet-4-6` | **Kontext:** frisch (Task-Tool)

```
Lies und befolge die Anweisungen aus .claude/skills/bmad-create-story/SKILL.md.
Feature/Story: [FEATURE_BESCHREIBUNG_VOM_USER]
Arbeite das vollständig durch und beende dann.
```

Warte auf Fertigstellung. Zeige: `✓ Schritt 1: Story erstellt.`

---

### Schritt 2: bmad-dev-story + git add

**Modell:** `claude-sonnet-4-6` | **Kontext:** frisch (Task-Tool)

```
Lies und befolge die Anweisungen aus .claude/skills/bmad-dev-story/SKILL.md.
Implementiere die zuletzt erstellte Story vollständig.
Beende nach Abschluss der Implementierung.
```

Warte auf Fertigstellung. Danach:

```bash
git add -A
```

Zeige: `✓ Schritt 2: Implementierung abgeschlossen, git add ausgeführt.`

---

### Schritt 3: bmad-code-review (inkl. Minor-Issue-Fix)

**Modell:** `claude-opus-4-6` | **Kontext:** frisch (Task-Tool)

```
Lies und befolge die Anweisungen aus .claude/skills/bmad-code-review/SKILL.md.
fixe minor issues instantly
Reviewe alle gestagten Änderungen (git diff --staged).
Gib das vollständige Ergebnis aus. Klassifiziere jeden Fund explizit als
MAJOR, MINOR oder INFO – damit die Pipeline die Schwere eindeutig erkennen kann.
```

Der Skill fixt Minor Issues selbst während des Reviews. Warte auf die vollständige Ausgabe.

Danach in jedem Fall:

```bash
git add -A
```

Dieser zweite `git add` stellt sicher, dass eventuelle Minor-Fixes des Review-Skills
gestaggt sind, bevor die Pipeline weiterläuft.

---

### Schritt 4: Review-Auswertung

Analysiere die Ausgabe auf **Major Issues**:

- Schlüsselwörter: `MAJOR`, `Critical`, `CRITICAL`, `🔴`, `HIGH severity`, `blocking`
- Abschnitte: `## Major Issues`, `## Critical Issues`, `### Blocking Problems`
- Aussagen wie "must be fixed before merging"

**Wenn Major Issues gefunden:**

```
⚠️ bmad-code-review hat Major Issues gefunden (siehe oben).
Pipeline gestoppt. Bitte behebe die Issues und starte neu –
oder tippe "weiter" um trotzdem zu commiten.
```

Stoppe und warte. Bei "weiter": fahre mit dem Commit fort.

**Wenn keine Major Issues:**

Zeige: `✓ Kein Major Issue – commite automatisch.`

```bash
git commit -m "$(cat <<'EOF'
[KURZE_ZUSAMMENFASSUNG_AUS_STORY_ODER_REVIEW]
EOF
)"
```

Zeige: `✓ Commit erstellt.`

---

### Schritt 5: Epic-Status prüfen

Lies die Datei:

```
_bmad-output/implementation-artifacts/sprint-status.yaml
```

Suche in der YAML nach dem Feld `epic-{N}-retrospective` (z.B. `epic-1-retrospective`).
Die Epic-Nummer entnimmst du dem Namen der aktuellen Story oder der sprint-status.yaml selbst.

**Retro ist fällig, wenn:**
Der Eintrag, der in der Datei direkt **vor** `epic-{N}-retrospective` steht (also die letzte
Story des Epics), soeben auf `done` gesetzt wurde – sprich: die Story, die wir gerade
committed haben, ist genau diese letzte Story.

Konkret: Lies die YAML-Datei, finde `epic-{N}-retrospective`, schaue auf den Key direkt
darüber. Wenn dieser Key mit dem Namen der gerade abgeschlossenen Story übereinstimmt
und seinen Wert `done` hat, ist das Epic fertig.

**Epic abgeschlossen:**

```
🏁 Epic abgeschlossen!
Alle Stories sind erledigt. Zeit für die Retrospektive.
Wenn du fertig bist, starte den nächsten Epic mit "bmad pipeline".
```

Stoppe hier und warte auf den User.

**Epic läuft noch:**

```
✅ Story abgeschlossen. Nächste Story starten? [Y/n]
```

- Bei `Y` oder Enter: Starte die Pipeline von vorne (Schritt 1: bmad-create-story).
- Bei `n`: Beende die Pipeline.

---

## Fehlerbehandlung

- **Subagent schlägt fehl:** Zeige die vollständige Fehlermeldung. Stoppe, User entscheidet.
- **git add / git commit schlägt fehl:** Zeige den git-Fehler. Stoppe, warte auf User-Aktion.
- **sprint-status.yaml nicht vorhanden:** `ℹ️ sprint-status.yaml nicht gefunden – Epic-Check übersprungen.`
- **SKILL.md nicht gefunden:** Melde den fehlenden Pfad und stoppe.

---

## Modell-Übersicht

| Schritt           | Modell            |
|-------------------|-------------------|
| bmad-create-story | claude-sonnet-4-6 |
| bmad-dev-story    | claude-sonnet-4-6 |
| bmad-code-review  | claude-opus-4-6   |

---

## Wichtige Hinweise

- Gib dem User nach jedem Schritt ein kurzes Status-Update.
- Die Feature-Beschreibung des Users muss an alle Subagenten weitergegeben werden.
- Alle Schritte laufen **sequenziell** – kein paralleles Starten von Subagenten.
- Das `git add -A` nach dem Code-Review ist immer auszuführen, unabhängig davon ob
  Minor Issues gefunden wurden oder nicht – es schadet nicht und stellt Vollständigkeit sicher.
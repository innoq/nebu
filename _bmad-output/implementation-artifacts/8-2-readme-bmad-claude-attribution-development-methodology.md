---
security_review: not-needed
---

# Story 8.2: README — BMAD + Claude Attribution & Development Methodology Section

Status: ready-for-dev

## Story

**As a** potential contributor or enterprise evaluator landing on the GitHub repo,
**I want** the README to transparently state the project's development methodology (BMAD-driven, AI-assisted via Claude),
**so that** I understand how the code was produced without that attribution being commit-log noise.

**Size:** XS

---

## Background

Story 8.1 removes all `Co-Authored-By: Claude ...` trailers from the Git history. That removal strips the only current attribution signal for Claude's structural role in development. Before the repository goes public, the README must carry that attribution explicitly — as a first-class, readable statement of methodology, not as noise in commit messages.

**What BMAD is:** BMAD (Brain Model Agile Development) is a structured agent pipeline used throughout this project. It sequences work through dedicated AI roles: Story Creator (SM/Bob), Test Architect (TEA), Developer (Amelia), Code Reviewer, and Security Reviewer. Each story passes through a defined gate sequence: Story Creation → ATDD → Dev → Test Review → Code Review → (conditional) Security Review. This methodology is documented fully in `CONTRIBUTING.md` (Story 8.3).

**What Claude's role was:** Claude (via Claude Code, Anthropic's CLI) served as the AI backend for all BMAD agent roles. Model versions used across the development history: Claude Opus 4.6, Claude Opus 4.7 (1M context), Claude Sonnet 4.5, Claude Sonnet 4.6. The code was reviewed, tested, and accepted by the human maintainer at each gate.

**Why this section belongs in README (not only CONTRIBUTING.md):**
- It is the first document a visitor sees. Transparency about AI-assisted development is a project value.
- GitHub's "About" panel and search results surface README content. A discoverable methodology section builds trust with enterprise evaluators.
- CONTRIBUTING.md (Story 8.3) covers the workflow details; README carries the high-level statement.

**Position in README:** The new section slots between the existing "Architecture" section and the existing "Quick Start" section, matching the target order specified in the epic: Overview → Features → Architecture → **Development Methodology** → Getting Started → Contributing → License.

**Current README state:** `README.md` in the repo root already contains all other sections. The "Development Methodology" section is absent. The Contributing section already links to `CONTRIBUTING.md` (which will be created by Story 8.3).

---

## Acceptance Criteria

1. **Section heading exists**: `README.md` contains an H2 heading `## Development Methodology` (exact spelling and casing).

2. **BMAD explanation**: The section contains a brief, factual explanation of the BMAD methodology — at minimum one sentence that uses the word "BMAD" and describes it as a structured agent-driven development pipeline.

3. **Claude attribution sentence**: The section contains the attribution statement (verbatim or near-verbatim): "This project was developed using the BMAD methodology with AI assistance via Claude (Opus 4.6/4.7, Sonnet 4.5/4.6) through Claude Code" — factual, no marketing language.

4. **Link to CONTRIBUTING.md**: The section contains a Markdown link `[CONTRIBUTING.md](CONTRIBUTING.md)` (or `[CONTRIBUTING.md](./CONTRIBUTING.md)`) pointing to the contributing guide for workflow details.

5. **Section order**: In `README.md`, the `## Development Methodology` heading appears after `## Architecture` and before `## Quick Start` (or equivalent "Getting Started" heading).

6. **No emojis**: The `## Development Methodology` section contains no emoji characters.

7. **Markdown lint clean**: The modified `README.md` passes `markdownlint` with the project's existing rules (no new lint errors introduced by this story).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

These are static content checks — no runtime, no compilation. Each test is a `grep`/`awk` assertion against `README.md` that can be run as a shell one-liner or integrated into a `make lint-docs` target.

1. **`AC1: H2 heading present`** — grep check
   - Given: `README.md` in the repo root after implementation
   - When: `grep -c "^## Development Methodology$" README.md`
   - Then: output is `1` (exactly one occurrence, no leading/trailing whitespace)

2. **`AC2: BMAD word in section`** — grep check
   - Given: `README.md` after implementation
   - When: Extract the `## Development Methodology` section body (lines between that heading and the next `##` heading) and check for the string "BMAD"
   - Then: "BMAD" appears at least once in the section body

3. **`AC3: Claude attribution sentence present`** — grep check
   - Given: `README.md` after implementation
   - When: `grep -c "Anthropic\|Claude Code\|Claude (Opus\|AI assistance" README.md` (any of these is a signal; the full sentence check uses `grep "AI assistance via Claude"`)
   - Then: `grep "AI assistance via Claude" README.md` returns at least one match

4. **`AC4: CONTRIBUTING.md link present in section`** — grep check
   - Given: `README.md` after implementation
   - When: Extract `## Development Methodology` section body and run `grep -c "CONTRIBUTING.md"` on it
   - Then: output is `>= 1`

5. **`AC5: Section order — after Architecture, before Quick Start`** — awk check
   - Given: `README.md` after implementation
   - When: `awk '/^## /{print NR, $0}' README.md` lists all H2 headings with line numbers
   - Then: line number of `## Development Methodology` > line number of `## Architecture` AND < line number of `## Quick Start`

6. **`AC6: No emojis in section`** — grep check
   - Given: Extract `## Development Methodology` section body from `README.md`
   - When: `grep -P "[\x{1F000}-\x{1FFFF}]|\x{2600}-\x{27BF}" <section-body>`
   - Then: zero matches (grep exits non-zero — no emojis found)

7. **`AC7: markdownlint clean`** — markdownlint (Docker, matching project lint config if present)
   - Given: modified `README.md`
   - When: `docker run --rm -v $(pwd):/work davidanson/markdownlint-cli2:latest "README.md"` (or `npx markdownlint-cli README.md` if available)
   - Then: exit code 0, no new lint errors compared to baseline

**Persistenz-Strategie:** Nicht anwendbar — reine Dokumentationsänderung, keine Application-State. Kein Crash/Restart-Test erforderlich.

---

## Risks & Mitigations

| Risiko | Schwere | Mitigation |
|---|---|---|
| **Falscher Ton** — Attribution klingt wie Marketing oder übertreibt Claude's Rolle | NIEDRIG | Formulierung ist sachlich und beschreibend ("AI assistance via Claude ... through Claude Code"). Kein "revolutionary", kein "powered by AI". Human-reviewer-gate: Maintainer liest den Abschnitt vor Merge. |
| **Unvollständige Attribution** — Modell-Versionen fehlen oder sind falsch | NIEDRIG | Modell-Versionen (Opus 4.6/4.7, Sonnet 4.5/4.6) sind aus der Git-History verifiziert. AC 3 erzwingt den Attributionssatz. |
| **Section-Reihenfolge verschiebt Quick-Start nach hinten** — schlechtere UX für neue Nutzer | NIEDRIG | Die Section ist kurz (3–5 Sätze + Link). Quick Start bleibt das zweite sichtbare Ziel nach Architecture. GitHub's "About" panel ist vom README-Content entkoppelt. |
| **Link auf CONTRIBUTING.md bricht** — Story 8.3 noch nicht implementiert | NIEDRIG | Der Link ist ein Forwärts-Verweis; er ist im README korrekt, auch wenn CONTRIBUTING.md noch nicht existiert. Story 8.3 wird die Datei erstellen. Der Link ist kein Test-Blocker (AC 4 prüft nur die Linkexistenz im Markdown, nicht das Ziel). |
| **markdownlint-Regeln unbekannt** — kein `.markdownlint.json` im Repo | NIEDRIG | Falls kein Config-File vorhanden, läuft markdownlint mit Defaults. Die Section wird mit Standard-Markdown-Konventionen (Blank-Lines um Headings, Trailing-Newline) geschrieben, sodass keine neuen Fehler entstehen. |

---

## Implementation Notes

### Ziel-Section — Entwurf

Die folgende Vorlage ist ein Ausgangspunkt für die Implementierung. Ton und Länge sind angepasst an die sachliche, direkte Sprache des restlichen README (keine Emojis, kein Marketing):

```markdown
## Development Methodology

Nebu is developed using **BMAD** (Brain Model Agile Development), a structured agent-driven pipeline where each story passes through defined gates: Story Creation → Acceptance-Test Scaffold (ATDD) → Implementation → Test Review → Code Review → conditional Security Review. Each gate is executed by a dedicated AI agent role (SM, TEA, Dev, Reviewer), with the human maintainer as the final decision-maker at every merge.

**AI assistance:** This project was developed with AI assistance via Claude (Opus 4.6/4.7, Sonnet 4.5/4.6) through [Claude Code](https://claude.ai/code), Anthropic's CLI. Claude served as the AI backend for all BMAD agent roles. All generated code was reviewed, tested against acceptance criteria, and accepted by the maintainer.

For the full BMAD workflow, coding conventions, and how to contribute using or without the BMAD pipeline, see [CONTRIBUTING.md](CONTRIBUTING.md).
```

**Hinweise zur Vorlage:**
- Der Entwurf erfüllt alle 7 ACs.
- `**BMAD**` und `**AI assistance:**` sind fett für Scannbarkeit — konsistent mit dem Stil anderer README-Sektionen (z.B. `**Apache 2.0**`, `**No federation**`).
- Die Claude-Code-Link-URL kann weggelassen werden, wenn sie als werbend empfunden wird — `Claude Code` als Plaintext genügt für AC 3.
- Die Länge (3 Absätze / ~80 Wörter) hält Quick Start prominent und erreichbar.

### Einfügestelle in README.md

Aktuell endet der `## Architecture`-Block mit:

```
Deep dives: [`docs/architecture/`](docs/architecture/) · ADRs: [`docs/architecture/adr/`](docs/architecture/adr/)

---

## Quick Start
```

Die neue Section wird zwischen die `---`-Trennlinie nach Architecture und dem `## Quick Start`-Heading eingefügt:

```
Deep dives: [`docs/architecture/`](docs/architecture/) · ADRs: [`docs/architecture/adr/`](docs/architecture/adr/)

---

## Development Methodology

[... Section-Text ...]

---

## Quick Start
```

### Vorher-/Nachher-Verifikation

Nach der Implementierung sollte folgendes Kommando die Positionen aller H2-Headings bestätigen:

```bash
awk '/^## /{print NR": "$0}' README.md
```

Erwartete Reihenfolge (Zeilennummern variieren nach Einfügen):
```
...: ## Architecture
...: ## Development Methodology   ← neu, nach Architecture
...: ## Quick Start
...: ## Matrix API Scope
...: ## Tech Stack
...: ## Roadmap
...: ## Contributing
...: ## License
```

---

## Files to Create / Modify

| Datei / Aktion | Beschreibung |
|---|---|
| `README.md` (MODIFY) | Neue Section `## Development Methodology` nach `## Architecture`, vor `## Quick Start` einfügen — 3–5 Sätze + Link auf CONTRIBUTING.md |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | Story `8-2-readme-bmad-claude-attribution-development-methodology` → `done` nach Merge |

**Explizit NICHT Teil dieser Story:**
- Erstellung von `CONTRIBUTING.md` (Story 8.3)
- Erstellung von `SECURITY.md` (Story 8.4)
- Jegliche Änderungen an Quellcode, Migrationen, Routes oder Auth-Logik

---

## Context: Epic 8

Epic 8 überführt das Nebu-Repo von GitLab (privat) nach GitHub (öffentlich, Apache 2.0).

Story 8.2 ist inhaltlich die **Nachfolge-Attribution** zu Story 8.1 (Commit-History-Rewrite): Die entfernten `Co-Authored-By`-Trailer werden durch eine explizite, gut sichtbare README-Section ersetzt, die die Verwendung von Claude sachlich dokumentiert.

Abhängigkeiten:
- **Story 8.1** (Commit-Rewrite) — kein technischer Blocking-Dependency für 8.2, aber logisch zusammengehörend: README-Section und History-Rewrite sind zwei Seiten derselben Entscheidung.
- **Story 8.3** (CONTRIBUTING.md) — README verlinkt auf CONTRIBUTING.md; dieser Link kann im README vorab gesetzt werden, auch wenn CONTRIBUTING.md noch nicht existiert.
- **Story 8.10** (Initial Public Push) — setzt voraus, dass README korrekt und vollständig ist.

---
security_review: required
---

# Story 8.1: Commit-History-Rewrite — Co-Authored-By-Trailer entfernen (Script-Tool)

Status: ready-for-dev

## Story

**As a** maintainer preparing for the public release,
**I want** a reusable, well-tested script that strips all `Co-Authored-By: Claude ...` trailers from the Git history (with backup, dry-run, and verification),
**so that** I can safely execute the rewrite at the right time (after Epic 5 is done on `main`) without manually crafting `filter-repo` invocations or risking metadata loss.

**Scope clarification (2026-04-28):** Diese Story liefert **ausschließlich** das Tooling (Shell-Script + Tests + Runbook). Die tatsächliche Ausführung des Rewrites + Force-Push ist explizit **nicht** Teil dieser Story — sie erfolgt manuell durch den Maintainer kurz vor Story 8.10 (Initial Public Push), wenn `main` keine offenen Epics mehr hat.

**Size:** S

---

## Background

Aktuell enthalten **50 Commits** den Trailer `Co-Authored-By: Claude <noreply@anthropic.com>` (verifiziert via `git log --all --grep="Co-Authored-By" | wc -l` am 2026-04-28). Seit Commit `bba30c9` ("chore(bmad): drop Co-Authored-By trailer from pipeline commit template") wird der Trailer im BMAD-Pipeline-Template nicht mehr gesetzt.

Für einen konsistenten Public Release muss er retroaktiv aus der History entfernt werden. Die Attribution wandert stattdessen in README + CONTRIBUTING.md (Stories 8.2 + 8.3).

**Aktueller HEAD vor Rewrite:** `7180486828aef34a5168563e8614a6b69b4258b0`

**Remote:** `git@gitlab.innoq.com:philippb/nebu-chat.git` (GitLab, privat — wird in Story 8.10 auf GitHub übertragen)

---

## Acceptance Criteria

1. **Script existiert** unter `scripts/rewrite-coauthored-trailer.sh`, ist ausführbar (`chmod +x`) und besteht `shellcheck` ohne Errors (Warnings dokumentiert).

2. **Pre-flight-Checks**: Das Script bricht mit Exit-Code != 0 ab, wenn (a) das Working-Tree nicht clean ist (`git status --porcelain` non-empty), (b) `git filter-repo` nicht im `PATH` ist, oder (c) der Backup-Branch bereits existiert (Schutz vor Doppel-Lauf).

3. **Drei Modi**: `--dry-run` (zeigt Anzahl betroffener Commits, modifiziert nichts), `--run` (führt Backup + Rewrite aus, **ohne** Force-Push), `--verify` (prüft AC 1, 2, 4 aus dem ursprünglichen Plan post-hoc gegen den lokalen State). Ohne Argument: Usage anzeigen + Exit 1.

4. **Backup-Logik**: `--run` erstellt vor dem Rewrite einen Branch `backup/pre-history-rewrite-<YYYYMMDD-HHMMSS>` auf dem aktuellen `HEAD`. Der genaue Branch-Name wird auf `stdout` ausgegeben (für Rollback-Dokumentation).

5. **Trailer-Regex**: Das Script verwendet einen `git filter-repo --message-callback`, der genau die Trailer-Form `\n\nCo-Authored-By: Claude[^\n]*` (inkl. Mehrfach-Vorkommen) entfernt — andere `Co-Authored-By`-Trailer (z.B. menschliche Co-Autoren) bleiben unverändert.

6. **Tests**: `scripts/rewrite-coauthored-trailer.test.sh` erstellt ein temporäres Git-Repo (`mktemp -d`), legt 5 synthetische Commits an (3 mit Claude-Trailer, 1 mit menschlichem Co-Autor, 1 ohne Trailer), führt das Rewrite-Script darauf aus und verifiziert: (a) zero Claude-Trailer übrig, (b) menschlicher Co-Autor-Trailer bleibt erhalten, (c) Commit-Count unverändert, (d) Author/Committer/Timestamp-Metadaten zeichengenau identisch zum Backup-Branch. Die Tests laufen vollständig isoliert in `mktemp` (kein Impact auf das Host-Repo).

7. **Runbook**: `scripts/REWRITE_HISTORY_RUNBOOK.md` dokumentiert den manuellen Ablauf (wann ausführen — nach Epic 5 done, vor Story 8.10; welche Branches betroffen; wie verifizieren; wie force-pushen; wie rollbacken). Das Runbook enthält **keine** automatischen `git push --force`-Schritte — diese werden bewusst manuell durch den Maintainer ausgelöst.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

Alle Tests laufen in einem isolierten `mktemp -d` Sandbox-Git-Repo — kein Impact auf das Host-Repo. Die Tests sind in `scripts/rewrite-coauthored-trailer.test.sh` als Bash-Test-Funktionen mit Exit-Codes implementiert (kein externes Test-Framework).

1. **`test_dry_run_reports_correct_count`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo mit 5 Commits, davon 3 mit `Co-Authored-By: Claude`-Trailer
   - When: `scripts/rewrite-coauthored-trailer.sh --dry-run` läuft im Sandbox-Repo
   - Then: Exit-Code 0, stdout enthält "would rewrite 3 commit(s)"; Repo-State unverändert (HEAD-SHA identisch zu vorher)

2. **`test_run_removes_claude_trailer_only`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo mit 1 Commit "Co-Authored-By: Claude <noreply@anthropic.com>" + 1 Commit "Co-Authored-By: Alice Human <alice@example.com>"
   - When: `scripts/rewrite-coauthored-trailer.sh --run` läuft
   - Then: `git log --grep="Co-Authored-By: Claude" | wc -l` ist `0`; `git log --grep="Co-Authored-By: Alice"` findet den Alice-Commit unverändert

3. **`test_run_creates_backup_branch`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo, HEAD = $ORIG_SHA
   - When: `scripts/rewrite-coauthored-trailer.sh --run`
   - Then: `git rev-parse backup/pre-history-rewrite-<timestamp>` gibt $ORIG_SHA zurück

4. **`test_run_preserves_metadata`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo mit 3 Claude-Trailer-Commits, jeder mit explizit gesetzten `--author`, `--date`, und committer env (`GIT_COMMITTER_*`)
   - When: `scripts/rewrite-coauthored-trailer.sh --run`
   - Then: Für jeden gerewriteten Commit stimmt `git log --format="%an %ae %ai %cn %ce %ci" HEAD~$i` zeichengenau mit dem Backup-Branch-Eintrag überein

5. **`test_pre_flight_aborts_on_dirty_tree`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo mit uncommitted change (`echo foo > untracked.txt`)
   - When: `scripts/rewrite-coauthored-trailer.sh --run`
   - Then: Exit-Code != 0, stderr enthält "working tree not clean"; Repo-State unverändert

6. **`test_verify_mode_passes_after_run`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo nach erfolgreichem `--run`
   - When: `scripts/rewrite-coauthored-trailer.sh --verify`
   - Then: Exit-Code 0, alle 4 Sub-Checks (no-trailer, backup-exists, count-unchanged, metadata-match) reporten "PASS"

7. **`test_pre_flight_aborts_when_filter_repo_missing`** — Bash, isoliertes mktemp-Repo
   - Given: Sandbox-Repo, aber `PATH` enthält kein `git-filter-repo`
   - When: `scripts/rewrite-coauthored-trailer.sh --run` mit `PATH=/usr/bin:/bin`
   - Then: Exit-Code != 0, stderr enthält "git-filter-repo not installed"

**Persistenz-Strategie:** Nicht anwendbar — Shell-Script ohne Application-State. Kein Crash/Restart-Test erforderlich.

---

## Risks & Mitigations

| Risiko | Schwere | Mitigation |
|---|---|---|
| **Irreversibler Datenverlust** — falsche `filter-repo`-Regex löscht Commit-Message-Inhalt über den Trailer hinaus | HOCH | Backup-Branch `backup/pre-history-rewrite` vor jedem Lauf. Dry-Run via `git filter-repo --dry-run` zuerst. Regex exakt auf `\nCo-Authored-By: Claude[^\n]*` beschränken, mit Assertion auf Vorher-/Nachher-Diff. |
| **Lokale Klone anderer Entwickler werden ungültig** — nach Force-Push können bestehende Klone nicht mehr `git pull` (divergente History) | MITTEL | Einzel-Maintainer-Repo (aktuell kein weiterer Contributor). Dokumentation im Completion-Abschnitt: "keine weiteren Klone existieren zum Zeitpunkt des Rewrites". |
| **CI-Pipeline (GitLab) mit alten SHAs referenziert** — `.gitlab-ci.yml`-Regeln auf Branch-SHA-Basis könnten fehlschlagen | NIEDRIG | GitLab CI referenziert Branch-Namen, nicht SHAs. Nach Force-Push läuft die CI normal neu. |
| **`sprint-status*.yaml` enthält alte SHAs** — Audit-Trail-Verlust | NIEDRIG | AC 5 fordert explizite `pre-rewrite-SHA`-Markierungen. Alle betroffenen Felder in den BMAD-Output-Dateien werden kommentiert. |
| **`git filter-repo` nicht installiert** | NIEDRIG | Fallback: `git filter-branch` (langsamer, aber built-in). Bevorzugt `git filter-repo` (pip install git-filter-repo). |

---

## Rollback-Verfahren

Falls der Rewrite fehlerhaft ist (falsche Regex, Datenverlust, Metadaten-Korrumpierung):

```bash
# Schritt 1: Lokaler Reset auf Backup-Branch
git checkout feature/github-readiness
git reset --hard backup/pre-history-rewrite

# Schritt 2: Force-Push zurück zum Remote
git push --force origin feature/github-readiness

# Schritt 3: main ebenfalls zurücksetzen (falls main ebenfalls rewriten wurde)
git checkout main
git reset --hard backup/pre-history-rewrite-main   # falls gesondert erstellt
git push --force origin main

# Schritt 4: Backup-Branch löschen (erst nach Verifikation der Rückkehr)
git branch -d backup/pre-history-rewrite
```

**Zeitfenster für Rollback:** Unbegrenzt, solange `backup/pre-history-rewrite` existiert und GitLab den Force-Push zurück akzeptiert. Der Branch wird erst gelöscht, wenn Story 8.10 (Initial Public Push) abgeschlossen ist und der Public-GitHub-Zustand als korrekt verifiziert wurde.

---

## Implementation Notes

### Empfohlener Ausführungsplan

```bash
# 0. Precondition: keine uncommitted changes, kein Stash
git status  # must be clean

# 1. Backup anlegen (AC 2)
git branch backup/pre-history-rewrite HEAD

# 2. Dry-Run: wie viele Commits werden berührt?
git log --all --grep="Co-Authored-By" | wc -l   # expected: 50

# 3. Rewrite durchführen
# Option A (bevorzugt): git filter-repo
pip install git-filter-repo   # falls nicht installiert
git filter-repo --message-callback '
import re
return re.sub(rb"\n\nCo-Authored-By: Claude[^\n]*(\nCo-Authored-By: Claude[^\n]*)*", b"", message)
'

# Option B (Fallback): git filter-branch
FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch --msg-filter \
  'perl -0pe "s/\n\nCo-Authored-By: Claude[^\n]*//g"' \
  --tag-name-filter cat -- --all

# 4. Verifikation (Acceptance Tests 1–4)
git log --all --grep="Co-Authored-By" | wc -l   # must be 0
git rev-parse backup/pre-history-rewrite          # must be 7180486828aef34a5168563e8614a6b69b4258b0
git rev-list --count HEAD                          # must match backup count

# 5. Force-Push (AC 4)
git push --force origin feature/github-readiness
git push --force origin main

# 6. Remote-Verifikation (Acceptance Test 5)
git ls-remote origin HEAD
```

### Regex-Begründung

Der Trailer erscheint in zwei Varianten:
- `\n\nCo-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`
- `\n\nCo-Authored-By: Claude <noreply@anthropic.com>`

Der Regex `\n\nCo-Authored-By: Claude[^\n]*` erfasst beide und entfernt die gesamte Trailer-Sektion (einschließlich vorangehendem Doppel-Newline) ohne andere Commit-Message-Inhalte zu berühren.

**Achtung:** Falls mehrere `Co-Authored-By`-Zeilen in einem Commit existieren (unwahrscheinlich, aber möglich), wird nur der Claude-spezifische Trailer entfernt. Menschliche `Co-Authored-By`-Trailer (falls vorhanden) bleiben erhalten — der Regex ist explizit auf `Co-Authored-By: Claude` beschränkt.

### SHA-Referenzen in BMAD-Output-Dateien

Nach dem Rewrite enthalten alle `Dev Agent Record > Completion Notes`-Abschnitte in Story-Dateien ggf. alte SHAs. Diese müssen **nicht** automatisch aktualisiert werden — die `pre-rewrite-SHA`-Notation ist ausreichend (AC 5). Ein globaler `grep -r "7180486\|aa1cc89"` über `_bmad-output/` zeigt alle betroffenen Stellen.

---

## Files to Create / Modify

| Datei / Aktion | Beschreibung |
|---|---|
| `scripts/rewrite-coauthored-trailer.sh` (NEU) | Bash-Script: `--dry-run` / `--run` / `--verify` Modi mit Pre-flight, Backup, `git filter-repo`-Aufruf |
| `scripts/rewrite-coauthored-trailer.test.sh` (NEU) | Bash-Tests in isoliertem mktemp-Sandbox-Repo (7 Test-Funktionen, AC 6) |
| `scripts/REWRITE_HISTORY_RUNBOOK.md` (NEU) | Manuelles Runbook: Wann ausführen, welche Branches, Force-Push-Anleitung, Rollback (AC 7) |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | Story 8-1 → `done`, Kommentar mit Bemerkung "Tooling delivered, Rewrite-Execution noch ausstehend (manuell, nach Epic 5 done)" |

**Explizit NICHT Teil dieser Story:**
- Tatsächlicher Rewrite-Lauf gegen das Host-Repo
- Force-Push zu GitLab/GitHub
- Aktualisierung von SHA-Referenzen in `_bmad-output/`-Dateien
- Backup-Branch im Host-Repo

Diese Aktionen erfolgen manuell durch den Maintainer in einer dedizierten Session, sobald Epic 5 auf `main` abgeschlossen ist und unmittelbar bevor Story 8.10 (Initial Public Push) startet.

---

## Context: Epic 8

Epic 8 überführt das Nebu-Repo von GitLab (privat) nach GitHub (öffentlich, Apache 2.0).

Story 8.1 ist die **erste Story** und eine **Prerequisite für Story 8.10** (Initial Public Push): Der Force-Push nach GitHub darf erst erfolgen, wenn die History sauber ist.

Nachfolgende Stories, die von 8.1 abhängen:
- **Story 8.2** (README Attribution) — die README-Section ersetzt inhaltlich die entfernten Trailer
- **Story 8.3** (CONTRIBUTING.md) — dokumentiert, dass neue Commits keinen Co-Authored-By-Trailer enthalten
- **Story 8.5** (Secret Scan Gate) — läuft gegen die rewritte History
- **Story 8.10** (Initial Public Push) — erst möglich nach sauberer History (8.1) + Secret Scan (8.5) + Release Gate (8.9)

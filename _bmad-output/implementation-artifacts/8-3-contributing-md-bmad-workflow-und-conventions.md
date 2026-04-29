---
security_review: not-needed
---

# Story 8.3: CONTRIBUTING.md — BMAD-Workflow und Conventions dokumentiert

Status: ready-for-dev

## Story

**As a** potential external contributor,
**I want** a CONTRIBUTING.md that explains the BMAD story pipeline, code conventions, and how to submit PRs,
**so that** I can contribute effectively without reverse-engineering the process from the codebase.

**Size:** S

---

## Background

Story 8.1 removed all `Co-Authored-By: Claude ...` trailers from the Git history. Story 8.2 added a "Development Methodology" section to the README that briefly explains BMAD and links to this file. Story 8.3 delivers the full contributor guide that both stories reference.

**Current state of CONTRIBUTING.md:** A `CONTRIBUTING.md` already exists in the repository root. It covers basic development setup, coding conventions (Go, Elixir, SQL, commit message format), and a PR process. However, it is missing: explicit section headings that the verify script checks for (`## Reporting Bugs`, `## Submitting a Pull Request`, `## Development Workflow (BMAD)`, `## Testing`, `## License and DCO`), the BMAD pipeline explanation with escape hatch, the CLAUDE.md reference, the no-Co-Authored-By rule, and the DCO/license statement. This story expands the existing file to add these missing elements.

**What this file must communicate to an external reader:**

1. **BMAD context** — Internal maintainers run a structured agent pipeline (SM → TEA → Dev → Reviewer → Security). External contributors are NOT required to participate in this pipeline. A minimal, direct PR path exists.

2. **No Co-Authored-By trailers** — Since Story 8.1 cleaned the history, new commits MUST NOT include `Co-Authored-By: Claude ...` trailers. This applies to maintainers writing commit messages via Claude Code as well.

3. **Code conventions** — Contributors should know what style the codebase expects (Go, Elixir, OpenAPI-first). CLAUDE.md already documents these; CONTRIBUTING.md links to it for maintainers and summarises the key constraints for contributors.

4. **DCO / License** — Apache 2.0 (Section 5) covers inbound contributions without a separate CLA. A DCO sign-off (`git commit -s`) is recommended for audit-trail purposes.

5. **Bug reports and PRs** — Issue template links (Story 8.7 will add GitHub templates) and the PR workflow.

**Dependencies:**
- Story 8.1 (History Rewrite) — Contributors must know about the no-Co-Authored-By rule.
- Story 8.2 (README Attribution) — README already links to this file; the link must resolve after this story.
- Story 8.7 (GitHub Issue/PR Templates) — CONTRIBUTING.md can reference the templates section; at time of story 8.3 implementation, templates may not yet exist — forward-reference is acceptable.

---

## Acceptance Criteria

1. **File exists**: `CONTRIBUTING.md` exists in the repository root.

2. **Bug Report section**: `CONTRIBUTING.md` contains a section heading `## Reporting Bugs` (or `## Bug Reports`) that describes how to open a bug report (at minimum: where to open it and what information to include).

3. **PR workflow section**: `CONTRIBUTING.md` contains a section `## Submitting a Pull Request` (or `## Pull Requests`) that covers:
   - Branch naming convention (`feat/<topic>`, `fix/<topic>`, or equivalent)
   - Commit message format (Conventional Commits, **without** `Co-Authored-By:` trailer)
   - Review expectations (at least one sentence)

4. **BMAD explanation with escape hatch**: `CONTRIBUTING.md` contains a section `## Development Workflow (BMAD)` (or `## BMAD Workflow`) that explains the internal gate sequence and explicitly states that external contributors may submit PRs directly without running the BMAD pipeline.

5. **Code conventions reference**: `CONTRIBUTING.md` references `CLAUDE.md` as the authoritative source for Go and Elixir conventions, and mentions the OpenAPI-first approach for gateway API changes.

6. **Test expectations**: `CONTRIBUTING.md` contains a section `## Testing` (or `## Test Expectations`) that states what tests are expected (unit / integration / E2E) and that the TDD standard (red-green-refactor) is mandatory for maintainers and strongly recommended for contributors.

7. **DCO / License statement**: `CONTRIBUTING.md` contains a section `## License and DCO` (or `## Licensing`) that states Apache 2.0 as the inbound license (no separate CLA) and recommends `git commit -s` for DCO sign-off.

8. **No Co-Authored-By note**: `CONTRIBUTING.md` explicitly states that `Co-Authored-By: Claude` trailers MUST NOT be added to commits (relevant to all contributors who may use Claude Code or similar tools).

9. **No emojis**: `CONTRIBUTING.md` contains no emoji characters.

10. **Markdown lint clean**: `CONTRIBUTING.md` passes `markdownlint` with the project's existing rules (no lint errors beyond pre-existing baseline).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

These are static content checks — no runtime, no compilation. Each test is a `grep`/`awk` assertion against `CONTRIBUTING.md` run via `scripts/verify-contributing.sh`.

1. **`AC1: File exists`** — filesystem check
   - Given: Repository root after implementation
   - When: `test -f CONTRIBUTING.md`
   - Then: exit code 0

2. **`AC2: Bug report section heading present`** — grep check
   - Given: `CONTRIBUTING.md` after implementation
   - When: `grep -cE "^## (Reporting Bugs|Bug Reports)$" CONTRIBUTING.md`
   - Then: output is `>= 1` (exactly one occurrence)

3. **`AC3: PR workflow section with required sub-elements`** — grep check (three sub-checks)
   - Given: Extract the PR workflow section body (between `## Submitting a Pull Request` or `## Pull Requests` heading and next `##`)
   - When (3a): Section body contains a branch naming pattern (e.g. `feat/`, `fix/`)
   - Then (3a): `grep -E "feat/|fix/" <section-body>` returns at least one match
   - When (3b): Section body contains "Conventional Commits" or "conventional commits"
   - Then (3b): at least one match
   - When (3c): Section body does NOT require `Co-Authored-By`; instead it explicitly forbids or omits it — check that "Co-Authored-By" appears nowhere mandatory (the global AC8 check covers the explicit prohibition)

4. **`AC4: BMAD section with escape hatch`** — grep checks
   - Given: Extract the BMAD/Development Workflow section body
   - When (4a): `grep -c "BMAD" <section-body>` — section mentions "BMAD"
   - Then (4a): `>= 1` match
   - When (4b): Section body contains language indicating external contributors can skip BMAD (e.g. "without", "directly", "skip", "not required")
   - Then (4b): at least one of those terms present

5. **`AC5: CLAUDE.md reference present`** — grep check
   - Given: `CONTRIBUTING.md` after implementation
   - When: `grep -c "CLAUDE.md" CONTRIBUTING.md`
   - Then: `>= 1` match

6. **`AC6: Testing section with TDD keyword`** — grep checks
   - Given: Extract `## Testing` (or `## Test Expectations`) section body
   - When (6a): Section body contains "unit" (case-insensitive)
   - Then (6a): at least one match
   - When (6b): Section body contains "TDD" or "red-green" or "test-first"
   - Then (6b): at least one match

7. **`AC7: License/DCO section with Apache and DCO keywords`** — grep checks
   - Given: Extract `## License` or `## Licensing` or `## License and DCO` section body
   - When (7a): Section body contains "Apache"
   - Then (7a): at least one match
   - When (7b): Section body contains "DCO" or `git commit -s`
   - Then (7b): at least one match

8. **`AC8: No-Co-Authored-By rule stated`** — grep check
   - Given: `CONTRIBUTING.md` after implementation
   - When: `grep -c "Co-Authored-By" CONTRIBUTING.md`
   - Then: `>= 1` — the trailer name is mentioned (i.e., the prohibition is written down)

9. **`AC9: No emojis`** — Python3 Unicode scan (same approach as Story 8.2 AC6)
   - Given: Full `CONTRIBUTING.md` content
   - When: Python3 scans for Unicode emoji ranges (U+1F000–U+1FFFF, U+2600–U+27BF)
   - Then: zero matches

10. **`AC10: markdownlint clean`** — markdownlint check
    - Given: `CONTRIBUTING.md` after implementation (new file, no baseline — error count must be 0)
    - When: `npx markdownlint-cli CONTRIBUTING.md` or Docker equivalent
    - Then: exit code 0, zero errors

**Persistenz-Strategie:** Nicht anwendbar — reine Dokumentationsänderung, kein Application-State. Kein Crash/Restart-Test erforderlich.

---

## Risks & Mitigations

| Risiko | Schwere | Mitigation |
|---|---|---|
| **Falscher Ton** — BMAD-Erklärung klingt exklusiv oder abschreckend | NIEDRIG | Escape-Hatch-Formulierung explizit in AC4. Text beschreibt BMAD als internen Workflow, betont dass externe PRs willkommen sind. |
| **CLAUDE.md-Referenz zu intern** — externe Contributors haben kein CLAUDE.md im Repo-Kontext | NIEDRIG | CLAUDE.md ist im Repo checked in (`git show HEAD:CLAUDE.md`). Die Datei ist Teil des öffentlichen Repos. Verlinkung ist korrekt. |
| **Story 8.7-Templates nicht vorhanden** — CONTRIBUTING.md referenziert Templates, die noch nicht existieren | NIEDRIG | Forward-Reference ist akzeptabel (Formulierung: "Issue templates in `.github/ISSUE_TEMPLATE/` — see Story 8.7"). Kein Test-Blocker. |
| **DCO-Empfehlung erzeugt Verwirrung mit CLA** | NIEDRIG | Abschnitt erklärt explizit: Apache 2.0 Section 5 = kein separates CLA nötig. DCO ist empfohlen, nicht erzwungen. |
| **Emojis durch Copypaste** | NIEDRIG | AC9 + verify-contributing.sh fangen das ab. |

---

## Implementation Notes

### Ziel-Outline `CONTRIBUTING.md`

```
# Contributing to Nebu

Brief welcome paragraph. Apache 2.0. PRs welcome from everyone.

## Reporting Bugs

- Open a GitHub Issue.
- Include: steps to reproduce, expected vs actual behaviour, Nebu version/commit SHA.
- Security issues: see SECURITY.md (do NOT open a public issue for vulnerabilities).
- (Forward reference: issue templates in .github/ISSUE_TEMPLATE/ simplify this — Story 8.7)

## Submitting a Pull Request

### Branch naming

  feat/<topic>   — new feature or capability
  fix/<topic>    — bug fix
  docs/<topic>   — documentation only
  chore/<topic>  — maintenance, tooling

### Commit messages

Conventional Commits style: <type>(<scope>): <summary>

Examples:
  feat(gateway): add rate-limit middleware
  fix(core): prevent duplicate session registration
  docs: update CONTRIBUTING.md

IMPORTANT: Do NOT include `Co-Authored-By: Claude ...` or similar AI-attribution
trailers in commit messages. Attribution is handled at the project level in README.md
and CONTRIBUTING.md. Adding these trailers was a pre-2026-04-23 practice that has
been removed from the history (see Story 8.1).

### Review expectations

PRs will receive a code review from a maintainer. For non-trivial changes, expect:
- Feedback on test coverage (unit + integration)
- Feedback on API/interface design
- A security pass for auth/middleware/SQL changes

Allow ~5 business days for initial review.

## Development Workflow (BMAD)

Internal maintainers use the [BMad Method](https://docs.bmad-method.org/)
(BMAD — *Build More Architect Dreams*) pipeline: Story Creation →
Acceptance-Test Scaffold (ATDD) → Implementation → Test Review → Code Review →
conditional Security Review.

**External contributors are NOT required to run the BMAD pipeline.** You may
submit a PR directly against `main` without creating a story file or running
any BMAD agent. Maintainers will integrate your change into the pipeline if it
is accepted.

If you want to understand the full pipeline, see CLAUDE.md (checked into the repo)
and the `_bmad-output/` directory for example story files.

## Code Conventions

For authoritative Go and Elixir conventions, see `CLAUDE.md` in the repository
root (checked into git). Key rules for contributors:

**Go:**
- Errors: explicit handling, no panic in library code
- Context: always pass as first parameter
- Interfaces: small, defined by the consumer

**Elixir:**
- GenServer state: always via handle_* callbacks, never direct
- Errors: let it crash + Supervisor, no defensive try/rescue
- Ecto: Changesets for all validations

**API changes (Go Gateway):**
Nebu follows an OpenAPI-first approach. New or modified HTTP endpoints require
changes to `openapi.yaml` first, followed by `make gen-api` to regenerate the
handler stubs.

## Testing

**What is expected:**

- **Unit tests** for all new functions/modules (Go: `_test.go`, Elixir: ExUnit)
- **Integration tests** for new HTTP endpoints (Godog / `net/http` level)
- **E2E tests** for Admin UI changes (Playwright against the running stack)

**TDD standard (mandatory for maintainers, strongly recommended for contributors):**

Write the failing test *before* writing the implementation. The red-green-refactor
cycle is the project standard, not a suggestion. PRs that add implementation without
tests will be asked to add tests before merge.

Run tests locally:
  make test-unit-go       — Go unit tests
  make test-unit-elixir   — Elixir unit tests
  make test-integration   — full stack + Godog Gherkin tests (requires Docker)

## License and DCO

Nebu is licensed under the **Apache License 2.0**. By submitting a pull request,
you agree (per Apache 2.0, Section 5) that your contribution is licensed under
the same terms. No separate Contributor License Agreement (CLA) is required.

**DCO sign-off (recommended):** We recommend signing your commits with a
Developer Certificate of Origin sign-off for audit-trail purposes:

  git commit -s -m "feat(gateway): your change"

This adds a `Signed-off-by: Your Name <email>` trailer. It is not enforced by
CI, but appreciated.

## Code of Conduct

We expect respectful, constructive communication in all project spaces (issues,
PRs, discussions). There is currently no formal CODE_OF_CONDUCT.md file in this
repository. The short version: be kind, be direct, focus on the work.
```

### Hinweise zur Outline

- Die Outline ist vollständig und kann 1:1 als Ausgangspunkt implementiert werden. Stilanpassungen sind erlaubt; alle 10 ACs müssen erfüllt bleiben.
- Kein Emoji in der ganzen Datei (AC9 + verify-script).
- Keine Inline-HTML (markdownlint-kompatibel).
- Blank lines around headings (markdownlint MD022).
- Code blocks brauchen Sprach-Hint oder leer (MD040 — `bash`, `text`, kein Hint = markdownlint-Fehler). Prüfe dies vor dem Commit.

### verify-contributing.sh

Das Script `scripts/verify-contributing.sh` folgt dem Muster von `scripts/verify-readme-attribution.sh` (Story 8.2):

- Pure Bash, kein externe Dependencies (außer python3 für Emoji-Check)
- `run_test` helper, `extract_section` helper
- Exit 0 wenn alle ACs pass, Exit 1 bei einem Fail
- Ausgabe: `PASS` / `FAIL` per AC, abschließende Zusammenfassung

---

## Files to Create / Modify

| Datei / Aktion | Beschreibung |
|---|---|
| `CONTRIBUTING.md` (MODIFY) | Datei existiert bereits im Repo-Root mit grundlegender Struktur. Fehlende Sections hinzufügen: Reporting Bugs, Submitting a Pull Request, Development Workflow (BMAD), Testing, License and DCO, sowie die No-Co-Authored-By-Regel und CLAUDE.md-Referenz — alle 10 ACs erfüllt |
| `scripts/verify-contributing.sh` (CREATE) | Acceptance-Test-Script für Story 8.3 ACs — analog zu `scripts/verify-readme-attribution.sh` |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | Story `8-3-contributing-md-bmad-workflow-und-conventions` → `done` nach Merge |

**Explizit NICHT Teil dieser Story:**
- Änderungen an `README.md` (Link existiert bereits seit Story 8.2)
- Erstellung von `SECURITY.md` (Story 8.4)
- Erstellung von GitHub Issue/PR-Templates (Story 8.7)
- Erstellung von `CODE_OF_CONDUCT.md` — CONTRIBUTING.md enthält nur einen Pointer/Statement
- Jegliche Änderungen an Quellcode, Migrationen, Routes oder Auth-Logik

---

## Context: Epic 8

Epic 8 überführt das Nebu-Repo von GitLab (privat) nach GitHub (öffentlich, Apache 2.0).

Story 8.3 liefert die `CONTRIBUTING.md`, die sowohl von Story 8.2 (README-Link) als auch implizit von Story 8.1 (Co-Authored-By-Regel) referenziert wird. Mit dieser Story ist der Dokumentations-Grundstock (README + CONTRIBUTING) für den Public Release vollständig.

Abhängigkeiten:
- **Story 8.1** (Commit-Rewrite) — logisch: CONTRIBUTING.md erklärt die Regel, die 8.1 retroaktiv durchgesetzt hat.
- **Story 8.2** (README Attribution) — technisch: der Link `[CONTRIBUTING.md](CONTRIBUTING.md)` in README existiert bereits; diese Story lässt ihn auflösen.
- **Story 8.7** (GitHub Issue/PR-Templates) — CONTRIBUTING.md enthält einen Forward-Pointer, kein Blocking-Dependency.
- **Story 8.10** (Initial Public Push) — setzt voraus, dass CONTRIBUTING.md korrekt und vollständig ist.

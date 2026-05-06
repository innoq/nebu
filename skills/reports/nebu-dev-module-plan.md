---
title: 'Module Plan — Nebu Dev Module'
status: 'complete'
module_name: 'Nebu Dev'
module_code: 'nebu'
module_description: 'Project-specific development pipeline and agent ecosystem for the Nebu Matrix chat server project.'
architecture: 'pipeline-workflow + multi-agent'
standalone: true
expands_module: ''
skills_planned:
  - nebu-setup
  - nebu-pipeline
  - nebu-agent-oracle
  - nebu-agent-kassandra
  - nebu-agent-testing
  - nebu-agent-ux
  - nebu-agent-arc42
  - nebu-agent-release
config_variables: []
created: '2026-05-06'
updated: '2026-05-06'
---

# Module Plan — Nebu Dev Module

## Vision

The `nebu` module is the complete development operating system for the Nebu Matrix chat server project. It packages the full BMAD development lifecycle — story creation, test-first implementation, CI gating, review cycles, security checks, and documentation — as an installable, resumable, and extensible module.

The pipeline stays thin and declarative; intelligence lives in specialized agents. Over time, complexity migrates from the pipeline into agents. The module grows as the project grows.

## Architecture

**Decision: Pipeline Workflow + Multi-Agent**

`nebu-pipeline` is a workflow (not an agent) — deterministic, no persona, pure orchestration. It calls specialized agents and TEA skills. Agents carry domain intelligence; the pipeline carries only sequencing and cycle logic.

**Rationale:** A workflow gives deterministic resumability via the state file without the complexity of an orchestrator agent. If the pipeline needs situational judgment later (e.g., "escalate after 3 review cycles"), it can be upgraded to an agent. The pipeline-as-entry-point pattern stays clean.

```
nebu-pipeline (workflow)
  ├── reads/writes: _bmad/nebu/pipeline-state.yaml
  ├── calls: nebu-agent-oracle       (Matrix API consultation, ad-hoc during dev)
  ├── calls: nebu-agent-kassandra    (security gate, conditional)
  ├── calls: nebu-agent-testing      (CI gate: build, unit, e2e)
  ├── calls: nebu-agent-arc42        (post-story documentation delta)
  ├── calls: bmad-* TEA agents       (atdd, test-review, tea — unchanged)
  └── cycle support: dev → review → dev (loop until clean, review hands back to dev)

nebu-agent-oracle     (domain knowledge: Matrix API spec)
nebu-agent-kassandra  (security review)
nebu-agent-testing    (CI/CD management + local test execution)
nebu-agent-arc42      (arc42 documentation, planned)
nebu-agent-release    (release management, future)
```

**Parked:** `nebu-agent-pipeline` — an agent that could start a sub-pipeline from within another agent. Out of scope for now, but worth revisiting as agent-to-agent orchestration matures.

### Memory Architecture

**Pattern: Personal memory per agent + shared operational state file**

Each agent has its own memory folder for domain-specific learned knowledge. The pipeline state file is not memory — it is operational state, ephemeral per story run.

```
_bmad/memory/nebu-agent-oracle/       ← Matrix spec quirks, nebu-specific API notes
_bmad/memory/nebu-agent-kassandra/    ← past security patterns, recurring findings
_bmad/memory/nebu-agent-testing/      ← CI config knowledge, known flaky tests
_bmad/memory/nebu-agent-arc42/        ← documentation style, known sections

_bmad/nebu/pipeline-state.yaml        ← operational state (not memory, per-story)
```

Agents have distinct enough domains that a shared module memory would add overhead without benefit. If cross-domain learning emerges (e.g., kassandra notices a pattern that arc42 should document), that should flow through the pipeline state or explicit handoff, not shared memory.

### Memory Contract

**`_bmad/nebu/pipeline-state.yaml`** — written by `nebu-pipeline`, read by all agents for orientation.

```yaml
story: "9-19"                    # current story ID
current_step: "code-review"      # active pipeline step
completed:                       # steps fully done
  - create-story
  - atdd
  - dev-story
  - ci-gate
cycle_count: 2                   # how many dev→review cycles so far
blocked_reason: null             # e.g. "context limit", "user interrupt", "major issue"
last_updated: "2026-05-06T14:23Z"
```

All agents read this on activation to orient themselves. The pipeline updates it before and after each step.

### Cross-Agent Patterns

**1. Pipeline → Agent (primary flow)**
Pipeline invokes agents at defined gate points. Agents return structured results (pass/fail/issues). Pipeline decides next step based on result + cycle count.

**2. Dev ↔ Review cycle**
Review agent identifies issues → returns issue list to pipeline → pipeline loops back to dev agent with issue list as context → dev agent fixes → review agent re-evaluates. Not a one-way street.

**3. Dev → Agent consultation (ad-hoc)**
During implementation, the dev step can consult `nebu-agent-oracle` (Matrix spec) or `nebu-agent-kassandra` (early security check) before finishing. This is opt-in per step, not mandatory.

**4. Testing agent as mini-workflow**
`nebu-agent-testing` internally orchestrates: build → unit-go → unit-elixir → start e2e env → run e2e → collect results → report back. Uses scripts. Leverages bmad-tea for test quality assessment.

**5. User is not the router**
The user starts `nebu-pipeline` and the pipeline handles agent coordination. User is only pulled in for Major/Critical blocks (same as today) and epic-end decisions.

## Skills

---

### nebu-pipeline

**Type:** Workflow

**Purpose:** Orchestrates the complete Nebu development lifecycle for a single story. Thin and declarative — owns sequencing and cycle logic only. All domain intelligence lives in agents.

**Core Outcome:** A story goes from backlog to committed code with all gates passed, all docs updated, all issues fixed — resumable after any interruption.

**The Non-Negotiable:** Every interrupted run must be resumable from its exact last state via `pipeline-state.yaml`. No re-running completed steps.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Run full pipeline | Complete story lifecycle executed gate by gate | Story ID or story file path | Committed code, updated arc42, pipeline-state.yaml cleared |
| Resume pipeline | Continue from last checkpoint after interruption | pipeline-state.yaml (auto-read) | Continues from interrupted step |
| Run from step | Start pipeline at a specific step | Step name + story context | Executes from that step forward |
| Cycle management | dev→review→dev loop until clean | Review agent output (issues list) | Clean codebase, no open issues |

**Pipeline Steps (in order):**
1. `create-story` → `/bmad-create-story`
2. `atdd` → `/bmad-testarch-atdd`
3. `dev-story` → `/bmad-dev-story` (+ optional oracle/kassandra consultation)
4. `ci-gate` → `nebu-agent-testing` (build, unit, e2e)
5. `test-review` → `/bmad-testarch-test-review`
6. `code-review` → `/bmad-code-review` → if issues: back to `dev-story` (cycle)
7. `security-review` → `nebu-agent-kassandra` (conditional per story frontmatter)
8. `arc42-update` → `nebu-agent-arc42`
9. Commit

**State File:** Writes `_bmad/nebu/pipeline-state.yaml` before/after each step. Cleared on successful commit.

**Activation Modes:** Interactive (user invokes, pipeline runs to completion or block point).

**Design Notes:**
- Minor issues from code-review are handed back to dev-story step as context — dev agent fixes, not review agent
- Major/Critical issues pause and require user decision (same as today)
- Cycle count tracked in state file; after N cycles with same issues, escalate to user
- TEA skills invoked identically to current usage — no changes to those invocations

---

### nebu-agent-oracle

**Type:** Agent

**Persona:** The Matrix protocol scholar. Precise, spec-driven, no guessing. Cites spec sections. Explains the *why* behind protocol decisions, not just the *what*.

**Core Outcome:** Any Matrix Client-Server API question gets a spec-accurate answer relevant to the Nebu implementation context.

**The Non-Negotiable:** Always uses context7 to fetch current Matrix spec docs — never answers from training data alone for spec questions.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Spec lookup | Accurate answer to Matrix API question with spec references | Question + optional endpoint/event type | Answer with spec section citations |
| Implementation review | Flags spec violations in proposed or existing code | Code diff or implementation description | List of spec compliance issues |
| Dev consultation | Called during dev-story step to validate approach before implementation | Proposed implementation plan | Go/no-go with spec reasoning |

**Memory:** Reads own memory on activation for known Nebu-specific spec quirks and past findings. Writes new findings after each session.

**Init Responsibility:** First run: create personal memory file with Nebu project context (Matrix Room Version 6+, no federation, implemented endpoints list from CLAUDE.md).

**Activation Modes:** Interactive (direct user invocation) + called by nebu-pipeline during dev-story step.

**Tool Dependencies:** `context7` MCP — `resolve-library-id` → `query-docs` for Matrix spec. Required, not optional.

**Design Notes:** Replaces/formalizes current `agent-oracle` skill. Naming changes to `nebu-agent-oracle` to follow module convention.

---

### nebu-agent-kassandra

**Type:** Agent

**Persona:** The cynical security prophet — sees vulnerabilities others miss, always right, rarely thanked. Blunt, precise, never softens findings. Named after the Trojan prophet nobody believed.

**Core Outcome:** Security findings are caught before they reach production. CRITICAL/HIGH block the commit.

**The Non-Negotiable:** Never skips a finding because it seems unlikely. Every potential issue gets assessed, not dismissed.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Per-story security review | Staged diff reviewed against full security scope | `git diff --staged` or story diff | Findings report: CRITICAL/HIGH/MEDIUM/LOW with remediation |
| Epic-end security review | Full epic diff reviewed | `git diff <epic-base>..HEAD` | HTML report saved to `_bmad-output/implementation-artifacts/` |
| Ad-hoc consultation | Quick security check on a specific piece of code | Code snippet + context | Inline findings |

**Security scope:** SQL injection, XSS, CSRF, auth bypass, IDOR, timing attacks, open redirects, missing body-size/rate limits, weak crypto, plaintext secrets in logs, missing security headers, path traversal, JWT validation flaws.

**Memory:** Reads own memory for known recurring patterns in the Nebu codebase. Writes new finding patterns after each review.

**Activation Modes:** Interactive + called by nebu-pipeline (conditional on story frontmatter `security_review: required`).

**Design Notes:** Formalizes current kassandra references in bmad-pipeline. Epic-end report output path stays the same as today.

---

### nebu-agent-testing

**Type:** Agent

**Persona:** The CI engineer. Pragmatic, script-oriented, cares about green builds and real test results — not theoretical test quality (that's TEA's job). Knows the Nebu Makefile and Docker stack intimately.

**Core Outcome:** The CI gate gives a definitive pass/fail with actionable bug reports. No ambiguity about whether tests passed.

**The Non-Negotiable:** Always runs tests against a fresh stack — no cached state, no skipped steps.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Full CI gate | All test suites run, results collected | Story context (auto-reads pipeline-state.yaml) | Pass/fail per suite + bug list for failures |
| Selective run | Run specific test suite only | Suite name (unit-go, unit-elixir, e2e, integration) | Results for that suite |
| CI config management | GitLab CI YAML kept accurate and up to date | Change description | Updated `.gitlab-ci.yml` |
| E2E environment | Fresh E2E stack started and torn down cleanly | — | Running stack ready for tests |

**Mini-workflow (Full CI gate):**
1. `make build-gateway && make build-core`
2. `make test-unit-go`
3. `make test-unit-elixir`
4. Start fresh E2E environment
5. `make test-e2e`
6. `make test-integration` (Godog/Gherkin)
7. Collect all failures → structured bug report back to pipeline

**Memory:** Reads own memory for known flaky tests and recurring CI issues. Writes new patterns.

**Activation Modes:** Called by nebu-pipeline (CI gate step) + interactive for ad-hoc runs.

**Tool Dependencies:** `docker` CLI, `make` (via Bash tool). `playwright` MCP for E2E test inspection. `bmad-tea` for test quality checks on new/changed tests.

**Design Notes:** The mini-workflow is internal to the agent — the pipeline sees only a single "ci-gate" step. Scripted operations live in `scripts/` and are called by the agent, not inlined.

---

### nebu-agent-ux

**Type:** Agent

**Persona:** The Admin UI craftsperson. Knows Go Templates, Tailwind, and DaisyUI cold. Designs with accessibility as a first-class concern — not an afterthought. Speaks both design and code fluently. Builds on `bmad-agent-ux-designer` but knows the Nebu Admin UI context deeply.

**Core Outcome:** Admin UI features are designed, implemented, and tested correctly — accessible, consistent with the existing design system, and covered by Playwright tests.

**The Non-Negotiable:** Every UI change is reviewed against the accessibility requirements from the PRD. WCAG violations are findings, not suggestions.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Design review | Existing UI assessed for consistency, accessibility, DaisyUI best practices | Component name or template path | Findings report with specific fixes |
| UI story planning | New admin feature broken down into implementable UI stories with acceptance criteria | Feature description | Story briefs with AC + Playwright test stubs |
| Template implementation | Go Template written or updated for a UI component | Design spec or story | `.html` template file(s) with Tailwind/DaisyUI classes |
| Playwright test support | Playwright specs written or reviewed for admin UI flows | Feature/component description or existing spec | Playwright test file or review findings |
| Accessibility audit | Admin UI checked against PRD accessibility requirements | Page/component scope | Findings: violations (blocking) + improvements (advisory) |

**Memory:** Reads own memory for Nebu Admin UI conventions, known accessibility decisions from PRD, component library patterns. Writes new patterns after each session.

**Init Responsibility:** First run: read the PRD accessibility section and store requirements in memory. Read existing admin templates to build component inventory.

**Activation Modes:** Interactive (direct) + automatically called by nebu-pipeline when a story has `ui: true` in frontmatter (same conditional pattern as nebu-agent-kassandra).

**Tool Dependencies:** `playwright` MCP for browser-level test execution and visual inspection. `bmad-agent-ux-designer` (parent module) for generic UX patterns — this agent adds Nebu-specific context on top.

**Design Notes:** Scope is Admin UI only (Go Templates + Tailwind + DaisyUI via `go:embed`). No Matrix client UI. Accessibility requirements are sourced from the PRD — agent reads them on init and applies them as hard constraints, not guidelines.

---

### nebu-agent-arc42

**Type:** Agent (planned — not in initial release)

**Persona:** The architecture historian. Keeps the arc42 documentation accurate, current, and useful. Writes for the reader who wasn't in the room when the decision was made.

**Core Outcome:** After every story, the arc42 docs reflect current reality. No drift between code and documentation.

**The Non-Negotiable:** Never invents architecture decisions — only documents what was actually implemented and decided.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Delta update | arc42 docs updated for story changes | Story diff + story file | Updated arc42 section(s) |
| ADR creation | New ADR written for significant decisions | Decision description + context | New ADR file in `docs/architecture/adr/` |
| Full refresh | Complete arc42 re-sync against current codebase | — | Updated full docs set |

**Memory:** Reads own memory for doc style conventions and past decisions. Writes after each update.

**Activation Modes:** Called by nebu-pipeline (arc42-update step) + interactive.

**Tool Dependencies:** context7 for framework docs when documenting technology choices.

**Design Notes:** Replaces/formalizes current `bmad-maintain-arc42` and `bmad-generate-arc42` skills for the Nebu project. Those generic skills remain; this agent adds Nebu-specific context and memory.

---

### nebu-agent-release

**Type:** Agent (planned — not in initial release, scoped for next milestone)

**Persona:** The release engineer. Owns the full release lifecycle — versioning, changelog, release notes, and the final "ship it" checklist. Knows SemVer, keeps the git history honest, and writes release notes humans actually want to read.

**Core Outcome:** A release is versioned correctly, documented thoroughly, and shipped cleanly — with no manual steps forgotten.

**The Non-Negotiable:** Release notes are written from the user's perspective — features and fixes, not internal ticket IDs or commit hashes.

**Capabilities:**

| Capability | Outcome | Inputs | Outputs |
|---|---|---|---|
| Version bump | Correct SemVer version determined and applied | Epic/milestone scope + change log | Updated version files, git tag |
| Release notes | Human-readable release notes written | `git log <prev-tag>..HEAD` + story files | `CHANGELOG.md` entry + release notes document |
| Release checklist | All pre-release gates verified | Release branch | Pass/fail checklist: tests green, security clean, docs updated, version bumped |
| Upgrade validation | Release verified as safely deployable onto existing production releases | Previous release tag + current branch | Pass: upgrade path confirmed. Fail: stories created for breaking changes (migration missing, API incompatibility, config drift, etc.) |
| GitLab release | GitLab release created with notes and artifacts | Release notes + tag | GitLab release page |

**Memory:** Reads own memory for versioning conventions, past release patterns, known release checklist items.

**Activation Modes:** Interactive only (user invokes explicitly — releases are deliberate, not automatic).

**Design Notes:** Not in V1. Becomes relevant when the project approaches its first production release. Will integrate with `nebu-agent-testing` (final green-build verification) and `nebu-agent-kassandra` (final security sign-off before tag).

Upgrade validation checks: DB migration continuity (all golang-migrate files present and ordered), API backward compatibility (no removed/renamed endpoints without version bump), config schema changes (new required env vars without defaults), gRPC proto breaking changes. If any check fails, the agent does not block manually — it creates correction stories via `/bmad-create-story` so the issues enter the normal development cycle.

---

## Configuration

This module requires no custom configuration variables beyond core BMad settings. All project-specific paths (migrations, test dirs, CI config) are fixed by the Nebu project structure and do not need user prompts at setup time.

## External Dependencies

| Dependency | Type | Used By | Setup Skill Action |
|---|---|---|---|
| `context7` | MCP Server | All agents (framework docs lookup) | Check configured in MCP settings, link install docs if missing |
| `playwright` | MCP Server | nebu-agent-testing, nebu-pipeline (E2E step) | Check configured in MCP settings, link install docs if missing |
| `rtk` | CLI binary | All skills (token optimizer via hook) | Check `rtk --version` passes, warn if missing |
| `bmad-tea` | BMad module | nebu-pipeline (atdd, test-review gates), nebu-agent-testing | Check skills exist, error if missing |
| `docker` | CLI binary | nebu-agent-testing (CI execution via make targets) | Check `docker info` passes, warn if missing |

## UI and Visualization

No dedicated UI planned for this module. The `pipeline-state.yaml` file is human-readable and serves as the status view. If a dashboard becomes desirable later, `bmad-sprint-status` already covers the sprint-level view.

## Setup Extensions

Beyond config collection and dependency checks, the setup skill should:
1. Create `_bmad/nebu/` directory if it doesn't exist
2. Write an empty `pipeline-state.yaml` stub so agents can always find the file
3. Verify `bmad-tea` module skills are present (atdd, test-review, tea)
4. Check all three external tools (context7 MCP, playwright MCP, rtk) — warn but don't block if missing

## Integration

Standalone module — provides full value independently. Expands on the existing `bmad-pipeline` skill by replacing it with a properly structured, installable module. The existing `bmad-pipeline` SKILL.md can be removed once the module is live.

External module dependency: `bmad-tea` (must be installed). All TEA skill invocations stay identical to current usage.

## Creative Use Cases

- **Mid-sprint security pulse**: Call `nebu-agent-kassandra` ad-hoc against any branch to get early warning before the formal gate — especially useful before a big PR
- **Oracle-driven TDD**: During `dev-story`, consult oracle before writing any Matrix handler to get the spec constraints first — tests become spec-accurate automatically
- **UX + accessibility CI**: `nebu-agent-ux` runs an accessibility audit as part of every pipeline run that touches admin templates — zero-regression guarantee
- **Testing agent as local CI**: Run `nebu-agent-testing` standalone on any branch to reproduce a CI failure locally without pushing to GitLab
- **Resume after vacation**: Return to a paused story after days away — `pipeline-state.yaml` shows exactly where things stopped, which review cycle you were in, and why

## Ideas Captured

<!-- Raw ideas from brainstorming — preserved for context even if not all made it into the plan -->
<!-- Write here freely during phases 1-2. Don't write structured sections until phase 3+. -->

- Auslöser: bmad-pipeline SKILL.md ist 732 Zeilen groß und wächst weiter
- Nebu-Chat hat eigene, projektspezifische Skills (agent-oracle für Matrix API) neben Standard-BMad Skills
- Frage: Sollten alle nebu-spezifischen Skills als installierbares Modul gebündelt werden?

### Phase 1 Inputs (Phil, 2026-05-06)
- `bmad-install` Integration: Modul soll sauber installierbar sein
- Strukturtrennung: Workflows vs. Agents klarer aufteilen
- **State-Persistenz**: Pipeline-Zustand tracken — bei Unterbrechung (context limit, user break) wieder einsetzen können
- Bessere Agent-Integration: Agents sollen im Modul first-class sein
- Skalierung: Es werden mit der Zeit immer mehr Agents kommen

### Phase 2 Exploration (Phil, 2026-05-06)
- **State-Tracking**: Datei ähnlich sprint-status.yaml — aktueller Step, done-Liste; hilft auch Sub-Agents bei Orientierung
- **Agent-Ökosystem-Vision**: Komplexität wandert schrittweise aus der Pipeline in spezialisierte Agents
  - Existierend: `agent-oracle` (Matrix API Spec), `agent-kassandra` (Security Review)
  - Geplant: arc42-doc-Agent, Release-Management-Agent, Testing-Agent (GitLab-CI lokal ausführen)
  - Pipeline bleibt als sauberer Orchestrierungs-Workflow
  - Agents können flexibel auch innerhalb einzelner Pipeline-Steps miteinander interagieren
- **Kern-Muster**: Pipeline = Dirigent; Agents = Domänen-Spezialisten
  - Was heute in der Pipeline "eingebaut" ist, wandert nach und nach in Agents
  - Langfristig: Pipeline-Workflow ist dünn + deklarativ, Agents tragen die Intelligenz

- **Testing-Agent konkret**:
  - Verwaltet GitLab-CI-Konfiguration
  - Mini-Workflow als Teil des Review-Steps: build → unit-go → unit-elixir → leere E2E-Env starten → E2E-Tests ausführen → Bugs zurückmelden
  - Nutzt Skripte; starke Integration mit bmad-tea-Modul
  - Ist ein aktiver Agent mit Tool-Zugriff, nicht nur Wissensträger

- **Agent-Kollaboration während Dev-Step**:
  - Dev-Agent könnte während Implementierung andere Agents konsultieren (oracle für Matrix-Spec, Kassandra für Security-Check in-progress)
  - Heute: strikt sequenziell getrennt → Ziel: Agents können sich in Steps gegenseitig befragen

- **Minor Issues / Zyklen** (großes Pain Point heute):
  - Pipeline ist heute Einbahnstraße: create-story → atdd → dev → review → done
  - Ziel: Zyklen möglich — dev → review → dev → review ... bis sauber
  - Fixes im Review-Agent sind oft schlechter als wenn der Dev-Agent sie macht
  - → Review identifiziert Issues, gibt sie zurück an Dev-Agent für echten Fix-Zyklus
  - Review-Agent fixt nicht selbst (Qualitätsgründe, Rollentrennung)

- **Modul-Prefix: `nebu`** — klar diesem Projekt zugehörig, alle Skills heißen `nebu-{skillname}`

- **TEA-Integration bleibt unverändert**: bmad-tea, bmad-testarch-atdd, bmad-testarch-test-review etc. werden vom nebu-Modul aufgerufen wie bisher — keine Änderung an den TEA-Skills selbst, die Nutzung war gut

- **Test-Driven Pipeline (Phil, 2026-05-06)**: TDD-Ansatz für den gesamten Pipeline-Flow etablieren:
  - Story → test → test-review → dev → dev-review → test-agent-review → ...
  - Bug im Review gefunden → erst Test schreiben → dann dev → dann review
  - Kein Code ohne vorher geschriebenen fehlschlagenden Test — auch für Bug-Fixes
  - Ziel: nachhaltige Issue-Reduktion durch test-first in jedem Zyklus
  - Auswirkung auf nebu-pipeline: Pipeline-Steps explizit in test-first-Reihenfolge ordnen

- **Pipeline-Agent (geparkt)**: Idee: ein Agent der eine Sub-Pipeline starten kann — interessant für Agent-zu-Agent-Orchestrierung, aber zu weit für jetzt. Für spätere Iteration vormerken.

- **UX-Agent für Admin-Pages**: Nebu-spezifischer UX-Agent der die Admin-UI kennt (Go Templates + Tailwind + DaisyUI). Ergänzt `bmad-agent-ux-designer` mit Projekt-Kontext.
  - Scope: Admin-UI only (kein Matrix-Client)
  - Capabilities: Design-Review, UI-Stories planen, Templates schreiben
  - Stützt sich auf bmad-ux-Modul
  - Berücksichtigt Accessibility aus dem PRD zwingend
  - Unterstützt bei Playwright-Tests für UI

## Build Roadmap

Recommended build order — each skill is independently useful, earlier skills unblock later ones:

1. **`nebu-agent-oracle`** — No dependencies, already has a working predecessor (`agent-oracle`). Migration + memory init. Delivers immediate value for dev consultation.

2. **`nebu-agent-kassandra`** — No dependencies, already referenced in bmad-pipeline. Formalize + add memory. Immediately usable for security reviews.

3. **`nebu-agent-testing`** — Depends on Makefile + Docker being stable (they are). Replaces the inline CI-gate logic in bmad-pipeline. Most complex agent — build + test carefully.

4. **`nebu-agent-ux`** — Requires bmad-ux module to be installed. Build after oracle/kassandra so the pattern is established. Depends on PRD being readable for accessibility init.

5. **`nebu-pipeline`** — Can now reference all agents by name. Rewrite from bmad-pipeline: thin orchestration + state file + cycle logic. Remove old bmad-pipeline once this is green.

6. **`nebu-agent-arc42`** — Planned but not V1. Build once pipeline is stable and the documentation delta pattern is proven.

7. **`nebu-setup`** — Last, once all skills are built. Scaffolds installation, dependency checks, creates `_bmad/nebu/` structure.

**After all skills built:** Run `/bmad-module-builder` → Create Module (CM) to scaffold the installable module infrastructure.

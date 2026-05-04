---
security_review: not-needed
---

# Story 9.11: arc42-Dokumentation aus BMAD-Artefakten generieren + BMAD-Skill für laufende Pflege

Status: review

## Story

**As a** maintainer or external contributor landing on the Nebu repository,
**I want** a complete `docs/` folder in arc42 format that is derived from the existing BMAD planning artifacts and kept current via a dedicated BMAD pipeline skill,
**so that** I can understand the architecture, ADRs, and project scope without needing to know the BMAD toolchain or read the raw `_bmad-output/` internals.

**Size:** M

---

## Background

### Motivation

Story 8-2/8-3/8-8 produced README, CONTRIBUTING, and repo metadata for the public release.
Post-release, five doc links in the README still point to `_(coming soon)_` placeholders:

| Placeholder | Target |
|---|---|
| `docs/getting-started.md` | Full setup walkthrough |
| `docs/architecture/` | arc42 architecture overview |
| `docs/architecture/adr/` | 9 existing ADRs + 2 open placeholders |
| `docs/matrix-api-scope.md` | Full endpoint table from Epic 7 traceability |
| `docs/roadmap.md` | Roadmap derived from epics.md |

All source content already exists in `_bmad-output/planning-artifacts/`:
- `architecture.md` (56 K) — Core Architectural Decisions, patterns, ADRs 001–009
- `prd.md` (37 K) — Functional Requirements, quality goals, personas
- `epics.md` (200 K) — Full epic/story scope (roadmap source)

### arc42 Mapping

| arc42 Section | Primary Source |
|---|---|
| 01 Introduction & Goals | `prd.md` §Goals, §User-Personas, §FRs overview |
| 02 Architecture Constraints | `architecture.md` §Constraints |
| 03 Context & Scope | `architecture.md` §Context, ASCII diagram from README |
| 04 Solution Strategy | `architecture.md` §Core Architectural Decisions §Key Decisions |
| 05 Building Block View | `architecture.md` §Project Structure & Boundaries |
| 06 Runtime View | `architecture.md` §Implementation Patterns, gRPC flow |
| 07 Deployment View | `architecture.md` §Docker Compose, Makefile section |
| 08 Cross-cutting Concepts | `architecture.md` §Cross-cutting: Auth, Crypto, Audit |
| 09 Architecture Decisions | ADR index → `docs/architecture/adr/ADR-00N.md` |
| 10 Quality Requirements | `prd.md` §Non-Functional Requirements |
| 11 Risks & Technical Debt | `sprint-status.yaml` open items + retro deferred-work tables |
| 12 Glossary | `CLAUDE.md` + `architecture.md` key terms |

### New BMAD Skill: `bmad-generate-arc42`

A dedicated skill that:

1. Reads BMAD planning artifacts
2. Maps content to arc42 sections (defined in `customize.toml` as `arc42_section_map`)
3. Writes / updates `docs/` files idempotently
4. Maintains `docs/.arc42-manifest.json`: per-file `source`, `generated_at`, `editable` flag
   - `editable: false` — fully auto-generated, safe to overwrite on next run
   - `editable: true` — seeded from BMAD, then manually maintained; skill skips on update unless `--force`
5. CI-friendly: `scripts/verify-docs.sh` checks existence + non-empty + manifest freshness

### Output Structure

```
docs/
  .arc42-manifest.json          ← generation manifest (source, timestamp, editable flag)
  architecture/
    README.md                   ← arc42 01: Introduction + Goals + context diagram
    02-constraints.md           ← arc42 02
    03-context.md               ← arc42 03
    04-solution-strategy.md     ← arc42 04
    05-building-blocks.md       ← arc42 05
    06-runtime.md               ← arc42 06
    07-deployment.md            ← arc42 07
    08-concepts.md              ← arc42 08
    09-decisions.md             ← arc42 09 (ADR index)
    10-quality.md               ← arc42 10
    11-risks.md                 ← arc42 11
    12-glossary.md              ← arc42 12
    adr/
      ADR-001-elixir-otp.md
      ADR-002-no-redis-nats.md
      ADR-003-content-hash-event-id.md
      ADR-004-horde-registry.md
      ADR-005-grpc-eventbus.md
      ADR-006-message-buffer-drain.md
      ADR-007-ed25519-x25519-keypairs.md
      ADR-008-node-registration-psk.md
      ADR-009-openapi-spec-first.md
      ADR-010-fts-strategy.md           ← placeholder: "Decision pending — see Issue #N"
      ADR-011-managed-e2ee-key-escrow.md ← placeholder: "Decision pending — see Issue #N"
  getting-started.md            ← editable: true (seeded from README Quick Start, extended)
  matrix-api-scope.md           ← editable: false (auto from Epic 7 traceability)
  roadmap.md                    ← editable: false (auto from epics.md status)
```

### Pipeline Extension

The skill is invokable as `/bmad-generate-arc42` and integrates into the BMAD pipeline as an **optional documentation gate**:

```
bmad-create-story → ... → bmad-dev-story → bmad-code-review → [bmad-generate-arc42]
```

Trigger conditions for re-running the skill:
- Any PR that modifies `_bmad-output/planning-artifacts/architecture.md` or `prd.md`
- At the end of each epic (alongside `/bmad-testarch-trace`)
- Manually at any time via `/bmad-generate-arc42`

CI check (`scripts/verify-docs.sh`) only validates existence + manifest freshness (not content correctness).

---

## Acceptance Criteria

**AC1 — Output files exist:**
All files in the output structure above are present after the skill runs. No file is empty (minimum 200 bytes).

**AC2 — README `_(coming soon)_` links resolve:**
All five `_(coming soon)_` links in `README.md` point to files that now exist:
`docs/getting-started.md`, `docs/architecture/`, `docs/architecture/adr/`, `docs/matrix-api-scope.md`, `docs/roadmap.md`.
The `_(coming soon)_` markers are removed from README.

**AC3 — arc42 section completeness:**
Each arc42 section file (`README.md` through `12-glossary.md`) contains:
- A level-1 heading with the arc42 section number and name
- At least one paragraph of substantive content derived from BMAD artifacts (not lorem ipsum / placeholder-only)
- A `_Source: <artifact-file>, <section>_` footer line

**AC4 — ADR files:**
`docs/architecture/adr/ADR-001` through `ADR-009` each contain:
- Status (Accepted), Date, Context, Decision, Consequences
- Content matches the corresponding ADR entry in `CLAUDE.md` and `architecture.md`

`ADR-010` and `ADR-011` exist as placeholders with status `Proposed` and a one-line context statement.

**AC5 — `.arc42-manifest.json` is valid:**
`docs/.arc42-manifest.json` is valid JSON containing at minimum:
- `generated_at` (ISO 8601 datetime)
- `source_artifacts` array listing the BMAD files used
- `files` object: each output file keyed by relative path with `editable` boolean and `generated_at`

**AC6 — `scripts/verify-docs.sh` passes:**
A new script `scripts/verify-docs.sh` runs without error when docs are present:
- Checks all required files exist and are non-empty
- Checks `.arc42-manifest.json` is valid JSON
- Checks that `editable: false` files have a `generated_at` within the last 180 days (staleness warning, not error)
- Exits 0 on pass, 1 on failure; prints per-check PASS/FAIL

**AC7 — CI integration:**
`scripts/verify-docs.sh` is added to both `.github/workflows/ci.yml` and `.gitlab-ci.yml` as a `docs` job that runs on every PR. Job failure is `allow_failure: true` (warning, not blocking) until the docs are established as authoritative.

**AC8 — BMAD skill installable:**
`.claude/skills/bmad-generate-arc42/` exists with:
- `customize.toml` defining `arc42_section_map` (BMAD artifact → arc42 section mappings)
- `skill.md` (the skill workflow)
- The skill is listed in available skills and invokable via `/bmad-generate-arc42`

**AC9 — `getting-started.md` is complete:**
`docs/getting-started.md` covers the full local dev setup:
- Prerequisites (Docker Desktop, make, git)
- `make setup` + `make dev` + `/etc/hosts` step
- Bootstrap Wizard walkthrough (OIDC config, first login)
- Connect a Matrix client (Element Web: server URL, login)
- Common `make` targets reference table
- Troubleshooting section (5 most common first-run issues from `docs/stories/` bug writeups)

**AC10 — `matrix-api-scope.md` is complete:**
`docs/matrix-api-scope.md` contains:
- A full endpoint table (Implemented / Stub / Not implemented) derived from `gateway/cmd/gateway/main.go` route registrations cross-referenced with Epic 7 traceability
- "Intentionally excluded" section (federation, identity server, key server)
- "Current stubs" section (E2EE keys, `/_matrix/key/v2/server` gap noted)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `test_all_arc42_files_exist` — Bash**
- Given: `/bmad-generate-arc42` has been run
- When: `scripts/verify-docs.sh` executes
- Then: exits 0; all required files present and non-empty

**2. `test_manifest_is_valid_json` — Bash**
- Given: skill ran successfully
- When: `python3 -c "import json; json.load(open('docs/.arc42-manifest.json'))"` executes
- Then: exits 0 (valid JSON)

**3. `test_readme_coming_soon_links_removed` — Bash**
- Given: story is complete
- When: `grep -c "coming soon" README.md`
- Then: exits with count 0 (no remaining placeholders)

**4. `test_adr_files_have_required_sections` — Bash**
For ADR-001 through ADR-009:
- When: each file is read
- Then: file contains "## Status", "## Context", "## Decision", "## Consequences"

**5. `test_verify_docs_ci_job_registered` — Bash**
- When: `grep -q "verify-docs" .github/workflows/ci.yml`
- Then: exits 0

**6. `test_skill_dir_exists` — Bash**
- When: `[[ -d .claude/skills/bmad-generate-arc42 ]]`
- Then: true; `customize.toml` and skill file present

---

## Dev Notes

### Skill Implementation Approach

The skill workflow should follow the `bmad-document-project` pattern but with an explicit arc42 section map in `customize.toml`:

```toml
[workflow.arc42_section_map]
"01-intro"        = { source = "prd.md",          section = "Goals|Overview" }
"02-constraints"  = { source = "architecture.md",  section = "Constraints" }
"03-context"      = { source = "architecture.md",  section = "Context|Three-tier" }
# ... etc.
```

The skill reads each mapped section, distils to arc42 prose, writes the output file, and records the manifest entry. For `editable: true` files (getting-started, ADR bodies that need manual enrichment), the skill seeds the file once and then skips on subsequent runs unless `--force` is passed.

### ADR Content Sources

The 9 ADRs are described in two places — reconcile both:
1. **CLAUDE.md** `## Resolved Architecture Decisions` table (one-line summaries)
2. **`_bmad-output/planning-artifacts/architecture.md`** `## Core Architectural Decisions` section (detailed rationale)

ADR-010 (FTS) and ADR-011 (Managed E2EE) are `Proposed` status — the placeholders should link to the relevant GitHub Issues once they exist.

### `matrix-api-scope.md` Generation

Parse `gateway/cmd/gateway/main.go` for `mux.Handle(` registrations to produce an authoritative endpoint table. Cross-reference with CLAUDE.md `## Matrix API Scope` and Epic 7 traceability matrix. Categorise each endpoint as:
- ✅ Implemented (real handler)
- 🔶 Stub (returns hardcoded/minimal response, comment says "stub" or "501")
- ❌ Intentionally excluded
- ⏳ Planned (ADR required)

### `verify-docs.sh` Structure

Follow the `run_test` / pass-fail counter pattern from Stories 8.2–8.9:

```bash
run_test "test_name" "condition" "expected"
```

Print `=== PASS/FAIL` per test, summary at end, exit code matches pass/fail.

### CI Job Placement

Add `docs` job to both CI files after the existing `verify` stage. Use `allow_failure: true` / `allow_failure: false` as a toggle — start with `true` for the first merge, flip to `false` once the doc generation is part of the standard workflow.

---

## Out of Scope

- Automated re-generation on every commit (manual trigger + PR CI check is sufficient for now)
- Translation / i18n of docs (English only per `document_output_language` config)
- API reference generation from OpenAPI spec (separate story if needed)
- Hosting / publishing (GitHub Pages, Docusaurus) — plain Markdown for now

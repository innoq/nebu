# bmad-generate-arc42

**Role:** arc42 Documentation Generator for Nebu

**Invocation:** `/bmad-generate-arc42`

**Trigger conditions (automatic):**
- Any PR that modifies `_bmad-output/planning-artifacts/architecture.md` or `prd.md`
- At the end of each epic (alongside `/bmad-testarch-trace`)
- Manual invocation at any time

---

## Purpose

This skill generates and maintains the `docs/` folder in arc42 format by reading BMAD planning
artifacts and mapping their content to arc42 sections. It replaces manual documentation work
with a repeatable, artifact-driven generation process.

The skill is idempotent: re-running it on unchanged source artifacts produces identical output.
Files marked `editable: true` in `docs/.arc42-manifest.json` are skipped on subsequent runs
unless `--force` is passed.

---

## Workflow

### Step 1 — Read Source Artifacts

Read all source artifacts listed in `customize.toml`:
- `_bmad-output/planning-artifacts/architecture.md` — Core architectural decisions, patterns, ADRs
- `_bmad-output/planning-artifacts/prd.md` — Functional requirements, quality goals, personas
- `_bmad-output/planning-artifacts/epics.md` — Full epic/story scope (roadmap source)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — Open items, deferred work
- `CLAUDE.md` — Tech stack, ADR table, Matrix API scope
- `README.md` — Current state, quick start
- `gateway/cmd/gateway/main.go` — Route registrations for matrix-api-scope.md

### Step 2 — Map Content to arc42 Sections

For each section defined in `[workflow.arc42_section_map]` in `customize.toml`:
1. Read the specified source artifact
2. Extract the relevant section(s) as listed in the `section` field
3. Distil to arc42 prose: clear headings, substantive content (not lorem ipsum)
4. Add the required `_Source: <artifact>, <section>_` footer line

### Step 3 — Generate ADR Files

For each ADR in `[workflow.adr_map]`:
- ADR-001 through ADR-009: Full ADR with `## Status` (Accepted), `## Context`, `## Decision`, `## Consequences`
- ADR-010 and ADR-011: Placeholder with `## Status` (Proposed) and one-line context statement

### Step 4 — Generate Extra Files

- `docs/getting-started.md` (editable: true — seeded once): Full local dev setup walkthrough
- `docs/matrix-api-scope.md` (editable: false): Parse `gateway/cmd/gateway/main.go` for `mux.Handle(` registrations
- `docs/roadmap.md` (editable: false): Roadmap from epics.md + sprint-status.yaml

### Step 5 — Update Manifest

Write/update `docs/.arc42-manifest.json` with:
```json
{
  "generated_at": "<ISO 8601 timestamp>",
  "generator": "bmad-generate-arc42 skill",
  "source_artifacts": ["..."],
  "files": {
    "docs/architecture/README.md": {
      "editable": false,
      "generated_at": "<timestamp>",
      "source": "<artifact>",
      "arc42_section": "01 Introduction and Goals"
    },
    ...
  }
}
```

### Step 6 — Verify

Run `scripts/verify-docs.sh` to confirm all checks pass. If any check fails, re-examine the
generated files and fix the issue before declaring the run complete.

### Step 7 — Report

Report:
- Number of files written/updated/skipped
- Any `editable: true` files that were skipped (and how to force-update them)
- Result of `scripts/verify-docs.sh`

---

## File Categorization

### editable: false (auto-generated, safe to overwrite)

These files are fully derived from BMAD artifacts. They should never be edited manually.
The skill overwrites them on every run.

- All `docs/architecture/*.md` sections (01–12)
- All `docs/architecture/adr/ADR-001` through `ADR-009`
- `docs/matrix-api-scope.md`
- `docs/roadmap.md`

### editable: true (seeded, manually maintained)

These files are seeded from BMAD artifacts but are expected to be maintained by hand.
The skill skips them on subsequent runs unless `--force` is passed.

- `docs/getting-started.md`
- `docs/architecture/adr/ADR-010-fts-strategy.md`
- `docs/architecture/adr/ADR-011-managed-e2ee-key-escrow.md`

---

## CI Integration

`scripts/verify-docs.sh` runs in CI as the `docs` job (currently `allow_failure: true`).

It checks:
- All required files exist and are ≥200 bytes
- `docs/.arc42-manifest.json` is valid JSON with required keys
- `editable: false` files have a `generated_at` within the last 180 days (staleness warning)

---

## Pipeline Position

```
bmad-create-story → ... → bmad-dev-story → bmad-code-review → [bmad-generate-arc42]
```

`bmad-generate-arc42` is an **optional documentation gate** invoked at epic boundaries or when
planning artifacts change. It does not block story delivery.

---

## Requirements

- Python 3 (for manifest validation)
- Bash (for `scripts/verify-docs.sh`)
- Standard POSIX tools (`wc`, `grep`, `find`)
- No external dependencies beyond the repo itself

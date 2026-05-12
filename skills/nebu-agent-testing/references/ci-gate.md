---
name: ci-gate
code: ci-gate
description: Full CI gate — runs all test suites against a fresh stack in order, collects failures, and returns a structured pass/fail report to nebu-pipeline.
---

# CI Gate

## What Success Looks Like

Every suite runs to completion against a fresh stack. The result is unambiguous: green (all suites passed) or broken (list of failures with file, test name, and error). No partial results reported as complete. No cached state contaminating results.

## Check Memory First

Before starting: read MEMORY.md for known flaky tests. If a known flaky test fails in isolation, note it but do not let it block — re-run that suite once. If it fails again, it counts.

## Run the Gate Script

The full CI gate is scripted. Run it and read the JSON output:

```bash
python3 skills/nebu-agent-testing/scripts/ci_gate.py --story [STORY_ID]
```

The script orchestrates in order: build → unit-go → unit-elixir → `docker compose down --volumes` → `docker compose up -d --wait` → health check (Gateway + Keycloak) → e2e → integration → teardown. Build failure aborts immediately. Environment startup timeout (120s) skips e2e and integration.

**Options:**
- `--only SUITE` — run one suite: `build` | `unit-go` | `unit-elixir` | `e2e` | `integration`
- `--skip SUITE` — skip a suite (repeatable)
- `--no-reset` — skip volume wipe (use when environment is already running cleanly)
- `--env-timeout N` — override 120s startup timeout

Exit code 0 = pass, 1 = fail.

## Coverage Checks (apply after script completes)

These checks are not automated by the script — apply them manually against the JSON output and staged diff:

**All E2E tests — Gherkin format required:** Every new Playwright test must have a `.feature` file in `e2e/features/` and step definitions in `e2e/steps/`. Check:
```bash
rtk git diff --staged --name-only | grep "^e2e/"
```
If new `e2e/` files exist but no `.feature` is among them → set `missing_gherkin_feature: true` in the report and block.

**Matrix feature stories** (story touches `/_matrix/`, `m.room.*`, or sync): At least one Gherkin scenario must exercise the feature through a browser-level Matrix client. Godog HTTP tests alone do not satisfy this. Check:
```bash
rtk grep -r "_matrix/\|MatrixClient\|element\|web-client" e2e/features/ 2>/dev/null | head -20
```
If missing → set `missing_matrix_e2e_coverage: true` and block.

## Output Format

```json
{
  "status": "pass" | "fail",
  "story": "<story-id>",
  "timestamp": "ISO",
  "suites": {
    "build": { "status": "pass" | "fail", "errors": [] },
    "unit-go": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } },
    "unit-elixir": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } },
    "e2e": { "status": "pass" | "fail", "failures": [], "missing_gherkin_feature": false, "missing_coverage": false, "missing_matrix_e2e_coverage": false },
    "integration": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } }
  },
  "blocking_failures": [],
  "known_flaky": []
}
```

## Memory Integration

After gate: note any new flaky test discoveries in session log. If a suite consistently fails in specific ways, note the pattern.

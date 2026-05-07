---
name: selective-run
code: selective-run
description: Run a specific test suite in isolation — useful for debugging a CI failure locally without running the full gate.
---

# Selective Run

## What Success Looks Like

The requested suite runs and the result is clear — which tests passed, which failed, and what the errors are. Useful for quick local debugging or reproducing a CI failure.

## Script Interface

All selective runs go through the same `ci_gate.py` script. Use `--only` to target a single suite:

| Goal | Command |
|---|---|
| Build only | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only build` |
| Go unit tests only | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only unit-go` |
| Elixir unit tests only | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only unit-elixir` |
| E2E only (fresh env) | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only e2e` |
| E2E only (env already up) | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only e2e --no-reset` |
| Integration only (fresh env) | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only integration` |
| Integration only (env already up) | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --only integration --no-reset` |
| Skip E2E (build + unit + integration) | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --skip e2e` |
| Start environment only | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --env-up` |
| Tear down environment only | `python3 skills/nebu-agent-testing/scripts/ci_gate.py --env-down` |

**`--no-reset`** skips `docker compose down --volumes` — use when the environment is already running cleanly from a previous step.

## E2E Format and Coverage Rules

**Gherkin format required:** All Playwright E2E tests must have a `.feature` file in `e2e/features/` and step definitions in `e2e/steps/`. Plain `.spec.ts` files without a `.feature` counterpart are not accepted. Flag `missing_gherkin_feature: true` if new `e2e/` files lack a corresponding `.feature`.

**Matrix feature stories:** At least one Gherkin scenario must exercise the feature through a browser-level Matrix client (Element Web or similar). Godog integration tests alone do not satisfy this. Flag `missing_matrix_e2e_coverage: true` if no browser-client scenario covers the Matrix behavior.

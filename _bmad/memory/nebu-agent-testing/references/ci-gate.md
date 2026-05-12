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

## Mini-Workflow (run in order, stop on CRITICAL failure)

### Step 1 — Build

```bash
make build-gateway && make build-core
```

**On failure:** Stop. Report build errors. Do not continue to tests — broken build means tests are meaningless.

### Step 2 — Unit Tests: Go

```bash
make test-unit-go
```

Collect failing test names and error output.

### Step 3 — Unit Tests: Elixir

```bash
make test-unit-elixir
```

Collect failing test names and error output.

### Step 4 — Start E2E Environment

```bash
make dev
```

Wait for all services healthy. Check:
- Gateway: `GET http://localhost:8008/_matrix/client/versions` → 200
- Core: Elixir application started (check compose logs)
- PostgreSQL: accepting connections
- Keycloak: `GET http://localhost:8080/` → 200

**On timeout (120s):** Report environment startup failure. Do not run E2E tests against a broken stack.

### Step 5 — E2E Tests

```bash
make test-e2e
```

Uses Playwright. Collect failing test names, screenshots (if available), and error output.

If this is a story with `ui: true`: verify that new E2E tests exist in `e2e/tests/` or `e2e/features/` for the story's UI behavior. Flag missing coverage as a finding.

### Step 6 — Integration Tests

```bash
make test-integration
```

Godog/Gherkin tests. Collect failing scenario names and step errors.

### Step 7 — Tear Down

```bash
docker compose down
```

Always tear down, even on failure.

## Output Format

```json
{
  "status": "pass" | "fail",
  "story": "<story-id>",
  "suites": {
    "build": { "status": "pass" | "fail", "errors": [] },
    "unit-go": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } },
    "unit-elixir": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } },
    "e2e": { "status": "pass" | "fail", "failures": [], "missing_coverage": false },
    "integration": { "status": "pass" | "fail", "failures": [], "count": { "passed": N, "failed": N } }
  },
  "blocking_failures": [],
  "known_flaky": []
}
```

## Memory Integration

After gate: note any new flaky test discoveries in session log. If a suite consistently fails in specific ways, note the pattern.

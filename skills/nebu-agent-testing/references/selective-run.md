---
name: selective-run
code: selective-run
description: Run a specific test suite in isolation — useful for debugging a CI failure locally without running the full gate.
---

# Selective Run

## What Success Looks Like

The requested suite runs and the result is clear — which tests passed, which failed, and what the errors are. Useful for quick local debugging or reproducing a CI failure.

## Available Suites

| Suite | Command | Notes |
|---|---|---|
| `build` | `make build-gateway && make build-core` | No environment needed |
| `unit-go` | `make test-unit-go` | No environment needed |
| `unit-elixir` | `make test-unit-elixir` | No environment needed |
| `e2e` | `make test-e2e` | Requires running E2E environment (start with `make dev`) |
| `integration` | `make test-integration` | Requires running E2E environment |

## E2E Environment

If running `e2e` or `integration`: check if the environment is already running before starting a new one.

```bash
docker compose ps
```

If not running: start with `make dev`, wait for services healthy (see ci-gate.md health checks), then run the suite.

## Output

Report the same structured format as ci-gate but for the selected suite only:

```json
{
  "suite": "<suite-name>",
  "status": "pass" | "fail",
  "failures": ["<test-name>: <error>"],
  "count": { "passed": N, "failed": N }
}
```

# Capabilities

## Built-in

| Code | Name | Description | Source |
|------|------|-------------|--------|
| [ci-config] | ci-config | Manage the GitLab CI configuration — keep .gitlab-ci.yml accurate, add new jobs for new test types, and verify the pipeline matches the local Makefile targets. | `./references/ci-config.md` |
| [ci-gate] | ci-gate | Full CI gate — runs all test suites against a fresh stack in order, collects failures, and returns a structured pass/fail report to nebu-pipeline. | `./references/ci-gate.md` |
| [selective-run] | selective-run | Run a specific test suite in isolation — useful for debugging a CI failure locally without running the full gate. | `./references/selective-run.md` |

## Learned

_Capabilities added by the owner over time. Prompts live in `capabilities/`._

| Code | Name | Description | Source | Added |
|------|------|-------------|--------|-------|

## How to Add a Capability

Tell me "I want you to be able to run X" and we'll create it together.
Load `references/capability-authoring.md` for the full creation framework.

## Tools

### Docker + Make (Bash tool)
Primary tools for running the CI suite. `make` targets defined in project Makefile.

### Playwright MCP
Used for E2E test inspection and visual verification of browser-level test failures.

### User-Provided Tools
_Additional tools the owner has made available._

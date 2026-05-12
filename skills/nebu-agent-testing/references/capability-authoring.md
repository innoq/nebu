---
name: capability-authoring
description: Guide for creating and evolving learned CI capabilities
---

# Capability Authoring

When a new test type or CI concern emerges, you can learn to handle it.

## Format

```markdown
---
name: {capability-name}
code: {short-code}
description: {one-line summary}
---

# {Capability Name}

## What Success Looks Like
{What does a complete run of this capability look like?}

## Command
{The make target or command to run}

## Output
{How to report results}
```

## Tester-Specific Capability Ideas

- **Performance baseline**: run k6 or similar load test and compare against baseline
- **Contract tests**: run Pact or similar consumer-driven contract tests
- **Migration check**: verify migration files are in sequence and not broken
- **Dependency audit**: `go mod audit` + `mix deps.audit` for security advisories

---
name: memory-guidance
description: Memory philosophy and practices for Tester
---

# Memory Guidance

## What to Remember

- **Known flaky tests**: name, suite, frequency, workaround. Re-run once before counting as failure.
- **E2E environment quirks**: startup timing, known issues, service dependency order
- **CI drift**: any known differences between local `make` and GitLab CI behavior
- **Recurring failure patterns**: test failures that indicate a systemic issue (not just a flaky test)

## What NOT to Remember

- Individual test run results (those are in git history and CI logs)
- Passing test outputs
- Things derivable from Makefile or docker-compose directly

## Session Log Format

```markdown
## Session — {story-id or context}

**Gate result:** pass | fail

**Suite results:** build ✓, unit-go ✓, unit-elixir ✗ (3 failures), e2e ✓, integration ✓

**Failures:**
- {test name}: {brief error}

**New flaky tests discovered:** {if any}

**Follow-up:** {anything needing attention}
```

## Token Discipline

Keep MEMORY.md focused: flaky test registry, environment quirks, CI drift. Not test results history — that's in logs.

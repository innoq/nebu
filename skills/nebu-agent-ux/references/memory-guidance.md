---
name: memory-guidance
description: Memory philosophy and practices for UX
---

# Memory Guidance

## What to Remember

- **PRD accessibility requirements**: The exact requirements, not just "WCAG AA" — specific mandates from the PRD that apply to every session
- **Component library**: Which DaisyUI components are in use and how — established patterns to extend
- **Template conventions**: Layout structure, partial organization, shared components
- **Playwright patterns**: Auth helper, base URL, selector conventions
- **Known accessibility issues**: Pages that have known gaps to watch

## What NOT to Remember

- Individual review results — those are in git history
- Generic WCAG knowledge — that's in training, not memory
- Things derivable from reading the templates directly

## Session Log Format

```markdown
## Session — {story or context}

**What changed:** {templates modified, tests written}

**New patterns established:** {any component or Playwright patterns worth saving}

**Accessibility findings:** {brief summary}

**Follow-up:** {anything needing attention}
```

## Key MEMORY.md Sections

```markdown
## PRD Accessibility Requirements
[The specific requirements from the PRD — filled during First Breath]

## Component Library
[Established DaisyUI patterns and how they're used in this project]

## Template Conventions
[Layout structure, partial organization, naming]

## Playwright Patterns
[Auth helper, base URL, selector conventions]
```

## Token Discipline

Keep MEMORY.md focused on the things that need to survive across sessions: PRD requirements, established patterns, conventions. Not implementation details derivable from reading the code.

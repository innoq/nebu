---
name: memory-guidance
description: Memory philosophy and practices for Kassandra
---

# Memory Guidance

## The Fundamental Truth

You are stateless. Every conversation begins with total amnesia. Your sanctum is the ONLY bridge between sessions. If you don't write it down, it never happened.

## What to Remember

- **Architecture context**: Go Gateway auth layer, Elixir Core event handling, which layer owns which security responsibility
- **Accepted risks**: Formally acknowledged deviations with owner sign-off — don't re-flag these
- **Recurring patterns**: Finding types that appear repeatedly in this codebase — these indicate systemic issues worth tracking
- **Sensitive surfaces**: Parts of the codebase that deserve extra scrutiny (auth, token handling, admin endpoints)
- **Past epic findings**: Summary of what was found in previous epics for trend awareness

## What NOT to Remember

- Full review transcripts — distill the pattern, not the session
- Resolved findings that were fixed — they're in git history
- General security knowledge — that's in your training, not your memory
- Things derivable from the codebase directly

## Two-Tier Memory

### Session Logs (raw, append-only)
After each review: `sessions/YYYY-MM-DD.md`

```markdown
## Session — {story/epic ID or context}

**Scope:** {what was reviewed}

**Finding summary:** {N} CRITICAL, {N} HIGH, {N} MEDIUM, {N} LOW

**New patterns discovered:** {anything new for the Nebu codebase}

**Accepted risks this session:** {any formally acknowledged}

**Follow-up:** {anything needing attention}
```

### MEMORY.md (curated, distilled)

Keep tight. Key sections:
- Nebu architecture security context
- Accepted risks (with date and justification)
- Recurring finding patterns
- Epic-end review history (date, finding counts)

## When to Write

- **After every review**: session log with finding summary
- **When accepted**: immediately record to MEMORY.md
- **New pattern found**: add to session log, curate to MEMORY.md if it recurs

## Token Discipline

Keep MEMORY.md under 200 lines. Security knowledge is in training — memory tracks Nebu-specific patterns, accepted risks, and architecture context only.

---
name: memory-guidance
description: Memory philosophy and practices for Oracle
---

# Memory Guidance

## The Fundamental Truth

You are stateless. Every conversation begins with total amnesia. Your sanctum is the ONLY bridge between sessions. If you don't write it down, it never happened. If you don't read your files, you know nothing.

This is not a limitation to work around. It is your nature. Embrace it honestly.

## What to Remember

- Nebu implementation decisions — so you don't re-litigate them
- Spec quirks discovered for this specific codebase — the surprises that came up during reviews
- Owner's review preferences (inline vs. structured, detail level)
- Spec areas that are currently in-flight (being implemented now)
- Compliance findings that were accepted as known deviations
- What worked — framings, levels of detail that clicked

## What NOT to Remember

- The full content of spec sections — those come from context7, not memory
- Transient task details — completed reviews, resolved questions
- Things derivable from the codebase directly
- Raw conversation — distill the insight, not the dialogue

## Two-Tier Memory

### Session Logs (raw, append-only)

After each session, append key notes to `sessions/YYYY-MM-DD.md`. These are raw notes.

```markdown
## Session — {time or context}

**What happened:** {1-2 sentence summary}

**Key outcomes:**
- {finding or decision}

**Nebu spec decisions recorded:** {any implementation choices made}

**Follow-up:** {open questions or pending reviews}
```

### MEMORY.md (curated, distilled)

Your long-term memory. Keep it tight and current. MEMORY.md IS loaded on every rebirth.

**Key sections to maintain:**
- Nebu implementation scope (which endpoints live, which planned, spec version)
- Known implementation decisions (where Nebu intentionally deviates or makes a spec choice)
- Recurring spec pain points for this codebase
- Owner working preferences

## When to Write

- **Immediately** — when a spec decision is made during a session
- **End of session** — session log with outcomes
- **After compliance review** — any accepted deviations go into MEMORY.md as known exceptions

## Token Discipline

Keep MEMORY.md under 200 lines. Spec knowledge lives in context7, not memory — memory tracks Nebu-specific decisions and patterns, not general Matrix spec content.

---
name: capability-authoring
description: Guide for creating and evolving learned UI capabilities
---

# Capability Authoring

When a new Admin UI concern emerges — a new component type, a new testing pattern, a new accessibility requirement — you can learn to handle it.

## Format

```markdown
---
name: {capability-name}
code: {short-code}
description: {one-line summary}
---

# {Capability Name}

## What Success Looks Like
{Outcome-focused description}

## Approach
{Only include if non-obvious}

## Memory Integration
{What to remember after running this capability}
```

## UX-Specific Capability Ideas

- **Dark mode audit**: Check Admin UI for dark mode compatibility with DaisyUI themes
- **Mobile review**: Verify Admin UI works on 768px viewport minimum
- **Form validation patterns**: Review or implement client-side validation patterns
- **Loading state patterns**: Implement and test loading states for async Admin UI actions

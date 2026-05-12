---
name: design-review
code: design-review
description: Review existing Admin UI templates or designs for accessibility compliance, DaisyUI consistency, and Tailwind best practices. Produces structured findings.
---

# Design Review

## What Success Looks Like

Every accessibility violation is found and classified. Every DaisyUI pattern that deviates from the project's established conventions is flagged. The review gives the developer a concrete fix for each finding, not just a diagnosis.

## Preparation

Read MEMORY.md for:
- PRD accessibility requirements (WCAG level, specific mandates)
- Established component patterns — what DaisyUI components are in use, how they're structured
- Known Admin UI conventions (layout, navigation patterns)

## Review Dimensions

### Accessibility (from PRD requirements — hard constraints)

- **Semantic HTML**: Correct element types (`<button>` not `<div>` for interactive elements, `<nav>` for navigation, `<main>` for main content)
- **ARIA labels**: Interactive elements without visible labels have `aria-label` or `aria-labelledby`
- **Color contrast**: Text meets WCAG AA contrast ratio (4.5:1 for normal text, 3:1 for large text)
- **Keyboard navigation**: All interactive elements reachable and operable via keyboard; focus order is logical; focus visible
- **Form labels**: Every `<input>` has an associated `<label>` (explicit or `aria-label`)
- **Image alt text**: All `<img>` elements have `alt` attributes (empty for decorative images)
- **Error identification**: Form errors are programmatically associated with their fields (`aria-describedby`)
- **Heading hierarchy**: `<h1>` → `<h2>` → `<h3>` — no skipped levels
- **Link text**: Links have descriptive text, not "click here" or bare URLs

### DaisyUI Consistency

- Uses DaisyUI component classes where a component exists (e.g., `btn`, `card`, `badge`, `alert`, `form-control`, `input`)
- Does not reinvent what DaisyUI provides
- DaisyUI theme variables used for colors (not hardcoded Tailwind colors that bypass theming)
- Responsive modifiers consistent with project conventions

### Tailwind Usage

- Utility classes follow mobile-first responsive order
- No inline styles where Tailwind utilities suffice
- Custom colors/spacing use the project's Tailwind config values

### Go Template Structure

- Template inheritance via `{{template}}` blocks follows project layout conventions
- Shared partials used where components repeat

## Output Format

```markdown
## Design Review — [page/component name]

### Accessibility Findings (from PRD requirements)

| # | Severity | Element | Issue | Fix |
|---|----------|---------|-------|-----|
| 1 | WCAG violation (blocking) | `<button>` line 42 | Missing accessible label | Add `aria-label="..."` |
| 2 | WCAG violation (blocking) | ... | ... | ... |
| 3 | Advisory | ... | ... | ... |

### DaisyUI / Tailwind Findings

| # | Severity | Location | Issue | Fix |
|---|----------|----------|-------|-----|

### Summary

Blocking: [N] WCAG violations
Advisory: [N] consistency findings
```

## Severity Definitions

- **WCAG violation (blocking)**: Fails an accessibility requirement from the PRD. Must be fixed before merge.
- **Advisory**: Consistency or best-practice issue. Address before epic end.

## Memory Integration

After review: if a new pattern was established (e.g., how to correctly implement a specific accessible component), add it to the session log for curation into MEMORY.md.

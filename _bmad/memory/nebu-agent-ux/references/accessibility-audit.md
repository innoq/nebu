---
name: accessibility-audit
code: accessibility-audit
description: Audit Admin UI pages against PRD accessibility requirements. WCAG violations are blocking findings. Called by nebu-pipeline on ui:true stories.
---

# Accessibility Audit

## What Success Looks Like

Every page changed by the current story is checked against PRD accessibility requirements. WCAG violations are identified with exact location, requirement, and fix. Advisory findings are noted. The audit runs against the running stack where possible.

## Preparation

Read MEMORY.md for PRD accessibility requirements. These are the hard constraints — not general WCAG best practices, but the specific requirements this project committed to.

## Audit Method

### Static Analysis (always)

Review the template diff for:
- Missing `alt` attributes on `<img>`
- Interactive `<div>` or `<span>` without `role` and `tabindex`
- Form inputs without associated labels
- Missing ARIA attributes on dynamic content
- Hardcoded color values that may fail contrast

### Browser-Level (when Playwright MCP available)

Use `playwright` MCP to inspect the rendered page:
- Tab through all interactive elements — verify keyboard reachability and focus order
- Run axe-core or similar via `page.evaluate()` for automated checks
- Verify `aria-live` regions update correctly for dynamic content
- Check focus management after modal open/close or form submit

```typescript
// Run axe accessibility check
const results = await page.evaluate(async () => {
  // @ts-ignore
  return await window.axe.run();
});
const violations = results.violations;
```

## Output Format

```markdown
## Accessibility Audit — [story/page]

**PRD requirements checked:** [list]

### Violations (blocking)

| # | Requirement | Element | Location | Fix |
|---|-------------|---------|----------|-----|
| 1 | WCAG 1.1.1 Alt text | `<img>` | template line 23 | Add `alt="[description]"` |

### Advisory

| # | Issue | Location | Recommendation |
|---|-------|----------|----------------|

### Summary

Violations: [N] — block merge
Advisory: [N]

**Verdict:** PASS / BLOCKED
```

## Memory Integration

After audit: note any new accessibility pattern or fix that should be added to the component guidelines in MEMORY.md.

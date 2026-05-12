---
name: ui-story-planning
code: ui-story-planning
description: Break down a new Admin UI feature into implementable stories with acceptance criteria and Playwright test stubs — test-first, following the nebu TDD pipeline flow.
---

# UI Story Planning

## What Success Looks Like

The feature is broken into stories small enough to implement in one cycle. Each story has acceptance criteria that drive a failing Playwright test. The test is written before the template — test-first per the nebu pipeline TDD standard.

## TDD Flow for UI Stories

The nebu pipeline requires: **test first, then implementation.**

For UI stories:
1. Write the failing Playwright spec (what the user should be able to do)
2. The test fails because the template doesn't exist yet
3. Implement the template until the test passes
4. Run design-review and accessibility-audit

Every story brief must include Playwright test stubs ready to be made failing.

## Story Structure

For each UI story, produce:

```markdown
## Story: [Feature Name]

**As a** [user type — admin, operator]
**I want to** [action]
**So that** [outcome]

### Acceptance Criteria

- [ ] AC1: [visible behavior]
- [ ] AC2: [accessible behavior — e.g., keyboard navigation works]
- [ ] AC3: [error state behavior]

### Playwright Test Stubs (write first, make failing)

\`\`\`typescript
test('AC1: [brief description]', async ({ page }) => {
  await page.goto('/admin/[path]');
  // [What to assert — be specific about selectors and expected states]
  await expect(page.getByRole('[role]', { name: '[label]' })).toBeVisible();
});

test('AC2: keyboard navigation works', async ({ page }) => {
  // [Tab through interactive elements, verify focus order]
});

test('AC3: [error state]', async ({ page }) => {
  // [Trigger error, assert error message is associated with field]
});
\`\`\`

### Accessibility Requirements

[Which PRD accessibility requirements apply to this story specifically]

### Component Notes

[Which DaisyUI components to use; any existing templates to extend]
```

## Scope

Admin UI only. Go Templates + Tailwind + DaisyUI via `go:embed`. No Matrix client UI.

## Memory Integration

After planning: note any new component patterns that will be established by this story.

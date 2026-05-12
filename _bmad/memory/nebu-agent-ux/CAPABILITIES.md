# Capabilities

## Built-in

| Code | Name | Description | Source |
|------|------|-------------|--------|
| [accessibility-audit] | accessibility-audit | Audit Admin UI pages against PRD accessibility requirements. WCAG violations are blocking findings. Called by nebu-pipeline on ui:true stories. | `./references/accessibility-audit.md` |
| [design-review] | design-review | Review existing Admin UI templates or designs for accessibility compliance, DaisyUI consistency, and Tailwind best practices. Produces structured findings. | `./references/design-review.md` |
| [playwright-support] | playwright-support | Write or review Playwright specs for Admin UI flows. Real browser tests against the running stack — no mocks, no shortcuts. | `./references/playwright-support.md` |
| [template-implementation] | template-implementation | Write or update Go Templates for Admin UI features using Tailwind and DaisyUI. Accessibility-correct from the first line. | `./references/template-implementation.md` |
| [ui-story-planning] | ui-story-planning | Break down a new Admin UI feature into implementable stories with acceptance criteria and Playwright test stubs — test-first, following the nebu TDD pipeline flow. | `./references/ui-story-planning.md` |

## Learned

_Capabilities added by the owner over time. Prompts live in `capabilities/`._

| Code | Name | Description | Source | Added |
|------|------|-------------|--------|-------|

## How to Add a Capability

Tell me "I want you to be able to do X" and we'll create it together.
Load `references/capability-authoring.md` for the full creation framework.

## Tools

### Playwright MCP
Browser-level test execution and visual inspection of Admin UI.
Use for accessibility audits and E2E test validation.

### bmad-agent-ux-designer
Parent module for generic UX patterns. Consult for design decisions beyond Admin UI scope.

### User-Provided Tools
_Additional tools the owner has made available._

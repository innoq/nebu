# Capabilities

## Built-in

| Code | Name | Description | Source |
|------|------|-------------|--------|
| [consultation] | consultation | Ad-hoc security assessment of a specific code snippet, design decision, or implementation approach. Fast, focused, informal — not a full review. | `./references/consultation.md` |
| [epic-review] | epic-review | Mandatory epic-end security review against the full epic diff. Produces an HTML report saved to _bmad-output/implementation-artifacts/. Always runs — even at zero findings (audit trail). | `./references/epic-review.md` |
| [security-review] | security-review | Per-story adversarial security review of staged changes. CRITICAL/HIGH findings block the commit. Produces structured findings report. | `./references/security-review.md` |

## Learned

_Capabilities added by the owner over time. Prompts live in `capabilities/`._

| Code | Name | Description | Source | Added |
|------|------|-------------|--------|-------|

## How to Add a Capability

Tell me "I want you to check for X" and we'll create it together.
Load `references/capability-authoring.md` for the full creation framework.

## Tools

### User-Provided Tools
_MCP servers or services the owner has made available._

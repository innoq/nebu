# Capabilities

## Built-in

| Code | Name | Description | Source |
|------|------|-------------|--------|
| [compliance-review] | compliance-review | Review code, stories, PRs, or feature implementations for Matrix Client-Server API spec compliance. Produces structured findings with spec citations. | `./references/compliance-review.md` |
| [dev-support] | dev-support | Guide developers implementing Matrix Client-Server API features — correct approaches, field schemas, flow sequences, and common implementation traps. | `./references/dev-support.md` |
| [spec-lookup] | spec-lookup | Deep reference lookup for the Matrix Client-Server API — events, endpoints, fields, error codes, flows, and behavioral rules. | `./references/spec-lookup.md` |
| [sync-deep-dive] | sync-deep-dive | Complete Matrix Client-Server API v1.18 reference for GET /sync — request parameters, full response structure, all event placement rules, timeline semantics, incremental vs full sync, long-polling, filters, lazy loading, bundled aggregations, and stripped state. | `./references/sync.md` |
| [test-guidance] | test-guidance | Help TEA agents and developers design acceptance tests that verify Matrix Client-Server API spec compliance — both happy paths and spec-required error behaviors. | `./references/test-guidance.md` |

## Learned

_Capabilities added by the owner over time. Prompts live in `capabilities/`._

| Code | Name | Description | Source | Added |
|------|------|-------------|--------|-------|

## How to Add a Capability

Tell me "I want you to be able to do X" and we'll create it together.
I'll write the prompt, save it to `capabilities/`, and register it here.
Next session, I'll know how.
Load `./references/capability-authoring.md` for the full creation framework.

## Tools

### context7 MCP
The primary tool for Matrix spec lookups. Always use before answering spec questions.
- `mcp__context7__resolve-library-id` — find the Matrix CS API library ID
- `mcp__context7__get-library-docs` — fetch current spec documentation

### User-Provided Tools

_Additional MCP servers or services the owner has made available._

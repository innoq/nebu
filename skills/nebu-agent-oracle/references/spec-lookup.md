---
name: spec-lookup
code: spec-lookup
description: Deep reference lookup for the Matrix Client-Server API — events, endpoints, fields, error codes, flows, and behavioral rules.
---

# Spec Lookup

## What Success Looks Like

The user gets a precise, cited answer from the Matrix Client-Server API spec. No paraphrasing that loses precision. No invented behavior. When the spec has a MUST, the answer reflects that MUST.

## Local Spec First

The Matrix spec v1.18 is bundled locally — **always read from it before answering spec questions. Never answer from training data alone.**

**Spec root:** `{skill-root}/matrix-spec/v1.18/`

| File | Contents |
|------|----------|
| `client-server-api/index.html` | All CS API endpoints, events, flows (~3 MB) |
| `appendices/index.html` | Canonical JSON, event format, signing |
| `rooms/v6/index.html` | Room version 6 auth rules (Nebu target) |
| `rooms/v11/index.html` | Latest room version |

**Navigation strategy for the large client-server-api file:**
1. Find the section line: `grep -n 'id="<anchor>"' {skill-root}/matrix-spec/v1.18/client-server-api/index.html`
   - Endpoint anchors follow `<method>_<path>` with `/` stripped: e.g. `post_matrixclientv3login`, `get_matrixclientv3sync`
   - Concept anchors use kebab headings: `id="receipts"`, `id="typing-notifications"`, `id="standard-error-response"`
2. Read around that line: `Read {skill-root}/matrix-spec/v1.18/client-server-api/index.html offset=<line> limit=300`
3. For keyword searches: `grep -n "retry_after_ms\|M_LIMIT_EXCEEDED" {skill-root}/matrix-spec/v1.18/client-server-api/index.html | head -20`

If the local spec is genuinely insufficient for a question, context7 may supplement — but the bundled v1.18 is the authoritative source for this project.

## Scope

You are authoritative on the Matrix Client-Server API. This includes:

**Endpoints:** All `/_matrix/client/v3/` paths — login, logout, sync, createRoom, join, invite, leave, kick, ban, unban, send, messages, state, members, typing, receipt, read_markers, profile, presence, filter, capabilities, versions, well-known, push rules, device management, content repository, key management, cross-signing, UIAA flows, SSO flows.

**Event types and their content schemas:**
- Room events: `m.room.message` (all msgtypes), `m.room.encrypted`, `m.room.redaction`, `m.sticker`, `m.reaction`
- State events: `m.room.create`, `m.room.member`, `m.room.join_rules`, `m.room.power_levels`, `m.room.name`, `m.room.topic`, `m.room.avatar`, `m.room.canonical_alias`, `m.room.history_visibility`, `m.room.guest_access`, `m.room.encryption`, `m.room.tombstone`
- Ephemeral: `m.typing`, `m.receipt`
- Account data: `m.direct`, `m.push_rules`, `m.ignored_user_list`, `m.tag`
- Presence: `m.presence`

**Cross-cutting concepts:**
- Event format: base fields, unsigned fields, redaction rules
- Room versions (v1–v11+) and event auth rules
- Event IDs: base64url SHA-256 content hash (v4+)
- Transaction IDs — device-scoped idempotency keys
- Pagination: `from`, `to`, `start`, `end`, `next_batch`, `prev_batch`, `dir`
- Sync response structure — for full detail load `references/sync.md`
- Filter syntax
- Error format: `{"errcode": "M_*", "error": "human string"}` with correct HTTP status codes
- Rate limiting: 429 + `Retry-After` + `retry_after_ms`
- Authentication: Bearer token, token refresh, soft logout
- Power levels and defaults

## Answering Questions

Provide precise answers with spec section citations. State explicitly whether the spec uses MUST, SHOULD, or MAY. Include format constraints (enums, regex, length limits) when relevant.

For "what does X look like": give the JSON schema with fields labeled required/optional per spec.

For "how does X flow work": describe the request/response sequence with exact HTTP methods, paths, body fields, and response codes.

If the question falls outside the Client-Server API scope (federation, identity server, push gateway), say so clearly.

## Memory Integration

After answering, check: did this reveal a Nebu-specific decision or a spec quirk the project has encountered before? If so, note it in the session log.

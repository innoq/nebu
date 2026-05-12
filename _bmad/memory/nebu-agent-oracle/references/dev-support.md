---
name: dev-support
code: dev-support
description: Guide developers implementing Matrix Client-Server API features — correct approaches, field schemas, flow sequences, and common implementation traps.
---

# Dev Support

## What Success Looks Like

The developer has a clear, correct implementation path grounded in the spec. They know which fields are required, which error codes to return, what the spec requires vs. what is left to implementation, and where common mistakes are made. They do not need to re-read the spec themselves to verify the guidance.

## Context7 First

Use context7 to fetch current spec docs before giving implementation guidance. Check MEMORY.md first — has this been discussed before? Any Nebu-specific decisions already made for this endpoint?

## Approach

Answer implementation questions with:
1. The correct spec-required behavior (cite section)
2. The exact request/response shape (fields, types, HTTP status codes)
3. Common implementation traps for this endpoint/event/flow
4. What the spec explicitly leaves to implementation discretion

## High-Value Implementation Notes

The spec behaviors that most frequently trip up Matrix server implementors. Surface proactively when the topic arises:

**Sync (`GET /sync`)**
- `since` is optional on first call; required for incremental sync.
- `timeout` is milliseconds, long-poll. Return immediately if there is data.
- Timeline `limited: true` means a gap exists — `prev_batch` MUST be provided.
- `state` in `rooms.join` is the room state as-of the start of the timeline, not current state.
- `ephemeral` events (typing, receipts) are NOT in the timeline — they are in their own key.

**Message sending (`PUT /rooms/{roomId}/send/{eventType}/{txnId}`)**
- `txnId` is the client's idempotency key, scoped per device. Same `txnId` + same device → same `event_id`, no re-processing.
- Response: `{"event_id": "$..."}` only.
- Do NOT accept POST for send.

**Room creation (`POST /createRoom`)**
- `room_id` MUST be fully qualified including server name.
- `preset` events run before `initial_state`.
- `preset` values: `private_chat`, `public_chat`, `trusted_private_chat`.

**Login (`POST /login`)**
- Response MUST include `access_token`, `device_id`, `user_id`.
- `m.login.password` requires `identifier` object (type `m.id.user` with `user` field). Do NOT accept bare `user` field at top level.
- Wrong password → `M_FORBIDDEN` (403), not `M_UNKNOWN_TOKEN`.

**Presence (`PUT /_matrix/client/v3/presence/{userId}/status`)**
- Valid `presence` values: `online`, `offline`, `unavailable`.

**Typing (`PUT /rooms/{roomId}/typing/{userId}`)**
- `typing: false` MUST be accepted to stop notification.
- `timeout` (ms) required when `typing: true`.

**Receipts (`POST /rooms/{roomId}/receipt/{receiptType}/{eventId}`)**
- Only `m.read` and `m.read.private` are spec-defined types.
- Response: `{}`, HTTP 200.

**Error codes**
- Always: `{"errcode": "M_...", "error": "human readable"}`.
- 401: `M_MISSING_TOKEN`, `M_UNKNOWN_TOKEN`.
- 403: `M_FORBIDDEN`.
- 404: `M_NOT_FOUND`.
- 400: `M_BAD_JSON`, `M_UNRECOGNIZED`, `M_MISSING_PARAM`, `M_INVALID_PARAM`.
- 429: `M_LIMIT_EXCEEDED` with `retry_after_ms`.
- 500: `M_UNKNOWN`.

**Event IDs (room version 4+)**
- SHA-256 hash of canonical JSON, base64url-encoded (no padding), prefixed `$`.

## Memory Integration

After giving guidance: did this involve a Nebu-specific architecture decision (e.g., which Go layer vs. Elixir layer handles this)? Record it in the session log and update MEMORY.md if it's a settled decision.

---
name: dev-support
description: Guide developers implementing Matrix Client-Server API features — correct approaches, field schemas, flow sequences, and common implementation traps.
---

# Dev Support

## What Success Looks Like

The developer has a clear, correct implementation path grounded in the spec. They know which fields are required, which error codes to return, what the spec requires vs. what is left to implementation, and where common mistakes are made. They do not need to re-read the spec themselves to verify the guidance.

## Approach

Answer implementation questions with:
1. The correct spec-required behavior (cite section)
2. The exact request/response shape (fields, types, HTTP status codes)
3. Common implementation traps for this endpoint/event/flow
4. What the spec explicitly leaves to implementation discretion

## High-Value Implementation Notes

These are the spec behaviors that most frequently trip up Matrix server implementors. Surface proactively when the topic arises:

**Sync (`GET /sync`)**
- `since` is optional on first call; required for incremental sync. Never return a stale `next_batch`.
- `timeout` is milliseconds, long-poll. Return immediately if there is data; only block up to `timeout` if empty.
- `filter` can be a filter ID (stored filter) or a filter JSON object inline.
- Timeline `limited: true` means a gap exists — `prev_batch` MUST be provided so the client can backfill.
- `state` in `rooms.join` contains the room state as-of the start of the timeline, not current state.
- `ephemeral` events (typing, receipts) are NOT in the timeline — they are in their own key.
- `account_data` at the top level is global; `account_data` inside `rooms.join[roomId]` is per-room.

**Message sending (`PUT /rooms/{roomId}/send/{eventType}/{txnId}`)**
- `txnId` is the client's idempotency key, scoped per device. If the server receives the same `txnId` for the same device, it MUST return the same `event_id` without re-processing.
- Response is `{"event_id": "$..."}` — no other fields required.
- Do NOT accept POST for send. The spec requires PUT.

**Room creation (`POST /createRoom`)**
- `room_id` in response MUST be the fully qualified ID including server name.
- Initial state events in `initial_state` are applied after the built-in state events (create, join, power_levels, etc.) but `preset` events run before `initial_state`.
- `preset` values: `private_chat`, `public_chat`, `trusted_private_chat`.

**Login (`POST /login`)**
- Response MUST include `access_token`, `device_id`, `user_id`.
- `home_server` field in response is deprecated but many clients still check it.
- `m.login.password` type: `identifier` object required (type `m.id.user` with `user` field, or `m.id.thirdparty`, or `m.id.phone`). Do NOT accept bare `user` field at top level (deprecated).
- Wrong password → `M_FORBIDDEN` (403), not `M_UNKNOWN_TOKEN`.

**Presence (`PUT /_matrix/client/v3/presence/{userId}/status`)**
- Presence is set per user, not per device.
- Valid `presence` values: `online`, `offline`, `unavailable`.
- `status_msg` is optional.

**Typing (`PUT /rooms/{roomId}/typing/{userId}`)**
- `typing: false` MUST be accepted to stop typing notification.
- `timeout` field (ms) required when `typing: true`. Ignored when `typing: false`.

**Receipts (`POST /rooms/{roomId}/receipt/{receiptType}/{eventId}`)**
- Only `m.read` and `m.read.private` are spec-defined receipt types.
- Response is `{}` (empty object), HTTP 200.

**Error codes**
- Always return the error as `{"errcode": "M_...", "error": "human readable"}`.
- `errcode` uses the `M_` prefix namespace. Custom errors must use a namespace prefix.
- 401 for missing/invalid token (`M_MISSING_TOKEN`, `M_UNKNOWN_TOKEN`).
- 403 for authorization failures (`M_FORBIDDEN`).
- 404 for not found (`M_NOT_FOUND`).
- 400 for bad requests (`M_BAD_JSON`, `M_UNRECOGNIZED`, `M_MISSING_PARAM`, `M_INVALID_PARAM`).
- 429 for rate limiting (`M_LIMIT_EXCEEDED`) with `retry_after_ms` in body.
- 500 for server errors (`M_UNKNOWN`).

**Room membership and power levels**
- Default power level for all users: 0. Default required level for `events_default`: 0, `state_default`: 50, `ban`: 50, `kick`: 50, `invite`: 50, `redact`: 50.
- `m.room.power_levels` state event controls all of this — if the room has one, its values override defaults.
- Kick requires the kicker's PL ≥ kick level AND kicker's PL > target's PL.
- Ban same logic as kick but for the ban level.

**Event IDs (room version 4+)**
- SHA-256 hash of the event in canonical JSON, base64url-encoded (no padding), prefixed `$`.
- Do not generate sequential or opaque event IDs in room versions 4+.

**Rate limiting**
- Return 429 with `M_LIMIT_EXCEEDED` and `retry_after_ms` field.
- `Retry-After` HTTP header is also recommended.

When a developer asks about something outside the CS API scope (federation, push gateways, application services), name the correct spec to consult rather than speculating.

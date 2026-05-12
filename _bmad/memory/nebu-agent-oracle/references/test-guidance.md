---
name: test-guidance
code: test-guidance
description: Help TEA agents and developers design acceptance tests that verify Matrix Client-Server API spec compliance — both happy paths and spec-required error behaviors.
---

# Test Guidance

## What Success Looks Like

The test covers the spec behavior — not just the happy path, but the error conditions and edge cases the spec explicitly defines. Every MUST that is testable produces an assertion. Tests are grounded in what a real Matrix client would send and what the spec requires the server to return.

## Approach

For any Matrix feature, identify:
1. The happy path the spec defines
2. Every MUST error condition for this endpoint
3. SHOULD behaviors worth verifying
4. Idempotency where the spec requires it
5. Spec-defined edge cases

Check MEMORY.md: are there known Nebu-specific behaviors or decisions that should be reflected in the tests?

## Test Patterns by Feature Area

### Sync
- First sync (no `since`) → full state + `next_batch`
- Incremental sync with previous `next_batch` → only new events
- `limited: true` → accompanied by `prev_batch`
- `timeout=0` → returns immediately
- Ephemeral events in `rooms.join[roomId].ephemeral`, not timeline

### Message Send
- PUT `/{txnId}` → `{"event_id": "$..."}` 200
- Same `txnId` same device → same `event_id`, no duplicate event
- Missing `msgtype` → 400
- Sender lacks power → 403 `M_FORBIDDEN`

### Login
- Valid credentials → 200 with `access_token`, `device_id`, `user_id`
- Wrong password → 403 `M_FORBIDDEN`
- Unknown user → 403 `M_FORBIDDEN` (do NOT distinguish — timing attack prevention)
- Missing `type` → 400 `M_BAD_JSON`

### Room Creation
- No body → 200 with fully qualified `room_id`
- Creator automatically joined
- `preset: private_chat` → `join_rules: invite`, `history_visibility: shared`
- `initial_state` events applied

### Membership
- Join invited room → `membership: join` in `m.room.member`
- Leave → `membership: leave`
- Kick by sufficient PL → 200; target membership `leave`
- Kick by insufficient PL → 403 `M_FORBIDDEN`
- Ban → `membership: ban`; banned user cannot rejoin

### Typing
- `typing: true` with `timeout` → 200 `{}`
- `typing: false` → 200 `{}` stops notification
- Typing event in sync `ephemeral` for other room members
- Missing `timeout` when `typing: true` → 400

### Receipts
- `m.read` → 200 `{}`
- Receipt in sync `ephemeral` for room members
- Unknown receipt type → 400 `M_UNRECOGNIZED`

### Presence
- `presence: online/offline/unavailable` → 200 `{}`
- GET presence → correct `presence`, `last_active_ago`
- Invalid value → 400 `M_BAD_JSON`

## Writing Scenarios

When writing Gherkin or ExUnit scenarios for Matrix features:
1. Use exact spec-defined field names in Given/When/Then
2. Assert HTTP status codes explicitly
3. Assert `errcode` values from error responses
4. For sync: assert structural position (timeline vs. state vs. ephemeral)
5. For idempotency: send identical request twice, verify identical `event_id` and no duplicate

Flag any acceptance criterion not testable via the CS API — those need in-process testing or are non-observable at the API level.

## Memory Integration

After test design: did we discover untested spec behaviors worth flagging? Any spec areas where Nebu doesn't have coverage yet? Note in session log.

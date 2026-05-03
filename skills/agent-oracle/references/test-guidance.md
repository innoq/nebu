---
name: test-guidance
description: Help TEA agents and developers design acceptance tests that verify Matrix Client-Server API v1.18 spec compliance — both happy paths and spec-required error behaviors.
---

# Test Guidance

## What Success Looks Like

The test covers the spec behavior — not just the happy path, but the error conditions and edge cases that the spec explicitly defines. Every MUST in the spec that is testable produces a test assertion. Tests are grounded in what a real Matrix client would send and what the spec says the server MUST return.

## Approach

For any Matrix feature, identify:
1. The happy path the spec defines (correct request → correct response)
2. Every MUST error condition the spec defines for this endpoint (wrong input → specific errcode + HTTP status)
3. The SHOULD behaviors worth verifying (optional but important)
4. Idempotency where the spec requires it (txnId, duplicate event detection)
5. The spec-defined edge cases (empty room, first sync, etc.)

## Test Patterns by Feature Area

### Sync Tests

Must verify:
- First sync (no `since`) returns full state + `next_batch`
- Incremental sync with `since=<previous next_batch>` returns only new events
- `limited: true` in timeline is accompanied by a `prev_batch` token
- `timeout=0` returns immediately even with no events
- Ephemeral events (typing, receipts) appear in `rooms.join[roomId].ephemeral`, not timeline
- Missing `since` on incremental (token not found) → `M_UNKNOWN` or appropriate error

### Message Send Tests

Must verify:
- PUT `/{txnId}` returns `{"event_id": "$..."}` with 200
- Same `txnId` from same device returns same `event_id` without duplicating the event (idempotency)
- Missing required `msgtype` in content → 400 `M_BAD_JSON` or similar
- Sender lacks power to send → 403 `M_FORBIDDEN`

### Login Tests

Must verify:
- Valid credentials → 200 with `access_token`, `device_id`, `user_id`
- Wrong password → 403 `M_FORBIDDEN`
- Unknown user → 403 `M_FORBIDDEN` (spec: do not distinguish user-not-found from wrong-password — timing attack prevention)
- Missing `type` field → 400 `M_BAD_JSON`
- Invalid login type → 400 `M_UNRECOGNIZED`

### Room Creation Tests

Must verify:
- `POST /createRoom` with no body → 200 with `room_id`
- `room_id` in response is fully qualified (`!opaque:server`)
- Creator is automatically joined
- `preset: private_chat` → `join_rules: invite`, `history_visibility: shared`
- `preset: public_chat` → `join_rules: public`, `history_visibility: shared`
- `initial_state` events applied

### Membership Tests

Must verify:
- Join invited room → user in `m.room.member` with `membership: join`
- Leave → `membership: leave`
- Kick by sufficient PL user → target membership `leave`, 200 response
- Kick by insufficient PL → 403 `M_FORBIDDEN`
- Ban → `membership: ban`; banned user cannot rejoin

### Typing Tests

Must verify:
- PUT `typing: true` with `timeout` → 200 `{}`
- PUT `typing: false` → 200 `{}` (stops notification)
- Typing event appears in sync `ephemeral` for other users in room
- Missing `timeout` when `typing: true` → 400 (spec requires it)

### Receipt Tests

Must verify:
- POST receipt with `m.read` → 200 `{}`
- Receipt event appears in sync `ephemeral` for room members
- POST with unknown receipt type → 400 `M_UNRECOGNIZED`

### Presence Tests

Must verify:
- PUT `presence: online` → 200 `{}`
- PUT `presence: offline` → 200 `{}`
- GET presence for a user → correct `presence`, `last_active_ago` fields
- Invalid `presence` value → 400 `M_BAD_JSON`

### Profile Tests

Must verify:
- GET profile returns `displayname` and `avatar_url` (both optional if unset)
- PUT displayname → 200 `{}`
- GET after PUT returns the new value
- PUT another user's profile → 403 `M_FORBIDDEN`

## Writing Scenarios

When writing Gherkin or ExUnit test scenarios for Matrix features, ensure:

1. Use the exact spec-defined field names in Given/When/Then — not display names or abbreviations.
2. Assert HTTP status codes explicitly, not just "success" or "failure."
3. Assert `errcode` values (e.g. `M_FORBIDDEN`, `M_BAD_JSON`) from error responses.
4. For sync: always assert the structural position of events (timeline vs. state vs. ephemeral vs. account_data).
5. For idempotency: the test MUST send the identical request twice and verify identical `event_id` and no duplicate in the room timeline.

Flag any acceptance criterion that is not testable via the CS API — these require either in-process testing (GenServer state checks) or are non-observable at the API level. The TEA / Dev should decide the test layer.

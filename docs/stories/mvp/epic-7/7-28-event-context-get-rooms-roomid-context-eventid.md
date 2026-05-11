---
id: 7-28
type: feature
security_review: not-needed
created: 2026-04-30
---

# Story 7.28: Event Context — GET /rooms/{roomId}/context/{eventId}

Status: ready-for-dev

## Story

As an end-user,
I want to load the surrounding events around a specific message (e.g. when jumping to a notification or a search result),
so that my Matrix client can display the message in its conversational context without fetching the entire room history.

## Context / Background

The event context endpoint is used by clients for "jump to message" deep-links and notification navigation. It returns the target event, up to `limit` events before it, up to `limit` events after it, and the relevant room state at the time of the target event.

Pagination tokens `start` and `end` are compatible with `GET /rooms/{roomId}/messages` (same token format), so clients can extend the view in either direction using the messages endpoint.

The existing `messages.go` handler already implements paginated event retrieval and its pagination token logic. The context handler can share the underlying Room GenServer query mechanism.

## Acceptance Criteria

1. `GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}?limit=N` returns a JSON object with:
   - `event`: the full timeline event for `eventId`
   - `events_before`: up to `limit` events older than `eventId` (chronological order, newest last)
   - `events_after`: up to `limit` events newer than `eventId` (chronological order, oldest first)
   - `start`: pagination token usable as `to` in `GET /messages`
   - `end`: pagination token usable as `from` in `GET /messages`
   - `state`: array of relevant state events at the time of `eventId` (at minimum: `m.room.member` for the sender, `m.room.power_levels`, `m.room.name`)

2. `limit` query param controls events before AND after independently. Default: 10. Maximum: 100. Values above 100 are silently clamped to 100.

3. If `eventId` does not exist in `roomId`, returns HTTP 404 with `errcode` = `M_NOT_FOUND`.

4. If the authenticated user is not a member of `roomId`, returns HTTP 403 with `errcode` = `M_FORBIDDEN`. Membership check uses the same guard as `GET /rooms/{roomId}/messages`.

5. `events_before` and `events_after` may each contain fewer than `limit` events if the target event is near the start or end of the timeline — this is not an error.

6. The `start` and `end` tokens, when used in `GET /rooms/{roomId}/messages?from=end` and `GET /rooms/{roomId}/messages?to=start`, produce contiguous results with no gaps or overlaps relative to the context window.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Returns target event with surrounding context] — Godog (`gateway/features/event_context.feature`)
   - Given: authenticated user `@alice:nebu.test` is a member of `!room1:nebu.test`; room has 10 messages; target is the 5th message `$evt5`
   - When: GET `/_matrix/client/v3/rooms/!room1:nebu.test/context/$evt5?limit=3`
   - Then: HTTP 200; `event.event_id` = `$evt5`; `events_before` has 3 entries (events 4, 3, 2); `events_after` has 3 entries (events 6, 7, 8)

2. [Context near start of timeline — fewer events_before] — Godog
   - Given: target is the 2nd message `$evt2` in a room with 10 messages
   - When: GET `/_matrix/client/v3/rooms/!room1:nebu.test/context/$evt2?limit=5`
   - Then: `events_before` has 1 entry (event 1); `events_after` has 5 entries; no error

3. [M_NOT_FOUND for unknown eventId] — Godog
   - Given: authenticated user in `!room1:nebu.test`
   - When: GET `/_matrix/client/v3/rooms/!room1:nebu.test/context/$nonexistent`
   - Then: HTTP 404, `errcode` = `M_NOT_FOUND`

4. [M_FORBIDDEN for non-member] — Godog
   - Given: authenticated user `@bob:nebu.test` who is NOT a member of `!room1:nebu.test`
   - When: GET `/_matrix/client/v3/rooms/!room1:nebu.test/context/$evt5`
   - Then: HTTP 403, `errcode` = `M_FORBIDDEN`

5. [limit clamped to 100] — Go httptest (`gateway/internal/matrix/messages_test.go`)
   - Given: authenticated room member, valid event
   - When: GET `/context/$evt?limit=999`
   - Then: HTTP 200; `events_before` and `events_after` each have at most 100 entries; no error

6. [Pagination tokens are compatible with /messages] — Godog
   - Given: context response for `$evt5` with `start` and `end` tokens
   - When: GET `/rooms/!room1:nebu.test/messages?from=<end>&dir=f&limit=5`
   - Then: first returned event is the one immediately after `events_after`'s last entry (no gap or overlap)

7. [state array includes sender membership and power levels] — Go httptest
   - Given: valid context request for `$evt5`
   - When: inspect `state` array in response
   - Then: contains at least one `m.room.member` event (sender of `$evt5`) and one `m.room.power_levels` event

## Implementation Notes

**Handler location:** Add `GetEventContextHandler` to `gateway/internal/matrix/messages.go` (existing file, consistent with messages pagination logic).

**gRPC proto additions** (`proto/core.proto`):
```proto
rpc GetEventContext(GetEventContextRequest) returns (GetEventContextResponse);
// Request: room_id, event_id, limit (int32)
// Response: event (Event), events_before ([]Event), events_after ([]Event),
//           state ([]Event), start_token (string), end_token (string)
```

The Elixir gRPC handler in `room_manager` executes:
1. Locate `event_id` in the room's event log (error if absent).
2. Fetch `limit` events before and `limit` events after using existing messages pagination query.
3. Fetch state snapshot: query the latest `m.room.member` (for the event sender), `m.room.power_levels`, and `m.room.name` events with `origin_server_ts <= event.origin_server_ts`.
4. Construct `start_token` (before the window) and `end_token` (after the window) using the same token format as `GetMessages`.

**Membership guard** — reuse the existing `assertRoomMember(ctx, roomId, userId)` helper already used in `messages.go` and `members.go`.

**Route registration** in `gateway/cmd/gateway/main.go`:
```
GET /_matrix/client/v3/rooms/{roomId}/context/{eventId} → jwtMiddleware(GetEventContextHandler)
```

**limit parsing** — share the existing `parseLimitParam(r, defaultVal, maxVal int)` helper from `messages.go`, or extract it into `gateway/internal/matrix/validate.go` if not already there.

## Tasks

- [ ] Write failing Godog scenarios in `gateway/features/event_context.feature`
- [ ] Write failing Go httptest additions in `gateway/internal/matrix/messages_test.go`
- [ ] Extend `proto/core.proto` with `GetEventContext`; run `make proto`
- [ ] Implement Elixir gRPC handler in `room_manager` (locate event, fetch before/after, state snapshot, tokens)
- [ ] Implement `GetEventContextHandler` in `gateway/internal/matrix/messages.go`
- [ ] Register route in `main.go`
- [ ] Run `make test-unit-go` + `make test-unit-elixir` — all pass
- [ ] Run `make test-integration` — Godog scenarios green

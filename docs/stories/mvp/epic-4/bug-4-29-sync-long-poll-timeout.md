---
story_id: 4-29f
title: "Bug: Sync Long-Poll Times Out After 30s When :pg Broadcast Not Delivered"
type: bug
severity: high
epic: 4
status: ready-for-dev
security_review: not-needed
created: 2026-04-29
---

## Summary

When a new message event is broadcast via `:pg` (PostgreSQL Process Groups), the sync long-poll sometimes sleeps for the full 30-second timeout instead of waking up within seconds. Multiple E2E tests use 35-second timeouts as a workaround, indicating the `:pg` broadcast fix is not complete or has edge cases.

## Evidence from E2E Tests

### Test 1: `e2e/tests/features/room/invites.spec.ts` (Invite Delivery)

```typescript
// Wait up to 35 s for the sync response that includes rooms.invite[roomId].
// Invites don't trigger a :pg broadcast, so the long-poll runs its full 30 s.
// We wait up to 35 s for the sync response...
await Promise.race([
  syncWithInvitePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Invite not delivered in sync within 35 s')), 35_000)
  ),
]);
```

**Comment analysis**: "Invites don't trigger a :pg broadcast, so the long-poll runs its full 30 s."

### Test 2: `e2e/tests/features/room/room-lifecycle.spec.ts` (Leave Delivery)

```typescript
// Primary assertion: sync must deliver rooms.leave[roomId] within 10 s.
// With :pg broadcast fix in emit_membership_event: sync wakes up in ~3 s → PASS.
// Without the fix: long-poll sleeps 30 s → Promise.race timeout fires → FAIL.
const syncResp = await Promise.race([
  syncWithLeavePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Sync did not deliver rooms.leave within 10 s')), 10_000)
  ),
]);
```

**Comment analysis**: The test expects sync to wake in ~3 seconds with the fix, but falls back to 30-second long-poll without it.

## Root Cause Analysis

### Current Behavior

1. **Event Dispatcher**: `event_dispatcher` broadcasts `:pg` events via `pglisten`
2. **Sync Handler**: `sync_handler` implements long-poll with 30-second timeout
3. **Problem**: When `:pg` notification arrives, the sync long-poll doesn't always wake immediately

### Hypothesized Causes

1. **Incomplete :pg broadcast implementation**: `emit_membership_event` may not broadcast for all event types
2. **Race condition**: Event broadcast and sync notification listener registration may have a race
3. **Missing event types**: Some event types (e.g., invites) don't trigger `:pg` broadcast per comment
4. **Listener not registered**: Sync long-poll's `pglisten` listener may not be attached to the correct channel

## Impact

- **User-facing**: Slow room updates (30-second delay) when new messages/invites arrive
- **Developer-facing**: E2E tests use 35-second timeouts as workaround
- **Severity**: High - degrades UX significantly, indicates incomplete fix from previous story

## Acceptance Criteria

### AC1: Sync long-poll wakes within 5 seconds for all event types
- `rooms.invite` events wake sync within 5 seconds
- `rooms.leave` events wake sync within 5 seconds
- `m.room.message` events wake sync within 5 seconds
- All membership change events wake sync within 5 seconds

### AC2: No 30-second long-poll timeouts in happy path
- When events are broadcast, sync returns immediately (< 5s)
- Long-poll only sleeps full 30 seconds when no events exist (expected behavior)

### AC3: E2E tests pass with 10-second timeout
- `invites.spec.ts` Test 1 passes with 10-second timeout (currently 35s)
- `room-lifecycle.spec.ts` Test 2 passes with 10-second timeout (currently 10s race)

## Implementation Notes

### Files to Investigate

1. `core/apps/event_dispatcher/` - Event broadcast logic
2. `core/apps/event_dispatcher/pglisten.go` - PostgreSQL listener implementation
3. `gateway/internal/matrix/sync_handler.go` - Long-poll implementation
4. `core/apps/room_manager/` - Membership event emission

### Questions to Answer

1. Does `emit_membership_event` broadcast for ALL event types (invite, leave, join, kick)?
2. Is the `pglisten` listener registered before the long-poll starts?
3. Are there any code paths where events are emitted without broadcasting?
4. Is there a race between event emission and listener registration?

### Debug Steps

1. Add logging to `pglisten` when notifications arrive
2. Add logging to `emit_membership_event` when events are broadcast
3. Measure time from event emission to sync response in test environment
4. Check if `pglisten` channel name matches in both sender and receiver

## Test Coverage

### E2E Tests (to be updated)

```typescript
// Current: 35-second timeout (workaround)
await Promise.race([
  syncWithInvitePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Invite not delivered in sync within 35 s')), 35_000)
  ),
]);

// Expected: 10-second timeout (after fix)
await Promise.race([
  syncWithInvitePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Invite not delivered in sync within 10 s')), 10_000)
  ),
]);
```

### Integration Test (to be added)

```go
// gateway/test/integration/sync_long_poll_test.go
func TestSyncWakesOnPgNotification(t *testing.T) {
  // Create room, send message
  // Measure time from message send to sync response
  // Assert: < 5 seconds
}
```

## Related Stories

- **Story 4-29**: Room lifecycle (invite, leave, join)
- **Story 5-1**: GET /filter (sync integration)
- **Previous fix**: `emit_membership_event` :pg broadcast fix (mentioned in test comments)

## References

- E2E Test: `e2e/tests/features/room/invites.spec.ts` line 74-101
- E2E Test: `e2e/tests/features/room/room-lifecycle.spec.ts` line 91-124
- Matrix Spec: [Sync API](https://spec.matrix.org/v1.3/client-server-api/#syncing)

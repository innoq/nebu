---
story_id: 8-10a
title: "Pre-Release Bug Fixes: Invite :pg Notification + Profile Upsert on Login + E2E Coverage"
type: bug
severity: high
epic: 8
status: ready-for-dev
security_review: optional
created: 2026-04-29
sources:
  - docs/stories/bug-4-29-sync-long-poll-timeout.md
  - docs/stories/bug-5-29e-dm-creation-hangs.md
  - docs/stories/feature-5-admin-ui-bootstrap-e2e.md
  - docs/stories/consolidated-5-29-multi-fix.md
---

## Context

Three bugs discovered during E2E testing that block the initial public push (Story 8-10):

1. **Invite long-poll timeout** — Sync long-poll sleeps 30s on new invites because
   `invite_user/2` in the Elixir EventDispatcher inserts the DB row but never sends
   a `:pg` broadcast to the invitee's waiting sync task.

2. **Profile 404 for bootstrap users** — `GET /profile/{userId}` returns 404 after
   OIDC login because `validate_token/2` calls `TokenValidator.validate` (which writes
   to `users` + `user_keys`) but never calls `profile_db_module().upsert_profile`.
   The `profiles` table row is never created at provisioning time.

3. **keys/query stub** — Already fixed in Story 5-29e (`keys_query.go` fully
   implemented and wired in `main.go`). No action needed.

## Acceptance Criteria

### AC1: Invite delivered in sync within 10 seconds
- `rooms.invite[roomId]` appears in sync response within 10 s of `POST /invite`
- Long-poll wakes up on `{:new_invite, room_id}` from `:pg` user group
- `invites.spec.ts` Test 1 passes with 10 s timeout (was 35 s)

### AC2: Profile lookup returns 200 for all OIDC-provisioned users
- `GET /_matrix/client/v3/profile/@alex:localhost` returns 200 OK after OIDC login
- Response includes `displayname` from OIDC `preferred_username`
- `dm_create_bug_5_29e.spec.ts` AC2 test passes

### AC3: DM creation flow completes
- `POST /createRoom` with `is_direct=true` returns 200 OK
- No "Profile not found" warning in Element Web
- `dm_create_bug_5_29e.spec.ts` AC4 test passes

### AC4: Elixir unit tests pass for invite notification
- Unit test: `invite_user` broadcasts `{:new_invite, room_id}` to `"user:#{invitee}"`
- Unit test: `do_incremental_sync` wakes on `{:new_invite, _}` and returns delta

### AC5: Elixir unit tests pass for profile upsert
- Unit test: `validate_token` calls `profile_db_module().upsert_profile` with displayname
- Unit test: profile upsert failure does NOT block login (non-fatal)
- Unit test: empty display_name becomes `nil` in upsert call (preserves existing row via COALESCE)
- Note: read-back correctness ("GET /profile returns 200") is covered at E2E level by
  `dm_create_bug_5_29e.spec.ts` AC2-a/AC2-b — no separate Elixir unit test for DB read-back

## Implementation Notes

### Fix 1: Invite :pg notification
**File**: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`

In `invite_user/2`, after `db_module_invite().insert_invitation(:ok)`:
```elixir
# Notify invitee's sync task (if long-polling) via user-level :pg group
:pg.get_local_members("user:#{invitee}")
|> Enum.each(&send(&1, {:new_invite, room_id}))
```

In `do_incremental_sync/3`, add user-level subscription:
```elixir
# Subscribe to user-level :pg group for invite notifications
:pg.join("user:#{user_id}", self())

# In receive loop, handle {:new_invite, _room_id}:
{:new_invite, _room_id} ->
  Process.cancel_timer(timer_ref)
  flush_long_poll_timeout()
  []  # Return empty delta — Go gateway's buildInviteRooms queries DB for invite data
```

Cleanup: leave `"user:#{user_id}"` group before returning (in both paths).

### Fix 2: Profile upsert on login
**File**: `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`

In `validate_token/2`, after `{:ok, user} ->`:
```elixir
# Upsert profile row so GET /profile/{userId} returns 200 (Bug 2a)
# Non-fatal: profile upsert failure must not block login
display_name_for_profile = if request.display_name == "", do: nil, else: request.display_name
case profile_db_module().upsert_profile(user_id, display_name_for_profile, nil) do
  :ok -> :ok
  {:error, reason} ->
    Logger.warning("validate_token: profile upsert failed for #{user_id}: #{inspect(reason)}")
end
```

### Migration Check
The `profiles` table already exists (created in Story 4-18). No new migration needed.

### E2E test fixes
After fixing the invite notification:
- `e2e/tests/features/room/invites.spec.ts` Test 1: reduce timeout from 35_000 to 10_000

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **[ExUnit: invite_user broadcasts to user pg group]** — ExUnit
   - Given: FakeInviteDB accepts the insert
   - When: invite_user request for room_id + invitee
   - Then: `:pg.get_local_members("user:#{invitee}")` receives `{:new_invite, room_id}`

2. **[ExUnit: validate_token calls profile upsert]** — ExUnit
   - Given: FakeProfileDB spy records calls
   - When: validate_token for a newly provisioned user with display_name "alex"
   - Then: `upsert_profile(user_id, "alex", nil)` is called exactly once

3. **[E2E: invite delivered in sync within 10 s]** — Playwright (invites.spec.ts)
   - Given: alex is long-polling sync (30 s timeout)
   - When: marie sends invite to alex
   - Then: alex's sync returns `rooms.invite[roomId]` within 10 s

4. **[E2E: GET /profile returns 200 after login]** — Playwright (dm_create_bug_5_29e.spec.ts)
   - Given: alex logged in via OIDC for the first time
   - When: GET /_matrix/client/v3/profile/@alex:localhost
   - Then: 200 OK with displayname field

## Related Stories

- **Story 4-29**: Room lifecycle (invite was partially fixed here but invite notification missed)
- **Story 5-29e**: DM creation bugs (keys/query fixed, profile 404 was missed)
- **Story 8-10**: Initial Public Push (blocked until this story is done)

---
status: ready-for-dev
epic: 9
story: 19
security_review: not-needed
---

# Story 9.19: Sync Gap Fixes — GAP-JOIN-PUBLIC, GAP-LEAVE-ONCE, GAP-FORGET

Status: ready-for-dev

## Story

As a Matrix client (Element Web),
I want sync to deliver join/leave/forget events correctly and promptly,
So that room state is consistent without spinning or stale tiles.

**Size:** M

---

## Background

Three confirmed sync gap bugs from `tmp/sync-issues.md` (agent-oracle analysis 2026-05-06).
The acceptance tests are pre-existing (red phase) in `e2e/tests/features/room/join_leave_sync.spec.ts`.

### GAP-JOIN-PUBLIC (MUST — §9.2)
After `POST /join/{roomIdOrAlias}` for a public room (no prior invite), sync must deliver
`rooms.join[roomId]` within 10 s. Currently the long-poll hangs for 30 s because the sync
task only subscribes to `:pg` groups for rooms already in `get_rooms_for_user` +
`get_pending_invite_rooms_for_user`. A fresh public-room join has neither → the
`:pg` broadcast from `emit_membership_event` goes to `room:#{roomId}` but no sync task
is subscribed → broadcast lost.

**Fix**: In `join_room/2`, after successful `:ok` from `Room.Server.join`, broadcast
`{:new_join, room_id}` to all pids in `"user:#{user_id}"` `:pg` group. In
`do_incremental_sync` receive block, handle `{:new_join, room_id}` analogously to
`{:new_invite, room_id}` — cancel timer and re-query with the new room_id added.

### GAP-LEAVE-ONCE (MUST — §6.3)
After the first incremental sync delivers `rooms.leave[roomId]`, the NEXT incremental
sync with a fresh since-token SHOULD NOT include the same left room again.
Currently `buildLeaveRooms` queries `left_at IS NOT NULL` with no time filter → every
sync returns ALL ever-left rooms → Element Web re-processes the leave on every cycle.

**Fix**: Pass the `updated_at` timestamp from `sync_tokens` (milliseconds since epoch,
BIGINT) to `buildLeaveRooms`. This timestamp is the wall-clock time of the PREVIOUS
sync response for this user. Filter: `AND left_at > $sinceMs` and `AND rejected_at > $sinceMs`.
For initial sync (no since_token): pass `sinceMs = 0` so all left rooms are included.

### GAP-FORGET (MUST — §11.3)
After `POST /rooms/{roomId}/forget`, the room must not appear in any subsequent sync.
MVP implementation is a no-op. This story implements it properly:
1. New migration `000040_forgotten_rooms.up.sql` — table `forgotten_rooms (user_id, room_id, forgotten_at_ms BIGINT)`
2. Go `PostForgetRoom` handler inserts into `forgotten_rooms` after successful gRPC precondition check
3. `buildLeaveRooms` and `buildJoinRooms` exclude forgotten rooms

---

## Acceptance Criteria

**AC1 — GAP-JOIN-PUBLIC: public room join delivers rooms.join within 10 s:**
After `POST /join/{roomId}` for a public room (no prior invite), the long-polling sync
task delivers `rooms.join[roomId]` within 10 s. The root fix is in `join_room/2` in
`server.ex`: broadcast `{:new_join, room_id}` to `user:#{user_id}` `:pg` group after
a successful `Room.Server.join`. `do_incremental_sync` receives this signal and
re-queries with the new room.

**AC2 — GAP-LEAVE-ONCE: left rooms do not repeat in subsequent syncs:**
After the first incremental sync delivers `rooms.leave[roomId]`, a subsequent sync with
a fresh since-token must NOT include that room again. Implemented by filtering
`left_at > sinceMs` and `rejected_at > sinceMs` in `buildLeaveRooms`, where
`sinceMs` is queried from `sync_tokens.updated_at` for the user.

**AC3 — GAP-FORGET: forgotten room absent from subsequent syncs:**
After `POST /forget` (post-leave), subsequent syncs must not include the room in
`rooms.join`, `rooms.leave`, or `rooms.invite`.
- New table `forgotten_rooms` stores (user_id, room_id, forgotten_at_ms).
- `PostForgetRoom` in Go inserts into `forgotten_rooms` after successful gRPC call.
- `buildLeaveRooms` excludes rooms present in `forgotten_rooms` for the user.

**AC4 — GAP-JOIN-INVITE regression guard remains green:**
The existing healthy-path test (`GAP-JOIN-INVITE`) must continue to pass. No regression
to the invite-accept `:pg` broadcast path.

**AC5 — GAP-LEAVE-UI regression guard remains green:**
The existing leave UI test must continue to pass. The `buildLeaveRooms` time-filter must
NOT break the immediate delivery of rooms.leave after a leave (the first sync after leave
has `left_at > 0` which is always true since sinceMs is from a previous sync).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

The Playwright E2E tests are pre-existing in red phase:
`e2e/tests/features/room/join_leave_sync.spec.ts`

1. **`[P0] GAP-JOIN-PUBLIC`** — Playwright E2E
   - Given: alex is logged in, marie created a public room
   - When: alex POSTs /join/{roomId} (no prior invite)
   - Then: sync delivers rooms.join[roomId] within 10 s

2. **`[P0] GAP-LEAVE-ONCE`** — Playwright E2E
   - Given: alex has left a room, first sync delivered rooms.leave
   - When: second incremental sync with fresh since-token
   - Then: rooms.leave SHOULD NOT include the already-processed room

3. **`[P0] GAP-FORGET`** — Playwright E2E
   - Given: alex has left then forgotten a room
   - When: incremental sync after POST /forget
   - Then: room not in rooms.join, rooms.leave, or rooms.invite

4. **Unit test — `TestBuildLeaveRooms_SinceFilter`** — Go (sync_test.go)
   - Given: user left room at time T, sinceMs = T+1
   - When: buildLeaveRooms called
   - Then: room NOT in result (left before since)

5. **Unit test — `TestBuildLeaveRooms_ForgottenExcluded`** — Go (sync_test.go)
   - Given: user left room AND has entry in forgotten_rooms
   - When: buildLeaveRooms called
   - Then: room NOT in result

6. **ExUnit test — `test "join_room/2 broadcasts {:new_join} to user pg group"`** — Elixir
   - Given: a mock :pg group for user, Room.Server.join returns :ok
   - When: join_room gRPC handler is called
   - Then: {:new_join, room_id} is received by the subscribed pid

---

## Technical Implementation Plan

### Files to create

| File | Change |
|---|---|
| `gateway/migrations/000040_forgotten_rooms.up.sql` | NEW — CREATE TABLE forgotten_rooms |
| `gateway/migrations/000040_forgotten_rooms.down.sql` | NEW — DROP TABLE forgotten_rooms |

### Files to modify

| File | Change |
|---|---|
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | GAP-JOIN-PUBLIC: broadcast in join_room/2 + receive handler in do_incremental_sync |
| `gateway/internal/matrix/sync.go` | GAP-LEAVE-ONCE: buildLeaveRooms signature + sinceMs filter + forgotten exclusion |
| `gateway/internal/matrix/room_moderation.go` | GAP-FORGET: add db *sql.DB to ModerationHandler; PostForgetRoom inserts into forgotten_rooms |
| `gateway/cmd/gateway/main.go` | Pass db to ModerationConfig |
| `gateway/internal/matrix/sync_test.go` | Add unit tests for since-filter and forgotten exclusion |
| `core/apps/event_dispatcher/test/nebu/event_dispatcher/join_room_test.exs` | Add ExUnit test for {:new_join} broadcast |

### Step 1 — Migration: forgotten_rooms table

```sql
-- gateway/migrations/000040_forgotten_rooms.up.sql
CREATE TABLE forgotten_rooms (
    user_id       TEXT    NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    room_id       TEXT    NOT NULL,
    forgotten_at_ms BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1000)::BIGINT,
    PRIMARY KEY (user_id, room_id)
);
```

```sql
-- gateway/migrations/000040_forgotten_rooms.down.sql
DROP TABLE IF EXISTS forgotten_rooms;
```

### Step 2 — Elixir: GAP-JOIN-PUBLIC fix in server.ex

In `join_room/2`, add after `Room.Server.join` returns `:ok` (before `db_module_invite().accept_invitation`):

```elixir
# Wake user's long-polling sync task immediately (mirrors invite_user/2 pattern).
# Without this, the public-room join :pg broadcast goes to room:#{room_id} but
# no sync task is subscribed to that group for a fresh join → 30 s spinner.
:pg.get_local_members("user:#{user_id}")
|> Enum.each(&send(&1, {:new_join, room_id}))
```

In `do_incremental_sync/3`, in the `receive` block (after `{:new_invite, _room_id}` handler):

```elixir
{:new_join, new_room_id} ->
  # User joined a public room — add it to the subscription set and re-query.
  Process.cancel_timer(timer_ref)
  flush_long_poll_timeout()
  :pg.join("room:#{new_room_id}", self())
  fetch_delta_rooms(Enum.uniq([new_room_id | room_ids]), last_event_id)
```

### Step 3 — Go: GAP-LEAVE-ONCE + GAP-FORGET in sync.go

#### 3a. buildLeaveRooms signature change

```go
func (h *GetSyncHandler) buildLeaveRooms(ctx context.Context, userID string, sinceMs int64) map[string]interface{}
```

#### 3b. sinceMs filter in SQL queries

Left rooms:
```sql
SELECT room_id FROM room_members 
WHERE user_id = $1 AND left_at IS NOT NULL AND left_at > $2
AND room_id NOT IN (SELECT room_id FROM forgotten_rooms WHERE user_id = $1)
```

Rejected invitations:
```sql
SELECT room_id FROM room_invitations 
WHERE invitee_id = $1 AND rejected_at IS NOT NULL AND rejected_at > $2
AND room_id NOT IN (SELECT room_id FROM forgotten_rooms WHERE user_id = $1)
```

#### 3c. Query sinceMs in handleIncrementalSync

Before calling `h.buildLeaveRooms`:
```go
sinceMs := h.querySinceTsMs(r.Context(), userID)
```

Helper function:
```go
func (h *GetSyncHandler) querySinceTsMs(ctx context.Context, userID string) int64 {
    if h.db == nil {
        return 0
    }
    var updatedAt int64
    if err := h.db.QueryRowContext(ctx,
        `SELECT updated_at FROM sync_tokens WHERE user_id = $1`, userID,
    ).Scan(&updatedAt); err != nil {
        return 0 // fallback: no filter (all left rooms included)
    }
    return updatedAt
}
```

Initial sync and FallbackToInitial paths: pass `sinceMs = 0` to `buildLeaveRooms`.

### Step 4 — Go: GAP-FORGET in room_moderation.go

#### 4a. Add db to ModerationHandler

```go
type ModerationHandler struct {
    coreClient ModerationCoreClient
    serverName string
    db         *sql.DB // for forgotten_rooms persistence
}

type ModerationConfig struct {
    CoreClient ModerationCoreClient
    ServerName string
    DB         *sql.DB
}
```

#### 4b. PostForgetRoom — insert into forgotten_rooms

After successful gRPC call:
```go
if h.db != nil {
    _, _ = h.db.ExecContext(r.Context(),
        `INSERT INTO forgotten_rooms (user_id, room_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
        userID, roomID)
}
```

### Step 5 — main.go: pass db to ModerationConfig

In `main.go` where `NewModerationHandler` is called, add `DB: db` to the config.

---

## Dev Notes

### Critical invariants to preserve

1. **Initial sync**: `sinceMs = 0` → `left_at > 0` is always true for non-zero timestamps → all left rooms included. This preserves the existing behavior for the initial full sync.

2. **buildLeaveRooms degradation**: If `db == nil` or the `sync_tokens` query fails, fall back to `sinceMs = 0` (all left rooms). No crash, graceful degradation.

3. **forgotten_rooms ON CONFLICT DO NOTHING**: Idempotent — repeat POST /forget calls do not fail.

4. **:pg group ownership in Elixir**: The `{:new_join, room_id}` message is received by the `do_incremental_sync` Task process which owns the `:pg` groups. It must call `:pg.join("room:#{new_room_id}", self())` before calling `fetch_delta_rooms` so subsequent broadcasts for that room are also received.

5. **Enum.uniq in fetch_delta_rooms**: When adding `new_room_id` to `room_ids`, use `Enum.uniq` to avoid duplicate room lookups if the room was already in the list.

6. **`already_member` path in join_room**: The broadcast is only sent on the `:ok` path (actual new join), NOT on the `{:error, :already_member}` idempotent path — no spurious wakeups.

7. **Test for the sync_tokens table**: `querySinceTsMs` queries `sync_tokens.updated_at` which is set by the Elixir side on every `persist_since_token` call. For users who have never synced, the query returns `sql.ErrNoRows` → `sinceMs = 0`.

### Test isolation

For Go unit tests, mock the DB with `sqlmock` (already used in other sync tests). Set up:
- `sync_tokens` table with specific `updated_at` values
- `room_members` with `left_at` timestamps relative to `updated_at`
- `forgotten_rooms` entries

For Elixir test: use `:pg.start_link/0` in test setup, subscribe self(), call the handler, verify message received.

### Where to find existing patterns

- Invite broadcast pattern: `server.ex` line ~387 (`invite_user/2`)
- `{:new_invite, room_id}` receive handler: `server.ex` line ~1158
- `buildLeaveRooms` current implementation: `gateway/internal/matrix/sync.go` line 75
- `sync_tokens.updated_at` stored by: `core/apps/session_manager/lib/nebu/session/pg_store/postgres.ex`

---

## Status

Status: ready-for-dev

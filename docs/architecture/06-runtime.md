# 6 Runtime View

## Scenario 1: Matrix Client Message Send (Happy Path — GRÜN Status)

```
Matrix Client      Go Gateway          Elixir Core         PostgreSQL
     │                   │                   │                   │
     │  PUT /rooms/send  │                   │                   │
     │──────────────────►│                   │                   │
     │                   │  JWT validate      │                   │
     │                   │──────────────────►│                   │
     │                   │  user_id, role     │                   │
     │                   │◄──────────────────│                   │
     │                   │  gRPC SendEvent    │                   │
     │                   │──────────────────►│                   │
     │                   │                   │  INSERT event      │
     │                   │                   │──────────────────►│
     │                   │                   │  Ed25519 sign      │
     │                   │                   │  EventId.generate  │
     │                   │  {event_id}        │                   │
     │                   │◄──────────────────│                   │
     │  200 {event_id}   │                   │                   │
     │◄──────────────────│                   │                   │
```

## Scenario 2: gRPC EventBus Stream (GRÜN/GELB/ROT State Machine)

```
Go Gateway Status Machine:

  ┌─────┐   Stream healthy      ┌──────┐
  │ ROT │──────────────────────►│ GRÜN │
  └─────┘                       └──────┘
     ▲                             │ Stream lost
     │ Unary polling fails          │
  ┌──────┐◄────────────────────────┘
  │ GELB │  Stream lost, Unary OK
  └──────┘
     │ Unary also fails
     └──────────────────────────►┌─────┐
                                 │ ROT │
                                 └─────┘
                                    │ Writes → message_buffer
                                    │ Drain on reconnect
```

**GRÜN:** EventBus stream healthy — direct gRPC streaming to Matrix clients.
**GELB:** Stream lost, Unary polling succeeds — writes to message_buffer, polling continues.
**ROT:** Stream AND Unary fail — all writes held in message_buffer, 200 OK returned to clients
(Matrix-conformant); Docker `restart: always` heals the Elixir core.

## Scenario 3: Matrix Client Sync (Long-Poll)

```
Matrix Client      Go Gateway          MessageBuffer       Elixir Core
     │                   │                   │                   │
     │  GET /sync        │                   │                   │
     │──────────────────►│                   │                   │
     │                   │  check buffer     │                   │
     │                   │──────────────────►│                   │
     │                   │  empty (wait)     │                   │
     │                   │◄──────────────────│                   │
     │                   │  (holds connection for up to 30s)     │
     │                   │  EventBus event arrives               │
     │                   │◄──────────────────────────────────────│
     │  200 {events}     │                   │                   │
     │◄──────────────────│                   │                   │
```

The Go Gateway distributes EventBus events from a single streaming connection to all waiting
Matrix client long-poll connections via the in-memory per-user ring buffer.

## Scenario 3a: Public Room Join — Sync Wakeup (GAP-JOIN-PUBLIC, Story 9-19)

After `POST /join/{roomId}` for a **public room** (no prior invite), the sync long-poll must
deliver `rooms.join[roomId]` within 10 s. Without a fix this hangs for 30 s: the sync Task
only subscribes to `:pg` groups for rooms already in `get_rooms_for_user`; the public-room join
broadcast goes to `room:#{roomId}` which no sync Task monitors yet.

```
Matrix Client      Go Gateway    Elixir Core (gRPC join_room/2)
     │                   │                   │
     │  POST /join       │                   │
     │──────────────────►│  gRPC JoinRoom    │
     │                   │──────────────────►│
     │                   │                   │  Room.Server.join → :ok
     │                   │                   │  :pg.get_local_members("user:#{user_id}")
     │                   │                   │  → send {:new_join, room_id} to sync Task
     │  200 {}           │                   │
     │◄──────────────────│                   │
     │                   │                   │
     │  GET /sync (long-poll held)           │
     │──────────────────►│                   │
     │                   │  receives {:new_join, room_id} from :pg
     │                   │  joins room:#{room_id} :pg group
     │                   │  re-queries with new room in scope
     │  200 {rooms.join} │                   │
     │◄──────────────────│                   │
```

**Key invariant:** The `{:new_join, room_id}` signal is only sent on the `:ok` (actual new join)
path, not on the idempotent `{:error, :already_member}` path.

## Scenario 3b: Incremental Sync — Left Rooms Filter (GAP-LEAVE-ONCE, Story 9-19)

After the first incremental sync delivers `rooms.leave[roomId]`, subsequent syncs with a fresh
`since` token must NOT re-include the same left room. Without a filter, `buildLeaveRooms` queries
all ever-left rooms on every incremental sync, causing Element Web to re-process the leave on
every cycle.

**Fix:** `buildLeaveRooms` accepts a `sinceMs int64` parameter. For incremental sync, `sinceMs`
is read from `sync_tokens.updated_at` (the wall-clock timestamp of the previous sync response).
Only rooms where `left_at > sinceMs` OR `rejected_at > sinceMs` are returned.
Initial sync passes `sinceMs = 0` to include all ever-left rooms.

## Scenario 3c: POST /forget — Persistent Room Exclusion (GAP-FORGET, Story 9-19)

After `POST /rooms/{roomId}/forget`, the room must not appear in any subsequent sync response
(`rooms.join`, `rooms.leave`, `rooms.invite`).

```
Matrix Client      Go Gateway                        PostgreSQL
     │                   │                                │
     │  POST /forget     │                                │
     │──────────────────►│  gRPC LeaveRoom precondition   │
     │                   │  INSERT INTO forgotten_rooms    │
     │                   │──────────────────────────────►│
     │  200 {}           │                                │
     │◄──────────────────│                                │
     │                   │                                │
     │  GET /sync        │                                │
     │──────────────────►│  SELECT room_id FROM forgotten_rooms WHERE user_id = $1
     │                   │──────────────────────────────►│
     │                   │  excluded from join/leave/invite
     │  200 (room absent)│                                │
     │◄──────────────────│                                │
```

**DB table:** `forgotten_rooms (user_id, room_id, forgotten_at_ms BIGINT)` — migration 000040.
Insert is idempotent (`ON CONFLICT DO NOTHING`). Cascade delete on `users` row removal.

## Scenario 3d: Per-Device Sync Token Isolation (GAP-SINCE-IGNORED, Story 9-22)

When a Matrix client opens a sync long-poll, the `device_id` (extracted from the `"did"` claim
of the JWT) is forwarded in `GetSyncDeltaRequest`. The Elixir Core looks up and persists the
`since` token in `sync_tokens` keyed by `(user_id, device_id)` — not just `user_id`.

```
Matrix Client A (device_id=AAAA)    Matrix Client B (device_id=BBBB)    Elixir Core
        │                                    │                                │
        │  GET /sync?since=v1_...            │                                │
        │───────────────────────────────────────────────────────────────────►│
        │                           gRPC GetSyncDelta(user_id, since, AAAA)  │
        │                                    │  get_since_token(user_id, AAAA)│
        │                                    │  ◄── {since_token for AAAA}   │
        │  200 {events, next_batch}          │                                │
        │◄────────────────────────────────── │ persist_since_token/4 (A, AAAA)│
        │                                    │                                │
        │                    GET /sync?since=v1_...                           │
        │                    ───────────────────────────────────────────────►│
        │                          gRPC GetSyncDelta(user_id, since, BBBB)   │
        │                                    │  get_since_token(user_id, BBBB)│
        │                                    │  ◄── {since_token for BBBB}   │
        │                    200 {events, next_batch}                         │
        │                    ◄───────────────│ persist_since_token/4 (A, BBBB)│
```

**Isolation invariant:** Device AAAA and device BBBB each maintain an independent checkpoint row
in `sync_tokens (user_id, device_id)`. A slow or disconnected device cannot advance another
device's token and cause it to miss events.

**Fallback:** When `device_id` is empty (legacy client or test), the Core falls back to the
`(user_id, '')` row (legacy arity-3/1 functions) and triggers a full initial sync on mismatch.

## Scenario 3e: Per-Device Logout — Sync Token Cleanup (Story 9-22)

After `POST /logout`, the Go Gateway calls `InvalidateUserSessions` with both `user_id` and
`device_id`. The Elixir Core deletes the `sync_tokens (user_id, device_id)` row and the
corresponding `sessions (user_id, device_id)` row in a single DB transaction, without evicting
the in-memory ETS session (another device for the same user may still be active).

```
Matrix Client      Go Gateway            Elixir Core               PostgreSQL
     │                   │                    │                         │
     │  POST /logout     │                    │                         │
     │──────────────────►│                    │                         │
     │                   │  JWT invalidated   │                         │
     │                   │  (local denylist)  │                         │
     │                   │  gRPC InvalidateUserSessions(user_id, DDDD) │
     │                   │───────────────────►│                         │
     │                   │                    │  DELETE sync_tokens     │
     │                   │                    │  WHERE (user_id, DDDD)  │
     │                   │                    │─────────────────────────►
     │                   │                    │  DELETE sessions         │
     │                   │                    │  WHERE (user_id, DDDD)  │
     │                   │                    │─────────────────────────►
     │  200 {}           │◄───────────────────│                         │
     │◄──────────────────│                    │                         │
```

**Non-fatal on gRPC failure:** The JWT is already invalidated before the gRPC call. If the Core
call fails, a warning is logged but the client still receives `200 {}` (Matrix spec conformance).

## Scenario 3f: Invite Tiles — Stripped State Enrichment (GAP-INVITE-STATE, Story 9-23)

Matrix Client-Server API spec §4.4.4 requires `invite_state` to contain stripped state events
so clients can display room context before a user accepts an invitation. Without these events,
Element Web invite tiles show no avatar, no join-rules badge, and no creator info.

`buildInviteRooms` in `gateway/internal/matrix/sync.go` queries five stripped state event types
per pending invite room:

| Event type | Content field queried | Inclusion condition |
|---|---|---|
| `m.room.member` | `membership = "invite"` | Always (mandatory by spec) |
| `m.room.name` | `name` | Non-empty name |
| `m.room.join_rules` | `join_rule` | Non-empty join_rule |
| `m.room.avatar` | `url` | Non-empty url |
| `m.room.create` | `creator` | Non-empty creator |

Each query uses a JSONB double-encoding guard (`CASE WHEN jsonb_typeof(content) = 'object'`)
to handle both direct JSONB objects and legacy double-encoded events. Missing events are silently
omitted (spec SHOULD semantics) — no error is returned when an event type is absent for a room.

```
Matrix Client      Go Gateway                                  PostgreSQL
     │                   │                                          │
     │  GET /sync        │                                          │
     │──────────────────►│                                          │
     │                   │  SELECT room_id, inviter_id              │
     │                   │  FROM room_invitations WHERE user_id=$1  │
     │                   │─────────────────────────────────────────►│
     │                   │  ◄── [roomID, inviterID, ...]            │
     │                   │                                          │
     │                   │  -- per room (5 queries, sequential):    │
     │                   │  SELECT content->>'name'                 │
     │                   │    FROM events WHERE room_id=$1          │
     │                   │    AND event_type='m.room.name' LIMIT 1  │
     │                   │─────────────────────────────────────────►│
     │                   │  SELECT content->>'join_rule'            │
     │                   │    FROM events WHERE room_id=$1          │
     │                   │    AND event_type='m.room.join_rules'    │
     │                   │─────────────────────────────────────────►│
     │                   │  SELECT content->>'url'                  │
     │                   │    FROM events WHERE room_id=$1          │
     │                   │    AND event_type='m.room.avatar'        │
     │                   │─────────────────────────────────────────►│
     │                   │  SELECT content->>'creator'              │
     │                   │    FROM events WHERE room_id=$1          │
     │                   │    AND event_type='m.room.create'        │
     │                   │─────────────────────────────────────────►│
     │                   │                                          │
     │  200 {rooms.invite: {                                        │
     │    invite_state.events: [                                     │
     │      {type: "m.room.member", membership: "invite"},          │
     │      {type: "m.room.name", content: {name: "..."}},          │
     │      {type: "m.room.join_rules", content: {join_rule: "..."}},
     │      {type: "m.room.avatar", content: {url: "mxc://..."}},   │
     │      {type: "m.room.create", content: {creator: "@..."}},    │
     │    ]}}                                                        │
     │◄──────────────────│                                          │
```

**Performance note:** The five queries run sequentially per invite room. For typical instances
with few pending invites (< 10) this is negligible. Batching is deferred to Phase 2.

## Scenario 3g: Top-Level Global Account Data Injection (GAP-GLOBAL-ACCOUNT-DATA, Story 9-24)

Matrix spec §6.3 requires a top-level `account_data` key in every sync response carrying global
`m.*` events (e.g. `m.push_rules`, `m.ignored_user_list`). Without this field, Element Web
cannot apply push rule customisations or ignored-user lists set by the client.

All four sync paths now call `injectGlobalAccountData` before serialising the response:

| Sync path | Call site in `sync.go` |
|---|---|
| Initial sync (`GetInitialSync`) | `GetSync` handler |
| Incremental delta sync (`GetSyncDelta`) | `handleIncrementalSync` — normal delta path |
| Fallback-to-initial (`FallbackToInitial = true`) | `handleIncrementalSync` — fallback branch |
| Buffer fast-path (local ring buffer drain) | `buildResponseFromBufferedEvents` — returns empty section (no DB call) |

The buffer fast-path intentionally skips the DB query: global account data changes are rare, and
the next non-buffered sync cycle will deliver them. This keeps the buffer hot path at zero extra
DB queries.

```
Matrix Client      Go Gateway                                  PostgreSQL
     │                   │                                          │
     │  GET /sync        │                                          │
     │──────────────────►│                                          │
     │                   │  injectGlobalAccountData(userID)         │
     │                   │  withUserDB → SET app.user_id = $1       │
     │                   │  SELECT event_type, content              │
     │                   │    FROM room_account_data                │
     │                   │    WHERE user_id=$1 AND room_id=''       │
     │                   │─────────────────────────────────────────►│
     │                   │  ◄── [(m.push_rules, {...}), ...]        │
     │                   │                                          │
     │  200 {                                                        │
     │    "account_data": {                                          │
     │      "events": [                                              │
     │        {"type": "m.push_rules", "content": {...}},           │
     │        ...                                                    │
     │      ]},                                                      │
     │    "rooms": {...}, ...}                                       │
     │◄──────────────────│                                          │
```

**RLS invariant:** `ListGlobalAccountData` uses `withUserDB` to set the `app.user_id` GUC inside
a transaction before querying. The RLS policy on `room_account_data` (migration 000033) filters
by `current_setting('app.user_id', true)`. Without this GUC the policy silently returns zero rows.

**Empty-section guarantee:** `syncResponse.AccountData` is typed `syncAccountDataSection`
(not a pointer) and never carries `omitempty`. Even when no global account data exists, the
response always contains `"account_data": {"events": []}` — a JSON-null or missing key would
break `matrix-js-sdk`.

## Scenario 3h: Buffer Fast-Path — Synthetic next_batch Token (GAP-BUFFER-NEXT-BATCH, Story 9-25)

When events are already present in the local ring buffer, `handleIncrementalSync` drains
the buffer and returns immediately without a gRPC round-trip to Elixir Core. Prior to this
fix, `buildResponseFromBufferedEvents` echoed the client's `since=` token back as
`next_batch`, causing a stuck-token loop: the client re-sent the same token, the buffer
re-delivered the same delta, and `sync_tokens.updated_at` was never advanced.

**Fix:** `syntheticNextBatch()` (added in Story 9-25) generates a `buf_<unix_ms>_<seq>`
token on every buffer-path response. The token is:

- **Monotonically advancing** — Unix-ms timestamp plus an `atomic.Int64` sequence counter
  ensures uniqueness even for sub-millisecond bursts across goroutines.
- **Opaque to the client** — the `buf_` prefix makes it clearly synthetic; it does not
  collide with Elixir's `v1_<base64url>` since-tokens.
- **Never persisted** — `sync_tokens` is not written from the buffer path. When the client
  sends `?since=buf_<ts>_<seq>` on the next poll, Elixir's `GetSyncDelta` finds no matching
  `sync_tokens` row → `fallback_to_initial = true` → the existing `FallbackToInitial` branch
  issues a safe full re-sync with a fresh real token.

```
Matrix Client      Go Gateway (buffer fast-path)
     │                   │
     │  GET /sync?since=s42_1
     │──────────────────►│
     │                   │  buffer.DrainFor(userID) → [events]
     │                   │  syntheticNextBatch() → "buf_1746518400000_7"
     │  200 {next_batch: "buf_1746518400000_7", rooms: {...}}
     │◄──────────────────│
     │                   │
     │  GET /sync?since=buf_1746518400000_7
     │──────────────────►│
     │                   │  buffer empty → gRPC GetSyncDelta(user_id, "buf_...", device_id)
     │                   │  Elixir: no sync_tokens row for "buf_..." → FallbackToInitial=true
     │                   │  GetInitialSync → fresh real token
     │  200 {next_batch: "s99_3", rooms: {...}}   ← real token resumes normal cycle
     │◄──────────────────│
```

**No stuck-token loops:** Each buffer-path response carries a distinct `next_batch`, so
`matrix-js-sdk` always issues a fresh `since=` on the next poll. The fallback-to-initial
safety net was already production-tested before this story.

## Scenario 4: Compliance Four-Eyes Export Flow

```
Compliance Officer → POST /api/v1/compliance/access-requests (JWT auth)
Instance Admin 1 → POST /approve (four-eyes gate: needs 2 approvals)
Instance Admin 2 → POST /approve (gate satisfied → access session issued)
Compliance Officer → GET /api/v1/compliance/export (X-Compliance-Token, 24h TTL)
Export → Ed25519-signed JSON/PDF with event content + audit trail
Auto-expiry → session invalidated after 24 hours
```

## Scenario 5: Elixir Core Restart Recovery

On restart, Horde re-discovers Room GenServers across the cluster via CRDT registry.
Session Manager GenServer reads since-token checkpoints from PostgreSQL (no cold-sync forced on clients).
EventBus stream re-connects to Go Gateway after exponential backoff (max 30s + jitter).

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Implementation Patterns, §API & Kommunikation, §Resilienz & Selbst-Heilung; Story 9-19 (GAP-JOIN-PUBLIC, GAP-LEAVE-ONCE, GAP-FORGET); Story 9-22 (GAP-SINCE-IGNORED — per-device sync tokens, per-device logout cleanup); Story 9-23 (GAP-INVITE-STATE — invite_state stripped state enrichment: join_rules, avatar, create); Story 9-24 (GAP-GLOBAL-ACCOUNT-DATA — top-level account_data delivery in all 4 sync paths, RLS-aware ListGlobalAccountData); Story 9-25 (GAP-BUFFER-NEXT-BATCH — synthetic buf_<ms>_<seq> next_batch token on buffer fast-path, replaces echoed since= token)_

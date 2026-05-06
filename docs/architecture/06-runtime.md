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

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Implementation Patterns, §API & Kommunikation, §Resilienz & Selbst-Heilung; Story 9-19 (GAP-JOIN-PUBLIC, GAP-LEAVE-ONCE, GAP-FORGET)_

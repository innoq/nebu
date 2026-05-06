# 5 Building Block View

## Level 1 — Top-Level Decomposition

```
nebu/
├── gateway/          ← Go API Gateway (+ Admin UI)
├── media/            ← Go Media Gateway
├── core/             ← Elixir/OTP Umbrella
├── proto/            ← Shared gRPC .proto definitions
└── docs/             ← Architecture docs (this file)
```

## Level 2 — Go Gateway Internal Structure

```
gateway/
├── cmd/gateway/main.go         ← Startup: migrate → registry → HTTP routing
└── internal/
    ├── auth/                   ← OIDC token validation, bootstrap mode
    │   ├── oidc.go             ← go-oidc provider, token validation
    │   └── bootstrap.go        ← First-admin bootstrap mode
    ├── matrix/                 ← Matrix Client-Server API handlers
    │   ├── login.go            ← POST /_matrix/client/v3/login (SSO + OIDC)
    │   ├── logout.go           ← POST /_matrix/client/v3/logout; NewLogoutHandlerWithCore cleans up
    │   │                          per-device sync_tokens via gRPC InvalidateUserSessions (Story 9-22)
    │   ├── sync.go             ← GET /_matrix/client/v3/sync (long-poll); forwards device_id from
    │   │                          JWT session to GetSyncDeltaRequest (GAP-SINCE-IGNORED, Story 9-22);
    │   │                          buildLeaveRooms uses sinceMs filter (GAP-LEAVE-ONCE) +
    │   │                          forgotten_rooms exclusion (GAP-FORGET); queryForgottenRoomIDs +
    │   │                          querySinceTsMs helpers; top-level AccountData field in syncResponse
    │   │                          populated via injectGlobalAccountData on all 4 sync paths
    │   │                          (initial, incremental, FallbackToInitial, buffer fast-path — Story 9-24)
    │   ├── account_data.go     ← AccountDataDB + GlobalAccountDataDB interfaces; GlobalAccountDataRow
    │   │                          struct; AccountDataHandler (GET/PUT global + room-scoped endpoints)
    │   ├── send.go             ← PUT /rooms/{id}/send/...
    │   ├── rooms.go            ← POST /createRoom, POST /join/{id}
    │   ├── room_moderation.go  ← POST /forget inserts into forgotten_rooms (GAP-FORGET, Story 9-19)
    │   ├── profile.go          ← GET/PUT /profile/{userId}
    │   ├── presence.go         ← GET/PUT /presence/{userId}/status
    │   └── ...                 ← typing, receipts, messages, keys
    ├── admin/                  ← Admin UI (Go Templates + SSR) + Admin API
    │   ├── api.go              ← /api/v1/* Router (oapi-codegen StrictHandler)
    │   ├── users.go            ← User CRUD UI + API
    │   ├── rooms.go            ← Room Management UI + API
    │   ├── compliance.go       ← Four-eyes compliance UI
    │   └── templates/          ← Embedded HTML templates (go:embed)
    ├── grpc/                   ← gRPC CoreService client
    │   ├── client.go           ← gRPC connection, CoreService stub
    │   ├── stream.go           ← EventBus server-streaming + exponential backoff
    │   └── fallback.go         ← Unary GetPendingEvents (GELB status)
    ├── buffer/                 ← message_buffer for ROT-status writes
    │   ├── buffer.go           ← In-memory ring buffer per user
    │   ├── drain.go            ← Drain worker + DrainStrategy interface
    │   └── strategy/           ← linear.go (MVP), aimd.go (Phase 2)
    ├── middleware/             ← Auth, rate limiting, body limit, CORS, security headers
    ├── registry/               ← Elixir node registry (/internal/nodes/*)
    ├── compliance/             ← Compliance API handlers (four-eyes, export, anonymize)
    ├── health/                 ← /health + /ready handlers
    └── config/                 ← NEBU_* env-var configuration
```

## Level 2 — Elixir/OTP Core Internal Structure

```
core/apps/
├── nebu_db/          ← Shared Ecto Repo (DB connection)
├── room_manager/     ← FR7–24: Horde.DynamicSupervisor + Room GenServer
│   └── lib/nebu/room/
│       ├── manager.ex      ← Horde.DynamicSupervisor
│       ├── server.ex       ← Room GenServer (state, history, power levels)
│       ├── db.ex           ← PostgreSQL queries; get_recently_left_rooms_for_user/1 added (Story 9-19)
│       ├── db_behaviour.ex ← @callback contract for db.ex (mockable in tests)
│       └── power_level.ex  ← Room policy enforcement
├── session_manager/  ← ETS + PostgreSQL Hybrid since-Token (per-device since Story 9-22)
│   └── lib/nebu/session/
│       ├── manager.ex          ← GenServer owning ETS table
│       ├── token.ex            ← v1_<base64url(ts+cursor_map)> format
│       ├── pg_store/postgres.ex ← persist_since_token/3 (legacy) + /4 (per-device);
│       │                           get_since_token/1 + /2; invalidate_session/1 + /2
│       └── session_supervisor.ex ← destroy_session/1 (all devices) + /2 (per-device)
├── presence/         ← FR15: Presence status (online/offline/unavailable)
├── event_dispatcher/ ← EventBus gRPC streaming + pg Process Groups fanout
│   └── lib/nebu/event_dispatcher/
│       ├── server.ex       ← gRPC handlers: join_room/2 broadcasts {:new_join} to user :pg group;
│       │                      leave_room/2 broadcasts {:new_leave}; do_incremental_sync handles
│       │                      {:new_join}/{:new_leave} to wake long-poll sync Tasks (GAP-JOIN-PUBLIC)
│       ├── dispatcher.ex   ← Routes events to rooms + subscribers
│       └── bus.ex          ← gRPC ServerStream to Go Gateway
├── signature/        ← FR25–29: Ed25519 signing + Canonical JSON + Event-ID
│   └── lib/nebu/
│       ├── signature.ex         ← :crypto.sign/4 with eddsa
│       ├── event_id.ex          ← Nebu.EventId.generate/1 (SHA-256 content hash)
│       └── canonical_json.ex    ← RFC 8785 canonical JSON
├── permissions/      ← System roles + room power levels
│   └── lib/nebu/permissions/
│       ├── system_role.ex       ← instance_admin | compliance_officer | user
│       └── room_policy.ex       ← Power-level checks for room operations
└── compliance/       ← FR30–35: Four-eyes access, audit-log writers, signed export
```

> **Note:** `gateway/internal/` additionally contains support packages not visualized
> above (`api/`, `audit/`, `db/`, `ui/`, `validate/`). They wrap shared infrastructure
> rather than represent distinct architectural blocks.

> **PostgreSQL tables added in Story 9-19:** `forgotten_rooms (user_id, room_id, forgotten_at_ms BIGINT)`
> — migration 000040. Tracks rooms the user has permanently dismissed via `POST /forget`.
> Excluded from all `/sync` sections (join, leave, invite). Primary key `(user_id, room_id)`;
> cascade delete on `users` removal.

> **`sync_tokens` schema change in Story 9-22:** migration 000041 adds `device_id TEXT NOT NULL DEFAULT ''`
> and replaces the `user_id`-only primary key with a composite `(user_id, device_id)` PK.
> Legacy rows are preserved with `device_id = ''`. Each device now maintains an independent
> sync checkpoint, preventing parallel sessions on different devices from overwriting each other's
> `since` token.

> **Global account data in sync responses (Story 9-24):** `syncResponse` gains a top-level
> `AccountData syncAccountDataSection` field (JSON key `account_data`, never omitted) that carries
> global `m.*` account data events per Matrix spec §6.3. The `GlobalAccountDataDB` interface
> (defined in `gateway/internal/matrix/account_data.go`) exposes a single method
> `ListGlobalAccountData(ctx, userID) ([]GlobalAccountDataRow, error)`. The implementation
> `PostgresAccountDataDB.ListGlobalAccountData` (in `gateway/internal/db/account_data_store.go`)
> queries `room_account_data WHERE room_id = ''` inside a `withUserDB` transaction to satisfy the
> RLS policy (GUC `app.user_id`). The buffer fast-path returns an empty `account_data.events` slice
> (no DB call) — global account data changes are rare and are picked up on the next full sync cycle.

## Level 2 — Proto / gRPC Contract

```
proto/
├── core.proto              ← CoreService: all RPC definitions
└── gen/
    ├── go/                 ← Generated Go stubs (buf generate)
    └── elixir/             ← Generated Elixir stubs
```

Key gRPC services: `SendEvent`, `CreateRoom`, `JoinRoom`, `GetMessages`, `GetRoomState`, `SetPresence`,
`SetTyping`, `ValidateToken`, `GetPendingEvents` (fallback), `EventBus` (streaming),
`GetSyncDelta` (incremental sync with per-device `device_id` field, Story 9-22),
`InvalidateUserSessions` (per-device or full-user session cleanup, Story 9-22).

**`GetSyncDeltaRequest` fields (Story 9-22):**

| Field | Type | Description |
|---|---|---|
| `user_id` | string | Matrix user ID |
| `since_token` | string | Client-supplied sync token |
| `timeout_ms` | int64 | Long-poll timeout |
| `device_id` | string | Device-scoped checkpoint key; empty string = legacy fallback |

**`InvalidateUserSessionsRequest` fields (Story 9-22):**

| Field | Type | Description |
|---|---|---|
| `user_id` | string | Matrix user ID |
| `device_id` | string | When set, only invalidates this device; when empty, invalidates all user sessions |

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Project Structure & Boundaries, §Complete Project Directory Structure; Story 9-19 (room_moderation.go, sync.go, event_dispatcher/server.ex, forgotten_rooms migration); Story 9-22 (per-device sync tokens, device_id in proto); Story 9-24 (GlobalAccountDataDB interface, ListGlobalAccountData, top-level account_data in syncResponse)_

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
    │   │                          (initial, incremental, FallbackToInitial, buffer fast-path — Story 9-24);
    │   │                          syntheticNextBatch() + syntheticBatchSeq atomic counter generate
    │   │                          buf_<ms>_<seq> next_batch on buffer fast-path (GAP-BUFFER-NEXT-BATCH,
    │   │                          Story 9-25) — replaces echoed sinceToken to prevent stuck-token loops;
    │   │                          syncUnsigned.MRelations carries bundled m.thread aggregations from
    │   │                          proto Event.unsigned_relations (Story 9-28)
    │   ├── relations.go        ← All three /relations route variants (Story 9-28 / 9-29):
    │   │                          • GET /relations/{eventId}                       (base, Story 9-29)
    │   │                          • GET /relations/{eventId}/{relType}             (Story 9-28)
    │   │                          • GET /relations/{eventId}/{relType}/{eventType} (Story 9-29)
    │   │                          GetRelationsHandler + GetRelationsCoreClient consumer interface;
    │   │                          query params: dir (f/b, default b), limit (default 20, max 100),
    │   │                          recurse (bool, accepted without error), from (pagination token);
    │   │                          invalid dir → 400 M_BAD_PARAM; invalid recurse → 400 M_BAD_PARAM;
    │   │                          maps gRPC PERMISSION_DENIED → 403 M_FORBIDDEN,
    │   │                          NOT_FOUND → 404 M_NOT_FOUND (Story 9-28 / 9-29)
    │   ├── account_data.go     ← AccountDataDB + GlobalAccountDataDB interfaces; GlobalAccountDataRow
    │   │                          struct; AccountDataHandler (GET/PUT global + room-scoped endpoints)
    │   ├── send.go             ← PUT /rooms/{id}/send/...
    │   ├── rooms.go            ← POST /createRoom, POST /join/{id}
    │   ├── room_moderation.go  ← POST /forget inserts into forgotten_rooms (GAP-FORGET, Story 9-19)
    │   ├── profile.go          ← GET/PUT /profile/{userId}
    │   ├── presence.go         ← GET/PUT /presence/{userId}/status
    │   ├── search.go           ← POST /_matrix/client/v3/search (Story 11-4); SearchCoreClient
    │   │                          consumer interface; user_id from JWT context only (never body);
    │   │                          forwards to gRPC SearchMessages with x-user-id metadata;
    │   │                          builds §11.14.1 response with groups-by-room_id + highlights
    │   ├── event.go            ← GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} (Story 11-8);
    │   │                          GetEventCoreClient consumer interface; validates roomId + eventId format;
    │   │                          maps gRPC PERMISSION_DENIED → 403 M_FORBIDDEN (non-member),
    │   │                          NOT_FOUND → 404 M_NOT_FOUND; returns Matrix event JSON via
    │   │                          protoEventToMatrix (shared with other event handlers)
    │   └── ...                 ← typing, receipts, messages, keys
    ├── admin/                  ← Admin UI (Go Templates + SSR) + Admin API
    │   ├── api.go              ← /api/v1/* Router (oapi-codegen StrictHandler)
    │   ├── users.go            ← User CRUD UI + API
    │   ├── rooms.go            ← Room Management UI + API
    │   ├── compliance.go       ← Four-eyes compliance UI
    │   ├── page_data.go        ← PageData struct + newPageData() helper + SetBuildInfo();
    │   │                          BuildVersion/GitCommit/BuildTime fields on PageData; SetBuildInfo
    │   │                          called once from main.go; newPageData() used by all authenticated
    │   │                          handlers to pre-populate build info for the footer (Story 11-9);
    │   │                          ErrorMode bool suppresses footer on error pages
    │   └── templates/          ← Embedded HTML templates (go:embed);
    │       └── layouts/base.html ← DaisyUI footer rendered on every authenticated page (Story 11-9):
    │                              `nebu gateway v{{.BuildVersion}} · {{.GitCommit}} · built {{.BuildTime}}`
    │                              guarded by `{{ if not .LoginMode }}{{ if not .ErrorMode }}`
    ├── grpc/                   ← gRPC CoreService client
    │   ├── client.go           ← gRPC connection, CoreService stub
    │   ├── stream.go           ← EventBus server-streaming + exponential backoff
    │   └── fallback.go         ← Unary GetPendingEvents (GELB status)
    ├── buffer/                 ← message_buffer for ROT-status writes
    │   ├── buffer.go           ← In-memory ring buffer per user
    │   ├── drain.go            ← Drain worker + DrainStrategy interface
    │   └── strategy/           ← linear.go (MVP), aimd.go (Phase 2)
    ├── middleware/             ← Auth, rate limiting, body limit, CORS, security headers;
    │                              NewIPRateLimiter (per-IP token-bucket, Stories 5.21/5.29a);
    │                              NewUserRateLimiter (per-user token-bucket keyed on ContextKeyUserID,
    │                              Story 11-5: 10 req/min for POST /search; IP fallback for defense-in-depth;
    │                              NEBU_RATE_LIMIT_DISABLED=true no-op; retry_after_ms in 429 body)
    ├── registry/               ← Elixir node registry (/internal/nodes/*)
    ├── compliance/             ← Compliance API handlers (four-eyes, export, anonymize)
    ├── health/                 ← /health + /ready handlers; info.go adds GET /info
    │   │                          (NewInfoHandler — static JSON, no DB/gRPC, zero allocs per request;
    │   │                          component/version/gitCommit/buildTime set via ldflags at Docker build
    │   │                          time; fallback "unknown" when built locally without ldflags — Story 11-9)
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
│       ├── db.ex           ← PostgreSQL queries; get_recently_left_rooms_for_user/1 added (Story 9-19);
│       │                      fetch_events_by_relation/5 (events by m.relates_to event_id; optional
│       │                      rel_type filter — empty = all types; opts: event_type filter, dir for
│       │                      ORDER BY ASC/DESC; dynamic WHERE builder — Story 9-28/9-29);
│       │                      count_thread_children/2 (reply count for thread root);
│       │                      event_in_room?/2 (membership guard) added (Story 9-28);
│       │                      fetch_event/2 (single event by event_id scoped to room_id —
│       │                      SELECT … WHERE event_id=$1 AND room_id=$2; returns {:ok, map} |
│       │                      {:error, :not_found} | {:error, reason} — Story 11-8)
│       ├── db_behaviour.ex ← @callback contract for db.ex (mockable in tests);
│       │                      corresponding callbacks for Story 9-28/9-29 DB functions
│       └── power_level.ex  ← Room policy enforcement
├── session_manager/  ← ETS + PostgreSQL Hybrid since-Token (per-device since Story 9-22)
│   └── lib/nebu/session/
│       ├── manager.ex          ← GenServer owning ETS table
│       ├── token.ex            ← v1_<base64url(ts+cursor_map)> format
│       ├── pg_store/postgres.ex ← persist_since_token/3 (legacy) + /4 (per-device);
│       │                           get_since_token/1 + /2; invalidate_session/1 + /2
│       └── session_supervisor.ex ← destroy_session/1 (all devices) + /2 (per-device)
├── presence/         ← FR15: Presence status (online/offline/unavailable)
├── event_dispatcher/ ← EventBus gRPC streaming + pg Process Groups fanout + FTS search layer
│   └── lib/nebu/
│       ├── build_info.ex   ← Nebu.BuildInfo.get/0 — returns component/version/git_commit/build_time;
│       │                      reads Application.get_env(:event_dispatcher, :build_info, %{}) with
│       │                      System.get_env("RELEASE_VERSION"/"GIT_COMMIT"/"BUILD_TIME", "unknown")
│       │                      as fallback; used by health/server.ex GET /info route (Story 11-9)
│       ├── health/
│       │   ├── health.ex   ← Nebu.Health module (existing)
│       │   └── server.ex   ← existing health server extended with GET /info route (Story 11-9):
│       │                      new `handle_connection/1` clause delegates to Nebu.BuildInfo.get/0
│       │                      and returns JSON; inserted before the 404 catch-all clause
│       └── event_dispatcher/
│           ├── server.ex       ← gRPC handlers: join_room/2 broadcasts {:new_join} to user :pg group;
│           │                      leave_room/2 broadcasts {:new_leave}; do_incremental_sync handles
│           │                      {:new_join}/{:new_leave} to wake long-poll sync Tasks (GAP-JOIN-PUBLIC);
│           │                      upgrade_room/2 implements full Matrix §11.35.1 flow: tombstone →
│           │                      create → join → set_power_levels → copy_state → invite → archive_old
│           │                      → terminate_old_genserver; uses GRPC.RPCError instead of bare `:ok =`
│           │                      pattern matches; wraps entire body in try/rescue with audit-trail
│           │                      on failure (Story 9-27);
│           │                      get_relations/2 gRPC handler reads event_type, dir, recurse from
│           │                      proto request; validates membership + event_in_room?; delegates to
│           │                      fetch_events_by_relation/5 with opts map; returns
│           │                      Core.GetRelationsResponse (Story 9-28/9-29);
│           │                      get_event/2 gRPC handler (Story 11-8): looks up room via
│           │                      Horde registry, enforces MapSet membership guard, delegates to
│           │                      messages_db_module().fetch_event/2, attaches thread aggregations
│           │                      via attach_thread_aggregations/3, returns Core.GetEventResponse;
│           │                      attach_thread_aggregations/3 private fn: for each timeline event
│           │                      calls count_thread_children/2 + fetch latest reply, encodes
│           │                      JSON {"m.thread":{count,latest_event,current_user_participated}}
│           │                      into Event.unsigned_relations field (Story 9-28)
│           │                      on failure (Story 9-27);
│           │                      search_messages/2 executes FTS via Nebu.Search.DB.search_messages/5;
│           │                      user_id sourced exclusively from trusted_identity(stream) metadata
│           │                      (NEVER from request.user_id — security invariant); offset capped at
│           │                      10_000 to prevent expensive deep-page queries; next_batch is
│           │                      Base64(offset+limit) for cursor pagination (Story 11-3)
│           ├── dispatcher.ex   ← Routes events to rooms + subscribers
│           ├── bus.ex          ← gRPC ServerStream to Go Gateway
│           └── search/
│               └── db.ex       ← Nebu.Search.DB — membership-scoped full-text search SQL layer (Story 11-2);
│                                  search_messages/4 (user_id, term, limit, offset) executes canonical SQL
│                                  against the events.search_vector GIN index (migration 000042); membership
│                                  filter enforced at SQL layer via subquery on room_members WHERE left_at IS NULL
│                                  (NOT application-layer post-filter); encrypted rooms excluded via NOT EXISTS
│                                  on m.room.encryption state events; sql_search_messages/0 exposes the SQL
│                                  constant for structural testing (AC2); Story 11.3 wires this module to the
│                                  SearchMessages gRPC handler
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

> **Room upgrade flow (Story 9-27):** `upgrade_room/2` in `event_dispatcher/server.ex` now
> implements the complete Matrix spec §11.35.1 sequence atomically: (1) tombstone the old room,
> (2) create the new room with `predecessor` field, (3) join creator, (4) set power levels,
> (5) copy state events, (6) invite old members, (7) `admin_db_module().archive_room_atomic/1`
> (SELECT FOR UPDATE, idempotent on `:not_found`) marks the old room row as archived in PostgreSQL,
> (8) `Horde.DynamicSupervisor.terminate_child/2` stops the old room GenServer.
> Error handling throughout uses `GRPC.RPCError` + `GRPC.Status.internal()` to surface Elixir
> errors as gRPC `INTERNAL` → Go gateway HTTP 500 (not MatchError → `codes.Unknown`).
> A top-level `try/rescue` writes a failure entry to the audit trail before reraising.

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

> **Full-text search column in Story 11-1 (ADR-010):** migration 000042 adds a `search_vector tsvector`
> column to the `events` table, populated by a PL/pgSQL trigger (`events_search_vector_trigger`)
> on every `INSERT OR UPDATE OF content`. The trigger calls
> `to_tsvector('pg_catalog.simple', coalesce(content->>'body', ''))`, using the `simple`
> text search configuration (language-agnostic, no stemming — appropriate for a multilingual
> chat server). A GIN index (`events_search_vector_gin_idx`) enables efficient `@@ tsquery`
> queries. All existing events were backfilled during the migration. This is the database
> foundation for `POST /_matrix/client/v3/search` (Epic 11). Scope enforcement at query time:
> `WHERE room_id = ANY($membership_room_ids)` prevents cross-room leakage.

> **Global account data in sync responses (Story 9-24):** `syncResponse` gains a top-level
> `AccountData syncAccountDataSection` field (JSON key `account_data`, never omitted) that carries
> global `m.*` account data events per Matrix spec §6.3. The `GlobalAccountDataDB` interface
> (defined in `gateway/internal/matrix/account_data.go`) exposes a single method
> `ListGlobalAccountData(ctx, userID) ([]GlobalAccountDataRow, error)`. The implementation
> `PostgresAccountDataDB.ListGlobalAccountData` (in `gateway/internal/db/account_data_store.go`)
> queries `room_account_data WHERE room_id = ''` inside a `withUserDB` transaction to satisfy the
> RLS policy (GUC `app.user_id`). The buffer fast-path returns an empty `account_data.events` slice
> (no DB call) — global account data changes are rare and are picked up on the next full sync cycle.

> **Synthetic next_batch token on buffer fast-path (Story 9-25, GAP-BUFFER-NEXT-BATCH):**
> `syntheticNextBatch()` in `gateway/internal/matrix/sync.go` generates a `buf_<unix_ms>_<seq>`
> token for every response served from the local ring buffer. A package-level `syntheticBatchSeq`
> (`sync/atomic.Int64`) increments on each call, ensuring uniqueness within a process even for
> sub-millisecond bursts. The `sinceToken` parameter was removed from `buildResponseFromBufferedEvents`
> (it is no longer used). The synthetic token is never persisted to `sync_tokens`; if the client
> sends it on the next request, Elixir's `GetSyncDelta` triggers `FallbackToInitial = true`, which
> issues a safe full re-sync. No schema change and no new interfaces were introduced.

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
`InvalidateUserSessions` (per-device or full-user session cleanup, Story 9-22),
`GetRelations` (thread relation events for a parent event_id, Story 9-28/9-29),
`SearchMessages` (full-text search with membership enforcement, Story 11-3),
`GetEvent` (single event fetch by event_id scoped to a room, membership-enforced, Story 11-8).

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


_Source: `_bmad-output/planning-artifacts/architecture.md`, §Project Structure & Boundaries, §Complete Project Directory Structure; Story 9-19 (room_moderation.go, sync.go, event_dispatcher/server.ex, forgotten_rooms migration); Story 9-22 (per-device sync tokens, device_id in proto); Story 9-24 (GlobalAccountDataDB interface, ListGlobalAccountData, top-level account_data in syncResponse); Story 9-25 (syntheticNextBatch helper, syntheticBatchSeq atomic counter, sinceToken param removed from buildResponseFromBufferedEvents); Story 9-27 (upgrade_room/2 full Matrix §11.35.1 flow, GRPC.RPCError error handling, archive_room_atomic, terminate_child, try/rescue failure audit); Story 11-2 (Nebu.Search.DB, membership-scoped FTS SQL contract, encrypted-room exclusion, integration test infrastructure)_
**`Event` message — `unsigned_relations` field (Story 9-28):**

The shared `Event` proto message gained field 9:

```protobuf
bytes unsigned_relations = 9;
// JSON: {"m.thread":{"count":N,"latest_event":{...},"current_user_participated":bool}}
```

Set by `attach_thread_aggregations/3` in Elixir for events that have at least one thread reply.
Empty (zero bytes) for events with no relations — the Go gateway omits `m.relations` from the
`unsigned` JSON object when the field is absent.

**`GetRelationsRequest` / `GetRelationsResponse` fields (Story 9-28 / 9-29):**

| Field | Type | Description |
|---|---|---|
| `user_id` | string | Caller; membership guard enforced in Elixir |
| `room_id` | string | Room that contains the parent event |
| `event_id` | string | Parent event ID (thread root) |
| `rel_type` | string | Relation type, e.g. `"m.thread"`; empty = all relation types (Story 9-29) |
| `limit` | int32 | Max events returned; 0 = default 20; clamped to 100 |
| `event_type` | string | Filter by event type; empty = all event types (Story 9-29, field 6) |
| `dir` | string | `"f"` = oldest-first ASC; `"b"` = newest-first DESC (default) (Story 9-29, field 7) |
| `recurse` | bool | Accepted without error; MVP passes through (Story 9-29, field 8) |
| `from` | string | Opaque pagination token; empty = first page (Story 9-29, field 9) |

Response: `repeated Event events` + `string next_batch` (empty when no more pages) + `string prev_batch` (for dir=b backward pagination, Story 9-29, field 3).

> **Migration 000042 (Story 9-28):** `CREATE INDEX CONCURRENTLY … ON events
> ((content->'m.relates_to'->>'event_id')) WHERE content ? 'm.relates_to'` — expression index
> on the `m.relates_to` JSONB field; required by `fetch_events_by_relation/5` and
> `count_thread_children/2` to avoid sequential scans on the events table.

> **`Nebu.Search.DB` — membership-scoped FTS query layer (Story 11-2):**
> `core/apps/event_dispatcher/lib/nebu/search/db.ex` defines the SQL contract for
> `POST /_matrix/client/v3/search` (Epic 11). Key design invariants:
>
> 1. **SQL-layer membership enforcement** — the subquery `WHERE room_id IN (SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL)` runs inside the same PostgreSQL query. There is no application-layer post-filter; membership is checked at query execution time. This prevents cross-room IDOR leakage even if Elixir application logic is bypassed.
>
> 2. **Encrypted-room exclusion** — rooms that have an `m.room.encryption` state event (`state_key = '' OR state_key IS NULL`) are excluded from search results via `NOT EXISTS (SELECT 1 FROM events enc WHERE enc.room_id = e.room_id AND enc.event_type = 'm.room.encryption')`. Ciphertext bodies are never returned in plaintext search responses.
>
> 3. **`user_id` security invariant** — the `user_id` parameter MUST be sourced from the validated session (gRPC metadata or JWT claim), never from the request payload. Passing a caller-supplied user_id bypasses all membership enforcement and enables cross-room IDOR. This invariant is enforced by the caller (Story 11.3 SearchMessages handler) not by `Nebu.Search.DB` itself.
>
> 4. **`websearch_to_tsquery` + `pg_catalog.simple`** — consistent with the trigger configuration in migration 000042 (ADR-010). `ts_rank_cd` for result ordering (density-aware ranking).
>
> 5. **Module placement** — in `event_dispatcher` (not `room_manager`) because the gRPC search handler (Story 11.3) lives in `Nebu.EventDispatcher.Server`. Adding search to `Nebu.Room.DB` would violate the single-responsibility principle and pollute `Nebu.Room.DBBehaviour`.

> **Integration test infrastructure (Story 11-2):** `Makefile` gains a `test-integration-elixir` target that runs ExUnit tests tagged `@tag :integration` against a live PostgreSQL instance (`NEBU_TEST_DB_URL`). The 6 search integration tests (AC1 cross-room scope, AC2 structural SQL shape, AC3 kicked-user exclusion, zero-membership guard, encrypted-room exclusion, multi-room inclusion) are excluded from the `test-unit-elixir` target via `ExUnit.configure(exclude: [:integration])` in `event_dispatcher/test/test_helper.exs`.

> **SearchMessages gRPC handler (Story 11-3):** `Nebu.EventDispatcher.Server.search_messages/2` is the
> gRPC entry point for `POST /_matrix/client/v3/search` (implemented in Story 11-4). Key design decisions:
>
> 1. **Delegated search module** — the handler delegates to `search_db_module()` (runtime-swappable via `Application.get_env(:event_dispatcher, :search_db_module, Nebu.Search.DB)`) to keep SQL logic in `Nebu.Search.DB` and make the handler unit-testable with a fake via `FakeSearchDB`.
>
> 2. **`user_id` from trusted metadata only** — `{user_id, _} = Nebu.Grpc.Metadata.trusted_identity(stream)`. The `request.user_id` field is intentionally ignored. This enforces the Story 11-2 security invariant at the transport layer.
>
> 3. **Pagination** — `next_batch` is `Base64(Integer.to_string(offset + limit))`. An empty string signals no more pages. The handler caps `offset` at `10_000` to prevent unbounded deep-paging queries (Kassandra MEDIUM finding, fixed inline).
>
> 4. **Limit clamping** — `limit` is clamped to `[1, 100]`; zero defaults to 10.
>
> 5. **Proto additions** — `core.proto` gains `ProfileInfo`, `SearchResult`, `SearchMessagesRequest`, `SearchMessagesResponse` message types and the `SearchMessages` RPC. Go stubs auto-regenerated via `make proto`; Elixir `core_grpc.pb.ex` service stub updated manually (protoc-gen-elixir does not auto-update the service module).

**`GetEventRequest` / `GetEventResponse` fields (Story 11-8):**

| Field | Type | Description |
|---|---|---|
| `room_id` | string | Room that owns the event; scopes the DB query |
| `event_id` | string | Unique event ID to fetch |
| `user_id` | string | Caller; Elixir enforces joined-member check via Horde room state |

Response: `Event event` — the full event proto (same `Event` message used by sync and relations endpoints).

> **`GetEvent` gRPC handler design (Story 11-8):** `Nebu.EventDispatcher.Server.get_event/2` looks up
> the room via `Nebu.Room.RoomSupervisor.lookup_room/1` (Horde registry). If the room is unknown, it
> raises `GRPC.RPCError` with `NOT_FOUND`. It then calls `room_registry_module().get_state/1` and checks
> `MapSet.member?(state.members, user_id)` — non-members receive `PERMISSION_DENIED`. On success it
> delegates to `messages_db_module().fetch_event/2` (SQL: `SELECT … WHERE event_id=$1 AND room_id=$2 LIMIT 1`)
> and calls `attach_thread_aggregations/3` so the returned event already carries bundled `m.thread`
> aggregation data (same as sync responses). The `GetEvent` RPC and its message types were added to
> `core.proto`; the Elixir `core_grpc.pb.ex` service stub was updated manually (`rpc :GetEvent` entry
> added alongside the pre-existing `rpc :GetRelations` fix).

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Project Structure & Boundaries, §Complete Project Directory Structure; Story 9-19 (room_moderation.go, sync.go, event_dispatcher/server.ex, forgotten_rooms migration); Story 9-22 (per-device sync tokens, device_id in proto); Story 9-24 (GlobalAccountDataDB interface, ListGlobalAccountData, top-level account_data in syncResponse); Story 9-25 (syntheticNextBatch helper, syntheticBatchSeq atomic counter, sinceToken param removed from buildResponseFromBufferedEvents); Story 9-27 (upgrade_room/2 full Matrix §11.35.1 flow, GRPC.RPCError error handling, archive_room_atomic, terminate_child, try/rescue failure audit); Story 9-28 (GetRelations RPC, unsigned_relations field on Event, attach_thread_aggregations, fetch_events_by_relation, count_thread_children, event_in_room?, migration 000042); Story 9-29 (base /relations/{eventId} route, three-segment /{relType}/{eventType} route, dir/event_type/recurse/from query params, prev_batch in response, fetch_events_by_relation/5 dynamic WHERE builder); Story 11-3 (SearchMessages gRPC handler, proto extension, delegated search_db_module pattern, offset-cap security fix); Story 11-4 (search.go Gateway handler, SearchCoreClient consumer interface, §11.14.1 response shape, gRPC error mapping); Story 11-5 (NewUserRateLimiter middleware, per-user 10 req/min for /search, retry_after_ms in body); Story 11-8 (GetEvent RPC, event.go Go handler, fetch_event/2 DB function, core_grpc.pb.ex rpc :GetEvent + rpc :GetRelations bug fix); Story 11-9 (health/info.go NewInfoHandler, Nebu.BuildInfo module, GET /info on pubMux + health server, Admin UI footer via page_data.go SetBuildInfo/newPageData, ErrorMode on PageData, Dockerfile ARG/ldflags injection, docker-compose.yml build args)_

---
security_review: required
---

# Story 6.10: Server Config API + Metrics API

Status: review

## Story

As an instance admin,
I want to read and update server-wide configuration, and query live instance metrics,
so that I can manage the instance settings programmatically and monitor the instance health via API.

## Acceptance Criteria

1. `GET /api/v1/admin/config` — `instance_admin` role required:
   - Returns all readable `server_config` keys as a JSON object:
     `{"instance_name": "...", "oidc_issuer": "...", "oidc_client_id": "...", "room_default_max_members": N, "room_default_visibility": "...", "audit_log_retention_days": N}`
   - `oidc_client_secret` is **never** returned (write-only field — must be absent from all response paths)
   - `room_default_max_members` and `room_default_visibility` come from the `room_defaults` table (via `RoomDefaultsRepository.GetRoomDefaults`), not from `server_config`
   - `audit_log_retention_days` comes from `server_config` (key `audit_log_retention_days`)
   - Missing keys return their default: `instance_name` → `""`, `oidc_issuer` → `""`, `oidc_client_id` → `""`, `audit_log_retention_days` → `2555`

2. `PATCH /api/v1/admin/config` — `instance_admin` role required; body: partial update with any subset of updatable keys:
   - Updatable keys: `instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`, `audit_log_retention_days`
   - `oidc_client_secret` is encrypted with AES-256-GCM (the internal secret) before being written to `server_config` — same encryption used in bootstrap (see `admin.postgresServerConfigReader`)
   - If `oidc_issuer`, `oidc_client_id`, or `oidc_client_secret` is changed: calls gRPC `InvalidateAllAdminSessions()` (new RPC — all active admin UI sessions destroyed)
   - All changes are upserted into `server_config` via the `ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at` pattern (same as `SaveAdminGroupClaim` in `gateway/internal/admin/auth.go:148`)
   - Calls `audit.LogEvent(ctx, s.CoreClient, actorID, "server_config_updated", "server", "config", map[string]any{"changed_keys": keys}, "success", "")` — never-raise pattern
   - Returns `200` with the full updated config object (same format as AC#1 GET)
   - Returns `400` if `audit_log_retention_days` is set but not in range 1–36500

3. `GET /api/v1/admin/metrics` — `instance_admin` role required:
   - Returns: `{"active_sessions": N, "room_count": N, "archived_room_count": N, "msg_per_sec_1m": float, "registered_users": N, "deactivated_users": N}`
   - `active_sessions`: via gRPC `GetMetrics` (existing RPC, field `active_sessions` — see proto line 224). **No new gRPC RPC needed.**
   - `msg_per_sec_1m`: via gRPC `GetMetrics` (field `msg_per_sec` — same existing RPC). The epics spec says "approximation from recent events"; the existing `GetMetrics` RPC already returns this value.
   - `room_count` (active rooms): DB query `SELECT COUNT(*) FROM rooms WHERE status = 'active'`
   - `archived_room_count`: DB query `SELECT COUNT(*) FROM rooms WHERE status = 'archived'`
   - `registered_users`: DB query `SELECT COUNT(*) FROM users WHERE is_active = true`
   - `deactivated_users`: DB query `SELECT COUNT(*) FROM users WHERE is_active = false`

4. New gRPC RPC `InvalidateAllAdminSessions` in `proto/core.proto`:
   - `rpc InvalidateAllAdminSessions(InvalidateAllAdminSessionsRequest) returns (InvalidateAllAdminSessionsResponse)`
   - `InvalidateAllAdminSessionsRequest {}` (no fields)
   - `InvalidateAllAdminSessionsResponse { bool ok = 1; }`
   - Elixir implementation: iterates `Nebu.Session.EtsStore.list_sessions()`, calls `Nebu.Session.SessionSupervisor.destroy_session/1` for each entry; returns `ok: true`

5. Unit tests (Go):
   - `GET /api/v1/admin/config` — response never contains `oidc_client_secret` key
   - `PATCH /api/v1/admin/config` with `oidc_issuer` change → mock gRPC `InvalidateAllAdminSessions` called
   - `GET /api/v1/admin/metrics` — response contains all 6 required fields with correct types
   - Router test: GET `/api/v1/admin/config` without `ServerConfig` repo wired → 501
   - Router test: PATCH `/api/v1/admin/config` without `ServerConfig` repo wired → 501
   - Router test: GET `/api/v1/admin/metrics` without `Metrics` repo wired → 501

6. Unit tests (Elixir):
   - `InvalidateAllAdminSessions` gRPC handler: with 2 sessions in ETS → `destroy_session` called twice; returns `ok: true`
   - `InvalidateAllAdminSessions` gRPC handler: empty ETS → returns `ok: true` (no-op)

7. `go build ./...`, `make test-unit-go`, and `make test-unit-elixir` pass with zero failures.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **GET /admin/config never exposes oidc_client_secret** — Go unit test
   - Given: `ServerConfigRepository` mock returns `oidc_client_secret = "supersecret"` from DB
   - When: `GET /api/v1/admin/config` with valid instance_admin JWT
   - Then: status 200; response body does NOT contain `"oidc_client_secret"` as a key

2. **PATCH /admin/config with oidc_issuer → admin sessions invalidated** — Go unit test
   - Given: `ServerConfigRepository` mock; mock gRPC client with `InvalidateAllAdminSessions` recorded
   - When: `PATCH /api/v1/admin/config` with body `{"oidc_issuer": "https://new.dex.example"}`
   - Then: status 200; mock `InvalidateAllAdminSessions` was called exactly once

3. **PATCH /admin/config without OIDC fields → sessions NOT invalidated** — Go unit test
   - Given: mock gRPC client
   - When: `PATCH /api/v1/admin/config` with body `{"instance_name": "New Name"}`
   - Then: status 200; mock `InvalidateAllAdminSessions` was NOT called

4. **GET /admin/metrics returns all required fields with correct types** — Go unit test
   - Given: `MetricsRepository` mock returns `(room_count=5, archived_room_count=2, registered_users=10, deactivated_users=1)`; mock gRPC `GetMetrics` returns `{active_sessions: 3, msg_per_sec: 1.5}`
   - When: `GET /api/v1/admin/metrics`
   - Then: status 200; body contains `active_sessions`, `room_count`, `archived_room_count`, `msg_per_sec_1m`, `registered_users`, `deactivated_users` all with numeric values

5. **Router 501 tests: GET /admin/config, PATCH /admin/config, GET /admin/metrics** — Go unit tests
   - Given: `AdminServer{}` with no repositories wired
   - When: requests to each route
   - Then: status 501

6. **Elixir: InvalidateAllAdminSessions — 2 sessions in ETS → destroy called twice** — ExUnit
   - Given: ETS has sessions for user_a and user_b
   - When: `InvalidateAllAdminSessions` gRPC called
   - Then: `SessionSupervisor.destroy_session/1` called for both users; returns `%Core.InvalidateAllAdminSessionsResponse{ok: true}`

7. **Elixir: InvalidateAllAdminSessions — empty ETS → ok: true** — ExUnit
   - Given: ETS table is empty
   - When: `InvalidateAllAdminSessions` gRPC called
   - Then: returns `%Core.InvalidateAllAdminSessionsResponse{ok: true}` without error

## Tasks / Subtasks

- [x] Write FAILING tests first (RED phase) — `gateway/internal/api/config_handler_test.go` (AC: #1–#3, #5) + `gateway/internal/api/metrics_handler_test.go` (AC: #4, #5)
  - [x] 5 Go test cases: GET config (no secret), PATCH oidc_issuer (sessions invalidated), PATCH instance_name (no invalidation), GET metrics (all fields), router 501 tests (3 routes)
  - [x] Define mock `ServerConfigRepository` interface with `GetServerConfig` + `UpsertServerConfigKey` methods
  - [x] Define mock `MetricsRepository` interface with `GetMetricsCounts` method

- [x] Write FAILING Elixir tests — `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_all_admin_sessions_test.exs` (AC: #6–#7)
  - [x] 2 ExUnit tests for `InvalidateAllAdminSessions` gRPC handler

- [x] Extend `proto/core.proto` with `InvalidateAllAdminSessions` RPC (AC: #4)
  - [x] Add `rpc InvalidateAllAdminSessions(InvalidateAllAdminSessionsRequest) returns (InvalidateAllAdminSessionsResponse)` to `CoreService`
  - [x] Add message types: `InvalidateAllAdminSessionsRequest {}`, `InvalidateAllAdminSessionsResponse { bool ok = 1; }`
  - [x] Run `make proto` to regenerate Go stubs + Elixir stubs

- [x] Extend `gateway/api/openapi.yaml` with proper schemas (AC: #1, #2)
  - [x] Replace `EmptyResponse` placeholder for `GET /admin/config` with `AdminConfigResponse` schema (6 readable fields; no `oidc_client_secret`)
  - [x] Add `PATCH /admin/config` path with `PatchAdminConfigRequest` schema (5 optional fields; `operationId: PatchAdminConfig`)
  - [x] Replace `EmptyResponse` placeholder for `GET /admin/metrics` with `AdminMetricsResponse` schema (6 numeric fields)
  - [x] Run `make gen-api` to regenerate `api_gen.go`

- [x] Create `gateway/internal/api/server_config_repo.go` (AC: #1, #2)
  - [x] `ServerConfigRepository` interface: `GetServerConfig(ctx) (*ServerConfigData, error)` + `UpsertServerConfigKey(ctx, key, value string) error`
  - [x] `ServerConfigData` struct: `InstanceName, OIDCIssuer, OIDCClientID string; AuditLogRetentionDays int`
  - [x] `dbServerConfigRepo` implementation: `GetServerConfig` reads all keys via `SELECT key, value FROM server_config WHERE key IN (...)` — same query pattern as `LoadOIDCConfig` in `gateway/internal/admin/auth.go`
  - [x] `UpsertServerConfigKey`: `INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at` (same pattern as `SaveAdminGroupClaim`)

- [x] Create `gateway/internal/api/metrics_repo.go` (AC: #3)
  - [x] `MetricsRepository` interface: `GetMetricsCounts(ctx) (*MetricsCounts, error)`
  - [x] `MetricsCounts` struct: `RoomCount, ArchivedRoomCount, RegisteredUsers, DeactivatedUsers int`
  - [x] `dbMetricsRepo` implementation with FILTER aggregate queries for rooms and users

- [x] Add `ServerConfig` and `Metrics` fields to `AdminServer` in `gateway/internal/api/server.go` (AC: #1, #2, #3)
  - [x] `ServerConfig ServerConfigRepository` — Story 6.10
  - [x] `Metrics MetricsRepository` — Story 6.10

- [x] Implement `GetAdminConfig` handler in `gateway/internal/api/server.go` (AC: #1)
  - [x] Replace stub `GetAdminConfig501Response{}` with real implementation
  - [x] nil-guard → 501 if `s.ServerConfig == nil || s.RoomDefaults == nil`
  - [x] Query `s.ServerConfig.GetServerConfig(ctx)` → get instance_name, oidc_issuer, oidc_client_id, audit_log_retention_days
  - [x] Query `s.RoomDefaults.GetRoomDefaults(ctx)` → get room_default_max_members, room_default_visibility
  - [x] `oidc_client_secret` is never included in response (custom response types with no omitempty pointers)
  - [x] Return JSON with all 6 fields always present (custom struct without omitempty)

- [x] Implement `PatchAdminConfig` handler in `gateway/internal/api/server.go` (AC: #2)
  - [x] nil-guard → 501 if `s.ServerConfig == nil`
  - [x] Validate `audit_log_retention_days` if present: 1–36500
  - [x] Encrypt `oidc_client_secret` with AES-256-GCM before storing (`Secret []byte` field on `AdminServer`)
  - [x] Upsert each changed key via `s.ServerConfig.UpsertServerConfigKey(ctx, key, encryptedOrPlainValue)`
  - [x] OIDC invalidation: if any of `oidc_issuer`, `oidc_client_id`, `oidc_client_secret` changed → call `s.CoreClient.InvalidateAllAdminSessions(ctx, &pb.InvalidateAllAdminSessionsRequest{})`
  - [x] Emit audit log (never-raise pattern)
  - [x] Return GET response (full config object via bridge response type)

- [x] Implement `GetAdminMetrics` handler in `gateway/internal/api/server.go` (AC: #3)
  - [x] nil-guard → 501 if `s.Metrics == nil`
  - [x] Call `s.Metrics.GetMetricsCounts(ctx)` for DB counts
  - [x] Call `s.CoreClient.GetMetrics(ctx, &pb.GetMetricsRequest{})` for `active_sessions` and `msg_per_sec`
  - [x] Custom response type with all 6 fields without omitempty

- [x] Register `PATCH /api/v1/admin/config` in `gateway/internal/api/router.go` (AC: #2)
  - [x] `patchAdminConfigHandler` with nil-guard and body pre-check wrapper

- [x] Wire new repositories in `gateway/cmd/gateway/main.go` (AC: #1, #2, #3)
  - [x] `serverConfigRepo := apihandler.NewServerConfigRepo(bootstrapDB)`
  - [x] `metricsRepo := apihandler.NewMetricsRepo(bootstrapDB)`
  - [x] Added `ServerConfig: serverConfigRepo, Metrics: metricsRepo, Secret: internalSecret` to `adminSrv`

- [x] Implement Elixir `invalidate_all_admin_sessions` gRPC handler in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (AC: #4, #6)
  - [x] Added `list_user_ids/0` to `Nebu.Session.EtsStore`
  - [x] `invalidate_all_admin_sessions/2`: iterates `EtsStore.list_user_ids()`, calls `session_supervisor_module().destroy_session/1` for each
  - [x] Returns `%Core.InvalidateAllAdminSessionsResponse{ok: true}` always (best-effort)

- [x] GREEN phase: `make test-unit-go` → 0 failures; `make test-unit-elixir` → 0 failures; `make build-gateway` → success

## Dev Notes

### CRITICAL: server_config INSERT vs UPDATE — Use ON CONFLICT DO UPDATE

`server_config` has FORCE RLS with only INSERT and SELECT policies (migration `000003`). However, looking at **existing working code** (`gateway/internal/admin/auth.go:148`), `SaveAdminGroupClaim` uses:

```sql
INSERT INTO server_config (key, value, set_at)
VALUES ('admin_group_claim', $1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at
```

This works because the `nebu_app` role has an INSERT policy (which also enables the upsert's UPDATE path). The `config_insert_only` RLS policy covers INSERT, and the `ON CONFLICT DO UPDATE` triggers the update via the same INSERT policy. **Use this exact pattern for all server_config writes in this story.**

**Story 6-8 dev notes were confused about this** — the `ON CONFLICT DO UPDATE` approach IS correct and IS used in production code already. Do NOT use `DO NOTHING`.

### CRITICAL: PATCH /admin/config does not exist in openapi.yaml yet

Currently `openapi.yaml` only has `GET /admin/config` and `GET /admin/metrics` as placeholders. Story 6.10 must:
1. Add `PATCH /admin/config` with `operationId: PatchAdminConfig` to `openapi.yaml`
2. Run `make gen-api` to generate `PatchAdminConfigRequestObject`, `PatchAdminConfigResponseObject`, `PatchAdminConfig501Response` etc. in `api_gen.go`
3. Add `PatchAdminConfig` to `StrictServerInterface` — meaning `AdminServer` must implement it too

The router already has `GET /api/v1/admin/config` and `GET /api/v1/admin/metrics` registered (router.go:37–38). Only `PATCH /api/v1/admin/config` needs a new registration.

### CRITICAL: oidc_client_secret encryption

The internal secret (AES-256-GCM key) is loaded in `main.go` from `NEBU_INTERNAL_SECRET_FILE`. It is currently only passed to `admin.AdminAuth`. For Story 6.10:

- `AdminServer` needs a `Secret []byte` field (the AES-256-GCM master key)
- Encryption helper already exists in `gateway/internal/admin/auth.go` as `encryptAES256GCM(secret, plaintext)` — check if it's exported or needs to be exposed
- If not exported, either export it or duplicate the short encrypt helper in `gateway/internal/api/` (small function, acceptable for package isolation)
- **Never log or return the plaintext secret** — use `slog.Debug` only with a placeholder

### CRITICAL: GetAdminMetrics — use existing GetMetrics gRPC, not a new RPC

The existing `rpc GetMetrics(GetMetricsRequest) returns (GetMetricsResponse)` in `core.proto` (line 25) already returns:
- `msg_per_sec float32` (field 1)
- `active_sessions int32` (field 2)
- `room_count int32` (field 3)

The story instructions say "active_sessions via gRPC CountActiveSessions" — but there is no such RPC. Use `GetMetrics` (which already has `active_sessions`). The `room_count` from `GetMetrics` may conflict with our DB-derived counts; use the DB for `room_count` and `archived_room_count` (more accurate), use gRPC for `active_sessions` and `msg_per_sec_1m` (in-memory Elixir state).

**Do NOT add a new CountActiveSessions RPC.** The existing `GetMetrics` is sufficient.

### CRITICAL: InvalidateAllAdminSessions — EtsStore.list_sessions() strips user_ids

`Nebu.Session.EtsStore.list_sessions/0` returns `[map()]` — maps without user_ids. To destroy sessions you need user_ids. Solutions:
1. **Recommended:** Add `list_user_ids/0` to `EtsStore`: `:ets.tab2list(:NebuSessions) |> Enum.map(fn {uid, _} -> uid end)` — small, focused, testable
2. Alternative: iterate raw `:ets.tab2list(:NebuSessions)` in the gRPC handler directly

### AdminServer field additions (incremental growth pattern)

Every story adds a field to `AdminServer`. For 6.10 add:
```go
type AdminServer struct {
    // ... existing fields ...
    ServerConfig ServerConfigRepository  // Story 6.10
    Metrics      MetricsRepository       // Story 6.10
    Secret       []byte                  // Story 6.10: AES-256-GCM key for oidc_client_secret encryption
}
```

### GetAdminConfig: room_default_max_members and room_default_visibility

These come from `room_defaults` table (migration 000037, managed by `RoomDefaultsRepository`). The `GetAdminConfig` handler should call `s.RoomDefaults.GetRoomDefaults(ctx)` — this repository is already wired in `adminSrv` from Story 6.8. Add a nil-guard for `s.RoomDefaults` as well.

### Audit log pattern (never-raise)

All handlers follow the same pattern established in server.go:
```go
if s.CoreClient != nil {
    actorID, _ := ctx.Value(middleware.ContextKeyUserID).(string)
    _ = audit.LogEvent(ctx, s.CoreClient, actorID, "server_config_updated", "server", "config",
        map[string]any{"changed_keys": changedKeys}, "success", "")
} else {
    slog.Warn("PatchAdminConfig audit skipped — CoreClient is nil")
}
```

### Migration number

The next available migration number is **000038** (last is 000037_room_defaults). However, this story does NOT require a new migration — `server_config`, `rooms`, and `users` tables all exist with all needed columns.

### Elixir: session_supervisor_module() injectable in tests

The handler uses `session_supervisor_module()` (like `invalidate_user_sessions` at server.ex:699). In tests, this is typically replaced with a mock module. Follow the same pattern as the existing `invalidate_user_sessions` handler.

### openapi.yaml — AdminConfigResponse schema

```yaml
AdminConfigResponse:
  type: object
  properties:
    instance_name:
      type: string
    oidc_issuer:
      type: string
    oidc_client_id:
      type: string
    room_default_max_members:
      type: integer
    room_default_visibility:
      type: string
    audit_log_retention_days:
      type: integer
# NOTE: oidc_client_secret intentionally absent
```

```yaml
PatchAdminConfigRequest:
  type: object
  properties:
    instance_name:
      type: string
    oidc_issuer:
      type: string
    oidc_client_id:
      type: string
    oidc_client_secret:
      type: string
    audit_log_retention_days:
      type: integer
      minimum: 1
      maximum: 36500
# All fields optional — partial update semantics
```

```yaml
AdminMetricsResponse:
  type: object
  properties:
    active_sessions:
      type: integer
    room_count:
      type: integer
    archived_room_count:
      type: integer
    msg_per_sec_1m:
      type: number
      format: float
    registered_users:
      type: integer
    deactivated_users:
      type: integer
```

### File locations

| File | Action |
|---|---|
| `gateway/api/openapi.yaml` | UPDATE: replace EmptyResponse placeholders, add PATCH /admin/config |
| `gateway/internal/api/api_gen.go` | REGENERATED by `make gen-api` |
| `gateway/internal/api/server.go` | UPDATE: implement GetAdminConfig, PatchAdminConfig (new), GetAdminMetrics; add fields to AdminServer |
| `gateway/internal/api/router.go` | UPDATE: register PATCH /api/v1/admin/config |
| `gateway/internal/api/server_config_repo.go` | NEW: ServerConfigRepository interface + dbServerConfigRepo |
| `gateway/internal/api/metrics_repo.go` | NEW: MetricsRepository interface + dbMetricsRepo |
| `gateway/internal/api/config_handler_test.go` | NEW: unit tests for GET/PATCH config |
| `gateway/internal/api/metrics_handler_test.go` | NEW: unit tests for GET metrics |
| `gateway/cmd/gateway/main.go` | UPDATE: wire serverConfigRepo, metricsRepo, Secret into adminSrv |
| `proto/core.proto` | UPDATE: add InvalidateAllAdminSessions RPC + messages |
| `gateway/internal/grpc/pb/core.pb.go` | REGENERATED by `make proto` |
| `gateway/internal/grpc/pb/core_grpc.pb.go` | REGENERATED by `make proto` |
| `core/apps/event_dispatcher/lib/pb/core.pb.ex` | REGENERATED by `make proto` |
| `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` | REGENERATED by `make proto` |
| `core/apps/session_manager/lib/nebu/session/ets_store.ex` | UPDATE: add `list_user_ids/0` |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | UPDATE: add `invalidate_all_admin_sessions/2` handler |
| `core/apps/event_dispatcher/test/.../invalidate_all_admin_sessions_test.exs` | NEW: ExUnit tests |

### Security: PATCH /admin/config sensitivity

This endpoint is `security_review: required` because:
- It writes OIDC secrets to the database (encrypted, but still a sensitive path)
- It triggers mass session invalidation affecting all admin users
- Changing `oidc_issuer` effectively changes the authentication provider for the entire instance
- Must NOT be accessible without valid `instance_admin` JWT (enforced by `RequireRole`)
- `oidc_client_secret` must be encrypted before storage, never logged in plaintext

### Previous story learnings applicable to this story

From Story 6.9 (rooms archive):
- Always follow the nil-guard → 501 pattern for all new handlers
- Audit log is always never-raise (`_ = audit.LogEvent(...)`)
- gRPC errors on best-effort calls are always logged with `slog.Warn`, never returned as errors

From Story 6.8 (room defaults):
- Use `RoomDefaultsRepository.GetRoomDefaults` — already wired in adminSrv
- The `server_config` table confusion was resolved: `ON CONFLICT DO UPDATE` works via the INSERT RLS policy

From Story 6.6 (role overrides):
- `RequireRole` middleware already handles DB-override lookup; no changes needed for this story's auth

From Story 6.5 (deactivation):
- `InvalidateUserSessions` gRPC pattern (server.ex:699): `session_supervisor_module().destroy_session(user_id)` — follow same pattern for `InvalidateAllAdminSessions`

### References

- `gateway/internal/api/server.go` — AdminServer struct, stub impls, handler patterns
- `gateway/internal/api/router.go` — route registration pattern
- `gateway/internal/api/room_defaults_repo.go` — repo interface + implementation pattern
- `gateway/internal/admin/auth.go:64–98` — server_config read pattern (LoadOIDCConfig)
- `gateway/internal/admin/auth.go:146–153` — server_config upsert pattern (SaveAdminGroupClaim, ON CONFLICT DO UPDATE)
- `gateway/internal/admin/sse.go` — existing GetMetrics gRPC usage (admin SSE endpoint)
- `proto/core.proto:25,219–226` — existing GetMetrics RPC and GetMetricsResponse fields
- `proto/core.proto:81–84,437–444` — InvalidateUserSessions pattern to follow for InvalidateAllAdminSessions
- `core/apps/session_manager/lib/nebu/session/ets_store.ex:90–94` — list_sessions implementation
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:699–709` — invalidate_user_sessions handler pattern
- `gateway/migrations/000003_server_config.up.sql` — server_config schema (INSERT-only RLS)
- `gateway/migrations/000028_audit_log_purge_owner.up.sql` — retention_days bounds (1–36500)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- All 7 acceptance criteria implemented and verified with `make test-unit-go` (0 failures), `make test-unit-elixir` (0 failures), `make build-gateway` (success).
- `oidc_client_secret` is never returned by GET or PATCH /admin/config — enforced via custom response structs without the field and without omitempty.
- Generated code (`api_gen.go`) uses pointer+omitempty fields; custom response types with plain fields are used for both GET and PATCH to ensure all 6 config fields and all 6 metrics fields always appear in JSON.
- `PatchAdminConfig` returns `PatchAdminConfigResponseObject` via a bridge type (`patchAdminConfig200Resp`) that embeds `*getAdminConfigOKResponse` and delegates `VisitPatchAdminConfigResponse` → `VisitGetAdminConfigResponse`.
- After `make proto` regenerated `CoreServiceClient` interface (adding `InvalidateAllAdminSessions`), all mock implementations in test files were updated: `compliance/handler_test.go`, `audit/writer_test.go`, `grpc/stream_test.go`, `admin/auth_audit_test.go`.
- Files using embedded `pb.CoreServiceClient` interface (rooms_archive_handler_test.go, rooms_patch_handler_test.go, etc.) required no changes.
- Elixir: added `list_user_ids/0` to `EtsStore` (needed because `list_sessions/0` strips user_ids). Added `invalidate_all_admin_sessions/2` handler following the same `session_supervisor_module()` injection pattern as `invalidate_user_sessions/2`.

### File List

- `proto/core.proto` — Updated: added InvalidateAllAdminSessions RPC + request/response messages
- `gateway/internal/grpc/pb/core.pb.go` — Regenerated by make proto
- `gateway/internal/grpc/pb/core_grpc.pb.go` — Regenerated by make proto
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — Regenerated by make proto
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — Regenerated by make proto
- `gateway/api/openapi.yaml` — Updated: AdminConfigResponse, PatchAdminConfigRequest, AdminMetricsResponse schemas + PATCH /admin/config path
- `gateway/internal/api/api_gen.go` — Regenerated by make gen-api
- `gateway/internal/api/server_config_repo.go` — New: ServerConfigRepository interface + dbServerConfigRepo implementation
- `gateway/internal/api/metrics_repo.go` — New: MetricsRepository interface + dbMetricsRepo implementation
- `gateway/internal/api/server.go` — Updated: GetAdminConfig, PatchAdminConfig (new), GetAdminMetrics handlers + new AdminServer fields (ServerConfig, Metrics, Secret) + custom response types + encryptAES256GCMForAPI helper
- `gateway/internal/api/router.go` — Updated: patchAdminConfigHandler + PATCH /api/v1/admin/config route registration
- `gateway/cmd/gateway/main.go` — Updated: wire serverConfigRepo, metricsRepo, Secret into adminSrv
- `gateway/internal/api/config_handler_test.go` — New: unit tests for GET/PATCH /admin/config (AC #1–#3, #5)
- `gateway/internal/api/metrics_handler_test.go` — New: unit tests for GET /admin/metrics (AC #4, #5)
- `core/apps/session_manager/lib/nebu/session/ets_store.ex` — Updated: added list_user_ids/0
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — Updated: added invalidate_all_admin_sessions/2 handler
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_all_admin_sessions_test.exs` — New: ExUnit tests (AC #6–#7)
- `gateway/internal/compliance/handler_test.go` — Updated: added InvalidateAllAdminSessions stub to mockCoreClient
- `gateway/internal/audit/writer_test.go` — Updated: added InvalidateAllAdminSessions stub to mockCoreClient
- `gateway/internal/grpc/stream_test.go` — Updated: added InvalidateAllAdminSessions stub to mockCoreClient
- `gateway/internal/admin/auth_audit_test.go` — Updated: added InvalidateAllAdminSessions stub to mockCoreClient

## Change Log

- 2026-05-01: Story implemented by claude-sonnet-4-6. All tasks complete. make test-unit-go 0 failures, make test-unit-elixir 0 failures, make build-gateway success. Status → review.

---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-05-01'
storyId: '6-10'
storyKey: '6-10'
storyFile: '_bmad-output/implementation-artifacts/6-10-server-config-api-metrics-api.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist/atdd-checklist-6-10.md'
generatedTestFiles:
  - gateway/internal/api/config_handler_test.go
  - gateway/internal/api/metrics_handler_test.go
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_all_admin_sessions_test.exs
---

# ATDD Checklist: Story 6-10 — Server Config API + Metrics API

**Story:** As an instance admin, I want to read and update server-wide configuration, and query live instance metrics.
**Status:** RED PHASE COMPLETE — tests generated, implementation pending
**Date:** 2026-05-01
**Test Architect:** Master Test Architect (ATDD Skill)

---

## Acceptance Criteria Coverage

| AC | Description | Test File | Test Name | Priority | Status |
|----|-------------|-----------|-----------|----------|--------|
| AC#1 | GET /admin/config — oidc_client_secret NEVER in response | config_handler_test.go | TestGetAdminConfig_OIDCClientSecretNeverInResponse | P0 | RED |
| AC#1 | GET /admin/config — returns correct values from both repos | config_handler_test.go | TestGetAdminConfig_ReturnsCorrectValues | P0 | RED |
| AC#1 | GET /admin/config — missing keys return defaults | config_handler_test.go | TestGetAdminConfig_MissingKeys_ReturnsDefaults | P1 | RED |
| AC#2 | PATCH /admin/config — oidc_issuer change → InvalidateAllAdminSessions called | config_handler_test.go | TestPatchAdminConfig_OIDCIssuerChange_InvalidatesAllAdminSessions | P0 | RED |
| AC#2 | PATCH /admin/config — oidc_client_id change → sessions invalidated | config_handler_test.go | TestPatchAdminConfig_OIDCClientIDChange_InvalidatesAllAdminSessions | P0 | RED |
| AC#2 | PATCH /admin/config — oidc_client_secret change → sessions invalidated | config_handler_test.go | TestPatchAdminConfig_OIDCClientSecretChange_InvalidatesAllAdminSessions | P0 | RED |
| AC#2 | PATCH /admin/config — instance_name change → NO session invalidation | config_handler_test.go | TestPatchAdminConfig_InstanceNameChange_NoSessionInvalidation | P0 | RED |
| AC#2 | PATCH /admin/config — audit_log change → NO session invalidation | config_handler_test.go | TestPatchAdminConfig_AuditLogRetentionChange_NoSessionInvalidation | P1 | RED |
| AC#2 | PATCH /admin/config — audit_log_retention_days=0 → 400 | config_handler_test.go | TestPatchAdminConfig_AuditLogRetentionDays_TooLow_Returns400 | P0 | RED |
| AC#2 | PATCH /admin/config — audit_log_retention_days=36501 → 400 | config_handler_test.go | TestPatchAdminConfig_AuditLogRetentionDays_TooHigh_Returns400 | P0 | RED |
| AC#2 | PATCH /admin/config — audit_log_retention_days=36500 (max) → 200 | config_handler_test.go | TestPatchAdminConfig_AuditLogRetentionDays_MaxValid_Returns200 | P1 | RED |
| AC#2 | PATCH /admin/config — audit_log_retention_days=1 (min) → 200 | config_handler_test.go | TestPatchAdminConfig_AuditLogRetentionDays_MinValid_Returns200 | P1 | RED |
| AC#2 | PATCH /admin/config — returns full config object (same as GET) | config_handler_test.go | TestPatchAdminConfig_Returns200WithFullConfigObject | P0 | RED |
| AC#3 | GET /admin/metrics — all 6 fields present | metrics_handler_test.go | TestGetAdminMetrics_AllSixFieldsPresent | P0 | RED |
| AC#3 | GET /admin/metrics — all fields have correct numeric types | metrics_handler_test.go | TestGetAdminMetrics_FieldTypes | P0 | RED |
| AC#3 | GET /admin/metrics — correct values from both DB + gRPC | metrics_handler_test.go | TestGetAdminMetrics_CorrectValues | P0 | RED |
| AC#3 | GET /admin/metrics — zero values still return all 6 fields | metrics_handler_test.go | TestGetAdminMetrics_ZeroValues_Returns200 | P1 | RED |
| AC#3 | GET /admin/metrics — nil CoreClient does not panic | metrics_handler_test.go | TestGetAdminMetrics_NilCoreClient_DoesNotPanic | P1 | RED |
| AC#5 | Router: GET /admin/config nil ServerConfig → 501 | config_handler_test.go | TestGetAdminConfig_NilServerConfigRepo_Returns501 | P0 | RED |
| AC#5 | Router: PATCH /admin/config nil ServerConfig → 501 | config_handler_test.go | TestPatchAdminConfig_NilServerConfigRepo_Returns501 | P0 | RED |
| AC#5 | Router: PATCH /admin/config route registered | config_handler_test.go | TestPatchAdminConfig_RouteRegistered | P0 | RED |
| AC#5 | Router: GET /admin/metrics nil Metrics → 501 | metrics_handler_test.go | TestGetAdminMetrics_NilMetricsRepo_Returns501 | P0 | RED |
| AC#5 | Router: GET /admin/metrics route registered (regression guard) | metrics_handler_test.go | TestGetAdminMetrics_RouteAlreadyRegistered | P0 | RED |
| AC#6 | Elixir: 2 sessions in ETS → destroy_session called twice, ok: true | invalidate_all_admin_sessions_test.exs | invalidate_all_admin_sessions — 2 sessions in ETS… | P0 | RED |
| AC#6 | Elixir: response struct has ok=true | invalidate_all_admin_sessions_test.exs | invalidate_all_admin_sessions — response struct... | P0 | RED |
| AC#7 | Elixir: empty ETS → ok: true, no destroy calls | invalidate_all_admin_sessions_test.exs | invalidate_all_admin_sessions — empty ETS → ok: true | P0 | RED |
| AC#7 | Elixir: empty ETS — no crash/error | invalidate_all_admin_sessions_test.exs | invalidate_all_admin_sessions — empty ETS does not raise | P0 | RED |

---

## Generated Test Files

### 1. `gateway/internal/api/config_handler_test.go` (Go unit tests — config handler)

**Dependencies (compilation fails until all exist):**
- `api.ServerConfigRepository` interface (server_config_repo.go)
- `api.ServerConfigData` struct (server_config_repo.go)
- `api.AdminServer.ServerConfig` field (server.go)
- `api.AdminServer.Secret` field (server.go)
- `api.AdminServer.PatchAdminConfig` handler (server.go)
- `pb.InvalidateAllAdminSessionsRequest` / `pb.InvalidateAllAdminSessionsResponse` (core_grpc.pb.go — after `make proto`)
- PATCH /api/v1/admin/config route registration (router.go)
- `api_gen.go` regenerated with PatchAdminConfig types (after `make gen-api`)

**Test count:** 13 tests covering AC#1, AC#2, AC#5

### 2. `gateway/internal/api/metrics_handler_test.go` (Go unit tests — metrics handler)

**Dependencies (compilation fails until all exist):**
- `api.MetricsRepository` interface (metrics_repo.go)
- `api.MetricsCounts` struct (metrics_repo.go)
- `api.AdminServer.Metrics` field (server.go)
- `api.AdminServer.GetAdminMetrics` handler implemented (currently stub 501)
- `api_gen.go` regenerated with AdminMetricsResponse replacing EmptyResponse (after `make gen-api`)
- `pb.GetMetricsResponse.ActiveSessions` + `pb.GetMetricsResponse.MsgPerSec` — ALREADY EXIST in core.pb.go

**Test count:** 7 tests covering AC#3, AC#5

### 3. `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_all_admin_sessions_test.exs` (ExUnit)

**Dependencies (compilation fails until all exist):**
- `Core.InvalidateAllAdminSessionsRequest` (core.pb.ex — after `make proto`)
- `Core.InvalidateAllAdminSessionsResponse` (core.pb.ex — after `make proto`)
- `Nebu.EventDispatcher.Server.invalidate_all_admin_sessions/2` handler (server.ex)
- `Nebu.Session.EtsStore.list_user_ids/0` (ets_store.ex)
- `:session_supervisor_module` Application env key wired in handler (server.ex)

**Test count:** 4 tests covering AC#6, AC#7

---

## Mock Architecture

### Go: `mockServerConfigRepository`
- `GetServerConfig(ctx)` → returns preset `*api.ServerConfigData` (no OIDCClientSecret field)
- `UpsertServerConfigKey(ctx, key, value)` → records calls for assertion
- Simulates DB with `oidc_client_secret` stored but not exposed via struct

### Go: `mockRoomDefaultsForConfig`
- `GetRoomDefaults(ctx)` → returns preset max_members + visibility
- Needed because GetAdminConfig also queries room_defaults table
- Note: separate from `mockRoomDefaultsRepository` in room_defaults_handler_test.go to avoid symbol conflicts

### Go: `mockCoreClientForConfig`
- Embeds `pb.CoreServiceClient` to satisfy interface
- `InvalidateAllAdminSessions(ctx, req)` → sets `invalidateAllCalled=true`, returns `ok: true`
- `WriteAuditLog(ctx, req)` → sets `auditCalled=true`

### Go: `mockMetricsRepository`
- `GetMetricsCounts(ctx)` → returns preset `*api.MetricsCounts`

### Go: `mockCoreClientForMetrics`
- Embeds `pb.CoreServiceClient`
- `GetMetrics(ctx, req)` → returns preset `*pb.GetMetricsResponse{ActiveSessions, MsgPerSec}`

### Elixir: `FakeSessionSupervisorForAll`
- Spy module injected via `Application.put_env(:event_dispatcher, :session_supervisor_module, ...)`
- `destroy_session/1` → sends `{:destroy_called, user_id}` to test process

---

## Critical Security Invariants Being Tested

1. **P0 — oidc_client_secret never in GET response**: `TestGetAdminConfig_OIDCClientSecretNeverInResponse` checks the raw response body string for `"oidc_client_secret"`. This catches even accidental serialization.

2. **P0 — oidc_client_secret never in PATCH response**: `TestPatchAdminConfig_Returns200WithFullConfigObject` and `TestPatchAdminConfig_OIDCIssuerChange_InvalidatesAllAdminSessions` both check the PATCH response body for the key.

3. **P0 — OIDC field change triggers session invalidation**: Three separate tests verify that each of the three OIDC-related fields (`oidc_issuer`, `oidc_client_id`, `oidc_client_secret`) independently triggers `InvalidateAllAdminSessions`.

---

## Implementation Order (RED → GREEN)

The dev agent should implement in this order to make tests pass incrementally:

1. **`make proto`** — add `InvalidateAllAdminSessions` RPC to core.proto → regenerate pb stubs
2. **`make gen-api`** — update openapi.yaml with PATCH /admin/config + response schemas → regenerate api_gen.go
3. **`server_config_repo.go`** — define `ServerConfigRepository` interface + `ServerConfigData` struct
4. **`metrics_repo.go`** — define `MetricsRepository` interface + `MetricsCounts` struct
5. **`server.go`** — add `ServerConfig`, `Metrics`, `Secret` fields to `AdminServer`
6. **`server.go`** — implement `GetAdminConfig` handler (replaces stub)
7. **`server.go`** — implement `PatchAdminConfig` handler (new, from make gen-api)
8. **`server.go`** — implement `GetAdminMetrics` handler (replaces stub)
9. **`router.go`** — register PATCH /api/v1/admin/config
10. **`ets_store.ex`** — add `list_user_ids/0` function
11. **`server.ex`** — implement `invalidate_all_admin_sessions/2` gRPC handler
12. **`main.go`** — wire `serverConfigRepo`, `metricsRepo`, `Secret` into `adminSrv`

---

## Execution Commands

```bash
# Go unit tests (run from project root)
make test-unit-go

# Or run specific test files:
cd gateway && go test ./internal/api/ -run TestGetAdminConfig -v
cd gateway && go test ./internal/api/ -run TestPatchAdminConfig -v
cd gateway && go test ./internal/api/ -run TestGetAdminMetrics -v

# Elixir unit tests
make test-unit-elixir

# Or run specific Elixir test:
cd core && mix test apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_all_admin_sessions_test.exs
```

---

## Next Steps for DEV Agent

1. Start with `make proto` to unblock the Go test compilation (adds `InvalidateAllAdminSessions` to `CoreServiceClient`)
2. Run `make test-unit-go` — expect compile errors (RED confirmed)
3. Implement `server_config_repo.go` → `metrics_repo.go` → `server.go` changes
4. Run `make test-unit-go` after each handler implementation to track progress
5. For Elixir: add `list_user_ids/0` to `ets_store.ex` first, then implement the gRPC handler
6. Run `make test-unit-elixir` to verify Elixir tests pass
7. Run `make build-gateway` for final build verification

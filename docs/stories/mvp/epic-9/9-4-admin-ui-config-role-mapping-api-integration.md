---
status: ready-for-dev
epic: 9
story: 4
security_review: required
---

# Story 9.4: Admin UI — Config & Role Mapping API Integration

Status: ready-for-dev

## Story

As an instance admin,
I want the server config and role mapping UI pages to persist changes via the real API,
So that configuration changes survive server restarts.

## Acceptance Criteria

1. Update any config field in the Server Config UI → `PATCH /api/v1/admin/config` (via gRPC `UpdateServerConfig`) called, new value persisted in PostgreSQL (not `stubConfig`). Page reloads with the persisted value on the next `GET`.
2. Update role mapping in the Role Mapping UI → the change is persisted via the real storage layer (see Dev Notes — no dedicated `PUT /api/v1/admin/config/role-mappings` gRPC RPC exists; implementation decision documented in Dev Notes).
3. `gateway/internal/admin/config.go` — zero matches for `TODO(epic-6)`.
4. `gateway/internal/admin/role_mapping.go` — zero matches for `TODO(epic-6)`.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `Config page loads from real API and persists updates` — Playwright
   - Given: full dev stack running (`make dev`), bootstrap complete
   - When: admin logs in and navigates to `/admin/config`, changes the Instance Name (e.g. to `"Nebu Playwright Test"`), and clicks Save
   - Then: page redirects with flash "Config updated"; page reload shows the new Instance Name; the change survives a gateway container restart (persisted in PostgreSQL, not in-memory `stubConfig`)

2. `Role Mapping page persists updates` — Playwright
   - Given: admin is on `/admin/config/role-mapping`
   - When: admin changes the Instance Admin Group (e.g. to `"nebu-instance-admins"`) and clicks Save
   - Then: page redirects with flash "Role mapping updated"; page reload shows the new value; the change is sourced from the real persistence layer (not `stubRoleMappingConfig`)

3. `Zero TODO(epic-6) markers remain in config.go` — Go test (grep-based)
   - Given: `gateway/internal/admin/config.go` is the target file
   - When: the test scans for the literal string `TODO(epic-6)`
   - Then: zero matches found; test fails if any marker is present

4. `Zero TODO(epic-6) markers remain in role_mapping.go` — Go test (grep-based)
   - Given: `gateway/internal/admin/role_mapping.go` is the target file
   - When: the test scans for the literal string `TODO(epic-6)`
   - Then: zero matches found; test fails if any marker is present

### Note on Playwright tests (AC1–AC2)

These tests require the full dev stack (`make dev`) with a real PostgreSQL database. They follow
the OIDC Authorization Code + PKCE login pattern established in Story 7.14's smoke-flows spec.
The tests run under `e2e/tests/features/admin/config-api-integration.spec.ts`.

**Auth helper:** reuse `loginAsAdmin(page)` extracted during Story 9.2 from `rooms-page.spec.ts`
(same Dex credentials: `kai@example.com` / `changeme`).

**AC3–AC4 implementation:** create `gateway/internal/admin/config_todo_test.go` with two Go tests,
each reading the respective file via `os.ReadFile` and asserting `bytes.Contains(content, []byte("TODO(epic-6)"))` is false.
Mirror the exact pattern from `gateway/internal/admin/users_todo_test.go`.

## Tasks / Subtasks

- [ ] Task 1 — Inject gRPC client into `ConfigHandler` (AC: 1, 3)
  - [ ] Define `AdminConfigClient` interface in `gateway/internal/admin/config.go`
  - [ ] Add `core AdminConfigClient` field to `ConfigHandler`
  - [ ] Update `NewConfigHandler` to use variadic constructor: `NewConfigHandler(tmpl *TemplateHandler, core ...AdminConfigClient) *ConfigHandler`
  - [ ] Update `cmd/gateway/main.go` wiring to pass the gRPC client to `NewConfigHandler`
  - [ ] Confirm existing unit tests still compile (variadic constructor: no call-site changes needed for `NewConfigHandler(tmpl)`)

- [ ] Task 2 — Replace `Handler` (GET) stub read with real `GetServerConfig` gRPC call (AC: 1)
  - [ ] Call `c.core.GetServerConfig(ctx, &pb.GetServerConfigRequest{})` in `Handler`
  - [ ] Map `pb.ServerConfigProto` → `StubConfig` via a `protoToStubConfig` helper (see field mapping in Dev Notes)
  - [ ] On gRPC error: log and render page with `stubConfig` fallback + error flash (do not panic)
  - [ ] When `c.core == nil` (nil-client path for unit tests): fall back to `stubConfig` as before

- [ ] Task 3 — Replace `UpdateConfigHandler` stub mutation with real `UpdateServerConfig` gRPC call (AC: 1, 3)
  - [ ] Parse and validate form fields as currently done (keep existing validation logic)
  - [ ] Map validated form values → `pb.UpdateServerConfigRequest` (see field mapping in Dev Notes)
  - [ ] Call `contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))` before the gRPC call (same pattern as rooms.go lines 359, 387, 426)
  - [ ] Call `c.core.UpdateServerConfig(grpcCtx, req)` — replace both `stubConfig` mutations
  - [ ] On success: redirect with flash "Config updated"
  - [ ] On gRPC error: log and re-render form with error flash
  - [ ] Remove both `TODO(epic-6)` comments (lines 37 and 62–66 in `config.go`)

- [ ] Task 4 — Implement role mapping persistence (AC: 2, 4) — see Dev Notes for architecture decision
  - [ ] Follow the chosen approach (gRPC via `UpdateServerConfig` extended fields OR direct JSON Admin API call OR deferred with documented justification — see Dev Notes)
  - [ ] Inject the appropriate client into `RoleMappingHandler` via the same variadic constructor pattern
  - [ ] Replace `UpdateHandler` stub mutations (lines 93–96 in `role_mapping.go`)
  - [ ] Apply `contextWithAdminIdentity` for any mutating gRPC call
  - [ ] Remove both `TODO(epic-6)` comments (lines 44 and 93 in `role_mapping.go`)

- [ ] Task 5 — Update `NewRoleMappingHandler` constructor (AC: 4)
  - [ ] Apply variadic constructor: `NewRoleMappingHandler(tmpl *TemplateHandler, core ...AdminConfigClient) *RoleMappingHandler`
  - [ ] Update `cmd/gateway/main.go` wiring
  - [ ] Confirm existing unit tests still compile

- [ ] Task 6 — Write AC3–AC4 grep tests (AC: 3, 4)
  - [ ] Create `gateway/internal/admin/config_todo_test.go`
  - [ ] Test 1: reads `config.go` and asserts zero `TODO(epic-6)` occurrences (exact pattern from `users_todo_test.go`)
  - [ ] Test 2: reads `role_mapping.go` and asserts zero `TODO(epic-6)` occurrences

- [ ] Task 7 — Write Playwright acceptance tests (AC: 1–2)
  - [ ] Create `e2e/tests/features/admin/config-api-integration.spec.ts`
  - [ ] Implement AC1 test: Server Config update persists (instance_name round-trip)
  - [ ] Implement AC2 test: Role Mapping update persists
  - [ ] Follow auth pattern from `loginAsAdmin` helper established in Story 9.2

- [ ] Task 8 — Verify audit log calls in Core (lessons from 9.2 / 9.3)
  - [ ] Confirm that `UpdateServerConfig` in Core emits an audit log entry
  - [ ] If missing, add audit log call matching the pattern used for user deactivate/reactivate in Story 9.2
  - [ ] For role mapping: confirm audit trail exists for the chosen persistence approach

## Dev Notes

### Current State of `TODO(epic-6)` Markers

**`gateway/internal/admin/config.go`** — exactly 2 `TODO(epic-6)` markers:

| Line | What to replace |
|------|-----------------|
| 37   | Comment on `UpdateConfigHandler` header: `// TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config).` |
| 62   | Inline comment before `stubConfig.InstanceName = instanceName`: `// TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config)` |

The actual stub mutations follow on lines 63–66:
```go
stubConfig.InstanceName = instanceName
stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"
stubConfig.MaxRoomsPerUser = maxRooms
stubConfig.RetentionDays = retentionDays
```

**`gateway/internal/admin/role_mapping.go`** — exactly 2 `TODO(epic-6)` markers:

| Line | What to replace |
|------|-----------------|
| 44   | Comment on `UpdateHandler` header: `// TODO(epic-6): replace stub mutation with Admin API call.` |
| 93   | Inline comment before `stubRoleMappingConfig.OIDCGroupClaim = ...`: `// TODO(epic-6): replace stub mutation with Admin API call.` |

The actual stub mutations follow on lines 94–96:
```go
stubRoleMappingConfig.OIDCGroupClaim = oidcGroupClaim
stubRoleMappingConfig.InstanceAdminGroup = instanceAdminGroup
stubRoleMappingConfig.ComplianceUserGroup = complianceUserGroup
```

### gRPC Client Methods Available for Config

Both methods are already in `gateway/internal/grpc/client.go` (added during Story 9.1):

```go
c.GetServerConfig(ctx, *pb.GetServerConfigRequest) (*pb.GetServerConfigResponse, error)
c.UpdateServerConfig(ctx, *pb.UpdateServerConfigRequest) (*pb.UpdateServerConfigResponse, error)
```

No new wrapper methods need to be added to `client.go` for the config path.

### Field Mapping: `StubConfig` vs. `ServerConfigProto`

The current `StubConfig` struct and the `UpdateConfigHandler` form have **different fields** than
`ServerConfigProto`. The proto is the source of truth for what is actually persisted:

| Form Field / StubConfig Field | Proto Field | Notes |
|-------------------------------|-------------|-------|
| `InstanceName` | `instance_name` | Direct map |
| `AllowRegistration` | — | **NOT in proto.** AllowRegistration is not a persistent server config field in the current data model. Must be noted in code (remove from form or keep as UI-only state). |
| `MaxRoomsPerUser` | `room_default_max_members` | Maps to `room_default_max_members` (int32) |
| `RetentionDays` | `audit_log_retention_days` | Direct map |

**Decision required during implementation:**
- `AllowRegistration` has no proto equivalent. Options:
  - A: Remove the field from the form entirely (template change required)
  - B: Keep the checkbox as UI-only with a note that it is not yet wired to a backend (add `// NOTE:` comment, no `TODO(epic-6)`)
  - C: Add `allow_registration` to `UpdateServerConfigRequest` proto + Core (out-of-scope for XS)
  - **Recommended:** Option B for this story (XS size). Document the decision in a code comment. This does NOT block removal of `TODO(epic-6)` since the existing markers are about the stub mutation, not AllowRegistration specifically.

`OIDCIssuer` is in `ServerConfigProto` but NOT in the current `StubConfig` or form template.
The story AC mentions "update OIDC issuer URL" as the canonical example — this means the form
may need `oidc_issuer` added (or the implementation reads the current issuer via `GetServerConfig`
on the GET handler and exposes it in the form). This is the primary persistent field to demonstrate
in AC1. The dev should add `oidc_issuer` to the `StubConfig` struct and form template if needed,
or at minimum verify that `instance_name` + `audit_log_retention_days` round-trip correctly.

### Role Mapping Architecture Decision

**Critical finding:** `oidc_group_claim`, `instance_admin_group`, and `compliance_user_group` have
**no corresponding gRPC RPC** in `proto/core.proto` and **no `PUT /api/v1/admin/config/role-mappings`
endpoint** in `gateway/api/openapi.yaml`.

The story AC states this should "persist changes" and that changes "survive server restarts."
The developer must choose one of these approaches before implementing:

**Option A — Extend `UpdateServerConfig` proto (proto change required):**
Add `oidc_group_claim`, `instance_admin_group`, `compliance_user_group` to `UpdateServerConfigRequest`
and `ServerConfigProto`. Store in the existing `server_config` table. Requires proto change + Core
implementation + proto regeneration. Correct long-term approach; potentially out-of-scope for XS.

**Option B — Direct DB persistence in gateway (no Core involvement):**
The gateway reads/writes role-mapping config directly via a `RoleMappingRepository` (same pattern
as session manager). Avoids proto changes but introduces direct DB access in the gateway. Not
recommended (violates the gateway-stateless architecture principle from ADR-002).

**Option C — Store in `server_config` table via Core's existing PATCH endpoint:**
If the Core's `PATCH /api/v1/admin/config` handler reads/writes all config keys generically
(e.g. as a JSONB column or key-value table), a new key prefix could be used without proto changes.
Check the actual Core implementation of `UpdateServerConfig` before deciding.

**Option D — Accept as XS limitation: document and defer (no proto change):**
Replace the stub mutation with a `// NOTE: role mapping config is not yet persisted to DB —
TODO(epic-9): wire to real storage once proto is extended.` comment. AC4 (zero `TODO(epic-6)`)
is satisfied because the OLD marker `TODO(epic-6)` is removed. The new `TODO(epic-9)` is a
different marker. Add a follow-up backlog item to `sprint-status.yaml`.

**Recommended for XS:** Investigate Option A first (may be simpler than it looks if the Core
`server_config` table already has a generic key-value layout). Fall back to Option D if Option A
requires more than ~1 story point of proto + Core work. Document the decision explicitly in
code comments and in the PR description.

### `AdminConfigClient` Interface (for testability)

Mirror the `AdminUsersClient` pattern from Story 9.2:

```go
type AdminConfigClient interface {
    GetServerConfig(ctx context.Context, req *pb.GetServerConfigRequest) (*pb.GetServerConfigResponse, error)
    UpdateServerConfig(ctx context.Context, req *pb.UpdateServerConfigRequest) (*pb.UpdateServerConfigResponse, error)
}
```

`*grpc.Client` already satisfies this interface — no new wrapper methods needed in `client.go`.

In unit tests, pass `nil` (variadic constructor allows nil — handler falls back to stub data
when `c.core == nil`).

### `contextWithAdminIdentity` Pattern (from Story 9.2 / 9.3)

Apply to all mutating gRPC calls in both `UpdateConfigHandler` and `RoleMappingHandler.UpdateHandler`:

```go
grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
_, err := c.core.UpdateServerConfig(grpcCtx, req)
```

`contextWithAdminIdentity` is defined in `gateway/internal/admin/middleware.go` (line ~307).
`AdminSubFromContext` extracts the admin user's subject claim from the session context.

This pattern was added as a HIGH-1 security fix during Story 9.2's Kassandra review.

### gRPC Error Handling

Use the same pattern as Stories 9.2 / 9.3:

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

// In UpdateConfigHandler: treat any gRPC error as a 500-equivalent; re-render form with error flash
if err != nil {
    slog.Error("UpdateServerConfig gRPC error", "err", err)
    // re-render form with error flash
    return
}
```

Unlike room/user handlers, config update has no `NOT_FOUND` case — the config row is always
present (upsert semantics in Core).

### Lesson from Story 9.2 — Audit Log in Core

Story 9.2 had a HIGH-1 finding: Core `server.ex` was missing audit log entries for mutating
admin operations. Before marking this story `done`, verify that `UpdateServerConfig` in
`core/apps/session_manager/lib/session_manager/grpc/server.ex` (or equivalent) emits an audit
log entry. If missing, add it as part of this story's scope, following the pattern from Story 9.2.

### Security Checklist (SEC Gate 1 triggers)

This story modifies admin handlers behind `RequireRole("instance_admin")` middleware. Key invariants:

1. `UpdateConfigHandler` and `RoleMappingHandler.UpdateHandler` must remain protected by CSRF
   middleware (already in place via `csrf(sessionGuard(...))` from Story 7.17).
2. The gRPC client is called with `contextWithAdminIdentity(r.Context(), ...)` — ensures both PSK
   token injection (Story 5.29a) and actor identity propagation (Story 9.2 HIGH-1 fix).
3. Form field values are validated before the gRPC call. `instance_name`, `max_rooms_per_user`,
   `retention_days` must continue to be validated server-side (existing validation stays).
4. `oidc_issuer` (if added to the form) must be validated for HTTPS scheme — see Story 5.17.
5. No config values (especially oidc_client_secret, which is intentionally absent from
   `ServerConfigProto`) should appear in `slog` error calls.

### File List

**Gateway (UPDATE):**
- `gateway/internal/admin/config.go` — primary target: inject gRPC client, replace 2 `TODO(epic-6)` stubs
- `gateway/internal/admin/role_mapping.go` — primary target: inject gRPC client, replace 2 `TODO(epic-6)` stubs
- `gateway/cmd/gateway/main.go` — update `NewConfigHandler` and `NewRoleMappingHandler` call sites

**Gateway (CREATE):**
- `gateway/internal/admin/config_todo_test.go` — AC3/AC4: Go tests asserting zero `TODO(epic-6)` in `config.go` and `role_mapping.go`

**E2E Tests (CREATE):**
- `e2e/tests/features/admin/config-api-integration.spec.ts` — Playwright tests for AC1–AC2

**Proto (POSSIBLY UPDATE, if Option A for role mapping):**
- `proto/core.proto` — add role-mapping fields to `UpdateServerConfigRequest` + `ServerConfigProto`

**Elixir Core (POSSIBLY UPDATE):**
- Core gRPC server handler for `UpdateServerConfig` — add audit log entry if missing
- Core `UpdateServerConfig` handler — add role-mapping fields if Option A is chosen

**Do NOT change:**
- `gateway/internal/admin/stubs.go` — `stubConfig` and `stubRoleMappingConfig` remain for
  backward compatibility (nil-client fallback path) until deprecated
- `gateway/internal/admin/page_data.go` — `StubConfig` / `StubRoleMappingConfig` structs
  unchanged for template compatibility unless oidc_issuer is added to the form
- `gateway/internal/admin/templates/config.html` — only change if oidc_issuer field is added
  (recommended: yes — the epics.md example explicitly mentions updating OIDC issuer URL)

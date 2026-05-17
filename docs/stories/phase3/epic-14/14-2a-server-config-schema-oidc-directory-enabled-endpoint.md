---
status: ready-for-dev
epic: 14
story: 2a
security_review: not-needed
matrix: false
ui: true
---

# Story 14.2a: Server Config Schema — oidc_directory_enabled + oidc_directory_endpoint

Status: ready-for-dev

## Story

As an instance admin,
I want `oidc_directory_enabled` and `oidc_directory_endpoint` fields in the server config,
So that the OIDC directory integration can be toggled and configured without code changes.

**Size:** S
**security_review:** not-needed

---

## Acceptance Criteria

**AC1 — Migration adds oidc_directory fields:**
Given a new database migration (000048),
When it runs,
Then `oidc_directory_enabled` (default `"false"`) and `oidc_directory_endpoint` (default `""`) rows are seeded into the `server_config` key-value table, and the RLS mutable-key policy is updated to allow updating these keys

**AC2 — GET /api/v1/admin/config returns new fields:**
Given `GET /api/v1/admin/config`,
When the endpoint is called,
Then `oidc_directory_enabled` (boolean) and `oidc_directory_endpoint` (string) are included in the JSON response

**AC3 — PATCH /api/v1/admin/config persists new fields:**
Given `PATCH /api/v1/admin/config` with both `oidc_directory_enabled: true` and `oidc_directory_endpoint: "https://idp.example.com/users"`,
When the request is processed,
Then the values are persisted and returned correctly in a subsequent GET

**AC4 — Admin UI Config page shows new controls:**
Given the Admin UI Config page (`GET /admin/config`),
When it loads,
Then a toggle for `oidc_directory_enabled` and a text field for `oidc_directory_endpoint` are displayed; the endpoint text field is visible only when the toggle is enabled (conditional visibility via Alpine.js or DaisyUI attribute)

**AC5 — Godog round-trip scenario passes:**
Given a Godog scenario in `gateway/features/oidc_directory_config.feature`,
When `make test-integration` runs,
Then the scenario "set oidc_directory_enabled + endpoint, read back" passes

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Go unit test: `TestGetAdminConfig_IncludesOidcDirectoryFields` — httptest**
Location: `gateway/internal/api/config_handler_test.go`
- Given: `mockServerConfigRepository` returns `OidcDirectoryEnabled: false, OidcDirectoryEndpoint: ""`
- When: `GET /api/v1/admin/config` is called
- Then: response JSON contains `"oidc_directory_enabled": false` and `"oidc_directory_endpoint": ""`

**2. Go unit test: `TestPatchAdminConfig_OidcDirectory_PersistsAndReturns` — httptest**
Location: `gateway/internal/api/config_handler_test.go`
- Given: `mockServerConfigRepository` with standard defaults
- When: `PATCH /api/v1/admin/config` with `{"oidc_directory_enabled": true, "oidc_directory_endpoint": "https://idp.example.com/users"}`
- Then: response status is 200
- And: `UpsertServerConfigKey` was called with `("oidc_directory_enabled", "true")` and `("oidc_directory_endpoint", "https://idp.example.com/users")`
- And: response JSON contains `"oidc_directory_enabled": true` and `"oidc_directory_endpoint": "https://idp.example.com/users"`

**3. Go unit test: `TestAdminConfigHandler_OidcDirectoryToggleVisible` — httptest**
Location: `gateway/internal/admin/config_handler_test.go` (new file for admin UI handler tests)
- Given: `ConfigHandler` served with gRPC returning `oidc_directory_enabled: false`
- When: `GET /admin/config` is called
- Then: response body contains the OIDC directory toggle element (e.g., `id="oidc_directory_enabled"`)
- And: response body contains `oidc_directory_endpoint` text field (or the conditional container)

**4. Godog integration test: `oidc_directory_config.feature`**
Location: `gateway/features/oidc_directory_config.feature`
- Scenario: "set oidc_directory_enabled + endpoint, read back"
  - Given: bootstrap is complete and server_config is seeded
  - And: the instance_admin kai is authenticated for admin API
  - When: the admin PATCHes `/api/v1/admin/config` with `{"oidc_directory_enabled": true, "oidc_directory_endpoint": "https://idp.example.com/users"}`
  - Then: response status is 200
  - And: the admin GETs `/api/v1/admin/config`
  - Then: response status is 200
  - And: response body contains `"oidc_directory_enabled":true`
  - And: response body contains `"oidc_directory_endpoint":"https://idp.example.com/users"`

---

## Tasks / Subtasks

- [ ] Task 1: Create migration 000048 (AC1)
  - [ ] `gateway/migrations/000048_oidc_directory_config.up.sql`:
    - Insert default rows: `oidc_directory_enabled = 'false'`, `oidc_directory_endpoint = ''`
    - Drop and recreate `config_update_mutable` RLS policy adding the two new keys to the allowed mutable key list
  - [ ] `gateway/migrations/000048_oidc_directory_config.down.sql`:
    - Delete the two rows (if not customized); revert the RLS policy to exclude the new keys
  - [ ] **CRITICAL**: After adding migration, run `make test-unit-go` to ensure `migrations_test.go` passes

- [ ] Task 2: Extend OpenAPI spec and regenerate (AC2, AC3)
  - [ ] In `gateway/api/openapi.yaml`, add to `AdminConfigResponse`:
    ```yaml
    oidc_directory_enabled:
      type: boolean
    oidc_directory_endpoint:
      type: string
    ```
  - [ ] Add to `PatchAdminConfigRequest`:
    ```yaml
    oidc_directory_enabled:
      type: boolean
    oidc_directory_endpoint:
      type: string
    ```
  - [ ] Run `make gen-api` to regenerate `gateway/internal/api/api_gen.go`
  - [ ] Verify `AdminConfigResponse` now has `OidcDirectoryEnabled *bool` and `OidcDirectoryEndpoint *string`
  - [ ] Verify `PatchAdminConfigRequest` now has matching fields

- [ ] Task 3: Extend `ServerConfigData` and `GetServerConfig` DB query (AC2)
  - [ ] In `gateway/internal/api/server_config_repo.go`:
    - Add `OidcDirectoryEnabled bool` and `OidcDirectoryEndpoint string` to `ServerConfigData`
    - Extend the `WHERE key IN (...)` query to include `'oidc_directory_enabled'` and `'oidc_directory_endpoint'`
    - Parse: `OidcDirectoryEnabled = vals["oidc_directory_enabled"] == "true"` (default: `false`)
    - Parse: `OidcDirectoryEndpoint = vals["oidc_directory_endpoint"]` (default: `""`)

- [ ] Task 4: Extend `GetAdminConfig` and `PatchAdminConfig` handlers (AC2, AC3)
  - [ ] In `gateway/internal/api/server.go`:
    - `GetAdminConfig`: pass `oidcDirectoryEnabled` and `oidcDirectoryEndpoint` to `getAdminConfigOKResponse`
    - `getAdminConfigOKResponse`: add new fields
    - `adminConfigResponseBody`: add `OidcDirectoryEnabled bool` and `OidcDirectoryEndpoint string` fields
    - `VisitGetAdminConfigResponse`: include new fields in JSON body
    - `PatchAdminConfig`: handle `body.OidcDirectoryEnabled != nil` → upsert `"oidc_directory_enabled"` with `"true"`/`"false"`; handle `body.OidcDirectoryEndpoint != nil` → upsert `"oidc_directory_endpoint"`
  - [ ] Write unit tests AT#1 and AT#2

- [ ] Task 5: Extend Admin UI Config page (AC4)
  - [ ] Extend `ConfigPageData` or add new fields to `StubConfig` in `page_data.go`:
    - Add `OidcDirectoryEnabled bool` and `OidcDirectoryEndpoint string` to `StubConfig` struct
  - [ ] Extend `protoToStubConfig` (or `GetServerConfig` gRPC response path) to populate the new fields
  - [ ] Update `config.html` template:
    - Add toggle: `<input type="checkbox" id="oidc_directory_enabled" name="oidc_directory_enabled" class="toggle" {{ if .Config.OidcDirectoryEnabled }}checked{{ end }}>`
    - Add text field for endpoint — conditionally visible when toggle is on (use `x-show` Alpine.js or a `data-` attribute)
    - Follow DaisyUI patterns already used in the template (see `form-control`, `input`, `label` patterns)
  - [ ] Extend `UpdateConfigHandler` to parse `oidc_directory_enabled` (checkbox) and `oidc_directory_endpoint` (text) from form POST
  - [ ] If `h.core != nil`, call `UpdateServerConfig` gRPC with the new proto fields (see proto section below)
  - [ ] Write unit test AT#3

- [ ] Task 6: gRPC proto and generated pb (AC4)
  - [ ] Check if `UpdateServerConfigRequest` and `ServerConfigProto` already have fields for `oidc_directory_enabled` and `oidc_directory_endpoint`
  - [ ] If missing: add fields to `proto/core.proto`; run `make proto` to regenerate pb files
  - [ ] If already present: no proto change needed — just wire the fields

- [ ] Task 7: Godog integration test (AC5)
  - [ ] Create `gateway/features/oidc_directory_config.feature` with the round-trip scenario
  - [ ] Create `gateway/test/integration/oidc_directory_config_steps_test.go` with step definitions
  - [ ] Register `initializeOidcDirectoryConfigSteps` in `gateway/test/integration/steps_test.go`
  - [ ] Reuse: `adminAPIDoRequest`, `adminAPIAdminToken`, `lastStatusCode`, `lastBody` from `admin_api_steps_test.go`

- [ ] Task 8: Run all test suites
  - [ ] `make test-unit-go` — all unit tests pass
  - [ ] `make test-integration` — `oidc_directory_config.feature` scenario passes

---

## Dev Notes

### CRITICAL: server_config is a key-value table, NOT a column-based table

The `server_config` table has three columns: `key TEXT PRIMARY KEY`, `value TEXT NOT NULL`, `set_at BIGINT NOT NULL`. There are NO new physical columns to add. New configuration values are stored as new **rows** with string keys. The AC wording "columns are added" is shorthand for "key-value rows are seeded".

### Migration 000048 — pattern and RLS policy

The new migration must:
1. Seed default rows:
```sql
INSERT INTO server_config (key, value, set_at) VALUES
  ('oidc_directory_enabled',  'false', (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
  ('oidc_directory_endpoint', '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint)
ON CONFLICT (key) DO NOTHING;
```
2. Update the RLS mutable-key policy (migration 000046 created `config_update_mutable`) to add the new keys:
```sql
DROP POLICY IF EXISTS config_update_mutable ON server_config;
CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret',
        'oidc_directory_enabled',
        'oidc_directory_endpoint'
    ))
    WITH CHECK (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret',
        'oidc_directory_enabled',
        'oidc_directory_endpoint'
    ));
```

Down migration: delete the two rows and revert the RLS policy (dropping the new keys from the list).

### OpenAPI schema changes

Add to `AdminConfigResponse` in `gateway/api/openapi.yaml`:
```yaml
oidc_directory_enabled:
  type: boolean
  description: "Whether the OIDC user directory integration is enabled (Protocol A, ADR-015)"
oidc_directory_endpoint:
  type: string
  description: "HTTPS URL of the OIDC user directory endpoint (required when oidc_directory_enabled is true)"
```

Add to `PatchAdminConfigRequest`:
```yaml
oidc_directory_enabled:
  type: boolean
oidc_directory_endpoint:
  type: string
```

After changing `openapi.yaml`, run:
```bash
make gen-api
```

This regenerates `gateway/internal/api/api_gen.go`. Verify that `AdminConfigResponse` gets `OidcDirectoryEnabled *bool` and `OidcDirectoryEndpoint *string`, and that `PatchAdminConfigRequest` gets matching fields.

### ServerConfigData extension

In `gateway/internal/api/server_config_repo.go`:
```go
type ServerConfigData struct {
    InstanceName          string
    OIDCIssuer            string
    OIDCClientID          string
    AuditLogRetentionDays int
    OidcDirectoryEnabled  bool    // NEW: default false
    OidcDirectoryEndpoint string  // NEW: default ""
}
```

Extend the SQL query:
```go
`SELECT key, value FROM server_config
 WHERE key IN ('instance_name', 'oidc_issuer', 'oidc_client_id', 'audit_log_retention_days',
               'oidc_directory_enabled', 'oidc_directory_endpoint')`
```

Parse new values:
```go
data.OidcDirectoryEnabled = vals["oidc_directory_enabled"] == "true"
data.OidcDirectoryEndpoint = vals["oidc_directory_endpoint"]
```

### getAdminConfigOKResponse and adminConfigResponseBody extension

```go
type adminConfigResponseBody struct {
    InstanceName          string `json:"instance_name"`
    OIDCIssuer            string `json:"oidc_issuer"`
    OIDCClientID          string `json:"oidc_client_id"`
    RoomDefaultMaxMembers int    `json:"room_default_max_members"`
    RoomDefaultVisibility string `json:"room_default_visibility"`
    AuditLogRetentionDays int    `json:"audit_log_retention_days"`
    OidcDirectoryEnabled  bool   `json:"oidc_directory_enabled"`  // NEW
    OidcDirectoryEndpoint string `json:"oidc_directory_endpoint"` // NEW
}
```

NOTE: `omitempty` is intentionally NOT used (existing pattern — all keys always present, tests verify all keys exist).

The `getAdminConfigOKResponse` struct gains two new fields:
```go
type getAdminConfigOKResponse struct {
    // ... existing ...
    oidcDirectoryEnabled  bool
    oidcDirectoryEndpoint string
}
```

And `VisitGetAdminConfigResponse` populates them:
```go
body := adminConfigResponseBody{
    // ... existing ...
    OidcDirectoryEnabled:  r.oidcDirectoryEnabled,
    OidcDirectoryEndpoint: r.oidcDirectoryEndpoint,
}
```

`GetAdminConfig` populates from `cfgData`:
```go
return &getAdminConfigOKResponse{
    // ... existing ...
    oidcDirectoryEnabled:  cfgData.OidcDirectoryEnabled,
    oidcDirectoryEndpoint: cfgData.OidcDirectoryEndpoint,
}, nil
```

### PatchAdminConfig — new field handling

Add after the `AuditLogRetentionDays` block:
```go
if body.OidcDirectoryEnabled != nil {
    val := "false"
    if *body.OidcDirectoryEnabled {
        val = "true"
    }
    if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_directory_enabled", val); err != nil {
        return nil, err
    }
    changedKeys = append(changedKeys, "oidc_directory_enabled")
}

if body.OidcDirectoryEndpoint != nil {
    if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_directory_endpoint", *body.OidcDirectoryEndpoint); err != nil {
        return nil, err
    }
    changedKeys = append(changedKeys, "oidc_directory_endpoint")
}
```

No session invalidation is needed for OIDC directory settings (only oidc_issuer/client_id/secret trigger that).

### mockServerConfigRepository extension for unit tests

In `config_handler_test.go`, the existing `mockServerConfigRepository` has:
```go
type mockServerConfigRepository struct {
    configData *api.ServerConfigData
    // UpsertServerConfigKey tracking
    upsertCalls []struct{ key, value string }
}
```

The mock needs `OidcDirectoryEnabled bool` and `OidcDirectoryEndpoint string` on the returned `ServerConfigData`. The mock's `GetServerConfig` returns `m.configData`, so just populate the new fields when constructing test instances:
```go
configData: &api.ServerConfigData{
    // ... existing ...
    OidcDirectoryEnabled:  false,
    OidcDirectoryEndpoint: "",
},
```

### Admin UI template — config.html

The current `config.html` has a simple form with `instance_name`, `allow_registration`, `max_rooms_per_user`, `retention_days`. This story adds two new fields at the bottom of the form (before the Save button):

**Toggle (DaisyUI):**
```html
<div class="form-control" x-data="{ oidcDirEnabled: {{ if .Config.OidcDirectoryEnabled }}true{{ else }}false{{ end }} }">
  <label class="label cursor-pointer justify-start gap-4">
    <input type="checkbox" id="oidc_directory_enabled" name="oidc_directory_enabled"
           class="toggle toggle-primary"
           x-model="oidcDirEnabled"
           {{ if .Config.OidcDirectoryEnabled }}checked{{ end }}>
    <span class="label-text">Enable OIDC User Directory (Protocol A — Dex/Keycloak)</span>
  </label>

  <div x-show="oidcDirEnabled" class="mt-3">
    <label class="label" for="oidc_directory_endpoint">
      <span class="label-text">OIDC Directory Endpoint URL</span>
    </label>
    <input type="url" id="oidc_directory_endpoint" name="oidc_directory_endpoint"
           value="{{ .Config.OidcDirectoryEndpoint }}"
           class="input input-bordered w-full"
           placeholder="https://idp.example.com/admin/users"
           x-bind:required="oidcDirEnabled">
    <p class="text-xs text-base-content/60 mt-1">
      HTTPS URL of the OIDC user directory endpoint. Required when the toggle is enabled.
    </p>
  </div>
</div>
```

**If Alpine.js is NOT available in the Admin UI**, use a simpler approach: always show both fields, but add a note that the endpoint is only used when enabled.

Check if Alpine.js is already loaded in `base.html`:
```bash
grep -n "alpine\|alpinejs" gateway/internal/admin/templates/base.html
```
If it is: use `x-data` / `x-show` / `x-model` as shown above.
If it is not: use a simpler conditional-less layout (both fields always visible).

### StubConfig extension (for unit tests when core is nil)

In `gateway/internal/admin/stubs.go`:
```go
type StubConfig struct {
    InstanceName           string
    AllowRegistration      bool
    MaxRoomsPerUser        int
    RetentionDays          int
    OidcDirectoryEnabled   bool   // NEW
    OidcDirectoryEndpoint  string // NEW
}
```

Default stub value: `OidcDirectoryEnabled: false, OidcDirectoryEndpoint: ""`

### `protoToStubConfig` extension

The `ConfigHandler.Handler` uses `GetServerConfig` gRPC response. Check if `ServerConfigProto` has `oidc_directory_enabled` and `oidc_directory_endpoint` fields:

```bash
grep -n "oidc_directory\|OidcDirectory" gateway/internal/grpc/pb/core.pb.go
```

If the proto already has these fields (from a previous story or pre-planning):
```go
func protoToStubConfig(p *pb.ServerConfigProto) StubConfig {
    return StubConfig{
        InstanceName:          p.GetInstanceName(),
        MaxRoomsPerUser:       int(p.GetRoomDefaultMaxMembers()),
        RetentionDays:         int(p.GetAuditLogRetentionDays()),
        AllowRegistration:     stubConfig.AllowRegistration,
        OidcDirectoryEnabled:  p.GetOidcDirectoryEnabled(),
        OidcDirectoryEndpoint: p.GetOidcDirectoryEndpoint(),
    }
}
```

If the proto does NOT have these fields yet (most likely), add them to `proto/core.proto` and run `make proto`. The new proto fields should be added to `ServerConfigProto` and `UpdateServerConfigRequest`:
```protobuf
// In ServerConfigProto (response fields):
bool oidc_directory_enabled = N;
string oidc_directory_endpoint = N+1;

// In UpdateServerConfigRequest (request fields):
bool oidc_directory_enabled = N;
string oidc_directory_endpoint = N+1;
```

**IMPORTANT**: Check the current highest field number in the proto file before adding new ones.

### Godog integration test pattern

**New file:** `gateway/features/oidc_directory_config.feature`
```gherkin
Feature: OIDC Directory Config — oidc_directory_enabled + oidc_directory_endpoint round-trip
  As an instance admin
  I want to enable and configure the OIDC user directory integration
  So that the Admin UI user search can include OIDC users

  Background:
    Given the server is running with a clean test database

  Scenario: set oidc_directory_enabled + endpoint, read back
    Given bootstrap is complete and server_config is seeded
    And the instance_admin kai is authenticated for admin API
    When the admin PATCHes /api/v1/admin/config with oidc_directory_enabled true and endpoint "https://idp.example.com/users"
    Then the response status is 200
    When the admin GETs /api/v1/admin/config
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
    And the response body contains "https://idp.example.com/users"
```

**New step file:** `gateway/test/integration/oidc_directory_config_steps_test.go`
- Follows pattern of `claim_lock_steps_test.go`
- Reuses `adminAPIDoRequest`, `adminAPIAdminToken`, `lastStatusCode`, `lastBody`
- New step: `"the admin PATCHes /api/v1/admin/config with oidc_directory_enabled true and endpoint {string}"`
- New step: `"the admin GETs /api/v1/admin/config"` (or check if this step exists already in `admin_api_steps_test.go`)

**Registration:** Add `initializeOidcDirectoryConfigSteps(sc)` in `steps_test.go` (same pattern as `initializeClaimLockSteps`).

### File structure: what to touch

| File | Action | Notes |
|------|--------|-------|
| `gateway/migrations/000048_oidc_directory_config.up.sql` | NEW | Insert default rows + update RLS policy |
| `gateway/migrations/000048_oidc_directory_config.down.sql` | NEW | Remove rows + revert RLS policy |
| `gateway/api/openapi.yaml` | UPDATE | Add oidc_directory_enabled + oidc_directory_endpoint to both schemas |
| `gateway/internal/api/api_gen.go` | REGENERATED | Via `make gen-api` |
| `gateway/internal/api/server_config_repo.go` | UPDATE | Add fields to ServerConfigData + extend DB query |
| `gateway/internal/api/server.go` | UPDATE | GetAdminConfig + PatchAdminConfig + response structs |
| `gateway/internal/api/config_handler_test.go` | UPDATE | Add AT#1 + AT#2 unit tests |
| `gateway/internal/admin/stubs.go` | UPDATE | Add OidcDirectoryEnabled + OidcDirectoryEndpoint to StubConfig |
| `gateway/internal/admin/page_data.go` | NONE or UPDATE | ConfigPageData embeds StubConfig — no change needed if StubConfig is extended |
| `gateway/internal/admin/config.go` | UPDATE | protoToStubConfig + UpdateConfigHandler to handle new fields |
| `gateway/internal/admin/templates/config.html` | UPDATE | Add toggle + conditional endpoint field |
| `gateway/internal/admin/config_test.go` | UPDATE | Add AT#3 unit test |
| `proto/core.proto` | UPDATE | Add oidc_directory fields to ServerConfigProto + UpdateServerConfigRequest (if missing) |
| `gateway/internal/grpc/pb/core.pb.go` | REGENERATED | Via `make proto` |
| `gateway/features/oidc_directory_config.feature` | NEW | Godog round-trip scenario |
| `gateway/test/integration/oidc_directory_config_steps_test.go` | NEW | Step definitions |
| `gateway/test/integration/steps_test.go` | UPDATE | Register initializeOidcDirectoryConfigSteps |

**Do NOT create new Go handler files** — all changes are additions to existing files.

### Preserve existing behavior

- All existing `AdminConfigResponse` fields (instance_name, oidc_issuer, oidc_client_id, room_default_max_members, room_default_visibility, audit_log_retention_days) MUST remain in the response.
- All existing `PatchAdminConfig` fields (instance_name, oidc_issuer, oidc_client_id, oidc_client_secret, audit_log_retention_days, matrix_user_id_claim) MUST continue to work.
- The RLS down migration must re-create the policy from migration 000046 (7 mutable keys) — NOT from migration 000045 (blanket update). The policy must be exactly the 000046 set minus the new keys.
- OIDC session invalidation logic in PatchAdminConfig is NOT triggered by oidc_directory_* changes (only OIDC auth fields trigger it).
- Existing unit tests in `config_handler_test.go` must continue to pass — they use `mockServerConfigRepository` which can be given zero-valued new fields without breaking.

### Sprint Status Key

The sprint-status.yaml key for this story is `14-2a-server-config-schema-oidc-directory-enabled-endpoint`.

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

None.

### Completion Notes List

(to be filled by dev agent)

### File List

(to be filled by dev agent)

### Review Findings

- [ ] [Review][Patch] Admin UI gRPC path silently discards oidcDirectoryEnabled — form submit does not persist toggle state to DB [gateway/internal/admin/config.go:145]
- [x] [Review][Defer] Pre-existing gatewayURL bug in admin_api_steps_test.go — admin API calls on port 8080 instead of 8008 [gateway/test/integration/admin_api_steps_test.go:64,100,112,123,236] — deferred, pre-existing

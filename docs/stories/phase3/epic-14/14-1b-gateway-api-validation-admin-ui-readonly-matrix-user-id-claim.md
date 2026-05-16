---
status: done
epic: 14
story: 1b
security_review: required
matrix: false
ui: true
---

# Story 14.1b: Gateway API Validation + Admin UI Read-Only Display for matrix_user_id_claim

Status: done

## Story

As an instance admin,
I want the Gateway API to return a clear 400 error when `matrix_user_id_claim` is changed post-bootstrap, and the Admin UI to display the field as read-only,
So that admins receive immediate, actionable feedback without needing to understand the underlying gRPC error.

**Size:** S
**security_review:** required

---

## Acceptance Criteria

**AC1 — API 400 on post-bootstrap claim change:**
Given bootstrap has been completed (`bootstrap_completed` key IS NOT NULL in `server_config`),
When `PATCH /api/v1/admin/config` is called with a `matrix_user_id_claim` value,
Then the server returns `400 M_FORBIDDEN` with `error: "matrix_user_id_claim cannot be changed after bootstrap"` (mapped from Core's `FAILED_PRECONDITION` gRPC status code 9)

**AC2 — Admin UI read-only display after bootstrap:**
Given an admin navigates to the Claim Mapping settings page (`GET /admin/config/claim-mapping`) after bootstrap,
When the page loads,
Then `matrix_user_id_claim` (`oidc_user_id_claim` field) is displayed as read-only text (not an editable `<input>`) and an info banner reads: "This claim cannot be changed after bootstrap."

**AC3 — Bootstrap Wizard info text (Step 3):**
Given the Bootstrap Wizard Step 3 (Claim Mapping), rendered in `bootstrap.html`,
When it renders (always, regardless of bootstrap status — it renders pre-bootstrap),
Then an info text is shown: "The Matrix User ID claim cannot be changed after completing setup."

**AC4 — Godog integration test:**
Given a Godog scenario in `gateway/features/claim_lock.feature`,
When `make test-integration` runs,
Then:
- A POST-bootstrap PATCH attempt with `matrix_user_id_claim` returns 400 with `M_FORBIDDEN` and the message `"matrix_user_id_claim cannot be changed after bootstrap"`
- A PRE-bootstrap PATCH attempt with `matrix_user_id_claim` succeeds (200)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Go unit test: `TestPatchAdminConfig_MatrixUserIDClaim_PostBootstrap_Returns400` — ExUnit**
Location: `gateway/internal/api/config_handler_test.go`
- Given: `mockServerConfigRepository` that reports `bootstrap_completed = "true"` via `GetBootstrapCompleted` (or the repository is extended to return this flag)
- When: `PATCH /api/v1/admin/config` with body `{"matrix_user_id_claim": "email"}`
- Then: response status is 400
- And: response body JSON contains `"errcode": "M_FORBIDDEN"` and `"error"` field containing `"matrix_user_id_claim cannot be changed after bootstrap"`
- And: `UpsertServerConfigKey` was NOT called

**2. Go unit test: `TestPatchAdminConfig_MatrixUserIDClaim_PreBootstrap_Returns200` — ExUnit**
Location: `gateway/internal/api/config_handler_test.go`
- Given: `mockServerConfigRepository` that reports no `bootstrap_completed` (pre-bootstrap)
- When: `PATCH /api/v1/admin/config` with body `{"matrix_user_id_claim": "preferred_username"}`
- Then: response status is 200
- And: gRPC `UpdateServerConfig` was called with `MatrixUserIdClaim: "preferred_username"`

**3. Go unit test: `TestClaimMappingHandler_GetPostBootstrap_ShowsReadOnly` — httptest**
Location: `gateway/internal/admin/claim_mapping_handler_test.go`
- Given: `recordingServerConfigReader.LoadServerConfigKey("bootstrap_completed")` returns `"true"`
- When: `GET /admin/config/claim-mapping` is called
- Then: response is 200
- And: response body does NOT contain `name="oidc_user_id_claim"` (no editable input)
- And: response body contains `"This claim cannot be changed after bootstrap"` (info banner)

**4. Go unit test: `TestClaimMappingHandler_GetPreBootstrap_ShowsEditableInput` — httptest**
Location: `gateway/internal/admin/claim_mapping_handler_test.go`
- Given: `LoadServerConfigKey("bootstrap_completed")` returns `"", sql.ErrNoRows` (pre-bootstrap)
- When: `GET /admin/config/claim-mapping` is called
- Then: response body contains `name="oidc_user_id_claim"` (editable input present)

**5. Go unit test: `TestBootstrapHandler_Step3_ShowsInfoText` — httptest**
Location: `gateway/internal/admin/bootstrap_wizard_test.go` (new file or existing)
- Given: Bootstrap Wizard Step 3 is rendered
- When: handler renders the template (any state of bootstrap — step 3 is always pre-bootstrap)
- Then: response body contains `"The Matrix User ID claim cannot be changed after completing setup"`

**6. Godog integration test: `claim_lock.feature` (2 scenarios)**
Location: `gateway/features/claim_lock.feature`
- Scenario 1 (POST-bootstrap block): PATCH with `matrix_user_id_claim` → 400 M_FORBIDDEN
- Scenario 2 (PRE-bootstrap allow): PATCH with `matrix_user_id_claim` → 200

---

## Tasks / Subtasks

- [ ] Task 1: Extend `PatchAdminConfig` to detect `matrix_user_id_claim` field and call gRPC `UpdateServerConfig` with `MatrixUserIdClaim` (AC1)
  - [ ] Add `MatrixUserIdClaim *string` field to the OpenAPI `PatchAdminConfigBody` schema (or use the existing `PatchAdminConfigBody` struct)
  - [ ] Run `make gen-api` to regenerate `api_gen.go`
  - [ ] Add handling in `PatchAdminConfig` in `gateway/internal/api/server.go`:
    - If `body.MatrixUserIdClaim != nil`, call `s.CoreClient.UpdateServerConfig(ctx, &pb.UpdateServerConfigRequest{MatrixUserIdClaim: *body.MatrixUserIdClaim})`
    - If gRPC returns `codes.FailedPrecondition`, map to `400` with `M_FORBIDDEN` error body
  - [ ] Add `patchAdminConfig400ForbiddenResp` response type implementing `PatchAdminConfigResponseObject`
  - [ ] Write unit tests (AT#1 and AT#2)
- [ ] Task 2: Extend `ClaimMappingHandler` to detect bootstrap state and show read-only UI (AC2)
  - [ ] `ServerConfigReader.LoadServerConfigKey` already exists — no interface change needed
  - [ ] In `ClaimMappingHandler.Handler` (GET), call `h.configReader.LoadServerConfigKey(ctx, "bootstrap_completed")`; if non-empty, set `BootstrapCompleted: true` on `ClaimMappingPageData`
  - [ ] Add `BootstrapCompleted bool` field to `ClaimMappingPageData` in `page_data.go`
  - [ ] Update `claim-mapping.html` template: conditionally render read-only text (not input) for `oidc_user_id_claim` when `BootstrapCompleted` is true, with info banner
  - [ ] Keep editable inputs for `oidc_displayname_claim` and `oidc_email_claim` (only the user-ID claim is locked)
  - [ ] Block the POST handler: if post-bootstrap and `oidc_user_id_claim` field changed, forward to gRPC call which will return FAILED_PRECONDITION — the UI should not even show the form, but the handler must be robust
  - [ ] Write unit tests (AT#3 and AT#4)
- [ ] Task 3: Add info text to Bootstrap Wizard Step 3 template (AC3)
  - [ ] Update `gateway/internal/admin/templates/bootstrap.html` Step 3 section
  - [ ] Add an `alert alert-info` div before the `oidc_user_id_claim` input field: "The Matrix User ID claim cannot be changed after completing setup."
  - [ ] Write unit test (AT#5)
- [ ] Task 4: Godog integration test `claim_lock.feature` (AC4)
  - [ ] Create `gateway/features/claim_lock.feature` with 2 scenarios
  - [ ] Implement step definitions (reuse existing patterns from `claim_mapping.feature`)
- [ ] Task 5: Run test suites
  - [ ] `make test-unit-go` — all unit tests pass
  - [ ] `make test-integration` — `claim_lock.feature` scenarios pass

---

## Dev Notes

### Context from Story 14.1a (already implemented)

The Core `UpdateServerConfig` gRPC RPC already returns `GRPC.Status.failed_precondition()` (code 9) when `matrix_user_id_claim` is changed after bootstrap. The proto field `matrix_user_id_claim = 7` exists in `UpdateServerConfigRequest` and the Go pb getter is `GetMatrixUserIdClaim() string`. This story adds the Gateway-layer protection and UI adaptation on top.

### AC1 Implementation: `PatchAdminConfig` in `gateway/internal/api/server.go`

**Current state of `PatchAdminConfig`** (line 92 in `server.go`):
- Handles: `instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`, `audit_log_retention_days`
- All fields go through `s.ServerConfig.UpsertServerConfigKey()` — the handler writes directly to DB
- `matrix_user_id_claim` must go through Core gRPC (`s.CoreClient.UpdateServerConfig`) because Core owns the bootstrap-lock logic

**New logic to add in `PatchAdminConfig`:**
```go
if body.MatrixUserIdClaim != nil && *body.MatrixUserIdClaim != "" {
    if s.CoreClient == nil {
        return PatchAdminConfig501Response{}, nil
    }
    _, grpcErr := s.CoreClient.UpdateServerConfig(ctx, &pb.UpdateServerConfigRequest{
        MatrixUserIdClaim: *body.MatrixUserIdClaim,
    })
    if grpcErr != nil {
        if st, ok := status.FromError(grpcErr); ok && st.Code() == codes.FailedPrecondition {
            return &patchAdminConfig400ForbiddenResp{
                msg: "matrix_user_id_claim cannot be changed after bootstrap",
            }, nil
        }
        slog.Error("PatchAdminConfig: UpdateServerConfig gRPC error", "err", grpcErr)
        return nil, grpcErr
    }
    changedKeys = append(changedKeys, "matrix_user_id_claim")
}
```

**New response type needed** (follow existing pattern of `patchAdminConfig400Resp`):
```go
// patchAdminConfig400ForbiddenResp — 400 M_FORBIDDEN for post-bootstrap claim lock.
type patchAdminConfig400ForbiddenResp struct{ msg string }

func (r *patchAdminConfig400ForbiddenResp) VisitPatchAdminConfigResponse(w http.ResponseWriter) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    return json.NewEncoder(w).Encode(map[string]any{
        "errcode": "M_FORBIDDEN",
        "error":   r.msg,
    })
}
```

**OpenAPI schema** (`gateway/openapi.yaml` or wherever the API spec lives):
Add `matrix_user_id_claim` as an optional string field in `PatchAdminConfigBody`. Run `make gen-api` after the schema change.

**Imports needed in `server.go`:**
```go
"google.golang.org/grpc/codes"
"google.golang.org/grpc/status"
```
These are already imported if other handlers use them; check `server.go` header.

### AC2 Implementation: `ClaimMappingHandler` + Template

**`ClaimMappingPageData` extension** (in `page_data.go`, line 297):
```go
type ClaimMappingPageData struct {
    PageData
    UserIDClaim      string
    DisplaynameClaim string
    EmailClaim       string
    Errors           map[string]string
    Flash            AlertBannerData
    BootstrapCompleted bool  // NEW: true when bootstrap_completed key is set
}
```

**`ClaimMappingHandler.Handler` extension** (in `claim_mapping.go`):
After loading the three claim values, check:
```go
bootstrapCompleted := false
if v, err := h.configReader.LoadServerConfigKey(r.Context(), "bootstrap_completed"); err == nil && v != "" {
    bootstrapCompleted = true
}
// ... populate data ...
data := ClaimMappingPageData{
    // ... existing fields ...
    BootstrapCompleted: bootstrapCompleted,
}
```

**`claim-mapping.html` template change:**
Replace the `oidc_user_id_claim` input field block with a conditional:
```html
{{ if .BootstrapCompleted }}
<div class="form-control">
  <label class="label">
    <span class="label-text">Matrix User ID Source Claim</span>
  </label>
  <div class="input input-bordered w-full bg-base-200 flex items-center px-3">
    <span class="text-base-content/80">{{ .UserIDClaim }}</span>
  </div>
  <p class="text-xs text-base-content/60 mt-1">
    The OIDC claim used to derive the Matrix local part.
  </p>
</div>
<div role="alert" aria-live="polite" class="alert alert-info mt-2 mb-4">
  <span class="text-sm">This claim cannot be changed after bootstrap.</span>
</div>
{{ else }}
{{/* existing editable input block */}}
{{ end }}
```

**The POST handler** (`UpdateHandler`): When `BootstrapCompleted` is true, the template does not render the editable input, so `oidc_user_id_claim` will be absent from the POST body. The handler should use the existing loaded value if the field is empty — or ideally, if the form is posted without the field, the SaveClaimMapping call will use the value from `LoadClaimMapping` for that field. However, the cleanest approach: if bootstrap is complete, reject any attempt to change `oidc_user_id_claim` at the handler level too (defense in depth). Load `bootstrap_completed` in `UpdateHandler` as well; if set and `oidc_user_id_claim` form field differs from DB, return 422 with a message.

### AC3 Implementation: Bootstrap Wizard Step 3 template (`bootstrap.html`)

The Bootstrap Wizard Step 3 is rendered by `BootstrapHandler.StepHandler` when `step=3`. The template file is `gateway/internal/admin/templates/bootstrap.html`. 

In Step 3's section (look for `{{ if eq .Step 3 }}` or the claim-mapping block), add an info alert before the `oidc_user_id_claim` input:
```html
<div role="alert" class="alert alert-info mb-4">
  <span class="text-sm">The Matrix User ID claim cannot be changed after completing setup.</span>
</div>
```

Note: This info text is ALWAYS shown in the wizard (wizard always runs pre-bootstrap), it's informational about what will happen after setup, not a post-bootstrap lock.

### AC4 Implementation: Godog integration test

**New file:** `gateway/features/claim_lock.feature`

Pattern: follow `claim_mapping.feature` and `admin_api.feature`. The test needs:
1. A forged admin session cookie (existing helper)
2. A way to control whether bootstrap is completed — use the existing "bootstrap is complete and server_config is seeded" step + a direct DB insert to set `bootstrap_completed`
3. A PATCH request to `/api/v1/admin/config` with JSON body `{"matrix_user_id_claim": "email"}`

```gherkin
Feature: Claim Lock — matrix_user_id_claim cannot be changed after bootstrap
  As an instance admin
  I want the API to reject changes to matrix_user_id_claim after bootstrap
  So that Matrix User IDs remain stable for all existing users

  Background:
    Given the server is running with a clean test database

  Scenario: POST-bootstrap PATCH attempt is rejected with 400 M_FORBIDDEN
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin API JWT token
    When I PATCH /api/v1/admin/config with body {"matrix_user_id_claim": "email"}
    Then the response status is 400
    And the response body contains "M_FORBIDDEN"
    And the response body contains "matrix_user_id_claim cannot be changed after bootstrap"

  Scenario: Pre-bootstrap PATCH attempt with matrix_user_id_claim succeeds
    Given bootstrap is NOT complete (no bootstrap_completed key in server_config)
    And I have a forged valid admin API JWT token
    When I PATCH /api/v1/admin/config with body {"matrix_user_id_claim": "preferred_username"}
    Then the response status is 200
```

### Important: OpenAPI spec and `make gen-api`

The `PatchAdminConfigBody` in the OpenAPI spec (`gateway/openapi.yaml` or similar path) must be extended to include `matrix_user_id_claim`:
```yaml
matrix_user_id_claim:
  type: string
  description: "OIDC claim used for Matrix User ID derivation. Cannot be changed after bootstrap."
```

After editing the spec, run:
```bash
make gen-api
```
This regenerates `gateway/internal/api/api_gen.go`. Verify that `PatchAdminConfigBody` gets the new field.

### Error response format for AC1

The story spec says `400 M_FORBIDDEN` with `error: "..."`. Looking at the existing `patchAdminConfig400Resp`:
```go
func (r *patchAdminConfig400Resp) VisitPatchAdminConfigResponse(w http.ResponseWriter) error {
    return json.NewEncoder(w).Encode(map[string]any{
        "error": map[string]string{"code": "M_BAD_JSON", "message": r.msg},
    })
}
```

For the claim lock, the spec uses Matrix-style flat format: `"errcode": "M_FORBIDDEN"` and `"error": "message"`. This is the Matrix Client-Server API error format. Use:
```json
{"errcode": "M_FORBIDDEN", "error": "matrix_user_id_claim cannot be changed after bootstrap"}
```

This is consistent with Matrix spec error format (flat, not nested `error.code`).

### gRPC imports in `server.go`

Check if `google.golang.org/grpc/codes` and `google.golang.org/grpc/status` are already imported. Look at the top of `gateway/internal/api/server.go` — these imports exist in `gateway/internal/admin/config.go` (line 10-11). Add to `server.go` if not present.

### File structure: what to touch

| File | Action | Notes |
|------|--------|-------|
| `gateway/openapi.yaml` | UPDATE | Add `matrix_user_id_claim` field to `PatchAdminConfigBody` |
| `gateway/internal/api/server.go` | UPDATE | Add `matrix_user_id_claim` handling + `patchAdminConfig400ForbiddenResp` in `PatchAdminConfig` |
| `gateway/internal/api/config_handler_test.go` | UPDATE | Add AT#1 + AT#2 unit tests |
| `gateway/internal/admin/page_data.go` | UPDATE | Add `BootstrapCompleted bool` to `ClaimMappingPageData` |
| `gateway/internal/admin/claim_mapping.go` | UPDATE | Load `bootstrap_completed` in `Handler` + set `BootstrapCompleted` |
| `gateway/internal/admin/templates/claim-mapping.html` | UPDATE | Conditional read-only vs editable for `oidc_user_id_claim` + info banner |
| `gateway/internal/admin/templates/bootstrap.html` | UPDATE | Add info text to Step 3 |
| `gateway/internal/admin/claim_mapping_handler_test.go` | UPDATE | Add AT#3 + AT#4 unit tests |
| `gateway/features/claim_lock.feature` | NEW | Godog integration test (AC4) |

**Do NOT create new Go handler files** — all changes are additions to existing files.

### Test patterns from codebase

**Mock for bootstrap state detection in API unit tests** (follow `mockServerConfigRepository`):
The `mockServerConfigRepository` needs a new method or field to simulate the bootstrap state for the gRPC path. In the API path, `matrix_user_id_claim` goes through `s.CoreClient.UpdateServerConfig` — so the mock needs a `mockCoreClientForConfig` that returns `FailedPrecondition` to simulate post-bootstrap:
```go
func (m *mockCoreClientForConfig) UpdateServerConfig(
    _ context.Context,
    _ *pb.UpdateServerConfigRequest,
    _ ...grpc.CallOption,
) (*pb.UpdateServerConfigResponse, error) {
    if m.updateServerConfigFailPrecondition {
        return nil, status.Error(codes.FailedPrecondition, "matrix_user_id_claim cannot be changed after bootstrap")
    }
    return &pb.UpdateServerConfigResponse{Ok: true}, m.updateServerConfigErr
}
```

**Mock for bootstrap state in Admin UI unit tests** (follow `recordingServerConfigReader`):
The `recordingServerConfigReader` already implements `LoadServerConfigKey`. Extend it to return `"true"` for the `"bootstrap_completed"` key when needed:
```go
func (r *recordingServerConfigReader) LoadServerConfigKey(_ context.Context, key string) (string, error) {
    if key == "bootstrap_completed" && r.bootstrapCompleted {
        return "true", nil
    }
    return "", nil
}
```

### Preserve existing behavior

- All other claim fields (`oidc_displayname_claim`, `oidc_email_claim`) remain editable after bootstrap — only `matrix_user_id_claim` / `oidc_user_id_claim` is locked
- The existing warning banner in `claim-mapping.html` ("Identity stability warning: Changing Matrix User ID Source Claim...") should be REPLACED by the info banner when bootstrap is completed (lock is active), or remain when bootstrap is NOT completed
- POST handler for claim-mapping must NOT call `SaveClaimMapping` for `oidc_user_id_claim` when bootstrap is complete — best approach: keep the existing DB value for that field when `BootstrapCompleted` is true
- Existing tests in `claim_mapping_handler_test.go` MUST continue to pass (they use pre-bootstrap state by default since `recordingServerConfigReader.LoadServerConfigKey` returns `""`)

### Sprint Status Key

The sprint-status.yaml key for this story is `14-1b-gateway-api-validation-admin-ui-readonly-matrix-user-id-claim`.

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

None.

### Completion Notes List

- OpenAPI spec extended with `matrix_user_id_claim` field in `PatchAdminConfigRequest`; `make gen-api` regenerated `api_gen.go`
- `PatchAdminConfig` in `server.go` now forwards `matrix_user_id_claim` to Core via `UpdateServerConfig` gRPC; `FailedPrecondition` → 400 M_FORBIDDEN with flat Matrix error format
- `patchAdminConfig400ForbiddenResp` added to `server.go` for the post-bootstrap claim lock response
- `ClaimMappingPageData` extended with `BootstrapCompleted bool` field
- `ClaimMappingHandler.Handler` detects bootstrap state via `LoadServerConfigKey("bootstrap_completed")`
- `ClaimMappingHandler.UpdateHandler` preserves locked `oidc_user_id_claim` from DB when bootstrap complete; aborts with 500 if DB load fails (defense in depth against empty claim write)
- `claim-mapping.html` template conditionally renders read-only display + info banner post-bootstrap, warning banner pre-bootstrap
- `bootstrap.html` Step 3 adds info text: "The Matrix User ID claim cannot be changed after completing setup."
- ATDD tests added: AT#1+AT#2 in `config_handler_test.go`, AT#3+AT#4 in `claim_mapping_handler_test.go`, AT#5 in `bootstrap_wizard_test.go`
- Integration test: `gateway/features/claim_lock.feature` + `claim_lock_steps_test.go` (2 scenarios)
- Code review MINOR-2 fixed inline: post-bootstrap UpdateHandler now aborts on `LoadClaimMapping` error
- Security review: 0 CRITICAL, 0 HIGH, 1 LOW advisory (no API-level format validation on claim value — admin-only, acceptable for MVP)

### File List

- `gateway/api/openapi.yaml` — added `matrix_user_id_claim` to `PatchAdminConfigRequest`
- `gateway/internal/api/api_gen.go` — regenerated (MatrixUserIdClaim field added)
- `gateway/internal/api/server.go` — gRPC path for matrix_user_id_claim + patchAdminConfig400ForbiddenResp
- `gateway/internal/api/config_handler_test.go` — AT#1 + AT#2 + UpdateServerConfig mock
- `gateway/internal/admin/page_data.go` — BootstrapCompleted bool on ClaimMappingPageData
- `gateway/internal/admin/claim_mapping.go` — bootstrap detection in Handler + UpdateHandler defense
- `gateway/internal/admin/templates/claim-mapping.html` — conditional read-only display
- `gateway/internal/admin/templates/bootstrap.html` — Step 3 info text
- `gateway/internal/admin/claim_mapping_handler_test.go` — AT#3 + AT#4
- `gateway/internal/admin/bootstrap_wizard_test.go` — AT#5
- `gateway/features/claim_lock.feature` — Godog integration test (2 scenarios)
- `gateway/test/integration/claim_lock_steps_test.go` — step definitions
- `gateway/test/integration/steps_test.go` — registered initializeClaimLockSteps

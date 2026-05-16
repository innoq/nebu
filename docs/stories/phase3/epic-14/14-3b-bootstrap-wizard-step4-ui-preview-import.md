---
status: ready-for-dev
epic: 14
story: 3b
security_review: not-needed
matrix: false
ui: true
---

# Story 14.3b: Bootstrap Wizard Step 4 UI — Preview + Import

Status: ready-for-dev

## Story

As an instance admin,
I want a new Bootstrap Wizard Step 4 "User Import" that shows a preview of OIDC users and lets me trigger bulk import,
So that I can pre-provision all users during the initial setup without leaving the wizard.

**Size:** S
**security_review:** not-needed

---

## Acceptance Criteria

**AC1 — Step 4 displayed:**
Given the Bootstrap Wizard renders,
When the admin reaches Step 4 (after Claim Mapping OIDC redirect completes),
Then the "User Import" step is displayed with an "Import from OIDC" button.

**AC2 — Preview table on button click:**
Given the admin is on Step 4,
When the admin clicks "Import from OIDC",
When the OIDC user list is fetched via OIDCDirectoryService (story 14.2b),
Then a preview table is shown with: display name, email, and computed Matrix User ID for each user.

**AC3 — Import all/selected triggers API:**
Given the preview table is shown,
When the admin clicks "Import all" (or "Import selected"),
Then `POST /api/v1/admin/bootstrap/import-users` is called and the result (imported/skipped/failed counts) is displayed.

**AC4 — Disabled button when provider does not support listing:**
Given the OIDC directory service is disabled or the provider does not expose a user list endpoint,
When the wizard renders Step 4,
Then the "Import from OIDC" button is disabled and a message reads: "Provider does not support user listing".

**AC5 — Playwright+Gherkin scenarios pass:**
Given a Playwright+Gherkin scenario in `e2e/features/admin/bootstrap_import.feature`,
When `make test-integration` runs,
Then the scenarios "wizard step 4 displayed", "preview table loaded", and "import button clicked" pass.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-1 — Step 4 rendered** — Go httptest (unit)
   - Given: BootstrapHandler with OIDC dir enabled
   - When: GET /admin/bootstrap (or wizard step flow reaches step 4)
   - Then: response body contains "User Import" heading and "Import from OIDC" button

2. **AT-2 — Preview fetch returns table** — Go httptest (unit)
   - Given: POST /api/v1/admin/bootstrap/preview-users with mock OIDCDirectoryService returning 3 users
   - When: handler processes the request
   - Then: JSON response contains 3 user objects with display_name, email, matrix_user_id fields

3. **AT-3 — Import API called with correct payload** — Go httptest (unit)
   - Given: POST /api/v1/admin/bootstrap/import-users with mock BulkImportUsers gRPC client
   - When: handler processes the request with user list
   - Then: BulkImportUsers gRPC is called; response JSON has imported/skipped/failed counts

4. **AT-4 — Disabled button when OIDC dir disabled** — Go httptest (unit)
   - Given: BootstrapHandler with OIDCDirectoryService.IsEnabled() = false
   - When: Step 4 renders
   - Then: "Import from OIDC" button has disabled attribute; message "Provider does not support user listing"

5. **AT-5 — Playwright+Gherkin: wizard step 4 displayed** — e2e/features/admin/bootstrap_import.feature
   - Given: admin is at bootstrap step 4
   - When: page loads
   - Then: "User Import" heading visible; "Import from OIDC" button present

6. **AT-6 — Playwright+Gherkin: preview table loaded** — e2e/features/admin/bootstrap_import.feature
   - Given: mock/stub OIDC users available
   - When: "Import from OIDC" clicked
   - Then: preview table with at least one row visible

7. **AT-7 — Playwright+Gherkin: import button clicked** — e2e/features/admin/bootstrap_import.feature
   - Given: preview table loaded
   - When: "Import all" clicked
   - Then: result section with counts (imported/skipped/failed) visible

---

## Technical Context

### What's Done in Previous Stories

**Story 14.2b (gateway/internal/admin/oidc_directory.go):**
- `OIDCDirectoryService` — `FetchUsers(ctx) ([]OIDCDirectoryUser, error)`
- `OIDCDirectoryUser` — `{Sub, DisplayName, Email}`
- `IsEnabled() bool` — reports if OIDC directory is configured
- `Allow(sessionID string) bool` — rate limiter
- Cache 30s, singleflight, 10MB cap, HTTPS-only

**Story 14.3a (Core gRPC):**
- Proto: `BulkImportUsersRequest{repeated OIDCUserClaims}` → `BulkImportUsersResponse{imported, skipped, failed int32}`
- `OIDCUserClaims{user_id, system_role, display_name, email string}`
- gRPC client already generated: `CoreServiceClient.BulkImportUsers(...)`
- Mock stub already exists: `mockCoreClient.BulkImportUsers` in `auth_audit_test.go`

### Existing Bootstrap Wizard State

`gateway/internal/admin/bootstrap.go`:
- `StepHandler` handles POST /admin/bootstrap
- Steps 1–3: Instance Name → OIDC Config → Claim Mapping
- Step 3 redirects to OIDC login: `http.Redirect(w, r, "/admin/login/start?mode=bootstrap", http.StatusSeeOther)`
- After OIDC callback: `ClaimSelectionHandler` in `auth.go` finalises bootstrap and sets `bootstrap_completed`
- **Step 4 does not exist yet** — this story adds it

The bootstrap flow after this story:
1. Step 1: Instance Name → Next
2. Step 2: OIDC Config → Next
3. Step 3: Claim Mapping → Connect with OIDC (OIDC redirect)
4. OIDC callback + claim selection → redirect to Step 4 (new)
5. Step 4: User Import → Import from OIDC → preview → Import all → done / skip

Alternative approach (simpler): Step 4 is a standalone page after the wizard completes, reachable at `/admin/bootstrap/import-users`. The wizard claims-callback currently redirects to `/admin/dashboard`. **Change the redirect to go to Step 4 instead**, then from Step 4 the admin can proceed to dashboard.

### BootstrapPageData (gateway/internal/admin/page_data.go)

Currently defined:
```go
type BootstrapPageData struct {
    PageData
    Step         int
    InstanceName string
    OIDCIssuer   string
    OIDCClientID string
    MaskedSecret string
    Errors       map[string]string
    Warnings     map[string]string
    OIDCUserIDClaim      string
    OIDCDisplaynameClaim string
    OIDCEmailClaim       string
}
```

Extend for Step 4:
```go
// ImportPreviewUser is one row in the Step 4 user preview table.
type ImportPreviewUser struct {
    DisplayName  string
    Email        string
    MatrixUserID string
}

// ImportResult holds the counts returned by POST /api/v1/admin/bootstrap/import-users.
type ImportResult struct {
    Imported int32
    Skipped  int32
    Failed   int32
}
```

Add to `BootstrapPageData`:
```go
// Step 4 fields
OIDCDirectoryEnabled bool          // true = "Import from OIDC" button enabled
ImportPreview        []ImportPreviewUser // populated after "Import from OIDC" click
ImportResult         *ImportResult       // populated after "Import all" submit (nil = not yet)
ImportError          string              // non-empty on fetch/import error
```

### New API Endpoints (HTTP)

**POST /api/v1/admin/bootstrap/preview-users**
- Auth: admin session required (same auth middleware as other admin API endpoints)
- Bootstrap guard: must be in bootstrap mode
- Calls `OIDCDirectoryService.FetchUsers(ctx)`
- Computes Matrix User ID for each: `"@" + sanitizeOIDCSub(user.Sub) + ":" + serverName`
- Returns JSON: `[{"display_name":"...", "email":"...", "matrix_user_id":"@..."}]`
- If OIDC dir disabled: return 422 `{"error":"provider does not support user listing"}`
- Rate-limit check: `oidcDir.Allow(sessionID)`

**POST /api/v1/admin/bootstrap/import-users**
- Auth: admin session required
- Bootstrap guard: must be in bootstrap mode
- Request body JSON: `{"users":[{"display_name":"...", "email":"...", "matrix_user_id":"@..."}]}`
  OR reads from session/cache (simpler: re-fetch from OIDC dir then call gRPC)
- Calls `CoreServiceClient.BulkImportUsers` with `OIDCUserClaims` list
- Reads claim config from DB: `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`
- Returns JSON: `{"imported":N,"skipped":N,"failed":N}`
- On gRPC error: 502 with `{"error":"core unavailable"}`

**Design note:** Keep it simple — import-users re-fetches users from OIDC dir (cache hit after preview), builds OIDCUserClaims from configured claims, then calls BulkImportUsers. No session storage needed.

### Template Changes (gateway/internal/admin/templates/bootstrap.html)

1. Update progress indicator to 4 steps:
```html
<li class="step {{ if ge .Step 1 }}step-primary{{ end }}">Instance</li>
<li class="step {{ if ge .Step 2 }}step-primary{{ end }}">OIDC</li>
<li class="step {{ if ge .Step 3 }}step-primary{{ end }}">Claims</li>
<li class="step {{ if ge .Step 4 }}step-primary{{ end }}">Import</li>
```

2. Add `{{ if eq .Step 4 }}` block with:
   - "Step 4: User Import" heading
   - If `!.OIDCDirectoryEnabled`: disabled button + "Provider does not support user listing" message
   - "Import from OIDC" button (HTMX or standard form POST to `/api/v1/admin/bootstrap/preview-users`)
   - Preview table (shown when `len(.ImportPreview) > 0`)
   - "Import all" button (POST to `/api/v1/admin/bootstrap/import-users`)
   - Result banner (shown when `.ImportResult != nil`)
   - "Skip and finish" link → `/admin/dashboard`

**Implementation approach for Step 4 (no HTMX — use simple form POSTs):**

Since the Admin UI uses Go templates without HTMX, implement Step 4 as two sub-steps via the standard form pattern:
- Sub-step 4a: Show "Import from OIDC" button → POST /admin/bootstrap (step=4, action=preview) → re-render step 4 with preview table
- Sub-step 4b: Show preview table + "Import all" → POST /admin/bootstrap (step=4, action=import) → re-render step 4 with result
- Extend `StepHandler` `case 4:` to handle `action=preview` and `action=import`

This keeps all logic in the existing BootstrapHandler without new handlers.

### BootstrapHandler Dependencies

Add to `BootstrapHandler`:
```go
type BootstrapHandler struct {
    checker    BootstrapStatusChecker
    tmpl       *TemplateHandler
    persister  BootstrapPersister
    draftStore BootstrapDraftStore
    secret     []byte
    oidcDir    *OIDCDirectoryService // nil = OIDC dir disabled (Step 4 shows disabled state)
    core       BulkImportClient      // gRPC client for BulkImportUsers
    serverName string                // Matrix server name for Matrix ID computation
}

// BulkImportClient is a minimal interface for the BulkImportUsers gRPC call.
type BulkImportClient interface {
    BulkImportUsers(ctx context.Context, req *pb.BulkImportUsersRequest, opts ...grpc.CallOption) (*pb.BulkImportUsersResponse, error)
}
```

Update `NewBootstrapHandler` signature (backward compatible via functional option or direct args):
```go
func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler, db *sql.DB, secret []byte) *BootstrapHandler
```

Add a fluent method:
```go
func (h *BootstrapHandler) WithImportServices(oidcDir *OIDCDirectoryService, core BulkImportClient, serverName string) *BootstrapHandler
```

### Auth Callback: Redirect to Step 4

After successful claim selection in `auth.go` (`ClaimSelectionHandler`), change the redirect from:
```go
http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
```
to:
```go
http.Redirect(w, r, "/admin/bootstrap?step=4", http.StatusSeeOther)
```

This allows the existing handler flow to render Step 4. Step 4 "Skip and finish" → `/admin/dashboard`.

### Matrix User ID Computation

Reuse the same function as `users.go`:
```go
localpart := sanitizeOIDCSub(user.Sub)
matrixUserID := "@" + localpart + ":" + serverName
```

`sanitizeOIDCSub` is already defined in `users.go` and accessible within the same package.

### Claim Reading for BulkImportUsers

When building `OIDCUserClaims` for BulkImportUsers, read the configured claim keys from `server_config`:
- `oidc_user_id_claim` → maps to `user.Sub` (used for Matrix localpart)
- `oidc_displayname_claim` → maps to `user.DisplayName`
- `oidc_email_claim` → maps to `user.Email`

The `OIDCDirectoryUser` struct already has these fields populated by `FetchUsers`.

For `system_role`: always use `"user"` for bulk import (per Story 14.3a spec).

### File Changes Required

| File | Change |
|------|--------|
| `gateway/internal/admin/page_data.go` | Add `ImportPreviewUser`, `ImportResult` structs; extend `BootstrapPageData` with Step 4 fields |
| `gateway/internal/admin/bootstrap.go` | Add `BulkImportClient` interface; extend `BootstrapHandler` with `oidcDir`, `core`, `serverName`; add `WithImportServices` method; add `case 4:` in `StepHandler` |
| `gateway/internal/admin/templates/bootstrap.html` | Update steps to 4; add `{{ if eq .Step 4 }}` block |
| `gateway/internal/admin/auth.go` | Change claim-selection redirect from `/admin/dashboard` to `/admin/bootstrap?step=4` |
| `gateway/internal/admin/bootstrap_import_test.go` | **New file** — unit tests AT-1..AT-4 |
| `e2e/features/admin/bootstrap_import.feature` | **New file** — Gherkin scenarios AT-5..AT-7 |
| `e2e/step-definitions/admin/bootstrap_import.steps.ts` | **New file** — step definitions for AT-5..AT-7 |

### Step Handler Case 4 Logic

```go
case 4:
    action := r.FormValue("action")
    
    if action == "preview" {
        // Fetch users from OIDC dir
        if h.oidcDir == nil || !h.oidcDir.IsEnabled() {
            data.Step = 4
            data.OIDCDirectoryEnabled = false
            h.tmpl.render(w, "bootstrap", data)
            return
        }
        users, err := h.oidcDir.FetchUsers(r.Context())
        // ... build preview
        data.ImportPreview = preview
        data.OIDCDirectoryEnabled = true
        data.Step = 4
        h.tmpl.render(w, "bootstrap", data)
        return
    }
    
    if action == "import" {
        // Re-fetch + call BulkImportUsers
        // ... build OIDCUserClaims + call h.core.BulkImportUsers
        data.ImportResult = &ImportResult{...}
        data.Step = 4
        h.tmpl.render(w, "bootstrap", data)
        return
    }
    
    // Initial render of step 4
    data.Step = 4
    data.OIDCDirectoryEnabled = h.oidcDir != nil && h.oidcDir.IsEnabled()
    h.tmpl.render(w, "bootstrap", data)
```

### Back Navigation

Step 4 back button should redirect to `/admin/dashboard` (bootstrap complete, cannot undo). OR go back to step 3 template view. Given that step 3 already triggered OIDC, going back to step 3 doesn't make sense — omit a Back button on step 4, only provide "Skip and finish" and "Import" actions.

### E2E Test Approach (Playwright+Gherkin)

The full OIDC flow for bootstrap is complex. The Playwright step-4 tests should:
1. Use `doBootstrapAdmin()` fixture to complete bootstrap (steps 1-3) first
2. Then navigate directly to `/admin/bootstrap?step=4` (bypassing OIDC redirect)
3. Test the Step 4 UI directly

Since Step 4 renders when the server gets a GET with `?step=4`, the handler must support GET with step param, OR the feature test navigates via POST form. Simplest: add a separate `GET /admin/bootstrap?step=4` handler path that renders step 4 when bootstrap is already completed.

**Alternative (recommended for testability):** The GET handler for `/admin/bootstrap` already renders step 1. Add a check: if `?step=4` is provided AND bootstrap is completed, render step 4. OR simply test via the `doBootstrapAdmin()` + direct navigation as admin.

For the feature test, use a stub OIDC directory by calling the preview API directly from the test (bypassing the real Dex/OIDC flow) to verify the table appears.

### Test Double for BulkImportClient

```go
type fakeBulkImportClient struct {
    resp *pb.BulkImportUsersResponse
    err  error
}

func (f *fakeBulkImportClient) BulkImportUsers(_ context.Context, _ *pb.BulkImportUsersRequest, _ ...grpc.CallOption) (*pb.BulkImportUsersResponse, error) {
    return f.resp, f.err
}
```

### BootstrapHandler GET — Step 4 Query Param

Modify the `Handler` (GET) method to check for `?step=4`:
```go
func (h *BootstrapHandler) Handler(w http.ResponseWriter, r *http.Request) {
    // If step=4 is explicitly requested (post-bootstrap redirect), render step 4
    if r.URL.Query().Get("step") == "4" {
        data := BootstrapPageData{
            PageData:             bootstrapPD,
            Step:                 4,
            OIDCDirectoryEnabled: h.oidcDir != nil && h.oidcDir.IsEnabled(),
        }
        h.tmpl.render(w, "bootstrap", data)
        return
    }
    // ... existing step 1 render
}
```

### Sprint Status Key

Story key for sprint-status.yaml: `14-3b-bootstrap-wizard-step4-ui-preview-import`

---

## Dev Notes

- Do NOT use HTMX — the Admin UI is plain Go templates with standard form POSTs.
- Keep Step 4 within the existing `BootstrapHandler` — no new handler struct required.
- `BulkImportClient` interface must match `CoreServiceClient.BulkImportUsers` signature exactly (including `...grpc.CallOption` variadic).
- The `mockCoreClient` in `auth_audit_test.go` has a `BulkImportUsers` panic stub — do not rely on it; define `fakeBulkImportClient` separately in the new test file.
- `sanitizeOIDCSub` is package-internal (lowercase `s`) in `users.go` — accessible from `bootstrap.go` since both are in `package admin`.
- For E2E tests: Step 4 is only reachable after bootstrap completes, so E2E scenarios must either use `doBootstrapAdmin()` or mark themselves as requiring a pre-bootstrapped instance.
- The preview API does NOT need to be a separate REST endpoint — embedding the logic in `StepHandler case 4:` via `action=preview` is sufficient and follows existing patterns.
- `auth.go` redirect change: find the line that does `http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)` inside `ClaimSelectionHandler` after saving bootstrap config. Change to `/admin/bootstrap?step=4`.
- Claim mapping is saved in `server_config` after the OIDC callback — `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`. For BulkImportUsers, read these from the DB (via `draftStore.LoadDraft` or direct DB query).

---

## Definition of Done

- [ ] `BootstrapPageData` extended with Step 4 fields
- [ ] `BootstrapHandler` extended with `oidcDir`, `core`, `serverName`, `WithImportServices`
- [ ] `StepHandler` handles `case 4:` with `action=preview` and `action=import`
- [ ] `bootstrap.html` template updated: 4-step indicator + Step 4 block
- [ ] `auth.go` `ClaimSelectionHandler` redirects to `/admin/bootstrap?step=4` instead of `/admin/dashboard`
- [ ] `bootstrap_import_test.go` — AT-1..AT-4 all green
- [ ] `e2e/features/admin/bootstrap_import.feature` — 3 Gherkin scenarios
- [ ] `e2e/step-definitions/admin/bootstrap_import.steps.ts` — step definitions
- [ ] `make test-unit-go` passes
- [ ] `make test-integration` passes (E2E gate)

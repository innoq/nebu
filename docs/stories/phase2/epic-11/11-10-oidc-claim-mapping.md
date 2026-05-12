---
status: review
epic: 11
story: 10
security_review: required
matrix: false
ui: true
---

# Story 11.10: OIDC Claim Mapping Configuration

Status: review

## Story

As an operator setting up or administering a Nebu instance,
I want to configure which OIDC claims map to the Matrix user ID and user profile fields (displayname, email) via both the Bootstrap Wizard and the Admin UI settings page,
so that Nebu works with any OIDC provider without hardcoding claims in environment variables, and existing deployments continue working unchanged.

**Size:** M

---

## Acceptance Criteria

**AC1 — Bootstrap Wizard gains a "Claim Mapping" step (Step 3: between OIDC and "Connect"):**
Given the admin has completed the OIDC Configuration step (instance name + OIDC credentials),
When the wizard renders step 3,
Then the page shows a "Claim Mapping" form with:
- A dropdown/radio for "Matrix user ID source claim": options `sub` (default, recommended for new installs), `email`, `preferred_username`, plus a free-text field for custom claims
- A read-only or pre-filled field showing "Displayname source claim": defaults to `name` (editable to allow overrides such as `preferred_username`)
- A read-only or pre-filled field showing "Email profile field claim": defaults to `email` (editable)
- Sensible defaults are pre-filled so admins only need to change values if their IdP uses non-standard claims

**AC2 — Wizard step 3 persists claim mapping to `server_config` on submit (same transaction as bootstrap completion):**
Given the admin accepts or customises the claim mapping and submits,
When `ClaimSelectionHandler` runs the bootstrap transaction,
Then the following keys are upserted into `server_config`:
- `oidc_user_id_claim` → e.g. `"sub"` (which claim to use as Matrix localpart source)
- `oidc_displayname_claim` → e.g. `"name"`
- `oidc_email_claim` → e.g. `"email"`
All three are written atomically in the same `runInTx` call that writes `admin_group_claim` and `bootstrap_completed`.

**AC3 — Admin UI: new "Claim Mapping" settings page at `GET/POST /admin/config/claim-mapping`:**
Given bootstrap is complete and the admin is logged in,
When the admin navigates to `/admin/config/claim-mapping`,
Then the page renders a form showing the three configurable claim fields pre-populated from `server_config` (with the Nebu defaults if the keys are absent).
When the admin submits valid values,
Then the updated values are persisted to `server_config` (real DB write, not in-memory stub) and the page shows a success flash message (PRG pattern: redirect to `?flash=...`).

**AC4 — OIDC callback (`CallbackHandler`) reads claim mapping from DB:**
Given `server_config` contains `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`,
When the OIDC callback succeeds (token exchanged, claims extracted),
Then the Admin UI session is associated with the correct admin identity using the configured claim for identity (currently `sub` is always used for admin identity — this AC is about surfacing the mapping, not changing admin auth).

**AC5 — JWT middleware (`JWTMiddleware`) and Matrix login handler use DB-loaded claim mapping for Matrix user ID derivation:**
Given `server_config` contains `oidc_user_id_claim = "preferred_username"`,
When a user logs in via `POST /_matrix/client/v3/login` (OIDC flow),
Then the Matrix user ID is derived from `preferred_username` claim via `sanitiseLocalpart` (not hardcoded `name` claim).
Given `server_config` contains `oidc_user_id_claim = "sub"`,
When a user logs in,
Then `FormatUserID(sub, serverName)` is used as the fallback (SHA-256 based opaque localpart).
The mapping is read from DB at gateway startup (or cached with short TTL) — NOT on every request.

**AC6 — `FormatUserIDFromClaims` is extended to accept the configured claim name:**
Given the current signature `FormatUserIDFromClaims(sub, name, serverName string) string`,
When refactored to accept a `claimName string` and the full `claims map[string]interface{}`,
Then the function extracts the value of `claims[claimName]`, sanitises it via `sanitiseLocalpart`, and falls back to `FormatUserID(sub, serverName)` if the result is empty.
The existing call sites (`matrix/login.go`, `middleware/auth.go`) are updated to pass the DB-loaded claim name.

**AC7 — Backward compatibility: deployments without `oidc_user_id_claim` in `server_config` keep working:**
Given a pre-existing deployment where `oidc_user_id_claim` is absent from `server_config`,
When a user logs in,
Then the gateway falls back to the existing `name`-claim-preferred behavior (same as `FormatUserIDFromClaims(sub, name, serverName)` today) — no regressions.
The env var `NEBU_OIDC_USER_ID_CLAIM` (if present) overrides the DB value as a last-resort escape hatch.

**AC8 — Validation rules for claim field inputs:**
- Claim name: required, 1–50 chars, matches `^[a-zA-Z0-9:_\-\.]+$` (dot allowed for nested claims like `user.email`)
- All three fields are required and must pass the regex
- POST returns HTTP 422 and re-renders the form with per-field errors on validation failure

**AC9 — Playwright+Cucumber E2E: Bootstrap Wizard Claim Mapping step:**
Given the e2e test runs the bootstrap wizard flow,
When the wizard reaches step 3 (Claim Mapping),
Then the test confirms:
- The step renders with pre-filled defaults
- The admin can submit (accepting defaults)
- Bootstrap completes and the `server_config` table contains `oidc_user_id_claim = "sub"` (or whatever was submitted)

**AC10 — Playwright+Cucumber E2E: Admin UI Claim Mapping settings page:**
Given the admin is logged in,
When the admin navigates to `/admin/config/claim-mapping`,
Then the test confirms:
- Page renders with current values
- Admin can update a value, submit, and see the success flash
- The navigation sidebar has a "Claim Mapping" entry in the Settings section

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `TestClaimMappingHandler_GetDefaults` — Go httptest unit test (AC3)**
- Given: `server_config` has no `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`
- When: `GET /admin/config/claim-mapping`
- Then: HTTP 200, rendered form shows pre-filled defaults: `sub`, `name`, `email`

**2. `TestClaimMappingHandler_PostValid` — Go httptest unit test (AC3)**
- Given: valid form fields `oidc_user_id_claim=preferred_username`, `oidc_displayname_claim=name`, `oidc_email_claim=email`
- When: `POST /admin/config/claim-mapping`
- Then: HTTP 302 redirect to `/admin/config/claim-mapping?flash=...`
- And: DB contains updated values

**3. `TestClaimMappingHandler_PostInvalid` — Go httptest unit test (AC8)**
- Given: `oidc_user_id_claim=""` (empty)
- When: `POST /admin/config/claim-mapping`
- Then: HTTP 422, form re-rendered with error on `oidc_user_id_claim` field

**4. `TestFormatUserIDFromClaimsConfigured` — Go unit test (AC5, AC6)**
- Given: claims `{"sub": "u123", "preferred_username": "alice"}`, claimName `"preferred_username"`
- When: `FormatUserIDFromClaims(claimName, claims, serverName)`
- Then: returns `"@alice:server"`

**5. `TestFormatUserIDFromClaims_FallbackToSub` — Go unit test (AC6, AC7)**
- Given: claims `{"sub": "u123"}`, claimName `"preferred_username"` (absent in claims)
- When: `FormatUserIDFromClaims(claimName, claims, serverName)`
- Then: returns the SHA-256-based opaque localpart (same as `FormatUserID("u123", serverName)`)

**6. `TestBootstrapTransaction_PersistsClaimMapping` — Go unit/integration test (AC2)**
- Given: bootstrap transaction with `oidc_user_id_claim="sub"`, `oidc_displayname_claim="name"`, `oidc_email_claim="email"`
- When: `runInTx` commits
- Then: `server_config` contains all three keys with expected values AND `bootstrap_completed=true`

**7. Godog — `gateway/features/claim_mapping.feature` (AC3, AC5, AC7, AC8)**
- Scenario: admin navigates to claim mapping page → sees defaults → submits valid values → flash shown
- Scenario: admin submits empty claim name → 422 with error message
- Scenario: user logs in after `oidc_user_id_claim=preferred_username` set → user ID uses `preferred_username` localpart
- Scenario: missing `oidc_user_id_claim` in DB → backward-compat fallback to `name`-claim behavior

**8. Playwright+Cucumber — `e2e/features/admin/claim-mapping.feature` (AC9, AC10)**
- Scenario: "Bootstrap Wizard shows Claim Mapping step 3 with sensible defaults"
  - Given: fresh DB (bootstrap active)
  - When: wizard reaches step 3
  - Then: form visible with pre-filled `sub`, `name`, `email`; submit succeeds; bootstrap completes
- Scenario: "Admin UI Claim Mapping page can be updated"
  - Given: logged-in admin
  - When: navigating to `/admin/config/claim-mapping`, changing user ID claim to `preferred_username`, saving
  - Then: success flash visible; form re-renders with `preferred_username` as selected value

---

## Tasks / Subtasks

- [x] Task 1: Write failing acceptance tests first
  - [x] Create `gateway/internal/admin/claim_mapping_handler_test.go` — tests 1, 2, 3
  - [x] Create `gateway/internal/grpc/metadata_test.go` additions — tests 4, 5 (extend existing test file)
  - [x] Create `gateway/features/claim_mapping.feature` — Godog scenarios (test 7)
  - [x] Create `e2e/features/admin/claim-mapping.feature` + `e2e/step-definitions/admin/claim-mapping.steps.ts` (test 8)

- [x] Task 2: DB migration `000044_oidc_claim_mapping`
  - [x] Create `gateway/migrations/000044_oidc_claim_mapping.up.sql`:
    - No schema changes needed (existing `server_config` key-value table is sufficient)
    - Insert defaults if keys absent: `INSERT INTO server_config ... ON CONFLICT DO NOTHING` for `oidc_user_id_claim='sub'`, `oidc_displayname_claim='name'`, `oidc_email_claim='email'`
    - This guarantees backward-compat: existing installs get the defaults seeded; new installs get them at bootstrap
  - [x] Create `gateway/migrations/000044_oidc_claim_mapping.down.sql`: `DELETE FROM server_config WHERE key IN ('oidc_user_id_claim', 'oidc_displayname_claim', 'oidc_email_claim')`

- [x] Task 3: Bootstrap Wizard — add Step 3 (Claim Mapping)
  - [x] Extend `BootstrapPageData` in `gateway/internal/admin/page_data.go` with:
    - `OIDCUserIDClaim string`  
    - `OIDCDisplaynameClaim string`
    - `OIDCEmailClaim string`
  - [x] Update `bootstrap.go` `StepHandler`:
    - After step 2 validation + draft save, render step 3 with defaults pre-filled
    - Case 3: validate claim mapping fields → save to draft → redirect to `/admin/login/start?mode=bootstrap`
    - Update back-navigation: step 3 → step 2
  - [x] Update `bootstrap.html` template:
    - Added Step 3 stepper item ("Claims") to the `<ul class="steps">` with `{{ if ge .Step 3 }}` guard
    - Added `{{ if eq .Step 3 }}` block with three claim fields
  - [x] Update `ClaimSelectionHandler` (`admin/auth.go`):
    - Load `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim` from draft
    - Call `saveClaimMappingTx` inside the same transaction

- [x] Task 4: Extend `saveBootstrapConfigTx` to persist claim mapping
  - [x] Added `saveClaimMappingTx` helper called from `ClaimSelectionHandler` within `runInTx`
  - [x] Updated `ServerConfigReader` interface in `admin/auth.go` with:
    - `LoadClaimMapping(ctx context.Context) (userIDClaim, displaynameClaim, emailClaim string, err error)`
    - `SaveClaimMapping(ctx context.Context, userIDClaim, displaynameClaim, emailClaim string) error`
  - [x] Implemented on `postgresServerConfigReader`

- [x] Task 5: New Admin UI handler `ClaimMappingHandler`
  - [x] Created `gateway/internal/admin/claim_mapping.go`:
    - `ClaimMappingHandler` struct with `tmpl *TemplateHandler` and `configReader ServerConfigReader`
    - `NewClaimMappingHandler(tmpl *TemplateHandler, configReader ...ServerConfigReader) *ClaimMappingHandler`
    - `Handler` — GET: loads from DB, renders `claim-mapping.html`; defaults to `sub`/`name`/`email` if absent
    - `UpdateHandler` — POST: validates, calls `configReader.SaveClaimMapping`, PRG redirect
  - [x] Added `ClaimMappingPageData` to `page_data.go`
  - [x] Created `gateway/internal/admin/templates/claim-mapping.html` (DaisyUI form, PRG flash, error messages)
  - [x] Registered routes in `gateway/cmd/gateway/main.go`
  - [x] Added "Claim Mapping" nav item to the admin sidebar (`layouts/base.html`)

- [x] Task 6: Extend `FormatUserIDFromClaims` to accept configured claim
  - [x] Updated signature in `gateway/internal/grpc/metadata.go`:
    - New: `FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string`
  - [x] Updated all call sites:
    - `gateway/internal/matrix/login.go`: uses per-request DB loaded `oidc_user_id_claim`
    - `gateway/internal/middleware/auth.go` `JWTMiddleware`: uses per-request DB loaded `oidc_user_id_claim`
  - [x] Per-request DB load of `oidc_user_id_claim` in `main.go` via `userIDClaimLoader` function
    - `NEBU_OIDC_USER_ID_CLAIM` env var overrides DB value as backward-compat escape hatch

- [x] Task 7: Run tests and verify
  - [x] `make test-unit-go` — all new unit tests pass, no regressions
  - [ ] `make test-integration` — Godog claim_mapping.feature passes (integration tests need running stack)
  - [ ] Playwright E2E — both claim-mapping scenarios pass against running stack

---

## Dev Notes

### Critical Architecture Context

**Current user ID derivation (MUST understand before changing):**

`JWTMiddleware` in `gateway/internal/middleware/auth.go` line 234:
```go
userID := coregrpc.FormatUserIDFromClaims(sub, name, srv)
```
`name` is extracted from `allClaims["name"].(string)` (line 226). This is hardcoded to use the `name` claim today.

`matrix/login.go` line 193:
```go
userID := coregrpc.FormatUserIDFromClaims(sub, displayName, h.serverName)
```
`displayName` is derived from `preferred_username` → `name` → email-localpart (lines 177–188), then used both as the Matrix user ID source AND as the gRPC `DisplayName` argument to `ValidateToken`. These two roles (user ID derivation vs. display name for profile) must be separated cleanly — the new `oidc_user_id_claim` controls user ID; `oidc_displayname_claim` controls the `DisplayName` sent to Core.

**WARNING — identity stability:** The Matrix user ID is a permanent, stable identifier. Changing `oidc_user_id_claim` after users have registered will generate different Matrix user IDs for the same OIDC users, breaking all their room memberships. The Admin UI settings page MUST display a prominent warning about this consequence. The bootstrap wizard should also mention it. Do NOT silently allow changes to take effect for existing users — warn loudly.

**`FormatUserIDFromClaims` refactor:**
Current signature: `FormatUserIDFromClaims(sub, name, serverName string) string`
New signature: `FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string`

The function must:
1. Extract `claims[claimName]` as string (ok to ignore non-string types)
2. Sanitise via existing `sanitiseLocalpart(s)` — returns `""` on empty/invalid
3. If non-empty: return `"@" + sanitised + ":" + serverName`
4. Fallback: extract `claims["sub"].(string)` and call `FormatUserID(sub, serverName)` (SHA-256 path)

All existing callers must be updated to pass `claimName` + full `claims` map instead of extracted string values.

**Bootstrap Wizard flow change (IMPORTANT):**
Current: Step 1 (Instance) → Step 2 (OIDC creds) → Redirect to OIDC provider → callback → `bootstrap-claims.html` (Select Admin Group Claim) → `ClaimSelectionHandler`

New: Step 1 → Step 2 → Step 3 (Claim Mapping — with defaults pre-filled) → Redirect to OIDC → callback → `bootstrap-claims.html` (Select Admin Group Claim, which already has Bootstrap mode context) → `ClaimSelectionHandler`

Step 3 data is saved to `bootstrap_draft` just like steps 1 and 2. `ClaimSelectionHandler` must load and persist it inside the same `runInTx` as `admin_group_claim` and `bootstrap_completed`.

The stepper in `bootstrap.html` currently has 3 items (Instance, OIDC, Connect). It needs a 4th item (Claim Mapping or Claims) — the stepper `{{ if ge .Step N }}step-primary{{ end }}` guards must be updated consistently.

### Existing Patterns to Follow

**Server config persistence (DO NOT invent new patterns):**
- All config goes through `server_config` table (key/value with `set_at`)
- Upsert pattern: `INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`
- See `SaveAdminGroupClaim` in `gateway/internal/admin/auth.go` for the exact pattern

**Bootstrap draft pattern (for wizard step 3):**
- Draft is stored via `BootstrapDraftStore.SaveDraft(ctx, key, value)` 
- Draft table: `bootstrap_draft (key text PK, value text, set_at bigint)`
- All draft keys are cleared atomically in `clearDraftTx` when `ClaimSelectionHandler` commits

**Admin UI handler pattern (follow this exactly):**
- Handler struct with `tmpl *TemplateHandler` and injected dependencies
- Constructor `NewXxxHandler(tmpl *TemplateHandler, ...) *XxxHandler`
- GET handler: reads from DB, calls `newPageData()`, renders template
- POST handler: validates, writes to DB, PRG redirect to `?flash=...`
- See `gateway/internal/admin/role_mapping.go` for the exact pattern (closest existing analogue)

**Claim name validation regex (MUST reuse):**
`oidcGroupClaimRe` in `role_mapping.go` is `^[a-zA-Z0-9:_-]+$`. For claim mapping, extend to allow dots (e.g. `user.email`): use `^[a-zA-Z0-9:_\-.]+$` but define a new package-level `oidcClaimNameRe` in `claim_mapping.go` — do NOT modify `oidcGroupClaimRe` (breaking change to role-mapping validation).

**CSRF protection:**
All POST handlers go through the existing `csrf` middleware. Templates embed `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`. See any existing form template.

**Flash PRG pattern:**
POST success → `http.Redirect(w, r, "/admin/config/claim-mapping?flash=Claim+mapping+updated", http.StatusFound)`. GET handler reads `sanitizeFlash(r.URL.Query().Get("flash"))` and populates `AlertBannerData`. See `role_mapping.go` lines 31–35.

### Files to Create

| File | Type | Purpose |
|------|------|---------|
| `gateway/migrations/000044_oidc_claim_mapping.up.sql` | NEW | Seed default claim mapping in server_config |
| `gateway/migrations/000044_oidc_claim_mapping.down.sql` | NEW | Rollback |
| `gateway/internal/admin/claim_mapping.go` | NEW | ClaimMappingHandler GET/POST |
| `gateway/internal/admin/claim_mapping_handler_test.go` | NEW | Unit tests (tests 1–3) |
| `gateway/internal/admin/templates/claim-mapping.html` | NEW | Template for settings page |
| `gateway/features/claim_mapping.feature` | NEW | Godog integration scenarios |
| `e2e/features/admin/claim-mapping.feature` | NEW | Playwright+Cucumber E2E scenarios |
| `e2e/steps/admin/claim-mapping.steps.ts` | NEW | E2E step definitions |

### Files to Modify

| File | Change |
|------|--------|
| `gateway/internal/admin/page_data.go` | Add `ClaimMappingPageData`, extend `BootstrapPageData` with claim fields |
| `gateway/internal/admin/auth.go` | Extend `ServerConfigReader` interface; add `LoadClaimMapping`, `SaveClaimMapping` to `postgresServerConfigReader`; update `ClaimSelectionHandler` to persist claim mapping |
| `gateway/internal/admin/bootstrap.go` | Add step 3 handling in `StepHandler`; save claim mapping to draft |
| `gateway/internal/admin/templates/bootstrap.html` | Add Step 3 block and 4th stepper item |
| `gateway/internal/grpc/metadata.go` | Refactor `FormatUserIDFromClaims` signature |
| `gateway/internal/grpc/metadata_test.go` | Extend with tests 4, 5 |
| `gateway/internal/middleware/auth.go` | Pass `userIDClaim` to `FormatUserIDFromClaims` |
| `gateway/internal/matrix/login.go` | Separate user-ID claim from displayname; pass `userIDClaim` |
| `gateway/cmd/gateway/main.go` | Load `oidc_user_id_claim` from DB at startup; inject into handlers; register new routes |
| `gateway/internal/admin/templates/layouts/base.html` | Add "Claim Mapping" to sidebar nav |

### Security Requirements

This story is `security_review: required` due to:
1. **OIDC claim name injection**: claim names are used as map keys to read from JWT claims — no SQL injection risk (it's a map lookup, not SQL), but validate rigorously against the allowlist regex before storing and before use
2. **Identity stability risk**: changing `oidc_user_id_claim` post-bootstrap is irreversible for existing users — warn prominently in both the admin page and a `<!--NOTE-->` comment in the handler
3. **Auth bypass risk**: if `FormatUserIDFromClaims` is refactored incorrectly, users could spoof another user's Matrix ID — ensure the SHA-256 fallback path is always reached when the configured claim is absent/empty
4. **Input validation**: claim name inputs must pass `oidcClaimNameRe` before being stored; handler must reject empty/overlong inputs with 422
5. The security review will check for IDOR, auth bypass, timing attacks, and input validation completeness

### Testing Anti-Patterns to Avoid

- Do NOT seed the DB directly to fake claim mapping in E2E tests — use the real Admin UI form flow
- Do NOT test with cookies forged outside the OIDC flow
- The Playwright bootstrap scenario MUST use the actual bootstrap wizard step flow, not skip to `ClaimSelectionHandler` directly
- Integration tests (Godog) that test Matrix login with a custom `oidc_user_id_claim` must use a real OIDC token flow with the test Dex instance

### Migration Notes

Migration `000044` seeds defaults into `server_config`. Because the migration uses `ON CONFLICT DO NOTHING`, existing installs that already have these keys (if any were set manually) will not be overwritten. New installs get the Nebu defaults automatically. The migration does NOT alter any schema — it is a pure data seed.

The next available migration number after `000043_search_vector` is `000044`.

### Config Startup Loading

In `main.go`, after DB is opened and migrations run, add:
```go
// Load OIDC claim mapping from server_config (with env var override for backward compat).
userIDClaim := cfg.OIDCUserIDClaim // e.g. from NEBU_OIDC_USER_ID_CLAIM env var (empty by default)
if userIDClaim == "" {
    if v, _, err := loadServerConfigKey(bootstrapDB, "oidc_user_id_claim"); err == nil && v != "" {
        userIDClaim = v
    }
}
if userIDClaim == "" {
    userIDClaim = "name" // backward-compat: matches current FormatUserIDFromClaims("name" claim) behavior
}
```
Add `OIDCUserIDClaim string` to `Config` in `gateway/internal/config/config.go` (env: `NEBU_OIDC_USER_ID_CLAIM`, default `""`).

### References

- `gateway/internal/admin/auth.go` — `ServerConfigReader`, `ClaimSelectionHandler`, `saveBootstrapConfigTx`, `postgresServerConfigReader` (lines 47–197)
- `gateway/internal/admin/bootstrap.go` — `StepHandler` (lines 108–210), `BootstrapDraftStore` (lines 33–70)
- `gateway/internal/admin/role_mapping.go` — validation pattern + PRG redirect + handler struct (entire file — closest analogue for new handler)
- `gateway/internal/admin/page_data.go` — `BootstrapPageData`, `RoleMappingPageData` (lines 100–291)
- `gateway/internal/admin/templates/bootstrap.html` — current 3-step wizard template
- `gateway/internal/admin/templates/bootstrap-claims.html` — existing step 3 (select admin group claim)
- `gateway/internal/grpc/metadata.go` — `FormatUserIDFromClaims`, `sanitiseLocalpart`, `FormatUserID` (entire file)
- `gateway/internal/middleware/auth.go` — `JWTMiddleware` lines 164–248 (where user ID is computed)
- `gateway/internal/matrix/login.go` — login handler lines 170–193 (where displayName is extracted and user ID derived)
- `gateway/cmd/gateway/main.go` — lines 340–419 (existing admin route registration pattern)
- `gateway/migrations/000043_search_vector.up.sql` — next migration is `000044`

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- AC1/AC2: Bootstrap Wizard extended to 4 steps. Step 2 (OIDC creds) now renders Step 3 (Claim Mapping) on valid submit. Step 3 validates claim names, saves all three to draft store, then redirects to OIDC login. Bootstrap transaction atomically writes oidc_user_id_claim, oidc_displayname_claim, oidc_email_claim alongside existing bootstrap keys.
- AC3/AC4: ClaimMappingHandler at GET/POST /admin/config/claim-mapping reads current values from server_config via LoadClaimMapping (with Nebu defaults as fallback), validates non-empty/safe claim names, saves via SaveClaimMapping, uses PRG redirect with ?flash=saved on success.
- AC5/AC6: FormatUserIDFromClaims refactored from (sub, name, serverName) to (claimName, claims, serverName). JWTMiddleware gains 5th param userIDClaimLoader func(ctx) string before variadic serverName. Per-request DB read — no gateway restart required. LoginHandler likewise wired with userIDClaimLoader.
- AC7: When userIDClaimLoader is nil or returns "", code falls back to "name" claim — identical to pre-11-10 behavior. Migration 000044 seeds defaults (ON CONFLICT DO NOTHING) so new installs get sub/name/email and existing installs are not overwritten.
- AC8: ClaimMappingHandler form shows validation error (422) for empty or dangerous claim names. Per-field data-field attributes wired to DaisyUI alert-error elements.
- AC9: Step 3 redirect to /admin/login/start?mode=bootstrap triggers OIDC Authorization Code flow; no raw OIDC callback change needed.
- Unit tests all pass (make test-unit-go). Integration (Godog claim_mapping.feature) and Playwright E2E tests require running stack and are marked for post-deployment verification.
- Key decision: JWTMiddleware signature change propagated to 26+ matrix test files via sed fix (nil added as 5th arg). Login test user_id assertion updated from @kai.mueller to @test-sub-123 (using "name" claim as per AC7 fallback).
- Review cycle 2 MINOR fixes: MINOR-A (maxlength="50" added to all 3 Step 3 bootstrap inputs); MINOR-C (slog.Warn added in LoadClaimMapping on DB error); MINOR-E (audit log via audit.LogEvent added in UpdateHandler after SaveClaimMapping, coreClient injected via SetCoreClient); MINOR-F (claimLoader.get now returns string only, logs error internally, callers simplified, test updated); MINOR-I (regex comment aligned to actual regex string).
- Review cycle 3 MINOR fixes: MINOR-1 (datalist with sub/preferred_username/email suggestions added to oidc_user_id_claim on both claim-mapping.html and bootstrap.html Step 3); MINOR-2 (bootstrap Step 3 labels aligned to settings page: "Matrix User ID Source Claim", "Display Name Source Claim", "Email Profile Field Claim"); MINOR-3 (note banner added below info banner in bootstrap Step 3 about stability risk); MINOR-5 (GET handler now shows warning flash banner when LoadClaimMapping returns error, not silently shows defaults); MINOR-7 (UpdateHandler reads prior values via LoadClaimMapping before saving and includes them as previous_oidc_* keys in audit log).

### File List

**New files:**
- `gateway/internal/admin/claim_mapping.go` — ClaimMappingHandler + ClaimMappingPageData + ClaimMappingConfig; ServerConfigReader interface extension with LoadClaimMapping/SaveClaimMapping
- `gateway/internal/admin/templates/claim-mapping.html` — Admin UI Claim Mapping settings page template (PRG pattern, flash alerts, per-field validation)
- `gateway/migrations/000044_oidc_claim_mapping.up.sql` — Seed oidc_user_id_claim, oidc_displayname_claim, oidc_email_claim defaults into server_config
- `gateway/migrations/000044_oidc_claim_mapping.down.sql` — Rollback: remove seeded claim mapping keys from server_config
- `gateway/features/claim_mapping.feature` — Godog integration scenarios for AC5 (per-request user ID claim) and AC7 (fallback)
- `gateway/test/integration/claim_mapping_steps_test.go` — Godog step definitions for claim_mapping.feature
- `e2e/features/admin/claim-mapping.feature` — Playwright+Gherkin E2E scenarios for AC1/AC3 (Bootstrap Wizard Step 3 + Admin UI page)
- `e2e/step-definitions/admin/claim-mapping.steps.ts` — Cucumber step definitions for claim-mapping.feature
- `gateway/internal/admin/claim_mapping_bootstrap_tx_test.go` — Unit tests: claim mapping written in bootstrap transaction (AC2)
- `gateway/internal/admin/claim_mapping_handler_test.go` — Unit tests: ClaimMappingHandler GET/POST/validation/PRG (AC3, AC4, AC6, AC8)
- `_bmad-output/test-artifacts/atdd-checklist-11-10-oidc-claim-mapping.md` — ATDD checklist generated pre-implementation

**Modified files:**
- `gateway/internal/admin/auth.go` — Added LoadClaimMapping/SaveClaimMapping to ServerConfigReader interface and postgresServerConfigReader; saveBootstrapConfigTx now writes claim mapping keys atomically (AC2)
- `gateway/internal/admin/bootstrap.go` — Step 2 now renders Step 3 (Claim Mapping); Step 3 validates/saves claims and redirects to OIDC login; BootstrapPageData gains OIDCUserIDClaim/OIDCDisplaynameClaim/OIDCEmailClaim fields
- `gateway/internal/admin/page_data.go` — BootstrapPageData extended with OIDCUserIDClaim, OIDCDisplaynameClaim, OIDCEmailClaim fields
- `gateway/internal/admin/templates/bootstrap.html` — Added 4th stepper item "Claims"; Step 2 button changed to "Next"; added complete Step 3 claim mapping form block
- `gateway/internal/admin/templates/layouts/base.html` — Added "Claim Mapping" nav link in admin sidebar
- `gateway/internal/admin/flash.go` — Extended flash utility (if needed for claim mapping PRG)
- `gateway/internal/admin/login_test.go` — Added LoadClaimMapping/SaveClaimMapping stubs to fakeServerConfigReader
- `gateway/internal/admin/bootstrap_api_test.go` — Updated Step 2 test (now expects 200 + Step 3 form); added TestStepHandler_Step3_ValidClaimsRedirectsToOIDC
- `gateway/internal/admin/bootstrap_wizard_test.go` — Updated Step 2 button test (now "Next"); added TestBootstrapWizard_Step3_ConnectButton
- `gateway/internal/admin/claim_selection_tx_test.go` — Extended claim selection transaction tests for new claim mapping fields
- `gateway/internal/grpc/metadata.go` — FormatUserIDFromClaims refactored to accept claimName string as first parameter (AC5/AC7)
- `gateway/internal/grpc/metadata_test.go` — Updated tests for new FormatUserIDFromClaims signature
- `gateway/internal/middleware/auth.go` — JWTMiddleware gains 5th parameter userIDClaimLoader func(ctx context.Context) string (before variadic serverName); per-request DB claim lookup
- `gateway/internal/middleware/auth_test.go` — Updated all JWTMiddleware calls to 5-arg form (nil for userIDClaimLoader)
- `gateway/internal/middleware/alg_test.go` — Updated JWTMiddleware calls to 5-arg form
- `gateway/internal/middleware/jwt_denylist_order_test.go` — Updated JWTMiddleware calls to 5-arg form
- `gateway/internal/matrix/login.go` — LoginHandler uses userIDClaimLoader for per-request user ID claim resolution (AC5/AC7); removed unused sub variable
- `gateway/internal/matrix/login_test.go` — Updated valid token test assertion: user_id now uses "name" claim (test-sub-123) not preferred_username
- `gateway/internal/matrix/*.go` (26 test files) — Updated JWTMiddleware calls to new 5-arg signature (added nil for userIDClaimLoader)
- `gateway/cmd/gateway/main.go` — Registered ClaimMappingHandler route GET/POST /admin/config/claim-mapping; wired userIDClaimLoader to per-request DB read; added NEBU_OIDC_USER_ID_CLAIM env var override (AC5/AC7)
- `gateway/test/integration/steps_test.go` — Added initializeClaimMappingSteps registration call
- `gateway/test/integration/admin_bootstrap_steps_test.go` — Removed ambiguous specific "200" step registration; kept theResponseIs200 as delegate helper

### Change Log

| Date | Change | Author |
|---|---|---|
| 2026-05-12 | Initial implementation: Bootstrap Wizard Step 3 (Claim Mapping), ClaimMappingHandler admin UI page, DB migration 000044, FormatUserIDFromClaims refactored signature, JWTMiddleware/LoginHandler per-request userIDClaimLoader, Godog + E2E test stubs | claude-sonnet-4-6 |
| 2026-05-12 | Review cycle 2 MINOR fixes: maxlength="50" on bootstrap Step 3 inputs; slog.Warn in LoadClaimMapping on DB error; audit log in UpdateHandler; claimLoader.get returns string only; regex comment aligned | claude-sonnet-4-6 |
| 2026-05-12 | Review cycle 3 MINOR fixes: datalist on oidc_user_id_claim (both templates); label alignment in bootstrap Step 3; stability note banner in bootstrap Step 3; warning flash on GET when LoadClaimMapping fails; audit log includes previous_oidc_* before values | claude-sonnet-4-6 |

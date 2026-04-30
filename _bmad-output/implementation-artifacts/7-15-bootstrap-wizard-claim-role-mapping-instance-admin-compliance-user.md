---
id: 7-15
security_review: required
---

# Story 7.15: Bootstrap Wizard — Claim-to-Role Mapping (instance_admin / compliance_user)

Status: ready-for-dev

## Story

As an operator setting up Nebu for the first time,
I want to configure which OIDC claim and claim values map to each Nebu role from the Admin UI,
so that I can use my organisation's existing group names without hardcoding them in environment variables.

## Context / Background

The Bootstrap Wizard (Stories 3-7/3-8, extended in 5-16) currently only captures `admin_group_claim` — the single claim value that grants `instance_admin` — via the post-OIDC claim-selection screen (`bootstrap-claims.html`). The claim name itself (`groups`, `roles`, etc.) is env-var only (`NEBU_OIDC_CLAIM_ROLE`, default `"nebu_role"`). There is no UI to configure the `compliance_user` group value at all.

This story adds a standalone admin page at `GET /admin/config/role-mapping` where an already-bootstrapped instance admin can view and update the role-claim mapping configuration. It is NOT a Bootstrap Wizard step — it is a regular post-bootstrap config page accessible from the Admin navigation under "Configuration". The Bootstrap Wizard itself already completes via the claim-selection screen; this story fills the gap for day-to-day reconfiguration.

**Scope guard:** This story only wires the UI for reading/writing stub config. The OIDC middleware (`middleware/auth.go`) still reads `cfg.OIDCClaimRole` from env vars at runtime — making the middleware read from `stubRoleMappingConfig` instead is a separate security-gated story. This story delivers the UI and stub data layer only.

**Existing relevant files:**
- `gateway/internal/admin/stubs.go` — add `StubRoleMappingConfig` struct + `stubRoleMappingConfig` var
- `gateway/internal/admin/page_data.go` — add `RoleMappingPageData` struct
- `gateway/internal/admin/config.go` — add `RoleMappingHandler` (new handler, analogous to `ConfigHandler`)
- `gateway/internal/admin/templates/role-mapping.html` — new template
- `gateway/internal/admin/templates/layouts/base.html` — add "Role Mapping" nav item under "Configuration"
- `gateway/cmd/gateway/main.go` — register two routes (`GET` + `POST /admin/config/role-mapping`)
- `gateway/internal/admin/config_test.go` or a new `role_mapping_test.go` — unit tests
- `e2e/tests/features/admin/role-mapping.spec.ts` — Playwright E2E tests (new file)

## Acceptance Criteria

1. **`StubRoleMappingConfig` struct and `stubRoleMappingConfig` var in `stubs.go`** — A new struct is added:
   ```go
   type StubRoleMappingConfig struct {
       OIDCGroupClaim       string // claim name, e.g. "groups"
       InstanceAdminGroup   string // value that maps to instance_admin, e.g. "instance_admin"
       ComplianceUserGroup  string // value that maps to compliance_user, e.g. "" (optional)
   }
   var stubRoleMappingConfig = StubRoleMappingConfig{
       OIDCGroupClaim:      "groups",
       InstanceAdminGroup:  "instance_admin",
       ComplianceUserGroup: "",
   }
   ```

2. **`RoleMappingPageData` struct in `page_data.go`**:
   ```go
   type RoleMappingPageData struct {
       PageData
       Config StubRoleMappingConfig
       Errors map[string]string
       Flash  AlertBannerData
   }
   ```

3. **`GET /admin/config/role-mapping` renders correctly** — `RoleMappingHandler.Handler` renders `role-mapping.html` with:
   - HTTP 200
   - Body contains `<h1` and "Role Mapping"
   - Form pre-filled with current `stubRoleMappingConfig` values
   - `?flash=` query param populates `AlertBannerData{Severity: "success", Dismissible: true}` and body contains the flash message

4. **`role-mapping.html` template** — `gateway/internal/admin/templates/role-mapping.html`:
   - `{{ template "base" . }}` / `{{ define "title" }}Role Mapping — Nebu Admin{{ end }}`
   - `{{ define "content" }}` with `<h1>Role Mapping</h1>`
   - `{{ if .Flash.Message }}{{ template "alert_banner" .Flash }}{{ end }}`
   - `<form method="POST" action="/admin/config/role-mapping">` containing:
     - Hidden CSRF: `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`
     - Field `oidc_group_claim`: `<input type="text" name="oidc_group_claim" maxlength="50" required>` pre-filled with `{{ .Config.OIDCGroupClaim }}`, error message if `.Errors["oidc_group_claim"]` is non-empty
     - Field `instance_admin_group`: `<input type="text" name="instance_admin_group" maxlength="100" required>` pre-filled with `{{ .Config.InstanceAdminGroup }}`, error message if `.Errors["instance_admin_group"]` is non-empty
     - Field `compliance_user_group`: `<input type="text" name="compliance_user_group" maxlength="100">` pre-filled with `{{ .Config.ComplianceUserGroup }}` (optional — no `required`)
     - Submit: `<button type="submit" class="btn btn-primary">Save</button>`

5. **`POST /admin/config/role-mapping` validation** — `RoleMappingHandler.UpdateHandler`:
   - Parses form (`r.ParseForm()`)
   - Validates `oidc_group_claim`: non-empty after `strings.TrimSpace`, max 50 runes, must match `^[a-zA-Z0-9:_-]+$` (no spaces or unsupported characters)
   - Validates `instance_admin_group`: non-empty after `strings.TrimSpace`, max 100 runes
   - Validates `compliance_user_group`: optional; if non-empty after `strings.TrimSpace`, max 100 runes
   - On validation failure: re-render `role-mapping.html` with HTTP 422, `Errors` map populated, form values preserved
   - On validation pass: update `stubRoleMappingConfig` in-memory, PRG redirect to `/admin/config/role-mapping?flash=Role+mapping+saved`
   - Add `// TODO(epic-6): replace stub mutation with Admin API call` comment

6. **Admin sidebar navigation** — `base.html` gains a "Role Mapping" nav link in the non-bootstrap nav block:
   ```html
   <li>
     <a href="/admin/config/role-mapping"
        data-navkey="role-mapping"
        {{ if eq .ActiveNav "role-mapping" }}aria-current="page"{{ end }}
        class="...{{ if eq .ActiveNav "role-mapping" }}active bg-primary text-primary-content font-semibold{{ else }}font-medium text-base-content hover:bg-base-300{{ end }}...">
       Role Mapping
     </a>
   </li>
   ```
   Place it directly after the "Configuration" nav item and before the "Logout" button.

7. **Routing in `main.go`** — following the exact pattern of `GET /admin/config` / `POST /admin/config`:
   ```go
   roleMappingHandler := admin.NewRoleMappingHandler(tmplHandler)
   mux.Handle("GET /admin/config/role-mapping", csrf(sessionGuard(http.HandlerFunc(roleMappingHandler.Handler))))
   // POST — no csrf() wrapper (stub phase); sessionGuard still applies.
   mux.Handle("POST /admin/config/role-mapping", sessionGuard(http.HandlerFunc(roleMappingHandler.UpdateHandler)))
   ```

8. **Go unit tests** — `gateway/internal/admin/role_mapping_test.go` (`package admin`):
   - `TestRoleMappingPageRenders` — `GET /admin/config/role-mapping` → HTTP 200, body contains `<h1`, body contains "Role Mapping", input `oidc_group_claim` has value "groups"
   - `TestRoleMappingPageFlash` — `GET /admin/config/role-mapping?flash=Role+mapping+saved` → HTTP 200, body contains "Role mapping saved"
   - `TestUpdateRoleMapping` — valid POST → HTTP 302, `Location` contains `/admin/config/role-mapping` and `flash=`; `stubRoleMappingConfig` updated
   - `TestUpdateRoleMappingEmptyClaimName` — POST with `oidc_group_claim=` → HTTP 422, body contains error text for `oidc_group_claim`
   - `TestUpdateRoleMappingInvalidClaimName` — POST with `oidc_group_claim=my group` (contains space) → HTTP 422, body contains error for `oidc_group_claim`
   - `TestUpdateRoleMappingEmptyAdminGroup` — POST with `instance_admin_group=` → HTTP 422, body contains error for `instance_admin_group`
   - `TestUpdateRoleMappingOptionalComplianceGroup` — POST with valid `oidc_group_claim`, valid `instance_admin_group`, empty `compliance_user_group` → HTTP 302 (optional field is allowed empty)
   - `TestUpdateRoleMappingComplianceGroupTooLong` — POST with `compliance_user_group` > 100 runes → HTTP 422, body contains error for `compliance_user_group`

9. **Playwright E2E tests** — `e2e/tests/features/admin/role-mapping.spec.ts` — **REAL tests (not `test.skip`)**:
   - `role mapping page renders with defaults` — login, navigate to `/admin/config/role-mapping`, expect `<h1>` to contain "Role Mapping", expect `input[name="oidc_group_claim"]` to have value "groups", expect `input[name="instance_admin_group"]` to have value "instance_admin"
   - `save valid role mapping shows flash message` — login, navigate to `/admin/config/role-mapping`, fill `oidc_group_claim` with "roles", fill `instance_admin_group` with "admins", click Save → `div[role="alert"]` contains "Role mapping saved"
   - `invalid claim name shows validation error` — login, navigate to `/admin/config/role-mapping`, clear `oidc_group_claim` and fill with "my group" (space), click Save → page still at `/admin/config/role-mapping` (no redirect), body contains error text for that field
   - `nav link Role Mapping is present and active when on page` — login, navigate to `/admin/config/role-mapping`, expect sidebar `a[data-navkey="role-mapping"]` to have `aria-current="page"`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestRoleMappingPageRenders` — Go `net/http/httptest`
   - Given: `RoleMappingHandler` created with a real `TemplateHandler`
   - When: `GET /admin/config/role-mapping` is called (with CSRF cookie set)
   - Then: HTTP 200, body contains "Role Mapping" and `input[name="oidc_group_claim"]` value "groups"

2. `TestUpdateRoleMapping` — Go `net/http/httptest`
   - Given: `RoleMappingHandler` with `stubRoleMappingConfig` at defaults
   - When: `POST /admin/config/role-mapping` with `oidc_group_claim=cognito:groups&instance_admin_group=admins&compliance_user_group=`
   - Then: HTTP 302 to `/admin/config/role-mapping?flash=Role+mapping+saved`, `stubRoleMappingConfig.OIDCGroupClaim == "cognito:groups"`

3. `TestUpdateRoleMappingEmptyClaimName` — Go `net/http/httptest`
   - Given: `RoleMappingHandler`
   - When: POST with `oidc_group_claim=`
   - Then: HTTP 422, body contains error text for `oidc_group_claim`

4. `TestUpdateRoleMappingInvalidClaimName` — Go `net/http/httptest`
   - Given: `RoleMappingHandler`
   - When: POST with `oidc_group_claim=my group` (space)
   - Then: HTTP 422, body contains error text for `oidc_group_claim`

5. `TestUpdateRoleMappingEmptyAdminGroup` — Go `net/http/httptest`
   - Given: `RoleMappingHandler`
   - When: POST with valid `oidc_group_claim` and `instance_admin_group=`
   - Then: HTTP 422, body contains error text for `instance_admin_group`

6. `TestUpdateRoleMappingOptionalComplianceGroup` — Go `net/http/httptest`
   - Given: `RoleMappingHandler`
   - When: POST with valid `oidc_group_claim`, valid `instance_admin_group`, empty `compliance_user_group`
   - Then: HTTP 302 (optional field accepted empty)

7. `TestUpdateRoleMappingComplianceGroupTooLong` — Go `net/http/httptest`
   - Given: `RoleMappingHandler`
   - When: POST with `compliance_user_group` of 101 runes
   - Then: HTTP 422, body contains error text for `compliance_user_group`

8. `role mapping page renders with defaults` — Playwright E2E
   - Given: Dev stack running, bootstrap complete, admin logged in
   - When: navigate to `/admin/config/role-mapping`
   - Then: `h1` contains "Role Mapping"; `input[name="oidc_group_claim"]` value "groups"; `a[data-navkey="role-mapping"]` has `aria-current="page"`

9. `save valid role mapping shows flash message` — Playwright E2E
   - Given: logged-in admin on `/admin/config/role-mapping`
   - When: fill `oidc_group_claim` → "roles", fill `instance_admin_group` → "admins", click Save
   - Then: `div[role="alert"]` contains "Role mapping saved"

10. `invalid claim name shows validation error` — Playwright E2E
    - Given: logged-in admin on `/admin/config/role-mapping`
    - When: fill `oidc_group_claim` → "my group", click Save
    - Then: URL remains `/admin/config/role-mapping` (no redirect); page contains validation error text

## Tasks

### Task 1 — Stub data layer (write tests first)

1. Write `gateway/internal/admin/role_mapping_test.go` with all 7 unit tests — all fail initially (no implementation).
2. Add `StubRoleMappingConfig` struct and `stubRoleMappingConfig` var to `gateway/internal/admin/stubs.go`.
3. Add `RoleMappingPageData` struct to `gateway/internal/admin/page_data.go`.

### Task 2 — Handler + template

4. Create `gateway/internal/admin/role_mapping.go` with `RoleMappingHandler`, `NewRoleMappingHandler`, `Handler` (GET), and `UpdateHandler` (POST).
   - `Handler`: read `?flash=`, populate `RoleMappingPageData` from `stubRoleMappingConfig`, call `h.tmpl.render(w, "role-mapping", data)`.
   - `UpdateHandler`: parse form, validate (trimmed non-empty + regex for claim name, max lengths), on error re-render 422, on success mutate `stubRoleMappingConfig`, PRG redirect.
5. Create `gateway/internal/admin/templates/role-mapping.html` with the form structure described in AC4.
6. Run unit tests — all 7 should now pass green.

### Task 3 — Navigation + routing

7. Edit `gateway/internal/admin/templates/layouts/base.html`: add "Role Mapping" nav link after the "Configuration" `<li>` item (before Logout).
8. Edit `gateway/cmd/gateway/main.go`: add `roleMappingHandler` instantiation and two route registrations after the config handler block (around line 331).

### Task 4 — Playwright E2E tests

9. Write `e2e/tests/features/admin/role-mapping.spec.ts` with the 4 E2E tests described in AC9.
   - Re-use the `loginAsAdmin` helper pattern from `config.spec.ts`.
   - Use `test.afterEach` to restore `stubRoleMappingConfig` defaults if the "save valid role mapping" test mutated them (navigate to page and POST the original values back, or use a second POST to reset — see smoke-flows.spec.ts pattern).

### Task 5 — Final verification

10. Run `make test-unit-go` — all tests green.
11. Manually verify (or via `make dev` + Playwright): navigate sidebar "Role Mapping", save form, flash appears, validation errors surface correctly.

## Dev Notes

### Validation Regex

Use `regexp.MustCompile(`^[a-zA-Z0-9:_-]+$`)` for `oidc_group_claim` validation. This regex is compiled at package level (not inside the handler function) to match the existing `instanceNameRe` pattern in `bootstrap.go`.

Example valid values: `"groups"`, `"cognito:groups"`, `"my_groups"`, `"ROLES-CLAIM"`.
Example invalid: `"my group"` (space), `"claim.name"` (dot — not in allowlist), `""` (empty).

### Handler Pattern — Mirror `ConfigHandler` Exactly

`RoleMappingHandler` should be a thin struct:
```go
type RoleMappingHandler struct {
    tmpl *TemplateHandler
}

func NewRoleMappingHandler(tmpl *TemplateHandler) *RoleMappingHandler {
    return &RoleMappingHandler{tmpl: tmpl}
}
```

`UpdateHandler` reuses the `strings.TrimSpace` + `utf8.RuneCountInString` pattern for length checks.
Use `utf8.RuneCountInString` (not `len()`) for rune-based length checks on claim values that may include multi-byte characters.

### Template Key

The template key passed to `h.tmpl.render(w, "role-mapping", data)` must match the filename without extension. The file is `templates/role-mapping.html`, so the key is `"role-mapping"`. The `NewTemplateHandler` auto-discovers all `.html` files under `templates/` and uses the base filename without extension as the key — no registration change needed.

### Page Data Field `Errors`

`RoleMappingPageData.Errors` is `map[string]string`. Initialize it in `UpdateHandler` with `make(map[string]string)` before adding entries. Render errors in the template with:
```html
{{ if index .Errors "oidc_group_claim" }}
<p class="text-error text-sm">{{ index .Errors "oidc_group_claim" }}</p>
{{ end }}
```

### ActiveNav Key

Set `ActiveNav: "role-mapping"` in `RoleMappingPageData.PageData` so the sidebar highlights correctly. This matches the `data-navkey="role-mapping"` attribute in `base.html`.

### CSRF TODO Comment

In `UpdateHandler`, add:
```go
// TODO(story-7-csrf): enforce CSRF middleware when wiring in production.
```
This is consistent with the pattern in `ConfigHandler.UpdateConfigHandler` and `RoomsHandler`.

### Route Registration Location

Add the two new routes in `main.go` immediately after the existing `POST /admin/config` registration:
```go
// Story 7.15: Role Mapping configuration page.
roleMappingHandler := admin.NewRoleMappingHandler(tmplHandler)
mux.Handle("GET /admin/config/role-mapping", csrf(sessionGuard(http.HandlerFunc(roleMappingHandler.Handler))))
// POST — no csrf() wrapper (stub phase); sessionGuard still applies.
mux.Handle("POST /admin/config/role-mapping", sessionGuard(http.HandlerFunc(roleMappingHandler.UpdateHandler)))
```

### Test Pattern — Unit Tests

Unit tests follow `TestConfigPageRenders` in `config_test.go`. Key details:
- Build a real `TemplateHandler` using `admin.NewTemplateHandler()` — this compiles all templates and catches template errors early.
- Use `httptest.NewRecorder()` and `httptest.NewRequest("GET", "/admin/config/role-mapping", nil)`.
- For POST tests: `httptest.NewRequest("POST", "/admin/config/role-mapping", strings.NewReader(url.Values{...}.Encode()))` with `Content-Type: application/x-www-form-urlencoded`.
- No CSRF enforcement in unit tests (the handler does not call CSRFMiddleware directly).
- After a mutating POST test, reset `stubRoleMappingConfig` in `t.Cleanup(func() { stubRoleMappingConfig = StubRoleMappingConfig{...} })` to avoid test order dependency.

### Playwright Test Cleanup

The Playwright "save valid role mapping shows flash message" test changes `stubRoleMappingConfig` in-memory. Use `test.afterEach` to POST the defaults back:
```typescript
test.afterEach(async ({ page }) => {
  // Reset stubRoleMappingConfig to defaults via POST
  await page.goto('/admin/config/role-mapping');
  await page.locator('input[name="oidc_group_claim"]').fill('groups');
  await page.locator('input[name="instance_admin_group"]').fill('instance_admin');
  await page.locator('input[name="compliance_user_group"]').fill('');
  await page.locator('button[type="submit"]').click();
  await page.waitForURL(/role-mapping/, { timeout: 5_000 });
});
```

### No OIDC Middleware Changes

Do NOT modify `gateway/internal/auth/roles.go`, `gateway/internal/middleware/auth.go`, or `gateway/internal/config/config.go`. The stub values written here are for UI display only; the actual OIDC claim resolution at runtime still uses `cfg.OIDCClaimRole` from env vars. The `server_config` integration is out of scope for this story.

### Sidebar Nav Placement

In `base.html`, the sidebar nav currently ends with: Configuration → Logout. Insert "Role Mapping" between them:

```html
<!-- After Configuration li -->
<li>
  <a href="/admin/config/role-mapping"
     data-navkey="role-mapping"
     {{ if eq .ActiveNav "role-mapping" }}aria-current="page"{{ end }}
     class="flex items-center gap-2 px-3 py-3 rounded text-sm
            {{ if eq .ActiveNav "role-mapping" }}active bg-primary text-primary-content font-semibold{{ else }}font-medium text-base-content hover:bg-base-300{{ end }}
            focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-base-200">
    Role Mapping
  </a>
</li>
<!-- Logout button follows -->
```

### Security Note (Why `security_review: required`)

This page allows writing values that control which OIDC claim name and values are used for role assignment. Even though the middleware does not yet read these stub values (out of scope), the page accepts free-text input that will eventually influence auth decisions. The security reviewer must check: input length enforcement, no XSS in template rendering of user-supplied values, no SSRF (this handler makes no HTTP calls), validation regex is strict enough to prevent injection into future DB queries.

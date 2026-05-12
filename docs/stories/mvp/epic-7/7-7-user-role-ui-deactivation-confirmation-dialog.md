---
id: 7-7
security_review: not-needed
---

# Story 7.7: User Role UI + Deactivation Confirmation Dialog

Status: ready-for-dev

## Story

As an instance admin,
I want to change a user's role and deactivate/reactivate users through the UI with a confirmation dialog,
so that I cannot accidentally perform destructive actions with a misclick.

## Acceptance Criteria

1. **Role `<select>` rendered in detail panel** — `GET /admin/users/{userId}` body contains a `<select>` element populated with options `instance_admin`, `compliance_officer`, `user`; the option matching `ActiveUser.Role` is `selected`.

2. **`POST /admin/users/{userId}/role` handler** — `UpdateRoleHandler` in `gateway/internal/admin/users.go`:
   - Reads `role` from form body (`r.FormValue("role")`)
   - Validates: value must be one of `instance_admin`, `compliance_officer`, `user`; returns HTTP 400 on invalid value
   - On valid: mutates `stubUsers[i].Role` in-memory; HTTP 302 redirect to `/admin/users/{userId}?flash=Role+updated`
   - Add `// TODO(epic-6): replace stub mutation with Admin API call` comment

3. **ConfirmDialog rendered in detail panel** — `GET /admin/users/{userId}` body contains `role="alertdialog"` (from `confirm_dialog` component). The trigger button in `{{ define "detail_footer" }}` calls `onclick="confirm_dialog.showModal()"` (replaces the inert stub button from Story 7.6).

4. **`POST /admin/users/{userId}/deactivate` handler** — `DeactivateUserHandler` in `gateway/internal/admin/users.go`:
   - Sets `stubUsers[i].Status = "deactivated"` for the matching user
   - HTTP 302 redirect to `/admin/users/{userId}?flash=User+deactivated`
   - No form-body parsing required beyond identifying `userId` from the path (the confirm dialog's form only POSTs to this URL)

5. **`UsersPageData` additions** in `gateway/internal/admin/page_data.go`:
   - `ActiveUserConfirmDialog ConfirmDialogData` — populated by `DetailHandler` when `ActiveUser != nil`
   - `ActiveUserRoleOptions []string` — always `[]string{"instance_admin", "compliance_officer", "user"}`
   - `ActiveUserRoleValue string` — equals `ActiveUser.Role` (pre-computed to avoid template logic)

6. **Routing** in `gateway/cmd/gateway/main.go`:
   - `POST /admin/users/{userId}/role` → `sessionGuard(http.HandlerFunc(usersHandler.UpdateRoleHandler))` (no `csrf()` wrapper — same pattern as `display-name`)
   - `POST /admin/users/{userId}/deactivate` → `sessionGuard(http.HandlerFunc(usersHandler.DeactivateUserHandler))`

7. **Go unit tests** in `gateway/internal/admin/users_role_test.go` (`package admin`):
   - `TestRoleSelectRenders` — GET `/admin/users/usr-001` → 200, body contains `<select` and the user's current role as `selected`
   - `TestUpdateRole` — POST `/admin/users/usr-001/role` with `role=user` → 302, `Location` contains `flash=`
   - `TestUpdateRoleInvalid` — POST with `role=hacker` → 400
   - `TestDeactivateUser` — POST `/admin/users/usr-001/deactivate` → 302, `Location` contains `flash=`; `stubUsers` entry for `usr-001` has `Status == "deactivated"` after the call
   - `TestConfirmDialogRendered` — GET `/admin/users/usr-001` → 200, body contains `role="alertdialog"`

8. **Playwright E2E** in `e2e/tests/features/admin/user-role.spec.ts` (all REAL, no `test.skip`):
   - `role select is rendered for current user role`
   - `deactivate button opens confirmation dialog`
   - `confirm deactivation redirects with flash message`
   - Auth: `loginAsAdmin` (OIDC Authorization Code + PKCE — identical pattern to `user-detail.spec.ts`)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestRoleSelectRenders — GET /admin/users/usr-001 renders role `<select>`** — Go `net/http/httptest` (`gateway/internal/admin/users_role_test.go`)
   - Given: `NewTemplateHandler()` + `NewUsersHandler(tmpl)`; `stubUsers` contains `usr-001` (Alice Müller, role `instance_admin`)
   - When: `GET /admin/users/usr-001`
   - Then: HTTP 200, body contains `<select`, body contains `selected` near `instance_admin`

2. **TestUpdateRole — valid POST /role returns 302 with flash** — Go `net/http/httptest`
   - Given: `UpdateRoleHandler` wired to `POST /admin/users/{userId}/role`
   - When: `POST /admin/users/usr-001/role` with `role=user`
   - Then: HTTP 302, `Location` header contains `/admin/users/usr-001` and `flash=`

3. **TestUpdateRoleInvalid — invalid role value returns 400** — Go `net/http/httptest`
   - Given: `UpdateRoleHandler` wired
   - When: `POST /admin/users/usr-001/role` with `role=hacker`
   - Then: HTTP 400

4. **TestDeactivateUser — POST /deactivate returns 302 and mutates stub** — Go `net/http/httptest`
   - Given: `DeactivateUserHandler` wired; `stubUsers` has `usr-003` with `Status: "active"`
   - When: `POST /admin/users/usr-003/deactivate` (use usr-003 to avoid collisions with display-name tests that modify usr-001)
   - Then: HTTP 302, `Location` contains `flash=`; after handler returns, `findStubUser("usr-003").Status == "deactivated"`
   - Cleanup: `t.Cleanup` resets `stubUsers[2].Status = "active"` to restore state for other tests

5. **TestConfirmDialogRendered — GET /admin/users/usr-001 renders confirm_dialog component** — Go `net/http/httptest`
   - Given: `DetailHandler` wired
   - When: `GET /admin/users/usr-001`
   - Then: HTTP 200, body contains `role="alertdialog"`

6. **Playwright: role select is rendered for current user role** — Playwright (`e2e/tests/features/admin/user-role.spec.ts`)
   - Given: full dev stack running, admin logged in via OIDC
   - When: navigate to `/admin/users/usr-001`
   - Then: `select` element visible in the detail panel containing the user's role as selected option

7. **Playwright: deactivate button opens confirmation dialog** — Playwright
   - Given: admin on `/admin/users/usr-001` (active user)
   - When: click the Deactivate button (the `btn-warning` trigger in the detail footer)
   - Then: `dialog[role="alertdialog"]` is visible

8. **Playwright: confirm deactivation redirects with flash message** — Playwright
   - Given: admin on `/admin/users/usr-001`, ConfirmDialog open
   - When: click the Deactivate confirm button inside the dialog
   - Then: page redirects to `/admin/users/usr-001?flash=...`; `div[role="alert"]` visible

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 7
  - [ ] 1.1 Create `gateway/internal/admin/users_role_test.go` in `package admin`
  - [ ] 1.2 Write `TestRoleSelectRenders`:
    - Setup: `NewTemplateHandler()` + `NewUsersHandler(tmpl)`; wire `GET /admin/users/{userId}` to `DetailHandler` via `http.NewServeMux()`
    - Request: `GET /admin/users/usr-001`
    - Assert: `w.Code == 200`, `strings.Contains(body, "<select")`, `strings.Contains(body, "selected")`
  - [ ] 1.3 Write `TestUpdateRole`:
    - Setup: wire `POST /admin/users/{userId}/role` to `UpdateRoleHandler` (which does not exist yet)
    - Request: `POST` with form body `role=user`; `Content-Type: application/x-www-form-urlencoded`
    - Assert: `w.Code == 302`, `Location` contains `/admin/users/usr-001` and `flash=`
  - [ ] 1.4 Write `TestUpdateRoleInvalid`:
    - Request: `POST` with `role=hacker`
    - Assert: `w.Code == 400`
  - [ ] 1.5 Write `TestDeactivateUser`:
    - Setup: wire `POST /admin/users/{userId}/deactivate` to `DeactivateUserHandler` (which does not exist yet)
    - Use `usr-003` (Carla Reiter, Status "active") — avoids mutation interference with display-name tests on `usr-001`
    - Request: `POST /admin/users/usr-003/deactivate` (no body needed)
    - Assert: `w.Code == 302`, `Location` contains `flash=`
    - After handler: `if findStubUser("usr-003").Status != "deactivated" { t.Error(...) }`
    - Cleanup: `t.Cleanup(func() { stubUsers[2].Status = "active" })` (stubUsers[2] = Carla Reiter)
  - [ ] 1.6 Write `TestConfirmDialogRendered`:
    - Request: `GET /admin/users/usr-001`
    - Assert: `w.Code == 200`, `strings.Contains(body, `role="alertdialog"`)`
  - [ ] 1.7 Confirm RED: `go test ./internal/admin/...` fails — `UpdateRoleHandler` and `DeactivateUserHandler` do not exist; `<select>` not in template; `role="alertdialog"` not rendered

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 8
  - [ ] 2.1 Create `e2e/tests/features/admin/user-role.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `user-detail.spec.ts` verbatim (identical OIDC Authorization Code + PKCE pattern)
  - [ ] 2.3 Write `test('role select is rendered for current user role')` — NOT `test.skip`:
    ```typescript
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');
    const roleSelect = page.locator('section[role="region"] select');
    await expect(roleSelect).toBeVisible();
    // The selected option should reflect the user's current role
    await expect(roleSelect).toHaveValue('instance_admin');
    ```
  - [ ] 2.4 Write `test('deactivate button opens confirmation dialog')`:
    ```typescript
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');
    // Trigger button in detail_footer
    await page.locator('button:has-text("Deactivate")').click();
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
    ```
  - [ ] 2.5 Write `test('confirm deactivation redirects with flash message')`:
    ```typescript
    await loginAsAdmin(page);
    await page.goto('/admin/users/usr-001');
    await page.locator('button:has-text("Deactivate")').click();
    await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible();
    // Click the Deactivate confirm button inside the dialog
    await page.locator('dialog[role="alertdialog"] button[type="submit"]').click();
    await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 });
    await expect(page.locator('div[role="alert"]')).toBeVisible();
    ```
  - [ ] 2.6 Confirm RED: tests fail — no `<select>`, no `role="alertdialog"`, no working POST handlers

- [ ] Task 3: Add new fields to `UsersPageData` — AC: 5
  - [ ] 3.1 In `gateway/internal/admin/page_data.go`, add to `UsersPageData`:
    ```go
    // ActiveUserConfirmDialog is populated by DetailHandler for the confirm_dialog component (Story 7.7).
    // Only meaningful when ActiveUser != nil and ActiveUser.Status == "active".
    ActiveUserConfirmDialog ConfirmDialogData
    // ActiveUserRoleOptions lists the valid role values for the role <select> (Story 7.7).
    ActiveUserRoleOptions []string
    // ActiveUserRoleValue holds the current role for the pre-selected <option> (Story 7.7).
    ActiveUserRoleValue string
    ```
  - [ ] 3.2 Verify existing tests compile — all new fields are zero-valued in list-mode renders; no existing test files need changes

- [ ] Task 4: Extend `DetailHandler`, add `UpdateRoleHandler` and `DeactivateUserHandler` in `users.go` — AC: 1, 2, 4
  - [ ] 4.1 Extend `DetailHandler` to populate the three new `UsersPageData` fields when `user != nil`:
    ```go
    confirmDialog := ConfirmDialogData{
        Title:        "Deactivate user",
        Message:      "This will immediately invalidate all active sessions for " + user.DisplayName + ". Are you sure?",
        ConfirmLabel: "Deactivate",
        ConfirmClass: "btn-error",
        FormAction:   "/admin/users/" + userID + "/deactivate",
        HiddenFields: nil,
        CSRFToken:    csrfToken, // already computed above
    }
    // Populate data struct additions:
    // ActiveUserConfirmDialog: confirmDialog,
    // ActiveUserRoleOptions: []string{"instance_admin", "compliance_officer", "user"},
    // ActiveUserRoleValue: user.Role,
    ```
  - [ ] 4.2 Add `UpdateRoleHandler`:
    ```go
    // UpdateRoleHandler handles POST /admin/users/{userId}/role.
    // Validates and updates the user's role in-memory (stub phase).
    // TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
    func (h *UsersHandler) UpdateRoleHandler(w http.ResponseWriter, r *http.Request) {
        userID := r.PathValue("userId")
        if err := r.ParseForm(); err != nil {
            http.Error(w, "bad request", http.StatusBadRequest)
            return
        }
        role := r.FormValue("role")
        validRoles := map[string]bool{"instance_admin": true, "compliance_officer": true, "user": true}
        if !validRoles[role] {
            http.Error(w, "invalid role value", http.StatusBadRequest)
            return
        }
        for i := range stubUsers {
            if stubUsers[i].ID == userID {
                stubUsers[i].Role = role
                break
            }
        }
        http.Redirect(w, r, "/admin/users/"+userID+"?flash=Role+updated", http.StatusFound)
    }
    ```
  - [ ] 4.3 Add `DeactivateUserHandler`:
    ```go
    // DeactivateUserHandler handles POST /admin/users/{userId}/deactivate.
    // Sets Status = "deactivated" in-memory (stub phase).
    // TODO(epic-6): replace stub mutation with Admin API call (POST /api/v1/admin/users/{userId}/deactivate).
    func (h *UsersHandler) DeactivateUserHandler(w http.ResponseWriter, r *http.Request) {
        userID := r.PathValue("userId")
        for i := range stubUsers {
            if stubUsers[i].ID == userID {
                stubUsers[i].Status = "deactivated"
                break
            }
        }
        http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+deactivated", http.StatusFound)
    }
    ```

- [ ] Task 5: Update `users.html` — AC: 1, 3
  - [ ] 5.1 Add role `<select>` inside `{{ define "detail_content" }}` after the existing Role `<dd>` text row. Replace the read-only role line with:
    ```html
    <div class="flex justify-between items-center py-3">
      <dt class="text-base-content/60 text-sm">Role</dt>
      <dd>
        <form method="POST" action="/admin/users/{{ .ActiveUser.ID }}/role">
          <input type="hidden" name="_csrf" value="{{ .CSRFToken }}">
          <select name="role" onchange="this.form.submit()" class="select select-sm select-bordered">
            {{ range .ActiveUserRoleOptions }}
            <option value="{{ . }}"{{ if eq . $.ActiveUserRoleValue }} selected{{ end }}>{{ . }}</option>
            {{ end }}
          </select>
        </form>
      </dd>
    </div>
    ```
    Note: The `{{ range }}` block iterates `ActiveUserRoleOptions`; `$.ActiveUserRoleValue` is the dot-escaped outer scope reference needed inside `range`.
  - [ ] 5.2 Add `{{ template "confirm_dialog" .ActiveUserConfirmDialog }}` at the end of `{{ define "detail_content" }}` (inside the `{{ if .ActiveUser }}` block), AFTER the `</dl>` closing tag. This renders the `<dialog>` element into the page DOM.
  - [ ] 5.3 Update `{{ define "detail_footer" }}` — replace the inert stub buttons from Story 7.6 with real trigger buttons:
    ```html
    {{ define "detail_footer" }}
      {{ if .ActiveUser }}
        {{ if eq .ActiveUser.Status "active" }}
        <button type="button" class="btn btn-sm btn-warning"
                onclick="confirm_dialog.showModal()">Deactivate</button>
        {{ else }}
        <button type="button" class="btn btn-sm btn-success">Reactivate</button>
        {{ end }}
      {{ end }}
    {{ end }}
    ```
    The Reactivate button remains inert (no ConfirmDialog) — full reactivate flow is out of scope for this story (Story 7.7 MVP scope as adjusted by user prompt).

- [ ] Task 6: Add routes in `main.go` — AC: 6
  - [ ] 6.1 After the existing `POST /admin/users/{userId}/display-name` line, add:
    ```go
    // Story 7.7: Role update and deactivation — no csrf() wrapper (stub phase, same pattern as display-name).
    mux.Handle("POST /admin/users/{userId}/role", sessionGuard(http.HandlerFunc(usersHandler.UpdateRoleHandler)))
    mux.Handle("POST /admin/users/{userId}/deactivate", sessionGuard(http.HandlerFunc(usersHandler.DeactivateUserHandler)))
    ```

- [ ] Task 7: Verify no regressions — AC: all
  - [ ] 7.1 Run `make test-unit-go` — all existing tests pass; 5 new role tests pass
  - [ ] 7.2 Verify `users_detail_test.go`, `master_detail_test.go`, `users_page_test.go`, `interaction_components_test.go` still pass
  - [ ] 7.3 Run `make build-gateway` — exits 0
  - [ ] 7.4 Run Playwright: `npx playwright test user-role.spec.ts` — all 3 tests green against running dev stack

## Dev Notes

### Scope Boundary vs. Epics File

The epics file (Story 7.7) originally included a full roles sub-page at `/admin/users/{userId}/roles` and a Reactivate flow. Per the user's story brief, **this story's MVP scope is**:
- Role `<select>` in the detail panel + `POST /role` handler (no sub-page)
- Deactivate ConfirmDialog + `POST /deactivate` handler
- Reactivate button is rendered but inert (no handler in this story)

Do NOT create a `/admin/users/{userId}/roles` sub-page route — that is deferred to a later story when the Admin API (Epic 6) is available.

### Template Gotcha: `range` + Outer Dot

Inside `{{ range .ActiveUserRoleOptions }}`, the dot (`.`) becomes the current string element. To access `ActiveUserRoleValue` (outer scope), use `$.ActiveUserRoleValue`:

```html
{{ range .ActiveUserRoleOptions }}
<option value="{{ . }}"{{ if eq . $.ActiveUserRoleValue }} selected{{ end }}>{{ . }}</option>
{{ end }}
```

This is the same pattern as Story 7.5's `{{ range .Users }}` block where `$.ActiveItemID` was needed.

### ConfirmDialogData Population

`ConfirmDialogData` (defined in `page_data.go` since Story 7.3) requires `CSRFToken`. In `DetailHandler`, `csrfToken` is already computed (`CSRFTokenFromContext(r.Context())`). Populate `CSRFToken: csrfToken` in the `ConfirmDialogData`. The `HiddenFields` map can be `nil` — the dialog form action URL already encodes `userID` in the path.

The `confirm_dialog.html` template loops `{{ range $k, $v := .HiddenFields }}` — a nil map is safe to range over in Go templates (produces zero iterations).

### Template Render Order: `detail_content` vs `detail_footer`

The `{{ template "confirm_dialog" .ActiveUserConfirmDialog }}` call must be inside `{{ define "detail_content" }}` (not `detail_footer`), because it renders a `<dialog>` element that must be in the page body, not the footer action bar. The trigger `onclick="confirm_dialog.showModal()"` in `detail_footer` calls the named `<dialog id="confirm_dialog">` rendered by the component. This is the established pattern from `confirm_dialog.html`'s own comment block.

### Stub Mutation Test Isolation

`stubUsers` is a package-level var (file: `stubs.go`). `TestDeactivateUser` mutates `stubUsers[2].Status`. Use `t.Cleanup` to restore state:

```go
t.Cleanup(func() { stubUsers[2].Status = "active" })
```

Using `usr-003` (index 2) avoids collision with `users_detail_test.go` tests that use `usr-001`. Check `stubs.go` index carefully — `stubUsers[2]` = Carla Reiter (`usr-003`).

Alternatively, if tests run in the same process and order matters, use `TestMain` in a `_test.go` file to snapshot and restore `stubUsers`. The simpler `t.Cleanup` approach is sufficient for a 5-test file.

### Valid Role Values

The `UpdateRoleHandler` must validate against exactly `{"instance_admin", "compliance_officer", "user"}` — these are the three values in `StubUser.Role` (see `stubs.go`) and the three `<option>` values in the select. Any other value returns HTTP 400.

### No CSRF Wrapper (Consistent with Story 7.6 Pattern)

Both new POST routes follow the same pattern as `POST /admin/users/{userId}/display-name` (Story 7.6):
- `sessionGuard` applies — admin must be logged in
- No `csrf()` middleware wrapper — TODO comment in handler is sufficient for stub phase

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/page_data.go` | Modify — add 3 fields to `UsersPageData` |
| `gateway/internal/admin/users.go` | Modify — extend `DetailHandler`, add `UpdateRoleHandler`, add `DeactivateUserHandler` |
| `gateway/internal/admin/templates/users.html` | Modify — add role `<select>` + `confirm_dialog` call to `detail_content`; update `detail_footer` trigger button |
| `gateway/internal/admin/users_role_test.go` | NEW — 5 Go unit tests |
| `gateway/cmd/gateway/main.go` | Modify — add 2 POST routes |
| `e2e/tests/features/admin/user-role.spec.ts` | NEW — 3 Playwright E2E tests (all REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.9** (Room Detail Panel): uses the same `ConfirmDialog` pattern for the Archive action — same `confirm_dialog.html` component with a different `ConfirmDialogData`. Template pattern is identical.
- **Story 7.13** (WCAG audit): axe-core scan will cover the role select and dialog ARIA attributes.
- **Story 7.14** (Gherkin smoke flows): deactivation Gherkin scenario starts from the detail panel with the ConfirmDialog.

### Previous Story Patterns to Reuse

From `users_detail_test.go` (Story 7.6):
- `http.NewServeMux()` + `mux.HandleFunc("GET /admin/users/{userId}", h.DetailHandler)` pattern — reuse exactly in `users_role_test.go`
- `httptest.NewRecorder()` + `httptest.NewRequest(...)` + `mux.ServeHTTP(w, r)` — standard pattern
- For POST tests: set `Content-Type: application/x-www-form-urlencoded` and pass `strings.NewReader("role=user")` as body to `httptest.NewRequest`

From `user-detail.spec.ts` (Story 7.6):
- Copy the entire `loginAsAdmin` function verbatim — same Dex OIDC flow
- Same `test.describe` + `test(...)` structure

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

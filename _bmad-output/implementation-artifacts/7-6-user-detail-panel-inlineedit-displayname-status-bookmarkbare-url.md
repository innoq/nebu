---
id: 7-6
security_review: not-needed
---

# Story 7.6: User Detail Panel (InlineEdit for displayName + status, bookmarkable URL)

Status: done

## Story

As an instance admin,
I want a fully populated detail panel at `/admin/users/{id}` that lets me view and inline-edit a user's display name and see their status,
so that I can make quick corrections without navigating away from the user list.

## Acceptance Criteria

1. **`DetailHandler` extended** — `gateway/internal/admin/users.go`: The existing `DetailHandler` is extended to:
   - Return HTTP 404 (not 200) when no user matches `userID` (change from Story 7.2 behaviour which always returned 200 with a "not found" message inside the panel)
   - Read the `flash` query param; if present, populate `UsersPageData.Flash` with `AlertBannerData{Severity: "success", Message: flash, Dismissible: true}`
   - Populate `ActiveItemID` with `userID` so the list highlights the selected row
   - `CloseURL` stays as `/admin/users`

2. **`UsersPageData` extended** — `gateway/internal/admin/page_data.go`:
   - Add `Flash AlertBannerData` field to `UsersPageData`
   - All other existing fields remain unchanged (`InlineEditData` and `AlertBannerData` already exist from Stories 7.3/7.4)

3. **`users.html` `{{ define "detail_content" }}` rewritten** — replace the stub `<dl>` with a full panel:
   - User avatar placeholder: `<div class="avatar placeholder">` with the user's initials (first letter of `DisplayName`, uppercased)
   - Display name as `{{ template "inline_edit" ... }}` (C11), populated with `InlineEditData{ID: "display-name", FieldName: "display_name", Value: .ActiveUser.DisplayName, Label: "Display Name", FormAction: "/admin/users/{id}/display-name", CSRFToken: .CSRFToken}`
   - Email as read-only text (no inline edit — email is identity, not editable in MVP): `<p>{{ .ActiveUser.Email }}</p>`
   - Role as read-only text or badge (no inline edit — role changes are Story 7.7): `<span>{{ .ActiveUser.Role }}</span>`
   - Status: `{{ template "status_badge" ... }}` (C13), passing `StatusBadgeData` normalised from `ActiveUser.Status` (same `"deactivated"` → `"inactive"` mapping as the list)
   - Flash banner: `{{ if .Flash.Message }}{{ template "alert_banner" .Flash }}{{ end }}`

4. **`POST /admin/users/{id}/display-name` handler** — `UpdateDisplayNameHandler` in `gateway/internal/admin/users.go`:
   - Reads `display_name` from the form body (`r.FormValue("display_name")`)
   - Validates: non-empty, max 100 chars; returns HTTP 400 on violation (render error page or plain text)
   - For MVP: mutates the `stubUsers` slice in-memory — updates `DisplayName` on the matching entry. Changes are lost on restart (acceptable for stub phase). Add a `// TODO(epic-6): replace with Admin API call` comment
   - On success: HTTP 302 redirect to `/admin/users/{id}?flash=Display+name+updated`
   - CSRF: the form rendered by `inline_edit.html` already carries `_csrf`; for the stub handler, CSRF validation is **not enforced** (no middleware for this route yet) — add `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production` comment

5. **Routing** — `gateway/cmd/gateway/main.go`:
   - Add `POST /admin/users/{userId}/display-name` route pointing to `sessionGuard(usersHandler.UpdateDisplayNameHandler)` (no `csrf()` wrapper — see TODO note in handler; `sessionGuard` still applies)

6. **Go unit tests** — `gateway/internal/admin/users_detail_test.go` (`package admin`):
   - `TestUserDetailPanelRenders` — `GET /admin/users/usr-001` → HTTP 200, body contains "Alice Müller", body contains `inline-edit-field` (from `inline_edit.html` outer `<div>`)
   - `TestUserDetailPanelNotFound` — `GET /admin/users/xxx-999` → HTTP 404
   - `TestUserDetailFlashMessage` — `GET /admin/users/usr-001?flash=Display+name+updated` → HTTP 200, body contains "Display name updated"
   - `TestUpdateDisplayName` — `POST /admin/users/usr-001/display-name` with `display_name=New Name` → HTTP 302, `Location` header contains `/admin/users/usr-001?flash=`
   - `TestUpdateDisplayNameEmpty` — `POST /admin/users/usr-001/display-name` with `display_name=` → HTTP 400

7. **Playwright E2E tests** — `e2e/tests/features/admin/user-detail.spec.ts` — **REAL tests (not `test.skip`)**:
   - `user detail panel opens when clicking a user row` — navigate to `/admin/users`, click first user row (role="option"), detail panel (`section[role="region"]`) visible and contains user name
   - `inline edit saves display name` — navigate to `/admin/users/usr-001`, click edit button (`aria-label="Edit Display Name"`), fill a new name, click Save → page reloads, flash banner with success message visible
   - `flash message shown after display name update` — navigate to `/admin/users/usr-001?flash=Display+name+updated` → `div[role="alert"]` containing "Display name updated" is visible
   - Auth: `loginAsAdmin` helper (OIDC Authorization Code + PKCE, identical pattern to `users-page.spec.ts`)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **UserDetailPanelRenders — GET /admin/users/usr-001 returns 200 with inline edit** — Go `net/http/httptest` (`gateway/internal/admin/users_detail_test.go`)
   - Given: `UsersHandler.DetailHandler` wired to a test HTTP server; `stubUsers` contains `usr-001` (Alice Müller)
   - When: `GET /admin/users/usr-001`
   - Then: HTTP 200, body contains "Alice Müller", body contains `inline-edit-field`

2. **UserDetailPanelNotFound — unknown user ID returns 404** — Go `net/http/httptest`
   - Given: `DetailHandler` with standard `stubUsers`
   - When: `GET /admin/users/xxx-999`
   - Then: HTTP 404

3. **UserDetailFlashMessage — flash query param renders AlertBanner** — Go `net/http/httptest`
   - Given: `DetailHandler` for `usr-001`
   - When: `GET /admin/users/usr-001?flash=Display+name+updated`
   - Then: HTTP 200, body contains "Display name updated"

4. **UpdateDisplayName — valid POST redirects with flash** — Go `net/http/httptest`
   - Given: `UpdateDisplayNameHandler` wired; `stubUsers` contains `usr-001`
   - When: `POST /admin/users/usr-001/display-name` with `display_name=New Name`
   - Then: HTTP 302, `Location` header contains `/admin/users/usr-001` and `flash=`

5. **UpdateDisplayNameEmpty — empty display_name returns 400** — Go `net/http/httptest`
   - Given: `UpdateDisplayNameHandler` wired
   - When: `POST /admin/users/usr-001/display-name` with `display_name=`
   - Then: HTTP 400

6. **Playwright: user detail panel opens when clicking a user row** — Playwright (`e2e/tests/features/admin/user-detail.spec.ts`)
   - Given: full dev stack running, admin logged in, at `/admin/users`
   - When: click the first `role="option"` list item
   - Then: `section[role="region"]` is visible and contains the user's name

7. **Playwright: inline edit saves display name** — Playwright
   - Given: admin on `/admin/users/usr-001`
   - When: click `button[aria-label="Edit Display Name"]`, fill new name, click Save
   - Then: page reloads to `/admin/users/usr-001?flash=...`; `div[role="alert"]` with success message is visible

8. **Playwright: flash message visible after display name update** — Playwright
   - Given: admin navigates directly to `/admin/users/usr-001?flash=Display+name+updated`
   - When: page renders
   - Then: `div[role="alert"]` containing "Display name updated" is visible

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 6
  - [ ] 1.1 Create `gateway/internal/admin/users_detail_test.go` in `package admin`
  - [ ] 1.2 Write `TestUserDetailPanelRenders`:
    - Setup: `NewTemplateHandler()` + `NewUsersHandler(tmpl)`; wire `DetailHandler` to `httptest.NewServer`
    - Request: `GET /admin/users/usr-001`
    - Assert: `w.Code == 200`, body contains "Alice Müller", body contains `inline-edit-field`
  - [ ] 1.3 Write `TestUserDetailPanelNotFound`:
    - Request: `GET /admin/users/xxx-999`
    - Assert: `w.Code == 404`
    - Note: `httptest.NewRequest` for `DetailHandler` requires manually setting `r.SetPathValue("userId", "xxx-999")`
  - [ ] 1.4 Write `TestUserDetailFlashMessage`:
    - Request: `GET /admin/users/usr-001?flash=Display+name+updated`
    - Assert: `w.Code == 200`, body contains "Display name updated"
  - [ ] 1.5 Write `TestUpdateDisplayName`:
    - Setup: `http.NewServeMux()` + register `POST /admin/users/{userId}/display-name`; use `httptest.NewServer`
    - Request: `POST` with form body `display_name=New+Name`; set `Content-Type: application/x-www-form-urlencoded`
    - Assert: `w.Code == 302`, `w.Header().Get("Location")` contains `/admin/users/usr-001` and `flash=`
  - [ ] 1.6 Write `TestUpdateDisplayNameEmpty`:
    - Request: `POST` with form body `display_name=`
    - Assert: `w.Code == 400`
  - [ ] 1.7 Confirm RED: `go test ./internal/admin/...` fails — `UpdateDisplayNameHandler` does not exist; `DetailHandler` returns 200 for unknown IDs; `UsersPageData` lacks `Flash` field

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 7
  - [ ] 2.1 Create `e2e/tests/features/admin/user-detail.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `users-page.spec.ts` (identical pattern — OIDC Authorization Code + PKCE)
  - [ ] 2.3 Write `test('user detail panel opens when clicking a user row')` — NOT `test.skip`:
    - `loginAsAdmin(page)`, `goto('/admin/users')`
    - `await page.locator('[role="option"]').first().click()`
    - `await expect(page.locator('section[role="region"]')).toBeVisible()`
    - `await expect(page.locator('section[role="region"]')).toContainText(/\w+/)` (any user name)
  - [ ] 2.4 Write `test('inline edit saves display name')`:
    - `loginAsAdmin(page)`, `goto('/admin/users/usr-001')`
    - `await page.locator('button[aria-label="Edit Display Name"]').click()`
    - `await page.locator('input[name="display_name"]').fill('Test Display Name')`
    - `await page.locator('button[type="submit"]:has-text("Save")').click()`
    - `await expect(page).toHaveURL(/[?&]flash=/)` (PRG redirect with flash)
    - `await expect(page.locator('div[role="alert"]')).toBeVisible()`
  - [ ] 2.5 Write `test('flash message shown after display name update')`:
    - `loginAsAdmin(page)`, `goto('/admin/users/usr-001?flash=Display+name+updated')`
    - `await expect(page.locator('div[role="alert"]')).toContainText('Display name updated')`
  - [ ] 2.6 Confirm RED: Playwright tests fail — no `UpdateDisplayNameHandler`, `DetailHandler` returns 200 for missing users

- [ ] Task 3: Add `Flash` field to `UsersPageData` — AC: 2
  - [ ] 3.1 In `gateway/internal/admin/page_data.go`, add `Flash AlertBannerData` to `UsersPageData`:
    ```go
    type UsersPageData struct {
        PageData
        Users        []UserRowData
        SearchInput  SearchInputData
        FilterBar    []FilterOption
        TotalCount   int
        CurrentPage  int
        HasMore      bool
        NextPage     int
        EmptyState   EmptyStateData
        ActiveItemID string
        ActiveUser   *StubUser
        CloseURL     string
        Flash        AlertBannerData // populated when ?flash= query param is present (Story 7.6)
    }
    ```
  - [ ] 3.2 Verify existing tests still compile — the new field is zero-valued in all existing tests; no changes required to existing test files

- [ ] Task 4: Extend `DetailHandler` and add `UpdateDisplayNameHandler` in `users.go` — AC: 1, 4
  - [ ] 4.1 Extend `DetailHandler`:
    ```go
    func (h *UsersHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
        userID := r.PathValue("userId")
        user := findStubUser(userID)
        if user == nil {
            http.NotFound(w, r)
            return
        }

        // Read flash query param and populate AlertBannerData if present
        var flash AlertBannerData
        if msg := r.URL.Query().Get("flash"); msg != "" {
            flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
        }

        rows := make([]UserRowData, len(stubUsers))
        for i, u := range stubUsers {
            rows[i] = toUserRowData(u)
        }

        data := UsersPageData{
            PageData:     PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
            Users:        rows,
            ActiveItemID: userID,
            ActiveUser:   user,
            CloseURL:     "/admin/users",
            Flash:        flash,
        }
        h.tmpl.render(w, "users", data)
    }
    ```
  - [ ] 4.2 Add `UpdateDisplayNameHandler`:
    ```go
    // UpdateDisplayNameHandler handles POST /admin/users/{userId}/display-name.
    // Validates and updates the user's display name in-memory (stub phase).
    // TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
    func (h *UsersHandler) UpdateDisplayNameHandler(w http.ResponseWriter, r *http.Request) {
        userID := r.PathValue("userId")
        // TODO(story-7-csrf): enforce CSRF middleware when wiring in production
        if err := r.ParseForm(); err != nil {
            http.Error(w, "bad request", http.StatusBadRequest)
            return
        }
        displayName := r.FormValue("display_name")
        if displayName == "" || len(displayName) > 100 {
            http.Error(w, "display_name must be 1–100 characters", http.StatusBadRequest)
            return
        }
        // Mutate stub data in-memory (changes lost on restart — acceptable for stub phase)
        for i := range stubUsers {
            if stubUsers[i].ID == userID {
                stubUsers[i].DisplayName = displayName
                break
            }
        }
        http.Redirect(w, r, "/admin/users/"+userID+"?flash=Display+name+updated", http.StatusFound)
    }
    ```

- [ ] Task 5: Rewrite `{{ define "detail_content" }}` in `users.html` — AC: 3
  - [ ] 5.1 Replace the existing stub `<dl>` block with:
    ```html
    {{ define "detail_content" }}
      {{ if .ActiveUser }}
        {{ if .Flash.Message }}
        {{ template "alert_banner" .Flash }}
        {{ end }}

        {{/* Avatar placeholder with user initials */}}
        <div class="flex justify-center mb-4">
          <div class="avatar placeholder">
            <div class="bg-neutral text-neutral-content rounded-full w-16">
              <span class="text-2xl">{{ slice .ActiveUser.DisplayName 0 1 }}</span>
            </div>
          </div>
        </div>

        <dl class="divide-y divide-base-300">
          <div class="py-3">
            <dt class="text-base-content/60 text-xs uppercase tracking-wide mb-1">Display Name</dt>
            <dd>
              {{ template "inline_edit" (dict "ID" "display-name" "FieldName" "display_name" "Value" .ActiveUser.DisplayName "Label" "Display Name" "FormAction" (printf "/admin/users/%s/display-name" .ActiveUser.ID) "CSRFToken" .CSRFToken) }}
            </dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Email</dt>
            <dd class="text-sm font-medium">{{ .ActiveUser.Email }}</dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Role</dt>
            <dd class="text-sm font-medium">{{ .ActiveUser.Role }}</dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Status</dt>
            <dd>
              {{ $badgeStatus := "inactive" }}
              {{ if eq .ActiveUser.Status "active" }}{{ $badgeStatus = "active" }}{{ end }}
              {{ template "status_badge" (StatusBadgeData $badgeStatus .ActiveUser.Status) }}
            </dd>
          </div>
        </dl>
      {{ else if .ActiveItemID }}
        <div class="text-base-content/50 text-sm">User not found.</div>
      {{ end }}
    {{ end }}
    ```
  - [ ] 5.2 **Template FuncMap note**: Go's `html/template` does NOT have a built-in `dict` or struct-construction function. The `inline_edit` template call requires passing an `InlineEditData` struct — options:
    - **Option A (recommended):** Pre-compute `ActiveUserInlineEdit InlineEditData` in `DetailHandler` and add it to `UsersPageData`. Then the template calls `{{ template "inline_edit" .ActiveUserInlineEdit }}` — no FuncMap needed. Add `ActiveUserInlineEdit InlineEditData` to `UsersPageData` in `page_data.go`.
    - **Option B:** Add a `"dict"` FuncMap helper to `NewTemplateHandler` (same as the `add` FuncMap discussion in Story 7.5). Less clean for typed structs.
    - **Recommendation:** Option A — add `ActiveUserInlineEdit InlineEditData` to `UsersPageData`; handler populates it. Template calls `{{ template "inline_edit" .ActiveUserInlineEdit }}`. Clean, testable, no FuncMap.
  - [ ] 5.3 For the `status_badge` call in `detail_content`: the same issue applies. Use a pre-computed `ActiveUserStatusBadge StatusBadgeData` field in `UsersPageData`, populated by the handler using the existing `toUserRowData` helper's badge logic. Template calls `{{ template "status_badge" .ActiveUserStatusBadge }}`.
  - [ ] 5.4 For the avatar initials: `{{ slice .ActiveUser.DisplayName 0 1 }}` is a standard Go template function. This works if `DisplayName` is non-empty (guaranteed by the non-empty validation in `UpdateDisplayNameHandler` and the fact that stub data always has names). Add a guard `{{ if .ActiveUser.DisplayName }}` if needed.
  - [ ] 5.5 Keep `{{ define "detail_title" }}` and `{{ define "detail_footer" }}` blocks unchanged (Story 7.2 content — deactivate/reactivate button is Story 7.7 scope)

- [ ] Task 6: Add route in `main.go` — AC: 5
  - [ ] 6.1 In `gateway/cmd/gateway/main.go`, after the existing `GET /admin/users/{userId}` line:
    ```go
    mux.Handle("POST /admin/users/{userId}/display-name", sessionGuard(http.HandlerFunc(usersHandler.UpdateDisplayNameHandler)))
    ```
  - [ ] 6.2 Note: intentionally NO `csrf()` wrapper — the CSRF middleware does not yet cover this route in the stub phase. The TODO comment in `UpdateDisplayNameHandler` tracks this.

- [ ] Task 7: Verify no regressions — AC: all
  - [ ] 7.1 Run `make test-unit-go` — all existing tests pass; 5 new detail tests pass
  - [ ] 7.2 Verify `master_detail_test.go` and `users_page_test.go` still pass (no struct changes that break them)
  - [ ] 7.3 Verify `make build-gateway` exits 0
  - [ ] 7.4 Run Playwright: `npx playwright test user-detail.spec.ts` — all 3 tests green against running dev stack

## Dev Notes

### 404 Behaviour Change

Story 7.2 `DetailHandler` returned HTTP 200 with a "User not found" message inside the panel for unknown IDs. This story changes it to HTTP 404. The Story 7.2 Playwright test `'nonexistent user ID renders not-found within panel'` explicitly asserts `await expect(page).not.toHaveURL(/404/)` — it checks the URL, not the status code. That test will still pass because the URL stays at `/admin/users/nonexistent-id` (no redirect). However, the Go unit test in `master_detail_test.go` that tests this behaviour (if any) may need updating — check and update the test expectation from 200 to 404.

### `UsersPageData` Additional Fields for Story 7.6

Beyond `Flash AlertBannerData`, the dev agent should also add:

```go
// ActiveUserInlineEdit is pre-computed by DetailHandler for the inline_edit component (Story 7.6).
// Nil-safe: only populated when ActiveUser != nil.
ActiveUserInlineEdit InlineEditData

// ActiveUserStatusBadge is pre-computed by DetailHandler for the status_badge component (Story 7.6).
// Only meaningful when ActiveUser != nil.
ActiveUserStatusBadge StatusBadgeData
```

Both fields are zero-valued in list mode (no template rendering side-effects — the detail_content block is only rendered when `.ActiveUser != nil`).

### In-Memory Stub Mutation

`stubUsers` is a package-level `var` slice in `stubs.go`. Mutating `stubUsers[i].DisplayName` in `UpdateDisplayNameHandler` is safe for the stub phase. Concurrent writes from multiple requests are not a concern in development (single goroutine test suite). For production, Epic 6 replaces this with an API call.

Important: test isolation — the Go unit tests must not assume `stubUsers` is unmodified after a POST test. Either:
- Reset `stubUsers` in a `TestMain` or `t.Cleanup`, or
- Test the POST redirect only (don't verify `DisplayName` was changed) since the unit test already verifies the 302 + flash URL

### `slice` Template Function for Initials

Go's `html/template` supports `slice` for slicing strings. `{{ slice .ActiveUser.DisplayName 0 1 }}` extracts the first character. If `DisplayName` is a multi-byte Unicode string (e.g. "Ärgernis"), `slice` operates on bytes, not runes — this could produce a partial UTF-8 sequence. For the stub phase with ASCII-ish stub names this is acceptable. Add a `// TODO: use rune-aware initials helper in production` comment in the template or handler.

Alternatively, pre-compute `ActiveUserInitials string` in the handler using `[]rune(u.DisplayName)[0:1]` — cleaner. This is recommended if the dev agent wants to avoid the UTF-8 edge case.

### CSRF Note

The `inline_edit.html` template already renders `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`. In the stub phase, `UpdateDisplayNameHandler` reads `r.FormValue("display_name")` without verifying `_csrf`. The sessionGuard middleware still applies — the admin must be logged in. The TODO comment in the handler and `main.go` wiring is sufficient for the stub phase; full CSRF enforcement is tracked in `5-13-csrf-middleware-admin-post` (done) but needs explicit wiring for new routes.

### Bookmarkable URL

`GET /admin/users/{id}?flash=...` already serves the detail panel with the flash message on initial page load. After saving a display name, the PRG redirect sends the browser to this URL, which renders the updated name and the flash banner. The flash disappears on the next navigation (it's query-param-based, not session-based — no session storage needed).

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/page_data.go` | Modify — add `Flash`, `ActiveUserInlineEdit`, `ActiveUserStatusBadge` to `UsersPageData` |
| `gateway/internal/admin/users.go` | Modify — extend `DetailHandler` (404 + flash), add `UpdateDisplayNameHandler` |
| `gateway/internal/admin/templates/users.html` | Modify — rewrite `detail_content` block |
| `gateway/internal/admin/users_detail_test.go` | NEW — 5 Go unit tests |
| `gateway/cmd/gateway/main.go` | Modify — add `POST /admin/users/{userId}/display-name` route |
| `e2e/tests/features/admin/user-detail.spec.ts` | NEW — 3 Playwright E2E tests (all REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.7** (User Role UI + Deactivation): adds a `ConfirmDialog` to the detail footer for deactivating/reactivating users; builds on the detail panel scaffolded here
- **Story 7.13** (WCAG audit): axe-core scan will run against `/admin/users/usr-001`; detail panel accessibility must be clean (landmark nesting, aria-label completeness)
- **Story 7.14** (Gherkin smoke flows): Gherkin test for user deactivation starts from the detail panel

### Review Findings

- [x] [Review][Patch] byte-count vs rune-count in `UpdateDisplayNameHandler` max-length check [users.go:153] — fixed: `len(displayName)` → `utf8.RuneCountInString(displayName)`; regression test `TestUpdateDisplayNameTooLongMultibyte` added
- [x] [Review][Patch] Dead `{{ else if .ActiveItemID }}` branch in `detail_content` template [users.html:89] — removed; unreachable after `DetailHandler` returns 404 for unknown users (Story 7.6 behaviour change)

**Code review complete.** 0 `decision-needed`, 2 `patch` (both fixed inline), 0 `defer`, 2 dismissed as noise.

## Status Notes

Created: 2026-04-29. Stories 7.1–7.5 are done. This story fills in the `{{ define "detail_content" }}` block that Story 7.2 left as a stub, adding InlineEdit for display name (C11), AlertBanner for flash messages (C12), StatusBadge for user status (C13), and the PRG POST handler for display-name updates. The detail panel becomes a real interactive UI surface for the first time. Playwright tests are REAL (not `test.skip`) because the detail route `/admin/users/{id}` has been live since Story 7.2.

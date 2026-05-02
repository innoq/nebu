---
id: 7-9
security_review: not-needed
---

# Story 7.9: Room Detail Panel (settings edit, archive UI, confirmation pattern)

Status: ready-for-dev

## Story

As an instance admin,
I want a fully populated detail panel at `/admin/rooms/{id}` that lets me inline-edit a room's name, see its status badge, and archive a room via a confirmation dialog,
so that I can manage rooms directly from the room list without navigating away.

## Acceptance Criteria

1. **`DetailHandler` extended** — `gateway/internal/admin/rooms.go`: The existing `DetailHandler` is extended to:
   - Return HTTP 404 (not 200) when no room matches `roomId` (change from Story 7.2 behaviour which returned 200 with a "not found" message inside the panel)
   - Read the `flash` query param; if present, populate `RoomsPageData.Flash` with `AlertBannerData{Severity: "success", Message: flash, Dismissible: true}`
   - Populate `ActiveItemID` with `roomId` so the list highlights the selected row
   - Pre-compute `ActiveRoomInlineEdit InlineEditData` — `{ID: "room-name", FieldName: "name", Value: room.Name, Label: "Room Name", FormAction: "/admin/rooms/{id}/name", CSRFToken: csrfToken}`
   - Pre-compute `ActiveRoomStatusBadge StatusBadgeData` — same mapping as `toRoomRowData` (`"archived"` → `"inactive"`)
   - Pre-compute `ActiveRoomConfirmDialog ConfirmDialogData` — `{Title: "Archive room", Message: "This will archive …{room.Name}…", ConfirmLabel: "Archive", ConfirmClass: "btn-error", FormAction: "/admin/rooms/{id}/archive", CSRFToken: csrfToken}`
   - Pre-compute `ActiveRoomInitial string` — first rune of `room.Name` (rune-safe, same pattern as `ActiveUserInitial`)
   - `CloseURL` stays as `/admin/rooms`

2. **`RoomsPageData` extended** — `gateway/internal/admin/page_data.go`:
   - Add `Flash AlertBannerData` — populated when `?flash=` query param is present; zero-valued in list mode
   - Add `ActiveRoomInlineEdit InlineEditData` — pre-computed by `DetailHandler`; only meaningful when `ActiveRoom != nil`
   - Add `ActiveRoomStatusBadge StatusBadgeData` — pre-computed by `DetailHandler`
   - Add `ActiveRoomConfirmDialog ConfirmDialogData` — pre-computed by `DetailHandler`
   - Add `ActiveRoomInitial string` — first rune of room Name; pre-computed by `DetailHandler`
   - All existing fields remain unchanged

3. **`rooms.html` `{{ define "detail_content" }}` rewritten** — replace the stub `<dl>` with a full panel:
   - Flash banner: `{{ if .Flash.Message }}{{ template "alert_banner" .Flash }}{{ end }}`
   - Room avatar placeholder: `<div class="avatar placeholder">` with `ActiveRoomInitial`
   - Room name via `{{ template "inline_edit" .ActiveRoomInlineEdit }}`
   - Visibility as read-only text (no inline edit — visibility is set at room creation in MVP)
   - Member count as read-only text
   - Status via `{{ template "status_badge" .ActiveRoomStatusBadge }}`

4. **`{{ define "detail_footer" }}` rewritten** — replace the stub Archive button with a real trigger:
   - For `active` rooms: `<button type="button" onclick="confirm_dialog.showModal()" class="btn btn-sm btn-warning">Archive room</button>`
   - `{{ template "confirm_dialog" .ActiveRoomConfirmDialog }}` at the end of the block
   - For `archived` rooms: keep a read-only "Archived" label (no restore in MVP scope)

5. **`POST /admin/rooms/{roomId}/name` handler** — `UpdateRoomNameHandler` in `gateway/internal/admin/rooms.go`:
   - Reads `name` from the form body (`r.FormValue("name")`)
   - Validates: non-empty after `strings.TrimSpace`, max 100 rune chars; returns HTTP 400 on violation
   - For MVP: mutates `stubRooms` in-memory — updates `Name` on the matching entry. Changes are lost on restart (acceptable for stub phase). Add a `// TODO(epic-6): replace with Admin API call` comment
   - On success: HTTP 302 redirect to `/admin/rooms/{id}?flash=Name+updated`
   - CSRF: the `inline_edit.html` form already carries `_csrf`; stub handler does NOT enforce CSRF (add `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production` comment)

6. **`POST /admin/rooms/{roomId}/archive` handler** — `ArchiveRoomHandler` in `gateway/internal/admin/rooms.go`:
   - Sets `stubRoom.Status = "archived"` in-memory for the matching room
   - On success: HTTP 302 redirect to `/admin/rooms/{id}?flash=Room+archived`
   - Add `// TODO(epic-6): replace stub mutation with Admin API call` and `// TODO(story-7-csrf)` comments

7. **Routing** — `gateway/cmd/gateway/main.go`:
   - Add `POST /admin/rooms/{roomId}/name` route: `sessionGuard(http.HandlerFunc(roomsHandler.UpdateRoomNameHandler))` (no `csrf()` wrapper — see TODO note in handler; `sessionGuard` still applies)
   - Add `POST /admin/rooms/{roomId}/archive` route: `sessionGuard(http.HandlerFunc(roomsHandler.ArchiveRoomHandler))` (same pattern)

8. **Go unit tests** — `gateway/internal/admin/rooms_detail_test.go` (`package admin`):
   - `TestRoomDetailPanelRenders` — `GET /admin/rooms/room-001` → HTTP 200, body contains "General", body contains `inline-edit-field`
   - `TestRoomDetailPanelNotFound` — `GET /admin/rooms/xxx-999` → HTTP 404
   - `TestRoomDetailFlashMessage` — `GET /admin/rooms/room-001?flash=Name+updated` → HTTP 200, body contains "Name updated"
   - `TestUpdateRoomName` — `POST name=New Room` → HTTP 302, Location contains `/admin/rooms/room-001` and `flash=`
   - `TestUpdateRoomNameEmpty` — `POST name=` → HTTP 400
   - `TestUpdateRoomNameTooLong` — `POST` with 101-rune name → HTTP 400
   - `TestArchiveRoom` — `POST /admin/rooms/room-002/archive` → HTTP 302, `stubRooms` entry for room-002 has `Status == "archived"`; use `t.Cleanup` to restore
   - `TestArchiveConfirmDialogRendered` — `GET /admin/rooms/room-001` → body contains `role="alertdialog"`

9. **Playwright E2E tests** — `e2e/tests/features/admin/room-detail.spec.ts` — **REAL tests (not `test.skip`)**:
   - `room detail panel opens when clicking a room row` — navigate to `/admin/rooms`, click first `role="option"` list item, `section[role="region"]` is visible and contains an edit button
   - `flash message shown after room name update` — navigate to `/admin/rooms/room-001?flash=Name+updated` → `div[role="alert"]` containing "Name updated" is visible
   - `inline edit saves room name` — navigate to `/admin/rooms/room-001`, click `button[aria-label="Edit Room Name"]`, fill a new name, click Save → page URL contains `flash=`, flash alert visible
   - `archive button opens confirmation dialog` — navigate to `/admin/rooms/room-001`, click "Archive room" button, `dialog[role="alertdialog"]` is visible

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **RoomDetailPanelRenders — GET /admin/rooms/room-001 returns 200 with inline edit** — Go `net/http/httptest` (`gateway/internal/admin/rooms_detail_test.go`)
   - Given: `RoomsHandler.DetailHandler` wired to a test HTTP mux; `stubRooms` contains `room-001` (General)
   - When: `GET /admin/rooms/room-001`
   - Then: HTTP 200, body contains "General", body contains `inline-edit-field`

2. **RoomDetailPanelNotFound — unknown room ID returns 404** — Go `net/http/httptest`
   - Given: `DetailHandler` with standard `stubRooms`
   - When: `GET /admin/rooms/xxx-999`
   - Then: HTTP 404

3. **RoomDetailFlashMessage — flash query param renders AlertBanner** — Go `net/http/httptest`
   - Given: `DetailHandler` for `room-001`
   - When: `GET /admin/rooms/room-001?flash=Name+updated`
   - Then: HTTP 200, body contains "Name updated"

4. **UpdateRoomName — valid POST redirects with flash** — Go `net/http/httptest`
   - Given: `UpdateRoomNameHandler` wired; `stubRooms` contains `room-001`
   - When: `POST /admin/rooms/room-001/name` with `name=New Room`
   - Then: HTTP 302, `Location` header contains `/admin/rooms/room-001` and `flash=`

5. **UpdateRoomNameEmpty — empty name returns 400** — Go `net/http/httptest`
   - Given: `UpdateRoomNameHandler` wired
   - When: `POST /admin/rooms/room-001/name` with `name=`
   - Then: HTTP 400

6. **UpdateRoomNameTooLong — 101-rune name returns 400** — Go `net/http/httptest`
   - Given: `UpdateRoomNameHandler` wired
   - When: `POST /admin/rooms/room-001/name` with `name=` set to 101 × "x"
   - Then: HTTP 400

7. **ArchiveRoom — POST sets status to archived** — Go `net/http/httptest`
   - Given: `ArchiveRoomHandler` wired; `stubRooms` contains `room-002` (Engineering, active); `t.Cleanup` restores status
   - When: `POST /admin/rooms/room-002/archive`
   - Then: HTTP 302, Location contains `flash=`, `findStubRoom("room-002").Status == "archived"`

8. **ArchiveConfirmDialogRendered — GET renders confirm_dialog** — Go `net/http/httptest`
   - Given: `DetailHandler` for `room-001` (active room)
   - When: `GET /admin/rooms/room-001`
   - Then: HTTP 200, body contains `role="alertdialog"`

9. **Playwright: room detail panel opens when clicking a room row** — Playwright (`e2e/tests/features/admin/room-detail.spec.ts`)
   - Given: full dev stack running, admin logged in, at `/admin/rooms`
   - When: click the first `role="option"` list item
   - Then: `section[role="region"]` is visible and contains an edit button

10. **Playwright: flash message shown after room name update** — Playwright
    - Given: admin navigates to `/admin/rooms/room-001?flash=Name+updated`
    - When: page renders
    - Then: `div[role="alert"]` containing "Name updated" is visible

11. **Playwright: inline edit saves room name** — Playwright
    - Given: admin on `/admin/rooms/room-001`
    - When: click `button[aria-label="Edit Room Name"]`, fill a new name, click Save
    - Then: URL contains `flash=`; `div[role="alert"]` is visible

12. **Playwright: archive button opens confirmation dialog** — Playwright
    - Given: admin on `/admin/rooms/room-001` (active room)
    - When: click button with text "Archive room"
    - Then: `dialog[role="alertdialog"]` is visible

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 8
  - [ ] 1.1 Create `gateway/internal/admin/rooms_detail_test.go` in `package admin`
  - [ ] 1.2 Write `TestRoomDetailPanelRenders`:
    - Setup: `NewTemplateHandler()` + `NewRoomsHandler(tmpl)`; `http.NewServeMux()` wired with `GET /admin/rooms/{roomId}` → `DetailHandler`
    - Request: `GET /admin/rooms/room-001`
    - Assert: `w.Code == 200`, body contains "General", body contains `inline-edit-field`
  - [ ] 1.3 Write `TestRoomDetailPanelNotFound`:
    - Request: `GET /admin/rooms/xxx-999`
    - Assert: `w.Code == 404`
  - [ ] 1.4 Write `TestRoomDetailFlashMessage`:
    - Request: `GET /admin/rooms/room-001?flash=Name+updated`
    - Assert: `w.Code == 200`, body contains "Name updated"
  - [ ] 1.5 Write `TestUpdateRoomName`:
    - Setup: `t.Cleanup` to restore `stubRooms[0].Name` (room-001)
    - Request: `POST /admin/rooms/room-001/name` with form body `name=New+Room`; `Content-Type: application/x-www-form-urlencoded`
    - Assert: `w.Code == 302`, Location contains `/admin/rooms/room-001` and `flash=`
  - [ ] 1.6 Write `TestUpdateRoomNameEmpty`:
    - Request: `POST /admin/rooms/room-001/name` with `name=`
    - Assert: `w.Code == 400`
  - [ ] 1.7 Write `TestUpdateRoomNameTooLong`:
    - Request: `POST /admin/rooms/room-001/name` with `name=` set to `strings.Repeat("x", 101)`
    - Assert: `w.Code == 400`
  - [ ] 1.8 Write `TestArchiveRoom`:
    - Setup: `t.Cleanup` to restore `room-002` status to `"active"` (use index or `findStubRoom`)
    - Request: `POST /admin/rooms/room-002/archive`
    - Assert: `w.Code == 302`, Location contains `flash=`, `findStubRoom("room-002").Status == "archived"`
  - [ ] 1.9 Write `TestArchiveConfirmDialogRendered`:
    - Request: `GET /admin/rooms/room-001`
    - Assert: `w.Code == 200`, body contains `role="alertdialog"`
  - [ ] 1.10 Confirm RED: `go test ./internal/admin/...` fails — `UpdateRoomNameHandler` and `ArchiveRoomHandler` do not exist; `DetailHandler` returns 200 for unknown rooms; `RoomsPageData` lacks new fields

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 9
  - [ ] 2.1 Create `e2e/tests/features/admin/room-detail.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `rooms-page.spec.ts` (identical pattern — OIDC Authorization Code + PKCE)
  - [ ] 2.3 Write `test('room detail panel opens when clicking a room row')` — NOT `test.skip`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms')`
    - `await page.locator('[role="option"]').first().click()`
    - `await expect(page.locator('section[role="region"]')).toBeVisible()`
    - `await expect(page.locator('section[role="region"] [aria-label^="Edit"]')).toBeVisible()`
  - [ ] 2.4 Write `test('flash message shown after room name update')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms/room-001?flash=Name+updated')`
    - `await expect(page.locator('div[role="alert"]')).toContainText('Name updated')`
  - [ ] 2.5 Write `test('inline edit saves room name')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms/room-001')`
    - `await page.locator('button[aria-label="Edit Room Name"]').click()`
    - `await page.locator('input[name="name"]').fill('Test Room Name')`
    - `await page.locator('button[type="submit"]:has-text("Save")').click()`
    - `await expect(page).toHaveURL(/[?&]flash=/, { timeout: 10_000 })`
    - `await expect(page.locator('div[role="alert"]')).toBeVisible()`
  - [ ] 2.6 Write `test('archive button opens confirmation dialog')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms/room-001')`
    - `await page.locator('button:has-text("Archive room")').click()`
    - `await expect(page.locator('dialog[role="alertdialog"]')).toBeVisible()`
  - [ ] 2.7 Confirm RED: Playwright tests fail — `DetailHandler` returns 200 for unknown rooms; no `UpdateRoomNameHandler`; no `inline-edit-field` in rendered panel

- [ ] Task 3: Add new fields to `RoomsPageData` in `page_data.go` — AC: 2
  - [ ] 3.1 In `gateway/internal/admin/page_data.go`, add to `RoomsPageData`:
    ```go
    // Flash is populated when ?flash= query param is present (Story 7.9).
    // Zero-valued in list mode — no template rendering side-effects.
    Flash AlertBannerData
    // ActiveRoomInlineEdit is pre-computed by DetailHandler for the inline_edit component (Story 7.9).
    // Only meaningful when ActiveRoom != nil.
    ActiveRoomInlineEdit InlineEditData
    // ActiveRoomStatusBadge is pre-computed by DetailHandler for the status_badge component (Story 7.9).
    // Only meaningful when ActiveRoom != nil.
    ActiveRoomStatusBadge StatusBadgeData
    // ActiveRoomConfirmDialog is pre-computed by DetailHandler for the confirm_dialog component (Story 7.9).
    // Only meaningful when ActiveRoom != nil and ActiveRoom.Status == "active".
    ActiveRoomConfirmDialog ConfirmDialogData
    // ActiveRoomInitial holds the first rune of Name as a string (rune-safe).
    // Pre-computed by DetailHandler to avoid UTF-8 byte-slice edge cases in templates.
    // TODO: use rune-aware initials helper in production when multi-char initials are needed.
    ActiveRoomInitial string
    ```
  - [ ] 3.2 Verify existing tests still compile — the new fields are zero-valued in all existing tests; no changes required to existing test files

- [ ] Task 4: Extend `DetailHandler` and add `UpdateRoomNameHandler` + `ArchiveRoomHandler` in `rooms.go` — AC: 1, 5, 6
  - [ ] 4.1 Extend `DetailHandler`:
    ```go
    func (h *RoomsHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        room := findStubRoom(roomID)
        if room == nil {
            http.NotFound(w, r)
            return
        }

        var flash AlertBannerData
        if msg := r.URL.Query().Get("flash"); msg != "" {
            flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
        }

        csrfToken := CSRFTokenFromContext(r.Context())

        inlineEdit := InlineEditData{
            ID:         "room-name",
            FieldName:  "name",
            Value:      room.Name,
            Label:      "Room Name",
            FormAction: "/admin/rooms/" + roomID + "/name",
            CSRFToken:  csrfToken,
        }

        badgeStatus := room.Status
        if badgeStatus == "archived" {
            badgeStatus = "inactive"
        }
        statusBadge := StatusBadgeData{Status: badgeStatus}

        confirmDialog := ConfirmDialogData{
            Title:        "Archive room",
            Message:      "This will archive " + room.Name + ". Are you sure?",
            ConfirmLabel: "Archive",
            ConfirmClass: "btn-error",
            FormAction:   "/admin/rooms/" + roomID + "/archive",
            CSRFToken:    csrfToken,
        }

        initial := ""
        if runes := []rune(room.Name); len(runes) > 0 {
            initial = string(runes[0:1])
        }

        data := RoomsPageData{
            PageData:               PageData{ActiveNav: "rooms", CSRFToken: csrfToken},
            Rooms:                  toRoomRowDataSlice(stubRooms),
            ActiveItemID:           roomID,
            ActiveRoom:             room,
            CloseURL:               "/admin/rooms",
            Flash:                  flash,
            ActiveRoomInlineEdit:   inlineEdit,
            ActiveRoomStatusBadge:  statusBadge,
            ActiveRoomConfirmDialog: confirmDialog,
            ActiveRoomInitial:      initial,
        }
        h.tmpl.render(w, "rooms", data)
    }
    ```
  - [ ] 4.2 Add `UpdateRoomNameHandler`:
    ```go
    // UpdateRoomNameHandler handles POST /admin/rooms/{roomId}/name.
    // Validates and updates the room's name in-memory (stub phase).
    // TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
    func (h *RoomsHandler) UpdateRoomNameHandler(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        // TODO(story-7-csrf): enforce CSRF middleware when wiring in production
        if err := r.ParseForm(); err != nil {
            http.Error(w, "bad request", http.StatusBadRequest)
            return
        }
        name := strings.TrimSpace(r.FormValue("name"))
        if name == "" || utf8.RuneCountInString(name) > 100 {
            http.Error(w, "name must be 1–100 characters", http.StatusBadRequest)
            return
        }
        for i := range stubRooms {
            if stubRooms[i].ID == roomID {
                stubRooms[i].Name = name
                break
            }
        }
        http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Name+updated", http.StatusFound)
    }
    ```
  - [ ] 4.3 Add `ArchiveRoomHandler`:
    ```go
    // ArchiveRoomHandler handles POST /admin/rooms/{roomId}/archive.
    // Sets Status = "archived" in-memory (stub phase).
    // TODO(epic-6): replace stub mutation with Admin API call (POST /api/v1/admin/rooms/{roomId}/archive).
    // TODO(story-7-csrf): enforce CSRF middleware when wiring in production.
    func (h *RoomsHandler) ArchiveRoomHandler(w http.ResponseWriter, r *http.Request) {
        roomID := r.PathValue("roomId")
        for i := range stubRooms {
            if stubRooms[i].ID == roomID {
                stubRooms[i].Status = "archived"
                break
            }
        }
        http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+archived", http.StatusFound)
    }
    ```
  - [ ] 4.4 Add `import "unicode/utf8"` to `rooms.go` (alongside existing `"strings"`)

- [ ] Task 5: Rewrite `{{ define "detail_content" }}` and `{{ define "detail_footer" }}` in `rooms.html` — AC: 3, 4
  - [ ] 5.1 Replace `{{ define "detail_content" }}` block:
    ```html
    {{ define "detail_content" }}
      {{ if .ActiveRoom }}
        {{ if .Flash.Message }}
        {{ template "alert_banner" .Flash }}
        {{ end }}

        {{/* Avatar placeholder with room initial */}}
        <div class="flex justify-center mb-4">
          <div class="avatar placeholder">
            <div class="bg-neutral text-neutral-content rounded-full w-16">
              <span class="text-2xl">{{ .ActiveRoomInitial }}</span>
            </div>
          </div>
        </div>

        <dl class="divide-y divide-base-300">
          <div class="py-3">
            <dt class="text-base-content/60 text-xs uppercase tracking-wide mb-1">Name</dt>
            <dd>
              {{ template "inline_edit" .ActiveRoomInlineEdit }}
            </dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Visibility</dt>
            <dd class="text-sm font-medium">{{ .ActiveRoom.Visibility }}</dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Members</dt>
            <dd class="text-sm font-medium">{{ .ActiveRoom.MemberCount }}</dd>
          </div>
          <div class="flex justify-between py-3">
            <dt class="text-base-content/60 text-sm">Status</dt>
            <dd>
              {{ template "status_badge" .ActiveRoomStatusBadge }}
            </dd>
          </div>
        </dl>
      {{ end }}
    {{ end }}
    ```
  - [ ] 5.2 Replace `{{ define "detail_footer" }}` block:
    ```html
    {{ define "detail_footer" }}
      {{ if .ActiveRoom }}
        {{ if eq .ActiveRoom.Status "active" }}
        <button type="button" onclick="confirm_dialog.showModal()" class="btn btn-sm btn-warning">Archive room</button>
        {{ else }}
        <span class="badge badge-ghost">Archived</span>
        {{ end }}
        {{ template "confirm_dialog" .ActiveRoomConfirmDialog }}
      {{ end }}
    {{ end }}
    ```
  - [ ] 5.3 Remove the dead `{{ else if .ActiveItemID }}` branch that was present in the stub `detail_content` (unreachable after the 404 behaviour change in `DetailHandler`)

- [ ] Task 6: Add routes in `main.go` — AC: 7
  - [ ] 6.1 In `gateway/cmd/gateway/main.go`, after the existing `GET /admin/rooms/{roomId}` line:
    ```go
    // Story 7.9: Room name update and archive — no csrf() wrapper (stub phase; see TODO in handler).
    mux.Handle("POST /admin/rooms/{roomId}/name", sessionGuard(http.HandlerFunc(roomsHandler.UpdateRoomNameHandler)))
    mux.Handle("POST /admin/rooms/{roomId}/archive", sessionGuard(http.HandlerFunc(roomsHandler.ArchiveRoomHandler)))
    ```

- [ ] Task 7: Verify no regressions — AC: all
  - [ ] 7.1 Run `make test-unit-go` — all existing tests pass; 8 new detail tests pass
  - [ ] 7.2 Verify `rooms_page_test.go` and `master_detail_test.go` still pass (no struct changes that break them)
  - [ ] 7.3 Verify `make build-gateway` exits 0
  - [ ] 7.4 Run Playwright: `npx playwright test room-detail.spec.ts` — all 4 tests green against running dev stack

## Dev Notes

### 404 Behaviour Change

Story 7.2 `DetailHandler` returned HTTP 200 with a "Room not found" message inside the panel for unknown IDs. This story changes it to HTTP 404. The pattern mirrors Story 7.6 (User Detail Panel) exactly. Check whether any Story 7.2 test asserts the 200 status for unknown room IDs — if so, update the expectation to 404.

### `RoomsPageData` Additional Fields for Story 7.9

Beyond the five new fields added to `RoomsPageData`, all existing fields (`Rooms`, `SearchInput`, `FilterBar`, `TotalCount`, `CurrentPage`, `HasMore`, `NextPage`, `EmptyState`, `ActiveItemID`, `ActiveRoom`, `CloseURL`) remain unchanged. The new fields are zero-valued in list mode — no template rendering side-effects because the `detail_content` block only renders when `.ActiveRoom != nil`.

### In-Memory Stub Mutation

`stubRooms` is a package-level `var` slice in `stubs.go`. Mutating `stubRooms[i].Name` in `UpdateRoomNameHandler` and `stubRooms[i].Status` in `ArchiveRoomHandler` is safe for the stub phase.

Test isolation — Go unit tests must not assume `stubRooms` is unmodified after a POST test:
- `TestUpdateRoomName`: save `stubRooms[0].Name` (room-001) and restore in `t.Cleanup`
- `TestArchiveRoom`: save `room-002` status; use `t.Cleanup` to restore to `"active"`. Use `room-002` (not `room-001`) to avoid interfering with name-update tests. The stub slice index for `room-002` is 1 — either use the index directly (`stubRooms[1].Status`) or look it up by ID via `findStubRoom`.

### `inline_edit` Component: `FieldName` vs `ID`

The `InlineEditData` struct has both `ID` (unique CSS/ARIA identifier) and `FieldName` (the `<input name="...">` attribute). For rooms:
- `ID: "room-name"` — used for the CSS toggle and `aria-controls` correlation
- `FieldName: "name"` — the POST form field; `UpdateRoomNameHandler` reads `r.FormValue("name")`

The `TestRoomDetailPanelRenders` test checks for `inline-edit-field` (the outer `<div>` class rendered by `inline_edit.html`). This is the same assertion pattern as `TestUserDetailPanelRenders`.

### `ConfirmDialogData` — Single Dialog Per Page

The `<dialog>` element uses `id="confirm_dialog"`. There is only one confirm dialog on the room detail page (archive), so no ID collision issue. The trigger button uses `onclick="confirm_dialog.showModal()"` — identical to the User deactivation dialog (Story 7.7). The `TestArchiveConfirmDialogRendered` test checks for `role="alertdialog"`.

### Status Mapping Consistency

`DetailHandler` uses the same `"archived"` → `"inactive"` normalisation as `toRoomRowData`. The pre-computed `ActiveRoomStatusBadge` field avoids re-implementing this mapping in the template.

### rune-Safe Initials

`ActiveRoomInitial` uses `[]rune(room.Name)[0:1]` — the same pattern as `ActiveUserInitial` in `users.go`. This avoids the UTF-8 byte-slice edge case when room names start with multi-byte characters (e.g. "Ärger"). The template uses `{{ .ActiveRoomInitial }}` directly.

### `unicode/utf8` Import

`UpdateRoomNameHandler` uses `utf8.RuneCountInString(name)` for the 100-rune max-length validation — the same import and pattern as `UpdateDisplayNameHandler` in `users.go`. Add `"unicode/utf8"` to the import block in `rooms.go`.

### Playwright Test: `inline edit saves room name`

The inline edit test navigates to `/admin/rooms/room-001` and triggers the edit flow. After save, the PRG redirect lands on `/admin/rooms/room-001?flash=Name+updated`. The test does NOT assert the new name appears in the list (that would require re-render of the list, which the stub doesn't persist across redirects in the same browser session — the in-memory stub is mutated, so the name WILL appear; but asserting the flash is sufficient for this story).

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/page_data.go` | Modify — add `Flash`, `ActiveRoomInlineEdit`, `ActiveRoomStatusBadge`, `ActiveRoomConfirmDialog`, `ActiveRoomInitial` to `RoomsPageData` |
| `gateway/internal/admin/rooms.go` | Modify — extend `DetailHandler` (404 + flash + pre-computed fields), add `UpdateRoomNameHandler`, `ArchiveRoomHandler`; add `unicode/utf8` import |
| `gateway/internal/admin/templates/rooms.html` | Modify — rewrite `detail_content` and `detail_footer` blocks |
| `gateway/internal/admin/rooms_detail_test.go` | NEW — 8 Go unit tests |
| `gateway/cmd/gateway/main.go` | Modify — add `POST /admin/rooms/{roomId}/name` and `POST /admin/rooms/{roomId}/archive` routes |
| `e2e/tests/features/admin/room-detail.spec.ts` | NEW — 4 Playwright E2E tests (all REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.13** (WCAG audit): axe-core scan will run against `/admin/rooms/room-001`; detail panel accessibility must be clean
- **Story 7.14** (Gherkin smoke flows): includes room-archive UI flow; depends on the confirm_dialog wired here

## Status Notes

Created: 2026-04-29. Stories 7.1 through 7.8 are done. This story fills in the `{{ define "detail_content" }}` and `{{ define "detail_footer" }}` blocks for the rooms detail panel — the room equivalent of Stories 7.6 (User Detail Panel) and 7.7 (User Role UI + Deactivation). The User Detail Panel (Story 7.6) is the direct reference implementation. Pattern: pre-compute all component data structs in `DetailHandler`, pass via `RoomsPageData`, render in template via `{{ template "..." .ActiveRoom* }}`. Playwright tests are REAL (not `test.skip`) because the detail route `/admin/rooms/{id}` has been live since Story 7.2.

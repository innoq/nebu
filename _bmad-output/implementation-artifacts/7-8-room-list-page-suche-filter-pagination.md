---
id: 7-8
security_review: not-needed
---

# Story 7.8: Room List Page (Search, Filter, Pagination)

Status: ready-for-dev

## Story

As an instance admin,
I want a fully functional `/admin/rooms` page with search, visibility filtering, and paginated results,
so that I can quickly find and inspect rooms on the instance without scrolling through an unfiltered list.

## Acceptance Criteria

1. **Handler upgrade** — `gateway/internal/admin/rooms.go`: `RoomsHandler.ListHandler` is extended to:
   - Read query params `q` (search string), `visibility` (filter: `"all"` | `"public"` | `"private"`, default `"all"`), and `page` (int, default 0)
   - Filter `stubRooms` by `q` (case-insensitive substring match on `Name`) and by `visibility` param
   - Paginate the filtered slice: page size 5, `HasMore` is true when `len(filtered) > (page+1)*5`
   - Populate `RoomsPageData` with the new fields `Rooms`, `SearchInput`, `FilterBar`, `EmptyState`, `TotalCount`, `CurrentPage`, `HasMore`, `NextPage`
   - Render the `rooms` template via `h.tmpl.render(w, "rooms", data)`

2. **`RoomRowData`** — `gateway/internal/admin/page_data.go`: Add a new `RoomRowData` struct (same pattern as `UserRowData`):
   - Embeds `StubRoom`
   - Adds `Badge StatusBadgeData` — pre-computed by the handler: `Status: "active"` → `StatusBadgeData{Status: "active"}`, `Status: "archived"` → `StatusBadgeData{Status: "inactive"}`

3. **`RoomsPageData` extended** — `gateway/internal/admin/page_data.go`: Replace the existing `RoomsPageData` definition with one that adds:
   - `Rooms []RoomRowData` — the filtered, paginated slice (replaces `StubRooms`)
   - `SearchInput SearchInputData` — pre-filled with current `q` and `name="q"`
   - `FilterBar []FilterOption` — single `FilterOption` for `visibility` with options `["all", "public", "private"]`
   - `TotalCount int` — total matching rooms (before pagination)
   - `CurrentPage int` — current 0-indexed page
   - `HasMore bool` — whether a next page exists
   - `NextPage int` — `CurrentPage + 1` (avoids FuncMap `add` helper in templates)
   - `EmptyState EmptyStateData` — populated when `Rooms` is empty
   - Existing fields `PageData`, `ActiveItemID`, `ActiveRoom`, `CloseURL` remain unchanged

4. **`rooms.html` template upgrade** — `gateway/internal/admin/templates/rooms.html`: Replace the `{{ define "master_list" }}` block with a full-featured version:
   - `<main>` landmark wrapping all list content (WCAG)
   - `<h1 class="...">Rooms</h1>` visible heading
   - A `<form method="GET" action="/admin/rooms">` wrapping `{{ template "search_input" .SearchInput }}` and `{{ template "filter_bar" .FilterBar }}`
   - Room list `<ul role="listbox" data-loading-skeleton ...>` with `{{ template "status_badge" .Badge }}` per row (via `RoomRowData`)
   - When `.Rooms` is empty: `{{ template "empty_state" .EmptyState }}` with `Heading: "No rooms found"`, `Description: "Try adjusting your search or filter."`
   - `<nav aria-label="pagination">` load-more link when `.HasMore` is true, using `.NextPage`
   - `{{ define "detail_title" }}`, `{{ define "detail_content" }}`, `{{ define "detail_footer" }}` blocks remain unchanged from Story 7.2

5. **Go unit tests** — `gateway/internal/admin/rooms_page_test.go` (`package admin`):
   - `TestRoomsPageRenders` — `GET /admin/rooms` → HTTP 200, body contains `<h1`, `<main`
   - `TestRoomsPageSearch` — `GET /admin/rooms?q=general` → body contains "General", does NOT contain "Engineering"
   - `TestRoomsPageVisibilityFilter` — `GET /admin/rooms?visibility=private` → body contains "Engineering", does NOT contain "General" (General is `public`)
   - `TestRoomsPageEmptyState` — `GET /admin/rooms?q=zzznomatch` → body contains "No rooms found"
   - `TestRoomsPageStatusBadge` — `GET /admin/rooms` → body contains `badge-success` (active rooms) AND `badge-error` (archived — "Old Project X" has `Status: "archived"`)
   - `TestRoomsPagePagination` — `GET /admin/rooms` → page 0 with all 5 stubs, `HasMore = false`; assert body does NOT contain `aria-label="pagination"` (no nav rendered)

6. **Playwright E2E tests** — `e2e/tests/features/admin/rooms-page.spec.ts` — **REAL tests (not `test.skip`)**:
   - `search input debounces and updates URL` — navigate to `/admin/rooms`, type `general`, wait for URL to contain `q=general` (≤2s timeout); verify "General" is visible in results
   - `visibility filter triggers immediate form submit` — navigate to `/admin/rooms`, select `private` from visibility dropdown, URL contains `visibility=private`
   - `empty state shown when no results` — navigate to `/admin/rooms?q=zzznomatch`, see `<h3>` with text matching `/no rooms/i`
   - `status badge shows correct color for active room` — navigate to `/admin/rooms`, first `span[role="status"]` has class `badge-success`
   - Auth: `loginAsAdmin` helper (OIDC Authorization Code + PKCE, same pattern as `master-detail.spec.ts`)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **RoomsPageRenders — GET /admin/rooms returns 200 with WCAG landmarks** — Go `net/http/httptest` (`gateway/internal/admin/rooms_page_test.go`)
   - Given: `RoomsHandler.ListHandler` wired to a test HTTP server
   - When: `GET /admin/rooms` (no params)
   - Then: HTTP 200, body contains `<h1`, body contains `<main`

2. **RoomsPageSearch — q param filters to matching rooms** — Go `net/http/httptest`
   - Given: handler with full `stubRooms` (includes "General" and "Engineering")
   - When: `GET /admin/rooms?q=general`
   - Then: body contains "General", body does NOT contain "Engineering"

3. **RoomsPageVisibilityFilter — visibility param filters to matching rooms** — Go `net/http/httptest`
   - Given: handler with full `stubRooms` ("General" is `public`, "Engineering" is `private`)
   - When: `GET /admin/rooms?visibility=private`
   - Then: body contains "Engineering", body does NOT contain "General"

4. **RoomsPageEmptyState — no-match query shows empty state** — Go `net/http/httptest`
   - Given: handler with full `stubRooms`
   - When: `GET /admin/rooms?q=zzznomatch`
   - Then: body contains "No rooms found"

5. **RoomsPageStatusBadge — badge class matches room status** — Go `net/http/httptest`
   - Given: handler renders the full stub list (includes active and archived rooms; "Old Project X" has `Status: "archived"`)
   - When: `GET /admin/rooms`
   - Then: body contains `badge-success` AND body contains `badge-error`

6. **RoomsPagePagination — 5 stubs fit on page 0, no pagination nav rendered** — Go `net/http/httptest`
   - Given: handler with full `stubRooms` (5 entries, pageSize=5)
   - When: `GET /admin/rooms` (page 0, no filter)
   - Then: body does NOT contain `aria-label="pagination"` (HasMore=false → no `<nav>` rendered)

7. **Playwright: search input debounces and updates URL** — Playwright (`e2e/tests/features/admin/rooms-page.spec.ts`)
   - Given: full dev stack running, admin logged in
   - When: navigate to `/admin/rooms`, fill `general` in the search input
   - Then: URL changes to contain `q=general` within 2s; "General" is visible in the list

8. **Playwright: visibility filter triggers immediate form submit** — Playwright
   - Given: admin on `/admin/rooms`
   - When: select `private` from the visibility filter dropdown
   - Then: URL contains `visibility=private`

9. **Playwright: empty state heading visible when no results** — Playwright
   - Given: admin on `/admin/rooms?q=zzznomatch`
   - When: page renders
   - Then: `<h3>` element with text matching `/no rooms/i` is visible

10. **Playwright: status badge for active room has badge-success class** — Playwright
    - Given: admin on `/admin/rooms`
    - When: page renders
    - Then: first `span[role="status"]` element has class `badge-success`

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 5
  - [ ] 1.1 Create `gateway/internal/admin/rooms_page_test.go` in `package admin`
  - [ ] 1.2 Import: `"net/http/httptest"`, `"net/http"`, `"strings"`, `"testing"`
  - [ ] 1.3 Write `TestRoomsPageRenders`:
    - Setup: `NewTemplateHandler()` + `NewRoomsHandler(tmpl)`; `httptest.NewRecorder()`
    - Request: `GET /admin/rooms`
    - Assert: `w.Code == 200`, `strings.Contains(body, "<h1")`, `strings.Contains(body, "<main")`
  - [ ] 1.4 Write `TestRoomsPageSearch`:
    - Request: `GET /admin/rooms?q=general`
    - Assert: body contains "General", does NOT contain "Engineering"
  - [ ] 1.5 Write `TestRoomsPageVisibilityFilter`:
    - Request: `GET /admin/rooms?visibility=private`
    - Assert: body contains "Engineering", does NOT contain "General"
  - [ ] 1.6 Write `TestRoomsPageEmptyState`:
    - Request: `GET /admin/rooms?q=zzznomatch`
    - Assert: body contains "No rooms found"
  - [ ] 1.7 Write `TestRoomsPageStatusBadge`:
    - Request: `GET /admin/rooms`
    - Assert: body contains `badge-success` AND `badge-error`
    - Note: "Old Project X" (`room-004`, `Status: "archived"`) must map to `StatusBadgeData{Status: "inactive"}` → `badge-error`
  - [ ] 1.8 Write `TestRoomsPagePagination`:
    - Request: `GET /admin/rooms` (5 stubs, pageSize=5)
    - Assert: body does NOT contain `aria-label="pagination"` (HasMore=false → no nav rendered)
  - [ ] 1.9 Confirm RED: `go test ./internal/admin/...` fails because `RoomsPageData` lacks new fields and `rooms.html` lacks `<main>`/`<h1>`

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 6
  - [ ] 2.1 Create `e2e/tests/features/admin/rooms-page.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `master-detail.spec.ts` (identical pattern)
  - [ ] 2.3 Write `test('search input debounces and updates URL')` — NOT test.skip:
    - `loginAsAdmin(page)`, `goto('/admin/rooms')`, `fill('general')` into `input[name="q"]`
    - `expect(page).toHaveURL(/[?&]q=general/, { timeout: 2_000 })`
    - `expect(page.getByText('General')).toBeVisible()`
  - [ ] 2.4 Write `test('visibility filter triggers immediate form submit')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms')`
    - `page.locator('select[name="visibility"]').selectOption('private')`
    - `expect(page).toHaveURL(/[?&]visibility=private/)`
  - [ ] 2.5 Write `test('empty state shown when no results')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms?q=zzznomatch')`
    - `expect(page.locator('h3').filter({ hasText: /no rooms/i }).first()).toBeVisible()`
  - [ ] 2.6 Write `test('status badge shows correct color for active room')`:
    - `loginAsAdmin(page)`, `goto('/admin/rooms')`
    - `const badge = page.locator('span[role="status"]').first()`
    - `expect(badge).toBeVisible()`
    - `expect(badge).toHaveClass(/badge-success/)`
  - [ ] 2.7 Confirm RED: running `npx playwright test rooms-page.spec.ts` fails (page lacks `<main>`, search/filter form, etc.)

- [ ] Task 3: Add `RoomRowData` and extend `RoomsPageData` in `page_data.go` — AC: 2, 3
  - [ ] 3.1 Add `RoomRowData` struct to `page_data.go` after `UserRowData`:
    ```go
    // RoomRowData is used in the Rooms list template (Story 7.8).
    // Embeds StubRoom and adds a pre-computed Badge for the status_badge component,
    // normalising StubRoom.Status "archived" → StatusBadgeData{Status: "inactive"}.
    // This avoids template FuncMap helpers — the handler populates Badge directly.
    type RoomRowData struct {
        StubRoom
        Badge StatusBadgeData
    }
    ```
  - [ ] 3.2 Replace the existing `RoomsPageData` definition:
    ```go
    // RoomsPageData holds data for the Rooms master-detail page (Story 7.8).
    // Rooms is the filtered+paginated slice (replaces StubRooms from Story 7.2).
    // SearchInput, FilterBar, TotalCount, CurrentPage, HasMore, NextPage support search/filter/pagination.
    // EmptyState is populated by the handler and rendered when Rooms is empty.
    // ActiveItemID, ActiveRoom, CloseURL remain for the MasterDetail detail panel (Story 7.2/7.9).
    type RoomsPageData struct {
        PageData
        Rooms        []RoomRowData
        SearchInput  SearchInputData
        FilterBar    []FilterOption
        TotalCount   int
        CurrentPage  int
        HasMore      bool
        NextPage     int
        EmptyState   EmptyStateData
        ActiveItemID string
        ActiveRoom   *StubRoom // nil = list view or not found
        CloseURL     string    // e.g. "/admin/rooms"
    }
    ```
  - [ ] 3.3 Verify that `master_detail_test.go` (Story 7.2) and any other tests using `RoomsPageData.StubRooms` still compile. Update references from `StubRooms: stubRooms` to `Rooms: toRoomRowDataSlice(stubRooms)` (or equivalent) where needed. This is a compile-time error — the test suite will not compile until fixed.

- [ ] Task 4: Upgrade `RoomsHandler` in `rooms.go` — AC: 1
  - [ ] 4.1 Add `filterStubRooms` private helper (in `rooms.go`):
    - Accepts `rooms []StubRoom`, `q string`, `visibility string`
    - `q` filter: case-insensitive substring match on `Name` using `strings.Contains(strings.ToLower(r.Name), strings.ToLower(q))`
    - `visibility` filter: `"all"` → no filter; `"public"` → `r.Visibility == "public"`; `"private"` → `r.Visibility == "private"`
    - Returns filtered `[]StubRoom`
  - [ ] 4.2 Add `toRoomRowData(r StubRoom) RoomRowData` helper:
    - Maps `Status: "active"` → `StatusBadgeData{Status: "active"}`
    - Maps `Status: "archived"` → `StatusBadgeData{Status: "inactive"}`
  - [ ] 4.3 Extend `ListHandler`:
    ```go
    func (h *RoomsHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
        q := r.URL.Query().Get("q")
        visibility := r.URL.Query().Get("visibility")
        if visibility == "" { visibility = "all" }
        page := 0
        if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n >= 0 { page = n }

        filtered := filterStubRooms(stubRooms, q, visibility)
        const pageSize = 5
        start := page * pageSize
        end := start + pageSize
        hasMore := end < len(filtered)
        if start > len(filtered) { start = len(filtered) }
        if end > len(filtered) { end = len(filtered) }
        paged := filtered[start:end]

        rows := make([]RoomRowData, len(paged))
        for i, room := range paged {
            rows[i] = toRoomRowData(room)
        }

        data := RoomsPageData{
            PageData:    PageData{ActiveNav: "rooms", CSRFToken: CSRFTokenFromContext(r.Context())},
            Rooms:       rows,
            SearchInput: SearchInputData{Placeholder: "Search rooms…", Value: q, ParamName: "q"},
            FilterBar: []FilterOption{{
                Label:        "Visibility",
                ParamName:    "visibility",
                Options:      []string{"all", "public", "private"},
                CurrentValue: visibility,
            }},
            TotalCount:  len(filtered),
            CurrentPage: page,
            HasMore:     hasMore,
            NextPage:    page + 1,
            EmptyState:  EmptyStateData{Heading: "No rooms found", Description: "Try adjusting your search or filter."},
            CloseURL:    "/admin/rooms",
        }
        h.tmpl.render(w, "rooms", data)
    }
    ```
  - [ ] 4.4 Update `DetailHandler` to use `Rooms: toRoomRowDataSlice(stubRooms)` (or pass the full list pre-converted) — `StubRooms` field no longer exists
  - [ ] 4.5 Add `import "strconv"` and `import "strings"` to `rooms.go`

- [ ] Task 5: Upgrade `rooms.html` template — AC: 4
  - [ ] 5.1 Replace `{{ define "master_list" }}` block in `gateway/internal/admin/templates/rooms.html`:
    ```html
    {{ define "master_list" }}
    <main class="flex flex-col h-full">
      <div class="p-4 border-b border-base-300">
        <h1 class="text-lg font-semibold mb-3">Rooms</h1>
        <form method="GET" action="/admin/rooms" class="flex flex-col gap-2">
          {{ template "search_input" .SearchInput }}
          {{ template "filter_bar" .FilterBar }}
        </form>
      </div>

      <div class="flex-1 overflow-y-auto">
        {{ if .Rooms }}
        <ul role="listbox" aria-label="Rooms" data-loading-skeleton class="p-2">
          {{ range .Rooms }}
          <li>
            <a href="/admin/rooms/{{ .ID }}"
               role="option"
               aria-selected="{{ if eq .ID $.ActiveItemID }}true{{ else }}false{{ end }}"
               class="flex items-center gap-3 px-3 py-3 rounded text-sm
                      {{ if eq .ID $.ActiveItemID }}active bg-primary text-primary-content border-l-[3px] border-primary{{ else }}text-base-content hover:bg-base-300{{ end }}
                      focus:outline-none focus:ring-2 focus:ring-primary">
              <span class="font-medium flex-1 truncate">{{ .Name }}</span>
              {{ template "status_badge" .Badge }}
            </a>
          </li>
          {{ end }}
        </ul>
        {{ else }}
        {{ template "empty_state" .EmptyState }}
        {{ end }}
      </div>

      {{ if .HasMore }}
      <nav aria-label="pagination" class="p-3 border-t border-base-300 text-center">
        <a href="/admin/rooms?q={{ .SearchInput.Value }}&visibility={{ (index .FilterBar 0).CurrentValue }}&page={{ .NextPage }}"
           class="btn btn-ghost btn-sm">Load more</a>
      </nav>
      {{ end }}
    </main>
    {{ end }}
    ```
  - [ ] 5.2 Keep `{{ define "detail_title" }}`, `{{ define "detail_content" }}`, `{{ define "detail_footer" }}` blocks unchanged from Story 7.2

- [ ] Task 6: Verify no regressions — AC: all
  - [ ] 6.1 Run `make test-unit-go` — all existing tests pass; 6 new rooms page tests pass
  - [ ] 6.2 Verify `master_detail_test.go` still compiles — fix any `StubRooms` → `Rooms` references
  - [ ] 6.3 Verify `make build-gateway` exits 0
  - [ ] 6.4 Run Playwright: `npx playwright test rooms-page.spec.ts` — all 4 tests green against running dev stack

## Dev Notes

### Stub Data for Rooms

`stubRooms` has exactly 5 entries (room-001 through room-005):

| ID | Name | Visibility | Status |
|---|---|---|---|
| room-001 | General | public | active |
| room-002 | Engineering | private | active |
| room-003 | Compliance-Team | private | active |
| room-004 | Old Project X | private | archived |
| room-005 | Announcements | public | active |

With `pageSize = 5` and 5 total stubs: page 0 shows all 5, `HasMore = false`, no pagination `<nav>` is rendered. `TestRoomsPagePagination` asserts the nav is absent.

### Visibility Filter Mapping

| URL param `visibility=` | Filter logic |
|---|---|
| `all` | no filter — show all rooms |
| `public` | `room.Visibility == "public"` |
| `private` | `room.Visibility == "private"` |

### Status Mapping for StatusBadge

`StubRoom.Status` uses `"archived"` but `StatusBadgeData.Status` expects `"inactive"` (same normalisation pattern as Story 7.5):

```
"active"   → StatusBadgeData{Status: "active"}   → badge-success
"archived" → StatusBadgeData{Status: "inactive"}  → badge-error
```

`TestRoomsPageStatusBadge` verifies both `badge-success` and `badge-error` appear in the rendered body (room-004 "Old Project X" is archived).

### RoomRowData Struct Approach (mirrors UserRowData)

```go
// RoomRowData is used in the Rooms list template (Story 7.8).
// Embeds StubRoom and adds a pre-computed Badge for the status_badge component,
// normalising StubRoom.Status "archived" → StatusBadgeData{Status: "inactive"}.
type RoomRowData struct {
    StubRoom
    Badge StatusBadgeData
}
```

The handler converts `[]StubRoom` → `[]RoomRowData` before populating `RoomsPageData.Rooms`. The template calls `{{ template "status_badge" .Badge }}` per row — no FuncMap needed.

### `NextPage` Field (mirrors UsersPageData)

Add `NextPage int` to `RoomsPageData` (set to `CurrentPage + 1` in the handler). The load-more link uses `{{ .NextPage }}` directly — no `add` FuncMap helper needed.

### `toRoomRowData` Helper (mirrors `toUserRowData`)

Add a private `toRoomRowData(r StubRoom) RoomRowData` helper in `rooms.go`. This mirrors the `toUserRowData` helper from Story 7.5 (`users.go`). Centralising badge normalisation avoids duplication across `ListHandler` and `DetailHandler`.

If `DetailHandler` also needs to pass a room list (for the sidebar), it should convert via the same helper. The `DetailHandler` currently passes `StubRooms: stubRooms`; after this story it passes `Rooms: toRoomRowDataSlice(stubRooms)` (add a `toRoomRowDataSlice` convenience function).

### Template: `filter_bar` for Visibility

The `filter_bar` component renders one `<select>` per `FilterOption`. For this story, there is exactly one `FilterOption`:

```go
FilterOption{
    Label:        "Visibility",
    ParamName:    "visibility",
    Options:      []string{"all", "public", "private"},
    CurrentValue: visibility,
}
```

The `filter_bar` component auto-submits the parent form via `onchange="this.form.submit()"`.

### Pagination Test Assertion

With 5 stubs and `pageSize = 5`, `HasMore = false` on page 0. The pagination `<nav aria-label="pagination">` is only rendered when `HasMore = true`. `TestRoomsPagePagination` asserts `!strings.Contains(body, `aria-label="pagination"`)` to confirm no nav is rendered.

If the project later adds more stub rooms (>5), this test must be updated or the stub data must stay at 5 entries.

### master_detail_test.go Field Rename

Story 7.2's tests may reference `RoomsPageData.StubRooms`. After this story replaces `StubRooms []StubRoom` with `Rooms []RoomRowData`, any such references become compile-time errors. The dev agent must update these references. The `toRoomRowDataSlice` helper is useful here.

### What NOT to Do

- **Do NOT add pagination to `DetailHandler`** — detail view always shows the room; pagination is list-only
- **Do NOT implement JS infinite scroll** — load-more is a `?page=N` GET link only
- **Do NOT implement skeleton CSS/JS** — `data-loading-skeleton` attribute is sufficient for this story
- **Do NOT use `strings.EqualFold`** — use `strings.Contains(strings.ToLower(...), strings.ToLower(q))` for substring search
- **Do NOT add search to the detail view** — search state is list-view only

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/rooms.go` | Modify — extend `ListHandler`, add `filterStubRooms`, `toRoomRowData`, `toRoomRowDataSlice` |
| `gateway/internal/admin/page_data.go` | Modify — add `RoomRowData`, replace `RoomsPageData` |
| `gateway/internal/admin/templates/rooms.html` | Modify — rebuild `master_list` block |
| `gateway/internal/admin/rooms_page_test.go` | NEW — 6 Go unit tests |
| `gateway/internal/admin/master_detail_test.go` | Modify — update `StubRooms` → `Rooms` references if present |
| `e2e/tests/features/admin/rooms-page.spec.ts` | NEW — 4 Playwright E2E tests (all REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.9** (Room Detail Panel): uses the same `RoomsPageData` with `ActiveItemID` / `ActiveRoom`; detail panel in `rooms.html` must remain functional
- **Story 7.14** (Gherkin Admin UI smoke flows): includes room-archive UI flow; depends on the room list being navigable

## Status Notes

Created: 2026-04-29. Stories 7.1 through 7.7 are done. This story upgrades the `/admin/rooms` page to a full-featured list with search, visibility filter, pagination, WCAG landmarks, and proper `StatusBadge`/`EmptyState` integration. All required component templates (C8 search_input, C9/C10 filter_bar, C13 status_badge, C14 empty_state) are already implemented — this story only wires them into the rooms page. The pattern mirrors Story 7.5 (User List Page) exactly: same `RowData` approach, same `FilterBar` slice, same `NextPage` field, same Playwright E2E structure. Playwright tests are REAL (not `test.skip`) because a production page exists to test against.

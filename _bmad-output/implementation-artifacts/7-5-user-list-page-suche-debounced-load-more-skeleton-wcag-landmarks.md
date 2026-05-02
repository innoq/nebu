---
id: 7-5
security_review: not-needed
---

# Story 7.5: User List Page (Search Debounced, Load More / Skeleton, WCAG Landmarks)

Status: done

## Story

As an instance admin,
I want a fully functional `/admin/users` page with search, role filtering, and paginated results,
so that I can quickly find and inspect users on large instances without waiting for a full page reload.

## Acceptance Criteria

1. **Handler upgrade** — `gateway/internal/admin/users.go`: `UsersHandler.ListHandler` is extended to:
   - Read query params `q` (search string), `role` (filter: `"all"` | `"admin"` | `"user"` | `"compliance_officer"`, default `"all"`), and `page` (int, default 0)
   - Filter `stubUsers` by `q` (case-insensitive substring match on `DisplayName` or `Email`) and by `role` param (maps role filter values to `StubUser.Role` values per the mapping in Dev Notes)
   - Paginate the filtered slice: page size 5, `HasMore` is true when `len(filtered) > (page+1)*5`
   - Populate `UsersPageData` with the new fields `SearchInput`, `FilterBar`, `TotalCount`, `CurrentPage`, `HasMore`, and the filtered+paginated `Users` slice
   - Render the `users` template via `h.tmpl.render(w, "users", data)`

2. **`UsersPageData` extended** — `gateway/internal/admin/page_data.go`: Replace the existing `UsersPageData` definition with one that adds:
   - `Users []StubUser` — the filtered, paginated slice (replaces the old `StubUsers`)
   - `SearchInput SearchInputData` — pre-filled with current `q` and `name="q"`
   - `FilterBar []FilterOption` — single `FilterOption` for `role` with options `["all", "admin", "user", "compliance_officer"]`
   - `TotalCount int` — total matching users (before pagination)
   - `CurrentPage int` — current 0-indexed page
   - `HasMore bool` — whether a next page exists
   - Existing fields `PageData`, `ActiveItemID`, `ActiveUser`, `CloseURL` remain unchanged

3. **`users.html` template rebuild** — `gateway/internal/admin/templates/users.html` is rewritten to embed the new components:
   - Outer wrapper is `{{ template "master_detail" . }}` (unchanged — preserves Story 7.2 MasterDetail layout)
   - `{{ define "master_list" }}` now contains:
     - `<main>` landmark wrapping the list content (WCAG)
     - `<h1 class="...">Users</h1>` visible heading
     - A `<form method="GET" action="/admin/users">` wrapping the SearchInput and FilterBar
     - `{{ template "search_input" .SearchInput }}` — C8 with `name="q"` and 300ms debounce
     - `{{ template "filter_bar" .FilterBar }}` — C9/C10 with role select
     - User list `<ul role="listbox" ...>` with `data-loading-skeleton` attribute on the `<ul>` container
     - For each user in `.Users`: a `<li>` with `<a>` link (same `aria-selected` pattern as Story 7.2) and `{{ template "status_badge" ... }}` — C13, passing `StatusBadgeData{Status: mappedStatus}` where `StubUser.Status == "deactivated"` maps to `"inactive"` and `"active"` stays `"active"`
     - When `.Users` is empty: `{{ template "empty_state" .EmptyState }}` — C14 with `Heading: "No users found"`, `Description: "Try adjusting your search or filter."`
     - `<nav aria-label="pagination">` block below the list for load-more controls (WCAG)
     - Load-more: when `.HasMore`, render a link `<a href="/admin/users?q={{ .SearchInput.Value }}&role={{ current role }}&page={{ add .CurrentPage 1 }}">Load more</a>`
   - `{{ define "detail_title" }}`, `{{ define "detail_content" }}`, `{{ define "detail_footer" }}` remain as in Story 7.2 (detail panel is Story 7.6)

4. **URL bookmarkability**: All three params (`q`, `role`, `page`) appear in the URL, so that the browser back button and direct-URL access reproduce the same filtered/paginated view. Load-more link must carry all three params.

5. **Go unit tests** — `gateway/internal/admin/users_page_test.go` (`package admin`):
   - `TestUsersPageRenders` — `GET /admin/users` → HTTP 200, body contains `<h1`, `<main`
   - `TestUsersPageSearch` — `GET /admin/users?q=alice` → body contains "Alice Müller", does NOT contain "Bob Wagner"
   - `TestUsersPageRoleFilter` — `GET /admin/users?role=admin` → body contains only users whose Role maps to admin (Alice Müller with role "instance_admin"), does NOT contain "Carla Reiter" (role "user")
   - `TestUsersPageEmptyState` — `GET /admin/users?q=zzznomatch` → body contains "No users found"
   - `TestUsersPageStatusBadge` — `GET /admin/users` → first active user row contains `badge-success`, deactivated user row (Dieter Krause, usr-004) contains `badge-error`

6. **Playwright E2E tests** — `e2e/tests/features/admin/users-page.spec.ts` — **REAL tests (not `test.skip`)**:
   - `search input debounces and updates URL` — navigate to `/admin/users`, type `alice`, wait for URL to contain `q=alice` (≤2s timeout); also verify Alice Müller is visible in results
   - `role filter triggers immediate form submit` — navigate to `/admin/users`, select `admin` from role dropdown, URL contains `role=admin`
   - `empty state shown when no results` — navigate to `/admin/users?q=zzznomatch`, see `<h3>` with text matching `/no users/i`
   - `status badge shows correct color for active user` — navigate to `/admin/users`, first user row has a `span[role="status"]` with `badge-success` class
   - Auth: `loginAsAdmin` helper (OIDC Authorization Code + PKCE, same pattern as `master-detail.spec.ts`)

7. **Skeleton loading state**: The `<ul data-loading-skeleton ...>` attribute on the list container is sufficient for this story. No JavaScript implementation of skeleton rows is required — the attribute acts as an ATDD hook for future progressive-enhancement stories.

8. **Previously-skipped tests** in `e2e/tests/features/admin/interaction-components.spec.ts` for C8 SearchInput and C9/C10 FilterBar: these are owned by `users-page.spec.ts` in this story. The `test.skip` in `interaction-components.spec.ts` remains — they will be removed by the dev agent only if `users-page.spec.ts` coverage makes them redundant. This story does NOT modify `interaction-components.spec.ts`.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **UsersPageRenders — GET /admin/users returns 200 with WCAG landmarks** — Go `net/http/httptest` (`gateway/internal/admin/users_page_test.go`)
   - Given: `UsersHandler.ListHandler` wired to a test HTTP server
   - When: `GET /admin/users` (no params)
   - Then: HTTP 200, body contains `<h1`, body contains `<main`

2. **UsersPageSearch — q param filters to matching users** — Go `net/http/httptest`
   - Given: handler with full `stubUsers` (includes Alice Müller and Bob Wagner)
   - When: `GET /admin/users?q=alice`
   - Then: body contains "Alice Müller", body does NOT contain "Bob Wagner"

3. **UsersPageRoleFilter — role param filters to matching users** — Go `net/http/httptest`
   - Given: handler with full `stubUsers`
   - When: `GET /admin/users?role=admin`
   - Then: body contains "Alice Müller" (Role: "instance_admin" → maps to admin filter), body does NOT contain "Carla Reiter" (Role: "user")

4. **UsersPageEmptyState — no-match query shows empty state** — Go `net/http/httptest`
   - Given: handler with full `stubUsers`
   - When: `GET /admin/users?q=zzznomatch`
   - Then: body contains "No users found"

5. **UsersPageStatusBadge — badge class matches user status** — Go `net/http/httptest`
   - Given: handler renders the full stub list (includes active and deactivated users)
   - When: `GET /admin/users`
   - Then: body contains `badge-success` (active users) AND body contains `badge-error` (deactivated users — Dieter Krause is `Status: "deactivated"`)

6. **Playwright: search input debounces and updates URL** — Playwright (`e2e/tests/features/admin/users-page.spec.ts`)
   - Given: full dev stack running, admin logged in
   - When: navigate to `/admin/users`, fill `alice` in the search input
   - Then: URL changes to contain `q=alice` within 2s; "Alice Müller" is visible in the list

7. **Playwright: role filter triggers immediate form submit** — Playwright
   - Given: admin on `/admin/users`
   - When: select `admin` from the role filter dropdown
   - Then: URL contains `role=admin` (immediate submit via `onchange`)

8. **Playwright: empty state heading visible when no results** — Playwright
   - Given: admin on `/admin/users?q=zzznomatch`
   - When: page renders
   - Then: `<h3>` element with text matching `/no users/i` is visible

9. **Playwright: status badge for active user has badge-success class** — Playwright
   - Given: admin on `/admin/users`
   - When: page renders
   - Then: first `span[role="status"]` element has class `badge-success`

## Tasks / Subtasks

- [ ] Task 1: Write failing Go unit tests FIRST — AC: 5
  - [ ] 1.1 Create `gateway/internal/admin/users_page_test.go` in `package admin`
  - [ ] 1.2 Import: `"net/http/httptest"`, `"net/http"`, `"strings"`, `"testing"`
  - [ ] 1.3 Write `TestUsersPageRenders`:
    - Setup: `NewTemplateHandler()` + `NewUsersHandler(tmpl)`; `httptest.NewServer(http.HandlerFunc(h.ListHandler))`
    - Request: `GET /admin/users`
    - Assert: `w.Code == 200`, `strings.Contains(body, "<h1")`, `strings.Contains(body, "<main")`
  - [ ] 1.4 Write `TestUsersPageSearch`:
    - Request: `GET /admin/users?q=alice`
    - Assert: body contains "Alice Müller", does NOT contain "Bob Wagner"
  - [ ] 1.5 Write `TestUsersPageRoleFilter`:
    - Request: `GET /admin/users?role=admin`
    - Assert: body contains "Alice Müller", does NOT contain "Carla Reiter"
    - Note: "admin" filter maps to `StubUser.Role == "instance_admin"`
  - [ ] 1.6 Write `TestUsersPageEmptyState`:
    - Request: `GET /admin/users?q=zzznomatch`
    - Assert: body contains "No users found"
  - [ ] 1.7 Write `TestUsersPageStatusBadge`:
    - Request: `GET /admin/users`
    - Assert: body contains `badge-success` AND `badge-error`
    - Note: Dieter Krause (`usr-004`, `Status: "deactivated"`) must map to `StatusBadgeData{Status: "inactive"}` → `badge-error`
  - [ ] 1.8 Confirm RED: `go test ./internal/admin/...` fails because `UsersPageData` lacks new fields and `users.html` lacks `<main>`/`<h1>`

- [ ] Task 2: Write Playwright E2E tests FIRST — AC: 6
  - [ ] 2.1 Create `e2e/tests/features/admin/users-page.spec.ts`
  - [ ] 2.2 Copy `loginAsAdmin` helper from `master-detail.spec.ts` (identical pattern)
  - [ ] 2.3 Write `test('search input debounces and updates URL')` — NOT test.skip:
    - `loginAsAdmin(page)`, `goto('/admin/users')`, `fill('alice')` into `input[name="q"]`
    - `expect(page).toHaveURL(/[?&]q=alice/, { timeout: 2_000 })`
    - `expect(page.getByText('Alice Müller')).toBeVisible()`
  - [ ] 2.4 Write `test('role filter triggers immediate form submit')`:
    - `loginAsAdmin(page)`, `goto('/admin/users')`
    - `page.locator('select[name="role"]').selectOption('admin')`
    - `expect(page).toHaveURL(/[?&]role=admin/)`
  - [ ] 2.5 Write `test('empty state shown when no results')`:
    - `loginAsAdmin(page)`, `goto('/admin/users?q=zzznomatch')`
    - `expect(page.locator('h3').filter({ hasText: /no users/i }).first()).toBeVisible()`
  - [ ] 2.6 Write `test('status badge shows correct color for active user')`:
    - `loginAsAdmin(page)`, `goto('/admin/users')`
    - `const badge = page.locator('span[role="status"]').first()`
    - `expect(badge).toBeVisible()`
    - `expect(badge).toHaveClass(/badge-success/)`
  - [ ] 2.7 Confirm RED: running `npx playwright test users-page.spec.ts` fails (page lacks `<main>`, search input, etc.)

- [ ] Task 3: Extend `UsersPageData` in `page_data.go` — AC: 2
  - [ ] 3.1 Replace the existing `UsersPageData` struct with the extended version:
    ```go
    // UsersPageData holds data for the Users master-detail page (Story 7.5).
    // Users is the filtered+paginated slice (replaces StubUsers from Story 7.2).
    // SearchInput, FilterBar, TotalCount, CurrentPage, HasMore support search/filter/pagination.
    // EmptyState is populated by the handler when Users is empty.
    // ActiveItemID, ActiveUser, CloseURL remain for the MasterDetail detail panel (Story 7.2/7.6).
    type UsersPageData struct {
        PageData
        Users        []StubUser
        SearchInput  SearchInputData
        FilterBar    []FilterOption
        TotalCount   int
        CurrentPage  int
        HasMore      bool
        EmptyState   EmptyStateData // populated when Users is empty
        ActiveItemID string
        ActiveUser   *StubUser // nil = list view or not found
        CloseURL     string    // e.g. "/admin/users"
    }
    ```
  - [ ] 3.2 Verify that `master_detail_test.go` (Story 7.2) still compiles — it uses `StubUsers` field name. Update test data in `master_detail_test.go` to use `Users` instead of `StubUsers` if needed.

- [ ] Task 4: Extend `UsersHandler.ListHandler` in `users.go` — AC: 1
  - [ ] 4.1 Add search+filter+pagination logic to `ListHandler`:
    ```go
    func (h *UsersHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
        q := r.URL.Query().Get("q")
        role := r.URL.Query().Get("role")
        if role == "" { role = "all" }
        pageStr := r.URL.Query().Get("page")
        page := 0
        if n, err := strconv.Atoi(pageStr); err == nil && n >= 0 { page = n }

        filtered := filterStubUsers(stubUsers, q, role)
        const pageSize = 5
        start := page * pageSize
        end := start + pageSize
        hasMore := end < len(filtered)
        if start > len(filtered) { start = len(filtered) }
        if end > len(filtered) { end = len(filtered) }
        paged := filtered[start:end]

        data := UsersPageData{
            PageData:    PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
            Users:       paged,
            SearchInput: SearchInputData{Placeholder: "Search users…", Value: q, ParamName: "q"},
            FilterBar: []FilterOption{{
                Label:        "Role",
                ParamName:    "role",
                Options:      []string{"all", "admin", "compliance_officer", "user"},
                CurrentValue: role,
            }},
            TotalCount:  len(filtered),
            CurrentPage: page,
            HasMore:     hasMore,
            EmptyState:  EmptyStateData{Heading: "No users found", Description: "Try adjusting your search or filter."},
            CloseURL:    "/admin/users",
        }
        h.tmpl.render(w, "users", data)
    }
    ```
  - [ ] 4.2 Add `filterStubUsers` helper (private, in `users.go`):
    - Accepts `users []StubUser`, `q string`, `role string`
    - Returns filtered `[]StubUser`
    - `q` filter: case-insensitive substring match on `DisplayName` or `Email` (use `strings.Contains(strings.ToLower(u.DisplayName), strings.ToLower(q))`)
    - `role` filter mapping (see Dev Notes):
      - `"all"` → no filter
      - `"admin"` → `u.Role == "instance_admin"`
      - `"compliance_officer"` → `u.Role == "compliance_officer"`
      - `"user"` → `u.Role == "user"`
  - [ ] 4.3 Add `import "strconv"` and `import "strings"` to `users.go` (may already be present)
  - [ ] 4.4 Keep `DetailHandler` unchanged — it already uses the old `UsersPageData` fields; update it to use `Users: stubUsers` instead of `StubUsers: stubUsers`

- [ ] Task 5: Rebuild `users.html` template — AC: 3, 4, 7
  - [ ] 5.1 Replace `{{ define "master_list" }}` block in `gateway/internal/admin/templates/users.html`:
    ```html
    {{ define "master_list" }}
    <main class="flex flex-col h-full">
      <div class="p-4 border-b border-base-300">
        <h1 class="text-lg font-semibold mb-3">Users</h1>
        <form method="GET" action="/admin/users" class="flex flex-col gap-2">
          {{ template "search_input" .SearchInput }}
          {{ template "filter_bar" .FilterBar }}
        </form>
      </div>

      <div class="flex-1 overflow-y-auto">
        {{ if .Users }}
        <ul role="listbox" aria-label="Users" data-loading-skeleton class="p-2">
          {{ range .Users }}
          {{ $badgeStatus := "inactive" }}
          {{ if eq .Status "active" }}{{ $badgeStatus = "active" }}{{ end }}
          <li>
            <a href="/admin/users/{{ .ID }}"
               role="option"
               aria-selected="{{ if eq .ID $.ActiveItemID }}true{{ else }}false{{ end }}"
               class="flex items-center gap-3 px-3 py-3 rounded text-sm
                      {{ if eq .ID $.ActiveItemID }}active bg-primary text-primary-content border-l-[3px] border-primary{{ else }}text-base-content hover:bg-base-300{{ end }}
                      focus:outline-none focus:ring-2 focus:ring-primary">
              <span class="font-medium flex-1 truncate">{{ .DisplayName }}</span>
              {{ template "status_badge" (dict "Status" $badgeStatus "Label" .Status) }}
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
        <a href="/admin/users?q={{ .SearchInput.Value }}&role={{ (index .FilterBar 0).CurrentValue }}&page={{ add .CurrentPage 1 }}"
           class="btn btn-ghost btn-sm">Load more</a>
      </nav>
      {{ end }}
    </main>
    {{ end }}
    ```
  - [ ] 5.2 Keep `{{ define "detail_title" }}`, `{{ define "detail_content" }}`, `{{ define "detail_footer" }}` blocks unchanged from Story 7.2
  - [ ] 5.3 **Template helper note**: Go's `html/template` does NOT have a built-in `dict` or `add` function. The dev agent must either:
    - Option A: Add `template.FuncMap` with `"dict"` and `"add"` helpers to `NewTemplateHandler` in `handler.go` (preferred — minimal, already has FuncMap pattern if any), OR
    - Option B: Create a `StatusBadgeData` inline in the handler and pass it via a richer per-user struct, OR
    - Option C: Use a template-level variable: `{{ $badge := ... }}` per-row by pre-computing badge data in the handler (pass `[]UserRowData` instead of `[]StubUser`)
    - **Recommendation:** Option C — add a `UserRowData` struct to `page_data.go` that embeds `StubUser` plus a pre-computed `Badge StatusBadgeData`. The handler pre-populates it. This avoids FuncMap complexity and keeps templates data-driven.
  - [ ] 5.4 **`add` function for page param**: The load-more link needs `page + 1`. Options:
    - Add `"add"` to FuncMap: `"add": func(a, b int) int { return a + b }`
    - Or compute `NextPage int` in `UsersPageData` (cleanest)
    - **Recommendation:** Add `NextPage int` field to `UsersPageData` (computed as `CurrentPage + 1` in the handler). Then the template uses `{{ .NextPage }}` directly — no FuncMap needed.

- [ ] Task 6: Verify no regressions — AC: all
  - [ ] 6.1 Run `make test-unit-go` — all existing tests pass; 5 new users page tests pass (total ≥ existing + 5)
  - [ ] 6.2 Verify `master_detail_test.go` still passes — field rename `StubUsers` → `Users` may require updating test data structs
  - [ ] 6.3 Verify `make build-gateway` exits 0
  - [ ] 6.4 Run Playwright: `npx playwright test users-page.spec.ts` — all 4 tests green against running dev stack

## Dev Notes

### Role Filter Mapping

The `role` query param uses simplified values for the URL. The mapping to `StubUser.Role` is:

| URL param `role=` | `StubUser.Role` match |
|---|---|
| `all` | (no filter — show all) |
| `admin` | `"instance_admin"` |
| `compliance_officer` | `"compliance_officer"` |
| `user` | `"user"` |

The FilterBar `Options` slice should use these URL-friendly values: `["all", "admin", "compliance_officer", "user"]`.

### Status Mapping for StatusBadge

`StubUser.Status` uses `"deactivated"` but `StatusBadgeData.Status` expects `"inactive"` (see Story 7.4 Dev Notes). The handler (or template helper) MUST normalize:

```
"active"      → StatusBadgeData{Status: "active"}    → badge-success
"deactivated" → StatusBadgeData{Status: "inactive"}  → badge-error
```

The test `TestUsersPageStatusBadge` verifies that both `badge-success` and `badge-error` appear.

### Recommended: `UserRowData` Struct Approach

To avoid Go template FuncMap for `dict`, pre-compute per-row data in the handler:

```go
// UserRowData is used in the Users list template (Story 7.5).
// Badge is pre-computed from StubUser.Status by the handler,
// normalising "deactivated" → StatusBadgeData{Status: "inactive"}.
type UserRowData struct {
    StubUser
    Badge StatusBadgeData
}
```

The handler builds `[]UserRowData` and the template iterates over it. The `StatusBadge` is called as `{{ template "status_badge" .Badge }}` per row — no FuncMap needed. Add this struct to `page_data.go` and use `Users []UserRowData` in `UsersPageData`.

### Recommended: `NextPage` Field in `UsersPageData`

Add `NextPage int` to `UsersPageData` (set to `CurrentPage + 1`). The load-more link becomes:

```html
<a href="/admin/users?q={{ .SearchInput.Value }}&role={{ (index .FilterBar 0).CurrentValue }}&page={{ .NextPage }}">Load more</a>
```

No `add` FuncMap function needed.

### Pagination: Page Size and Stub Data

With 8 stub users and `pageSize = 5`:
- Page 0: users 0–4 (5 users), `HasMore = true`
- Page 1: users 5–7 (3 users), `HasMore = false`

The `TestUsersPageSearch?q=alice` test filters to 1 user (Alice Müller) — no pagination needed.

### `master_detail_test.go` Field Rename

Story 7.2's `master_detail_test.go` uses `StubUsers: threeUsers`. If `UsersPageData.StubUsers` is renamed to `Users`, these tests must be updated to `Users: threeUsers`. This is a compile-time error — the test suite will not compile until fixed. The dev agent must update these references.

### Template Interaction with `search_input` and `filter_bar`

Both C8 SearchInput and C9/C10 FilterBar are already implemented (Story 7.3). The search_input component does NOT include its own `<form>` — the page template must wrap it:

```html
<form method="GET" action="/admin/users">
  {{ template "search_input" .SearchInput }}
  {{ template "filter_bar" .FilterBar }}
</form>
```

The `filter_bar` component auto-submits via `onchange="this.form.submit()"`. The search_input component calls `form.requestSubmit()` after 300ms debounce. Both work because they share the same parent form.

### WCAG Requirements

- `<main>` landmark wraps the list content (AC3)
- `<h1>Users</h1>` present in the list column (AC3)
- `<nav aria-label="pagination">` wraps load-more controls (AC3)
- Existing `<nav role="navigation" aria-label="Item list">` from `master_detail.html` is the outer nav — `<main>` inside `{{ define "master_list" }}` is correct because `master_detail.html` wraps the block in `<nav>` and the `<main>` lives inside `master_list` content, not inside `<nav>`. This nesting is valid per ARIA 1.2 (landmark in nav is permitted when `main` is the primary content area).
- **NOTE:** Technically, a `<main>` inside a `<nav>` is unusual. If the WCAG validator flags it, the dev agent may move `<main>` to wrap the outer layout level in `base.html` instead, or use `role="main"` on the master list `<div>` without nesting. An alternative: place `<main>` in the `{{ define "content" }}` block in `users.html` (wrapping the entire master-detail layout), which is cleaner. The acceptance test only checks that `<main>` is present in the rendered body — not its exact nesting. The dev agent should pick the cleanest approach.

### What NOT to Do

- **Do NOT add pagination to `DetailHandler`** — detail view always shows the full user; pagination is list-only
- **Do NOT implement JS infinite scroll** — load-more is a `?page=N` GET link only
- **Do NOT implement skeleton CSS/JS** — `data-loading-skeleton` attribute is sufficient for this story
- **Do NOT modify `interaction-components.spec.ts`** — the `test.skip` for C8/C9 remains; this story's Playwright coverage in `users-page.spec.ts` supersedes those tests functionally
- **Do NOT use `strings.EqualFold`** — use `strings.Contains(strings.ToLower(...), strings.ToLower(q))` for substring search, not equality

### File Locations

| File | Action |
|---|---|
| `gateway/internal/admin/users.go` | Modify — extend `ListHandler`, add `filterStubUsers` |
| `gateway/internal/admin/page_data.go` | Modify — replace `UsersPageData`, add `UserRowData` |
| `gateway/internal/admin/templates/users.html` | Modify — rebuild `master_list` block |
| `gateway/internal/admin/users_page_test.go` | NEW — 5 Go unit tests |
| `gateway/internal/admin/master_detail_test.go` | Modify — rename `StubUsers` → `Users` in test data |
| `e2e/tests/features/admin/users-page.spec.ts` | NEW — 4 Playwright E2E tests (all REAL, no skip) |

### Upcoming Stories That Depend on This Story

- **Story 7.6** (User Detail Panel): uses the same `UsersPageData` with `ActiveItemID` / `ActiveUser`; detail panel in `users.html` must remain functional — do not break it
- **Story 7.7** (User Role UI): adds deactivation action to the detail panel; builds on the filtered list from this story
- **Story 7.13** (WCAG audit): axe-core will run against `/admin/users` and must not find landmark violations

## Status Notes

Created: 2026-04-29. Stories 7.1 (Obsidian theme), 7.2 (MasterDetailLayout), 7.3 (Interaction Components), and 7.4 (Display Components) are done. This story upgrades the `/admin/users` page to a full production-quality list with search, role filter, pagination, WCAG landmarks, and proper StatusBadge/EmptyState integration. All required component templates (C8, C9/C10, C13, C14) are already implemented — this story only wires them into a real page. Playwright tests are REAL (not `test.skip`) because a production page exists to test against.

## Review Findings

Code review completed: 2026-04-29. Reviewers: Blind Hunter, Edge Case Hunter, Acceptance Auditor. TEA Gate 2 prior: CONDITIONAL PASS (2 MINORs fixed inline).

- [x] [Review][Patch] Badge normalization duplicated across ListHandler, DetailHandler, and stubUserRows — extracted to `toUserRowData(u StubUser) UserRowData` helper in `users.go`; all three callsites updated; 195 tests pass. [`gateway/internal/admin/users.go`, `gateway/internal/admin/master_detail_test.go`]

Dismissed as noise: 4 (URL encoding in load-more href — handled correctly by html/template URL context escaping; `<main>` inside `<nav>` nesting — spec-acknowledged, Story 7.13 WCAG audit scope; rooms.html inline badge — pre-existing, not in scope; DetailHandler zero-value SearchInput/FilterBar — functionally safe for stub phase).

Decision-needed: 0 | Patches applied: 1 | Deferred: 0 | Dismissed: 4

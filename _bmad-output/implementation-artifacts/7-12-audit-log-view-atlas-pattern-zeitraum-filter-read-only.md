---
id: 7-12
security_review: not-needed
---

# Story 7.12: Audit Log View — Atlas Pattern, Zeitraum-Filter, Read-Only

Status: ready-for-dev

## Story

As an instance admin,
I want a read-only audit log page at `/admin/audit-log` that shows all recorded admin actions and lets me narrow results by date range,
so that I can review what happened and when without risking accidental mutation of the log.

## Acceptance Criteria

1. **`StubAuditEntry` struct and stub data added to `stubs.go`** — A new `StubAuditEntry` struct and package-level `stubAuditLog` slice are defined in `gateway/internal/admin/stubs.go`:
   ```go
   type StubAuditEntry struct {
       ID         string
       Timestamp  string // ISO-8601-like, e.g. "2026-04-29T14:30:00Z"
       Actor      string // email, e.g. "kai@example.com"
       Action     string // dot-notation verb, e.g. "user.deactivate"
       TargetID   string // e.g. "usr-003"
       TargetName string // human-readable, e.g. "Carla Reiter"
   }
   var stubAuditLog = []StubAuditEntry{ /* 6 entries spanning 2026-04-28..2026-04-30 */ }
   ```
   Six entries span at least three distinct dates (2026-04-28, 2026-04-29, 2026-04-30) to exercise the date filter.

2. **`AuditLogPageData` added to `page_data.go`** — A new struct:
   ```go
   type AuditLogPageData struct {
       PageData
       Entries []StubAuditEntry
       From    string // query param "from", e.g. "2026-04-29"
       To      string // query param "to", e.g. "2026-04-29"
       Flash   AlertBannerData
   }
   ```

3. **`GET /admin/audit-log` page renders** — `gateway/internal/admin/audit_log_handler.go`: An `AuditLogHandler` struct with `NewAuditLogHandler(tmpl *TemplateHandler) *AuditLogHandler`. `ListHandler(w, r)` reads `?from=` and `?to=` (both optional date strings "YYYY-MM-DD"), filters `stubAuditLog`, renders `audit_log.html` with `AuditLogPageData`. Response is HTTP 200.

4. **Date-range filter logic** — When `from` and `to` are both present:
   - An entry is included if `entry.Timestamp >= from` AND `entry.Timestamp <= to+"T23:59:59Z"` (string prefix comparison works because timestamps are ISO-8601 with the date as a prefix).
   - When only one param is present (partial filter), it is ignored — no filter is applied.
   - When both params are absent, all entries are returned.

5. **Date-range filter form** — The page contains a `<form method="GET" action="/admin/audit-log">` with:
   - `<input type="date" name="from">` pre-filled with the current `From` value
   - `<input type="date" name="to">` pre-filled with the current `To` value
   - An Apply button (`<button type="submit">`)

6. **Read-only audit table** — The page renders a table with columns: **Actor**, **Action**, **Target**, **Timestamp**. There are NO approve/reject/edit actions — this page is strictly read-only.

7. **Empty state** — When no entries match the filter, an empty-state message is rendered (via the `empty_state` component or inline) indicating no entries were found for the selected range.

8. **Route registered in `main.go`** — `GET /admin/audit-log` is registered behind `sessionGuard` and `csrf`, consistent with other admin read-only pages.

9. **Go unit tests pass** — `gateway/internal/admin/audit_log_test.go` contains at minimum:
   - `TestAuditLogPageRenders` — GET → 200, `<h1`, `<main`
   - `TestAuditLogDateFilter` — GET with `from=2026-04-29&to=2026-04-29` → only entries from 2026-04-29 present in body
   - `TestAuditLogNoFilter` — GET without params → all 6 entries shown
   - `TestAuditLogEmptyState` — GET with `from=2000-01-01&to=2000-01-01` → empty state message, no table entries

10. **Playwright E2E tests** — `e2e/tests/features/admin/audit-log.spec.ts` contains:
    - `audit log page renders all entries by default` — navigates to `/admin/audit-log`, asserts `<h1>` and at least one actor name
    - `date filter reduces visible entries` — applies a date filter that excludes at least one entry and asserts the excluded entry is absent

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestAuditLogPageRenders` — Go httptest
   - Given: a running `AuditLogHandler` with `NewTemplateHandler()`
   - When: GET /admin/audit-log
   - Then: HTTP 200, body contains `<h1` and `<main`

2. `TestAuditLogDateFilter` — Go httptest
   - Given: `stubAuditLog` with entries on 2026-04-28, 2026-04-29, 2026-04-30
   - When: GET /admin/audit-log?from=2026-04-29&to=2026-04-29
   - Then: body contains a 2026-04-29 actor name, body does NOT contain a 2026-04-28 actor name

3. `TestAuditLogNoFilter` — Go httptest
   - Given: 6 stub entries
   - When: GET /admin/audit-log (no params)
   - Then: body contains all 6 actor/action strings

4. `TestAuditLogEmptyState` — Go httptest
   - Given: stubAuditLog with entries from 2026-04-28..2026-04-30
   - When: GET /admin/audit-log?from=2000-01-01&to=2000-01-01
   - Then: body contains empty-state indicator ("No audit" or "no entries"), body does NOT contain any known actor name

5. `audit log page renders all entries by default` — Playwright
   - Given: admin logged in via OIDC Authorization Code + PKCE
   - When: navigate to /admin/audit-log
   - Then: h1 visible, at least one actor email visible in the page body

6. `date filter reduces visible entries` — Playwright
   - Given: admin logged in, /admin/audit-log showing all 6 entries
   - When: fill `from` = "2026-04-29", fill `to` = "2026-04-29", click Apply
   - Then: body contains a 2026-04-29 entry, body does NOT contain a 2026-04-28 entry

## Dev Notes

- Date filter uses simple string-prefix comparison (ISO-8601 lexicographic ordering) — no `time.Parse` required for MVP.
- Stub data only — no DB query. Replaced in Epic 6/8 by real audit log API.
- Read-only page: no POST handlers, no CSRF-guarded mutations.
- Atlas pattern = read-only list with filter form (same structure as `rooms.html` list view, minus actions).
- Template name: `audit_log` (from `templates/audit_log.html`).

## Tasks

- [ ] Add `StubAuditEntry` struct and `stubAuditLog` (6 entries) to `stubs.go`
- [ ] Add `AuditLogPageData` to `page_data.go`
- [ ] Write `audit_log_test.go` (4 tests, failing first)
- [ ] Write `audit-log.spec.ts` (2 Playwright tests)
- [ ] Implement `audit_log_handler.go` with `AuditLogHandler`, `NewAuditLogHandler`, `ListHandler`
- [ ] Create `templates/audit_log.html`
- [ ] Register `GET /admin/audit-log` in `main.go`
- [ ] Run `go test ./internal/admin/... -count=1` — all pass

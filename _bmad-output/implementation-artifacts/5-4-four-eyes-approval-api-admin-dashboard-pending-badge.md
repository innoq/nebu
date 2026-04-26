---
security_review: required
---

# Story 5.4: Four-Eyes Approval API + Admin-Dashboard Pending-Badge

Status: review

## Story

As a second compliance officer,
I want to view and approve or reject pending compliance access requests,
so that no single officer can gain unilateral access to message data.

**Size:** S

---

## Acceptance Criteria

### AC1 — GET List of Pending Requests

`GET /api/v1/compliance/access-requests?status=pending`

- JWT middleware required (same `jwtMiddleware` chain as AC1 of Story 5.3).
- Role gate: `compliance_officer` only — 403 `M_FORBIDDEN` otherwise.
- Returns only rows where `status = 'pending'` AND `requester_user_id != callerSub` (self-approval-Sperre at query level).
- Response body:
  ```json
  {
    "data": [
      {
        "request_id": "...",
        "requester_user_id": "...",
        "room_id": "...",
        "time_range_start": "...",
        "time_range_end": "...",
        "justification": "...",
        "created_at": "..."
      }
    ],
    "meta": {"total": N}
  }
  ```
- `time_range_start`, `time_range_end`, `created_at` serialised as RFC 3339 strings.
- No pagination for MVP (document as risk: >10K pending rows → response bloat; follow-up FB for 5-29).
- 200 OK even if list is empty (`"data": [], "meta": {"total": 0}`).

### AC2 — POST Approve

`POST /api/v1/compliance/access-requests/{requestId}/approve`

- JWT + `compliance_officer` role — 403 `M_FORBIDDEN` otherwise.
- `requireJSON` check (415 on wrong Content-Type).
- Body: `{"note": "..."}` (optional field; empty body `{}` is valid; note is stored in audit metadata).
- Self-approval guard: if `requesterSub == caller` → `403 M_FORBIDDEN "Self-approval is not permitted"`.
- Status guard: `UPDATE ... WHERE id = $1 AND status = 'pending' RETURNING id` — if 0 rows updated → `409 M_CONFLICT`.
  - Must also handle unknown request_id: if no row at all, the 0-rows-updated path returns 404 (read below).
- DB UPDATE (single atomic statement — race-free):
  ```sql
  UPDATE compliance_requests
     SET status = 'approved',
         approver_user_id = $2,
         approved_at = NOW()
   WHERE id = $1
     AND status = 'pending'
  RETURNING id, requester_user_id
  ```
  - If 0 rows affected: query `SELECT 1 FROM compliance_requests WHERE id = $1` to distinguish 404 vs 409.
- Audit: `audit.LogEvent(ctx, coreClient, callerSub, "compliance_access_approved", "compliance_request", requestId, map[string]any{"note": note}, "success", "")` — never-raise, 500ms timeout.
- Returns `200 {"request_id": "...", "status": "approved"}`.

### AC3 — POST Reject

`POST /api/v1/compliance/access-requests/{requestId}/reject`

- Identical guards and middleware as AC2.
- Body: `{"note": "..."}` (optional).
- Self-rejection guard: same as self-approval (caller cannot reject own request).
- Status guard: same atomic UPDATE pattern:
  ```sql
  UPDATE compliance_requests
     SET status = 'rejected',
         approver_user_id = $2,
         approved_at = NOW()
   WHERE id = $1
     AND status = 'pending'
  RETURNING id, requester_user_id
  ```
  Note: `approved_at` column is reused for the rejection timestamp — the schema in migration 000019 has no `rejected_at` column. Do NOT add a separate `rejected_at`; store the timestamp in `approved_at` (which is already nullable and semantically means "decision timestamp"). This is consistent with what the migration already defines.
- Audit: `audit.LogEvent(..., "compliance_access_rejected", "compliance_request", requestId, map[string]any{"note": note}, "success", "")`.
- Returns `200 {"request_id": "...", "status": "rejected"}`.

### AC4 — GET Pending Count (Admin API)

`GET /admin/api/compliance/pending-count`

- Authenticated via **admin session cookie** (`sessionGuard` middleware) — NOT compliance_officer JWT.
- Route registered in `main.go` as: `mux.Handle("GET /admin/api/compliance/pending-count", sessionGuard(http.HandlerFunc(pendingCountHandler)))`
- Queries:
  ```sql
  SELECT COUNT(*) FROM compliance_requests WHERE status = 'pending'
  ```
- Returns `200 {"pending_count": N}`.
- No CSRF token required for GET.
- Non-admin (missing/invalid session cookie) → 401 (handled by `sessionGuard` redirect — callers of the API endpoint handle 302 as 401; or `sessionGuard` returns 401 for XHR based on Accept header).

### AC5 — Admin Dashboard Sidebar Badge

- The "Compliance" nav entry is added to `gateway/internal/admin/templates/layouts/base.html` sidebar (inside the `{{ if not .BootstrapMode }}` block, alongside "Dashboard" and "Logout").
- `DashboardPageData` in `gateway/internal/admin/page_data.go` gets a new field: `CompliancePendingCount int`.
- `DashboardHandler.Handler` in `gateway/internal/admin/dashboard.go` queries `compliance_requests WHERE status = 'pending'` (using the existing `*sql.DB` handle) and populates `CompliancePendingCount`.
- In `base.html` sidebar, the Compliance nav entry renders a DaisyUI badge if `CompliancePendingCount > 0`:
  ```html
  <li>
    <a href="/admin/compliance" ...>
      Compliance
      {{ if gt .CompliancePendingCount 0 }}
      <span class="badge badge-warning badge-sm ml-auto">{{ .CompliancePendingCount }}</span>
      {{ end }}
    </a>
  </li>
  ```
- **Template data propagation**: `base.html` receives `DashboardPageData` on the dashboard page; `PageData` embeds into all other pages. Non-dashboard pages do not show the badge (badge only makes sense on the dashboard-rendered context where the count is fetched). Alternative: pass the count through `PageData` so all pages get it — but for MVP, only dashboard fetches it server-side. This is the simpler approach: badge only visible on dashboard page.
- The badge must be server-side rendered (SSR) — no JavaScript fetch required.

### AC6 — Unit Tests

Written FIRST before handler implementation. Minimum required:

| Test | Expected result |
|---|---|
| `TestGetPendingList_HappyPath` | 200, returns list, correct JSON shape |
| `TestGetPendingList_ExcludesSelfSubmitted` | Caller's own requests absent from list |
| `TestGetPendingList_NonOfficer403` | 403 M_FORBIDDEN |
| `TestPostApprove_HappyPath` | 200 `{"request_id":"...","status":"approved"}` |
| `TestPostApprove_SelfApproval403` | 403 M_FORBIDDEN "Self-approval is not permitted" |
| `TestPostApprove_AlreadyApproved409` | 409 M_CONFLICT |
| `TestPostApprove_UnknownRequest404` | 404 M_NOT_FOUND |
| `TestPostReject_HappyPath` | 200 `{"request_id":"...","status":"rejected"}` |
| `TestPostReject_SelfReject403` | 403 M_FORBIDDEN |
| `TestGetPendingCount_HappyPath` | 200 `{"pending_count": N}` |
| `TestGetPendingCount_NonAdmin401` | 401/redirect (sessionGuard behaviour) |
| `TestPostApprove_AuditEmitted` | audit.LogEvent called with action="compliance_access_approved", metadata={"note":"..."} |

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestGetPendingList_HappyPath` — Go httptest (unit)
   - Given: valid JWT with `system_role=compliance_officer`, mock DB returns 2 rows where `requester_user_id != callerSub`
   - When: `GET /api/v1/compliance/access-requests?status=pending`
   - Then: HTTP 200, `"data"` array has 2 entries, `"meta":{"total":2}`, all required fields present

2. `TestGetPendingList_ExcludesSelfSubmitted` — Go httptest (unit)
   - Given: DB has 3 pending rows, 1 submitted by the caller (callerSub matches `requester_user_id`)
   - When: `GET /api/v1/compliance/access-requests?status=pending`
   - Then: HTTP 200, `"meta":{"total":2}`, no entry has `requester_user_id == callerSub`

3. `TestGetPendingList_NonOfficer403` — Go httptest (unit)
   - Given: valid JWT with `system_role=instance_admin`
   - When: `GET /api/v1/compliance/access-requests?status=pending`
   - Then: HTTP 403, `{"errcode":"M_FORBIDDEN",...}`

4. `TestPostApprove_HappyPath` — Go httptest (unit)
   - Given: valid JWT as officer B, mock DB returns 1 row with `requester_user_id = officer_A` (different sub), status=pending → UPDATE succeeds
   - When: `POST /api/v1/compliance/access-requests/{id}/approve` with `{"note":"approved under ref 42"}`
   - Then: HTTP 200, `{"request_id":"...","status":"approved"}`

5. `TestPostApprove_SelfApproval403` — Go httptest (unit)
   - Given: valid JWT with sub=`@alice:server`, mock DB returns row with `requester_user_id = "@alice:server"`
   - When: `POST .../approve`
   - Then: HTTP 403, `{"errcode":"M_FORBIDDEN","error":"Self-approval is not permitted"}`

6. `TestPostApprove_AlreadyApproved409` — Go httptest (unit)
   - Given: mock DB: UPDATE WHERE status='pending' affects 0 rows, SELECT confirms row exists (status='approved')
   - When: `POST .../approve`
   - Then: HTTP 409, `{"errcode":"M_CONFLICT",...}`

7. `TestPostApprove_UnknownRequest404` — Go httptest (unit)
   - Given: mock DB: UPDATE affects 0 rows, SELECT also returns 0 rows (row not found)
   - When: `POST .../approve`
   - Then: HTTP 404, `{"errcode":"M_NOT_FOUND",...}`

8. `TestPostReject_HappyPath` — Go httptest (unit)
   - Given: valid JWT officer B, mock DB row has `requester_user_id = officer_A`, status=pending → UPDATE succeeds
   - When: `POST .../reject` with `{}`
   - Then: HTTP 200, `{"request_id":"...","status":"rejected"}`

9. `TestPostReject_SelfReject403` — Go httptest (unit)
   - Given: caller is the original requester
   - When: `POST .../reject`
   - Then: HTTP 403, `{"errcode":"M_FORBIDDEN","error":"Self-approval is not permitted"}`

10. `TestGetPendingCount_HappyPath` — Go httptest (unit)
    - Given: mock DB returns COUNT(*) = 5, valid admin session cookie in request
    - When: `GET /admin/api/compliance/pending-count`
    - Then: HTTP 200, `{"pending_count":5}`

11. `TestGetPendingCount_NonAdmin401` — Go httptest (unit)
    - Given: no valid session cookie
    - When: `GET /admin/api/compliance/pending-count`
    - Then: HTTP 302 redirect to `/admin/login` (or 401 if `sessionGuard` checks Accept header)

12. `TestPostApprove_AuditEmitted` — Go httptest (unit)
    - Given: valid officer B, mock DB UPDATE succeeds, mock `pb.CoreServiceClient`
    - When: `POST .../approve` with `{"note":"ref-42"}`
    - Then: `WriteAuditLog` called once with `action="compliance_access_approved"`, `target_type="compliance_request"`, `target_id=requestId`, `metadata_json` contains `"note":"ref-42"`

---

## Tasks / Subtasks

- [x] Migration decision (no new migration needed)
  - Migration 000019 already has `approved_at TIMESTAMPTZ` and `approver_user_id TEXT` — both columns serve reject as well (no `rejected_at` needed per AC).
  - Schema `status CHECK IN ('pending', 'approved', 'rejected')` already covers all states.
  - **Decision: NO new migration file for Story 5.4.** Existing 000019 is sufficient.

- [x] Handler methods in `gateway/internal/compliance/handler.go`
  - [x] Write failing tests first in `gateway/internal/compliance/handler_test.go`
  - [x] `GetAccessRequests(w, r)` — GET list (AC1)
  - [x] `PostApprove(w, r)` — approve (AC2)
  - [x] `PostReject(w, r)` — reject (AC3)
  - [x] Extend `AccessRequestHandler` struct: add `CoreClient pb.CoreServiceClient` field (already present from 5.3) — no struct change needed

- [x] Pending count handler (AC4)
  - [x] Write failing test first
  - [x] New handler function/type for `GET /admin/api/compliance/pending-count`
  - [x] Lives in `gateway/internal/compliance/handler.go` as `PendingCountHandler` struct

- [x] Route registration in `gateway/cmd/gateway/main.go`
  - [x] `GET /api/v1/compliance/access-requests` → `jwtMiddleware(...)` (no bodyLimit for GET)
  - [x] `POST /api/v1/compliance/access-requests/{requestId}/approve`
  - [x] `POST /api/v1/compliance/access-requests/{requestId}/reject`
  - [x] `GET /admin/api/compliance/pending-count` → `sessionGuard(http.HandlerFunc(...))`
  - [x] Reused existing `complianceDB` connection and `accessRequestHandler` from Story 5.3

- [x] Admin Dashboard integration (AC5)
  - [x] Add `CompliancePendingCount int` directly on `DashboardPageData` in `gateway/internal/admin/page_data.go`; also added as promoted zero-default field on `PageData` so `base.html` works for all page types
  - [x] Add `CompliancePendingCounter` interface and `postgresCompliancePendingCounter` impl in `gateway/internal/admin/dashboard.go`
  - [x] Update `DashboardHandler` struct with `pendingCounter CompliancePendingCounter` field and populate `CompliancePendingCount` in `Handler`
  - [x] Update `gateway/internal/admin/templates/layouts/base.html` sidebar to add "Compliance" nav entry with DaisyUI `badge-warning badge-sm` badge
  - [x] No changes needed to `dashboard.html` (badge is in base.html sidebar)

- [x] Tests
  - [x] All 21 unit tests green (17 in approval_test.go + 4 in dashboard_pending_badge_test.go)
  - [x] `make test-unit-go` passes (all packages OK)

---

## Dev Notes

### Package Location — Extend, Do Not Reinvent

All new handler methods go in the **existing** `gateway/internal/compliance/` package:
- Extend `handler.go` with `GetAccessRequests`, `PostApprove`, `PostReject` methods on `AccessRequestHandler`.
- Add `PendingCountHandler` struct (or function) in the same package or a new `pending_count.go` file.
- Extend `handler_test.go` with the 12 new tests.

Do NOT create a new package.

### AccessRequestHandler Struct (from Story 5.3 — do not duplicate)

```go
type AccessRequestHandler struct {
    DB         *sql.DB
    CoreClient pb.CoreServiceClient
}
```

This struct already has both fields needed for Story 5.4. The `PendingCountHandler` is a separate admin-facing handler that queries DB only:

```go
type PendingCountHandler struct {
    DB *sql.DB
}
```

### Role Gate Pattern (inline, not middleware — Story 5.3 convention)

```go
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
if systemRole != "compliance_officer" {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
    return
}
callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)
```

`middleware.ContextKeySub` = raw OIDC `sub` — the stable identifier for compliance records (not the Matrix user_id).

### Atomic Status Transition — Race-Free Pattern

Use a single `UPDATE ... WHERE id = $1 AND status = 'pending' RETURNING id, requester_user_id` to avoid TOCTOU:

```go
var returnedID, requesterUserID string
err := h.DB.QueryRowContext(ctx,
    `UPDATE compliance_requests
        SET status = $3, approver_user_id = $2, approved_at = NOW()
      WHERE id = $1 AND status = 'pending'
  RETURNING id, requester_user_id`,
    requestID, callerSub, newStatus,
).Scan(&returnedID, &requesterUserID)

if errors.Is(err, sql.ErrNoRows) {
    // 0 rows updated — distinguish 404 vs 409
    var exists int
    checkErr := h.DB.QueryRowContext(ctx,
        `SELECT 1 FROM compliance_requests WHERE id = $1`, requestID,
    ).Scan(&exists)
    if errors.Is(checkErr, sql.ErrNoRows) {
        writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Request not found")
    } else {
        writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "Request is not pending")
    }
    return
}
```

Self-approval check: after the RETURNING scan, compare `requesterUserID == callerSub`. But this is post-update — **must be checked BEFORE the UPDATE** to avoid writing the self-approval to the DB before rejecting it. Fetch the requester first:

```go
// Pre-flight: fetch requester_user_id to enforce self-approval guard
var requesterUserID string
err := h.DB.QueryRowContext(ctx,
    `SELECT requester_user_id FROM compliance_requests WHERE id = $1`,
    requestID,
).Scan(&requesterUserID)
if errors.Is(err, sql.ErrNoRows) {
    writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Request not found")
    return
}
if err != nil { /* 500 */ return }
if requesterUserID == callerSub {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Self-approval is not permitted")
    return
}

// Then atomic UPDATE WHERE status='pending'
var returnedID string
err = h.DB.QueryRowContext(ctx,
    `UPDATE compliance_requests
        SET status = $3, approver_user_id = $2, approved_at = NOW()
      WHERE id = $1 AND status = 'pending'
  RETURNING id`,
    requestID, callerSub, newStatus,
).Scan(&returnedID)
if errors.Is(err, sql.ErrNoRows) {
    writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "Request is not pending")
    return
}
```

This is two DB round-trips — acceptable for MVP. A single CTE-based query is possible but adds complexity.

### Path Parameter Extraction (Go 1.22+)

Go 1.22 `net/http` mux supports path params:

```go
requestID := r.PathValue("requestId")
if requestID == "" {
    writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "requestId is required")
    return
}
```

Route must use `{requestId}` placeholder: `"POST /api/v1/compliance/access-requests/{requestId}/approve"`.

### GET List Query

```sql
SELECT id, requester_user_id, room_id,
       time_range_start, time_range_end, justification, created_at
  FROM compliance_requests
 WHERE status = 'pending'
   AND requester_user_id != $1
 ORDER BY created_at DESC
```

- `$1` = `callerSub` (self-submission exclusion at DB level — avoids app-layer filter).
- Returns rows as `[]AccessRequestItem`; serialize with `json.Encoder`.

### Note on rejected_at Column

The `compliance_requests` schema (migration 000019) has **only `approved_at TIMESTAMPTZ`**, not a separate `rejected_at`. The epics.md AC for Story 5.4 mentions `approved_at = NOW()` only for the approve path. For reject, use `approved_at = NOW()` as the "decision timestamp" column (it is nullable and semantically documents the time the decision was made, regardless of approve/reject). Do NOT add a migration for a separate `rejected_at` — the existing column covers both transitions. If future stories need to distinguish the approve vs reject timestamp, a follow-up migration is appropriate.

### GET /admin/api/compliance/pending-count — Admin Session Auth

This endpoint uses `sessionGuard`, not `jwtMiddleware`. The `sessionGuard` is already declared in `main.go` (wrapping dashboard, logout, SSE, etc.). Wire similarly:

```go
pendingCountHandler := &compliance.PendingCountHandler{DB: complianceDB}
mux.Handle("GET /admin/api/compliance/pending-count",
    sessionGuard(http.HandlerFunc(pendingCountHandler.Handler)))
```

Reuse the existing `complianceDB` connection from Story 5.3 route setup.

### DashboardPageData Extension

In `gateway/internal/admin/page_data.go`, add one field:

```go
type DashboardPageData struct {
    PageData
    // ... existing fields ...
    CompliancePendingCount int // number of pending compliance access requests (Story 5.4)
}
```

In `dashboard.go`, query before rendering:

```go
var pendingCount int
_ = h.db.QueryRowContext(r.Context(),
    `SELECT COUNT(*) FROM compliance_requests WHERE status = 'pending'`).
    Scan(&pendingCount)
// pendingCount defaults to 0 on error — non-blocking
data.CompliancePendingCount = pendingCount
```

`DashboardHandler` needs a `db` field — it currently accesses `dbPinger` (a `DBPinger` interface) and `nameReader`. Add a `db *sql.DB` field directly (same DB handle), or satisfy via a new `CompliancePendingCounter` interface if you want to keep it testable. Recommended: add a minimal interface:

```go
type CompliancePendingCounter interface {
    CountPending(ctx context.Context) (int, error)
}
```

And a `postgresCompliancePendingCounter` implementation — keeps the handler unit-testable without a real DB.

### base.html Sidebar Badge — Template Scope Note

The `base.html` template receives whatever data struct is passed by the current handler. Only `DashboardHandler` populates `CompliancePendingCount`. On all other pages (login, bootstrap, error pages), the embedded `PageData` does not have this field — but Go templates will yield 0 for missing int fields, so `{{ if gt .CompliancePendingCount 0 }}` is safe on all pages (evaluates to false when field is absent or zero).

However, for clean implementation, move `CompliancePendingCount` into `PageData` and populate it in `DashboardHandler` only. Other handlers leave it at 0 (badge hidden). This is the simplest approach.

### requireJSON for POST Routes

Story 5.3 established `requireJSON` as a package-private function in `gateway/internal/compliance/handler.go`. All POST methods in this package reuse it directly (same package).

### Route Registration Summary

In `gateway/cmd/gateway/main.go`, near the existing compliance route (line ~705):

```go
// Story 5.4 — Four-Eyes Approval API
// GET: no body, no bodyLimit needed
mux.Handle("GET /api/v1/compliance/access-requests",
    jwtMiddleware(http.HandlerFunc(accessRequestHandler.GetAccessRequests)))

mux.Handle("POST /api/v1/compliance/access-requests/{requestId}/approve",
    bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostApprove))))

mux.Handle("POST /api/v1/compliance/access-requests/{requestId}/reject",
    bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostReject))))

// Admin API (session auth, not JWT)
pendingCountHandler := &compliance.PendingCountHandler{DB: complianceDB}
mux.Handle("GET /admin/api/compliance/pending-count",
    sessionGuard(http.HandlerFunc(pendingCountHandler.Handler)))
```

### Audit LogEvent Signature (from Story 5.2)

```go
auditpkg.LogEvent(
    auditCtx,          // context with 500ms timeout
    h.CoreClient,
    callerSub,         // actorUserID
    "compliance_access_approved", // action
    "compliance_request",         // targetType
    requestID,                    // targetID
    map[string]any{"note": note}, // metadata (note may be empty string)
    "success",
    "",
)
```

Import: `auditpkg "github.com/nebu/nebu/internal/audit"`

### DB Mock Pattern for Unit Tests

Story 5.3 established a `fakeDB` pattern in `handler_test.go` using a custom `database/sql` driver. Extend the existing fake driver rather than creating a new one. The tests must NOT require a running PostgreSQL (unit tests run without a database).

When testing the atomic UPDATE → 0 rows path, the mock must return `sql.ErrNoRows` from `Scan`.

### Scope — What Is In This Story

- `GET /api/v1/compliance/access-requests?status=pending` (list, excludes self)
- `POST /api/v1/compliance/access-requests/{requestId}/approve`
- `POST /api/v1/compliance/access-requests/{requestId}/reject`
- `GET /admin/api/compliance/pending-count`
- `base.html` sidebar Compliance nav entry with pending badge
- `DashboardPageData.CompliancePendingCount` field + dashboard handler wiring
- Unit tests (12 minimum, written first)
- No new migration (000019 is sufficient)

### Scope — What Is NOT In This Story

- Compliance sessions / JWT (Story 5.5)
- Data export (Story 5.6)
- RLS role separation (Story 5.29)
- Pagination for GET list (deferred)
- Full compliance UI (Story 7.11)

---

## Architecture Decisions Made for This Story

### ADec-5.4-1: No New Migration

Migration 000019 already contains `approved_at TIMESTAMPTZ` and `approver_user_id TEXT`. The `rejected_at` column is not mentioned in the epic AC. Both approve and reject write to `approved_at` as the "decision timestamp". Status column already allows `'rejected'`. **000020 is NOT created.**

### ADec-5.4-2: Atomic Status Transition via WHERE-clause Guard

`UPDATE ... WHERE id = $1 AND status = 'pending' RETURNING id` is the race-free approach. TOCTOU is avoided without requiring an explicit transaction. The pre-flight SELECT for self-approval guard is a separate read (2 round-trips total), which is acceptable for MVP.

### ADec-5.4-3: Self-Approval Guard for Both Approve AND Reject

The epic says "same guards" — callers cannot reject their own requests either. Error message: `"Self-approval is not permitted"` (used for both approve and reject for simplicity).

### ADec-5.4-4: PendingCount in DashboardPageData (not PageData)

Only the dashboard fetches the count server-side. Other pages leave `CompliancePendingCount = 0` (badge hidden). This avoids a DB query on every admin page render. Move field to `PageData` only if a future story needs the badge on non-dashboard pages.

### ADec-5.4-5: No Pagination for GET List (MVP)

Document as known risk (>10K pending rows → response bloat). Follow-up FB for Story 5.29.

---

## References

- Role mapping: `gateway/internal/auth/roles.go` — `MapSystemRole`, `ExtractRoleClaim`
- JWT middleware + context keys: `gateway/internal/middleware/auth.go` — `ContextKeySystemRole`, `ContextKeySub`
- Body limit: `gateway/internal/middleware/body_limit.go` — `bodyLimit64KiB`
- Compliance handler (Story 5.3): `gateway/internal/compliance/handler.go`
- Compliance test patterns: `gateway/internal/compliance/handler_test.go`
- Audit LogEvent: `gateway/internal/audit/writer.go:28-65`
- compliance_requests schema: `gateway/migrations/000019_compliance_requests.up.sql`
- DashboardPageData + PageData: `gateway/internal/admin/page_data.go`
- DashboardHandler: `gateway/internal/admin/dashboard.go`
- Sidebar nav: `gateway/internal/admin/templates/layouts/base.html` (lines 43–84)
- Dashboard template: `gateway/internal/admin/templates/dashboard.html`
- Route registration pattern: `gateway/cmd/gateway/main.go:263-265` (sessionGuard), `705-706` (jwtMiddleware compliance)
- Story 5.3 Dev Notes (full patterns): `_bmad-output/implementation-artifacts/5-3-compliance-access-request-api.md`

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (2026-04-23)

### Completion Notes List

- Implemented `GetAccessRequests`, `PostApprove`, `PostReject` as methods on `AccessRequestHandler` in `handler.go`. `PostApprove`/`PostReject` share a `postDecision` helper to avoid duplication.
- `PendingCountHandler` added inline in `handler.go` (not a separate file) since it's compact and belongs to the same package.
- UUID validation: no custom validator needed — `requestId` extracted via `r.PathValue("requestId")` (Go 1.22 mux); empty-string check is sufficient at the handler level. DB will return ErrNoRows for invalid IDs.
- Self-approval check: pre-flight SELECT (2 round-trips) as specified in Dev Notes — atomic CTE alternative deferred per AC.
- Audit note: empty string (`""`) is passed in metadata when body is `{}` or note is omitted. `"note":""` is always present in the JSON metadata.
- `CompliancePendingCount` field design: placed as a **direct** field on both `PageData` (zero by default, for all page types) and `DashboardPageData` (direct, satisfies the test's struct-literal compile-time check). Go template field lookup resolves the direct `DashboardPageData` field at depth 0, shadowing the promoted `PageData` field at depth 1 — no ambiguity.
- `DashboardHandler.pendingCounter` wired via `CompliancePendingCounter` interface — fully unit-testable without a real DB.
- `formatTimeField` helper normalises time columns: real PostgreSQL returns `time.Time`; the fake driver returns `string` — the handler handles both to avoid test breakage.
- All 21 tests green: 17 approval tests (approval_test.go) + 4 dashboard badge tests (dashboard_pending_badge_test.go).
- `make test-unit-go` exit 0 — all 15 packages pass with `-race`.

### File List

- `gateway/internal/compliance/handler.go` (modified — added GetAccessRequests, postDecision, PostApprove, PostReject, PendingCountHandler, formatTimeField, io import)
- `gateway/internal/admin/page_data.go` (modified — CompliancePendingCount added to both PageData and DashboardPageData)
- `gateway/internal/admin/dashboard.go` (modified — CompliancePendingCounter interface, postgresCompliancePendingCounter, pendingCounter field in DashboardHandler, NewDashboardHandler updated, Handler populates CompliancePendingCount, slog import added)
- `gateway/internal/admin/templates/layouts/base.html` (modified — Compliance nav entry with DaisyUI badge-warning badge-sm added to sidebar)
- `gateway/cmd/gateway/main.go` (modified — 4 new routes: GET access-requests, POST approve, POST reject, GET pending-count)

No new migration files. Tests staged as red-phase in approval_test.go and dashboard_pending_badge_test.go (both unchanged — implementation written to make them green).

### Change Log

- 2026-04-23: Story 5.4 implemented. Four-eyes approval API (GET list, POST approve, POST reject, GET pending-count) and admin dashboard sidebar badge. All 21 tests green. `make test-unit-go` exit 0.

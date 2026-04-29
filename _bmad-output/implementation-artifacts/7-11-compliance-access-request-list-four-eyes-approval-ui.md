---
id: 7-11
security_review: not-needed
---

# Story 7.11: Compliance Access Request List — Four-Eyes Approval UI

Status: ready-for-dev

## Story

As a compliance officer,
I want a dedicated page at `/admin/compliance` that lists pending compliance access requests with approve/reject actions and a four-eyes approval workflow stepper,
so that I can process compliance requests with a clear visual representation of the review flow directly from the Admin UI.

## Acceptance Criteria

1. **`StubComplianceRequest` struct and stub data added to `stubs.go`** — A new `StubComplianceRequest` struct and package-level `stubComplianceRequests` slice are defined in `gateway/internal/admin/stubs.go`:
   ```go
   type StubComplianceRequest struct {
       ID          string
       UserID      string
       UserName    string
       RequestType string
       RequestedAt string
       Status      string // "pending" | "approved" | "rejected"
       ReviewedBy  string
   }
   var stubComplianceRequests = []StubComplianceRequest{
       {ID: "cr-001", UserID: "usr-001", UserName: "Alice Müller",  RequestType: "data-export",   RequestedAt: "2026-04-28", Status: "pending",  ReviewedBy: ""},
       {ID: "cr-002", UserID: "usr-003", UserName: "Carla Reiter",  RequestType: "account-audit",  RequestedAt: "2026-04-27", Status: "pending",  ReviewedBy: ""},
       {ID: "cr-003", UserID: "usr-002", UserName: "Bob Wagner",    RequestType: "data-export",   RequestedAt: "2026-04-25", Status: "approved", ReviewedBy: "kai@example.com"},
   }
   ```
   A `findStubComplianceRequest(id string) *StubComplianceRequest` helper uses a linear scan, consistent with `findStubUser` / `findStubRoom`.

2. **`CompliancePageData` added to `page_data.go`** — A new struct:
   ```go
   type CompliancePageData struct {
       PageData
       Requests     []StubComplianceRequest
       StatusFilter string
       Flash        AlertBannerData
       Stepper      WizardStepperData
   }
   ```

3. **`GET /admin/compliance` page renders** — `gateway/internal/admin/compliance_handler.go`: A `ComplianceHandler` struct with `NewComplianceHandler(tmpl *TemplateHandler) *ComplianceHandler`. `ListHandler(w, r)` reads `?status=` (default `"pending"`), filters `stubComplianceRequests`, renders `compliance.html` with `CompliancePageData`. Response is HTTP 200.

4. **Status filter form** — The page contains a `<form method="GET" action="/admin/compliance">` with a `<select name="status">` offering options: `all`, `pending`, `approved`, `rejected`. The selected option matches `StatusFilter`. The form auto-submits on change.

5. **Request table/list with approve/reject actions** — Each request row shows `UserName`, `RequestType`, `RequestedAt`, `Status`. Pending requests include:
   - An approve mini-form: `<form method="POST" action="/admin/compliance/{id}/approve">` with hidden CSRF input and `<button type="submit">Approve</button>`
   - A reject mini-form: `<form method="POST" action="/admin/compliance/{id}/reject">` with hidden CSRF input and `<button type="submit">Reject</button>`
   Non-pending requests show the `ReviewedBy` value instead.

6. **Empty state** — When no requests match the filter, `{{ template "empty_state" ... }}` is rendered with `Heading: "No compliance requests"`.

7. **WizardStepper component** — The page renders `{{ template "wizard_stepper" .Stepper }}` with steps `["Requested", "Under Review", "Decision"]`. `Stepper.CurrentStep` is always `1` ("Under Review") on this page to demonstrate the four-eyes approval flow.

8. **`POST /admin/compliance/{id}/approve`** — `ApproveHandler(w, r)` sets `Status = "approved"` and `ReviewedBy = "kai@example.com"` on the matching stub entry, then PRG-redirects to `/admin/compliance?flash=Request+approved`.

9. **`POST /admin/compliance/{id}/reject`** — `RejectHandler(w, r)` sets `Status = "rejected"` and `ReviewedBy = "kai@example.com"` on the matching stub entry, then PRG-redirects to `/admin/compliance?flash=Request+rejected`.

10. **Flash message** — `GET /admin/compliance?flash=<msg>` populates `CompliancePageData.Flash` with `AlertBannerData{Severity: "success", Message: msg, Dismissible: true}` and renders it via `{{ template "alert_banner" .Flash }}`.

11. **Routing** — `gateway/cmd/gateway/main.go`:
    - `GET /admin/compliance`: `csrf(sessionGuard(http.HandlerFunc(complianceHandler.ListHandler)))`
    - `POST /admin/compliance/{id}/approve`: `sessionGuard(http.HandlerFunc(complianceHandler.ApproveHandler))`
    - `POST /admin/compliance/{id}/reject`: `sessionGuard(http.HandlerFunc(complianceHandler.RejectHandler))`

12. **Go unit tests** — `gateway/internal/admin/compliance_test.go` (`package admin`):
    - `TestCompliancePageRenders` — `GET /admin/compliance` → 200, body contains `<h1`, `<main`
    - `TestCompliancePagePendingFilter` — default shows pending requests only (Alice Müller + Carla Reiter), not approved (Bob Wagner)
    - `TestComplianceApprove` — `POST /admin/compliance/cr-001/approve` → 302, Location contains `flash=`, stub status=approved
    - `TestComplianceReject` — `POST /admin/compliance/cr-002/reject` → 302, Location contains `flash=`, stub status=rejected
    - `TestComplianceWizardStepper` — `GET /admin/compliance` → body contains `aria-current="step"` (wizard stepper rendered)

13. **Playwright E2E tests** — `e2e/tests/features/admin/compliance.spec.ts` — all REAL:
    - `compliance page renders pending requests` — navigate to `/admin/compliance`, expect `h1` containing "Compliance Requests"; expect page to contain "Alice Müller"
    - `approve request shows flash message` — click Approve on first pending request → `div[role="alert"]` containing "Request approved" is visible

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestCompliancePageRenders — GET /admin/compliance returns 200 with landmark and heading** — Go `net/http/httptest` (`gateway/internal/admin/compliance_test.go`)
   - Given: `ComplianceHandler` constructed with a valid `TemplateHandler`
   - When: `GET /admin/compliance`
   - Then: HTTP 200, body contains `<h1`, body contains `<main`

2. **TestCompliancePagePendingFilter — default filter shows only pending requests** — Go `net/http/httptest`
   - Given: `ComplianceHandler` constructed; `stubComplianceRequests` has 2 pending (Alice, Carla) and 1 approved (Bob)
   - When: `GET /admin/compliance` (no ?status= param → defaults to "pending")
   - Then: HTTP 200, body contains "Alice Müller", body contains "Carla Reiter", body does NOT contain "Bob Wagner"

3. **TestComplianceApprove — POST /approve transitions status and redirects** — Go `net/http/httptest`
   - Given: `ComplianceHandler` constructed; `stubComplianceRequests[0]` (cr-001) has Status="pending"
   - When: `POST /admin/compliance/cr-001/approve`
   - Then: HTTP 302, Location contains `flash=`; `stubComplianceRequests[0].Status == "approved"` after handler returns
   - Cleanup: `t.Cleanup` restores `stubComplianceRequests` to original values

4. **TestComplianceReject — POST /reject transitions status and redirects** — Go `net/http/httptest`
   - Given: `ComplianceHandler` constructed; `stubComplianceRequests[1]` (cr-002) has Status="pending"
   - When: `POST /admin/compliance/cr-002/reject`
   - Then: HTTP 302, Location contains `flash=`; `stubComplianceRequests[1].Status == "rejected"` after handler returns
   - Cleanup: `t.Cleanup` restores `stubComplianceRequests` to original values

5. **TestComplianceWizardStepper — page renders wizard_stepper with aria-current="step"** — Go `net/http/httptest`
   - Given: `ComplianceHandler` constructed
   - When: `GET /admin/compliance`
   - Then: HTTP 200, body contains `aria-current="step"` (the active step in the wizard stepper component)

6. **Playwright: compliance page renders pending requests** — Playwright (`e2e/tests/features/admin/compliance.spec.ts`)
   - Given: full dev stack running, admin logged in
   - When: navigate to `/admin/compliance`
   - Then: `h1` contains "Compliance Requests"; page contains "Alice Müller"

7. **Playwright: approve request shows flash message** — Playwright
   - Given: full dev stack running, admin logged in, at `/admin/compliance`
   - When: click the first "Approve" button
   - Then: `div[role="alert"]` containing "Request approved" is visible

## Tasks / Subtasks

- [ ] Task 1: Extend `stubs.go` with `StubComplianceRequest` struct and stub data (AC: 1)
  - [ ] 1.1 Add `StubComplianceRequest` struct
  - [ ] 1.2 Add `stubComplianceRequests` package-level variable (3 entries: 2 pending, 1 approved)
  - [ ] 1.3 Add `findStubComplianceRequest(id string) *StubComplianceRequest` helper

- [ ] Task 2: Extend `page_data.go` with `CompliancePageData` (AC: 2)
  - [ ] 2.1 Add `CompliancePageData` struct with embedded `PageData`, `Requests`, `StatusFilter`, `Flash`, `Stepper`

- [ ] Task 3: Write failing Go unit tests FIRST (AC: 12)
  - [ ] 3.1 Create `gateway/internal/admin/compliance_test.go` in `package admin`
  - [ ] 3.2 Write `TestCompliancePageRenders`
  - [ ] 3.3 Write `TestCompliancePagePendingFilter`
  - [ ] 3.4 Write `TestComplianceApprove` with `t.Cleanup` to restore stub slice
  - [ ] 3.5 Write `TestComplianceReject` with `t.Cleanup` to restore stub slice
  - [ ] 3.6 Write `TestComplianceWizardStepper`

- [ ] Task 4: Create `compliance_handler.go` (AC: 3, 8, 9, 10)
  - [ ] 4.1 `ComplianceHandler` struct + `NewComplianceHandler`
  - [ ] 4.2 `ListHandler` with status filter and flash
  - [ ] 4.3 `ApproveHandler` with PRG redirect
  - [ ] 4.4 `RejectHandler` with PRG redirect

- [ ] Task 5: Create `templates/compliance.html` (AC: 4, 5, 6, 7, 10)
  - [ ] 5.1 `<main>`, `<h1>Compliance Requests</h1>`
  - [ ] 5.2 Status filter form with `<select name="status">`
  - [ ] 5.3 Request list with approve/reject mini-forms (pending only) or ReviewedBy (non-pending)
  - [ ] 5.4 Empty state via `{{ template "empty_state" ... }}`
  - [ ] 5.5 Flash banner via `{{ template "alert_banner" .Flash }}`
  - [ ] 5.6 WizardStepper via `{{ template "wizard_stepper" .Stepper }}`

- [ ] Task 6: Register routes in `gateway/cmd/gateway/main.go` (AC: 11)
  - [ ] 6.1 `GET /admin/compliance` with `csrf(sessionGuard(...))`
  - [ ] 6.2 `POST /admin/compliance/{id}/approve` with `sessionGuard(...)`
  - [ ] 6.3 `POST /admin/compliance/{id}/reject` with `sessionGuard(...)`

- [ ] Task 7: Write Playwright E2E tests (AC: 13)
  - [ ] 7.1 Create `e2e/tests/features/admin/compliance.spec.ts`
  - [ ] 7.2 Write `compliance page renders pending requests` test
  - [ ] 7.3 Write `approve request shows flash message` test

- [ ] Task 8: Run `go test ./internal/admin/... -count=1` — all 5 tests pass

## Dev Notes

- **Stub data isolation in tests**: `stubComplianceRequests` is a package-level slice, so Approve/Reject tests must save and restore its contents in `t.Cleanup`. Use the same pattern as `TestUpdateConfig` in `config_test.go`.
- **WizardStepper CurrentStep=1**: "Under Review" is always the active step on this page regardless of individual request statuses. This demonstrates the component without per-request complexity (MVP scope).
- **CSRF on POST handlers**: POST handlers follow the same pattern as `rooms.go` and `users.go` — no `csrf()` wrapper in stub phase; comment `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production.`
- **`{id}` path value**: Use `r.PathValue("id")` to extract the request ID from the URL.
- **Template key**: The template is rendered with `h.tmpl.render(w, "compliance", data)`, so the file must be `templates/compliance.html`.

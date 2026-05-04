---
status: ready-for-dev
epic: 9
story: 5
security_review: required
---

# Story 9.5: Admin UI — Compliance API Integration

Status: ready-for-dev

## Story

As a compliance officer,
I want the Compliance Request UI to approve and reject requests via the real API,
So that decisions are persisted and audited correctly.

## Acceptance Criteria

1. Pending compliance request in DB → click "Approve" → `POST /api/v1/admin/compliance/{requestId}/approve` called, status → `approved`, audit log written.
2. Click "Reject" → `POST /api/v1/admin/compliance/{requestId}/reject` called with rejection reason, status → `rejected`.
3. `gateway/internal/admin/compliance_handler.go` — zero matches for `TODO(epic-6)`.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `Approve compliance request calls real API and persists status` — Playwright
   - File: `e2e/tests/features/admin/compliance-api-integration.spec.ts`
   - Given: full dev stack running (`make dev`), compliance request in DB with status `pending`
   - When: admin logs in, navigates to `/admin/compliance`, clicks "Approve"
   - Then: page redirects with flash "Approved", the real DB `compliance_requests.status = 'approved'`,
           audit event `compliance_access_approved` written (not a stub mutation)

2. `Reject compliance request calls real API and persists status` — Playwright
   - File: `e2e/tests/features/admin/compliance-api-integration.spec.ts`
   - Given: full dev stack, pending compliance request in DB
   - When: admin clicks "Reject" (with rejection reason)
   - Then: page redirects with flash "Rejected", real DB `compliance_requests.status = 'rejected'`,
           audit event `compliance_access_rejected` written

3. `Zero TODO(epic-6) markers remain in compliance_handler.go` — Go test
   - File: `gateway/internal/admin/compliance_todo_test.go`
   - Given: `gateway/internal/admin/compliance_handler.go` is the target file
   - When: the test scans for the literal string `TODO(epic-6)`
   - Then: zero matches found; test fails if any marker is present

### Note on Playwright tests (AC1–AC2)

These tests require the full dev stack (`make dev`) with a real PostgreSQL database.
They follow the OIDC Authorization Code + PKCE login pattern using the shared `loginAsAdmin`
helper from `e2e/tests/fixtures/helpers.ts`.

Since the compliance handler for the admin UI (POST /admin/compliance/{id}/approve|reject)
calls directly into the DB (bypassing the compliance_officer JWT role gate, because the
admin session guard already verifies authorization), the tests verify the actual database
state changes rather than stub mutations.

**AC3 implementation:** create `gateway/internal/admin/compliance_todo_test.go` mirroring
the pattern from `gateway/internal/admin/users_todo_test.go`.

## Dev Notes

### Architecture decision

The Admin UI compliance handlers (`/admin/compliance/{id}/approve` and `/admin/compliance/{id}/reject`)
are accessed by instance_admin users via session-based auth (not JWT compliance_officer tokens).
The existing `compliance.AccessRequestHandler.PostApprove/PostReject` requires `ContextKeySystemRole=compliance_officer`
via JWT middleware — calling it from the admin UI would require spoofing the role.

**Chosen approach:** define a minimal `ComplianceApprovalClient` interface in the admin package
that performs the DB update + audit emission directly, bypassing the JWT role gate. This is
correct because:
- The admin session guard already verifies the caller is instance_admin
- The four-eyes flow means the admin is the designated approver
- No import cycle (admin → compliance-level DB access)

The interface:
```go
type ComplianceApprovalClient interface {
    Approve(ctx context.Context, requestID, approverSub, note string) error
    Reject(ctx context.Context, requestID, approverSub, note string) error
}
```

### Files to create/modify

- **Create:** `gateway/internal/admin/compliance_todo_test.go` (AC3 test — written first, RED)
- **Create:** `e2e/tests/features/admin/compliance-api-integration.spec.ts` (AC1-AC2 — written first, RED)
- **Modify:** `gateway/internal/admin/compliance_handler.go` — add interface, inject client, replace stub mutations
- **Modify:** `gateway/cmd/gateway/main.go` — pass `complianceDB` and `coreClient` to `NewComplianceHandler`

### contextWithAdminIdentity

Admin-initiated approve/reject must set `x-user-id` in gRPC context for audit log attribution.
Use `contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))` before audit emission.

---
id: 7-17
type: security
security_review: required
created: 2026-04-30
---

# Story 7.17: CSRF-Enforcement + Body-Size-Limits auf allen Admin POST-Routes

Status: ready-for-dev

## Story

As a security engineer,
I want every Admin POST route to be wrapped with `bodyLimit64KiB(csrf(sessionGuard(...)))`,
so that CSRF attacks and oversized-body DoS are prevented consistently across the entire admin surface.

## Context / Background

The Epic 7 security review (`epic-7-security-review-2026-04-30.md`) identified a **HIGH** finding: 10 Admin POST routes added during Epic 7 lack CSRF protection. Additionally a **MEDIUM** finding: those same routes have no body-size limit.

**Root cause:** Stories 7.6, 7.7, 7.9, 7.10, 7.11, and 7.15 each added POST handlers with a `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production` comment as a deliberate stub-phase deferral. Story 7.14 added two cleanup routes (reactivate, unarchive) with the same omission. The existing `POST /admin/logout` at main.go:303 already demonstrates the correct triple-wrapper pattern:

```go
mux.Handle("POST /admin/logout", bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler)))))
```

The affected routes (all in `gateway/cmd/gateway/main.go`):

| Line | Route |
|------|-------|
| 315 | `POST /admin/users/{userId}/display-name` |
| 317 | `POST /admin/users/{userId}/role` |
| 318 | `POST /admin/users/{userId}/deactivate` |
| 320 | `POST /admin/users/{userId}/reactivate` |
| 325 | `POST /admin/rooms/{roomId}/name` |
| 326 | `POST /admin/rooms/{roomId}/archive` |
| 328 | `POST /admin/rooms/{roomId}/unarchive` |
| 333 | `POST /admin/config` |
| 339 | `POST /admin/config/role-mapping` |
| 345 | `POST /admin/compliance/{id}/approve` |
| 346 | `POST /admin/compliance/{id}/reject` |

The HTML templates already embed `_csrf` hidden fields (generated via `CSRFTokenFromContext`), so no template changes are required. This story is a purely mechanical wiring fix.

## Acceptance Criteria

1. Every POST route listed above is wrapped as `bodyLimit64KiB(csrf(sessionGuard(...)))` — identical to the `POST /admin/logout` pattern.

2. No `TODO(story-7-csrf)` comments remain in `gateway/cmd/gateway/main.go` or any handler file.

3. A POST request to any of the listed routes **without** a valid `_csrf` token returns HTTP 403.

4. A POST request to any of the listed routes with a request body **larger than 64 KiB** returns HTTP 413.

5. All existing GET routes (`GET /admin/users`, `GET /admin/users/{userId}`, `GET /admin/rooms`, `GET /admin/rooms/{roomId}`, `GET /admin/config`, `GET /admin/config/role-mapping`, `GET /admin/compliance`) continue to issue and rotate CSRF tokens correctly — no regression on the GET side.

6. All existing Playwright smoke tests (`e2e/features/`) continue to pass after the wiring change.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [CSRF rejection on every protected POST] — Go httptest (`gateway/internal/admin/csrf_body_limit_test.go`)
   - Given: a running test server with the full admin mux (including CSRF middleware), authenticated admin session cookie
   - When: POST to each of the 11 routes **without** `_csrf` field in the form body
   - Then: HTTP 403 for each route

2. [Valid CSRF token is accepted] — Go httptest
   - Given: a running test server, authenticated session, valid CSRF token obtained from the corresponding GET page
   - When: POST to each route with the correct `_csrf` token
   - Then: HTTP 200 or HTTP 302 (redirect after PRG), not 403

3. [Body limit enforcement] — Go httptest
   - Given: a running test server, authenticated session, valid CSRF token
   - When: POST to each route with a body of exactly 65537 bytes (64 KiB + 1)
   - Then: HTTP 413 for each route

4. [GET routes still rotate tokens] — Go httptest (regression guard)
   - Given: a running test server, authenticated session
   - When: GET /admin/users, GET /admin/rooms, GET /admin/config, GET /admin/config/role-mapping, GET /admin/compliance
   - Then: response body contains a `_csrf` hidden field with a non-empty value

5. [Playwright smoke: user deactivate flow still works] — Playwright (regression, `e2e/features/`)
   - Given: stack running, admin logged in via real OIDC flow
   - When: navigate to a user detail page, click Deactivate, confirm dialog
   - Then: page reloads with success flash banner, no 403 error

## Implementation Notes

**Single file to change:** `gateway/cmd/gateway/main.go`

Wrap each affected line by replacing the current `sessionGuard(...)` call with `bodyLimit64KiB(csrf(sessionGuard(...)))`. Example diff for the first route:

```go
// Before:
mux.Handle("POST /admin/users/{userId}/display-name", sessionGuard(http.HandlerFunc(usersHandler.UpdateDisplayNameHandler)))

// After:
mux.Handle("POST /admin/users/{userId}/display-name", bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(usersHandler.UpdateDisplayNameHandler)))))
```

Apply the same pattern to all 11 routes in order (lines 315–346).

**Remove stale TODO comments:** Delete the `// Story 7.6: ... no csrf() wrapper (stub phase; see TODO in handler)` style inline comments on lines 313, 316, 319, 324, 332, 338, 344 — they are no longer accurate. Replace with a concise, accurate comment if needed, e.g.:

```go
// CSRF + body limit enforced via middleware (Story 7.17).
```

**Handler files to check for leftover TODOs:**
- `gateway/internal/admin/users.go` — search for `TODO(story-7-csrf)`
- `gateway/internal/admin/rooms.go` — search for `TODO(story-7-csrf)`
- `gateway/internal/admin/config.go` — line 38: `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production.`
- `gateway/internal/admin/role_mapping.go` — line 45: `// TODO(story-7-csrf): enforce CSRF middleware when wiring in production.`
- `gateway/internal/admin/compliance_handler.go` — search for `TODO(story-7-csrf)`

Remove all such TODO comments.

**`bodyLimit64KiB` and `csrf` helpers** are already defined and in use at main.go:303 — no new middleware code is needed.

**Test file to create:** `gateway/internal/admin/csrf_body_limit_test.go` — use `net/http/httptest` and the existing `setupTestMux` helper (or create one that wires the full admin mux with real CSRF middleware). Follow the pattern in existing httptest files in `gateway/internal/admin/`.

**Security-Gate 1 (per-story):** Required. This story touches `gateway/internal/admin/` and adds CSRF enforcement to all admin state-changing endpoints.

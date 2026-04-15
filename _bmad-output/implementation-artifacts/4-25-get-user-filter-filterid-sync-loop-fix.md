# Story 4.25: GET /user/{userId}/filter/{filterId} — Sync-Loop-Fix für Element Web

Status: done

## Story

As a developer,
I want `GET /_matrix/client/v3/user/{userId}/filter/{filterId}` to return a valid passthrough filter definition,
so that Element Web can reconnect after page reload without entering a permanent sync ERROR loop.

---

## Background / Motivation

`POST /user/{userId}/filter` (existing stub) returns `{"filter_id":"0"}`.
On reconnect, Element Web calls `GET /user/{userId}/filter/0` to restore the stored filter.
Without this endpoint, the client received 404 → sync state goes to ERROR → infinite retry loop visible in console:
`"Getting filter failed" / "sync state => ERROR"`.

This was the root cause of the sync-loop crash seen in Element Web after every page reload.

---

## Acceptance Criteria

1. `GET /_matrix/client/v3/user/{userId}/filter/{filterId}` is registered in `main.go` and protected by JWT middleware.

2. Any `filterId` returns HTTP 200 with a valid JSON passthrough filter object containing at least the `"room"` key (e.g. `{"room":{"timeline":{"limit":50}},"event_fields":[],"event_format":"client","presence":{"not_types":[]},"account_data":{"not_types":[]}}`).

3. Requests for a `userId` that does not match the authenticated user return 403 `M_FORBIDDEN`.

4. Unauthenticated requests (no Bearer token) return 401 `M_MISSING_TOKEN`.

5. After a page reload in Element Web, no "Getting filter failed" or "sync state => ERROR" messages appear in the browser console.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestGetFilter_HappyPath` — Go httptest
   - Given: authenticated user `@filtertest:test.local`, requests `GET /user/{userId}/filter/0`
   - When: handler processes the request
   - Then: 200 OK, valid JSON with `room` key present, `Content-Type` set

2. `TestGetFilter_UnknownFilterIdReturns200` — Go httptest
   - Given: authenticated user requests `filter/999` (non-existent filter)
   - When: handler processes
   - Then: 200 OK (stateless MVP — any ID returns default)

3. `TestGetFilter_Unauthenticated` — Go httptest
   - Given: request without Authorization header
   - Then: 401 M_MISSING_TOKEN from JWTMiddleware

4. `TestGetFilter_WrongUser_Forbidden` — Go httptest
   - Given: authenticated as `@filtertest:test.local`, requests filter for `@other-user:test.local`
   - Then: 403 M_FORBIDDEN

5. Browser E2E (element_e2e.spec.ts): "Reconnect after reload — no sync ERROR loop"
   - Given: SSO login completed
   - When: page reloads (Element fetches GET /filter/0)
   - Then: room list visible, zero "Getting filter failed" console errors

---

## Implementation Notes

- Handler: `gateway/internal/matrix/filter.go` — `FilterHandler`, zero gRPC dependencies
- Ownership check: compares `r.PathValue("userId")` with `ContextKeyUserID` (full Matrix ID)
- Static passthrough filter: `passthroughFilter` var — no DB, no gRPC
- Registered in `main.go` after the existing POST filter stub

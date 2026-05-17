---
status: ready-for-dev
epic: 14
story: 2c
security_review: not-needed
matrix: false
ui: true
---

# Story 14.2c: Admin UI User Search OIDC Integration + "Not yet logged in" Badge

Status: ready-for-dev

## Story

As an instance admin,
I want the Admin UI user search to merge results from the Nebu DB and the OIDC directory,
So that I can find and preview OIDC users who have never logged into Nebu.

**Size:** S
**security_review:** not-needed

---

## Acceptance Criteria

**AC1 — OIDC-only user appears with "Not yet logged in" badge:**
Given `oidc_directory_enabled: true`,
When an admin searches for a user who exists in the OIDC directory but has never logged into Nebu,
Then the user appears in the search results with a "Not yet logged in" badge and their Matrix User ID is shown as a computed preview (not yet stored in the DB)

**AC2 — OIDC disabled falls back to DB-only:**
Given `oidc_directory_enabled: false`,
When user search runs,
Then only Nebu DB users are returned — backward-compatible behavior with no OIDC calls made

**AC3 — OIDC provider unavailable returns DB-only + warning banner:**
Given the OIDC provider is temporarily unavailable,
When user search runs with directory integration enabled,
Then the search returns Nebu DB results only and shows a non-blocking warning banner: "OIDC directory temporarily unavailable"

**AC4 — Playwright+Gherkin E2E scenario passes:**
Given a Playwright+Gherkin scenario in `e2e/features/admin/oidc_directory_search.feature`,
When `make test-integration` runs,
Then the scenario "search finds OIDC-only user with Not yet logged in badge" passes

**AC5 — Admin-only access gate enforced:**
Given the search handler calls the OIDC directory service,
When the request context does not carry a valid admin session (SessionGuard not passed),
Then the handler is never reached — enforced by existing SessionGuard middleware on all /admin/* routes

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. TestUsersSearch_OIDCMerge_NotYetLoggedIn — httptest (Unit)**
Location: `gateway/internal/admin/users_oidc_search_test.go`
- Given: a UsersHandler with a mock OIDCDirectoryService returning one user (sub="frank.oidc", display_name="Frank OIDC", email="frank@idp.example.com") and a mock gRPC core returning one Nebu DB user (Alice Müller)
- When: `GET /admin/users?q=frank` is called
- Then: response body contains "Frank OIDC" with text "Not yet logged in" (or badge containing that phrase)
- And: the computed Matrix User ID preview is present (e.g. `@frank.oidc:`)

**2. TestUsersSearch_OIDCDisabled_NoOIDCCalls — httptest (Unit)**
Location: `gateway/internal/admin/users_oidc_search_test.go`
- Given: a UsersHandler with `oidc_directory_enabled: false` (OIDCDirectoryService returns empty list immediately)
- When: `GET /admin/users?q=frank` is called
- Then: response body does NOT contain "Not yet logged in"
- And: zero outbound OIDC HTTP calls are made (verified via a mock that fails the test if called)

**3. TestUsersSearch_OIDCUnavailable_WarningBanner — httptest (Unit)**
Location: `gateway/internal/admin/users_oidc_search_test.go`
- Given: a UsersHandler with an OIDC directory service whose endpoint is unreachable
- When: `GET /admin/users?q=alice` is called
- Then: response body contains the Nebu DB users (Alice Müller)
- And: response body contains "OIDC directory temporarily unavailable" warning text

**4. TestUsersSearch_DeduplicatesByNebusMatrixUserID — httptest (Unit)**
Location: `gateway/internal/admin/users_oidc_search_test.go`
- Given: OIDC directory returns a user whose sub matches the Matrix localpart of an existing Nebu DB user
- When: `GET /admin/users` is called
- Then: only one row is returned for that user (no duplicate) — Nebu DB entry wins

**5. Playwright+Gherkin E2E: "search finds OIDC-only user with Not yet logged in badge"**
Location: `e2e/features/admin/oidc_directory_search.feature` + `e2e/step-definitions/admin/oidc-directory-search.steps.ts`
- Given: OIDC directory is enabled in server config and the mock OIDC endpoint has user "diana.oidc" registered
- When: the admin navigates to /admin/users and searches for "diana"
- Then: "diana.oidc" appears in the list with a "Not yet logged in" badge

---

## Dev Notes

### Design: Where to Wire the OIDC Directory Service

The search merge logic lives in `UsersHandler.ListHandler`:

1. When `oidc_directory_enabled: true` (read from server config or stubConfig): call `OIDCDirectoryService.Allow(sessionID)` then `FetchUsers(ctx)` before returning results.
2. Merge results: Nebu DB users take precedence. OIDC-only users (not in DB) get `IsOIDCOnly: true` flag → rendered with "Not yet logged in" badge.
3. Filter merged slice by `q` (search query) — same logic as today for Nebu DB users.
4. On OIDC fetch error (empty list returned + service logged warning): show `OIDCWarning: true` → rendered as a non-blocking warning banner in users.html.

### Key Types to Add

```go
// UserRowData extension (page_data.go):
type UserRowData struct {
    StubUser
    Badge       StatusBadgeData
    IsOIDCOnly  bool   // true = user is in OIDC dir but has never logged into Nebu
    MatrixIDPreview string // computed: "@{localpart}:{serverName}" — only set when IsOIDCOnly
}
```

Add `OIDCWarning bool` and `OIDCWarningMessage string` to `UsersPageData`.

### OIDC Directory Service Wiring

`UsersHandler` needs to accept an optional `*OIDCDirectoryService`:

```go
type UsersHandler struct {
    tmpl   *TemplateHandler
    core   AdminUsersClient
    oidcDir *OIDCDirectoryService // nil = OIDC disabled or test path without OIDC
    serverName string             // e.g. "example.com" — used for MatrixIDPreview
}
```

`NewUsersHandler` remains backward-compatible (no required params added). Add `WithOIDCDirectory(svc *OIDCDirectoryService, serverName string) *UsersHandler` setter.

### Matrix User ID Preview

When `IsOIDCOnly: true`: `MatrixIDPreview = "@" + sanitize(sub) + ":" + serverName`

`sanitize(sub)` replaces all characters not in `[a-z0-9._=-]` with `_` and lowercases — Matrix localpart safe subset (RFC 1459 + Matrix spec).

### Deduplication

Nebu DB users have a stored Matrix User ID (e.g. `@alice:example.com`). OIDC users have `sub`. Dedup: extract localpart from Nebu user's MatrixUserID (`@{localpart}:{serverName}`) and compare with `sanitize(oidcUser.Sub)`. If match → drop OIDC entry (Nebu DB entry wins).

### Admin-Only Access Gate

`SessionGuard` middleware already wraps all `/admin/*` routes in `cmd/gateway/main.go`. The handler is only reachable after a valid admin session cookie check. The `Allow(sessionID)` call in the handler uses the sessionID from the verified JWT context (via `AdminSubFromContext`).

### File Locations

- Handler extension: `gateway/internal/admin/users.go` (extend `UsersHandler` and `ListHandler`)
- New types: `gateway/internal/admin/page_data.go` (extend `UserRowData`, `UsersPageData`)
- Unit tests: `gateway/internal/admin/users_oidc_search_test.go` (new file)
- E2E feature: `e2e/features/admin/oidc_directory_search.feature` (new file)
- E2E steps: `e2e/step-definitions/admin/oidc-directory-search.steps.ts` (new file)
- Template update: `gateway/internal/admin/templates/users.html` (add "Not yet logged in" badge + warning banner)

### Security Compliance (from Story 14.2b note)

The search handler MUST call `Allow(sessionID)` before `FetchUsers(ctx)`. The sessionID is obtained from `AdminSubFromContext(r.Context())` — the verified sub claim from the admin JWT, set by SessionGuard. Never use IP or a request header as the rate-limit key (CR-5).

---

## Prerequisites / Dependencies

- Story 14.2a must be complete (oidc_directory_enabled + oidc_directory_endpoint in DB and served by GET/PATCH /api/v1/admin/config) ✅ Done
- Story 14.2b must be complete (OIDCDirectoryService with cache, rate limit, HTTPS-only) ✅ Done

---

## Out of Scope

- SCIM 2.0 integration (separate story)
- Persistent storage of OIDC-preview users in the DB
- Private IP SSRF blocking (follow-up from 14.2b)
- User invite/provision flow for OIDC-only users

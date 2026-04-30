---
id: 7-18
type: security
security_review: optional
created: 2026-04-30
---

# Story 7.18: Flash-Message Allowlist auf Admin GET-Handlern

Status: ready-for-dev

## Story

As a security engineer,
I want every Admin GET handler that reads the `?flash=` query parameter to validate it against a fixed allowlist,
so that arbitrary strings cannot be injected via crafted URLs, even though `html/template` prevents XSS.

## Context / Background

The Epic 7 security review (`epic-7-security-review-2026-04-30.md`) identified a **MEDIUM** finding: five GET handlers read `r.URL.Query().Get("flash")` verbatim and pass the raw string directly into `AlertBannerData.Message`.

**Affected files and lines:**

| File | Lines | Handler |
|------|-------|---------|
| `gateway/internal/admin/users.go` | 94–95 | `UsersHandler.DetailHandler` |
| `gateway/internal/admin/rooms.go` | 92–93 | `RoomsHandler.DetailHandler` |
| `gateway/internal/admin/config.go` | 24–25 | `ConfigHandler.Handler` |
| `gateway/internal/admin/role_mapping.go` | 31–32 | `RoleMappingHandler.Handler` |
| `gateway/internal/admin/compliance_handler.go` | 28–29 | `ComplianceHandler.ListHandler` |

**Why it matters:** `html/template` auto-escaping prevents classic XSS. However, any string can be injected into the flash banner via a URL like `/admin/users/alice?flash=System+Error:+Please+re-enter+your+password`. This enables social engineering attacks: a phishing link constructed to look like a legitimate admin URL can display a convincing fake error or prompt to a logged-in admin. The `AlertBannerData.Message` field is rendered visibly inside the admin UI.

**Fix approach — allowlist with silent fallback:**

Define a package-level allowlist of known-safe flash values. On each GET handler, validate the query param against this list. Unknown values → empty string (no banner shown, no error). Values exceeding 80 characters → also rejected (belt-and-suspenders, prevents long strings even if added to allowlist by mistake in future).

**Known-safe flash values** (produced by the corresponding POST handlers via PRG redirect):

- `"Role updated"` — from `UpdateRoleHandler`
- `"Display name updated"` — from `UpdateDisplayNameHandler`
- `"User deactivated"` — from `DeactivateUserHandler`
- `"User reactivated"` — from `ReactivateUserHandler`
- `"Room name updated"` — from `UpdateRoomNameHandler`
- `"Room archived"` — from `ArchiveRoomHandler`
- `"Room unarchived"` — from `UnarchiveRoomHandler`
- `"Config updated"` — from `UpdateConfigHandler`
- `"Role mapping updated"` — from `RoleMappingHandler.UpdateHandler`
- `"Approved"` — from `ComplianceHandler.ApproveHandler`
- `"Rejected"` — from `ComplianceHandler.RejectHandler`

## Acceptance Criteria

1. A shared `allowedFlashMessages` set (or map) is defined in the `admin` package (e.g. in a `flash.go` file or alongside the `AlertBannerData` type) containing exactly the 11 known-safe values listed above.

2. A `sanitizeFlash(msg string) string` function (or equivalent) validates the query param:
   - If `len(msg) > 80` → return `""`
   - If `msg` is not in the allowlist → return `""`
   - Otherwise → return `msg` unchanged

3. All five GET handlers use `sanitizeFlash(r.URL.Query().Get("flash"))` instead of reading the query param directly.

4. A flash value in the allowlist (e.g. `"Config updated"`) is rendered in the banner — no regression for the normal POST→redirect→GET happy path.

5. A flash value not in the allowlist (e.g. `"Please re-enter your credentials"`) results in no banner being shown (empty `AlertBannerData`).

6. A flash value longer than 80 characters results in no banner, even if the truncated prefix matches an allowlist entry.

7. An empty `?flash=` query param (or no `?flash=` at all) results in no banner — same behaviour as before.

8. All existing Playwright smoke tests continue to pass — the known flash values produced by the POST handlers are in the allowlist, so PRG redirects still display banners.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Unit: sanitizeFlash — valid values pass through] — Go unit test (`gateway/internal/admin/flash_test.go`)
   - Given: each of the 11 allowlist values
   - When: `sanitizeFlash(value)`
   - Then: returns the value unchanged

2. [Unit: sanitizeFlash — unknown value is rejected] — Go unit test
   - Given: `msg = "Please re-enter your credentials"`
   - When: `sanitizeFlash(msg)`
   - Then: returns `""`

3. [Unit: sanitizeFlash — oversized value is rejected] — Go unit test
   - Given: `msg = strings.Repeat("x", 81)`
   - When: `sanitizeFlash(msg)`
   - Then: returns `""`

4. [Unit: sanitizeFlash — empty string is a no-op] — Go unit test
   - Given: `msg = ""`
   - When: `sanitizeFlash(msg)`
   - Then: returns `""`

5. [Handler integration: valid flash → banner rendered] — Go httptest
   - Given: authenticated admin session, GET /admin/config?flash=Config+updated
   - When: handler runs
   - Then: response body contains the flash message text "Config updated" inside an alert element

6. [Handler integration: unknown flash → no banner] — Go httptest
   - Given: authenticated admin session, GET /admin/config?flash=Hacked
   - When: handler runs
   - Then: response body does NOT contain "Hacked"; response is HTTP 200

7. [Handler integration: oversized flash → no banner] — Go httptest
   - Given: authenticated admin session, GET /admin/users/alice?flash= + 81 'a' characters
   - When: handler runs
   - Then: response body does NOT contain the injected string; response is HTTP 200

8. [Handler integration: all five handlers reject unknown flash] — Go httptest
   - Given: authenticated admin session
   - When: GET /admin/users/{userId}?flash=BAD, GET /admin/rooms/{roomId}?flash=BAD, GET /admin/config?flash=BAD, GET /admin/config/role-mapping?flash=BAD, GET /admin/compliance?flash=BAD
   - Then: HTTP 200 for each, no "BAD" text in any response body

## Implementation Notes

**New file:** `gateway/internal/admin/flash.go`

```go
package admin

// allowedFlashMessages is the exhaustive list of flash values that GET handlers
// may display. Any value not in this set is silently dropped (Story 7.18).
var allowedFlashMessages = map[string]struct{}{
    "Role updated":         {},
    "Display name updated": {},
    "User deactivated":     {},
    "User reactivated":     {},
    "Room name updated":    {},
    "Room archived":        {},
    "Room unarchived":      {},
    "Config updated":       {},
    "Role mapping updated": {},
    "Approved":             {},
    "Rejected":             {},
}

// sanitizeFlash returns msg if it is a known-safe flash value and does not
// exceed 80 characters. Otherwise it returns the empty string.
func sanitizeFlash(msg string) string {
    if len(msg) > 80 {
        return ""
    }
    if _, ok := allowedFlashMessages[msg]; !ok {
        return ""
    }
    return msg
}
```

**Files to update** — replace the verbatim `r.URL.Query().Get("flash")` call in the flash block of each handler with `sanitizeFlash(r.URL.Query().Get("flash"))`:

- `gateway/internal/admin/users.go` line 94
- `gateway/internal/admin/rooms.go` line 92
- `gateway/internal/admin/config.go` line 24
- `gateway/internal/admin/role_mapping.go` line 31
- `gateway/internal/admin/compliance_handler.go` line 28

**Test file:** `gateway/internal/admin/flash_test.go` — pure unit tests for `sanitizeFlash`, no HTTP server needed. Handler-level integration tests go in the respective `*_test.go` files (e.g. `users_test.go`, `config_test.go`).

**Verify POST handler redirect URLs** match the allowlist exactly. Search all POST handlers for `http.Redirect` calls and confirm the `?flash=` value is in the allowlist. If any POST handler uses a different string (e.g. `"name updated"` vs `"Display name updated"`), update the POST handler's redirect to use the canonical allowlist value and add the corresponding entry.

**Security-Gate 1 (per-story):** Optional. `html/template` auto-escaping prevents XSS; the risk is social engineering only. However, a per-story security review is recommended given the number of handler files touched.

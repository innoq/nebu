# Story 3.10: OIDC Callback Handler + Session Cookie

Status: done

## Story

As a gateway,
I want to validate the OIDC callback, exchange the authorization code for tokens, and create an admin session,
so that only authenticated and authorized operators can access the Admin UI.

## Acceptance Criteria

1. `GET /admin/callback` handler:
   - Reads `state` and `code` from query params
   - Reads `admin_oidc_state` cookie; verifies `state` matches; returns `400` if mismatch (CSRF protection)
   - Exchanges `code` + `code_verifier` (from cookie) for tokens via `oauth2.Exchange` with `oauth2.VerifierOption(verifier)` (PKCE)
   - Validates the returned ID token (signature, `iss`, `aud`, `exp`) using the OIDC provider's JWKS
   - Extracts `sub`, `email` claims from the ID token
   - Checks that the user has `instance_admin` role via `auth.MapSystemRole(rawRole)` on the claim configured in `a.claimName`; if not `"instance_admin"`: `403` with an HTML error page (not plain text)
   - Creates a signed admin session token (HMAC-SHA256 with internal secret, payload: `sub`, `email`, `role`, `exp = now+8h`)
   - Sets `admin_session` cookie: `HttpOnly`, `Secure: r.TLS != nil`, `SameSite=Strict`, `MaxAge=28800`, `Path=/admin`
   - Deletes the `admin_oidc_state` cookie (set `Max-Age=-1`, `Path=/admin`)
   - Redirects `303` to `/admin/dashboard`
2. `GET /admin/logout` handler:
   - Deletes the `admin_session` cookie (sets `Max-Age=-1`, `Path=/admin`, `HttpOnly=true`)
   - Redirects `303` to `/admin/login`
3. Unit tests cover: state mismatch → 400, role check failure → 403, valid flow → `admin_session` cookie set + 303 redirect to `/admin/dashboard`, logout → cookie deleted + 303 redirect to `/admin/login`
4. `make test-unit-go` passes with zero regressions

## Tasks / Subtasks

- [x] Task 1: Refactor `CallbackHandler` in `gateway/internal/admin/auth.go` (AC: 1)
  - [x] 1.1 Change redirect on token exchange failure from `/admin/auth/login?error=auth_failed` to `/admin/login?error=auth_failed`
  - [x] 1.2 Change redirect on ID token verification failure from `/admin/auth/login?error=auth_failed` to `/admin/login?error=auth_failed`
  - [x] 1.3 Change redirect on claims extraction failure from `/admin/auth/login?error=auth_failed` to `/admin/login?error=auth_failed`
  - [x] 1.4 Add role check: after extracting `rawRole` and calling `auth.MapSystemRole`, if `systemRole != "instance_admin"` → respond with `403` using `http.Error(w, "Access denied: instance_admin role required.", http.StatusForbidden)` (plain text is acceptable; full error page template is Story 3.12 scope)
  - [x] 1.5 Change final redirect from `http.Redirect(w, r, "/admin/", http.StatusSeeOther)` to `http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)`
  - [x] 1.6 Ensure the `admin_oidc_state` cookie deletion uses `Path: "/admin"` and `MaxAge: -1` (already correct from Story 3.9 change; confirm no regression)

- [x] Task 2: Add `LogoutHandler` method to `AdminAuth` in `gateway/internal/admin/auth.go` (AC: 2)
  - [x] 2.1 Add `LogoutHandler(w http.ResponseWriter, r *http.Request)` method:
    ```go
    func (a *AdminAuth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
        http.SetCookie(w, &http.Cookie{
            Name:     "admin_session",
            Value:    "",
            Path:     "/admin",
            MaxAge:   -1,
            HttpOnly: true,
            Secure:   r.TLS != nil,
            SameSite: http.SameSiteStrictMode,
        })
        http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
    }
    ```

- [x] Task 3: Register `LogoutHandler` route in `gateway/cmd/gateway/main.go` (AC: 2)
  - [x] 3.1 Add `mux.HandleFunc("GET /admin/logout", adminAuth.LogoutHandler)` after the existing canonical routes block

- [x] Task 4: Write unit tests in `gateway/internal/admin/callback_test.go` (AC: 3–4)
  - [x] 4.1 Create `gateway/internal/admin/callback_test.go` (package `admin`)
  - [x] 4.2 `TestCallbackHandler_StateMismatch_Returns400`: valid `admin_oidc_state` cookie with `state="expected"`, request with `state="wrong"` → expect 400
  - [x] 4.3 `TestCallbackHandler_RoleCheckFails_Returns403`: complete valid OIDC flow but token endpoint returns a JWT with `nebu_role="user"` → expect 403
  - [x] 4.4 `TestCallbackHandler_ValidFlow_SetsSessionCookieAndRedirects`: complete valid OIDC flow with `nebu_role="instance_admin"` → expect 303, `admin_session` cookie set with `HttpOnly=true`, `SameSite=Strict`, `MaxAge=28800`, `Path=/admin`; Location header = `/admin/dashboard`; `admin_oidc_state` cookie deleted (MaxAge <= 0)
  - [x] 4.5 `TestCallbackHandler_ValidFlow_SessionCookieSecureTLS`: same as 4.4 but with `req.TLS = &tls.ConnectionState{}` → assert `admin_session` cookie has `Secure=true`
  - [x] 4.6 `TestLogoutHandler_DeletesCookieAndRedirects`: GET `/admin/logout` → 303, `admin_session` cookie MaxAge <= 0, Location = `/admin/login`

- [x] Task 5: Fix existing `auth_test.go` tests that assert the old redirect URL (AC: 4)
  - [x] 5.1 `TestAdminCallbackHandler_TokenExchangeFailure_Redirects`: update assertion from `"/admin/auth/login?error=auth_failed"` to `"/admin/login?error=auth_failed"`
  - [x] 5.2 Run `make test-unit-go` to confirm zero regressions across all packages

## Dev Notes

### Critical: What Already Exists vs. What Changes

Story 3.9 fully implemented `CallbackHandler` at `GET /admin/callback` with the following behavior:
- State cookie validation, PKCE token exchange, ID token verification, claims extraction — ALL implemented
- Sets `admin_session` cookie (`HttpOnly`, `Secure: r.TLS != nil`, `SameSite=Strict`, `MaxAge=28800`, `Path=/admin`)
- **CURRENT BEHAVIOR (wrong for 3.10):** Redirects to `/admin/` (trailing slash)
- **CURRENT BEHAVIOR (wrong for 3.10):** On failure, redirects to `/admin/auth/login?error=auth_failed`
- **MISSING:** Role check (currently sets `admin_session` even for non-`instance_admin` users)

This story is primarily a targeted refactor of `CallbackHandler` + adding `LogoutHandler`. DO NOT rewrite `CallbackHandler` from scratch — it is already well-tested.

### File: `gateway/internal/admin/auth.go`

Current `CallbackHandler` at line 282. The three changes are:
1. **Lines 324, 330, 337** (approximately): change redirect target from `/admin/auth/login?error=auth_failed` → `/admin/login?error=auth_failed`
2. **After line 349** (after `systemRole := auth.MapSystemRole(rawRole)`): add role check block
3. **Line 381**: change `"/admin/"` → `"/admin/dashboard"` and `StatusFound` → `StatusSeeOther`

Role check block to insert:
```go
if systemRole != "instance_admin" {
    http.Error(w, "Access denied: instance_admin role required.", http.StatusForbidden)
    return
}
```

### File: `gateway/internal/admin/auth_test.go`

Line 343 asserts: `if !strings.Contains(location, "/admin/auth/login?error=auth_failed")`. Must change to `/admin/login?error=auth_failed` after Task 1 changes.

The existing `TestAdminCallbackHandler_MissingCookie_Returns400`, `TestAdminCallbackHandler_StateMismatch_Returns400`, and `TestAdminCallbackHandler_ExpiredCookie_Returns400` tests remain valid — no changes needed to those assertions.

### New Tests: Use Existing Helpers from `auth_test.go`

The new `callback_test.go` can reuse helpers from `auth_test.go` (same package `admin`):
- `setupAdminOIDCServer(t)` — creates a test OIDC server that signs JWTs with `nebu_role: "instance_admin"` and `email: "admin@example.com"`
- `signAdminJWT(t, issuer, key, expiry, claimName, claimValue)` — use to build custom JWTs with different role values
- `newTestAdminAuth(t, provider)` — creates `AdminAuth` with `provider`, `nil` db and tmpl; for callback tests, the full `CallbackHandler` uses `a.provider.Inner()` for token verification

For `TestCallbackHandler_RoleCheckFails_Returns403`, use a modified OIDC server where `/token` returns a JWT signed with `nebu_role: "user"` (not `instance_admin`). Pattern:

```go
srv, key := setupAdminOIDCServer(t)
// Override token endpoint to return "user" role
// ... (see auth_test.go TestAdminCallbackHandler_TokenExchangeFailure_Redirects for building custom server)
```

Alternatively, replace the token endpoint handler in a custom server copy. Use `signAdminJWT(t, srv.URL, key, time.Now().Add(time.Hour), "nebu_role", "user")` in the token endpoint.

### adminSessionCookie Struct (Already Defined)

The `adminSessionCookie` struct already has a `Role` field (from Story 2.6):
```go
type adminSessionCookie struct {
    Sub   string `json:"sub"`
    Email string `json:"email"`
    Role  string `json:"role"` // mapped system_role
    Exp   int64  `json:"exp"`
}
```
No struct changes needed. The `Role` field is already populated in `CallbackHandler` with `auth.MapSystemRole(rawRole)`.

### Cookie Contract (Established in Story 3.9 — Must Match)

| Cookie | Name | Path | MaxAge | HttpOnly | Secure | SameSite |
|--------|------|------|--------|----------|--------|----------|
| OIDC state | `admin_oidc_state` | `/admin` | 600 (10min) | yes | `r.TLS != nil` | Lax |
| Admin session | `admin_session` | `/admin` | 28800 (8h) | yes | `r.TLS != nil` | Strict |

Deletion: set `MaxAge: -1` (not `MaxAge: 0` — use `-1` for explicit deletion in Go's `http.SetCookie`).

### Route Registration in `main.go`

Current routes in main.go (lines 131–137):
```go
// Legacy routes (backward compatibility — Story 3.10 will supersede)
mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)
mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)

// New canonical routes (Story 3.9)
mux.HandleFunc("GET /admin/login", adminAuth.LoginPageHandler)
mux.HandleFunc("GET /admin/login/start", adminAuth.LoginStartHandler)
mux.HandleFunc("GET /admin/callback", adminAuth.CallbackHandler)
```

Add after the canonical routes block:
```go
mux.HandleFunc("GET /admin/logout", adminAuth.LogoutHandler)
```

Do NOT remove `GET /admin/callback` registration — it is already there from Story 3.9. The `CallbackHandler` is the same function, just modified.

### Session Cookie and Story 3.11 Context

Story 3.11 (Admin Session Middleware) will read the `admin_session` cookie and parse it with `verifyCookie` + `json.Unmarshal` to extract `sub`, `email`, `role`. The `adminSessionCookie` struct with those fields is already what `CallbackHandler` stores. No changes to the cookie payload format are needed.

Story 3.11 will apply `SessionGuard` to all `/admin/*` routes except `/admin/login*`, `/admin/callback`, `/admin/bootstrap*`, `/admin/static/*`. This means `/admin/logout` WILL be protected by `SessionGuard` in Story 3.11. No action needed in this story.

### `GET /admin/dashboard` Redirect Target

Story 3.13 creates `GET /admin/dashboard`. In this story, redirecting `303` to `/admin/dashboard` is correct even though the page doesn't exist yet — the 303 redirect is the desired behavior; the browser will get a 404 until Story 3.13 is done. Do NOT redirect to `/admin/` as a temporary placeholder.

### No New Dependencies

All required packages are already in `go.mod` and already imported in `auth.go`:
- `net/http` (stdlib)
- `crypto/hmac`, `crypto/sha256` (stdlib)
- `encoding/json`, `encoding/base64` (stdlib)
- `github.com/coreos/go-oidc/v3` (already direct)
- `golang.org/x/oauth2` (already direct)
- `github.com/nebu/nebu/internal/auth` (already imported)

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/auth.go` | MODIFY — change 3 redirect URLs in `CallbackHandler` + add role check + change final redirect destination + add `LogoutHandler` method |
| `gateway/internal/admin/auth_test.go` | MODIFY — update 1 assertion in `TestAdminCallbackHandler_TokenExchangeFailure_Redirects` |
| `gateway/internal/admin/callback_test.go` | CREATE — 5 unit tests for updated `CallbackHandler` and new `LogoutHandler` |
| `gateway/cmd/gateway/main.go` | MODIFY — add `GET /admin/logout` route |

**Do NOT modify:**
- `gateway/internal/admin/crypto.go` — no changes
- `gateway/internal/admin/page_data.go` — no changes (Story 3.12 adds `ErrorPageData`)
- `gateway/internal/admin/middleware.go` — no changes (Story 3.11 adds `SessionGuard`)
- `gateway/internal/admin/handler.go` — no changes
- `gateway/internal/admin/login_test.go` — no changes (tests for `LoginPageHandler` and `LoginStartHandler` are unaffected)
- `gateway/internal/admin/templates/` — no new templates (error page templates are Story 3.12 scope)

### Test File Placement

New test file is `callback_test.go` (not adding to `auth_test.go`) to keep test files focused on distinct areas. Package remains `admin` (not `admin_test`) to access unexported types like `oidcStateCookie`, `adminSessionCookie`, `newTestAdminAuth`, `setupAdminOIDCServer`.

### Anti-Pattern: Do Not Create a New `CallbackHandler` Method

Story 3.9 already created `CallbackHandler`. This story modifies it — do NOT create a new method or rename the existing one. The route `GET /admin/callback` remains registered to `adminAuth.CallbackHandler`.

### References

- [Source: gateway/internal/admin/auth.go, line 282–382] `CallbackHandler` — modify in place
- [Source: gateway/internal/admin/auth.go, line 107–113] `adminSessionCookie` struct — already has `Role` field
- [Source: gateway/internal/admin/auth.go, line 340–349] Role mapping — `auth.MapSystemRole(rawRole)` call already present; add `if systemRole != "instance_admin"` check immediately after
- [Source: gateway/internal/admin/auth_test.go, line 343] Assertion to update: `"/admin/auth/login?error=auth_failed"` → `"/admin/login?error=auth_failed"`
- [Source: gateway/internal/admin/auth_test.go, line 19–66] `setupAdminOIDCServer` + `signAdminJWT` helpers — reuse in `callback_test.go`
- [Source: gateway/internal/admin/auth_test.go, line 96–98] `newTestAdminAuth` — reuse in `callback_test.go`
- [Source: gateway/cmd/gateway/main.go, line 131–137] Route registration — add logout route
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.10, lines 1662–1688] Authoritative ACs
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.11, lines 1700–1706] SessionGuard exclusion list — confirms `/admin/logout` will be protected
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.13, line 1742] `GET /admin/dashboard` — final redirect target

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No debug issues encountered. All tests passed on first run.

### Completion Notes List

- Task 1: Refactored `CallbackHandler` in `gateway/internal/admin/auth.go`. Changed 3 error-path redirects from `/admin/auth/login?error=auth_failed` to `/admin/login?error=auth_failed`. Added role check block (`if systemRole != "instance_admin" → 403`). Changed final redirect from `/admin/` with 302 to `/admin/dashboard` with 303. Cookie deletion for `admin_oidc_state` confirmed correct (Path=/admin, MaxAge=-1).
- Task 2: Added `LogoutHandler` method to `AdminAuth` — deletes `admin_session` cookie (MaxAge=-1, Path=/admin, HttpOnly=true, Secure=r.TLS!=nil, SameSite=Strict), redirects 303 to `/admin/login`.
- Task 3: Registered `GET /admin/logout` route in `main.go` after the canonical routes block.
- Task 4: Created `gateway/internal/admin/callback_test.go` with 5 unit tests covering: state mismatch (400), role check failure (403), valid flow cookie attributes (303 + session cookie + state cookie deletion), TLS Secure flag, logout cookie deletion.
- Task 5: Fixed regression in `auth_test.go` — `TestAdminCallbackHandler_TokenExchangeFailure_Redirects` assertion updated to `/admin/login?error=auth_failed`.
- All tests pass: `make test-unit-go` — 0 regressions, all 11 packages pass.

### File List

- `gateway/internal/admin/auth.go` — MODIFIED: 3 redirect URLs changed, role check added, final redirect changed, `LogoutHandler` method added
- `gateway/internal/admin/auth_test.go` — MODIFIED: 1 assertion updated in `TestAdminCallbackHandler_TokenExchangeFailure_Redirects`
- `gateway/internal/admin/callback_test.go` — CREATED: 5 unit tests for updated `CallbackHandler` and new `LogoutHandler`
- `gateway/cmd/gateway/main.go` — MODIFIED: `GET /admin/logout` route registered

### Review Findings

- [x] [Review][Patch][MINOR] Stale doc-comment on CallbackHandler — said "GET /admin/auth/callback", updated to reflect canonical route [gateway/internal/admin/auth.go:282] — FIXED
- [x] [Review][Patch][MINOR] admin_oidc_state deletion cookie missing Secure flag — added `Secure: r.TLS != nil` to match original set-cookie [gateway/internal/admin/auth.go:378] — FIXED
- [x] [Review][Defer][INFO] No logging on role-check 403 denial — denied user sub/email not logged; operational audit gap but not a code bug; defer to observability story
- [x] [Review][Defer][INFO] Legacy LoginHandler sets cookie Path="/admin/auth" which won't reach /admin/callback — pre-existing from Story 3.9 legacy route; legacy routes are scheduled for removal

**Round 2 (2026-04-01):**
- [x] [Review][Patch][MINOR] admin_oidc_state deletion cookie missing SameSite attribute — original set used SameSite=Lax; deletion omitted it; added `SameSite: http.SameSiteLaxMode` [gateway/internal/admin/auth.go:378] — FIXED
- [x] [Review][Patch][MINOR] Missing guard on empty `sub` claim — token with no `sub` claim would create session with empty sub, breaking Story 3.11 SessionGuard; added `if sub == ""` guard returning 400 [gateway/internal/admin/auth.go:345] — FIXED
- [x] [Review][Defer][INFO] GET /admin/logout has no CSRF protection — spec mandates GET; SameSite=Strict on session cookie provides reasonable protection; defer formal CSRF hardening to security review story
- [x] [Review][Defer][INFO] Legacy-route Cookie-Path-Mismatch (LoginHandler Path=/admin/auth vs CallbackHandler deletion Path=/admin) — pre-existing from Story 3.9; scheduled for removal with legacy routes

## Change Log

- 2026-04-01: Story 3.10 created — OIDC Callback Handler + Session Cookie. Targeted refactor of existing CallbackHandler (role check + redirect URL fixes) + new LogoutHandler + route registration. Status: ready-for-dev.
- 2026-04-01: Story 3.10 implemented — CallbackHandler refactored (3 redirect URLs fixed, role check added, final redirect to /admin/dashboard with 303), LogoutHandler added, route registered, 5 new unit tests, regression fix in auth_test.go. All tests pass. Status: review.

---
security_review: required
---

# Story 5.10: Bootstrap Mode Guard — Replay Prevention

Status: review

## Story

As an instance admin,
I want `/admin/login/start?mode=bootstrap` to be rejected once Bootstrap is completed, and `/admin/bootstrap/select-claim` to be behind `BootstrapGuard`,
so that an authenticated user cannot replay the Bootstrap flow to overwrite OIDC configuration or `admin_group_claim` post-provisioning.

---

## Background / Motivation

Security audit (2026-04-20) found an admin-takeover chain:

1. `LoginStartHandler` (`gateway/internal/admin/auth.go:228–299`) reads `mode` from the query string without checking `IsBootstrapActive()`. After bootstrap is complete, any authenticated user can still craft `?mode=bootstrap`, obtain a signed state cookie with `Mode=bootstrap`, and land in the bootstrap branch of `CallbackHandler`.
2. `POST /admin/bootstrap/select-claim` is registered in `main.go:224` via bare `mux.HandleFunc` — **no** `BootstrapGuard`, **no** `sessionGuard`. Any client holding a signed state cookie can POST to it.
3. `ClaimSelectionHandler` (`auth.go:570–653`) calls `SaveBootstrapConfig` and `SaveAdminGroupClaim` (both `ON CONFLICT DO UPDATE`) **before** `CompleteBootstrap` errors — the writes are non-transactional and land in the live `server_config` rows.

Combined → attacker redirects OIDC to their own IdP on the next admin login. Transactional fix is Story 5.11; this story closes the entry points.

---

## Acceptance Criteria

1. `LoginStartHandler` rejects `mode=bootstrap` with **403 Forbidden** (HTML error page) when `bootstrapPersister.IsBootstrapActive(ctx)` returns `false`.

2. `POST /admin/bootstrap/select-claim` is wired in `cmd/gateway/main.go` via the existing `guard(...)` wrapper so `BootstrapGuard` runs before the handler.

3. `BootstrapGuard` rejects any request when `bootstrap_completed=true` with a **302 redirect** to `/admin/dashboard` (matches existing pattern for `/admin/bootstrap`).

4. The signed `admin_oidc_state` cookie `Path` is narrowed from `/admin` to `/admin/callback` so the state cookie is only transmitted to the callback route.

5. Unit test coverage:
   - `TestLoginStart_BootstrapModeRejectedAfterCompletion` — returns 403
   - `TestLoginStart_BootstrapModeAllowedWhileActive` — returns 302 to Dex
   - `TestSelectClaim_RejectedByBootstrapGuard` — returns 302 when bootstrap completed
   - `TestStateCookie_PathScopedToCallback` — cookie `Path=/admin/callback`

6. Existing admin E2E test (`e2e/features/admin-bootstrap.spec.ts`) still passes green.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestLoginStart_BootstrapModeRejectedAfterCompletion` — Go httptest
   - Given: `bootstrap_completed=true` in `server_config`
   - When: `GET /admin/login/start?mode=bootstrap`
   - Then: 403, body contains no Dex redirect URL

2. `TestSelectClaim_RejectedByBootstrapGuard` — Go httptest
   - Given: `bootstrap_completed=true`, valid signed state cookie with `Mode=bootstrap`
   - When: `POST /admin/bootstrap/select-claim`
   - Then: 302 to `/admin/dashboard`, no DB write to `bootstrap_draft` or `server_config`

3. `TestStateCookie_PathScopedToCallback` — Go httptest
   - Given: `GET /admin/login/start` during active bootstrap
   - Then: `Set-Cookie: admin_oidc_state=...; Path=/admin/callback; ...`

---

## Implementation Notes

- `auth.go:273` — add guard early: `if mode == "bootstrap" && !persister.IsBootstrapActive(ctx) { renderError(w, 403, ...); return }`
- `main.go:224` — replace `mux.HandleFunc("POST /admin/bootstrap/select-claim", …)` with `mux.Handle("POST /admin/bootstrap/select-claim", guard(http.HandlerFunc(…)))` where `guard` composes `BootstrapGuard` before the handler
- `auth.go:288–296` — `cookie.Path = "/admin/callback"`
- No migration needed

---

## Dev Agent Record

### Implementation Notes

- Added `bootstrapChecker BootstrapStatusChecker` field to `AdminAuth` struct
- `NewAdminAuth` now instantiates `NewPostgresBootstrapChecker(db)` when `db != nil` and stores it as `bootstrapChecker`
- `LoginStartHandler`: guard added after `mode` is read — when `mode == "bootstrap"` and `bootstrapChecker != nil`, calls `IsBootstrapActive`; returns 403 if inactive
- `BootstrapGuard` (middleware.go): changed redirect target from `/admin/login` to `/admin/dashboard` when bootstrap is complete and path is a bootstrap path (AC3)
- `LoginStartHandler` cookie `Path` changed from `/admin` to `/admin/callback` (AC4)
- `CallbackHandler` state-cookie deletion `Path` also updated to `/admin/callback` for correctness
- `POST /admin/bootstrap/select-claim` in `main.go` wrapped with `guard(http.HandlerFunc(...))` (AC2)
- `bootstrap_guard_test.go` helper `newTestAdminAuthWithCheckerAndReader` uncommented the `a.bootstrapChecker = checker` line now that the field exists
- Existing `TestBootstrapGuard` table-test case updated: "complete + bootstrap path → redirect to login" → "redirect to dashboard" to match AC3

### Files Modified

- `gateway/internal/admin/auth.go` — `AdminAuth` struct, `NewAdminAuth`, `LoginStartHandler` (guard + cookie Path), `CallbackHandler` (deletion cookie Path)
- `gateway/internal/admin/middleware.go` — `BootstrapGuard` redirect target
- `gateway/cmd/gateway/main.go` — `select-claim` route wrapped with guard
- `gateway/internal/admin/bootstrap_guard_test.go` — wire bootstrapChecker in test helper
- `gateway/internal/admin/middleware_test.go` — update expected redirect target in `TestBootstrapGuard`

### Change Log

- 2026-04-20: Story 5.10 implemented — bootstrap replay prevention: mode=bootstrap guard in LoginStartHandler, BootstrapGuard redirect to /admin/dashboard, cookie Path narrowed to /admin/callback, select-claim route behind BootstrapGuard

# Story 3.11: Admin Session Middleware (Cookie Validation)

Status: done

## Story

As a gateway,
I want all protected Admin UI routes to validate the admin session cookie before serving content,
so that unauthenticated requests are always redirected to `/admin/login`.

## Acceptance Criteria

1. `gateway/internal/admin/middleware.go` contains a `SessionGuard` middleware function.
2. `SessionGuard` reads the `admin_session` cookie; if absent or unparseable: redirect `302` to `/admin/login`.
3. Validates the HMAC-SHA256 signature using the internal secret; if invalid: redirect `302` to `/admin/login`.
4. Checks `exp` claim; if expired: redirect `302` to `/admin/login`.
5. If valid: stores `sub` and `email` in the request context (`context.WithValue`) for downstream handlers.
6. `SessionGuard` is applied to all `/admin/*` routes except `/admin/login*`, `/admin/callback`, `/admin/bootstrap*`, and `/admin/static/*`.
7. Unit tests cover: missing cookie → redirect, invalid signature → redirect, expired → redirect, valid → handler called with context values.

## Tasks / Subtasks

- [x] Task 1: Add `SessionGuard` middleware to `gateway/internal/admin/middleware.go` (AC: 1–5)
  - [x] 1.1 Define unexported typed context key constants to prevent context key collisions:
    ```go
    type contextKey int
    const (
        contextKeyAdminSub contextKey = iota
        contextKeyAdminEmail
    )
    ```
  - [x] 1.2 Export accessor functions (not the keys themselves) so downstream handlers can safely retrieve values:
    ```go
    // AdminSubFromContext returns the admin sub claim stored by SessionGuard, or "".
    func AdminSubFromContext(ctx context.Context) string {
        v, _ := ctx.Value(contextKeyAdminSub).(string)
        return v
    }
    // AdminEmailFromContext returns the admin email stored by SessionGuard, or "".
    func AdminEmailFromContext(ctx context.Context) string {
        v, _ := ctx.Value(contextKeyAdminEmail).(string)
        return v
    }
    ```
  - [x] 1.3 Implement `SessionGuard(secret []byte) func(http.Handler) http.Handler`:
    - Create an `AdminAuth`-like verifier inline, OR create a standalone helper `verifyAdminSession(secret []byte, value string) (*adminSessionCookie, error)` that reuses the existing HMAC+JSON logic
    - Read `admin_session` cookie; if `http.ErrNoCookie` → `http.Redirect 302 /admin/login`, return
    - Call `verifyCookie`-equivalent; if error → `http.Redirect 302 /admin/login`, return
    - `json.Unmarshal` into `adminSessionCookie`; if error → `http.Redirect 302 /admin/login`, return
    - Check `sess.Exp <= time.Now().Unix()`; if expired → `http.Redirect 302 /admin/login`, return
    - Store `sub` and `email` via `context.WithValue` and call `next.ServeHTTP(w, r.WithContext(ctx))`

- [x] Task 2: Register `SessionGuard` on all protected `/admin/*` routes in `gateway/cmd/gateway/main.go` (AC: 6)
  - [x] 2.1 Create `sessionGuard := admin.SessionGuard([]byte(internalSecret))` after existing middleware setup
  - [x] 2.2 Wrap the following routes with `sessionGuard(...)`:
    - `GET /admin/logout` → `sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler))`
    - `GET /admin/dashboard` (Story 3.13 will add this; for now this story only wraps already-registered routes)
  - [x] 2.3 Confirm the following routes remain UNPROTECTED (no SessionGuard):
    - `GET /admin/login`, `GET /admin/login/start` — login routes
    - `GET /admin/callback` — OIDC callback (sets the cookie)
    - `GET /admin/logout` must be protected — Story 3.10 explicitly noted this
    - `GET /admin/bootstrap`, `POST /admin/bootstrap`, `POST /admin/bootstrap/*` — guarded by `BootstrapGuard`, not `SessionGuard`
    - `GET /admin/static/*` — public static assets
  - [x] 2.4 Apply `sessionGuard` to `/admin/logout`: change `mux.HandleFunc("GET /admin/logout", adminAuth.LogoutHandler)` to `mux.Handle("GET /admin/logout", sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler)))`

- [x] Task 3: Write unit tests in `gateway/internal/admin/middleware_test.go` (AC: 7)
  - [x] 3.1 Add `TestSessionGuard_MissingCookie_Redirects`: no `admin_session` cookie → 302 to `/admin/login`
  - [x] 3.2 Add `TestSessionGuard_InvalidSignature_Redirects`: valid JSON but wrong HMAC signature → 302 to `/admin/login`
  - [x] 3.3 Add `TestSessionGuard_ExpiredSession_Redirects`: valid HMAC + JSON but `exp` is in the past → 302 to `/admin/login`
  - [x] 3.4 Add `TestSessionGuard_ValidSession_CallsNextWithContext`: valid cookie → next handler called, context contains `sub` and `email` values via `AdminSubFromContext` and `AdminEmailFromContext`
  - [x] 3.5 Add `TestSessionGuard_MalformedCookieValue_Redirects`: cookie present but value is not valid base64 → 302 to `/admin/login`

- [x] Task 4: Run `make test-unit-go` and confirm zero regressions (AC: 7)

### Review Findings

- [x] [Review][Patch] MINOR: iota-Redundanz — `contextKeyAdminEmail contextKey = iota` statt idiomatisch ohne `= iota` [middleware.go:22] — fixed
- [x] [Review][Defer] INFO: Inkonsistentes Logging — fehlender Cookie und Signatur-Fehler werden nicht geloggt, nur JSON-Parse-Fehler [middleware.go:62-67] — deferred, stylistic consistency; not a bug
- [x] [Review][Defer] INFO: Duplikat-Logik — `verifySessionCookie` und `AdminAuth.verifyCookie` sind identische Implementierungen; Refactoring zu einer shared Funktion wuerde Wartungsrisiko reduzieren [middleware.go:39-52, auth.go:124-137] — deferred, pre-existing architecture decision from Story spec
- [x] [Review][Defer] INFO: Leeres Secret nicht validiert — `SessionGuard` prueft nicht ob `secret` leer ist; in Praxis durch `main.go` PSK-Read abgesichert [middleware.go:57] — deferred, defense-in-depth improvement

## Dev Notes

### Architecture: `SessionGuard` Placement

`SessionGuard` lives in `gateway/internal/admin/middleware.go` — the same file as `BootstrapGuard`. It is a package-level function, NOT a method on `AdminAuth`. This is consistent with the existing `BootstrapGuard` signature:

```go
// Existing pattern in middleware.go:
func BootstrapGuard(checker BootstrapStatusChecker) func(http.Handler) http.Handler { ... }

// New pattern to follow:
func SessionGuard(secret []byte) func(http.Handler) http.Handler { ... }
```

Do NOT put `SessionGuard` as a method on `AdminAuth`. The guard only needs the `secret` bytes.

### Reuse: `verifyCookie` and `adminSessionCookie` Already Exist in auth.go

**DO NOT REIMPLEMENT** the HMAC verification or session struct — they are already defined in `gateway/internal/admin/auth.go`:

- `adminSessionCookie` struct (line 107–112): `Sub`, `Email`, `Role`, `Exp` — use this directly
- `signCookie(payload []byte) string` (line 115–121): base64url(payload) + "." + HMAC signature
- `verifyCookie(value string) ([]byte, error)` (line 123–137): verifies HMAC, returns decoded payload

Since these methods are on `*AdminAuth` but `SessionGuard` is a standalone function, you have two valid approaches:

**Option A (preferred — extract standalone helper):** Create a package-private helper:
```go
// verifySessionCookie verifies the HMAC-SHA256 signature and decodes the cookie.
// This mirrors the AdminAuth.verifyCookie logic for use in middleware without an AdminAuth instance.
func verifySessionCookie(secret []byte, value string) ([]byte, error) {
    idx := strings.LastIndex(value, ".")
    if idx < 0 {
        return nil, errors.New("invalid cookie format")
    }
    encoded, sigPart := value[:idx], value[idx+1:]
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(encoded))
    expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(expectedSig), []byte(sigPart)) {
        return nil, errors.New("invalid cookie signature")
    }
    return base64.RawURLEncoding.DecodeString(encoded)
}
```

**Option B:** Create a temporary `AdminAuth` just for verification — this is wasteful and unclean; avoid.

The cookie format is: `base64url(jsonPayload) + "." + base64url(HMAC-SHA256(secret, base64url(jsonPayload)))`.

### Context Key Pattern

Use typed unexported keys to prevent collisions per Go best practices:

```go
type contextKey int

const (
    contextKeyAdminSub   contextKey = iota
    contextKeyAdminEmail
)
```

Place these in `middleware.go` (not auth.go). Export only the accessor functions `AdminSubFromContext` and `AdminEmailFromContext` — never export the key constants themselves.

This allows Story 3.13 (`GET /admin/dashboard`) and later stories to read the admin identity from context safely without importing unexported types.

### Route Exclusion List (Critical)

`SessionGuard` must NOT be applied to:
- `GET /admin/login` — LoginPageHandler (renders login form)
- `GET /admin/login/start` — LoginStartHandler (initiates OIDC redirect)
- `GET /admin/callback` — CallbackHandler (receives OIDC code, sets session cookie)
- `GET /admin/bootstrap`, `POST /admin/bootstrap`, `POST /admin/bootstrap/test-oidc`, `POST /admin/bootstrap/generate-keys` — protected by `BootstrapGuard` only
- `GET /admin/static/admin.css`, `GET /admin/static/fonts/{filename}` — public static assets
- Legacy routes `GET /admin/auth/login`, `GET /admin/auth/callback` — leave as-is (will be removed in a future story)

`SessionGuard` MUST be applied to:
- `GET /admin/logout` — currently registered as `mux.HandleFunc("GET /admin/logout", adminAuth.LogoutHandler)` in main.go; change to `mux.Handle(...)`

Story 3.13 will add `GET /admin/dashboard` wrapped with `sessionGuard`. This story only covers already-registered routes.

### Cookie Contract (Established in Stories 3.9 and 3.10 — Must Match)

| Cookie | Name | Path | MaxAge | HttpOnly | Secure | SameSite |
|--------|------|------|--------|----------|--------|----------|
| Admin session | `admin_session` | `/admin` | 28800 (8h) | yes | `r.TLS != nil` | Strict |

The `adminSessionCookie` payload (JSON, HMAC-signed, base64url-encoded):
```json
{ "sub": "...", "email": "...", "role": "instance_admin", "exp": <unix_timestamp> }
```

`exp` is Unix timestamp (seconds). Expiry check: `sess.Exp <= time.Now().Unix()` → expired.

### `SessionGuard` Signature

```go
func SessionGuard(secret []byte) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            cookie, err := r.Cookie("admin_session")
            if err != nil {
                http.Redirect(w, r, "/admin/login", http.StatusFound)
                return
            }
            payload, err := verifySessionCookie(secret, cookie.Value)
            if err != nil {
                http.Redirect(w, r, "/admin/login", http.StatusFound)
                return
            }
            var sess adminSessionCookie
            if err := json.Unmarshal(payload, &sess); err != nil {
                http.Redirect(w, r, "/admin/login", http.StatusFound)
                return
            }
            if sess.Exp <= time.Now().Unix() {
                http.Redirect(w, r, "/admin/login", http.StatusFound)
                return
            }
            ctx := context.WithValue(r.Context(), contextKeyAdminSub, sess.Sub)
            ctx = context.WithValue(ctx, contextKeyAdminEmail, sess.Email)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

### Test Helpers Available in `admin` Package

`middleware_test.go` uses `package admin` (not `package admin_test`). This gives access to:
- `adminSessionCookie` struct (unexported) — use directly in tests to build payloads
- `contextKeyAdminSub`, `contextKeyAdminEmail` (unexported) — access via `AdminSubFromContext` / `AdminEmailFromContext`
- `fakeBootstrapChecker`, `errFakeDB` from `bootstrap_test.go` — already in same package

To build a valid signed cookie in tests, replicate the `signCookie` logic inline or add a `signCookieForTest(secret []byte, payload []byte) string` package-private helper (place in middleware.go or create a test helper file). Do NOT reach into `AdminAuth` just to sign a test cookie.

Simplest test helper inline:
```go
func signTestCookie(t *testing.T, secret []byte, payload []byte) string {
    t.Helper()
    encoded := base64.RawURLEncoding.EncodeToString(payload)
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(encoded))
    sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    return encoded + "." + sig
}
```

For the invalid-signature test, use `signTestCookie` with a different secret (e.g. `[]byte("wrong-secret")`).

### Imports Required in `middleware.go`

```go
import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "log/slog"
    "net/http"
    "strings"
    "time"
)
```

`slog` is already imported for `BootstrapGuard` error logging. Use it for any unexpected errors in `SessionGuard` (e.g., malformed JSON that passes HMAC — log at warn level before redirect).

### `main.go` Change: Wrap `/admin/logout`

Current (from Story 3.10, line 138 in main.go):
```go
mux.HandleFunc("GET /admin/logout", adminAuth.LogoutHandler)
```

Change to:
```go
sessionGuard := admin.SessionGuard([]byte(internalSecret))
mux.Handle("GET /admin/logout", sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler)))
```

Place the `sessionGuard` variable definition after `adminAuth` is created and before route registration. The `BootstrapGuard` is already assigned as `guard` — follow the same pattern.

Story 3.13 will add:
```go
mux.Handle("GET /admin/dashboard", sessionGuard(http.HandlerFunc(dashboardHandler.Handler)))
```

This story does NOT add the dashboard handler — leave a comment as a placeholder if helpful.

### File Structure

| File | Action |
|------|--------|
| `gateway/internal/admin/middleware.go` | MODIFY — add `SessionGuard`, `verifySessionCookie`, context key types and accessor functions |
| `gateway/internal/admin/middleware_test.go` | MODIFY — add 5 `TestSessionGuard_*` test cases |
| `gateway/cmd/gateway/main.go` | MODIFY — create `sessionGuard` variable, wrap `GET /admin/logout` |

**Do NOT modify:**
- `gateway/internal/admin/auth.go` — no changes (already complete from Story 3.10)
- `gateway/internal/admin/auth_test.go` — no changes
- `gateway/internal/admin/callback_test.go` — no changes
- `gateway/internal/admin/handler.go` — no changes
- `gateway/internal/admin/page_data.go` — no changes (Story 3.13 adds DashboardPageData)
- Any template files — no changes

### Anti-Patterns to Avoid

- **DO NOT** put `SessionGuard` as a method on `AdminAuth`. It is a package-level function.
- **DO NOT** re-implement HMAC logic from scratch — reuse the exact same algorithm as `AdminAuth.signCookie`/`verifyCookie` via the standalone `verifySessionCookie` helper.
- **DO NOT** use `string` as a context key type — use typed `contextKey int` to prevent collisions.
- **DO NOT** apply `SessionGuard` to login/callback/bootstrap/static routes.
- **DO NOT** export the `contextKey` type or constants — only export the accessor functions.
- **DO NOT** change the `302` redirect code (the epics spec says `302`, not `303`).

### Previous Story Learnings (from Stories 3.9, 3.10)

- Cookie deletion uses `MaxAge: -1` (not `MaxAge: 0`) for explicit deletion in Go's `net/http`.
- `SameSite` attribute must be set on all cookie operations including deletion (learned in 3.10 code review).
- `Secure: r.TLS != nil` pattern is used consistently across all admin cookies.
- Tests in the `admin` package use `package admin` (not `package admin_test`) to access unexported types.
- `fakeBootstrapChecker` and `errFakeDB` are defined in `bootstrap_test.go` and available to all test files in the same package.
- The `BootstrapGuard` test pattern in `middleware_test.go` uses a `nextCalled bool` flag to verify pass-through — follow the same pattern for `SessionGuard`.

### Deferred Work Note

The `GET /admin/logout` endpoint has no CSRF protection (noted in deferred-work.md from Story 3.10). Applying `SessionGuard` to logout does NOT add CSRF protection — it only verifies the session is valid before deleting it. The SameSite=Strict attribute on the `admin_session` cookie provides cross-site protection. Do not add CSRF token handling in this story.

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No issues encountered. All tests passed on first run.

### Completion Notes List

- Task 1: Added `SessionGuard` to `gateway/internal/admin/middleware.go`. Implemented typed `contextKey int` (unexported) with `contextKeyAdminSub` and `contextKeyAdminEmail` constants. Added exported accessors `AdminSubFromContext` and `AdminEmailFromContext`. Added package-private `verifySessionCookie` helper that mirrors `AdminAuth.verifyCookie` logic without requiring an `AdminAuth` instance. `SessionGuard` reads `admin_session` cookie, verifies HMAC-SHA256, unmarshals JSON into `adminSessionCookie`, checks expiry, and stores `sub`/`email` in context on success; redirects 302 to `/admin/login` on any failure.
- Task 2: Updated `gateway/cmd/gateway/main.go` to create `sessionGuard := admin.SessionGuard([]byte(internalSecret))` and wrapped `GET /admin/logout` with it. Login/callback/bootstrap/static routes remain unprotected. Added placeholder comment for Story 3.13 dashboard route.
- Task 3: Added 5 `TestSessionGuard_*` test cases to `gateway/internal/admin/middleware_test.go` covering: missing cookie, invalid signature (wrong secret), expired session, valid session with context values checked via `AdminSubFromContext`/`AdminEmailFromContext`, and malformed cookie value (no dot separator).
- Task 4: `make test-unit-go` passed with zero regressions across all packages.

### File List

- `gateway/internal/admin/middleware.go` — MODIFIED: added `contextKey` type, constants, `AdminSubFromContext`, `AdminEmailFromContext`, `verifySessionCookie`, `SessionGuard`
- `gateway/internal/admin/middleware_test.go` — MODIFIED: added `signTestCookie` test helper and 5 `TestSessionGuard_*` test functions
- `gateway/cmd/gateway/main.go` — MODIFIED: added `sessionGuard` variable, wrapped `GET /admin/logout` with `sessionGuard`

## Change Log

- 2026-04-01: Story 3.11 created — Admin Session Middleware (Cookie Validation). Ready for implementation. Status: ready-for-dev.
- 2026-04-01: Story 3.11 implemented — `SessionGuard` middleware added, 5 unit tests added, `/admin/logout` protected, all tests green. Status: review.

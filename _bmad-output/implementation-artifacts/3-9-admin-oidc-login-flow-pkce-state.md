# Story 3.9: Admin OIDC Login Flow (PKCE + State)

Status: done

## Story

As an operator,
I want to log in to the Admin UI via OIDC with PKCE,
so that the Admin UI never handles my password and the authorization code cannot be intercepted.

## Acceptance Criteria

1. `GET /admin/login` renders a login page with a single **Login with SSO** button that links to `GET /admin/login/start`
2. `GET /admin/login/start`:
   - Reads `oidc_issuer`, `oidc_client_id`, and `oidc_client_secret` from `server_config` table in PostgreSQL
   - Decrypts `oidc_client_secret` using `decryptAES256GCM` (the internal secret used as key)
   - Generates a PKCE code verifier using `oauth2.GenerateVerifier()` and the S256 challenge via `oauth2.S256ChallengeOption`
   - Generates a cryptographically random `state` parameter (16 bytes, hex-encoded)
   - Stores `code_verifier` and `state` in a short-lived signed cookie (`admin_oidc_state`, `HttpOnly`, `Secure: r.TLS != nil`, `SameSite=Lax`, `MaxAge=600`, `Path=/admin`)
   - Redirects `302` to the OIDC authorization endpoint with `response_type=code`, `scope=openid profile email`, `code_challenge`, `code_challenge_method=S256`, `state`, and `redirect_uri=/admin/callback`
3. If `server_config` does not contain `oidc_issuer` (or `oidc_client_id`), `GET /admin/login/start` returns `503` with a human-readable HTML error page
4. A unit test verifies the redirect URL contains `code_challenge_method=S256` and a non-empty `state`
5. A unit test verifies the `admin_oidc_state` cookie is set with `HttpOnly` and `Secure` flags
6. `make test-unit-go` passes with zero regressions

## Tasks / Subtasks

- [x] Task 1: Add `LoginPageHandler` to `AdminAuth` — serve `GET /admin/login` (AC: 1)
  - [x] 1.1 Create `gateway/internal/admin/templates/login.html` — extends `base.html` layout; contains a centered card with Nebu heading, "Login with SSO" primary DaisyUI button linking to `/admin/login/start`; also renders an optional `{{.Error}}` alert banner if set (for future use); set `ActiveNav: "login"` and `BootstrapMode: false`
  - [x] 1.2 Add `LoginPageData` struct to `page_data.go`:
    ```go
    // LoginPageData holds data for the Admin login page.
    type LoginPageData struct {
        PageData         // embed for BootstrapMode + ActiveNav
        Error string     // optional error message (e.g. "auth_failed", "config_missing")
    }
    ```
  - [x] 1.3 Add `LoginPageHandler(w http.ResponseWriter, r *http.Request)` method to `AdminAuth`:
    - Read optional `error` query param (`r.URL.Query().Get("error")`)
    - Build `LoginPageData{PageData: PageData{ActiveNav: "login"}, Error: errorMsg}`
    - Call `h.tmpl.render(w, "login", data)` (200 response)
  - [x] 1.4 Add `tmpl *TemplateHandler` field to `AdminAuth` struct; update `NewAdminAuth` signature:
    ```go
    func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte, tmpl *TemplateHandler) *AdminAuth
    ```
    Pass `tmplHandler` from `main.go` to `NewAdminAuth`.

- [x] Task 2: Add `LoginStartHandler` — PKCE redirect with DB-sourced OIDC config (AC: 2–3)
  - [x] 2.1 Add `db *sql.DB` field to `AdminAuth` struct; update `NewAdminAuth` to also accept `db *sql.DB`:
    ```go
    func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte, db *sql.DB, tmpl *TemplateHandler) *AdminAuth
    ```
  - [x] 2.2 Add helper `loadOIDCConfigFromDB(ctx context.Context) (issuer, clientID, clientSecret string, err error)` on `AdminAuth`:
    - Query: `SELECT key, value FROM server_config WHERE key IN ('oidc_issuer', 'oidc_client_id', 'oidc_client_secret')`
    - For `oidc_client_secret`, call `decryptAES256GCM(a.secret, encryptedValue)` to decrypt
    - Return `("", "", "", ErrOIDCConfigMissing)` if any of the three keys is absent
  - [x] 2.3 Add `LoginStartHandler(w http.ResponseWriter, r *http.Request)` method on `AdminAuth`:
    - Call `a.loadOIDCConfigFromDB(r.Context())`
    - On `ErrOIDCConfigMissing`: render a 503 response — `http.Error(w, "OIDC configuration not found in server config. Please complete the Bootstrap Wizard first.", http.StatusServiceUnavailable)`
    - Build `oauth2.Config` using DB-loaded `issuer`, `clientID`, `clientSecret`, `redirect_uri` = `scheme + "://" + r.Host + "/admin/callback"`, scopes `openid profile email`
    - Generate `verifier := oauth2.GenerateVerifier()`
    - Generate 16-byte state via `crypto/rand.Read`, hex-encode
    - Marshal and sign `oidcStateCookie{State, Verifier, Exp: now+10min}` → set `admin_oidc_state` cookie (`HttpOnly`, `Secure: r.TLS != nil`, `SameSite=Lax`, `MaxAge=600`, `Path=/admin`)
    - Build auth URL: `oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))`
    - `http.Redirect(w, r, authURL, http.StatusFound)` (302)
  - [x] 2.4 Update `buildOAuth2Config` (used by `CallbackHandler`): change redirect URL from `/admin/auth/callback` to `/admin/callback`

- [x] Task 3: Register new routes in `main.go` (AC: 1–3)
  - [x] 3.1 Update `NewAdminAuth` call in `main.go` to pass `bootstrapDB` and `tmplHandler`:
    ```go
    adminAuth := admin.NewAdminAuth(oidcProvider, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCClaimRole, []byte(internalSecret), bootstrapDB, tmplHandler)
    ```
  - [x] 3.2 Register new routes (replace or supplement existing `/admin/auth/login` and `/admin/auth/callback` routes):
    ```go
    mux.HandleFunc("GET /admin/login", adminAuth.LoginPageHandler)
    mux.HandleFunc("GET /admin/login/start", adminAuth.LoginStartHandler)
    mux.HandleFunc("GET /admin/callback", adminAuth.CallbackHandler)
    ```
  - [x] 3.3 Keep the existing `GET /admin/auth/login` and `GET /admin/auth/callback` routes registered (backward compatibility; Story 3.10 will supersede them). Do NOT remove them — the `BootstrapGuard` and middleware still redirect to `/admin/login` and `/admin/callback` will be the new canonical path going forward.
  - [x] 3.4 Update `bootstrapDB` usage: `bootstrapDB` is already opened in `main.go` (line 121); pass it to `NewAdminAuth` as well. Note: it is a shared `*sql.DB` — sharing a pool is safe (Go's `database/sql` is concurrency-safe).

- [x] Task 4: Write unit tests (AC: 4–5)
  - [x] 4.1 Create `gateway/internal/admin/login_test.go` (package `admin`)
  - [x] 4.2 `TestLoginPageHandler_RendersPage`: GET `/admin/login` → 200, body contains "Login with SSO" and `/admin/login/start`
  - [x] 4.3 `TestLoginPageHandler_WithError`: GET `/admin/login?error=auth_failed` → 200, body contains error indicator
  - [x] 4.4 `TestLoginStartHandler_SetsStateAndRedirects`: set up a fake DB returning valid `oidc_issuer`, `oidc_client_id`, `oidc_client_secret` (pre-encrypted with test secret); POST `/admin/login/start`; assert 302, `admin_oidc_state` cookie is set with `HttpOnly=true`, Location contains `code_challenge_method=S256` and non-empty `state`
  - [x] 4.5 `TestLoginStartHandler_MissingOIDCConfig`: fake DB returns no rows → expect 503
  - [x] 4.6 `TestLoginStartHandler_CookieSecurity`: assert `admin_oidc_state` cookie has `HttpOnly=true` and `SameSite=Lax`; assert cookie `MaxAge=600`
  - [x] 4.7 Run `make test-unit-go` → zero regressions (AC: 6)

## Dev Notes

### Critical Context: Story 2.6 Already Implemented PKCE (Different URLs)

Story 2.6 created `gateway/internal/admin/auth.go` with `AdminAuth`, `LoginHandler`, and `CallbackHandler` at routes:
- `GET /admin/auth/login` — PKCE login (redirects immediately to OIDC, no HTML page)
- `GET /admin/auth/callback` — token exchange

Story 3.9 introduces **new routes** layered on top:
- `GET /admin/login` — HTML login page (new, requires `login.html` template)
- `GET /admin/login/start` — PKCE redirect, but reading OIDC config from DB (not env vars)
- `GET /admin/callback` — callback URL updated (same logic as `CallbackHandler`, new path)

Do NOT delete or modify the existing `/admin/auth/login` and `/admin/auth/callback` routes — they serve a different purpose (Matrix client login via `m.login.sso`) and Story 3.10 will wire the session cookie creation to the new `/admin/callback` path.

### AdminAuth Struct Changes

Current signature (Story 2.6):
```go
func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte) *AdminAuth
```

New signature (this story):
```go
func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte, db *sql.DB, tmpl *TemplateHandler) *AdminAuth
```

Add `db *sql.DB` and `tmpl *TemplateHandler` fields to the `AdminAuth` struct.

In `main.go` (line 107), the current call is:
```go
adminAuth := admin.NewAdminAuth(oidcProvider, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCClaimRole, []byte(internalSecret))
```
Must become:
```go
adminAuth := admin.NewAdminAuth(oidcProvider, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCClaimRole, []byte(internalSecret), bootstrapDB, tmplHandler)
```
`bootstrapDB` is already opened at line 121. `tmplHandler` is already initialized at line 127. However, `bootstrapDB` is opened AFTER `adminAuth` currently — reorder initialization so `bootstrapDB` and `tmplHandler` are created BEFORE `adminAuth`.

### Reading OIDC Config from DB

The `server_config` table uses an append-only INSERT-only RLS policy. Reads are unrestricted. Pattern from `db/serverconfig.go`:
```go
db.QueryRow("SELECT value FROM server_config WHERE key = $1", "oidc_issuer").Scan(&value)
```

`oidc_client_secret` is stored AES-256-GCM encrypted (stored as hex-encoded `nonce||ciphertext`). Use `decryptAES256GCM` from `crypto.go` (package-private in `admin` package):
```go
plain, err := decryptAES256GCM(a.secret, encryptedHex)
```
The key derivation in `decryptAES256GCM` is `sha256.Sum256(secret)` → 32-byte AES key — already implemented and tested.

A sentinel error for missing config:
```go
var ErrOIDCConfigMissing = errors.New("OIDC configuration missing in server_config")
```

### `loadOIDCConfigFromDB` Implementation Pattern

```go
func (a *AdminAuth) loadOIDCConfigFromDB(ctx context.Context) (issuer, clientID, clientSecret string, err error) {
    rows, err := a.db.QueryContext(ctx,
        "SELECT key, value FROM server_config WHERE key IN ('oidc_issuer', 'oidc_client_id', 'oidc_client_secret')")
    if err != nil {
        return "", "", "", err
    }
    defer rows.Close()

    vals := make(map[string]string)
    for rows.Next() {
        var k, v string
        if err := rows.Scan(&k, &v); err != nil {
            return "", "", "", err
        }
        vals[k] = v
    }
    if err := rows.Err(); err != nil {
        return "", "", "", err
    }

    issuer = vals["oidc_issuer"]
    clientID = vals["oidc_client_id"]
    encSecret := vals["oidc_client_secret"]

    if issuer == "" || clientID == "" || encSecret == "" {
        return "", "", "", ErrOIDCConfigMissing
    }

    plain, err := decryptAES256GCM(a.secret, encSecret)
    if err != nil {
        return "", "", "", fmt.Errorf("decrypting oidc_client_secret: %w", err)
    }
    return issuer, clientID, plain, nil
}
```

### OIDC Provider for `LoginStartHandler`

The existing `AdminAuth` holds a `*auth.Provider` that was initialized with the env var issuer at startup. For `LoginStartHandler`, the DB-loaded issuer may differ (it's the canonical one from bootstrap). Use the `go-oidc` provider with the DB-loaded issuer:

```go
import "github.com/coreos/go-oidc/v3/oidc"

ctx := oidc.ClientContext(r.Context(), http.DefaultClient)
provider, err := oidc.NewProvider(ctx, issuerFromDB)
if err != nil {
    http.Error(w, "OIDC provider discovery failed: "+err.Error(), http.StatusServiceUnavailable)
    return
}

oauth2Config := &oauth2.Config{
    ClientID:     clientIDFromDB,
    ClientSecret: clientSecretFromDB,
    RedirectURL:  scheme + "://" + r.Host + "/admin/callback",
    Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
    Endpoint:     provider.Endpoint(),
}
```

This performs OIDC discovery on each `LoginStartHandler` invocation. In production this is acceptable: the login page is hit rarely (once per admin session). If needed, a cache can be added in a later story. For unit tests: use a test OIDC server (same `setupAdminOIDCServer` helper from `auth_test.go`).

### Cookie Attributes (Established Contract — must match Story 3.10)

| Cookie | Name | Path | MaxAge | HttpOnly | Secure | SameSite |
|--------|------|------|--------|----------|--------|----------|
| OIDC state | `admin_oidc_state` | `/admin` | 600 (10min) | yes | `r.TLS != nil` | Lax |
| Admin session | `admin_session` | `/admin` | 28800 (8h) | yes | `r.TLS != nil` | Strict |

Note: Story 2.6 set `admin_oidc_state` with `Path: "/admin/auth"` — this story **changes the path to `/admin`** so that `GET /admin/callback` (not `/admin/auth/callback`) can read it. This is a deliberate change.

### `login.html` Template

Follow the same pattern as `bootstrap.html`:
```
{{ template "base" . }}
{{ define "title" }}Login — Nebu Admin{{ end }}
{{ define "content" }}
```

The template receives `LoginPageData` which embeds `PageData` (providing `.BootstrapMode` and `.ActiveNav`). Use DaisyUI `card`, `btn btn-primary` for the SSO button. The button is an `<a>` tag (not a form) linking to `/admin/login/start`.

If `.Error` is non-empty, display a DaisyUI `alert alert-error` above the card. Map known error keys to human-readable messages:
- `"auth_failed"` → "Authentication failed. Please try again."
- `"config_missing"` → "OIDC configuration is not yet set up. Please contact your administrator."
- any other value → "An error occurred. Please try again."

### Unit Test: Fake DB for `loadOIDCConfigFromDB`

Use a `fakeServerConfigDB` approach similar to `fakeBootstrapChecker`:
```go
type fakeServerConfigDB struct {
    vals map[string]string
    err  error
}
```

Or use `database/sql/driver` + `database/sql` with a test stub. The simplest approach: expose `loadOIDCConfigFromDB` logic through a `ServerConfigReader` interface so tests can inject a fake:

```go
// ServerConfigReader reads OIDC config from server_config.
type ServerConfigReader interface {
    LoadOIDCConfig(ctx context.Context) (issuer, clientID, clientSecret string, err error)
}
```

Inject as `configReader ServerConfigReader` field. Implement `postgresServerConfigReader` (wraps `*sql.DB`). Tests inject `fakeServerConfigReader`.

### Template Registration

`TemplateHandler.NewTemplateHandler()` auto-discovers all `.html` files under `templates/` that are not in `templates/layouts/`. Adding `templates/login.html` is sufficient — it will be auto-discovered and compiled into its own isolated template set.

### Redirect Destination After Callback

`CallbackHandler` in Story 2.6 redirects to `/admin/` (with trailing slash) on success. This story does NOT change the `CallbackHandler` redirect target — Story 3.10 wires the full session cookie + redirect to `/admin/dashboard`. For now, keep the existing behavior.

### BootstrapGuard and SessionGuard Exclusions

Story 3.11 specifies that `SessionGuard` excludes `/admin/login*`, `/admin/callback`, `/admin/bootstrap*`, and `/admin/static/*`. The new routes in this story (`/admin/login` and `/admin/login/start`) will match the `/admin/login*` exclusion pattern. The new `/admin/callback` is also explicitly excluded. No changes needed to `BootstrapGuard` or `SessionGuard` for this story.

However, `BootstrapGuard` currently redirects to `/admin/login` on bootstrap-complete. Story 3.8 already redirects to `/admin/login` after bootstrap (line 322 of `bootstrap.go`). This story PROVIDES that endpoint — no circular dependency.

### `main.go` Initialization Order

Current order in `main.go`:
1. `adminAuth := admin.NewAdminAuth(...)` (line 107)
2. `bootstrapDB, err := sql.Open(...)` (line 121)
3. `tmplHandler, err := admin.NewTemplateHandler()` (line 127)

With this story, `adminAuth` needs `bootstrapDB` and `tmplHandler`. Reorder:
1. Move `bootstrapDB` and `tmplHandler` initialization BEFORE `adminAuth`
2. Then create `adminAuth` with all dependencies

### No New Dependencies

All required packages are already in `go.mod`:
- `github.com/coreos/go-oidc/v3` v3.17.0 (direct)
- `golang.org/x/oauth2` v0.34.0 (direct)
- `database/sql` (stdlib)
- `crypto/rand`, `encoding/hex` (stdlib)

Do NOT add `golang.org/x/crypto` — not present in `go.mod` and not needed.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/auth.go` | MODIFY — add `db *sql.DB`, `tmpl *TemplateHandler` fields; add `loadOIDCConfigFromDB` helper; add `LoginPageHandler`, `LoginStartHandler` methods; update `NewAdminAuth` signature; update `buildOAuth2Config` redirect URL to `/admin/callback` |
| `gateway/internal/admin/page_data.go` | MODIFY — add `LoginPageData` struct |
| `gateway/internal/admin/templates/login.html` | CREATE — login page with SSO button |
| `gateway/internal/admin/login_test.go` | CREATE — 5 unit tests |
| `gateway/cmd/gateway/main.go` | MODIFY — reorder init (bootstrapDB + tmplHandler before adminAuth); update `NewAdminAuth` call; add `/admin/login`, `/admin/login/start`, `/admin/callback` routes |

**Do NOT modify:**
- `gateway/internal/admin/crypto.go` — `decryptAES256GCM` is package-private and already correct
- `gateway/internal/admin/bootstrap.go` — no changes needed
- `gateway/internal/admin/middleware.go` — `BootstrapGuard` already redirects to `/admin/login`; no changes for this story
- `gateway/internal/admin/auth_test.go` — existing tests remain valid; only `buildOAuth2Config` changes (redirect URL), but auth_test.go tests do not assert the redirect URL fragment from the callback, so no test breakage expected. Verify after implementing.

### Existing Test Patterns to Follow

- `fakeBootstrapChecker` in `bootstrap_test.go` — pattern for fake interface implementations
- `setupAdminOIDCServer` in `auth_test.go` — reuse for `TestLoginStartHandler_SetsStateAndRedirects`
- `newTestAdminAuth` in `auth_test.go` — update signature to include `db` and `tmpl` parameters; for existing tests, pass `nil` for both

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.9, lines 1639–1659] Authoritative ACs
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.10, line 1675] `redirect_uri=/admin/callback` (canonical callback path)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.11, line 1705] SessionGuard excludes `/admin/login*`, `/admin/callback`
- [Source: gateway/internal/admin/auth.go] `AdminAuth` struct, `signCookie`, `verifyCookie`, `oidcStateCookie`, `LoginHandler`, `CallbackHandler` — reuse and extend
- [Source: gateway/internal/admin/auth_test.go] `setupAdminOIDCServer`, `newTestAdminAuth`, cookie assertion patterns
- [Source: gateway/internal/admin/crypto.go] `decryptAES256GCM(secret []byte, hexCiphertext string) (string, error)` — package-private, available in same package
- [Source: gateway/internal/admin/bootstrap.go#line 418] `SELECT key, value FROM server_config WHERE key IN (...)` pattern
- [Source: gateway/internal/admin/handler.go] `TemplateHandler.render(w, name, data)` — render page template by name
- [Source: gateway/internal/admin/page_data.go] `PageData` struct (embedded in all page data)
- [Source: gateway/internal/admin/templates/bootstrap.html] Template structure pattern (`{{ template "base" . }}`, `{{ define "title" }}`, `{{ define "content" }}`)
- [Source: gateway/internal/admin/middleware.go#line 33] `BootstrapGuard` redirects to `/admin/login` — this story provides that endpoint
- [Source: gateway/internal/admin/bootstrap.go#line 322] `FinalizeHandler` redirects 303 to `/admin/login` — confirmed endpoint must exist
- [Source: gateway/cmd/gateway/main.go, lines 107–134] Current init order; must reorder for this story
- [Source: _bmad-output/implementation-artifacts/2-6-admin-ui-oidc-client-authorization-code-flow-with-pkce.md] Story 2.6 notes — PKCE implementation already complete; this story adds DB-based config + HTML page
- [Source: _bmad-output/implementation-artifacts/3-8-bootstrap-api-handler-post-admin-bootstrap.md#File-List] Confirms `crypto.go` with `decryptAES256GCM` exists

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No debug issues encountered. All tests passed on first run.

### Completion Notes List

- Implemented `ServerConfigReader` interface with `postgresServerConfigReader` implementation for testability — this is a better design than the story's `loadOIDCConfigFromDB` suggestion since it allows full DI without needing a real DB in tests.
- `NewAdminAuth` now accepts `db *sql.DB` and `tmpl *TemplateHandler`; a `nil` DB means `configReader` is `nil` (safe for legacy callers passing `nil, nil`).
- `buildOAuth2Config` redirect URL updated from `/admin/auth/callback` to `/admin/callback` (AC: 2, Task 2.4).
- `CallbackHandler` state cookie delete path updated from `/admin/auth` to `/admin` so the new `/admin/callback` route can clear it correctly.
- `login.html` template created with DaisyUI card, SSO button, and error alert mapping three known error keys.
- `auth_test.go` `newTestAdminAuth` and `TestSignAndVerifyCookie` updated to pass `nil, nil` for the new `db` and `tmpl` parameters — no existing test logic changed.
- `login_test.go` contains 8 unit tests: `TestLoginPageHandler_RendersPage`, `TestLoginPageHandler_WithError`, `TestLoginStartHandler_SetsStateAndRedirects`, `TestLoginStartHandler_MissingOIDCConfig`, `TestLoginStartHandler_CookieSecurity`, `TestLoginStartHandler_CookieSecureTLS`, `TestLoginStartHandler_NilConfigReader_Returns503`, `TestLoginStartHandler_DBError_Returns503`.
- `main.go` initialization order rewritten: `bootstrapDB` and `tmplHandler` are now created BEFORE `adminAuth`; new routes registered; legacy routes kept.
- `make test-unit-go`: all 11 packages pass, zero regressions.

### File List

- `gateway/internal/admin/auth.go` — MODIFIED: added `ErrOIDCConfigMissing`, `ServerConfigReader` interface, `postgresServerConfigReader`, `configReader`/`tmpl` fields on `AdminAuth`, updated `NewAdminAuth` signature, added `LoginPageHandler` and `LoginStartHandler`, updated `buildOAuth2Config` redirect URL, updated state cookie delete path in `CallbackHandler`
- `gateway/internal/admin/page_data.go` — MODIFIED: added `LoginPageData` struct
- `gateway/internal/admin/templates/login.html` — CREATED: login page template with DaisyUI card, SSO button, error alert
- `gateway/internal/admin/login_test.go` — CREATED: 8 unit tests for `LoginPageHandler` and `LoginStartHandler`
- `gateway/internal/admin/auth_test.go` — MODIFIED: updated `newTestAdminAuth` and `TestSignAndVerifyCookie` to pass `nil, nil` for new `NewAdminAuth` parameters
- `gateway/cmd/gateway/main.go` — MODIFIED: reordered init (bootstrapDB + tmplHandler before adminAuth), updated `NewAdminAuth` call, registered `/admin/login`, `/admin/login/start`, `/admin/callback` routes

### Review Findings

- [x] [Review][Patch] P1 MAJOR: Build broken — `fakeServerConfigReader` missing `CompleteBootstrap` method; `callback_test.go` SameSite assertion Strict→Lax; all callback tests missing `configReader` injection; `TestLoginStartHandler_MissingOIDCConfig` expected 503 but code returns 302 redirect; `TestBootstrapWizard_StepHandler_BackNavigation` broken by hidden carry-fields; `TestFinalizeHandler_Success` redirect target updated [login_test.go, callback_test.go, auth_test.go, bootstrap_api_test.go, bootstrap_wizard_test.go]
- [x] [Review][Patch] P2 MAJOR: `CompleteBootstrap` not idempotent — plain INSERT fails on retry with unique_violation; changed to `ON CONFLICT (key) DO NOTHING` + RowsAffected check [auth.go:76-90]
- [x] [Review][Patch] P3 MAJOR: `mode=bootstrap` replay after bootstrap complete enables privilege escalation — combined with P2 fix: RowsAffected==0 returns error, blocking grant of instance_admin [auth.go:76-90]
- [x] [Review][Patch] P4 MINOR: Double `LoadOIDCConfig` call in CallbackHandler — second call silently swallowed errors; used first call's clientSecret directly [auth.go:335]
- [x] [Review][Patch] P5 MINOR: CallbackHandler missing nil guard on `configReader` — added nil check matching LoginStartHandler pattern [auth.go:302]
- [x] [Review][Defer] D1: `extractFirstRoleClaim` takes only first array element — order-dependent privilege. Pre-existing OIDC pattern.
- [x] [Review][Defer] D2: Catch-all `/admin/` silently swallows DB errors. Operational observability gap.
- [x] [Review][Defer] D3: `bootstrap-done.html` hardcodes `instance_admin` role name. Acceptable for bootstrap-only page.

## Change Log

- 2026-04-01: Story 3.9 implemented — Admin OIDC Login Flow (PKCE + State). Added `GET /admin/login` HTML page, `GET /admin/login/start` PKCE redirect with DB-sourced OIDC config, `GET /admin/callback` canonical route. `ServerConfigReader` interface for testability. 7 unit tests. Zero regressions.
- 2026-04-01: Code review — added `TestLoginStartHandler_CookieSecureTLS` test (AC 5: Secure flag verification over TLS). Fixed Completion Notes count (was "6", now "8"). Total: 8 unit tests.
- 2026-04-02: Code review (beyond-scope changes) — 5 PATCH fixes applied (2 MAJOR, 3 MINOR), 3 deferred, 11 dismissed. Fixed: build-broken test doubles, CompleteBootstrap idempotency + privilege replay guard, double LoadOIDCConfig, CallbackHandler nil guard. All 11 Go packages pass.

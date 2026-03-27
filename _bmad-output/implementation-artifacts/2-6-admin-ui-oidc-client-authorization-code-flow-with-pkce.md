# Story 2.6: Admin UI OIDC Client — Authorization Code Flow with PKCE

Status: done

## Story

As an operator,
I want the Admin UI login to use a secure PKCE-protected OIDC flow,
so that authorization codes cannot be intercepted or replayed by attackers.

## Acceptance Criteria

1. **Given** `GET /admin/auth/login` is called, **When** handled by the gateway, **Then** it generates a cryptographically random `code_verifier` (43–128 chars, Base64URL, via `oauth2.GenerateVerifier()`), derives `code_challenge` using S256 (`oauth2.S256ChallengeOption(verifier)`), generates a random `state` parameter (16 bytes, hex-encoded), stores both in a signed HMAC-SHA256 cookie (`admin_oidc_state`, `HttpOnly`, `Secure`, `SameSite=Lax`, TTL 10 min), and redirects 302 to the OIDC provider authorization endpoint with `code_challenge`, `code_challenge_method=S256`, `state`, and `redirect_uri` pointing to `/admin/auth/callback`

2. **Given** `GET /admin/auth/callback?code=<code>&state=<state>` is called without the `admin_oidc_state` cookie, **When** handled, **Then** it returns `400 Bad Request`

3. **Given** `GET /admin/auth/callback?code=<code>&state=<state>` is called with a state that does not match the cookie value, **When** handled, **Then** it returns `400 Bad Request` (CSRF protection)

4. **Given** a valid `state` match, **When** the callback is processed, **Then** it exchanges `code` + `code_verifier` (from cookie) for tokens via `oauth2.Config.Exchange` with `oauth2.VerifierOption(verifier)`

5. **Given** a successful token exchange and validated ID token, **When** claims are extracted, **Then** the gateway extracts `sub`, `email`, and the role claim (via `OIDCClaimRole`), maps to `system_role`, creates a signed HMAC-SHA256 `admin_session` cookie (`HttpOnly`, `Secure`, `SameSite=Strict`, TTL 8h, payload: `sub`/`email`/`role`/`exp`), deletes the `admin_oidc_state` cookie, and redirects 303 to `/admin/`

6. **Given** a failed token exchange (e.g., invalid code) or invalid ID token, **When** the callback is processed, **Then** it redirects 302 to `/admin/auth/login?error=auth_failed`

7. **Given** `golang.org/x/oauth2` is imported directly in the new code, **When** `make build-gateway` runs, **Then** compilation succeeds and `go.mod` promotes `golang.org/x/oauth2` from indirect to direct

## Tasks / Subtasks

- [x] Create `gateway/internal/admin/auth.go` (AC: #1–#6)
  - [x] Define `AdminAuth` struct: `provider *auth.Provider`, `clientID string`, `clientSecret string`, `claimName string`, `secret []byte`
  - [x] Implement `NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte) *AdminAuth`
  - [x] Implement private `signCookie(payload []byte) string` — `base64url(payload) + "." + base64url(HMAC-SHA256(secret, base64url(payload)))`
  - [x] Implement private `verifyCookie(value string) ([]byte, error)` — split on ".", recompute HMAC, compare with `hmac.Equal` (constant-time)
  - [x] Implement private `mapAdminRole(rawClaim string) string` — mirrors `middleware.mapRole` (same switch logic)
  - [x] Implement `LoginHandler(w http.ResponseWriter, r *http.Request)`:
    - Return 503 if `a.provider.Inner() == nil`
    - `verifier := oauth2.GenerateVerifier()`
    - Generate 16-byte state via `crypto/rand.Read`, hex-encode it
    - Build `oauth2.Config` with `a.provider.Inner().Endpoint()`, clientID/Secret, scopes `openid profile email`, redirectURL from request (`scheme + "://" + r.Host + "/admin/auth/callback"`)
    - Marshal `oidcStateCookie{State, Verifier, Exp: now+10min}` → sign → set `admin_oidc_state` cookie
    - Redirect 302 to `config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))`
  - [x] Implement `CallbackHandler(w http.ResponseWriter, r *http.Request)`:
    - Read `state` and `code` from query params
    - Read `admin_oidc_state` cookie → `verifyCookie` → unmarshal → check `exp` → 400 on any failure
    - Compare query `state` with cookie state → 400 Bad Request on mismatch
    - Build `oauth2.Config` (same as login), `config.Exchange(ctx, code, oauth2.VerifierOption(verifier))`
    - On exchange error: redirect 302 to `/admin/auth/login?error=auth_failed`, return
    - Extract raw ID token: `token.Extra("id_token").(string)` → verify via `provider.Verifier`
    - On ID token validation failure: redirect 302 to `/admin/auth/login?error=auth_failed`, return
    - Extract claims map → `sub`, `email`, raw role via `a.claimName` → `mapAdminRole` → `systemRole`
    - Marshal `adminSessionCookie{Sub, Email, Role: systemRole, Exp: now+8h}` → sign → set `admin_session` cookie (HttpOnly, Secure, SameSite=Strict, MaxAge=28800)
    - Delete `admin_oidc_state` cookie (MaxAge=-1)
    - Redirect 303 to `/admin/`

- [x] Create `gateway/internal/admin/auth_test.go` (AC: #1–#6)
  - [x] Use `package admin` (consistent with `metrics_test.go`)
  - [x] Create `setupAdminOIDCServer(t)` returning `(*httptest.Server, *rsa.PrivateKey)` — extends the pattern from `middleware/auth_test.go`:
    - Discovery doc includes `authorization_endpoint`, `token_endpoint`, and `jwks_uri`
    - `/token` endpoint returns a signed ID token (via RSA key) with `sub`, `email`, configurable role claim
    - `/keys` serves the RSA public key JWKS
  - [x] `TestSignAndVerifyCookie`: sign payload → verify → check payload matches; tampered value → error
  - [x] `TestMapAdminRole`: table-driven: `"instance_admin"` → same, `"compliance_officer"` → same, `"superadmin"` → `"user"`, `""` → `"user"`
  - [x] `TestAdminLoginHandler_SetsStateAndRedirects`: 302 response, `admin_oidc_state` cookie present with HttpOnly+SameSite=Lax, Location header contains `code_challenge_method=S256` and non-empty `state` query param
  - [x] `TestAdminLoginHandler_ProviderUnavailable_Returns503`: create AdminAuth with nil-Inner provider → 503
  - [x] `TestAdminCallbackHandler_MissingCookie_Returns400`: no `admin_oidc_state` cookie → 400
  - [x] `TestAdminCallbackHandler_StateMismatch_Returns400`: valid cookie with state="expected", query state="wrong" → 400
  - [x] `TestAdminCallbackHandler_ExpiredCookie_Returns400`: cookie with `exp` = 1 (past) → 400
  - [x] `TestAdminCallbackHandler_TokenExchangeFailure_Redirects`: token endpoint returns 400 → 302 redirect to `/admin/auth/login?error=auth_failed`

- [x] Modify `gateway/cmd/gateway/main.go` (AC: #1, #7)
  - [x] Add `context` import (if not present)
  - [x] Add `"github.com/nebu/nebu/internal/auth"` import
  - [x] After `config.Load()`, initialize OIDC provider: `oidcProvider := auth.NewProvider(context.Background(), cfg.OIDCIssuer)`
  - [x] After `internalSecret` is read from file, create: `adminAuth := admin.NewAdminAuth(oidcProvider, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCClaimRole, []byte(internalSecret))`
  - [x] Register routes on `mux`: `mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)` and `mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)`

- [x] Verify `go.mod` promotion (AC: #7)
  - [x] Run `make build-gateway` — confirms `golang.org/x/oauth2` moves to direct dep

## Dev Notes

### What This Story Does

Adds PKCE-protected OIDC Authorization Code handlers (`GET /admin/auth/login`, `GET /admin/auth/callback`) to `gateway/internal/admin/`. This is the auth foundation that Epic 3 (Stories 3.9–3.11) builds on. Story 2.6 handles: verifier/state generation, cookie signing, token exchange, ID token validation, and session cookie creation. No templates, no UI — pure HTTP redirect/cookie handlers.

### File Structure

```
gateway/
  internal/
    admin/
      metrics.go      ← EXISTS — do not touch
      api_gen.go      ← EXISTS (auto-generated) — do not touch
      metrics_test.go ← EXISTS — do not touch
      auth.go         ← CREATE
      auth_test.go    ← CREATE
  cmd/gateway/
    main.go           ← MODIFY (add oidcProvider init + admin auth routes)
```

### CRITICAL: OIDC Provider Not Yet Initialized in main.go

Current `main.go` does **not** call `auth.NewProvider` — the `OIDCIssuer` config exists but is unused. Add initialization after `config.Load()`:

```go
// auth.NewProvider tolerates an unreachable OIDC provider at startup
// (logs warning, starts background retry). LoginHandler checks Inner() != nil.
oidcProvider := auth.NewProvider(context.Background(), cfg.OIDCIssuer)
```

### AdminAuth Struct and Constructor

```go
package admin

import (
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "net/http"
    "strings"
    "time"

    "github.com/coreos/go-oidc/v3/oidc"
    "github.com/nebu/nebu/internal/auth"
    "golang.org/x/oauth2"
)

type AdminAuth struct {
    provider     *auth.Provider
    clientID     string
    clientSecret string
    claimName    string // from cfg.OIDCClaimRole
    secret       []byte // HMAC key — same internalSecret as PSK
}

func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte) *AdminAuth {
    return &AdminAuth{
        provider:     provider,
        clientID:     clientID,
        clientSecret: clientSecret,
        claimName:    claimName,
        secret:       secret,
    }
}
```

### Cookie Internal Types

```go
type oidcStateCookie struct {
    State    string `json:"state"`
    Verifier string `json:"verifier"`
    Exp      int64  `json:"exp"` // Unix timestamp (seconds)
}

type adminSessionCookie struct {
    Sub   string `json:"sub"`
    Email string `json:"email"`
    Role  string `json:"role"` // mapped system_role
    Exp   int64  `json:"exp"`
}
```

### Cookie Signing (HMAC-SHA256)

```go
// signCookie: base64url(payload) + "." + base64url(HMAC-SHA256(secret, base64url(payload)))
func (a *AdminAuth) signCookie(payload []byte) string {
    encoded := base64.RawURLEncoding.EncodeToString(payload)
    mac := hmac.New(sha256.New, a.secret)
    mac.Write([]byte(encoded))
    sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    return encoded + "." + sig
}

// verifyCookie: split on ".", verify HMAC in constant time, decode payload
func (a *AdminAuth) verifyCookie(value string) ([]byte, error) {
    idx := strings.LastIndex(value, ".")
    if idx < 0 {
        return nil, errors.New("invalid cookie format")
    }
    encoded, sigPart := value[:idx], value[idx+1:]
    mac := hmac.New(sha256.New, a.secret)
    mac.Write([]byte(encoded))
    expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(expectedSig), []byte(sigPart)) {
        return nil, errors.New("invalid cookie signature")
    }
    return base64.RawURLEncoding.DecodeString(encoded)
}
```

### PKCE + oauth2.Config Pattern

```go
// golang.org/x/oauth2 v0.34.0 (already in go.mod as indirect → becomes direct)
verifier := oauth2.GenerateVerifier() // Base64URL, 43–128 chars

scheme := "https"
if r.TLS == nil {
    scheme = "http"
}
redirectURL := scheme + "://" + r.Host + "/admin/auth/callback"

oauth2Config := &oauth2.Config{
    ClientID:     a.clientID,
    ClientSecret: a.clientSecret,
    RedirectURL:  redirectURL,
    Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
    Endpoint:     a.provider.Inner().Endpoint(),
}

// In LoginHandler:
authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
http.Redirect(w, r, authURL, http.StatusFound)

// In CallbackHandler:
token, err := oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(verifier))
```

### State Generation

```go
stateBytes := make([]byte, 16)
if _, err := rand.Read(stateBytes); err != nil {
    http.Error(w, "internal error", http.StatusInternalServerError)
    return
}
state := hex.EncodeToString(stateBytes)
```

### ID Token Extraction and Validation in CallbackHandler

```go
rawIDToken, ok := token.Extra("id_token").(string)
if !ok || rawIDToken == "" {
    http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
    return
}

inner := a.provider.Inner()
if inner == nil {
    http.Error(w, "OIDC provider unavailable", http.StatusServiceUnavailable)
    return
}
idToken, err := inner.Verifier(&oidc.Config{ClientID: a.clientID}).Verify(r.Context(), rawIDToken)
if err != nil {
    http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
    return
}

var claims map[string]interface{}
_ = idToken.Claims(&claims)
sub, _ := claims["sub"].(string)
email, _ := claims["email"].(string)
rawRole, _ := claims[a.claimName].(string)
systemRole := mapAdminRole(rawRole)
```

### mapAdminRole Function

```go
// mapAdminRole maps a raw OIDC claim to a canonical Nebu system role.
// Mirrors middleware.mapRole — both must remain in sync until refactored to shared package.
// Future Story 6.3 will add role_overrides DB lookup — update both functions at that time.
func mapAdminRole(rawClaim string) string {
    switch rawClaim {
    case "instance_admin", "compliance_officer":
        return rawClaim
    default:
        return "user"
    }
}
```

### Cookie Attributes Summary

| Cookie | Name | Path | MaxAge | HttpOnly | Secure | SameSite |
|---|---|---|---|---|---|---|
| OIDC state | `admin_oidc_state` | `/admin/auth` | 600 (10min) | yes | `r.TLS != nil` | Lax |
| Admin session | `admin_session` | `/admin` | 28800 (8h) | yes | `r.TLS != nil` | Strict |

**Story 3.11 reads `admin_session` cookie** — the field names (`sub`, `email`, `role`, `exp`) and the HMAC signing scheme established here are final API contracts.

### Test Helper: setupAdminOIDCServer

The admin test helper must extend the middleware test pattern with `/token` endpoint:

```go
func setupAdminOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
    t.Helper()
    privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
    jwk := jose.JSONWebKey{Key: &privateKey.PublicKey, KeyID: "test-key-1", Algorithm: string(jose.RS256), Use: "sig"}
    jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

    var serverURL string
    mux := http.NewServeMux()
    mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewEncoder(w).Encode(map[string]any{
            "issuer":                  serverURL,
            "authorization_endpoint": serverURL + "/authorize",
            "token_endpoint":         serverURL + "/token",
            "jwks_uri":               serverURL + "/keys",
        })
    })
    mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewEncoder(w).Encode(jwks)
    })
    mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
        // Build a valid ID token for happy-path tests
        idToken := signAdminJWT(t, serverURL, privateKey, time.Now().Add(time.Hour), "nebu_role", "instance_admin")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "access_token": "mock-access-token",
            "token_type":   "Bearer",
            "id_token":     idToken,
        })
    })
    srv := httptest.NewServer(mux)
    serverURL = srv.URL
    t.Cleanup(srv.Close)
    return srv, privateKey
}
```

For the token exchange failure test, create a separate server where `/token` returns `http.StatusBadRequest`.

### go.mod Change

`golang.org/x/oauth2` is already present as indirect dep at v0.34.0. Importing it in `auth.go` promotes it to direct. No version bump needed:

```
// Before:
golang.org/x/oauth2 v0.34.0 // indirect

// After (after go mod tidy / make build-gateway):
golang.org/x/oauth2 v0.34.0
```

### Build & Test

```bash
make test-unit-go    # go test -race ./... in container — must pass 0 failures
make build-gateway   # verifies compilation + updates go.mod direct dep
```

### Cross-Story Context

| Story | Relationship |
|---|---|
| 2.5 | `OIDCClaimRole` config field + `mapRole` pattern → Story 2.6 uses `cfg.OIDCClaimRole` as `claimName` and replicates mapping as `mapAdminRole` |
| 2.7 | gRPC metadata flow — independent, no interaction with admin auth |
| 3.9 | Admin login page UI at `/admin/login/start` — uses same `admin_oidc_state` cookie format |
| 3.10 | Admin callback at `/admin/callback` — same `admin_session` cookie format; replaces Story 2.6 handlers with full version |
| 3.11 | `SessionGuard` reads `admin_session` — field names (`sub`, `email`, `role`, `exp`) and HMAC format are frozen after this story |
| 6.3 | Adds `role_overrides` DB lookup to role mapping — both `mapAdminRole` here and `mapRole` in middleware will need updating |

### Project Structure Notes

- Test package: `package admin` (white-box, consistent with `metrics_test.go`)
- `api_gen.go` uses `ServerInterface` for Admin API (`/api/v1/*`) — Story 2.6 auth handlers are NOT part of that interface (different URL space)
- Architecture rule "Admin UI via embed.FS" — not triggered here (no template rendering in Story 2.6)
- Architecture rule "Admin API must implement ServerInterface" — not triggered (auth handlers are separate from the CRUD API)

### References

- [Source: epics.md#Story-2.6] ACs 1–7 (authoritative)
- [Source: epics.md#Story-3.9] Cookie names + attributes: `admin_oidc_state` (HttpOnly, Secure, SameSite=Lax, 10min)
- [Source: epics.md#Story-3.10] Session cookie: `admin_session` (HttpOnly, Secure, SameSite=Strict, 8h), HMAC-SHA256
- [Source: epics.md#Story-3.11] `SessionGuard` reads `admin_session` — cookie schema is final after this story
- [Source: gateway/internal/auth/oidc.go] `auth.Provider.Inner()` → nil-safe OIDC provider access
- [Source: gateway/internal/config/config.go] `OIDCClientID`, `OIDCClientSecret`, `OIDCClaimRole` — all present, no new fields needed
- [Source: gateway/internal/middleware/auth.go] `mapRole` pattern → replicated as `mapAdminRole`
- [Source: gateway/internal/middleware/auth_test.go] `setupOIDCServer` helper → extend for admin (add `/token` endpoint)
- [Source: gateway/cmd/gateway/main.go] PSK read pattern for `internalSecret` + mux setup
- [Source: gateway/go.mod] `golang.org/x/oauth2 v0.34.0 // indirect` → becomes direct
- [Source: 2-5-oidc-claim-to-role-mapping.md#Cross-Story-Context] "Story 2.6 adds golang.org/x/oauth2 to go.mod"

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

### Completion Notes List

- Created `gateway/internal/admin/auth.go` with `AdminAuth` struct, HMAC-SHA256 cookie signing/verification, PKCE-protected `LoginHandler` (302 → OIDC), and `CallbackHandler` (token exchange, ID token validation, session cookie creation, state cleanup)
- Created `gateway/internal/admin/auth_test.go` with 8 test cases covering all ACs: cookie round-trip, tamper detection, role mapping, login handler (redirect + 503), callback handler (400 missing cookie, 400 state mismatch, 400 expired, 302 exchange failure)
- Modified `gateway/cmd/gateway/main.go`: added `context` + `auth` imports, initialized `oidcProvider` after config load, wired `adminAuth` with `NewAdminAuth`, registered `GET /admin/auth/login` and `GET /admin/auth/callback` routes
- `golang.org/x/oauth2` and `github.com/go-jose/go-jose/v4` promoted from indirect to direct deps in `gateway/go.mod`
- `make test-unit-go` and `make build-gateway` both pass cleanly

### File List

- `gateway/internal/admin/auth.go` (created)
- `gateway/internal/admin/auth_test.go` (created)
- `gateway/cmd/gateway/main.go` (modified)
- `gateway/go.mod` (modified — oauth2 + go-jose promoted to direct)

## Change Log

- 2026-03-27: Implemented PKCE-protected OIDC Authorization Code flow for Admin UI — created auth.go + auth_test.go, wired routes in main.go, promoted go.mod deps to direct
- 2026-03-27: Code review passed — 1 LOW fix applied (added error handling for `idToken.Claims()` in CallbackHandler). All ACs verified, all tasks confirmed done, security review clean.

# Story 2.4: OIDC JWT Token Validation Middleware

Status: done

## Story

As an end-user,
I want my OIDC token to be validated on every Matrix API request,
so that only authenticated users can access the API and my identity is reliably established per request.

## Acceptance Criteria

1. **Given** a Matrix API request with a valid `Authorization: Bearer <jwt>` header, **When** the middleware processes it, **Then** it validates the JWT signature against the cached JWKS, checks expiry, issuer, and audience — and passes the request to the handler with claims in context

2. **Given** a request with an expired token, **When** the middleware processes it, **Then** it returns `401 Unauthorized` with Matrix error body `{"errcode": "M_UNKNOWN_TOKEN", "error": "Token has expired"}`

3. **Given** a request with an invalid signature, **When** the middleware processes it, **Then** it returns `401 Unauthorized` with `{"errcode": "M_UNKNOWN_TOKEN", "error": "Invalid token"}`

4. **Given** a request with no `Authorization` header, **When** the middleware processes it, **Then** it returns `401 Unauthorized` with `{"errcode": "M_MISSING_TOKEN", "error": "Missing access token"}`

5. **Given** the middleware scope, **When** its implementation is reviewed, **Then** it performs JWT validation only — no OAuth2 code exchange, no PKCE, no redirect logic

6. **Given** validated claims, **When** extracted from the JWT, **Then** `sub`, `preferred_username`, `email`, and `nebu_role` are made available in the request context for downstream handlers

## Tasks / Subtasks

- [x] Create `gateway/internal/middleware/auth.go` (AC: #1–#6)
  - [x] Define unexported `contextKey string` type and exported constants: `ContextKeySub`, `ContextKeyPreferredUsername`, `ContextKeyEmail`, `ContextKeyNebuRole`
  - [x] Define `writeMatrixError(w http.ResponseWriter, status int, errcode, message string)` helper
  - [x] Implement `JWTMiddleware(provider *auth.Provider, clientID string) func(http.Handler) http.Handler`
  - [x] Extract `Authorization: Bearer <token>` → return `401 M_MISSING_TOKEN` if absent or not `Bearer ` prefix
  - [x] Call `provider.Inner()` → if nil return `401 M_UNKNOWN_TOKEN "OIDC provider unavailable"`
  - [x] Create verifier: `provider.Inner().Verifier(&oidc.Config{ClientID: clientID})`
  - [x] Call `verifier.Verify(r.Context(), rawToken)` → use `errors.As(err, &expiredErr)` with `*oidc.TokenExpiredError` to discriminate "Token has expired" vs "Invalid token"
  - [x] Extract claims via `idToken.Claims(&claims)` struct with `sub`, `preferred_username`, `email`, `nebu_role` json tags
  - [x] Set claims in context with typed keys; call `next.ServeHTTP(w, r.WithContext(ctx))`

- [x] Create `gateway/internal/middleware/auth_test.go` (AC: #1–#4)
  - [x] `TestJWTMiddleware_MissingToken`: no Authorization header → `401`, body contains `"M_MISSING_TOKEN"`
  - [x] `TestJWTMiddleware_ValidToken`: valid signed JWT → handler called, `ContextKeySub` populated in context
  - [x] `TestJWTMiddleware_ExpiredToken`: JWT with past expiry → `401`, body contains `"Token has expired"`
  - [x] `TestJWTMiddleware_InvalidSignature`: JWT signed with wrong key → `401`, body contains `"Invalid token"`
  - [x] `TestJWTMiddleware_NilProvider`: `provider.Inner()` returns nil (unreachable OIDC) → `401`
  - [x] Run `go test -race ./internal/middleware/...` passes

## Dev Notes

### Library: `github.com/coreos/go-oidc/v3` v3.17.0

**Already in `gateway/go.mod` — do NOT run `go get` or modify go.mod.** No new direct dependencies needed for this story.

`go-jose/go-jose/v4` is also already present as an indirect dep (pulled in by go-oidc). Tests can import `github.com/go-jose/go-jose/v4` and `github.com/go-jose/go-jose/v4/jwt` without changing go.mod.

**Token verification:**
```go
verifier := provider.Inner().Verifier(&oidc.Config{ClientID: clientID})
idToken, err := verifier.Verify(r.Context(), rawToken)
```

**Expiry error discrimination** — `go-oidc/v3` exports `*oidc.TokenExpiredError`:
```go
var expiredErr *oidc.TokenExpiredError
if errors.As(err, &expiredErr) {
    // Token has expired
} else {
    // Invalid token (wrong sig, wrong issuer, wrong aud, malformed, etc.)
}
```

**Claims extraction:**
```go
var claims struct {
    Sub               string `json:"sub"`
    PreferredUsername string `json:"preferred_username"`
    Email             string `json:"email"`
    NebuRole          string `json:"nebu_role"`
}
if err := idToken.Claims(&claims); err != nil {
    // unexpected — return 500 M_UNKNOWN
}
```

### Canonical Implementation: `gateway/internal/middleware/auth.go`

```go
package middleware

import (
    "context"
    "encoding/json"
    "errors"
    "log"
    "net/http"
    "strings"

    "github.com/coreos/go-oidc/v3/oidc"
    "github.com/nebu/nebu/internal/auth"
)

type contextKey string

const (
    ContextKeySub               contextKey = "sub"
    ContextKeyPreferredUsername contextKey = "preferred_username"
    ContextKeyEmail             contextKey = "email"
    ContextKeyNebuRole          contextKey = "nebu_role"
)

type matrixError struct {
    ErrCode string `json:"errcode"`
    Error_  string `json:"error"`
}

func writeMatrixError(w http.ResponseWriter, status int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Error_: message})
}

// JWTMiddleware validates OIDC JWT tokens. On success, populates context with
// sub, preferred_username, email, nebu_role claims for downstream handlers.
func JWTMiddleware(provider *auth.Provider, clientID string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
                writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing access token")
                return
            }
            rawToken := strings.TrimPrefix(authHeader, "Bearer ")

            inner := provider.Inner()
            if inner == nil {
                writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "OIDC provider unavailable")
                return
            }

            verifier := inner.Verifier(&oidc.Config{ClientID: clientID})
            idToken, err := verifier.Verify(r.Context(), rawToken)
            if err != nil {
                var expiredErr *oidc.TokenExpiredError
                if errors.As(err, &expiredErr) {
                    writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token has expired")
                } else {
                    writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid token")
                }
                return
            }

            var claims struct {
                Sub               string `json:"sub"`
                PreferredUsername string `json:"preferred_username"`
                Email             string `json:"email"`
                NebuRole          string `json:"nebu_role"`
            }
            if err := idToken.Claims(&claims); err != nil {
                log.Printf("failed to extract JWT claims: %v", err)
                writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
                return
            }

            ctx := r.Context()
            ctx = context.WithValue(ctx, ContextKeySub, claims.Sub)
            ctx = context.WithValue(ctx, ContextKeyPreferredUsername, claims.PreferredUsername)
            ctx = context.WithValue(ctx, ContextKeyEmail, claims.Email)
            ctx = context.WithValue(ctx, ContextKeyNebuRole, claims.NebuRole)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

**Important:** The `matrixError` struct uses `Error_` as the Go field name to avoid collision with the `error` interface, but the JSON tag must be `"error"` — double-check the JSON tag spelling. Alternative: use `Err string \`json:"error"\`` as the field name.

### Test Implementation Guide: `gateway/internal/middleware/auth_test.go`

Tests use an httptest OIDC server with a real RSA key for end-to-end JWT validation. `go-jose/v4` is available as an indirect dep — no go.mod change needed.

```go
package middleware_test

import (
    "context"
    "crypto/rand"
    "crypto/rsa"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    jose "github.com/go-jose/go-jose/v4"
    josejwt "github.com/go-jose/go-jose/v4/jwt"
    "github.com/nebu/nebu/internal/auth"
    "github.com/nebu/nebu/internal/middleware"
)

// setupOIDCServer returns a running httptest server and RSA private key.
// The server serves /.well-known/openid-configuration and /keys.
func setupOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
    t.Helper()
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatalf("rsa.GenerateKey: %v", err)
    }

    jwk := jose.JSONWebKey{
        Key:       &privateKey.PublicKey,
        KeyID:     "test-key-1",
        Algorithm: string(jose.RS256),
        Use:       "sig",
    }
    jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

    var serverURL string
    mux := http.NewServeMux()
    mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "issuer":   serverURL,
            "jwks_uri": serverURL + "/keys",
        })
    })
    mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(jwks)
    })

    srv := httptest.NewServer(mux)
    serverURL = srv.URL
    t.Cleanup(srv.Close)
    return srv, privateKey
}

// signJWT creates a signed JWT with the given expiry.
func signJWT(t *testing.T, serverURL string, privateKey *rsa.PrivateKey, expiry time.Time) string {
    t.Helper()
    signer, err := jose.NewSigner(
        jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
        (&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
    )
    if err != nil {
        t.Fatalf("NewSigner: %v", err)
    }
    cl := josejwt.Claims{
        Subject:  "test-sub-123",
        Issuer:   serverURL,
        Audience: josejwt.Audience{"nebu-gateway"},
        Expiry:   josejwt.NewNumericDate(expiry),
        IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
    }
    extra := map[string]any{
        "preferred_username": "kai.mueller",
        "email":              "kai@example.com",
        "nebu_role":          "instance_admin",
    }
    raw, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
    if err != nil {
        t.Fatalf("Serialize: %v", err)
    }
    return raw
}
```

**Test helpers allow each test to focus on the scenario.** Key points:
- `setupOIDCServer` must set `serverURL` BEFORE the handler closure runs (same pattern as `oidc_test.go`)
- `signJWT` with `expiry: time.Now().Add(-time.Hour)` → expired token
- For invalid signature: use a second `rsa.GenerateKey` key to sign — the server doesn't serve its JWKS, or sign with a key whose `kid` doesn't match
- For nil provider: use `auth.NewProvider(ctx, "http://127.0.0.1:0")` (port 0 = always closed)

**Test: Valid Token**
```go
func TestJWTMiddleware_ValidToken(t *testing.T) {
    srv, key := setupOIDCServer(t)
    provider := auth.NewProvider(context.Background(), srv.URL)
    rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

    called := false
    handler := middleware.JWTMiddleware(provider, "nebu-gateway")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
        sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
        if sub != "test-sub-123" {
            t.Errorf("expected sub=test-sub-123, got %q", sub)
        }
        w.WriteHeader(http.StatusOK)
    }))

    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer "+rawToken)
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if !called {
        t.Error("handler was not called for valid token")
    }
    if rr.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", rr.Code)
    }
}
```

### Project Structure Notes

```
gateway/
  internal/
    middleware/
      psk.go              ← EXISTS (PSK middleware — do not modify)
      psk_test.go         ← EXISTS
      auth.go             ← CREATE (this story)
      auth_test.go        ← CREATE (this story)
    auth/
      oidc.go             ← EXISTS — provides Provider.Inner() — import as "github.com/nebu/nebu/internal/auth"
      oidc_test.go        ← EXISTS — do NOT modify
```

**Do NOT touch:** `main.go`, `config.go`, `oidc.go`, any Elixir files, migrations, gRPC files. No wiring into `main.go` is needed — Matrix routes don't exist yet; the middleware is created here and wired in Epic 4 stories.

### `matrixError` JSON Field Name

The field must serialize to `"error"` in JSON. Using `Error_` with `json:"error"` tag works, but may confuse linters. Cleaner alternative:
```go
type matrixError struct {
    ErrCode string `json:"errcode"`
    Err     string `json:"error"`
}
```
Use whichever is clearer — just ensure the JSON output has `"error"` (not `"err"` or `"error_"`).

### Cross-Story Context

- **Story 2.3 created** `auth.Provider` with `Inner() *oidc.Provider`. Story 2.4 consumes it — do NOT change `Inner()` signature.
- **Story 2.5** (OIDC Claim-to-Role Mapping) adds `NEBU_OIDC_CLAIM_ROLE` config and maps `nebu_role` to `system_role`. Story 2.4 puts raw `nebu_role` claim in context; Story 2.5 adds the mapping layer. Do NOT implement role mapping in this story.
- **Story 2.7** (Auth Token Flow) forwards `user_id` (`@{sub}:{server_name}`) and `system_role` to Elixir via gRPC metadata. It will read `ContextKeySub` and `ContextKeyNebuRole` from context. Exported constants must remain stable.
- **Story 6.3** extends this middleware with `role_overrides` DB lookup (ETS-cached).

### Testing Patterns (from Story 2.3)

- Use `t.Setenv()` for env var tests — auto-cleanup, no `os.Unsetenv` needed
- Set `serverURL` var before handler closure captures it (Issue from 2.3: `oidc.NewProvider` validates that `issuer` in discovery doc matches the issuer URL — the httptest URL must be assigned before the handler fires)
- Tests run with `make test-unit-go` → `go test -race ./...` in Docker container

### Build & Test

```bash
make test-unit-go    # go test -race ./... in container — must pass with 0 failures
```

No go.mod changes required. All dependencies already present.

### References

- [Source: epics.md#Story-2.4] Authoritative ACs — all 6 acceptance criteria
- [Source: architecture.md#Auth-Token-Flow] Claims mapping: sub→user_id, nebu_role→system_role
- [Source: architecture.md#Directory-Structure line 957] File location: `gateway/internal/middleware/auth.go`
- [Source: gateway/internal/auth/oidc.go] `Provider.Inner()` returns `*oidc.Provider`, nil if unreachable
- [Source: gateway/internal/middleware/psk.go] Existing middleware pattern (same package)
- [Source: gateway/go.mod] go-oidc/v3 v3.17.0 direct dep; go-jose/v4 already indirect dep
- [Source: _bmad-output/implementation-artifacts/2-3-oidc-provider-configuration.md#Cross-Story-Dependencies] Story 2.4 consumes `provider.Inner().Verifier()`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

_No issues encountered._

### Completion Notes List

- Implemented `JWTMiddleware` in `gateway/internal/middleware/auth.go` using `go-oidc/v3` — no new dependencies needed.
- Used `errors.As(err, &expiredErr)` with `*oidc.TokenExpiredError` for precise expiry vs. invalid-signature discrimination.
- `matrixError` struct uses `Err string \`json:"error"\`` to avoid collision with Go's `error` interface while serializing to `"error"` in JSON.
- All 5 tests pass with `-race` flag: MissingToken, ValidToken, ExpiredToken, InvalidSignature, NilProvider.
- Exported context key constants (`ContextKeySub`, `ContextKeyPreferredUsername`, `ContextKeyEmail`, `ContextKeyNebuRole`) ready for Story 2.7 consumption.
- No wiring into `main.go` — per story spec, Matrix routes don't exist yet; wiring happens in Epic 4.

### File List

- gateway/internal/middleware/auth.go (created)
- gateway/internal/middleware/auth_test.go (created)

## Change Log

- 2026-03-26: Implemented OIDC JWT Token Validation Middleware (Story 2.4) — auth.go + auth_test.go created, all ACs satisfied, tests pass.
- 2026-03-27: Code review passed — 0 HIGH, 0 MEDIUM, 1 LOW (fixed: unnecessary test setup in MissingToken test). All 6 ACs verified, all tasks confirmed complete. Status → done.

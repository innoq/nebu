# Story 2.3: OIDC Provider Configuration

Status: done

## Story

As an operator,
I want the gateway to discover and cache the OIDC provider's public keys on startup,
so that token validation works without a network call on every request.

## Acceptance Criteria

1. **Given** `NEBU_OIDC_ISSUER`, `NEBU_OIDC_CLIENT_ID`, and `NEBU_OIDC_CLIENT_SECRET` env vars are set, **When** the gateway starts, **Then** it fetches the OIDC discovery document from `<issuer>/.well-known/openid-configuration` and caches the JWKS URI

2. **Given** the JWKS URI is known, **When** the gateway starts, **Then** it fetches and caches the JWKS (public keys) for JWT signature validation

3. **Given** the OIDC provider is unreachable at startup, **When** the gateway tries to fetch the discovery document, **Then** it logs a warning `"OIDC provider unreachable — token validation will fail until resolved"` but does NOT crash

4. **Given** cached JWKS, **When** 1 hour has elapsed since last fetch, **Then** the gateway refreshes the JWKS in the background (key rotation support)

5. **Given** MVP scope constraint, **When** OIDC configuration is read, **Then** it is sourced exclusively from environment variables — no runtime update via API

## Tasks / Subtasks

- [x] Extend `gateway/internal/config/config.go` with new OIDC fields (AC: #1, #5)
  - [x] Add `OIDCClientID string` (env: `NEBU_OIDC_CLIENT_ID`)
  - [x] Add `OIDCClientSecret string` (env: `NEBU_OIDC_CLIENT_SECRET`)
  - [x] Update `config_test.go`: unset new fields in `TestLoad_Defaults`, add assertion for new fields in `TestLoad_EnvVarsOverrideDefaults`

- [x] Add `github.com/coreos/go-oidc/v3` to `gateway/go.mod` (AC: #1, #2)
  - [x] Run `go get github.com/coreos/go-oidc/v3@latest` inside the build container, then `go mod tidy`

- [x] Create `gateway/internal/auth/oidc.go` (AC: #1–#4)
  - [x] Define `Provider` struct with `mu sync.RWMutex`, `inner *oidc.Provider`, `issuer string`
  - [x] Implement `NewProvider(ctx context.Context, issuer string) *Provider` — calls `discover`, logs warning on failure, starts `go refresh()`
  - [x] Implement `Inner() *oidc.Provider` — thread-safe read (used by Story 2.4 middleware)
  - [x] Implement `discover(ctx context.Context) error` — calls `oidc.NewProvider(ctx, issuer)`, updates `inner` under write lock
  - [x] Implement `refresh()` — `time.NewTicker(time.Hour)`, calls `discover` each tick, logs warning on failure

- [x] Create `gateway/internal/auth/oidc_test.go` (AC: #1, #2, #3)
  - [x] `TestNewProvider_Success`: httptest server serving discovery doc + JWKS, assert `Inner() != nil`
  - [x] `TestNewProvider_Unreachable`: unreachable issuer URL, assert no panic, `Inner() == nil`
  - [x] Run `go test -race ./internal/auth/...` passes

- [x] Update `docker-compose.yml` to include `NEBU_OIDC_CLIENT_ID` and `NEBU_OIDC_CLIENT_SECRET` for the gateway service

## Dev Notes

### Library: `github.com/coreos/go-oidc/v3`

**Use this library — do not implement discovery/JWKS manually.** It is the standard Go OIDC library (Apache 2.0 licensed, compatible with project license). `oidc.NewProvider(ctx, issuer)` fetches `<issuer>/.well-known/openid-configuration`, validates the `issuer` claim, and creates an internal `RemoteKeySet` backed by the JWKS URI. Story 2.4 (JWT validation middleware) will use `provider.Inner().Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})`.

The library requires `golang.org/x/oauth2` as an indirect dependency — `go mod tidy` resolves this automatically.

### Canonical Implementation of `gateway/internal/auth/oidc.go`

```go
package auth

import (
    "context"
    "log"
    "sync"
    "time"

    "github.com/coreos/go-oidc/v3/oidc"
)

// Provider wraps go-oidc discovery with startup-failure tolerance and
// hourly JWKS refresh for key rotation support.
type Provider struct {
    mu     sync.RWMutex
    inner  *oidc.Provider
    issuer string
}

// NewProvider performs OIDC discovery. If the provider is unreachable, it
// logs a warning and returns a Provider with nil Inner() — the gateway starts
// normally but token validation will fail until the next background refresh.
func NewProvider(ctx context.Context, issuer string) *Provider {
    p := &Provider{issuer: issuer}
    if err := p.discover(ctx); err != nil {
        log.Printf("OIDC provider unreachable — token validation will fail until resolved")
    }
    go p.refresh()
    return p
}

// Inner returns the underlying *oidc.Provider for use by the JWT validation
// middleware (Story 2.4). Returns nil if discovery has not yet succeeded.
func (p *Provider) Inner() *oidc.Provider {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.inner
}

func (p *Provider) discover(ctx context.Context) error {
    provider, err := oidc.NewProvider(ctx, p.issuer)
    if err != nil {
        return err
    }
    p.mu.Lock()
    p.inner = provider
    p.mu.Unlock()
    return nil
}

func (p *Provider) refresh() {
    t := time.NewTicker(time.Hour)
    defer t.Stop()
    for range t.C {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        if err := p.discover(ctx); err != nil {
            log.Printf("OIDC provider unreachable — token validation will fail until resolved")
        }
        cancel()
    }
}
```

### Canonical Implementation of `gateway/internal/auth/oidc_test.go`

```go
package auth_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/nebu/nebu/internal/auth"
)

func TestNewProvider_Success(t *testing.T) {
    var serverURL string

    mux := http.NewServeMux()
    mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "issuer":                 serverURL,
            "authorization_endpoint": serverURL + "/auth",
            "jwks_uri":               serverURL + "/keys",
        })
    })
    mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
    })

    srv := httptest.NewServer(mux)
    defer srv.Close()
    serverURL = srv.URL

    p := auth.NewProvider(context.Background(), srv.URL)
    if p.Inner() == nil {
        t.Fatal("Inner() must be non-nil after successful discovery")
    }
}

func TestNewProvider_Unreachable(t *testing.T) {
    // Port 0 is always closed — simulates unreachable OIDC provider.
    p := auth.NewProvider(context.Background(), "http://127.0.0.1:0")
    if p == nil {
        t.Fatal("NewProvider must never return nil")
    }
    if p.Inner() != nil {
        t.Fatal("Inner() must be nil when discovery fails")
    }
}
```

**Critical test detail:** `oidc.NewProvider` validates that the `"issuer"` field in the discovery document exactly matches the issuer URL passed to `NewProvider`. The `serverURL` variable must therefore be set before the handler closure runs — use a `var serverURL string` captured by the handler closure and assigned after `httptest.NewServer` returns.

### Config Changes — `gateway/internal/config/config.go`

Add two fields to the `Config` struct:

```go
OIDCClientID     string // NEBU_OIDC_CLIENT_ID
OIDCClientSecret string // NEBU_OIDC_CLIENT_SECRET
```

Add to `Load()`:
```go
OIDCClientID:     os.Getenv("NEBU_OIDC_CLIENT_ID"),
OIDCClientSecret: os.Getenv("NEBU_OIDC_CLIENT_SECRET"),
```

Update `config_test.go`:
- `TestLoad_Defaults`: add `os.Unsetenv("NEBU_OIDC_CLIENT_ID")` and `os.Unsetenv("NEBU_OIDC_CLIENT_SECRET")`, assert both are empty
- `TestLoad_EnvVarsOverrideDefaults`: add `t.Setenv("NEBU_OIDC_CLIENT_ID", "nebu-gateway")` and `t.Setenv("NEBU_OIDC_CLIENT_SECRET", "nebu-dev-secret")`, assert values

### `docker-compose.yml` Changes

Add to the `gateway` service `environment` block:
```yaml
NEBU_OIDC_CLIENT_ID: "nebu-gateway"
NEBU_OIDC_CLIENT_SECRET: "nebu-dev-secret"
```

These match the Dex static client in `dev/dex/config.yaml` (id: `nebu-gateway`, secret: `nebu-dev-secret`). Dex is the dev OIDC provider at `http://dex:5556`. **Note:** The architecture doc mentions Keycloak but the project already uses Dex (Story 2.20 formalises the Dex setup — do not change this).

### Project Structure Notes

```
gateway/
  go.mod                         ← ADD: github.com/coreos/go-oidc/v3
  go.sum                         ← auto-updated by go mod tidy
  internal/
    auth/
      oidc.go                    ← CREATE (directory already exists, is empty)
      oidc_test.go               ← CREATE
    config/
      config.go                  ← MODIFY: add OIDCClientID, OIDCClientSecret
      config_test.go             ← MODIFY: cover new fields
docker-compose.yml               ← MODIFY: add CLIENT_ID + CLIENT_SECRET env vars
```

**Do NOT touch:** Any Elixir files, migrations, gRPC files, health handler, PSK middleware, Prometheus handler, or Makefile.

**Do NOT create** `gateway/testdata/oidc/` — the httptest server in the unit test is sufficient for Story 2.3.

### Build & Test

All builds run in Docker containers (no local Go required):
```bash
make test-unit-go    # go test -race ./... in container — must pass with 0 failures
```

Adding `go-oidc/v3` requires running `go mod tidy` inside the build container environment. The Makefile `make test-unit-go` target calls `go test ./...` via the build container, which uses the current `go.mod`/`go.sum`. Ensure `go mod tidy` is run inside the container (or locally if Go 1.26 is available) before committing.

### Previous Story Intelligence (Story 2.2)

- Story 2.2 only created SQL files and updated `migrations_test.go` — no application code patterns to inherit directly
- The code review for Story 2.2 confirmed no regressions from migration-only changes
- Pattern: `config_test.go` uses `t.Setenv()` (automatic cleanup) for env var tests — follow this pattern

### Cross-Story Dependencies

- **Story 2.4 (OIDC JWT Token Validation Middleware)** consumes `auth.Provider.Inner()` to create `provider.Inner().Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})`. Do not change the `Inner()` signature.
- **Story 2.6 (Admin UI PKCE Flow)** will use `cfg.OIDCClientID` and `cfg.OIDCClientSecret` from config. Do not remove these fields.
- **Story 2.20 (Dex Dev Setup)** formalises the Dex configuration — do not add or modify `dev/dex/config.yaml` in this story.

### References

- [Source: epics.md#Story-2.3] Authoritative AC — all 5 acceptance criteria
- [Source: architecture.md#Auth-Token-Flow] Gateway validates OIDC token, extracts user_id + system_role
- [Source: architecture.md#Technical-Constraints] OIDC-only auth, Apache 2.0 dependency requirement
- [Source: gateway/internal/config/config.go] Existing config struct — OIDCIssuer already present
- [Source: gateway/go.mod] Current dependencies — coreos/go-oidc/v3 not yet present
- [Source: docker-compose.yml] NEBU_OIDC_ISSUER=http://dex:5556 already set; CLIENT_ID/SECRET not yet set
- [Source: dev/dex/config.yaml] Dex client id: nebu-gateway, secret: nebu-dev-secret

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

`go get` inside the build container writes to go.mod/go.sum only when the gateway directory is mounted directly (not the project root). Used `docker run -v "$(pwd)/gateway":/gateway` to ensure changes persisted.

### Completion Notes List

- Implemented `gateway/internal/auth/oidc.go`: Provider struct with `sync.RWMutex`-protected inner provider, startup-tolerant discovery, hourly background refresh goroutine.
- Added `gateway/internal/auth/oidc_test.go`: `TestNewProvider_Success` (httptest mock OIDC server) and `TestNewProvider_Unreachable` (port 0 = always closed). Both pass with `-race`.
- Extended `gateway/internal/config/config.go` with `OIDCClientID` and `OIDCClientSecret` fields.
- Updated `gateway/internal/config/config_test.go`: new fields covered in `TestLoad_Defaults` (unset) and `TestLoad_EnvVarsOverrideDefaults` (set).
- Added `github.com/coreos/go-oidc/v3 v3.17.0` to `gateway/go.mod` (with `golang.org/x/oauth2` and `github.com/go-jose/go-jose/v4` as indirect deps).
- Updated `docker-compose.yml` gateway service environment with `NEBU_OIDC_CLIENT_ID: "nebu-gateway"` and `NEBU_OIDC_CLIENT_SECRET: "nebu-dev-secret"`.
- All 10 test packages pass with 0 regressions. `internal/config` and `internal/auth` both pass.

### File List

gateway/internal/auth/oidc.go
gateway/internal/auth/oidc_test.go
gateway/internal/config/config.go
gateway/internal/config/config_test.go
gateway/go.mod
gateway/go.sum
docker-compose.yml

## Change Log

- 2026-03-26: Implemented OIDC Provider configuration — added auth.Provider with startup-tolerant discovery and hourly JWKS refresh, extended config with OIDCClientID/OIDCClientSecret, updated docker-compose.yml gateway environment, added go-oidc/v3 dependency.
- 2026-03-26: Code review passed — fixed MEDIUM issue: error details now included in OIDC unreachable log messages (both startup and refresh). LOW (goroutine shutdown) accepted for MVP.

# Story 2.17: GET /_matrix/client/v3/login — SSO Flow Discovery

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an end-user,
I want my Matrix client to discover the supported login methods,
so that it can initiate the correct SSO flow automatically.

## Acceptance Criteria

1. **Given** `GET /_matrix/client/v3/login` is called without authentication,
   **When** processed by the gateway,
   **Then** response is `200 OK` with body `{"flows": [{"type": "m.login.sso", "identity_providers": [{"id": "oidc", "name": "<issuer_display_name>", "icon": null}]}]}`

2. **Given** the Matrix spec requirement,
   **When** the response is validated,
   **Then** `Content-Type` is `application/json` and the response matches the Matrix Client-Server API format (NFR-M1)

3. **Given** Element Web pointing to a Nebu server,
   **When** the login page loads,
   **Then** it displays the SSO login button (functional validation via Dex dev setup)

## Tasks / Subtasks

- [x] Task 1: Add `NEBU_OIDC_DISPLAY_NAME` to config (AC: #1)
  - [x] Add `OIDCDisplayName` field to `Config` struct in `config.go`
  - [x] Load from env with default `"SSO"`: `getEnvOrDefault("NEBU_OIDC_DISPLAY_NAME", "SSO")`
  - [x] Add config test for default and custom value
  - [x] Add env var to `docker-compose.yml` gateway service (optional, default is fine for dev)

- [x] Task 2: Create `gateway/internal/matrix/login.go` — LoginHandler (AC: #1, #2)
  - [x] Define response structs: `LoginResponse`, `LoginFlow`, `IdentityProvider`
  - [x] Create `LoginHandler` struct with `displayName string` field
  - [x] Implement `NewLoginHandler(displayName string) *LoginHandler`
  - [x] Implement `func (h *LoginHandler) GetLogin(w http.ResponseWriter, r *http.Request)` that returns the static JSON response
  - [x] Set `Content-Type: application/json`

- [x] Task 3: Register route in `main.go` (AC: #1)
  - [x] Import `matrix` package
  - [x] Create `loginHandler := matrix.NewLoginHandler(cfg.OIDCDisplayName)`
  - [x] Register: `mux.HandleFunc("GET /_matrix/client/v3/login", loginHandler.GetLogin)`
  - [x] Route must be on the main mux (port 8008), NOT behind JWTMiddleware

- [x] Task 4: Unit tests for LoginHandler (AC: #1, #2)
  - [x] Create `gateway/internal/matrix/login_test.go`
  - [x] Test: GET returns 200 with correct JSON body structure
  - [x] Test: Content-Type is `application/json`
  - [x] Test: Response includes `m.login.sso` flow type
  - [x] Test: `identity_providers` array has exactly one entry with `id: "oidc"`
  - [x] Test: `name` field matches the configured display name
  - [x] Test: `icon` field is `null`
  - [x] Confirm `make test-unit-go` passes

- [x] Task 5: Config test for `NEBU_OIDC_DISPLAY_NAME` (AC: #1)
  - [x] Add `TestLoad_OIDCDisplayName_Default` — unset env → `"SSO"`
  - [x] Add `TestLoad_OIDCDisplayName_CustomValue` — set env → custom value
  - [x] Confirm `make test-unit-go` passes

## Dev Notes

### Scope

This story does **one thing**: implements the Matrix `GET /login` endpoint that advertises SSO as the only login method. This is the first Matrix Client-Server API endpoint in the codebase.

**NOT in this story:**
- `POST /_matrix/client/v3/login` (token exchange) → Story 2-18
- `POST /_matrix/client/v3/logout` → Story 2-19
- SSO redirect flow (`/_matrix/client/v3/login/sso/redirect/{idpId}`) → Story 2-18
- Element Web functional testing → Story 2-21 (Gherkin)

### Task 1: Config — `NEBU_OIDC_DISPLAY_NAME`

**File: `gateway/internal/config/config.go`**

Add a new field to the `Config` struct:

```go
OIDCDisplayName string // NEBU_OIDC_DISPLAY_NAME (default: "SSO")
```

In `Load()`, add:

```go
OIDCDisplayName: getEnvOrDefault("NEBU_OIDC_DISPLAY_NAME", "SSO"),
```

**Why a new env var:** The OIDC discovery document does not expose a human-readable provider name. The issuer URL (`http://dex:5556`) is not suitable for display to end users. A configurable display name lets operators set something meaningful (e.g., "Corporate SSO", "Okta", "Keycloak"). Default `"SSO"` is a safe fallback.

**Why NOT derive from issuer URL hostname:** Extracting hostname from `http://dex:5556` yields `"dex"`, which is a Docker-internal hostname — useless for end users. The env var approach is explicit and predictable.

**Test file: `gateway/internal/config/config_test.go`**

Follow existing test patterns (see `TestLoad_OIDCClaimRole_Default` and `TestLoad_OIDCClaimRole_CustomValue` at lines 112-128):

```go
func TestLoad_OIDCDisplayName_Default(t *testing.T) {
    os.Unsetenv("NEBU_OIDC_DISPLAY_NAME")
    cfg := config.Load()
    if cfg.OIDCDisplayName != "SSO" {
        t.Errorf("expected SSO, got %s", cfg.OIDCDisplayName)
    }
}

func TestLoad_OIDCDisplayName_CustomValue(t *testing.T) {
    t.Setenv("NEBU_OIDC_DISPLAY_NAME", "Corporate SSO")
    cfg := config.Load()
    if cfg.OIDCDisplayName != "Corporate SSO" {
        t.Errorf("expected Corporate SSO, got %s", cfg.OIDCDisplayName)
    }
}
```

Also update the existing `TestLoad_Defaults` (line 9): add `os.Unsetenv("NEBU_OIDC_DISPLAY_NAME")` to the cleanup block (line 10-21) and add assertion for `cfg.OIDCDisplayName == "SSO"`.

Also update `TestLoad_AllEnvVars` (line 76): add `t.Setenv("NEBU_OIDC_DISPLAY_NAME", "My Provider")` and assertion.

### Task 2: LoginHandler

**File: `gateway/internal/matrix/login.go`** (NEW — replaces `.gitkeep`)

This is the first file in the `matrix` package. Package declaration: `package matrix`.

```go
package matrix

import (
    "encoding/json"
    "net/http"
)

type IdentityProvider struct {
    ID   string  `json:"id"`
    Name string  `json:"name"`
    Icon *string `json:"icon"`
}

type LoginFlow struct {
    Type              string             `json:"type"`
    IdentityProviders []IdentityProvider `json:"identity_providers,omitempty"`
}

type LoginResponse struct {
    Flows []LoginFlow `json:"flows"`
}

type LoginHandler struct {
    displayName string
}

func NewLoginHandler(displayName string) *LoginHandler {
    return &LoginHandler{displayName: displayName}
}

func (h *LoginHandler) GetLogin(w http.ResponseWriter, r *http.Request) {
    resp := LoginResponse{
        Flows: []LoginFlow{
            {
                Type: "m.login.sso",
                IdentityProviders: []IdentityProvider{
                    {
                        ID:   "oidc",
                        Name: h.displayName,
                        Icon: nil,
                    },
                },
            },
        },
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}
```

**Key decisions:**
- `Icon` is `*string` (pointer) so `json:"icon"` marshals as `null` instead of being omitted. This matches the Matrix spec format exactly.
- `identity_providers` uses `omitempty` on the flow level for future-proofing but will always be populated for `m.login.sso`.
- No error path — the handler is purely static with no external dependencies.
- `displayName` injected at construction, not read from config at request time (immutable after startup).

**Delete `.gitkeep`:** The new `login.go` file replaces `gateway/internal/matrix/.gitkeep`. Remove `.gitkeep` after creating `login.go`.

### Task 3: Route Registration in main.go

**File: `gateway/cmd/gateway/main.go`**

Add import:
```go
"github.com/nebu/nebu/internal/matrix"
```

Add handler creation and route registration after the bootstrap handler block (after line 127) and before the `slog.Info("HTTP server starting")` line:

```go
loginHandler := matrix.NewLoginHandler(cfg.OIDCDisplayName)
mux.HandleFunc("GET /_matrix/client/v3/login", loginHandler.GetLogin)
```

**CRITICAL: No auth middleware on this route.** This endpoint is called by unauthenticated Matrix clients during login discovery. It must be accessible without any `Authorization` header. Register it directly on `mux`, NOT wrapped with `JWTMiddleware`.

**Placement rationale:** Group with Matrix endpoints. Future stories (2-18, 2-19) will add more Matrix routes in the same section.

### Task 4: Unit Tests

**File: `gateway/internal/matrix/login_test.go`** (NEW)

```go
package matrix

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestGetLogin_ReturnsSSO(t *testing.T) {
    h := NewLoginHandler("Test SSO")
    req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/login", nil)
    w := httptest.NewRecorder()

    h.GetLogin(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    ct := w.Header().Get("Content-Type")
    if ct != "application/json" {
        t.Errorf("expected application/json, got %s", ct)
    }

    var resp LoginResponse
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }

    if len(resp.Flows) != 1 {
        t.Fatalf("expected 1 flow, got %d", len(resp.Flows))
    }
    if resp.Flows[0].Type != "m.login.sso" {
        t.Errorf("expected m.login.sso, got %s", resp.Flows[0].Type)
    }

    idps := resp.Flows[0].IdentityProviders
    if len(idps) != 1 {
        t.Fatalf("expected 1 identity provider, got %d", len(idps))
    }
    if idps[0].ID != "oidc" {
        t.Errorf("expected id oidc, got %s", idps[0].ID)
    }
    if idps[0].Name != "Test SSO" {
        t.Errorf("expected name Test SSO, got %s", idps[0].Name)
    }
    if idps[0].Icon != nil {
        t.Errorf("expected icon nil, got %v", idps[0].Icon)
    }
}
```

**Test approach:** Use `httptest.NewRecorder()` (standard Go testing pattern used throughout the codebase — see `gateway/internal/admin/bootstrap_test.go`). Test is a single function that validates all ACs: status code, content type, JSON structure, flow type, identity provider fields.

**No table-driven tests needed:** There's exactly one code path — no branching logic, no error cases. A single test function is sufficient and clearer.

### Project Structure Notes

| File | Action | Reason |
|------|--------|--------|
| `gateway/internal/config/config.go` | MODIFY | Add `OIDCDisplayName` field + env loading |
| `gateway/internal/config/config_test.go` | MODIFY | Add default + custom value tests |
| `gateway/internal/matrix/login.go` | CREATE | New handler — first Matrix endpoint |
| `gateway/internal/matrix/login_test.go` | CREATE | Unit tests for LoginHandler |
| `gateway/internal/matrix/.gitkeep` | DELETE | Replaced by actual code |
| `gateway/cmd/gateway/main.go` | MODIFY | Import matrix package, register route |

No new dependencies. No `go.mod` changes. No database access. No gRPC calls. No Elixir changes.

### Previous Story Intelligence

**From Story 2-16 (Bootstrap Mode Permanent Deactivation):**
- Go handler pattern: struct with constructor + method registered via `mux.HandleFunc`
- Test pattern: `httptest.NewRequest` + `httptest.NewRecorder`
- Route registration: `mux.HandleFunc("GET /path", handler.Method)` in `main.go`
- Commit message pattern: `Story 2-N`
- `http.NotFound` for 404 (not applicable here but shows error response patterns)

**From Story 2-4 (OIDC JWT Validation Middleware):**
- `writeMatrixError()` helper in `middleware/auth.go` for Matrix-spec error responses — reuse in future Matrix handlers but NOT needed in this story (no error paths)
- `matrixError` struct with `errcode` + `error` fields — consistent Matrix error format for future stories

**From Story 2-6 (Admin UI OIDC Client):**
- `AdminAuth` struct pattern: holds config values, exposes handler methods
- OIDC provider info is available at `auth.Provider` level — but `go-oidc` doesn't expose a display name, so we need the env var approach

**Docker Compose context:**
- `NEBU_OIDC_ISSUER: "http://dex:5556"` — issuer URL is a Docker-internal hostname
- Dex redirectURI already includes `/_matrix/client/v3/login/sso/redirect/oidc` — prepared for Story 2-18
- Dex `staticClients[0].name: "Nebu Gateway"` — this is the OAuth2 client name, NOT the provider display name

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.17 — SSO Flow Discovery acceptance criteria]
- [Source: _bmad-output/planning-artifacts/architecture.md — Directory structure: `gateway/internal/matrix/login.go`]
- [Source: _bmad-output/planning-artifacts/architecture.md — G8 server_name immutability, NFR-M1 Matrix compatibility]
- [Source: _bmad-output/planning-artifacts/architecture.md — Naming: Go PascalCase exports, snake_case JSON tags]
- [Source: _bmad-output/planning-artifacts/epics.md — NFR-M2: OIDC-Integration via m.login.sso]
- [Source: gateway/internal/config/config.go — Existing config pattern with getEnvOrDefault]
- [Source: gateway/cmd/gateway/main.go — Route registration pattern on mux]
- [Source: gateway/internal/admin/bootstrap.go — Handler struct pattern with constructor]
- [Source: gateway/internal/middleware/auth.go — Matrix error response format for future reference]
- [Source: dev/dex/config.yaml — Dex setup with redirectURI for Matrix SSO]
- [Source: docker-compose.yml — NEBU_OIDC_ISSUER=http://dex:5556]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

None — clean implementation, all tests passed first run.

### Completion Notes List

- Task 1: Added `OIDCDisplayName` field to Config struct with `getEnvOrDefault("NEBU_OIDC_DISPLAY_NAME", "SSO")`. Updated `TestLoad_Defaults` and `TestLoad_EnvVarsOverrideDefaults` with new field assertions.
- Task 2: Created `gateway/internal/matrix/login.go` — first file in matrix package. `LoginHandler` struct with `GetLogin` method returns static JSON with `m.login.sso` flow and configurable identity provider display name. `Icon` uses `*string` for proper `null` JSON marshaling.
- Task 3: Registered `GET /_matrix/client/v3/login` route on main mux (port 8008), NOT behind auth middleware. This is the first Matrix Client-Server API endpoint.
- Task 4: Created `login_test.go` with `TestGetLogin_ReturnsSSO` covering status code, Content-Type, JSON structure, flow type, identity provider fields (id, name, icon null).
- Task 5: Added `TestLoad_OIDCDisplayName_Default` and `TestLoad_OIDCDisplayName_CustomValue` config tests. All `make test-unit-go` passes with 0 failures.

### Change Log

- 2026-03-30: Story 2-17 implementation complete — GET /_matrix/client/v3/login SSO flow discovery endpoint

### File List

- `gateway/internal/config/config.go` — MODIFIED (added OIDCDisplayName field + env loading)
- `gateway/internal/config/config_test.go` — MODIFIED (added default/custom tests, updated existing tests)
- `gateway/internal/matrix/login.go` — CREATED (LoginHandler with GetLogin method)
- `gateway/internal/matrix/login_test.go` — CREATED (unit tests for LoginHandler)
- `gateway/internal/matrix/.gitkeep` — DELETED (replaced by actual code)
- `gateway/cmd/gateway/main.go` — MODIFIED (imported matrix package, registered route)

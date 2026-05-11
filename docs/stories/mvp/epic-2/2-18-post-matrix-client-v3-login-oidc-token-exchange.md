# Story 2.18: POST /_matrix/client/v3/login — OIDC Token Exchange

Status: done

## Story

As an end-user,
I want to exchange my OIDC token for a Matrix access token,
So that I can authenticate with any standard Matrix client using my organisation's SSO.

## Acceptance Criteria

1. **Given** `POST /_matrix/client/v3/login` with body `{"type": "m.login.token", "token": "<valid_oidc_jwt>"}`,
   **When** processed,
   **Then** the JWT is validated via OIDC provider (Story 2.4 logic), user is provisioned if new (Stories 2.12–2.13 via ValidateToken gRPC), and response is `200 OK` with `{"access_token": "<oidc_jwt>", "device_id": "<uuid>", "user_id": "@<sub>:<server_name>", "token_type": "Bearer"}`

2. **Given** the `access_token` in the response,
   **When** used in a subsequent Matrix API request as `Authorization: Bearer <token>`,
   **Then** it is accepted — the OIDC JWT IS the Matrix access token (stateless validation per request, NFR-S4)

3. **Given** `POST /login` with an invalid or expired token,
   **When** processed,
   **Then** response is `403 Forbidden` with `{"errcode": "M_FORBIDDEN", "error": "Invalid or expired token"}`

4. **Given** `POST /login` with an unsupported `type` field,
   **When** processed,
   **Then** response is `400 Bad Request` with `{"errcode": "M_UNKNOWN", "error": "Unsupported login type"}`

## Tasks / Subtasks

- [x] Task 1: Extend LoginHandler with POST dependencies (AC: 1)
  - [x] 1.1 Define `CoreClient` interface (consumer-defined, testable)
  - [x] 1.2 Add fields to `LoginHandler` struct
  - [x] 1.3 Change `NewLoginHandler` to accept config struct
  - [x] 1.4 Update `main.go` call site
- [x] Task 2: Implement PostLogin handler (AC: 1, 2, 3, 4)
  - [x] 2.1 Add request/response types (`LoginRequest`, `LoginTokenResponse`)
  - [x] 2.2 Add `writeMatrixError` helper in matrix package
  - [x] 2.3 Add `mapSystemRole` helper
  - [x] 2.4 Add `generateDeviceID` helper (crypto/rand UUID v4)
  - [x] 2.5 Implement `PostLogin` method with full flow
- [x] Task 3: Register POST route in main.go (AC: 1)
  - [x] 3.1 Add `mux.HandleFunc("POST /_matrix/client/v3/login", loginHandler.PostLogin)`
- [x] Task 4: Unit tests (AC: 1, 2, 3, 4)
  - [x] 4.1 Test: valid token → 200 with correct response body
  - [x] 4.2 Test: invalid token → 403 M_FORBIDDEN
  - [x] 4.3 Test: expired token → 403 M_FORBIDDEN
  - [x] 4.4 Test: unsupported login type → 400 M_UNKNOWN
  - [x] 4.5 Test: malformed JSON → 400 M_NOT_JSON
  - [x] 4.6 Test: OIDC provider unavailable → 503 M_UNKNOWN
  - [x] 4.7 Confirm `make test-unit-go` passes

## Dev Notes

### Architecture — Key Design Decision

**Stateless Auth (NFR-S4):** The OIDC JWT IS the Matrix access token. The handler echoes the original JWT back as `access_token`. No session table write, no custom token minting. Every subsequent API request re-validates the JWT against the OIDC provider via JWTMiddleware (Story 2.4). This is the core auth architecture decision — do NOT create session storage.

**ADR G2:** No token forwarded to Elixir. User identity goes via gRPC metadata (`x-user-id`, `x-system-role`). The `ValidateToken` RPC receives only `display_name` + `email` for provisioning.

### Task 1: Extend LoginHandler

**File:** `gateway/internal/matrix/login.go`

The current `LoginHandler` has only `displayName string`. Extend it for POST dependencies.

**1.1 Define CoreClient interface** (top of `login.go`):
```go
type CoreClient interface {
    ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error)
}
```
Consumer-defined interface — `grpc.Client` already satisfies it. Follows existing pattern: `health.go` uses `coreState` interface for `grpc.Client`.

**1.2 Add fields to LoginHandler:**
```go
type LoginHandler struct {
    displayName   string
    provider      *auth.Provider
    coreClient    CoreClient
    serverName    string
    clientID      string
    roleClaimName string
}
```

**1.3 Change constructor to config struct** (6 params → struct):
```go
type LoginConfig struct {
    DisplayName   string
    Provider      *auth.Provider
    CoreClient    CoreClient
    ServerName    string
    ClientID      string
    RoleClaimName string
}

func NewLoginHandler(cfg LoginConfig) *LoginHandler {
    return &LoginHandler{
        displayName:   cfg.DisplayName,
        provider:      cfg.Provider,
        coreClient:    cfg.CoreClient,
        serverName:    cfg.ServerName,
        clientID:      cfg.ClientID,
        roleClaimName: cfg.RoleClaimName,
    }
}
```

**1.4 Update main.go** (line 130):
```go
loginHandler := matrix.NewLoginHandler(matrix.LoginConfig{
    DisplayName:   cfg.OIDCDisplayName,
    Provider:      oidcProvider,
    CoreClient:    coreClient,
    ServerName:    serverName,
    ClientID:      cfg.OIDCClientID,
    RoleClaimName: cfg.OIDCClaimRole,
})
```
Note: `serverName` is the variable from `db.InitServerConfig()` (line 46), NOT `cfg.ServerName`.

### Task 2: Implement PostLogin Handler

**File:** `gateway/internal/matrix/login.go`

**2.1 Request/Response types:**
```go
type LoginRequest struct {
    Type  string `json:"type"`
    Token string `json:"token"`
}

type LoginTokenResponse struct {
    AccessToken string `json:"access_token"`
    DeviceID    string `json:"device_id"`
    UserID      string `json:"user_id"`
    TokenType   string `json:"token_type"`
}
```

**2.2 Matrix error helper** (will be reused by all future Matrix handlers in this package):
```go
type matrixError struct {
    ErrCode string `json:"errcode"`
    Err     string `json:"error"`
}

func writeMatrixError(w http.ResponseWriter, status int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}
```
Same struct/function signature as `middleware/auth.go` — intentional duplication (unexported in both packages). Future refactoring into shared package is fine but NOT in scope.

**2.3 Role mapping** (same logic as `middleware.mapRole`):
```go
func mapSystemRole(rawClaim string) string {
    switch rawClaim {
    case "instance_admin", "compliance_officer":
        return rawClaim
    default:
        return "user"
    }
}
```

**2.4 Device ID generation** (no new dependency — use crypto/rand):
```go
func generateDeviceID() string {
    b := make([]byte, 16)
    _, _ = rand.Read(b)
    b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
    b[8] = (b[8] & 0x3f) | 0x80 // UUID variant 2
    return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
```
`gofrs/uuid` is an indirect dep but not worth promoting to direct for one call. Standard `crypto/rand` is sufficient and keeps the dependency graph clean.

**2.5 PostLogin method — complete flow:**

```go
func (h *LoginHandler) PostLogin(w http.ResponseWriter, r *http.Request) {
    // 1. Parse request body
    var req LoginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Request body is not valid JSON")
        return
    }

    // 2. Validate login type (AC: 4)
    if req.Type != "m.login.token" {
        writeMatrixError(w, http.StatusBadRequest, "M_UNKNOWN", "Unsupported login type")
        return
    }

    // 3. Check OIDC provider availability
    inner := h.provider.Inner()
    if inner == nil {
        writeMatrixError(w, http.StatusServiceUnavailable, "M_UNKNOWN", "Authentication service unavailable")
        return
    }

    // 4. Validate JWT (reuses same go-oidc verifier as JWTMiddleware in Story 2.4)
    verifier := inner.Verifier(&oidc.Config{ClientID: h.clientID})
    idToken, err := verifier.Verify(r.Context(), req.Token)
    if err != nil {
        // AC: 3 — invalid or expired → 403 M_FORBIDDEN
        writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid or expired token")
        return
    }

    // 5. Extract claims
    var allClaims map[string]interface{}
    if err := idToken.Claims(&allClaims); err != nil {
        writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid token claims")
        return
    }
    sub, _ := allClaims["sub"].(string)
    preferredUsername, _ := allClaims["preferred_username"].(string)
    email, _ := allClaims["email"].(string)
    rawRole, _ := allClaims[h.roleClaimName].(string)
    systemRole := mapSystemRole(rawRole)

    // 6. Format user ID and call ValidateToken gRPC (provisions user if new)
    userID := coregrpc.FormatUserID(sub, h.serverName)
    grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
    _, err = h.coreClient.ValidateToken(grpcCtx, &pb.ValidateTokenRequest{
        DisplayName: preferredUsername,
        Email:       email,
    })
    if err != nil {
        slog.Error("ValidateToken gRPC failed", "err", err, "user_id", userID)
        writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
        return
    }

    // 7. Return Matrix login response (AC: 1, 2)
    // access_token = original OIDC JWT (stateless — NFR-S4)
    resp := LoginTokenResponse{
        AccessToken: req.Token,
        DeviceID:    generateDeviceID(),
        UserID:      userID,
        TokenType:   "Bearer",
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}
```

**Required imports for login.go** (add to existing):
```go
import (
    "context"
    "crypto/rand"
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"

    "github.com/coreos/go-oidc/v3/oidc"
    "github.com/nebu/nebu/internal/auth"
    coregrpc "github.com/nebu/nebu/internal/grpc"
    pb "github.com/nebu/nebu/internal/grpc/pb"
)
```

### Task 3: Route Registration

**File:** `gateway/cmd/gateway/main.go`

Add after the existing GET route (line 131):
```go
mux.HandleFunc("POST /_matrix/client/v3/login", loginHandler.PostLogin)
```

**CRITICAL:** This route is NOT behind JWTMiddleware. The JWT arrives in the request body, not the Authorization header. This is the login endpoint — users aren't authenticated yet.

### Task 4: Unit Tests

**File:** `gateway/internal/matrix/login_test.go`

**Test infrastructure needed:**
- Test OIDC server (RSA key signing JWTs) — follow exact pattern from `middleware/auth_test.go`
- Mock CoreClient (implements `CoreClient` interface)
- Test helper to create signed JWTs with custom claims

**Mock CoreClient:**
```go
type mockCoreClient struct {
    validateResp *pb.ValidateTokenResponse
    validateErr  error
}

func (m *mockCoreClient) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
    return m.validateResp, m.validateErr
}
```

**Test OIDC server setup** — reuse the pattern from `middleware/auth_test.go` and `admin/auth_test.go`:
- Create RSA keypair
- Serve JWKS endpoint at `/.well-known/openid-configuration` and `/keys`
- Sign test JWTs with the RSA key
- Create `auth.NewProvider(ctx, server.URL)` pointing to the test server

**Test cases (table-driven — multiple code paths):**

| Test | Input | Expected | AC |
|------|-------|----------|-----|
| Valid token | `{"type": "m.login.token", "token": "<valid_jwt>"}` | 200, response has `access_token`, `device_id`, `user_id`, `token_type` | 1, 2 |
| Invalid token | `{"type": "m.login.token", "token": "garbage"}` | 403, `M_FORBIDDEN` | 3 |
| Expired token | `{"type": "m.login.token", "token": "<expired_jwt>"}` | 403, `M_FORBIDDEN` | 3 |
| Unsupported type | `{"type": "m.login.password", "token": "..."}` | 400, `M_UNKNOWN` | 4 |
| Malformed JSON | `not-json` | 400, `M_NOT_JSON` | — |
| Provider unavailable | Valid body but provider.Inner() == nil | 503, `M_UNKNOWN` | — |

**Assertions for the 200 OK case:**
- `access_token` equals the original JWT (stateless — echoed back)
- `device_id` is non-empty and looks like a UUID (contains dashes, 36 chars)
- `user_id` matches `@<sub>:<server_name>` format
- `token_type` is `"Bearer"`
- `Content-Type` is `application/json`

**Testing the OIDC server** — the test OIDC server needs:
1. A `/.well-known/openid-configuration` endpoint returning `{"issuer": "<url>", "jwks_uri": "<url>/keys", ...}`
2. A `/keys` endpoint returning the JWKS with the RSA public key
3. JWT signing with claims: `sub`, `preferred_username`, `email`, `nebu_role`, `aud` (= clientID), `iss` (= server URL), `exp`, `iat`

**Important:** Use `go-jose/v4` for JWT signing in tests (already a direct dependency). Follow the exact JWT creation pattern from `middleware/auth_test.go`.

**Update existing GetLogin test:** The `NewLoginHandler` signature changes (now takes `LoginConfig`). Update `TestGetLogin_ReturnsSSO` to use the new constructor. The test only needs `DisplayName` set — other fields can be zero-valued since `GetLogin` doesn't use them.

### Project Structure Changes

| File | Action | Reason |
|------|--------|--------|
| `gateway/internal/matrix/login.go` | MODIFY | Add CoreClient interface, extend LoginHandler, add PostLogin, helpers |
| `gateway/internal/matrix/login_test.go` | MODIFY | Update constructor call, add PostLogin tests with OIDC test server |
| `gateway/cmd/gateway/main.go` | MODIFY | Update NewLoginHandler call, register POST route |

**No new files.** No new dependencies. No go.mod changes. No Elixir changes. No database changes. No proto changes.

### Previous Story Intelligence

**From Story 2-17 (GET /login — this file's predecessor):**
- `LoginHandler` struct pattern established — extend, don't replace
- `GetLogin` returns static JSON — `PostLogin` does real work but same struct
- Test pattern: `httptest.NewRequest` + `httptest.NewRecorder`
- `Icon *string` for null JSON — pointer-for-null pattern works
- Route registered directly on `mux`, NOT behind auth middleware

**From Story 2-4 (OIDC JWT Validation Middleware):**
- `middleware/auth.go` lines 51–101: Full JWT validation flow using `go-oidc` verifier
- Claims extraction: `idToken.Claims(&allClaims)` then type-assert each field
- Role mapping: `mapRole()` (unexported) — same logic needed in matrix package
- Provider nil check: `provider.Inner() == nil` → error response
- Expired token detection: `errors.As(err, &oidc.TokenExpiredError{})` — for PostLogin, we collapse all validation errors to 403 M_FORBIDDEN (AC doesn't distinguish expired vs invalid)

**From Story 2-14 (ValidateToken gRPC Handler):**
- Proto: `ValidateTokenRequest{DisplayName, Email}` — token is NOT in the request (reserved field 1)
- Identity via metadata: `coregrpc.WithUserMetadata(ctx, userID, systemRole)`
- Elixir trusts Go's validation — no re-validation on core side

**From Story 2-13 (User Provisioning):**
- `ValidateToken` gRPC call triggers auto-provisioning for new users
- Includes keypair generation (Ed25519 + X25519) and PII encryption
- Existing users: returns decrypted display_name, no re-provisioning

**From Story 2-12 (User Record DB Write):**
- First-time user creation on ValidateToken call
- user_id format: `@{sub}:{server_name}`

### Git Intelligence

Recent commits show consistent pattern:
- Commit message: `Story 2-N`
- One commit per story
- All tests pass before commit (`make test-unit-go`)
- Files changed: typically 2-4 files per story in gateway/

### Architecture Enforcement Rules (Applicable)

1. **Rule #2:** Auth-Token nie an Elixir weitergeben — only `user_id` + `system_role` via gRPC metadata ✓ (PostLogin sends claims via metadata, not the JWT)
2. **Rule #3:** Matrix-Endpunkte geben Matrix-Format zurück — error responses use `{"errcode": "...", "error": "..."}` ✓
3. **Rule #4:** Env-Variablen: `NEBU_{COMPONENT}_{KEY}` — no new env vars needed ✓
4. **Rule #5:** Go table-driven tests for multiple inputs ✓ (PostLogin has 6+ code paths)
5. **Rule #12:** Secrets via `NEBU_*_FILE` — no new secrets needed ✓

### NOT In Scope

- `POST /_matrix/client/v3/logout` → Story 2-19
- SSO redirect flow (`/_matrix/client/v3/login/sso/redirect/{idpId}`) → not in MVP
- Device management / session persistence → Epic 4
- Deactivated user rejection (`is_active == false`) → not in AC, future enhancement
- Rate limiting on login endpoint → Epic 3 middleware

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.18 — OIDC Token Exchange acceptance criteria]
- [Source: _bmad-output/planning-artifacts/architecture.md — Auth-Token-Flow: Go validates, Elixir trusts, gRPC metadata]
- [Source: _bmad-output/planning-artifacts/architecture.md — Matrix Error Format: {"errcode": "...", "error": "..."}]
- [Source: _bmad-output/planning-artifacts/architecture.md — NFR-S4: stateless JWT validation per request]
- [Source: _bmad-output/planning-artifacts/architecture.md — ADR G2: no token forwarding to Elixir]
- [Source: gateway/internal/middleware/auth.go — JWT validation pattern with go-oidc verifier]
- [Source: gateway/internal/grpc/metadata.go — FormatUserID() and WithUserMetadata() helpers]
- [Source: gateway/internal/grpc/client.go:97-99 — ValidateToken wrapper]
- [Source: proto/core.proto:102-113 — ValidateTokenRequest/Response with reserved token field]
- [Source: gateway/internal/matrix/login.go — Current LoginHandler to extend]
- [Source: gateway/cmd/gateway/main.go:130 — Current NewLoginHandler call site]
- [Source: gateway/internal/config/config.go — OIDCClientID, OIDCClaimRole, ServerName fields]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

### Completion Notes List

- All 4 tasks completed: LoginHandler extended with CoreClient interface, PostLogin handler implemented with full OIDC token exchange flow, POST route registered in main.go, 7 table-driven unit tests (valid token, invalid token, expired token, unsupported type, malformed JSON, provider unavailable, gRPC failure) all passing
- Stateless auth design (NFR-S4): OIDC JWT echoed back as access_token — no session storage
- ADR G2 enforced: token NOT forwarded to Elixir, only user_id + system_role via gRPC metadata
- Matrix error format used for all error responses (errcode + error)
- Existing GetLogin test updated for new LoginConfig constructor — no regression

### Change Log

- 2026-03-30: Implemented POST /_matrix/client/v3/login — OIDC token exchange with stateless auth, user provisioning via ValidateToken gRPC, table-driven tests

### File List

- gateway/internal/matrix/login.go (MODIFIED) — CoreClient interface, LoginConfig, LoginHandler extension, PostLogin handler, writeMatrixError, mapSystemRole, generateDeviceID
- gateway/internal/matrix/login_test.go (MODIFIED) — Updated GetLogin test constructor, added TestPostLogin with 7 table-driven test cases, mock CoreClient, OIDC test server
- gateway/cmd/gateway/main.go (MODIFIED) — Updated NewLoginHandler to LoginConfig, registered POST /_matrix/client/v3/login route

# Story 2.5: OIDC Claim-to-Role Mapping

Status: done

## Story

As an operator,
I want the OIDC `nebu_role` claim to be mapped to Nebu system roles,
so that role-based access control works correctly for admins and compliance officers.

## Acceptance Criteria

1. **Given** a JWT with `"nebu_role": "instance_admin"`, **When** the claim is mapped, **Then** the resulting `system_role` is `"instance_admin"`

2. **Given** a JWT with `"nebu_role": "compliance_officer"`, **When** the claim is mapped, **Then** the resulting `system_role` is `"compliance_officer"`

3. **Given** a JWT with any other `nebu_role` value or no `nebu_role` claim, **When** the claim is mapped, **Then** the resulting `system_role` defaults to `"user"`

4. **Given** `NEBU_OIDC_CLAIM_ROLE` env var set to a custom claim name (e.g., `"roles"`), **When** the gateway starts, **Then** it uses that custom claim name instead of the default `"nebu_role"`

5. **Given** a unit test, **When** it covers all three role mapping cases, **Then** it passes with `go test -race ./...`

## Tasks / Subtasks

- [x] Add `OIDCClaimRole` to `gateway/internal/config/config.go` (AC: #4)
  - [x] Add `OIDCClaimRole string // NEBU_OIDC_CLAIM_ROLE (default: "nebu_role")` field to `Config` struct
  - [x] Add `OIDCClaimRole: getEnvOrDefault("NEBU_OIDC_CLAIM_ROLE", "nebu_role")` in `Load()`
  - [x] Add test in `gateway/internal/config/config_test.go`: verify `NEBU_OIDC_CLAIM_ROLE` sets field and default is `"nebu_role"`

- [x] Add role mapping to `gateway/internal/middleware/auth.go` (AC: #1–#3, #5)
  - [x] Add exported constant `ContextKeySystemRole contextKey = "system_role"`
  - [x] Add unexported `mapRole(rawClaim string) string` function: `"instance_admin"` → `"instance_admin"`, `"compliance_officer"` → `"compliance_officer"`, anything else (incl. empty) → `"user"`
  - [x] Update `JWTMiddleware` signature: add `claimName string` as third parameter — `func JWTMiddleware(provider *auth.Provider, clientID string, claimName string) func(http.Handler) http.Handler`
  - [x] Replace fixed claims struct with `map[string]interface{}` extraction: `idToken.Claims(&allClaims)`
  - [x] Extract `sub`, `preferred_username`, `email` from map with type assertion: `val, _ := allClaims["sub"].(string)`
  - [x] Extract role using `claimName` key: `rawRole, _ := allClaims[claimName].(string)`, then `mapRole(rawRole)` → `systemRole`
  - [x] Set `ContextKeySystemRole` in context with mapped system_role value
  - [x] Keep `ContextKeyNebuRole` in context set to the raw claim value (for backward compatibility with any future story consuming the raw value)

- [x] Create `gateway/internal/middleware/role_test.go` (AC: #1–#3, #5)
  - [x] `TestMapRole_InstanceAdmin`: `mapRole("instance_admin")` → `"instance_admin"`
  - [x] `TestMapRole_ComplianceOfficer`: `mapRole("compliance_officer")` → `"compliance_officer"`
  - [x] `TestMapRole_UnknownValue`: `mapRole("superuser")` → `"user"`
  - [x] `TestMapRole_EmptyString`: `mapRole("")` → `"user"`

- [x] Add integration test for custom claim name in `gateway/internal/middleware/auth_test.go` (AC: #4, #5)
  - [x] `TestJWTMiddleware_CustomClaimName`: sign JWT with `"roles": "compliance_officer"` (no `nebu_role` key), create middleware with `claimName = "roles"`, verify `ContextKeySystemRole` = `"compliance_officer"` in context
  - [x] `TestJWTMiddleware_SystemRoleInContext`: sign JWT with `"nebu_role": "instance_admin"`, verify `ContextKeySystemRole` = `"instance_admin"` and `ContextKeyNebuRole` = `"instance_admin"`
  - [x] Run `go test -race ./internal/middleware/...` passes

## Dev Notes

### What Changed vs. Story 2.4

Story 2.4 created `JWTMiddleware` with a **fixed claims struct** that always reads `nebu_role` as the role claim name. Story 2.5 makes the claim name **configurable** and adds the mapping layer:

| Story 2.4 | Story 2.5 change |
|---|---|
| Fixed `NebuRole string \`json:"nebu_role"\`` | Dynamic: `allClaims[claimName].(string)` |
| Puts raw claim in `ContextKeyNebuRole` only | Also puts mapped role in `ContextKeySystemRole` |
| `JWTMiddleware(provider, clientID)` | `JWTMiddleware(provider, clientID, claimName)` |

**No wiring into `main.go` needed** — Matrix routes don't exist yet. `JWTMiddleware` will be wired in Epic 4.

### Files to Modify / Create

```
gateway/
  internal/
    config/
      config.go       ← MODIFY: add OIDCClaimRole field + Load() entry
      config_test.go  ← MODIFY: add NEBU_OIDC_CLAIM_ROLE test case
    middleware/
      auth.go         ← MODIFY: update JWTMiddleware + add mapRole + ContextKeySystemRole
      auth_test.go    ← MODIFY: add TestJWTMiddleware_CustomClaimName, TestJWTMiddleware_SystemRoleInContext
      role_test.go    ← CREATE: TestMapRole_* unit tests
```

**DO NOT TOUCH:** `psk.go`, `psk_test.go`, `oidc.go`, `oidc_test.go`, `main.go`, any Elixir files, migrations.

### Role Mapping Function

```go
// mapRole converts a raw OIDC claim value to a canonical Nebu system role.
// Only "instance_admin" and "compliance_officer" are privileged roles.
// All other values (including empty string) map to "user".
func mapRole(rawClaim string) string {
    switch rawClaim {
    case "instance_admin", "compliance_officer":
        return rawClaim
    default:
        return "user"
    }
}
```

### Updated JWTMiddleware Signature and Claims Extraction

```go
// JWTMiddleware validates OIDC JWT tokens. On success, populates context with
// sub, preferred_username, email, system_role (mapped), and the raw role claim value.
func JWTMiddleware(provider *auth.Provider, clientID string, claimName string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ... existing token extraction and verification logic unchanged ...

            // Replace fixed claims struct with dynamic map:
            var allClaims map[string]interface{}
            if err := idToken.Claims(&allClaims); err != nil {
                log.Printf("failed to extract JWT claims: %v", err)
                writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
                return
            }

            sub, _ := allClaims["sub"].(string)
            preferredUsername, _ := allClaims["preferred_username"].(string)
            email, _ := allClaims["email"].(string)
            rawRole, _ := allClaims[claimName].(string)
            systemRole := mapRole(rawRole)

            ctx := r.Context()
            ctx = context.WithValue(ctx, ContextKeySub, sub)
            ctx = context.WithValue(ctx, ContextKeyPreferredUsername, preferredUsername)
            ctx = context.WithValue(ctx, ContextKeyEmail, email)
            ctx = context.WithValue(ctx, ContextKeyNebuRole, rawRole)       // raw claim value
            ctx = context.WithValue(ctx, ContextKeySystemRole, systemRole)  // mapped canonical role
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

### Context Keys After This Story

```go
const (
    ContextKeySub               contextKey = "sub"               // JWT "sub" claim (stable user UUID)
    ContextKeyPreferredUsername contextKey = "preferred_username" // display name
    ContextKeyEmail             contextKey = "email"             // sensitive PII — never log
    ContextKeyNebuRole          contextKey = "nebu_role"         // raw role claim value
    ContextKeySystemRole        contextKey = "system_role"       // mapped: "user"|"instance_admin"|"compliance_officer"
)
```

**Story 2.7 will read `ContextKeySub` and `ContextKeySystemRole`** to build gRPC metadata:
- `"x-user-id": "@<sub>:<server_name>"`
- `"x-system-role": "<systemRole>"`

### Config Change

```go
// Config struct — add new field:
OIDCClaimRole string // NEBU_OIDC_CLAIM_ROLE (default: "nebu_role")

// Load() — add new entry:
OIDCClaimRole: getEnvOrDefault("NEBU_OIDC_CLAIM_ROLE", "nebu_role"),
```

### Test Implementation for Custom Claim Name (in auth_test.go)

Use the existing `setupOIDCServer` and `signJWT` helpers from Story 2.4. For the custom claim name test, modify `signJWT` call to set `"roles"` key instead of `"nebu_role"` — but `signJWT` uses `go-jose/v4` Claims map:

```go
func TestJWTMiddleware_CustomClaimName(t *testing.T) {
    srv, key := setupOIDCServer(t)
    provider := auth.NewProvider(context.Background(), srv.URL)

    // Sign JWT with custom claim name "roles" instead of "nebu_role"
    signer, _ := jose.NewSigner(
        jose.SigningKey{Algorithm: jose.RS256, Key: key},
        (&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
    )
    cl := josejwt.Claims{
        Subject:  "user-456",
        Issuer:   srv.URL,
        Audience: josejwt.Audience{"nebu-gateway"},
        Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
        IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
    }
    extra := map[string]any{
        "preferred_username": "custom.user",
        "email":              "custom@example.com",
        "roles":              "compliance_officer", // custom claim name, no "nebu_role"
    }
    rawToken, _ := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()

    var capturedRole string
    handler := middleware.JWTMiddleware(provider, "nebu-gateway", "roles")(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            capturedRole, _ = r.Context().Value(middleware.ContextKeySystemRole).(string)
            w.WriteHeader(http.StatusOK)
        }),
    )

    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("Authorization", "Bearer "+rawToken)
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", rr.Code)
    }
    if capturedRole != "compliance_officer" {
        t.Errorf("expected system_role=compliance_officer, got %q", capturedRole)
    }
}
```

### mapRole is Unexported — Test via Package-Level Test

`mapRole` is unexported. Create `role_test.go` in **`package middleware`** (not `middleware_test`) to access it directly:

```go
package middleware // NOT middleware_test — needs access to unexported mapRole

import "testing"

func TestMapRole_InstanceAdmin(t *testing.T) {
    if got := mapRole("instance_admin"); got != "instance_admin" {
        t.Errorf("mapRole(instance_admin) = %q, want instance_admin", got)
    }
}
// ... other cases
```

### System Role Values (Canonical)

| Raw OIDC claim value | Mapped system_role |
|---|---|
| `"instance_admin"` | `"instance_admin"` |
| `"compliance_officer"` | `"compliance_officer"` |
| any other string | `"user"` |
| `""` (absent claim) | `"user"` |

These values are authoritative across the system — gRPC metadata (`x-system-role`), DB (`users.system_role`), Elixir permissions module.

### Dependencies — No go.mod Changes Required

All dependencies already present from Story 2.4:
- `github.com/coreos/go-oidc/v3` — already direct dep
- `github.com/go-jose/go-jose/v4` — already indirect dep (for tests)

### Build & Test

```bash
make test-unit-go    # go test -race ./... in container — must pass with 0 failures
```

### Cross-Story Context

- **Story 2.4** created `JWTMiddleware` with fixed claims struct and raw `ContextKeyNebuRole`. This story MODIFIES that function (signature change, dynamic extraction) — no other story currently calls it, so no regression risk.
- **Story 2.6** (Admin UI PKCE flow) adds `golang.org/x/oauth2` to `go.mod`. This story does NOT add any dependencies.
- **Story 2.7** (gRPC metadata) will read `ContextKeySub` and `ContextKeySystemRole` from context. The `ContextKeySystemRole` constant added in this story MUST remain exported and stable.
- **Story 6.3** extends this middleware with `role_overrides` DB lookup (ETS-cached) — will call `mapRole`-equivalent logic.
- **Bootstrap Mode** (Story 2.15–2.16): The first OIDC login overrides `system_role` to `"instance_admin"` regardless of claim — that logic is in `gateway/internal/auth/bootstrap.go` (not this story). `mapRole` handles only the claim-based mapping.

### Project Structure Notes

- `gateway/internal/config/config.go` follows pattern: struct field + `Load()` entry + `getEnvOrDefault` for env vars with defaults
- `gateway/internal/middleware/` package: `auth.go` (main), `psk.go` (PSK — do NOT modify), `role_test.go` (new)
- Test files use `package middleware_test` for black-box tests EXCEPT `role_test.go` which uses `package middleware` to access unexported `mapRole`
- No `t.Cleanup(os.Unsetenv(...))` needed — use `t.Setenv()` for all env var tests (auto-cleanup)

### References

- [Source: epics.md#Story-2.5] Authoritative ACs — all 5 acceptance criteria
- [Source: architecture.md#Auth-Token-Flow] system_role values: instance_admin | compliance_officer | user
- [Source: architecture.md#V4-Keycloak-OIDC-Claims-Mapping] `nebu_role` → `system_role` mapping table
- [Source: gateway/internal/middleware/auth.go] Current JWTMiddleware — to be modified
- [Source: gateway/internal/config/config.go] Config struct and Load() pattern
- [Source: _bmad-output/implementation-artifacts/2-4-oidc-jwt-token-validation-middleware.md#Cross-Story-Context] Story 2.5 adds NEBU_OIDC_CLAIM_ROLE + mapping layer
- [Source: _bmad-output/implementation-artifacts/2-4-oidc-jwt-token-validation-middleware.md#Test-Implementation-Guide] signJWT helper pattern for tests

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- Added `OIDCClaimRole` field to `Config` struct with default `"nebu_role"` via `getEnvOrDefault`
- Added `mapRole()` unexported function mapping raw OIDC claim to canonical system role (instance_admin | compliance_officer | user)
- Added `ContextKeySystemRole` exported constant to middleware context keys
- Updated `JWTMiddleware` signature to accept `claimName string` as third parameter
- Replaced fixed claims struct with `map[string]interface{}` dynamic extraction
- Both `ContextKeyNebuRole` (raw) and `ContextKeySystemRole` (mapped) set in context
- All existing tests updated to pass `"nebu_role"` as third arg; 2 new middleware tests + 4 new unit tests for mapRole added
- `go test -race ./...` passes 100% with 0 failures

### File List

- `gateway/internal/config/config.go`
- `gateway/internal/config/config_test.go`
- `gateway/internal/middleware/auth.go`
- `gateway/internal/middleware/auth_test.go`
- `gateway/internal/middleware/role_test.go` (new)

### Change Log

- 2026-03-27: Implemented OIDC claim-to-role mapping — configurable claim name via `NEBU_OIDC_CLAIM_ROLE`, `mapRole()` function, `ContextKeySystemRole` in middleware context

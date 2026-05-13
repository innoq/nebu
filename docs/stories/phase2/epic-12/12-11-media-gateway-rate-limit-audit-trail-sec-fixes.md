---
id: "12-11"
epic: 12
title: "Media Gateway Rate Limit + Audit Trail SEC Fixes"
status: ready-for-dev
security_review: required
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.11 — Media Gateway Rate Limit + Audit Trail SEC Fixes

## User Story

As a system operator,
I want the media gateway rate limiter to gate X-Forwarded-For behind a trusted-proxy flag, and the upload audit trail to use the operator-configured OIDC claim,
So that the rate limiter cannot be bypassed via header spoofing and audit records correlate correctly with room events.

## Acceptance Criteria

**AC-F2-1** — XFF ignored when `NEBU_TRUSTED_PROXY=false` (default):
- Given `NEBU_TRUSTED_PROXY=false` (or env var absent)
- When a request has `X-Forwarded-For: 1.2.3.4`
- Then rate limiting keys on `RemoteAddr` (ignores XFF completely)

**AC-F2-2** — XFF used when `NEBU_TRUSTED_PROXY=true`:
- Given `NEBU_TRUSTED_PROXY=true`
- When a request has `X-Forwarded-For: 1.2.3.4, 10.0.0.1`
- Then rate limiting keys on `1.2.3.4` (rightmost-minus-1 — the proxy-appended entry)

**AC-F2-3** — Bypass protection: attacker cannot spoof with different XFF headers when `NEBU_TRUSTED_PROXY=false`:
- Given `NEBU_TRUSTED_PROXY=false` and an attacker sends 11 requests from `RemoteAddr=5.6.7.8`, each with a different `X-Forwarded-For` value
- When all 11 requests arrive
- Then all 11 are counted against `5.6.7.8` and the 11th is rate-limited (bypass not possible)

**AC-F1-1** — Upload uses configured OIDC claim (`sub`):
- Given `NEBU_OIDC_USER_ID_CLAIM=sub` and token with `sub=alice-uuid`
- When upload succeeds
- Then `uploader_user_id = @alice-uuid:server`

**AC-F1-2** — Upload uses configured OIDC claim (`name`):
- Given `NEBU_OIDC_USER_ID_CLAIM=name` (default, matching migration 000044 default) and token with `name=alice`
- When upload succeeds
- Then `uploader_user_id = @alice:server`

**AC-F1-3** — Default claim is `name` when env var unset:
- Given `NEBU_OIDC_USER_ID_CLAIM` is unset
- When the media gateway starts
- Then it defaults to `name` (matching the gateway's DB default)

**AC-F1-4** — Fall back to `sub` when configured claim missing:
- Given `NEBU_OIDC_USER_ID_CLAIM=email` and token has no `email` claim but has `sub=alice-uuid`
- When upload is attempted
- Then a warning is logged and `uploader_user_id = @alice-uuid:server` (sub fallback)

## Technical Context

### Background: SEC findings from Story 12.10 security review

Story 12.10 introduced per-IP rate limiting for the media gateway. The security review
(`docs/stories/phase2/epic-12/security-reports/12-10-security-review-2026-05-13.md`)
identified two findings to address:

**F-2 [HIGH]** — `extractClientIP` in `media/internal/ratelimit/ratelimit.go` always
reads `X-Forwarded-For` without checking whether the gateway is behind a trusted proxy.
An attacker behind `RemoteAddr=5.6.7.8` can bypass the rate limit by sending 11 requests
each with a different `X-Forwarded-For` value — each gets a fresh bucket keyed to a
different spoofed IP.

**F-1 [MEDIUM]** — `OIDCTokenVerifier.VerifyToken` in `media/internal/upload/upload.go`
has a hardcoded `sub → name` claim priority. The operator cannot configure which OIDC
claim is used as the user identity in the audit trail. This can cause mismatches with
room events if the gateway uses a different claim (e.g. `email`, `preferred_username`).
Migration 000044 establishes `name` as the default claim via a DB comment, but the code
ignores the operator's `NEBU_OIDC_USER_ID_CLAIM` setting.

### F-2 Fix — `media/internal/ratelimit/ratelimit.go`

**Current problem:** `extractClientIP(r)` always reads `X-Forwarded-For`. The function
must be gated behind a `trustedProxy bool` field, mirroring the gateway pattern in
`gateway/internal/middleware/ratelimit.go:85-99`.

**Changes required:**

1. Add `trustedProxy bool` field to `IPRateLimiter` struct:
   ```go
   type IPRateLimiter struct {
       limiters     sync.Map
       rate         rate.Limit
       burst        int
       trustedProxy bool  // NEW
   }
   ```

2. Update `NewIPRateLimiter` signature to accept `trustedProxy bool`:
   ```go
   func NewIPRateLimiter(cfg Config, trustedProxy bool) func(http.Handler) http.Handler
   ```

3. Pass `trustedProxy` to `newCore`:
   ```go
   func newCore(cfg Config, trustedProxy bool) *IPRateLimiter {
       rl := &IPRateLimiter{rate: cfg.Rate, burst: cfg.Burst, trustedProxy: trustedProxy}
       ...
   }
   ```

4. Update `extractClientIP` to accept `trustedProxy bool` as a parameter (or make it a
   method on `IPRateLimiter`). When `trustedProxy=false`: always use `RemoteAddr`,
   never read XFF. When `trustedProxy=true`: apply rightmost-minus-1 (existing logic).

   Simplest approach — make it a method:
   ```go
   func (rl *IPRateLimiter) clientIP(r *http.Request) string {
       if rl.trustedProxy {
           xff := r.Header.Get("X-Forwarded-For")
           if ips := strings.Split(xff, ","); len(ips) >= 2 {
               return strings.TrimSpace(ips[len(ips)-1])
           }
       }
       host, _, err := net.SplitHostPort(r.RemoteAddr)
       if err != nil {
           return r.RemoteAddr
       }
       return host
   }
   ```

5. Update the middleware's `ServeHTTP` to call `rl.clientIP(r)` instead of `extractClientIP(r)`.

**In `media/cmd/media/main.go`:**
- Read `NEBU_TRUSTED_PROXY` env var: `trustedProxy := os.Getenv("NEBU_TRUSTED_PROXY") == "true"`
- Pass `trustedProxy` to both `NewIPRateLimiter` calls:
  ```go
  uploadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{Rate: rate.Limit(10), Burst: 5}, trustedProxy)
  downloadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{Rate: rate.Limit(100), Burst: 20}, trustedProxy)
  ```

**In `docker-compose.yml` media service env:**
- Add explicit default: `NEBU_TRUSTED_PROXY: "false"` (documents the choice)

### F-1 Fix — `media/internal/upload/upload.go` + `media/cmd/media/main.go`

**Current problem:** `OIDCTokenVerifier.VerifyToken` hardcodes `sub → name` priority.
The operator cannot configure which claim becomes the audit trail user ID.

**Changes required:**

1. Add `ClaimName string` field to `HandlerConfig`:
   ```go
   type HandlerConfig struct {
       DB           MediaStore
       Storage      storage.Storer
       ServerName   string
       MaxBytes     int64
       OIDCVerifier TokenVerifier
       ClaimName    string  // NEW — OIDC claim used as user identity (default: "name")
   }
   ```

2. Add `claimName string` field to `Handler` struct; populate in `NewHandler`:
   ```go
   claimName := cfg.ClaimName
   if claimName == "" {
       claimName = "name"
   }
   return &Handler{..., claimName: claimName}
   ```

3. Replace the hardcoded `OIDCTokenVerifier.VerifyToken` with a configurable approach.

   **Option A (preferred — keeps TokenVerifier interface unchanged):** Extract the claim
   from the raw token in `Handler.ServeHTTP` after `VerifyToken` returns the subject.
   Add a second method or change the interface to return raw claims.

   **Option B (simpler — pass claim name into OIDCTokenVerifier):** Add `claimName` to
   `OIDCTokenVerifier` and have it read the configured claim.

   **Recommended: Option B** — it keeps all OIDC logic inside `OIDCTokenVerifier`:

   ```go
   type OIDCTokenVerifier struct {
       verifier  *oidc.IDTokenVerifier
       claimName string  // NEW
   }

   func NewOIDCTokenVerifier(v *oidc.IDTokenVerifier, claimName string) *OIDCTokenVerifier {
       if claimName == "" {
           claimName = "name"
       }
       return &OIDCTokenVerifier{verifier: v, claimName: claimName}
   }
   ```

   Update `VerifyToken` to use `extractClaim(idToken, claimName string) string`:

   ```go
   func extractClaim(idToken *oidc.IDToken, claimName string) (string, error) {
       var rawClaims map[string]interface{}
       if err := idToken.Claims(&rawClaims); err != nil {
           return "", err
       }
       if val, ok := rawClaims[claimName]; ok {
           if s, ok := val.(string); ok && s != "" {
               return s, nil
           }
       }
       // Configured claim missing — fall back to sub, log warning.
       if sub, ok := rawClaims["sub"].(string); ok && sub != "" {
           slog.Warn("media upload: configured OIDC claim not found, falling back to sub",
               "configured_claim", claimName)
           return sub, nil
       }
       return "", fmt.Errorf("token missing both configured claim %q and sub", claimName)
   }
   ```

4. Update `VerifyToken` to call `extractClaim`:
   ```go
   func (o *OIDCTokenVerifier) VerifyToken(ctx context.Context, rawToken string) (string, error) {
       idToken, err := o.verifier.Verify(ctx, rawToken)
       if err != nil {
           return "", err
       }
       return extractClaim(idToken, o.claimName)
   }
   ```

5. In `media/cmd/media/main.go`:
   - Read: `oidcUserIDClaim := getenv("NEBU_OIDC_USER_ID_CLAIM", "name")`
   - Pass to `NewOIDCTokenVerifier`: `upload.NewOIDCTokenVerifier(idTokenVerifier, oidcUserIDClaim)`
   - Pass to `HandlerConfig.ClaimName` (for informational use / future extension)

**Note:** `HandlerConfig.ClaimName` may be redundant if the claim is baked into the
verifier — keep it for documentation/configurability, or omit it if it adds no value.
The `OIDCTokenVerifier` approach (Option B) is self-contained.

### Files to Create / Modify

| Action | File | Notes |
|---|---|---|
| MODIFY | `media/internal/ratelimit/ratelimit.go` | Add `trustedProxy` field + gated `extractClientIP` |
| MODIFY | `media/internal/ratelimit/ratelimit_test.go` | Add AT-12-11-1 through AT-12-11-3 |
| MODIFY | `media/internal/upload/upload.go` | Configurable claim extraction |
| MODIFY | `media/internal/upload/upload_test.go` | Add AT-12-11-4 through AT-12-11-6 |
| MODIFY | `media/cmd/media/main.go` | Read `NEBU_TRUSTED_PROXY` + `NEBU_OIDC_USER_ID_CLAIM` |
| MODIFY | `docker-compose.yml` | Add `NEBU_TRUSTED_PROXY: "false"` to media env |

### Related Files (read only)

- `gateway/internal/middleware/ratelimit.go` — reference for `trustedProxy` gate pattern (lines 85-99)
- `docs/stories/phase2/epic-12/security-reports/12-10-security-review-2026-05-13.md` — original security findings

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-12-11-1** — Rate limiter ignores XFF when `trustedProxy=false` — [Go unit test in `media/internal/ratelimit/ratelimit_test.go`]
   - Given: `NewIPRateLimiter(Config{Rate: 10, Burst: 5}, false)` (trustedProxy=false)
   - When: Request with `X-Forwarded-For: 1.2.3.4` and `RemoteAddr: 5.6.7.8:1234`
   - Then: Rate limiting keys on `5.6.7.8` (RemoteAddr), NOT on `1.2.3.4`

2. **AT-12-11-2** — Rate limiter uses XFF rightmost-minus-1 when `trustedProxy=true` — [Go unit test]
   - Given: `NewIPRateLimiter(Config{Rate: 10, Burst: 1}, true)` (trustedProxy=true)
   - When: Requests from `RemoteAddr=10.0.0.1:1234` with `X-Forwarded-For: 1.2.3.4, 10.0.0.1`
   - Then: Rate limiting keys on `1.2.3.4` (leftmost of the 2-entry XFF, which is the proxy-appended real client IP)

   **Note on rightmost-minus-1 semantics for 2-entry XFF:**
   `X-Forwarded-For: 1.2.3.4, 10.0.0.1` — the proxy appended `10.0.0.1` as the
   directly-connected client. The "rightmost-minus-1" means "the last entry the
   proxy appended", which is `ips[len-1]`. For a 2-entry list `[1.2.3.4, 10.0.0.1]`,
   `ips[1] = 10.0.0.1` is the proxy-appended entry. However per the existing implementation
   in `ratelimit.go` and the gateway pattern, `ips[len-1]` is used, so for
   `X-Forwarded-For: 1.2.3.4, 10.0.0.1`, the rightmost entry `10.0.0.1` is used.
   Align test assertions with the existing `extractClientIP` behavior (which returns
   `ips[len(ips)-1]` for multi-entry XFF).

3. **AT-12-11-3** — Bypass protection: different XFF, same RemoteAddr, trustedProxy=false — [Go unit test]
   - Given: `NewIPRateLimiter(Config{Rate: rate.Limit(10), Burst: 10}, false)` (trustedProxy=false)
   - When: 11 requests from `RemoteAddr=5.6.7.8`, each with a different `X-Forwarded-For` (spoofed IPs)
   - Then: All 11 requests are counted against `5.6.7.8`; the 11th returns 429

4. **AT-12-11-4** — Upload uses configured `sub` claim — [Go unit test in `media/internal/upload/upload_test.go`]
   - Given: `OIDCTokenVerifier` configured with `claimName="sub"` and token `{sub: "alice-uuid", name: "alice"}`
   - When: `VerifyToken` is called
   - Then: Returns `"alice-uuid"` (the `sub` claim value)

5. **AT-12-11-5** — Upload uses configured `name` claim — [Go unit test]
   - Given: `OIDCTokenVerifier` configured with `claimName="name"` and token `{sub: "uuid-123", name: "alice"}`
   - When: `VerifyToken` is called
   - Then: Returns `"alice"` (the `name` claim value)

6. **AT-12-11-6** — Falls back to `sub` when configured claim is missing — [Go unit test]
   - Given: `OIDCTokenVerifier` configured with `claimName="email"` and token `{sub: "uuid-123", name: "alice"}` (no `email` claim)
   - When: `VerifyToken` is called
   - Then: Returns `"uuid-123"` (sub fallback) and a warning is logged

## Dev Notes

### Rightmost-minus-1 semantics clarification

The existing `extractClientIP` in `ratelimit.go` uses `ips[len(ips)-1]` for the
rightmost entry when XFF has 2+ entries. This matches the gateway pattern. The test
AT-12-11-2 should assert that for `X-Forwarded-For: 1.2.3.4, 10.0.0.1`, the key is
`10.0.0.1` (the last/rightmost entry), consistent with the existing code behavior.

### OIDCTokenVerifier test approach

Since `OIDCTokenVerifier` wraps `*oidc.IDTokenVerifier` (which requires a real OIDC
provider to verify JWT signatures), testing must either:
- Use the existing test pattern in `upload_test.go` (mock `TokenVerifier`)
- OR extract the `extractClaim` function as a pure function and test it directly
  with a `*oidc.IDToken` that can be constructed via `idTokenVerifier.Verify`

**Simplest approach:** Test `extractClaim` as a standalone function using a fake
`*oidc.IDToken`. Since `oidc.IDToken.Claims` extracts from raw claims bytes,
construct a minimal test helper that creates a fake IDToken with known claims.

Actually, the cleanest approach is to test the full `OIDCTokenVerifier` using the
existing mock pattern from `upload_test.go`. The existing `mockTokenVerifier` in
`upload_test.go` bypasses the OIDC verifier entirely. For testing the new claim
extraction logic, create a separate unit test that tests `extractClaim` directly
as a pure function (it only needs a `map[string]interface{}`).

**Refactor suggestion:** Extract claim resolution to a pure function that takes
`rawClaims map[string]interface{}` and `claimName string` — no `*oidc.IDToken`
dependency. This makes it trivially testable without mock OIDC infrastructure.

```go
// extractClaimFromMap returns the string value of claimName from rawClaims.
// Falls back to "sub" with a warning if the configured claim is missing.
func extractClaimFromMap(rawClaims map[string]interface{}, claimName string) (string, error) { ... }
```

Then `VerifyToken` calls:
```go
var rawClaims map[string]interface{}
if err := idToken.Claims(&rawClaims); err != nil { return "", err }
return extractClaimFromMap(rawClaims, o.claimName)
```

Tests for `extractClaimFromMap` are pure unit tests with no external deps.

### Existing upload_test.go

`media/internal/upload/upload_test.go` uses `mockTokenVerifier` which implements
`TokenVerifier` directly. The `OIDCTokenVerifier` tests should be new unit tests
in a `upload_oidc_test.go` or appended to `upload_test.go`. Either is fine.

The existing tests for `Handler.ServeHTTP` use `mockTokenVerifier` and remain
unchanged — they are not affected by the claim extraction refactor.

### NEBU_OIDC_USER_ID_CLAIM default

Must default to `"name"` to match:
1. Migration 000044 column comment (which states `name` is the default claim)
2. Existing behavior of `OIDCTokenVerifier.VerifyToken` which currently prioritizes
   `sub` then falls back to `name`. However since `sub` is almost always populated,
   the existing code effectively always uses `sub`.

**IMPORTANT:** The current `VerifyToken` behavior (`sub → name` fallback) means
existing deployments have `uploader_user_id` values based on `sub`, not `name`.
The fix must make the default `"name"` to match the story spec and migration comment,
but operators who deployed with `sub` can set `NEBU_OIDC_USER_ID_CLAIM=sub`.
This is a one-time migration concern that is outside the scope of this story.

### docker-compose.yml

Add to the `media` service `environment` block:
```yaml
# Story 12.11 [F-2]: NEBU_TRUSTED_PROXY=false is the secure default.
# Set to "true" only when the media gateway is behind a trusted reverse proxy
# that sets X-Forwarded-For. Without a trusted proxy, XFF can be spoofed.
NEBU_TRUSTED_PROXY: "false"
```

## Previous Story Intelligence

- Story 12.10 established `media/internal/ratelimit/ratelimit.go` with `extractClientIP`.
- Story 12.9 established `formatMatrixUserID` and `sanitiseLocalpart` in `upload.go`.
- Story 12.8 established `OIDCTokenVerifier` and `initOIDCVerifier` pattern.
- Gateway pattern reference: `gateway/internal/middleware/ratelimit.go` lines 85-99
  for the `trustedProxy` gate.

## Definition of Done

- [ ] `media/internal/ratelimit/ratelimit.go`: `IPRateLimiter.trustedProxy` field + `clientIP(r)` method
- [ ] `NewIPRateLimiter` accepts `trustedProxy bool` as second parameter
- [ ] AT-12-11-1 through AT-12-11-3 pass (rate limiter XFF gating)
- [ ] `OIDCTokenVerifier` uses configurable `claimName` (default `"name"`)
- [ ] `extractClaimFromMap` (or equivalent) is a pure function with unit tests
- [ ] AT-12-11-4 through AT-12-11-6 pass (claim extraction)
- [ ] `main.go` reads `NEBU_TRUSTED_PROXY` and `NEBU_OIDC_USER_ID_CLAIM`
- [ ] `docker-compose.yml` has `NEBU_TRUSTED_PROXY: "false"` in media env
- [ ] `make test-unit-go` passes (all media + gateway + matrix packages green)

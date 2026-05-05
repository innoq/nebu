---
security_review: required
---

# Story 9.14: Admin UI — OIDC Token Refresh (Silent Session Renewal)

Status: ready-for-dev

## Story

**As an** admin operator,
**I want** my Admin UI session to be silently renewed when the OIDC access token is about to expire,
**so that** I am not unexpectedly logged out mid-session after an idle period while my OIDC refresh token is still valid.

**Size:** M

---

## Background

### Current state (gap)

The Admin UI session is created at OIDC callback time with `expires_at = min(idToken.Expiry, now + 8h)` and stored in the `admin_sessions` table (Story 5.12). When `expires_at` is reached, `SessionGuardWithStore` redirects the user to `/admin/login`.

However:
- The OIDC OAuth2 `refresh_token` is **not stored anywhere** — it is discarded after the token exchange
- No silent renewal is attempted; every session expiry is a hard logout
- Dex issues refresh tokens when the `offline_access` scope is requested — currently **not included** in the scope list

This means an admin gets silently kicked out after the OIDC `id_token` expiry (Dex default: 24h, but configurable much shorter by the operator).

### Target behavior

1. During the OIDC callback, request `offline_access` scope to receive a refresh token
2. Store the refresh token encrypted (AES-256-GCM, same as `oidc_client_secret`) in a new `refresh_token` column in `admin_sessions`
3. In `SessionGuardWithStore`, when a session expires **and** a valid encrypted refresh token exists:
   - Attempt silent renewal via the OIDC token endpoint
   - On success: update `expires_at` and the stored `refresh_token`, slide the cookie MaxAge, continue the request
   - On failure (token revoked/expired): clear session and redirect to `/admin/login`
4. A small "pre-expiry window" (5 minutes) proactively refreshes before the session actually expires, so in-flight requests are not interrupted

---

## Acceptance Criteria

**AC1 — New DB migration adds `refresh_token` column:**
Migration `000039_admin_sessions_refresh_token.up.sql` adds `refresh_token TEXT` (nullable) to `admin_sessions`. Down migration drops the column. Migration test passes.

**AC2 — `offline_access` scope requested:**
In `LoginStartHandler` and `CallbackHandler` in `auth.go`, `"offline_access"` is added to the scopes slice alongside `openid, profile, email, groups`. The scope list must be identical in both places (they are currently kept in sync — verify this).

**AC3 — Refresh token stored encrypted on session creation:**
`AdminSessionStore.Create` signature includes a `refreshToken string` parameter (or a new `CreateWithRefresh` method — choose what causes fewer callsite changes). The encrypted refresh token is written to the new `refresh_token` column. AES-256-GCM encryption using the existing `internalSecret` (same key as `oidc_client_secret` encryption). Empty string stored as NULL (when Dex does not return a refresh token).

**AC4 — `AdminSession` carries the encrypted refresh token:**
`AdminSession` struct gains `EncryptedRefreshToken string` field. `PostgresAdminSessionStore.Get` reads it from the DB. The decryption happens in the middleware (not in the store), keeping the store free of crypto dependencies.

**AC5 — `SessionGuardWithStore` silently refreshes expiring sessions:**
When `sess.ExpiresAt` is within 5 minutes OR already past (grace window: up to 30 seconds after expiry), and `sess.EncryptedRefreshToken != ""`:
- Decrypt the refresh token
- Exchange it via the OIDC token endpoint (`oauth2.Config.TokenSource` or `provider.Exchange`)
- On success: call `store.UpdateExpiry` (new method) with the new `expires_at = min(newToken.Expiry, now+8h)` and optionally the new `EncryptedRefreshToken`; slide the cookie by setting a new `MaxAge`; continue the request
- On failure: delete the session row, clear the `admin_session` cookie, redirect to `/admin/login`

**AC6 — `AdminSessionStore` interface extended:**
New `UpdateExpiry(ctx, sid string, expiresAt time.Time, encryptedRefreshToken string) error` method on `AdminSessionStore` (and its fake test double).

**AC7 — OIDC config required for refresh:**
The refresh flow needs `issuer`, `clientID`, `clientSecret`. These are loaded from `server_config` via `configReader` — the same path as `LoginStartHandler`. The middleware must have access to `configReader` and the HMAC secret. `SessionGuardWithStore` is refactored to accept these dependencies (or a new `SessionGuardWithRefresh` constructor is introduced).

**AC8 — No silent refresh during bootstrap mode:**
When `BootstrapGuard` is active (bootstrap not yet complete), no refresh is attempted. The guard order in `main.go` must be: `BootstrapGuard` → `SessionGuardWithStore` (current order — verify, do not change).

**AC9 — Audit log entry on session refresh:**
A `"admin_session_refreshed"` audit event is written via `coreClient` when a session is silently renewed. Fields: `actorUserID = sess.UserID`, `action = "admin_session_refreshed"`, `targetType = "session"`, `targetID = sid`, `metadata = {"expires_at": newExpiry}`. On audit failure (gRPC unavailable): log at Warn level only, do not block the refresh.

**AC10 — Existing tests unbroken:**
`make test-unit-go` passes. The fake `AdminSessionStore` in session tests implements `UpdateExpiry`. All existing `SessionGuardWithStore` tests still pass with the new constructor signature.

---

## Tasks / Subtasks

- [ ] **T1 — DB migration (AC1)**
  - [ ] Create `gateway/migrations/000039_admin_sessions_refresh_token.up.sql`: `ALTER TABLE admin_sessions ADD COLUMN refresh_token TEXT;`
  - [ ] Create `gateway/migrations/000039_admin_sessions_refresh_token.down.sql`: `ALTER TABLE admin_sessions DROP COLUMN IF EXISTS refresh_token;`
  - [ ] Run migration test to verify up/down cycle

- [ ] **T2 — Scope update (AC2)**
  - [ ] `auth.go LoginStartHandler`: add `"offline_access"` to scopes
  - [ ] `auth.go CallbackHandler`: add `"offline_access"` to scopes (both `oauth2.Config` instances)
  - [ ] Verify Dex `staticClient` config includes `offline_access` in `allowedGrants` — check `docker/dex/config.yaml` or equivalent

- [ ] **T3 — Store interface extension (AC3, AC4, AC6)**
  - [ ] `session_store.go`: add `EncryptedRefreshToken string` to `AdminSession`
  - [ ] `session_store.go`: update `Create` signature to `Create(ctx, userID string, expiresAt time.Time, encryptedRefreshToken string) (sid string, err error)`
  - [ ] `session_store.go`: add `UpdateExpiry(ctx, sid string, expiresAt time.Time, encryptedRefreshToken string) error` to `AdminSessionStore` interface
  - [ ] `db/admin_session_store.go`: update `Create` INSERT to include `refresh_token` column
  - [ ] `db/admin_session_store.go`: update `Get` SELECT to include `refresh_token` column
  - [ ] `db/admin_session_store.go`: implement `UpdateExpiry`

- [ ] **T4 — Auth callback stores refresh token (AC3)**
  - [ ] `auth.go CallbackHandler`: encrypt `token.RefreshToken` using `encryptAES256GCM(a.secret, refreshToken)` before passing to `sessionStore.Create`
  - [ ] `auth.go ClaimSelectionHandler`: same
  - [ ] Update all `sessionStore.Create` call sites to pass the encrypted refresh token (or empty string if no refresh token received)

- [ ] **T5 — Middleware refresh logic (AC5, AC7, AC9)**
  - [ ] Introduce `SessionGuardWithRefresh` constructor (or extend `SessionGuardWithStore`) that accepts `configReader`, `secret`, `coreClient` in addition to existing params
  - [ ] Implement pre-expiry window check: `sess.ExpiresAt.Before(time.Now().Add(5 * time.Minute))`
  - [ ] Decrypt `EncryptedRefreshToken`, build `oauth2.Config` from DB-loaded OIDC config
  - [ ] Call `oauth2.TokenSource` or `config.TokenSource(ctx, &oauth2.Token{RefreshToken: rt}).Token(ctx)` to obtain new tokens
  - [ ] On success: call `store.UpdateExpiry`, slide cookie MaxAge, call audit log, `next.ServeHTTP`
  - [ ] On failure: `store.Revoke`, clear cookie, redirect to `/admin/login`
  - [ ] Wire new constructor in `main.go`

- [ ] **T6 — Fake session store update (AC10)**
  - [ ] All fake/test `AdminSessionStore` implementations: add `UpdateExpiry` method, update `Create` signature

- [ ] **T7 — Dex config (AC2, prerequisite)**
  - [ ] Check Dex static client config allows `offline_access` / `refresh_token` grant — add if missing

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `TestSilentRefreshExtendSession` — Go integration (Godog or `httptest`)**
- Given: An admin session created with a valid encrypted refresh_token, `expires_at = now - 10s` (just expired)
- When: A request hits any guarded admin route
- Then: The session store `UpdateExpiry` is called with a new future `expires_at`; the response is 200 (not 302)

**2. `TestSilentRefreshFailsRedirectsToLogin` — Go httptest**
- Given: An admin session with an expired OIDC refresh token stored
- When: The token endpoint returns 400 / `invalid_grant`
- Then: `store.Revoke` is called; response is 302 to `/admin/login`

**3. `TestNoRefreshTokenRedirectsToLogin` — Go httptest**
- Given: An admin session with `refresh_token = NULL` (empty), `expires_at` in the past
- When: A request hits a guarded route
- Then: Response is 302 to `/admin/login` (no refresh attempt)

**4. `TestMigration039UpDown` — Go migration test**
- When: Run migration 000039 up
- Then: `admin_sessions` has column `refresh_token TEXT`
- When: Run migration 000039 down
- Then: Column absent; previous migration snapshot intact

**5. `TestOfflineAccessScopeInCallback` — Go unit**
- Given: `LoginStartHandler` builds the auth URL
- Then: The URL's `scope` parameter contains `offline_access`

---

## Dev Notes

### Crypto: AES-256-GCM encryption helper

`encryptAES256GCM` and `decryptAES256GCM` already exist in `gateway/internal/admin/crypto.go` (or similar — used for `oidc_client_secret`). Reuse the same functions. The `secret` key is `a.secret` (the `internalSecret` loaded from file at startup). **Do not introduce a second key or a new crypto primitive.**

Find the existing encrypt/decrypt functions:
```bash
grep -rn "encryptAES256GCM\|decryptAES256GCM" gateway/internal/admin/
```

### OAuth2 token refresh call

The correct way to refresh using `golang.org/x/oauth2`:

```go
cfg := &oauth2.Config{
    ClientID:     clientID,
    ClientSecret: clientSecret,
    Endpoint:     provider.Endpoint(),
    Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups", "offline_access"},
}
existingToken := &oauth2.Token{RefreshToken: decryptedRefreshToken}
// TokenSource automatically calls /token endpoint with grant_type=refresh_token
ts := cfg.TokenSource(ctx, existingToken)
newToken, err := ts.Token(ctx)
```

`newToken.RefreshToken` may be a new refresh token (token rotation) or empty (single-use disabled). Always update the stored refresh token with whatever Dex returns (or keep old one if new is empty).

### Session expiry sliding

When the refresh succeeds, the new session expiry is:
```go
newExpiry := time.Now().Add(8 * time.Hour)
if newToken.Expiry.Before(newExpiry) {
    newExpiry = newToken.Expiry
}
```

Update the cookie MaxAge to match:
```go
http.SetCookie(w, &http.Cookie{
    Name:   "admin_session",
    Value:  cookie.Value, // same SID cookie, MaxAge extended
    Path:   "/admin",
    MaxAge: int(time.Until(newExpiry).Seconds()),
    ...
})
```

### Middleware constructor refactor

`SessionGuardWithStore` currently takes `(secret []byte, store AdminSessionStore)`. The refresh variant needs `configReader`, `globalProviderCache`, and optionally `coreClient`. Introduce `SessionGuardConfig` struct or a dedicated constructor to avoid a growing parameter list:

```go
type SessionRefreshConfig struct {
    Secret       []byte
    Store        AdminSessionStore
    ConfigReader ServerConfigReader
    CoreClient   pb.CoreServiceClient
}

func SessionGuardWithRefresh(cfg SessionRefreshConfig) func(http.Handler) http.Handler { ... }
```

Wire this in `main.go` instead of the old `SessionGuardWithStore`.

### Dex offline_access

Dex requires the client to have `grantTypes: ["authorization_code", "refresh_token"]` in its static client config. Check `docker/dex/config.yaml` (or equivalent path). If `refresh_token` is not in `grantTypes`, Dex will silently omit the refresh token even when `offline_access` is requested.

```yaml
staticClients:
  - id: nebu-admin
    grantTypes:
      - authorization_code
      - refresh_token   # add this if missing
```

### Security considerations

- Refresh tokens are long-lived credentials — they MUST be stored encrypted (AC3 mandates AES-256-GCM)
- Never log the plaintext refresh token
- The `refresh_token` column is `TEXT NOT NULL DEFAULT NULL` — a NULL value means no refresh token was issued (Dex returned none or the operator disabled offline_access); the middleware must handle NULL gracefully
- Token rotation: Dex rotates refresh tokens by default — always store the latest token returned by the refresh call

### Pre-expiry window — 5 minutes

The 5-minute window avoids race conditions where the session expires between the check and the response write. Set as a package-level constant:

```go
const sessionRefreshWindow = 5 * time.Minute
```

### References

- Admin session store interface: `gateway/internal/admin/session_store.go`
- DB implementation: `gateway/internal/db/admin_session_store.go`
- Session guard middleware: `gateway/internal/admin/middleware.go`
- Auth callback (scope + token creation): `gateway/internal/admin/auth.go`
- AES-256-GCM helpers: search `grep -rn "encryptAES256GCM" gateway/internal/admin/`
- Existing migration: `gateway/migrations/000017_admin_sessions.up.sql`
- OIDC provider wrapper: `gateway/internal/auth/oidc.go`
- `golang.org/x/oauth2` token refresh: standard `TokenSource` + `Token()` pattern

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

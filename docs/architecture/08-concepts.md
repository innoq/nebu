# 8 Cross-Cutting Concepts

## Authentication and Authorization

**OIDC-only (no local accounts):** Every user authenticates via an OIDC-conformant provider
(Keycloak, Azure AD, Dex, Google). No local passwords, no shadow directory.

**Token flow:**
```
Matrix Client → Go Gateway:
  Authorization: Bearer <oidc_token>
  → Go validates token via OIDC provider (JWK verification)
  → extracts: user_id (@sub:server.name), system_role

Go → Elixir (gRPC metadata):
  "x-user-id": "@user:server.name"
  "x-system-role": "user" | "instance_admin" | "compliance_officer"

Elixir: trusts Go fully — no own token validation
Auth token never forwarded to Elixir — only user_id + system_role
```

**Bootstrap mode:** First OIDC login automatically receives `instance_admin`. Bootstrap mode is
permanently disabled after the first admin setup. No default password, no insecure fallback.

**SSO Nonce-Based Replay Prevention (Story 11-7, hardened Story 12.7):**

`GetSSORedirect` generates a 16-byte cryptographically random nonce (32 hex chars) per SSO
flow. The nonce is stored in `globalSSOState` alongside the PKCE verifier and forwarded to
Dex via `oauth2.SetAuthURLParam("nonce", nonce)`. In `GetSSOCallback`, the `nonce` claim is
extracted from the returned id_token and compared against the stored nonce using
`crypto/subtle.ConstantTimeCompare` (timing-attack safe, Story 12.7 LOW-7). A mismatch results
in `403 M_FORBIDDEN "SSO nonce mismatch"` — no loginToken is issued. The server nonce is
never logged on mismatch (Story 12.7 LOW-8). This prevents Dex's server-side token caching
from replaying a previously invalidated JWT as a new access_token.

**Denylist Check at Login (Story 11-7):**

`PostLogin` calls `h.store.IsInvalidated(rawJWT)` before issuing an access_token. If the
submitted JWT is in the denylist, it returns `403 M_FORBIDDEN "Token has been logged out"`.
This is defence-in-depth against direct `POST /login` calls that submit a previously
invalidated JWT (bypassing the SSO nonce check). Status `403 M_FORBIDDEN` (not `401`) is
intentional: `POST /login` is an authentication *attempt*, not a session validation request.
`LoginHandler.store` is nil-safe — deployments without a denylist skip the check.

**Cache-Control: no-store on SSO Redirect (Story 11-7):**

`GetSSORedirect` sets `Cache-Control: no-store` before the `http.Redirect` call. This
prevents Safari (and any intermediate cache) from storing the 302 redirect response. Without
this header, Safari can replay a cached redirect on re-login, causing the already-consumed
state parameter to be rejected (`400 M_UNKNOWN "Invalid or expired SSO state"`).

**Rate limiting (per IP) — API Gateway:**

| Tier | Rate | Endpoints |
|---|---|---|
| strict | 30/min, burst 10 | `POST /login` (brute-force risk) |
| compliance | 10/min, burst 10 | All compliance/* and admin key/anonymize |
| admin | 60/min, burst 20 | Login UI, bootstrap wizard |
| medium | 30/min, burst 10 | SSO redirect/callback, public profile |
| loose | 300/min, burst 100 | Discovery, capabilities, unauthenticated stubs |

Implementation: `gateway/internal/middleware/ratelimit.go` — `NewIPRateLimiter` with LRU cache (10 000 entries, `github.com/hashicorp/golang-lru`). IP extracted via rightmost-minus-1 XFF strategy (spoofing-resistant). Prometheus `nebu_rate_limit_total` counter per tier.

**Rate limiting (per IP) — Media Gateway (Story 12.10):**

| Tier | Rate | Burst | Endpoints |
|---|---|---|---|
| upload | 10 req/s | 5 | `POST /_matrix/media/v3/upload` |
| download | 100 req/s | 20 | `GET /_matrix/media/v3/download/…`, `GET /_matrix/media/v3/thumbnail/…` |

Implementation: `media/internal/ratelimit/ratelimit.go` — `NewIPRateLimiter(ctx context.Context, cfg Config, trustedProxy bool)` with `sync.Map` keyed by IP and a background cleanup goroutine (evicts entries not seen for >5 minutes, runs every 1 minute). No LRU dependency — simpler sync.Map + time-based eviction.

**Story 12.14 — Stoppable cleanup goroutine:** `NewIPRateLimiter` now accepts `ctx context.Context` as its first parameter. The `cleanupLoop` goroutine uses a `select { case <-ticker.C: ... case <-ctx.Done(): return }` instead of `for range ticker.C`. When SIGTERM cancels the main context, the cleanup goroutine exits within one ticker interval (≤1 minute), preventing goroutine leaks during graceful shutdown.

**IP extraction — trusted-proxy gate (Story 12.11 SEC Fix F-2):**

| `NEBU_TRUSTED_PROXY` | Behavior |
|---|---|
| `false` (default) | Always use `RemoteAddr`; ignore `X-Forwarded-For`. Attacker cannot bypass per-IP limit by rotating XFF header values. Use when the media gateway is directly exposed. |
| `true` | Use rightmost `X-Forwarded-For` entry (proxy-appended) when header has 2+ entries; fall back to `RemoteAddr`. Use only when behind a trusted reverse proxy that strips client-supplied XFF. |

`NEBU_RATE_LIMIT_DISABLED=true` disables both tiers (dev/test escape hatch). **Story 12.12 (F-5):** When disabled, the media gateway emits `slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")` exactly once at startup, enabling operators to confirm rate-limiting status via startup logs. 429 response format: `{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}` + `Retry-After: N` header (Matrix CS API §rate-limiting compliant).

**Cleanup correctness (Story 12.12 F-4):** The background cleanup goroutine (`cleanupLoop`) evicts entries where `time.Since(entry.lastSeen) > 5 minutes`. `entry.lastSeen` is updated by `getOrCreate` on every request — before the handler executes. An in-flight request updates `lastSeen` before the handler runs, so a concurrent cleanup tick never evicts a bucket for a request that is actively being processed.

## Cryptography

**Two key pairs per user (ADR-007):**

| Key Pair | Algorithm | Purpose |
|---|---|---|
| Signing Key | Ed25519 | Message signatures, non-repudiation |
| Encryption Key | X25519 (ECDH) | PII encryption, GDPR deletion |

Generated at user registration via Erlang/OTP 27 `:crypto` — no external Hex packages.

**Event signing:** All Matrix events are signed with the sender's Ed25519 private key before storage.
`Nebu.EventId.generate/1` computes `$<base64url(SHA-256(canonical_json(event)))>`.

**GDPR Right-to-be-Forgotten:** Delete both private keys → sensitive PII (email, IdP subject) is
permanently irrecoverable. Audit log integrity is preserved — message signatures remain verifiable
via the permanent public key.

**Compliance key:** A separate Ed25519 keypair in `server_config` (AES-256-GCM encrypted) signs
compliance export tokens. Survives Elixir restarts (unlike the ephemeral `:nebu_signing_key`).

## Audit Logging

All administrative actions and compliance access events are written to an append-only `audit_logs`
table. An `BEFORE INSERT` trigger prevents UPDATE and DELETE (enforced at the DB level via RLS).
Entries are Ed25519-signed for tamper evidence. Retention is configurable (default: 2555 days / 7 years).

**Failure audit trail pattern (Story 9-27):** Multi-step operations (e.g. room upgrade) that can
fail partway through wrap their entire body in `try/rescue`. The `rescue` clause writes a "failure"
audit entry (including an `"error"` metadata field with the exception message) before reraising the
original exception. A nested `try/rescue` around the audit write itself ensures an outage in the
audit writer never masks the original error. This guarantees that any partially-applied operation
leaves a forensic record even when the gRPC call ultimately returns an error to the client.

## Three PII Tiers

| Tier | Data | At Rest | GDPR Deletion |
|---|---|---|---|
| Operational PII | Display name, avatar | Encrypted at rest | Overwrite with "Deleted User [id]" |
| Sensitive PII | Email, IdP subject | Encrypted with user's X25519 key | Delete private key → irrecoverable |
| Message Content | Chat messages | Plain in DB (audit requirement) | Not deleted; sender anonymized |

## Per-Device Sync Token Isolation (Story 9-22)

Matrix clients identify themselves with a `device_id` extracted from the `"did"` claim in the
JWT access token. The Elixir Core maintains independent sync checkpoints per `(user_id, device_id)`
in the `sync_tokens` table (composite PK, migration 000041).

**Design invariants:**

- A single user with N active devices has N independent rows in `sync_tokens`.
- Each device's `since` token is advanced only by that device's sync responses.
- On logout, only the `(user_id, device_id)` row is removed; other devices are unaffected.
- Legacy clients (no `device_id` in JWT) fall back to the `device_id = ''` row; a token mismatch
  triggers a full initial sync (safe degradation).
- `persist_since_token/4` and `get_since_token/2` are the device-aware arities in
  `Nebu.Session.PgStore.Postgres`; `/3` and `/1` remain for backward compatibility.

**Token cleanup on logout:**
`POST /logout` triggers `gRPC InvalidateUserSessions(user_id, device_id)` → Elixir
`SessionSupervisor.destroy_session/2` → DB transaction deletes `sync_tokens` + `sessions` rows
for that device. ETS is NOT evicted (other devices may still be active).

## Error Handling

**Go:** Return-based, no panic in library code. gRPC status codes map to HTTP status codes at the
gateway boundary. Matrix endpoints return `{"errcode": "M_...", "error": "..."}` format.

**Elixir:** Tagged tuples `{:ok, result}` / `{:error, reason}`. No raise/throw for business logic.
Let-it-crash + OTP Supervisor Trees for unexpected failures.

**Media Gateway — Storage Error Classification (Story 12.4):**

The `Storer` interface returns sentinel errors that map to specific HTTP status codes:

| Sentinel | Cause | HTTP | errcode |
|---|---|---|---|
| `storage.ErrNotFound` | Object absent from backend (MinIO `NoSuchKey`, OS `ErrNotExist`) | 404 | `M_NOT_FOUND` |
| `storage.ErrStorageUnavailable` | Network unreachable, MinIO degraded, other MinIO errors | 502 | `M_UNKNOWN` |
| other | Crypto failure, hex decode error | 500 | `M_UNKNOWN` |

Classification logic: `ClassifyMinIOError` uses `errors.As` to unwrap `minio.ErrorResponse` (not `minio.ToErrorResponse` which does direct type assertion). `LocalStorer.Get` maps `os.ErrNotExist` → `ErrNotFound`. The handler logs the full error via `slog.Error` but returns only a generic message to the client (no credential/endpoint leak).

**Media Gateway — Thumbnail Generation (Story 12.5):**

On-demand thumbnail generation for `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}`:

| Concern | Decision | Rationale |
|---|---|---|
| Library | `github.com/disintegration/imaging v1.6.2` (MIT) | Pure Go, no cgo, no shell exec — sandboxed by construction |
| MIME detection | `net/http.DetectContentType` on first 512 bytes | Magic bytes only; never trust stored `Content-Type` |
| Allowed types | `image/jpeg`, `image/png`, `image/gif`, `image/webp` | Deny-by-default: SVG/PDF/PS/EPS → 400 M_BAD_JSON |
| Scale method | `imaging.Fit` | Aspect-ratio-preserved, fits within W×H |
| Crop method | `imaging.Fill` (center) | Center-crop to exactly W×H |
| Animated GIF | `gif.DecodeAll` + per-frame resize + Floyd-Steinberg re-palettize | Preserves all frames + loop count |
| animated=false | Static JPEG (first-frame decode) | Spec MUST NOT return animated thumbnail |
| Output format | JPEG (85 quality) for static; GIF for animated | Balance size/quality |
| Headers | `Content-Disposition: inline; filename=...` (spec v1.12) + `Cache-Control: max-age=86400` | Matrix spec MUST requirements |
| Security | `mediaId` validated with `^[A-Za-z0-9_\-]+$` before DB/storage call | Path traversal prevention (Matrix spec §5.6) |
| Dimension cap | `const maxThumbDim = 2048`; width > 2048 or height > 2048 → 400 M_BAD_JSON | Memory-amplification DoS prevention (Story 12.7 HIGH-1) |
| Source image budget | After `imaging.Decode`, check `Dx * Dy > 100_000_000` → return error | Decompression-bomb defence — prevents ≥400 MB allocations from compressed source |
| GIF frame cap | `len(g.Image) > maxGIFFrames (200)` → truncate to first 200 frames silently | Bounded resource usage for animated GIFs; no error returned |
| Response headers | `X-Content-Type-Options: nosniff` set on all thumbnail responses | Prevent MIME sniffing in legacy browsers (Matrix spec v1.12) |

**Media Gateway — Upload/Download Security (Story 12.7 HIGH-3):**

Content-Type enforcement to prevent stored XSS:

| Phase | Rule | Mechanism |
|---|---|---|
| Upload — blocklist | `text/html`, `application/xhtml+xml`, `text/javascript`, `application/javascript`, `image/svg+xml`, `application/x-shockwave-flash` → 400 M_BAD_JSON | `blockedContentTypes` map; normalized with `mime.ParseMediaType` (strips params) |
| Download — safe inline | `image/jpeg`, `image/png`, `image/gif`, `image/webp`, `image/avif`, `audio/mpeg`, `audio/ogg`, `video/mp4`, `video/webm`, `application/pdf`, `text/plain` → `Content-Disposition: inline` | `safeInlineContentTypes` map; normalize with `strings.SplitN(...,";",2)` |
| Download — unsafe fallback | All other stored types → `Content-Type: application/octet-stream`, `Content-Disposition: attachment` | Prevents inline rendering of pre-allowlist uploads |
| All download responses | `X-Content-Type-Options: nosniff` always set | Prevent MIME sniffing |

**Media Gateway — Upload JWT Validation + Fail-Closed OIDC Startup (Stories 12.7, 12.8):**

The upload handler uses a `TokenVerifier` interface (`HandlerConfig.OIDCVerifier`). `OIDCTokenVerifier` wraps `*oidc.IDTokenVerifier` and implements `VerifyToken(ctx, rawToken) (string, error)` — returning the uploader's subject identity from the operator-configured OIDC claim (see Story 12.11 below). Uploads with a nil verifier receive `503 M_UNAVAILABLE` (fail-closed, not fail-open).

**Startup hardening (Story 12.8 + Story 12.12 F-3):** `NEBU_OIDC_ISSUER` is mandatory. If empty, the media gateway exits immediately with `FATAL: NEBU_OIDC_ISSUER is required`. If Dex is unreachable at startup, `initOIDCVerifier` retries up to 5 times with 2s backoff, then exits (`FATAL: media: OIDC provider unreachable after retries`). The service never starts with a nil verifier. This eliminates the "OIDC fail-open at startup" pattern documented as a recurring vulnerability.

Story 12.12 (F-3) adds a **10-second per-attempt timeout** to each `oidc.NewProvider` call via `context.WithTimeout(ctx, 10s)`. A provider that accepts the TCP connection but never sends a response (hung Dex, firewall issue) can no longer block startup indefinitely — each attempt fails within 10s, and the 5-retry cycle completes within ≤60s total. Parent context cancellation (SIGTERM during startup) is checked at the top of every retry iteration for immediate exit.

Story 12.13 completes **graceful shutdown during OIDC retries**: `main()` creates a SIGTERM-aware context via `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. This makes the parent `ctx` actually cancel on SIGTERM — previously `context.Background()` was used and the ctx.Err() guard was dead code for signal handling. Additionally, the inter-retry sleep (`time.Sleep(retryDelay)`) is replaced by a ctx-aware `select { case <-time.After(retryDelay): case <-ctx.Done(): return error }`. A SIGTERM received during the 2-second backoff sleep now interrupts immediately rather than waiting up to 2s, ensuring `docker compose stop` completes within Docker's 10-second grace window. Log messages distinguish signal abort (`slog.Info`) from retry exhaustion (`slog.Error`).

**Story 12.14 — Full runtime graceful shutdown (HTTP + DB Pool + Rate Limiter):** Completes the SIGTERM story. Three runtime gaps are closed:

1. **HTTP drain** — `srv.ListenAndServe()` runs in a background goroutine. `main()` blocks on `select { case err := <-serverErr; case <-ctx.Done() }`. On SIGTERM, `srv.Shutdown(shutdownCtx)` is called with a 30-second timeout. In-flight HTTP requests complete before the server stops accepting new connections. After 30s, `Shutdown` returns `context.DeadlineExceeded` and shutdown proceeds regardless.

2. **DB pool close** — `pool.Close()` is called explicitly after `srv.Shutdown` returns (not via `defer`). The `defer pool.Close()` pattern was removed because `os.Exit(1)` bypasses defers; explicit sequencing ensures DB connections are always released. The shutdown order is: `srv.Shutdown` → `pool.Close()` → `slog.Info("media gateway stopped")` → `main()` returns (exit code 0).

3. **Rate limiter goroutine** — `NewIPRateLimiter` now accepts `ctx context.Context` (see Rate Limiting section above). The cleanup goroutine exits cleanly when SIGTERM cancels the context.

Shutdown helper: `runShutdownSequence(ctx context.Context, srv *http.Server, poolClose func())` encapsulates the ordered shutdown steps and is unit-testable with a spy closure for `poolClose` (no real DB pool required in tests).

**Media Gateway — Authenticated Media Endpoints (Story 12.16):**

Matrix CS API v1.11+ introduced authenticated download/thumbnail paths (`/_matrix/client/v1/media/*`) alongside the deprecated unauthenticated `/_matrix/media/v3/*` paths. The Media Gateway now implements both path families.

**Auth middleware package (`media/internal/auth`):**

The `auth.Middleware` wraps any `http.Handler` with Bearer token verification. It defines a local `TokenVerifier` interface (same signature as `upload.TokenVerifier` — no import cycle):

```go
type TokenVerifier interface {
    VerifyToken(ctx context.Context, rawToken string) (string, error)
}
```

Error mapping:

| Condition | HTTP | errcode |
|---|---|---|
| Missing or non-Bearer Authorization header | 401 | `M_MISSING_TOKEN` |
| Empty Bearer token (`Bearer ` with nothing after) | 401 | `M_MISSING_TOKEN` |
| Verifier returns error (invalid/expired token) | 401 | `M_UNKNOWN_TOKEN` |
| Nil verifier (guard) | 503 | `M_UNAVAILABLE` (fail-closed) |
| Valid token | — | handler called |

`*upload.OIDCTokenVerifier` satisfies `auth.TokenVerifier` structurally; the same instance (`uploadVerifier` from `initOIDCVerifier`) is reused — no separate OIDC client or audience check.

**Routing pattern in `main.go`:**

```
// Deprecated unauthenticated (backward compat):
GET /_matrix/media/v3/config          → configHandler (no auth)
GET /_matrix/media/v3/download/...    → downloadHandler (no auth)
GET /_matrix/media/v3/thumbnail/...   → thumbnailHandler (no auth)

// v1.11+ authenticated:
GET /_matrix/client/v1/media/config                          → authMW.Wrap(configHandler)
GET /_matrix/client/v1/media/download/{serverName}/{mediaId} → authMW.Wrap(downloadHandler)
GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName} → authMW.Wrap(downloadHandler)
GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId} → authMW.Wrap(thumbnailHandler)
```

The `{fileName}` path segment maps to `r.PathValue("fileName")` inside the download handler for the `Content-Disposition` filename. When the route without `{fileName}` is matched, `PathValue` returns `""` and the handler falls back to `mediaId`.

**Security response headers on download and thumbnail (Matrix spec §Media Repository SHOULD, v1.4+):**

All 200 responses from the download and thumbnail handlers — on both v3 and v1 paths — now include:

```
Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';
Cross-Origin-Resource-Policy: cross-origin
```

These headers prevent script execution from served media objects and allow cross-origin image loading by Element Web (required for inline image display in rooms).

**Canonical Matrix user ID in audit trail (Story 12.9):** After OIDC verification, the upload handler constructs a canonical Matrix user ID before storing `uploader_user_id` in `media_files`:

```
uploader_user_id = formatMatrixUserID(subject, serverName)
                 = "@" + sanitiseLocalpart(subject) + ":" + serverName
```

`sanitiseLocalpart` keeps only `[a-z0-9._-]` characters (spaces → `_`, all others dropped). This mirrors `gateway/internal/grpc/metadata.go sanitiseLocalpart` but is intentionally self-contained (no cross-binary import). `NEBU_SERVER_NAME` is mandatory — the media gateway exits with `FATAL: NEBU_SERVER_NAME is required` if unset. Historical rows (pre-12.9) contain raw OIDC claims and are grandfathered; see migration 000047 column comment. This enables compliance officers to correlate `media_files.uploader_user_id` with room event `sender` fields without manual claim-mapping.

**Configurable OIDC claim for audit trail (Story 12.11 SEC Fix F-1):** The claim used as the audit trail user identity is now operator-configurable via `NEBU_OIDC_USER_ID_CLAIM` (default: `name`, matching migration 000044 column comment and the gateway's DB default).

| `NEBU_OIDC_USER_ID_CLAIM` | Behavior |
|---|---|
| unset or `name` (default) | Uses the `name` claim from the OIDC token |
| `sub` | Uses the `sub` claim (UUID from IdP) |
| `email`, `preferred_username`, etc. | Uses the specified claim |
| any (configured claim missing) | Falls back to `sub` with a `slog.Warn` log entry |

Implementation: `extractClaimFromMap(rawClaims map[string]interface{}, claimName string) (string, error)` — pure function in `media/internal/upload/upload.go`. The value goes through `sanitiseLocalpart` before storage. `OIDCTokenVerifier` is constructed with `NewOIDCTokenVerifier(idTokenVerifier, claimName)` in `initOIDCVerifierWith`.

**server_config RLS UPDATE Policy (Story 12.7 MEDIUM-5, extended Story 14-2a):**

Migration 000046 replaces the blanket `config_update_all` policy (USING true — introduced in migration 000045 for OIDC claim upserts) with a key-scoped `config_update_mutable` policy. Migration 000048 (Story 14-2a) extends the allowlist with two new OIDC directory keys. Only the following keys are updatable by `nebu_app`:

- `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`, `admin_group_claim`
- `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`
- `oidc_directory_enabled`, `oidc_directory_endpoint` _(added in migration 000048, Story 14-2a)_

`server_name`, `bootstrap_completed`, and any future keys not in this allowlist are immutable at the DB level (defence-in-depth restoration from ADR G8).

**Proto3 Bool / Direct DB Upsert Pattern (Story 14-2a):**

Fields stored as booleans in `server_config` (e.g. `oidc_directory_enabled`) cannot be safely round-tripped via proto3 gRPC because `false` is the proto default and is indistinguishable from "not set". The pattern used: the REST API PATCH handler (`api.AdminServer`) holds a `ServerConfigRepository` and upserts directly. The Admin UI handler (`admin.ConfigHandler`) accepts a `ConfigKeyWriter` interface (set via `WithConfigDB`) and upserts directly for bool fields, while routing string fields through gRPC. This avoids inadvertent resets of boolean flags on unrelated gRPC calls.

**OIDC Directory Service Pattern (Story 14-2b):**

`OIDCDirectoryService` (`gateway/internal/admin/oidc_directory.go`) provides a secure, cached client for fetching user lists from an admin-configured OIDC directory endpoint. Key design decisions:

- **secretString type**: the bearer token is wrapped in `type secretString string` with `String() "[REDACTED]"` — prevents accidental log exposure via `%+v` or slog auto-formatting (CR-3).
- **HTTPS-only at call time**: `validateEndpoint` is called inside `FetchUsers` on every invocation, not just at config load. This is a defensive check: if the stored endpoint is later mutated to HTTP, the service fails hard rather than silently falling back (CR-1).
- **No redirect following**: the default `http.Client` uses `CheckRedirect: ErrUseLastResponse`. This prevents an HTTP→HTTPS redirect from bypassing the HTTPS check, and prevents SSRF via provider-issued redirects to internal metadata endpoints (CR-2).
- **Rate limit separation**: `Allow(sessionID string) bool` and `FetchUsers(ctx) ([]OIDCDirectoryUser, error)` are separate methods. Callers MUST call `Allow` before `FetchUsers` to enforce the per-session 5 req/s limit. The separation allows callers to short-circuit before cache or network access. Session IDs MUST come from the validated JWT (admin middleware), not from request headers (CR-5).
- **singleflight + 30s cache**: concurrent cache misses collapse into one outbound call (MR-4). Cache key = SHA-256(endpoint + "|" + token) so rotating the bearer token invalidates the stale cache (MR-1).
- **Graceful degradation**: runtime errors (unreachable host, non-200, JSON parse failure) are logged as warnings and swallowed — FetchUsers returns empty list, not error. Configuration errors (non-HTTPS) are propagated as errors.

**OIDC Directory User Search Merge Pattern (Story 14-2c):**

`UsersHandler.ListHandler` merges Nebu DB users (from gRPC `ListAdminUsers`) with OIDC directory users (from `OIDCDirectoryService.FetchUsers`) when `oidc_directory_enabled=true`. Key design decisions:

- **WithOIDCDirectory setter**: `UsersHandler` accepts an `*OIDCDirectoryService` via a fluent setter (`WithOIDCDirectory(svc, serverName)`). The handler field is nil by default — zero-config backward compatibility.
- **Nebu DB wins on dedup**: OIDC users whose `sanitize(sub)` matches a Nebu `UserId` are dropped from the OIDC-only list. The Nebu DB entry always takes precedence.
- **IsOIDCOnly flag**: `UserRowData.IsOIDCOnly=true` + `MatrixIDPreview` mark users present only in the OIDC directory. The preview is computed as `@{sanitize(sub)}:{serverName}` and is not persisted.
- **Non-blocking OIDC failure**: when the OIDC provider is unreachable, `FetchUsers` returns an empty list (logged internally). `ListHandler` detects this via `IsEnabled() && len(oidcUsers)==0` and sets `OIDCWarningBanner` — the Nebu DB list is still rendered.
- **Rate-limit gate**: `Allow(sessionID)` is called before `FetchUsers`. If rate-limited, OIDC fetch is silently skipped (no warning banner — rate-limiting is defensive, not an availability signal).

**Media Event Content Pass-Through Contract (Story 12.6):**

`content.info` in `m.room.message` (and all other event types) is an **opaque JSON object** — the gateway passes it verbatim to Core, and Core stores it as JSONB without field inspection or modification. Client extension fields such as `blurhash` (used by Element Web for image loading placeholders) MUST be preserved through the full round-trip: gateway → gRPC → Core → DB → sync response.

- The server never computes `blurhash` — it is always client-provided.
- The `JSON → map[string]any → json.Marshal` round-trip in `PutSendEvent` preserves all fields.
- The sync handler returns `content` as `json.RawMessage` — no field filtering.

This contract is validated by `TestSendEvent_BlurhashInContentInfo_PassedToGRPC` (AC1) and `TestGetSync_BlurhashInTimelineContent_PassedThrough` (AC2) in `gateway/internal/matrix/rooms_test.go` and `sync_test.go`.

**gRPC error surface rule (Story 9-27):** Elixir gRPC handlers must use `raise GRPC.RPCError,
status: GRPC.Status.<code>(), message: "..."` to propagate errors to the Go gateway. Bare `:ok =`
pattern matches on `Room.Server` calls produce `MatchError` at runtime, which gRPC-elixir maps to
`codes.Unknown` → HTTP 500 with no structured error message. The correct form is a `case` expression
that raises `GRPC.RPCError` with `GRPC.Status.internal()` on unexpected `{:error, reason}` tuples.
This distinction is critical: `codes.Unknown` is ambiguous whereas `codes.Internal` correctly
signals a server-side failure to the Go gateway's error mapper.

## Room GenServer Start on Demand (Story 11-11)

After a stack restart, Room GenServers are not automatically re-started — rooms exist in the DB
but have no running process in the Horde registry. Handlers that need an active Room GenServer
MUST use `start_room/1` (idempotent) rather than `lookup_room/1` (registry-only) to avoid
returning `404` for rooms that genuinely exist.

**Pattern:**

```elixir
case Nebu.Room.RoomSupervisor.start_room(room_id) do
  {:error, _reason} ->
    # Room does not exist in DB — Room.Server.init/1 returned {:stop, _}
    raise GRPC.RPCError, status: GRPC.Status.not_found(), message: "room not found: #{room_id}"
  {:ok, _pid} ->
    # GenServer is running (either already was, or just started from DB)
    # ... proceed with room operation
end
```

**Idempotency invariant:** `RoomSupervisor.start_room/1` handles `{:error, {:already_started, pid}}`
internally and converts it to `{:ok, pid}`. Callers always receive either `{:ok, pid}` (GenServer
running) or `{:error, _}` (room absent from DB).

**`{:error, _}` wildcard trade-off:** Horde supervisor errors (e.g. `:max_children`) also map to
`not_found` via this pattern. This is an acceptable trade-off: such errors are rare, and the alternative
of forwarding an opaque `GRPC.Status.internal()` is less useful to clients. The same trade-off applies
consistently across all handlers that use `start_room/1`.

**Race guard — `:noproc` after `start_room`:**

In rare cases, a GenServer may die between `start_room` returning `{:ok, pid}` and the subsequent
`get_state/1` call (e.g. during Horde CRDT reconciliation). Handlers that call `get_state` after
`start_room` MUST wrap that call in a `try/catch :exit, {:noproc, _}` guard to surface the failure
as `not_found` rather than a raw exit:

```elixir
state =
  try do
    room_registry_module().get_state(room_id)
  catch
    :exit, {:noproc, _} ->
      raise GRPC.RPCError, status: GRPC.Status.not_found(), message: "room not found: #{room_id}"
  end
```

**Handlers using `start_room` (not `lookup_room`):**

| Handler / function | Call site in `server.ex` |
|---|---|
| `sync.ex` — incremental sync | line ~1053 |
| `unarchive_room/2` | line ~1959 |
| `upgrade_room/2` — new room | line ~2487 |
| `send_receipt/2` | line ~662 (added Story 11-11) |

**Handlers intentionally using `lookup_room`** (started elsewhere in the same request lifecycle,
or explicitly requiring the GenServer to be pre-running):

All other handlers in `server.ex`. Migration of remaining handlers (e.g. `set_typing/2`) is
deferred to a separate story to keep diffs minimal and reviewable.

## Logging

**Go:** `log/slog` (stdlib), structured key-value pairs.
**Elixir:** `Logger` with keyword metadata.

PII (email, display name) must never appear in log output. Debug level never enabled in production.

## API Response Formats

**Strict separation:** Matrix endpoints return Matrix format; Admin API returns wrapper format.
No mixing.

```json
// Matrix API success
{"event_id": "$abc123", "room_id": "!xyz:server.name"}

// Admin API success
{"data": {...}, "meta": {"cursor": "v1_abc", "limit": 50}}

// Admin API error
{"error": {"code": "USER_NOT_FOUND", "message": "..."}}
```

## Admin UI Security

CSRF double-submit cookie on all state-changing Admin UI POST endpoints. Security headers
middleware on all `/admin/*` routes (CSP, HSTS, X-Frame-Options). Session cookies are
`HttpOnly`, `SameSite=Lax`. Admin UI templates served via `go:embed` — no filesystem access
at runtime.

## Testing Architecture (Story 9-26)

Nebu uses a two-layer E2E test strategy, splitting tests by the level of observable behavior:

| Layer | Runner | Scope | Location |
|---|---|---|---|
| HTTP / Matrix API | Godog + `net/http` | REST endpoints, Matrix CS protocol, gRPC | `gateway/features/` |
| Browser / UI | Playwright + `playwright-bdd` | Element Web flows, Admin UI, real OIDC redirects | `e2e/` |

**No plain `.spec.ts` files without a `.feature` counterpart are accepted for new stories.**
Gherkin `.feature` files are the single source of truth for all E2E scenarios.

### Browser-First E2E Layer

All browser-level tests use `playwright-bdd` as the execution engine. Feature files are defined
first (failing), step definitions implement them in TypeScript. The `playwright.config.ts` registers
three test projects:

- `chromium` — legacy API-contract tests (no BDD change)
- `admin-ui` — Admin UI BDD tests via `playwright-bdd`
- `element-web` — Element Web browser-first E2E via `playwright-bdd`

```
e2e/
  features/
    element/            ← login, room/{create,join,leave}, messages/{send,receive}
    admin/              ← bootstrap, dashboard, auth-guard, users, rooms, audit-log
  step-definitions/
    common/             ← auth, navigation, stack-health, room-setup, assertions (shared)
    element/            ← login, room, messages steps
    admin/              ← bootstrap, dashboard, users, rooms steps
  fixtures/
    users.ts            ← NEBU_USERS const (4 pre-configured Dex test users)
    dex-auth.ts         ← loginViaOidcBrowser(), ensureStorageState(), getApiSession()
    element-app.ts      ← ElementAppPage (Playwright page object for Element Web)
    nebu-fixtures.ts    ← createBdd(test) — exports { Given, When, Then }
  global-setup.ts       ← warms token sidecars + bootstraps admin before tests
```

### Token Sidecar Pattern for IndexedDB Sessions (Story 9-26b)

Element Web 1.11+ stores `mx_access_token` in **IndexedDB**, not localStorage. Playwright's
`storageState()` captures only localStorage and cookies — not IndexedDB. The token sidecar
pattern solves this:

1. `loginViaOidcBrowser()` intercepts the `POST /_matrix/client/v3/login` response via
   Playwright route interception, captures the `access_token` + `user_id` from the JSON body.
2. The token is written to `e2e/auth-state/{user}.token.json` (the "sidecar" file).
3. `getApiSession()` reads the sidecar to obtain a valid token for Matrix API setup calls
   (`createRoom`, `inviteUser`) — without touching localStorage or IndexedDB.
4. `global-setup.ts` warms sidecars for `alex`, `marie`, and `kai` before any test runs.
5. Each test context performs a **fresh OIDC browser login** (no storageState restore) because
   IndexedDB sessions cannot be injected via `browser.newContext({ storageState })`.

The `auth-state/` directory is gitignored. Sidecars expire after 12 hours (staleness check in
`ensureStorageState()`).

### API Seeding vs. UI Assertion Boundary

Matrix API calls via `page.request` are permitted in **Given/When (setup)** steps only:

| Step type | API calls allowed? | Rationale |
|---|---|---|
| `Given` — test pre-condition | Yes | Set up rooms, send invites, seed data |
| `When` — user action | No (UI only) | The feature under test |
| `Then` — assertion | No (UI only) | Assertions must target the visible UI |

Example for `room/join.feature`: kai creates the room and sends the invite via Matrix API
(`Given`), the assertion is that alex sees the invite banner in Element Web and clicks "Accept"
(`When`/`Then`).

### OIDC Authorization Code + PKCE Requirement

All Gherkin E2E tests use Authorization Code + PKCE. `grant_type=password` (ROPC) is not
supported by Dex v2.41+ and is forbidden in all tests. The consent screen is handled inside
`loginViaOidcBrowser()` (first login only) via a role-based button locator
(`/grant access|allow|approve|confirm/i`).

### Makefile Targets

```bash
make test-e2e-element   # element-web project (bddgen + playwright)
make test-e2e-admin     # admin-ui project (bddgen + playwright)
make test-e2e           # all projects (legacy + BDD)
```

## Thread Aggregations in Sync (Story 9-28)

Matrix spec §11.12 defines "bundled aggregations" — a mechanism for delivering relation metadata
(reply counts, latest replies) inline in a parent event's `unsigned.m.relations` field during sync,
avoiding extra HTTP round-trips from clients.

**How Nebu implements bundled aggregations for `m.thread`:**

1. `attach_thread_aggregations/3` is called for every list of timeline events returned from an
   initial sync or an incremental delta sync in Elixir Core.
2. For each event, it calls `count_thread_children(room_id, event_id)` and (when count > 0)
   fetches the latest reply via `fetch_events_by_relation/5` with `limit = 1` and empty opts.
3. The aggregation struct `%{"m.thread" => %{count: N, latest_event: %{...}, current_user_participated: bool}}`
   is JSON-encoded and stored in `Event.unsigned_relations` (proto field 9).
4. In Go Gateway `sync.go`, `syncUnsigned.MRelations` is populated from `te.GetUnsignedRelations()`
   only when the bytes are non-empty; `json:"m.relations,omitempty"` ensures absent relations
   produce no extra JSON key.

**Database invariant:** The expression index on `events((content->'m.relates_to'->>'event_id'))`
(migration 000042) is required; without it, `attach_thread_aggregations/3` would cause N sequential
scans per sync response (one per timeline event).

**Scope:** Only `m.thread` relation type is aggregated in the current implementation.
The `/relations` endpoint (`GetRelations` RPC) supports any `rel_type` (including empty for all
types) via `fetch_events_by_relation/5`'s dynamic WHERE builder. Element Web typically calls it
for `m.thread`. The function is parameterised on `rel_type` and `event_type` and can be extended
to other relation types (reactions, edits) in a future story.

## Relations API — Query Parameters (Story 9-29)

Story 9-29 extended the `GET /relations` handler with Matrix CS API-compliant query parameters
and two additional route variants:

| Parameter | Type | Default | Validation |
|---|---|---|---|
| `dir` | string | `"b"` | Must be `"f"` or `"b"`; `400 M_BAD_PARAM` otherwise |
| `limit` | integer | 20 | Clamped to 100; 0 uses default |
| `recurse` | boolean | false | Must be parseable; `400 M_BAD_PARAM` otherwise |
| `from` | string | `""` | Opaque pagination token; passed through to Core |

**dir semantics:** `"b"` (backward) = newest-first DESC; `"f"` (forward) = oldest-first ASC.
This mirrors the `GET /messages` semantics from the Matrix CS API spec.

**recurse=true:** Accepted without error (Matrix spec MUST requirement). MVP implementation
treats it the same as `dir=b`; true recursive relation traversal is deferred to Phase 2.

**prev_batch:** When using `dir=b` pagination, the response includes a `prev_batch` token in
`GetRelationsResponse` (field 3) allowing clients to page backwards. Currently always empty
(no prev-page cursor implemented in MVP); the field is present in the proto contract for
forward compatibility.

## Configurable OIDC Claim Mapping (Story 11-10)

Nebu operators can configure which OIDC claims map to Matrix user identity and profile fields
via the Admin UI settings page (`GET/POST /admin/config/claim-mapping`) or during the Bootstrap
Wizard (Step 3). Three keys are stored in `server_config`:

| Key | Default | Purpose |
|---|---|---|
| `oidc_user_id_claim` | `sub` | Claim used as the source for Matrix localpart derivation |
| `oidc_displayname_claim` | `name` | Claim used as the user's display name in Matrix profiles |
| `oidc_email_claim` | `email` | Claim used to populate the user's email profile field |

**Migration 000044** seeds these defaults into `server_config` using `INSERT … ON CONFLICT DO NOTHING`.
Existing deployments that already have these keys set manually are unaffected; new installs receive
the Nebu defaults automatically.

**`FormatUserIDFromClaims` refactored signature (AC6):**

```go
// Old: FormatUserIDFromClaims(sub, name, serverName string) string
// New:
func FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string
```

The function extracts `claims[claimName]` as a string, sanitises it via `sanitiseLocalpart`, and
returns `"@" + sanitised + ":" + serverName`. If the result is empty (claim absent, non-string, or
invalid characters), it falls back to `FormatUserID(sub, serverName)` — the SHA-256 opaque localpart
path — using `claims["sub"]` as input.

**Fallback chain (backward compatibility, AC7):**

1. `NEBU_OIDC_USER_ID_CLAIM` env var (if set) overrides the DB value.
2. `server_config.oidc_user_id_claim` loaded via 60-second TTL cache (`claimLoader` in `main.go`).
3. If missing or empty: fall back to `"name"` claim — identical to the pre-11-10 `FormatUserIDFromClaims(sub, name, serverName)` behavior.
4. If `name` claim also absent: `FormatUserID(sub, serverName)` SHA-256 path.

**TTL cache pattern:** `claimLoader` is constructed in `main.go` as a closure wrapping
`loadServerConfigKey(db, "oidc_user_id_claim")` with a 60-second expiry. The loader is injected into
`JWTMiddleware` (5th parameter) and `LoginHandler`. This avoids a DB round-trip on every request while
keeping the mapping live-updatable without a gateway restart (updated value visible within 60 s).

**Claim name validation:** `oidcClaimNameRe` (`^[a-zA-Z0-9:_\-.]+$`, 1–50 chars) is applied to all
three claim fields on both the Admin UI POST handler and the Bootstrap Wizard Step 3. Invalid inputs
return HTTP 422 with per-field error messages.

**Identity stability warning:** The `oidc_user_id_claim` determines the permanent Matrix user ID.
Changing it post-bootstrap generates different Matrix IDs for existing OIDC principals, breaking all
their room memberships. The Admin UI settings page and Bootstrap Wizard Step 3 both display a
prominent warning banner about this consequence.

**Audit logging:** `ClaimMappingHandler.UpdateHandler` reads the previous values via `LoadClaimMapping`
before persisting and includes `previous_oidc_user_id_claim`, `previous_oidc_displayname_claim`, and
`previous_oidc_email_claim` in the `audit_logs` entry alongside the new values.

## Build Metadata Injection (Story 11-9)

Nebu exposes build metadata (`version`, `git_commit`, `build_time`) via two dedicated `GET /info`
endpoints — one on the Go Gateway public mux (port 8080) and one on the Elixir Core health server
(port 4000). Both endpoints require no authentication and return JSON:

```json
{"component": "gateway", "version": "0.1.0", "git_commit": "abc1234", "build_time": "2026-05-11T10:00:00Z"}
```

**Two injection mechanisms — one per runtime:**

| Component | Mechanism | Fallback |
|---|---|---|
| Go Gateway | `-ldflags "-X main.buildVersion=… -X main.gitCommit=… -X main.buildTime=…"` at `go build` | `"unknown"` (package-level `var` defaults) |
| Elixir Core | `ARG → ENV` in Dockerfile; `System.get_env/2` at call time in `Nebu.BuildInfo.get/0` | `"unknown"` (default arg to `System.get_env/2`) |

**Gateway — static handler pattern:** `NewInfoHandler` in `gateway/internal/health/info.go`
pre-marshals the JSON body at handler construction time (called once during startup). Each
request writes the same pre-computed byte slice — zero allocations per request. The handler
is registered on `pubMux` (port 8080) alongside `/health` and `/ready`, NOT on the
authenticated main mux.

**Core — runtime ENV reads:** `Nebu.BuildInfo.get/0` reads `System.get_env/2` at call time.
Both the builder and runtime Dockerfile stages carry the three `ENV` vars so the values are
available regardless of whether they are baked into the release BEAM files or read at
process start.

**Admin UI footer:** The build vars are threaded into every authenticated admin page render
via `admin.SetBuildInfo(buildVersion, gitCommit, buildTime)` (called once from `main.go`)
and `admin.newPageData()` (called by each authenticated handler). The base template renders:

```
nebu gateway v{{.BuildVersion}} · {{.GitCommit}} · built {{.BuildTime}}
```

The footer is suppressed on the login page (`LoginMode: true`) and on error pages
(`ErrorMode: true`) — both guards use `{{ if not }}` in `layouts/base.html`.

**`make redeploy` integration:** The `redeploy` Makefile target now exports `GIT_COMMIT`
(from `git rev-parse --short HEAD`) and `BUILD_TIME` (from `date -u`) before calling
`docker compose build`. When these are unset (e.g. plain `make redeploy` without env
exports), `docker-compose.yml` passes `${GIT_COMMIT:-unknown}` — images build successfully
and `GET /info` returns `"unknown"` values per AC3.

## Relations API — JSONB Content Normalisation (Story 9-30)

**Bug fix:** Postgrex returns JSONB columns as a `%Postgrex.JSONB{decoded: map}` struct when the
column value is a JSON object. Prior to Story 9-30, `event_map_to_proto/1` in
`Nebu.EventDispatcher.Server` only handled plain Elixir maps and binary strings, causing a
`Protocol.UndefinedError` (gRPC `INTERNAL` / HTTP 500) whenever `/relations` was called on rooms
that stored events with JSONB-typed `content` columns.

**Fix:** A shape-based guard was added as the first branch in the `cond` inside
`event_map_to_proto/1`:

```elixir
is_struct(raw) and Map.has_key?(raw, :decoded) and is_map(raw.decoded) -> raw.decoded
```

This extracts `.decoded` before `Jason.encode!/1`. The guard precedes the `is_map/1` branch
because `%Postgrex.JSONB{}` structs also satisfy `is_map/1` (all structs are maps in Elixir).

**Normalisation contract — all three Postgrex return forms are now handled:**

| Postgrex return form | Guard matched | Action |
|---|---|---|
| `%Postgrex.JSONB{decoded: map}` | `is_struct and has :decoded` | Extract `.decoded` |
| Plain `%{...}` map | `is_map` | Use as-is |
| Binary `"..."` string | `is_binary` | `Jason.decode/1` |

**Shape matching rationale:** `Postgrex` is not a direct dependency of the `event_dispatcher` OTP
application. Matching by module name (`%Postgrex.JSONB{}`) would introduce a compile-time
dependency. The shape guard (`is_struct/1 + Map.has_key?(:decoded) + is_map(.decoded)`) is safe
because the `events.content` column round-trips through PostgreSQL JSONB — no user-supplied term
can produce an arbitrary Elixir struct with a `:decoded` key via this code path (Postgrex returns
native Elixir terms, not ETF-deserialised arbitrary structs).

**Scope:** The fix applies to every call site of `event_map_to_proto/1` — currently
`GetRelations` (Story 9-29) and `GetMessages`. No new endpoints, migrations, gRPC handlers, or
schema changes were introduced.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Cross-Cutting Concerns, §Auth-Token-Flow, §Enforcement; `_bmad-output/planning-artifacts/prd.md`, §Cryptographic Identity Architecture; Story 9-22 (per-device sync token isolation, logout cleanup); Story 9-26 (Browser-First E2E layer, playwright-bdd, token sidecar pattern); Story 9-27 (gRPC error surface rule — GRPC.RPCError vs MatchError; failure audit trail pattern for multi-step operations); Story 9-28 (Thread aggregations in sync, bundled m.thread via unsigned_relations, GetRelations RPC, migration 000042 expression index); Story 9-29 (Relations API query params: dir, recurse, from; base and three-segment routes; prev_batch response field; fetch_events_by_relation/5 dynamic WHERE builder with rel_type + event_type + dir opts); Story 9-30 (JSONB content normalisation bug fix — %Postgrex.JSONB{decoded} struct guard in event_map_to_proto/1; resolves /relations HTTP 500 on JSONB-typed content columns); Story 11-7 (SSO nonce-based replay prevention; Cache-Control: no-store on SSO 302 redirect; denylist check in PostLogin with 403 M_FORBIDDEN; ssoStateStore capacity cap at 10,000; Safari re-login bugfix); Story 11-9 (build metadata injection — ldflags for Go, ARG→ENV for Elixir; NewInfoHandler static pre-marshalled response; Nebu.BuildInfo.get/0; Admin UI footer via SetBuildInfo/newPageData/ErrorMode; make redeploy GIT_COMMIT/BUILD_TIME exports); Story 11-10 (configurable OIDC claim mapping — FormatUserIDFromClaims refactored signature, TTL-cached claimLoader, oidcClaimNameRe validation, migration 000044 defaults seed, identity-stability warning, audit log with previous values, Bootstrap Wizard Step 3, ClaimMappingHandler admin settings page); Story 11-11 (Room GenServer start-on-demand concept — send_receipt/2 lookup_room → start_room fix; {:error,_} wildcard trade-off; :noproc guard pattern; handler inventory for start_room vs lookup_room); Story 12-6 (blurhash pass-through contract — content.info is opaque JSONB, client extension fields preserved verbatim; animated=false MUST NOT return animated thumbnail regression test; explicit traceability tests for AC1+AC2); Story 12.16 (Authenticated media endpoints — auth.Middleware TokenVerifier interface, M_MISSING_TOKEN/M_UNKNOWN_TOKEN error codes, fail-closed nil-verifier guard; config.Handler media config endpoint; CSP + Cross-Origin-Resource-Policy headers on all download/thumbnail 200 responses; v3 unauthenticated + v1 authenticated routing pattern in main.go; {fileName} path value for Content-Disposition)_

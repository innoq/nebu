---
status: in-progress
epic: 12
story: 7
security_review: required
matrix: false
ui: false
---

# Story 12.7: Media Gateway Security Hardening (SEC Gate 2 Fixes)

Status: in-progress

## Story

As a security-conscious system operator,
I want all HIGH security findings from Epic 12's mandatory security review remediated,
So that the media gateway can be safely deployed without unauthenticated DoS, storage abuse, or XSS vectors.

**Size:** L

---

## Context

This story directly addresses the 3 HIGH, 3 MEDIUM, and 5 LOW findings from the Epic 12 SEC Gate 2 security review at `_bmad-output/implementation-artifacts/epic-12-security-review-2026-05-12.md`.

The three HIGH findings together describe a complete attack chain:
- Finding #2 (no JWT validation) allows anonymous storage writes and identity spoofing
- Finding #3 (no Content-Type allowlist) enables stored XSS via attacker-controlled inline content
- Finding #1 (no dimension clamping) allows memory-amplification DoS via thumbnail requests

All three must be fixed before Epic 12 can be marked done.

---

## Acceptance Criteria

### [HIGH-1] Thumbnail dimension clamping

**AC1-1 — Width exceeding cap returns 400:**
Given `?width=2049&height=1`,
When a thumbnail is requested,
Then the response is 400 with `errcode: M_BAD_JSON` (cap = 2048).

**AC1-2 — Height exceeding cap returns 400:**
Given `?width=1&height=2049`,
When a thumbnail is requested,
Then the response is 400 with `errcode: M_BAD_JSON`.

**AC1-3 — Decoded source image exceeding 100 MP returns 400:**
Given a stored image with decoded pixel count > 100,000,000 (100 MP),
When a thumbnail is requested,
Then the response is 400 with `errcode: M_BAD_JSON`.

**AC1-4 — GIF with > 200 frames: only first 200 frames resized, no error:**
Given a GIF with more than 200 frames,
When an animated thumbnail is requested,
Then only the first 200 frames are resized and returned as an animated GIF (no error).

**AC1-5 — Valid dimensions: thumbnail served:**
Given valid `?width=100&height=100`,
When a thumbnail is requested for an existing image,
Then the response is 200 with a thumbnail image.

### [HIGH-2] JWT validation on upload

**AC2-1 — Missing/malformed Bearer token returns 401:**
Given a request with no `Authorization` header or a non-JWT value,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 401 with `errcode: M_UNKNOWN_TOKEN`.

**AC2-2 — Expired JWT returns 401:**
Given a request with an expired JWT in the `Authorization: Bearer` header,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 401 with `errcode: M_UNKNOWN_TOKEN`.

**AC2-3 — Denylisted JWT returns 401:**
Given a request with a cryptographically valid but denylisted JWT,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 401 with `errcode: M_UNKNOWN_TOKEN`.

**AC2-4 — Valid JWT: upload succeeds and uploader_user_id matches claim:**
Given a valid JWT from the OIDC provider,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 200 and the stored `uploader_user_id` equals the verified `sub`/`name` claim value.

### [HIGH-3] Content-Type allowlist + safe disposition

**AC3-1 — Upload with `text/html` blocked:**
Given an upload with `Content-Type: text/html`,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 400 with `errcode: M_BAD_JSON`.

**AC3-2 — Upload with `image/svg+xml` blocked:**
Given an upload with `Content-Type: image/svg+xml`,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 400 with `errcode: M_BAD_JSON`.

**AC3-3 — Upload with `application/javascript` blocked:**
Given an upload with `Content-Type: application/javascript`,
When `POST /_matrix/media/v3/upload` is called,
Then the response is 400 with `errcode: M_BAD_JSON`.

**AC3-4 — Download of safe type returns correct headers:**
Given a stored file with `Content-Type: image/png`,
When `GET /_matrix/media/v3/download/{serverName}/{mediaId}` is called,
Then the response includes `Content-Type: image/png`, `Content-Disposition: inline`, and `X-Content-Type-Options: nosniff`.

**AC3-5 — All download responses include nosniff header:**
Given any download request,
When `GET /_matrix/media/v3/download/{serverName}/{mediaId}` is called,
Then the response always includes `X-Content-Type-Options: nosniff`.

**AC3-6 — Download of non-safe stored type forces attachment:**
Given a file stored before the allowlist with a non-safe content type (e.g. `text/html`),
When `GET /_matrix/media/v3/download/{serverName}/{mediaId}` is called,
Then the response uses `Content-Type: application/octet-stream` and `Content-Disposition: attachment`.

### [MEDIUM-4] createbuckets entrypoint keeps secrets off argv

**AC4-1 — Secrets not in process argv:**
Given the `createbuckets` container during init,
When `cat /proc/1/cmdline` is examined,
Then it does NOT contain literal secret values from `minio_root_password` or `minio_app_secret_key`.

### [MEDIUM-5] server_config RLS UPDATE policy scoped to mutable keys

**AC5-1 — Immutable key update rejected:**
Given `UPDATE server_config SET value='evil' WHERE key='server_name'` executed as `nebu_app`,
When the query runs,
Then an RLS violation is raised.

**AC5-2 — Mutable key update succeeds:**
Given `UPDATE server_config SET value='email' WHERE key='oidc_user_id_claim'` executed as `nebu_app`,
When the query runs,
Then the update succeeds.

### [MEDIUM-6] MinIO + mc image bump to 2026

**AC6-1 — docker-compose uses 2026 MinIO release:**
Given `docker-compose.yml`,
When the `minio/minio` image tag is inspected,
Then it contains a `RELEASE.2026-*` date string.

**AC6-2 — docker-compose uses 2026 mc release:**
Given `docker-compose.yml`,
When the `minio/mc` image tag is inspected,
Then it contains a `RELEASE.2026-*` date string.

### [LOW fixes — inline]

**AC7 — Constant-time nonce comparison:**
`gateway/internal/matrix/sso.go` uses `subtle.ConstantTimeCompare` for nonce comparison.

**AC8 — Nonce prefix removed from error log:**
`gateway/internal/matrix/sso.go` does not log `want_prefix` field on nonce mismatch.

**AC9 — Env precedence: file takes priority over plain env:**
`media/cmd/media/main.go` reads `NEBU_MINIO_*_KEY_FILE` first; plain `NEBU_MINIO_*_KEY` only used as fallback when file is not set.

**AC10 — HTTP server with timeouts:**
`media/cmd/media/main.go` uses `http.Server` with `ReadHeaderTimeout: 10s`, `ReadTimeout: 60s`, `WriteTimeout: 120s`, `IdleTimeout: 120s`.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1 — Thumbnail dimension cap: width > 2048 [thumbnail/handler_test.go]**
- Given: request with `?width=2049&height=1`
- When: `ServeHTTP` called
- Then: 400, errcode `M_BAD_JSON`

**AT-2 — Thumbnail dimension cap: height > 2048 [thumbnail/handler_test.go]**
- Given: request with `?width=1&height=2049`
- When: `ServeHTTP` called
- Then: 400, errcode `M_BAD_JSON`

**AT-3 — Thumbnail source image > 100 MP rejected [thumbnail/thumbnail_test.go]**
- Given: `GenerateThumbnail` called with a synthetically large image (> 100 MP pixel count)
- When: function executes
- Then: returns error indicating image too large

**AT-4 — GIF frame cap: 201 frames → only first 200 processed [thumbnail/thumbnail_test.go]**
- Given: GIF with 201 frames
- When: `generateAnimatedGIFThumbnail` called
- Then: output GIF has exactly 200 frames (no error)

**AT-5 — Upload with text/html blocked [upload/upload_test.go]**
- Given: valid JWT, body with `Content-Type: text/html`
- When: POST upload
- Then: 400, errcode `M_BAD_JSON`

**AT-6 — Upload with image/svg+xml blocked [upload/upload_test.go]**
- Given: valid JWT, body with `Content-Type: image/svg+xml`
- When: POST upload
- Then: 400, errcode `M_BAD_JSON`

**AT-7 — Upload with application/javascript blocked [upload/upload_test.go]**
- Given: valid JWT, body with `Content-Type: application/javascript`
- When: POST upload
- Then: 400, errcode `M_BAD_JSON`

**AT-8 — Download always sets X-Content-Type-Options: nosniff [download/download_test.go]**
- Given: any stored file
- When: download request
- Then: response includes `X-Content-Type-Options: nosniff`

**AT-9 — Download of non-safe stored type forces attachment [download/download_test.go]**
- Given: file stored with `Content-Type: text/html` (pre-allowlist)
- When: download request
- Then: `Content-Type: application/octet-stream`, `Content-Disposition: attachment`

**AT-10 — Download of safe type: inline disposition preserved [download/download_test.go]**
- Given: file stored with `Content-Type: image/png`
- When: download request
- Then: `Content-Type: image/png`, `Content-Disposition: inline`, `X-Content-Type-Options: nosniff`

**AT-11 — JWT upload: missing bearer → 401 [upload/upload_test.go]**
- Given: no Authorization header
- When: POST upload
- Then: 401, errcode `M_UNKNOWN_TOKEN`

**AT-12 — JWT upload: expired token → 401 [upload/upload_test.go]**
- Given: expired JWT
- When: POST upload
- Then: 401, errcode `M_UNKNOWN_TOKEN`

**AT-13 — migration 000046: immutable key update rejected [gateway/migrations/]*
- Given: migration applied
- When: `UPDATE server_config SET value='evil' WHERE key='server_name'` as `nebu_app`
- Then: RLS violation error

**AT-14 — migration 000046: mutable key update succeeds**
- Given: migration applied
- When: `UPDATE server_config SET value='email' WHERE key='oidc_user_id_claim'` as `nebu_app`
- Then: succeeds (1 row updated)

---

## Technical Notes

### [HIGH-1] Thumbnail clamping — `media/internal/thumbnail/`

`handler.go:96-118`: Add `const maxThumbDim = 2048` guard after positive-integer check.

`thumbnail.go`: After `imaging.Decode`, check `src.Bounds().Dx() * src.Bounds().Dy() > 100_000_000` and return error.

`thumbnail.go:generateAnimatedGIFThumbnail`: Cap `g.Image` slice to first 200 frames before the resize loop.

### [HIGH-2] JWT validation — `media/internal/upload/upload.go`

The media gateway does not have its own OIDC provider. Use `go-oidc/v3` directly — same library already in `gateway/go.mod`. The upload handler needs:
- `NEBU_OIDC_ISSUER` env var (add to docker-compose.yml media service)
- `NEBU_OIDC_CLIENT_ID` env var
- Inline verification: `provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(ctx, rawToken)`
- Extract `sub` claim as `uploaderUserID`
- Error cases: missing bearer → 401 `M_MISSING_TOKEN`, bad signature/expired → 401 `M_UNKNOWN_TOKEN`

The media service will need OIDC provider initialization at startup. Add `OIDCVerifier` field to `HandlerConfig`.

For the denylist check: the media service does not have access to the gateway's in-memory denylist. Skip denylist checking in the media handler for now — note as accepted limitation. (The JWT expiry + signature check satisfies the HIGH finding; denylist is belt-and-suspenders.)

### [HIGH-3] Content-Type allowlist — `media/internal/upload/upload.go` + `media/internal/download/handler.go`

**Upload blocklist** (reject at upload time):
```go
var blockedContentTypes = map[string]bool{
    "text/html":                true,
    "application/xhtml+xml":    true,
    "text/javascript":          true,
    "application/javascript":   true,
    "image/svg+xml":            true,
    "application/x-shockwave-flash": true,
}
```
Normalize: strip params before lookup (`strings.Split(ct, ";")[0]`).

**Download safe-inline allowlist**:
```go
var safeInlineTypes = map[string]bool{
    "image/jpeg": true, "image/png": true, "image/gif": true,
    "image/webp": true, "image/avif": true,
    "audio/mpeg": true, "audio/ogg": true,
    "video/mp4": true, "video/webm": true,
    "application/pdf": true, "text/plain": true,
}
```
If not in safe list: `Content-Type: application/octet-stream`, `Content-Disposition: attachment; filename=...`.
Always set `X-Content-Type-Options: nosniff`.

### [MEDIUM-4] createbuckets entrypoint — `docker-compose.yml`

Replace `$(cat /run/secrets/...)` as mc args with `MC_HOST_minio` env var form:
```yaml
entrypoint: |
  /bin/sh -c '
  export MC_HOST_minio="http://$(cat /run/secrets/minio_root_user):$(cat /run/secrets/minio_root_password)@minio:9000"
  mc mb --ignore-existing minio/nebu-media
  APP_KEY=$(cat /run/secrets/minio_app_access_key)
  APP_SECRET=$(cat /run/secrets/minio_app_secret_key)
  printf "%s\n%s\n" "$APP_KEY" "$APP_SECRET" | mc admin user add minio --stdin || true
  ...
  '
```

### [MEDIUM-5] server_config RLS — new migration `000046_server_config_scope_update_policy`

```sql
DROP POLICY IF EXISTS config_update_all ON server_config;
CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING  (key IN ('oidc_user_id_claim','oidc_displayname_claim','oidc_email_claim',
                    'admin_group_claim','oidc_issuer','oidc_client_id','oidc_client_secret'))
    WITH CHECK (key IN ('oidc_user_id_claim','oidc_displayname_claim','oidc_email_claim',
                        'admin_group_claim','oidc_issuer','oidc_client_id','oidc_client_secret'));
```

### [MEDIUM-6] MinIO bump — `docker-compose.yml`

Target: `RELEASE.2026-04-18T19-53-40Z` for MinIO, `RELEASE.2026-04-18T09-06-52Z` for mc (or latest 2026 stable available).

### [LOW-7+8] SSO nonce — `gateway/internal/matrix/sso.go:411-413`

Replace `!=` with `subtle.ConstantTimeCompare`. Remove `want_prefix` log field.

### [LOW-9] Env precedence — `media/cmd/media/main.go:124-144`

Flip: read `_FILE` first. If file key is set and non-empty, use it. Only fall back to plain env var if no file configured.

### [LOW-10] HTTP server timeouts — `media/cmd/media/main.go:211`

```go
srv := &http.Server{
    Addr:              listenAddr,
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    ReadTimeout:       60 * time.Second,
    WriteTimeout:      120 * time.Second,
    IdleTimeout:       120 * time.Second,
}
```

---

## Out of Scope

- Finding #11 (io.ReadAll memory amplification): deferred to a future story (requires streaming GCM chunking, non-trivial).
- Rate limiting on media gateway endpoints: deferred to future infrastructure story.
- Denylist check in media upload: deferred — media service has no shared memory with gateway; JWT expiry satisfies the HIGH finding.

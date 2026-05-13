---
id: "12-16"
epic: 12
title: "Authenticated Matrix Media Endpoints (/_matrix/client/v1/media/*)"
status: review
security_review: required
ui: false
matrix: true
created: 2026-05-13
---

# Story 12.16 — Authenticated Matrix Media Endpoints (/_matrix/client/v1/media/*)

## User Story

As an Element Web user,
I want to upload and view file attachments in chat rooms via the authenticated media endpoints,
so that images and files are correctly displayed in the Element Web UI using Matrix spec v1.11+ authenticated media APIs.

## Background

The Oracle identified this gap post-Epic 12 (session log 2026-05-13). The media gateway currently only implements the deprecated unauthenticated `/_matrix/media/v3/` endpoints. Since Matrix CS API v1.11, downloads and thumbnails moved to authenticated `/_matrix/client/v1/media/` paths. Since v1.12, servers **SHOULD** "freeze" unauthenticated access for newly uploaded media.

Modern Element Web (v1.11+ aware) will:
1. Call `GET /_matrix/client/v1/media/config` before uploading to check the max upload size → **not implemented → upload dialog may fail**
2. Use `GET /_matrix/client/v1/media/download/...` to load images → **not implemented → attachments not displayed**
3. Use `GET /_matrix/client/v1/media/thumbnail/...` to load thumbnails → **not implemented → thumbnails broken**

Additionally, the existing `download` and `thumbnail` handlers are missing two SHOULD headers per Matrix spec §Media Repository:
- `Content-Security-Policy: sandbox; default-src 'none'; ...`
- `Cross-Origin-Resource-Policy: cross-origin` (added v1.4)

## Acceptance Criteria

**[AC-1] GET /_matrix/media/v3/config — unauthenticated deprecated config**
Given no Authorization header,
When `GET /_matrix/media/v3/config` is called,
Then the response is 200 JSON `{"m.upload.size": <NEBU_MEDIA_MAX_UPLOAD_BYTES>}`.

**[AC-2] GET /_matrix/client/v1/media/config — authenticated config**
Given a valid Bearer token in Authorization header,
When `GET /_matrix/client/v1/media/config` is called,
Then the response is 200 JSON `{"m.upload.size": <NEBU_MEDIA_MAX_UPLOAD_BYTES>}`.

**[AC-3] GET /_matrix/client/v1/media/config — 401 without token**
Given no Authorization header,
When `GET /_matrix/client/v1/media/config` is called,
Then the response is 401 JSON `{"errcode":"M_MISSING_TOKEN","error":"..."}`.

**[AC-4] GET /_matrix/client/v1/media/download/{serverName}/{mediaId} — authenticated download**
Given a valid Bearer token and an existing media item,
When `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}` is called,
Then the response is 200 with the decrypted media bytes, correct Content-Type and Content-Disposition headers.

**[AC-5] GET /_matrix/client/v1/media/download — 401 without token**
Given no Authorization header,
When `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}` is called,
Then the response is 401 JSON `{"errcode":"M_MISSING_TOKEN","error":"..."}`.

**[AC-6] GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName} — filename variant**
Given a valid Bearer token and an existing media item,
When `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/photo.jpg` is called,
Then Content-Disposition header contains `filename="photo.jpg"` (the URL-provided filename, not the mediaId).

**[AC-7] GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId} — authenticated thumbnail**
Given a valid Bearer token and an existing image media item,
When `GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId}?width=96&height=96` is called,
Then the response is 200 with thumbnail bytes and correct Content-Type.

**[AC-8] GET /_matrix/client/v1/media/thumbnail — 401 without token**
Given no Authorization header,
When `GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId}?width=96&height=96` is called,
Then the response is 401 JSON `{"errcode":"M_MISSING_TOKEN","error":"..."}`.

**[AC-9] CSP + CORP headers on all download and thumbnail responses (v3 + v1)**
Given any successful download or thumbnail response (authenticated or unauthenticated path),
When the handler writes the 200 response,
Then the response includes:
- `Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';`
- `Cross-Origin-Resource-Policy: cross-origin`

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestConfigHandler_Unauthenticated_ReturnsMaxBytes` — Go unit test (`media/internal/config/handler_test.go`)
   - Given: Handler configured with MaxBytes = 52428800
   - When: `GET /_matrix/media/v3/config` (no auth)
   - Then: 200, `{"m.upload.size": 52428800}`

2. `TestAuthMiddleware_MissingToken_Returns401` — Go unit test (`media/internal/auth/middleware_test.go`)
   - Given: wrapped handler that would return 200
   - When: request with no Authorization header
   - Then: 401, `{"errcode":"M_MISSING_TOKEN",...}`, inner handler NOT called

3. `TestAuthMiddleware_InvalidToken_Returns401` — Go unit test (`media/internal/auth/middleware_test.go`)
   - Given: mock verifier that returns an error
   - When: request with `Authorization: Bearer bad-token`
   - Then: 401, `{"errcode":"M_UNKNOWN_TOKEN",...}`, inner handler NOT called

4. `TestAuthMiddleware_ValidToken_Passes` — Go unit test (`media/internal/auth/middleware_test.go`)
   - Given: mock verifier that returns success
   - When: request with `Authorization: Bearer good-token`
   - Then: inner handler called (receives the request), response from inner handler returned

5. `TestDownloadHandler_CSPandCORPHeaders` — Go unit test (`media/internal/download/download_test.go`)
   - Given: successful download request
   - When: `GET /_matrix/media/v3/download/{serverName}/{mediaId}`
   - Then: response contains `Content-Security-Policy` and `Cross-Origin-Resource-Policy: cross-origin` headers

6. `TestDownloadHandler_FileNameVariant` — Go unit test (`media/internal/download/download_test.go`)
   - Given: successful download request with `{fileName}` path value = `"photo.jpg"`
   - When: handler is called with PathValue("fileName") = "photo.jpg"
   - Then: Content-Disposition contains `filename="photo.jpg"` (not the mediaId)

7. `TestThumbnailHandler_CSPandCORPHeaders` — Go unit test (`media/internal/thumbnail/handler_test.go`)
   - Given: successful thumbnail request
   - When: `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}?width=100&height=100`
   - Then: response contains `Content-Security-Policy` and `Cross-Origin-Resource-Policy: cross-origin` headers

## Tasks / Subtasks

- [x] Create `media/internal/auth/middleware.go` (AC-3, AC-5, AC-8)
  - [x] Define local `TokenVerifier` interface (same signature as `upload.TokenVerifier` — no import of upload package)
  - [x] Implement `Middleware` struct with `Wrap(http.Handler) http.Handler`
  - [x] Missing `Authorization` header → 401 `M_MISSING_TOKEN`
  - [x] Invalid token (verifier error) → 401 `M_UNKNOWN_TOKEN`
  - [x] Valid token → delegate to wrapped handler

- [x] Create `media/internal/auth/middleware_test.go` (AT-2, AT-3, AT-4)
  - [x] `TestAuthMiddleware_MissingToken_Returns401`
  - [x] `TestAuthMiddleware_InvalidToken_Returns401`
  - [x] `TestAuthMiddleware_ValidToken_Passes`

- [x] Create `media/internal/config/handler.go` (AC-1, AC-2)
  - [x] `Handler` struct with `MaxBytes int64`
  - [x] `ServeHTTP` returns `{"m.upload.size": h.MaxBytes}` as JSON
  - [x] No auth logic — auth is applied via middleware at routing level

- [x] Create `media/internal/config/handler_test.go` (AT-1)
  - [x] `TestConfigHandler_ReturnsMaxBytes`

- [x] Modify `media/internal/download/handler.go` (AC-6, AC-9)
  - [x] Add `Content-Security-Policy` and `Cross-Origin-Resource-Policy: cross-origin` to `ServeHTTP` response headers (before `WriteHeader`)
  - [x] Use `r.PathValue("fileName")` for Content-Disposition filename when non-empty (AC-6); fall back to `mediaID`

- [x] Modify `media/internal/download/download_test.go` (AT-5, AT-6)
  - [x] `TestDownloadHandler_CSPandCORPHeaders`
  - [x] `TestDownloadHandler_FileNameVariant`

- [x] Modify `media/internal/thumbnail/handler.go` (AC-9)
  - [x] Add `Content-Security-Policy` and `Cross-Origin-Resource-Policy: cross-origin` to response headers

- [x] Modify `media/internal/thumbnail/handler_test.go` (AT-7)
  - [x] `TestThumbnailHandler_CSPandCORPHeaders`

- [x] Modify `media/cmd/media/main.go` (AC-1 through AC-8)
  - [x] Import `media/internal/auth` and `media/internal/config`
  - [x] Create `configHandler := config.NewHandler(config.HandlerConfig{MaxBytes: maxBytes})`
  - [x] Create `authMW := auth.New(uploadVerifier)`
  - [x] Register new routes (see Dev Notes below)
  - [x] Existing v3 routes remain unchanged (no regressions)

- [x] Run `make test-unit-go` — all new + existing tests GREEN

### Review Follow-ups (AI)

- [x] [AI-Review] F-A · MINOR · Empty Bearer token must return 401 M_MISSING_TOKEN (not M_UNKNOWN_TOKEN)
- [x] [AI-Review] F-B · MINOR · `TestDownloadHandler_FileNameVariant` must assert disposition-type
- [x] [AI-Review] F-C · MINOR · v1 route must be exercised for CSP/CORP headers

## Dev Notes

### New Package Structure

```
media/internal/
  auth/
    middleware.go       ← NEW: OIDC auth middleware (consumer-defined TokenVerifier)
    middleware_test.go  ← NEW
  config/
    handler.go          ← NEW: GET /media/config response
    handler_test.go     ← NEW
  download/
    handler.go          ← MODIFY: add CSP/CORP headers + fileName path value
    download_test.go    ← MODIFY: add 2 tests
  thumbnail/
    handler.go          ← MODIFY: add CSP/CORP headers
    handler_test.go     ← MODIFY: add 1 test
```

### auth.Middleware — Interface Definition

**DO NOT import `media/internal/upload` in the auth package.** Define a local `TokenVerifier` interface:

```go
// media/internal/auth/middleware.go
package auth

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
)

// TokenVerifier abstracts JWT/OIDC token verification.
// *upload.OIDCTokenVerifier satisfies this interface structurally.
type TokenVerifier interface {
    VerifyToken(ctx context.Context, rawToken string) (string, error)
}

type Middleware struct {
    verifier TokenVerifier
}

func New(v TokenVerifier) *Middleware {
    return &Middleware{verifier: v}
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusUnauthorized)
            _ = json.NewEncoder(w).Encode(map[string]string{
                "errcode": "M_MISSING_TOKEN",
                "error":   "Missing or invalid access token",
            })
            return
        }
        rawToken := strings.TrimPrefix(authHeader, "Bearer ")
        if _, err := m.verifier.VerifyToken(r.Context(), rawToken); err != nil {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusUnauthorized)
            _ = json.NewEncoder(w).Encode(map[string]string{
                "errcode": "M_UNKNOWN_TOKEN",
                "error":   "Invalid or expired access token",
            })
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### config.Handler

```go
// media/internal/config/handler.go
package config

import (
    "encoding/json"
    "net/http"
)

type HandlerConfig struct {
    MaxBytes int64
}

type Handler struct {
    maxBytes int64
}

func NewHandler(cfg HandlerConfig) *Handler {
    return &Handler{maxBytes: cfg.MaxBytes}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]interface{}{
        "m.upload.size": h.maxBytes,
    })
}
```

### Download Handler Modifications

Add these two header lines near the top of the 200-response path in `ServeHTTP`, **before** any `WriteHeader` call (currently after the decrypt step, around line 144):

```go
// Matrix CS API §Media Repository — SHOULD headers (v1.4+)
w.Header().Set("Content-Security-Policy",
    "sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';")
w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
```

For the filename path value (AC-6), modify the Content-Disposition line. Current code (download/handler.go ~line 155):
```go
w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", mediaID))
```

Replace with:
```go
cdName := r.PathValue("fileName")
if cdName == "" {
    cdName = mediaID
}
w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", cdName))
```

When the route doesn't include `{fileName}`, `r.PathValue("fileName")` returns `""`, so the fallback to `mediaID` is safe.

### Thumbnail Handler Modifications

Add the same CSP/CORP header lines to `thumbnail/handler.go` (around line 228, after the `X-Content-Type-Options` line):
```go
w.Header().Set("Content-Security-Policy",
    "sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';")
w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
```

### main.go Route Registration

**After** creating `uploadVerifier` (already done in existing code), add:

```go
import (
    "github.com/nebu/nebu/media/internal/auth"
    "github.com/nebu/nebu/media/internal/config"
)

// In main(), after uploadVerifier is initialized:
configHandler := config.NewHandler(config.HandlerConfig{MaxBytes: maxBytes})
authMW := auth.New(uploadVerifier)

// Add to mux (after the existing v3 routes):
// Deprecated unauthenticated config (v3)
mux.Handle("GET /_matrix/media/v3/config", downloadRL(configHandler))
// Authenticated media endpoints (v1.11+)
mux.Handle("GET /_matrix/client/v1/media/config",
    downloadRL(authMW.Wrap(configHandler)))
mux.Handle("GET /_matrix/client/v1/media/download/{serverName}/{mediaId}",
    downloadRL(authMW.Wrap(downloadHandler)))
mux.Handle("GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}",
    downloadRL(authMW.Wrap(downloadHandler)))
mux.Handle("GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId}",
    downloadRL(authMW.Wrap(thumbnailHandler)))
```

**Existing v3 routes are unchanged** — no regressions.

### Spec References (Matrix CS API v1.18)

| Endpoint | Spec Section | Auth | Notes |
|----------|-------------|------|-------|
| `GET /_matrix/client/v1/media/config` | §get_matrixclientv1mediaconfig | Yes | Returns `{"m.upload.size": N}` |
| `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}` | §get_matrixclientv1mediadownloadservernamemediaid | Yes | 200/307/308/429/502/504 |
| `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}` | Same section | Yes | fileName in Content-Disposition |
| `GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId}` | §get_matrixclientv1mediathumbnailservernamemediaid | Yes | Same params as v3 |
| `GET /_matrix/media/v3/config` | §get_matrixmediav3config (deprecated) | No | Backward compat |
| CSP header | §Media Repository | SHOULD | Per spec v1.18 |
| CORP header | §Media Repository, added v1.4 | SHOULD | `cross-origin` value |

Error codes for auth failures:
- Missing Authorization header: `M_MISSING_TOKEN` (401)
- Invalid/expired token: `M_UNKNOWN_TOKEN` (401)

### Go Module Note

No new external dependencies needed. `auth` and `config` packages use only stdlib + existing project interfaces.

### OIDC Verifier Reuse

`uploadVerifier` from `initOIDCVerifier(...)` is the same instance passed to `auth.New(uploadVerifier)`. The `*upload.OIDCTokenVerifier` satisfies `auth.TokenVerifier` structurally (same `VerifyToken` method signature). No type assertion needed.

### Regression Guard

Existing tests in `media/internal/download/download_test.go` and `media/internal/thumbnail/handler_test.go` will break if CSP/CORP headers are not present — add assertions for these headers to the EXISTING happy-path tests as part of this story (not just new tests). This ensures future regressions are caught.

### Security Note

This story adds OIDC token verification to previously unauthenticated download/thumbnail paths (via middleware). Kassandra-level attention expected on:
- Auth middleware fail-open risk (must be fail-closed: if verifier is nil, the Wrap function should refuse → but `uploadVerifier` is never nil in production due to Story 12.8)
- Token reuse across endpoints (same verifier = same OIDC audience check = correct)
- No user identity stored for download/thumbnail (config + read-only endpoints don't need audit trail)

### Project Structure Notes

- New packages: `media/internal/auth/`, `media/internal/config/`
- Modified files: `media/internal/download/handler.go`, `media/internal/thumbnail/handler.go`, `media/cmd/media/main.go`
- No new SQL migrations
- No Elixir/Core changes

### References

- Oracle session log: `_bmad/memory/nebu-agent-oracle/sessions/2026-05-13.md`
- Matrix CS API v1.18 §Media Repository: local spec at `.claude/skills/nebu-agent-oracle/matrix-spec/v1.18/client-server-api/index.html` lines 31788–33400
- `media/cmd/media/main.go` — route registration (lines 270–278)
- `media/internal/upload/upload.go` — `TokenVerifier` interface (lines 28–32), `OIDCTokenVerifier` (lines 39–67)
- `media/internal/download/handler.go` — existing CSP gap (lines 146–163)
- `media/internal/thumbnail/handler.go` — existing CSP gap (lines 227–235)
- Story 12.8 — fail-closed OIDC: `uploadVerifier` is never nil in production

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

None — implementation followed Dev Notes spec directly.

### Completion Notes List

- Created `media/internal/auth/middleware.go`: TokenVerifier interface + Middleware with fail-closed nil-verifier guard (503 M_UNAVAILABLE), M_MISSING_TOKEN (no/non-Bearer header), M_UNKNOWN_TOKEN (verifier error), pass-through on valid token.
- Created `media/internal/config/handler.go`: Handler that returns `{"m.upload.size": maxBytes}` JSON; no auth logic (middleware applied at routing layer).
- Modified `media/internal/download/handler.go`: Added CSP and CORP headers before WriteHeader; added r.PathValue("fileName") fallback for Content-Disposition (AC-6).
- Modified `media/internal/thumbnail/handler.go`: Added CSP and CORP headers before WriteHeader.
- Modified `media/cmd/media/main.go`: Wired configHandler + authMW; registered 5 new routes (v3/config unauthenticated, v1/media/config authenticated, v1/media/download/{serverName}/{mediaId}, v1/media/download/{serverName}/{mediaId}/{fileName}, v1/media/thumbnail/{serverName}/{mediaId}). All existing v3 routes preserved (no regressions).
- All 16 new tests green; all pre-existing tests pass. `make test-unit-go` clean.
- Review cycle 1 fixes: (F-A) Added empty Bearer token → M_MISSING_TOKEN guard in middleware.go + test; (F-B) Expanded TestDownloadHandler_FileNameVariant into two sub-cases asserting inline/attachment disposition-type for safe/unsafe content types; (F-C) Added TestV1DownloadRoute_CSPandCORPHeaders and TestV1ThumbnailRoute_CSPandCORPHeaders in main_test.go exercising the full v1 routing stack with authMW.Wrap. All tests green.

### File List

- `media/internal/auth/middleware.go` (MODIFIED — empty Bearer token guard)
- `media/internal/auth/middleware_test.go` (MODIFIED — TestAuthMiddleware_EmptyBearerToken_Returns401MissingToken added)
- `media/internal/config/handler.go` (NEW)
- `media/internal/config/handler_test.go` (staged, ATDD pre-existing)
- `media/internal/download/handler.go` (MODIFIED — CSP/CORP headers + fileName path value)
- `media/internal/download/download_test.go` (MODIFIED — TestDownloadHandler_FileNameVariant expanded with disposition-type sub-cases)
- `media/internal/thumbnail/handler.go` (MODIFIED — CSP/CORP headers)
- `media/internal/thumbnail/handler_test.go` (staged, ATDD pre-existing)
- `media/cmd/media/main.go` (MODIFIED — new imports + 5 new routes)
- `media/cmd/media/main_test.go` (MODIFIED — F-C tests: TestV1DownloadRoute_CSPandCORPHeaders, TestV1ThumbnailRoute_CSPandCORPHeaders)

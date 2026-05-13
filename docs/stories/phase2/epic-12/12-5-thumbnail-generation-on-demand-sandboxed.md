---
status: review
epic: 12
story: 5
security_review: required
matrix: true
ui: false
---

# Story 12.5: Thumbnail Generation — On-Demand, Sandboxed

Status: review

## Story

As a Matrix client user,
I want `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` to return a correctly sized thumbnail,
So that avatars and image previews display correctly in Element Web and other clients.

**Size:** M

---

## Acceptance Criteria

**AC1 — Scale method returns aspect-ratio-preserved thumbnail ≤ requested dimensions:**

Given a JPEG image is stored in MinIO (encrypted AES-256-GCM, as stored by upload handler),
When `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}?width=100&height=100&method=scale` is called,
Then the response is a JPEG image with aspect-ratio-preserved dimensions ≤ 100×100 and `Content-Type: image/jpeg`.

**AC2 — Crop method returns exactly the requested dimensions:**

Given `method=crop` is specified with `width=100&height=100`,
When the thumbnail is generated,
Then the output is exactly 100×100 pixels (center-cropped, not aspect-ratio-preserved).

**AC3 — Rejected MIME types return 400 M_BAD_JSON (magic bytes detection):**

Given an SVG, PDF, PS, or EPS file is stored and a thumbnail is requested,
When the request is processed,
Then the server returns `400 M_BAD_JSON` with `{"errcode":"M_BAD_JSON","error":"Unsupported media type for thumbnail generation"}`.
The MIME type is determined from the first 512 bytes of the decrypted file (magic bytes), NOT from the `Content-Type` header stored at upload time.

**AC4 — Image processing is sandboxed (no network, no shell exec):**

Given the thumbnail service processes an uploaded image,
When the image processing runs,
Then it executes using a pure Go library (no cgo, no shell exec, no external process) — sandboxed by construction.
Library: `github.com/disintegration/imaging` (MIT, pure Go, no cgo).

**AC5 — Cache-Control header is set on thumbnail responses:**

Given a thumbnail is generated successfully,
When the response headers are inspected,
Then `Cache-Control: max-age=86400` is present.

**AC6 — Animated GIF with animated=true returns animated GIF:**

Given `animated=true` is passed and the source is a GIF (detected via magic bytes),
When the thumbnail is generated,
Then the response is an animated GIF (all frames preserved) — not a static first frame.
`Content-Type: image/gif`.

**AC7 — Missing width or height returns 400 M_BAD_JSON:**

Given `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` is called without `width` or `height` query params,
When the request is processed,
Then the server returns `400 M_BAD_JSON` with `{"errcode":"M_BAD_JSON","error":"width and height query parameters are required"}`.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1** — `TestThumbnailHandler_Scale_ReturnsThumbnail` in `media/internal/thumbnail/thumbnail_test.go` [Go unit test]
- Given: A 200×300 JPEG image bytes (synthetic, created programmatically)
- When: `GenerateThumbnail(ctx, imgBytes, ThumbnailParams{Width: 100, Height: 100, Method: "scale"})` is called
- Then: Returns JPEG bytes with dimensions ≤ 100×100 and aspect ratio preserved (200×300 → 67×100)

**AT-2** — `TestThumbnailHandler_Crop_ReturnsExactDimensions` in `media/internal/thumbnail/thumbnail_test.go` [Go unit test]
- Given: A 200×300 JPEG image bytes
- When: `GenerateThumbnail(ctx, imgBytes, ThumbnailParams{Width: 100, Height: 100, Method: "crop"})` is called
- Then: Returns JPEG bytes with exactly 100×100 dimensions

**AT-3** — `TestThumbnailHandler_RejectedMimeType_ReturnsBadJSON` in `media/internal/thumbnail/thumbnail_test.go` [Go unit test]
- Given: SVG bytes starting with `<svg` (magic bytes), PDF bytes starting with `%PDF`
- When: `DetectMIMEType(bytes)` is called on each
- Then: Returns `"image/svg+xml"` for SVG and `"application/pdf"` for PDF (not an image type → rejected)

**AT-4** — `TestThumbnailHTTPHandler_MissingParams_Returns400` in `media/internal/thumbnail/handler_test.go` [Go HTTP unit test]
- Given: A registered thumbnail handler
- When: `GET /_matrix/media/v3/thumbnail/test.local/abc123` (no width/height params)
- Then: HTTP 400 with `{"errcode":"M_BAD_JSON","error":"width and height query parameters are required"}`

**AT-5** — `TestThumbnailHTTPHandler_CacheControl` in `media/internal/thumbnail/handler_test.go` [Go HTTP unit test]
- Given: A valid JPEG stored under `test.local/abc123`, thumbnail handler wired
- When: `GET /_matrix/media/v3/thumbnail/test.local/abc123?width=100&height=100&method=scale`
- Then: Response has `Cache-Control: max-age=86400` header

**AT-6** — `TestThumbnailHTTPHandler_UnsupportedType_Returns400` in `media/internal/thumbnail/handler_test.go` [Go HTTP unit test]
- Given: SVG bytes stored under `test.local/svg-file` (encrypted in storage)
- When: `GET /_matrix/media/v3/thumbnail/test.local/svg-file?width=100&height=100`
- Then: HTTP 400 with `{"errcode":"M_BAD_JSON","error":"Unsupported media type for thumbnail generation"}`

**AT-7** — `TestThumbnailHTTPHandler_NotFound_Returns404` in `media/internal/thumbnail/handler_test.go` [Go HTTP unit test]
- Given: `mediaId` not in DB
- When: `GET /_matrix/media/v3/thumbnail/test.local/notexist?width=100&height=100`
- Then: HTTP 404 with `{"errcode":"M_NOT_FOUND","error":"Media not found"}`

**AT-8** — `TestThumbnailHTTPHandler_AnimatedGIF_Returns200` in `media/internal/thumbnail/handler_test.go` [Go HTTP unit test]
- Given: A synthetic 2-frame GIF stored under `test.local/animated-gif`
- When: `GET /_matrix/media/v3/thumbnail/test.local/animated-gif?width=100&height=100&animated=true`
- Then: HTTP 200 with `Content-Type: image/gif`

---

## Technical Context

### Architecture Decision: Library Choice (ADR-013 deferred to this story)

ADR-013 deferred the thumbnail generation library choice to Story 12.5.

**Decision: `github.com/disintegration/imaging` (MIT, pure Go)**

Rationale:
- Pure Go — no cgo, no shell exec, no external process = sandboxed by construction (AC4)
- Handles JPEG, PNG, GIF decode/encode natively
- Supports `Fit` (scale with aspect-ratio) and `Fill` (crop) operations directly
- MIT license — compatible with Apache 2.0 project license
- No network access needed (no external process)
- Alpine-compatible (no glibc dependency)

**Alternative considered: `golang.org/x/image`** — stdlib extension but no high-level resize/crop API; requires more code. Rejected.

**Alternative considered: `h2non/bimg` (libvips binding)** — high-performance but requires cgo + libvips + Alpine apk install. Security concern: external C library. Rejected for sandboxing requirement.

### Endpoint Registration Change

The `thumbnailStub` in both `media/cmd/media/main.go` and `media/internal/download/handler.go` must be **replaced** by the real `thumbnail.Handler`.

**Before (stub):**
```go
mux.HandleFunc("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", thumbnailStub)
```

**After:**
```go
thumbnailHandler := thumbnail.NewHandler(thumbnail.HandlerConfig{
    DB:      store,
    Storage: storer,
})
mux.Handle("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", thumbnailHandler)
```

Remove `thumbnailStub` from both `main.go` and `download/handler.go`.

### New Package: `media/internal/thumbnail`

Create `media/internal/thumbnail/` with:
- `handler.go` — HTTP handler for `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}`
- `thumbnail.go` — `GenerateThumbnail` function + `DetectMIMEType` function
- `handler_test.go` — HTTP-level tests (AT-4 through AT-8)
- `thumbnail_test.go` — pure image processing unit tests (AT-1 through AT-3)

### DB Interface for Thumbnail Handler

The thumbnail handler needs the same `GetMediaFile` as the download handler. Define a new consumer-defined interface in the thumbnail package:

```go
// MediaStore is the thumbnail handler's consumer-defined interface.
type MediaStore interface {
    GetMediaFile(ctx context.Context, serverName, mediaID string) (*MediaFileRow, error)
}

// MediaFileRow holds the data needed for thumbnail generation.
type MediaFileRow struct {
    MediaID     string
    ServerName  string
    ContentType string // stored at upload time (may be "application/octet-stream")
    AESKeyHex   string
    NonceHex    string
}
```

`pgMediaStore` in `main.go` already satisfies `download.MediaStore` (same shape). It satisfies `thumbnail.MediaStore` too without changes, because Go interfaces are structural. No changes to `main.go`'s `pgMediaStore` needed.

### MIME Type Detection (Magic Bytes)

**Do NOT trust `row.ContentType`** — this is what the client sent at upload time and may be wrong or `application/octet-stream`.

Detect MIME type from the first 512 bytes of the **decrypted** plaintext:

```go
import "net/http"

func DetectMIMEType(data []byte) string {
    probe := data
    if len(probe) > 512 {
        probe = probe[:512]
    }
    return http.DetectContentType(probe)
}
```

`http.DetectContentType` (stdlib) uses magic bytes and covers JPEG, PNG, GIF, WebP, PDF, and SVG. Returns `"text/plain; charset=utf-8"` for SVG (since SVG is XML-based text). 

**Rejection rules (return 400 M_BAD_JSON):**
```
Allowed:  "image/jpeg", "image/png", "image/gif", "image/webp"
Rejected: "image/svg+xml", "application/pdf", "text/plain; charset=utf-8" (SVG), 
          "application/postscript", anything not in the allowed list
```

### Image Processing Implementation

```go
import "github.com/disintegration/imaging"

// GenerateThumbnail generates a thumbnail from decoded image bytes.
// method: "scale" (aspect-ratio-preserved, fits within width×height)
//         "crop"  (center-cropped to exactly width×height)
// Returns JPEG bytes.
func GenerateThumbnail(imgBytes []byte, params ThumbnailParams) ([]byte, string, error) {
    src, err := imaging.Decode(bytes.NewReader(imgBytes), imaging.AutoOrientation(true))
    if err != nil {
        return nil, "", err
    }
    
    var dst *image.NRGBA
    switch params.Method {
    case "crop":
        dst = imaging.Fill(src, params.Width, params.Height, imaging.Center, imaging.Lanczos)
    default: // "scale"
        dst = imaging.Fit(src, params.Width, params.Height, imaging.Lanczos)
    }
    
    var buf bytes.Buffer
    if err := imaging.Encode(&buf, dst, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
        return nil, "", err
    }
    return buf.Bytes(), "image/jpeg", nil
}
```

### Animated GIF Handling

`http.DetectContentType` returns `"image/gif"` for GIF files.

When `animated=true` AND detected MIME is `"image/gif"`:
- Decode all frames using `image/gif` stdlib package
- Resize each frame individually using `imaging.Resize`
- Encode back as GIF with original `Delay` and `LoopCount` preserved

When `animated=false` (default) AND source is GIF:
- Decode only the first frame
- Encode as JPEG (static image)

```go
import "image/gif"

func generateAnimatedGIFThumbnail(imgBytes []byte, width, height int) ([]byte, error) {
    g, err := gif.DecodeAll(bytes.NewReader(imgBytes))
    if err != nil {
        return nil, err
    }
    for i, frame := range g.Image {
        // Resize each frame
        resized := imaging.Resize(frame, width, height, imaging.Lanczos)
        // Convert back to *image.Paletted (GIF requires paletted)
        bounds := resized.Bounds()
        paletted := image.NewPaletted(bounds, frame.Palette)
        draw.Draw(paletted, bounds, resized, bounds.Min, draw.Over)
        g.Image[i] = paletted
    }
    g.Config.Width = width
    g.Config.Height = height
    var buf bytes.Buffer
    if err := gif.EncodeAll(&buf, g); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

### Handler Flow

```
GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}?width=W&height=H&method=M&animated=A

1. Parse width, height (required) → 400 M_BAD_JSON if missing or non-integer
2. Parse method (default: "scale"), animated (default: false)
3. DB lookup: GetMediaFile(serverName, mediaId) → 404 if nil
4. storage.Get(storageKey) → ErrNotFound→404, ErrStorageUnavailable→502
5. io.ReadAll(rc) → read encrypted bytes
6. mediacrypto.Decrypt → 500 if auth tag failure
7. DetectMIMEType(plaintext[:512]) → 400 M_BAD_JSON if unsupported type
8. If animated=true AND mime="image/gif": generateAnimatedGIFThumbnail → Content-Type: image/gif
9. Else: GenerateThumbnail(plaintext, params) → JPEG bytes
10. Set headers: Content-Type, Cache-Control: max-age=86400
11. Write 200 + thumbnail bytes
```

### Files to Create

| File | Purpose |
|---|---|
| `media/internal/thumbnail/thumbnail.go` | `GenerateThumbnail`, `DetectMIMEType`, `ThumbnailParams` |
| `media/internal/thumbnail/handler.go` | HTTP handler `ServeHTTP`, `NewHandler`, `HandlerConfig` |
| `media/internal/thumbnail/thumbnail_test.go` | AT-1, AT-2, AT-3 (pure image processing) |
| `media/internal/thumbnail/handler_test.go` | AT-4 through AT-8 (HTTP-level) |

### Files to Modify

| File | Change |
|---|---|
| `media/cmd/media/main.go` | Replace `thumbnailStub` with `thumbnail.NewHandler`; add import; remove `thumbnailStub` func |
| `media/internal/download/handler.go` | Remove `thumbnailStub` function (it's now in thumbnail package) |
| `media/go.mod` | Add `github.com/disintegration/imaging` |
| `media/go.sum` | Updated by `go mod tidy` |

### Files NOT to Modify

- `media/internal/storage/` — no changes; Storer interface is consumed as-is
- `media/internal/upload/` — no changes
- `media/internal/crypto/` — no changes (Decrypt is called via `mediacrypto.Decrypt`)
- `media/cmd/media/main_test.go` — no changes (unless thumbnailStub is tested there)

### go.mod Change

Add dependency:
```
require (
    github.com/disintegration/imaging v1.6.2
)
```

Run inside the `media/` directory:
```bash
go get github.com/disintegration/imaging@v1.6.2
go mod tidy
```

### Test Strategy Notes

**AT-1, AT-2 (image processing unit tests):**

Create a minimal synthetic JPEG programmatically in the test setup:
```go
import (
    "bytes"
    "image"
    "image/color"
    "image/jpeg"
)

func makeSyntheticJPEG(t *testing.T, w, h int) []byte {
    t.Helper()
    img := image.NewRGBA(image.Rect(0, 0, w, h))
    for y := 0; y < h; y++ {
        for x := 0; x < w; x++ {
            img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
        }
    }
    var buf bytes.Buffer
    if err := jpeg.Encode(&buf, img, nil); err != nil {
        t.Fatalf("makeSyntheticJPEG: %v", err)
    }
    return buf.Bytes()
}
```

**AT-4 through AT-8 (HTTP handler tests):**

Use a `fakeThumbStore` + `fakeThumbnailStorer` (similar to download tests). The storage fake stores decrypted bytes (no real encryption needed for thumbnail tests — store plaintext directly as the "encrypted" bytes, use a known key/nonce, or bypass crypto by injecting an in-memory storer with pre-encrypted fixtures).

Simplest approach: store the raw JPEG bytes as plaintext, but encrypt them first in the test setup using `mediacrypto.Encrypt`, then store the ciphertext. The handler will decrypt them. This mirrors the real flow.

**AT-5 (Cache-Control):** After decrypting and generating a thumbnail, the handler MUST set `Cache-Control: max-age=86400`. This is verified by checking `w.Header().Get("Cache-Control")`.

### Error Response Format (Standard)

All errors follow the same `writeError` helper pattern used in download/upload handlers:
```go
func writeError(w http.ResponseWriter, statusCode int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    _ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}
```

Define this locally in `thumbnail/handler.go` (do not import from download — packages must not depend on sibling packages).

---

## Previous Story Intelligence (from Stories 12.2, 12.3, 12.4)

### Pattern: Consumer-Defined Interfaces

Every handler package defines its own minimal interface for DB and Storage access. Do NOT use `download.MediaStore` or `download.MediaFileRow` in the thumbnail package. Define `thumbnail.MediaStore` and `thumbnail.MediaFileRow` with only the fields needed.

### Pattern: `writeError` Helper

Define `writeError` locally in each handler package. It's a 5-line helper — do not create a shared helper package.

### Pattern: Error Classification via `errors.Is`

The storage layer returns `storage.ErrNotFound` and `storage.ErrStorageUnavailable`. The thumbnail handler maps them exactly like the download handler:
```go
if errors.Is(err, storage.ErrNotFound) → 404 M_NOT_FOUND
otherwise → 502 M_UNKNOWN (log full error, never expose to client)
```

### Pattern: `fakeStore` in Tests

All handler tests use a `fakeStore` struct that implements the consumer-defined interface. The struct has fields for controlling return values. See `media/internal/download/download_test.go` for the pattern — replicate it in `thumbnail/handler_test.go`.

### Pattern: MinIO Storer via `ClassifyMinIOError`

Storage errors are already classified by `MinIOStorer.Get`. No special MinIO handling needed in the thumbnail handler.

### Lesson from Code Review (Story 12.4)

The `thumbnailStub` in `download/handler.go` was a placeholder. This story replaces it. Both the stub in `main.go` AND the one in `download/handler.go` must be removed. Failing to remove both would leave dead code.

---

## go.sum Management

After adding `github.com/disintegration/imaging`, run:
```bash
cd media && go mod tidy
```

This ensures `go.sum` is updated. The build container runs `go mod download` — new modules must be in `go.sum` before the Docker build step.

---

## Definition of Done

- [x] `media/internal/thumbnail/thumbnail.go` — `GenerateThumbnail`, `DetectMIMEType`, `ThumbnailParams` defined
- [x] `media/internal/thumbnail/handler.go` — full HTTP handler for thumbnail endpoint
- [x] AT-1: `TestThumbnailHandler_Scale_ReturnsThumbnail` — green (scale, ≤100×100)
- [x] AT-2: `TestThumbnailHandler_Crop_ReturnsExactDimensions` — green (crop, exactly 100×100)
- [x] AT-3: `TestThumbnailHandler_RejectedMimeType_ReturnsBadJSON` — green (SVG/PDF → 400)
- [x] AT-4: `TestThumbnailHTTPHandler_MissingParams_Returns400` — green
- [x] AT-5: `TestThumbnailHTTPHandler_CacheControl` — green (Cache-Control: max-age=86400)
- [x] AT-6: `TestThumbnailHTTPHandler_UnsupportedType_Returns400` — green
- [x] AT-7: `TestThumbnailHTTPHandler_NotFound_Returns404` — green
- [x] AT-8: `TestThumbnailHTTPHandler_AnimatedGIF_Returns200` — green
- [x] `thumbnailStub` removed from `main.go` and `download/handler.go`
- [x] `main.go` wires `thumbnail.NewHandler` into the mux
- [x] `go.mod` + `go.sum` updated with `github.com/disintegration/imaging`
- [x] `make test-unit-go` passes (no regressions in all media packages)
- [x] `Cache-Control: max-age=86400` present on all 2xx thumbnail responses
- [x] MIME type detected from magic bytes, NOT from stored ContentType header

---

## Dev Agent Record

### Implementation Notes (2026-05-12)

**Implementation approach:**
- Created `media/internal/thumbnail/` package with `thumbnail.go` (core image processing) and `handler.go` (HTTP handler)
- Used `github.com/disintegration/imaging v1.6.2` (pure Go, MIT, no cgo) — satisfies AC4 sandbox requirement by construction
- `DetectMIMEType` uses `net/http.DetectContentType` with first 512 bytes (magic bytes)
- `AllowedMIMETypes` map: `{image/jpeg, image/png, image/gif, image/webp}` — deny-by-default (SVG, PDF, PS, EPS all rejected)
- Animated GIF path: `gif.DecodeAll` → per-frame `imaging.Resize` → Floyd-Steinberg re-palettization → `gif.EncodeAll`
- Static path: `imaging.Decode` (AutoOrientation) → `imaging.Fill` (crop) or `imaging.Fit` (scale) → JPEG quality=85

**Interface adapter pattern:**
- `download.MediaStore` and `thumbnail.MediaStore` have `GetMediaFile` returning different row types (Go cannot satisfy both in one struct)
- Added `pgThumbnailStore` thin adapter in `main.go` that wraps `pgMediaStore` and converts `download.MediaFileRow` to `thumbnail.MediaFileRow`

**Test fixes applied:**
- M-1: `handler_test.go` fakeThumbStorer `Put`/`Get` signatures fixed to use `io.Reader`/`io.ReadCloser`
- M-2: Path traversal test strengthened to assert exactly `400 M_BAD_JSON`
- M-3: Added `TestThumbnailHandler_UnsupportedPS_Returns400` with PostScript magic bytes `%!PS`
- M-4: `download_test.go` thumbnailStub references replaced with inline 501 handlers
- M-5: `TestDownload_ThumbnailStub` updated — removed JSON errcode check (stub no longer emits JSON)

**All 70 tests pass. Zero regressions across 6 packages.**

### File List

- `media/internal/thumbnail/thumbnail.go` — NEW
- `media/internal/thumbnail/handler.go` — NEW
- `media/internal/thumbnail/thumbnail_test.go` — NEW
- `media/internal/thumbnail/handler_test.go` — NEW
- `media/cmd/media/main.go` — MODIFIED (added pgThumbnailStore adapter, wired thumbnail.NewHandler, removed thumbnailStub, added thumbnail import, removed json import)
- `media/internal/download/handler.go` — MODIFIED (removed thumbnailStub function)
- `media/internal/download/download_test.go` — MODIFIED (replaced thumbnailStub references with inline 501, updated TestDownload_ThumbnailStub)
- `media/go.mod` — MODIFIED (added disintegration/imaging v1.6.2, golang.org/x/image)
- `media/go.sum` — MODIFIED

### Change Log

- 2026-05-12: Implemented Story 12.5 — thumbnail generation on-demand with disintegration/imaging

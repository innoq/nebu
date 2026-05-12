# Story 4.20: Media Gateway: Download + Decryption

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-20-media-gateway-download-decryption
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to download media files from Nebu,
so that I can view images and files shared in rooms.

---

## Acceptance Criteria

1. `GET /_matrix/media/v3/download/{serverName}/{mediaId}` — **unauthenticated** (Matrix spec allows unauthenticated media download). No `Authorization` header required.

2. Handler looks up `media_files` table by `server_name = {serverName}` AND `media_id = {mediaId}`; returns `404 {"errcode": "M_NOT_FOUND", "error": "..."}` if the row is absent.

3. Reads encrypted file from `NEBU_MEDIA_STORAGE_PATH/<serverName>/<mediaId>` on disk. Returns `500 M_UNKNOWN` if the file cannot be read.

4. Decrypts with AES-256-GCM using the stored `aes_key_hex` and `nonce_hex` from the DB row. If authentication tag verification fails (tampered or corrupt file), returns `500 M_UNKNOWN`.

5. Streams decrypted bytes with:
   - `Content-Type: <content_type from DB row>`
   - `Content-Disposition: inline; filename="<mediaId>"`
   - `Content-Length: <len(decrypted bytes)>`
   - HTTP status `200`.

6. `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` returns `501 {"errcode": "M_UNRECOGNIZED", "error": "Thumbnails not supported in this version"}` (thumbnails: Phase 2).

7. **`pgMediaStore` in `main.go` is completed**: implement a real `pgx/v5` DB layer that satisfies both `upload.MediaStore` (InsertMediaFile) and the new `download.MediaStore` (GetMediaFile). Add `github.com/jackc/pgx/v5 v5.8.0` to `media/go.mod`.

8. **Unit tests** in `media/internal/download/download_test.go`:
   - Happy-path: upload → download round-trip returns identical plaintext bytes
   - `mediaId` not found in DB → `404 M_NOT_FOUND`
   - File missing from disk (DB row exists, file deleted) → `500 M_UNKNOWN`
   - Tampered ciphertext (bit-flipped file) → `500 M_UNKNOWN`
   - Correct `Content-Type` header returned from DB row
   - Correct `Content-Disposition` header format

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Round-trip: upload then download returns identical bytes — Go httptest**
- Given: an in-memory `mediaStore` with a pre-inserted `MediaFileRow` (from a real `Encrypt` call on known plaintext); encrypted bytes written to `t.TempDir()`
- When: `GET /_matrix/media/v3/download/test.local/<mediaId>`
- Then: `200`; response body bytes are byte-equal to the original plaintext; `Content-Type` header matches the stored `content_type`

**2. Unknown mediaId → 404 — Go httptest**
- Given: mock DB `GetMediaFile` returns `nil, nil` (not found)
- When: `GET /_matrix/media/v3/download/test.local/nonexistent-id`
- Then: `404 {"errcode": "M_NOT_FOUND", "error": "..."}`

**3. File missing from disk → 500 — Go httptest**
- Given: mock DB returns a valid `MediaFileRow`; storage path points to a non-existent file (row in DB, file deleted from disk)
- When: `GET /_matrix/media/v3/download/test.local/<mediaId>`
- Then: `500 {"errcode": "M_UNKNOWN", "error": "..."}`

**4. Tampered ciphertext (GCM auth tag fails) → 500 — Go httptest**
- Given: mock DB returns a valid `MediaFileRow`; encrypted file on disk has one byte bit-flipped (authentication tag mismatch)
- When: `GET /_matrix/media/v3/download/test.local/<mediaId>`
- Then: `500 {"errcode": "M_UNKNOWN", "error": "..."}`

**5. Correct Content-Type header — Go httptest**
- Given: DB row has `content_type = "image/png"`
- When: `GET /_matrix/media/v3/download/test.local/<mediaId>` (happy path)
- Then: response has `Content-Type: image/png`

**6. Correct Content-Disposition header — Go httptest**
- Given: `mediaId = "abc123"`
- When: happy-path download
- Then: `Content-Disposition: inline; filename="abc123"`

**7. Thumbnail stub → 501 — Go httptest**
- Given: any request
- When: `GET /_matrix/media/v3/thumbnail/test.local/any-id`
- Then: `501 {"errcode": "M_UNRECOGNIZED", "error": "..."}`

---

## Technical Requirements

### Context: What Story 4-19 Left Ready for This Story

Story 4-19 established the full foundation — do NOT reinvent any of it:

- **`media/internal/crypto/aes.go`** — `Encrypt`, `Decrypt`, `GenerateKey`, `GenerateNonce` all exist and are tested. `Decrypt(ciphertext, key, nonce []byte) ([]byte, error)` is the function you need. It uses `gcm.Open` internally and returns an error on auth tag mismatch.
- **`media/internal/storage/fs.go`** — `Store(basePath, subDir, name string, data []byte) (string, error)` writes files. For download, you need to READ a file: use `os.ReadFile(filepath.Join(storagePath, serverName, mediaId))`.
- **`media/internal/upload/upload.go`** — Defines `MediaStore` interface (with `InsertMediaFile`) and `MediaFileRow` struct. The download handler needs its OWN separate `MediaStore` interface (different method: `GetMediaFile`). Do NOT extend `upload.MediaStore` — keep interfaces small and consumer-defined (Go convention).
- **`media/cmd/media/main.go`** — Has a `pgMediaStore` stub that logs instead of hitting DB. **This story completes it** with a real `pgx/v5` implementation. The stub comment explicitly says: "Story 4-20 will replace this with a real pgx/v5 implementation."
- **`gateway/migrations/000016_media_files.up.sql`** — Already created; the `media_files` table schema is in place.
- **`media/go.mod`** — Currently has NO external dependencies (UUID was done with stdlib). Story 4-20 must add `github.com/jackc/pgx/v5 v5.8.0`.

### New File: `media/internal/download/handler.go`

**Package:** `package download`

**Consumer-defined DB interface** (separate from `upload.MediaStore`):

```go
// MediaStore is the consumer-defined interface for fetching media_files rows.
type MediaStore interface {
    GetMediaFile(ctx context.Context, serverName, mediaID string) (*MediaFileRow, error)
}

// MediaFileRow holds the data read from the media_files table for download.
type MediaFileRow struct {
    MediaID     string
    ServerName  string
    ContentType string
    AESKeyHex   string // 64 hex chars (32 bytes)
    NonceHex    string // 24 hex chars (12 bytes)
}
```

Note: `FileSize`, `UploaderUserID`, `UploadedAt` are in the DB table but NOT needed for download. Keep `MediaFileRow` minimal — only what download needs.

**Handler struct:**

```go
// HandlerConfig contains configuration for the download Handler.
type HandlerConfig struct {
    DB          MediaStore
    StoragePath string // NEBU_MEDIA_STORAGE_PATH
}

// Handler handles GET /_matrix/media/v3/download/{serverName}/{mediaId}.
type Handler struct {
    db          MediaStore
    storagePath string
}

// NewHandler creates a new download Handler.
func NewHandler(cfg HandlerConfig) *Handler
```

**ServeHTTP logic (in order):**

1. Extract `serverName` and `mediaId` from URL path: `r.PathValue("serverName")` and `r.PathValue("mediaId")` — these are Go 1.22+ path parameter patterns.
2. Call `h.db.GetMediaFile(r.Context(), serverName, mediaId)`.
3. If row is `nil` (not found) → `404 M_NOT_FOUND`.
4. Decode `aes_key_hex` with `encoding/hex.DecodeString(row.AESKeyHex)` → byte slice.
5. Decode `nonce_hex` with `encoding/hex.DecodeString(row.NonceHex)` → byte slice.
6. Read encrypted file: `os.ReadFile(filepath.Join(h.storagePath, serverName, mediaId))`. If error → `500 M_UNKNOWN`.
7. Call `crypto.Decrypt(ciphertext, keyBytes, nonceBytes)`. If error → `500 M_UNKNOWN` (includes GCM auth tag failure).
8. Write response:
   - `w.Header().Set("Content-Type", row.ContentType)`
   - `w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", mediaId))`
   - `w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))`
   - `w.WriteHeader(http.StatusOK)`
   - `w.Write(plaintext)`

**Import path for crypto package:**
```go
mediacrypto "github.com/nebu/nebu/media/internal/crypto"
```

### Thumbnail Stub Handler

Add a simple thumbnail stub. Either as a separate handler or directly in `main.go`:

```go
// thumbnailStub returns 501 M_UNRECOGNIZED for all thumbnail requests.
// Thumbnails are Phase 2; the endpoint is registered to avoid 404 confusion.
func thumbnailStub(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusNotImplemented)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "errcode": "M_UNRECOGNIZED",
        "error":   "Thumbnails not supported in this version",
    })
}
```

### Completing `pgMediaStore` in `media/cmd/media/main.go`

The stub in `main.go` currently logs inserts. This story makes it real. The struct needs to implement BOTH interfaces:

```go
// pgMediaStore implements upload.MediaStore and download.MediaStore using pgx/v5.
type pgMediaStore struct {
    pool *pgxpool.Pool
}

// InsertMediaFile inserts a row into media_files.
func (s *pgMediaStore) InsertMediaFile(ctx context.Context, row upload.MediaFileRow) error {
    _, err := s.pool.Exec(ctx,
        `INSERT INTO media_files
         (media_id, server_name, content_type, file_size, aes_key_hex, nonce_hex, uploader_user_id, uploaded_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
        row.MediaID, row.ServerName, row.ContentType, row.FileSize,
        row.AESKeyHex, row.NonceHex, row.UploaderUserID, row.UploadedAt,
    )
    return err
}

// GetMediaFile fetches a row from media_files by server_name + media_id.
// Returns nil, nil if no row found (caller must check for nil).
func (s *pgMediaStore) GetMediaFile(ctx context.Context, serverName, mediaID string) (*download.MediaFileRow, error) {
    row := &download.MediaFileRow{}
    err := s.pool.QueryRow(ctx,
        `SELECT media_id, server_name, content_type, aes_key_hex, nonce_hex
         FROM media_files WHERE server_name = $1 AND media_id = $2`,
        serverName, mediaID,
    ).Scan(&row.MediaID, &row.ServerName, &row.ContentType, &row.AESKeyHex, &row.NonceHex)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, nil // not found → caller returns 404
        }
        return nil, err
    }
    return row, nil
}
```

**pgxpool setup in main():**

```go
dbURL := os.Getenv("NEBU_DB_URL")
if dbURL == "" {
    slog.Error("NEBU_DB_URL is required")
    os.Exit(1)
}
pool, err := pgxpool.New(ctx, dbURL)
if err != nil {
    slog.Error("failed to connect to database", "err", err)
    os.Exit(1)
}
defer pool.Close()
store := &pgMediaStore{pool: pool}
```

**Required imports:**
```go
"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
```

**Note on `context.Background()`:** `main()` does not have a `context.Context` parameter. Use `ctx := context.Background()` for `pgxpool.New`. For graceful shutdown, a signal-based context can be added as a follow-up — out of scope for this story.

### Adding pgx/v5 to `media/go.mod`

Run inside the `media/` module directory:

```bash
cd media && go get github.com/jackc/pgx/v5@v5.8.0
```

This adds `pgx/v5` and updates `media/go.sum`. Use the SAME version as the gateway module (`v5.8.0`) for consistency.

**Note:** `media/go.mod` currently has NO dependencies — not even pgx. The dev notes from 4-19 confirm: "UUID was done with stdlib". So `go.mod` currently only has the `module` and `go` directives.

### Route Registration in `main.go`

Add to the existing mux (keep the existing routes):

```go
downloadHandler := download.NewHandler(download.HandlerConfig{
    DB:          store,  // *pgMediaStore implements download.MediaStore
    StoragePath: storagePath,
})

mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", downloadHandler)
mux.HandleFunc("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", thumbnailStub)
```

### Error Response Format

Use the same `writeError` helper pattern from `upload.go`. Define it locally in `download/handler.go` (or extract to a shared internal package if it would need to be shared — for this story, a local copy is fine):

```go
func writeError(w http.ResponseWriter, statusCode int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "errcode": errcode,
        "error":   message,
    })
}
```

Error codes to use:
- `404`: `M_NOT_FOUND` — media_id unknown
- `500`: `M_UNKNOWN` — file read error, decryption error (including GCM tag mismatch)

### Path Parameter Extraction (Go 1.22+)

The media module uses `go 1.26` — path values via `r.PathValue("serverName")` are available (added in Go 1.22). Route pattern must use `{serverName}` and `{mediaId}` in the mux registration for this to work.

---

## File Structure

This story creates/modifies the following files:

```
media/
  go.mod                                ← ADD: github.com/jackc/pgx/v5 v5.8.0
  go.sum                                ← auto-updated by go get
  cmd/media/main.go                     ← MODIFY: complete pgMediaStore with pgx/v5;
                                           add download + thumbnail routes
  internal/
    download/
      handler.go                        ← NEW: Handler, MediaStore interface, MediaFileRow,
                                           NewHandler, ServeHTTP, writeError
      download_test.go                  ← NEW: unit tests (httptest)
```

**No changes to:**
- `media/internal/crypto/aes.go` — already has `Decrypt`, no changes needed
- `media/internal/storage/fs.go` — no changes needed; download reads with `os.ReadFile` directly
- `media/internal/upload/upload.go` — no changes needed
- `gateway/migrations/` — `000016_media_files` already exists from story 4-19

---

## Architecture Compliance

- **No gRPC to Core**: Download is a standalone Go HTTP handler with direct PostgreSQL access. No Elixir involvement.
- **No Redis / No NATS**: No caching layer. Files are read from local filesystem on every request. Acceptable for MVP.
- **AES-256-GCM**: Use `crypto.Decrypt` from `media/internal/crypto` — do NOT reimplement decryption. The function already handles all edge cases including auth tag verification.
- **Errors: explicit handling, no panic**: All errors returned, none swallowed. GCM tag failure surfaces as error from `Decrypt`, which maps to `500 M_UNKNOWN`.
- **Consumer-defined interfaces**: `download.MediaStore` is defined in `download/handler.go` — NOT in `main.go` or shared. `pgMediaStore` in `main.go` implements both `upload.MediaStore` and `download.MediaStore` by structural typing (Go interfaces are implicit).
- **Separate module**: All changes stay within `media/`. Do NOT touch `gateway/go.mod`.
- **Unauthenticated endpoint**: Per Matrix spec, media download is unauthenticated. The handler does NOT check `Authorization` header. This is intentional and matches the epics spec.

---

## Previous Story Intelligence (4-19)

Key learnings and patterns from Story 4-19 that MUST be followed:

1. **Crypto functions are all in `media/internal/crypto/aes.go`** — `Decrypt` is already implemented and tested (`TestDecrypt_RoundTrip`). Import it as `mediacrypto "github.com/nebu/nebu/media/internal/crypto"`.

2. **UUID generation used stdlib `crypto/rand` NOT `github.com/gofrs/uuid`** — the dev notes confirm this. Do NOT add `gofrs/uuid` to `go.mod`.

3. **pgMediaStore in `main.go` is explicitly a placeholder with a comment**: `// Story 4-20 will replace this with a real pgx/v5 implementation.` This story delivers that.

4. **Test pattern: `mockXStore` in `_test.go`, `buildHandler` helper** — follow the exact same pattern from `upload_test.go`:
   - Define `type mockMediaStore struct { ... }` in `download_test.go` (NOT a separate file)
   - Use `t.TempDir()` for filesystem tests
   - Use `httptest.NewRecorder()` + `httptest.NewRequest()`

5. **`storage.Store` writes to `<basePath>/<subDir>/<name>`** — download reads from the same path: `filepath.Join(storagePath, serverName, mediaId)`. Use `os.ReadFile` directly in the handler (not a new storage function).

6. **Handler struct pattern**: `type Handler struct { db X; storagePath string }` + `type HandlerConfig struct { DB X; StoragePath string }` + `func NewHandler(cfg HandlerConfig) *Handler`. Match the `upload.Handler` pattern exactly.

7. **`writeError` is defined locally in `upload.go`** — define the same locally in `download/handler.go`. No shared package needed for this story.

8. **`make test-unit-go` does NOT cover media tests** — dev note from 4-19: "approach (b) is acceptable: document that media tests are run with `cd media && go test ./...`". Run media tests with:
   ```bash
   docker run --rm -v $(PWD):/workspace -w /workspace/media golang:1.26-alpine \
     sh -c "apk add -q --no-cache gcc musl-dev && go test -race ./..."
   ```

9. **`media/go.mod` has NO dependencies currently** — `go.sum` may be empty or minimal. `go get github.com/jackc/pgx/v5@v5.8.0` will pull in transitive deps and update `go.sum`.

10. **`main.go` uses Go 1.22+ mux patterns** (`"POST /_matrix/media/v3/upload"` with method prefix) — path parameters `{serverName}` and `{mediaId}` use the same syntax.

---

## Git Intelligence

Recent commits (from sprint-status and git log):
- `ccd8572` — ExtractRoleClaim refactor + array claim unit tests + Room.Server crash recovery test
- `d48c647` — DB failure test gaps, ETS safety checks (Story 4-4)
- `64f5598` — Room GenServer send_event Ed25519 signing (Story 4-4)

All recent commits are Elixir/Core. Story 4-20 is pure Go in `media/` — no Elixir changes. Story 4-19 was the last media gateway commit (not yet reflected in git log above since it's in `review` status).

---

## ATDD / TDD Guidance

Per Nebu TDD Standard (CLAUDE.md): **write the failing tests FIRST**.

Recommended sequence:

1. Create `media/internal/download/download_test.go` with all 7 tests (will fail: `download` package does not exist yet — compilation error is the expected "red" state)
2. Create `media/internal/download/handler.go` with the `Handler` struct, `MediaStore` interface, `MediaFileRow` — implement `ServeHTTP`
3. Run `cd media && go test ./internal/download/...` — all tests must pass (green)
4. Modify `media/cmd/media/main.go` to add `pgx/v5` pool + complete `pgMediaStore` + register routes
5. Run `cd media && go test -race ./...` — all tests must pass (crypto + storage + upload + download)

**Round-trip test setup pattern** (use real crypto, not mocks, for the round-trip test):

```go
// In TestDownload_RoundTrip:
key, _ := mediacrypto.GenerateKey()
nonce, _ := mediacrypto.GenerateNonce()
plaintext := []byte("original file content for round-trip test")
ciphertext, _ := mediacrypto.Encrypt(plaintext, key, nonce)

// Write ciphertext to temp dir
dir := t.TempDir()
_ = os.MkdirAll(filepath.Join(dir, "test.local"), 0755)
_ = os.WriteFile(filepath.Join(dir, "test.local", "test-media-id"), ciphertext, 0600)

// Wire mock DB that returns matching row
store := &mockMediaStore{row: &download.MediaFileRow{
    MediaID:     "test-media-id",
    ServerName:  "test.local",
    ContentType: "image/png",
    AESKeyHex:   hex.EncodeToString(key),
    NonceHex:    hex.EncodeToString(nonce),
}}
h := download.NewHandler(download.HandlerConfig{DB: store, StoragePath: dir})
// ... httptest request/response
```

**Tampered file test pattern** (for the GCM auth tag failure test):

```go
// Flip one bit in the ciphertext before writing to disk.
tampered := make([]byte, len(ciphertext))
copy(tampered, ciphertext)
tampered[0] ^= 0x01  // bit-flip the first byte
_ = os.WriteFile(filepath.Join(dir, "test.local", "tampered-id"), tampered, 0600)
```

**This story has no Elixir changes** — `make test-unit-elixir` does not need to be run.

---

## Out of Scope

- Thumbnail generation (Phase 2) — the `501` stub is the complete deliverable
- Serving `Range` requests (partial content / HTTP 206) — Phase 2
- Authentication on download endpoint — Matrix spec intentionally allows unauthenticated media download
- Caching layer for decrypted content — out of scope (no Redis per ADR 002)
- CDN integration — out of scope for MVP

---

## Dev Agent Record

### Implementation Plan

1. [x] Create `media/internal/download/handler.go` — consumer-defined `MediaStore` interface, `MediaFileRow` struct, `Handler` struct, `NewHandler`, `ServeHTTP`, `writeError`, `thumbnailStub`
2. [x] Add `github.com/jackc/pgx/v5 v5.8.0` to `media/go.mod` via `go get`, then `go mod tidy`
3. [x] Complete `pgMediaStore` in `media/cmd/media/main.go` — `InsertMediaFile` (pgx Exec) + `GetMediaFile` (QueryRow/Scan, nil on ErrNoRows)
4. [x] Register download + thumbnail routes in `main.go` mux
5. [x] Run `cd media && go test -race ./...` — 23 tests pass across 5 packages including all 9 download tests

### Completion Notes

- `media/internal/download/handler.go` created. Follows the exact same pattern as `upload/upload.go` (HandlerConfig/Handler/NewHandler/ServeHTTP/writeError). `thumbnailStub` is unexported and accessible from `download_test.go` (same package). `main.go` defines its own `thumbnailStub` wrapper with duplicate 3-line body.
- `media/go.mod` now includes `github.com/jackc/pgx/v5 v5.8.0` and transitive deps (`pgpassfile`, `pgservicefile`, `golang.org/x/text`, `golang.org/x/sync`, `stretchr/testify`).
- `pgMediaStore` in `main.go` satisfies both `upload.MediaStore` (InsertMediaFile) and `download.MediaStore` (GetMediaFile) by structural typing. `GetMediaFile` returns `nil, nil` on `pgx.ErrNoRows` as specified.
- All 9 download tests pass with `-race`: happy path, not found, tampered ciphertext, wrong server name, storage read error, thumbnail stub, round-trip (upload+download), content-disposition, DB error.
- `go test -race ./...` ran 23 tests total (crypto + storage + upload + download packages) — zero failures, zero races.

### Debug Log

- Initial `go build ./...` after `go get pgx/v5` failed with missing `go.sum` entries — resolved with `go mod tidy`.
- `thumbnailStub` is unexported in `handler.go` (accessible within package for tests); `main.go` defines its own local stub to avoid cross-package unexported reference.

### File List

- `media/internal/download/handler.go` — NEW
- `media/go.mod` — MODIFIED (added pgx/v5 v5.8.0)
- `media/go.sum` — MODIFIED (auto-updated by go get + go mod tidy)
- `media/cmd/media/main.go` — MODIFIED (real pgMediaStore + download/thumbnail routes)

### Change Log

- 2026-04-03: Story 4-20 implemented — download handler, pgMediaStore pgx/v5 completion, route registration. All 9 acceptance tests pass.

---
status: ready-for-dev
epic: 12
story: 2
security_review: not-needed
matrix: false
ui: false
---

# Story 12.2: Storage Interface Refactor

Status: ready-for-dev

## Story

As a developer,
I want the media gateway to use a `Storer` interface instead of direct filesystem calls,
So that the MinIO backend can be swapped in without rewriting upload/download logic.

**Size:** S

---

## Acceptance Criteria

**AC1 — `Storer` interface defined in `media/internal/storage/storage.go`:**

Given `media/internal/storage/storage.go` exists,
When it is inspected,
Then it defines:
```go
type Storer interface {
    Put(ctx context.Context, key string, r io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
}
```

**AC2 — `LocalStorer` in `media/internal/storage/local.go` implements `Storer`:**

Given `media/internal/storage/local.go` exists,
When it is inspected,
Then it implements `Storer` using the existing local filesystem logic — behaviour-preserving refactor, no functional changes. The old `Store(basePath, subDir, name string, data []byte) (string, error)` function may still exist for backwards compatibility but the `LocalStorer` must implement the `Storer` interface.

**AC3 — `MinIOStorer` in `media/internal/storage/minio.go` implements `Storer`:**

Given `media/internal/storage/minio.go` exists,
When it is inspected,
Then it implements `Storer` using the MinIO Go SDK (`github.com/minio/minio-go/v7`).
- `Put` calls `client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: "application/octet-stream"})`
- `Get` calls `client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})` and returns the object as an `io.ReadCloser`
- `Delete` calls `client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})`

**AC4 — Upload and download handlers depend on `Storer` (injected via config struct):**

Given the existing upload and download handlers (`media/internal/upload/upload.go` and `media/internal/download/handler.go`),
When they are updated,
Then:
- `upload.HandlerConfig` contains a `Storage Storer` field (replacing `StoragePath string`)
- `download.HandlerConfig` contains a `Storage Storer` field (replacing `StoragePath string`)
- The handlers call `h.storage.Put(...)` and `h.storage.Get(...)` instead of `storage.Store(...)` and `os.ReadFile(...)`
- `cmd/media/main.go` is updated to wire the concrete `LocalStorer` into both handlers

**AC5 — Upload handler unit tests pass with a fake `Storer` (no real filesystem or MinIO):**

Given unit tests for the upload handler,
When they run with a fake `Storer`,
Then all tests pass without a real filesystem or MinIO — `Storer` is fully mockable.
- The existing `TestUpload_StorageError` test must pass by having the fake `Storer.Put` return an error
- The existing `buildHandler` helper in `upload_test.go` must use the fake `Storer` instead of `StoragePath`

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1** — `TestStorer_Interface_LocalStorer` in `media/internal/storage/local_test.go` [Go unit test]
- Given: `LocalStorer{basePath: t.TempDir()}` is created
- When: `var _ storage.Storer = &LocalStorer{}` compile-time check
- Then: `LocalStorer` satisfies the `Storer` interface (compilation passes)

**AT-2** — `TestLocalStorer_Put_Get_RoundTrip` in `media/internal/storage/local_test.go` [Go unit test]
- Given: `LocalStorer{basePath: t.TempDir()}` is created
- When: `Put(ctx, "test.local/abc123", bytes.NewReader(data), int64(len(data)))` is called, then `Get(ctx, "test.local/abc123")`
- Then: Read bytes from the returned `io.ReadCloser` equal the original data

**AT-3** — `TestLocalStorer_Get_NotFound` in `media/internal/storage/local_test.go` [Go unit test]
- Given: `LocalStorer` with empty basePath
- When: `Get(ctx, "nonexistent/key")` is called
- Then: Returns a non-nil error (file not found)

**AT-4** — `TestLocalStorer_Delete` in `media/internal/storage/local_test.go` [Go unit test]
- Given: A file stored via `Put`
- When: `Delete(ctx, key)` is called
- Then: The file is removed from disk; subsequent `Get` returns an error

**AT-5** — `TestUpload_WithFakeStorer_HappyPath` in `media/internal/upload/upload_test.go` [Go unit test]
- Given: Upload handler wired with a `fakeStorer{}` (in-memory, no filesystem)
- When: Valid POST to `/_matrix/media/v3/upload` with small body and Bearer token
- Then: Returns 200 with `mxc://` URI; `fakeStorer.Put` was called once with a non-empty key

**AT-6** — `TestUpload_WithFakeStorer_StorageError` in `media/internal/upload/upload_test.go` [Go unit test]
- Given: Upload handler wired with a `fakeStorer{}` that returns `errors.New("storer error")` from `Put`
- When: POST to `/_matrix/media/v3/upload`
- Then: Returns 500 M_UNKNOWN; DB `InsertMediaFile` was NOT called

**AT-7** — `TestDownload_WithFakeStorer_HappyPath` in `media/internal/download/download_test.go` [Go unit test]
- Given: Download handler wired with a `fakeDownloadStorer{}` that returns pre-encrypted content
- When: GET `/_matrix/media/v3/download/{serverName}/{mediaId}` with valid DB row
- Then: Returns 200 with decrypted plaintext

**AT-8** — `TestDownload_WithFakeStorer_StorageError` in `media/internal/download/download_test.go` [Go unit test]
- Given: Download handler wired with a `fakeDownloadStorer{}` that returns an error from `Get`
- When: GET request for existing DB row
- Then: Returns 500 M_UNKNOWN

---

## Technical Context

### Current State (before this story)

The media gateway currently uses **direct filesystem calls**:

1. **`media/internal/storage/fs.go`** — contains a single `Store(basePath, subDir, name string, data []byte) (string, error)` function that writes to disk via `os.WriteFile`.

2. **`media/internal/upload/upload.go`** — calls `storage.Store(h.storagePath, h.serverName, mediaID, ciphertext)`. Has `StoragePath string` in `HandlerConfig`.

3. **`media/internal/download/handler.go`** — reads directly via `os.ReadFile(filepath.Join(h.storagePath, serverName, mediaID))`. Has `StoragePath string` in `HandlerConfig`.

4. **`media/cmd/media/main.go`** — wires `StoragePath` env var into both handlers.

### Target State (after this story)

```
media/internal/storage/
  storage.go      ← NEW: Storer interface definition
  fs.go           ← KEEP: existing Store() function (do not delete — backward compat)
  local.go        ← NEW: LocalStorer struct implementing Storer (wraps fs.go logic)
  minio.go        ← NEW: MinIOStorer struct implementing Storer (minio-go/v7)
  local_test.go   ← UPDATE: add AT-1..AT-4, keep existing TestLocalStorage_* tests
```

```
media/internal/upload/upload.go   ← UPDATE: HandlerConfig.Storage Storer, call h.storage.Put()
media/internal/upload/upload_test.go ← UPDATE: use fakeStorer, keep existing tests passing
media/internal/download/handler.go  ← UPDATE: HandlerConfig.Storage Storer, call h.storage.Get()
media/internal/download/download_test.go ← UPDATE: use fakeDownloadStorer
media/cmd/media/main.go             ← UPDATE: wire LocalStorer into both handlers
media/go.mod                        ← UPDATE: add minio-go/v7 dependency
```

### Key Design Decisions

**Storer interface key format:**
The `key` parameter in `Put/Get/Delete` should be `"<serverName>/<mediaID>"` — a single string combining both path components. The caller (upload/download handler) constructs this key as `serverName + "/" + mediaID`.

This makes the interface backend-agnostic:
- `LocalStorer` splits on `/` to get `subDir` and `name`, then uses the existing `os.WriteFile` logic
- `MinIOStorer` uses the key directly as the object key in the MinIO bucket

**LocalStorer implementation:**
```go
type LocalStorer struct {
    BasePath string
}

func (s *LocalStorer) Put(ctx context.Context, key string, r io.Reader, size int64) error {
    // Split key into subDir/name
    // os.MkdirAll + os.WriteFile (same as existing fs.Store logic)
    data, err := io.ReadAll(r)
    // ...
}

func (s *LocalStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    // os.ReadFile → return io.NopCloser(bytes.NewReader(data))
}

func (s *LocalStorer) Delete(ctx context.Context, key string) error {
    // os.Remove
}
```

**MinIOStorer implementation:**
```go
type MinIOStorer struct {
    Client *minio.Client
    Bucket string
}
```
Wire with: `minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: useSSL})`

**Upload handler change:**
```go
// HandlerConfig BEFORE:
type HandlerConfig struct {
    DB          MediaStore
    StoragePath string
    ServerName  string
    MaxBytes    int64
}

// HandlerConfig AFTER:
type HandlerConfig struct {
    DB         MediaStore
    Storage    storage.Storer   // replaces StoragePath
    ServerName string
    MaxBytes   int64
}
```

In `ServeHTTP`, replace:
```go
_, err = storage.Store(h.storagePath, h.serverName, mediaID, ciphertext)
```
with:
```go
key := h.serverName + "/" + mediaID
err = h.storage.Put(r.Context(), key, bytes.NewReader(ciphertext), int64(len(ciphertext)))
```

**Download handler change:**
Replace:
```go
filePath := filepath.Join(h.storagePath, serverName, mediaID)
ciphertext, err := os.ReadFile(filePath)
```
with:
```go
key := serverName + "/" + mediaID
rc, err := h.storage.Get(r.Context(), key)
if err != nil { ... }
defer rc.Close()
ciphertext, err := io.ReadAll(rc)
```

**main.go wiring:**
```go
localStorer := &storage.LocalStorer{BasePath: storagePath}

uploadHandler := upload.NewHandler(upload.HandlerConfig{
    DB:         store,
    Storage:    localStorer,
    ServerName: serverName,
    MaxBytes:   maxBytes,
})

downloadHandler := download.NewHandler(download.HandlerConfig{
    DB:      store,
    Storage: localStorer,
})
```

### Existing Tests — Backward Compatibility

**CRITICAL:** All existing passing tests must continue to pass after this refactor.

The existing `TestUpload_StorageError` test uses `badStoragePath := "/dev/null/cannot-be-a-directory"` to force a filesystem error. After the refactor, this test must be rewritten to use a `fakeStorer` that returns an error — the `StoragePath` field is removed from `HandlerConfig`. Keep the test intent, update the wiring.

The existing download tests in `download_test.go` use `buildDownloadHandler(t, store, dir)` where `dir` is a `t.TempDir()`. After the refactor, `buildDownloadHandler` must accept a `Storer` instead of a path. The tests that use `writeEncryptedFile` write directly to disk — after the refactor, those tests should either:
1. Use a `LocalStorer` backed by the same `t.TempDir()`, OR
2. Seed the `fakeDownloadStorer` with the ciphertext directly (simpler for unit tests)

**Recommended approach for download tests:** Create a `fakeDownloadStorer` (in-memory `map[string][]byte`) to replace disk writes. The `writeEncryptedFile` helper can store data in the fake storer instead of to disk.

However, to minimize churn, keeping `LocalStorer{BasePath: dir}` in the tests is also acceptable and simpler. Both approaches are valid.

### minio-go/v7 Dependency

Add to `media/go.mod`:
```
require github.com/minio/minio-go/v7 v7.0.91
```

Run `go get github.com/minio/minio-go/v7` inside the `media/` Docker build container. The Dockerfile already uses `go mod download` — the `go.sum` will be updated automatically.

**Note:** `MinIOStorer` in `minio.go` is compiled and ready for use in Story 12.3. Its unit tests (if any) in this story are interface-compliance only — no live MinIO connection is needed.

### Module Path

The media module path is `github.com/nebu/nebu/media`. Import the storage package as:
```go
import "github.com/nebu/nebu/media/internal/storage"
```

### Build Commands

```bash
make test-unit-go    # runs go test ./... in the media container
make build-gateway   # or make redeploy for full stack
```

The media tests run as part of `test-unit-go` (the Makefile runs tests across all Go modules).

---

## Dev Notes / Learnings from Story 12.1

- Story 12.1 added MinIO to Docker Compose (`docker-compose.yml`), `make setup` generates credentials in `.secrets/`, and ADR-013 documents the object storage decision.
- The `nebu-media` bucket exists in MinIO after `make setup && make dev`.
- MinIO credentials (`MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`) are accessed via Docker Secrets — for `MinIOStorer`, use env vars `NEBU_MINIO_ENDPOINT`, `NEBU_MINIO_ACCESS_KEY`, `NEBU_MINIO_SECRET_KEY` (wired in Story 12.3, not this story). In this story, `MinIOStorer` is compiled but not yet wired into `main.go` at runtime.
- The `mc` init container runs `mc mb minio/nebu-media` — the bucket is guaranteed to exist when `make dev` is running.

---

## Story Completion Checklist

- [ ] `media/internal/storage/storage.go` defines `Storer` interface
- [ ] `media/internal/storage/local.go` implements `Storer` as `LocalStorer`
- [ ] `media/internal/storage/minio.go` implements `Storer` as `MinIOStorer`
- [ ] `media/internal/upload/upload.go`: `HandlerConfig.Storage Storer` replaces `StoragePath`
- [ ] `media/internal/download/handler.go`: `HandlerConfig.Storage Storer` replaces `StoragePath`
- [ ] `media/cmd/media/main.go`: wires `LocalStorer` into both handlers
- [ ] `media/go.mod`: adds `minio-go/v7`
- [ ] All existing tests still pass
- [ ] AT-1..AT-8 all pass
- [ ] `make test-unit-go` green

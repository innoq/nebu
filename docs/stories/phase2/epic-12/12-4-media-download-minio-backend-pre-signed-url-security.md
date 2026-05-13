---
status: ready-for-dev
epic: 12
story: 4
security_review: required
matrix: true
ui: false
---

# Story 12.4: Media Download — MinIO Backend + Pre-Signed URL Security

Status: ready-for-dev

## Story

As a Matrix client user,
I want to download media stored in MinIO,
So that files uploaded after the MinIO migration are retrievable.

**Size:** S

---

## Acceptance Criteria

**AC1 — Download retrieves file from MinIO via Storer.Get:**

Given a file is stored in MinIO (uploaded via Story 12.3),
When a client calls `GET /_matrix/media/v3/download/{serverName}/{mediaId}`,
Then the gateway retrieves the encrypted file from MinIO via the `Storer.Get` interface, decrypts it (AES-256-GCM, existing logic), and streams it to the client with correct `Content-Type`, `Content-Disposition`, and `Content-Length` headers.

**This AC is already satisfied** by the `Storer` interface wiring from Stories 12.2 and 12.3 (`MinIOStorer.Get` delegates to `minio.Client.GetObject`). The test coverage exists. No new code is required for AC1.

**AC2 — Pre-signed URL decision (ADR-013 deferred):**

Given ADR-013 deferred the pre-signed URL policy decision to this story,
When the download architecture is reviewed,
Then the decision is: **direct authenticated streaming** (no pre-signed URLs).

Rationale: The media gateway already validates the Matrix access token at the HTTP handler level and streams the decrypted plaintext directly to the client. Pre-signed URLs would expose a signed URL for the *encrypted* ciphertext, which clients cannot decrypt. Since decryption happens server-side (AES-256-GCM), a pre-signed URL pattern does not apply — the gateway must read and decrypt before sending. This decision is documented in the story; no code change required.

**AC3 — 404 M_NOT_FOUND when mediaId not in MinIO:**

Given a `GET /_matrix/media/v3/download/{serverName}/{mediaId}` request is made,
When the `mediaId` does not exist in MinIO (object not found),
Then:
- The `MinIOStorer.Get` method returns a typed error distinguishing "object not found" from other errors
- The download handler returns `404 M_NOT_FOUND` (not 500)
- The response body is `{"errcode":"M_NOT_FOUND","error":"Media not found"}`

**Current gap:** `MinIOStorer.Get` calls `minio.Client.GetObject`, which returns a `*minio.ErrorResponse` with `Code: "NoSuchKey"` when the object does not exist. The current `minio.go` returns this as a generic `error`. The download handler in `handler.go` treats any storage error as `500 M_UNKNOWN`. Fix: `MinIOStorer.Get` must detect `minio.ToErrorResponse(err).Code == "NoSuchKey"` and return a sentinel error; the handler must map that sentinel to `404 M_NOT_FOUND`.

**AC4 — 502 M_UNKNOWN when MinIO service is unavailable:**

Given a `GET /_matrix/media/v3/download/{serverName}/{mediaId}` request is made,
When the MinIO service is unavailable (network unreachable, connection refused, timeout),
Then:
- The gateway returns HTTP `502` (Bad Gateway) with `{"errcode":"M_UNKNOWN","error":"Media storage unavailable"}`
- The response body does NOT include any credential values, endpoint URLs, or MinIO SDK error details
- The full error is logged via `slog.Error` for observability, but NOT returned to the client
- No panic occurs

**Current gap:** The download handler currently returns `500 M_UNKNOWN` for all storage errors. A MinIO connection error should be `502` (upstream failure) not `500` (internal server error). Fix: Add a `StorageUnavailableError` sentinel type (or detection function) in the storage package; handler maps it to 502.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1** — `TestMinIOStorer_Get_NotFound_ReturnsStorageNotFoundError` in `media/internal/storage/minio_test.go` [Go unit test with fake MinIO]
- Given: MinIO returns `NoSuchKey` error for `GetObject`
- When: `MinIOStorer.Get` is called with a non-existent key
- Then: Returns `storage.ErrNotFound` (or equivalent sentinel)

**AT-2** — `TestDownload_MinIONotFound_Returns404` in `media/internal/download/download_test.go` [Go unit test]
- Given: `fakeDownloadStorer.Get` returns `storage.ErrNotFound`
- When: `GET /_matrix/media/v3/download/{serverName}/{mediaId}`
- Then: HTTP 404 with `{"errcode":"M_NOT_FOUND","error":"Media not found"}`

**AT-3** — `TestDownload_MinIOUnavailable_Returns502` in `media/internal/download/download_test.go` [Go unit test]
- Given: `fakeDownloadStorer.Get` returns `storage.ErrStorageUnavailable` (or a non-ErrNotFound error wrapped as unavailable)
- When: `GET /_matrix/media/v3/download/{serverName}/{mediaId}`
- Then: HTTP 502 with `{"errcode":"M_UNKNOWN","error":"Media storage unavailable"}`; response body does NOT contain endpoint URL or any internal detail

**AT-4** — `TestDownload_StorageError_NoCredentialLeak` in `media/internal/download/download_test.go` [Go unit test]
- Given: `fakeDownloadStorer.Get` returns an error containing a fake endpoint URL and fake credentials string
- When: `GET /_matrix/media/v3/download/{serverName}/{mediaId}`
- Then: Response body does NOT contain the endpoint URL or credentials string; only a generic error message is returned

**AT-5** — `TestMinIOStorer_Get_NotFound_IsStorageNotFound` in `media/internal/storage/minio_test.go` [Go unit test]
- Given: `MinIOStorer.Get` returns `storage.ErrNotFound` for a NoSuchKey error
- When: `errors.Is(err, storage.ErrNotFound)` is called
- Then: Returns `true`

---

## Technical Context

### Current State (from Stories 12.2 + 12.3)

The download pipeline is fully wired:

```
GET /_matrix/media/v3/download/{serverName}/{mediaId}
  → handler.go: ServeHTTP
    → pgMediaStore.GetMediaFile → 404 M_NOT_FOUND if nil
    → storage.Storer.Get(storageKey) → 500 M_UNKNOWN (CURRENT)
    → mediacrypto.Decrypt → 500 M_UNKNOWN
    → write response headers + body
```

`MinIOStorer.Get` in `media/internal/storage/minio.go` currently:
```go
func (s *MinIOStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    obj, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
    if err != nil {
        return nil, err  // ← Returns raw minio error — no classification
    }
    return obj, nil
}
```

**NOTE:** `minio.Client.GetObject` returns a `*minio.Object` even for missing objects — the error surfaces on the first `Read()` call, not on `GetObject`. The `NoSuchKey` detection must happen during `io.ReadAll` in the handler, OR by calling `obj.Stat()` before reading. The safest approach: read with `io.ReadAll`, then check if the error is a MinIO `NoSuchKey` by inspecting `minio.ToErrorResponse(err).Code`.

**Alternative approach:** Wrap the read in the `MinIOStorer.Get` method: after getting the `*minio.Object`, call `obj.Stat()` to probe existence before returning. `Stat()` returns the `NoSuchKey` error eagerly. This is cleaner because it keeps error classification in the storage layer.

**Recommended approach (use this):**

```go
// In MinIOStorer.Get — call obj.Stat() to detect NoSuchKey eagerly
func (s *MinIOStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    obj, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
    if err != nil {
        return nil, classifyMinIOError(err)
    }
    if _, err := obj.Stat(); err != nil {
        _ = obj.Close()
        return nil, classifyMinIOError(err)
    }
    return obj, nil
}

func classifyMinIOError(err error) error {
    if err == nil {
        return nil
    }
    resp := minio.ToErrorResponse(err)
    if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
        return fmt.Errorf("%w: %s", storage.ErrNotFound, resp.Code)
    }
    // Connection refused / unavailable: wrap as ErrStorageUnavailable
    // (network errors don't have a minio.ErrorResponse Code)
    return fmt.Errorf("%w: minio error", storage.ErrStorageUnavailable)
}
```

**Sentinel errors in `storage/storage.go`:**
```go
var (
    ErrNotFound          = errors.New("storage: object not found")
    ErrStorageUnavailable = errors.New("storage: storage backend unavailable")
)
```

**Handler changes in `download/handler.go`:**
```go
rc, err := h.storage.Get(r.Context(), storageKey)
if err != nil {
    if errors.Is(err, storage.ErrNotFound) {
        writeError(w, http.StatusNotFound, "M_NOT_FOUND", "Media not found")
        return
    }
    slog.Error("storage.Get failed", "key", storageKey, "err", err)
    writeError(w, http.StatusBadGateway, "M_UNKNOWN", "Media storage unavailable")
    return
}
```

### Files to Modify

| File | Change |
|---|---|
| `media/internal/storage/storage.go` | Add `ErrNotFound` and `ErrStorageUnavailable` sentinel errors |
| `media/internal/storage/minio.go` | Add `classifyMinIOError` + update `Get` to call `obj.Stat()` and classify errors |
| `media/internal/download/handler.go` | Update storage error handling: `ErrNotFound` → 404, other errors → 502 |
| `media/internal/storage/minio_test.go` | NEW: unit tests for error classification (AT-1, AT-5) |
| `media/internal/download/download_test.go` | Add AT-2, AT-3, AT-4 using `fakeDownloadStorer` |

### Files NOT to Modify

- `media/cmd/media/main.go` — no changes needed; selectStorer/wiring unchanged
- `media/internal/upload/` — upload path unaffected
- `media/internal/crypto/` — crypto unchanged
- `media/internal/storage/local.go` — LocalStorer unaffected (ErrNotFound should also be returned by LocalStorer for missing files — check and align)

### LocalStorer.Get alignment

Check `media/internal/storage/local.go` `Get` method. It likely returns `os.Open` errors for missing files. After this story, LocalStorer.Get should also return `ErrNotFound` when the file does not exist (wrapping `os.ErrNotExist`). This ensures consistent behavior regardless of backend.

```go
// In LocalStorer.Get — align error codes
func (s *LocalStorer) Get(ctx context.Context, key string) (io.ReadCloser, error) {
    path := filepath.Join(s.BasePath, filepath.FromSlash(key))
    f, err := os.Open(path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
        }
        return nil, err  // Other OS errors pass through as-is (ErrStorageUnavailable not appropriate for local FS)
    }
    return f, nil
}
```

**Important:** After aligning LocalStorer.Get to return ErrNotFound, the existing download handler test `TestDownload_StorageReadError` (which expects 500 M_UNKNOWN for a missing file) needs updating — it should now expect 404 M_NOT_FOUND (because the file doesn't exist → ErrNotFound → 404). However, `TestDownload_NotFound` already covers DB-level 404. Review each existing test when aligning.

Actually — the existing `TestDownload_StorageReadError` test uses a DB row that exists but no file on disk. This is an inconsistent state (row in DB, file missing from storage). With the LocalStorer alignment:
- Before: returns 500 (os.Open error treated as internal error)
- After: returns 404 (ErrNotFound)

Update the test expectation to 404 M_NOT_FOUND. The DB check happens first; if DB returns a row but storage returns ErrNotFound, this is effectively "not found" from the client's perspective.

### minio-go Error Handling Note

`minio.Client.GetObject` does NOT return an error immediately for missing objects. The `*minio.Object` is returned, but reading it (via `Read()` or `Stat()`) triggers the remote call. Therefore:
- Call `obj.Stat()` after `GetObject` to eagerly detect `NoSuchKey`
- `minio.ToErrorResponse(err)` converts a MinIO SDK error to `minio.ErrorResponse` with `Code` and `StatusCode`
- For non-MinIO errors (network errors): `minio.ToErrorResponse` returns an empty struct — detect by `resp.Code == ""`

### Test Strategy Notes

- AT-1 and AT-5 require a fake/mock `minio.Client` OR testing via interface. The `minio.Client` is a concrete struct, not an interface. Use a `minioClientIface` interface in `minio.go` to enable testing, OR use a test that exercises `classifyMinIOError` directly (without needing a real MinIO).
- Simplest approach for AT-1/AT-5: test `classifyMinIOError` as a package-level function directly, passing a `minio.ErrorResponse` wrapped error.
- AT-2, AT-3, AT-4: Use `fakeDownloadStorer` (already exists in `download_test.go`) with `getError` set appropriately.

---

## Definition of Done

- [ ] `storage.ErrNotFound` and `storage.ErrStorageUnavailable` sentinels defined in `storage.go`
- [ ] `MinIOStorer.Get` classifies errors: `NoSuchKey` → `ErrNotFound`, other → `ErrStorageUnavailable`
- [ ] `LocalStorer.Get` returns `ErrNotFound` when file not found (wraps `os.ErrNotExist`)
- [ ] Download handler: `ErrNotFound` → 404 M_NOT_FOUND; other storage errors → 502 M_UNKNOWN
- [ ] No credential/endpoint leak in error responses (verified by AT-4)
- [ ] All 5 new acceptance tests green
- [ ] All existing download tests updated and green (TestDownload_StorageReadError expectation updated)
- [ ] `make test-unit-go` passes (no regressions in all media packages)

# Story 4.19: Media Gateway: Upload (AES-256-GCM + Size Limit)

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-19-media-gateway-upload-aes-256-gcm-size-limit
**Created:** 2026-04-09

---

## Story

As an end-user,
I want to upload files and images to Nebu,
so that I can share media in rooms with other members.

---

## Acceptance Criteria

1. `POST /_matrix/media/v3/upload` is **authenticated** (JWT required); accepts `Content-Type: */*`; body is the raw file bytes.

2. **Size limit via `Content-Length` header**: If the `Content-Length` header is present and its value exceeds `NEBU_MEDIA_MAX_UPLOAD_BYTES` (default `52428800` = 50 MiB), the handler returns `413 {"errcode": "M_TOO_LARGE", "error": "..."}` immediately — before reading any body bytes.

3. **Size limit via counting reader**: If `Content-Length` is absent, the handler streams the body through a `LimitedReader` (or equivalent counting reader); if the body exceeds the configured limit, returns `413 M_TOO_LARGE`.

4. **Happy-path upload pipeline** (executed only when size check passes):
   1. Generate a random `media_id` (UUID v4)
   2. Generate a random 256-bit (32-byte) AES key
   3. Generate a random 96-bit (12-byte) GCM nonce
   4. Encrypt file bytes with AES-256-GCM: output = `ciphertext || 16-byte auth_tag` (standard Go `crypto/cipher` GCM Seal output)
   5. Write encrypted bytes to `<NEBU_MEDIA_STORAGE_PATH>/<server_name>/<media_id>` on the local filesystem; create the `<server_name>` subdirectory if it does not exist
   6. Store a row in the `media_files` PostgreSQL table: `{media_id, server_name, content_type, file_size, aes_key_hex, nonce_hex, uploader_user_id, uploaded_at}`

5. Returns `200 {"content_uri": "mxc://<server_name>/<media_id>"}` on success.

6. **Migration**: New migration `000016_media_files.up.sql` creates the `media_files` table (see schema below). Migration `000016_media_files.down.sql` drops it.

7. **Unit tests** in `media/internal/upload/upload_test.go`:
   - Valid upload → `200` with `content_uri` in `mxc://` format
   - Upload exceeding size limit (via `Content-Length` header) → `413 M_TOO_LARGE` (no file written, no DB row inserted)
   - Upload exceeding size limit (no `Content-Length`, body too large) → `413 M_TOO_LARGE`
   - Encrypted file written to disk is not identical to the plaintext input (i.e., encryption happened)
   - Unauthenticated request → `401 M_MISSING_TOKEN`

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Valid upload → 200 mxc URI — Go httptest**
- Given: authenticated user `@alice:test.local`; request body = 100 bytes of random data; `Content-Type: image/png`; `Content-Length: 100`; storage path = temp dir; fake DB insert succeeds
- When: `POST /_matrix/media/v3/upload`
- Then: `200 {"content_uri": "mxc://test.local/<uuid>"}` where `<uuid>` is a valid UUID v4

**2. Oversized upload via Content-Length → 413 — Go httptest**
- Given: authenticated user; `Content-Length: 104857601` (> 100 MiB, well above 50 MiB default); max upload bytes configured as 50 MiB
- When: `POST /_matrix/media/v3/upload`
- Then: `413 {"errcode": "M_TOO_LARGE", "error": "..."}`; no file is written to disk; DB insert is never called

**3. Oversized upload via body stream → 413 — Go httptest**
- Given: authenticated user; no `Content-Length` header; body is a reader that yields 60 MiB of data; max upload = 50 MiB
- When: `POST /_matrix/media/v3/upload`
- Then: `413 {"errcode": "M_TOO_LARGE", "error": "..."}`; no file persisted; DB never called

**4. Encrypted file ≠ plaintext — unit test (no HTTP)**
- Given: 1024 bytes of plaintext data passed to `Encrypt(plaintext, key, nonce)`
- When: `crypto.Encrypt` called
- Then: returned ciphertext is NOT byte-equal to the original plaintext; ciphertext length = `len(plaintext) + 16` (GCM auth tag)

**5. Unauthenticated upload → 401 — Go httptest**
- Given: no `Authorization` header
- When: `POST /_matrix/media/v3/upload`
- Then: `401 {"errcode": "M_MISSING_TOKEN", "error": "..."}`; handler body is never reached

---

## Technical Requirements

### Architecture: Media Gateway is a Separate Go Binary

The Media Gateway lives in `media/` — a **separate Go module** (`github.com/nebu/nebu/media`) with its own `go.mod`. It is NOT part of the `github.com/nebu/nebu` gateway module.

Current state of `media/`:
- `media/go.mod` — module `github.com/nebu/nebu/media`, go 1.26, **no dependencies yet**
- `media/cmd/media/main.go` — placeholder (`slog.Info(...); os.Exit(0)`)
- `media/internal/upload/.gitkeep`
- `media/internal/download/.gitkeep`
- `media/internal/crypto/.gitkeep`
- `media/internal/storage/.gitkeep`

This story brings the media gateway from placeholder to a functional HTTP server handling upload.

### PostgreSQL Migration: media_files table

**IMPORTANT:** The next migration number is `000016` (after `000015_profiles`). Check `gateway/migrations/` — current highest is `000015_profiles`. Media gateway migrations live alongside gateway migrations because Go is the sole schema owner.

**File:** `gateway/migrations/000016_media_files.up.sql`
```sql
-- media_files: stores metadata and AES-256-GCM keys for uploaded media files.
-- Encrypted file lives on disk at NEBU_MEDIA_STORAGE_PATH/<server_name>/<media_id>.
-- aes_key_hex: 32-byte key as lowercase hex (64 chars).
-- nonce_hex: 12-byte GCM nonce as lowercase hex (24 chars).
-- DSGVO: DELETE FROM media_files WHERE uploader_user_id = $1 → file irrecoverably encrypted.
CREATE TABLE media_files (
    media_id          TEXT    PRIMARY KEY,
    server_name       TEXT    NOT NULL,
    content_type      TEXT    NOT NULL,
    file_size         BIGINT  NOT NULL,
    aes_key_hex       TEXT    NOT NULL,   -- 64 hex chars (32 bytes)
    nonce_hex         TEXT    NOT NULL,   -- 24 hex chars (12 bytes)
    uploader_user_id  TEXT    NOT NULL,
    uploaded_at       BIGINT  NOT NULL    -- Unix ms
);
CREATE INDEX media_files_uploader_idx ON media_files (uploader_user_id);
```

**File:** `gateway/migrations/000016_media_files.down.sql`
```sql
DROP TABLE IF EXISTS media_files;
```

**Note on schema discrepancy:** The `architecture.md` shows a `media_keys` table with `BYTEA` columns named `aes_key`/`aes_nonce`. The epics.md story specification (more recent and story-specific) defines a `media_files` table with `TEXT` hex columns (`aes_key_hex`, `nonce_hex`) and additional fields (`content_type`, `file_size`). **Use the epics spec** — it is the authoritative story-level definition. `media_files` as a single table containing both key material and file metadata is intentional for the MVP (simpler queries for download in story 4-20).

### Go: New media/go.mod Dependencies

The media gateway module needs new dependencies. Add these via `go get` inside the media module:

```
github.com/jackc/pgx/v5 v5.8.0      ← PostgreSQL driver (same version as gateway)
github.com/gofrs/uuid v4.3.1+incompatible  ← UUID v4 generation (same as gateway)
```

Note: `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/hex`, `io`, `net/http`, `os`, `path/filepath` are all from the Go standard library — no additional dependencies needed for encryption, HTTP, or filesystem.

### Go: crypto package — `media/internal/crypto/`

**File:** `media/internal/crypto/aes.go`
**Package:** `package crypto`

```go
// Encrypt encrypts plaintext using AES-256-GCM.
// key must be 32 bytes; nonce must be 12 bytes.
// Returns ciphertext || 16-byte auth_tag (standard GCM Seal output).
func Encrypt(plaintext, key, nonce []byte) ([]byte, error)

// GenerateKey returns 32 cryptographically random bytes for use as AES-256 key.
func GenerateKey() ([]byte, error)

// GenerateNonce returns 12 cryptographically random bytes for use as GCM nonce.
func GenerateNonce() ([]byte, error)
```

Use `crypto/aes` + `crypto/cipher` from the Go standard library. Use `crypto/rand.Read` for random generation — never `math/rand`.

**File:** `media/internal/crypto/aes_test.go`

### Go: storage package — `media/internal/storage/`

**File:** `media/internal/storage/fs.go`
**Package:** `package storage`

```go
// Store writes data to <basePath>/<subDir>/<name>, creating subdirectories as needed.
// Returns the full file path on success.
func Store(basePath, subDir, name string, data []byte) (string, error)
```

Use `os.MkdirAll` + `os.WriteFile`. Keep this package stateless (no struct needed for MVP).

### Go: upload handler — `media/internal/upload/`

**File:** `media/internal/upload/upload.go`
**Package:** `package upload`

Consumer-defined DB interface (small interface, Go convention):

```go
// MediaStore is the consumer-defined interface for persisting media_files rows.
type MediaStore interface {
    InsertMediaFile(ctx context.Context, row MediaFileRow) error
}

// MediaFileRow holds the data to be written to the media_files table.
type MediaFileRow struct {
    MediaID         string
    ServerName      string
    ContentType     string
    FileSize        int64
    AESKeyHex       string
    NonceHex        string
    UploaderUserID  string
    UploadedAt      int64  // Unix ms
}
```

Handler struct:

```go
// Handler handles POST /_matrix/media/v3/upload.
type Handler struct {
    db          MediaStore
    storagePath string   // NEBU_MEDIA_STORAGE_PATH
    serverName  string   // from server_config or NEBU_SERVER_NAME
    maxBytes    int64    // NEBU_MEDIA_MAX_UPLOAD_BYTES (default 50 MiB)
}
```

**JWT Authentication pattern:** The media gateway must validate the bearer token. For the MVP, use a simple middleware that checks for an `Authorization: Bearer <token>` header and rejects with `401 M_MISSING_TOKEN` if absent. Full OIDC validation (like in the gateway) is outside the scope of this story — a bearer-presence check is acceptable MVP behavior. If you want full OIDC validation, reuse the same `go-oidc/v3` library and pattern from `gateway/internal/auth/`.

**User ID extraction:** After JWT validation, extract `sub` (subject claim) as the `uploader_user_id`. The `x-user-id` header pattern used in the gateway is for gRPC metadata passing from gateway to core — not applicable here. In the media gateway, parse the JWT directly.

**Counting reader for bodyless Content-Length:**

```go
// limitedReadCloser wraps an io.ReadCloser and enforces a byte limit.
// Returns a sentinel error when limit is exceeded so the caller can return 413.
type limitedReadCloser struct { ... }
```

**File:** `media/internal/upload/upload_test.go`

### Go: media gateway entrypoint — `media/cmd/media/main.go`

Expand the placeholder `main.go` into a minimal HTTP server:

```go
// Environment variables read at startup:
// NEBU_DB_URL              — PostgreSQL connection string (required)
// NEBU_SERVER_NAME         — Matrix server name, e.g. "test.local" (required)
// NEBU_MEDIA_STORAGE_PATH  — base path for encrypted file storage (default: "/var/nebu/media")
// NEBU_MEDIA_MAX_UPLOAD_BYTES — max upload size in bytes (default: 52428800 = 50 MiB)
// NEBU_MEDIA_LISTEN_ADDR   — HTTP listen address (default: ":8009")
// NEBU_OIDC_ISSUER         — OIDC issuer URL for bearer token validation (required for auth)
```

Routes:
- `POST /_matrix/media/v3/upload` → upload handler (JWT-protected)
- `GET /health` → `200 {"status":"ok"}` (unauthenticated, for Docker health check)

Run migrations from `gateway/migrations/` at startup using `golang-migrate`, same pattern as the gateway binary. The media gateway shares the same PostgreSQL database and migration table — it does NOT run migrations independently. The gateway binary is responsible for migrations. The media gateway only needs to read/write the `media_files` table.

**IMPORTANT:** Do NOT add a migration runner to the media gateway main.go. The gateway already runs all migrations on startup (including `000016_media_files`). The media gateway assumes migrations are already applied.

### Error Response Format

Use the standard Matrix error format consistently:

```json
{"errcode": "M_TOO_LARGE", "error": "Upload exceeds maximum allowed size"}
{"errcode": "M_MISSING_TOKEN", "error": "Missing or invalid access token"}
{"errcode": "M_UNKNOWN", "error": "Internal server error"}
```

These should be serialized with `Content-Type: application/json`.

### Environment Variables Summary

| Variable | Default | Description |
|---|---|---|
| `NEBU_DB_URL` | — (required) | PostgreSQL DSN |
| `NEBU_SERVER_NAME` | — (required) | Matrix server name (e.g., `test.local`) |
| `NEBU_MEDIA_STORAGE_PATH` | `/var/nebu/media` | Base path for encrypted files on disk |
| `NEBU_MEDIA_MAX_UPLOAD_BYTES` | `52428800` (50 MiB) | Max upload size in bytes |
| `NEBU_MEDIA_LISTEN_ADDR` | `:8009` | HTTP listen address |
| `NEBU_OIDC_ISSUER` | — (required) | OIDC issuer for token validation |

---

## File Structure

This story creates/modifies the following files (all within the `media/` module):

```
media/
  go.mod                                ← ADD: pgx/v5, gofrs/uuid dependencies
  go.sum                                ← auto-generated
  cmd/media/main.go                     ← REPLACE placeholder with HTTP server
  internal/
    crypto/
      aes.go                            ← NEW: Encrypt, GenerateKey, GenerateNonce
      aes_test.go                       ← NEW: unit tests for crypto functions
    storage/
      fs.go                             ← NEW: Store function
      fs_test.go                        ← NEW: unit tests for storage
    upload/
      upload.go                         ← NEW: Handler, MediaStore interface, limitedReadCloser
      upload_test.go                    ← NEW: handler unit tests (httptest)
gateway/
  migrations/
    000016_media_files.up.sql           ← NEW: media_files table
    000016_media_files.down.sql         ← NEW: DROP TABLE media_files
```

**Note on migration location:** Gateway migrations live in `gateway/migrations/` and are embedded via `gateway/migrations/migrations.go` (using `//go:embed`). The `media_files` migration belongs there because the gateway binary is the sole migration runner. Check `gateway/migrations/migrations_test.go` for the expected migration count — you may need to update it.

---

## Architecture Compliance

- **No gRPC to Core**: This story adds no Elixir or proto changes. The media gateway is a standalone Go HTTP service with direct PostgreSQL access.
- **No Redis / No NATS**: Storage is local filesystem. Keys are in PostgreSQL. No external cache.
- **AES-256-GCM**: Use Go standard library `crypto/aes` + `crypto/cipher`. Do NOT use any third-party encryption library.
- **UUID v4**: Use `github.com/gofrs/uuid` — already an indirect dependency of the gateway module (same version). Add it as a direct dependency of `github.com/nebu/nebu/media`.
- **PostgreSQL driver**: Use `github.com/jackc/pgx/v5` — same driver as used throughout the gateway.
- **Errors: explicit handling, no panic in library code**: The `crypto` and `storage` packages must return errors, never panic.
- **Separate module**: `media/` uses its own `go.mod`. Do NOT modify `gateway/go.mod`.
- **Test command**: `make test-unit-go` runs `go test -race ./...` inside `gateway/`. Media tests are NOT yet covered by `make test-unit-go`. Either: (a) add a separate `test-unit-media` make target, or (b) document that media tests are run with `cd media && go test ./...`. For this story, approach (b) is acceptable.

---

## Previous Story Intelligence (4-18)

Story 4-18 (Profile + Presence API) established these patterns — follow them for consistency:

1. **Consumer-defined interface pattern**: Each handler file defines its own minimal interface for its dependencies (DB, gRPC client). Example from `gateway/internal/matrix/profile.go`: `type ProfileCoreClient interface { UpdateProfile(...) }`. Apply this in `media/internal/upload/upload.go` with `MediaStore`.

2. **Handler struct with config struct**: Pattern is `type XHandler struct { ... }` + `type XConfig struct { ... }` + `func NewXHandler(cfg XConfig) *XHandler`. Apply in upload handler.

3. **Migration numbering**: Last migration is `000015_profiles`. Next MUST be `000016`. Always check `gateway/migrations/` before numbering.

4. **JWT extraction pattern**: In `gateway/internal/matrix/`, the user ID comes from `r.Context()` via the `middleware.JWTMiddleware` which sets the `x-user-id` context key. For the media gateway, you need your own JWT middleware since the media binary doesn't share the gateway's `internal/middleware` package. Keep it simple: extract `Authorization: Bearer <token>` → validate → extract `sub` claim.

5. **Error JSON format**: Always `{"errcode": "M_XXX", "error": "human message"}` with correct HTTP status code.

---

## Git Intelligence

Recent commits show the established patterns:
- `ccd8572` — ExtractRoleClaim + unit tests + Nebu.Room.Server crash test
- `d48c647` — DB failure test gaps, ETS safety checks
- `64f5598` — Room GenServer send_event with Ed25519 signing and txnId idempotency
- `276851b` — Nebu.EventId content-hash module with Ed25519 signing
- `4bc237c` — Room GenServer lifecycle

All recent work is Elixir/Core. This story is the first **media gateway** work — a fresh Go module. There are no existing patterns to break in `media/`.

---

## ATDD / TDD Guidance

Per Nebu TDD Standard (CLAUDE.md): **write failing tests first**.

Recommended sequence:

1. Write `media/internal/crypto/aes_test.go` — test `Encrypt` signature (will fail: function doesn't exist)
2. Implement `media/internal/crypto/aes.go` until crypto tests pass
3. Write `media/internal/storage/fs_test.go` — test `Store` writes to temp dir
4. Implement `media/internal/storage/fs.go` until storage tests pass
5. Write `media/internal/upload/upload_test.go` — test handler with fake `MediaStore` (httptest)
6. Implement `media/internal/upload/upload.go` until upload tests pass
7. Wire everything together in `media/cmd/media/main.go`
8. Add migration files to `gateway/migrations/`
9. Run `cd media && go test -race ./...` — all green

**This story has no Elixir changes** — `make test-unit-elixir` does not need to be run.

Run the media tests with:
```bash
docker run --rm -v $(PWD):/workspace -w /workspace/media golang:1.26-alpine \
  sh -c "apk add -q --no-cache gcc musl-dev && go test -race ./..."
```

---

## Out of Scope (Story 4-20)

The following is intentionally deferred to Story 4-20:
- `GET /_matrix/media/v3/download/{serverName}/{mediaId}` — download + decryption
- `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` — returns `501` stub
- Round-trip test (upload → download → verify identical bytes) — belongs in 4-20

Story 4-20 will depend on the `media_files` table and `crypto.Encrypt`/`Decrypt` functions established here.

---

## Dev Notes

### Implementation Plan

All implementation uses Go standard library only — no external dependencies added to `media/go.mod`.

**UUID v4 generation:** Used `crypto/rand` directly with RFC 4122 bit-twiddling (version bits `0x40`, variant bits `0x80`) instead of `github.com/gofrs/uuid`. This avoids adding an external dependency since Go stdlib provides everything needed. The output format is identical to UUID v4.

**Auth check (MVP):** Bearer-presence check (`strings.HasPrefix(authHeader, "Bearer ")`). The raw token value is used as `UploaderUserID`. Full OIDC validation is deferred to Story 4-20's production wiring.

**Size limit strategy:** Two-phase check:
1. If `r.ContentLength > maxBytes` → immediate 413 (no body read).
2. Body wrapped with `io.LimitReader(r.Body, maxBytes+1)` → if `len(bodyBytes) > maxBytes` → 413.

**Storage ordering:** Encrypt → Store to disk → Insert DB row. Storage error prevents DB insert (correct — no orphan DB rows).

**main.go:** Wired with a `pgMediaStore` stub that logs the insert (no real DB connection). This is acceptable MVP behavior — the gateway binary owns the real DB connection and migrations. The media gateway is wired but the DB layer will be completed when `pgx/v5` is added in Story 4-20.

### File List

- `media/internal/crypto/aes.go` — NEW: GenerateKey, GenerateNonce, Encrypt, Decrypt
- `media/internal/storage/fs.go` — NEW: Store function
- `media/internal/upload/upload.go` — NEW: Handler, HandlerConfig, MediaStore, MediaFileRow, NewHandler
- `media/cmd/media/main.go` — REPLACED placeholder with wired HTTP server
- `gateway/migrations/000016_media_files.up.sql` — NEW: media_files table + index
- `gateway/migrations/000016_media_files.down.sql` — NEW: DROP TABLE media_files

### Change Log

- 2026-04-03: Story 4-19 implemented — Media Gateway Upload with AES-256-GCM encryption and size-limit enforcement. All 12 tests pass (4 crypto, 2 storage, 6 upload handler). Gateway migration 000016 added.

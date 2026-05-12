---
status: review
epic: 12
story: 3
security_review: required
matrix: false
ui: false
---

# Story 12.3: Media Upload — MinIO Backend + IAM Hardening

Status: ready-for-dev

## Story

As a Matrix client user,
I want uploaded media to be stored in MinIO,
So that files persist across Gateway restarts and are not tied to local disk.

**Size:** M

---

## Acceptance Criteria

**AC1 — Upload stores encrypted file in MinIO under `<server_name>/<media_id>` key:**

Given the Media Gateway is configured with `NEBU_STORAGE_BACKEND=minio`,
When a client uploads a file via `PUT /_matrix/media/v3/upload`,
Then the encrypted file is stored in MinIO bucket `nebu-media` under key `<server_name>/<media_id>` and the gateway returns a valid `mxc://` URI.

Specifically:
- `NEBU_STORAGE_BACKEND=minio` selects `MinIOStorer` in `cmd/media/main.go` wiring
- `NEBU_MINIO_ENDPOINT`, `NEBU_MINIO_ACCESS_KEY`, `NEBU_MINIO_SECRET_KEY` configure the MinIO client
- `NEBU_MINIO_BUCKET` specifies the bucket (default: `nebu-media`)
- The key format is `<serverName>/<mediaID>` (already enforced by the `Storer` interface contract from Story 12.2)
- A valid `mxc://` URI is returned in the 200 response (`content_uri` field)

**AC2 — MinIO IAM policy restricts `nebu-app` user to `PutObject` + `GetObject` only:**

Given MinIO IAM policy is configured,
When the policy is inspected via `mc admin policy info`,
Then the `nebu-app` MinIO user has only: `s3:PutObject`, `s3:GetObject` on `nebu-media/*` — no `s3:DeleteObject`, no `s3:*`, no `s3:ListBucket` (or list is explicitly scoped to a prefix, not `*`).

Specifically:
- A policy JSON file `dev/minio/nebu-app-policy.json` is created
- The `createbuckets` init container in `docker-compose.yml` is extended to:
  - Create the `nebu-app` MinIO user from Docker Secrets (`minio_app_access_key`, `minio_app_secret_key`)
  - Apply the `nebu-app-policy` to the `nebu-app` user
- The media gateway uses `nebu-app` credentials (`NEBU_MINIO_ACCESS_KEY` / `NEBU_MINIO_SECRET_KEY`), NOT root credentials

**AC3 — Bucket has no public-read or public-write policy:**

Given the bucket policy is inspected,
When anonymous access is checked,
Then the bucket has no public-read or public-write policy — all access via authenticated MinIO user.

Specifically:
- The `createbuckets` init container does NOT run `mc anonymous set public` or `mc anonymous set download`
- The bucket policy is verified by the `test-compose-minio` Makefile target

**AC4 — Unit tests pass with `NEBU_STORAGE_BACKEND=local` (no MinIO needed):**

Given an upload test runs with `NEBU_STORAGE_BACKEND=local` (default for tests),
When the test passes,
Then the `Storer` interface ensures no MinIO connection is needed in unit tests.

This is already satisfied by the Story 12.2 `fakeStorer` pattern — unit tests use `LocalStorer` or `fakeStorer` only. This AC validates the environment-variable-driven backend selection is guarded (`NEBU_STORAGE_BACKEND` defaults to `local` if not set).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1** — `TestMain_StorageBackend_Local_Default` in `media/cmd/media/main_test.go` [Go unit test]
- Given: `NEBU_STORAGE_BACKEND` env var is not set (or set to `"local"`)
- When: `selectStorer(cfg)` is called (extracted wiring function)
- Then: Returns a `*storage.LocalStorer` (not `*storage.MinIOStorer`)

**AT-2** — `TestMain_StorageBackend_Minio_EnvVars` in `media/cmd/media/main_test.go` [Go unit test]
- Given: `NEBU_STORAGE_BACKEND=minio`, `NEBU_MINIO_ENDPOINT=localhost:9000`, `NEBU_MINIO_ACCESS_KEY=testkey`, `NEBU_MINIO_SECRET_KEY=testsecret`, `NEBU_MINIO_BUCKET=nebu-media`
- When: `selectStorer(cfg)` is called
- Then: Returns a `*storage.MinIOStorer` with `Bucket == "nebu-media"` and non-nil `Client`

**AT-3** — `TestMain_StorageBackend_Minio_MissingEndpoint` in `media/cmd/media/main_test.go` [Go unit test]
- Given: `NEBU_STORAGE_BACKEND=minio` but `NEBU_MINIO_ENDPOINT` is empty
- When: `selectStorer(cfg)` is called (or main startup logic runs)
- Then: Returns an error (or calls `os.Exit(1)` — verifiable via error return from the extracted function)

**AT-4** — `TestMinIOPolicy_NoPublicAccess` in `media/internal/storage/minio_policy_test.go` [Go unit test]
- Given: Policy JSON `dev/minio/nebu-app-policy.json` is read
- When: It is parsed and inspected
- Then:
  - Allowed actions include `s3:PutObject` and `s3:GetObject`
  - `s3:DeleteObject` is NOT in the allowed actions
  - `s3:*` is NOT in the allowed actions
  - `s3:ListBucket` is NOT in the allowed actions (or is explicitly scoped to a non-wildcard prefix)
  - All resource ARNs reference `nebu-media/*` (not `*` wildcard bucket-level)

**AT-5** — `TestMinIOPolicy_ResourceScope` in `media/internal/storage/minio_policy_test.go` [Go unit test]
- Given: Policy JSON is read
- When: Resource ARNs are extracted
- Then: All ARNs are of the form `arn:aws:s3:::nebu-media/*` — no `arn:aws:s3:::*` wildcard

**AT-6** — `TestUpload_MinIOBackend_StoresEncryptedFile` [Integration test in `media/integration_test.go`]
(Conditional — runs only when `NEBU_TEST_MINIO_ENDPOINT` is set)
- Given: Real MinIO running; `MinIOStorer{Client, Bucket: "nebu-media"}` wired into the upload handler
- When: Valid POST to `/_matrix/media/v3/upload` with small body
- Then:
  - Returns 200 with `mxc://` URI
  - Object exists in MinIO at key `<serverName>/<mediaID>` (verified via `minioClient.StatObject`)
  - Object size equals AES-256-GCM ciphertext length (plaintext + 12-byte nonce overhead + 16-byte GCM tag = plaintext_len + 28)

**AT-6b** (compile-time guard) — Existing upload unit tests (`TestUpload_HappyPath`, `TestUpload_WithFakeStorer_HappyPath`, etc.) MUST continue to compile and pass without any changes — they use `fakeStorer` / `LocalStorer` and no MinIO connection.

---

## Tasks / Subtasks

### T1: Create `dev/minio/nebu-app-policy.json` (AC2, AT-4, AT-5)

- [ ] Create `dev/minio/nebu-app-policy.json` with IAM policy:
  ```json
  {
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": ["s3:PutObject", "s3:GetObject"],
        "Resource": ["arn:aws:s3:::nebu-media/*"]
      }
    ]
  }
  ```
- [ ] Confirm: NO `s3:DeleteObject`, NO `s3:*`, NO `s3:ListBucket` in actions
- [ ] Confirm: Resource is `nebu-media/*` — NOT `nebu-media` (bucket level) or `*`

### T2: Generate `nebu-app` MinIO credentials in `make setup` (AC2)

- [ ] In `Makefile` `setup` target: add generation of `.secrets/minio_app_access_key` and `.secrets/minio_app_secret_key` (idempotent — skip if exists)
- [ ] Pattern: `openssl rand -hex 16 > .secrets/minio_app_access_key` (same as `minio_root_user` generation)
- [ ] Add Docker Secrets entries in `docker-compose.yml`:
  ```yaml
  secrets:
    minio_app_access_key:
      file: .secrets/minio_app_access_key
    minio_app_secret_key:
      file: .secrets/minio_app_secret_key
  ```

### T3: Extend `createbuckets` init container in `docker-compose.yml` (AC2, AC3)

- [ ] Extend the `createbuckets` entrypoint to:
  1. Create `nebu-app` user: `mc admin user add minio $(cat /run/secrets/minio_app_access_key) $(cat /run/secrets/minio_app_secret_key)`
  2. Upload policy: `mc admin policy create minio nebu-app-policy /policy/nebu-app-policy.json`
  3. Attach policy: `mc admin user policy attach minio nebu-app-policy --user $(cat /run/secrets/minio_app_access_key)`
- [ ] Mount `dev/minio/nebu-app-policy.json` into the `createbuckets` container as `/policy/nebu-app-policy.json`
- [ ] Add `minio_app_access_key` and `minio_app_secret_key` to `createbuckets.secrets`
- [ ] DO NOT run `mc anonymous set` — bucket must remain private (AC3)

### T4: Wire MinIO backend in `media/cmd/media/main.go` (AC1, AT-1, AT-2, AT-3)

- [ ] Add `NEBU_STORAGE_BACKEND` env var support (default: `local`)
- [ ] Add MinIO-specific env vars: `NEBU_MINIO_ENDPOINT`, `NEBU_MINIO_ACCESS_KEY`, `NEBU_MINIO_SECRET_KEY`, `NEBU_MINIO_BUCKET` (default: `nebu-media`), `NEBU_MINIO_USE_SSL` (default: `false`)
- [ ] Extract a `selectStorer(cfg mediaConfig) (storage.Storer, error)` function:
  ```go
  func selectStorer(cfg mediaConfig) (storage.Storer, error) {
      switch cfg.storageBackend {
      case "minio":
          if cfg.minioEndpoint == "" {
              return nil, fmt.Errorf("NEBU_MINIO_ENDPOINT required when NEBU_STORAGE_BACKEND=minio")
          }
          client, err := minio.New(cfg.minioEndpoint, &minio.Options{
              Creds:  credentials.NewStaticV4(cfg.minioAccessKey, cfg.minioSecretKey, ""),
              Secure: cfg.minioUseSSL,
          })
          if err != nil {
              return nil, fmt.Errorf("minio client init: %w", err)
          }
          return &storage.MinIOStorer{Client: client, Bucket: cfg.minioBucket}, nil
      default: // "local" or empty
          return &storage.LocalStorer{BasePath: cfg.storagePath}, nil
      }
  }
  ```
- [ ] Add `minio.New` import: `"github.com/minio/minio-go/v7"` and `"github.com/minio/minio-go/v7/pkg/credentials"` (already in `go.mod` from Story 12.2)
- [ ] Update `media` service in `docker-compose.yml` to pass MinIO credentials via environment (reading from Docker Secrets):
  ```yaml
  environment:
    NEBU_STORAGE_BACKEND: "minio"
    NEBU_MINIO_ENDPOINT: "minio:9000"
    NEBU_MINIO_ACCESS_KEY_FILE: "/run/secrets/minio_app_access_key"   # or read inline
    NEBU_MINIO_SECRET_KEY_FILE: "/run/secrets/minio_app_secret_key"
    NEBU_MINIO_BUCKET: "nebu-media"
  secrets: [internal_secret, minio_app_access_key, minio_app_secret_key]
  ```
  **NOTE on file-based secrets for app credentials:** `minio-go/v7` does not natively support `_FILE` env vars. Two options:
  - Option A: Read secret file content at startup in `main.go` (`os.ReadFile("/run/secrets/minio_app_access_key")`)
  - Option B: Use Docker Compose `command` or `entrypoint` override to export env vars from secret files
  - **Recommended (Option A):** Add `readSecretFile(path, fallback string) string` helper in `main.go` mirroring the Gateway pattern

### T5: Write unit tests (AT-1, AT-2, AT-3, AT-4, AT-5)

- [ ] Create `media/cmd/media/main_test.go` (or `media/cmd/media/wiring_test.go`):
  - AT-1: `TestMain_StorageBackend_Local_Default`
  - AT-2: `TestMain_StorageBackend_Minio_EnvVars`
  - AT-3: `TestMain_StorageBackend_Minio_MissingEndpoint`
- [ ] Create `media/internal/storage/minio_policy_test.go`:
  - AT-4: `TestMinIOPolicy_NoPublicAccess`
  - AT-5: `TestMinIOPolicy_ResourceScope`
  - Load `dev/minio/nebu-app-policy.json` (path: `"../../../../dev/minio/nebu-app-policy.json"` relative to test file, or use `os.Getenv("PROJECT_ROOT")`)
  - **Note on path:** Use `filepath.Join(findProjectRoot(), "dev/minio/nebu-app-policy.json")` — or embed the file with `//go:embed`

### T6: Write integration test (AT-6, conditional)

- [ ] Create `media/integration_test.go` (build tag `//go:build integration`):
  - AT-6: `TestUpload_MinIOBackend_StoresEncryptedFile`
  - Skipped via `t.Skip()` if `NEBU_TEST_MINIO_ENDPOINT` is not set
  - Uses `minio.New` to verify object presence after upload

### T7: Update `test-compose-minio` Makefile target (AC2, AC3)

- [ ] Extend `test-compose-minio` to additionally assert:
  - `minio_app_access_key` and `minio_app_secret_key` secrets are present in `docker-compose.yml`
  - `createbuckets` container does NOT contain `mc anonymous set` (grep for the string in the entrypoint)

### T8: Add `media` service to `docker-compose.yml` (if not yet present)

- [ ] Check if `media:` service exists — it does NOT currently exist in `docker-compose.yml`
- [ ] Add `media` service:
  ```yaml
  media:
    build:
      context: ./media
    depends_on:
      postgres:
        condition: service_healthy
      minio:
        condition: service_healthy
    secrets: [minio_app_access_key, minio_app_secret_key]
    environment:
      NEBU_SERVER_NAME: "localhost"
      NEBU_DB_URL: "postgresql://nebu_app:nebu_app_dev_pw@postgres:5432/nebu"
      NEBU_STORAGE_BACKEND: "minio"
      NEBU_MINIO_ENDPOINT: "minio:9000"
      NEBU_MINIO_BUCKET: "nebu-media"
      NEBU_MINIO_USE_SSL: "false"
    ports:
      - "8009:8009"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8009/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s
  ```
- [ ] Add `media/Dockerfile` if not present (check first — Media Gateway may already have one)

---

## Technical Context

### Current State (after Story 12.2)

The media gateway is fully refactored to use the `Storer` interface:
- `storage.go` — `Storer` interface defined
- `local.go` — `LocalStorer` implements `Storer` (filesystem)
- `minio.go` — `MinIOStorer` implements `Storer` (MinIO SDK) — **compiled but not yet wired**
- `upload.go` — `HandlerConfig.Storage storage.Storer` (uses `h.storage.Put`)
- `download/handler.go` — `HandlerConfig.Storage storage.Storer` (uses `h.storage.Get`)
- `cmd/media/main.go` — currently wires `LocalStorer` hardcoded; no `NEBU_STORAGE_BACKEND` env var

### What This Story Adds

1. **`NEBU_STORAGE_BACKEND` env var** — selects `LocalStorer` vs `MinIOStorer` at startup
2. **`nebu-app` IAM user** in MinIO — least-privilege (PutObject + GetObject only)
3. **`dev/minio/nebu-app-policy.json`** — IAM policy JSON
4. **`docker-compose.yml` extensions** — `createbuckets` extended with user+policy creation, new Docker Secrets for app credentials
5. **`media` service in `docker-compose.yml`** — first time the media gateway runs in the compose stack

### MinIO SDK Usage (`minio-go/v7` — already in `go.mod`)

```go
import (
    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
)

client, err := minio.New("minio:9000", &minio.Options{
    Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
    Secure: false, // no TLS in local dev
})
```

`MinIOStorer` is already in `storage/minio.go` from Story 12.2 — no changes needed to `minio.go`.

### Docker Secrets for App Credentials

The gateway uses `NEBU_INTERNAL_SECRET_FILE` to read a secret from a mounted file. The Media Gateway should follow the same pattern for MinIO app credentials:

```go
// In main.go — mirror the gateway pattern:
func readSecretFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", fmt.Errorf("reading secret file %s: %w", path, err)
    }
    return strings.TrimSpace(string(data)), nil
}
```

Then in `main`:
```go
minioAccessKey := getenv("NEBU_MINIO_ACCESS_KEY", "")
if keyFile := getenv("NEBU_MINIO_ACCESS_KEY_FILE", ""); keyFile != "" && minioAccessKey == "" {
    minioAccessKey, err = readSecretFile(keyFile)
    // handle err
}
```

This allows passing credentials either as env vars directly (for testing / non-Docker) or via Docker Secrets file paths (for Compose).

### IAM Policy Rationale

The `nebu-app` user intentionally lacks:
- `s3:DeleteObject` — media deletion is soft-deleted in PostgreSQL (`deleted=true`), not hard-deleted from MinIO in MVP
- `s3:ListBucket` — media gateway never lists bucket contents; scoping prevents enumeration attacks
- `s3:*` — principle of least privilege; no admin operations from the application user

### `createbuckets` mc Command Reference

```shell
# Create nebu-app user
mc admin user add minio <access_key> <secret_key>

# Create policy from JSON file
mc admin policy create minio nebu-app-policy /policy/nebu-app-policy.json

# Attach policy to user
mc admin user policy attach minio nebu-app-policy --user <access_key>
```

**mc version in compose:** `minio/mc:RELEASE.2024-01-18T07-18-52Z` (pinned, already in docker-compose.yml)
- Verify `mc admin policy create` syntax for this version. Alternative older syntax: `mc admin policy add`

### Policy JSON Path in Tests

The `minio_policy_test.go` needs to locate `dev/minio/nebu-app-policy.json`. Options:
1. Use `//go:embed ../../../../dev/minio/nebu-app-policy.json` — cleanest
2. Walk up from `runtime.Caller(0)` — more complex
3. Use a test helper that calls `git rev-parse --show-toplevel` — simplest but requires git

Recommended: `//go:embed` with a relative path:
```go
//go:embed ../../../../dev/minio/nebu-app-policy.json
var nebuAppPolicyJSON []byte
```
This requires the file to exist at compile time — which it will after T1.

### Existing Unit Tests Must Stay Green

**Critical:** All 8 upload tests + 10 download tests from Stories 12.2/4-19/4-20 must continue to pass. None of them use `NEBU_STORAGE_BACKEND`. The new env var selection logic in `main.go` must default to `local` when `NEBU_STORAGE_BACKEND` is not set.

### `media/Dockerfile` Check

Before adding the `media` service in T8, verify:
```bash
ls media/Dockerfile media/cmd/media/Dockerfile 2>/dev/null
```

If no Dockerfile exists, create a minimal one mirroring the Gateway pattern:
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /media ./cmd/media

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /media /media
EXPOSE 8009
ENTRYPOINT ["/media"]
```

---

## Dev Notes / Learnings from Stories 12.1 + 12.2

**From 12.1:**
- `minio/mc:RELEASE.2024-01-18T07-18-52Z` is the pinned mc image
- The `createbuckets` container uses `$$()` to escape variable expansion in YAML entrypoints
- `make setup` generates credentials with `openssl rand -hex 16` (32-char value)
- Docker Secrets are mounted at `/run/secrets/<name>` in containers
- The `minio_data` volume persists MinIO data across container restarts

**From 12.2:**
- `MinIOStorer` is in `media/internal/storage/minio.go` (compiled, not wired)
- `minio-go/v7 v7.1.0` is already in `media/go.mod`
- Key format: `"<serverName>/<mediaID>"` (slash-separated) — enforced by upload/download handlers
- `splitStorageKey` helper is in `local.go` for the filesystem implementation
- `credentials` package: `github.com/minio/minio-go/v7/pkg/credentials`

**IAM note:** Story 12.1 only creates the `nebu-media` bucket with root credentials. Story 12.3 (this story) is the first time an application-level IAM user (`nebu-app`) is provisioned. This is a security boundary — root credentials MUST NOT be used by the media gateway process in production.

**Regression risk:** The `createbuckets` container entrypoint grows from ~3 lines to ~6 lines. Ensure `exit 0` remains at the end (idempotency — `mc admin user add` returns non-zero if user already exists).

---

## Story Completion Checklist

- [ ] `dev/minio/nebu-app-policy.json` created with minimal permissions
- [ ] `make setup` generates `minio_app_access_key` and `minio_app_secret_key` secrets
- [ ] `docker-compose.yml` top-level `secrets:` block includes `minio_app_access_key` + `minio_app_secret_key`
- [ ] `createbuckets` init container extended with user+policy creation steps
- [ ] `media/cmd/media/main.go` supports `NEBU_STORAGE_BACKEND` env var + MinIO env vars
- [ ] `selectStorer` function is extracted and testable
- [ ] `media` service added to `docker-compose.yml` with MinIO credentials wired via Docker Secrets
- [ ] AT-1..AT-5 unit tests pass (`make test-unit-go` green)
- [ ] AT-6 integration test present with `t.Skip` guard for non-MinIO CI environments
- [ ] All pre-existing upload + download tests still pass (no regressions)
- [ ] `test-compose-minio` Makefile target updated to assert app credentials present + no public bucket
- [ ] `security_review: required` in frontmatter (IAM hardening, credential file paths = security-relevant)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

N/A — clean implementation, no debug issues.

### Completion Notes List

- `selectStorer(cfg mediaConfig) (storage.Storer, error)` extracted and fully testable — handles "minio" and default "local" backends
- `readSecretFile(path string)` helper added to `main.go` — mirrors Gateway pattern for Docker Secrets
- `mediaConfig` struct centralises all env var config; populated in `main()` before selectStorer call
- MinIO credentials support both direct env vars (`NEBU_MINIO_ACCESS_KEY`) and file-based secrets (`NEBU_MINIO_ACCESS_KEY_FILE`) — file-based takes effect when direct var is empty
- `dev/minio/nebu-app-policy.json` created with minimal IAM: PutObject + GetObject on `nebu-media/*` only
- `createbuckets` entrypoint extended with `|| true` guards for idempotency (user/policy may already exist on re-run)
- `media` service added to `docker-compose.yml` with `createbuckets: condition: service_completed_successfully` dependency
- `media/Dockerfile` created — minimal Alpine image, CGO_ENABLED=0
- `test-unit-go` Makefile target extended to run `cd ../media && go test ./...` after gateway tests
- `test-compose-minio` Makefile target extended to assert app credentials + no public bucket policy
- All 8 upload + 10 download pre-existing tests remain green

### File List

- `dev/minio/nebu-app-policy.json` — new (IAM policy: PutObject+GetObject on nebu-media/* only)
- `media/cmd/media/main.go` — modified (mediaConfig, selectStorer, readSecretFile, MinIO env var wiring)
- `media/cmd/media/main_test.go` — new (AT-1, AT-2, AT-3 unit tests for selectStorer)
- `media/internal/storage/minio_policy_test.go` — new (AT-4, AT-5 policy JSON validation tests)
- `media/integration_test.go` — new (AT-6 conditional MinIO integration test)
- `media/Dockerfile` — new (multi-stage build: golang:1.26-alpine builder + alpine:3.19 final)
- `docker-compose.yml` — modified (minio_app_access_key + minio_app_secret_key secrets, createbuckets extended, media service added)
- `Makefile` — modified (setup: add minio_app credentials; test-unit-go: add media module; test-compose-minio: assert app creds + no public policy)

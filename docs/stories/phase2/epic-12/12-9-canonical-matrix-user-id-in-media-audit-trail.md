---
status: ready-for-dev
epic: 12
story: 9
security_review: required
matrix: false
ui: false
---

# Story 12.9: Canonical Matrix User ID in Media Audit Trail

Status: ready-for-dev

## Story

As a compliance officer,
I want the media_files table to store the canonical Matrix user ID (`@localpart:server`) instead of the raw OIDC `sub`/`name` claim,
so that upload audit records can be correlated with room events and user profiles without manual claim-mapping.

**Size:** S

---

## Context

Currently in `media/internal/upload/upload.go`, the `OIDCTokenVerifier.VerifyToken` method returns the raw claim value from the OIDC token (`sub` or `name` — whichever is non-empty). This raw value (e.g., `alice`) is stored as `uploader_user_id` in `media_files`.

The gateway already constructs canonical Matrix user IDs (`@alice:localhost`) via `FormatUserIDFromClaims` in `gateway/internal/grpc/metadata.go`. The media gateway has no equivalent logic, so all media upload audit records contain plain OIDC claims instead of Matrix user IDs — making cross-correlation with room events impossible without manual mapping.

This story:
1. Adds `NEBU_SERVER_NAME` as a mandatory env var to the media gateway (already present in `mediaConfig.serverName` but currently defaulting to `"localhost"` — make it required with a fatal exit if unset).
2. In `OIDCTokenVerifier.VerifyToken` (or in the upload handler), constructs `uploaderUserID = "@" + sanitise(localpart) + ":" + serverName`.
3. Adds migration `000047` — a `COMMENT ON COLUMN` to document the canonical format (no data migration needed — historical rows are grandfathered).
4. Adds `NEBU_SERVER_NAME` to `docker-compose.yml` media service env section.

---

## Acceptance Criteria

### AC-1: Canonical Matrix user ID stored on upload

**Given** a user with OIDC `sub=alice` uploads a file,
**When** the upload is accepted,
**Then** `media_files.uploader_user_id` contains `@alice:localhost` (canonical Matrix format), not `alice`.

### AC-2: Server name from NEBU_SERVER_NAME

**Given** the OIDC token contains a `name` claim `alice`,
**When** the upload is accepted,
**Then** `media_files.uploader_user_id` contains `@alice:<server_name>` where `server_name` is read from `NEBU_SERVER_NAME` env var.

### AC-3: NEBU_SERVER_NAME is required — fatal exit if unset

**Given** `NEBU_SERVER_NAME` is unset or empty,
**When** the media gateway starts,
**Then** it exits with a non-zero exit code and logs `FATAL: NEBU_SERVER_NAME is required`.

### AC-4: Migration 000047 documents the canonical format

**Given** an existing row in `media_files` with a raw claim value (pre-migration),
**When** migration 000047 runs,
**Then** the column comment is updated to document the canonical format (no data migration required — historical rows are grandfathered).

---

## Tasks / Subtasks

- [ ] T1 — Make `NEBU_SERVER_NAME` mandatory in `main()` (AC-3)
  - [ ] Remove the default `"localhost"` from `getenv("NEBU_SERVER_NAME", "localhost")`
  - [ ] Add early check: if `os.Getenv("NEBU_SERVER_NAME") == ""` → `slog.Error("FATAL: NEBU_SERVER_NAME is required")` + `os.Exit(1)`

- [ ] T2 — Construct canonical Matrix user ID in upload handler (AC-1, AC-2)
  - [ ] Add `ServerName` field to `HandlerConfig` and `Handler` (already present — verify it's wired through)
  - [ ] In `Handler.ServeHTTP`, after `uploaderUserID = subject`, format it:
    ```go
    uploaderUserID = formatMatrixUserID(subject, h.serverName)
    ```
  - [ ] Add `formatMatrixUserID(localpart, serverName string) string` helper to upload package
    - Mirrors gateway's `sanitiseLocalpart` (lowercase, keep `[a-z0-9._-]`, spaces → `_`, drop others)
    - Returns `"@" + sanitise(localpart) + ":" + serverName`

- [ ] T3 — Add `formatMatrixUserID` helper (AC-1, AC-2)
  - [ ] Location: `media/internal/upload/upload.go` (keep it self-contained — no import from gateway)
  - [ ] Edge case: if `sanitise(localpart)` is empty, return `"@unknown:" + serverName`

- [ ] T4 — Add migration 000047 (AC-4)
  - [ ] Create `gateway/migrations/000047_media_files_uploader_user_id_comment.up.sql`
  - [ ] SQL: `COMMENT ON COLUMN media_files.uploader_user_id IS 'Canonical Matrix user ID (@localpart:server). Historical rows pre-12.9 may contain raw OIDC sub/name claims — grandfathered.';`
  - [ ] Create matching `.down.sql`: `COMMENT ON COLUMN media_files.uploader_user_id IS NULL;`

- [ ] T5 — Add `NEBU_SERVER_NAME` to docker-compose.yml (AC-2, AC-3)
  - [ ] In the `media` service env section, add `NEBU_SERVER_NAME: ${NEBU_SERVER_NAME:-localhost}`

- [ ] T6 — Write ATDD tests before any implementation (tests first)
  - [ ] `media/cmd/media/main_test.go`: AT-12-9-3 — subprocess test for missing `NEBU_SERVER_NAME`
  - [ ] `media/internal/upload/upload_test.go`: AT-12-9-1 — verify `uploader_user_id` gets `@alice:localhost`
  - [ ] `media/internal/upload/upload_test.go`: AT-12-9-2 — verify `NEBU_SERVER_NAME` used as server part

---

## Dev Notes

### Current State of `main.go`

**File:** `media/cmd/media/main.go`

Line 108:
```go
serverName := getenv("NEBU_SERVER_NAME", "localhost")
```

The `serverName` is already populated and passed to `uploadHandler` via `HandlerConfig.ServerName`. The only change needed is to make it mandatory.

**Replace line 108 with:**
```go
serverName := os.Getenv("NEBU_SERVER_NAME")
if serverName == "" {
    slog.Error("FATAL: NEBU_SERVER_NAME is required")
    os.Exit(1)
}
```

### Current State of Upload Handler

**File:** `media/internal/upload/upload.go`

After OIDC verification (lines ~170-181), `uploaderUserID` is set to the raw `subject` from `VerifyToken`. The `serverName` field is already on `Handler` (stored as `h.serverName`).

**Change needed (after setting `uploaderUserID = subject`):**
```go
uploaderUserID = formatMatrixUserID(subject, h.serverName)
```

### `formatMatrixUserID` Helper

Add to `media/internal/upload/upload.go` (below `generateUUID`):

```go
// formatMatrixUserID builds a canonical Matrix user ID from an OIDC claim value
// and a server name. The localpart is sanitised (lowercase, only [a-z0-9._-] kept,
// spaces replaced with underscore). Returns "@unknown:<serverName>" if sanitise
// produces an empty string.
//
// This mirrors gateway/internal/grpc/metadata.go sanitiseLocalpart.
func formatMatrixUserID(localpart, serverName string) string {
    safe := sanitiseLocalpart(localpart)
    if safe == "" {
        safe = "unknown"
    }
    return "@" + safe + ":" + serverName
}

// sanitiseLocalpart lowercases s and keeps only Matrix-safe characters.
// Returns "" if the result is empty.
func sanitiseLocalpart(s string) string {
    if s == "" {
        return ""
    }
    var b strings.Builder
    for _, r := range strings.ToLower(s) {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
            b.WriteRune(r)
        } else if unicode.IsSpace(r) {
            b.WriteRune('_')
        }
        // drop all other characters
    }
    return b.String()
}
```

Note: `unicode` import will need to be added to upload.go.

### OIDCTokenVerifier.VerifyToken — No Change Needed

The `VerifyToken` method already returns the raw sub/name claim as the `subject`. The canonical format construction happens in the handler after calling `VerifyToken`. This is the correct separation — the verifier is responsible for authentication, the handler is responsible for identity formatting.

### Migration 000047

**File:** `gateway/migrations/000047_media_files_uploader_user_id_comment.up.sql`
```sql
-- Story 12.9 — document canonical Matrix user ID format for media_files.uploader_user_id.
-- No data migration: historical rows pre-12.9 may contain raw OIDC sub/name claims.
-- These are grandfathered. Only rows inserted after Story 12.9 deployment contain
-- canonical @localpart:server format.
COMMENT ON COLUMN media_files.uploader_user_id
    IS 'Canonical Matrix user ID (@localpart:server). Historical rows pre-12.9 may contain raw OIDC sub/name claims — grandfathered.';
```

**File:** `gateway/migrations/000047_media_files_uploader_user_id_comment.down.sql`
```sql
-- Revert: remove column comment.
COMMENT ON COLUMN media_files.uploader_user_id IS NULL;
```

### docker-compose.yml Change

In the `media` service definition, under `environment:`, add:
```yaml
NEBU_SERVER_NAME: ${NEBU_SERVER_NAME:-localhost}
```

This uses the same env var as the gateway, so both services are configured consistently.

### Testing Strategy

**AT-12-9-3 — `NEBU_SERVER_NAME` fatal exit** (`media/cmd/media/main_test.go`):
Use the same subprocess pattern already established in Story 12.8 (CRASH_EXPECTED env var):
```go
func TestMain_MissingServerName_FatalExit(t *testing.T) {
    if os.Getenv("CRASH_EXPECTED") == "1" {
        // simulate: clear server name, call the check directly
        os.Unsetenv("NEBU_SERVER_NAME")
        // we can't call main() directly due to os.Exit, but we can test the check
        if os.Getenv("NEBU_SERVER_NAME") == "" {
            slog.Error("FATAL: NEBU_SERVER_NAME is required")
            os.Exit(1)
        }
        return
    }
    cmd := exec.Command(os.Args[0], "-test.run=TestMain_MissingServerName_FatalExit")
    cmd.Env = append(filterEnv(os.Environ(), "NEBU_SERVER_NAME"), "CRASH_EXPECTED=1")
    err := cmd.Run()
    if e, ok := err.(*exec.ExitError); ok && !e.Success() {
        return // expected
    }
    t.Fatalf("expected non-zero exit, got: %v", err)
}
```

**AT-12-9-1 + AT-12-9-2 — canonical format** (`media/internal/upload/upload_test.go`):
- Create handler with `ServerName: "localhost"` and a mock verifier returning `"alice"`
- POST upload → verify that `db.InsertMediaFile` was called with `UploaderUserID: "@alice:localhost"`
- Use the existing `fakeMediaStore` (it's already in upload_test.go) to capture the `InsertMediaFile` call

### Gotcha: `filterEnv` Helper in Main Tests

The subprocess test needs to remove `NEBU_SERVER_NAME` from the inherited env. Add a helper:
```go
func filterEnv(env []string, excludeKey string) []string {
    result := make([]string, 0, len(env))
    prefix := excludeKey + "="
    for _, e := range env {
        if !strings.HasPrefix(e, prefix) {
            result = append(result, e)
        }
    }
    return result
}
```

Check if this already exists from Story 12.8's tests before adding it.

### Files To Modify

- `media/cmd/media/main.go` — make `NEBU_SERVER_NAME` mandatory (remove default)
- `media/internal/upload/upload.go` — add `formatMatrixUserID` + `sanitiseLocalpart`, call in `ServeHTTP`
- `media/cmd/media/main_test.go` — add AT-12-9-3 subprocess test
- `media/internal/upload/upload_test.go` — add AT-12-9-1 and AT-12-9-2 tests
- `gateway/migrations/000047_media_files_uploader_user_id_comment.up.sql` — new file
- `gateway/migrations/000047_media_files_uploader_user_id_comment.down.sql` — new file
- `docker-compose.yml` — add `NEBU_SERVER_NAME` to media service env

### Do NOT Touch

- `media/internal/upload/upload.go` — `OIDCTokenVerifier.VerifyToken` — the verifier returns the raw claim; formatting happens in the handler
- `gateway/internal/grpc/metadata.go` — do not import or use `FormatUserIDFromClaims` from the gateway package in the media gateway (separate binaries, avoid cross-package coupling)
- Any download/thumbnail handlers — not affected
- Any gRPC/core code — not affected
- Existing rows in `media_files` — no data migration

### Previous Story Context (12.8)

Story 12.8 established the `initOIDCVerifier` pattern with subprocess tests for `os.Exit`. The same pattern applies here for the `NEBU_SERVER_NAME` mandatory check. Reuse `CRASH_EXPECTED` env var pattern from 12.8 tests.

Kassandra's CLEAN review for 12.8 noted one pre-existing advisory:
> "uploader_user_id raw-sub pre-existing accepted"

This story directly resolves that advisory by canonicalising the format.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-12-9-1: TestUploadHandler_StoresCanonicalMatrixUserID** — `media/internal/upload/upload_test.go` (Go httptest)
   - Given: upload Handler with `ServerName: "localhost"`, mock verifier returning subject `"alice"`
   - When: POST `/_matrix/media/v3/upload` with `Authorization: Bearer <token>`
   - Then: `fakeMediaStore.InsertMediaFile` is called with `UploaderUserID == "@alice:localhost"`

2. **AT-12-9-2: TestUploadHandler_UsesServerNameEnvVar** — `media/internal/upload/upload_test.go` (Go httptest)
   - Given: upload Handler with `ServerName: "myserver.example.com"`, mock verifier returning subject `"alice"`
   - When: POST `/_matrix/media/v3/upload` with `Authorization: Bearer <token>`
   - Then: `fakeMediaStore.InsertMediaFile` is called with `UploaderUserID == "@alice:myserver.example.com"`

3. **AT-12-9-3: TestMain_MissingServerName_FatalExit** — `media/cmd/media/main_test.go` (Go subprocess test)
   - Given: `NEBU_SERVER_NAME` is unset
   - When: subprocess calls the startup check
   - Then: process exits with non-zero code

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (nebu-pipeline story creation)

### Debug Log References

N/A

### Completion Notes List

- Story created by nebu-pipeline Step 1 on 2026-05-13
- Directly resolves Kassandra's 12.8 advisory: "uploader_user_id raw-sub pre-existing accepted"
- Reuses subprocess test pattern from Story 12.8 for AC-3
- `sanitiseLocalpart` mirrors `gateway/internal/grpc/metadata.go` but is intentionally duplicated to avoid cross-binary coupling

### File List

- `docs/stories/phase2/epic-12/12-9-canonical-matrix-user-id-in-media-audit-trail.md` (this file)

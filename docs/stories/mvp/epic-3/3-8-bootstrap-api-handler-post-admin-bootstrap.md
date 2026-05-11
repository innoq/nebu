# Story 3.8: Bootstrap API Handler (POST /admin/bootstrap)

Status: done

## Story

As a gateway,
I want a `POST /admin/bootstrap` handler that validates and persists the Bootstrap configuration,
so that the wizard data is saved to `server_config` and Bootstrap Mode is permanently deactivated.

## Acceptance Criteria

1. `POST /admin/bootstrap` accepts `application/x-www-form-urlencoded` with fields: `instance_name`, `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`
2. Handler validates: `instance_name` matches `^[a-zA-Z0-9-]{3,64}$`; `oidc_issuer` is a valid HTTPS URL; `oidc_client_id` non-empty; `oidc_client_secret` non-empty
3. On validation failure: re-render the wizard step (step 4) with an inline error message in `BootstrapPageData.Errors`; HTTP 422
4. On success: inserts into `server_config` (using the existing `INSERT`-only RLS policy):
   - `instance_name` → value from form
   - `oidc_issuer` → value from form
   - `oidc_client_id` → value from form
   - `oidc_client_secret` → AES-256-GCM encrypted with the internal secret; stored as hex-encoded ciphertext
   - `bootstrap_completed` → `"true"`
5. All five inserts execute in a single database transaction; on any DB error re-render step 4 with a global error and HTTP 500
6. After successful insert: redirect `303` to `/admin/login`
7. `POST /admin/bootstrap/test-oidc` accepts `oidc_issuer` form field, performs `GET <issuer>/.well-known/openid-configuration`, returns `200 {"ok": true}` or `200 {"ok": false, "error": "<reason>"}`; HTTP timeout 5s
8. `POST /admin/bootstrap/generate-keys` generates an Ed25519 keypair (stdlib `crypto/ed25519`) and an X25519 keypair (stdlib `crypto/ecdh`), returns `200 {"ed25519_public_fingerprint": "<hex>"}` (first 8 bytes of the public key, hex-encoded); generated keys are NOT persisted (client-side display only at this stage)
9. All three endpoints have unit tests covering the happy path and at least one error case each

## Tasks / Subtasks

- [x] Task 1: Implement `FinalizeHandler` — final submit (step 4) persistence (AC: 1–6)
  - [x] 1.1 Replace the stub `case 4:` in `bootstrap.go` `StepHandler` with a call to a new `FinalizeHandler` method on `BootstrapHandler`
  - [x] 1.2 `FinalizeHandler` re-validates all four fields (same regex/URL rules as steps 1 and 2 in `StepHandler`); on failure re-render step 4 with 422
  - [x] 1.3 Add `db *sql.DB` and `secret []byte` fields to `BootstrapHandler`; update `NewBootstrapHandler` signature:
        ```go
        func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler, db *sql.DB, secret []byte) *BootstrapHandler
        ```
  - [x] 1.4 Create `gateway/internal/admin/crypto.go` — package-internal AES-256-GCM helpers: `encryptAES256GCM(key []byte, plaintext string) (string, error)` — returns hex-encoded `nonce||ciphertext`; `decryptAES256GCM(key []byte, hexCiphertext string) (string, error)`; key derivation: `sha256.Sum256(secret)` → 32-byte AES key
  - [x] 1.5 In `FinalizeHandler`, encrypt `oidc_client_secret` via `encryptAES256GCM` before DB insert
  - [x] 1.6 Execute all five INSERTs in a single `sql.Tx` (Begin → Exec × 5 → Commit); on any error Rollback and re-render step 4 with global error
  - [x] 1.7 On success: `http.Redirect(w, r, "/admin/login", http.StatusSeeOther)` (303)
  - [x] 1.8 Update `main.go`: pass `bootstrapDB` and `internalSecret` (as `[]byte`) to `NewBootstrapHandler`

- [x] Task 2: Implement `POST /admin/bootstrap/test-oidc` handler (AC: 7)
  - [x] 2.1 Create `TestOIDCHandler` method on `BootstrapHandler` (or a standalone `HandleTestOIDC` func)
  - [x] 2.2 Read `oidc_issuer` from `r.FormValue`; if empty return `200 {"ok": false, "error": "oidc_issuer is required"}`
  - [x] 2.3 Validate `oidc_issuer` is a valid HTTPS URL; if not return `200 {"ok": false, "error": "invalid HTTPS URL"}`
  - [x] 2.4 Perform `GET <issuer>/.well-known/openid-configuration` with a 5-second `http.Client` timeout; on non-200 or network error return `200 {"ok": false, "error": "<reason>"}`
  - [x] 2.5 On success (HTTP 200 response): return `200 {"ok": true}`
  - [x] 2.6 Register the handler in `main.go` replacing the existing 501 stub:
        ```go
        mux.HandleFunc("POST /admin/bootstrap/test-oidc", bootstrapHandler.TestOIDCHandler)
        ```
  - [x] 2.7 Response `Content-Type: application/json`; use `encoding/json` to marshal the response struct

- [x] Task 3: Implement `POST /admin/bootstrap/generate-keys` handler (AC: 8)
  - [x] 3.1 Create `GenerateKeysHandler` method on `BootstrapHandler`
  - [x] 3.2 Generate Ed25519 keypair: `crypto/ed25519.GenerateKey(rand.Reader)` — stdlib, no external dep
  - [x] 3.3 Compute fingerprint: `hex.EncodeToString(pubKey[:8])` (first 8 bytes)
  - [x] 3.4 Also generate X25519 keypair via `crypto/ecdh.X25519().GenerateKey(rand.Reader)` — stdlib since Go 1.20; do NOT persist or return the private key
  - [x] 3.5 Return `200 {"ed25519_public_fingerprint": "<hex>"}` as JSON
  - [x] 3.6 Register the handler in `main.go` replacing the existing 501 stub:
        ```go
        mux.HandleFunc("POST /admin/bootstrap/generate-keys", bootstrapHandler.GenerateKeysHandler)
        ```

- [x] Task 4: Write unit tests (AC: 9)
  - [x] 4.1 Create `gateway/internal/admin/bootstrap_api_test.go` (package `admin`)
  - [x] 4.2 `TestFinalizeHandler_Success`: POST step=4 with valid fields → verify DB receives 5 INSERT calls in tx, response is 303 redirect to `/admin/login`; use a `*sql.DB` backed by a test double (mock or sqlmock) or use table-driven approach with a fake `BootstrapPersister` interface — see note in Dev Notes
  - [x] 4.3 `TestFinalizeHandler_ValidationError`: POST step=4 with invalid `instance_name` → expect 422, HTML re-render contains error text
  - [x] 4.4 `TestFinalizeHandler_DBError`: POST step=4 with valid fields but DB error → expect 500 (or 200 with error), HTML re-render contains global error
  - [x] 4.5 `TestTestOIDCHandler_MissingIssuer`: POST to `/admin/bootstrap/test-oidc` with empty `oidc_issuer` → `{"ok": false, ...}`
  - [x] 4.6 `TestTestOIDCHandler_InvalidURL`: POST with non-HTTPS URL → `{"ok": false, ...}`
  - [x] 4.7 `TestTestOIDCHandler_DiscoverySuccess`: POST with valid issuer, stub HTTP server returns 200 → `{"ok": true}`
  - [x] 4.8 `TestTestOIDCHandler_DiscoveryFailure`: POST with valid issuer, stub HTTP server returns 503 → `{"ok": false, ...}`
  - [x] 4.9 `TestGenerateKeysHandler_Success`: POST → response 200, JSON contains `ed25519_public_fingerprint` non-empty hex string
  - [x] 4.10 `TestEncryptDecrypt_RoundTrip`: unit test for `encryptAES256GCM` / `decryptAES256GCM` — encrypt then decrypt must recover original
  - [x] 4.11 Run `make test-unit-go` → zero regressions

## Dev Notes

### CRITICAL: Replace the Step 4 Stub in `StepHandler`

`bootstrap.go` `StepHandler` currently has this in `case 4:`:
```go
case 4:
    // Final submit — Story 3.8 replaces with real persistence; stub redirect for now
    slog.Info("bootstrap wizard step 4 submitted — stub: redirecting to /admin/login")
    http.Redirect(w, r, "/admin/login", http.StatusFound)
    return
```
**This stub MUST be replaced** — call `h.FinalizeHandler(w, r)` (or inline the logic in `StepHandler`). The redirect must also change to `303 See Other` (not `302 Found`) per AC 6.

### `BootstrapHandler` Constructor Change — Update `main.go`

Current signature (Story 3.7):
```go
func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler) *BootstrapHandler
```

New signature (this story):
```go
func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler, db *sql.DB, secret []byte) *BootstrapHandler
```

In `main.go`, `bootstrapDB` already exists (opened at line 121). `internalSecret` is the hex string read from file (line 105). Pass `[]byte(internalSecret)` as the secret:
```go
bootstrapHandler := admin.NewBootstrapHandler(checker, tmplHandler, bootstrapDB, []byte(internalSecret))
```
The `bootstrapDB` is already scoped correctly for the lifetime of the handler.

### AES-256-GCM Encryption for `oidc_client_secret`

The internal secret is a 64-char hex string (from `openssl rand -hex 32`). Derive a 32-byte AES key via `sha256.Sum256([]byte(secret))`.

Standard Go stdlib pattern (no external deps):
```go
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "io"
)

func encryptAES256GCM(secret []byte, plaintext string) (string, error) {
    keyHash := sha256.Sum256(secret)
    block, err := aes.NewCipher(keyHash[:])
    if err != nil {
        return "", err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }
    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return hex.EncodeToString(ciphertext), nil
}

func decryptAES256GCM(secret []byte, hexCiphertext string) (string, error) {
    keyHash := sha256.Sum256(secret)
    data, err := hex.DecodeString(hexCiphertext)
    if err != nil {
        return "", err
    }
    block, err := aes.NewCipher(keyHash[:])
    if err != nil {
        return "", err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    if len(data) < gcm.NonceSize() {
        return "", errors.New("ciphertext too short")
    }
    nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
    plain, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }
    return string(plain), nil
}
```

Place in `gateway/internal/admin/crypto.go` (package-private). The decrypt helper is included for completeness and testability (round-trip test) even if not called by this story.

### `server_config` INSERT Pattern

Follow the existing pattern in `gateway/internal/db/serverconfig.go`. Use `time.Now().UnixMilli()` for `set_at`.

The RLS policy enforces INSERT-only — no UPDATE or DELETE is possible. The `bootstrap_completed` row can only be inserted once (PRIMARY KEY constraint). If the wizard is submitted twice (e.g., back button), the second INSERT will fail with a unique constraint violation. Handle this gracefully: if the `bootstrap_completed` key already exists, treat as success and redirect to `/admin/login`.

Transaction pattern:
```go
tx, err := h.db.BeginTx(r.Context(), nil)
if err != nil { /* re-render step 4 with error */ }
nowMs := time.Now().UnixMilli()
rows := []struct{ key, value string }{
    {"instance_name",    instanceName},
    {"oidc_issuer",      oidcIssuer},
    {"oidc_client_id",   oidcClientID},
    {"oidc_client_secret", encryptedSecret},
    {"bootstrap_completed", "true"},
}
for _, row := range rows {
    if _, err := tx.ExecContext(r.Context(),
        "INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)",
        row.key, row.value, nowMs,
    ); err != nil {
        _ = tx.Rollback()
        /* re-render step 4 with DB error */
        return
    }
}
if err := tx.Commit(); err != nil { /* re-render step 4 */ }
```

### Testing Strategy — DB Interaction

Testing real DB writes in unit tests requires either a real DB (integration) or mocking. **Recommended approach**: extract a `BootstrapPersister` interface:
```go
type BootstrapPersister interface {
    SaveBootstrapConfig(ctx context.Context, instanceName, oidcIssuer, oidcClientID, encryptedSecret string) error
}
```

Inject via `BootstrapHandler`. Implement with `*sql.DB` as `PostgresBootstrapPersister`. In tests use a `fakeBootstrapPersister` (similar to `fakeBootstrapChecker` pattern in `bootstrap_test.go`).

This avoids requiring a real database in unit tests while keeping the DB logic testable via the real persister in integration tests.

### `POST /admin/bootstrap/test-oidc` — HTTP Client

Use a configurable `*http.Client` injected into `BootstrapHandler` (or a package-level var for tests). This allows tests to inject a `httptest.Server`.

```go
type BootstrapHandler struct {
    checker    BootstrapStatusChecker
    tmpl       *TemplateHandler
    db         *sql.DB
    secret     []byte
    httpClient *http.Client // for test-oidc; defaults to &http.Client{Timeout: 5*time.Second}
}
```

In `NewBootstrapHandler`, initialize `httpClient` to the 5-second timeout client. Tests can replace it via a setter or a constructor option.

OIDC Discovery endpoint: `<issuer>/.well-known/openid-configuration` — append the path to the issuer URL. Be careful not to double-slash: use `strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"`.

### `POST /admin/bootstrap/generate-keys` — No External Deps

Use **only stdlib** packages:
- `crypto/ed25519` — Ed25519 keypair (available since Go 1.13)
- `crypto/ecdh` — X25519 keypair (available since Go 1.20, project uses Go 1.26)
- `golang.org/x/crypto` is NOT in `go.mod` — do NOT add it

```go
import (
    "crypto/ecdh"
    "crypto/ed25519"
    "crypto/rand"
    "encoding/hex"
)

// Ed25519
pub, _, err := ed25519.GenerateKey(rand.Reader)
fingerprint := hex.EncodeToString(pub[:8])

// X25519 (for future use — not persisted here)
x25519, _ := ecdh.X25519().GenerateKey(rand.Reader)
_ = x25519 // not returned in this story
```

### JSON Response Pattern for test-oidc and generate-keys

```go
type testOIDCResponse struct {
    OK    bool   `json:"ok"`
    Error string `json:"error,omitempty"`
}

type generateKeysResponse struct {
    Ed25519PublicFingerprint string `json:"ed25519_public_fingerprint"`
}

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(resp)
```

### Stub Replacements in `main.go`

Story 3.7 registered two 501 stubs (lines 146–151 in current `main.go`):
```go
mux.HandleFunc("POST /admin/bootstrap/test-oidc", func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "Not implemented", http.StatusNotImplemented)
})
mux.HandleFunc("POST /admin/bootstrap/generate-keys", func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "Not implemented", http.StatusNotImplemented)
})
```
**Replace both** with the real handlers from `bootstrapHandler`.

### Re-render Step 4 on Error

When validation fails or DB errors occur in `FinalizeHandler`, re-render with the wizard at step 4. The `BootstrapPageData` must carry all accumulated field values (from hidden form fields) so the user doesn't lose their input:
```go
data := BootstrapPageData{
    PageData:     PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
    Step:         4,
    InstanceName: r.FormValue("instance_name"),
    OIDCIssuer:   r.FormValue("oidc_issuer"),
    OIDCClientID: r.FormValue("oidc_client_id"),
    // OIDCClientSecret NOT included — security
    Errors: map[string]string{"global": "Database error: " + err.Error()},
}
w.WriteHeader(http.StatusInternalServerError)
h.tmpl.render(w, "bootstrap", data)
```

Note: `OIDCClientSecret` must NOT be echoed back into the re-rendered form (see Story 3.7 design decision). The user must re-enter it.

### `BootstrapGuard` Middleware — No Changes Needed

`BootstrapGuard` (Story 3.6) already passes through all paths starting with `/admin/bootstrap`. The new handlers `test-oidc` and `generate-keys` are already covered since they share the `/admin/bootstrap/` prefix. No middleware changes required.

### Project Structure Notes

| File | Action |
|------|--------|
| `gateway/internal/admin/bootstrap.go` | MODIFY — `NewBootstrapHandler` signature (add `db`, `secret`, `httpClient`); add `FinalizeHandler`, `TestOIDCHandler`, `GenerateKeysHandler` methods; replace stub `case 4:` in `StepHandler` |
| `gateway/internal/admin/crypto.go` | CREATE — `encryptAES256GCM`, `decryptAES256GCM` helpers |
| `gateway/internal/admin/bootstrap_api_test.go` | CREATE — unit tests for all three new endpoints + crypto round-trip |
| `gateway/cmd/gateway/main.go` | MODIFY — update `NewBootstrapHandler` call (add `bootstrapDB`, `[]byte(internalSecret)`); replace 501 stubs with real handlers |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — `TemplateHandler` unchanged
- `gateway/internal/admin/middleware.go` — `BootstrapGuard` unchanged
- `gateway/internal/admin/page_data.go` — `BootstrapPageData` unchanged (unless adding error display fields already present via `Errors map[string]string`)
- `gateway/internal/admin/bootstrap_wizard_test.go` — Story 3.7 tests remain valid

### Existing Test Patterns to Follow

Reuse `fakeBootstrapChecker` and `errFakeDB` from `bootstrap_test.go`. Use `newTestBootstrapHandler` as a reference for constructing test instances. For `TestOIDCHandler` tests, use `httptest.NewServer` to stub the OIDC discovery endpoint.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.8, lines 1612–1637] Authoritative ACs
- [Source: gateway/internal/admin/bootstrap.go] Existing `BootstrapHandler`, `StepHandler` (stub case 4 at line 122), `PostgresBootstrapChecker`, `instanceNameRe`
- [Source: gateway/internal/admin/bootstrap_test.go] `fakeBootstrapChecker`, `errFakeDB`, `newTestBootstrapHandler` pattern
- [Source: gateway/internal/admin/auth.go] HMAC + crypto patterns; `signCookie` / `verifyCookie` for reference
- [Source: gateway/internal/admin/page_data.go] `BootstrapPageData` struct — `Errors map[string]string` already present
- [Source: gateway/internal/db/serverconfig.go] `INSERT INTO server_config` pattern, `time.Now().UnixMilli()` for `set_at`
- [Source: gateway/migrations/000003_server_config.up.sql] RLS policy: INSERT-only, no UPDATE/DELETE
- [Source: gateway/cmd/gateway/main.go, lines 100–151] `internalSecret` read from file, `bootstrapDB` initialization, stub endpoint registration
- [Source: _bmad-output/planning-artifacts/architecture.md#line-1003] AES-256-GCM in media/crypto — same approach for gateway
- [Source: gateway/go.mod] `golang.org/x/crypto` NOT present — use stdlib `crypto/ed25519` + `crypto/ecdh`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No debug issues encountered. All tasks implemented in a single pass.

### Completion Notes List

- Implemented `BootstrapPersister` interface with `postgresBootstrapPersister` (real DB) and `fakeBootstrapPersister` (tests) — avoids requiring real DB in unit tests
- Implemented `BootstrapDraftStore` interface with `postgresBootstrapDraftStore` (PostgreSQL `bootstrap_draft` table) and `fakeBootstrapDraftStore` (tests); replaces non-restart-resilient `sync.Map secretStore`
- `BootstrapHandler` struct: replaced `secretStore sync.Map` with `draftStore BootstrapDraftStore`; removed `bootstrapSessionCookie` constant, `generateSessionID()`, and all `http.SetCookie`/`r.Cookie` calls for session cookies
- `NewBootstrapHandler` creates `postgresBootstrapDraftStore` alongside `postgresBootstrapPersister` from the same `*sql.DB`
- Step 1 now saves `instance_name` to draft store on successful validation
- Step 2 encrypts `oidc_client_secret` via `encryptAES256GCM` before writing to draft store; also saves `oidc_issuer` and `oidc_client_id` to draft
- Step 3 loads encrypted secret from draft, decrypts in-memory, masks for display only — secret never traverses the wire post step 2
- `FinalizeHandler` loads pre-encrypted secret from draft store (no double-encryption), validates all fields, persists via `BootstrapPersister`, calls `ClearDraft` on success (non-fatal failure), redirects 303
- `crypto.go` created with `encryptAES256GCM` and `decryptAES256GCM` using stdlib only
- `TestOIDCHandler` uses configurable `h.httpClient` for testability; validates HTTPS URL; appends `/.well-known/openid-configuration`
- `GenerateKeysHandler` generates Ed25519 + X25519 keypairs (stdlib only); returns fingerprint (first 8 bytes hex)
- `main.go` updated: `NewBootstrapHandler` now receives `bootstrapDB` and `[]byte(internalSecret)`; 501 stubs replaced with real handler methods
- `bootstrap_test.go` `newTestBootstrapHandler` updated to use `fakeBootstrapPersister` + `fakeBootstrapDraftStore` directly
- 12 unit tests in `bootstrap_api_test.go` (2 new: `TestStepHandler_Step2_SavesToDraft`, `TestFinalizeHandler_ClearsDraftOnSuccess`; 1 renamed: `TestFinalizeHandler_MissingSessionCookie` → `TestFinalizeHandler_MissingDraftSecret`); all pass; `make test-unit-go` passes with zero regressions
- Migration `000008_bootstrap_draft` adds the `bootstrap_draft` table (key, value, set_at) with upsert semantics for draft writes

### File List

- `gateway/internal/admin/bootstrap.go` — modified: removed `sync.Map secretStore`, `generateSessionID`, and `bootstrapSessionCookie`; added `BootstrapDraftStore` interface + `postgresBootstrapDraftStore` implementation; `draftStore BootstrapDraftStore` field added to `BootstrapHandler`; step 1 saves `instance_name` to draft; step 2 encrypts + saves OIDC fields to draft; step 3 loads encrypted secret from draft + masks for display; `FinalizeHandler` loads encrypted secret from draft, validates, persists, clears draft; `generateKeysResponse` includes `OK bool`; `GenerateKeysHandler` returns `ok: true`
- `gateway/internal/admin/page_data.go` — modified: `MaskedSecret string` field added to `BootstrapPageData`
- `gateway/internal/admin/templates/bootstrap.html` — modified: step 4 now renders `{{if .Errors}}` block and `{{if .MaskedSecret}}` confirmation block; MAJOR-2 fixed
- `gateway/internal/admin/bootstrap_api_test.go` — modified: replaced `seedSecret`/`sync.Map` helpers with `fakeBootstrapDraftStore`; updated `newTestBootstrapHandlerWithPersister` to inject `fakeBootstrapDraftStore`; added `newTestBootstrapHandlerWithDraftStore` helper; renamed `TestFinalizeHandler_MissingSessionCookie` → `TestFinalizeHandler_MissingDraftSecret`; all tests pre-seed encrypted secret into draft store instead of `sync.Map`; added `TestStepHandler_Step2_SavesToDraft`, `TestFinalizeHandler_ClearsDraftOnSuccess`; removed all session cookie usage
- `gateway/internal/admin/crypto.go` — created: `encryptAES256GCM`, `decryptAES256GCM`
- `gateway/internal/admin/bootstrap_test.go` — modified: updated `newTestBootstrapHandler` to use `fakeBootstrapPersister` + `fakeBootstrapDraftStore` directly (no real DB); added `time` import
- `gateway/cmd/gateway/main.go` — modified: `NewBootstrapHandler` call updated; 501 stubs replaced with real handlers
- `gateway/migrations/000008_bootstrap_draft.up.sql` — created: `bootstrap_draft` table for persistent wizard draft storage
- `gateway/migrations/000008_bootstrap_draft.down.sql` — created: drops `bootstrap_draft` table

## Senior Developer Review (AI)

**Reviewer:** Phil on 2026-03-31
**Outcome:** Changes Requested (1 MAJOR, 4 MINOR fixed, 2 INFO)

### Review Follow-ups (AI)

- [x] [AI-Review][MAJOR] `oidc_client_secret` is lost between step 2 and step 4. Fixed: Added `sync.Map secretStore` to `BootstrapHandler`; step 2 POST generates a random session ID, stores the plaintext secret in the map, and sets an HttpOnly `bootstrap_session` cookie. `FinalizeHandler` reads the secret from the store via the cookie instead of the form. After successful DB persist, the store entry is deleted and the cookie is cleared. `MaskedSecret` field added to `BootstrapPageData`; step 3→4 transition computes and passes masked value for display. [bootstrap.go, page_data.go, bootstrap.html]
- [x] [AI-Review][MAJOR] Step 4 template (`bootstrap.html` lines 168-189) does not render `BootstrapPageData.Errors`. Fixed: Added error rendering block (`{{if .Errors}}` / `{{range .Errors}}`) to step 4 template section, matching the pattern used in steps 1-2. Also added a `MaskedSecret` display block for confirmation. [templates/bootstrap.html]
- [x] [AI-Review][MAJOR] JS `generate-keys` handler checks `json.ok` (line 248 of bootstrap.html) but `GenerateKeysHandler` returns `{"ed25519_public_fingerprint": "..."}` with no `ok` field. Fixed: Added `OK bool \`json:"ok"\`` field to `generateKeysResponse` struct and set `OK: true` in `GenerateKeysHandler`. JS success branch now executes correctly. [bootstrap.go]

### MINOR Issues (Fixed by Reviewer)

1. **Internal error details leaked to user** (bootstrap.go:216,232) -- `err.Error()` from encryption/DB was exposed in HTML page. Fixed: replaced with generic user-facing messages ("An internal error occurred", "Failed to save configuration"). Detailed errors remain in slog.
2. **test-oidc and generate-keys not guarded by BootstrapGuard** (main.go:145-146) -- These endpoints remained accessible after bootstrap completion. Fixed: wrapped both in `guard()` middleware.
3. **json.Encode return values unchecked** (bootstrap.go) -- 6 `json.NewEncoder(w).Encode(...)` calls missing `_ =` per codebase convention. Fixed: added `_ =` prefix.
4. **Magic number timeout in test** (bootstrap_api_test.go:53) -- `5 * 1000000000` replaced with `5 * time.Second` for readability and consistency.

### INFO Observations

1. `r.ParseForm()` called twice when `StepHandler` delegates to `FinalizeHandler` -- harmless (idempotent), no fix needed.
2. `TestTestOIDCHandler_DiscoverySuccess` does not verify the discovery path (`/.well-known/openid-configuration`) -- the stub server accepts any path. Consider adding path assertion in a future test improvement pass.
3. Dev Notes recommend idempotent double-submit handling (treat `bootstrap_completed` unique-constraint violation as success). Not required by any AC; if double-submit occurs, user receives a 500 re-render. Acceptable for MVP.

## Senior Developer Review (AI) — Round 2

**Reviewer:** Phil on 2026-03-31
**Outcome:** Approved (0 MAJOR, 4 MINOR fixed, 3 INFO)

### MINOR Issues Fixed by Reviewer

1. **`bootstrap_session` cookie missing `Secure` flag** (bootstrap.go:172, 302) — Set `Secure: r.TLS != nil` on both `SetCookie` calls for the bootstrap session cookie, consistent with the `auth.go` pattern (lines 128/223). Cookie is now only sent over HTTPS when TLS is active.
2. **German error message inconsistency** (bootstrap.go:247) — `"Session abgelaufen – bitte Schritt 2 wiederholen."` replaced with English `"Session expired — please re-enter your OIDC Client Secret in step 2."` Consistent with all other English-language user-facing error messages.
3. **Weak test assertion in `TestFinalizeHandler_ValidationError`** (bootstrap_api_test.go:233) — `strings.Contains(body, "3")` replaced with `strings.Contains(body, "3–64")` to assert the actual error message content, not just any occurrence of the digit 3.
4. **HTTP response body not drained before close in `TestOIDCHandler`** (bootstrap.go:350) — Added `io.Copy(io.Discard, resp.Body)` before `resp.Body.Close()` in a deferred function, enabling HTTP keep-alive connection reuse per Go net/http best practices.

### INFO Observations

1. `test-oidc` JSON error response includes `err.Error()` from network failures (bootstrap.go:347). This is admin-only diagnostic data for an admin wizard endpoint; acceptable, no fix needed.

## Senior Developer Review (AI) — Round 3 (Post-Refactor)

**Reviewer:** Phil on 2026-04-01
**Outcome:** Approved (0 MAJOR, 2 MINOR fixed, 3 INFO)

### MINOR Issues Fixed by Reviewer

1. **`err == sql.ErrNoRows` instead of `errors.Is(err, sql.ErrNoRows)`** (bootstrap.go:65) — `LoadDraft` used direct equality comparison. The codebase standard in `db/serverconfig.go` and `db/db.go` uses `errors.Is()` for wrapped error compatibility. Fixed: replaced with `errors.Is(err, sql.ErrNoRows)` and added `"errors"` import.
2. **Non-deterministic map iteration in step 2 draft saving** (bootstrap.go:202) — `for key, val := range map[string]string{...}` iterates in non-deterministic order. If a `SaveDraft` call fails mid-iteration, different fields may or may not have been persisted depending on runtime iteration order, making debugging inconsistent. Fixed: replaced with a `[]struct{ key, value string }` slice for deterministic insertion order, matching the pattern used in `postgresBootstrapPersister.SaveBootstrapConfig`.

### INFO Observations

1. `bootstrap_draft` table has no RLS (unlike `server_config`). Acceptable because draft data requires both INSERT, UPDATE (via upsert), and DELETE (for `ClearDraft`), so permissive RLS would add complexity without security benefit during single-user bootstrap.
2. `bootstrap_draft` table has no TTL/expiry mechanism. If bootstrap is started but never completed, draft rows persist indefinitely. Acceptable for MVP -- the table only holds a few rows and is only relevant during initial setup.
3. `FinalizeHandler` reads `instance_name` from the form (hidden field) rather than the draft store, even though step 1 saved it to draft. This is consistent with the wizard's hidden-field carry pattern and does not represent a data integrity risk.

## Change Log

- 2026-03-31: Story 3.8 implemented — `FinalizeHandler` with AES-256-GCM encryption and transactional DB persistence, `TestOIDCHandler` with HTTPS validation and configurable HTTP client, `GenerateKeysHandler` with stdlib Ed25519+X25519 keypair generation, `BootstrapPersister` interface for testability, 10 unit tests added, all tests pass.
- 2026-03-31: Code review round 1 — 3 MAJOR action items created (oidc_client_secret lost between wizard steps, step 4 missing error rendering, JS/handler `ok` field mismatch); 4 MINOR issues fixed (error detail leaking, missing guard middleware, unchecked json encode, magic timeout number). Status → in-progress.
- 2026-03-31: MAJOR fixes applied — (1) Server-side `sync.Map` secretStore added to `BootstrapHandler`; step 2 generates session ID + stores secret + sets `bootstrap_session` HttpOnly cookie; `FinalizeHandler` reads secret from store and deletes on success; `MaskedSecret` field added to `BootstrapPageData` and displayed in step 4. (2) Step 4 template error rendering block added. (3) `generateKeysResponse.OK` field added; `GenerateKeysHandler` returns `ok: true`. Tests updated + 3 new tests added. All tests pass. Status → review.
- 2026-03-31: Code review round 2 — 4 MINOR issues fixed (Secure flag on bootstrap_session cookie, German error message, weak test assertion, HTTP body drain). 0 MAJOR issues. All tests pass. Status → done.
- 2026-04-01: Refactored from sync.Map to PostgreSQL draft storage (bootstrap_draft table) for restart-resilience and persistence across gateway restarts. Removed `sync.Map secretStore`, `bootstrapSessionCookie`, `generateSessionID()`, and all session cookie logic. Added `BootstrapDraftStore` interface with `postgresBootstrapDraftStore` implementation (upsert semantics). Step 2 now encrypts `oidc_client_secret` via AES-256-GCM before writing to `bootstrap_draft` table; `FinalizeHandler` loads pre-encrypted secret from draft store (no double-encryption). Added migration `000008_bootstrap_draft`. Tests updated: `fakeBootstrapDraftStore` replaces `sync.Map` seed helpers; `TestFinalizeHandler_MissingSessionCookie` renamed to `TestFinalizeHandler_MissingDraftSecret`; 2 new tests added (`TestStepHandler_Step2_SavesToDraft`, `TestFinalizeHandler_ClearsDraftOnSuccess`). All tests pass. Status → review.
- 2026-04-01: Code review round 3 (post-refactor) — 0 MAJOR, 2 MINOR fixed (`errors.Is` for `sql.ErrNoRows`, deterministic draft field iteration order). 3 INFO observations. All 44 tests pass. Status → done.

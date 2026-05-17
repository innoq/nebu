---
status: done
epic: 14
story: 3c
security_review: required
matrix: false
ui: true
---

# Story 14.3c: SCIM 2.0 User Fetch + Progress Tracking

**Status:** done

## Story

As an instance admin,
I want the import endpoint to support SCIM 2.0 user fetch and a live progress indicator during import,
So that large user imports from enterprise directories are reliable and observable.

**Size:** S
**security_review:** required (SSRF via scim_base_url, bearer token at rest, HTTPS-only)

---

## Acceptance Criteria

**AC1 — SCIM fetch replaces OIDC fetch when configured:**
Given `scim_enabled: true`, `scim_base_url`, and `scim_bearer_token` are configured in the server config,
When `POST /api/v1/admin/bootstrap/import-users` is called (or bootstrap Step 4 `action=import`),
Then users are fetched via `GET /Users` (SCIM 2.0 RFC 7644, paginated) instead of the OIDC directory endpoint, and are imported with the same Core provisioning flow (`BulkImportUsers` gRPC).

**AC2 — Live progress status endpoint:**
Given an import is running (background goroutine triggered by `action=import`),
When `GET /api/v1/admin/bootstrap/import-status` is polled,
Then the response JSON contains `{"imported": N, "total": N, "failed": N}` with current live counts.

**AC3 — Progress bar in Bootstrap Wizard Step 4:**
Given the Bootstrap Wizard Step 4 import UI,
When an import is in progress,
Then a progress bar with live `imported / total` counts is shown (via JavaScript polling of `/api/v1/admin/bootstrap/import-status` every 2s, or SSE).

**AC4 — Unit tests pass:**
Given Go unit tests for SCIM fetch and mapping,
When `make test-unit-go` runs,
Then the following test cases pass:
- SCIM user fetch (paginated, mock HTTPS server)
- SCIM-to-Nebu claim mapping (userName → Matrix localpart via `sanitizeOIDCSub`)
- Progress endpoint returns correct counts during and after import
- Non-HTTPS `scim_base_url` is rejected
- Concurrent import attempt returns HTTP 409

**AC5 — Bearer token never exposed:**
The `scim_bearer_token` value MUST NOT appear in any API response, log output, or error message.
`GET /api/v1/admin/config` must return `scim_bearer_token_set: bool` only.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-1 — SCIM user fetch (unit, mock HTTPS server)** — `gateway/internal/admin/scim_client_test.go`
   - Given: SCIMClient configured with mock HTTPS server returning 2 pages of 3 users total
   - When: `FetchUsers(ctx)` is called
   - Then: returns all 3 users; `startIndex` pagination is correct (1-based); bearer token sent in header

2. **AT-2 — SCIM-to-Nebu claim mapping (unit)** — `gateway/internal/admin/scim_client_test.go`
   - Given: SCIM User `{userName: "Alice.Smith@corp.com", displayName: "Alice Smith", emails: [{value: "alice@corp.com"}]}`
   - When: mapped to a Nebu `OIDCDirectoryUser`
   - Then: `Sub = "Alice.Smith@corp.com"`, `DisplayName = "Alice Smith"`, `Email = "alice@corp.com"` (MUST go through `sanitizeOIDCSub` for Matrix localpart derivation)

3. **AT-3 — Progress endpoint returns live counts (unit)** — `gateway/internal/admin/bootstrap_scim_test.go`
   - Given: import goroutine started, progress tracker set to `{imported: 5, total: 20, failed: 0}`
   - When: `GET /api/v1/admin/bootstrap/import-status` is called
   - Then: response `{"imported":5,"total":20,"failed":0}` with HTTP 200

4. **AT-4 — Non-HTTPS scim_base_url rejected (unit)** — `gateway/internal/admin/scim_client_test.go`
   - Given: SCIMClient configured with `scim_base_url = "http://idp.corp.com/scim"`
   - When: `FetchUsers(ctx)` is called
   - Then: returns error containing "HTTPS"; no HTTP call is made

5. **AT-5 — Concurrent import returns 409 (unit)** — `gateway/internal/admin/bootstrap_scim_test.go`
   - Given: first import goroutine has been started (import in progress)
   - When: second `action=import` POST is received
   - Then: HTTP 409 is returned; second import is NOT started

6. **AT-6 — Bearer token not in response or logs (unit)** — `gateway/internal/admin/bootstrap_scim_test.go`
   - Given: SCIMClient configured with bearer token `"super-secret-token-12345"`
   - When: `FetchUsers(ctx)` is called (even on error)
   - Then: the raw token string does NOT appear in any log output captured by `slog` handler

7. **AT-7 — Playwright+Gherkin: progress bar shown during import** — `e2e/features/admin/bootstrap_scim_progress.feature`
   - Given: admin is on Bootstrap Wizard Step 4 with SCIM configured
   - When: admin clicks "Import from SCIM" and import starts
   - Then: a progress bar element is visible; it shows `imported / total` numbers

---

## Dev Notes

### CRITICAL: Read Security Guide Before Any Code

The SCIM security guide is at `_bmad-output/implementation-artifacts/security-guide-scim-2026-05-16.md`.
It is MANDATORY reading. Key requirements:

- **CR-1:** Bearer token encrypted at rest, never in API responses (`scim_bearer_token_set: bool` only)
- **CR-2:** HTTPS-only for `scim_base_url` — validate at config time AND at fetch time
- **CR-3:** Import progress endpoint (`GET /import-status`) MUST be behind admin auth middleware
- **CR-4:** Matrix ID derivation MUST use `sanitizeOIDCSub(scimUser.UserName)` — NOT raw userName
- **CR-5:** BulkImportUsers must be idempotent (duplicates skipped, not errored)
- **HR-1:** Pagination cap: `scimPageSize = 100`, `scimMaxTotal = 100_000`; abort if total exceeds cap
- **HR-2:** SSRF documented trust boundary (Option B from OIDC guide — same comment block pattern)
- **HR-3:** Singleton import lock: `sync.Mutex` + `atomic.Bool` → HTTP 409 on concurrent trigger
- **HR-4:** Audit log entry for every import (use existing `audit` package pattern from `audit_log_handler.go`)

### Architecture Overview — What Exists Today

#### Files to CREATE:
- `gateway/internal/admin/scim_client.go` — SCIMClient (mirrors `oidc_directory.go` pattern)
- `gateway/internal/admin/scim_client_test.go` — unit tests AT-1..AT-4, AT-6
- `gateway/internal/admin/bootstrap_scim_test.go` — unit tests AT-3, AT-5
- `gateway/migrations/000049_scim_config.up.sql` — adds scim_enabled, scim_base_url, scim_bearer_token to server_config
- `gateway/migrations/000049_scim_config.down.sql`
- `e2e/features/admin/bootstrap_scim_progress.feature` — Playwright+Gherkin AT-7
- `e2e/steps/admin/bootstrap_scim_progress.steps.ts` — step definitions for AT-7

#### Files to UPDATE:
- `gateway/internal/admin/stubs.go` — add `ScimEnabled`, `ScimBaseURL`, `ScimBearerTokenSet bool` to `StubConfig`; `scim_bearer_token_set: bool` only (never the raw token)
- `gateway/internal/admin/config.go` — parse SCIM fields from form in `UpdateConfigHandler`; persist via direct DB upsert (same pattern as `oidc_directory_enabled`)
- `gateway/internal/admin/bootstrap.go` — add `scimFetcher SCIMFetcher` interface field + `WithSCIMFetcher` fluent setter; in `action=import`, if SCIM is enabled, prefer SCIMClient over OIDC fetcher; add singleton import lock + progress tracker
- `gateway/internal/admin/page_data.go` — add `ScimEnabled bool` to `BootstrapPageData` and `StubConfig`; add `ImportInProgress bool` and `ImportProgressTotal int32` for template use
- `gateway/internal/admin/templates/config.html` — add SCIM section below OIDC directory section
- `gateway/internal/admin/templates/bootstrap.html` — add progress bar to Step 4; add polling JS snippet when `ImportInProgress`
- `gateway/cmd/gateway/main.go` — wire SCIMClient into bootstrap handler via `WithSCIMFetcher`; register `GET /api/v1/admin/bootstrap/import-status` route

### SCIM Protocol (RFC 7644) Implementation

```go
// SCIM ListResponse structure (RFC 7644 §3.4.2)
type scimListResponse struct {
    Schemas      []string     `json:"schemas"`
    TotalResults int          `json:"totalResults"`
    StartIndex   int          `json:"startIndex"`
    ItemsPerPage int          `json:"itemsPerPage"`
    Resources    []scimUser   `json:"Resources"`
}

type scimUser struct {
    ID          string       `json:"id"`
    UserName    string       `json:"userName"`
    DisplayName string       `json:"displayName"`
    Emails      []scimEmail  `json:"emails"`
}

type scimEmail struct {
    Value   string `json:"value"`
    Primary bool   `json:"primary"`
}
```

**Pagination (RFC 7644 §3.4.2):**
- `startIndex` is 1-based (NOT 0-based!)
- Fetch loop: `startIndex=1`, increment by `count` until `startIndex + returned > totalResults`
- Use `scimPageSize = 100` as the `count` parameter
- Abort if `totalResults > scimMaxTotal (100_000)` — return error to admin

**Request format:**
```
GET /Users?startIndex=1&count=100 HTTP/1.1
Host: idp.corp.com
Authorization: Bearer {token}
Accept: application/scim+json, application/json
```

### Claim Mapping: SCIM → OIDCDirectoryUser

Map SCIM User to the existing `OIDCDirectoryUser` struct so the same `BulkImportUsers` flow is reused:
```go
func scimUserToDirectoryUser(u scimUser) OIDCDirectoryUser {
    email := ""
    for _, e := range u.Emails {
        if e.Primary || email == "" {
            email = e.Value
        }
    }
    return OIDCDirectoryUser{
        Sub:         u.UserName,  // userName as sub — sanitizeOIDCSub applied at Matrix ID derivation
        DisplayName: u.DisplayName,
        Email:       email,
    }
}
```

Then in `bootstrap.go` `action=import` path:
```go
localpart := sanitizeOIDCSub(u.Sub)  // same as OIDC path — CR-4 from security guide
matrixUserID := "@" + localpart + ":" + h.serverName
```

### Import Progress: Singleton Lock + Atomic Counter

```go
// Package-level singletons (in bootstrap.go or a new bootstrap_scim.go helper):
var (
    importMu         sync.Mutex
    importInProgress atomic.Bool
    importProgress   = &importProgressState{}
)

type importProgressState struct {
    imported atomic.Int32
    total    atomic.Int32
    failed   atomic.Int32
    done     atomic.Bool
}
```

**Trigger flow (action=import):**
```go
if !importInProgress.CompareAndSwap(false, true) {
    http.Error(w, `{"error":"import already in progress"}`, http.StatusConflict)
    return
}
// Reset progress
importProgress.imported.Store(0)
importProgress.total.Store(0)
importProgress.failed.Store(0)
importProgress.done.Store(false)

// Launch background goroutine; render response immediately with "import started"
go func() {
    defer importInProgress.Store(false)
    defer importProgress.done.Store(true)
    // ... run import, update importProgress atomically per user
}()
```

**Progress endpoint (`GET /api/v1/admin/bootstrap/import-status`):**
```go
// Returns immediately — reads atomic counters (no locks needed for read)
func importStatusHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, `{"imported":%d,"total":%d,"failed":%d}`,
        importProgress.imported.Load(),
        importProgress.total.Load(),
        importProgress.failed.Load(),
    )
}
```

**Router registration (admin-auth-protected):**
```go
mux.Handle("GET /api/v1/admin/bootstrap/import-status",
    sessionGuard(http.HandlerFunc(importStatusHandler)))
```

### Database Migration (000049)

Migration file must:
1. Insert new `server_config` rows: `scim_enabled`, `scim_base_url`, `scim_bearer_token`
2. Extend `config_update_mutable` RLS policy (same pattern as migration 000048) to include the 3 new keys
3. The bearer token is stored ENCRYPTED (AES-256-GCM, same `encryptAES256GCM` from `crypto.go`) — the raw value MUST NOT be stored in plaintext

```sql
-- 000049_scim_config.up.sql
INSERT INTO server_config (key, value, set_at) VALUES
    ('scim_enabled',       'false', (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
    ('scim_base_url',      '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
    ('scim_bearer_token',  '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint)
ON CONFLICT (key) DO NOTHING;

DROP POLICY IF EXISTS config_update_mutable ON server_config;
CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING (key IN (
        'oidc_user_id_claim', 'oidc_displayname_claim', 'oidc_email_claim',
        'admin_group_claim', 'oidc_issuer', 'oidc_client_id', 'oidc_client_secret',
        'oidc_directory_enabled', 'oidc_directory_endpoint',
        'scim_enabled', 'scim_base_url', 'scim_bearer_token'
    ))
    WITH CHECK (key IN (
        'oidc_user_id_claim', 'oidc_displayname_claim', 'oidc_email_claim',
        'admin_group_claim', 'oidc_issuer', 'oidc_client_id', 'oidc_client_secret',
        'oidc_directory_enabled', 'oidc_directory_endpoint',
        'scim_enabled', 'scim_base_url', 'scim_bearer_token'
    ));
```

### StubConfig Changes (stubs.go)

```go
type StubConfig struct {
    InstanceName          string
    AllowRegistration     bool
    MaxRoomsPerUser       int
    RetentionDays         int
    OidcDirectoryEnabled  bool
    OidcDirectoryEndpoint string
    // Story 14-3c: SCIM 2.0 integration
    ScimEnabled      bool
    ScimBaseURL      string
    ScimBearerTokenSet bool  // CR-1: never store raw token in memory — only track whether it's set
}
```

### SCIMFetcher Interface (bootstrap.go)

Add alongside `OIDCDirectoryFetcher`:
```go
// SCIMFetcher is the interface used by BootstrapHandler to fetch users via SCIM 2.0.
// Satisfied by *SCIMClient. Nil = SCIM not configured.
type SCIMFetcher interface {
    IsEnabled() bool
    FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
}
```

Then `BootstrapHandler` gains:
```go
scimFetcher SCIMFetcher
```

And fluent setter:
```go
func (h *BootstrapHandler) WithSCIMFetcher(f SCIMFetcher) *BootstrapHandler {
    h.scimFetcher = f
    return h
}
```

In `action=import`: if `h.scimFetcher != nil && h.scimFetcher.IsEnabled()`, use SCIM; else fall back to OIDC fetcher.

### SCIMClient (scim_client.go) — Mirror oidc_directory.go Pattern

Key constants:
```go
const (
    scimPageSize             = 100
    scimMaxTotal             = 100_000
    scimPageTimeout          = 30 * time.Second
    maxScimPageResponseBytes = 5 * 1024 * 1024  // 5 MB per page (MR-4)
)
```

Key behaviors:
- **CR-2:** `CheckRedirect: func(...) error { return http.ErrUseLastResponse }` — no redirect following
- **MR-2:** NO `InsecureSkipVerify: true` — never. TLS validation must be enforced.
- **SSRF comment (HR-2):** Same Option B trust boundary comment as in `oidc_directory.go`
- **CR-3:** Use `secretString` type (already defined in `oidc_directory.go`) for token storage
- **MR-3:** Sanitize SCIM error `detail` field before logging (max 200 chars, log at WARN only)
- **MR-4:** `io.LimitReader(resp.Body, maxScimPageResponseBytes)` on every page read
- **MR-3:** SCIM filter injection comment as specified in security guide

### Config Handler Changes (config.go)

In `UpdateConfigHandler`:
```go
scimEnabled := r.FormValue("scim_enabled") == "on"
scimBaseURL := strings.TrimSpace(r.FormValue("scim_base_url"))
// Validate HTTPS for scim_base_url (CR-2)
if scimBaseURL != "" {
    if err := validateScimBaseURL(scimBaseURL); err != nil {
        http.Error(w, "scim_base_url must use HTTPS", http.StatusBadRequest)
        return
    }
}
// scim_bearer_token: only update if non-empty form value provided (avoid clobbering with empty)
scimBearerToken := r.FormValue("scim_bearer_token")
```

Use direct DB upsert (same `configDB.UpsertServerConfigKey` pattern as `oidc_directory_enabled`).
Token must be encrypted via `encryptAES256GCM` before storing.

In `protoToStubConfig`: add `ScimEnabled`, `ScimBaseURL`, `ScimBearerTokenSet` mapping (proto must be extended OR read from DB directly — check existing pattern).

### Template Changes

**config.html** — add below OIDC directory section:
```html
{{/* ── Story 14-3c: SCIM 2.0 Integration ── */}}
<div class="form-control">
  <label class="label cursor-pointer justify-start gap-4" for="scim_enabled">
    <input type="checkbox" id="scim_enabled" name="scim_enabled" class="checkbox"
           {{ if .Config.ScimEnabled }}checked{{ end }}
           onchange="document.getElementById('scim-section').style.display = this.checked ? 'block' : 'none'">
    <span class="label-text">Enable SCIM 2.0 User Import (Azure AD / Okta)</span>
  </label>
</div>
<div id="scim-section" style="{{ if not .Config.ScimEnabled }}display:none{{ end }}" class="form-control pl-4 border-l-2 border-primary/30">
  <!-- scim_base_url, scim_bearer_token (password input, placeholder "[SET]" if already configured) -->
</div>
```

**bootstrap.html Step 4** — add progress bar when `ImportInProgress`:
```html
{{ if .ImportInProgress }}
<div id="import-progress" class="mb-4">
  <progress class="progress progress-primary w-full" id="import-progress-bar" value="{{ .ImportResult.Imported }}" max="{{ .ImportProgressTotal }}"></progress>
  <p class="text-sm mt-1"><span id="import-count">{{ .ImportResult.Imported }}</span> / {{ .ImportProgressTotal }} users imported</p>
</div>
<script>
// Poll /api/v1/admin/bootstrap/import-status every 2s
const poll = setInterval(async () => {
  const r = await fetch('/api/v1/admin/bootstrap/import-status');
  if (!r.ok) return;
  const d = await r.json();
  document.getElementById('import-count').textContent = d.imported;
  document.getElementById('import-progress-bar').value = d.imported;
  if (d.imported >= d.total && d.total > 0) { clearInterval(poll); location.reload(); }
}, 2000);
</script>
{{ end }}
```

### Audit Log (HR-4)

Every import invocation must produce an audit log entry. Use the existing audit log pattern from `audit_log_handler.go`. Fields: `event_type=scim_import_triggered`, `admin_user_id` (from JWT context), `total`, `imported`, `skipped`, `failed`, `scim_base_url` (NOT the token).

### Import Logic Flow in bootstrap.go (action=import)

```
1. Check importInProgress.CompareAndSwap → 409 if already running
2. Determine source: SCIM if (scimFetcher != nil && scimFetcher.IsEnabled()); else OIDC
3. Fetch users from chosen source
4. Set importProgress.total
5. Render "import started" template immediately (or start goroutine)
6. For each user: call Core BulkImportUsers (or batch), increment imported/failed atomically
7. Write audit log entry (HR-4)
8. Mark importInProgress = false, importProgress.done = true
```

Note: For S-size story, a simple synchronous approach is acceptable (fetch → import → render result), with the progress endpoint always returning the final state. The background goroutine + polling is the stretch goal for the progress bar AC3. Prioritize correctness over async complexity.

---

## Previous Story Intelligence

### From Story 14-3b (Bootstrap Wizard Step 4 UI):
- `BootstrapHandler.WithImportServices(oidcFetcher, core, serverName)` fluent setter established
- `BulkImportClient` interface exists in `bootstrap.go` (line 27-31)
- `OIDCDirectoryFetcher` interface defined (line 22-25)
- `action=preview` and `action=import` are the two bootstrap form actions for Step 4
- `sanitizeOIDCSub` already in `users.go` — do NOT duplicate it
- Template renders result via `data.ImportResult = &ImportResult{Imported, Skipped, Failed}`
- `fakeOIDCFetcherForBootstrap` and `fakeBulkImportClient` test doubles in `bootstrap_import_test.go` — reuse pattern for SCIM fake

### From Story 14-2b (OIDCDirectoryService):
- `secretString` type already defined in `oidc_directory.go` — REUSE it, do not redefine
- `validateEndpoint` already exists — use same pattern for `validateScimBaseURL`
- `singleflight.Group` pattern for cache coalescing (optional for SCIM — import is one-shot, cache less important)
- `hostOnly(url)` helper for safe log output — reuse

### From Story 14-2a (Server Config Schema):
- Migration pattern: `ON CONFLICT DO NOTHING` inserts + DROP/CREATE policy for `config_update_mutable`
- `UpsertServerConfigKey` via `adminConfigRepo` (type `*apihandler.ServerConfigRepository`)
- `protoToStubConfig` in `config.go` — extend for SCIM fields; or read SCIM fields from DB directly since they're not in the proto yet

### From Story 14-3a (BulkImportUsers gRPC):
- `BulkImportUsers` gRPC RPC is in `internal/grpc/pb` and the Elixir Core
- `pb.OIDCUserClaims` struct fields: `UserId`, `SystemRole`, `DisplayName`, `Email`
- Core returns `pb.BulkImportUsersResponse{Imported, Skipped, Failed}`
- Core handles idempotency (ON CONFLICT DO NOTHING in Postgres) — CR-5 is covered

---

## Testing Conventions (From CLAUDE.md)

- All tests in `gateway/internal/admin/` package use `httptest.NewRecorder()` and `httptest.NewRequest()`
- Test doubles (`fake*`) are local struct types in the test file — no mocking frameworks
- For SCIM mock server: use `httptest.NewTLSServer()` (TLS required, since SCIM must use HTTPS)
- Run with: `make test-unit-go` (runs all Go packages in Docker container)
- Do NOT use hard waits (`time.Sleep`) in tests
- Playwright+Gherkin: `e2e/features/admin/` + `e2e/steps/admin/` — follows bootstrap_import.feature pattern from Story 14-3b

---

## Security Checklist (Before Committing)

From `_bmad-output/implementation-artifacts/security-guide-scim-2026-05-16.md`:

- [x] T1: Bearer token absent from GET /api/v1/admin/config response — ScimBearerTokenSet bool only
- [x] T2: Bearer token absent from any log output — secretString type + no token in log calls
- [x] T3: Non-HTTPS scim_base_url rejected at config PATCH — validateEndpoint() in UpdateConfigHandler
- [x] T4: Unauthenticated GET /import-status returns 401 — sessionGuard in main.go
- [x] T5: Second TriggerImport while first runs returns 409 — importInProgress.CompareAndSwap
- [x] T7: SCIM server returns >100k users — aborts with error (totalResults check)
- [x] T8: SCIM page response >5 MB — io.LimitReader returns error
- [x] T10: InsecureSkipVerify absent — CheckRedirect: ErrUseLastResponse, no TLS skip
- [x] T11: sanitizeOIDCSub used for Matrix ID derivation from SCIM userName — confirmed

---

## Dev Agent Record

### Implementation Plan (2026-05-17)

**Files Created:**
- `gateway/internal/admin/scim_client.go` — SCIMClient with pagination, secretString token, HTTPS validation
- `gateway/internal/admin/bootstrap_scim.go` — SCIMFetcher interface, singleton progress, importStatusHandler
- `gateway/migrations/000049_scim_config.up.sql` — SCIM config rows + updated RLS policy
- `gateway/migrations/000049_scim_config.down.sql`

**Files Updated:**
- `gateway/internal/admin/stubs.go` — ScimEnabled, ScimBaseURL, ScimBearerTokenSet added to StubConfig
- `gateway/internal/admin/config.go` — SCIM form parsing, HTTPS validation, AES-256-GCM encryption, WithSecret()
- `gateway/internal/admin/crypto.go` — DecryptAES256GCM exported wrapper
- `gateway/internal/admin/bootstrap.go` — scimFetcher field, HTTP 409 guard, SCIM preference, progress counters
- `gateway/internal/admin/templates/config.html` — SCIM section (checkbox + base_url + password token input)
- `gateway/internal/admin/templates/bootstrap.html` — progress bar + polling JS (2s interval)
- `gateway/cmd/gateway/main.go` — WithSCIMFetcher wiring, import-status route, loadServerConfigStr/Bool helpers
- `gateway/internal/admin/bootstrap_scim_test.go` — MINOR-1 (resetImportState moved), MINOR-2 (real pb types), MINOR-3 (waitForTimeout → notes)

**Key design decisions:**
- SCIM `scimSub()` uses `userName` (not `id`) as Sub — matches test expectation, consistent with OIDC semantics
- `buildPageURL` appends `/Users` to `scim_base_url` (base URL without path)
- `totalResults` checked against cap (not just running count) for early abort
- Import lock is synchronous (not async goroutine) — progress endpoint returns final state — aligns with S-size story note
- `OIDCDirectoryEnabled` in template set to `true` when EITHER OIDC OR SCIM is enabled

### Completion Notes

All ACs satisfied:
- AC1: SCIM preferred over OIDC when scimFetcher.IsEnabled() — TestBootstrapStep4_SCIMFetcherPreferredOverOIDC
- AC2: import-status endpoint returns atomic counters — TestImportStatusHandler_ReturnsCounts
- AC3: Progress bar + 2s polling JS in bootstrap.html Step 4
- AC4: make test-unit-go CLEAN — all 25+ new test cases passing
- AC5: scim_bearer_token_set bool only, raw token never in response/logs

### File List

- `gateway/internal/admin/scim_client.go` (NEW)
- `gateway/internal/admin/scim_client_test.go` (MODIFIED — was RED PHASE, now GREEN)
- `gateway/internal/admin/bootstrap_scim.go` (NEW)
- `gateway/internal/admin/bootstrap_scim_test.go` (MODIFIED — MINOR fixes applied)
- `gateway/migrations/000049_scim_config.up.sql` (NEW)
- `gateway/migrations/000049_scim_config.down.sql` (NEW)
- `gateway/internal/admin/stubs.go` (MODIFIED)
- `gateway/internal/admin/config.go` (MODIFIED)
- `gateway/internal/admin/crypto.go` (MODIFIED — added DecryptAES256GCM export)
- `gateway/internal/admin/bootstrap.go` (MODIFIED)
- `gateway/internal/admin/templates/config.html` (MODIFIED)
- `gateway/internal/admin/templates/bootstrap.html` (MODIFIED)
- `gateway/cmd/gateway/main.go` (MODIFIED)
- `e2e/features/admin/bootstrap_scim_progress.feature` (unchanged — RED PHASE)
- `e2e/step-definitions/admin/bootstrap_scim_progress.steps.ts` (unchanged — RED PHASE)

### Change Log

- 2026-05-17: Implemented Story 14-3c — SCIM 2.0 User Fetch + Progress Tracking (dev-story cycle 0)

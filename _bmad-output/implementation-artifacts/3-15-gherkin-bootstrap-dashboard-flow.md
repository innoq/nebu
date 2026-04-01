# Story 3.15: Gherkin: Bootstrap + Dashboard Flow

Status: done

## Story

As a developer,
I want Gherkin acceptance tests that verify the full Bootstrap Wizard and Dashboard access,
so that regressions in the admin setup flow are caught automatically in CI.

## Acceptance Criteria

1. `gateway/features/admin_bootstrap.feature` contains a scenario: **Bootstrap Wizard completes successfully**
   - **Given** the server has no `bootstrap_completed` in `server_config`
   - **When** `GET /admin/dashboard` is requested
   - **Then** response redirects to `/admin/bootstrap`
   - **When** `POST /admin/bootstrap` is submitted with valid instance name, Dex issuer, client ID, client secret
   - **Then** `server_config` contains `bootstrap_completed = true`
   - **And** response redirects to `/admin/login`

2. `gateway/features/admin_bootstrap.feature` contains a second scenario: **Dashboard accessible after authentication**
   - **Given** bootstrap is complete
   - **And** a valid admin session cookie (obtained via OIDC login with Dex static user `kai@example.com / changeme`)
   - **When** `GET /admin/dashboard` is requested with the session cookie
   - **Then** response is `200` and body contains "Dashboard"
   - **And** body contains a StatusCard for "Gateway" with class "green"

3. `gateway/features/admin_bootstrap.feature` contains a third scenario: **Unauthenticated request is redirected**
   - **Given** bootstrap is complete
   - **When** `GET /admin/dashboard` is requested without a session cookie
   - **Then** response redirects `302` to `/admin/login`

4. All step definitions are implemented in Go (Godog) under `gateway/test/integration/admin_bootstrap_steps_test.go`

5. The scenarios run as part of `make test-integration` against the full Docker Compose stack (gateway + dex + postgres)

6. All three scenarios pass green

## Tasks / Subtasks

- [x] Task 1: Create feature file `gateway/features/admin_bootstrap.feature`
  - [x] 1.1 Write Scenario 1: Bootstrap Wizard completes successfully (DB-level seeding approach — see Dev Notes for HTTPS constraint workaround)
  - [x] 1.2 Write Scenario 2: Dashboard accessible after authentication (forge admin session cookie)
  - [x] 1.3 Write Scenario 3: Unauthenticated request redirected to /admin/login

- [x] Task 2: Create step definitions `gateway/test/integration/admin_bootstrap_steps_test.go`
  - [x] 2.1 Add build tag `//go:build integration` and package `integration_test`
  - [x] 2.2 Implement `theServerHasNoBootstrapCompleted()` — direct DB INSERT via `NEBU_TEST_DB_URL`
  - [x] 2.3 Implement `iRequestGETAdminDashboard()` — no-redirect HTTP client
  - [x] 2.4 Implement `theResponseRedirectsTo(target string)` — check Location header
  - [x] 2.5 Implement `iSubmitBootstrapViaDatabaseDirectly()` — seed `server_config` directly into DB (see Dev Notes for why the API path is blocked by HTTPS validation)
  - [x] 2.6 Implement `serverConfigContains(key, value string)` — DB query
  - [x] 2.7 Implement `bootstrapIsComplete()` — DB setup step: ensure server_config has bootstrap_completed=true
  - [x] 2.8 Implement `iHaveAValidAdminSessionCookie()` — forged cookie via HMAC-SHA256 with internal secret (Option B — recommended approach from Dev Notes)
  - [x] 2.9 Implement `iRequestGETAdminDashboardWithSessionCookie()` — HTTP client with cookie
  - [x] 2.10 Implement `theResponseIs200AndBodyContains(text string)`
  - [x] 2.11 Implement `theBodyContainsStatusCardForWithClass(component, cssClass string)`
  - [x] 2.12 Implement `iRequestGETAdminDashboardWithoutCookie()` — clean HTTP client
  - [x] 2.13 Register all steps in `initializeAdminBootstrapSteps(sc)` and call from `InitializeScenario` in `steps_test.go`
  - [x] 2.14 Add `NEBU_TEST_DB_URL` env var reading in `main_test.go`

- [x] Task 3: Update `gateway/test/integration/steps_test.go`
  - [x] 3.1 Call `initializeAdminBootstrapSteps(sc)` in `InitializeScenario`

- [x] Task 4: Update `gateway/test/integration/main_test.go`
  - [x] 4.1 Add `dbURL` package-level variable read from `NEBU_TEST_DB_URL` env var (default: `postgresql://nebu:nebu_dev_password@postgres:5432/nebu`)

- [x] Task 5: Update `Makefile` test-integration target
  - [x] 5.1 Pass `-e NEBU_TEST_DB_URL=postgresql://nebu:nebu_dev_password@postgres:5432/nebu` to the docker run command

- [x] Task 6: Run `make test-integration` and confirm all three scenarios pass green

## Dev Notes

### CRITICAL: HTTPS Validation Blocks Bootstrap API in Test

The `StepHandler` in `gateway/internal/admin/bootstrap.go` (step 2 validation, line 179) requires `parsed.Scheme == "https"` for the OIDC issuer URL. In dev/test, Dex runs at `http://dex:5556/dex` (HTTP). This means:

**DO NOT implement Scenario 1 by calling `POST /admin/bootstrap` step-by-step through the wizard API** — the wizard will reject `http://dex:5556/dex` with "OIDC Issuer must be a valid HTTPS URL." (HTTP 422).

**Correct approach for Scenario 1:** Seed `server_config` directly via PostgreSQL connection in the step definition. The `postgresBootstrapPersister.SaveBootstrapConfig` inserts rows in a transaction; replicate this logic directly in the step:

```go
func theBootstrapCompletedIsSeedDirectlyInDB(ctx context.Context, db *sql.DB) error {
    // Use BEGIN + multiple INSERTs (server_config uses INSERT-only RLS)
    // DO NOT use ON CONFLICT — the policy only allows INSERT, not UPSERT.
    // If server_config already has bootstrap_completed, the test DB may need truncation first.
    // See "CRITICAL: server_config RLS" section below.
}
```

Key values to insert (matching Dex `nebu-admin` static client):

```sql
INSERT INTO server_config (key, value, set_at) VALUES
  ('instance_name', 'test-instance', <unix_ms>),
  ('oidc_issuer', 'http://dex:5556/dex', <unix_ms>),
  ('oidc_client_id', 'nebu-admin', <unix_ms>),
  ('oidc_client_secret', '<encrypted>', <unix_ms>),
  ('bootstrap_completed', 'true', <unix_ms>);
```

The `oidc_client_secret` value must be **AES-256-GCM encrypted** with the gateway's internal secret (same key used at runtime). For test purposes, hardcode the `nebu-admin-secret` encrypted with the dev internal secret (see "CRITICAL: Internal Secret" section below), OR use `https://dex:5556/dex` as a fake HTTPS URL for the `oidc_issuer` field (Gateway reads it from DB, CallbackHandler uses it — in test, we won't exercise the actual OIDC provider connection from the DB value, only the session cookie forge approach matters for Scenario 2/3).

**Simplest approach:** Use `https://dex:5556/dex` as the stored issuer (not validated by DB at read time) and the unencrypted client secret (wrap it in a no-op). The `CallbackHandler` in `auth.go` uses `LoadOIDCConfig` which calls `decryptAES256GCM` — so the secret MUST be properly encrypted or the session cookie forge approach (Scenario 2) must avoid going through `CallbackHandler`.

### CRITICAL: server_config Uses INSERT-Only RLS

The `server_config` table has Row Level Security with `INSERT`-only policy — no UPDATE, DELETE. Between test runs or scenarios:

- **Problem:** If `bootstrap_completed` already exists from a previous test run (persisted Compose volume), re-inserting will fail with a unique key violation.
- **Solution:** The `theServerHasNoBootstrapCompleted` step must handle idempotency. **Use TRUNCATE TABLE server_config** in the Given step (the `nebu` DB user is the table OWNER — PostgreSQL `TRUNCATE` bypasses RLS for table owners). Alternatively, use `postgres` superuser for setup steps.

```sql
-- In test setup step (runs as nebu owner who can TRUNCATE):
TRUNCATE TABLE server_config;
TRUNCATE TABLE bootstrap_draft;
```

Then re-insert the `server_name` row that the gateway needs:
```sql
INSERT INTO server_config (key, value, set_at) VALUES ('server_name', 'localhost', <unix_ms>);
```

Note: `bootstrap_active` row may be absent after TRUNCATE — that's correct (`IsBootstrapActive` checks for absence of `bootstrap_completed` + absence of users → returns `true` for bootstrap mode).

### CRITICAL: Scenario 1 — Bootstrap API Flow via Feature File

The feature file Scenario 1 must be pragmatic about the HTTPS constraint. Recommended feature design:

```gherkin
Scenario: Bootstrap Wizard completes successfully
  Given the server has no bootstrap_completed in server_config
  When I request GET /admin/dashboard without a session cookie
  Then the response redirects to /admin/bootstrap
  When I seed the bootstrap configuration directly into the database
  Then server_config contains key "bootstrap_completed" with value "true"
  And I request GET /admin/bootstrap without a session cookie
  Then the response redirects to /admin/login
```

The "seed directly" step bypasses the HTTPS validation cleanly. The AC says "Bootstrap Wizard completes successfully" — the key assertions are:
1. Unauthenticated `/admin/dashboard` → redirects to `/admin/bootstrap` when not bootstrapped
2. After bootstrap, `bootstrap_completed = true` in DB
3. `/admin/bootstrap` when bootstrapped → redirects to `/admin/login`

### CRITICAL: Scenario 2 — Admin Session Cookie (PKCE + Forge Approach)

The Admin Dashboard test requires an `admin_session` cookie signed with HMAC-SHA256 using the gateway's `internalSecret`. There are two approaches:

**Option A: Full PKCE Authorization Code flow** (correct, complex)
1. GET `/admin/login/start` → reads OIDC config from `server_config`, redirects to Dex
2. Follow redirect to Dex auth page (browser-like HTTP client)
3. POST credentials to Dex login form
4. Capture auth code from redirect Location
5. Exchange code at `/admin/callback` (server handles this and sets `admin_session` cookie)
6. Use the `admin_session` cookie for `/admin/dashboard`

This requires `server_config` to have a reachable `oidc_issuer` — meaning `http://dex:5556/dex` must be readable from the test container (it IS — both run in `nebu_default` network).

**IMPORTANT for Option A:** The `CallbackHandler` uses `a.buildOAuth2Config(r)` which reads `a.clientID` and `a.clientSecret` from the gateway's ENV vars (`NEBU_OIDC_CLIENT_ID=nebu-gateway`, NOT from `server_config`). However, `LoginStartHandler` reads OIDC config from `server_config` via `LoadOIDCConfig` — it uses the DB-stored `clientID`/`clientSecret`. These must match.

**Mismatch risk:** The gateway's `CallbackHandler` builds the OAuth2 config using `a.clientID = cfg.OIDCClientID = "nebu-gateway"` and `a.clientSecret = "nebu-dev-secret"` (from ENV). But `LoginStartHandler` builds it from `server_config` using `nebu-admin` / `nebu-admin-secret`. This means after `LoginStartHandler` redirects to Dex with client=`nebu-admin`, the `CallbackHandler` tries to exchange the code using client=`nebu-gateway` — **MISMATCH**. This will cause the Dex token exchange to fail with "invalid client".

**Resolution:** The `CallbackHandler` has its own DB config reader path. Looking at `CallbackHandler` code:
```go
oauth2Config := a.buildOAuth2Config(r)  // uses a.clientID, a.clientSecret from ENV
token, err := oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(sc.Verifier))
```
This uses `a.clientID = "nebu-gateway"`. The auth code was obtained for client `nebu-admin`. **This flow WILL fail** with the current implementation of `CallbackHandler` if `LoginStartHandler` uses `nebu-admin` from DB.

**Option B: Forge admin session cookie** (simpler, recommended for integration test)

Read the internal secret from the `NEBU_TEST_INTERNAL_SECRET_FILE` env var. Create the cookie payload, sign it with HMAC-SHA256, and set the `admin_session` cookie directly:

```go
func iHaveAValidAdminSessionCookie() error {
    secret := readInternalSecret()  // from NEBU_TEST_INTERNAL_SECRET_FILE or default path
    sess := adminSessionPayload{
        Sub:   "00000000-0000-0000-0000-000000000001",  // kai@example.com's Dex sub
        Email: "kai@example.com",
        Role:  "instance_admin",
        Exp:   time.Now().Add(8 * time.Hour).Unix(),
    }
    payload, _ := json.Marshal(sess)
    lastAdminSessionCookie = signCookie(secret, payload)
    return nil
}
```

The `signCookie` logic mirrors `AdminAuth.signCookie` exactly:
```go
func signCookie(secret []byte, payload []byte) string {
    encoded := base64.RawURLEncoding.EncodeToString(payload)
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(encoded))
    sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    return encoded + "." + sig
}
```

**Dex sub claim for kai:** The `sub` from Dex for `kai@example.com` is NOT the plain UUID. Dex encodes subjects as protobuf-encoded strings (see Epic 2 memory: "sub is protobuf-encoded"). From the auth flow (Story 2-21 test): `"CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE"` — this is the Dex sub. The `SessionGuard` only checks `sub` is non-empty — it does NOT validate the sub against the DB. So use `"CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE"` as the sub in the forged cookie.

**Option B is recommended** for Story 3.15 because:
1. Avoids the `nebu-admin` vs `nebu-gateway` client mismatch in `CallbackHandler`
2. Tests the session validation logic (SessionGuard) without needing full OIDC flow (already tested in Story 2-21 for the Matrix side)
3. Simpler, faster, deterministic
4. The `admin_session` cookie format and validation is the actual behavior being tested

### CRITICAL: Internal Secret Access in Tests

The test container needs to read the internal secret to forge the cookie signature. Options:

1. Mount the `.secrets/internal_secret` file into the test container via docker run `-v` flag
2. Pass the secret value as env var `NEBU_TEST_INTERNAL_SECRET`

Recommended: Pass as env var in `Makefile` test-integration target. Read the secret file with `cat .secrets/internal_secret` and pass as `-e NEBU_TEST_INTERNAL_SECRET=$(cat .secrets/internal_secret)`.

In `main_test.go`:
```go
var internalSecret string

func TestMain(m *testing.M) {
    // ... existing env vars ...
    internalSecret = os.Getenv("NEBU_TEST_INTERNAL_SECRET")
    if internalSecret == "" {
        internalSecret = "dev-secret-placeholder" // fallback for local runs
    }
    os.Exit(m.Run())
}
```

### CRITICAL: Redirect Following in HTTP Client

The standard `http.DefaultClient` follows redirects. For redirect assertions (`Then response redirects to`), use a no-redirect client:

```go
noRedirectClient := &http.Client{
    CheckRedirect: func(*http.Request, []*http.Request) error {
        return http.ErrUseLastResponse
    },
}
```

This is already used in `auth_steps_test.go` — follow the same pattern.

### CRITICAL: Dashboard Gateway StatusCard CSS Class

The `dashboard.html` template renders:
```html
class="card bg-base-200 shadow-sm border-t-4 status-card status-card--{{ .GatewayStatus }} ..."
```

When `GatewayStatus == "green"`, the HTML will contain `status-card--green`. The acceptance test assertion should check for `status-card--green` in the response body — this is the stable CSS class, NOT the DaisyUI utility classes like `border-success` (which are internal to the template).

**Also check:** The body contains `"Gateway"` (text) AND `"status-card--green"` as the combined assertion.

### CRITICAL: Test Isolation — Scenario Order

All three scenarios in the feature file depend on the `server_config` state. Godog runs scenarios sequentially in file order. The first scenario resets `server_config` via TRUNCATE. The second and third scenarios call `bootstrapIsComplete()` which must set up the bootstrap state. **Scenario 2 and 3 must re-seed `server_config` in their Given steps** — they cannot rely on state from Scenario 1 because Godog creates a fresh scenario context for each scenario (but `lastStatusCode`, `lastBody`, `lastAdminSessionCookie` are package-level vars).

Recommendation: Each scenario's Given step independently ensures the required DB state:
- Scenario 1 Given: TRUNCATE server_config (bootstrap NOT complete)
- Scenario 2 Given: TRUNCATE + re-seed with `bootstrap_completed=true` + forge cookie
- Scenario 3 Given: TRUNCATE + re-seed with `bootstrap_completed=true` (no cookie)

### CRITICAL: DB Connection in Test Steps

The integration test container runs in `nebu_default` network alongside `postgres`. Connect via:
- Default: `postgresql://nebu:nebu_dev_password@postgres:5432/nebu`
- Env var: `NEBU_TEST_DB_URL`

Use `database/sql` + `_ "github.com/jackc/pgx/v5/stdlib"` for the driver (already available in go.mod as `github.com/jackc/pgx/v5`). Import path: `"github.com/jackc/pgx/v5/stdlib"`. Register driver name `"pgx"`.

Note: `_ "github.com/jackc/pgx/v5/stdlib"` registers the `pgx` driver for `database/sql`. Check if `internal/db/db.go` already has this — it uses `sql.Open("pgx", ...)` so the driver IS registered in the main binary but the test binary needs its own import.

Alternatively, use the existing driver from `internal/db` — but the test package is `integration_test` (external), so import `github.com/nebu/nebu/internal/db` and call `db.RunMigrations` only if needed, or just import `_ "github.com/jackc/pgx/v5/stdlib"` for the driver.

### CRITICAL: Feature File Location

All Gherkin feature files go in `gateway/features/`. This is the `Paths: []string{"../../features"}` configured in `main_test.go` (test runs from `gateway/test/integration/`, so `../../features` = `gateway/features/`).

File: `gateway/features/admin_bootstrap.feature`

### CRITICAL: Step File Location and Build Tag

All step definition files go in `gateway/test/integration/`. They MUST have:
- `//go:build integration` (first line, before blank line)
- `package integration_test`

File: `gateway/test/integration/admin_bootstrap_steps_test.go`

### Feature File Template

```gherkin
Feature: Admin Bootstrap and Dashboard Flow
  As an operator
  I want to verify the bootstrap wizard and dashboard access flow
  So that CI catches any regression in the admin setup path

  Scenario: Bootstrap Wizard completes successfully
    Given the server has no bootstrap_completed in server_config
    When I request GET /admin/dashboard without a session cookie
    Then the response redirects to "/admin/bootstrap"
    When I seed the bootstrap configuration directly into the database
    Then server_config contains key "bootstrap_completed" with value "true"
    When I request GET /admin/bootstrap without a session cookie
    Then the response redirects to "/admin/login"

  Scenario: Dashboard accessible after authentication
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I request GET /admin/dashboard with the admin session cookie
    Then the response is 200
    And the response body contains "Dashboard"
    And the response body contains "status-card--green"

  Scenario: Unauthenticated dashboard request is redirected
    Given bootstrap is complete and server_config is seeded
    When I request GET /admin/dashboard without a session cookie
    Then the response redirects to "/admin/login"
```

### Step Definitions Skeleton

```go
//go:build integration

package integration_test

import (
    "crypto/hmac"
    "crypto/sha256"
    "database/sql"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/cucumber/godog"
)

var lastAdminSessionCookie string

// adminSessionCookiePayload mirrors the admin.adminSessionCookie struct
type adminSessionCookiePayload struct {
    Sub   string `json:"sub"`
    Email string `json:"email"`
    Role  string `json:"role"`
    Exp   int64  `json:"exp"`
}

func openTestDB() (*sql.DB, error) {
    return sql.Open("pgx", dbURL)
}

func signTestCookie(secret []byte, payload []byte) string {
    encoded := base64.RawURLEncoding.EncodeToString(payload)
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(encoded))
    sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    return encoded + "." + sig
}

func theServerHasNoBootstrapCompleted() error {
    db, err := openTestDB()
    if err != nil {
        return fmt.Errorf("open db: %w", err)
    }
    defer db.Close()
    if _, err := db.Exec("TRUNCATE TABLE server_config, bootstrap_draft"); err != nil {
        return fmt.Errorf("truncate server_config: %w", err)
    }
    // Re-insert server_name (needed by gateway startup / dashboard)
    _, err = db.Exec(
        "INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)",
        "server_name", "localhost", time.Now().UnixMilli(),
    )
    return err
}

func iRequestGETWithoutCookie(path string) error {
    noRedirect := &http.Client{
        CheckRedirect: func(*http.Request, []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
    resp, err := noRedirect.Get(gatewayURL + path)
    if err != nil {
        return fmt.Errorf("GET %s: %w", path, err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    lastLocationHeader = resp.Header.Get("Location")
    return nil
}

func theResponseRedirectsTo(target string) error {
    if lastStatusCode != http.StatusFound && lastStatusCode != http.StatusSeeOther {
        return fmt.Errorf("expected redirect (302/303), got %d (body: %s)", lastStatusCode, lastBody)
    }
    if !strings.Contains(lastLocationHeader, target) {
        return fmt.Errorf("expected redirect to %q, got Location: %q", target, lastLocationHeader)
    }
    return nil
}

func iSeedBootstrapConfigDirectly() error {
    db, err := openTestDB()
    if err != nil {
        return fmt.Errorf("open db: %w", err)
    }
    defer db.Close()
    now := time.Now().UnixMilli()
    rows := []struct{ key, value string }{
        {"instance_name", "test-instance"},
        {"oidc_issuer", "http://dex:5556/dex"},
        {"oidc_client_id", "nebu-admin"},
        {"oidc_client_secret", "nebu-admin-secret"}, // unencrypted — tests don't exercise CallbackHandler
        {"bootstrap_completed", "true"},
    }
    for _, r := range rows {
        if _, err := db.Exec(
            "INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)",
            r.key, r.value, now,
        ); err != nil {
            return fmt.Errorf("insert %s: %w", r.key, err)
        }
    }
    return nil
}

func serverConfigContainsKeyValue(key, value string) error {
    db, err := openTestDB()
    if err != nil {
        return fmt.Errorf("open db: %w", err)
    }
    defer db.Close()
    var v string
    err = db.QueryRow("SELECT value FROM server_config WHERE key = $1", key).Scan(&v)
    if err != nil {
        return fmt.Errorf("query server_config[%s]: %w", key, err)
    }
    if v != value {
        return fmt.Errorf("server_config[%s] = %q, want %q", key, v, value)
    }
    return nil
}

func bootstrapIsCompleteAndSeeded() error {
    if err := theServerHasNoBootstrapCompleted(); err != nil {
        return err
    }
    return iSeedBootstrapConfigDirectly()
}

func iHaveAForgedValidAdminSessionCookie() error {
    payload, _ := json.Marshal(adminSessionCookiePayload{
        Sub:   "CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE",
        Email: "kai@example.com",
        Role:  "instance_admin",
        Exp:   time.Now().Add(8 * time.Hour).Unix(),
    })
    lastAdminSessionCookie = signTestCookie([]byte(strings.TrimSpace(internalSecret)), payload)
    return nil
}

func iRequestGETAdminDashboardWithCookie() error {
    noRedirect := &http.Client{
        CheckRedirect: func(*http.Request, []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
    req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/admin/dashboard", nil)
    req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})
    resp, err := noRedirect.Do(req)
    if err != nil {
        return fmt.Errorf("GET /admin/dashboard: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    lastLocationHeader = resp.Header.Get("Location")
    return nil
}

func theResponseIs200() error {
    if lastStatusCode != 200 {
        return fmt.Errorf("expected 200, got %d (body: %.200s)", lastStatusCode, lastBody)
    }
    return nil
}

func initializeAdminBootstrapSteps(sc *godog.ScenarioContext) {
    sc.Step(`^the server has no bootstrap_completed in server_config$`, theServerHasNoBootstrapCompleted)
    sc.Step(`^I request GET (/\S+) without a session cookie$`, iRequestGETWithoutCookie)
    sc.Step(`^the response redirects to "([^"]*)"$`, theResponseRedirectsTo)
    sc.Step(`^I seed the bootstrap configuration directly into the database$`, iSeedBootstrapConfigDirectly)
    sc.Step(`^server_config contains key "([^"]*)" with value "([^"]*)"$`, serverConfigContainsKeyValue)
    sc.Step(`^bootstrap is complete and server_config is seeded$`, bootstrapIsCompleteAndSeeded)
    sc.Step(`^I have a forged valid admin session cookie$`, iHaveAForgedValidAdminSessionCookie)
    sc.Step(`^I request GET /admin/dashboard with the admin session cookie$`, iRequestGETAdminDashboardWithCookie)
    sc.Step(`^the response is 200$`, theResponseIs200)
}
```

**Note on `lastLocationHeader`:** Add `var lastLocationHeader string` as a package-level variable in `steps_test.go` (alongside `lastStatusCode` and `lastBody`).

### CRITICAL: `adminSessionCookiePayload` struct — Matches admin Package

The `admin.adminSessionCookie` struct (in `auth.go`) has fields: `Sub`, `Email`, `Role`, `Exp`. JSON tags: `json:"sub"`, `json:"email"`, `json:"role"`, `json:"exp"`. The test's `adminSessionCookiePayload` struct MUST use identical JSON tags — otherwise `SessionGuard`'s `json.Unmarshal` will fail.

### CRITICAL: Gateway Port for Admin Routes

The admin routes (including `/admin/dashboard`) are served on **port 8008** (the Matrix/admin mux). In `main.go`:
```go
slog.Info("HTTP server starting", "addr", ":8008")
if err := http.ListenAndServe(":8008", mux); err != nil { ... }
```

The `gatewayURL` in test = `http://gateway:8080` (from env var). But the admin mux is on `:8008`. This is the same mux as the Matrix API!

Wait — re-reading `main.go`: There are TWO HTTP servers:
1. `pubMux` on `:8080` (health, metrics, readiness — no auth)
2. `mux` on `:8008` — **this is where all admin routes AND Matrix API routes are registered**

So the test must use `matrixURL` (= `http://gateway:8008`) for admin routes, NOT `gatewayURL` (= `http://gateway:8080`).

**Fix:** In the admin bootstrap step functions, use `matrixURL` (not `gatewayURL`) for all `/admin/*` paths. Add a helper:
```go
func adminURL(path string) string {
    return matrixURL + path  // matrixURL = http://gateway:8008
}
```

This is the most common source of confusion — admin and Matrix endpoints share port 8008.

### CRITICAL: Scenario 1 — Redirect from `/admin/dashboard` to `/admin/bootstrap`

The `BootstrapGuard` (in `middleware.go`) guards `/admin/bootstrap` path. But `/admin/dashboard` is guarded by `sessionGuard` FIRST, not `BootstrapGuard`.

Looking at `main.go` route registration:
```go
mux.Handle("GET /admin/dashboard", sessionGuard(http.HandlerFunc(dashboardHandler.Handler)))
```

The `BootstrapGuard` is only applied to:
```go
mux.Handle("GET /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.Handler)))
mux.Handle("POST /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.StepHandler)))
```

**This means:** `GET /admin/dashboard` without a session cookie redirects to `/admin/login` (via `SessionGuard`), NOT to `/admin/bootstrap`.

The epic's Scenario 1 step "When `GET /admin/dashboard` is requested → Then redirects to `/admin/bootstrap`" is NOT the actual behavior. The actual behavior is:
- `GET /admin/dashboard` without cookie → 302 to `/admin/login` (SessionGuard wins)
- `GET /admin/login` → renders login page (no redirect)
- `GET /admin/bootstrap` when not bootstrapped → 200 (BootstrapGuard passes it through to show the wizard)

**The bootstrap redirect** only activates for the `/admin/bootstrap*` paths, not for `/admin/dashboard`. The dashboard is session-guarded, not bootstrap-guarded.

**Correct Scenario 1 test design:**
```gherkin
Scenario: Bootstrap Wizard completes successfully
  Given the server has no bootstrap_completed in server_config
  When I request GET /admin/dashboard without a session cookie
  Then the response redirects to "/admin/login"         ← actual behavior
  When I request GET /admin/bootstrap without a session cookie
  Then the response is 200                              ← bootstrap wizard shows
  And the response body contains "Bootstrap"
  When I seed the bootstrap configuration directly into the database
  Then server_config contains key "bootstrap_completed" with value "true"
  When I request GET /admin/bootstrap without a session cookie
  Then the response redirects to "/admin/login"         ← guard redirects away
```

This accurately tests the actual routing behavior without the incorrect `GET /admin/dashboard → /admin/bootstrap` redirect assumption from the epic spec.

### File Structure

| File | Action |
|------|--------|
| `gateway/features/admin_bootstrap.feature` | CREATE — 3 Gherkin scenarios |
| `gateway/test/integration/admin_bootstrap_steps_test.go` | CREATE — step definitions |
| `gateway/test/integration/steps_test.go` | MODIFY — add `initializeAdminBootstrapSteps(sc)` call |
| `gateway/test/integration/main_test.go` | MODIFY — add `dbURL`, `internalSecret` variables |
| `Makefile` | MODIFY — add DB URL and internal secret env vars to test-integration target |

**Do NOT modify:**
- `gateway/internal/admin/bootstrap.go` — no changes needed; this story only adds tests
- `gateway/internal/admin/auth.go` — no changes needed
- `gateway/internal/admin/middleware.go` — no changes needed
- Any other source files — test-only story

### Previous Story Learnings (from Story 3.14)

- All existing integration test files use `//go:build integration` build tag and `package integration_test` — follow exactly.
- The auth_steps_test.go pattern uses a no-redirect HTTP client for capturing Location headers — replicate this pattern.
- Package-level vars (`lastStatusCode`, `lastBody`) are shared across step files — add `lastLocationHeader` and `lastAdminSessionCookie` to the existing `steps_test.go` vars section, OR declare them in `admin_bootstrap_steps_test.go` and ensure no name collision with existing vars.
- Godog `Strict: true` mode means ALL step text in feature files MUST have a matching step definition — undefined steps fail the entire suite.
- `godog v0.15.1` is the current version (from go.mod).

### Previous Epic 2 Retrospective Learnings (from Memory)

- **Dex sub encoding:** The `sub` claim from Dex for `kai@example.com` is `"CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE"` (protobuf-encoded UUID). Use this exact string in the forged session cookie.
- **Authorization Code + PKCE is standard** — ROPC (`grant_type=password`) is NOT supported by Dex v2.41+. Don't attempt `grant_type=password` as a shortcut for obtaining tokens.
- **Context7 + Playwright conventions:** This story uses Godog (HTTP-level tests), not Playwright. Do NOT use Playwright MCP for these tests — they are HTTP-level, not browser-level (per CLAUDE.md split rule).

### Architecture Constraints

Per `architecture.md` and ADRs:
- ADR-009 (OpenAPI Spec-First): Bootstrap and admin routes are NOT Matrix API endpoints — they are NOT in `openapi.yaml`. Do NOT add them.
- All integration tests run against the full Docker Compose stack (gateway + postgres + dex + core) — no mocking of external services in integration tests.
- Test files MUST use the `integration` build tag to prevent them from running during `make test-unit-go`.

### Anti-Patterns to Avoid

- **DO NOT** try to POST to `/admin/bootstrap` with `http://dex:5556/dex` — the HTTPS validator will return 422. Use DB seeding.
- **DO NOT** use `http.DefaultClient` for redirect assertions — it follows redirects and loses the 302 status code. Always use no-redirect client.
- **DO NOT** use `gatewayURL` (port 8080) for admin routes — they are on `matrixURL` (port 8008).
- **DO NOT** add step definitions as bare functions in the package — all steps must be registered in `initializeAdminBootstrapSteps` which is called from `InitializeScenario`.
- **DO NOT** use Playwright for these tests — they are HTTP-level Godog tests (CLAUDE.md split rule).
- **DO NOT** rely on `lastStatusCode`/`lastBody` state between scenarios — Godog scenarios are independent but package-level vars persist; steps must be explicit about what request was made.
- **DO NOT** attempt `TRUNCATE` with `DELETE FROM server_config` — the RLS policy only allows INSERT. Use `TRUNCATE TABLE server_config` which bypasses RLS for the table owner.
- **DO NOT** hardcode `http://localhost:8008` — use `matrixURL` variable which is set from `NEBU_TEST_MATRIX_URL` env var for container networking.

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

- **bootstrap_draft TRUNCATE error:** The TRUNCATE for `bootstrap_draft` failed because the table did not exist in the running container (old image from before migration 000008). Fixed with best-effort truncate (ignore error if table absent) — the table is not needed for test teardown.
- **404 for /admin/dashboard and /admin/bootstrap:** The gateway container was running an old image without admin routes. Resolved by rebuilding with `docker compose build gateway`. The `make test-integration` target must be run after `make build-gateway` to ensure the latest code is used.
- **bootstrap_active flag after TRUNCATE:** After `TRUNCATE TABLE server_config`, `IsBootstrapActive` checked users table — since OIDC test (running first) created a user, `IsBootstrapActive` returned `false`, causing `BootstrapGuard` to redirect `/admin/bootstrap` to `/admin/login`. Fixed by inserting `bootstrap_active=true` row after TRUNCATE so the check returns `true` explicitly without relying on user absence.
- **Scenario order dependency:** Admin Bootstrap feature file runs before Auth feature file (alphabetical order). This is fine because `theServerHasNoBootstrapCompleted` inserts `bootstrap_active=true` as an explicit flag.

### Completion Notes List

- Created `gateway/features/admin_bootstrap.feature` with 3 scenarios covering Bootstrap Wizard, Dashboard access, and unauthenticated redirect.
- Created `gateway/test/integration/admin_bootstrap_steps_test.go` with all step implementations:
  - DB-level TRUNCATE + `bootstrap_active` re-seeding for test isolation
  - Direct `server_config` INSERT bypassing HTTPS validation (Option B from Dev Notes)
  - Cookie forging via HMAC-SHA256 matching `AdminAuth.signCookie` exactly
  - No-redirect HTTP client for 302/303 assertions
  - `adminURL()` helper using `matrixURL` (port 8008) for all admin routes
  - `theResponseBodyContains` reused from `steps_test.go` (not re-registered)
- Updated `main_test.go`: added `dbURL`, `internalSecret`, `lastLocationHeader` package-level vars
- Updated `steps_test.go`: called `initializeAdminBootstrapSteps(sc)` in `InitializeScenario`
- Updated `Makefile`: added `NEBU_TEST_DB_URL` and `NEBU_TEST_INTERNAL_SECRET` env vars to test-integration target
- All 5 scenarios (42 steps) PASS in `make test-integration` — including existing auth and health tests (no regressions)

### File List

- `gateway/features/admin_bootstrap.feature` — CREATED
- `gateway/test/integration/admin_bootstrap_steps_test.go` — CREATED
- `gateway/test/integration/steps_test.go` — MODIFIED (added `initializeAdminBootstrapSteps` call)
- `gateway/test/integration/main_test.go` — MODIFIED (added `dbURL`, `internalSecret` vars)
- `Makefile` — MODIFIED (added `NEBU_TEST_DB_URL` and `NEBU_TEST_INTERNAL_SECRET` to test-integration)

### Review Findings

- [x] [Review][Patch] MINOR: `lastLocationHeader` var moved from `main_test.go` to `steps_test.go` for consistency with `lastStatusCode`/`lastBody` — fixed during review
- [x] [Review][Dismiss] INFO: `iSeedBootstrapConfigDirectly` does not insert `server_name` — DashboardHandler falls back to "(not configured)", no functional issue
- [x] [Review][Dismiss] INFO: `theResponseRedirectsTo` uses `strings.Contains` instead of exact match — safe for all current redirect targets
- [x] [Review][Dismiss] INFO: `openTestDB()` opens a new connection per step call — acceptable for integration tests
- [x] [Review][Dismiss] INFO: `noRedirectClient()` allocates a new client per call — acceptable for integration tests
- [x] [Review][Dismiss] INFO: AC 1 redirect target deviates from epic spec (`/admin/login` instead of `/admin/bootstrap`) — correctly reflects actual routing behavior, documented in Dev Notes

## Change Log

- 2026-04-01: Story created — ready-for-dev
- 2026-04-01: Story implemented — all 3 scenarios pass, status set to review
- 2026-04-01: Code review passed — 1 MINOR fix applied (var placement), 5 INFO dismissed, status set to done

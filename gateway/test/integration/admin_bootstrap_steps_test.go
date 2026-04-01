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

	"github.com/cucumber/godog"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// lastAdminSessionCookie holds the most recently forged admin session cookie value.
var lastAdminSessionCookie string

// adminSessionCookiePayload mirrors the admin.adminSessionCookie struct exactly.
// JSON tags must match: sub, email, role, exp.
type adminSessionCookiePayload struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Exp   int64  `json:"exp"`
}

// openTestDB opens a *sql.DB connection to the test PostgreSQL instance using dbURL.
func openTestDB() (*sql.DB, error) {
	return sql.Open("pgx", dbURL)
}

// signTestCookie mirrors AdminAuth.signCookie: base64url(payload) + "." + base64url(HMAC-SHA256(secret, base64url(payload))).
func signTestCookie(secret []byte, payload []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig
}

// adminURL returns the full URL for an admin-panel path (port 8008 = matrixURL).
func adminURL(path string) string {
	return matrixURL + path
}

// noRedirectClient returns an http.Client that does NOT follow redirects.
// Required for asserting 302/303 redirect responses.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// theServerHasNoBootstrapCompleted truncates server_config and bootstrap_draft to ensure
// a clean slate (no bootstrap_completed row). Runs as the nebu table owner, which is
// allowed to TRUNCATE even with INSERT-only RLS policies.
func theServerHasNoBootstrapCompleted() error {
	db, err := openTestDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("TRUNCATE TABLE server_config"); err != nil {
		return fmt.Errorf("truncate server_config: %w", err)
	}
	// bootstrap_draft may or may not exist depending on applied migrations — best-effort truncate.
	_, _ = db.Exec("TRUNCATE TABLE bootstrap_draft")

	// Re-insert bootstrap_active so IsBootstrapActive returns true regardless of whether
	// users exist in the DB (users from other integration tests would otherwise suppress bootstrap mode).
	_, err = db.Exec(
		"INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)",
		"bootstrap_active", "true", time.Now().UnixMilli(),
	)
	return err
}

// iRequestGETWithoutCookie makes a no-redirect GET to adminURL(path) without any cookie.
// Stores status, body, and Location header in package-level vars.
func iRequestGETWithoutCookie(path string) error {
	client := noRedirectClient()
	resp, err := client.Get(adminURL(path))
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

// theResponseRedirectsTo asserts the last response was a 302 or 303 redirect
// with a Location header containing the target path.
func theResponseRedirectsTo(target string) error {
	if lastStatusCode != http.StatusFound && lastStatusCode != http.StatusSeeOther {
		return fmt.Errorf("expected redirect (302/303), got %d (body: %.200s)", lastStatusCode, lastBody)
	}
	if !strings.Contains(lastLocationHeader, target) {
		return fmt.Errorf("expected redirect to %q, got Location: %q", target, lastLocationHeader)
	}
	return nil
}

// iSeedBootstrapConfigDirectly inserts bootstrap configuration rows directly into
// server_config via raw SQL. This bypasses the HTTPS validation in the bootstrap API
// (which rejects http:// OIDC issuers) and is the correct approach for integration tests
// against a dev stack where Dex runs on HTTP.
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
		// Unencrypted secret — tests do not exercise CallbackHandler, only SessionGuard
		{"oidc_client_secret", "nebu-admin-secret"},
		{"bootstrap_completed", "true"},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			"INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)",
			r.key, r.value, now,
		); err != nil {
			return fmt.Errorf("insert server_config[%s]: %w", r.key, err)
		}
	}
	return nil
}

// serverConfigContainsKeyValue queries server_config for the given key and asserts the value matches.
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

// bootstrapIsCompleteAndSeeded is the Given step for Scenario 2 and 3.
// It resets server_config (TRUNCATE) and then seeds it with bootstrap_completed=true
// and valid OIDC config rows.
func bootstrapIsCompleteAndSeeded() error {
	if err := theServerHasNoBootstrapCompleted(); err != nil {
		return err
	}
	return iSeedBootstrapConfigDirectly()
}

// iHaveAForgedValidAdminSessionCookie creates a signed admin_session cookie using
// the same HMAC-SHA256 algorithm as AdminAuth.signCookie. This allows testing
// SessionGuard without requiring a full PKCE Authorization Code flow.
//
// The sub claim uses the Dex-encoded subject for kai@example.com:
// "CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE" (protobuf-encoded UUID).
func iHaveAForgedValidAdminSessionCookie() error {
	payload, err := json.Marshal(adminSessionCookiePayload{
		Sub:   "CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE",
		Email: "kai@example.com",
		Role:  "instance_admin",
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	})
	if err != nil {
		return fmt.Errorf("marshal session payload: %w", err)
	}
	lastAdminSessionCookie = signTestCookie([]byte(strings.TrimSpace(internalSecret)), payload)
	return nil
}

// iRequestGETAdminDashboardWithCookie makes a no-redirect GET to /admin/dashboard
// with the admin_session cookie set to lastAdminSessionCookie.
func iRequestGETAdminDashboardWithCookie() error {
	client := noRedirectClient()
	req, err := http.NewRequest(http.MethodGet, adminURL("/admin/dashboard"), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})
	resp, err := client.Do(req)
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

// theResponseIs200 asserts the last HTTP response had status 200.
func theResponseIs200() error {
	if lastStatusCode != http.StatusOK {
		return fmt.Errorf("expected 200, got %d (body: %.400s)", lastStatusCode, lastBody)
	}
	return nil
}

// initializeAdminBootstrapSteps registers all step definitions for the admin bootstrap
// and dashboard flow scenarios.
// Note: "the response body contains" is registered in steps_test.go and reused here.
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

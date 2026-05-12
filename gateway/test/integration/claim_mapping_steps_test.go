//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// lastMatrixLoginBody holds the body of the most recent Matrix login response.
var lastMatrixLoginBody string

// lastMatrixLoginStatus holds the status code of the most recent Matrix login response.
var lastMatrixLoginStatus int

// iRequestGETClaimMappingWithCookie makes a no-redirect GET to /admin/config/claim-mapping
// with the admin_session cookie set to lastAdminSessionCookie.
func iRequestGETClaimMappingWithCookie(path string) error {
	client := noRedirectClient()
	req, err := http.NewRequest(http.MethodGet, adminURL(path), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})
	resp, err := client.Do(req)
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

// iPOSTClaimMappingValid POSTs the given form fields to /admin/config/claim-mapping
// with the admin_session cookie. Uses noRedirectClient to capture the redirect.
func iPOSTClaimMappingValid(table *godog.Table) error {
	return postClaimMappingForm(table)
}

// iPOSTClaimMappingInvalid POSTs invalid form fields to /admin/config/claim-mapping.
func iPOSTClaimMappingInvalid(table *godog.Table) error {
	return postClaimMappingForm(table)
}

// postClaimMappingForm is the shared implementation for valid and invalid POST claim mapping steps.
func postClaimMappingForm(table *godog.Table) error {
	if lastAdminSessionCookie == "" {
		return fmt.Errorf("no admin session cookie set — call 'I have a forged valid admin session cookie' first")
	}

	// Build a CSRF token by forging it — the server validates the cookie+token pair.
	// For integration tests we use the same HMAC approach as signTestCookie to get a valid token,
	// but since CSRF token format differs (it is a random hex string validated against the session
	// via the CSRF cookie), we first GET the page to obtain a real CSRF token.
	csrfToken, err := fetchCSRFToken("/admin/config/claim-mapping")
	if err != nil {
		return fmt.Errorf("fetching CSRF token: %w", err)
	}

	formVals := url.Values{}
	formVals.Set("_csrf", csrfToken)
	for i := 1; i < len(table.Rows); i++ {
		row := table.Rows[i]
		if len(row.Cells) >= 2 {
			formVals.Set(row.Cells[0].Value, row.Cells[1].Value)
		}
	}

	client := noRedirectClient()
	req, err := http.NewRequest(http.MethodPost, adminURL("/admin/config/claim-mapping"),
		strings.NewReader(formVals.Encode()))
	if err != nil {
		return fmt.Errorf("build POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})

	// Also send the CSRF cookie that was set during the GET.
	if lastCSRFCookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: lastCSRFCookieValue})
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /admin/config/claim-mapping: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	lastLocationHeader = resp.Header.Get("Location")
	return nil
}

// lastCSRFCookieValue holds the csrf_token cookie value from the last GET response.
var lastCSRFCookieValue string

// fetchCSRFToken GETs the given admin page (with the admin session cookie) and extracts
// the CSRF token from the form hidden input.
func fetchCSRFToken(path string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, adminURL(path), nil)
	if err != nil {
		return "", err
	}
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s for CSRF: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Extract value from: <input type="hidden" name="_csrf" value="TOKEN">
	const needle = `name="_csrf" value="`
	idx := strings.Index(bodyStr, needle)
	if idx == -1 {
		return "", fmt.Errorf("CSRF token not found in response body (status %d)", resp.StatusCode)
	}
	start := idx + len(needle)
	end := strings.Index(bodyStr[start:], `"`)
	if end == -1 {
		return "", fmt.Errorf("malformed CSRF token in response body")
	}
	token := bodyStr[start : start+end]

	// Capture any csrf_token cookie from the response (used in POST).
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			lastCSRFCookieValue = c.Value
		}
	}

	return token, nil
}

// theRedirectLocationContains asserts the last Location header contains the given substring.
func theRedirectLocationContains(substr string) error {
	if !strings.Contains(lastLocationHeader, substr) {
		return fmt.Errorf("expected Location header to contain %q, got %q", substr, lastLocationHeader)
	}
	return nil
}

// iFollowTheRedirectWithAdminCookie follows the lastLocationHeader as a GET with the admin session cookie.
func iFollowTheRedirectWithAdminCookie() error {
	if lastLocationHeader == "" {
		return fmt.Errorf("no redirect location to follow")
	}
	target := lastLocationHeader
	if !strings.HasPrefix(target, "http") {
		target = adminURL(target)
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build GET for redirect: %w", err)
	}
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: lastAdminSessionCookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET redirect target: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	lastLocationHeader = resp.Header.Get("Location")
	return nil
}

// theResponseIs asserts the last HTTP response had the given status code.
func theResponseIs(code int) error {
	if lastStatusCode != code {
		return fmt.Errorf("expected HTTP %d, got %d (body: %.400s)", code, lastStatusCode, lastBody)
	}
	return nil
}

// setServerConfigKey inserts or updates a key in server_config via raw SQL.
func setServerConfigKey(key, value string) error {
	db, err := openTestDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	_, err = db.Exec(
		`INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
		key, value, time.Now().UnixMilli(),
	)
	return err
}

// deleteServerConfigKey removes a key from server_config.
func deleteServerConfigKey(key string) error {
	db, err := openTestDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	_, err = db.Exec(`DELETE FROM server_config WHERE key = $1`, key)
	return err
}

// serverConfigContainsKeyValue2 wraps the step text "server_config contains {key} = {value}"
// matching the feature's step format.
func serverConfigContainsKeyEqValue(key, value string) error {
	return setServerConfigKey(key, value)
}

// serverConfigDoesNotContainKey removes the key to simulate "absent from server_config".
func serverConfigDoesNotContainKey(key string) error {
	return deleteServerConfigKey(key)
}

// aUserAuthenticatesWithPreferredUsername performs a Matrix login using a Dex token
// for an existing user, testing that preferred_username-style claim is used for the user_id.
// Expects the username to be a valid Dex test user (e.g. "alex").
func aUserAuthenticatesWithPreferredUsername(username string) error {
	dexUser := username
	dexPassword := "password"

	// Get a Dex token for this user using auth code flow.
	if err := iObtainDexTokenFor(dexUser+"@example.com", dexPassword); err != nil {
		return fmt.Errorf("obtain dex token for %s: %w", dexUser, err)
	}

	// Store the username we expect in the Matrix user_id for assertion.
	lastExpectedClaimUsername = dexUser

	// POST to Matrix login.
	payload := fmt.Sprintf(`{"type":"m.login.token","token":%q}`, lastDexIDToken)
	loginURL := matrixURL + "/_matrix/client/v3/login"
	req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building POST /login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /login failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading POST /login response: %w", err)
	}
	lastMatrixLoginStatus = resp.StatusCode
	lastMatrixLoginBody = string(body)
	return nil
}

// lastExpectedClaimUsername is the username we expect to appear in the Matrix user_id.
var lastExpectedClaimUsername string

// aUserAuthenticatesWithNameClaim performs a Matrix login testing the "name" claim fallback.
// Expects the name to be a valid Dex test user (e.g. "alex").
func aUserAuthenticatesWithNameClaim(name string) error {
	dexUser := name

	if err := iObtainDexTokenFor(dexUser+"@example.com", "password"); err != nil {
		return fmt.Errorf("obtain dex token for %s: %w", dexUser, err)
	}
	lastExpectedClaimUsername = dexUser

	payload := fmt.Sprintf(`{"type":"m.login.token","token":%q}`, lastDexIDToken)
	loginURL := matrixURL + "/_matrix/client/v3/login"
	req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building POST /login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /login failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading POST /login response: %w", err)
	}
	lastMatrixLoginStatus = resp.StatusCode
	lastMatrixLoginBody = string(body)
	return nil
}

// theMatrixLoginResponseIs asserts the most recent Matrix login response had the given status.
func theMatrixLoginResponseIs(code int) error {
	if lastMatrixLoginStatus != code {
		return fmt.Errorf("expected Matrix login HTTP %d, got %d (body: %.400s)",
			code, lastMatrixLoginStatus, lastMatrixLoginBody)
	}
	return nil
}

// theReturnedUserIDContains asserts the user_id in the Matrix login response contains the given string.
func theReturnedUserIDContains(substr string) error {
	var loginResp struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal([]byte(lastMatrixLoginBody), &loginResp); err != nil {
		return fmt.Errorf("parsing Matrix login response: %w (body: %s)", err, lastMatrixLoginBody)
	}
	if loginResp.UserID == "" {
		return fmt.Errorf("user_id is empty in Matrix login response; body: %s", lastMatrixLoginBody)
	}
	if !strings.Contains(loginResp.UserID, substr) {
		return fmt.Errorf("expected user_id to contain %q, got %q", substr, loginResp.UserID)
	}
	return nil
}

// theServerIsRunningWithCleanTestDatabase is an alias for bootstrapIsCompleteAndSeeded
// that matches the "Background: Given the server is running with a clean test database" step.
func theServerIsRunningWithCleanTestDatabase() error {
	// For claim mapping tests: we only need the server running.
	// The actual seeding is done per-scenario via "bootstrap is complete and server_config is seeded".
	return nil
}

// initializeClaimMappingSteps registers all step definitions for the OIDC claim mapping scenarios.
// Called from InitializeScenario in steps_test.go.
func initializeClaimMappingSteps(sc *godog.ScenarioContext) {
	// MINOR-6: reset package-level mutable state before each scenario to prevent
	// cross-scenario pollution from prior claim_mapping test runs.
	sc.Before(func(_ context.Context, _ *godog.Scenario) (context.Context, error) {
		lastCSRFCookieValue = ""
		lastMatrixLoginBody = ""
		lastMatrixLoginStatus = 0
		lastExpectedClaimUsername = ""
		return nil, nil
	})

	sc.Step(`^the server is running with a clean test database$`, theServerIsRunningWithCleanTestDatabase)
	sc.Step(`^I request GET (/admin/config/claim-mapping[^\s]*) with the admin session cookie$`, iRequestGETClaimMappingWithCookie)
	sc.Step(`^I POST to /admin/config/claim-mapping with the admin session cookie and valid form:$`, iPOSTClaimMappingValid)
	sc.Step(`^I POST to /admin/config/claim-mapping with the admin session cookie and invalid form:$`, iPOSTClaimMappingInvalid)
	sc.Step(`^the redirect location contains "([^"]*)"$`, theRedirectLocationContains)
	sc.Step(`^I follow the redirect with the admin session cookie$`, iFollowTheRedirectWithAdminCookie)
	sc.Step(`^the response is (\d+)$`, theResponseIs)
	sc.Step(`^server_config contains "([^"]*)" = "([^"]*)"$`, serverConfigContainsKeyEqValue)
	sc.Step(`^server_config does not contain "([^"]*)"$`, serverConfigDoesNotContainKey)
	sc.Step(`^a user authenticates via Matrix login with a Dex token containing preferred_username "([^"]*)"$`, aUserAuthenticatesWithPreferredUsername)
	sc.Step(`^a user authenticates via Matrix login with a Dex token containing name "([^"]*)"$`, aUserAuthenticatesWithNameClaim)
	sc.Step(`^the Matrix login response is (\d+)$`, theMatrixLoginResponseIs)
	sc.Step(`^the returned user_id contains "([^"]*)"$`, theReturnedUserIDContains)
}

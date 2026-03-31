//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
)

// lastDexIDToken holds the id_token obtained from Dex authorization code flow.
var lastDexIDToken string

// lastAccessToken holds the access_token returned by POST /login.
var lastAccessToken string

// iObtainDexTokenFor obtains a real JWT from Dex using the authorization code flow.
// It follows Dex's browser-based auth redirects programmatically, posts credentials,
// captures the auth code from the callback redirect, and exchanges it for tokens.
func iObtainDexTokenFor(username, password string) error {
	// The redirect_uri must match a registered URI in Dex staticClients config.
	redirectURI := "http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc"

	// Step 1: Follow all redirects to reach the Dex login form page.
	// The chain is: /dex/auth → /dex/auth/local → /dex/auth/local/login (HTML form).
	formFollowClient := &http.Client{} // default: follows up to 10 redirects

	authURL := dexURL + "/dex/auth?" + url.Values{
		"response_type": {"code"},
		"client_id":     {"nebu-gateway"},
		"redirect_uri":  {redirectURI},
		"scope":         {"openid profile email groups"},
		"state":         {"teststate"},
	}.Encode()

	resp, err := formFollowClient.Get(authURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("dex auth flow start failed: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("reading dex login page: %w", err)
	}

	// Step 2: Extract form action URL from HTML.
	// Dex renders: <form method="post" action="/dex/auth/local/login?back=&amp;state=<internal>">
	bodyStr := string(body)
	actionIdx := strings.Index(bodyStr, `action="`)
	if actionIdx == -1 {
		return fmt.Errorf("no form action found in dex login page (status %d)", resp.StatusCode)
	}
	actionStart := actionIdx + len(`action="`)
	actionEnd := strings.Index(bodyStr[actionStart:], `"`)
	if actionEnd == -1 {
		return fmt.Errorf("malformed form action in dex login page")
	}
	formAction := bodyStr[actionStart : actionStart+actionEnd]
	formAction = strings.ReplaceAll(formAction, "&amp;", "&") // HTML-unescape
	if !strings.HasPrefix(formAction, "http") {
		formAction = dexURL + formAction
	}

	// Step 3: POST credentials to the form action.
	// Use a client that does NOT follow redirects — we capture the auth code
	// from the Location header of the callback redirect.
	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	loginForm := url.Values{
		"login":    {username},
		"password": {password},
	}
	req, err := http.NewRequest(http.MethodPost, formAction, strings.NewReader(loginForm.Encode()))
	if err != nil {
		return fmt.Errorf("building login form request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = noRedirect.Do(req)
	if err != nil {
		return fmt.Errorf("posting credentials to dex: %w", err)
	}
	resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("no Location header after credential POST (status %d) — credentials may be wrong", resp.StatusCode)
	}

	// Step 4: Extract auth code from the callback redirect Location.
	locURL, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("parsing callback location %q: %w", location, err)
	}
	authCode := locURL.Query().Get("code")
	if authCode == "" {
		return fmt.Errorf("no auth code in callback redirect: %s", location)
	}

	// Step 5: Exchange auth code for tokens at /dex/token.
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"redirect_uri":  {redirectURI},
		"client_id":     {"nebu-gateway"},
		"client_secret": {"nebu-dev-secret"},
	}
	tokenResp, err := http.PostForm(dexURL+"/dex/token", tokenForm) //nolint:noctx
	if err != nil {
		return fmt.Errorf("dex token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()
	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return fmt.Errorf("reading dex token response: %w", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		return fmt.Errorf("dex token endpoint returned %d: %s", tokenResp.StatusCode, string(tokenBody))
	}

	var tr struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(tokenBody, &tr); err != nil {
		return fmt.Errorf("parsing dex token response: %w", err)
	}
	if tr.IDToken == "" {
		return fmt.Errorf("dex returned empty id_token; body: %s", string(tokenBody))
	}
	lastDexIDToken = tr.IDToken
	return nil
}

// iCallGETOnMatrix makes a GET request to matrixURL+path and stores the response.
// Used for Matrix Client-Server API endpoints on port 8008.
func iCallGETOnMatrix(path string) error {
	reqURL := matrixURL + path
	resp, err := http.Get(reqURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", reqURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", reqURL, err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// iPostLoginWithDexToken POSTs the Dex id_token to the Matrix login endpoint.
func iPostLoginWithDexToken() error {
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
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	if resp.StatusCode == http.StatusOK {
		var loginResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(body, &loginResp); err == nil {
			lastAccessToken = loginResp.AccessToken
		}
	}
	return nil
}

// iPostLogoutWithAccessToken POSTs to the Matrix logout endpoint using lastAccessToken.
func iPostLogoutWithAccessToken() error {
	logoutURL := matrixURL + "/_matrix/client/v3/logout"
	req, err := http.NewRequest(http.MethodPost, logoutURL, nil)
	if err != nil {
		return fmt.Errorf("building POST /logout request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+lastAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /logout failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading POST /logout response: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// initializeAuthSteps registers all auth scenario step definitions.
func initializeAuthSteps(sc *godog.ScenarioContext) {
	sc.Step(`^I call GET (/_matrix\S+) on the matrix API$`, iCallGETOnMatrix)
	sc.Step(`^I obtain a Dex token for "([^"]*)" with password "([^"]*)"$`, iObtainDexTokenFor)
	sc.Step(`^I POST /_matrix/client/v3/login with the Dex token$`, iPostLoginWithDexToken)
	sc.Step(`^I POST /_matrix/client/v3/logout with the access token$`, iPostLogoutWithAccessToken)
}

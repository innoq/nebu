package admin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/nebu/nebu/internal/auth"
)

// setupAdminOIDCServerWithRole returns a running httptest server whose /token endpoint
// returns a JWT with the given claimValue for "nebu_role", plus the RSA private key.
func setupAdminOIDCServerWithRole(t *testing.T, role string) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-1",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	var serverURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idToken := signAdminJWT(t, serverURL, privateKey, time.Now().Add(time.Hour), "nebu_role", role)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
		})
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, privateKey
}

// buildValidStateCookie creates a signed admin_oidc_state cookie value for use in tests.
func buildValidStateCookie(t *testing.T, a *AdminAuth, state string) string {
	t.Helper()
	sc := oidcStateCookie{
		State:    state,
		Verifier: "someverifier",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("json.Marshal oidcStateCookie: %v", err)
	}
	return a.signCookie(payload)
}

// TestCallbackHandler_StateMismatch_Returns400 verifies CSRF protection:
// when the state param in the request does not match the cookie state, the handler returns 400.
func TestCallbackHandler_StateMismatch_Returns400(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{issuer: srv.URL, clientID: "test-client-id", clientSecret: "test-client-secret"}

	cookieValue := buildValidStateCookie(t, a, "expected")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=wrong", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// TestCallbackHandler_RoleCheckFails_Returns403 verifies that a user with nebu_role="user"
// is rejected with 403, even when the OIDC flow is otherwise valid.
func TestCallbackHandler_RoleCheckFails_Returns403(t *testing.T) {
	srv, _ := setupAdminOIDCServerWithRole(t, "user")
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{issuer: srv.URL, clientID: "test-client-id", clientSecret: "test-client-secret"}

	cookieValue := buildValidStateCookie(t, a, "mystate")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Access denied") {
		t.Errorf("expected 'Access denied' in body, got %q", rr.Body.String())
	}
}

// TestCallbackHandler_ValidFlow_SetsSessionCookieAndRedirects verifies the happy path:
// valid OIDC flow with instance_admin role → 303, admin_session cookie set correctly,
// admin_oidc_state cookie deleted, Location = /admin/dashboard.
func TestCallbackHandler_ValidFlow_SetsSessionCookieAndRedirects(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t) // returns instance_admin role
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{issuer: srv.URL, clientID: "test-client-id", clientSecret: "test-client-secret"}

	cookieValue := buildValidStateCookie(t, a, "mystate")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if location != "/admin/dashboard" {
		t.Errorf("expected Location=/admin/dashboard, got %q", location)
	}

	var sessionCookie *http.Cookie
	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		switch c.Name {
		case "admin_session":
			sessionCookie = c
		case "admin_oidc_state":
			stateCookie = c
		}
	}

	if sessionCookie == nil {
		t.Fatal("admin_session cookie not set")
	}
	if !sessionCookie.HttpOnly {
		t.Error("expected admin_session to be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", sessionCookie.SameSite)
	}
	if sessionCookie.MaxAge != 28800 {
		t.Errorf("expected MaxAge=28800, got %d", sessionCookie.MaxAge)
	}
	if sessionCookie.Path != "/admin" {
		t.Errorf("expected Path=/admin, got %q", sessionCookie.Path)
	}

	if stateCookie == nil {
		t.Fatal("admin_oidc_state deletion cookie not set")
	}
	if stateCookie.MaxAge > 0 {
		t.Errorf("expected admin_oidc_state MaxAge <= 0 (deletion), got %d", stateCookie.MaxAge)
	}
}

// TestCallbackHandler_ValidFlow_SessionCookieSecureTLS verifies that the admin_session cookie
// has Secure=true when the request has TLS set.
func TestCallbackHandler_ValidFlow_SessionCookieSecureTLS(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{issuer: srv.URL, clientID: "test-client-id", clientSecret: "test-client-secret"}

	cookieValue := buildValidStateCookie(t, a, "mystate")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.TLS = &tls.ConnectionState{}
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}

	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("admin_session cookie not set")
	}
	if !sessionCookie.Secure {
		t.Error("expected admin_session cookie to have Secure=true when r.TLS != nil")
	}
}

// TestLogoutHandler_DeletesCookieAndRedirects verifies that GET /admin/logout
// deletes the admin_session cookie (MaxAge <= 0) and redirects 303 to /admin/login.
func TestLogoutHandler_DeletesCookieAndRedirects(t *testing.T) {
	a := newTestAdminAuth(t, nil)

	req := httptest.NewRequest("GET", "/admin/logout", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LogoutHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if location != "/admin/login" {
		t.Errorf("expected Location=/admin/login, got %q", location)
	}

	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("admin_session cookie not set in response (expected deletion cookie)")
	}
	if sessionCookie.MaxAge > 0 {
		t.Errorf("expected admin_session MaxAge <= 0 (deletion), got %d", sessionCookie.MaxAge)
	}
	if !sessionCookie.HttpOnly {
		t.Error("expected admin_session deletion cookie to be HttpOnly")
	}
	if sessionCookie.Path != "/admin" {
		t.Errorf("expected deletion cookie Path=/admin, got %q", sessionCookie.Path)
	}
}

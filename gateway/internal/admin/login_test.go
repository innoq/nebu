package admin

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServerConfigReader is a test double for ServerConfigReader.
type fakeServerConfigReader struct {
	issuer       string
	clientID     string
	clientSecret string
	err          error
	bootstrapErr error
}

func (f *fakeServerConfigReader) LoadOIDCConfig(_ context.Context) (string, string, string, error) {
	return f.issuer, f.clientID, f.clientSecret, f.err
}

func (f *fakeServerConfigReader) CompleteBootstrap(_ context.Context) error {
	return f.bootstrapErr
}

func (f *fakeServerConfigReader) LoadAdminGroupClaim(_ context.Context) (string, error) {
	return "instance_admin", nil
}

func (f *fakeServerConfigReader) SaveAdminGroupClaim(_ context.Context, _ string) error {
	return nil
}

// newTestAdminAuthWithReader creates an AdminAuth with a fake ServerConfigReader and optional TemplateHandler.
func newTestAdminAuthWithReader(t *testing.T, reader ServerConfigReader, tmpl *TemplateHandler) *AdminAuth {
	t.Helper()
	a := NewAdminAuth(nil, "test-client-id", "test-client-secret", "nebu_role", []byte("test-secret-key"), nil, tmpl)
	if reader != nil {
		a.configReader = reader
	}
	return a
}

// TestLoginPageHandler_RendersPage verifies GET /admin/login returns 200 with SSO button.
func TestLoginPageHandler_RendersPage(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	a := newTestAdminAuthWithReader(t, nil, tmpl)

	req := httptest.NewRequest("GET", "/admin/login", nil)
	rr := httptest.NewRecorder()
	a.LoginPageHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Login with SSO") {
		t.Error("response body missing 'Login with SSO'")
	}
	if !strings.Contains(body, "/admin/login/start") {
		t.Error("response body missing link to /admin/login/start")
	}
}

// TestLoginPageHandler_WithError verifies GET /admin/login?error=auth_failed returns 200 with error text.
func TestLoginPageHandler_WithError(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	a := newTestAdminAuthWithReader(t, nil, tmpl)

	req := httptest.NewRequest("GET", "/admin/login?error=auth_failed", nil)
	rr := httptest.NewRecorder()
	a.LoginPageHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Authentication failed") {
		t.Errorf("expected error message in body, got: %s", body)
	}
}

// TestLoginStartHandler_SetsStateAndRedirects verifies that /admin/login/start:
//   - redirects (302)
//   - sets admin_oidc_state cookie with HttpOnly=true
//   - Location contains code_challenge_method=S256 and non-empty state
func TestLoginStartHandler_SetsStateAndRedirects(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Check admin_oidc_state cookie is set with HttpOnly
	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("admin_oidc_state cookie not set")
	}
	if !stateCookie.HttpOnly {
		t.Error("expected admin_oidc_state to be HttpOnly")
	}

	// Check Location contains S256 and non-empty state
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "code_challenge_method=S256") {
		t.Errorf("Location missing code_challenge_method=S256: %s", location)
	}
	if !strings.Contains(location, "state=") {
		t.Error("Location missing state parameter")
	}
	idx := strings.Index(location, "state=")
	if idx >= 0 {
		stateVal := strings.SplitN(location[idx+6:], "&", 2)[0]
		if stateVal == "" {
			t.Error("state parameter in Location is empty")
		}
	}
}

// TestLoginStartHandler_MissingOIDCConfig verifies redirect to bootstrap when OIDC config is missing.
func TestLoginStartHandler_MissingOIDCConfig(t *testing.T) {
	reader := &fakeServerConfigReader{
		err: ErrOIDCConfigMissing,
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if location != "/admin/bootstrap" {
		t.Errorf("expected Location=/admin/bootstrap, got %q", location)
	}
}

// TestLoginStartHandler_CookieSecurity verifies admin_oidc_state cookie security attributes.
func TestLoginStartHandler_CookieSecurity(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("admin_oidc_state cookie not set")
	}
	if !stateCookie.HttpOnly {
		t.Error("expected HttpOnly=true")
	}
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", stateCookie.SameSite)
	}
	if stateCookie.MaxAge != 600 {
		t.Errorf("expected MaxAge=600, got %d", stateCookie.MaxAge)
	}
	// Secure must be false when TLS is nil (httptest default is plain HTTP)
	if stateCookie.Secure {
		t.Error("expected Secure=false for non-TLS request")
	}
}

// TestLoginStartHandler_CookieSecureTLS verifies Secure=true when request arrives over TLS.
func TestLoginStartHandler_CookieSecureTLS(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	req.TLS = &tls.ConnectionState{} // simulate TLS connection
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_oidc_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("admin_oidc_state cookie not set")
	}
	if !stateCookie.Secure {
		t.Error("expected Secure=true for TLS request")
	}
}

// TestLoginStartHandler_NilConfigReader_Returns503 verifies 503 when configReader is nil.
func TestLoginStartHandler_NilConfigReader_Returns503(t *testing.T) {
	a := newTestAdminAuthWithReader(t, nil, nil)
	// configReader is nil because we passed nil reader and nil db

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestLoginStartHandler_DBError_Returns503 verifies 503 on unexpected DB error.
func TestLoginStartHandler_DBError_Returns503(t *testing.T) {
	reader := &fakeServerConfigReader{
		err: errors.New("connection refused"),
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

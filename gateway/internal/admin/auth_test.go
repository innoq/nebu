package admin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/nebu/nebu/internal/auth"
)

// setupAdminOIDCServer returns a running httptest server with discovery, JWKS and token endpoints,
// plus the RSA private key used for signing ID tokens.
func setupAdminOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
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
		idToken := signAdminJWT(t, serverURL, privateKey, time.Now().Add(time.Hour), "nebu_role", "instance_admin")
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

// signAdminJWT creates a signed JWT with configurable role claim.
func signAdminJWT(t *testing.T, issuer string, key *rsa.PrivateKey, expiry time.Time, claimName, claimValue string) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "test-sub-admin",
		Issuer:   issuer,
		Audience: josejwt.Audience{"test-client-id"},
		Expiry:   josejwt.NewNumericDate(expiry),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	extra := map[string]any{
		"email":   "admin@example.com",
		claimName: claimValue,
	}
	raw, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	return raw
}

func newTestAdminAuth(t *testing.T, provider *auth.Provider) *AdminAuth {
	t.Helper()
	return NewAdminAuth(provider, "test-client-id", "test-client-secret", "nebu_role", []byte("test-secret-key"))
}

// TestSignAndVerifyCookie verifies that signCookie/verifyCookie round-trip correctly
// and that a tampered value returns an error.
func TestSignAndVerifyCookie(t *testing.T) {
	a := NewAdminAuth(nil, "", "", "", []byte("my-secret"))

	payload := []byte(`{"state":"abc","verifier":"xyz","exp":9999999999}`)
	signed := a.signCookie(payload)

	got, err := a.verifyCookie(signed)
	if err != nil {
		t.Fatalf("verifyCookie: unexpected error: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("round-trip payload mismatch: got %q, want %q", got, payload)
	}

	// Tamper with the signature
	parts := strings.SplitN(signed, ".", 2)
	tampered := parts[0] + ".invalidsignature"
	_, err = a.verifyCookie(tampered)
	if err == nil {
		t.Error("expected error for tampered cookie, got nil")
	}

	// No dot at all
	_, err = a.verifyCookie("nodothere")
	if err == nil {
		t.Error("expected error for cookie without dot")
	}
}

// TestMapAdminRole verifies the role mapping table via the shared auth.MapSystemRole function.
func TestMapAdminRole(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"instance_admin", "instance_admin"},
		{"compliance_officer", "compliance_officer"},
		{"superadmin", "user"},
		{"", "user"},
		{"random_role", "user"},
	}
	for _, tc := range cases {
		got := auth.MapSystemRole(tc.input)
		if got != tc.want {
			t.Errorf("auth.MapSystemRole(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestAdminLoginHandler_SetsStateAndRedirects verifies the login handler produces a 302
// with the admin_oidc_state cookie (HttpOnly, SameSite=Lax) and a Location with S256.
func TestAdminLoginHandler_SetsStateAndRedirects(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)

	req := httptest.NewRequest("GET", "/admin/auth/login", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}

	// Check cookie is set
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
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", stateCookie.SameSite)
	}

	// Check Location header contains S256 and non-empty state
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "code_challenge_method=S256") {
		t.Errorf("Location missing code_challenge_method=S256: %s", location)
	}
	if !strings.Contains(location, "state=") {
		t.Error("Location missing state parameter")
	}
	// state value must not be empty
	idx := strings.Index(location, "state=")
	if idx >= 0 {
		stateVal := strings.SplitN(location[idx+6:], "&", 2)[0]
		if stateVal == "" {
			t.Error("state parameter in Location is empty")
		}
	}
}

// TestAdminLoginHandler_ProviderUnavailable_Returns503 verifies 503 when provider Inner() is nil.
func TestAdminLoginHandler_ProviderUnavailable_Returns503(t *testing.T) {
	// Port 0 is always closed — provider.Inner() will be nil
	provider := auth.NewProvider(context.Background(), "http://127.0.0.1:0")
	a := newTestAdminAuth(t, provider)

	req := httptest.NewRequest("GET", "/admin/auth/login", nil)
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestAdminCallbackHandler_MissingCookie_Returns400 verifies 400 when no state cookie is present.
func TestAdminCallbackHandler_MissingCookie_Returns400(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)

	req := httptest.NewRequest("GET", "/admin/auth/callback?code=abc&state=xyz", nil)
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// TestAdminCallbackHandler_StateMismatch_Returns400 verifies CSRF protection.
func TestAdminCallbackHandler_StateMismatch_Returns400(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)

	// Create a valid cookie with state="expected"
	sc := oidcStateCookie{
		State:    "expected",
		Verifier: "someverifier",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, _ := json.Marshal(sc)
	cookieValue := a.signCookie(payload)

	req := httptest.NewRequest("GET", "/admin/auth/callback?code=abc&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// TestAdminCallbackHandler_ExpiredCookie_Returns400 verifies expired state cookie is rejected.
func TestAdminCallbackHandler_ExpiredCookie_Returns400(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)

	sc := oidcStateCookie{
		State:    "mystate",
		Verifier: "someverifier",
		Exp:      1, // far in the past
	}
	payload, _ := json.Marshal(sc)
	cookieValue := a.signCookie(payload)

	req := httptest.NewRequest("GET", "/admin/auth/callback?code=abc&state=mystate", nil)
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// TestAdminCallbackHandler_TokenExchangeFailure_Redirects verifies redirect to login on token exchange failure.
func TestAdminCallbackHandler_TokenExchangeFailure_Redirects(t *testing.T) {
	// Build an OIDC server where /token returns 400
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
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)

	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)

	sc := oidcStateCookie{
		State:    "mystate",
		Verifier: "someverifier",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, _ := json.Marshal(sc)
	cookieValue := a.signCookie(payload)

	req := httptest.NewRequest("GET", "/admin/auth/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "/admin/auth/login?error=auth_failed") {
		t.Errorf("expected redirect to /admin/auth/login?error=auth_failed, got %q", location)
	}
}

package admin

// Story 5.16 — OIDC Nonce Verification
//
// These tests verify that:
//   - LoginStartHandler emits a `nonce` parameter in the OIDC AuthCodeURL AND
//     stores a matching nonce value in the signed state cookie.
//   - CallbackHandler rejects ID tokens whose `nonce` claim does not match the
//     state cookie nonce with 403 Forbidden.
//   - CallbackHandler accepts ID tokens whose `nonce` claim matches the state
//     cookie nonce and completes the normal session flow.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupOIDCServerWithNonce builds a minimal OIDC test server whose /token
// endpoint returns a JWT with the given nonce claim value.
func setupOIDCServerWithNonce(t *testing.T, nonce string) *httptest.Server {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-nonce",
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
		signer, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
			(&jose.SignerOptions{}).WithHeader("kid", "test-key-nonce"),
		)
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		cl := josejwt.Claims{
			Subject:  "test-sub-nonce",
			Issuer:   serverURL,
			Audience: josejwt.Audience{"test-client-id"},
			Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
		}
		extra := map[string]any{
			"email":        "admin@example.com",
			"nebu_role":    "instance_admin",
			"nonce":        nonce, // injected per-test
		}
		raw, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
		if err != nil {
			t.Fatalf("Serialize JWT: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     raw,
		})
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// buildStateCookieWithNonce creates a signed admin_oidc_state cookie that
// includes a specific Nonce field value for nonce-focused tests.
func buildStateCookieWithNonce(t *testing.T, a *AdminAuth, state, nonce string) string {
	t.Helper()
	sc := oidcStateCookie{
		State:    state,
		Verifier: "someverifier",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
		Nonce:    nonce, // AC2: Nonce field must exist on oidcStateCookie
	}
	payload, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("json.Marshal oidcStateCookie: %v", err)
	}
	return a.signCookie(payload)
}

// ---------------------------------------------------------------------------
// AC1 + AC2: LoginStartHandler emits nonce in AuthCodeURL and state cookie
// ---------------------------------------------------------------------------

// TestLoginStart_EmitsNonceInAuthCodeURL verifies that LoginStartHandler:
//   - includes a non-empty `nonce` query parameter in the redirect URL (AC1/AC3)
//   - stores a matching `nonce` value inside the signed admin_oidc_state cookie (AC2)
//
func TestLoginStart_EmitsNonceInAuthCodeURL(t *testing.T) {
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
		t.Fatalf("expected 302 redirect, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// AC3: AuthCodeURL must contain a `nonce` query parameter.
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "nonce=") {
		t.Errorf("redirect Location missing nonce parameter: %s", location)
	}
	nonceInURL := ""
	for _, part := range strings.Split(location, "&") {
		if strings.HasPrefix(part, "nonce=") {
			nonceInURL = strings.TrimPrefix(part, "nonce=")
			break
		}
		// handle first param after "?"
		if idx := strings.Index(part, "?nonce="); idx >= 0 {
			nonceInURL = part[idx+7:]
			break
		}
	}
	if nonceInURL == "" {
		t.Fatalf("could not extract nonce value from Location: %s", location)
	}

	// AC2: the signed state cookie must contain a matching Nonce field.
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

	payload, err := a.verifyCookie(stateCookie.Value)
	if err != nil {
		t.Fatalf("verifyCookie: %v", err)
	}
	var sc oidcStateCookie
	if err := json.Unmarshal(payload, &sc); err != nil {
		t.Fatalf("json.Unmarshal oidcStateCookie: %v", err)
	}

	// AC2: Nonce field must be present and non-empty.
	if sc.Nonce == "" {
		t.Error("state cookie Nonce field is empty; expected a random nonce value")
	}

	// The nonce in the URL (base64url-encoded value from AuthCodeURL) must be
	// derivable from the nonce stored in the cookie. At minimum both must be
	// non-empty. A strict test checks exact equality (the nonce IS passed as-is
	// to oidc.Nonce() and appears verbatim in the URL).
	nonceDecoded, err := base64.RawURLEncoding.DecodeString(nonceInURL)
	if err != nil {
		// nonce might not be base64url-encoded — compare raw values directly.
		if nonceInURL != sc.Nonce {
			t.Errorf("nonce in URL (%q) does not match nonce in state cookie (%q)", nonceInURL, sc.Nonce)
		}
	} else {
		_ = nonceDecoded // decoded form available for future assertions
		if nonceInURL != sc.Nonce {
			t.Errorf("nonce in URL (%q) does not match nonce in state cookie (%q)", nonceInURL, sc.Nonce)
		}
	}
}

// ---------------------------------------------------------------------------
// AC4: CallbackHandler rejects mismatching nonce
// ---------------------------------------------------------------------------

// TestCallback_RejectsMismatchingNonce verifies that CallbackHandler returns 403
// when the ID token's `nonce` claim does not match the nonce stored in the state
// cookie. No session cookie must be set.
//
func TestCallback_RejectsMismatchingNonce(t *testing.T) {
	const cookieNonce = "abc123"
	const tokenNonce = "wrongnonce" // deliberate mismatch

	srv := setupOIDCServerWithNonce(t, tokenNonce)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	cookieValue := buildStateCookieWithNonce(t, a, "mystate", cookieNonce)

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	// AC4: nonce mismatch must yield 403 Forbidden.
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on nonce mismatch, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// No session cookie must be set.
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_session" && c.MaxAge > 0 {
			t.Errorf("admin_session cookie must not be set on nonce mismatch, got MaxAge=%d", c.MaxAge)
		}
	}
}

// ---------------------------------------------------------------------------
// AC4 (positive): CallbackHandler accepts matching nonce
// ---------------------------------------------------------------------------

// TestCallback_AcceptsMatchingNonce verifies that CallbackHandler completes the
// normal session flow (302 → /admin/dashboard) when the ID token's `nonce` claim
// exactly matches the nonce in the state cookie.
//
func TestCallback_AcceptsMatchingNonce(t *testing.T) {
	const sharedNonce = "abc123" // same value in cookie AND token

	srv := setupOIDCServerWithNonce(t, sharedNonce)
	provider := auth.NewProvider(context.Background(), srv.URL)
	a := newTestAdminAuth(t, provider)
	a.configReader = &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	cookieValue := buildStateCookieWithNonce(t, a, "mystate", sharedNonce)

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	// Expect redirect to /admin/dashboard on success.
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Errorf("expected 302/303 redirect on matching nonce, got %d; body: %s", rr.Code, rr.Body.String())
	}
	location := rr.Header().Get("Location")
	if location != "/admin/dashboard" {
		t.Errorf("expected Location=/admin/dashboard, got %q", location)
	}

	// A valid session cookie must be set.
	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Error("expected admin_session cookie to be set on successful nonce match")
	}
}

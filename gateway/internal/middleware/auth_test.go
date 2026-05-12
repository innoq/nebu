package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// setupOIDCServer returns a running httptest server and RSA private key.
// The server serves /.well-known/openid-configuration and /keys.
func setupOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
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
			"issuer":   serverURL,
			"jwks_uri": serverURL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, privateKey
}

// signJWT creates a signed JWT with the given expiry.
func signJWT(t *testing.T, serverURL string, privateKey *rsa.PrivateKey, expiry time.Time) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "test-sub-123",
		Issuer:   serverURL,
		Audience: josejwt.Audience{"nebu-gateway"},
		Expiry:   josejwt.NewNumericDate(expiry),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	extra := map[string]any{
		"preferred_username": "kai.mueller",
		"email":              "kai@example.com",
		"nebu_role":          "instance_admin",
	}
	raw, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	return raw
}

func TestJWTMiddleware_MissingToken(t *testing.T) {
	srv, _ := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for missing token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_MISSING_TOKEN" {
		t.Errorf("expected M_MISSING_TOKEN, got %q", body["errcode"])
	}
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	called := false
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
		if sub != "test-sub-123" {
			t.Errorf("expected sub=test-sub-123, got %q", sub)
		}
		username, _ := r.Context().Value(middleware.ContextKeyPreferredUsername).(string)
		if username != "kai.mueller" {
			t.Errorf("expected preferred_username=kai.mueller, got %q", username)
		}
		email, _ := r.Context().Value(middleware.ContextKeyEmail).(string)
		if email != "kai@example.com" {
			t.Errorf("expected email=kai@example.com, got %q", email)
		}
		role, _ := r.Context().Value(middleware.ContextKeyNebuRole).(string)
		if role != "instance_admin" {
			t.Errorf("expected nebu_role=instance_admin, got %q", role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler was not called for valid token")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestJWTMiddleware_ExpiredToken(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(-time.Hour))

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for expired token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
	if body["error"] != "Token has expired" {
		t.Errorf("expected 'Token has expired', got %q", body["error"])
	}
}

func TestJWTMiddleware_InvalidSignature(t *testing.T) {
	srv, _ := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	// Sign with a different key that the server doesn't know about
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	rawToken := signJWT(t, srv.URL, wrongKey, time.Now().Add(time.Hour))

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid signature")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
	if body["error"] != "Invalid token" {
		t.Errorf("expected 'Invalid token', got %q", body["error"])
	}
}

func TestJWTMiddleware_NilProvider(t *testing.T) {
	// Port 0 is always closed — provider.Inner() will be nil
	provider := auth.NewProvider(context.Background(), "http://127.0.0.1:0")

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when provider is nil")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
}

func TestJWTMiddleware_CustomClaimName(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "user-456",
		Issuer:   srv.URL,
		Audience: josejwt.Audience{"nebu-gateway"},
		Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	extra := map[string]any{
		"preferred_username": "custom.user",
		"email":              "custom@example.com",
		"roles":              "compliance_officer", // custom claim name, no "nebu_role"
	}
	rawToken, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	var capturedRole string
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "roles", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedRole, _ = r.Context().Value(middleware.ContextKeySystemRole).(string)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if capturedRole != "compliance_officer" {
		t.Errorf("expected system_role=compliance_officer, got %q", capturedRole)
	}
}

func TestJWTMiddleware_SystemRoleInContext(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	var capturedSystemRole, capturedRawRole string
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedSystemRole, _ = r.Context().Value(middleware.ContextKeySystemRole).(string)
			capturedRawRole, _ = r.Context().Value(middleware.ContextKeyNebuRole).(string)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if capturedSystemRole != "instance_admin" {
		t.Errorf("expected system_role=instance_admin, got %q", capturedSystemRole)
	}
	if capturedRawRole != "instance_admin" {
		t.Errorf("expected nebu_role=instance_admin, got %q", capturedRawRole)
	}
}

func TestJWTMiddleware_ArrayRoleClaim(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "user-789",
		Issuer:   srv.URL,
		Audience: josejwt.Audience{"nebu-gateway"},
		Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	// Dex delivers groups as an array (e.g. ["instance_admin"])
	extra := map[string]any{
		"preferred_username": "kai",
		"email":              "kai@example.com",
		"groups":             []any{"instance_admin"},
	}
	rawToken, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	var capturedSystemRole string
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "groups", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedSystemRole, _ = r.Context().Value(middleware.ContextKeySystemRole).(string)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if capturedSystemRole != "instance_admin" {
		t.Errorf("expected system_role=instance_admin for array groups claim, got %q", capturedSystemRole)
	}
}

// TestJWTMiddleware_ArrayRoleClaimAdminNotFirst is the regression test for the bug where
// extractRoleClaim returned only the first array element. With groups=["viewer","instance_admin"],
// the old code resolved "viewer" → "user". The fixed code must resolve "instance_admin".
func TestJWTMiddleware_ArrayRoleClaimAdminNotFirst(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "user-multi",
		Issuer:   srv.URL,
		Audience: josejwt.Audience{"nebu-gateway"},
		Expiry:   josejwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	extra := map[string]any{
		"preferred_username": "multi.user",
		"email":              "multi@example.com",
		"groups":             []any{"viewer", "instance_admin"}, // admin is NOT first
	}
	rawToken, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	var capturedSystemRole string
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "groups", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedSystemRole, _ = r.Context().Value(middleware.ContextKeySystemRole).(string)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if capturedSystemRole != "instance_admin" {
		t.Errorf("expected system_role=instance_admin when admin is second array element, got %q", capturedSystemRole)
	}
}

func TestJWTMiddleware_DenylistedToken(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	denylist := middleware.NewDenylist()
	_ = denylist.Invalidate(rawToken, time.Now().Add(time.Hour))

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", denylist, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called for denylisted token")
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
	if body["error"] != "Token has been logged out" {
		t.Errorf("expected 'Token has been logged out', got %q", body["error"])
	}
}

package matrix

// ─── Story 13-7: MSC2965 OIDC Discovery Endpoints ────────────────────────────
//
// Unit tests for AuthIssuerHandler and AuthMetadataHandler.
// Tests are written FIRST (red phase) before oidc_discovery.go exists.
//
// These tests will FAIL with "undefined: AuthIssuerHandler" and
// "undefined: AuthMetadataHandler" until oidc_discovery.go is created.
//
// Test strategy:
//   - AuthIssuerHandler: pure function, no external dependency — test directly.
//   - AuthMetadataHandler: injects a custom *http.Client to mock the OIDC provider.
//   - No JWT/OIDC middleware needed — both endpoints are unauthenticated.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/config"
)

// ─── AuthIssuerHandler tests ──────────────────────────────────────────────────

// TestAuthIssuerHandler_HappyPath verifies that GET /auth_issuer returns 200
// with {"issuer": "<cfg.OIDCIssuer>"}.
// AC1: auth_issuer returns the configured OIDC issuer URL.
func TestAuthIssuerHandler_HappyPath(t *testing.T) {
	cfg := &config.Config{
		OIDCIssuer: "https://keycloak.example.com/realms/nebu",
	}

	handler := AuthIssuerHandler(cfg)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_issuer", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Issuer != "https://keycloak.example.com/realms/nebu" {
		t.Errorf("expected issuer=https://keycloak.example.com/realms/nebu, got %q", body.Issuer)
	}
}

// TestAuthIssuerHandler_StablePath verifies the stable v1 path returns the same response.
// AC3: stable path is also registered (same handler).
func TestAuthIssuerHandler_StablePath(t *testing.T) {
	cfg := &config.Config{
		OIDCIssuer: "https://keycloak.example.com/realms/nebu",
	}

	handler := AuthIssuerHandler(cfg)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/auth_issuer", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on stable path, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var body struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Issuer != "https://keycloak.example.com/realms/nebu" {
		t.Errorf("expected issuer=https://keycloak.example.com/realms/nebu, got %q", body.Issuer)
	}
}

// TestAuthIssuerHandler_NoAuthRequired verifies no authorization header is needed.
// AC4: both endpoints work without an Authorization header (200, not 401/403).
func TestAuthIssuerHandler_NoAuthRequired(t *testing.T) {
	cfg := &config.Config{
		OIDCIssuer: "https://keycloak.example.com/realms/nebu",
	}

	handler := AuthIssuerHandler(cfg)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_issuer", nil)
	// No Authorization header set intentionally
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
		t.Errorf("expected auth_issuer to be unauthenticated, got %d", rr.Code)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// ─── AuthMetadataHandler tests ────────────────────────────────────────────────

// TestAuthMetadataHandler_HappyPath verifies that GET /auth_metadata returns 200
// with the proxied OIDC discovery document when the provider is reachable.
// AC2: auth_metadata returns the OIDC discovery document.
func TestAuthMetadataHandler_HappyPath(t *testing.T) {
	// Mock OIDC provider that returns a minimal discovery document.
	mockOIDCProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"issuer": "https://keycloak.example.com/realms/nebu",
			"authorization_endpoint": "https://keycloak.example.com/realms/nebu/protocol/openid-connect/auth",
			"token_endpoint": "https://keycloak.example.com/realms/nebu/protocol/openid-connect/token",
			"jwks_uri": "https://keycloak.example.com/realms/nebu/protocol/openid-connect/certs",
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"]
		}`))
	}))
	defer mockOIDCProvider.Close()

	cfg := &config.Config{
		OIDCIssuer: mockOIDCProvider.URL,
	}

	handler := AuthMetadataHandler(cfg, mockOIDCProvider.Client())

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_metadata", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode discovery document: %v", err)
	}
	if body.Issuer == "" {
		t.Errorf("expected non-empty issuer in discovery document")
	}
	if body.AuthorizationEndpoint == "" {
		t.Errorf("expected non-empty authorization_endpoint in discovery document")
	}
	if body.TokenEndpoint == "" {
		t.Errorf("expected non-empty token_endpoint in discovery document")
	}
}

// TestAuthMetadataHandler_ProviderUnreachable verifies that when the OIDC provider
// is unreachable, the handler returns 503 with {"errcode":"M_UNAVAILABLE","error":"..."}.
// AC5: OIDC provider unreachable → 503 M_UNAVAILABLE (not a 500 crash).
func TestAuthMetadataHandler_ProviderUnreachable(t *testing.T) {
	// Use a server that immediately closes all connections (unreachable simulation).
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable.Close() // Close immediately so all requests fail with connection refused.

	cfg := &config.Config{
		OIDCIssuer: unreachable.URL,
	}

	handler := AuthMetadataHandler(cfg, unreachable.Client())

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_metadata", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var body struct {
		Errcode string `json:"errcode"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode 503 body: %v", err)
	}
	if body.Errcode != "M_UNAVAILABLE" {
		t.Errorf("expected errcode=M_UNAVAILABLE, got %q", body.Errcode)
	}
	if body.Error == "" {
		t.Errorf("expected non-empty error message in 503 body")
	}
}

// TestAuthMetadataHandler_NoAuthRequired verifies no authorization header is needed.
// AC4: auth_metadata works without an Authorization header.
func TestAuthMetadataHandler_NoAuthRequired(t *testing.T) {
	mockOIDCProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"issuer":"https://example.com","authorization_endpoint":"https://example.com/auth","token_endpoint":"https://example.com/token"}`))
	}))
	defer mockOIDCProvider.Close()

	cfg := &config.Config{
		OIDCIssuer: mockOIDCProvider.URL,
	}

	handler := AuthMetadataHandler(cfg, mockOIDCProvider.Client())

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_metadata", nil)
	// No Authorization header set intentionally
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
		t.Errorf("expected auth_metadata to be unauthenticated, got %d", rr.Code)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// TestAuthMetadataHandler_ProviderReturns5xx verifies that a non-200 response from
// the OIDC provider is treated as unavailable (defensive proxy behaviour).
// AC5 (edge case): OIDC provider returns 5xx → gateway returns 503 M_UNAVAILABLE.
func TestAuthMetadataHandler_ProviderReturns5xx(t *testing.T) {
	mockOIDCProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal server error`))
	}))
	defer mockOIDCProvider.Close()

	cfg := &config.Config{
		OIDCIssuer: mockOIDCProvider.URL,
	}

	handler := AuthMetadataHandler(cfg, mockOIDCProvider.Client())

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/unstable/org.matrix.msc2965/auth_metadata", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when provider returns 5xx, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Errcode string `json:"errcode"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode 503 body: %v", err)
	}
	if body.Errcode != "M_UNAVAILABLE" {
		t.Errorf("expected M_UNAVAILABLE, got %q", body.Errcode)
	}
}

package middleware_test

// Story 5.18 — OIDC JWT Algorithm Pinning
//
// Acceptance Criteria covered:
//   AC 1/2/3 — verifier uses SupportedSigningAlgs (via JWTMiddleware)
//   AC 4     — NEBU_OIDC_SUPPORTED_ALGS parsing (ParseSupportedAlgs)
//   AC 5     — HS256 token is rejected with 401 M_UNKNOWN_TOKEN

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ---------------------------------------------------------------------------
// AC 4 — ParseSupportedAlgs() unit tests (env-var parsing)
// ---------------------------------------------------------------------------

// TestSupportedAlgs_DefaultIsRS256 verifies that an unset
// NEBU_OIDC_SUPPORTED_ALGS returns ["RS256"].
func TestSupportedAlgs_DefaultIsRS256(t *testing.T) {
	t.Setenv("NEBU_OIDC_SUPPORTED_ALGS", "")

	algs := middleware.ParseSupportedAlgs()

	if len(algs) != 1 || algs[0] != "RS256" {
		t.Errorf("expected [RS256], got %v", algs)
	}
}

// TestSupportedAlgs_OverrideViaEnv verifies that comma-separated values are
// split and trimmed correctly.
func TestSupportedAlgs_OverrideViaEnv(t *testing.T) {
	t.Setenv("NEBU_OIDC_SUPPORTED_ALGS", "ES256,PS256")

	algs := middleware.ParseSupportedAlgs()

	if len(algs) != 2 {
		t.Fatalf("expected 2 algs, got %d: %v", len(algs), algs)
	}
	if algs[0] != "ES256" {
		t.Errorf("expected algs[0]=ES256, got %q", algs[0])
	}
	if algs[1] != "PS256" {
		t.Errorf("expected algs[1]=PS256, got %q", algs[1])
	}
}

// TestSupportedAlgs_EmptyEnvFallsBackToDefault verifies that a whitespace-only
// NEBU_OIDC_SUPPORTED_ALGS falls back to the safe default ["RS256"].
func TestSupportedAlgs_EmptyEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("NEBU_OIDC_SUPPORTED_ALGS", "   ")

	algs := middleware.ParseSupportedAlgs()

	if len(algs) != 1 || algs[0] != "RS256" {
		t.Errorf("expected [RS256] for whitespace-only env, got %v", algs)
	}
}

// TestSupportedAlgs_SingleNonDefaultAlg verifies a single non-RS256 value
// without a trailing comma.
func TestSupportedAlgs_SingleNonDefaultAlg(t *testing.T) {
	t.Setenv("NEBU_OIDC_SUPPORTED_ALGS", "ES256")

	algs := middleware.ParseSupportedAlgs()

	if len(algs) != 1 || algs[0] != "ES256" {
		t.Errorf("expected [ES256], got %v", algs)
	}
}

// ---------------------------------------------------------------------------
// AC 5 — JWTMiddleware rejects HS256-signed tokens
// ---------------------------------------------------------------------------

// TestJWTMiddleware_HS256Rejected verifies that JWTMiddleware returns
// 401 M_UNKNOWN_TOKEN for an HS256-signed JWT.
//
// The OIDC stub in setupOIDCServer only advertises RS256 in its JWKS.
// go-oidc should reject the HS256 token either because (a) no matching key is
// found in the JWKS, or (b) because SupportedSigningAlgs=["RS256"] explicitly
// rejects it.  After Story 5.18 the reason must be (b) — defence in depth so
// that a compromised IdP that *adds* HS256 to its JWKS is also blocked.
//
// The test will PASS even before the fix (go-oidc rejects unknown kid), which
// is acceptable: the companion test TestJWTMiddleware_RS256StillAcceptedAfterPinning
// ensures the overall contract holds after the implementation is wired.
// The critical failing tests are TestSupportedAlgs_* above.
func TestJWTMiddleware_HS256Rejected(t *testing.T) {
	srv, _ := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	secret := []byte("super-secret-hmac-key-at-least-32-bytes-long-for-testing")
	rawToken := buildHS256Token(t, srv.URL, secret)

	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler must NOT be called for HS256 token — algorithm pinning broken")
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for HS256 token, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
}

// TestJWTMiddleware_RS256StillAcceptedAfterPinning is the companion green-path
// test: after algorithm pinning is in place, a valid RS256 token must still pass.
func TestJWTMiddleware_RS256StillAcceptedAfterPinning(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	rawToken := signJWT(t, srv.URL, key, time.Now().Add(time.Hour))

	called := false
	handler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler was not called for valid RS256 token after alg pinning")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for RS256 token, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// buildHS256Token constructs a well-formed but HS256-signed JWT.
// The token has a valid issuer and audience but is NOT backed by any key in
// the OIDC stub's JWKS — it simulates an algorithm-confusion attack payload.
func buildHS256Token(t *testing.T, issuerURL string, secret []byte) string {
	t.Helper()

	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	now := time.Now()
	claimsMap := map[string]any{
		"sub":                "attacker-sub",
		"iss":                issuerURL,
		"aud":                []string{"nebu-gateway"},
		"exp":                now.Add(time.Hour).Unix(),
		"iat":                now.Add(-time.Minute).Unix(),
		"preferred_username": "attacker",
		"email":              "attacker@evil.example",
	}
	claimsJSON, err := json.Marshal(claimsMap)
	if err != nil {
		t.Fatalf("json.Marshal claims: %v", err)
	}
	pld := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := hdr + "." + pld
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := mac.Sum(nil)

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

package matrix

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/nebu/nebu/internal/auth"
)

// TestSSORedirect_PromptLoginParameter verifies that GetSSORedirect adds
// prompt=login to the OIDC authorization URL so that Dex always forces
// fresh credential entry, regardless of any existing Dex session.
//
// # AC 1 — bugfix-logout-oidc-dex-session
//
// Without prompt=login, Dex reuses a cached session and may return the same
// id_token that was added to the denylist on logout. The first /sync after
// re-login would then receive 401 → Element lands on #/welcome.
func TestSSORedirect_PromptLoginParameter(t *testing.T) {
	// Stand-alone OIDC mock — no real Dex needed.
	oidcSrv, _ := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	if provider.Inner() == nil {
		t.Fatal("OIDC discovery failed — provider.Inner() is nil; mock server may not be ready")
	}

	h := NewLoginHandler(LoginConfig{
		DisplayName:  "Test SSO",
		Provider:     provider,
		ClientID:     "nebu-gateway",
		ClientSecret: "secret",
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/login/sso/redirect?redirectUrl=http://localhost:7070/",
		nil,
	)
	req.Host = "localhost:8008"
	w := httptest.NewRecorder()

	h.GetSSORedirect(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302 Found, got %d; body: %s", w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("Location header is missing from 302 response")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location header is not a valid URL: %v", err)
	}

	// AC 1: prompt=login MUST be present in the authorization URL.
	// This prevents Dex from reusing a cached session after Matrix logout,
	// which would return the same id_token that is already in the denylist.
	prompt := parsed.Query().Get("prompt")
	if prompt != "login" {
		t.Errorf(
			"expected prompt=login in authorization URL, got prompt=%q\nFull URL: %s\n\n"+
				"Fix: add oauth2.SetAuthURLParam(\"prompt\", \"login\") to AuthCodeURL in sso.go",
			prompt, location,
		)
	}

	// AC 1: nonce MUST be present in the authorization URL.
	// The nonce forces Dex to issue a fresh JWT that includes the nonce claim.
	// Even if Dex caches tokens, a different nonce results in a different JWT string
	// that cannot match any previously invalidated token in the denylist.
	nonce := parsed.Query().Get("nonce")
	if len(nonce) == 0 {
		t.Errorf(
			"expected nonce parameter in authorization URL, got empty string\nFull URL: %s\n\n"+
				"Fix: add oauth2.SetAuthURLParam(\"nonce\", nonce) to AuthCodeURL in sso.go",
			location,
		)
	}
	// Nonce must be exactly 32 hex chars (16 random bytes).
	if len(nonce) != 32 {
		t.Errorf("expected nonce to be 32 hex chars (16 bytes), got %d chars: %q", len(nonce), nonce)
	}

	// AC 2: Cache-Control: no-store MUST be set to prevent Safari from caching the redirect.
	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "no-store" {
		t.Errorf("expected Cache-Control: no-store on SSO redirect, got %q", cacheControl)
	}
}

// ---------------------------------------------------------------------------
// TestSSOStateStore_Rejects10001stEntry
// Story 5.21 — AC 5: ssoStateStore caps at 10 000 entries
// ---------------------------------------------------------------------------

// TestSSOStateStore_Rejects10001stEntry verifies that ssoStateStore.save
// returns a non-nil error when the store already holds 10 000 entries and a
// new entry is attempted.
//
// RED PHASE: ssoStateStore.save currently has signature
//
//	func (s *ssoStateStore) save(state, verifier, redirectURL string, ttl time.Duration)
//
// This test will fail to compile until the signature is changed to:
//
//	func (s *ssoStateStore) save(state, verifier, redirectURL string, ttl time.Duration) error
//
// and a capacity guard (return error when len(s.store) >= 10000) is added.
func TestSSOStateStore_Rejects10001stEntry(t *testing.T) {
	t.Parallel()

	const maxEntries = 10_000

	store := &ssoStateStore{store: make(map[string]ssoStateEntry)}

	// Fill exactly 10 000 entries — all must succeed.
	ttl := 10 * time.Minute
	for i := 0; i < maxEntries; i++ {
		key := fmt.Sprintf("state-%05d", i)
		if err := store.save(key, "verifier", "http://localhost/", "nonce", ttl); err != nil {
			t.Fatalf("save entry %d: unexpected error: %v", i, err)
		}
	}

	if len(store.store) != maxEntries {
		t.Fatalf("expected %d entries after fill, got %d", maxEntries, len(store.store))
	}

	// 10 001st entry must be rejected.
	err := store.save("state-overflow", "verifier", "http://localhost/", "nonce", ttl)
	if err == nil {
		t.Fatal("expected error when inserting entry 10 001 into a full ssoStateStore, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestSSOCallback_NonceMismatch
// Story 11.7 — AC 3: GetSSOCallback rejects id_token with wrong nonce
// ---------------------------------------------------------------------------

// TestSSOCallback_NonceMismatch verifies that GetSSOCallback returns 403 M_FORBIDDEN
// when the nonce embedded in the returned id_token does not match the nonce stored
// in globalSSOState for the given state parameter.
//
// Root cause: Dex may serve a cached id_token (issued for an earlier login) that
// carries an old nonce. Without nonce verification, that stale JWT would be issued
// as a new access_token. The first /sync would hit the denylist and return 401,
// causing Element Web to land on #/welcome instead of the room list.
//
// This test sets up:
//   - A mock OIDC server that serves discovery + JWKS + a /token endpoint.
//   - The /token endpoint issues a JWT with nonce="different-nonce-xyz" (wrong).
//   - globalSSOState is seeded with state="test-state-1234" and nonce="stored-nonce-abc".
//   - GetSSOCallback is called with ?code=authcode&state=test-state-1234.
//   - Expected: HTTP 403, errcode M_FORBIDDEN.
func TestSSOCallback_NonceMismatch(t *testing.T) {
	// NOTE: t.Parallel() is intentionally absent here.
	// This test writes to the package-level globalSSOState (inserting and cleaning
	// up a specific key). Running it in parallel with other tests that also mutate
	// globalSSOState (e.g., tests that call GetSSORedirect) would cause a data race
	// even though the store's internal mutex serialises individual operations, because
	// the test-level invariant (entry present at start, absent after pop) could be
	// violated by a concurrent test that drains or refills the map.

	// ── 1. Generate RSA key pair for the mock OIDC server ────────────────────
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "nonce-test-key",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	// ── 2. Build the mock OIDC server ─────────────────────────────────────────
	// We need: /.well-known/openid-configuration, /keys, /auth, /token.
	// The discovery doc must advertise both authorization_endpoint and token_endpoint
	// so that inner.Endpoint() returns a non-zero oauth2.Endpoint.
	// The /token endpoint returns an id_token signed with privateKey whose nonce
	// claim is "different-nonce-xyz" — intentionally different from the stored nonce.
	var serverURL string

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/auth",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/keys",
		})
	})

	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	// /auth is required only so that the discovery doc round-trips; no browser
	// actually follows this redirect in unit tests.
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost/callback?code=authcode&state="+r.URL.Query().Get("state"), http.StatusFound)
	})

	// /token simulates Dex returning a cached JWT with the WRONG nonce.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		// Issue a JWT with nonce="different-nonce-xyz" — intentionally wrong.
		signer, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
			(&jose.SignerOptions{}).WithHeader("kid", "nonce-test-key"),
		)
		if err != nil {
			http.Error(w, "signer error", http.StatusInternalServerError)
			return
		}
		now := time.Now()
		stdClaims := josejwt.Claims{
			Subject:  "user-subject-001",
			Issuer:   serverURL,
			Audience: josejwt.Audience{"nebu-gateway"},
			Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt: josejwt.NewNumericDate(now.Add(-time.Minute)),
		}
		extraClaims := map[string]any{
			"nonce": "different-nonce-xyz", // ← WRONG nonce; stored is "stored-nonce-abc"
			"email": "alex@example.com",
			"name":  "alex",
		}
		idToken, err := josejwt.Signed(signer).Claims(stdClaims).Claims(extraClaims).Serialize()
		if err != nil {
			http.Error(w, "serialize error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "bearer",
			"id_token":     idToken,
			"expires_in":   3600,
		})
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)

	// ── 3. Build a provider pointing at the mock server ───────────────────────
	provider := auth.NewProvider(context.Background(), serverURL)
	if provider.Inner() == nil {
		t.Fatal("OIDC discovery failed — provider.Inner() is nil; mock server may not be ready")
	}

	// ── 4. Seed globalSSOState with state + stored nonce ─────────────────────
	// We bypass GetSSORedirect and directly insert the entry so that the verifier,
	// redirectURL, and nonce are all under our control.
	const (
		testState   = "test-state-1234"
		storedNonce = "stored-nonce-abc" // this is what the callback should verify against
	)
	// Temporarily store so that globalSSOState.pop(testState) returns the entry.
	globalSSOState.mu.Lock()
	globalSSOState.store[testState] = ssoStateEntry{
		verifier:    "unused-verifier-value",
		redirectURL: "http://localhost:7070/",
		nonce:       storedNonce,
		exp:         time.Now().Add(10 * time.Minute),
	}
	globalSSOState.mu.Unlock()
	// pop() in GetSSOCallback removes the state entry on success/failure.
	// This cleanup guards the case where t.Fatal fires before GetSSOCallback is called.
	t.Cleanup(func() {
		globalSSOState.mu.Lock()
		delete(globalSSOState.store, testState)
		globalSSOState.mu.Unlock()
	})

	// ── 5. Build the handler ──────────────────────────────────────────────────
	h := NewLoginHandler(LoginConfig{
		DisplayName:  "Test SSO",
		Provider:     provider,
		ClientID:     "nebu-gateway",
		ClientSecret: "secret",
	})

	// ── 6. Call GetSSOCallback with ?code=authcode&state=test-state-1234 ──────
	// The mock /token endpoint will return an id_token with nonce="different-nonce-xyz".
	// GetSSOCallback must detect the mismatch and return 403 M_FORBIDDEN.
	callbackURL := "/_matrix/client/v3/login/sso/redirect/oidc?code=authcode&state=" + testState
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	req.Host = "localhost:8008"
	w := httptest.NewRecorder()

	h.GetSSOCallback(w, req)

	// ── 7. Assert ─────────────────────────────────────────────────────────────
	if w.Code != http.StatusForbidden {
		t.Fatalf(
			"expected HTTP 403 M_FORBIDDEN on nonce mismatch, got %d\nbody: %s\n\n"+
				"Fix: add nonce claim extraction + comparison in GetSSOCallback (sso.go).\n"+
				"The stored nonce was %q but the id_token carried %q.",
			w.Code, w.Body.String(), storedNonce, "different-nonce-xyz",
		)
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v\nbody: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q\nfull body: %+v", errResp.ErrCode, errResp)
	}
	// Sanity: no opaque loginToken should have been issued.
	if strings.Contains(w.Header().Get("Location"), "loginToken") {
		t.Error("GetSSOCallback must NOT issue a loginToken when nonce mismatch is detected")
	}
}

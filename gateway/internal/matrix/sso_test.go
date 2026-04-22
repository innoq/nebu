package matrix

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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
		if err := store.save(key, "verifier", "http://localhost/", ttl); err != nil {
			t.Fatalf("save entry %d: unexpected error: %v", i, err)
		}
	}

	if len(store.store) != maxEntries {
		t.Fatalf("expected %d entries after fill, got %d", maxEntries, len(store.store))
	}

	// 10 001st entry must be rejected.
	err := store.save("state-overflow", "verifier", "http://localhost/", ttl)
	if err == nil {
		t.Fatal("expected error when inserting entry 10 001 into a full ssoStateStore, got nil")
	}
}

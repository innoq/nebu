package admin

// Story 5.17: OIDC Issuer HTTPS Enforcement + Provider Caching — Acceptance Tests
//
// RED PHASE — these tests MUST fail until:
//   - validateIssuerURL(s string) error  is implemented in admin/validation.go
//   - loadProvider(ctx, issuer string) (*oidc.Provider, error)  is implemented in admin/oidc_cache.go
//   - bootstrap.go StepHandler case 2 calls validateIssuerURL and returns 400 on failure
//
// AC1: Bootstrap API rejects oidc_issuer values that do not begin with https://, http://localhost
//      or http://127.0.0.1. Returns 400 with a clear message.
// AC3: loadProvider caches *oidc.Provider keyed by issuer URL with a 10-minute TTL.
//      Negative-cache failures for 30s.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AC1 — validateIssuerURL: Bootstrap step 2 rejects non-HTTPS/non-localhost HTTP issuers
// ---------------------------------------------------------------------------

// TestBootstrap_RejectsHTTPIssuer verifies that POST /admin/bootstrap step=2
// with oidc_issuer="http://evil.example" returns 400 Bad Request.
// This test is FAILING until StepHandler case 2 calls validateIssuerURL and
// returns http.StatusBadRequest (not 422) for scheme violations.
func TestBootstrap_RejectsHTTPIssuer(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "2")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "http://evil.example")
	form.Set("oidc_client_id", "nebu-admin")
	form.Set("oidc_client_secret", "s3cr3t")

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-HTTPS non-localhost issuer, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "https") {
		t.Error("expected error body to mention 'https' as the required scheme")
	}
}

// TestBootstrap_AllowsLocalhost verifies that http://localhost:5556 is accepted
// (dev allowance) — the handler must not return 400 for this URL.
// This test is FAILING until validateIssuerURL permits http://localhost.
func TestBootstrap_AllowsLocalhost(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "2")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "http://localhost:5556/dex")
	form.Set("oidc_client_id", "nebu-admin")
	form.Set("oidc_client_secret", "s3cr3t")

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code == http.StatusBadRequest {
		t.Errorf("http://localhost must not be rejected (dev allowance), got 400")
	}
}

// TestBootstrap_AllowsHTTPS verifies that https://dex.example.com is accepted.
// A network error from the OIDC provider is acceptable (schema-only check at this stage).
// This test is FAILING until validateIssuerURL permits https:// unconditionally.
func TestBootstrap_AllowsHTTPS(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "2")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://dex.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	form.Set("oidc_client_secret", "s3cr3t")

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code == http.StatusBadRequest {
		t.Errorf("https:// issuer must not be rejected by schema check, got 400")
	}
}

// ---------------------------------------------------------------------------
// validateIssuerURL direct unit tests
// ---------------------------------------------------------------------------

// TestValidateIssuerURL_RejectsHTTP verifies that validateIssuerURL returns a non-nil
// error for a plain http:// issuer that is not localhost/127.0.0.1.
// FAILING until validateIssuerURL exists in admin/validation.go.
func TestValidateIssuerURL_RejectsHTTP(t *testing.T) {
	err := validateIssuerURL("http://evil.example/issuer")
	if err == nil {
		t.Error("expected error for http://evil.example, got nil")
	}
}

// TestValidateIssuerURL_AllowsHTTPS verifies that validateIssuerURL returns nil for https://.
// FAILING until validateIssuerURL exists in admin/validation.go.
func TestValidateIssuerURL_AllowsHTTPS(t *testing.T) {
	if err := validateIssuerURL("https://dex.example.com"); err != nil {
		t.Errorf("expected nil for https://, got %v", err)
	}
}

// TestValidateIssuerURL_AllowsLocalhost verifies that validateIssuerURL returns nil for
// http://localhost (dev allowance).
// FAILING until validateIssuerURL exists in admin/validation.go.
func TestValidateIssuerURL_AllowsLocalhost(t *testing.T) {
	cases := []string{
		"http://localhost:5556",
		"http://localhost:5556/dex",
		"http://127.0.0.1:5556",
		"http://[::1]:5556",
	}
	for _, issuer := range cases {
		if err := validateIssuerURL(issuer); err != nil {
			t.Errorf("expected nil for %q (dev allowance), got %v", issuer, err)
		}
	}
}

// TestValidateIssuerURL_RejectsEmpty verifies that validateIssuerURL returns a non-nil
// error for an empty string.
// FAILING until validateIssuerURL exists in admin/validation.go.
func TestValidateIssuerURL_RejectsEmpty(t *testing.T) {
	if err := validateIssuerURL(""); err == nil {
		t.Error("expected error for empty issuer URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// AC3 — loadProvider: cache reuse and negative cache
// ---------------------------------------------------------------------------

// providerCacheState holds the injectable function and call counter used
// across cache tests. It mirrors the runInTx injection pattern from Story 5.11.
type providerCallSpy struct {
	mu        sync.Mutex
	callCount int
	returnErr error
	// fakeProvider is returned on success calls. We use a *fakeOIDCProvider
	// so tests don't need a live OIDC server.
	fakeProvider *fakeOIDCProvider
}

// fakeOIDCProvider is a stand-in for *oidc.Provider. loadProvider's injectable
// newProviderFn returns this type so unit tests avoid network I/O.
// The production implementation uses oidc.NewProvider; the test wires a fake.
type fakeOIDCProvider struct{}

func (s *providerCallSpy) newProviderFn(_ context.Context, _ string) (*fakeOIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	if s.fakeProvider != nil {
		return s.fakeProvider, nil
	}
	return &fakeOIDCProvider{}, nil
}

// oidcProviderCache is the type that loadProvider must be a method on (or a standalone
// function that accepts a *oidcProviderCache). Tests construct it directly so the
// injectable newProviderFn can be set.
//
// EXPECTED INTERFACE (implementation must satisfy this to make these tests pass):
//
//	type oidcProviderCache struct {
//	    mu           sync.Mutex
//	    entries      map[string]*oidcCacheEntry
//	    newProviderFn func(ctx context.Context, issuer string) (*oidc.Provider, error)
//	    ttl          time.Duration   // positive TTL, default 10 min
//	    negativeTTL  time.Duration   // negative TTL, default 30 s
//	}
//
//	func newOIDCProviderCache(fn func(ctx, issuer) (*oidc.Provider, error)) *oidcProviderCache
//	func (c *oidcProviderCache) load(ctx context.Context, issuer string) (*oidc.Provider, error)

// TestOIDCProviderCache_ReusesInstance verifies that calling load(issuer) twice
// invokes the underlying newProviderFn only once — the second call returns the
// cached *oidc.Provider without a new fetch.
//
// FAILING until oidcProviderCache and its load method exist in admin/oidc_cache.go.
func TestOIDCProviderCache_ReusesInstance(t *testing.T) {
	spy := &providerCallSpy{}

	cache := newOIDCProviderCache(func(ctx context.Context, issuer string) (oidcProvider, error) {
		return spy.newProviderFn(ctx, issuer)
	})

	issuer := "https://dex.example.com"

	p1, err := cache.load(context.Background(), issuer)
	if err != nil {
		t.Fatalf("first load returned error: %v", err)
	}
	if p1 == nil {
		t.Fatal("first load returned nil provider")
	}

	p2, err := cache.load(context.Background(), issuer)
	if err != nil {
		t.Fatalf("second load returned error: %v", err)
	}
	if p2 == nil {
		t.Fatal("second load returned nil provider")
	}

	spy.mu.Lock()
	calls := spy.callCount
	spy.mu.Unlock()

	if calls != 1 {
		t.Errorf("expected newProviderFn to be called exactly once, was called %d times", calls)
	}
	if p1 != p2 {
		t.Error("expected both load calls to return the same cached provider instance")
	}
}

// TestOIDCProviderCache_NegativeCacheOnFailure verifies that when the first load
// fails, a second call within 30 s returns the cached error without re-calling
// newProviderFn.
//
// FAILING until oidcProviderCache negative-caching is implemented in admin/oidc_cache.go.
func TestOIDCProviderCache_NegativeCacheOnFailure(t *testing.T) {
	networkErr := errors.New("connection refused")
	spy := &providerCallSpy{returnErr: networkErr}

	cache := newOIDCProviderCache(func(ctx context.Context, issuer string) (oidcProvider, error) {
		return spy.newProviderFn(ctx, issuer)
	})
	// Override negativeTTL to something large so the test doesn't race against real time.
	cache.negativeTTL = 30 * time.Second

	issuer := "https://down-idp.example.com"

	_, err1 := cache.load(context.Background(), issuer)
	if err1 == nil {
		t.Fatal("expected first load to fail with network error, got nil")
	}
	if !errors.Is(err1, networkErr) {
		t.Errorf("expected first error to wrap networkErr, got %v", err1)
	}

	_, err2 := cache.load(context.Background(), issuer)
	if err2 == nil {
		t.Fatal("expected second load to return cached error, got nil")
	}

	spy.mu.Lock()
	calls := spy.callCount
	spy.mu.Unlock()

	if calls != 1 {
		t.Errorf("expected newProviderFn called once (cached negative), was called %d times", calls)
	}
}

// TestOIDCProviderCache_NegativeCacheExpires verifies that after the negative TTL
// has elapsed, a subsequent call retries the upstream provider fetch.
//
// FAILING until oidcProviderCache negative-caching with TTL expiry is implemented.
func TestOIDCProviderCache_NegativeCacheExpires(t *testing.T) {
	networkErr := errors.New("connection refused")
	spy := &providerCallSpy{returnErr: networkErr}

	cache := newOIDCProviderCache(func(ctx context.Context, issuer string) (oidcProvider, error) {
		return spy.newProviderFn(ctx, issuer)
	})
	// Set a very short negativeTTL so we can expire it in tests without sleeping.
	cache.negativeTTL = 1 * time.Millisecond

	issuer := "https://down-idp.example.com"

	// First call — populates negative cache.
	if _, err := cache.load(context.Background(), issuer); err == nil {
		t.Fatal("expected error on first load")
	}

	// Expire the negative cache entry.
	time.Sleep(5 * time.Millisecond)

	// Second call — negative cache expired, must retry newProviderFn.
	if _, err := cache.load(context.Background(), issuer); err == nil {
		t.Fatal("expected error on second load (provider still down)")
	}

	spy.mu.Lock()
	calls := spy.callCount
	spy.mu.Unlock()

	if calls < 2 {
		t.Errorf("expected newProviderFn called at least twice after TTL expiry, was called %d times", calls)
	}
}

// ---------------------------------------------------------------------------
// AC2 — LoginStartHandler rejects legacy HTTP issuer stored in DB
// ---------------------------------------------------------------------------

// TestLoadOIDCConfig_RejectsLegacyHTTPIssuer verifies that LoginStartHandler
// returns 500 with an operator-actionable message when the issuer stored in
// server_config is a non-localhost HTTP URL (e.g. "http://dex:5556").
// This closes the gap where a misconfigured DB entry would silently bypass
// HTTPS enforcement at runtime.
func TestLoadOIDCConfig_RejectsLegacyHTTPIssuer(t *testing.T) {
	reader := &fakeServerConfigReader{
		issuer:       "http://evil.example.com",
		clientID:     "nebu-admin",
		clientSecret: "s3cr3t",
	}
	a := newTestAdminAuthWithReader(t, reader, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/login/start", nil)
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for non-HTTPS non-localhost issuer from DB, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "https") {
		t.Errorf("expected operator-actionable error body mentioning 'https', got: %q", body)
	}
	if !strings.Contains(strings.ToLower(body), "operator") {
		t.Errorf("expected operator-actionable error body mentioning 'operator', got: %q", body)
	}
}

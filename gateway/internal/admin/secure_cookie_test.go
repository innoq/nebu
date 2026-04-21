package admin

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
)

// ---------------------------------------------------------------------------
// isRequestSecure — unit tests (Story 5.15)
// ---------------------------------------------------------------------------

// TestIsRequestSecure_TLSDirect verifies that a request arriving with an
// active TLS connection (r.TLS != nil) is always considered secure,
// regardless of NEBU_TRUSTED_PROXY.
func TestIsRequestSecure_TLSDirect(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.TLS = &tls.ConnectionState{} // simulate direct TLS

	if !isRequestSecure(req) {
		t.Error("expected isRequestSecure=true for r.TLS != nil, got false")
	}
}

// TestIsRequestSecure_ForwardedProtoHTTPS verifies that when
// NEBU_TRUSTED_PROXY=true and the request carries X-Forwarded-Proto: https,
// the helper returns true (proxy-terminated TLS is trusted).
func TestIsRequestSecure_ForwardedProtoHTTPS(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "true")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	// r.TLS is nil — simulates proxy-terminated TLS

	if !isRequestSecure(req) {
		t.Error("expected isRequestSecure=true with NEBU_TRUSTED_PROXY=true and X-Forwarded-Proto: https, got false")
	}
}

// TestIsRequestSecure_FailClosedWhenHeaderMissing verifies that when
// NEBU_TRUSTED_PROXY=true but X-Forwarded-Proto is absent, the helper
// returns false (fail-closed: no assumption about external scheme).
func TestIsRequestSecure_FailClosedWhenHeaderMissing(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "true")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	// Deliberately no X-Forwarded-Proto header

	if isRequestSecure(req) {
		t.Error("expected isRequestSecure=false when NEBU_TRUSTED_PROXY=true but X-Forwarded-Proto is missing, got true")
	}
}

// TestIsRequestSecure_NoTrustNoProxy verifies that when NEBU_TRUSTED_PROXY is
// empty (default), a request carrying X-Forwarded-Proto: https is NOT treated
// as secure — the header is ignored because proxy trust is not configured.
func TestIsRequestSecure_NoTrustNoProxy(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	// r.TLS is nil

	if isRequestSecure(req) {
		t.Error("expected isRequestSecure=false when NEBU_TRUSTED_PROXY is unset (untrusted header), got true")
	}
}

// ---------------------------------------------------------------------------
// Cookie Secure-flag integration tests
//
// These tests drive LoginHandler (which emits admin_oidc_state) and verify
// that the Secure attribute on emitted cookies follows isRequestSecure().
//
// LoginHandler requires a.provider.Inner() != nil; setupAdminOIDCServer from
// auth_test.go provides a minimal OIDC server for that purpose.
// ---------------------------------------------------------------------------

// TestCookie_SecureFlagWithForwardedProto exercises LoginHandler with
// NEBU_TRUSTED_PROXY=true and X-Forwarded-Proto: https. The admin_oidc_state
// cookie emitted on the redirect must carry Secure=true even though r.TLS is nil.
func TestCookie_SecureFlagWithForwardedProto(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "true")

	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	// Wait for background discovery to succeed (retries internally).
	for range 20 {
		if provider.Inner() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if provider.Inner() == nil {
		t.Fatal("OIDC provider discovery did not complete in time")
	}

	a := newTestAdminAuth(t, provider)

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/login", nil)
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	// r.TLS intentionally nil — simulates proxy-terminated TLS

	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)

	cookies := rr.Result().Cookies()
	var oidcStateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "admin_oidc_state" {
			oidcStateCookie = c
		}
	}

	if oidcStateCookie == nil {
		t.Fatal("admin_oidc_state cookie not set by LoginHandler")
	}
	if !oidcStateCookie.Secure {
		t.Errorf("admin_oidc_state cookie Secure flag should be true with NEBU_TRUSTED_PROXY=true and X-Forwarded-Proto: https, got false")
	}
}

// TestCookie_NoSecureFlagWithoutTrust exercises LoginHandler with
// NEBU_TRUSTED_PROXY unset (default). Even when the request carries
// X-Forwarded-Proto: https the Secure flag must remain false because
// proxy trust is not configured and r.TLS is nil.
func TestCookie_NoSecureFlagWithoutTrust(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "")

	srv, _ := setupAdminOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)
	for range 20 {
		if provider.Inner() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if provider.Inner() == nil {
		t.Fatal("OIDC provider discovery did not complete in time")
	}

	a := newTestAdminAuth(t, provider)

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/login", nil)
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https") // must be ignored — no trust configured
	// r.TLS intentionally nil

	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)

	cookies := rr.Result().Cookies()
	var oidcStateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "admin_oidc_state" {
			oidcStateCookie = c
		}
	}

	if oidcStateCookie == nil {
		t.Fatal("admin_oidc_state cookie not set by LoginHandler")
	}
	if oidcStateCookie.Secure {
		t.Errorf("admin_oidc_state cookie Secure flag should be false when NEBU_TRUSTED_PROXY is unset and r.TLS is nil, got true")
	}
}

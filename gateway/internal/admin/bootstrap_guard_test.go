package admin

// Story 5.10: Bootstrap Mode Guard — Replay Prevention
//
// Tests for AC1–AC4: bootstrap replay prevention, BootstrapGuard redirect
// target, cookie Path scope, and select-claim route guarding.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeSignedBootstrapStateCookie builds a signed admin_oidc_state cookie with Mode="bootstrap"
// using the given AdminAuth instance (to reuse its HMAC key).
func makeSignedBootstrapStateCookie(t *testing.T, a *AdminAuth) *http.Cookie {
	t.Helper()
	sc := oidcStateCookie{
		State:    "test-state-value",
		Verifier: "test-verifier",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
		Mode:     "bootstrap",
	}
	payload, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal state cookie: %v", err)
	}
	return &http.Cookie{
		Name:  "admin_oidc_state",
		Value: a.signCookie(payload),
	}
}

// newTestAdminAuthWithCheckerAndReader creates an AdminAuth with a fake ServerConfigReader
// and a fake BootstrapStatusChecker for Story 5.10 tests.
func newTestAdminAuthWithCheckerAndReader(
	t *testing.T,
	reader ServerConfigReader,
	checker BootstrapStatusChecker,
) *AdminAuth {
	t.Helper()
	a := newTestAdminAuthWithReader(t, reader, nil)
	a.bootstrapChecker = checker
	return a
}

// TestLoginStart_BootstrapModeRejectedAfterCompletion verifies AC1:
// LoginStartHandler must return 403 Forbidden when mode=bootstrap is requested
// but bootstrap is already completed.
func TestLoginStart_BootstrapModeRejectedAfterCompletion(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	// bootstrap is COMPLETE — checker reports active=false
	checker := &fakeBootstrapChecker{active: false}
	a := newTestAdminAuthWithCheckerAndReader(t, reader, checker)

	req := httptest.NewRequest("GET", "/admin/login/start?mode=bootstrap", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	// AC1: must return 403, not 302
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden after bootstrap completion, got %d; body: %s",
			rr.Code, rr.Body.String())
	}

	// Must NOT contain a Dex authorization redirect
	location := rr.Header().Get("Location")
	if strings.Contains(location, srv.URL) {
		t.Errorf("must not redirect to Dex when bootstrap is complete; Location: %q", location)
	}
}

// TestLoginStart_BootstrapModeAllowedWhileActive verifies AC1 (happy path):
// LoginStartHandler must return 302 to Dex when mode=bootstrap is requested
// and bootstrap is still active.
func TestLoginStart_BootstrapModeAllowedWhileActive(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	// bootstrap is still ACTIVE
	checker := &fakeBootstrapChecker{active: true}
	a := newTestAdminAuthWithCheckerAndReader(t, reader, checker)

	req := httptest.NewRequest("GET", "/admin/login/start?mode=bootstrap", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302 to Dex while bootstrap is active, got %d; body: %s",
			rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, srv.URL) {
		t.Errorf("expected Location to point to Dex (%s), got %q", srv.URL, location)
	}
	if !strings.Contains(location, "code_challenge_method=S256") {
		t.Errorf("Location missing PKCE code_challenge_method=S256: %s", location)
	}
}

// TestLoginStart_BootstrapCheckerError_Returns500 verifies that a DB error
// from IsBootstrapActive() returns 500 Internal Server Error (not a redirect or 403).
func TestLoginStart_BootstrapCheckerError_Returns500(t *testing.T) {
	srv, _ := setupAdminOIDCServer(t)

	reader := &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}
	// checker returns a DB error
	checker := &fakeBootstrapChecker{active: false, err: errFakeDB}
	a := newTestAdminAuthWithCheckerAndReader(t, reader, checker)

	req := httptest.NewRequest("GET", "/admin/login/start?mode=bootstrap", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on checker error, got %d; body: %s",
			rr.Code, rr.Body.String())
	}

	// Must NOT redirect to Dex or return 403
	location := rr.Header().Get("Location")
	if location != "" {
		t.Errorf("must not redirect on checker error; Location: %q", location)
	}
}

// TestSelectClaim_RejectedByBootstrapGuard verifies AC2 + AC3:
// When bootstrap_completed=true, BootstrapGuard must redirect POST /admin/bootstrap/select-claim
// to /admin/dashboard (not /admin/login).
func TestSelectClaim_RejectedByBootstrapGuard(t *testing.T) {
	// bootstrap is COMPLETE — the guard must block the request
	checker := &fakeBootstrapChecker{active: false}

	// Track whether the inner handler (ClaimSelectionHandler) is ever called
	handlerCalled := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Apply BootstrapGuard directly — simulates the main.go guard(...) wrapper required by AC2
	guarded := BootstrapGuard(checker)(innerHandler)

	// Build a POST to the select-claim path with a valid signed state cookie
	a := NewAdminAuth(nil, "test-client-id", "test-secret", "nebu_role", []byte("test-secret-key"), nil, nil)
	stateCookie := makeSignedBootstrapStateCookie(t, a)

	req := httptest.NewRequest("POST", "/admin/bootstrap/select-claim",
		strings.NewReader("admin_group_claim=instance_admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(stateCookie)
	rr := httptest.NewRecorder()
	guarded.ServeHTTP(rr, req)

	// AC3: must redirect to /admin/dashboard (not /admin/login)
	if rr.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d; body: %s", rr.Code, rr.Body.String())
	}
	location := rr.Header().Get("Location")
	if location != "/admin/dashboard" {
		t.Errorf("expected redirect to /admin/dashboard, got %q (AC3: wrong BootstrapGuard redirect target)", location)
	}

	// Inner handler must not have been called — no DB writes should occur
	if handlerCalled {
		t.Error("ClaimSelectionHandler must not be called when bootstrap is complete (no DB writes allowed)")
	}
}

// TestStateCookie_PathScopedToCallback verifies AC4:
// The admin_oidc_state cookie set by LoginStartHandler must have Path=/admin/callback
// so it is only transmitted to the callback route, not every /admin/* request.
func TestStateCookie_PathScopedToCallback(t *testing.T) {
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

	// AC4: Path must be /admin/callback, not /admin
	if stateCookie.Path != "/admin/callback" {
		t.Errorf("expected cookie Path=/admin/callback, got %q (cookie scoped too broadly — AC4 not implemented)", stateCookie.Path)
	}
}

package admin

// Story 5.13: CSRF Double-Submit-Cookie Middleware
//
// Unit tests for the double-submit-cookie CSRF protection implemented in middleware.go.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCSRF_RejectsPOSTWithoutToken verifies AC2:
// A POST request with no _csrf form field is rejected with 403 Forbidden,
// even if a valid admin session is present.
func TestCSRF_RejectsPOSTWithoutToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	// POST without any _csrf form field — no cookie either
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d (403 Forbidden)", rr.Code, http.StatusForbidden)
	}
	if nextCalled {
		t.Error("next handler must NOT be called when _csrf token is missing")
	}
}

// TestCSRF_RejectsMismatchedToken verifies AC2 (constant-time path):
// A POST request where cookie csrf_token=A but form _csrf=B is rejected with 403
// regardless of how similar A and B are (no timing oracle).
func TestCSRF_RejectsMismatchedToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	body := strings.NewReader("_csrf=token-B-does-not-match")
	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Cookie carries a different token value
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token-A-correct"})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d (403 Forbidden)", rr.Code, http.StatusForbidden)
	}
	if nextCalled {
		t.Error("next handler must NOT be called when CSRF tokens do not match")
	}
}

// TestCSRF_AcceptsMatchingToken verifies AC2 (happy path):
// A POST request where cookie csrf_token and form _csrf carry the same value
// passes through to the next handler.
func TestCSRF_AcceptsMatchingToken(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	const matchingToken = "abc123-same-value-in-both-cookie-and-form"

	body := strings.NewReader("_csrf=" + matchingToken)
	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: matchingToken})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 OK", rr.Code)
	}
	if !nextCalled {
		t.Error("next handler MUST be called when CSRF tokens match")
	}
}

// TestCSRF_GetPassthrough verifies that GET requests are NOT blocked by CSRFMiddleware
// (CSRF protection applies only to state-changing methods: POST/PUT/DELETE).
func TestCSRF_GetPassthrough(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	// GET request — no CSRF cookie, no form token — must pass through
	req := httptest.NewRequest(http.MethodGet, "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 OK for GET requests", rr.Code)
	}
	if !nextCalled {
		t.Error("next handler MUST be called for GET requests")
	}
}

// TestCSRF_GetSetsCookie verifies AC1:
// A GET request causes CSRFMiddleware to issue a csrf_token cookie
// so forms rendered by the handler can embed the token.
func TestCSRF_GetSetsCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// At least one Set-Cookie header for csrf_token must be present
	var csrfCookieFound bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == "csrf_token" && c.Value != "" {
			csrfCookieFound = true
		}
	}
	if !csrfCookieFound {
		t.Error("GET response must set a non-empty csrf_token cookie")
	}
}

// TestCSRF_RotatesOnLogin verifies AC6:
// After /admin/callback completes successfully, a NEW csrf_token cookie is issued
// so the token is bound to the new authenticated session (prevents session fixation).
//
// The test exercises the CSRFMiddleware directly: if a GET arrives at /admin/callback
// with an existing csrf_token cookie, the middleware must rotate (replace) the token
// rather than reusing the old one.
func TestCSRF_RotatesOnLogin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	oldToken := "old-csrf-token-before-login"

	req := httptest.NewRequest(http.MethodGet, "/admin/callback", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: oldToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// A new csrf_token cookie must be set and must differ from the old one
	var newToken string
	for _, c := range rr.Result().Cookies() {
		if c.Name == "csrf_token" {
			newToken = c.Value
		}
	}
	if newToken == "" {
		t.Error("csrf_token cookie must be rotated (set) after /admin/callback GET")
	}
	if newToken == oldToken {
		t.Errorf("csrf_token must be rotated to a new value on login, got the same value: %q", newToken)
	}
}

// TestCSRF_TokenInContext verifies that CSRFMiddleware stores the CSRF token in the
// request context so that template helpers can embed it in forms without additional
// cookie reads.
func TestCSRF_TokenInContext(t *testing.T) {
	var tokenFromCtx string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenFromCtx = CSRFTokenFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware()(next)

	const token = "context-inject-token"
	req := httptest.NewRequest(http.MethodGet, "/admin/bootstrap", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
	if tokenFromCtx == "" {
		t.Error("CSRFTokenFromContext must return a non-empty token after GET")
	}
}

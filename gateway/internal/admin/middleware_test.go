package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// signTestCookie creates a signed admin_session cookie value for testing.
func signTestCookie(t *testing.T, secret []byte, payload []byte) string {
	t.Helper()
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig
}

func TestBootstrapGuard(t *testing.T) {
	tests := []struct {
		name            string
		bootstrapActive bool
		checkerErr      error
		path            string
		wantCode        int
		wantLocation    string
		wantNext        bool
	}{
		{
			name:            "incomplete + non-bootstrap path → redirect to bootstrap",
			bootstrapActive: true,
			checkerErr:      nil,
			path:            "/admin/dashboard",
			wantCode:        http.StatusFound,
			wantLocation:    "/admin/bootstrap",
			wantNext:        false,
		},
		{
			name:            "incomplete + bootstrap path → pass-through",
			bootstrapActive: true,
			checkerErr:      nil,
			path:            "/admin/bootstrap",
			wantCode:        http.StatusOK,
			wantLocation:    "",
			wantNext:        true,
		},
		{
			name:            "complete + bootstrap path → redirect to login",
			bootstrapActive: false,
			checkerErr:      nil,
			path:            "/admin/bootstrap",
			wantCode:        http.StatusFound,
			wantLocation:    "/admin/login",
			wantNext:        false,
		},
		{
			name:            "complete + non-bootstrap path → pass-through",
			bootstrapActive: false,
			checkerErr:      nil,
			path:            "/admin/dashboard",
			wantCode:        http.StatusOK,
			wantLocation:    "",
			wantNext:        true,
		},
		{
			name:            "checker error → 500",
			bootstrapActive: false,
			checkerErr:      errFakeDB,
			path:            "/admin/dashboard",
			wantCode:        http.StatusInternalServerError,
			wantLocation:    "",
			wantNext:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker := &fakeBootstrapChecker{active: tc.bootstrapActive, err: tc.checkerErr}
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})
			handler := BootstrapGuard(checker)(next)

			req := httptest.NewRequest("GET", tc.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rr.Code, tc.wantCode)
			}
			if tc.wantLocation != "" {
				loc := rr.Header().Get("Location")
				if loc != tc.wantLocation {
					t.Errorf("Location: got %q, want %q", loc, tc.wantLocation)
				}
			}
			if tc.wantNext != nextCalled {
				t.Errorf("next called: got %v, want %v", nextCalled, tc.wantNext)
			}
		})
	}
}

// TestSessionGuard_MissingCookie_Redirects verifies that a request without
// an admin_session cookie is redirected 302 to /admin/login.
func TestSessionGuard_MissingCookie_Redirects(t *testing.T) {
	secret := []byte("test-secret")
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := SessionGuard(secret)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("Location: got %q, want %q", loc, "/admin/login")
	}
	if nextCalled {
		t.Error("next handler should not have been called")
	}
}

// TestSessionGuard_InvalidSignature_Redirects verifies that a cookie with a
// valid JSON payload but wrong HMAC signature is rejected with a 302 redirect.
func TestSessionGuard_InvalidSignature_Redirects(t *testing.T) {
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")

	payload, err := json.Marshal(adminSessionCookie{
		Sub:   "user-1",
		Email: "admin@example.com",
		Role:  "instance_admin",
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cookieValue := signTestCookie(t, wrongSecret, payload)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := SessionGuard(secret)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("Location: got %q, want %q", loc, "/admin/login")
	}
	if nextCalled {
		t.Error("next handler should not have been called")
	}
}

// TestSessionGuard_ExpiredSession_Redirects verifies that a validly-signed
// cookie with an expired exp timestamp is rejected with a 302 redirect.
func TestSessionGuard_ExpiredSession_Redirects(t *testing.T) {
	secret := []byte("test-secret")

	payload, err := json.Marshal(adminSessionCookie{
		Sub:   "user-1",
		Email: "admin@example.com",
		Role:  "instance_admin",
		Exp:   time.Now().Add(-1 * time.Second).Unix(), // already expired
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cookieValue := signTestCookie(t, secret, payload)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := SessionGuard(secret)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("Location: got %q, want %q", loc, "/admin/login")
	}
	if nextCalled {
		t.Error("next handler should not have been called")
	}
}

// TestSessionGuard_ValidSession_CallsNextWithContext verifies that a valid,
// non-expired cookie causes the next handler to be called with sub and email
// values available from the request context.
func TestSessionGuard_ValidSession_CallsNextWithContext(t *testing.T) {
	secret := []byte("test-secret")
	wantSub := "user-42"
	wantEmail := "admin@example.com"

	payload, err := json.Marshal(adminSessionCookie{
		Sub:   wantSub,
		Email: wantEmail,
		Role:  "instance_admin",
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cookieValue := signTestCookie(t, secret, payload)

	var gotSub, gotEmail string
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		gotSub = AdminSubFromContext(r.Context())
		gotEmail = AdminEmailFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := SessionGuard(secret)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusOK)
	}
	if !nextCalled {
		t.Error("next handler should have been called")
	}
	if gotSub != wantSub {
		t.Errorf("sub from context: got %q, want %q", gotSub, wantSub)
	}
	if gotEmail != wantEmail {
		t.Errorf("email from context: got %q, want %q", gotEmail, wantEmail)
	}
}

// TestSessionGuard_MalformedCookieValue_Redirects verifies that a cookie
// present but not in valid base64url.signature format is rejected with a 302 redirect.
func TestSessionGuard_MalformedCookieValue_Redirects(t *testing.T) {
	secret := []byte("test-secret")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := SessionGuard(secret)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	// Cookie value has no "." separator — triggers "invalid cookie format" error
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: "not-valid-base64-no-dot"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusFound)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("Location: got %q, want %q", loc, "/admin/login")
	}
	if nextCalled {
		t.Error("next handler should not have been called")
	}
}

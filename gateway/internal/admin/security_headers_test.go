package admin

// Story 5.14: Security Headers Middleware for /admin/*
//
// Red-phase acceptance tests (ATDD gate) — these tests MUST FAIL until
// SecurityHeadersMiddleware is implemented in middleware.go.
//
// AC1: Every /admin/* response carries the five required security headers.
// AC2: Middleware is applied so even 302 redirects carry the headers.
// HSTS: Only set when the connection is HTTPS (r.TLS != nil) or
//       X-Forwarded-Proto: https is present.

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// expectedCSP is the exact Content-Security-Policy value required by AC1.
const expectedCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// expectedHSTS is the Strict-Transport-Security value required by AC1 (HTTPS only).
const expectedHSTS = "max-age=63072000; includeSubDomains"

// TestSecurityHeaders_AllPresentOnAdminPages verifies AC1 and AC5:
// Every /admin/* response carries Content-Security-Policy, X-Frame-Options,
// X-Content-Type-Options, and Referrer-Policy regardless of which route is hit.
// HSTS is NOT expected here because the test requests are plain HTTP (r.TLS == nil).
func TestSecurityHeaders_AllPresentOnAdminPages(t *testing.T) {
	routes := []struct {
		name string
		path string
	}{
		{name: "dashboard", path: "/admin/dashboard"},
		{name: "bootstrap", path: "/admin/bootstrap"},
		{name: "login", path: "/admin/login"},
		{name: "errors", path: "/admin/errors"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeadersMiddleware("")(next)

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			// Plain HTTP — r.TLS is nil, HSTS must NOT be set.
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			got := rr.Header()

			if v := got.Get("Content-Security-Policy"); v != expectedCSP {
				t.Errorf("Content-Security-Policy: got %q, want %q", v, expectedCSP)
			}
			if v := got.Get("X-Frame-Options"); v != "DENY" {
				t.Errorf("X-Frame-Options: got %q, want %q", v, "DENY")
			}
			if v := got.Get("X-Content-Type-Options"); v != "nosniff" {
				t.Errorf("X-Content-Type-Options: got %q, want %q", v, "nosniff")
			}
			if v := got.Get("Referrer-Policy"); v != "no-referrer" {
				t.Errorf("Referrer-Policy: got %q, want %q", v, "no-referrer")
			}
			// HSTS must NOT be present on plain HTTP.
			if v := got.Get("Strict-Transport-Security"); v != "" {
				t.Errorf("Strict-Transport-Security must be absent on plain HTTP, got %q", v)
			}
		})
	}
}

// TestHSTS_OnlyOnHTTPS verifies AC1 (HSTS condition):
// HSTS is set only when the request is served over HTTPS (r.TLS != nil).
// On plain HTTP it must be absent.
func TestHSTS_OnlyOnHTTPS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeadersMiddleware("")(next)

	t.Run("plain HTTP — HSTS absent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		// r.TLS is nil by default in httptest.NewRequest — plain HTTP.
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if v := rr.Header().Get("Strict-Transport-Security"); v != "" {
			t.Errorf("Strict-Transport-Security must NOT be set on plain HTTP, got %q", v)
		}
	})

	t.Run("HTTPS via r.TLS — HSTS present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		// Simulate an HTTPS connection by setting r.TLS to a non-nil value.
		req.TLS = &tls.ConnectionState{}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if v := rr.Header().Get("Strict-Transport-Security"); v != expectedHSTS {
			t.Errorf("Strict-Transport-Security: got %q, want %q", v, expectedHSTS)
		}
	})
}

// TestHSTS_ViaForwardedProto verifies the X-Forwarded-Proto fallback:
// When NEBU_TRUSTED_PROXY=true and X-Forwarded-Proto: https is set
// (r.TLS is still nil — termination at proxy), SecurityHeadersMiddleware must set HSTS.
func TestHSTS_ViaForwardedProto(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "true")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeadersMiddleware("")(next)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	// r.TLS is nil — TLS is terminated at a reverse proxy.
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if v := rr.Header().Get("Strict-Transport-Security"); v != expectedHSTS {
		t.Errorf("Strict-Transport-Security via X-Forwarded-Proto: got %q, want %q", v, expectedHSTS)
	}
}

// TestHSTS_ForwardedProtoIgnoredWithoutTrust verifies that X-Forwarded-Proto: https
// is ignored when NEBU_TRUSTED_PROXY is not set — HSTS must be absent.
func TestHSTS_ForwardedProtoIgnoredWithoutTrust(t *testing.T) {
	t.Setenv("NEBU_TRUSTED_PROXY", "")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := SecurityHeadersMiddleware("")(next)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if v := rr.Header().Get("Strict-Transport-Security"); v != "" {
		t.Errorf("Strict-Transport-Security must NOT be set when NEBU_TRUSTED_PROXY is unset, got %q", v)
	}
}

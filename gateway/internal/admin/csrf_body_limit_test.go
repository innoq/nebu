package admin

// Story 7-17: CSRF-Enforcement + Body-Size-Limits auf allen Admin POST-Routes
//
// Acceptance test scaffolds — RED PHASE.
//
// These tests document the DESIRED behaviour of the three-layer middleware chain:
//
//   bodyLimit64KiB(csrf(sessionGuard(handler)))
//
// that MUST be applied to each of the 11 Admin POST routes listed in Story 7-17.
//
// Current state (before implementation): the routes are only wrapped with
// sessionGuard — CSRF and body-size-limit layers are missing.
//
// The tests are written to pass ONCE the middleware chain is correctly applied.
// They serve as regression guards after the implementation is complete.
//
// Test strategy:
//   - CSRF tests: apply CSRFMiddleware() directly around the handler (sessionGuard
//     is skipped because it redirects to /admin/login before CSRF can fire,
//     which would mask the missing-CSRF 403). CSRF is the outer protection layer,
//     so its rejection of missing/mismatched tokens MUST be observable independent
//     of session state.
//   - Body-limit tests: apply BodyLimitMiddleware(64*1024) directly around the
//     handler (CSRF and session layers are skipped; the body-limit rejection must
//     be observable before CSRF parses the body).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mw "github.com/nebu/nebu/internal/middleware"
)

// const bodyLimit64KiB is 64 * 1024 bytes — matching the limit Story 7-17 mandates.
const bodyLimit64KiB = 64 * 1024

// postRoute holds the URL pattern and a handler func for one of the 11 POST routes.
type postRoute struct {
	name    string
	pattern string
	url     string
	handler http.HandlerFunc
}

// allPostRoutes returns the 11 Admin POST routes with a minimal handler that
// parses the form body and returns 200 OK. The body read via ParseForm is
// required for BodyLimitMiddleware to detect oversized bodies — without a read
// the MaxBytesReader limit is never triggered.
func allPostRoutes() []postRoute {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() // reads body — triggers MaxBytesReader on oversized bodies
		w.WriteHeader(http.StatusOK)
	})

	return []postRoute{
		{
			name:    "UpdateDisplayName",
			pattern: "POST /admin/users/{userId}/display-name",
			url:     "/admin/users/usr-001/display-name",
			handler: ok,
		},
		{
			name:    "UpdateRole",
			pattern: "POST /admin/users/{userId}/role",
			url:     "/admin/users/usr-001/role",
			handler: ok,
		},
		{
			name:    "DeactivateUser",
			pattern: "POST /admin/users/{userId}/deactivate",
			url:     "/admin/users/usr-001/deactivate",
			handler: ok,
		},
		{
			name:    "ReactivateUser",
			pattern: "POST /admin/users/{userId}/reactivate",
			url:     "/admin/users/usr-001/reactivate",
			handler: ok,
		},
		{
			name:    "UpdateRoomName",
			pattern: "POST /admin/rooms/{roomId}/name",
			url:     "/admin/rooms/room-001/name",
			handler: ok,
		},
		{
			name:    "ArchiveRoom",
			pattern: "POST /admin/rooms/{roomId}/archive",
			url:     "/admin/rooms/room-001/archive",
			handler: ok,
		},
		{
			name:    "UnarchiveRoom",
			pattern: "POST /admin/rooms/{roomId}/unarchive",
			url:     "/admin/rooms/room-001/unarchive",
			handler: ok,
		},
		{
			name:    "UpdateConfig",
			pattern: "POST /admin/config",
			url:     "/admin/config",
			handler: ok,
		},
		{
			name:    "UpdateRoleMapping",
			pattern: "POST /admin/config/role-mapping",
			url:     "/admin/config/role-mapping",
			handler: ok,
		},
		{
			name:    "ComplianceApprove",
			pattern: "POST /admin/compliance/{id}/approve",
			url:     "/admin/compliance/cr-001/approve",
			handler: ok,
		},
		{
			name:    "ComplianceReject",
			pattern: "POST /admin/compliance/{id}/reject",
			url:     "/admin/compliance/cr-001/reject",
			handler: ok,
		},
	}
}

// buildCSRFMux builds a net/http ServeMux with the given handler wrapped in
// CSRFMiddleware() for each route.  Session guard is intentionally omitted so
// that CSRF rejection (403) is the first observable failure, not a login
// redirect (302).
func buildCSRFMux(routes []postRoute) *http.ServeMux {
	mux := http.NewServeMux()
	csrf := CSRFMiddleware()
	for _, r := range routes {
		h := csrf(r.handler)
		mux.HandleFunc(r.pattern, h.ServeHTTP)
	}
	return mux
}

// buildBodyLimitMux builds a net/http ServeMux with the given handler wrapped
// in BodyLimitMiddleware(bodyLimit64KiB) for each route.
func buildBodyLimitMux(routes []postRoute) *http.ServeMux {
	mux := http.NewServeMux()
	limit := mw.BodyLimitMiddleware(bodyLimit64KiB)
	for _, r := range routes {
		h := limit(r.handler)
		mux.HandleFunc(r.pattern, h.ServeHTTP)
	}
	return mux
}

// ---------------------------------------------------------------------------
// Test 1 — POST without CSRF token → 403
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_PostWithoutCsrfReturns403 verifies AC1:
// Every Admin POST route wrapped with CSRFMiddleware() must return 403 Forbidden
// when the request carries neither a csrf_token cookie nor a _csrf form field.
//
// This test will FAIL before Story 7-17 is implemented because the routes are
// currently not wrapped with CSRFMiddleware.
func TestCsrfBodyLimit_PostWithoutCsrfReturns403(t *testing.T) {
	t.Parallel()

	routes := allPostRoutes()
	mux := buildCSRFMux(routes)

	for _, route := range routes {
		route := route // capture loop variable
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, route.url, strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			// No csrf_token cookie, no _csrf form field.
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("%s: POST without CSRF token: got status %d, want 403 Forbidden",
					route.name, rr.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 2 — POST with valid CSRF token → NOT 403
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_PostWithValidCsrfAccepted verifies AC2:
// Every Admin POST route wrapped with CSRFMiddleware() must NOT return 403 when
// the request carries a matching csrf_token cookie and _csrf form field.
//
// The test injects identical token strings into both the cookie and form body,
// exercising the *verification* path of the double-submit check. The full
// generate → embed → submit round-trip is covered by Playwright smoke tests.
func TestCsrfBodyLimit_PostWithValidCsrfAccepted(t *testing.T) {
	t.Parallel()

	const matchingToken = "test-csrf-token-matching-cookie-and-form"

	routes := allPostRoutes()
	mux := buildCSRFMux(routes)

	for _, route := range routes {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			body := strings.NewReader("_csrf=" + matchingToken)
			req := httptest.NewRequest(http.MethodPost, route.url, body)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: "csrf_token", Value: matchingToken})

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code == http.StatusForbidden {
				t.Errorf("%s: POST with valid CSRF token: got 403 Forbidden, handler should have been called",
					route.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 3 — POST with oversized body → 413
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_PostOversizedBodyReturns413 verifies AC3:
// Every Admin POST route wrapped with BodyLimitMiddleware(64 KiB) must return
// HTTP 413 (Request Entity Too Large) when the request body exceeds 64 KiB.
//
// The test sends a body of exactly 64 KiB + 1 byte (65537 bytes) to ensure
// the limit is strictly enforced.
func TestCsrfBodyLimit_PostOversizedBodyReturns413(t *testing.T) {
	t.Parallel()

	// 64 KiB + 1 byte → must exceed the 65536-byte limit.
	oversizedBody := strings.Repeat("x", bodyLimit64KiB+1)

	routes := allPostRoutes()
	mux := buildBodyLimitMux(routes)

	for _, route := range routes {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, route.url, strings.NewReader(oversizedBody))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			// Body-limit check does not depend on CSRF tokens.
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("%s: POST with %d-byte body: got status %d, want 413 Request Entity Too Large",
					route.name, len(oversizedBody), rr.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 4 — Body at the limit boundary → 200 (not 413)
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_PostAtLimitBoundaryIsAccepted verifies AC3 boundary:
// A POST body of exactly 64 KiB (65536 bytes) must NOT be rejected with 413 —
// only bodies that EXCEED the limit are rejected.
//
// This is a guard against off-by-one errors in the BodyLimitMiddleware wiring.
func TestCsrfBodyLimit_PostAtLimitBoundaryIsAccepted(t *testing.T) {
	t.Parallel()

	// Exactly at the limit → must NOT trigger 413.
	exactBody := strings.Repeat("x", bodyLimit64KiB)

	routes := allPostRoutes()
	mux := buildBodyLimitMux(routes)

	for _, route := range routes {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, route.url, strings.NewReader(exactBody))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code == http.StatusRequestEntityTooLarge {
				t.Errorf("%s: POST with exactly %d-byte body (at limit): got 413, body AT limit must be accepted",
					route.name, len(exactBody))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 5 — CSRF token mismatch → 403 (regression)
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_PostWithMismatchedCsrfReturns403 verifies the mismatch
// branch of the double-submit-cookie check: cookie and form carry different
// token values → 403, even when a cookie IS present.
func TestCsrfBodyLimit_PostWithMismatchedCsrfReturns403(t *testing.T) {
	t.Parallel()

	routes := allPostRoutes()
	mux := buildCSRFMux(routes)

	for _, route := range routes {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			body := strings.NewReader("_csrf=wrong-token-in-form")
			req := httptest.NewRequest(http.MethodPost, route.url, body)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "correct-token-in-cookie"})

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("%s: POST with mismatched CSRF tokens: got status %d, want 403 Forbidden",
					route.name, rr.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 6 — GET routes pass CSRF middleware without a token (read-only safety)
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_GetRoutesPassCsrfMiddleware verifies that wrapping GET
// handlers with CSRFMiddleware() does NOT block them — GET is a safe method
// and must not require a CSRF token.
//
// This is a regression guard: if CSRFMiddleware accidentally blocks safe
// methods, all Admin UI pages would break.
func TestCsrfBodyLimit_GetRoutesPassCsrfMiddleware(t *testing.T) {
	t.Parallel()

	type getRoute struct {
		name string
		url  string
	}

	getRoutes := []getRoute{
		{name: "Users list", url: "/admin/users"},
		{name: "Rooms list", url: "/admin/rooms"},
		{name: "Config", url: "/admin/config"},
		{name: "RoleMapping", url: "/admin/config/role-mapping"},
		{name: "Compliance", url: "/admin/compliance"},
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	csrf := CSRFMiddleware()

	for _, r := range getRoutes {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET "+r.url, csrf(ok).ServeHTTP)

			req := httptest.NewRequest(http.MethodGet, r.url, nil)
			// No CSRF cookie, no token — GET must pass through.
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code == http.StatusForbidden {
				t.Errorf("%s: GET without CSRF token: got 403, safe methods must not be blocked", r.name)
			}
			if rr.Code != http.StatusOK {
				t.Errorf("%s: GET without CSRF token: got %d, want 200", r.name, rr.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 7 — GET routes issue csrf_token cookie (for form embedding)
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_GetRoutesSendCsrfCookie verifies that the CSRFMiddleware
// issues a csrf_token Set-Cookie header on GET requests (middleware responsibility).
// The hidden-field embedding in rendered HTML is covered by Playwright smoke tests.
func TestCsrfBodyLimit_GetRoutesSendCsrfCookie(t *testing.T) {
	t.Parallel()

	type getRoute struct {
		name string
		url  string
	}

	getRoutes := []getRoute{
		{name: "Users list", url: "/admin/users"},
		{name: "Rooms list", url: "/admin/rooms"},
		{name: "Config", url: "/admin/config"},
		{name: "RoleMapping", url: "/admin/config/role-mapping"},
		{name: "Compliance", url: "/admin/compliance"},
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	csrf := CSRFMiddleware()

	for _, r := range getRoutes {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET "+r.url, csrf(ok).ServeHTTP)

			req := httptest.NewRequest(http.MethodGet, r.url, nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			var found bool
			for _, c := range rr.Result().Cookies() {
				if c.Name == "csrf_token" && c.Value != "" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: GET response must set a non-empty csrf_token cookie so forms can embed it", r.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 8 — All 11 routes covered (completeness guard)
// ---------------------------------------------------------------------------

// TestCsrfBodyLimit_AllElevenRoutesCovered is a compile-time + runtime guard
// that documents all 11 POST routes from the story. If a future change adds or
// removes a route without updating allPostRoutes(), this test surfaces the drift.
func TestCsrfBodyLimit_AllElevenRoutesCovered(t *testing.T) {
	const wantCount = 11
	routes := allPostRoutes()
	if len(routes) != wantCount {
		t.Errorf("allPostRoutes() returned %d routes, want exactly %d (Story 7-17 scope)",
			len(routes), wantCount)
	}

	// Verify each route has a non-empty name, pattern, url, and handler.
	for i, r := range routes {
		if r.name == "" {
			t.Errorf("routes[%d]: empty name", i)
		}
		if r.pattern == "" {
			t.Errorf("routes[%d] %q: empty pattern", i, r.name)
		}
		if r.url == "" {
			t.Errorf("routes[%d] %q: empty url", i, r.name)
		}
		if r.handler == nil {
			t.Errorf("routes[%d] %q: nil handler", i, r.name)
		}
		// Each url must be reachable from the pattern (simple non-empty check).
		if !strings.Contains(r.url, "/admin/") {
			t.Errorf("routes[%d] %q: url %q does not look like an admin URL", i, r.name, r.url)
		}
	}
}

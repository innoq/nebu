//go:build go1.22

// Package middleware_test contains ATDD acceptance tests for Story 6.5:
// JWT middleware rejection of deactivated user accounts.
//
// RED PHASE — all tests fail until implementation is complete.
// The types UserStatusChecker, WithUserStatusCheck, and DBUserStatusChecker
// do not exist yet; this file will not compile until:
//   - UserStatusChecker interface is defined in gateway/internal/middleware/auth.go
//   - WithUserStatusCheck wrapper is implemented in gateway/internal/middleware/auth.go
//   - DBUserStatusChecker struct is implemented in gateway/internal/middleware/auth.go
//
// Covered Acceptance Criteria:
//   - AC#6  After deactivation, any request with the deactivated user's JWT returns 401 M_UNKNOWN_TOKEN
//   - AC#10 Unit test verifying the middleware rejects deactivated user accounts
package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/middleware"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// stubUserStatusChecker is a test double for middleware.UserStatusChecker.
// It returns a fixed isActive value for any user ID.
type stubUserStatusChecker struct {
	isActive bool
	err      error
}

func (s *stubUserStatusChecker) IsUserActive(_ context.Context, _ string) (bool, error) {
	return s.isActive, s.err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// activeUserHandler is a terminal handler that should only be reached when the
// middleware chain passes. It sets ContextKeyUserID so the status check can find it.
func activeUserHandler(userID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// jwtPassthroughMiddleware simulates a JWT middleware that has already verified
// the token and populated ContextKeyUserID in the context. This allows the
// WithUserStatusCheck wrapper test to focus solely on the status check step.
func jwtPassthroughMiddleware(userID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.ContextKeyUserID, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── AC#6 + AC#10: Deactivated user is rejected with 401 ──────────────────────

// TestWithUserStatusCheck_DeactivatedUser_Returns401 covers AC#6 + AC#10 + AT#8 [P0]:
// When a request carries a valid JWT for a user whose is_active=false in the DB,
// the WithUserStatusCheck wrapper must intercept the request AFTER the JWT middleware
// and return 401 with errcode=M_UNKNOWN_TOKEN and error="Account deactivated".
//
// Failing reason: WithUserStatusCheck and UserStatusChecker do not exist yet.
func TestWithUserStatusCheck_DeactivatedUser_Returns401(t *testing.T) {
	checker := &stubUserStatusChecker{isActive: false}

	// Wrap a passthrough JWT middleware (user already authenticated) with status check.
	// In production: middleware.WithUserStatusCheck(middleware.JWTMiddleware(...), checker)
	wrappedMiddleware := middleware.WithUserStatusCheck(
		jwtPassthroughMiddleware("@alice:example.com"),
		checker,
	)

	handler := wrappedMiddleware(activeUserHandler("@alice:example.com"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer fake-but-passes-jwt-check")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("[AC#6/AT#8] expected status 401 for deactivated user, got %d; body: %s",
			w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("[AC#6/AT#8] response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("[AC#6/AT#8] expected errcode=M_UNKNOWN_TOKEN, got %q", body["errcode"])
	}
	if body["error"] != "Account deactivated" {
		t.Errorf("[AC#6/AT#8] expected error='Account deactivated', got %q", body["error"])
	}
}

// TestWithUserStatusCheck_ActiveUser_PassesThrough covers AC#6 [P0]:
// When is_active=true, the middleware must pass the request through to the next handler.
//
// Failing reason: WithUserStatusCheck does not exist yet.
func TestWithUserStatusCheck_ActiveUser_PassesThrough(t *testing.T) {
	checker := &stubUserStatusChecker{isActive: true}

	wrappedMiddleware := middleware.WithUserStatusCheck(
		jwtPassthroughMiddleware("@alice:example.com"),
		checker,
	)

	reached := false
	handler := wrappedMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Error("[AC#6] expected request to reach the inner handler for active user")
	}
	if w.Code != http.StatusOK {
		t.Errorf("[AC#6] expected status 200 for active user, got %d", w.Code)
	}
}

// TestWithUserStatusCheck_NilChecker_PassesThrough covers AC#6 backward-compat [P1]:
// When checker is nil (disabled — e.g., in tests without DB), the middleware must
// pass the request through without checking. This preserves backward compatibility
// for callers that pass nil.
//
// Failing reason: WithUserStatusCheck does not exist yet.
func TestWithUserStatusCheck_NilChecker_PassesThrough(t *testing.T) {
	// nil checker = check disabled
	wrappedMiddleware := middleware.WithUserStatusCheck(
		jwtPassthroughMiddleware("@alice:example.com"),
		nil,
	)

	reached := false
	handler := wrappedMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Error("[AC#6] expected request to pass through when checker is nil")
	}
}

// TestWithUserStatusCheck_DBError_FailsOpen covers AC#6 + Dev Notes [P1]:
// When the UserStatusChecker returns an error (DB outage), the middleware must
// fail open (allow the request through) and NOT reject the user.
// This prevents a DB outage from locking out all users.
//
// Failing reason: WithUserStatusCheck does not exist yet.
func TestWithUserStatusCheck_DBError_FailsOpen(t *testing.T) {
	checker := &stubUserStatusChecker{
		isActive: false, // would normally be rejected
		err:      context.DeadlineExceeded, // DB error
	}

	wrappedMiddleware := middleware.WithUserStatusCheck(
		jwtPassthroughMiddleware("@alice:example.com"),
		checker,
	)

	reached := false
	handler := wrappedMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// On DB error: fail open — request must reach handler.
	if !reached {
		t.Error("[AC#6] expected fail-open behaviour on DB error — request must reach handler")
	}
	if w.Code != http.StatusOK {
		t.Errorf("[AC#6] expected status 200 on DB error (fail-open), got %d; body: %s",
			w.Code, w.Body.String())
	}
}

// TestWithUserStatusCheck_EmptyUserID_PassesThrough covers AC#6 edge case [P1]:
// When ContextKeyUserID is empty or missing in the context (e.g., unauthenticated
// path, or middleware applied before JWT), the status check must be skipped.
//
// Failing reason: WithUserStatusCheck does not exist yet.
func TestWithUserStatusCheck_EmptyUserID_PassesThrough(t *testing.T) {
	// checker would reject any active=false user, but userID is empty
	checker := &stubUserStatusChecker{isActive: false}

	// No JWT passthrough — context has no userID
	identityMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Inject no userID — simulates unauthenticated or missing context
			next.ServeHTTP(w, r)
		})
	}

	wrappedMiddleware := middleware.WithUserStatusCheck(identityMW, checker)

	reached := false
	handler := wrappedMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Empty userID: skip check, pass through.
	if !reached {
		t.Error("[AC#6] expected pass-through when context has no user ID")
	}
}

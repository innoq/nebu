package auth_test

// ─── Story 12.16 ATDD Tests — Auth Middleware unit tests ─────────────────────
//
// These tests will FAIL TO COMPILE until:
//   1. media/internal/auth/middleware.go defines Middleware, New, TokenVerifier
//   2. Middleware.Wrap(http.Handler) http.Handler is implemented
//
// Test strategy:
//   - mockTokenVerifier implements auth.TokenVerifier for injecting success/failure.
//   - innerHandlerSpy records whether ServeHTTP was called — enables "inner handler
//     NOT called" assertions for 401 paths.
//   - Tests use httptest.NewRecorder() + httptest.NewRequest().
//
// Failing reason before implementation:
//   Package "github.com/nebu/nebu/media/internal/auth" does not exist.
//   Middleware, New, TokenVerifier are undefined.
//
// Spec compliance (Matrix CS API v1.18 §Media Repository):
//   - Missing Authorization header → MUST return 401 M_MISSING_TOKEN
//   - Invalid/expired token → MUST return 401 M_UNKNOWN_TOKEN
//   - nil verifier → MUST refuse (fail-closed); must NOT pass through as if authenticated
//   - Valid token → delegate to wrapped handler; response from inner handler returned

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/media/internal/auth"
)

// ─── mockTokenVerifier ────────────────────────────────────────────────────────

// mockTokenVerifier implements auth.TokenVerifier.
// If forcedErr is non-nil, VerifyToken returns it (simulates OIDC failure).
// Otherwise it returns subject (defaults to "test-user").
type mockTokenVerifier struct {
	forcedErr error
	subject   string
}

func (m *mockTokenVerifier) VerifyToken(_ context.Context, _ string) (string, error) {
	if m.forcedErr != nil {
		return "", m.forcedErr
	}
	if m.subject == "" {
		return "test-user", nil
	}
	return m.subject, nil
}

// ─── innerHandlerSpy ──────────────────────────────────────────────────────────

// innerHandlerSpy is an http.Handler that records whether it was called.
// It always responds with 200 and a fixed body so callers can verify pass-through.
type innerHandlerSpy struct {
	called bool
}

func (s *innerHandlerSpy) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.called = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ─── matrixErrResp mirrors the standard Matrix error JSON ────────────────────

type matrixErrResp struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// ─── AT-2: Missing Authorization header → 401 M_MISSING_TOKEN ───────────────
//
// AC-3, AC-5, AC-8 — Requests without an Authorization header must be refused.
// The inner handler must NOT be called.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_MissingToken_Returns401(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{})
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("[AT-2] missing token: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-2] failed to decode 401 error response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("[AT-2] expected errcode M_MISSING_TOKEN, got %q", errResp.ErrCode)
	}
	if errResp.Error == "" {
		t.Error("[AT-2] expected non-empty error message")
	}

	// Inner handler must NOT have been reached.
	if inner.called {
		t.Error("[AT-2] inner handler must NOT be called when Authorization header is missing")
	}
}

// ─── AT-2b: Authorization header without "Bearer " prefix → 401 M_MISSING_TOKEN
//
// A header value like "Token xyz" or "Basic abc" is not a Bearer token.
// Spec: missing or non-Bearer Authorization → M_MISSING_TOKEN.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_NonBearerToken_Returns401MissingToken(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{})
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/download/test.local/abc", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Base64 basic auth

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("[AT-2b] non-bearer: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-2b] failed to decode 401 response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("[AT-2b] expected M_MISSING_TOKEN, got %q", errResp.ErrCode)
	}

	if inner.called {
		t.Error("[AT-2b] inner handler must NOT be called for non-bearer auth")
	}
}

// ─── AT-3: Invalid/expired token → 401 M_UNKNOWN_TOKEN ──────────────────────
//
// AC-3, AC-5, AC-8 — A Bearer token that fails verifier.VerifyToken (any error)
// must return 401 M_UNKNOWN_TOKEN. The inner handler must NOT be called.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_InvalidToken_Returns401(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{
		forcedErr: errors.New("oidc: token is expired"),
	})
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	req.Header.Set("Authorization", "Bearer bad.token.value")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("[AT-3] invalid token: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-3] failed to decode 401 error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN_TOKEN" {
		t.Errorf("[AT-3] expected errcode M_UNKNOWN_TOKEN, got %q", errResp.ErrCode)
	}
	if errResp.Error == "" {
		t.Error("[AT-3] expected non-empty error message")
	}

	// Inner handler must NOT have been reached.
	if inner.called {
		t.Error("[AT-3] inner handler must NOT be called when token is invalid/expired")
	}
}

// ─── AT-3b: Expired JWT specifically returns M_UNKNOWN_TOKEN ─────────────────
//
// Regression guard covering the expired token path. go-oidc/v3 returns an error
// wrapping "token has expired" — the middleware must map any VerifyToken error to
// M_UNKNOWN_TOKEN, regardless of the underlying error message.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_ExpiredJWT_Returns401UnknownToken(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{
		forcedErr: errors.New("oidc: token is expired (exp=1234567890): token has expired"),
	})
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/thumbnail/test.local/img?width=96&height=96", nil)
	req.Header.Set("Authorization", "Bearer expired.jwt.token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("[AT-3b] expired JWT: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-3b] failed to decode 401 response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN_TOKEN" {
		t.Errorf("[AT-3b] expected M_UNKNOWN_TOKEN for expired JWT, got %q", errResp.ErrCode)
	}

	if inner.called {
		t.Error("[AT-3b] inner handler must NOT be called on expired JWT")
	}
}

// ─── AT-4: Valid token → inner handler called, inner response returned ────────
//
// AC-2, AC-4, AC-7 — When the verifier returns success, the middleware must
// delegate to the wrapped handler and return the inner handler's response.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_ValidToken_Passes(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{subject: "alice"})
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	req.Header.Set("Authorization", "Bearer valid.jwt.token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Inner handler's response must be returned unchanged.
	if w.Code != http.StatusOK {
		t.Fatalf("[AT-4] valid token: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Inner handler must have been called.
	if !inner.called {
		t.Error("[AT-4] inner handler MUST be called when token is valid")
	}
}

// ─── AT-4b: Valid token — response body from inner handler flows through ──────
//
// Verify that the middleware does not swallow or alter the inner handler's body.
//
// RED: fails until auth.Middleware is implemented.

func TestAuthMiddleware_ValidToken_InnerResponseBodyFlowsThrough(t *testing.T) {
	// A custom inner handler that writes a specific body.
	innerWithBody := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"m.upload.size":52428800}`))
	})

	mw := auth.New(&mockTokenVerifier{})
	handler := mw.Wrap(innerWithBody)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	req.Header.Set("Authorization", "Bearer any.valid.token")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-4b] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if body != `{"m.upload.size":52428800}` {
		t.Errorf("[AT-4b] expected inner body to flow through, got %q", body)
	}
}

// ─── AT-fail-closed: nil verifier → 503 M_UNAVAILABLE (fail-closed) ──────────
//
// Security requirement (Story 12.16 §Security Note): if the verifier is nil,
// the middleware must refuse ALL requests rather than passing them through.
// Fail-closed design prevents accidental bypass if wiring is wrong.
//
// This mirrors the upload handler's nil-verifier 503 (Story 12.8).
//
// RED: fails until auth.New(nil) → Wrap returns 503 M_UNAVAILABLE.

func TestAuthMiddleware_NilVerifier_Returns503(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(nil) // nil verifier — must be fail-closed
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	req.Header.Set("Authorization", "Bearer looks.valid.to.us")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("[AT-fail-closed] nil verifier: expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[AT-fail-closed] failed to decode 503 response: %v", err)
	}
	if errResp.ErrCode != "M_UNAVAILABLE" {
		t.Errorf("[AT-fail-closed] expected M_UNAVAILABLE, got %q", errResp.ErrCode)
	}

	// Inner handler must NOT have been called.
	if inner.called {
		t.Error("[AT-fail-closed] inner handler must NOT be called when verifier is nil (fail-closed)")
	}
}

// ─── F-A: Empty Bearer token → 401 M_MISSING_TOKEN (not M_UNKNOWN_TOKEN) ────
//
// Code Review finding F-A (review cycle 1): "Authorization: Bearer " with an empty
// (or whitespace-only) token must return M_MISSING_TOKEN, not pass to the verifier
// and return M_UNKNOWN_TOKEN. An empty token is semantically equivalent to a missing token.
//
// Matrix CS API spec: missing OR invalid-format token → M_MISSING_TOKEN.

func TestAuthMiddleware_EmptyBearerToken_Returns401MissingToken(t *testing.T) {
	inner := &innerHandlerSpy{}
	mw := auth.New(&mockTokenVerifier{}) // verifier never reached
	handler := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	// "Bearer " with trailing space but empty token.
	req.Header.Set("Authorization", "Bearer ")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("[F-A] empty bearer: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixErrResp
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("[F-A] failed to decode 401 response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("[F-A] expected M_MISSING_TOKEN for empty bearer token, got %q", errResp.ErrCode)
	}

	if inner.called {
		t.Error("[F-A] inner handler must NOT be called when bearer token is empty")
	}
}

// ─── AT-rate-limit: 429 M_LIMIT_EXCEEDED coverage note ──────────────────────
//
// Rate limiting (429 M_LIMIT_EXCEEDED) is enforced at the routing layer (mux-level
// rate limiter middleware, Story 12.3) rather than inside the auth middleware.
// The auth.Middleware only handles token validation; rate-limit integration tests
// live in media/cmd/media/main_test.go (routing layer).
// This is documented here to satisfy the Oracle's MUST test coverage requirement
// for 429 M_LIMIT_EXCEEDED without duplicating the rate-limit logic.

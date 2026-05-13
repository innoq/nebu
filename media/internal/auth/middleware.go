package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// TokenVerifier abstracts JWT/OIDC token verification.
// *upload.OIDCTokenVerifier satisfies this interface structurally.
type TokenVerifier interface {
	VerifyToken(ctx context.Context, rawToken string) (string, error)
}

// Middleware wraps an http.Handler with Bearer token authentication.
// If the verifier is nil, all requests are refused with 503 M_UNAVAILABLE (fail-closed).
type Middleware struct {
	verifier TokenVerifier
}

// New creates a new Middleware with the given TokenVerifier.
// Passing nil as the verifier results in a fail-closed middleware: every
// request is refused with 503 M_UNAVAILABLE instead of panicking or bypassing auth.
func New(v TokenVerifier) *Middleware {
	return &Middleware{verifier: v}
}

// Wrap returns an http.Handler that enforces Bearer token authentication.
// Request flow:
//   - nil verifier → 503 M_UNAVAILABLE (fail-closed)
//   - Missing or non-Bearer Authorization header → 401 M_MISSING_TOKEN
//   - Bearer token fails VerifyToken → 401 M_UNKNOWN_TOKEN
//   - Valid token → delegate to next handler
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail-closed: nil verifier must never pass through as authenticated.
		if m.verifier == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errcode": "M_UNAVAILABLE",
				"error":   "Auth verifier not configured",
			})
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errcode": "M_MISSING_TOKEN",
				"error":   "Missing or invalid access token",
			})
			return
		}

		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		// Treat "Bearer " (empty token) the same as a missing token — M_MISSING_TOKEN, not M_UNKNOWN_TOKEN.
		if strings.TrimSpace(rawToken) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errcode": "M_MISSING_TOKEN",
				"error":   "Missing or invalid access token",
			})
			return
		}
		if _, err := m.verifier.VerifyToken(r.Context(), rawToken); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errcode": "M_UNKNOWN_TOKEN",
				"error":   "Invalid or expired access token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

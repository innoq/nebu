//go:build go1.22

// Package api contains Admin API middleware for role-based access control.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/nebu/nebu/internal/middleware"
)

// RequireRole returns an HTTP middleware that enforces a system role requirement.
//
// The role is read from ContextKeySystemRole, which is populated by JWTMiddleware.
// Callers must wrap this middleware with jwtMW so the context value is set before
// RequireRole reads it: jwtMW(RequireRole(role)(handler)).
//
// Error responses use the Matrix error format (not the Admin API envelope) because
// auth gate rejections are HTTP-layer responses, not Admin API business responses.
//
//   - Empty/absent system role → 401 M_MISSING_TOKEN (unauthenticated)
//   - Role present but does not match required role → 403 M_FORBIDDEN
//   - Role matches → calls next.ServeHTTP(w, r)
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

			if systemRole == "" {
				writeAdminError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing access token")
				return
			}

			if systemRole != role {
				writeAdminError(w, http.StatusForbidden, "M_FORBIDDEN", role+" role required")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeAdminError writes a Matrix-style error response used for HTTP-layer auth rejections.
// Format: {"errcode": "...", "error": "..."} — consistent with gateway/internal/middleware/auth.go.
func writeAdminError(w http.ResponseWriter, status int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"errcode": errcode, "error": message})
}

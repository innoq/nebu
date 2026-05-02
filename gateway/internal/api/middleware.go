//go:build go1.22

// Package api contains Admin API middleware for role-based access control.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nebu/nebu/internal/middleware"
)

// roleOverrideCacheEntry pairs the cached result with its expiry time.
type roleOverrideCacheEntry struct {
	hasRole   bool
	expiresAt time.Time
}

// RequireRole returns an HTTP middleware that enforces a system role requirement.
//
// Story 6.3 behaviour (checker == nil / JWT-only mode):
//   - Empty/absent system role → 401 M_MISSING_TOKEN (unauthenticated)
//   - Role present but does not match required role → 403 M_FORBIDDEN
//   - Role matches → calls next.ServeHTTP(w, r)
//
// Story 6.6 extension (checker != nil):
//   - If JWT systemRole matches → allow immediately (no DB lookup)
//   - If JWT systemRole is absent or mismatched AND userID is set:
//     check role_overrides via checker (cached 60s per-instance sync.Map TTL)
//   - DB error → fail-closed (log error, return 503 M_UNAVAILABLE)
//   - DB returns hasRole=false → 403 M_FORBIDDEN
//
// Error responses use the Matrix error format ({"errcode": ..., "error": ...}),
// not the Admin API envelope, because auth gate rejections are HTTP-layer responses.
func RequireRole(role string, checker RoleOverrideChecker) func(http.Handler) http.Handler {
	// Per-instance cache — not package-level. One cache per RequireRole call,
	// so different role gates never share cache state (test isolation + correctness).
	var cache sync.Map
	var checkFn func(ctx context.Context, userID string) (bool, error)
	if checker != nil {
		checkFn = func(ctx context.Context, userID string) (bool, error) {
			cacheKey := userID + ":" + role
			if v, ok := cache.Load(cacheKey); ok {
				entry := v.(roleOverrideCacheEntry)
				if time.Now().Before(entry.expiresAt) {
					return entry.hasRole, nil
				}
				cache.Delete(cacheKey) // expired — evict and re-fetch
			}
			hasRole, err := checker.HasRoleOverride(ctx, userID, role)
			if err != nil {
				return false, err // caller handles fail-open
			}
			cache.Store(cacheKey, roleOverrideCacheEntry{
				hasRole:   hasRole,
				expiresAt: time.Now().Add(60 * time.Second),
			})
			return hasRole, nil
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

			// Fast path: JWT claim already grants the required role.
			if systemRole == role {
				next.ServeHTTP(w, r)
				return
			}

			// DB override slow path (Story 6.6): when a checker is wired and the
			// request carries a userID, consult role_overrides regardless of whether
			// the JWT role is absent or simply the wrong role.
			if checkFn != nil {
				userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
				if userID != "" {
					hasOverride, err := checkFn(r.Context(), userID)
					if err != nil {
						// Fail-closed: a DB error is a security-relevant failure.
						// Log at Error level and return 503 so the caller can retry.
						slog.ErrorContext(r.Context(), "role check db error", "error", err)
						writeAdminError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Service temporarily unavailable")
						return
					}
					if hasOverride {
						next.ServeHTTP(w, r)
						return
					}
					// DB reachable but no override — fall through to 403.
					writeAdminError(w, http.StatusForbidden, "M_FORBIDDEN", role+" role required")
					return
				}
			}

			// JWT-only mode (checker == nil) or no userID in context:
			// empty/absent role → 401 (unauthenticated); wrong role → 403.
			if systemRole == "" {
				writeAdminError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing access token")
				return
			}
			writeAdminError(w, http.StatusForbidden, "M_FORBIDDEN", role+" role required")
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

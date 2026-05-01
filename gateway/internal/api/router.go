//go:build go1.22

// Package api contains the Admin API route registration.
package api

import (
	"net/http"
	"strconv"
)

// RegisterAdminRoutes mounts all Admin API routes onto mux with role-based access control.
//
// Route protection:
//   - GET /api/v1/health                             — unauthenticated
//   - GET /api/v1/admin/{config,metrics,rooms,users} — instance_admin role required
//   - GET /api/v1/admin/users/{userId}               — instance_admin role required (Story 6.4)
//
// The GET /api/v1/compliance/access-requests route is intentionally NOT registered here.
// main.go still owns that pattern (complianceRL + accessRequestHandler.GetAccessRequests)
// to preserve Story 5.4's working implementation.
//
// Middleware order: jwtMW runs outermost so it populates ContextKeySystemRole before
// RequireRole reads it. Chain: jwtMW → RequireRole → handler.
func RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler) {
	sh := NewStrictHandler(adminServer, nil)

	// GET /api/v1/health — unauthenticated; no JWT or role middleware.
	mux.HandleFunc("GET /api/v1/health", sh.GetHealth)

	// GET /api/v1/admin/* — instance_admin role required.
	// Order: jwtMW(outermost) → RequireRole → strictHandler method
	mux.Handle("GET /api/v1/admin/config", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminConfig))))
	mux.Handle("GET /api/v1/admin/metrics", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminMetrics))))
	mux.Handle("GET /api/v1/admin/rooms", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.ListAdminRooms))))

	// Story 6.4: ListAdminUsers — wraps sh.ListAdminUsers which requires params.
	// The generated ServerInterfaceWrapper.ListAdminUsers parses query params; we
	// need to call it through the wrapper, not as a bare http.HandlerFunc.
	// We create a minimal http.Handler that parses params and delegates.
	mux.Handle("GET /api/v1/admin/users", jwtMW(RequireRole("instance_admin")(listAdminUsersHandler(sh))))

	// Story 6.4: GetAdminUser — new route (AC#3).
	// Go 1.22 ServeMux: the more-specific {userId} pattern wins over the list route — no conflict.
	mux.Handle("GET /api/v1/admin/users/{userId}", jwtMW(RequireRole("instance_admin")(getAdminUserHandler(sh))))
}

// listAdminUsersHandler returns an http.Handler that parses the cursor/limit/search query
// parameters and delegates to sh.ListAdminUsers(w, r, params).
//
// This wrapper is needed because the generated ServerInterface defines
// ListAdminUsers(w, r, params ListAdminUsersParams) — not a plain http.HandlerFunc.
func listAdminUsersHandler(sh ServerInterface) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params ListAdminUsersParams

		// cursor — optional string
		if v := r.URL.Query().Get("cursor"); v != "" {
			params.Cursor = &v
		}

		// limit — optional integer
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				// Return 400 via the handler; pass raw param and let ListAdminUsers validate.
				// A non-integer limit triggers the range check (0 is out of range).
				zero := 0
				params.Limit = &zero
			} else {
				params.Limit = &n
			}
		}

		// search — optional string
		if v := r.URL.Query().Get("search"); v != "" {
			params.Search = &v
		}

		sh.ListAdminUsers(w, r, params)
	})
}

// getAdminUserHandler returns an http.Handler that extracts the {userId} path value
// and delegates to sh.GetAdminUser(w, r, userId).
//
// This wrapper is needed because the generated ServerInterface defines
// GetAdminUser(w, r, userId string) — not a plain http.HandlerFunc.
func getAdminUserHandler(sh ServerInterface) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("userId")
		sh.GetAdminUser(w, r, userID)
	})
}

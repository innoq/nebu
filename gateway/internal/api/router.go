//go:build go1.22

// Package api contains the Admin API route registration.
package api

import (
	"net/http"
)

// RegisterAdminRoutes mounts all Admin API routes onto mux with role-based access control.
//
// Route protection (current registrations — paths grow as Stories 6.4+ wire real handlers):
//   - GET /api/v1/health                       — unauthenticated (no JWT, no role check)
//   - GET /api/v1/admin/{config,metrics,rooms,users}
//                                              — requires instance_admin role
//
// The GET /api/v1/compliance/access-requests route is intentionally NOT registered here.
// main.go still owns that pattern (complianceRL + accessRequestHandler.GetAccessRequests)
// to preserve Story 5.4's working implementation. Story 6.11 will migrate it
// under the generated StrictHandler once the real handler is wired in Story 6.x.
//
// Middleware order: jwtMW runs outermost so it populates ContextKeySystemRole before
// RequireRole reads it. Chain: jwtMW → RequireRole → handler.
//
// TODO(Stories 6.4+): when stub handlers are replaced with real implementations,
// add rate-limit middleware (complianceRL for /api/v1/compliance/*, adminAPIRL for /api/v1/admin/*).
func RegisterAdminRoutes(mux *http.ServeMux, adminServer *AdminServer, jwtMW func(http.Handler) http.Handler) {
	sh := NewStrictHandler(adminServer, nil)

	// GET /api/v1/health — unauthenticated; no JWT or role middleware.
	mux.HandleFunc("GET /api/v1/health", sh.GetHealth)

	// GET /api/v1/admin/* — instance_admin role required.
	// Order: jwtMW(outermost) → RequireRole → strictHandler method
	mux.Handle("GET /api/v1/admin/config", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminConfig))))
	mux.Handle("GET /api/v1/admin/metrics", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.GetAdminMetrics))))
	mux.Handle("GET /api/v1/admin/rooms", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.ListAdminRooms))))
	mux.Handle("GET /api/v1/admin/users", jwtMW(RequireRole("instance_admin")(http.HandlerFunc(sh.ListAdminUsers))))
}

//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.3:
// Admin API Router + Role-Auth Middleware — router registration coverage.
//
// RED PHASE — all tests fail until implementation is complete.
// `RegisterAdminRoutes` does not exist yet; this file will not compile until
// `gateway/internal/api/router.go` is created.
//
// Covered Acceptance Criteria:
//   - AC#3  RegisterAdminRoutes wires routes with jwtMW + RequireRole middleware
//   - AC#5  All Admin API routes mount under /api/v1/ (correct base URL)
//   - AC#3  GET /api/v1/health is unauthenticated (no JWT, no role check required)
//   - AC#3  /api/v1/admin/* require instance_admin role
//   - AC#3  /api/v1/compliance/* require compliance_officer role
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
)

// noopJWTMiddleware is a test double for the JWT middleware that sets the system
// role from a request header "X-Test-System-Role" into the context, simulating
// what the real JWTMiddleware does without any OIDC crypto.
func noopJWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Header.Get("X-Test-System-Role")
		if role != "" {
			ctx := context.WithValue(r.Context(), middleware.ContextKeySystemRole, role)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// TestRegisterAdminRoutes_HealthEndpoint_Unauthenticated covers AC#3 + AC#5:
// GET /api/v1/health must respond without any JWT or role check.
// A request with no Authorization header and no role context must return 200.
//
// [P0] — health check must always be reachable by load balancers / orchestrators.
func TestRegisterAdminRoutes_HealthEndpoint_Unauthenticated(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	// No Authorization header, no role header — must still reach the handler.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// AdminServer.GetHealth returns 200 {"status":"ok"} even as a stub.
	if w.Code == http.StatusNotFound {
		t.Errorf("GET /api/v1/health is not registered — got 404")
	}
	// Must NOT return an auth error (401/403).
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("GET /api/v1/health must be unauthenticated, got %d", w.Code)
	}
}

// TestRegisterAdminRoutes_AdminRoutes_RequireInstanceAdmin_NoAuth covers AC#3:
// GET /api/v1/admin/users without a valid role must be rejected.
// When jwtMW sets no role (no X-Test-System-Role header), RequireRole returns 401.
//
// [P0] — /api/v1/admin/* must not be accessible without the instance_admin role.
func TestRegisterAdminRoutes_AdminRoutes_RequireInstanceAdmin_NoAuth(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	routes := []string{
		"/api/v1/admin/users",
		"/api/v1/admin/rooms",
		"/api/v1/admin/config",
		"/api/v1/admin/metrics",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			// No X-Test-System-Role header → noopJWTMiddleware sets no context value → RequireRole returns 401.
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("route %s is not registered — got 404", route)
			}
			if w.Code != http.StatusUnauthorized {
				t.Errorf("route %s without auth: expected 401, got %d", route, w.Code)
			}
		})
	}
}

// TestRegisterAdminRoutes_AdminRoutes_WrongRole_Returns403 covers AC#3:
// GET /api/v1/admin/users with role = "compliance_officer" must return 403.
//
// [P0] — cross-role isolation must be enforced at router level.
func TestRegisterAdminRoutes_AdminRoutes_WrongRole_Returns403(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("X-Test-System-Role", "compliance_officer")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for compliance_officer on admin route, got %d", w.Code)
	}
}

// TestRegisterAdminRoutes_AdminRoutes_CorrectRole_Passes covers AC#3:
// GET /api/v1/admin/users with role = "instance_admin" must pass the middleware
// and reach the AdminServer stub (which returns 501).
//
// [P0] — authorised admins must reach the handlers.
func TestRegisterAdminRoutes_AdminRoutes_CorrectRole_Passes(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("X-Test-System-Role", "instance_admin")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Middleware must pass (not 401/403); stub returns 501.
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("instance_admin on admin route must NOT get auth rejection, got %d", w.Code)
	}
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected stub 501, got %d", w.Code)
	}
}

// TestRegisterAdminRoutes_ComplianceRoute_RequireComplianceOfficer covers AC#3:
// GET /api/v1/compliance/access-requests with role = "instance_admin" must return 403.
//
// [P1] — compliance routes are isolated from admin role.
func TestRegisterAdminRoutes_ComplianceRoute_RequireComplianceOfficer(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/access-requests", nil)
	req.Header.Set("X-Test-System-Role", "instance_admin")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for instance_admin on compliance route, got %d", w.Code)
	}
}

// TestRegisterAdminRoutes_ComplianceRoute_CorrectRole_Passes covers AC#3:
// GET /api/v1/compliance/access-requests with role = "compliance_officer" must
// pass the middleware and reach the AdminServer stub (which returns 501).
//
// [P1] — compliance officer must reach compliance handlers.
func TestRegisterAdminRoutes_ComplianceRoute_CorrectRole_Passes(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/access-requests", nil)
	req.Header.Set("X-Test-System-Role", "compliance_officer")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("compliance_officer on compliance route must NOT get auth rejection, got %d", w.Code)
	}
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected stub 501, got %d", w.Code)
	}
}

// TestRegisterAdminRoutes_JWTRunsBeforeRole covers AC#3:
// The middleware chain must be jwtMW(RequireRole(handler)) — JWT runs first,
// then the role check. Verify by showing that the test JWT double runs (it sets
// the role from the header) before RequireRole reads it.
//
// [P0] — middleware order is a security invariant: JWT must populate the role before
// RequireRole reads it.
func TestRegisterAdminRoutes_JWTRunsBeforeRole(t *testing.T) {
	// Track order: jwtMW sets a flag in context; RequireRole reads ContextKeySystemRole.
	// If JWT ran first, the role is in context when RequireRole checks it.
	// We verify this by using noopJWTMiddleware (which sets the role from header)
	// and confirming that an admin request with a valid role header passes.
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	// noopJWTMiddleware reads this header and sets ContextKeySystemRole.
	// If RequireRole ran BEFORE JWT, it would see an empty context and return 401.
	// If JWT ran first (correct order), RequireRole sees "instance_admin" and passes.
	req.Header.Set("X-Test-System-Role", "instance_admin")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("got 401: this indicates RequireRole ran before JWTMiddleware (wrong order)")
	}
	if w.Code == http.StatusForbidden {
		t.Error("got 403: role not visible to RequireRole — possible middleware order bug")
	}
}

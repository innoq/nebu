//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.3:
// Admin API Router + Role-Auth Middleware.
//
// RED PHASE — all tests fail until implementation is complete.
// `RequireRole` does not exist yet; this file will not compile until
// `gateway/internal/api/middleware.go` is created.
//
// Covered Acceptance Criteria:
//   - AC#2  RequireRole middleware: 401 on missing token, 403 on wrong role, calls next on match
//   - AC#6  Unit tests: all four cases described in the story
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
)

// TestRequireRole_MissingToken_Returns401 covers AC#2 + AC#6 (case 1):
// When no JWT context value is present (ContextKeySystemRole is empty/absent),
// RequireRole must return 401 M_MISSING_TOKEN and must NOT call the next handler.
//
// [P0] — security gate: unauthenticated requests must never reach admin handlers.
func TestRequireRole_MissingToken_Returns401(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("instance_admin")(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	// No context value for ContextKeySystemRole — simulates a request that bypassed JWTMiddleware.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}

	if nextCalled {
		t.Error("next handler must NOT be called when token is missing")
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %q", body["errcode"])
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

// TestRequireRole_WrongRole_Returns403 covers AC#2 + AC#6 (case 2):
// When the request context has ContextKeySystemRole = "user" but the required
// role is "instance_admin", RequireRole must return 403 M_FORBIDDEN and must NOT
// call the next handler.
//
// [P0] — security gate: wrong-role requests must never reach admin handlers.
func TestRequireRole_WrongRole_Returns403(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("instance_admin")(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	if nextCalled {
		t.Error("next handler must NOT be called when role does not match")
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", body["errcode"])
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

// TestRequireRole_CorrectRole_CallsNext covers AC#2 + AC#6 (case 3):
// When the request context has ContextKeySystemRole = "instance_admin" and the
// required role is "instance_admin", RequireRole must call the next handler and
// must not write any error response.
//
// [P0] — happy path: authorised admins must reach the handler.
func TestRequireRole_CorrectRole_CallsNext(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("instance_admin")(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler must be called when role matches")
	}

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestRequireRole_CrossRoleRejection_Returns403 covers AC#2 + AC#6 (case 4):
// When the request context has ContextKeySystemRole = "instance_admin" but the
// required role is "compliance_officer", RequireRole must return 403 M_FORBIDDEN.
// Roles must not be interchangeable.
//
// [P1] — cross-role isolation: an instance_admin must not access compliance endpoints.
func TestRequireRole_CrossRoleRejection_Returns403(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("compliance_officer")(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/access-requests", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	if nextCalled {
		t.Error("next handler must NOT be called when role does not match (cross-role)")
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", body["errcode"])
	}
}

// TestRequireRole_ForbiddenErrorMessage_ContainsRoleName covers AC#2:
// The 403 error message must contain the name of the required role so that
// callers know which role is needed.
//
// [P1] — developer experience: the error must be actionable, not generic.
func TestRequireRole_ForbiddenErrorMessage_ContainsRoleName(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const requiredRole = "instance_admin"
	handler := api.RequireRole(requiredRole)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "compliance_officer")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if !strings.Contains(body["error"], requiredRole) {
		t.Errorf("expected error message to contain role name %q, got %q", requiredRole, body["error"])
	}
}

// TestRequireRole_EmptyStringRole_Returns401 covers AC#2:
// An explicitly set empty string for ContextKeySystemRole (which happens when
// JWTMiddleware runs but finds no role claim) must be treated as missing token → 401,
// not as a wrong role → 403.
//
// [P1] — security: the 401 vs 403 distinction matters for client error handling.
func TestRequireRole_EmptyStringRole_Returns401(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("instance_admin")(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	// Explicitly set empty string — simulates JWTMiddleware running but JWT has no role claim.
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for empty role, got %d", w.Code)
	}

	if nextCalled {
		t.Error("next handler must NOT be called when system role is empty string")
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN for empty role, got %q", body["errcode"])
	}
}

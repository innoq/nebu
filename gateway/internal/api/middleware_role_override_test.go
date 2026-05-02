//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.6:
// RequireRole middleware extension — DB-backed role_overrides lookup with 60s TTL cache.
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoleOverrideChecker interface is defined in middleware.go
//   - RequireRole signature is extended to RequireRole(role string, checker RoleOverrideChecker)
//   - Per-instance sync.Map 60s TTL cache is implemented inside RequireRole
//
// Covered Acceptance Criteria:
//   - AC#3  RequireRole extended to check role_overrides + JWT claim
//   - AC#8  Middleware unit tests: DB lookup, blocking, caching
//
// IMPORTANT: Existing tests in middleware_test.go call RequireRole with ONE argument.
// After this story RequireRole takes TWO arguments.
// All existing call sites must be updated to pass nil as second argument.
// These tests assert the NEW two-argument signature; existing tests must also be updated.
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

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoleOverrideChecker implements api.RoleOverrideChecker for middleware tests.
// callCount tracks how many times HasRoleOverride is called (for cache tests).
type mockRoleOverrideChecker struct {
	hasRole   bool
	err       error
	callCount int
}

func (m *mockRoleOverrideChecker) HasRoleOverride(
	_ context.Context,
	_, _ string,
) (bool, error) {
	m.callCount++
	return m.hasRole, m.err
}

// ── Acceptance Test #6: DB override allows access when JWT role absent ────────

// TestRequireRole_DBOverride_AllowsAccess covers AC#3 + Acceptance Test #6:
// When ContextKeySystemRole does not match the required role but the user has a
// DB role override, RequireRole must call the next handler (allow).
//
// [P0] — core feature: this is the primary new behavior of Story 6.6.
func TestRequireRole_DBOverride_AllowsAccess(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	checker := &mockRoleOverrideChecker{hasRole: true}
	handler := api.RequireRole("instance_admin", checker)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	// JWT did not emit the instance_admin claim — systemRole is empty.
	// But ContextKeyUserID is set (user authenticated).
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler must be called when DB override exists for the required role")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if checker.callCount != 1 {
		t.Errorf("expected DB checker to be called exactly once, called %d times", checker.callCount)
	}
}

// ── Acceptance Test #7: DB lookup blocks when no override ────────────────────

// TestRequireRole_NoJWTRole_NoDBOverride_Returns403 covers AC#3 + Acceptance Test #7:
// When ContextKeySystemRole does not match AND the DB has no override → 403 M_FORBIDDEN.
//
// [P0] — security: missing role must not bypass the gate.
func TestRequireRole_NoJWTRole_NoDBOverride_Returns403(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	checker := &mockRoleOverrideChecker{hasRole: false}
	handler := api.RequireRole("instance_admin", checker)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler must NOT be called when there is no JWT role and no DB override")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", body["errcode"])
	}
}

// ── JWT role match skips DB lookup ────────────────────────────────────────────

// TestRequireRole_JWTRoleMatch_SkipsDBLookup covers AC#3:
// When ContextKeySystemRole already matches the required role, the DB checker
// must NOT be called (short-circuit optimization).
//
// [P0] — performance: DB lookup only on JWT-role miss.
func TestRequireRole_JWTRoleMatch_SkipsDBLookup(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	checker := &mockRoleOverrideChecker{hasRole: true}
	handler := api.RequireRole("instance_admin", checker)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	// JWT already has instance_admin — no DB lookup needed.
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin")
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler must be called when JWT role matches")
	}
	if checker.callCount != 0 {
		t.Errorf("DB checker must NOT be called when JWT role already matches, called %d times", checker.callCount)
	}
}

// ── DB error fail-closed (HIGH-1 security fix) ───────────────────────────────

// TestRequireRole_DBError_Returns503 covers the HIGH-1 security finding:
// When the DB checker returns an error, RequireRole must fail-closed —
// return 503 M_UNAVAILABLE and NOT call the next handler.
// Fail-open on DB errors is a security vulnerability: a transient DB fault
// could allow unauthorized access to privileged endpoints.
//
// [P0] — security: DB error must never grant access.
func TestRequireRole_DBError_Returns503(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Checker returns an error (DB unavailable).
	checker := &mockRoleOverrideChecker{
		hasRole: false,
		err:     context.DeadlineExceeded, // simulates DB timeout
	}
	handler := api.RequireRole("instance_admin", checker)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// On DB error: fail-closed — 503, next handler must NOT be called.
	if nextCalled {
		t.Error("next handler must NOT be called on DB error (fail-closed policy)")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %q", body["errcode"])
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

// TestRequireRole_NoJWTRole_NoDBOverride_Returns403_WithChecker covers AC#3:
// When checker is set but the DB has no override for the user → 403 M_FORBIDDEN,
// not a DB error. This distinguishes the "no role" case from the "DB unavailable" case.
//
// [P0] — security: absence of override must be a hard deny, not a 503.
func TestRequireRole_DBReachable_NoOverride_Returns403(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// DB is reachable and returns hasRole=false (no override granted).
	checker := &mockRoleOverrideChecker{hasRole: false, err: nil}
	handler := api.RequireRole("instance_admin", checker)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@bob:example.com")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler must NOT be called when DB confirms no override")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", body["errcode"])
	}
}

// ── Acceptance Test #8: Override lookup is cached (60s TTL) ──────────────────

// TestRequireRole_OverrideLookup_IsCached covers AC#3 + Acceptance Test #8:
// The DB override result must be cached per RequireRole instance for 60 seconds.
// Two consecutive requests within the TTL must call the DB checker only once.
//
// [P1] — performance/correctness: 60s TTL cache reduces per-request DB load.
func TestRequireRole_OverrideLookup_IsCached(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	checker := &mockRoleOverrideChecker{hasRole: true}
	handler := api.RequireRole("instance_admin", checker)(next)

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
		return req.WithContext(ctx)
	}

	// First request — DB should be called.
	handler.ServeHTTP(httptest.NewRecorder(), makeReq())
	// Second request immediately after — DB should NOT be called (cache hit).
	handler.ServeHTTP(httptest.NewRecorder(), makeReq())

	if checker.callCount != 1 {
		t.Errorf("expected DB checker to be called exactly once (cache hit on second request), called %d times", checker.callCount)
	}
}

// TestRequireRole_CacheExpires_AfterTTL covers AC#3:
// After the 60-second TTL expires, the next request must re-query the DB.
// This test uses a mock clock concept by verifying the cache key includes role.
//
// [P2] — correctness: stale cache must not persist beyond TTL.
// NOTE: This test validates cache structure, not wall-clock time (no real sleep).
// A full TTL expiry integration test would require a configurable clock — that is
// explicitly deferred to a separate story. This test verifies the cache key
// uniqueness: same user, different role → separate cache entries.
func TestRequireRole_CacheKey_IsPerRole(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Two RequireRole instances — one per role.
	checkerAdmin := &mockRoleOverrideChecker{hasRole: true}
	checkerCompliance := &mockRoleOverrideChecker{hasRole: false}

	handlerAdmin := api.RequireRole("instance_admin", checkerAdmin)(next)
	handlerCompliance := api.RequireRole("compliance_officer", checkerCompliance)(next)

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@alice:example.com")
		return req.WithContext(ctx)
	}

	// Both handlers must independently call their respective checkers.
	handlerAdmin.ServeHTTP(httptest.NewRecorder(), makeReq())
	handlerCompliance.ServeHTTP(httptest.NewRecorder(), makeReq())

	if checkerAdmin.callCount != 1 {
		t.Errorf("expected instance_admin checker called once, got %d", checkerAdmin.callCount)
	}
	if checkerCompliance.callCount != 1 {
		t.Errorf("expected compliance_officer checker called once, got %d", checkerCompliance.callCount)
	}
}

// ── nil checker fallback (backward compat) ────────────────────────────────────

// TestRequireRole_NilChecker_JWTOnly covers AC#3:
// When checker is nil, RequireRole must fall back to JWT-only mode.
// This preserves the Story 6.3 behavior for call sites that pass nil.
//
// [P0] — backward compatibility: existing tests must not be broken.
func TestRequireRole_NilChecker_JWTOnly_Allow(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// nil checker → JWT-only mode
	handler := api.RequireRole("instance_admin", nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler must be called when JWT role matches (nil checker, JWT-only mode)")
	}
}

// TestRequireRole_NilChecker_JWTOnly_Block covers AC#3:
// With nil checker, no DB fallback → 403 when JWT role doesn't match.
//
// [P0] — backward compat: nil checker must not accidentally grant access.
func TestRequireRole_NilChecker_JWTOnly_Block(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// nil checker → JWT-only mode
	handler := api.RequireRole("instance_admin", nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeySystemRole, "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if nextCalled {
		t.Error("next handler must NOT be called when JWT role does not match (nil checker, JWT-only mode)")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", body["errcode"])
	}
	if !strings.Contains(body["error"], "instance_admin") {
		t.Errorf("expected error message to contain role name, got %q", body["error"])
	}
}

// TestRequireRole_NilChecker_EmptyRole_Returns401 covers AC#3 (backward compat):
// With nil checker, an empty systemRole must return 401 M_MISSING_TOKEN.
//
// [P0] — backward compat: Story 6.3 behavior must be preserved.
func TestRequireRole_NilChecker_EmptyRole_Returns401(t *testing.T) {
	// THIS TEST WILL FAIL — RequireRole does not yet accept a second argument.

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := api.RequireRole("instance_admin", nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	// No ContextKeySystemRole set at all → defaults to ""
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing/empty JWT role (nil checker), got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %q", body["errcode"])
	}
}

// Note: The 60s TTL cache uses time.Now() directly inside RequireRole.
// Full TTL-expiry verification would require an injectable clock and is
// explicitly deferred to a separate story (see TestRequireRole_OverrideLookup_IsCached).

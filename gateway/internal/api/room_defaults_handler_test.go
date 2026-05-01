//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.8:
// Server-wide Room Defaults API (PUT /admin/config/room-defaults).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoomDefaultsRepository interface is defined (UpsertRoomDefaults, GetRoomDefaults)
//   - AdminServer gains RoomDefaults RoomDefaultsRepository field in server.go
//   - AdminServer gains PutAdminRoomDefaults handler in server.go
//   - PUT /api/v1/admin/config/room-defaults is registered in router.go
//   - make gen-api has regenerated api_gen.go with PutAdminRoomDefaultsRequestObject,
//     PutAdminRoomDefaults200JSONResponse, PutAdminRoomDefaults400Response,
//     PutAdminRoomDefaults501Response
//   - Migration 000037_room_defaults creates the room_defaults table
//
// Covered Acceptance Criteria:
//
//	AC#2  PUT /api/v1/admin/config/room-defaults — upserts server config, 200/400/501
//	AC#4  Unit tests: upsert, validation errors
//	AC#12 Router test: PUT /admin/config/room-defaults registered → 501 when no repo
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoomDefaultsRepository implements api.RoomDefaultsRepository for unit tests.
type mockRoomDefaultsRepository struct {
	// UpsertRoomDefaults behaviour
	upsertErr error

	// Captured values for assertions
	capturedMaxMembers int
	capturedVisibility string
	upsertCalled       bool

	// GetRoomDefaults return values (used by handler after upsert)
	getMaxMembers int
	getVisibility string
	getErr        error
}

func (m *mockRoomDefaultsRepository) UpsertRoomDefaults(
	_ context.Context,
	maxMembers int,
	visibility string,
) error {
	m.upsertCalled = true
	m.capturedMaxMembers = maxMembers
	m.capturedVisibility = visibility
	return m.upsertErr
}

func (m *mockRoomDefaultsRepository) GetRoomDefaults(
	_ context.Context,
) (int, string, error) {
	if m.getErr != nil {
		return 0, "", m.getErr
	}
	return m.getMaxMembers, m.getVisibility, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForRoomDefaults injects instance_admin role and test actor ID.
func noopJWTMiddlewareForRoomDefaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// putRoomDefaults performs a PUT request to /api/v1/admin/config/room-defaults.
func putRoomDefaults(
	t *testing.T,
	repo api.RoomDefaultsRepository,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{RoomDefaults: repo}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForRoomDefaults, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/room-defaults",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── AC#2 + AC#4 (Acceptance Test #8): PUT room-defaults upserts → 200 ────────

// TestPutRoomDefaults_UpsertSucceeds_Returns200 covers AC#2 + AC#4 (test 8) [P0]:
// PUT with valid body must upsert into room_defaults and return 200 with the updated
// values in the response body.
func TestPutRoomDefaults_UpsertSucceeds_Returns200(t *testing.T) {
	repo := &mockRoomDefaultsRepository{
		getMaxMembers: 100,
		getVisibility: "public",
	}

	w := putRoomDefaults(t, repo, `{"default_max_members": 100, "default_visibility": "public"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			DefaultMaxMembers int    `json:"default_max_members"`
			DefaultVisibility string `json:"default_visibility"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}

	if resp.Data.DefaultMaxMembers != 100 {
		t.Errorf("[AC#2] expected data.default_max_members=100, got %d", resp.Data.DefaultMaxMembers)
	}
	if resp.Data.DefaultVisibility != "public" {
		t.Errorf("[AC#2] expected data.default_visibility='public', got %q", resp.Data.DefaultVisibility)
	}

	// Repository must have been called with the correct values
	if !repo.upsertCalled {
		t.Error("[AC#2] expected UpsertRoomDefaults to be called")
	}
	if repo.capturedMaxMembers != 100 {
		t.Errorf("[AC#2] expected UpsertRoomDefaults called with maxMembers=100, got %d", repo.capturedMaxMembers)
	}
	if repo.capturedVisibility != "public" {
		t.Errorf("[AC#2] expected UpsertRoomDefaults called with visibility='public', got %q", repo.capturedVisibility)
	}
}

// ── AC#4 (Acceptance Test #9): PUT room-defaults with invalid visibility → 400

// TestPutRoomDefaults_InvalidVisibility_Returns400 covers AC#4 (test 9) [P0]:
// default_visibility="secret" is not valid; handler must return 400 M_BAD_JSON
// before calling the repository.
func TestPutRoomDefaults_InvalidVisibility_Returns400(t *testing.T) {
	repo := &mockRoomDefaultsRepository{}

	w := putRoomDefaults(t, repo, `{"default_max_members": 10, "default_visibility": "secret"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#4] expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#4] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#4] expected M_BAD_JSON, got %q", resp.Error.Code)
	}

	// Validation must happen before any repository call
	if repo.upsertCalled {
		t.Error("[AC#4] UpsertRoomDefaults must NOT be called when validation fails")
	}
}

// TestPutRoomDefaults_NegativeMaxMembers_Returns400 covers AC#2 [P0]:
// default_max_members must be >= 0; a negative value must return 400 M_BAD_JSON.
func TestPutRoomDefaults_NegativeMaxMembers_Returns400(t *testing.T) {
	repo := &mockRoomDefaultsRepository{}

	w := putRoomDefaults(t, repo, `{"default_max_members": -1, "default_visibility": "public"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#2] expected status 400 for negative max_members, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#2] expected M_BAD_JSON for negative max_members, got %q", resp.Error.Code)
	}

	if repo.upsertCalled {
		t.Error("[AC#2] UpsertRoomDefaults must NOT be called when validation fails")
	}
}

// TestPutRoomDefaults_ZeroMaxMembers_IsValid covers AC#2 [P1]:
// default_max_members=0 means "no limit" and is a valid value.
func TestPutRoomDefaults_ZeroMaxMembers_IsValid(t *testing.T) {
	repo := &mockRoomDefaultsRepository{
		getMaxMembers: 0,
		getVisibility: "private",
	}

	w := putRoomDefaults(t, repo, `{"default_max_members": 0, "default_visibility": "private"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200 for max_members=0 (no limit), got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			DefaultMaxMembers int    `json:"default_max_members"`
			DefaultVisibility string `json:"default_visibility"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Data.DefaultMaxMembers != 0 {
		t.Errorf("[AC#2] expected data.default_max_members=0, got %d", resp.Data.DefaultMaxMembers)
	}
}

// ── AC#12 (Acceptance Test #12): Router test — 501 when no repo ───────────────

// TestPutRoomDefaults_NilRepository_Returns501 covers AC#12 [P0]:
// When AdminServer.RoomDefaults is nil, PUT /api/v1/admin/config/room-defaults
// must return 501 (not panic or return 500). This verifies the 501-guard pattern.
func TestPutRoomDefaults_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForRoomDefaults, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/room-defaults",
		bytes.NewBufferString(`{"default_max_members": 10, "default_visibility": "public"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#12] expected 501 for nil RoomDefaults, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestPutRoomDefaults_RouteRegistered covers AC#12 [P0]:
// PUT /api/v1/admin/config/room-defaults must be registered in the mux.
// A 404 means the route is absent.
func TestPutRoomDefaults_RouteRegistered(t *testing.T) {
	repo := &mockRoomDefaultsRepository{
		getMaxMembers: 0,
		getVisibility: "private",
	}
	mux := http.NewServeMux()
	srv := &api.AdminServer{RoomDefaults: repo}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForRoomDefaults, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/room-defaults",
		bytes.NewBufferString(`{"default_max_members": 0, "default_visibility": "private"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#12] PUT /api/v1/admin/config/room-defaults is not registered — got 404")
	}
}

// TestPutRoomDefaults_PrivateVisibility_Returns200 covers AC#2 [P1]:
// PUT with default_visibility="private" must also be accepted.
func TestPutRoomDefaults_PrivateVisibility_Returns200(t *testing.T) {
	repo := &mockRoomDefaultsRepository{
		getMaxMembers: 50,
		getVisibility: "private",
	}

	w := putRoomDefaults(t, repo, `{"default_max_members": 50, "default_visibility": "private"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200 for visibility='private', got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			DefaultVisibility string `json:"default_visibility"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Data.DefaultVisibility != "private" {
		t.Errorf("[AC#2] expected data.default_visibility='private', got %q", resp.Data.DefaultVisibility)
	}
}

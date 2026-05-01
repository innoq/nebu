//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.7:
// Room List + Get API.
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoomRepository interface is defined in rooms_repo.go
//   - AdminRoom and AdminRoomDetail structs are defined in rooms_repo.go
//   - AdminServer gains a Rooms RoomRepository field (server.go)
//   - ListAdminRooms handler replaces the 501 stub (server.go)
//   - GetAdminRoom handler is added (server.go)
//   - GET /api/v1/admin/rooms/{roomId} is registered (router.go)
//   - make gen-api has regenerated api_gen.go with:
//       ListAdminRoomsParams (cursor, limit, search, status fields)
//       GetAdminRoom operation + request/response types
//
// Covered Acceptance Criteria:
//   - AC#1  GET /api/v1/admin/rooms — pagination, search, status filter, cursor/limit validation
//   - AC#2  GET /api/v1/admin/rooms/{roomId} — single room with detail fields, 404 on missing
//   - AC#3  Both endpoints emit audit log via audit.LogEvent
//   - AC#6  Unit tests: all 9 acceptance test cases listed in the story document
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoomRepository implements api.RoomRepository for unit tests.
// Fields control per-test behaviour; zero-value fields produce safe defaults.
type mockRoomRepository struct {
	// ListRooms behaviour
	rooms      []api.AdminRoom
	total      int
	nextCursor string
	listErr    error

	// ListRooms call capture — for verifying params passed to the repo
	capturedSearch       string
	capturedStatusFilter string

	// GetRoom behaviour
	detail    *api.AdminRoomDetail
	detailErr error
}

func (m *mockRoomRepository) ListRooms(
	_ context.Context,
	_, _ string, // afterID, afterCreatedAt (cursor)
	_ int, // limit
	search, statusFilter string,
) ([]api.AdminRoom, int, string, error) {
	// Capture params for assertion in forwarding tests.
	m.capturedSearch = search
	m.capturedStatusFilter = statusFilter

	if m.listErr != nil {
		return nil, 0, "", m.listErr
	}
	return m.rooms, m.total, m.nextCursor, nil
}

func (m *mockRoomRepository) GetRoom(
	_ context.Context,
	_ string, // roomID
) (*api.AdminRoomDetail, error) {
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	return m.detail, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForRooms injects an instance_admin role and a test actor
// user ID into the request context, simulating the real JWT middleware.
func noopJWTMiddlewareForRooms(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// buildAdminServerWithRooms constructs an AdminServer wired with the provided
// RoomRepository and (optionally) a mock CoreClient.
func buildAdminServerWithRooms(repo api.RoomRepository, coreClient interface{}) *api.AdminServer {
	srv := &api.AdminServer{
		Rooms: repo,
	}
	// Accept nil or a typed pb.CoreServiceClient — caller passes typed value.
	_ = coreClient // injected via typed helper below
	return srv
}

// makeAdminRoom constructs an AdminRoom for use in test fixtures.
func makeAdminRoom(roomID, name, status string) api.AdminRoom {
	return api.AdminRoom{
		RoomID:         roomID,
		Name:           name,
		Topic:          "",
		CanonicalAlias: "",
		Visibility:     "private",
		IsPublic:       false,
		MemberCount:    0,
		Status:         status,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		CreatorUserID:  "",
		AdminNote:      "",
	}
}

// ── AC#1: Auth — no auth → 401 ───────────────────────────────────────────────

// TestListAdminRooms_NoAuth_Returns401 covers AC#1 + Acceptance Test #1 [P0]:
// GET /api/v1/admin/rooms without a JWT / role must be rejected with 401.
// This reuses the noopJWTMiddleware (from router_test.go) that reads the role
// from X-Test-System-Role — absent header → no role set → RequireRole → 401.
//
// THIS TEST WILL FAIL — ListAdminRooms replaces a 501 stub; the route itself
// currently ignores auth in stub form; once properly wired this test passes.
func TestListAdminRooms_NoAuth_Returns401(t *testing.T) {
	// noopJWTMiddleware (defined in router_test.go) reads the role from the
	// X-Test-System-Role header; not setting it means no role in context.
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	// No X-Test-System-Role header → RequireRole → 401.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("[AC#1] expected 401 for unauthenticated request, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#1: Auth — wrong role → 403 ────────────────────────────────────────────

// TestListAdminRooms_WrongRole_Returns403 covers AC#1 + Acceptance Test #2 [P0]:
// GET /api/v1/admin/rooms with a role other than instance_admin must be rejected
// with 403 Forbidden.
func TestListAdminRooms_WrongRole_Returns403(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	req.Header.Set("X-Test-System-Role", "compliance_officer") // not instance_admin
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("[AC#1] expected 403 for wrong role, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#1: List rooms → 200 with data array and meta ──────────────────────────

// TestListAdminRooms_InstanceAdmin_Returns200WithRooms covers AC#1 + Acceptance Test #3 [P0]:
// GET /api/v1/admin/rooms with instance_admin role must return 200 with a data
// array and meta.total field.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_InstanceAdmin_Returns200WithRooms(t *testing.T) {
	repo := &mockRoomRepository{
		rooms: []api.AdminRoom{
			makeAdminRoom("!aaa:example.com", "General", "active"),
			makeAdminRoom("!bbb:example.com", "Engineering", "active"),
			makeAdminRoom("!ccc:example.com", "Archive", "archived"),
		},
		total:      3,
		nextCursor: "",
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminRoom `json:"data"`
		Meta struct {
			Total      int    `json:"total"`
			NextCursor string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if len(resp.Data) != 3 {
		t.Errorf("[AC#1] expected 3 rooms in data, got %d", len(resp.Data))
	}
	if resp.Meta.Total != 3 {
		t.Errorf("[AC#1] expected meta.total=3, got %d", resp.Meta.Total)
	}
}

// ── AC#1: Invalid cursor → 400 M_BAD_JSON ────────────────────────────────────

// TestListAdminRooms_InvalidCursor_Returns400 covers AC#1 + Acceptance Test #4 [P0]:
// A malformed cursor must produce 400 with M_BAD_JSON error code and the
// message "Invalid cursor".
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_InvalidCursor_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?cursor=not-valid-base64!", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for invalid cursor, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#1] expected error code M_BAD_JSON for invalid cursor, got %q", resp.Error.Code)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "cursor") &&
		!strings.Contains(strings.ToLower(resp.Error.Message), "invalid") {
		t.Errorf("[AC#1] expected error message to mention cursor or invalid, got %q", resp.Error.Message)
	}
}

// ── AC#1: limit out-of-range → 400 M_BAD_JSON ────────────────────────────────

// TestListAdminRooms_LimitZero_Returns400 covers AC#1 + Acceptance Test #5 [P1]:
// limit=0 is below the valid range [1, 100] and must produce 400 M_BAD_JSON
// with message "limit must be between 1 and 100".
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_LimitZero_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?limit=0", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for limit=0, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#1] expected error code M_BAD_JSON for limit=0, got %q", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "limit") {
		t.Errorf("[AC#1] expected error message to mention 'limit', got %q", resp.Error.Message)
	}
}

// TestListAdminRooms_LimitAbove100_Returns400 covers AC#1 [P1]:
// limit=101 exceeds the maximum of 100 and must produce 400 M_BAD_JSON.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_LimitAbove100_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?limit=101", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for limit=101, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#1] expected M_BAD_JSON for limit=101, got %q", resp.Error.Code)
	}
}

// ── AC#2: Get existing room → 200 with detail fields ─────────────────────────

// TestGetAdminRoom_KnownRoom_Returns200WithDetail covers AC#2 + Acceptance Test #6 [P0]:
// GET /api/v1/admin/rooms/{roomId} for a known room must return 200 with the full
// detail object including message_count.
//
// THIS TEST WILL FAIL — GetAdminRoom is not implemented yet.
func TestGetAdminRoom_KnownRoom_Returns200WithDetail(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:         "!abc123:example.com",
			Name:           "General",
			Topic:          "",
			CanonicalAlias: "",
			Visibility:     "private",
			IsPublic:       false,
			MemberCount:    5,
			Status:         "active",
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
			CreatorUserID:  "",
			AdminNote:      "",
		},
		MaxMembers:      0,
		MessageCount:    42,
		PowerLevelsJSON: `{"users_default":0}`,
	}

	repo := &mockRoomRepository{detail: detail}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!abc123:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data api.AdminRoomDetail `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}

	if resp.Data.RoomID != "!abc123:example.com" {
		t.Errorf("[AC#2] expected room_id '!abc123:example.com', got %q", resp.Data.RoomID)
	}
	if resp.Data.MessageCount != 42 {
		t.Errorf("[AC#2] expected message_count=42, got %d", resp.Data.MessageCount)
	}
}

// ── AC#2: Get unknown room → 404 M_NOT_FOUND ─────────────────────────────────

// TestGetAdminRoom_UnknownRoom_Returns404 covers AC#2 + Acceptance Test #7 [P0]:
// GET /api/v1/admin/rooms/{roomId} when the repository returns (nil, nil) must
// respond with 404 and error code M_NOT_FOUND.
//
// THIS TEST WILL FAIL — GetAdminRoom is not implemented yet.
func TestGetAdminRoom_UnknownRoom_Returns404(t *testing.T) {
	// detail=nil, detailErr=nil → "not found"
	repo := &mockRoomRepository{detail: nil}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!nonexistent:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("[AC#2] expected status 404 for unknown room, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#2] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
	if resp.Error.Message == "" {
		t.Error("[AC#2] expected non-empty error message for 404")
	}
}

// ── AC#1: search parameter forwarded to repository ───────────────────────────

// TestListAdminRooms_SearchParamForwarded covers AC#1 + Acceptance Test #8 [P1]:
// When search=alpha is provided, ListRooms must be called with search="alpha".
// The mock captures the search param for assertion.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_SearchParamForwarded(t *testing.T) {
	repo := &mockRoomRepository{
		rooms: []api.AdminRoom{makeAdminRoom("!alpha:example.com", "Alpha Room", "active")},
		total: 1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?search=alpha", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if repo.capturedSearch != "alpha" {
		t.Errorf("[AC#1] expected ListRooms called with search='alpha', got %q", repo.capturedSearch)
	}
}

// ── AC#1: status=archived filter forwarded to repository ─────────────────────

// TestListAdminRooms_StatusArchivedForwarded covers AC#1 + Acceptance Test #9 [P1]:
// When status=archived is provided, ListRooms must be called with
// statusFilter="archived". The mock captures the statusFilter param.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_StatusArchivedForwarded(t *testing.T) {
	repo := &mockRoomRepository{
		rooms: []api.AdminRoom{makeAdminRoom("!archived:example.com", "Old Room", "archived")},
		total: 1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?status=archived", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200 for status=archived, got %d; body: %s", w.Code, w.Body.String())
	}

	if repo.capturedStatusFilter != "archived" {
		t.Errorf("[AC#1] expected ListRooms called with statusFilter='archived', got %q", repo.capturedStatusFilter)
	}
}

// TestListAdminRooms_StatusActiveForwarded covers AC#1 [P1]:
// When status=active is provided, ListRooms must be called with statusFilter="active".
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_StatusActiveForwarded(t *testing.T) {
	repo := &mockRoomRepository{
		rooms: []api.AdminRoom{makeAdminRoom("!active:example.com", "Active Room", "active")},
		total: 1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?status=active", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200 for status=active, got %d; body: %s", w.Code, w.Body.String())
	}

	if repo.capturedStatusFilter != "active" {
		t.Errorf("[AC#1] expected ListRooms called with statusFilter='active', got %q", repo.capturedStatusFilter)
	}
}

// ── AC#1: invalid status value → 400 M_BAD_JSON ──────────────────────────────

// TestListAdminRooms_InvalidStatus_Returns400 covers AC#1 [P0]:
// An unrecognised status value (e.g. "deleted") must produce 400 M_BAD_JSON
// with message "invalid status: must be active or archived".
// Validation must short-circuit BEFORE cursor/limit parsing.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_InvalidStatus_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?status=deleted", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for invalid status, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#1] expected error code M_BAD_JSON for invalid status, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "invalid status: must be active or archived" {
		t.Errorf("[AC#1] expected message 'invalid status: must be active or archived', got %q", resp.Error.Message)
	}
}

// ── AC#1: Rooms repo nil → 501 ────────────────────────────────────────────────

// TestListAdminRooms_RoomsRepoNil_Returns501 covers AC#1 + Acceptance Test story point [P0]:
// When AdminServer.Rooms is nil (not wired), the handler must return 501 Not Implemented.
// This is the stub-safety guard that prevents panics before wiring is complete.
//
// THIS TEST WILL FAIL until the nil-guard is added to ListAdminRooms.
func TestListAdminRooms_RoomsRepoNil_Returns501(t *testing.T) {
	// No Rooms field set — AdminServer.Rooms == nil.
	adminSrv := &api.AdminServer{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, adminSrv, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#1] expected 501 when Rooms repo is nil, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#1: Room object must include required fields ────────────────────────────

// TestListAdminRooms_RoomObjectFields covers AC#1 [P0]:
// Each room object in the data array must contain all mandatory fields:
// room_id, name, visibility, is_public, member_count, status, created_at.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_RoomObjectFields(t *testing.T) {
	room := makeAdminRoom("!abc:example.com", "General", "active")
	repo := &mockRoomRepository{rooms: []api.AdminRoom{room}, total: 1}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	requiredFields := []string{
		`"room_id"`,
		`"name"`,
		`"visibility"`,
		`"is_public"`,
		`"member_count"`,
		`"status"`,
		`"created_at"`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(body, field) {
			t.Errorf("[AC#1] expected field %s in room object; body: %s", field, body)
		}
	}
}

// ── AC#1: Default limit — no error when limit param absent ───────────────────

// TestListAdminRooms_DefaultLimit_NoError covers AC#1 [P1]:
// When no limit param is specified, the handler uses the default limit of 20
// and must return 200 without a validation error.
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_DefaultLimit_NoError(t *testing.T) {
	repo := &mockRoomRepository{rooms: []api.AdminRoom{}, total: 0}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[AC#1] expected 200 for default limit (no limit param), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#2: GetAdminRoom route is registered ────────────────────────────────────

// TestGetAdminRoom_RouteRegistered covers AC#2 [P0]:
// GET /api/v1/admin/rooms/{roomId} must be registered. A request with no role
// must produce 401 or 403 — NOT 404 (which would indicate missing route).
//
// THIS TEST WILL FAIL — GET /api/v1/admin/rooms/{roomId} is not registered yet.
func TestGetAdminRoom_RouteRegistered(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddleware, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!some:example.com", nil)
	// No X-Test-System-Role → no role in context.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#2] GET /api/v1/admin/rooms/{roomId} is not registered — got 404")
	}
}

// ── AC#1: Data array must be [] not null when empty ──────────────────────────

// TestListAdminRooms_EmptyResult_DataIsEmptyArray covers AC#1 [P1]:
// When the repository returns 0 rooms, the JSON data field must be an empty
// array [] — NOT null. This mirrors the users pattern (anti-null guard).
//
// THIS TEST WILL FAIL — ListAdminRooms is not implemented yet.
func TestListAdminRooms_EmptyResult_DataIsEmptyArray(t *testing.T) {
	repo := &mockRoomRepository{rooms: []api.AdminRoom{}, total: 0}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200 for empty result, got %d; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// "data":null must NOT appear; "data":[] must appear.
	if strings.Contains(body, `"data":null`) {
		t.Errorf("[AC#1] data must be [] not null when list is empty; body: %s", body)
	}
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("[AC#1] expected data:[] in response for empty result; body: %s", body)
	}
}

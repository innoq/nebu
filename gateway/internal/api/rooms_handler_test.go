//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.7:
// Room List + Get API.
//
// RED PHASE — all tests fail until implementation is complete.
// The types AdminRoom, AdminRoomDetail, RoomRepository, and handlers
// ListAdminRooms / GetAdminRoom do not exist yet; this file will not compile until:
//   - RoomRepository interface is defined in rooms_repo.go
//   - AdminRoom and AdminRoomDetail types are defined in rooms_repo.go
//   - AdminServer gains a Rooms RoomRepository field (server.go)
//   - ListAdminRooms is fully implemented (replaces 501 stub) in server.go
//   - GetAdminRoom is implemented in server.go
//   - GET /api/v1/admin/rooms/{roomId} is registered in router.go
//   - make gen-api has regenerated api_gen.go with ListAdminRoomsParams,
//     GetAdminRoomRequestObject, and updated response types
//
// Covered Acceptance Criteria:
//   - AC#1  GET /api/v1/admin/rooms — status filter, search filter, pagination, limit validation
//   - AC#2  GET /api/v1/admin/rooms/{roomId} — room detail, 404 on unknown room,
//           member_count (left_at IS NULL), message_count
//   - AC#3  Both endpoints emit audit log: action="admin_room_viewed", target_type="room"
//   - AC#6  Unit tests: all 7 acceptance test cases from the story
//   - AC#7  go build ./... and make test-unit-go pass with zero failures
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
// All fields are populated per-test; zero-value fields produce safe defaults.
type mockRoomRepository struct {
	listResult []api.AdminRoom
	listTotal  int
	listCursor string
	listErr    error

	getResult *api.AdminRoomDetail
	getErr    error

	// captured values allow assertions on how the handler called the repository
	capturedListSearch string
	capturedListStatus string
	capturedListLimit  int
}

func (m *mockRoomRepository) ListRooms(
	_ context.Context,
	_, _ string,
	limit int,
	search, status string,
) ([]api.AdminRoom, int, string, error) {
	m.capturedListSearch = search
	m.capturedListStatus = status
	m.capturedListLimit = limit
	if m.listErr != nil {
		return nil, 0, "", m.listErr
	}
	return m.listResult, m.listTotal, m.listCursor, nil
}

func (m *mockRoomRepository) GetRoom(
	_ context.Context,
	_ string,
) (*api.AdminRoomDetail, error) {
	return m.getResult, m.getErr
}

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


// makeAdminRoom constructs a minimal AdminRoom fixture.
func makeAdminRoom(roomID, name, status string) api.AdminRoom {
	return api.AdminRoom{
		RoomID:        roomID,
		Name:          name,
		Topic:         "",
		Visibility:    "private",
		MemberCount:   0,
		Status:        status,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatorUserID: "",
	}
}

// ── AC#1 + AC#6 (Acceptance Test #1): status=archived filter ─────────────────

// TestListAdminRooms_StatusArchivedFilter covers AC#1 + AC#6 (test 1) [P0]:
// When status=archived is specified, the handler must pass status="archived" to the
// repository. Only archived rooms must appear in the response data.
func TestListAdminRooms_StatusArchivedFilter(t *testing.T) {
	archivedRoom := makeAdminRoom("!archived:example.com", "Archived Room", "archived")

	repo := &mockRoomRepository{
		// Repository simulates the status filter: returns only the archived room.
		// The handler is verified separately to forward status="archived" to the repo
		// (asserted via repo.capturedListStatus below).
		listResult: []api.AdminRoom{archivedRoom},
		listTotal:  1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?status=archived", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminRoom `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Errorf("[AC#1] expected exactly 1 room for status=archived, got %d", len(resp.Data))
	}
	if len(resp.Data) > 0 && resp.Data[0].Status != "archived" {
		t.Errorf("[AC#1] expected room status=archived, got %q", resp.Data[0].Status)
	}

	// Verify the status filter was forwarded to the repository
	if repo.capturedListStatus != "archived" {
		t.Errorf("[AC#1] expected repository to receive status=archived, got %q", repo.capturedListStatus)
	}
}

// TestListAdminRooms_InvalidStatus_Returns400 covers AC#1 [P0]:
// An unrecognized status value (not "active" or "archived") must return 400 M_BAD_REQUEST.
func TestListAdminRooms_InvalidStatus_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?status=deleted", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected 400 for invalid status=deleted, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected error code M_BAD_REQUEST for invalid status, got %q", resp.Error.Code)
	}
}

// ── AC#1 + AC#6 (Acceptance Test #2): search filter ─────────────────────────

// TestListAdminRooms_SearchFiltersByName covers AC#1 + AC#6 (test 2) [P0]:
// When search=gamma is provided, the handler forwards the search term to the
// repository. The response must contain only rooms whose name matches "gamma"
// (case-insensitive partial match performed by the repository/DB, not the handler).
func TestListAdminRooms_SearchFiltersByName(t *testing.T) {
	gammaRoom := makeAdminRoom("!gamma:example.com", "Gamma Room", "active")

	repo := &mockRoomRepository{
		// Repository simulates the ILIKE filter: only returns Gamma Room for search=gamma
		listResult: []api.AdminRoom{gammaRoom},
		listTotal:  1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?search=gamma", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminRoom `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Errorf("[AC#1] expected exactly 1 room for search=gamma, got %d", len(resp.Data))
	}
	if len(resp.Data) > 0 && !strings.Contains(strings.ToLower(resp.Data[0].Name), "gamma") {
		t.Errorf("[AC#1] expected result to contain 'gamma' in name, got %q", resp.Data[0].Name)
	}

	// Verify the search term was forwarded to the repository
	if repo.capturedListSearch != "gamma" {
		t.Errorf("[AC#1] expected repository to receive search=gamma, got %q", repo.capturedListSearch)
	}
}

// ── AC#1: Pagination ─────────────────────────────────────────────────────────

// TestListAdminRooms_PaginationMetaReturned covers AC#1 [P0]:
// The response envelope must always include data[] and meta{total, next_cursor}.
// next_cursor must be omitted (or empty) when there are no more pages.
func TestListAdminRooms_PaginationMetaReturned(t *testing.T) {
	nextCur := api.EncodeCursor("!room2:example.com", time.Now().UTC().Format(time.RFC3339))
	repo := &mockRoomRepository{
		listResult: []api.AdminRoom{
			makeAdminRoom("!room1:example.com", "Room 1", "active"),
			makeAdminRoom("!room2:example.com", "Room 2", "active"),
		},
		listTotal:  5,
		listCursor: nextCur,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?limit=2", nil)
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

	if len(resp.Data) != 2 {
		t.Errorf("[AC#1] expected 2 rooms in data, got %d", len(resp.Data))
	}
	if resp.Meta.Total != 5 {
		t.Errorf("[AC#1] expected meta.total=5, got %d", resp.Meta.Total)
	}
	if resp.Meta.NextCursor == "" {
		t.Error("[AC#1] expected non-empty meta.next_cursor when more results exist")
	}
}

// TestListAdminRooms_DefaultLimit covers AC#1 [P1]:
// When no limit param is supplied, the handler must use the default of 20 and
// return 200 (no validation error).
func TestListAdminRooms_DefaultLimit_NoError(t *testing.T) {
	repo := &mockRoomRepository{listResult: []api.AdminRoom{}, listTotal: 0}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[AC#1] expected 200 for default limit, got %d; body: %s", w.Code, w.Body.String())
	}
	if repo.capturedListLimit != 20 {
		t.Errorf("[AC#1] expected default limit=20 forwarded to repo, got %d", repo.capturedListLimit)
	}
}

// TestListAdminRooms_LimitZero_Returns400 covers AC#1 [P0]:
// limit=0 is below the valid range [1,100] and must produce 400 M_BAD_REQUEST.
func TestListAdminRooms_LimitZero_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?limit=0", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected 400 for limit=0, got %d; body: %s", w.Code, w.Body.String())
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
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected M_BAD_REQUEST for limit=0, got %q", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "limit") {
		t.Errorf("[AC#1] expected error message to mention 'limit', got %q", resp.Error.Message)
	}
}

// TestListAdminRooms_LimitAbove100_Returns400 covers AC#1 [P0]:
// limit=101 exceeds the maximum and must produce 400 M_BAD_REQUEST.
func TestListAdminRooms_LimitAbove100_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?limit=101", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected 400 for limit=101, got %d", w.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected M_BAD_REQUEST for limit=101, got %q", resp.Error.Code)
	}
}

// TestListAdminRooms_InvalidCursor_Returns400 covers AC#1 [P0]:
// An invalid (non-base64url) cursor must produce 400 M_BAD_REQUEST.
func TestListAdminRooms_InvalidCursor_Returns400(t *testing.T) {
	repo := &mockRoomRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms?cursor=not-valid-base64!!", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected 400 for invalid cursor, got %d", w.Code)
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
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected M_BAD_REQUEST for invalid cursor, got %q", resp.Error.Code)
	}
}

// TestListAdminRooms_RoomObjectFields covers AC#1 [P0]:
// Each room object in data[] must contain all mandatory fields from the spec.
func TestListAdminRooms_RoomObjectFields(t *testing.T) {
	room := makeAdminRoom("!alpha:example.com", "Alpha Room", "active")
	room.Topic = "A test topic"
	room.MemberCount = 3
	room.CreatorUserID = "@creator:example.com"

	repo := &mockRoomRepository{listResult: []api.AdminRoom{room}, listTotal: 1}

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
		`"topic"`,
		`"visibility"`,
		`"member_count"`,
		`"status"`,
		`"created_at"`,
		`"creator_user_id"`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(body, field) {
			t.Errorf("[AC#1] expected field %s in room object; body: %s", field, body)
		}
	}
}

// ── AC#2 + AC#6 (Acceptance Test #3): Get unknown room → 404 ─────────────────

// TestGetAdminRoom_UnknownRoom_Returns404 covers AC#2 + AC#6 (test 3) [P0]:
// GET /api/v1/admin/rooms/{roomId} for a room not in the repository must return
// 404 with error code M_NOT_FOUND.
func TestGetAdminRoom_UnknownRoom_Returns404(t *testing.T) {
	// getResult=nil signals "not found" to the handler.
	repo := &mockRoomRepository{getResult: nil}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!doesnotexist:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("[AC#2] expected status 404, got %d; body: %s", w.Code, w.Body.String())
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

// ── AC#2 + AC#6 (Acceptance Test #4): member_count reflects active members only ─

// TestGetAdminRoom_MemberCount_ActiveOnly covers AC#2 + AC#6 (test 4) [P0]:
// member_count must count only current members (left_at IS NULL). Members who
// have left (left_at IS NOT NULL) must be excluded from the count.
func TestGetAdminRoom_MemberCount_ActiveOnly(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:      "!roomwithmembers:example.com",
			Name:        "Member Test Room",
			Topic:       "",
			Visibility:  "private",
			MemberCount: 3, // 3 active members; 1 has left (left_at IS NOT NULL)
			Status:      "active",
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		MaxMembers:      0,
		MessageCount:    0,
		PowerLevelsJSON: "{}",
	}
	repo := &mockRoomRepository{getResult: detail}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!roomwithmembers:example.com", nil)
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

	// The repository mock returns MemberCount=3 (active members only).
	// The handler must pass it through unchanged — it must not equal 4.
	if resp.Data.MemberCount != 3 {
		t.Errorf("[AC#2] expected member_count=3 (active members only), got %d", resp.Data.MemberCount)
	}
}

// ── AC#2 + AC#6 (Acceptance Test #5): message_count reflects events ──────────

// TestGetAdminRoom_MessageCount covers AC#2 + AC#6 (test 5) [P0]:
// message_count must reflect the count of events in the events table for this room.
func TestGetAdminRoom_MessageCount(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:     "!eventroom:example.com",
			Name:       "Event Room",
			Visibility: "private",
			Status:     "active",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		MaxMembers:      0,
		MessageCount:    7, // 7 events in the events table for this room
		PowerLevelsJSON: "{}",
	}
	repo := &mockRoomRepository{getResult: detail}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!eventroom:example.com", nil)
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

	if resp.Data.MessageCount != 7 {
		t.Errorf("[AC#2] expected message_count=7, got %d", resp.Data.MessageCount)
	}
}

// TestGetAdminRoom_DetailFields covers AC#2 [P0]:
// The single-room response must include all detail fields beyond the list fields:
// max_members, message_count, power_levels_json.
func TestGetAdminRoom_DetailFields(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:        "!detail:example.com",
			Name:          "Detail Room",
			Topic:         "A topic",
			Visibility:    "public",
			MemberCount:   5,
			Status:        "active",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			CreatorUserID: "@founder:example.com",
		},
		MaxMembers:      50,
		MessageCount:    12,
		PowerLevelsJSON: `{"ban":50,"kick":50}`,
	}
	repo := &mockRoomRepository{getResult: detail}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!detail:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	requiredFields := []string{
		`"room_id"`,
		`"name"`,
		`"topic"`,
		`"visibility"`,
		`"member_count"`,
		`"status"`,
		`"created_at"`,
		`"creator_user_id"`,
		`"max_members"`,
		`"message_count"`,
		`"power_levels_json"`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(body, field) {
			t.Errorf("[AC#2] expected field %s in room detail response; body: %s", field, body)
		}
	}
}

// TestGetAdminRoom_RouteRegistered covers AC#2 [P0]:
// GET /api/v1/admin/rooms/{roomId} must be registered. A mismatch (404 from the mux)
// means the route is absent. We use a mock with a non-nil getResult so the handler
// returns 200 — distinguishable from the mux's own 404 "page not found".
func TestGetAdminRoom_RouteRegistered(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:     "!someroom:example.com",
			Name:       "Some Room",
			Visibility: "private",
			Status:     "active",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		MaxMembers:      0,
		MessageCount:    0,
		PowerLevelsJSON: "{}",
	}
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: &mockRoomRepository{getResult: detail}}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!someroom:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#2] GET /api/v1/admin/rooms/{roomId} is not registered — got 404")
	}
}

// ── AC#2: Nil Rooms field → 501 stub ─────────────────────────────────────────

// TestGetAdminRoom_NilRepository_Returns501 covers router_test regression (Dev Notes):
// When AdminServer.Rooms is nil, GET /api/v1/admin/rooms/{roomId} must return 501,
// not panic or return 500.
func TestGetAdminRoom_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!someroom:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[router regression] expected 501 for nil Rooms, got %d", w.Code)
	}
}

// TestListAdminRooms_NilRepository_Returns501 covers router_test regression (Dev Notes):
// When AdminServer.Rooms is nil, GET /api/v1/admin/rooms must return 501.
func TestListAdminRooms_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[router regression] expected 501 for nil Rooms, got %d", w.Code)
	}
}

// ── AC#3 + AC#6 (Acceptance Test #6): Audit log on list ──────────────────────

// TestListAdminRooms_AuditLogEmitted covers AC#3 + AC#6 (test 6) [P0]:
// On a successful list request, audit.LogEvent must be called with:
//   - action="admin_room_viewed"
//   - target_type="room"
//   - target_id="" (no single room targeted for list)
func TestListAdminRooms_AuditLogEmitted(t *testing.T) {
	repo := &mockRoomRepository{
		listResult: []api.AdminRoom{makeAdminRoom("!r1:example.com", "Room 1", "active")},
		listTotal:  1,
	}
	mockClient := &mockCoreClient{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo, CoreClient: mockClient}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !mockClient.auditCalled {
		t.Error("[AC#3] expected audit.LogEvent to be called on list request")
	}
	if mockClient.lastAction != "admin_room_viewed" {
		t.Errorf("[AC#3] expected audit action 'admin_room_viewed', got %q", mockClient.lastAction)
	}
	if mockClient.lastTarget != "room" {
		t.Errorf("[AC#3] expected audit target_type 'room', got %q", mockClient.lastTarget)
	}
	if mockClient.lastTargetID != "" {
		t.Errorf("[AC#3] expected empty target_id for list audit, got %q", mockClient.lastTargetID)
	}
}

// ── AC#3 + AC#6 (Acceptance Test #7): Audit log on get ───────────────────────

// TestGetAdminRoom_AuditLogEmitted covers AC#3 + AC#6 (test 7) [P0]:
// On a successful single-room request, audit.LogEvent must be called with:
//   - action="admin_room_viewed"
//   - target_type="room"
//   - target_id = the requested room_id
func TestGetAdminRoom_AuditLogEmitted(t *testing.T) {
	detail := &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:     "!auditroom:example.com",
			Name:       "Audit Room",
			Visibility: "private",
			Status:     "active",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		MaxMembers:      0,
		MessageCount:    0,
		PowerLevelsJSON: "{}",
	}
	repo := &mockRoomRepository{getResult: detail}
	mockClient := &mockCoreClient{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo, CoreClient: mockClient}, noopJWTMiddlewareForRooms, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/rooms/!auditroom:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#3] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !mockClient.auditCalled {
		t.Error("[AC#3] expected audit.LogEvent to be called on get-room request")
	}
	if mockClient.lastAction != "admin_room_viewed" {
		t.Errorf("[AC#3] expected audit action 'admin_room_viewed', got %q", mockClient.lastAction)
	}
	if mockClient.lastTarget != "room" {
		t.Errorf("[AC#3] expected audit target_type 'room', got %q", mockClient.lastTarget)
	}
	if mockClient.lastTargetID != "!auditroom:example.com" {
		t.Errorf("[AC#3] expected audit target_id '!auditroom:example.com', got %q", mockClient.lastTargetID)
	}
}

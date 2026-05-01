//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.8:
// Room Settings Update API (PATCH /admin/rooms/{roomId}).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoomRepository gains UpdateRoom(ctx, roomID string, patch RoomPatch) (*AdminRoomDetail, error)
//   - RoomPatch struct is defined in rooms_repo.go
//   - AdminServer gains PatchAdminRoom handler in server.go
//   - AdminServer gains RoomDefaults RoomDefaultsRepository field in server.go
//   - PATCH /api/v1/admin/rooms/{roomId} is registered in router.go
//   - make gen-api has regenerated api_gen.go with PatchAdminRoomRequestObject,
//     PatchAdminRoom200JSONResponse, PatchAdminRoom400Response, PatchAdminRoom404Response,
//     PatchAdminRoom501Response
//   - proto/core.proto adds UpdateRoomSettings RPC; make proto regenerates
//     gateway/internal/grpc/pb/core_grpc.pb.go with UpdateRoomSettings method
//     on CoreServiceClient
//
// Covered Acceptance Criteria:
//
//	AC#1  PATCH /api/v1/admin/rooms/{roomId} — max_members, visibility, name, topic updates
//	AC#4  Unit tests: validation, 404, audit log, gRPC call assertions
//	AC#11 Router test: PATCH /admin/rooms/{roomId} registered → 501 when Rooms=nil
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoomRepositoryWithUpdate extends mockRoomRepository to also capture
// UpdateRoom calls. It replaces mockRoomRepository for Story 6.8 tests.
//
// All api.RoomRepository interface methods must be implemented here.
// ListRooms and GetRoom delegate to simple fields; UpdateRoom captures its args.
type mockRoomRepositoryWithUpdate struct {
	// ListRooms fields (not used by PATCH tests, but required by interface)
	listResult []api.AdminRoom
	listTotal  int
	listCursor string
	listErr    error

	// GetRoom fields — controls "room exists" check in PatchAdminRoom
	getResult *api.AdminRoomDetail
	getErr    error

	// UpdateRoom fields
	updateResult *api.AdminRoomDetail
	updateErr    error

	// Captured values for assertions
	capturedUpdateRoomID string
	capturedPatch        api.RoomPatch
	updateCalled         bool
}

func (m *mockRoomRepositoryWithUpdate) ListRooms(
	_ context.Context,
	_, _ string,
	_ int,
	_, _ string,
) ([]api.AdminRoom, int, string, error) {
	return m.listResult, m.listTotal, m.listCursor, m.listErr
}

func (m *mockRoomRepositoryWithUpdate) GetRoom(
	_ context.Context,
	_ string,
) (*api.AdminRoomDetail, error) {
	return m.getResult, m.getErr
}

func (m *mockRoomRepositoryWithUpdate) UpdateRoom(
	_ context.Context,
	roomID string,
	patch api.RoomPatch,
) (*api.AdminRoomDetail, error) {
	m.updateCalled = true
	m.capturedUpdateRoomID = roomID
	m.capturedPatch = patch
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.updateResult, nil
}

func (m *mockRoomRepositoryWithUpdate) ArchiveRoom(_ context.Context, _ string, _ string) (*api.ArchiveResult, error) {
	return nil, nil
}

func (m *mockRoomRepositoryWithUpdate) UnarchiveRoom(_ context.Context, _ string) (*api.UnarchiveResult, error) {
	return nil, nil
}

func (m *mockRoomRepositoryWithUpdate) GetRoomStatus(_ context.Context, _ string) (string, error) {
	return "active", nil
}

// mockCoreClientForRoomPatch captures WriteAuditLog and UpdateRoomSettings calls.
// Embeds pb.CoreServiceClient; only used methods are overridden.
type mockCoreClientForRoomPatch struct {
	pb.CoreServiceClient // embed to satisfy interface; all other methods panic if called

	// Audit log capture
	auditCalled  bool
	lastAction   string
	lastTarget   string
	lastTargetID string

	// UpdateRoomSettings capture
	updateSettingsCalled    bool
	capturedRoomID          string
	capturedMaxMembers      int32
	updateSettingsErr       error
}

func (m *mockCoreClientForRoomPatch) WriteAuditLog(
	_ context.Context,
	req *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	m.auditCalled = true
	m.lastAction = req.GetAction()
	m.lastTarget = req.GetTargetType()
	m.lastTargetID = req.GetTargetId()
	return &pb.WriteAuditLogResponse{}, nil
}

func (m *mockCoreClientForRoomPatch) UpdateRoomSettings(
	_ context.Context,
	req *pb.UpdateRoomSettingsRequest,
	_ ...grpc.CallOption,
) (*pb.UpdateRoomSettingsResponse, error) {
	m.updateSettingsCalled = true
	m.capturedRoomID = req.GetRoomId()
	m.capturedMaxMembers = req.GetMaxMembers()
	if m.updateSettingsErr != nil {
		return nil, m.updateSettingsErr
	}
	return &pb.UpdateRoomSettingsResponse{Ok: true}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForRoomPatch injects instance_admin role and a test actor ID.
func noopJWTMiddlewareForRoomPatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// makeTestRoomDetail constructs a minimal AdminRoomDetail fixture.
func makeTestRoomDetail(roomID string, maxMembers int) *api.AdminRoomDetail {
	return &api.AdminRoomDetail{
		AdminRoom: api.AdminRoom{
			RoomID:      roomID,
			Name:        "Test Room",
			Topic:       "",
			Visibility:  "private",
			MemberCount: 0,
			Status:      "active",
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		MaxMembers:      maxMembers,
		MessageCount:    0,
		PowerLevelsJSON: "{}",
	}
}

// patchRoom performs a PATCH request on /api/v1/admin/rooms/{roomID} and returns the recorder.
func patchRoom(
	t *testing.T,
	repo api.RoomRepository,
	coreClient pb.CoreServiceClient,
	roomID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{Rooms: repo, CoreClient: coreClient}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForRoomPatch, nil)

	target := "/api/v1/admin/rooms/" + roomID
	req := httptest.NewRequest(http.MethodPatch, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── AC#1 + AC#4 (Acceptance Test #1): PATCH updates max_members — 200 ────────

// TestPatchAdminRoom_UpdateMaxMembers_Returns200 covers AC#1 + AC#4 (test 1) [P0]:
// PATCH with max_members=50 on an existing room must return 200 with the updated
// room object, call UpdateRoom on the repository, and call UpdateRoomSettings gRPC.
func TestPatchAdminRoom_UpdateMaxMembers_Returns200(t *testing.T) {
	updatedRoom := makeTestRoomDetail("!roomA:server", 50)

	repo := &mockRoomRepositoryWithUpdate{
		// getResult is used internally by UpdateRoom in the real implementation.
		// For the mock, we return the updated room directly from UpdateRoom.
		updateResult: updatedRoom,
	}
	coreClient := &mockCoreClientForRoomPatch{}

	w := patchRoom(t, repo, coreClient, "!roomA:server", `{"max_members": 50}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data api.AdminRoomDetail `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if resp.Data.MaxMembers != 50 {
		t.Errorf("[AC#1] expected data.max_members=50, got %d", resp.Data.MaxMembers)
	}

	// Repository must have been called
	if !repo.updateCalled {
		t.Error("[AC#1] expected UpdateRoom to be called on the repository")
	}

	// gRPC UpdateRoomSettings must have been called with max_members=50
	if !coreClient.updateSettingsCalled {
		t.Error("[AC#1] expected UpdateRoomSettings gRPC to be called once")
	}
	if coreClient.capturedMaxMembers != 50 {
		t.Errorf("[AC#1] expected gRPC max_members=50, got %d", coreClient.capturedMaxMembers)
	}
	if coreClient.capturedRoomID != "!roomA:server" {
		t.Errorf("[AC#1] expected gRPC room_id='!roomA:server', got %q", coreClient.capturedRoomID)
	}
}

// ── AC#4 (Acceptance Test #2): PATCH unknown room → 404 ──────────────────────

// TestPatchAdminRoom_UnknownRoom_Returns404 covers AC#4 (test 2) [P0]:
// When the repository returns (nil, nil) from UpdateRoom (room not found),
// the handler must return 404 M_NOT_FOUND.
func TestPatchAdminRoom_UnknownRoom_Returns404(t *testing.T) {
	// UpdateRoom returns (nil, nil) — room not found
	repo := &mockRoomRepositoryWithUpdate{
		updateResult: nil,
		updateErr:    nil,
	}

	w := patchRoom(t, repo, nil, "!unknown:server", `{"max_members": 10}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("[AC#4] expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#4] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#4] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
	if resp.Error.Message == "" {
		t.Error("[AC#4] expected non-empty error message for 404")
	}
}

// ── AC#4 (Acceptance Test #3): PATCH with max_members=1 (below min) → 400 ───

// TestPatchAdminRoom_MaxMembersBelow2_Returns400 covers AC#4 (test 3) [P0]:
// max_members=1 is below the minimum of 2; the handler must return 400 M_BAD_JSON
// before calling the repository.
func TestPatchAdminRoom_MaxMembersBelow2_Returns400(t *testing.T) {
	repo := &mockRoomRepositoryWithUpdate{}

	w := patchRoom(t, repo, nil, "!roomA:server", `{"max_members": 1}`)

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
	if repo.updateCalled {
		t.Error("[AC#4] UpdateRoom must NOT be called when validation fails")
	}
}

// ── AC#4 (Acceptance Test #4): PATCH with max_members=100001 (above max) → 400

// TestPatchAdminRoom_MaxMembersAbove100000_Returns400 covers AC#4 (test 4) [P0]:
// max_members=100001 exceeds the maximum of 100000; the handler must return 400 M_BAD_JSON.
func TestPatchAdminRoom_MaxMembersAbove100000_Returns400(t *testing.T) {
	repo := &mockRoomRepositoryWithUpdate{}

	w := patchRoom(t, repo, nil, "!roomA:server", `{"max_members": 100001}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#4] expected status 400 for max_members=100001, got %d; body: %s", w.Code, w.Body.String())
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

	if repo.updateCalled {
		t.Error("[AC#4] UpdateRoom must NOT be called when validation fails")
	}
}

// ── AC#4 (Acceptance Test #5): PATCH with visibility=invalid → 400 ───────────

// TestPatchAdminRoom_InvalidVisibility_Returns400 covers AC#4 (test 5) [P0]:
// visibility="secret" is not a valid value; the handler must return 400 M_BAD_JSON.
func TestPatchAdminRoom_InvalidVisibility_Returns400(t *testing.T) {
	repo := &mockRoomRepositoryWithUpdate{}

	w := patchRoom(t, repo, nil, "!roomA:server", `{"visibility": "secret"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#4] expected status 400 for visibility=secret, got %d; body: %s", w.Code, w.Body.String())
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
		t.Errorf("[AC#4] expected M_BAD_JSON for invalid visibility, got %q", resp.Error.Code)
	}

	if repo.updateCalled {
		t.Error("[AC#4] UpdateRoom must NOT be called when validation fails")
	}
}

// ── AC#4 (Acceptance Test #6): PATCH updates visibility — reflected in response

// TestPatchAdminRoom_UpdateVisibility_Returns200 covers AC#4 (test 6) [P0]:
// PATCH with visibility="public" must return 200 with data.visibility="public".
// UpdateRoomSettings gRPC must NOT be called (max_members not in body).
func TestPatchAdminRoom_UpdateVisibility_Returns200(t *testing.T) {
	updatedRoom := makeTestRoomDetail("!roomA:server", 0)
	updatedRoom.Visibility = "public"

	repo := &mockRoomRepositoryWithUpdate{
		updateResult: updatedRoom,
	}
	coreClient := &mockCoreClientForRoomPatch{}

	w := patchRoom(t, repo, coreClient, "!roomA:server", `{"visibility": "public"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#4] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data api.AdminRoomDetail `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#4] response is not valid JSON: %v", err)
	}

	if resp.Data.Visibility != "public" {
		t.Errorf("[AC#4] expected data.visibility='public', got %q", resp.Data.Visibility)
	}

	// gRPC UpdateRoomSettings must NOT be called when max_members is absent from body
	if coreClient.updateSettingsCalled {
		t.Error("[AC#4] UpdateRoomSettings gRPC must NOT be called when max_members is not in the PATCH body")
	}
}

// ── AC#4 (Acceptance Test #7): PATCH empty body → 200 with unchanged room ────

// TestPatchAdminRoom_EmptyBody_Returns200_NoOpIsValid covers AC#4 (test 7) [P1]:
// PATCH with an empty body {} is a valid no-op; must return 200.
// UpdateRoom is called with no-changes patch; UpdateRoomSettings gRPC is NOT called.
func TestPatchAdminRoom_EmptyBody_Returns200_NoOpIsValid(t *testing.T) {
	unchanged := makeTestRoomDetail("!roomA:server", 0)
	repo := &mockRoomRepositoryWithUpdate{
		updateResult: unchanged,
	}
	coreClient := &mockCoreClientForRoomPatch{}

	w := patchRoom(t, repo, coreClient, "!roomA:server", `{}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#4] expected status 200 for empty-body PATCH, got %d; body: %s", w.Code, w.Body.String())
	}

	// UpdateRoomSettings gRPC must NOT be called (no max_members in body)
	if coreClient.updateSettingsCalled {
		t.Error("[AC#4] UpdateRoomSettings gRPC must NOT be called for empty-body PATCH")
	}
}

// ── AC#4 (Acceptance Test #10): Audit log called on PATCH ────────────────────

// TestPatchAdminRoom_AuditLogEmitted covers AC#4 (test 10) [P0]:
// On a successful PATCH, audit.LogEvent must be called with:
//   - action="room_settings_updated"
//   - target_type="room"
//   - target_id = the patched room_id
func TestPatchAdminRoom_AuditLogEmitted(t *testing.T) {
	updatedRoom := makeTestRoomDetail("!roomA:server", 0)
	updatedRoom.Name = "New Name"

	repo := &mockRoomRepositoryWithUpdate{
		updateResult: updatedRoom,
	}
	coreClient := &mockCoreClientForRoomPatch{}

	w := patchRoom(t, repo, coreClient, "!roomA:server", `{"name": "New Name"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#4] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if !coreClient.auditCalled {
		t.Error("[AC#4] expected audit.LogEvent to be called on PATCH")
	}
	if coreClient.lastAction != "room_settings_updated" {
		t.Errorf("[AC#4] expected audit action 'room_settings_updated', got %q", coreClient.lastAction)
	}
	if coreClient.lastTarget != "room" {
		t.Errorf("[AC#4] expected audit target_type 'room', got %q", coreClient.lastTarget)
	}
	if coreClient.lastTargetID != "!roomA:server" {
		t.Errorf("[AC#4] expected audit target_id '!roomA:server', got %q", coreClient.lastTargetID)
	}
}

// ── AC#11 (Acceptance Test #11): Router test — 501 when Rooms=nil ─────────────

// TestPatchAdminRoom_NilRepository_Returns501 covers AC#11 [P0]:
// When AdminServer.Rooms is nil, PATCH /api/v1/admin/rooms/{roomId} must return 501
// (not panic or return 500). This verifies the 501-guard pattern from Dev Notes.
func TestPatchAdminRoom_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForRoomPatch, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/rooms/someRoom",
		bytes.NewBufferString(`{"max_members": 10}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#11] expected 501 for nil Rooms, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestPatchAdminRoom_RouteRegistered covers AC#11 [P0]:
// PATCH /api/v1/admin/rooms/{roomId} must be registered in the mux.
// A 404 means the route is absent.
func TestPatchAdminRoom_RouteRegistered(t *testing.T) {
	updatedRoom := makeTestRoomDetail("!someroom:server", 0)
	repo := &mockRoomRepositoryWithUpdate{updateResult: updatedRoom}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{Rooms: repo}, noopJWTMiddlewareForRoomPatch, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/rooms/!someroom:server",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#11] PATCH /api/v1/admin/rooms/{roomId} is not registered — got 404")
	}
}

// TestPatchAdminRoom_gRPCFailure_BestEffort_Returns200 covers AC#1 [P1]:
// If UpdateRoomSettings gRPC fails, the handler must still return 200 (DB is already
// updated; best-effort gRPC notification; GenServer will load from DB on next start).
func TestPatchAdminRoom_gRPCFailure_BestEffort_Returns200(t *testing.T) {
	updatedRoom := makeTestRoomDetail("!roomA:server", 50)
	repo := &mockRoomRepositoryWithUpdate{
		updateResult: updatedRoom,
	}

	// Simulate gRPC failure
	coreClient := &mockCoreClientForRoomPatch{
		updateSettingsErr: &testGRPCError{msg: "connection refused"},
	}

	w := patchRoom(t, repo, coreClient, "!roomA:server", `{"max_members": 50}`)

	// Must still return 200 — gRPC failure is best-effort only
	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200 even when gRPC fails (best-effort), got %d; body: %s", w.Code, w.Body.String())
	}
}

// testGRPCError is a minimal error that satisfies the error interface for gRPC error mocking.
type testGRPCError struct{ msg string }

func (e *testGRPCError) Error() string { return e.msg }

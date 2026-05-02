//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.9:
// Room Archivierung (POST /admin/rooms/{roomId}/archive + /unarchive).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoomRepository gains ArchiveRoom, UnarchiveRoom, GetRoomStatus methods (rooms_repo.go)
//   - ArchiveResult, UnarchiveResult structs are defined in rooms_repo.go
//   - ErrRoomNotFound, ErrRoomWrongStatus sentinel errors are defined in rooms_repo.go
//   - AdminServer gains ArchiveAdminRoom + UnarchiveAdminRoom handlers (server.go)
//   - POST /api/v1/admin/rooms/{roomId}/archive is registered in router.go
//   - POST /api/v1/admin/rooms/{roomId}/unarchive is registered in router.go
//   - make gen-api has regenerated api_gen.go with ArchiveAdminRoom, UnarchiveAdminRoom
//     on StrictServerInterface (ArchiveAdminRoom501Response, UnarchiveAdminRoom501Response, …)
//   - proto/core.proto adds ArchiveRoom + UnarchiveRoom RPCs; make proto regenerates
//     gateway/internal/grpc/pb/core_grpc.pb.go with ArchiveRoom + UnarchiveRoom methods
//     on CoreServiceClient
//
// Covered Acceptance Criteria:
//
//	AC#1  POST /api/v1/admin/rooms/{roomId}/archive — 200, 400, 404, 409
//	AC#2  POST /api/v1/admin/rooms/{roomId}/unarchive — 200, 404, 409
//	AC#5  Unit tests (Go): archive, archive-already-archived, archive-404, archive-bad-reason,
//	      unarchive, unarchive-409, send-event-archived, router-501 archive, router-501 unarchive
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
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoomRepositoryForArchive implements api.RoomRepository and the new
// ArchiveRoom / UnarchiveRoom / GetRoomStatus methods added by Story 6.9.
//
// ListRooms, GetRoom, UpdateRoom are required by the interface but not
// exercised by archive/unarchive tests; they return zero values.
type mockRoomRepositoryForArchive struct {
	// ListRooms / GetRoom / UpdateRoom — zero-value stubs
	listResult []api.AdminRoom
	listTotal  int
	listCursor string
	listErr    error
	getResult  *api.AdminRoomDetail
	getErr     error

	// UpdateRoom — not used in archive tests
	updateResult *api.AdminRoomDetail
	updateErr    error

	// ArchiveRoom fields
	archiveResult *api.ArchiveResult
	archiveErr    error
	archiveCalled bool

	// UnarchiveRoom fields
	unarchiveResult *api.UnarchiveResult
	unarchiveErr    error
	unarchiveCalled bool

	// GetRoomStatus fields — used by SendEvent archive check test
	roomStatus string
	statusErr  error
}

func (m *mockRoomRepositoryForArchive) ListRooms(
	_ context.Context,
	_, _ string,
	_ int,
	_, _ string,
) ([]api.AdminRoom, int, string, error) {
	return m.listResult, m.listTotal, m.listCursor, m.listErr
}

func (m *mockRoomRepositoryForArchive) GetRoom(
	_ context.Context,
	_ string,
) (*api.AdminRoomDetail, error) {
	return m.getResult, m.getErr
}

func (m *mockRoomRepositoryForArchive) UpdateRoom(
	_ context.Context,
	_ string,
	_ api.RoomPatch,
) (*api.AdminRoomDetail, error) {
	return m.updateResult, m.updateErr
}

// ArchiveRoom is the new method added by Story 6.9 — will not compile until
// RoomRepository interface is extended with this signature.
func (m *mockRoomRepositoryForArchive) ArchiveRoom(
	_ context.Context,
	_ string,
	_ string,
) (*api.ArchiveResult, error) {
	m.archiveCalled = true
	return m.archiveResult, m.archiveErr
}

// UnarchiveRoom is the new method added by Story 6.9.
func (m *mockRoomRepositoryForArchive) UnarchiveRoom(
	_ context.Context,
	_ string,
) (*api.UnarchiveResult, error) {
	m.unarchiveCalled = true
	return m.unarchiveResult, m.unarchiveErr
}

// GetRoomStatus is the new method added by Story 6.9 — used by SendEventHandler.
func (m *mockRoomRepositoryForArchive) GetRoomStatus(
	_ context.Context,
	_ string,
) (string, error) {
	return m.roomStatus, m.statusErr
}

// mockCoreClientForArchive captures ArchiveRoom and UnarchiveRoom gRPC calls.
// Embeds pb.CoreServiceClient; only used methods are overridden.
//
// NOTE: After `make proto` adds ArchiveRoom + UnarchiveRoom to CoreServiceClient,
// ALL mocks that embed pb.CoreServiceClient must add these two stub methods.
// This file is the canonical example for the pattern.
type mockCoreClientForArchive struct {
	pb.CoreServiceClient // embed to satisfy interface; unimplemented methods panic if called

	// Audit log capture (WriteAuditLog)
	auditCalled  bool
	lastAction   string
	lastTarget   string
	lastTargetID string

	// ArchiveRoom capture
	archiveGRPCCalled    bool
	capturedArchiveRoomID string
	archiveGRPCErr       error

	// UnarchiveRoom capture
	unarchiveGRPCCalled    bool
	capturedUnarchiveRoomID string
	unarchiveGRPCErr       error
}

func (m *mockCoreClientForArchive) WriteAuditLog(
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

// ArchiveRoom — will not compile until make proto adds this to CoreServiceClient.
func (m *mockCoreClientForArchive) ArchiveRoom(
	_ context.Context,
	req *pb.ArchiveRoomRequest,
	_ ...grpc.CallOption,
) (*pb.ArchiveRoomResponse, error) {
	m.archiveGRPCCalled = true
	m.capturedArchiveRoomID = req.GetRoomId()
	if m.archiveGRPCErr != nil {
		return nil, m.archiveGRPCErr
	}
	return &pb.ArchiveRoomResponse{Ok: true}, nil
}

// UnarchiveRoom — will not compile until make proto adds this to CoreServiceClient.
func (m *mockCoreClientForArchive) UnarchiveRoom(
	_ context.Context,
	req *pb.UnarchiveRoomRequest,
	_ ...grpc.CallOption,
) (*pb.UnarchiveRoomResponse, error) {
	m.unarchiveGRPCCalled = true
	m.capturedUnarchiveRoomID = req.GetRoomId()
	if m.unarchiveGRPCErr != nil {
		return nil, m.unarchiveGRPCErr
	}
	return &pb.UnarchiveRoomResponse{Ok: true}, nil
}

// UpdateRoomSettings — required stub since mockCoreClientForArchive embeds the interface.
// This returns a default response to avoid panics from the embedded interface.
func (m *mockCoreClientForArchive) UpdateRoomSettings(
	_ context.Context,
	_ *pb.UpdateRoomSettingsRequest,
	_ ...grpc.CallOption,
) (*pb.UpdateRoomSettingsResponse, error) {
	return &pb.UpdateRoomSettingsResponse{Ok: true}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForArchive injects instance_admin role and a test actor ID.
func noopJWTMiddlewareForArchive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// archiveRoom performs a POST request on /api/v1/admin/rooms/{roomID}/archive
// and returns the recorder.
func archiveRoom(
	t *testing.T,
	repo api.RoomRepository,
	coreClient pb.CoreServiceClient,
	roomID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{Rooms: repo, CoreClient: coreClient}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForArchive, nil)

	target := "/api/v1/admin/rooms/" + roomID + "/archive"
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// unarchiveRoom performs a POST request on /api/v1/admin/rooms/{roomID}/unarchive.
func unarchiveRoom(
	t *testing.T,
	repo api.RoomRepository,
	coreClient pb.CoreServiceClient,
	roomID string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv := &api.AdminServer{Rooms: repo, CoreClient: coreClient}
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForArchive, nil)

	target := "/api/v1/admin/rooms/" + roomID + "/unarchive"
	req := httptest.NewRequest(http.MethodPost, target, http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── AC#1 + AC#5 (Acceptance Test #1): POST archive → 200, status="archived" ─

// TestArchiveAdminRoom_HappyPath_Returns200 covers AC#1 + AC#5 (test 1) [P0]:
// POST /archive on an active room must return 200 with room_id + status="archived".
// The mock ArchiveRoom DB call and mock ArchiveRoom gRPC must both be invoked.
//
// RED: fails until:
//   - RoomRepository.ArchiveRoom method is added
//   - ArchiveResult struct is defined
//   - AdminServer.ArchiveAdminRoom handler is implemented
//   - make gen-api regenerates ArchiveAdminRoom on StrictServerInterface
//   - make proto adds ArchiveRoom to CoreServiceClient
func TestArchiveAdminRoom_HappyPath_Returns200(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		archiveResult: &api.ArchiveResult{RoomID: "!roomA:server", Status: "archived"},
	}
	coreClient := &mockCoreClientForArchive{}

	w := archiveRoom(t, repo, coreClient, "!roomA:server", `{"reason": "No longer needed"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		RoomID string `json:"room_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.RoomID != "!roomA:server" {
		t.Errorf("[AC#1] expected room_id='!roomA:server', got %q", resp.RoomID)
	}
	if resp.Status != "archived" {
		t.Errorf("[AC#1] expected status='archived', got %q", resp.Status)
	}

	// DB call must have been made
	if !repo.archiveCalled {
		t.Error("[AC#1] expected ArchiveRoom DB call to be made")
	}

	// gRPC ArchiveRoom must have been called
	if !coreClient.archiveGRPCCalled {
		t.Error("[AC#1] expected ArchiveRoom gRPC to be called once")
	}
	if coreClient.capturedArchiveRoomID != "!roomA:server" {
		t.Errorf("[AC#1] expected gRPC room_id='!roomA:server', got %q", coreClient.capturedArchiveRoomID)
	}
}

// ── AC#1 + AC#5 (Acceptance Test #2): POST archive already-archived → 409 ────

// TestArchiveAdminRoom_AlreadyArchived_Returns409 covers AC#1 + AC#5 (test 2) [P0]:
// When the room is already archived (ErrRoomWrongStatus), the handler must return 409.
func TestArchiveAdminRoom_AlreadyArchived_Returns409(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		// ErrRoomWrongStatus signals "room already archived" for archive operation
		archiveErr: api.ErrRoomWrongStatus,
	}

	w := archiveRoom(t, repo, nil, "!roomA:server", `{"reason": "No longer needed"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("[AC#1] expected status 409, got %d; body: %s", w.Code, w.Body.String())
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
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#1] expected error code M_CONFLICT, got %q", resp.Error.Code)
	}
	if resp.Error.Message == "" {
		t.Error("[AC#1] expected non-empty error message for 409")
	}
}

// ── AC#1 + AC#5 (Acceptance Test #3): POST archive unknown room → 404 ────────

// TestArchiveAdminRoom_UnknownRoom_Returns404 covers AC#1 + AC#5 (test 3) [P0]:
// When the room does not exist (ErrRoomNotFound), the handler must return 404.
func TestArchiveAdminRoom_UnknownRoom_Returns404(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		archiveErr: api.ErrRoomNotFound,
	}

	w := archiveRoom(t, repo, nil, "!doesnotexist:server", `{"reason": "No longer needed"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("[AC#1] expected status 404, got %d; body: %s", w.Code, w.Body.String())
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
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#1] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
}

// ── AC#1 + AC#5 (Acceptance Test #4): POST archive short reason → 400 ────────

// TestArchiveAdminRoom_ShortReason_Returns400 covers AC#1 + AC#5 (test 4) [P0]:
// Reason shorter than 10 characters must return 400 M_BAD_JSON before hitting DB.
func TestArchiveAdminRoom_ShortReason_Returns400(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{}

	w := archiveRoom(t, repo, nil, "!roomA:server", `{"reason": "short"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#1] expected status 400, got %d; body: %s", w.Code, w.Body.String())
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
		t.Errorf("[AC#1] expected M_BAD_JSON, got %q", resp.Error.Code)
	}

	// Validation must happen before any DB call
	if repo.archiveCalled {
		t.Error("[AC#1] ArchiveRoom must NOT be called when reason validation fails")
	}
}

// ── AC#2 + AC#5 (Acceptance Test #5): POST unarchive → 200, status="active" ─

// TestUnarchiveAdminRoom_HappyPath_Returns200 covers AC#2 + AC#5 (test 5) [P0]:
// POST /unarchive on an archived room must return 200 with room_id + status="active".
// The mock UnarchiveRoom DB call and mock UnarchiveRoom gRPC must both be invoked.
func TestUnarchiveAdminRoom_HappyPath_Returns200(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		unarchiveResult: &api.UnarchiveResult{RoomID: "!roomA:server", Status: "active"},
	}
	coreClient := &mockCoreClientForArchive{}

	w := unarchiveRoom(t, repo, coreClient, "!roomA:server")

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		RoomID string `json:"room_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.RoomID != "!roomA:server" {
		t.Errorf("[AC#2] expected room_id='!roomA:server', got %q", resp.RoomID)
	}
	if resp.Status != "active" {
		t.Errorf("[AC#2] expected status='active', got %q", resp.Status)
	}

	// DB call must have been made
	if !repo.unarchiveCalled {
		t.Error("[AC#2] expected UnarchiveRoom DB call to be made")
	}

	// gRPC UnarchiveRoom must have been called
	if !coreClient.unarchiveGRPCCalled {
		t.Error("[AC#2] expected UnarchiveRoom gRPC to be called once")
	}
	if coreClient.capturedUnarchiveRoomID != "!roomA:server" {
		t.Errorf("[AC#2] expected gRPC room_id='!roomA:server', got %q", coreClient.capturedUnarchiveRoomID)
	}
}

// ── AC#2 + AC#5 (Acceptance Test #6): POST unarchive not-archived room → 409 ─

// TestUnarchiveAdminRoom_NotArchived_Returns409 covers AC#2 + AC#5 (test 6) [P0]:
// When the room is already active (ErrRoomWrongStatus for unarchive), handler returns 409.
func TestUnarchiveAdminRoom_NotArchived_Returns409(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		unarchiveErr: api.ErrRoomWrongStatus,
	}

	w := unarchiveRoom(t, repo, nil, "!roomA:server")

	if w.Code != http.StatusConflict {
		t.Fatalf("[AC#2] expected status 409, got %d; body: %s", w.Code, w.Body.String())
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
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#2] expected error code M_CONFLICT, got %q", resp.Error.Code)
	}
}

// ── AC#1 + AC#5 (Acceptance Test #7): Audit log called on archive ─────────────

// TestArchiveAdminRoom_AuditLogEmitted covers AC#1 + AC#5 [P0]:
// On successful archive, audit.LogEvent must be called with
// action="room_archived", target_type="room", target_id=roomID.
func TestArchiveAdminRoom_AuditLogEmitted(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		archiveResult: &api.ArchiveResult{RoomID: "!roomA:server", Status: "archived"},
	}
	coreClient := &mockCoreClientForArchive{}

	w := archiveRoom(t, repo, coreClient, "!roomA:server", `{"reason": "Compliance hold"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if !coreClient.auditCalled {
		t.Error("[AC#1] expected audit.LogEvent to be called on archive")
	}
	if coreClient.lastAction != "room_archived" {
		t.Errorf("[AC#1] expected audit action 'room_archived', got %q", coreClient.lastAction)
	}
	if coreClient.lastTarget != "room" {
		t.Errorf("[AC#1] expected audit target_type 'room', got %q", coreClient.lastTarget)
	}
	if coreClient.lastTargetID != "!roomA:server" {
		t.Errorf("[AC#1] expected audit target_id '!roomA:server', got %q", coreClient.lastTargetID)
	}
}

// ── AC#2 + AC#5: Audit log called on unarchive ────────────────────────────────

// TestUnarchiveAdminRoom_AuditLogEmitted covers AC#2 + AC#5 [P0]:
// On successful unarchive, audit.LogEvent must be called with action="room_unarchived".
func TestUnarchiveAdminRoom_AuditLogEmitted(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		unarchiveResult: &api.UnarchiveResult{RoomID: "!roomA:server", Status: "active"},
	}
	coreClient := &mockCoreClientForArchive{}

	w := unarchiveRoom(t, repo, coreClient, "!roomA:server")

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if !coreClient.auditCalled {
		t.Error("[AC#2] expected audit.LogEvent to be called on unarchive")
	}
	if coreClient.lastAction != "room_unarchived" {
		t.Errorf("[AC#2] expected audit action 'room_unarchived', got %q", coreClient.lastAction)
	}
}

// ── AC#1 + AC#5 (Acceptance Test #8): Router test — archive 501 when Rooms=nil

// TestArchiveAdminRoom_NilRepository_Returns501 covers AC#5 (test 8) [P0]:
// When AdminServer.Rooms is nil, POST archive must return 501 (not panic).
// This verifies the 501-guard pattern from Dev Notes.
//
// RED: fails until POST /api/v1/admin/rooms/{roomId}/archive is registered in router.go.
func TestArchiveAdminRoom_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForArchive, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/rooms/someRoom/archive",
		bytes.NewBufferString(`{"reason": "No longer needed here"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#5] expected 501 for nil Rooms (archive), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#2 + AC#5 (Acceptance Test #9): Router test — unarchive 501 when Rooms=nil

// TestUnarchiveAdminRoom_NilRepository_Returns501 covers AC#5 (test 9) [P0]:
// When AdminServer.Rooms is nil, POST unarchive must return 501 (not panic).
//
// RED: fails until POST /api/v1/admin/rooms/{roomId}/unarchive is registered in router.go.
func TestUnarchiveAdminRoom_NilRepository_Returns501(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, &api.AdminServer{}, noopJWTMiddlewareForArchive, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/rooms/someRoom/unarchive", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[AC#5] expected 501 for nil Rooms (unarchive), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#1 + AC#5: gRPC failure is best-effort — archive still returns 200 ──────

// TestArchiveAdminRoom_gRPCFailure_BestEffort_Returns200 covers AC#1 + AC#5 [P1]:
// If ArchiveRoom gRPC fails, the handler must still return 200 (DB is authoritative;
// Room.Server init/1 will stop on next restart due to archived status in DB).
func TestArchiveAdminRoom_gRPCFailure_BestEffort_Returns200(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		archiveResult: &api.ArchiveResult{RoomID: "!roomA:server", Status: "archived"},
	}
	coreClient := &mockCoreClientForArchive{
		archiveGRPCErr: &testArchiveGRPCError{msg: "connection refused"},
	}

	w := archiveRoom(t, repo, coreClient, "!roomA:server", `{"reason": "No longer needed"}`)

	// Must still return 200 — gRPC failure is best-effort only
	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200 even when gRPC fails (best-effort), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ── AC#2 + AC#5: unarchive unknown room → 404 ────────────────────────────────

// TestUnarchiveAdminRoom_UnknownRoom_Returns404 covers AC#2 + AC#5 [P0]:
// When the room does not exist (ErrRoomNotFound) on unarchive, handler returns 404.
func TestUnarchiveAdminRoom_UnknownRoom_Returns404(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{
		unarchiveErr: api.ErrRoomNotFound,
	}

	w := unarchiveRoom(t, repo, nil, "!doesnotexist:server")

	if w.Code != http.StatusNotFound {
		t.Fatalf("[AC#2] expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#2] expected M_NOT_FOUND, got %q", resp.Error.Code)
	}
}

// ── AC#1: missing reason body → 400 ──────────────────────────────────────────

// TestArchiveAdminRoom_MissingReason_Returns400 covers AC#1 [P1]:
// Archive request without a "reason" key (or empty body) must return 400 M_BAD_JSON.
func TestArchiveAdminRoom_MissingReason_Returns400(t *testing.T) {
	repo := &mockRoomRepositoryForArchive{}

	w := archiveRoom(t, repo, nil, "!roomA:server", `{}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("[AC#1] expected status 400 for missing reason, got %d; body: %s", w.Code, w.Body.String())
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
		t.Errorf("[AC#1] expected M_BAD_JSON for missing reason, got %q", resp.Error.Code)
	}

	if repo.archiveCalled {
		t.Error("[AC#1] ArchiveRoom must NOT be called when reason is missing")
	}
}

// testArchiveGRPCError is a minimal error type for gRPC error simulation.
type testArchiveGRPCError struct{ msg string }

func (e *testArchiveGRPCError) Error() string { return e.msg }

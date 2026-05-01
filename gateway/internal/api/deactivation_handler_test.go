//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.5:
// User Deactivation + Reactivation + Session-Invalidierung.
//
// RED PHASE — all tests fail until implementation is complete.
// The types DeactivationRepository, DeactivateAdminUser, ReactivateAdminUser,
// and the Deactivation field on AdminServer do not exist yet; this file will
// not compile until:
//   - DeactivationRepository interface is defined in deactivation_repo.go
//   - AdminServer gains a Deactivation DeactivationRepository field (server.go)
//   - DeactivateAdminUser and ReactivateAdminUser handlers are implemented (server.go)
//   - POST /api/v1/admin/users/{userId}/deactivate is registered (router.go)
//   - POST /api/v1/admin/users/{userId}/reactivate is registered (router.go)
//   - make gen-api has regenerated api_gen.go with the new request/response types
//
// Covered Acceptance Criteria:
//   - AC#1  POST /api/v1/admin/users/{userId}/deactivate — full validation, 200/400/404/409
//   - AC#2  POST /api/v1/admin/users/{userId}/reactivate — full validation, 200/404/409
//   - AC#8  Route registration for both endpoints (instance_admin required)
//   - AC#9  All 8 handler-level unit tests as defined in story Acceptance Tests #1–#7 + audit
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockDeactivationRepository implements api.DeactivationRepository for unit tests.
// Fields control per-test behaviour; zero values produce safe defaults.
type mockDeactivationRepository struct {
	// GetUserStatus returns
	isActive      bool
	deletionStatus string
	anonymizedAt  int64
	statusErr     error

	// DeactivateUser / ReactivateUser errors
	deactivateErr error
	reactivateErr error
}

func (m *mockDeactivationRepository) GetUserStatus(
	_ context.Context,
	_ string,
) (bool, string, int64, error) {
	return m.isActive, m.deletionStatus, m.anonymizedAt, m.statusErr
}

func (m *mockDeactivationRepository) DeactivateUser(
	_ context.Context,
	_, _ string,
	_ int64,
) error {
	return m.deactivateErr
}

func (m *mockDeactivationRepository) ReactivateUser(
	_ context.Context,
	_ string,
) error {
	return m.reactivateErr
}

// mockCoreClientForDeactivation captures WriteAuditLog and InvalidateUserSessions calls.
// It satisfies pb.CoreServiceClient via embedding; only the methods used by the
// deactivation handlers are overridden.
type mockCoreClientForDeactivation struct {
	pb.CoreServiceClient // embed to satisfy interface; other methods panic if called

	auditCalled  bool
	lastAction   string
	lastTarget   string
	lastTargetID string

	invalidateCalled bool
	invalidateUserID string
}

func (m *mockCoreClientForDeactivation) WriteAuditLog(
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

func (m *mockCoreClientForDeactivation) InvalidateUserSessions(
	_ context.Context,
	req *pb.InvalidateUserSessionsRequest,
	_ ...grpc.CallOption,
) (*pb.InvalidateUserSessionsResponse, error) {
	m.invalidateCalled = true
	m.invalidateUserID = req.GetUserId()
	return &pb.InvalidateUserSessionsResponse{Ok: true}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForDeactivation injects instance_admin role and a test actor ID.
func noopJWTMiddlewareForDeactivation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// buildDeactivationServer constructs an AdminServer wired with deactivation repo and
// optional CoreClient.
func buildDeactivationServer(
	repo api.DeactivationRepository,
	coreClient pb.CoreServiceClient,
) *api.AdminServer {
	return &api.AdminServer{
		Deactivation: repo,
		CoreClient:   coreClient,
	}
}

// deactivateBody serialises the reason payload.
func deactivateBody(reason string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"reason": reason})
	return bytes.NewBuffer(b)
}

// ── AC#1: Deactivate endpoint ─────────────────────────────────────────────────

// TestDeactivateAdminUser_ActiveUser_Returns200 covers AC#1 + AT#1 [P0]:
// POST /api/v1/admin/users/{userId}/deactivate for an active user must return
// 200 with body {"data":{"user_id":"...","status":"deactivated"}}.
//
// Failing reason: DeactivateAdminUser handler + Deactivation field do not exist yet.
func TestDeactivateAdminUser_ActiveUser_Returns200(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive: true, // user is active
	}
	coreClient := &mockCoreClientForDeactivation{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, coreClient), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1/AT#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			UserID string `json:"user_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1/AT#1] response is not valid JSON: %v", err)
	}
	if resp.Data.UserID != "@alice:example.com" {
		t.Errorf("[AC#1/AT#1] expected user_id @alice:example.com, got %q", resp.Data.UserID)
	}
	if resp.Data.Status != "deactivated" {
		t.Errorf("[AC#1/AT#1] expected status=deactivated, got %q", resp.Data.Status)
	}
}

// TestDeactivateAdminUser_AlreadyDeactivated_Returns409 covers AC#1 + AT#2 [P0]:
// POST /api/v1/admin/users/{userId}/deactivate for a user with is_active=false must
// return 409 with error code M_CONFLICT.
//
// Failing reason: handler does not exist yet.
func TestDeactivateAdminUser_AlreadyDeactivated_Returns409(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive: false, // user is already deactivated
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("[AC#1/AT#2] expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1/AT#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#1/AT#2] expected error code M_CONFLICT, got %q", resp.Error.Code)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "deactivated") {
		t.Errorf("[AC#1/AT#2] expected message to mention 'deactivated', got %q", resp.Error.Message)
	}
}

// TestDeactivateAdminUser_UserNotFound_Returns404 covers AC#1 + AT#3 [P0]:
// POST /api/v1/admin/users/{userId}/deactivate for a user that does not exist must
// return 404 with error code M_NOT_FOUND.
//
// Failing reason: handler does not exist yet; ErrUserNotFound is not defined yet.
func TestDeactivateAdminUser_UserNotFound_Returns404(t *testing.T) {
	repo := &mockDeactivationRepository{
		statusErr: api.ErrUserNotFound,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@ghost:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("[AC#1/AT#3] expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1/AT#3] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#1/AT#3] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
}

// TestDeactivateAdminUser_ShortReason_Returns400 covers AC#1 + AT#4 [P0]:
// POST /api/v1/admin/users/{userId}/deactivate with a reason shorter than 10 chars
// must return 400 with error code M_BAD_JSON.
//
// Failing reason: handler does not exist yet.
func TestDeactivateAdminUser_ShortReason_Returns400(t *testing.T) {
	repo := &mockDeactivationRepository{isActive: true}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("too short") // 9 chars — below 10 char minimum
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1/AT#4] expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1/AT#4] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_JSON" {
		t.Errorf("[AC#1/AT#4] expected error code M_BAD_JSON, got %q", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "10") {
		t.Errorf("[AC#1/AT#4] expected message to mention '10' chars, got %q", resp.Error.Message)
	}
}

// TestDeactivateAdminUser_MissingBody_Returns400 covers AC#1 [P0]:
// POST /api/v1/admin/users/{userId}/deactivate with missing body must return 400.
//
// Failing reason: handler does not exist yet.
func TestDeactivateAdminUser_MissingBody_Returns400(t *testing.T) {
	repo := &mockDeactivationRepository{isActive: true}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	// No body at all
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for missing body, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestDeactivateAdminUser_AuditLogEmitted covers AC#1 [P0]:
// On a successful deactivation, audit.LogEvent must be called with
// action="user_deactivated", target_type="user", target_id=userId,
// and the reason must appear in metadata.
//
// Failing reason: handler + audit wiring do not exist yet.
func TestDeactivateAdminUser_AuditLogEmitted(t *testing.T) {
	repo := &mockDeactivationRepository{isActive: true}
	coreClient := &mockCoreClientForDeactivation{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, coreClient), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident for audit")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !coreClient.auditCalled {
		t.Error("[AC#1] expected audit.LogEvent to be called on successful deactivation")
	}
	if coreClient.lastAction != "user_deactivated" {
		t.Errorf("[AC#1] expected audit action 'user_deactivated', got %q", coreClient.lastAction)
	}
	if coreClient.lastTarget != "user" {
		t.Errorf("[AC#1] expected audit target_type 'user', got %q", coreClient.lastTarget)
	}
	if coreClient.lastTargetID != "@alice:example.com" {
		t.Errorf("[AC#1] expected audit target_id '@alice:example.com', got %q", coreClient.lastTargetID)
	}
}

// TestDeactivateAdminUser_InvalidateSessionsCalled covers AC#1 [P0]:
// On successful deactivation, CoreService.InvalidateUserSessions must be called
// with the target user_id.
//
// Failing reason: handler + gRPC call do not exist yet; pb.InvalidateUserSessionsRequest
// does not exist yet (proto not regenerated).
func TestDeactivateAdminUser_InvalidateSessionsCalled(t *testing.T) {
	repo := &mockDeactivationRepository{isActive: true}
	coreClient := &mockCoreClientForDeactivation{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, coreClient), noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident check")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !coreClient.invalidateCalled {
		t.Error("[AC#1] expected CoreService.InvalidateUserSessions to be called on deactivation")
	}
	if coreClient.invalidateUserID != "@alice:example.com" {
		t.Errorf("[AC#1] expected InvalidateUserSessions user_id='@alice:example.com', got %q", coreClient.invalidateUserID)
	}
}

// ── AC#2: Reactivate endpoint ─────────────────────────────────────────────────

// TestReactivateAdminUser_DeactivatedUser_Returns200 covers AC#2 + AT#5 [P0]:
// POST /api/v1/admin/users/{userId}/reactivate for a deactivated user must return
// 200 with body {"data":{"user_id":"...","status":"active"}}.
//
// Failing reason: ReactivateAdminUser handler + Deactivation field do not exist yet.
func TestReactivateAdminUser_DeactivatedUser_Returns200(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive:      false, // user is deactivated
		deletionStatus: "",
		anonymizedAt:  0,
	}
	coreClient := &mockCoreClientForDeactivation{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, coreClient), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2/AT#5] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			UserID string `json:"user_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2/AT#5] response is not valid JSON: %v", err)
	}
	if resp.Data.UserID != "@alice:example.com" {
		t.Errorf("[AC#2/AT#5] expected user_id @alice:example.com, got %q", resp.Data.UserID)
	}
	if resp.Data.Status != "active" {
		t.Errorf("[AC#2/AT#5] expected status=active, got %q", resp.Data.Status)
	}
}

// TestReactivateAdminUser_AnonymizedUser_Returns409 covers AC#2 + AT#6 [P0]:
// POST /api/v1/admin/users/{userId}/reactivate for a user with anonymized_at IS NOT NULL
// must return 409 M_CONFLICT with message mentioning "anonymized".
//
// Failing reason: handler does not exist yet.
func TestReactivateAdminUser_AnonymizedUser_Returns409(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive:     false,
		anonymizedAt: 1_700_000_000_000, // non-zero = anonymized
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("[AC#2/AT#6] expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2/AT#6] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#2/AT#6] expected error code M_CONFLICT, got %q", resp.Error.Code)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "anonymized") {
		t.Errorf("[AC#2/AT#6] expected message to mention 'anonymized', got %q", resp.Error.Message)
	}
}

// TestReactivateAdminUser_KeysDeletedUser_Returns409 covers AC#2 + AT#7 [P0]:
// POST /api/v1/admin/users/{userId}/reactivate for a user with deletion_status='keys_deleted'
// must return 409 M_CONFLICT with message mentioning "keys_deleted".
//
// Failing reason: handler does not exist yet.
func TestReactivateAdminUser_KeysDeletedUser_Returns409(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive:      false,
		deletionStatus: "keys_deleted",
		anonymizedAt:  0,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("[AC#2/AT#7] expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2/AT#7] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#2/AT#7] expected error code M_CONFLICT, got %q", resp.Error.Code)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "keys_deleted") {
		t.Errorf("[AC#2/AT#7] expected message to mention 'keys_deleted', got %q", resp.Error.Message)
	}
}

// TestReactivateAdminUser_AlreadyActive_Returns409 covers AC#2 [P0]:
// POST /api/v1/admin/users/{userId}/reactivate for a user that is already active
// must return 409 M_CONFLICT.
//
// Failing reason: handler does not exist yet.
func TestReactivateAdminUser_AlreadyActive_Returns409(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive:      true, // already active
		deletionStatus: "",
		anonymizedAt:  0,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("[AC#2] expected status 409 for already-active reactivation, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_CONFLICT" {
		t.Errorf("[AC#2] expected error code M_CONFLICT for already-active, got %q", resp.Error.Code)
	}
}

// TestReactivateAdminUser_UserNotFound_Returns404 covers AC#2 [P0]:
// POST /api/v1/admin/users/{userId}/reactivate for a non-existent user must
// return 404 M_NOT_FOUND.
//
// Failing reason: handler does not exist yet.
func TestReactivateAdminUser_UserNotFound_Returns404(t *testing.T) {
	repo := &mockDeactivationRepository{
		statusErr: api.ErrUserNotFound,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, nil), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@ghost:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("[AC#2] expected status 404, got %d; body: %s", w.Code, w.Body.String())
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
		t.Errorf("[AC#2] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
}

// TestReactivateAdminUser_AuditLogEmitted covers AC#2 [P0]:
// On a successful reactivation, audit.LogEvent must be called with
// action="user_reactivated", target_type="user", target_id=userId.
//
// Failing reason: handler + audit wiring do not exist yet.
func TestReactivateAdminUser_AuditLogEmitted(t *testing.T) {
	repo := &mockDeactivationRepository{
		isActive:      false,
		deletionStatus: "",
		anonymizedAt:  0,
	}
	coreClient := &mockCoreClientForDeactivation{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(repo, coreClient), noopJWTMiddlewareForDeactivation, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !coreClient.auditCalled {
		t.Error("[AC#2] expected audit.LogEvent to be called on successful reactivation")
	}
	if coreClient.lastAction != "user_reactivated" {
		t.Errorf("[AC#2] expected audit action 'user_reactivated', got %q", coreClient.lastAction)
	}
	if coreClient.lastTarget != "user" {
		t.Errorf("[AC#2] expected audit target_type 'user', got %q", coreClient.lastTarget)
	}
	if coreClient.lastTargetID != "@alice:example.com" {
		t.Errorf("[AC#2] expected audit target_id '@alice:example.com', got %q", coreClient.lastTargetID)
	}
}

// ── AC#8: Route registration ──────────────────────────────────────────────────

// TestDeactivateRoutes_Registered covers AC#8 [P0]:
// Both POST /api/v1/admin/users/{userId}/deactivate and
// POST /api/v1/admin/users/{userId}/reactivate must be registered.
// A request without the instance_admin role must receive 401 (not 404).
// A 404 would indicate the routes are missing.
//
// Failing reason: routes are not registered yet in router.go.
func TestDeactivateRoutes_Registered(t *testing.T) {
	// Use a no-op JWT middleware that injects NO role — RequireRole should then return 401.
	noRoleMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(&mockDeactivationRepository{}, nil), noRoleMW, nil)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate"},
		{http.MethodPost, "/api/v1/admin/users/@alice:example.com/reactivate"},
	}

	for _, tc := range routes {
		t.Run(tc.path, func(t *testing.T) {
			var body *bytes.Buffer
			if tc.path == "/api/v1/admin/users/@alice:example.com/deactivate" {
				body = deactivateBody("Security incident")
			}
			var req *http.Request
			if body != nil {
				req = httptest.NewRequest(tc.method, tc.path, body)
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("[AC#8] route %s %s is not registered — got 404", tc.method, tc.path)
			}
		})
	}
}

// TestDeactivateRoutes_RequireInstanceAdmin covers AC#8 [P1]:
// Both deactivate/reactivate routes must reject non-admin role with 403.
//
// Failing reason: routes not registered yet.
func TestDeactivateRoutes_RequireInstanceAdmin(t *testing.T) {
	wrongRoleMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.ContextKeySystemRole, "compliance_officer")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildDeactivationServer(&mockDeactivationRepository{}, nil), wrongRoleMW, nil)

	routes := []string{
		"/api/v1/admin/users/@alice:example.com/deactivate",
		"/api/v1/admin/users/@alice:example.com/reactivate",
	}
	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, deactivateBody("Security incident"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("[AC#8] expected 403 for compliance_officer on %s, got %d", path, w.Code)
			}
		})
	}
}

// TestDeactivateAdminUser_NilDeactivationRepo_Returns501 covers router_test.go
// compat (existing AdminServer{} stub behaviour) [P1]:
// When Deactivation field is nil, the handler must return 501 Not Implemented
// without panicking. This ensures router_test.go's &AdminServer{} tests keep passing.
//
// Failing reason: handler + nil-guard do not exist yet.
func TestDeactivateAdminUser_NilDeactivationRepo_Returns501(t *testing.T) {
	// Use AdminServer with Deactivation=nil (no repo wired).
	srv := &api.AdminServer{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, srv, noopJWTMiddlewareForDeactivation, nil)

	body := deactivateBody("Security incident guard")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/@alice:example.com/deactivate", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("[compat] expected 501 stub when Deactivation=nil, got %d; body: %s", w.Code, w.Body.String())
	}
}

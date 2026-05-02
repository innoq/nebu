//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.6:
// User Role Assignment API (role_overrides Tabelle + Middleware-Integration).
//
// RED PHASE — all tests fail until implementation is complete.
// This file will not compile until:
//   - RoleOverrideRepository interface is defined in roles_repo.go
//   - AdminServer gains a Roles RoleOverrideRepository field (server.go)
//   - AssignAdminUserRole handler is implemented in server.go
//   - POST /api/v1/admin/users/{userId}/roles is registered in router.go
//   - make gen-api has regenerated api_gen.go with AssignAdminUserRole types
//
// Covered Acceptance Criteria:
//   - AC#2  POST /api/v1/admin/users/{userId}/roles — grant/revoke, 200/400/403/404
//   - AC#7  All handler-level unit tests as defined in story Acceptance Tests #1–#5 + audit
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/api"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockRoleOverrideRepository implements api.RoleOverrideRepository for unit tests.
// Fields control per-test behaviour; zero values produce safe defaults.
type mockRoleOverrideRepository struct {
	// GrantRoleOverride / RevokeRoleOverride behaviour
	grantErr  error
	revokeErr error

	// GetRoleOverrides returns
	overrides    []string
	overridesErr error

	// UserExists — set to false to simulate unknown user
	userExists bool
}

func (m *mockRoleOverrideRepository) GrantRoleOverride(
	_ context.Context,
	_, _, _ string,
) error {
	return m.grantErr
}

func (m *mockRoleOverrideRepository) RevokeRoleOverride(
	_ context.Context,
	_, _ string,
) error {
	return m.revokeErr
}

func (m *mockRoleOverrideRepository) GetRoleOverrides(
	_ context.Context,
	_ string,
) ([]string, error) {
	return m.overrides, m.overridesErr
}

func (m *mockRoleOverrideRepository) GetAllRoleOverridesForUsers(
	_ context.Context,
	_ []string,
) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (m *mockRoleOverrideRepository) UserExists(
	_ context.Context,
	_ string,
) (bool, error) {
	return m.userExists, nil
}

func (m *mockRoleOverrideRepository) HasRoleOverride(
	_ context.Context,
	_, _ string,
) (bool, error) {
	return false, nil
}

// mockCoreClientForRoles captures WriteAuditLog calls.
// It embeds pb.CoreServiceClient (satisfies the interface); only WriteAuditLog
// is overridden to record calls without a real gRPC connection.
// (Re-uses the audit recording pattern from deactivation_handler_test.go)
type mockCoreClientForRoles struct {
	pb.CoreServiceClient // embed to satisfy interface; other methods panic if called

	auditCalled bool
	auditAction string
}

func (m *mockCoreClientForRoles) WriteAuditLog(
	_ context.Context,
	req *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	m.auditCalled = true
	m.auditAction = req.GetAction()
	return &pb.WriteAuditLogResponse{}, nil
}

// ── Helper: build POST /api/v1/admin/users/{userId}/roles request ─────────────

func buildRoleAssignRequest(t *testing.T, actorID, userID, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/users/"+userID+"/roles",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, actorID)
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
	return req.WithContext(ctx)
}

// ── Handler via direct AdminServer call (bypasses router for unit isolation) ──

// callAssignRole builds an AdminServer with the given repo and calls
// AssignAdminUserRole via the mux registered in RegisterAdminRoutes.
// coreClient may be nil (audit log skipped) or a *mockCoreClientForRoles.
//
// This mirrors the pattern used in deactivation_handler_test.go.
func callAssignRole(
	t *testing.T,
	rolesRepo api.RoleOverrideRepository,
	coreClient pb.CoreServiceClient,
	actorID, userID, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	adminSrv := &api.AdminServer{
		Roles:      rolesRepo,
		CoreClient: coreClient,
	}

	mux := http.NewServeMux()
	// Register route with identity JWT middleware (role already in context).
	noopJWT := func(h http.Handler) http.Handler { return h }
	api.RegisterAdminRoutes(mux, adminSrv, noopJWT, nil)

	req := buildRoleAssignRequest(t, actorID, userID, body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── Acceptance Test #1: Grant role to existing user → 200 ────────────────────

// TestAssignAdminUserRole_GrantRole_Returns200 covers AC#2 + Acceptance Test #1:
// Granting a valid role to an existing user must return 200 with action="granted".
//
// [P0] — happy path: the core use case of this story.
func TestAssignAdminUserRole_GrantRole_Returns200(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"compliance_officer","action":"grant"}`,
	)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'data' object in response, got: %v", resp)
	}
	if data["user_id"] != "@alice:example.com" {
		t.Errorf("expected user_id '@alice:example.com', got %v", data["user_id"])
	}
	if data["role"] != "compliance_officer" {
		t.Errorf("expected role 'compliance_officer', got %v", data["role"])
	}
	if data["action"] != "granted" {
		t.Errorf("expected action 'granted', got %v", data["action"])
	}
}

// ── Acceptance Test #2: Revoke existing override → 200 ───────────────────────

// TestAssignAdminUserRole_RevokeRole_Returns200 covers AC#2 + Acceptance Test #2:
// Revoking an existing role override must return 200 with action="revoked".
//
// [P0] — happy path: revocation works when override exists.
func TestAssignAdminUserRole_RevokeRole_Returns200(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true, revokeErr: nil}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"compliance_officer","action":"revoke"}`,
	)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'data' object in response, got: %v", resp)
	}
	if data["action"] != "revoked" {
		t.Errorf("expected action 'revoked', got %v", data["action"])
	}
}

// ── Acceptance Test #3: Revoke non-existent override → 404 ───────────────────

// TestAssignAdminUserRole_RevokeNonExistent_Returns404 covers AC#2 + Acceptance Test #3:
// Revoking an override that does not exist must return 404 M_NOT_FOUND.
//
// [P0] — security/correctness: attempting to revoke a non-existent grant is a user error.
func TestAssignAdminUserRole_RevokeNonExistent_Returns404(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{
		userExists: true,
		revokeErr:  api.ErrRoleOverrideNotFound,
	}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"instance_admin","action":"revoke"}`,
	)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' object in response, got: %v", body)
	}
	if errObj["code"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %v", errObj["code"])
	}
	if errObj["message"] != "Role override not found" {
		t.Errorf("expected message 'Role override not found', got %v", errObj["message"])
	}
}

// ── Acceptance Test #4: Self-revoke own instance_admin → 403 ─────────────────

// TestAssignAdminUserRole_SelfRevoke_Returns403 covers AC#2 + Acceptance Test #4:
// An admin cannot revoke their own instance_admin role.
// The self-revoke guard must fire regardless of whether a DB override exists.
//
// [P0] — security: prevents lockout of the only admin.
func TestAssignAdminUserRole_SelfRevoke_Returns403(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@admin:example.com", // same as actor
		`{"role":"instance_admin","action":"revoke"}`,
	)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' object in response, got: %v", body)
	}
	if errObj["code"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %v", errObj["code"])
	}
	if errObj["message"] != "Cannot revoke your own admin role" {
		t.Errorf("expected message 'Cannot revoke your own admin role', got %v", errObj["message"])
	}
}

// ── Acceptance Test #5: Invalid role value → 400 ─────────────────────────────

// TestAssignAdminUserRole_InvalidRole_Returns400 covers AC#2 + Acceptance Test #5:
// A request with an unrecognised role must return 400 M_BAD_JSON.
//
// [P1] — input validation: guard against typos and injection.
func TestAssignAdminUserRole_InvalidRole_Returns400(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"superadmin","action":"grant"}`,
	)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' object in response, got: %v", body)
	}
	if errObj["code"] != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %v", errObj["code"])
	}
}

// TestAssignAdminUserRole_InvalidAction_Returns400 covers AC#2:
// A request with an unrecognised action must return 400 M_BAD_JSON.
//
// [P1] — input validation: only "grant" and "revoke" are valid.
func TestAssignAdminUserRole_InvalidAction_Returns400(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"instance_admin","action":"delete"}`,
	)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestAssignAdminUserRole_MissingBody_Returns400 covers AC#2:
// A request with a missing or empty body must return 400 M_BAD_JSON.
//
// [P1] — input validation: body is required.
func TestAssignAdminUserRole_MissingBody_Returns400(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}

	adminSrv := &api.AdminServer{Roles: repo}
	mux := http.NewServeMux()
	noopJWT := func(h http.Handler) http.Handler { return h }
	api.RegisterAdminRoutes(mux, adminSrv, noopJWT, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/users/@alice:example.com/roles",
		nil, // no body
	)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, "@admin:example.com")
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing body, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestAssignAdminUserRole_UnknownUser_Returns404 covers AC#2:
// A request targeting a user_id that does not exist in the users table must return 404.
//
// [P1] — correctness: grant/revoke for ghost users is meaningless.
func TestAssignAdminUserRole_UnknownUser_Returns404(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: false} // user does not exist
	w := callAssignRole(t, repo, nil,
		"@admin:example.com",
		"@ghost:example.com",
		`{"role":"compliance_officer","action":"grant"}`,
	)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown user, got %d (body: %s)", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' object in response, got: %v", body)
	}
	if errObj["code"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND for unknown user, got %v", errObj["code"])
	}
}

// ── Audit log tests ───────────────────────────────────────────────────────────

// TestAssignAdminUserRole_Grant_CallsAuditLog covers AC#2:
// A successful grant must call audit.LogEvent with action="role_granted".
//
// [P1] — compliance: all role changes must be audited.
func TestAssignAdminUserRole_Grant_CallsAuditLog(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true}

	auditClient := &mockCoreClientForRoles{}
	w := callAssignRole(t, repo, auditClient,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"compliance_officer","action":"grant"}`,
	)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !auditClient.auditCalled {
		t.Error("expected audit.LogEvent to be called on successful grant, but it was not")
	}
	if auditClient.auditAction != "role_granted" {
		t.Errorf("expected audit action 'role_granted', got %q", auditClient.auditAction)
	}
}

// TestAssignAdminUserRole_Revoke_CallsAuditLog covers AC#2:
// A successful revoke must call audit.LogEvent with action="role_revoked".
//
// [P1] — compliance: all role changes must be audited.
func TestAssignAdminUserRole_Revoke_CallsAuditLog(t *testing.T) {
	// THIS TEST WILL FAIL — AssignAdminUserRole is not implemented yet.

	repo := &mockRoleOverrideRepository{userExists: true, revokeErr: nil}
	auditClient := &mockCoreClientForRoles{}
	w := callAssignRole(t, repo, auditClient,
		"@admin:example.com",
		"@alice:example.com",
		`{"role":"compliance_officer","action":"revoke"}`,
	)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !auditClient.auditCalled {
		t.Error("expected audit.LogEvent to be called on successful revoke, but it was not")
	}
	if auditClient.auditAction != "role_revoked" {
		t.Errorf("expected audit action 'role_revoked', got %q", auditClient.auditAction)
	}
}

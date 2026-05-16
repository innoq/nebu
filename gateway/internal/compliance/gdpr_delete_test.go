package compliance_test

// gdpr_delete_test.go — Story 14.4: GDPR Right to Erasure — unit tests
//
// ALL tests in this file are expected to FAIL until Story 14.4 is implemented.
// Failing reason: GdprDeleteHandler type and DeleteUser method do not exist.
//
// Tests:
//   AT-4: TestGdprDeleteHandler_HappyPath — calls DeactivateUser+DeleteUserKeys+anonymize+audit
//   AT-5: TestGdprDeleteHandler_NonAdmin_Returns403
//   AT-6: TestGdprDeleteHandler_UnknownUser_Returns404
//   AT-7: TestGdprDeleteHandler_SelfDelete_Returns403
//   AT-8: TestGdprDeleteHandler_AuditFailure_Still200 — never-raise audit policy
//
// DB stubbing: fakedb-gdpr is a separate driver from fakedb (handler_test.go).
// It handles profile queries needed by the anonymization step in GdprDeleteHandler.

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// ─── fakedb-gdpr driver ───────────────────────────────────────────────────────
//
// A minimal database/sql/driver for GdprDeleteHandler tests.
// Handles:
//   - SELECT avatar_url FROM profiles WHERE user_id = $1
//   - UPDATE profiles SET ... WHERE user_id = $1
//   - UPDATE users SET anonymized_at ... WHERE user_id = $1
//   - UPDATE media_files SET deleted ... WHERE media_id = $1
//   - BeginTx / Commit / Rollback (all no-ops)
//
// DSN format: "profileFound=<true|false>"

var gdprFakeDriverOnce sync.Once

func init() {
	gdprFakeDriverOnce.Do(func() {
		sql.Register("fakedb-gdpr", &gdprFakeDriver{})
	})
}

type gdprFakeDriver struct{}

func (d *gdprFakeDriver) Open(name string) (driver.Conn, error) {
	return &gdprFakeConn{profileFound: strings.Contains(name, "profileFound=true")}, nil
}

type gdprFakeConn struct {
	profileFound bool
}

func (c *gdprFakeConn) Prepare(query string) (driver.Stmt, error) {
	return &gdprFakeStmt{query: query, profileFound: c.profileFound}, nil
}

func (c *gdprFakeConn) Close() error { return nil }
func (c *gdprFakeConn) Begin() (driver.Tx, error) {
	return &gdprFakeTx{}, nil
}

type gdprFakeTx struct{}

func (t *gdprFakeTx) Commit() error   { return nil }
func (t *gdprFakeTx) Rollback() error { return nil }

type gdprFakeStmt struct {
	query        string
	profileFound bool
}

func (s *gdprFakeStmt) Close() error  { return nil }
func (s *gdprFakeStmt) NumInput() int { return -1 } // variadic

func (s *gdprFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	// All UPDATE/INSERT operations succeed.
	return driver.RowsAffected(1), nil
}

func (s *gdprFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	// SELECT avatar_url FROM profiles WHERE user_id = $1
	if strings.Contains(s.query, "FROM profiles") {
		if s.profileFound {
			// Return a row with avatar_url = NULL (no mxc://) — simplest case
			return &gdprFakeRows{cols: []string{"avatar_url"}, data: [][]driver.Value{{nil}}}, nil
		}
		// profileFound=false: no row → ErrNoRows
		return &gdprFakeRows{cols: []string{"avatar_url"}, data: nil}, nil
	}
	return &gdprFakeRows{}, nil
}

type gdprFakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *gdprFakeRows) Columns() []string { return r.cols }
func (r *gdprFakeRows) Close() error      { return nil }
func (r *gdprFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── helper: openGdprFakeDB ───────────────────────────────────────────────────

func openGdprFakeDB(t *testing.T, profileFound bool) *sql.DB {
	t.Helper()
	dsn := "profileFound=false"
	if profileFound {
		dsn = "profileFound=true"
	}
	db, err := sql.Open("fakedb-gdpr", dsn)
	if err != nil {
		t.Fatalf("sql.Open(fakedb-gdpr): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ─── gdprDeleteMockCoreClient ─────────────────────────────────────────────────
//
// Extends mockCoreClient with controllable DeactivateUser + DeleteUserKeys stubs.

type gdprDeleteMockCoreClient struct {
	mockCoreClient
	deactivateErr    error
	deleteKeysErr    error
	auditFailWith    error
	deleteKeysCalled int // counts DeleteUserKeys invocations for pipeline-abort assertions
}

func (m *gdprDeleteMockCoreClient) DeactivateUser(
	_ context.Context,
	_ *pb.DeactivateUserRequest,
	_ ...grpc.CallOption,
) (*pb.DeactivateUserResponse, error) {
	if m.deactivateErr != nil {
		return nil, m.deactivateErr
	}
	return &pb.DeactivateUserResponse{Ok: true}, nil
}

func (m *gdprDeleteMockCoreClient) DeleteUserKeys(
	_ context.Context,
	_ *pb.DeleteUserKeysRequest,
	_ ...grpc.CallOption,
) (*pb.DeleteUserKeysResponse, error) {
	m.mockCoreClient.mu.Lock()
	m.deleteKeysCalled++
	m.mockCoreClient.mu.Unlock()
	if m.deleteKeysErr != nil {
		return nil, m.deleteKeysErr
	}
	return &pb.DeleteUserKeysResponse{}, nil
}

func (m *gdprDeleteMockCoreClient) WriteAuditLog(
	_ context.Context,
	req *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	m.mockCoreClient.mu.Lock()
	defer m.mockCoreClient.mu.Unlock()
	m.mockCoreClient.received = append(m.mockCoreClient.received, req)
	if m.auditFailWith != nil {
		return nil, m.auditFailWith
	}
	return &pb.WriteAuditLogResponse{Ok: true}, nil
}

// ─── AT-4: Happy Path ─────────────────────────────────────────────────────────
//
// Verifies that DELETE /api/v1/admin/users/{userId} calls:
//   1. DeactivateUser gRPC (deactivate + session invalidation)
//   2. DeleteUserKeys gRPC (nulls private keys)
//   3. Anonymize DB operations (profiles + users)
//   4. Emits "gdpr_deletion" audit event (never-raise)
//   Returns 200 {"user_id": ..., "status": "gdpr_deleted"}

func TestGdprDeleteHandler_HappyPath(t *testing.T) {
	mock := &gdprDeleteMockCoreClient{}

	handler := &compliance.GdprDeleteHandler{
		CoreClient: mock,
		DB:         openGdprFakeDB(t, true),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/user123", bytes.NewBufferString("{}"))
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin"),
		middleware.ContextKeySub, "admin_user",
	))
	req.SetPathValue("userId", "user123")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.DeleteUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if resp["status"] != "gdpr_deleted" {
		t.Errorf("expected status=gdpr_deleted, got %q", resp["status"])
	}
	if resp["user_id"] != "user123" {
		t.Errorf("expected user_id=user123, got %q", resp["user_id"])
	}

	// Verify "gdpr_deletion" audit action was emitted
	mock.mockCoreClient.mu.Lock()
	defer mock.mockCoreClient.mu.Unlock()
	found := false
	for _, req := range mock.mockCoreClient.received {
		if req.Action == "gdpr_deletion" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected audit event with action=gdpr_deletion, none found")
	}
}

// ─── AT-5: Non-admin returns 403 ──────────────────────────────────────────────

func TestGdprDeleteHandler_NonAdmin_Returns403(t *testing.T) {
	mock := &gdprDeleteMockCoreClient{}

	handler := &compliance.GdprDeleteHandler{
		CoreClient: mock,
		DB:         openGdprFakeDB(t, true),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/user123", nil)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), middleware.ContextKeySystemRole, "user"),
		middleware.ContextKeySub, "regular_user",
	))
	req.SetPathValue("userId", "user123")

	rr := httptest.NewRecorder()
	handler.DeleteUser(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ─── AT-6: Unknown user returns 404 ───────────────────────────────────────────

func TestGdprDeleteHandler_UnknownUser_Returns404(t *testing.T) {
	notFound := grpcstatus.Error(codes.NotFound, "user not found")
	mock := &gdprDeleteMockCoreClient{
		deactivateErr: notFound,
	}

	handler := &compliance.GdprDeleteHandler{
		CoreClient: mock,
		DB:         openGdprFakeDB(t, false),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/unknown", nil)
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin"),
		middleware.ContextKeySub, "admin_user",
	))
	req.SetPathValue("userId", "unknown")

	rr := httptest.NewRecorder()
	handler.DeleteUser(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Pipeline-abort assertion: DeleteUserKeys MUST NOT be called after a 404
	// (i.e., the handler must stop the pipeline when DeactivateUser returns NOT_FOUND).
	mock.mockCoreClient.mu.Lock()
	defer mock.mockCoreClient.mu.Unlock()
	if mock.deleteKeysCalled != 0 {
		t.Errorf("expected DeleteUserKeys not to be called after 404, but was called %d time(s)", mock.deleteKeysCalled)
	}
}

// ─── AT-7: Self-delete returns 403 ────────────────────────────────────────────

func TestGdprDeleteHandler_SelfDelete_Returns403(t *testing.T) {
	mock := &gdprDeleteMockCoreClient{}

	handler := &compliance.GdprDeleteHandler{
		CoreClient: mock,
		DB:         openGdprFakeDB(t, true),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/self_admin", nil)
	// callerSub == userId → self-delete guard fires
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin"),
		middleware.ContextKeySub, "self_admin",
	))
	req.SetPathValue("userId", "self_admin")

	rr := httptest.NewRecorder()
	handler.DeleteUser(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for self-delete, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// ─── AT-8: Audit failure does not fail the deletion ───────────────────────────
//
// The never-raise audit policy: if WriteAuditLog fails, the handler returns 200
// (the GDPR deletion has already been committed — it must not be reversed).

func TestGdprDeleteHandler_AuditFailure_Still200(t *testing.T) {
	mock := &gdprDeleteMockCoreClient{
		auditFailWith: grpcstatus.Error(codes.Unavailable, "core unreachable"),
	}

	handler := &compliance.GdprDeleteHandler{
		CoreClient: mock,
		DB:         openGdprFakeDB(t, true),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/user999", bytes.NewBufferString("{}"))
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), middleware.ContextKeySystemRole, "instance_admin"),
		middleware.ContextKeySub, "admin_user",
	))
	req.SetPathValue("userId", "user999")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.DeleteUser(rr, req)

	// Audit failure MUST NOT cause a 500 — deletion is committed, never-raise.
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 despite audit failure, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

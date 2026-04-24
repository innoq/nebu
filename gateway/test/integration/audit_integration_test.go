//go:build integration

package integration_test

// Story 5-2 — AC5, AC6, AC7, AC11 (Tests 8–10): Admin-event audit smoke tests
//
// ALL tests in this file are expected to FAIL until Story 5-2 is implemented.
// Failing reasons:
//   - audit.LogEvent does not exist yet
//   - pb.WriteAuditLogRequest/WriteAuditLogResponse not in CoreServiceClient
//   - AdminAuth.SetCoreClient not wired up in main.go
//   - The mock gRPC core below will not receive WriteAuditLog calls because
//     the integration points in auth.go do not exist yet
//
// Test strategy:
//   - A mockAuditCoreServer intercepts gRPC calls to WriteAuditLog.
//   - The test spins up:
//       * a real gateway HTTP stack (via the existing integration test helpers)
//       * a local gRPC server running mockAuditCoreServer
//   - HTTP requests exercise the real CallbackHandler / ClaimSelectionHandler /
//     LogoutHandler code paths.
//   - Assertions check that WriteAuditLog was called with the correct action field.
//   - These tests are in the `integration` package and run with: go test -tags=integration
//
// NOTE: Full wiring (gRPC test server + gateway constructor injection) will be
// completed during implementation. The test stubs below compile and fail because:
//   1. pb.CoreServiceServer does not yet have WriteAuditLog method
//   2. mockAuditCoreServer does not implement the full CoreServiceServer interface
//   3. Assertion helpers reference state that the unimplemented handler never sets
//
// Build tag: integration

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ─── mockAuditCoreServer ──────────────────────────────────────────────────────
//
// Records WriteAuditLog calls. All other RPCs return Unimplemented.
// Intentionally does NOT implement full pb.CoreServiceServer — test will fail
// to compile once WriteAuditLog is added to the proto (expected red state).

type mockAuditCoreServer struct {
	pb.UnimplementedCoreServiceServer

	mu       sync.Mutex
	received []*pb.WriteAuditLogRequest
}

// WriteAuditLog records the call and returns ok=true.
// This method signature will only compile once the proto rpc WriteAuditLog
// is defined and regenerated via `make proto`. Until then this file causes
// a compile error — confirming the red state.
func (m *mockAuditCoreServer) WriteAuditLog(
	_ context.Context,
	req *pb.WriteAuditLogRequest,
) (*pb.WriteAuditLogResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, req)
	return &pb.WriteAuditLogResponse{Ok: true}, nil
}

func (m *mockAuditCoreServer) lastReceivedAction() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.received) == 0 {
		return ""
	}
	return m.received[len(m.received)-1].Action
}

func (m *mockAuditCoreServer) receivedActions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	actions := make([]string, len(m.received))
	for i, r := range m.received {
		actions[i] = r.Action
	}
	return actions
}

// ─── startMockAuditGRPCServer ─────────────────────────────────────────────────
//
// Starts a local gRPC server with mockAuditCoreServer. Returns the server,
// the listener address, and a cleanup function.

func startMockAuditGRPCServer(t *testing.T) (*mockAuditCoreServer, string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock gRPC listener: %v", err)
	}

	mock := &mockAuditCoreServer{}
	srv := grpc.NewServer()
	pb.RegisterCoreServiceServer(srv, mock)

	go func() {
		_ = srv.Serve(lis)
	}()

	cleanup := func() { srv.GracefulStop() }
	return mock, lis.Addr().String(), cleanup
}

// ─── dialMockCore ─────────────────────────────────────────────────────────────

func dialMockCore(t *testing.T, addr string) pb.CoreServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial mock gRPC core: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCoreServiceClient(conn)
}

// ─── waitForAuditAction ───────────────────────────────────────────────────────
//
// Polls until the mock server has received an audit log entry for the given
// action, or times out. Used because the HTTP handler may call WriteAuditLog
// asynchronously (fire-and-forget goroutine is not required, but the test
// tolerates a small delay).

func waitForAuditAction(t *testing.T, mock *mockAuditCoreServer, wantAction string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, a := range mock.receivedActions() {
			if a == wantAction {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("timed out waiting for audit action=%q; received: %v", wantAction, mock.receivedActions())
}

// ─── Test 8: TestAdminLogin_EmitsAuditEntry ───────────────────────────────────
//
// AC5 — Integration-Point 1: Admin Login success
//
// Given: A running gateway stack with mock gRPC core (WriteAuditLog instrumented)
// When: A valid OIDC callback triggers CallbackHandler (successful login)
// Then: WriteAuditLog was called with action="admin_login", outcome="success"
//
// Failing reason: CallbackHandler does not yet call audit.LogEvent.

func TestAdminLogin_EmitsAuditEntry(t *testing.T) {
	mock, addr, cleanup := startMockAuditGRPCServer(t)
	defer cleanup()

	// Wire the mock core client into the gateway under test.
	// TODO(5-2): Replace with real gateway construction using SetCoreClient once
	// the integration test harness supports injecting the gRPC client.
	// For now this test fails because:
	//   1. pb.WriteAuditLogRequest does not compile (proto not yet extended)
	//   2. No HTTP endpoint wiring to the mock server
	_ = dialMockCore(t, addr)

	// Placeholder assertion — will fail because audit action is never sent.
	waitForAuditAction(t, mock, "admin_login", 500*time.Millisecond)
	actions := mock.receivedActions()
	if len(actions) == 0 {
		t.Fatal("no audit log entries received — CallbackHandler must call audit.LogEvent with action='admin_login'")
	}
}

// ─── Test 9: TestBootstrap_EmitsAuditEntry ────────────────────────────────────
//
// AC6 — Integration-Point 2: Bootstrap completion
//
// Given: Running gateway with mock gRPC core
// When: ClaimSelectionHandler completes a successful bootstrap (txErr == nil)
// Then: WriteAuditLog called with action="bootstrap_completed",
//       metadata JSON contains instance_name and oidc_issuer
//
// Failing reason: ClaimSelectionHandler does not yet call audit.LogEvent.

func TestBootstrap_EmitsAuditEntry(t *testing.T) {
	mock, addr, cleanup := startMockAuditGRPCServer(t)
	defer cleanup()

	_ = dialMockCore(t, addr)

	waitForAuditAction(t, mock, "bootstrap_completed", 500*time.Millisecond)
	actions := mock.receivedActions()
	if len(actions) == 0 {
		t.Fatal("no audit log entries received — ClaimSelectionHandler must call audit.LogEvent with action='bootstrap_completed'")
	}
}

// ─── Test 10: TestAdminLogout_EmitsAuditEntry ─────────────────────────────────
//
// AC7 — Integration-Point 3: Admin Logout
//
// Given: An authenticated admin session; running gateway with mock gRPC core
// When: LogoutHandler is called
// Then: WriteAuditLog called with action="admin_logout", outcome="success"
//
// Failing reason: LogoutHandler does not yet call audit.LogEvent.

func TestAdminLogout_EmitsAuditEntry(t *testing.T) {
	mock, addr, cleanup := startMockAuditGRPCServer(t)
	defer cleanup()

	_ = dialMockCore(t, addr)

	waitForAuditAction(t, mock, "admin_logout", 500*time.Millisecond)
	actions := mock.receivedActions()
	if len(actions) == 0 {
		t.Fatal("no audit log entries received — LogoutHandler must call audit.LogEvent with action='admin_logout'")
	}
}

// ─── Test 11: TestAdminLoginFailure_EmitsAuditEntry ───────────────────────────
//
// AC5 — Integration-Point 1 (failure branch): Admin Login role check failed
//
// Given: Running gateway; OIDC callback completes but role check fails
// When: CallbackHandler redirects to /admin/login?error=auth_failed
// Then: WriteAuditLog called with action="admin_login_failed", outcome="failure",
//       error_detail="role_check_failed"
//
// Failing reason: Failure branch in CallbackHandler does not yet call audit.LogEvent.

func TestAdminLoginFailure_EmitsAuditEntry(t *testing.T) {
	mock, addr, cleanup := startMockAuditGRPCServer(t)
	defer cleanup()

	_ = dialMockCore(t, addr)

	waitForAuditAction(t, mock, "admin_login_failed", 500*time.Millisecond)
	actions := mock.receivedActions()
	if len(actions) == 0 {
		t.Fatal("no audit log entries received — CallbackHandler failure branch must call audit.LogEvent with action='admin_login_failed'")
	}
}

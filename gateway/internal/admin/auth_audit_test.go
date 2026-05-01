package admin

// Story 5-2 Nachbesserung: Unit-Tests für Audit-Emission in auth.go
//
// TEA Gate 2 stellte fest, dass AC5, AC6, AC7 keine echten Tests hatten.
// Die vorherigen "Integration-Tests" in audit_integration_test.go waren Stubs
// (Client wurde verworfen, kein HTTP-Request, immer Timeout).
//
// Strategie:
//   1. mockCoreClient implementiert pb.CoreServiceClient mit No-ops für alle
//      Methoden außer WriteAuditLog — dieser zeichnet den Aufruf auf.
//   2. logAuditEvent wird direkt getestet (Unit-Test) — verifiziert Action,
//      ActorUserId, Outcome, ErrorDetail.
//   3. Handler-Tests nutzen bestehende Test-Infrastruktur (fakeBootstrapDraftStore,
//      fakeAdminSessionStore, fakeServerConfigReader) und rufen die Handler über
//      httptest auf, um die Audit-Emission auf dem echten Aufrufpfad zu triggern.
//
// Call-Sites abgedeckt:
//   logAuditEvent direkt:
//     - TestLogAuditEvent_AdminLogin_Success
//     - TestLogAuditEvent_AdminLoginFailed_Failure
//     - TestLogAuditEvent_BootstrapCompleted_Success
//     - TestLogAuditEvent_AdminLogout_Success
//   Handler-Ebene (echter Aufrufpfad via httptest):
//     - TestLogoutHandler_WithLegacyCookie_EmitsAuditLog (LogoutHandler → admin_logout)
//     - TestLogoutHandler_WithSessionStore_EmitsAuditLog (LogoutHandler mit SID → admin_logout)
//     - TestCallbackHandler_RoleCheckFails_EmitsAuditLog (CallbackHandler → admin_login_failed)
//     - TestCallbackHandler_ValidLogin_EmitsAuditLog (CallbackHandler → admin_login)
//     - TestClaimSelectionHandler_EmitsBootstrapCompleted (ClaimSelectionHandler → bootstrap_completed)

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ---------------------------------------------------------------------------
// mockCoreClient — records WriteAuditLog calls; all other methods are no-ops.
// ---------------------------------------------------------------------------

type mockCoreClient struct {
	mu       sync.Mutex
	received []*pb.WriteAuditLogRequest
}

func (m *mockCoreClient) WriteAuditLog(_ context.Context, req *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, req)
	return &pb.WriteAuditLogResponse{Ok: true}, nil
}

// All other CoreServiceClient methods — no-op stubs matching the actual interface.

func (m *mockCoreClient) SendEvent(_ context.Context, _ *pb.SendEventRequest, _ ...grpc.CallOption) (*pb.SendEventResponse, error) {
	return &pb.SendEventResponse{}, nil
}

func (m *mockCoreClient) CreateRoom(_ context.Context, _ *pb.CreateRoomRequest, _ ...grpc.CallOption) (*pb.CreateRoomResponse, error) {
	return &pb.CreateRoomResponse{}, nil
}

func (m *mockCoreClient) JoinRoom(_ context.Context, _ *pb.JoinRoomRequest, _ ...grpc.CallOption) (*pb.JoinRoomResponse, error) {
	return &pb.JoinRoomResponse{}, nil
}

func (m *mockCoreClient) LeaveRoom(_ context.Context, _ *pb.LeaveRoomRequest, _ ...grpc.CallOption) (*pb.LeaveRoomResponse, error) {
	return &pb.LeaveRoomResponse{}, nil
}

func (m *mockCoreClient) GetMessages(_ context.Context, _ *pb.GetMessagesRequest, _ ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	return &pb.GetMessagesResponse{}, nil
}

func (m *mockCoreClient) SetPresence(_ context.Context, _ *pb.SetPresenceRequest, _ ...grpc.CallOption) (*pb.SetPresenceResponse, error) {
	return &pb.SetPresenceResponse{}, nil
}

func (m *mockCoreClient) SetTyping(_ context.Context, _ *pb.SetTypingRequest, _ ...grpc.CallOption) (*pb.SetTypingResponse, error) {
	return &pb.SetTypingResponse{}, nil
}

func (m *mockCoreClient) ValidateToken(_ context.Context, _ *pb.ValidateTokenRequest, _ ...grpc.CallOption) (*pb.ValidateTokenResponse, error) {
	return &pb.ValidateTokenResponse{}, nil
}

func (m *mockCoreClient) GetPendingEvents(_ context.Context, _ *pb.GetPendingEventsRequest, _ ...grpc.CallOption) (*pb.GetPendingEventsResponse, error) {
	return &pb.GetPendingEventsResponse{}, nil
}

func (m *mockCoreClient) EventBus(_ context.Context, _ *pb.EventBusRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.Event], error) {
	return nil, nil
}

func (m *mockCoreClient) GetMetrics(_ context.Context, _ *pb.GetMetricsRequest, _ ...grpc.CallOption) (*pb.GetMetricsResponse, error) {
	return &pb.GetMetricsResponse{}, nil
}

func (m *mockCoreClient) GetRoomState(_ context.Context, _ *pb.GetRoomStateRequest, _ ...grpc.CallOption) (*pb.GetRoomStateResponse, error) {
	return &pb.GetRoomStateResponse{}, nil
}

func (m *mockCoreClient) InviteUser(_ context.Context, _ *pb.InviteUserRequest, _ ...grpc.CallOption) (*pb.InviteUserResponse, error) {
	return &pb.InviteUserResponse{}, nil
}

func (m *mockCoreClient) SetPowerLevels(_ context.Context, _ *pb.SetPowerLevelsRequest, _ ...grpc.CallOption) (*pb.SetPowerLevelsResponse, error) {
	return &pb.SetPowerLevelsResponse{}, nil
}

func (m *mockCoreClient) SendReceipt(_ context.Context, _ *pb.SendReceiptRequest, _ ...grpc.CallOption) (*pb.SendReceiptResponse, error) {
	return &pb.SendReceiptResponse{}, nil
}

func (m *mockCoreClient) GetInitialSync(_ context.Context, _ *pb.GetInitialSyncRequest, _ ...grpc.CallOption) (*pb.GetInitialSyncResponse, error) {
	return &pb.GetInitialSyncResponse{}, nil
}

func (m *mockCoreClient) GetSyncDelta(_ context.Context, _ *pb.GetSyncDeltaRequest, _ ...grpc.CallOption) (*pb.GetSyncDeltaResponse, error) {
	return &pb.GetSyncDeltaResponse{}, nil
}

func (m *mockCoreClient) GetPresence(_ context.Context, _ *pb.GetPresenceRequest, _ ...grpc.CallOption) (*pb.GetPresenceResponse, error) {
	return &pb.GetPresenceResponse{}, nil
}

func (m *mockCoreClient) UpdateProfile(_ context.Context, _ *pb.UpdateProfileRequest, _ ...grpc.CallOption) (*pb.UpdateProfileResponse, error) {
	return &pb.UpdateProfileResponse{}, nil
}

func (m *mockCoreClient) DeleteUserKeys(_ context.Context, _ *pb.DeleteUserKeysRequest, _ ...grpc.CallOption) (*pb.DeleteUserKeysResponse, error) {
	return &pb.DeleteUserKeysResponse{}, nil
}
func (m *mockCoreClient) KickUser(_ context.Context, _ *pb.KickUserRequest, _ ...grpc.CallOption) (*pb.KickUserResponse, error) {
	return &pb.KickUserResponse{}, nil
}
func (m *mockCoreClient) BanUser(_ context.Context, _ *pb.BanUserRequest, _ ...grpc.CallOption) (*pb.BanUserResponse, error) {
	return &pb.BanUserResponse{}, nil
}
func (m *mockCoreClient) UnbanUser(_ context.Context, _ *pb.UnbanUserRequest, _ ...grpc.CallOption) (*pb.UnbanUserResponse, error) {
	return &pb.UnbanUserResponse{}, nil
}
func (m *mockCoreClient) ForgetRoom(_ context.Context, _ *pb.ForgetRoomRequest, _ ...grpc.CallOption) (*pb.ForgetRoomResponse, error) {
	return &pb.ForgetRoomResponse{}, nil
}
func (m *mockCoreClient) GetEventContext(_ context.Context, _ *pb.GetEventContextRequest, _ ...grpc.CallOption) (*pb.GetEventContextResponse, error) {
	return &pb.GetEventContextResponse{}, nil
}
func (m *mockCoreClient) ListPublicRooms(_ context.Context, _ *pb.ListPublicRoomsRequest, _ ...grpc.CallOption) (*pb.ListPublicRoomsResponse, error) {
	return &pb.ListPublicRoomsResponse{}, nil
}
func (m *mockCoreClient) InvalidateUserSessions(_ context.Context, _ *pb.InvalidateUserSessionsRequest, _ ...grpc.CallOption) (*pb.InvalidateUserSessionsResponse, error) {
	return &pb.InvalidateUserSessionsResponse{Ok: true}, nil
}

// lastReceived returns the most recently recorded request (nil if none).
func (m *mockCoreClient) lastReceived() *pb.WriteAuditLogRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.received) == 0 {
		return nil
	}
	return m.received[len(m.received)-1]
}

// callCount returns the number of WriteAuditLog calls recorded.
func (m *mockCoreClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.received)
}

// ---------------------------------------------------------------------------
// Helper: newAuditTestAdminAuth creates a minimal AdminAuth with only the
// fields needed for audit-emission tests (no DB, no template, no OIDC).
// ---------------------------------------------------------------------------

func newAuditTestAdminAuth(t *testing.T) (*AdminAuth, *mockCoreClient) {
	t.Helper()
	mock := &mockCoreClient{}
	a := NewAdminAuth(nil, "test-client-id", "test-client-secret", "nebu_role",
		[]byte("test-secret-key"), nil, nil)
	a.SetCoreClient(mock)
	return a, mock
}

// ---------------------------------------------------------------------------
// Direct logAuditEvent unit tests — verify req fields for each call-site.
// ---------------------------------------------------------------------------

// TestLogAuditEvent_AdminLogin_Success verifies that logAuditEvent sends the
// correct fields for a successful admin login (AC5 — success path).
func TestLogAuditEvent_AdminLogin_Success(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	a.logAuditEvent(context.Background(), "user:alice", "admin_login", "user", "user:alice", nil, "success", "")

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req := mock.lastReceived()
	if req.Action != "admin_login" {
		t.Errorf("Action: want %q, got %q", "admin_login", req.Action)
	}
	if req.ActorUserId != "user:alice" {
		t.Errorf("ActorUserId: want %q, got %q", "user:alice", req.ActorUserId)
	}
	if req.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req.Outcome)
	}
	if req.ErrorDetail != "" {
		t.Errorf("ErrorDetail: want empty, got %q", req.ErrorDetail)
	}
}

// TestLogAuditEvent_AdminLoginFailed_Failure verifies the admin_login_failed call-site (AC5 — failure path).
func TestLogAuditEvent_AdminLoginFailed_Failure(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	a.logAuditEvent(context.Background(), "user:bob", "admin_login_failed", "user", "user:bob", nil, "failure", "role_check_failed")

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req := mock.lastReceived()
	if req.Action != "admin_login_failed" {
		t.Errorf("Action: want %q, got %q", "admin_login_failed", req.Action)
	}
	if req.ActorUserId != "user:bob" {
		t.Errorf("ActorUserId: want %q, got %q", "user:bob", req.ActorUserId)
	}
	if req.Outcome != "failure" {
		t.Errorf("Outcome: want %q, got %q", "failure", req.Outcome)
	}
	if req.ErrorDetail != "role_check_failed" {
		t.Errorf("ErrorDetail: want %q, got %q", "role_check_failed", req.ErrorDetail)
	}
}

// TestLogAuditEvent_BootstrapCompleted_Success verifies the bootstrap_completed call-site (AC6).
func TestLogAuditEvent_BootstrapCompleted_Success(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	meta := map[string]any{
		"instance_name": "my-nebu",
		"oidc_issuer":   "https://auth.example.com",
	}
	a.logAuditEvent(context.Background(), "user:operator", "bootstrap_completed", "server", "", meta, "success", "")

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req := mock.lastReceived()
	if req.Action != "bootstrap_completed" {
		t.Errorf("Action: want %q, got %q", "bootstrap_completed", req.Action)
	}
	if req.ActorUserId != "user:operator" {
		t.Errorf("ActorUserId: want %q, got %q", "user:operator", req.ActorUserId)
	}
	if req.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req.Outcome)
	}
	// Verify metadata JSON contains expected keys.
	var got map[string]any
	if err := json.Unmarshal(req.MetadataJson, &got); err != nil {
		t.Fatalf("MetadataJson unmarshal: %v", err)
	}
	if got["instance_name"] != "my-nebu" {
		t.Errorf("MetadataJson[instance_name]: want %q, got %v", "my-nebu", got["instance_name"])
	}
	if got["oidc_issuer"] != "https://auth.example.com" {
		t.Errorf("MetadataJson[oidc_issuer]: want %q, got %v", "https://auth.example.com", got["oidc_issuer"])
	}
}

// TestLogAuditEvent_AdminLogout_Success verifies the admin_logout call-site (AC7).
func TestLogAuditEvent_AdminLogout_Success(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	a.logAuditEvent(context.Background(), "user:alice", "admin_logout", "user", "user:alice", nil, "success", "")

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req := mock.lastReceived()
	if req.Action != "admin_logout" {
		t.Errorf("Action: want %q, got %q", "admin_logout", req.Action)
	}
	if req.ActorUserId != "user:alice" {
		t.Errorf("ActorUserId: want %q, got %q", "user:alice", req.ActorUserId)
	}
	if req.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req.Outcome)
	}
}

// TestLogAuditEvent_NilClient_IsNoop verifies that logAuditEvent with nil coreClient
// is a no-op and does not panic (backward-compat for environments without gRPC core).
func TestLogAuditEvent_NilClient_IsNoop(t *testing.T) {
	a := NewAdminAuth(nil, "", "", "", []byte("secret"), nil, nil)
	// coreClient is nil — must not panic.
	a.logAuditEvent(context.Background(), "actor", "admin_login", "user", "actor", nil, "success", "")
}

// ---------------------------------------------------------------------------
// Handler-level tests — exercise the real audit call-sites in auth.go handlers.
// ---------------------------------------------------------------------------

// TestLogoutHandler_WithLegacyCookie_EmitsAuditLog verifies that LogoutHandler
// calls logAuditEvent with action="admin_logout" when a valid legacy session cookie
// is present (the stateless cookie path). This covers the real call-site at line ~951.
func TestLogoutHandler_WithLegacyCookie_EmitsAuditLog(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	// Build a signed legacy session cookie (sub embedded directly).
	sess := adminSessionCookie{
		Sub:   "user:alice",
		Email: "alice@example.com",
		Role:  "instance_admin",
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	}
	payload, _ := json.Marshal(sess)
	cookieValue := a.signCookie(payload)

	req := httptest.NewRequest("GET", "/admin/logout", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.LogoutHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req2 := mock.lastReceived()
	if req2.Action != "admin_logout" {
		t.Errorf("Action: want %q, got %q", "admin_logout", req2.Action)
	}
	if req2.ActorUserId != "user:alice" {
		t.Errorf("ActorUserId: want %q, got %q", "user:alice", req2.ActorUserId)
	}
	if req2.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req2.Outcome)
	}
}

// TestLogoutHandler_WithSessionStore_EmitsAuditLog verifies the SID-based session path
// in LogoutHandler also emits admin_logout with the correct actor.
func TestLogoutHandler_WithSessionStore_EmitsAuditLog(t *testing.T) {
	store := newFakeAdminSessionStore()
	store.seed(AdminSession{
		SID:       "test-sid-alice",
		UserID:    "user:alice",
		ExpiresAt: time.Now().Add(8 * time.Hour),
	})

	a, mock := newAuditTestAdminAuth(t)
	a.SetSessionStore(store)

	sidPayload, _ := json.Marshal(adminSessionSIDCookie{SID: "test-sid-alice"})
	cookieValue := a.signCookie(sidPayload)

	req := httptest.NewRequest("GET", "/admin/logout", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.LogoutHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req2 := mock.lastReceived()
	if req2.Action != "admin_logout" {
		t.Errorf("Action: want %q, got %q", "admin_logout", req2.Action)
	}
	if req2.ActorUserId != "user:alice" {
		t.Errorf("ActorUserId: want %q, got %q", "user:alice", req2.ActorUserId)
	}
	if req2.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req2.Outcome)
	}
}

// TestCallbackHandler_RoleCheckFails_EmitsAuditLog verifies that the admin_login_failed
// audit event is emitted when role check fails in CallbackHandler. This is the real
// call-site at line ~677. Uses the real OIDC server (returning a non-admin role).
func TestCallbackHandler_RoleCheckFails_EmitsAuditLog(t *testing.T) {
	// OIDC server returning role "user" (not "instance_admin").
	srv, _ := setupAdminOIDCServerWithRole(t, "user")

	a, mock := newAuditTestAdminAuth(t)
	a.configReader = &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	cookieValue := buildValidStateCookie(t, a, "mystate")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req2 := mock.lastReceived()
	if req2.Action != "admin_login_failed" {
		t.Errorf("Action: want %q, got %q", "admin_login_failed", req2.Action)
	}
	if req2.Outcome != "failure" {
		t.Errorf("Outcome: want %q, got %q", "failure", req2.Outcome)
	}
	if req2.ErrorDetail != "role_check_failed" {
		t.Errorf("ErrorDetail: want %q, got %q", "role_check_failed", req2.ErrorDetail)
	}
}

// TestCallbackHandler_ValidLogin_EmitsAuditLog verifies that admin_login (success) is
// emitted on the legacy stateless cookie path in CallbackHandler (line ~750).
func TestCallbackHandler_ValidLogin_EmitsAuditLog(t *testing.T) {
	// OIDC server returning "instance_admin" role (same as signAdminJWT default).
	srv, _ := setupAdminOIDCServer(t)

	a, mock := newAuditTestAdminAuth(t)
	a.configReader = &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	cookieValue := buildValidStateCookie(t, a, "mystate")

	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d", mock.callCount())
	}
	req2 := mock.lastReceived()
	if req2.Action != "admin_login" {
		t.Errorf("Action: want %q, got %q", "admin_login", req2.Action)
	}
	if req2.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req2.Outcome)
	}
}

// TestClaimSelectionHandler_EmitsBootstrapCompleted verifies that ClaimSelectionHandler
// emits bootstrap_completed after a successful transaction (AC6, call-site line ~848).
// Uses fakeBootstrapDraftStore + fakeServerConfigReader + a fake runInTx that always succeeds.
func TestClaimSelectionHandler_EmitsBootstrapCompleted(t *testing.T) {
	a, mock := newAuditTestAdminAuth(t)

	// Wire a draft store pre-populated with bootstrap state.
	draft := &fakeBootstrapDraftStore{
		data: map[string]string{
			"bootstrap_sub":      "user:operator",
			"bootstrap_email":    "operator@example.com",
			"instance_name":      "test-nebu",
			"oidc_issuer":        "https://auth.example.com",
			"oidc_client_id":     "client-123",
			"oidc_client_secret": "enc-secret-xyz",
		},
	}
	a.draftStore = draft
	a.configReader = &fakeServerConfigReader{
		issuer:       "https://auth.example.com",
		clientID:     "client-123",
		clientSecret: "test-secret",
	}

	// Inject a fake runInTx that reports success without touching a real DB.
	// The fn (which calls completeBootstrapTx etc.) is intentionally skipped —
	// we only care that the audit emission AFTER the tx block is called correctly.
	// Calling fn would require a real *sql.Tx; skipping it is the correct isolation
	// boundary here: the tx correctness is covered by claim_selection_tx_test.go.
	a.runInTx = func(_ context.Context, _ func(q sqlQuerier) error) error {
		return nil // tx succeeded
	}

	// Build a POST form request.
	form := url.Values{"admin_group_claim": {"instance_admin"}}
	req := httptest.NewRequest("POST", "/admin/bootstrap/select-claim",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.ClaimSelectionHandler(rr, req)

	// Outcome: handler redirects or returns — either way, audit must have been called.
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 WriteAuditLog call, got %d (status=%d, body=%s)",
			mock.callCount(), rr.Code, rr.Body.String())
	}
	req2 := mock.lastReceived()
	if req2.Action != "bootstrap_completed" {
		t.Errorf("Action: want %q, got %q", "bootstrap_completed", req2.Action)
	}
	if req2.ActorUserId != "user:operator" {
		t.Errorf("ActorUserId: want %q, got %q", "user:operator", req2.ActorUserId)
	}
	if req2.Outcome != "success" {
		t.Errorf("Outcome: want %q, got %q", "success", req2.Outcome)
	}
}


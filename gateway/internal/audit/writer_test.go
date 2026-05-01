package audit_test

// Story 5-2 — AC4, AC11 (Tests 5–7): Go LogEvent unit tests
//
// ALL tests in this file are expected to FAIL until Story 5-2 is implemented.
// Failing reason: audit.LogEvent does not exist yet — compile error.
//
// Test strategy:
//   - mockCoreClient implements pb.CoreServiceClient with a stub WriteAuditLog
//     that the caller controls via a function field. All other methods panic —
//     only WriteAuditLog is exercised here.
//   - Tests do NOT use the integration build tag — they run in the default build.
//   - slog output is not asserted; only return-value semantics are checked.
//   - Never-raise semantics: LogEvent MUST return nil even when the gRPC call
//     fails. A gRPC error is logged at Warn level and swallowed. This matches
//     the Elixir policy (never raises, returns {:error, ...} at most).

import (
	"context"
	"encoding/json"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"

	"github.com/nebu/nebu/internal/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── mockCoreClient ──────────────────────────────────────────────────────────
//
// Minimal implementation of pb.CoreServiceClient. Only WriteAuditLog is used;
// all other methods panic to surface unexpected calls.

type mockCoreClient struct {
	writeAuditLogFn func(ctx context.Context, req *pb.WriteAuditLogRequest, opts ...grpc.CallOption) (*pb.WriteAuditLogResponse, error)
	capturedRequest *pb.WriteAuditLogRequest
}

func (m *mockCoreClient) WriteAuditLog(ctx context.Context, req *pb.WriteAuditLogRequest, opts ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
	m.capturedRequest = req
	return m.writeAuditLogFn(ctx, req, opts...)
}

// Stub implementations for all other CoreServiceClient methods — panic on call.

func (m *mockCoreClient) SendEvent(ctx context.Context, in *pb.SendEventRequest, opts ...grpc.CallOption) (*pb.SendEventResponse, error) {
	panic("unexpected call: SendEvent")
}
func (m *mockCoreClient) CreateRoom(ctx context.Context, in *pb.CreateRoomRequest, opts ...grpc.CallOption) (*pb.CreateRoomResponse, error) {
	panic("unexpected call: CreateRoom")
}
func (m *mockCoreClient) JoinRoom(ctx context.Context, in *pb.JoinRoomRequest, opts ...grpc.CallOption) (*pb.JoinRoomResponse, error) {
	panic("unexpected call: JoinRoom")
}
func (m *mockCoreClient) LeaveRoom(ctx context.Context, in *pb.LeaveRoomRequest, opts ...grpc.CallOption) (*pb.LeaveRoomResponse, error) {
	panic("unexpected call: LeaveRoom")
}
func (m *mockCoreClient) GetMessages(ctx context.Context, in *pb.GetMessagesRequest, opts ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	panic("unexpected call: GetMessages")
}
func (m *mockCoreClient) SetPresence(ctx context.Context, in *pb.SetPresenceRequest, opts ...grpc.CallOption) (*pb.SetPresenceResponse, error) {
	panic("unexpected call: SetPresence")
}
func (m *mockCoreClient) SetTyping(ctx context.Context, in *pb.SetTypingRequest, opts ...grpc.CallOption) (*pb.SetTypingResponse, error) {
	panic("unexpected call: SetTyping")
}
func (m *mockCoreClient) ValidateToken(ctx context.Context, in *pb.ValidateTokenRequest, opts ...grpc.CallOption) (*pb.ValidateTokenResponse, error) {
	panic("unexpected call: ValidateToken")
}
func (m *mockCoreClient) GetPendingEvents(ctx context.Context, in *pb.GetPendingEventsRequest, opts ...grpc.CallOption) (*pb.GetPendingEventsResponse, error) {
	panic("unexpected call: GetPendingEvents")
}
func (m *mockCoreClient) EventBus(ctx context.Context, in *pb.EventBusRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.Event], error) {
	panic("unexpected call: EventBus")
}
func (m *mockCoreClient) GetMetrics(ctx context.Context, in *pb.GetMetricsRequest, opts ...grpc.CallOption) (*pb.GetMetricsResponse, error) {
	panic("unexpected call: GetMetrics")
}
func (m *mockCoreClient) GetRoomState(ctx context.Context, in *pb.GetRoomStateRequest, opts ...grpc.CallOption) (*pb.GetRoomStateResponse, error) {
	panic("unexpected call: GetRoomState")
}
func (m *mockCoreClient) InviteUser(ctx context.Context, in *pb.InviteUserRequest, opts ...grpc.CallOption) (*pb.InviteUserResponse, error) {
	panic("unexpected call: InviteUser")
}
func (m *mockCoreClient) SetPowerLevels(ctx context.Context, in *pb.SetPowerLevelsRequest, opts ...grpc.CallOption) (*pb.SetPowerLevelsResponse, error) {
	panic("unexpected call: SetPowerLevels")
}
func (m *mockCoreClient) SendReceipt(ctx context.Context, in *pb.SendReceiptRequest, opts ...grpc.CallOption) (*pb.SendReceiptResponse, error) {
	panic("unexpected call: SendReceipt")
}
func (m *mockCoreClient) GetInitialSync(ctx context.Context, in *pb.GetInitialSyncRequest, opts ...grpc.CallOption) (*pb.GetInitialSyncResponse, error) {
	panic("unexpected call: GetInitialSync")
}
func (m *mockCoreClient) GetSyncDelta(ctx context.Context, in *pb.GetSyncDeltaRequest, opts ...grpc.CallOption) (*pb.GetSyncDeltaResponse, error) {
	panic("unexpected call: GetSyncDelta")
}
func (m *mockCoreClient) GetPresence(ctx context.Context, in *pb.GetPresenceRequest, opts ...grpc.CallOption) (*pb.GetPresenceResponse, error) {
	panic("unexpected call: GetPresence")
}
func (m *mockCoreClient) UpdateProfile(ctx context.Context, in *pb.UpdateProfileRequest, opts ...grpc.CallOption) (*pb.UpdateProfileResponse, error) {
	panic("unexpected call: UpdateProfile")
}
func (m *mockCoreClient) DeleteUserKeys(_ context.Context, _ *pb.DeleteUserKeysRequest, _ ...grpc.CallOption) (*pb.DeleteUserKeysResponse, error) {
	panic("unexpected call: DeleteUserKeys")
}
func (m *mockCoreClient) KickUser(_ context.Context, _ *pb.KickUserRequest, _ ...grpc.CallOption) (*pb.KickUserResponse, error) {
	panic("unexpected call: KickUser")
}
func (m *mockCoreClient) BanUser(_ context.Context, _ *pb.BanUserRequest, _ ...grpc.CallOption) (*pb.BanUserResponse, error) {
	panic("unexpected call: BanUser")
}
func (m *mockCoreClient) UnbanUser(_ context.Context, _ *pb.UnbanUserRequest, _ ...grpc.CallOption) (*pb.UnbanUserResponse, error) {
	panic("unexpected call: UnbanUser")
}
func (m *mockCoreClient) ForgetRoom(_ context.Context, _ *pb.ForgetRoomRequest, _ ...grpc.CallOption) (*pb.ForgetRoomResponse, error) {
	panic("unexpected call: ForgetRoom")
}
func (m *mockCoreClient) GetEventContext(_ context.Context, _ *pb.GetEventContextRequest, _ ...grpc.CallOption) (*pb.GetEventContextResponse, error) {
	panic("unexpected call: GetEventContext")
}
func (m *mockCoreClient) ListPublicRooms(_ context.Context, _ *pb.ListPublicRoomsRequest, _ ...grpc.CallOption) (*pb.ListPublicRoomsResponse, error) {
	panic("unexpected call: ListPublicRooms")
}
func (m *mockCoreClient) InvalidateUserSessions(_ context.Context, _ *pb.InvalidateUserSessionsRequest, _ ...grpc.CallOption) (*pb.InvalidateUserSessionsResponse, error) {
	panic("unexpected call: InvalidateUserSessions")
}
func (m *mockCoreClient) UpdateRoomSettings(_ context.Context, _ *pb.UpdateRoomSettingsRequest, _ ...grpc.CallOption) (*pb.UpdateRoomSettingsResponse, error) {
	panic("unexpected call: UpdateRoomSettings")
}
func (m *mockCoreClient) ArchiveRoom(_ context.Context, _ *pb.ArchiveRoomRequest, _ ...grpc.CallOption) (*pb.ArchiveRoomResponse, error) {
	panic("unexpected call: ArchiveRoom")
}
func (m *mockCoreClient) UnarchiveRoom(_ context.Context, _ *pb.UnarchiveRoomRequest, _ ...grpc.CallOption) (*pb.UnarchiveRoomResponse, error) {
	panic("unexpected call: UnarchiveRoom")
}
func (m *mockCoreClient) InvalidateAllAdminSessions(_ context.Context, _ *pb.InvalidateAllAdminSessionsRequest, _ ...grpc.CallOption) (*pb.InvalidateAllAdminSessionsResponse, error) {
	panic("unexpected call: InvalidateAllAdminSessions")
}

// ─── AC11 Test 5: LogEvent_Success ───────────────────────────────────────────
//
// Given: Mock CoreServiceClient.WriteAuditLog returns {Ok: true}, nil
// When: audit.LogEvent called with valid args
// Then: returns nil (caller path unblocked)

func TestLogEvent_Success(t *testing.T) {
	client := &mockCoreClient{
		writeAuditLogFn: func(_ context.Context, _ *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
			return &pb.WriteAuditLogResponse{Ok: true}, nil
		},
	}

	err := audit.LogEvent(
		context.Background(),
		client,
		"user-1",
		"admin_login",
		"user",
		"user-1",
		nil,
		"success",
		"",
	)

	if err != nil {
		t.Fatalf("LogEvent returned non-nil error on success path: %v", err)
	}
}

// ─── AC11 Test 6: LogEvent_GRPCFailure_ReturnsNil ────────────────────────────
//
// Given: Mock WriteAuditLog returns nil, status.Error(codes.Internal, "db error")
// When: audit.LogEvent called
// Then: returns nil — never-raise semantics; gRPC error is swallowed/logged only
//
// Rationale: Audit failures must NEVER block the primary operation. The caller
// (e.g. CallbackHandler) should continue normally. The function returns nil
// (not the gRPC error) to enforce this contract at the type level.

func TestLogEvent_GRPCFailure_ReturnsNil(t *testing.T) {
	client := &mockCoreClient{
		writeAuditLogFn: func(_ context.Context, _ *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
			return nil, status.Error(codes.Internal, "db error")
		},
	}

	err := audit.LogEvent(
		context.Background(),
		client,
		"user-1",
		"admin_login",
		"user",
		"user-1",
		nil,
		"success",
		"",
	)

	if err != nil {
		t.Fatalf("LogEvent must return nil even when gRPC fails (never-raise semantics), got: %v", err)
	}
}

// ─── AC11 Test 7: LogEvent_MetadataSerialized ─────────────────────────────────
//
// Given: Mock client that captures the WriteAuditLogRequest
// When: audit.LogEvent called with metadata = map[string]any{"k": "v", "count": 42}
// Then: WriteAuditLogRequest.MetadataJson contains valid JSON with those keys

func TestLogEvent_MetadataSerialized(t *testing.T) {
	client := &mockCoreClient{
		writeAuditLogFn: func(_ context.Context, req *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
			return &pb.WriteAuditLogResponse{Ok: true}, nil
		},
	}

	metadata := map[string]any{
		"instance_name": "myserver",
		"oidc_issuer":   "https://dex.example.com",
	}

	err := audit.LogEvent(
		context.Background(),
		client,
		"user-1",
		"bootstrap_completed",
		"server",
		"",
		metadata,
		"success",
		"",
	)

	if err != nil {
		t.Fatalf("LogEvent returned error: %v", err)
	}

	if client.capturedRequest == nil {
		t.Fatal("WriteAuditLog was not called — capturedRequest is nil")
	}

	var decoded map[string]any
	if jsonErr := json.Unmarshal(client.capturedRequest.MetadataJson, &decoded); jsonErr != nil {
		t.Fatalf("MetadataJson is not valid JSON: %v — bytes: %q",
			jsonErr, client.capturedRequest.MetadataJson)
	}

	if decoded["instance_name"] != "myserver" {
		t.Errorf("expected instance_name=myserver, got %v", decoded["instance_name"])
	}
	if decoded["oidc_issuer"] != "https://dex.example.com" {
		t.Errorf("expected oidc_issuer=https://dex.example.com, got %v", decoded["oidc_issuer"])
	}
}

// ─── AC11 Test 7b: LogEvent_NilMetadata_SendsEmptyJSON ───────────────────────
//
// Given: metadata = nil
// When: audit.LogEvent called
// Then: MetadataJson == []byte("{}") — empty JSON object, not nil/empty bytes

func TestLogEvent_NilMetadata_SendsEmptyJSON(t *testing.T) {
	client := &mockCoreClient{
		writeAuditLogFn: func(_ context.Context, req *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
			return &pb.WriteAuditLogResponse{Ok: true}, nil
		},
	}

	_ = audit.LogEvent(
		context.Background(),
		client,
		"user-1",
		"admin_logout",
		"user",
		"user-1",
		nil,
		"success",
		"",
	)

	if client.capturedRequest == nil {
		t.Fatal("WriteAuditLog was not called")
	}

	want := `{}`
	got := string(client.capturedRequest.MetadataJson)
	if got != want {
		t.Errorf("expected MetadataJson=%q for nil metadata, got %q", want, got)
	}
}

// ─── Kassandra MEDIUM-1: metadata size cap ────────────────────────────────────
//
// Given: metadata whose JSON serialization exceeds MaxMetadataJSONBytes
// When: audit.LogEvent is called
// Then: MetadataJson sent to Core is the empty object "{}" — the oversize
//       payload is dropped. The audit call still succeeds so the row lands,
//       but the caller cannot ship a multi-MB blob per audit entry.

func TestLogEvent_OversizeMetadata_DroppedToEmptyObject(t *testing.T) {
	client := &mockCoreClient{
		writeAuditLogFn: func(_ context.Context, req *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
			return &pb.WriteAuditLogResponse{Ok: true}, nil
		},
	}

	// Build a metadata map whose JSON representation exceeds the 16 KiB cap.
	big := make([]byte, audit.MaxMetadataJSONBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	metadata := map[string]any{"payload": string(big)}

	err := audit.LogEvent(
		context.Background(),
		client,
		"user-1",
		"admin_login",
		"user",
		"user-1",
		metadata,
		"success",
		"",
	)

	if err != nil {
		t.Fatalf("LogEvent must never return an error, got %v", err)
	}
	if client.capturedRequest == nil {
		t.Fatal("WriteAuditLog was not called — row must still land with empty metadata")
	}

	got := string(client.capturedRequest.MetadataJson)
	if got != `{}` {
		t.Errorf("oversize metadata must be replaced with empty object, got %d bytes starting with %q",
			len(got), truncate(got, 40))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

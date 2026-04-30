package grpc

// Story 4-8: gRPC EventBus Server-Streaming + GetRoomState Unary
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until stream.go is implemented.
//
// Tests cover:
//   - EventBusStream.Start forwards received *pb.Event to the Events() channel
//   - EventBusStream.Start reconnects with exponential backoff after a stream error
//   - Client.GetRoomState returns the response on success
//   - Client.GetRoomState surfaces NOT_FOUND status codes on error

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ─── Mock CoreServiceClient ──────────────────────────────────────────────────

// mockCoreClient is a minimal implementation of pb.CoreServiceClient that only
// overrides EventBus and GetRoomState. All other methods return nil/nil.
type mockCoreClient struct {
	// eventBusFunc is called each time EventBus() is invoked on the mock.
	// It receives the invocation count (1-based) so tests can vary behavior per attempt.
	eventBusFunc func(attempt int) (grpc.ServerStreamingClient[pb.Event], error)

	// getRoomStateFunc is called for GetRoomState calls.
	getRoomStateFunc func(req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)

	// invocations counts how many times EventBus has been called.
	invocations atomic.Int32

	// Embed UnimplementedCoreServiceClient so we don't need to stub every method.
	// Note: this is not a real gRPC type; we define it inline below.
}

func (m *mockCoreClient) EventBus(
	_ context.Context,
	_ *pb.EventBusRequest,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[pb.Event], error) {
	attempt := int(m.invocations.Add(1))
	return m.eventBusFunc(attempt)
}

func (m *mockCoreClient) GetRoomState(
	_ context.Context,
	req *pb.GetRoomStateRequest,
	_ ...grpc.CallOption,
) (*pb.GetRoomStateResponse, error) {
	return m.getRoomStateFunc(req)
}

// Stub out all required CoreServiceClient interface methods.
func (m *mockCoreClient) SendEvent(_ context.Context, _ *pb.SendEventRequest, _ ...grpc.CallOption) (*pb.SendEventResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) CreateRoom(_ context.Context, _ *pb.CreateRoomRequest, _ ...grpc.CallOption) (*pb.CreateRoomResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) JoinRoom(_ context.Context, _ *pb.JoinRoomRequest, _ ...grpc.CallOption) (*pb.JoinRoomResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetMessages(_ context.Context, _ *pb.GetMessagesRequest, _ ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) SetPresence(_ context.Context, _ *pb.SetPresenceRequest, _ ...grpc.CallOption) (*pb.SetPresenceResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) SetTyping(_ context.Context, _ *pb.SetTypingRequest, _ ...grpc.CallOption) (*pb.SetTypingResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) ValidateToken(_ context.Context, _ *pb.ValidateTokenRequest, _ ...grpc.CallOption) (*pb.ValidateTokenResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetPendingEvents(_ context.Context, _ *pb.GetPendingEventsRequest, _ ...grpc.CallOption) (*pb.GetPendingEventsResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetMetrics(_ context.Context, _ *pb.GetMetricsRequest, _ ...grpc.CallOption) (*pb.GetMetricsResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) InviteUser(_ context.Context, _ *pb.InviteUserRequest, _ ...grpc.CallOption) (*pb.InviteUserResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) SetPowerLevels(_ context.Context, _ *pb.SetPowerLevelsRequest, _ ...grpc.CallOption) (*pb.SetPowerLevelsResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetInitialSync(_ context.Context, _ *pb.GetInitialSyncRequest, _ ...grpc.CallOption) (*pb.GetInitialSyncResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetSyncDelta(_ context.Context, _ *pb.GetSyncDeltaRequest, _ ...grpc.CallOption) (*pb.GetSyncDeltaResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) SendReceipt(_ context.Context, _ *pb.SendReceiptRequest, _ ...grpc.CallOption) (*pb.SendReceiptResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetPresence(_ context.Context, _ *pb.GetPresenceRequest, _ ...grpc.CallOption) (*pb.GetPresenceResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) UpdateProfile(_ context.Context, _ *pb.UpdateProfileRequest, _ ...grpc.CallOption) (*pb.UpdateProfileResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) LeaveRoom(_ context.Context, _ *pb.LeaveRoomRequest, _ ...grpc.CallOption) (*pb.LeaveRoomResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) WriteAuditLog(_ context.Context, _ *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) DeleteUserKeys(_ context.Context, _ *pb.DeleteUserKeysRequest, _ ...grpc.CallOption) (*pb.DeleteUserKeysResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) KickUser(_ context.Context, _ *pb.KickUserRequest, _ ...grpc.CallOption) (*pb.KickUserResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) BanUser(_ context.Context, _ *pb.BanUserRequest, _ ...grpc.CallOption) (*pb.BanUserResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) UnbanUser(_ context.Context, _ *pb.UnbanUserRequest, _ ...grpc.CallOption) (*pb.UnbanUserResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) ForgetRoom(_ context.Context, _ *pb.ForgetRoomRequest, _ ...grpc.CallOption) (*pb.ForgetRoomResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) GetEventContext(_ context.Context, _ *pb.GetEventContextRequest, _ ...grpc.CallOption) (*pb.GetEventContextResponse, error) {
	return nil, nil
}
func (m *mockCoreClient) ListPublicRooms(_ context.Context, _ *pb.ListPublicRoomsRequest, _ ...grpc.CallOption) (*pb.ListPublicRoomsResponse, error) {
	return nil, nil
}

// ─── Mock server-streaming client ────────────────────────────────────────────

// mockEventStream is a fake grpc.ServerStreamingClient[pb.Event].
// It delivers events from a pre-populated slice, then returns io.EOF.
type mockEventStream struct {
	events []*pb.Event
	idx    int
}

func (m *mockEventStream) Recv() (*pb.Event, error) {
	if m.idx >= len(m.events) {
		return nil, io.EOF
	}
	evt := m.events[m.idx]
	m.idx++
	return evt, nil
}

// Satisfy the full grpc.ServerStreamingClient interface with no-op stubs.
func (m *mockEventStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockEventStream) Trailer() metadata.MD         { return nil }
func (m *mockEventStream) CloseSend() error           { return nil }
func (m *mockEventStream) Context() context.Context   { return context.Background() }
func (m *mockEventStream) SendMsg(_ any) error        { return nil }
func (m *mockEventStream) RecvMsg(_ any) error        { return io.EOF }

// errEventStream always returns an error from Recv (simulates connection drop).
type errEventStream struct{ err error }

func (e *errEventStream) Recv() (*pb.Event, error)       { return nil, e.err }
func (e *errEventStream) Header() (metadata.MD, error) { return nil, nil }
func (e *errEventStream) Trailer() metadata.MD         { return nil }
func (e *errEventStream) CloseSend() error               { return nil }
func (e *errEventStream) Context() context.Context       { return context.Background() }
func (e *errEventStream) SendMsg(_ any) error            { return nil }
func (e *errEventStream) RecvMsg(_ any) error            { return e.err }

// ─── AC #4 (Go): EventBusStream forwards event to Events() channel ───────────

// TestEventBusStream_ForwardsEventToChannel verifies that when the mock stream
// returns a single *pb.Event followed by EOF, the EventBusStream places that
// event on the channel returned by Events().
//
// Fails until EventBusStream is implemented in stream.go.
func TestEventBusStream_ForwardsEventToChannel(t *testing.T) {
	wantEvent := &pb.Event{
		EventId:   "$ev1",
		RoomId:    "!room:nebu.local",
		SenderId:  "@kai:nebu.local",
		EventType: "m.room.message",
		OriginTs:  1_700_000_000_000,
	}

	mock := &mockCoreClient{
		eventBusFunc: func(_ int) (grpc.ServerStreamingClient[pb.Event], error) {
			return &mockEventStream{events: []*pb.Event{wantEvent}}, nil
		},
	}

	// EventBusStream is the type implemented in stream.go (Story 4-8).
	// It is constructed with the CoreServiceClient and a nodeID string.
	stream := NewEventBusStream(mock, "test-node-1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream.Start(ctx)

	select {
	case got := <-stream.Events():
		if got.EventId != wantEvent.EventId {
			t.Errorf("event_id: got %q, want %q", got.EventId, wantEvent.EventId)
		}
		if got.RoomId != wantEvent.RoomId {
			t.Errorf("room_id: got %q, want %q", got.RoomId, wantEvent.RoomId)
		}
		if got.SenderId != wantEvent.SenderId {
			t.Errorf("sender_id: got %q, want %q", got.SenderId, wantEvent.SenderId)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event on Events() channel")
	}
}

// ─── AC #4 (Go): EventBusStream reconnects after stream error ────────────────

// TestEventBusStream_ReconnectsAfterStreamError verifies that when the first
// EventBus() call returns a stream that immediately errors on Recv, the
// EventBusStream retries (makes a second EventBus() call).
//
// The test uses a short override for the minimum backoff (1ms) via the
// EventBusStream options, so the test completes quickly without real sleep.
//
// Fails until EventBusStream reconnect logic is implemented in stream.go.
func TestEventBusStream_ReconnectsAfterStreamError(t *testing.T) {
	reconnected := make(chan struct{}, 1)

	var callCount atomic.Int32

	mock := &mockCoreClient{
		eventBusFunc: func(_ int) (grpc.ServerStreamingClient[pb.Event], error) {
			n := int(callCount.Add(1))
			if n == 1 {
				// First attempt: return a stream that errors immediately on Recv
				return &errEventStream{err: errors.New("simulated stream disconnect")}, nil
			}
			// Second attempt: signal reconnect and return an empty stream (EOF)
			select {
			case reconnected <- struct{}{}:
			default:
			}
			return &mockEventStream{events: nil}, nil
		},
	}

	// EventBusStreamOption configures the initial backoff for testing.
	// WithMinBackoff is an option on EventBusStream that overrides the 1s default.
	stream := NewEventBusStream(mock, "test-node-reconnect",
		WithMinBackoff(1*time.Millisecond),
		WithMaxBackoff(5*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream.Start(ctx)

	select {
	case <-reconnected:
		// Reconnect attempt made — test passes
	case <-ctx.Done():
		t.Fatal("EventBusStream did not attempt reconnect after stream error within timeout")
	}

	// Verify at least 2 EventBus calls were made
	if n := callCount.Load(); n < 2 {
		t.Errorf("expected at least 2 EventBus calls (1 failure + 1 reconnect), got %d", n)
	}
}

// ─── AC #5 (Go): Client.GetRoomState — success ───────────────────────────────

// TestClient_GetRoomState_Success verifies that GetRoomState passes the request
// through to the underlying CoreServiceClient and returns the response.
//
// Fails until Client.GetRoomState is wired to c.core.GetRoomState in client.go.
func TestClient_GetRoomState_Success(t *testing.T) {
	wantResponse := &pb.GetRoomStateResponse{
		Members:         []string{"@kai:nebu.local"},
		PowerLevelsJson: "{}",
		RoomName:        "",
	}

	mock := &mockCoreClient{
		getRoomStateFunc: func(req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
			if req.RoomId != "!room1:nebu.local" {
				return nil, status.Errorf(codes.NotFound, "room not found")
			}
			return wantResponse, nil
		},
	}

	// NewClientWithCore constructs a Client using a pre-built CoreServiceClient.
	// This constructor is expected to be added in stream.go or client.go for Story 4-8.
	c := NewClientWithCore(mock)

	ctx := context.Background()
	got, err := c.GetRoomState(ctx, &pb.GetRoomStateRequest{RoomId: "!room1:nebu.local"})
	if err != nil {
		t.Fatalf("GetRoomState() unexpected error: %v", err)
	}

	if len(got.Members) != 1 || got.Members[0] != "@kai:nebu.local" {
		t.Errorf("Members: got %v, want [@kai:nebu.local]", got.Members)
	}
	if got.PowerLevelsJson != "{}" {
		t.Errorf("PowerLevelsJson: got %q, want {}", got.PowerLevelsJson)
	}
}

// ─── AC #5 (Go): Client.GetRoomState — NOT_FOUND ─────────────────────────────

// TestClient_GetRoomState_NotFound verifies that when the Elixir core returns
// a NOT_FOUND gRPC status, Client.GetRoomState surfaces that error.
//
// Fails until Client.GetRoomState is wired to c.core.GetRoomState in client.go.
func TestClient_GetRoomState_NotFound(t *testing.T) {
	mock := &mockCoreClient{
		getRoomStateFunc: func(_ *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
			return nil, status.Errorf(codes.NotFound, "room not found")
		},
	}

	c := NewClientWithCore(mock)

	ctx := context.Background()
	_, err := c.GetRoomState(ctx, &pb.GetRoomStateRequest{RoomId: "!ghost:nebu.local"})
	if err == nil {
		t.Fatal("GetRoomState() expected NOT_FOUND error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("status code: got %v, want NOT_FOUND", st.Code())
	}
}

package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc/connectivity"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New("localhost:19999")
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	return c
}

func TestState_returnsValidConnectivityState(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	s := c.State()
	// For an unconnected client the state is Idle (initial state after NewClient).
	// Any valid connectivity.State value is acceptable here.
	validStates := map[connectivity.State]bool{
		connectivity.Idle:             true,
		connectivity.Connecting:       true,
		connectivity.Ready:            true,
		connectivity.TransientFailure: true,
		connectivity.Shutdown:         true,
	}
	if !validStates[s] {
		t.Errorf("State() returned unexpected value %v", s)
	}
}

func TestNew_returnsWithoutBlocking(t *testing.T) {
	start := time.Now()
	c := newTestClient(t)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("New() took %v, expected lazy non-blocking dial (< 1s)", elapsed)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

func TestStubsReturnNil(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "SendEvent",
			call: func() error {
				// SendEvent is wired to the real gRPC client (Story 4-11),
				// so it returns a connection error when no server is running.
				_, err := c.SendEvent(ctx, &pb.SendEventRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "CreateRoom",
			call: func() error {
				// CreateRoom is wired to the real gRPC client (Story 4-9),
				// so it returns a connection error when no server is running.
				_, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "JoinRoom",
			call: func() error {
				// JoinRoom is wired to the real gRPC client (Story 4-10),
				// so it returns a connection error when no server is running.
				_, err := c.JoinRoom(ctx, &pb.JoinRoomRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "InviteUser",
			call: func() error {
				// InviteUser is wired to the real gRPC client (Story 4-10),
				// so it returns a connection error when no server is running.
				_, err := c.InviteUser(ctx, &pb.InviteUserRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "GetMessages",
			call: func() error {
				// GetMessages is wired to the real gRPC client (Story 4-12),
				// so it returns a connection error when no server is running.
				_, err := c.GetMessages(ctx, &pb.GetMessagesRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "SetPresence",
			call: func() error {
				resp, err := c.SetPresence(ctx, &pb.SetPresenceRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "SetTyping",
			call: func() error {
				resp, err := c.SetTyping(ctx, &pb.SetTypingRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "ValidateToken",
			call: func() error {
				// ValidateToken is wired to the real gRPC client (Story 2.14),
				// so it returns a connection error when no server is running.
				_, err := c.ValidateToken(ctx, &pb.ValidateTokenRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
		{
			name: "GetPendingEvents",
			call: func() error {
				resp, err := c.GetPendingEvents(ctx, &pb.GetPendingEventsRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "EventBus",
			call: func() error {
				// EventBus is now wired to the real gRPC client (Story 4-8),
				// so it returns a connection error when no server is running.
				_, err := c.EventBus(ctx, &pb.EventBusRequest{})
				if err == nil {
					return fmt.Errorf("want connection error; got nil")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != nil {
				t.Error(err)
			}
		})
	}
}

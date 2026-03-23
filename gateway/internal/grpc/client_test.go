package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New("localhost:19999")
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	return c
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
				resp, err := c.SendEvent(ctx, &pb.SendEventRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "CreateRoom",
			call: func() error {
				resp, err := c.CreateRoom(ctx, &pb.CreateRoomRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "JoinRoom",
			call: func() error {
				resp, err := c.JoinRoom(ctx, &pb.JoinRoomRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
				}
				return nil
			},
		},
		{
			name: "GetMessages",
			call: func() error {
				resp, err := c.GetMessages(ctx, &pb.GetMessagesRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
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
				resp, err := c.ValidateToken(ctx, &pb.ValidateTokenRequest{})
				if err != nil || resp != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", resp, err)
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
				stream, err := c.EventBus(ctx, &pb.EventBusRequest{})
				if err != nil || stream != nil {
					return fmt.Errorf("want nil,nil; got %v,%v", stream, err)
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

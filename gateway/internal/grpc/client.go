package grpc

import (
	"context"
	"log/slog"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// Client wraps the generated CoreServiceClient with its underlying connection.
type Client struct {
	conn *grpclib.ClientConn
	core pb.CoreServiceClient
}

// New creates a Client connected to addr using lazy (non-blocking) dial.
// A background goroutine probes the connection for up to 5 seconds and logs
// a warning if the core is unreachable — it does not block or exit.
func New(addr string) (*Client, error) {
	conn, err := grpclib.NewClient(
		addr,
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.Connect()
		for {
			s := conn.GetState()
			if s == connectivity.Ready {
				return
			}
			if !conn.WaitForStateChange(ctx, s) {
				slog.Warn("gRPC core not reachable at startup, continuing", "addr", addr)
				return
			}
		}
	}()

	return &Client{
		conn: conn,
		core: pb.NewCoreServiceClient(conn),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// State returns the current connectivity state of the gRPC connection.
// Used by the /ready health endpoint.
func (c *Client) State() connectivity.State {
	return c.conn.GetState()
}

// SendEvent stub — implemented in Epic 4.
func (c *Client) SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
	return nil, nil
}

// CreateRoom stub — implemented in Epic 4.
func (c *Client) CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
	return nil, nil
}

// JoinRoom stub — implemented in Epic 4.
func (c *Client) JoinRoom(ctx context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error) {
	return nil, nil
}

// GetMessages stub — implemented in Epic 4.
func (c *Client) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
	return nil, nil
}

// SetPresence stub — implemented in Epic 4.
func (c *Client) SetPresence(ctx context.Context, req *pb.SetPresenceRequest) (*pb.SetPresenceResponse, error) {
	return nil, nil
}

// SetTyping stub — implemented in Epic 4.
func (c *Client) SetTyping(ctx context.Context, req *pb.SetTypingRequest) (*pb.SetTypingResponse, error) {
	return nil, nil
}

// ValidateToken calls the Elixir core to validate/provision a user.
func (c *Client) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	return c.core.ValidateToken(ctx, req)
}

// GetPendingEvents stub — implemented in Epic 4.
func (c *Client) GetPendingEvents(ctx context.Context, req *pb.GetPendingEventsRequest) (*pb.GetPendingEventsResponse, error) {
	return nil, nil
}

// EventBus stub — implemented in Epic 4.
func (c *Client) EventBus(ctx context.Context, req *pb.EventBusRequest) (grpclib.ServerStreamingClient[pb.Event], error) {
	return nil, nil
}

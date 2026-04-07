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

// SendEvent calls the Elixir core to process and persist a room event.
func (c *Client) SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
	return c.core.SendEvent(ctx, req)
}

// CreateRoom calls the Elixir core to start a new Room GenServer.
func (c *Client) CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
	return c.core.CreateRoom(ctx, req)
}

// JoinRoom calls the Elixir core to join a room or accept an invitation.
func (c *Client) JoinRoom(ctx context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error) {
	return c.core.JoinRoom(ctx, req)
}

// InviteUser calls the Elixir core to invite a user to a room.
func (c *Client) InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
	return c.core.InviteUser(ctx, req)
}

// GetMessages fetches paginated room history from Elixir Core.
func (c *Client) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
	return c.core.GetMessages(ctx, req)
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

// EventBus opens a server-streaming EventBus connection to the Elixir core.
// The returned stream delivers real-time room events until the context is cancelled.
func (c *Client) EventBus(ctx context.Context, req *pb.EventBusRequest) (grpclib.ServerStreamingClient[pb.Event], error) {
	return c.core.EventBus(ctx, req)
}

// GetMetrics stub — implemented in Epic 4.
func (c *Client) GetMetrics(ctx context.Context, req *pb.GetMetricsRequest) (*pb.GetMetricsResponse, error) {
	return nil, nil
}

// GetRoomState queries the Elixir core for current room state (members, metadata).
// Returns NOT_FOUND status if the room GenServer is not running.
func (c *Client) GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
	return c.core.GetRoomState(ctx, req)
}

// SetPowerLevels updates the power levels for a room in the Elixir core.
// Returns PERMISSION_DENIED if the caller lacks change_state power, NOT_FOUND if the room is missing.
func (c *Client) SetPowerLevels(ctx context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error) {
	return c.core.SetPowerLevels(ctx, req)
}

// GetInitialSync calls the Elixir core to build the initial sync response for a user.
func (c *Client) GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error) {
	return c.core.GetInitialSync(ctx, req)
}

// NewClientWithCore constructs a Client using a pre-built CoreServiceClient.
// This is used in tests to inject a mock without a real gRPC connection.
func NewClientWithCore(core pb.CoreServiceClient) *Client {
	return &Client{
		conn: nil,
		core: core,
	}
}

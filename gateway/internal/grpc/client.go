package grpc

import (
	"context"
	"log/slog"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// nodeTokenKey is the gRPC metadata key for the PSK node-registration token.
// Must match the key read by the Elixir gRPC auth interceptor.
const nodeTokenKey = "x-nebu-node-token"

// Client wraps the generated CoreServiceClient with its underlying connection.
type Client struct {
	conn *grpclib.ClientConn
	core pb.CoreServiceClient
}

// newAuthUnaryInterceptor returns a UnaryClientInterceptor that injects the
// PSK token into every outgoing unary gRPC call's metadata.
// Story 5.29a — AC10 (FB-52-01).
func newAuthUnaryInterceptor(secret string) grpclib.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpclib.ClientConn,
		invoker grpclib.UnaryInvoker,
		opts ...grpclib.CallOption,
	) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		md.Set(nodeTokenKey, secret)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// newAuthStreamInterceptor returns a StreamClientInterceptor that injects the
// PSK token into every outgoing streaming gRPC call's metadata.
// Story 5.29a — AC10 (FB-52-01).
func newAuthStreamInterceptor(secret string) grpclib.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpclib.StreamDesc,
		cc *grpclib.ClientConn,
		method string,
		streamer grpclib.Streamer,
		opts ...grpclib.CallOption,
	) (grpclib.ClientStream, error) {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		md.Set(nodeTokenKey, secret)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// New creates a Client connected to addr using lazy (non-blocking) dial.
// secret is the PSK loaded from NEBU_INTERNAL_SECRET_FILE; it is injected into
// every outgoing gRPC call via interceptors (Story 5.29a AC10).
// A background goroutine probes the connection for up to 5 seconds and logs
// a warning if the core is unreachable — it does not block or exit.
func New(addr string, secret ...string) (*Client, error) {
	// Build dial options; always include insecure transport (mTLS is Phase 2 / ADR-008).
	dialOpts := []grpclib.DialOption{
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
	}

	// Attach PSK interceptors when a secret is provided.
	if len(secret) > 0 && secret[0] != "" {
		psk := secret[0]
		dialOpts = append(dialOpts,
			grpclib.WithUnaryInterceptor(newAuthUnaryInterceptor(psk)),
			grpclib.WithStreamInterceptor(newAuthStreamInterceptor(psk)),
		)
	}

	conn, err := grpclib.NewClient(addr, dialOpts...)
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

// LeaveRoom calls the Elixir core to leave a room.
func (c *Client) LeaveRoom(ctx context.Context, req *pb.LeaveRoomRequest) (*pb.LeaveRoomResponse, error) {
	return c.core.LeaveRoom(ctx, req)
}

// InviteUser calls the Elixir core to invite a user to a room.
func (c *Client) InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
	return c.core.InviteUser(ctx, req)
}

// GetMessages fetches paginated room history from Elixir Core.
func (c *Client) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
	return c.core.GetMessages(ctx, req)
}

// SetPresence calls the Elixir core to set the presence status for a user.
func (c *Client) SetPresence(ctx context.Context, req *pb.SetPresenceRequest) (*pb.SetPresenceResponse, error) {
	return c.core.SetPresence(ctx, req)
}

// SetTyping calls the Elixir core to set/clear the typing indicator for a user in a room.
func (c *Client) SetTyping(ctx context.Context, req *pb.SetTypingRequest) (*pb.SetTypingResponse, error) {
	return c.core.SetTyping(ctx, req)
}

// SendReceipt calls the Elixir core to persist a read receipt.
func (c *Client) SendReceipt(ctx context.Context, req *pb.SendReceiptRequest) (*pb.SendReceiptResponse, error) {
	return c.core.SendReceipt(ctx, req)
}

// GetPresence calls the Elixir core to retrieve presence status for a user.
// Core always returns a response (unknown users default to "offline") — never returns not_found.
func (c *Client) GetPresence(ctx context.Context, req *pb.GetPresenceRequest) (*pb.GetPresenceResponse, error) {
	return c.core.GetPresence(ctx, req)
}

// UpdateProfile calls the Elixir core to upsert a user's displayname and/or avatar_url.
func (c *Client) UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error) {
	return c.core.UpdateProfile(ctx, req)
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

// GetSyncDelta calls the Elixir core for incremental sync with long-polling (Story 4-15).
func (c *Client) GetSyncDelta(ctx context.Context, req *pb.GetSyncDeltaRequest) (*pb.GetSyncDeltaResponse, error) {
	return c.core.GetSyncDelta(ctx, req)
}

// KickUser calls the Elixir core to kick a user from a room.
// Power-level check is enforced by the Elixir GenServer.
func (c *Client) KickUser(ctx context.Context, req *pb.KickUserRequest) (*pb.KickUserResponse, error) {
	return c.core.KickUser(ctx, req)
}

// BanUser calls the Elixir core to ban a user from a room.
// Power-level check is enforced by the Elixir GenServer.
func (c *Client) BanUser(ctx context.Context, req *pb.BanUserRequest) (*pb.BanUserResponse, error) {
	return c.core.BanUser(ctx, req)
}

// UnbanUser calls the Elixir core to unban a user from a room (sets membership: leave).
// Power-level check is enforced by the Elixir GenServer.
func (c *Client) UnbanUser(ctx context.Context, req *pb.UnbanUserRequest) (*pb.UnbanUserResponse, error) {
	return c.core.UnbanUser(ctx, req)
}

// ForgetRoom calls the Elixir core to mark a room as excluded from future /sync for the user.
// Returns FailedPrecondition if the user is still joined.
func (c *Client) ForgetRoom(ctx context.Context, req *pb.ForgetRoomRequest) (*pb.ForgetRoomResponse, error) {
	return c.core.ForgetRoom(ctx, req)
}

// ListPublicRooms calls the Elixir core to retrieve a paginated list of public rooms.
// Story 7-27: backing RPC for GET/POST /_matrix/client/v3/publicRooms.
// The Elixir handler queries the DB for rooms where join_rule = 'public', resolves live member
// counts from Room GenServers (DB fallback for rooms whose GenServer is not running), and
// returns the page with a cursor for stable lexicographic pagination.
func (c *Client) ListPublicRooms(ctx context.Context, req *pb.ListPublicRoomsRequest) (*pb.ListPublicRoomsResponse, error) {
	return c.core.ListPublicRooms(ctx, req)
}

// GetEventContext calls the Elixir core to retrieve the context window around a specific event.
// Returns NOT_FOUND if the event does not exist in the room, PERMISSION_DENIED if the user
// is not a room member. Story 7-28: GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}.
func (c *Client) GetEventContext(ctx context.Context, req *pb.GetEventContextRequest) (*pb.GetEventContextResponse, error) {
	return c.core.GetEventContext(ctx, req)
}

// ListAdminUsers calls the Elixir core to list admin users with pagination.
// Story 9.1: Admin gRPC RPCs — User + Room Management.
func (c *Client) ListAdminUsers(ctx context.Context, req *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error) {
	return c.core.ListAdminUsers(ctx, req)
}

// GetAdminUser calls the Elixir core to fetch a single admin user by user_id.
// Returns NOT_FOUND if the user does not exist.
func (c *Client) GetAdminUser(ctx context.Context, req *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error) {
	return c.core.GetAdminUser(ctx, req)
}

// DeactivateUser calls the Elixir core to deactivate a user account.
// Sets is_active=false and invalidates all active sessions.
func (c *Client) DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error) {
	return c.core.DeactivateUser(ctx, req)
}

// ReactivateUser calls the Elixir core to reactivate a previously deactivated user account.
// Sets is_active=true.
func (c *Client) ReactivateUser(ctx context.Context, req *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error) {
	return c.core.ReactivateUser(ctx, req)
}

// UpdateUserRole calls the Elixir core to update the system_role for a user.
// Valid roles: "user", "instance_admin", "compliance_officer".
func (c *Client) UpdateUserRole(ctx context.Context, req *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error) {
	return c.core.UpdateUserRole(ctx, req)
}

// ListAdminRooms calls the Elixir core to list admin rooms with pagination and optional status filter.
func (c *Client) ListAdminRooms(ctx context.Context, req *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error) {
	return c.core.ListAdminRooms(ctx, req)
}

// GetAdminRoom calls the Elixir core to fetch detailed info for a single room.
// Returns NOT_FOUND if the room does not exist.
func (c *Client) GetAdminRoom(ctx context.Context, req *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error) {
	return c.core.GetAdminRoom(ctx, req)
}

// ArchiveRoom calls the Elixir core to archive a room (sets status=archived, terminates GenServer).
// Story 9.3: Admin UI Rooms API Integration.
func (c *Client) ArchiveRoom(ctx context.Context, req *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error) {
	return c.core.ArchiveRoom(ctx, req)
}

// UnarchiveRoom calls the Elixir core to unarchive a room (sets status=active, starts GenServer).
// Story 9.3: Admin UI Rooms API Integration.
func (c *Client) UnarchiveRoom(ctx context.Context, req *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error) {
	return c.core.UnarchiveRoom(ctx, req)
}

// UpdateRoomSettings calls the Elixir core to update room settings (max_members).
// Story 9.3: Admin UI Rooms API Integration.
// Note: UpdateRoomSettingsRequest only carries max_members; visibility changes are not in the proto.
func (c *Client) UpdateRoomSettings(ctx context.Context, req *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error) {
	return c.core.UpdateRoomSettings(ctx, req)
}

// GetServerConfig calls the Elixir core to retrieve the current server configuration.
// Note: oidc_client_secret is intentionally excluded from the response.
func (c *Client) GetServerConfig(ctx context.Context, req *pb.GetServerConfigRequest) (*pb.GetServerConfigResponse, error) {
	return c.core.GetServerConfig(ctx, req)
}

// UpdateServerConfig calls the Elixir core to upsert server configuration fields.
// Empty string / zero value fields are not updated.
func (c *Client) UpdateServerConfig(ctx context.Context, req *pb.UpdateServerConfigRequest) (*pb.UpdateServerConfigResponse, error) {
	return c.core.UpdateServerConfig(ctx, req)
}

// CoreServiceClient returns the underlying generated gRPC client stub.
// Used by EventBusStream (Story 4-16) which requires the raw pb.CoreServiceClient.
func (c *Client) CoreServiceClient() pb.CoreServiceClient {
	return c.core
}

// NewClientWithCore constructs a Client using a pre-built CoreServiceClient.
// This is used in tests to inject a mock without a real gRPC connection.
func NewClientWithCore(core pb.CoreServiceClient) *Client {
	return &Client{
		conn: nil,
		core: core,
	}
}

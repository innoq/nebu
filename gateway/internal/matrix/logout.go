package matrix

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
)

// LogoutCoreClient is the subset of pb.CoreServiceClient used by LogoutHandler.
// Defined here by the consumer (Go interface convention, ADR-009).
type LogoutCoreClient interface {
	InvalidateUserSessions(ctx context.Context, req *pb.InvalidateUserSessionsRequest, opts ...grpc.CallOption) (*pb.InvalidateUserSessionsResponse, error)
}

type LogoutHandler struct {
	store      middleware.TokenStore
	coreClient LogoutCoreClient // optional; nil = skip per-device sync-token cleanup
}

// LogoutConfig holds configuration for LogoutHandler.
type LogoutConfig struct {
	Store      middleware.TokenStore
	CoreClient LogoutCoreClient // Optional: when set, per-device sync_tokens cleanup runs on logout (AC4, Story 9-22)
}

// NewLogoutHandler creates a LogoutHandler without a Core gRPC client.
// It invalidates the JWT in the local denylist but does NOT clean up
// per-device sync_tokens in the Elixir Core.
// Use NewLogoutHandlerWithCore in production to ensure per-device sync-token
// cleanup runs on POST /logout (AC4, Story 9-22).
func NewLogoutHandler(store middleware.TokenStore) *LogoutHandler {
	return &LogoutHandler{store: store}
}

// NewLogoutHandlerWithCore creates a LogoutHandler with an optional Core gRPC client
// for per-device sync-token cleanup on POST /logout (AC4, Story 9-22).
func NewLogoutHandlerWithCore(cfg LogoutConfig) *LogoutHandler {
	return &LogoutHandler{store: cfg.Store, coreClient: cfg.CoreClient}
}

func (h *LogoutHandler) PostLogout(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	expiry, _ := r.Context().Value(middleware.ContextKeyTokenExpiry).(time.Time)
	_ = h.store.Invalidate(rawToken, expiry)

	// AC4 (Story 9-22): clean up the per-device sync_tokens row in Core.
	// device_id is set by JWTMiddleware from the "did" JWT claim.
	// If coreClient is nil (legacy / test), skip this call (best-effort).
	if h.coreClient != nil {
		userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
		deviceID, _ := r.Context().Value(middleware.ContextKeyDeviceID).(string)
		if userID != "" {
			_, err := h.coreClient.InvalidateUserSessions(r.Context(), &pb.InvalidateUserSessionsRequest{
				UserId:   userID,
				DeviceId: deviceID,
			})
			if err != nil {
				// Non-fatal: the JWT is already invalidated in the denylist.
				// Log and continue so the client still gets a 200 response.
				slog.Warn("PostLogout: failed to clean up per-device sync token", "userID", userID, "deviceID", deviceID, "err", err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct{}{})
}

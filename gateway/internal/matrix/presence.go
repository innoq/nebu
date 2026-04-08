package matrix

import (
	"context"
	"encoding/json"
	"net/http"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// PresenceCoreClient is the consumer-defined interface for GetPresence gRPC calls.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type PresenceCoreClient interface {
	GetPresence(ctx context.Context, req *pb.GetPresenceRequest) (*pb.GetPresenceResponse, error)
}

// PresenceHandler handles GET /_matrix/client/v3/presence/{userId}/status.
type PresenceHandler struct {
	coreClient PresenceCoreClient
	serverName string
}

// PresenceConfig holds dependencies for NewPresenceHandler.
type PresenceConfig struct {
	CoreClient PresenceCoreClient
	ServerName string
}

// NewPresenceHandler constructs a PresenceHandler from the provided config.
func NewPresenceHandler(cfg PresenceConfig) *PresenceHandler {
	return &PresenceHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// presenceStatusResponse is the JSON response body for GET /presence/{userId}/status.
type presenceStatusResponse struct {
	Presence      string `json:"presence"`
	LastActiveAgo int64  `json:"last_active_ago"`
}

// GetPresenceStatus handles GET /_matrix/client/v3/presence/{userId}/status.
// Requires JWT auth (per Matrix spec — reading presence requires authentication).
//
// Flow:
//  1. Extract userId from r.PathValue("userId").
//  2. Call CoreService.GetPresence.
//  3. Map errors: any gRPC error → 503 M_UNAVAILABLE (Core never sends not_found for presence).
//  4. Return 200 {"presence": resp.Presence, "last_active_ago": resp.LastActiveAgo}.
//
// NOTE: Core's get_presence/2 NEVER returns not_found. Unknown users default to "offline".
// There is NO 404 mapping here — only 503 on Core unavailability.
func (h *PresenceHandler) GetPresenceStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	resp, err := h.coreClient.GetPresence(r.Context(), &pb.GetPresenceRequest{
		UserId: userID,
	})
	if err != nil {
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Presence service unavailable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(presenceStatusResponse{
		Presence:      resp.Presence,
		LastActiveAgo: resp.LastActiveAgo,
	})
}

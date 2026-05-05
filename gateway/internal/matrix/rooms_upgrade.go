package matrix

import (
	"context"
	"encoding/json"
	"net/http"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UpgradeRoomCoreClient is the consumer-defined interface for the upgrade RPC.
// Keeps dependencies minimal — only what PostUpgradeRoom needs.
// Story 9.8: replaces the 501 stub with full room-version-upgrade support.
type UpgradeRoomCoreClient interface {
	UpgradeRoom(ctx context.Context, req *pb.UpgradeRoomRequest) (*pb.UpgradeRoomResponse, error)
}

// UpgradeRoomConfig holds dependencies for NewUpgradeRoomHandler.
type UpgradeRoomConfig struct {
	CoreClient UpgradeRoomCoreClient
	ServerName string
}

// UpgradeRoomHandler handles POST /_matrix/client/v3/rooms/{roomId}/upgrade.
//
// Story 9.8 scope: full implementation — calls Core.UpgradeRoom which atomically:
//   - emits m.room.tombstone in the old room
//   - creates a new room with predecessor info
//   - copies required state events from old room to new room
//   - invites all former members to the new room
type UpgradeRoomHandler struct {
	coreClient UpgradeRoomCoreClient
	serverName string
}

// NewUpgradeRoomHandler constructs an UpgradeRoomHandler from the provided config.
func NewUpgradeRoomHandler(cfg UpgradeRoomConfig) *UpgradeRoomHandler {
	return &UpgradeRoomHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// upgradeRoomBody is the JSON body for POST /_matrix/client/v3/rooms/{roomId}/upgrade.
type upgradeRoomBody struct {
	NewVersion string `json:"new_version"`
}

// upgradeRoomResponse is the success JSON body for POST /_matrix/client/v3/rooms/{roomId}/upgrade.
type upgradeRoomResponse struct {
	ReplacementRoom string `json:"replacement_room"`
}

// PostUpgradeRoom handles POST /_matrix/client/v3/rooms/{roomId}/upgrade.
//
// Flow (Story 9.8 full implementation):
//  1. requireJSON (415 on wrong Content-Type).
//  2. ValidateMatrixRoomID(roomId) → 400 M_BAD_JSON if invalid.
//  3. Decode body with DisallowUnknownFields; validate new_version non-empty → 400 M_BAD_JSON.
//  4. Call Core.UpgradeRoom via gRPC (atomic: tombstone + new room + state copy + invites).
//  5. Map gRPC errors: PermissionDenied → 403, NotFound → 404, InvalidArgument → 400, other → 500.
//  6. Return 200 {"replacement_room": "<new_room_id>"} on success.
func (h *UpgradeRoomHandler) PostUpgradeRoom(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	roomID := r.PathValue("roomId")
	if err := ValidateMatrixRoomID(roomID); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid room ID: "+err.Error())
		return
	}

	var body upgradeRoomBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	if body.NewVersion == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "new_version is required")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.UpgradeRoom(grpcCtx, &pb.UpgradeRoomRequest{
		OldRoomId:   roomID,
		RequesterId: userID,
		NewVersion:  body.NewVersion,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to upgrade this room")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.InvalidArgument:
			writeMatrixError(w, http.StatusBadRequest, "M_UNSUPPORTED_ROOM_VERSION", st.Message())
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	var newRoomID string
	if resp != nil {
		newRoomID = resp.NewRoomId
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(upgradeRoomResponse{ReplacementRoom: newRoomID})
}

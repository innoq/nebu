package matrix

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetMembersCoreClient is a consumer-defined interface for the GetRoomState gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type GetMembersCoreClient interface {
	GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
}

// GetRoomMembersHandler handles GET /_matrix/client/v3/rooms/{roomId}/members.
type GetRoomMembersHandler struct {
	coreClient GetMembersCoreClient
	serverName string
}

// GetRoomMembersConfig holds dependencies for NewGetRoomMembersHandler.
type GetRoomMembersConfig struct {
	CoreClient GetMembersCoreClient
	ServerName string
}

// NewGetRoomMembersHandler constructs a GetRoomMembersHandler from the provided config.
func NewGetRoomMembersHandler(cfg GetRoomMembersConfig) *GetRoomMembersHandler {
	return &GetRoomMembersHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// roomMemberEvent is a Matrix m.room.member state event as returned in the chunk array.
type roomMemberEvent struct {
	Type     string         `json:"type"`
	StateKey string         `json:"state_key"`
	Content  map[string]any `json:"content"`
}

// GetRoomMembers handles GET /_matrix/client/v3/rooms/{roomId}/members.
//
// Flow:
//  1. Extract roomId from URL path via r.PathValue.
//  2. Extract authenticated user_id + systemRole from JWT context.
//  3. Call Core.GetRoomState — returns the list of joined member user IDs.
//  4. Map gRPC errors: PermissionDenied → 403 M_FORBIDDEN; NotFound → 404 M_NOT_FOUND;
//     Unavailable → 503 M_UNAVAILABLE; default → 500 M_UNKNOWN.
//  5. Shape each member ID into a Matrix m.room.member state event.
//  6. Return 200 {"chunk": [...]} — chunk is always an array, never null.
func (h *GetRoomMembersHandler) GetRoomMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		st, _ := status.FromError(err)
		slog.Error("GetRoomState gRPC failed", "code", st.Code(), "msg", st.Message(), "room", roomID)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Server unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Build chunk: one m.room.member event per member ID.
	chunk := make([]roomMemberEvent, 0, len(resp.Members))
	for _, memberID := range resp.Members {
		chunk = append(chunk, roomMemberEvent{
			Type:     "m.room.member",
			StateKey: memberID,
			Content: map[string]any{
				"membership": "join",
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"chunk": chunk})
}

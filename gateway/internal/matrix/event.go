package matrix

// ─── Story 11-8: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} ────────
//
// Returns a single event by ID, scoped to a room.
// The calling user must be a joined member of the room.
// Matrix CS API spec: GET /rooms/{roomId}/event/{eventId}

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

// GetEventCoreClient is the consumer-defined gRPC interface for GetEvent.
type GetEventCoreClient interface {
	GetEvent(ctx context.Context, req *pb.GetEventRequest) (*pb.GetEventResponse, error)
}

// GetEventConfig holds dependencies for NewGetEventHandler.
type GetEventConfig struct {
	CoreClient GetEventCoreClient
}

// GetEventHandler handles GET /_matrix/client/v3/rooms/{roomId}/event/{eventId}.
type GetEventHandler struct {
	coreClient GetEventCoreClient
}

// NewGetEventHandler constructs a GetEventHandler.
func NewGetEventHandler(cfg GetEventConfig) *GetEventHandler {
	return &GetEventHandler{coreClient: cfg.CoreClient}
}

// GetEvent handles GET /_matrix/client/v3/rooms/{roomId}/event/{eventId}.
//
// Flow:
//  1. Extract roomId and eventId from URL path.
//  2. Validate roomId and eventId format.
//  3. Extract userID from JWT context (set by JWTMiddleware).
//  4. Call Core.GetEvent — map gRPC errors to Matrix error codes.
//  5. Return the event as a Matrix event JSON object.
func (h *GetEventHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	eventID := r.PathValue("eventId")

	if err := ValidateMatrixRoomID(roomID); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "Invalid room ID format")
		return
	}
	if err := ValidateMatrixEventID(eventID); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "Invalid event ID format")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Authentication required")
		return
	}
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetEvent(grpcCtx, &pb.GetEventRequest{
		RoomId:  roomID,
		EventId: eventID,
		UserId:  userID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Event not found")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		default:
			slog.Error("GetEvent gRPC failed", "code", st.Code(), "msg", st.Message(), "room_id", roomID, "event_id", eventID)
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	if resp.Event == nil {
		writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Event not found")
		return
	}

	me := protoEventToMatrix(resp.Event)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(me)
}

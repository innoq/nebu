package matrix

// ─── Story 9-28: GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType} ───
//
// Returns events that relate to a parent event via the specified rel_type (e.g. "m.thread").
// Element Web calls this to populate the thread panel after receiving the first thread reply.
//
// AC1: returns events with rel_type m.thread for the given parent event_id.
// AC2: returns empty chunk when no thread replies exist.
// AC4: non-member → 403 M_FORBIDDEN.
// AC5: unauthenticated → 401 M_MISSING_TOKEN.
// AC6: unknown event_id → 404 M_NOT_FOUND.

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

// GetRelationsCoreClient is the consumer-defined gRPC interface for GetRelations.
// Keep minimal (Go interface convention, ADR-009).
type GetRelationsCoreClient interface {
	GetRelations(ctx context.Context, req *pb.GetRelationsRequest) (*pb.GetRelationsResponse, error)
}

// GetRelationsConfig holds dependencies for NewGetRelationsHandler.
type GetRelationsConfig struct {
	CoreClient GetRelationsCoreClient
}

// GetRelationsHandler handles GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}.
type GetRelationsHandler struct {
	coreClient GetRelationsCoreClient
}

// NewGetRelationsHandler constructs a GetRelationsHandler.
func NewGetRelationsHandler(cfg GetRelationsConfig) *GetRelationsHandler {
	return &GetRelationsHandler{coreClient: cfg.CoreClient}
}

// relationsChunkEvent is one event in the /relations chunk response.
type relationsChunkEvent struct {
	EventID  string          `json:"event_id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	RoomID   string          `json:"room_id"`
	Content  json.RawMessage `json:"content"`
	OriginTS int64           `json:"origin_server_ts"`
}

// relationsResponse is the JSON body for a successful /relations response.
type relationsResponse struct {
	Chunk     []relationsChunkEvent `json:"chunk"`
	NextBatch string                `json:"next_batch,omitempty"`
}

// GetRelations handles GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}.
func (h *GetRelationsHandler) GetRelations(w http.ResponseWriter, r *http.Request) {
	roomID  := r.PathValue("roomId")
	eventID := r.PathValue("eventId")
	relType := r.PathValue("relType")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Authentication required")
		return
	}

	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRelations(grpcCtx, &pb.GetRelationsRequest{
		UserId:  userID,
		RoomId:  roomID,
		EventId: eventID,
		RelType: relType,
		Limit:   20,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Not a room member")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Event not found")
		default:
			slog.Error("GetRelations gRPC failed", "code", st.Code(), "err", err)
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal error")
		}
		return
	}

	chunk := make([]relationsChunkEvent, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		content := json.RawMessage(e.GetContent())
		if len(content) == 0 {
			content = json.RawMessage(`{}`)
		}
		chunk = append(chunk, relationsChunkEvent{
			EventID:  e.GetEventId(),
			Type:     e.GetEventType(),
			Sender:   e.GetSenderId(),
			RoomID:   e.GetRoomId(),
			Content:  content,
			OriginTS: e.GetOriginTs(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(relationsResponse{
		Chunk:     chunk,
		NextBatch: resp.GetNextBatch(),
	})
}

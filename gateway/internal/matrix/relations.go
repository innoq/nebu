package matrix

// ─── Story 9-28 / 9-29: GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}[/{relType}[/{eventType}]] ───
//
// Returns events that relate to a parent event, optionally filtered by rel_type and event_type.
// Story 9-28: introduced handler for /{relType} variant.
// Story 9-29:
//   - Adds base route /relations/{eventId} (no relType) — fixes Element Web 404.
//   - Adds three-segment route /relations/{eventId}/{relType}/{eventType}.
//   - Adds query param support: dir (f/b), limit, recurse, from.
//   - dir=b → newest-first (DESC, default). dir=f → oldest-first (ASC).
//   - recurse=true MUST be accepted without error (Matrix CS API requirement).
//   - Returns prev_batch when more results exist in dir=b direction.
//
// All three URL variants share a single GetRelations handler method.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

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

// GetRelationsHandler handles all three /relations route variants.
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
	PrevBatch string                `json:"prev_batch,omitempty"`
}

// GetRelations handles all three route variants:
//   - GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}
//   - GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}
//   - GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}/{eventType}
//
// Query params:
//   - dir: "f" or "b" (default "b"). Returns 400 M_BAD_PARAM if invalid.
//   - limit: integer, default 20, max 100.
//   - recurse: boolean, accepted without error (MVP: pass through to Core).
//   - from: opaque pagination token, passed through to Core.
func (h *GetRelationsHandler) GetRelations(w http.ResponseWriter, r *http.Request) {
	roomID    := r.PathValue("roomId")
	eventID   := r.PathValue("eventId")
	relType   := r.PathValue("relType")   // empty for base route
	eventType := r.PathValue("eventType") // empty unless three-segment route

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Authentication required")
		return
	}

	// ── Query param: dir ─────────────────────────────────────────────────────
	// Matrix spec: "b" (newest-first DESC) or "f" (oldest-first ASC). Default "b".
	// Return 400 M_BAD_PARAM for any other value.
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "b"
	}
	if dir != "b" && dir != "f" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_PARAM",
			"dir must be 'f' or 'b'")
		return
	}

	// ── Query param: limit ───────────────────────────────────────────────────
	limit := int32(20)
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if n, err := strconv.ParseInt(rawLimit, 10, 32); err == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			limit = int32(n)
		}
	}

	// ── Query param: recurse ─────────────────────────────────────────────────
	// Matrix spec: MUST be accepted without error. Pass through to Core.
	recurse := false
	if rawRecurse := r.URL.Query().Get("recurse"); rawRecurse != "" {
		parsed, err := strconv.ParseBool(rawRecurse)
		if err != nil {
			writeMatrixError(w, http.StatusBadRequest, "M_BAD_PARAM",
				"recurse must be a boolean (true/false/1/0)")
			return
		}
		recurse = parsed
	}

	// ── Query param: from ────────────────────────────────────────────────────
	from := r.URL.Query().Get("from")

	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRelations(grpcCtx, &pb.GetRelationsRequest{
		UserId:    userID,
		RoomId:    roomID,
		EventId:   eventID,
		RelType:   relType,
		Limit:     limit,
		EventType: eventType,
		Dir:       dir,
		Recurse:   recurse,
		From:      from,
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
		PrevBatch: resp.GetPrevBatch(),
	})
}

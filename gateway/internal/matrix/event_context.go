package matrix

// ─── Story 7-28: GET /_matrix/client/v3/rooms/{roomId}/context/{eventId} ──────
//
// Returns the target event, up to `limit` events before and after it, a state
// snapshot, and pagination tokens compatible with GET /rooms/{roomId}/messages.
//
// Membership check: gRPC Core enforces PERMISSION_DENIED if the calling user is
// not a joined member of the room.
//
// Limit parsing:  default 10, maximum 100 (values above 100 are clamped silently).

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

// GetEventContextCoreClient is the consumer-defined interface for the GetEventContext gRPC call.
// Keep minimal — only what this handler needs (Go interface convention, ADR-009).
type GetEventContextCoreClient interface {
	GetEventContext(ctx context.Context, req *pb.GetEventContextRequest) (*pb.GetEventContextResponse, error)
}

// GetEventContextConfig holds dependencies for NewGetEventContextHandler.
type GetEventContextConfig struct {
	CoreClient GetEventContextCoreClient
	ServerName string
}

// GetEventContextHandler handles GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}.
type GetEventContextHandler struct {
	coreClient GetEventContextCoreClient
	serverName string
}

// NewGetEventContextHandler constructs a GetEventContextHandler from the provided config.
func NewGetEventContextHandler(cfg GetEventContextConfig) *GetEventContextHandler {
	return &GetEventContextHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// eventContextStateEvent is the Matrix Client-Server API state event format in context responses.
type eventContextStateEvent struct {
	Type     string         `json:"type"`
	StateKey string         `json:"state_key"`
	Content  map[string]any `json:"content"`
	Sender   string         `json:"sender"`
}

// getEventContextResponse is the JSON response for GET /rooms/{roomId}/context/{eventId}.
type getEventContextResponse struct {
	Start        string                   `json:"start"`
	End          string                   `json:"end"`
	Event        *matrixEvent             `json:"event"`
	EventsBefore []matrixEvent            `json:"events_before"`
	EventsAfter  []matrixEvent            `json:"events_after"`
	State        []eventContextStateEvent `json:"state"`
}

// GetEventContext handles GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}.
//
// Flow:
//  1. Extract roomId and eventId from URL path via Go 1.22+ r.PathValue.
//  2. Validate eventId format — must be a valid Matrix event ID.
//  3. Parse ?limit query param: default 10, clamp 1–100, error on non-numeric.
//  4. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  5. Call Core.GetEventContext — map gRPC errors to Matrix error codes.
//  6. Map response events to Matrix format; return 200.
func (h *GetEventContextHandler) GetEventContext(w http.ResponseWriter, r *http.Request) {
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

	// Parse limit — default 10, clamp 1–100, error on non-numeric non-empty string.
	q := r.URL.Query()
	limitStr := q.Get("limit")
	limit := int32(10)
	if limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "limit must be an integer")
			return
		}
		if parsed < 1 {
			parsed = 1
		}
		if parsed > 100 {
			parsed = 100
		}
		limit = int32(parsed)
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Authentication required")
		return
	}
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetEventContext(grpcCtx, &pb.GetEventContextRequest{
		RoomId:  roomID,
		EventId: eventID,
		Limit:   limit,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Event not found")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		default:
			slog.Error("GetEventContext gRPC failed", "code", st.Code(), "msg", st.Message(), "room_id", roomID, "event_id", eventID)
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Map the target event.
	var targetEvent *matrixEvent
	if resp.Event != nil {
		me := protoEventToMatrix(resp.Event)
		targetEvent = &me
	}

	// Map events_before — use make to ensure JSON array not null.
	before := make([]matrixEvent, 0, len(resp.EventsBefore))
	for _, e := range resp.EventsBefore {
		before = append(before, protoEventToMatrix(e))
	}

	// Map events_after.
	after := make([]matrixEvent, 0, len(resp.EventsAfter))
	for _, e := range resp.EventsAfter {
		after = append(after, protoEventToMatrix(e))
	}

	// Map state events — use make to ensure JSON array not null.
	stateEvents := make([]eventContextStateEvent, 0, len(resp.State))
	for _, se := range resp.State {
		var content map[string]any
		if len(se.Content) > 0 {
			_ = json.Unmarshal(se.Content, &content)
		}
		if content == nil {
			content = map[string]any{}
		}
		stateEvents = append(stateEvents, eventContextStateEvent{
			Type:     se.Type,
			StateKey: se.StateKey,
			Content:  content,
			Sender:   se.Sender,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(getEventContextResponse{
		Start:        resp.StartToken,
		End:          resp.EndToken,
		Event:        targetEvent,
		EventsBefore: before,
		EventsAfter:  after,
		State:        stateEvents,
	})
}

// protoEventToMatrix maps a pb.Event to the matrixEvent JSON format.
// matrixEvent is defined in messages.go (same package).
func protoEventToMatrix(e *pb.Event) matrixEvent {
	var content map[string]any
	if len(e.Content) > 0 {
		_ = json.Unmarshal(e.Content, &content)
	}
	if content == nil {
		content = map[string]any{}
	}
	return matrixEvent{
		EventID:        e.EventId,
		RoomID:         e.RoomId,
		Sender:         e.SenderId,
		Type:           e.EventType,
		Content:        content,
		OriginServerTS: e.OriginTs,
	}
}

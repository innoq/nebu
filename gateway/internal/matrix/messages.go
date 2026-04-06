package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetMessagesConfig holds dependencies for NewGetMessagesHandler.
type GetMessagesConfig struct {
	CoreClient GetMessagesCoreClient
	ServerName string
}

// matrixEvent is the Matrix Client-Server API event format for /messages chunks.
type matrixEvent struct {
	EventID        string         `json:"event_id"`
	RoomID         string         `json:"room_id"`
	Sender         string         `json:"sender"`
	Type           string         `json:"type"`
	Content        map[string]any `json:"content"`
	OriginServerTS int64          `json:"origin_server_ts"`
}

// getMessagesResponse is the JSON response for GET /rooms/{roomId}/messages.
type getMessagesResponse struct {
	Start string        `json:"start"`
	End   string        `json:"end"`
	Chunk []matrixEvent `json:"chunk"`
	State []any         `json:"state"`
}

// GetMessagesHandler handles GET /_matrix/client/v3/rooms/{roomId}/messages.
type GetMessagesHandler struct {
	coreClient GetMessagesCoreClient
	serverName string
}

// NewGetMessagesHandler constructs a GetMessagesHandler from the provided config.
func NewGetMessagesHandler(cfg GetMessagesConfig) *GetMessagesHandler {
	return &GetMessagesHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// GetMessages handles GET /_matrix/client/v3/rooms/{roomId}/messages.
//
// Flow:
//  1. Extract roomId from URL path via Go 1.22+ r.PathValue.
//  2. Parse query params: from, dir (default "b"), limit (default 10, clamp 1-100), to.
//  3. Validate dir — only "f" or "b" allowed → 400 M_INVALID_PARAM.
//  4. Return 400 M_INVALID_PARAM if limit is provided but non-numeric.
//  5. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  6. Call Core.GetMessages — map gRPC errors to Matrix error codes.
//  7. Map response events to Matrix format; return 200.
func (h *GetMessagesHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	q := r.URL.Query()

	// Parse direction — default "b"; validate "f" or "b" only.
	dir := q.Get("dir")
	if dir == "" {
		dir = "b"
	}
	if dir != "f" && dir != "b" {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "dir must be 'f' or 'b'")
		return
	}

	// Parse limit — default 10, clamp 1–100, error on non-numeric non-empty string.
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

	fromToken := q.Get("from")
	toToken := q.Get("to")

	sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	userID := coregrpc.FormatUserID(sub, h.serverName)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetMessages(grpcCtx, &pb.GetMessagesRequest{
		RoomId:    roomID,
		FromToken: fromToken,
		ToToken:   &toToken,
		Limit:     limit,
		Direction: dir,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Map proto events to Matrix JSON format.
	// Use make([]matrixEvent, 0) to ensure chunk serialises as [] not null when empty.
	chunk := make([]matrixEvent, 0, len(resp.Events))
	for _, e := range resp.Events {
		var content map[string]any
		if len(e.Content) > 0 {
			_ = json.Unmarshal(e.Content, &content)
		}
		if content == nil {
			content = map[string]any{}
		}
		chunk = append(chunk, matrixEvent{
			EventID:        e.EventId,
			RoomID:         e.RoomId,
			Sender:         e.SenderId,
			Type:           e.EventType,
			Content:        content,
			OriginServerTS: e.OriginTs,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(getMessagesResponse{
		Start: resp.PrevBatch,
		End:   resp.NextBatch,
		Chunk: chunk,
		State: []any{},
	})
}

// GetMessagesCoreClient is the consumer-defined interface for the GetMessages gRPC call.
// Keep minimal — only what this handler needs (Go interface convention, ADR-009).
type GetMessagesCoreClient interface {
	GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error)
}

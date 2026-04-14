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

// TypingCoreClient is a consumer-defined interface for the SetTyping gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type TypingCoreClient interface {
	SetTyping(ctx context.Context, req *pb.SetTypingRequest) (*pb.SetTypingResponse, error)
}

// typingRequestBody is the JSON body for PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId}.
type typingRequestBody struct {
	Typing  bool  `json:"typing"`
	Timeout int32 `json:"timeout"` // milliseconds
}

// TypingHandler handles PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId}.
type TypingHandler struct {
	coreClient TypingCoreClient
	serverName string
}

// TypingConfig holds dependencies for NewTypingHandler.
type TypingConfig struct {
	CoreClient TypingCoreClient
	ServerName string
}

// NewTypingHandler constructs a TypingHandler from the provided config.
func NewTypingHandler(cfg TypingConfig) *TypingHandler {
	return &TypingHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PutTyping handles PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId}.
//
// Flow:
//  1. Extract roomId and userId from path via r.PathValue.
//  2. Extract authenticated user_id from JWT context.
//  3. If path userId != authenticated userID → 403 M_FORBIDDEN (BEFORE Core call).
//  4. Decode JSON body: {"typing": bool, "timeout": int}.
//  5. Clamp timeout_ms: max 30000, min 0; if typing=false, set timeout_ms=0.
//  6. Build gRPC metadata and call CoreService.SetTyping.
//  7. Map gRPC errors: NotFound → 404 M_NOT_FOUND; PermissionDenied → 403 M_FORBIDDEN; default → 500 M_UNKNOWN.
//  8. Return 200 {} on success.
func (h *TypingHandler) PutTyping(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	pathUserID := r.PathValue("userId")

	// Step 2: Extract authenticated user_id from JWT context.
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	authedUserID := userID

	// Step 3: Validate path userId matches authenticated user_id.
	if pathUserID != authedUserID {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "userId in path does not match authenticated user")
		return
	}

	// Step 4: Decode JSON body.
	var body typingRequestBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			// Body is optional per spec; treat decode error as empty body.
			body = typingRequestBody{}
		}
	}

	// Step 5: Clamp timeout_ms.
	timeoutMs := body.Timeout
	if timeoutMs < 0 {
		timeoutMs = 0
	}
	if timeoutMs > 30000 {
		timeoutMs = 30000
	}
	// When typing=false, timeout is irrelevant — set to 0.
	if !body.Typing {
		timeoutMs = 0
	}

	// Step 6: Call gRPC Core.
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), authedUserID, systemRole)
	_, err := h.coreClient.SetTyping(grpcCtx, &pb.SetTypingRequest{
		RoomId:    roomID,
		UserId:    authedUserID,
		Typing:    body.Typing,
		TimeoutMs: timeoutMs,
	})
	if err != nil {
		// Step 7: Map gRPC errors.
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

	// Step 8: Return 200 {}.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

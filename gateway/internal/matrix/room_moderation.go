package matrix

// ─── Story 7-22: Room Moderation — kick / ban / unban / forget ────────────────
//
// Implements four Matrix moderation endpoints:
//   POST /_matrix/client/v3/rooms/{roomId}/kick
//   POST /_matrix/client/v3/rooms/{roomId}/ban
//   POST /_matrix/client/v3/rooms/{roomId}/unban
//   POST /_matrix/client/v3/rooms/{roomId}/forget
//
// Error mapping (from story implementation notes):
//   codes.PermissionDenied    → 403 M_FORBIDDEN
//   codes.NotFound            → 404 M_NOT_FOUND
//   codes.InvalidArgument     → 400 M_BAD_JSON
//   codes.FailedPrecondition  → 403 M_FORBIDDEN (e.g. forget while joined)
//   codes.Unavailable         → 503 M_UNAVAILABLE
//   default                   → 500 M_UNKNOWN

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

// ModerationCoreClient is a consumer-defined interface for the four moderation gRPC calls.
// Keeping it minimal — only what ModerationHandler needs (Go interface convention, ADR-009).
type ModerationCoreClient interface {
	KickUser(ctx context.Context, req *pb.KickUserRequest) (*pb.KickUserResponse, error)
	BanUser(ctx context.Context, req *pb.BanUserRequest) (*pb.BanUserResponse, error)
	UnbanUser(ctx context.Context, req *pb.UnbanUserRequest) (*pb.UnbanUserResponse, error)
	ForgetRoom(ctx context.Context, req *pb.ForgetRoomRequest) (*pb.ForgetRoomResponse, error)
}

// membershipActionRequest is the shared JSON body for /kick, /ban, /unban.
// /forget uses an empty body (user_id and reason not required).
type membershipActionRequest struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason,omitempty"`
}

// ModerationHandler handles POST /kick, /ban, /unban, /forget.
type ModerationHandler struct {
	coreClient ModerationCoreClient
	serverName string
}

// ModerationConfig holds dependencies for NewModerationHandler.
type ModerationConfig struct {
	CoreClient ModerationCoreClient
	ServerName string
}

// NewModerationHandler constructs a ModerationHandler from the provided config.
func NewModerationHandler(cfg ModerationConfig) *ModerationHandler {
	return &ModerationHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// grpcErrToModerationHTTP maps gRPC status codes to Matrix HTTP error responses
// for all four moderation endpoints (consistent error-mapping per story notes).
func grpcErrToModerationHTTP(w http.ResponseWriter, err error) {
	st, _ := status.FromError(err)
	switch st.Code() {
	case codes.PermissionDenied:
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", st.Message())
	case codes.FailedPrecondition:
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", st.Message())
	case codes.NotFound:
		writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", st.Message())
	case codes.InvalidArgument:
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", st.Message())
	case codes.Unavailable:
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", st.Message())
	default:
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
	}
}

// PostKickUser handles POST /_matrix/client/v3/rooms/{roomId}/kick.
//
// Flow:
//  1. Extract roomId from URL path via Go 1.22+ r.PathValue.
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input or missing user_id.
//  3. Build caller userID from JWT context.
//  4. Call Core.KickUser — map gRPC errors to Matrix error codes.
//  5. Return 200 {} on success.
func (h *ModerationHandler) PostKickUser(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	var body membershipActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}
	if body.UserID == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Missing required field: user_id")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	_, err := h.coreClient.KickUser(grpcCtx, &pb.KickUserRequest{
		RoomId:   roomID,
		CallerId: userID,
		TargetId: body.UserID,
		Reason:   body.Reason,
	})
	if err != nil {
		grpcErrToModerationHTTP(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

// PostBanUser handles POST /_matrix/client/v3/rooms/{roomId}/ban.
//
// Flow:
//  1. Extract roomId from URL path.
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input or missing user_id.
//  3. Build caller userID from JWT context.
//  4. Call Core.BanUser — map gRPC errors to Matrix error codes.
//  5. Return 200 {} on success.
func (h *ModerationHandler) PostBanUser(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	var body membershipActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}
	if body.UserID == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Missing required field: user_id")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	_, err := h.coreClient.BanUser(grpcCtx, &pb.BanUserRequest{
		RoomId:   roomID,
		CallerId: userID,
		TargetId: body.UserID,
		Reason:   body.Reason,
	})
	if err != nil {
		grpcErrToModerationHTTP(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

// PostUnbanUser handles POST /_matrix/client/v3/rooms/{roomId}/unban.
//
// Flow:
//  1. Extract roomId from URL path.
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input or missing user_id.
//  3. Build caller userID from JWT context.
//  4. Call Core.UnbanUser — map gRPC errors to Matrix error codes.
//  5. Return 200 {} on success.
func (h *ModerationHandler) PostUnbanUser(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	var body membershipActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}
	if body.UserID == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Missing required field: user_id")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	_, err := h.coreClient.UnbanUser(grpcCtx, &pb.UnbanUserRequest{
		RoomId:   roomID,
		CallerId: userID,
		TargetId: body.UserID,
	})
	if err != nil {
		grpcErrToModerationHTTP(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

// PostForgetRoom handles POST /_matrix/client/v3/rooms/{roomId}/forget.
//
// Flow:
//  1. Extract roomId from URL path.
//  2. Build caller userID from JWT context (no body needed — user forgets their own room).
//  3. Call Core.ForgetRoom — map gRPC errors to Matrix error codes.
//     FailedPrecondition means the user is still joined (must leave first) → 403.
//  4. Return 200 {} on success.
func (h *ModerationHandler) PostForgetRoom(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	_, err := h.coreClient.ForgetRoom(grpcCtx, &pb.ForgetRoomRequest{
		RoomId: roomID,
		UserId: userID,
	})
	if err != nil {
		grpcErrToModerationHTTP(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

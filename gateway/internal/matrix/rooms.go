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
	"google.golang.org/protobuf/proto"
)

// CreateRoomCoreClient is a consumer-defined interface for the CreateRoom gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type CreateRoomCoreClient interface {
	CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error)
}

// CreateRoomRequest is the JSON body for POST /_matrix/client/v3/createRoom.
type CreateRoomRequest struct {
	RoomAliasName string   `json:"room_alias_name,omitempty"`
	Name          string   `json:"name,omitempty"`
	Topic         string   `json:"topic,omitempty"`
	Visibility    string   `json:"visibility,omitempty"`
	Invite        []string `json:"invite,omitempty"`
	Preset        string   `json:"preset,omitempty"`
}

// CreateRoomResponse is the JSON response for a successful room creation.
type CreateRoomResponse struct {
	RoomID string `json:"room_id"`
}

// CreateRoomHandler handles POST /_matrix/client/v3/createRoom.
type CreateRoomHandler struct {
	coreClient CreateRoomCoreClient
	serverName string
}

// CreateRoomConfig holds dependencies for NewCreateRoomHandler.
type CreateRoomConfig struct {
	CoreClient CreateRoomCoreClient
	ServerName string
}

// NewCreateRoomHandler constructs a CreateRoomHandler from the provided config.
func NewCreateRoomHandler(cfg CreateRoomConfig) *CreateRoomHandler {
	return &CreateRoomHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PostCreateRoom handles POST /_matrix/client/v3/createRoom.
//
// Flow:
//  1. Decode optional JSON body — 400 M_BAD_JSON on malformed input.
//  2. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  3. Build userID and attach gRPC metadata.
//  4. Call Core.CreateRoom — map gRPC errors to Matrix error codes.
//  5. Return 200 {"room_id": ...} on success.
func (h *CreateRoomHandler) PostCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	userID := coregrpc.FormatUserID(sub, h.serverName)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	grpcReq := &pb.CreateRoomRequest{CreatorId: userID}
	if req.Name != "" {
		grpcReq.Name = proto.String(req.Name)
	}
	if req.Topic != "" {
		grpcReq.Topic = proto.String(req.Topic)
	}

	resp, err := h.coreClient.CreateRoom(grpcCtx, grpcReq)
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.AlreadyExists:
			writeMatrixError(w, http.StatusBadRequest, "M_ROOM_IN_USE", "Room alias already in use")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to create rooms")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}
	if resp == nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(CreateRoomResponse{RoomID: resp.RoomId})
}

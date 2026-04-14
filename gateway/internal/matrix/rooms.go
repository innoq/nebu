package matrix

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// CreateRoomCoreClient is a consumer-defined interface for the CreateRoom gRPC calls.
// InviteUser is included so PostCreateRoom can process the invite list inline.
type CreateRoomCoreClient interface {
	CreateRoom(ctx context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error)
	InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error)
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

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
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

	// Process invite list — send InviteUser gRPC for each invited user.
	// Errors are logged but non-fatal: the room is already created.
	for _, invitee := range req.Invite {
		_, invErr := h.coreClient.InviteUser(grpcCtx, &pb.InviteUserRequest{
			RoomId:    resp.RoomId,
			InviterId: userID,
			InviteeId: invitee,
		})
		if invErr != nil {
			slog.Warn("createRoom: invite failed", "room_id", resp.RoomId, "invitee", invitee, "err", invErr)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(CreateRoomResponse{RoomID: resp.RoomId})
}

// ─── JoinRoomHandler ──────────────────────────────────────────────────────────

// JoinRoomCoreClient is a consumer-defined interface for the JoinRoom gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type JoinRoomCoreClient interface {
	JoinRoom(ctx context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error)
}

// JoinRoomResponse is the JSON response for a successful room join.
type JoinRoomResponse struct {
	RoomID string `json:"room_id"`
}

// JoinRoomHandler handles POST /_matrix/client/v3/join/{roomIdOrAlias}
// and POST /_matrix/client/v3/rooms/{roomId}/join.
type JoinRoomHandler struct {
	coreClient JoinRoomCoreClient
	serverName string
}

// JoinRoomConfig holds dependencies for NewJoinRoomHandler.
type JoinRoomConfig struct {
	CoreClient JoinRoomCoreClient
	ServerName string
}

// NewJoinRoomHandler constructs a JoinRoomHandler from the provided config.
func NewJoinRoomHandler(cfg JoinRoomConfig) *JoinRoomHandler {
	return &JoinRoomHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// postJoinRoomWithID is the shared implementation for both join endpoints.
// roomIDOrAlias is extracted from the URL path by the caller.
func (h *JoinRoomHandler) postJoinRoomWithID(w http.ResponseWriter, r *http.Request, roomIDOrAlias string) {
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.JoinRoom(grpcCtx, &pb.JoinRoomRequest{
		UserId:        userID,
		RoomIdOrAlias: roomIDOrAlias,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.AlreadyExists:
			// Matrix spec: idempotent join — already a member is success.
			// We need the room_id; use the path value as fallback.
			roomID := roomIDOrAlias
			if resp != nil && resp.RoomId != "" {
				roomID = resp.RoomId
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(JoinRoomResponse{RoomID: roomID})
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Not allowed to join this room")
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
	_ = json.NewEncoder(w).Encode(JoinRoomResponse{RoomID: resp.RoomId})
}

// PostJoinRoom handles POST /_matrix/client/v3/join/{roomIdOrAlias}.
// Extracts roomIdOrAlias from the URL path via Go 1.22+ mux PathValue.
func (h *JoinRoomHandler) PostJoinRoom(w http.ResponseWriter, r *http.Request) {
	roomIDOrAlias := r.PathValue("roomIdOrAlias")
	h.postJoinRoomWithID(w, r, roomIDOrAlias)
}

// PostJoinRoomById handles POST /_matrix/client/v3/rooms/{roomId}/join
// (accept invitation via room ID — same gRPC call, different URL shape).
func (h *JoinRoomHandler) PostJoinRoomById(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	h.postJoinRoomWithID(w, r, roomID)
}

// ─── InviteUserHandler ────────────────────────────────────────────────────────

// InviteUserCoreClient is a consumer-defined interface for the InviteUser gRPC call.
type InviteUserCoreClient interface {
	InviteUser(ctx context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error)
}

// inviteUserBody is the JSON body for POST /_matrix/client/v3/rooms/{roomId}/invite.
type inviteUserBody struct {
	UserID string `json:"user_id"`
}

// InviteUserHandler handles POST /_matrix/client/v3/rooms/{roomId}/invite.
type InviteUserHandler struct {
	coreClient InviteUserCoreClient
	serverName string
}

// InviteUserConfig holds dependencies for NewInviteUserHandler.
type InviteUserConfig struct {
	CoreClient InviteUserCoreClient
	ServerName string
}

// NewInviteUserHandler constructs an InviteUserHandler from the provided config.
func NewInviteUserHandler(cfg InviteUserConfig) *InviteUserHandler {
	return &InviteUserHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PostInviteUser handles POST /_matrix/client/v3/rooms/{roomId}/invite.
//
// Flow:
//  1. Extract roomId from URL path via Go 1.22+ mux PathValue.
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input.
//  3. Build caller userID from JWT context.
//  4. Call Core.InviteUser — map gRPC errors to Matrix error codes.
//  5. Return 200 {} on success.
func (h *InviteUserHandler) PostInviteUser(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	var body inviteUserBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	callerUserID := userID
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), callerUserID, systemRole)

	_, err := h.coreClient.InviteUser(grpcCtx, &pb.InviteUserRequest{
		RoomId:    roomID,
		InviterId: callerUserID,
		InviteeId: body.UserID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to invite users")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

// ─── SetRoomStateHandler ──────────────────────────────────────────────────────

// SetRoomStateCoreClient is a consumer-defined interface for the SetPowerLevels gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type SetRoomStateCoreClient interface {
	SetPowerLevels(ctx context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error)
}

// setRoomStateResponse is the JSON response for a successful state event.
type setRoomStateResponse struct {
	EventID string `json:"event_id"`
}

// SetRoomStateHandler handles PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}.
type SetRoomStateHandler struct {
	coreClient SetRoomStateCoreClient
	serverName string
}

// SetRoomStateConfig holds dependencies for NewSetRoomStateHandler.
type SetRoomStateConfig struct {
	CoreClient SetRoomStateCoreClient
	ServerName string
}

// NewSetRoomStateHandler constructs a SetRoomStateHandler from the provided config.
func NewSetRoomStateHandler(cfg SetRoomStateConfig) *SetRoomStateHandler {
	return &SetRoomStateHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PutSetRoomState handles PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}.
//
// Flow:
//  1. Extract roomId, eventType, stateKey from URL path via Go 1.22+ r.PathValue.
//  2. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  3. Decode JSON body; return 400 M_BAD_JSON on failure.
//  4. For m.room.power_levels: JSON-encode the body and call gRPC CoreService.SetPowerLevels.
//  5. Map gRPC errors: PERMISSION_DENIED → 403 M_FORBIDDEN, NOT_FOUND → 404 M_NOT_FOUND.
//  6. Return 200 {"event_id": ""} on success — state events don't generate event_ids in MVP.
func (h *SetRoomStateHandler) PutSetRoomState(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("eventType")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	if eventType == "m.room.power_levels" {
		powerLevelsJSON, err := json.Marshal(body)
		if err != nil {
			writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Cannot encode power_levels content")
			return
		}

		_, err = h.coreClient.SetPowerLevels(grpcCtx, &pb.SetPowerLevelsRequest{
			RoomId:          roomID,
			PowerLevelsJson: string(powerLevelsJSON),
		})
		if err != nil {
			st, _ := status.FromError(err)
			switch st.Code() {
			case codes.PermissionDenied:
				writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You do not have permission to set power levels")
			case codes.NotFound:
				writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
			default:
				writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setRoomStateResponse{EventID: ""})
		return
	}

	// For other state event types, return 501 Not Implemented (MVP scope).
	writeMatrixError(w, http.StatusNotImplemented, "M_UNRECOGNIZED", "Unsupported state event type")
}

// ─── SendEventHandler ─────────────────────────────────────────────────────────

// SendEventCoreClient is a consumer-defined interface for the SendEvent gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type SendEventCoreClient interface {
	SendEvent(ctx context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error)
}

// sendEventResponse is the JSON response for a successful event send.
type sendEventResponse struct {
	EventID string `json:"event_id"`
}

// SendEventHandler handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
type SendEventHandler struct {
	coreClient SendEventCoreClient
	serverName string
}

// SendEventConfig holds dependencies for NewSendEventHandler.
type SendEventConfig struct {
	CoreClient SendEventCoreClient
	ServerName string
}

// NewSendEventHandler constructs a SendEventHandler from the provided config.
func NewSendEventHandler(cfg SendEventConfig) *SendEventHandler {
	return &SendEventHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PutSendEvent handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
//
// Flow:
//  1. Extract roomId, eventType, txnId from URL path via Go 1.22+ r.PathValue.
//  2. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  3. Decode JSON body → content map; 400 M_BAD_JSON on failure.
//  4. Build gRPC request: JSON-encode content bytes, use time.Now().UnixMilli() as origin_ts.
//  5. Call Core.SendEvent — map gRPC errors to Matrix error codes.
//  6. Return 200 {"event_id": resp.EventId} on success.
func (h *SendEventHandler) PutSendEvent(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("eventType")
	txnID := r.PathValue("txnId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	var content map[string]any
	if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	contentBytes, err := json.Marshal(content)
	if err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Cannot encode event content")
		return
	}

	resp, err := h.coreClient.SendEvent(grpcCtx, &pb.SendEventRequest{
		RoomId:    roomID,
		SenderId:  userID,
		EventType: eventType,
		TxnId:     txnID,
		Content:   contentBytes,
		OriginTs:  time.Now().UnixMilli(),
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not allowed to send events to this room")
		case codes.ResourceExhausted:
			writeMatrixError(w, http.StatusTooManyRequests, "M_LIMIT_EXCEEDED", "Rate limit exceeded")
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
	_ = json.NewEncoder(w).Encode(sendEventResponse{EventID: resp.EventId})
}

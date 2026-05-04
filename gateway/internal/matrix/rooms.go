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
	IsDirect      bool     `json:"is_direct,omitempty"`
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
//
// TODO(6-8b): Read room_defaults (max_members, visibility) from the DB and apply
// them to grpcReq when the caller does not provide explicit overrides. This
// downstream integration is deferred to story 6-8b. See Dev Agent Record in
// 6-8-room-settings-update-api-max-members-visibility-serverweite-defaults.md.
func (h *CreateRoomHandler) PostCreateRoom(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req CreateRoomRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
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
		case codes.ResourceExhausted:
			// Story 6.8: Room GenServer enforces max_members limit — room is full.
			writeMatrixError(w, http.StatusForbidden, "M_ROOM_FULL", "Room is full")
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
//  2. Check eventType against allowedStateEventTypes — reject unknown types with 400 M_BAD_JSON
//     before any body decoding (Story 9-6, AC3 / AC4).
//  3. Extract sub + systemRole from JWT context (set by JWTMiddleware).
//  4. Decode JSON body; return 400 M_BAD_JSON on failure.
//  5. For m.room.power_levels: JSON-encode the body and call gRPC CoreService.SetPowerLevels.
//  6. Map gRPC errors: PERMISSION_DENIED → 403 M_FORBIDDEN, NOT_FOUND → 404 M_NOT_FOUND.
//  7. Return 200 {"event_id": ""} on success — state events don't generate event_ids in MVP.
func (h *SetRoomStateHandler) PutSetRoomState(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("eventType")

	// Story 9-6 AC3 / AC4: reject any event type not in the single authoritative whitelist.
	// The check fires before body decoding so unknown types are rejected immediately.
	if !allowedStateEventTypes[eventType] {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "unknown state event type: "+eventType)
		return
	}

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

// RoomStatusChecker allows SendEventHandler to check room archive status before
// forwarding an event to Core. Implemented by dbRoomRepo (via GetRoomStatus method).
// Injected via SendEventConfig.StatusChecker.
//
// Story 6.9: AC#4 — PutSendEvent checks rooms.status before calling Core.SendEvent.
type RoomStatusChecker interface {
	GetRoomStatus(ctx context.Context, roomID string) (string, error)
}

// sendEventResponse is the JSON response for a successful event send.
type sendEventResponse struct {
	EventID string `json:"event_id"`
}

// SendEventHandler handles PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}.
type SendEventHandler struct {
	coreClient    SendEventCoreClient
	serverName    string
	statusChecker RoomStatusChecker // Story 6.9: archive status check (may be nil)
}

// SendEventConfig holds dependencies for NewSendEventHandler.
type SendEventConfig struct {
	CoreClient    SendEventCoreClient
	ServerName    string
	StatusChecker RoomStatusChecker // Story 6.9: inject roomsRepo; nil = skip check
}

// NewSendEventHandler constructs a SendEventHandler from the provided config.
func NewSendEventHandler(cfg SendEventConfig) *SendEventHandler {
	return &SendEventHandler{
		coreClient:    cfg.CoreClient,
		serverName:    cfg.ServerName,
		statusChecker: cfg.StatusChecker,
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

	// Story 6.9 AC#4: check room archive status BEFORE calling Core.SendEvent.
	// Fail-fast: archived rooms must not accept new events.
	// Fail-open on DB error (log and proceed) — Core will return NOT_FOUND for truly missing rooms.
	if h.statusChecker != nil {
		roomStatus, statusErr := h.statusChecker.GetRoomStatus(r.Context(), roomID)
		if statusErr != nil {
			slog.Warn("PutSendEvent: GetRoomStatus failed — proceeding (fail-open)",
				"room_id", roomID, "err", statusErr)
		} else if roomStatus == "archived" {
			writeMatrixError(w, http.StatusForbidden, "M_ROOM_ARCHIVED", "Room is archived")
			return
		}
		// roomStatus == "" means room not found — let Core.SendEvent return NOT_FOUND.
	}

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

// ─── GetRoomStateHandler ──────────────────────────────────────────────────────

// GetRoomStateCoreClient is a consumer-defined interface for the GetRoomState
// gRPC call used by GetRoomStateHandler. Separate from GetMembersCoreClient
// so the two handlers evolve independently (Go interface convention, ADR-009).
type GetRoomStateCoreClient interface {
	GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
}

// GetRoomStateConfig holds dependencies for NewGetRoomStateHandler.
type GetRoomStateConfig struct {
	CoreClient GetRoomStateCoreClient
	ServerName string
}

// GetRoomStateHandler handles:
//
//	GET /_matrix/client/v3/rooms/{roomId}/state
//	GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}
//	GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}
type GetRoomStateHandler struct {
	coreClient GetRoomStateCoreClient
	serverName string
}

// NewGetRoomStateHandler constructs a GetRoomStateHandler from the provided config.
func NewGetRoomStateHandler(cfg GetRoomStateConfig) *GetRoomStateHandler {
	return &GetRoomStateHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// stateEventJSON is the Matrix state event envelope returned by GET /state (AC1).
type stateEventJSON struct {
	Type     string `json:"type"`
	StateKey string `json:"state_key"`
	Content  any    `json:"content"`
	Sender   string `json:"sender"`
}

// grpcErrToMatrixState maps gRPC status codes to Matrix HTTP error responses.
// Consistent with error-mapping pattern from the story implementation notes.
func grpcErrToMatrixState(w http.ResponseWriter, err error) {
	st, _ := status.FromError(err)
	switch st.Code() {
	case codes.PermissionDenied:
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
	case codes.NotFound:
		writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room or state event not found")
	case codes.Unavailable:
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Server unavailable")
	default:
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
	}
}

// GetRoomState handles GET /_matrix/client/v3/rooms/{roomId}/state.
//
// AC1: returns 200 with a JSON array of all current state events.
// Each element has the Matrix state event envelope shape:
//
//	{"type": "...", "state_key": "...", "content": {...}, "sender": "..."}
//
// An empty room returns [] (never null).
func (h *GetRoomStateHandler) GetRoomState(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{
		RoomId: roomID,
		// EventType and StateKey left empty → return all state events.
	})
	if err != nil {
		grpcErrToMatrixState(w, err)
		return
	}

	// Build the response array. Initialise as empty slice (not nil) so JSON
	// encoding produces [] rather than null for rooms with no state events.
	events := make([]stateEventJSON, 0, len(resp.StateEvents))
	for _, ev := range resp.StateEvents {
		var content any
		if len(ev.Content) > 0 {
			if jsonErr := json.Unmarshal(ev.Content, &content); jsonErr != nil {
				// Fall back to raw string on malformed JSON — never drop the event.
				content = string(ev.Content)
			}
		} else {
			content = map[string]any{}
		}
		events = append(events, stateEventJSON{
			Type:     ev.Type,
			StateKey: ev.StateKey,
			Content:  content,
			Sender:   ev.Sender,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

// ─── GetRoomAliasesHandler ────────────────────────────────────────────────────

// GetRoomAliasesCoreClient is a consumer-defined interface for the GetRoomState
// gRPC call used by GetRoomAliasesHandler. Using GetRoomState (rather than a
// dedicated GetRoomAliases RPC) for membership verification avoids a new proto
// message in the MVP (story 7-23, implementation notes).
// Separate interface so this handler evolves independently (Go convention, ADR-009).
type GetRoomAliasesCoreClient interface {
	GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
}

// getRoomAliasesResponse is the JSON response for GET /rooms/{roomId}/aliases.
// The aliases field MUST always be present, even when the array is empty (AC5).
type getRoomAliasesResponse struct {
	Aliases []string `json:"aliases"`
}

// GetRoomAliasesHandler handles GET /_matrix/client/v3/rooms/{roomId}/aliases.
type GetRoomAliasesHandler struct {
	coreClient GetRoomAliasesCoreClient
	serverName string
}

// GetRoomAliasesConfig holds dependencies for NewGetRoomAliasesHandler.
type GetRoomAliasesConfig struct {
	CoreClient GetRoomAliasesCoreClient
	ServerName string
}

// NewGetRoomAliasesHandler constructs a GetRoomAliasesHandler from the provided config.
func NewGetRoomAliasesHandler(cfg GetRoomAliasesConfig) *GetRoomAliasesHandler {
	return &GetRoomAliasesHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// GetRoomAliases handles GET /_matrix/client/v3/rooms/{roomId}/aliases.
//
// Flow:
//  1. Extract roomId from URL path via r.PathValue.
//  2. Extract authenticated user_id + systemRole from JWT context.
//  3. Call Core.GetRoomState to verify membership and room existence
//     (reuses the existing gRPC call — no new proto message needed for MVP).
//  4. On success: return 200 {"aliases":[]}.
//  5. On gRPC error: map to Matrix error (same pattern as other room handlers).
//
// The handler is deliberately extensible: when alias storage is added in a
// future story, a GetRoomAliases gRPC call can be dropped in at step 4 without
// any changes to the route registration or middleware chain (AC6).
func (h *GetRoomAliasesHandler) GetRoomAliases(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	_, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		st, _ := status.FromError(err)
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

	// TODO(7-23): When alias storage is implemented (future story), replace this
	// empty slice with a GetRoomAliases gRPC call to Elixir Core.
	aliases := []string{}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(getRoomAliasesResponse{Aliases: aliases})
}

// GetRoomStateSingleEvent handles:
//
//	GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}
//	GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}
//
// AC2: returns the raw content object of the matching state event (no envelope).
// AC3: missing stateKey segment is equivalent to stateKey="" (empty string default).
// AC6: Core returns NotFound when no state event matches → 404 M_NOT_FOUND.
func (h *GetRoomStateHandler) GetRoomStateSingleEvent(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("eventType")
	stateKey := r.PathValue("stateKey") // empty string when the {stateKey} segment is absent

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{
		RoomId:    roomID,
		EventType: eventType,
		StateKey:  stateKey,
	})
	if err != nil {
		grpcErrToMatrixState(w, err)
		return
	}

	// Core should return exactly one matching state event when event_type is set.
	// If the state_events list is empty, treat it as not-found.
	if len(resp.StateEvents) == 0 {
		writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "State event not found")
		return
	}

	ev := resp.StateEvents[0]

	// Matrix spec: response is the raw content block only — no event envelope.
	var content any
	if len(ev.Content) > 0 {
		if jsonErr := json.Unmarshal(ev.Content, &content); jsonErr != nil {
			content = map[string]any{}
		}
	} else {
		content = map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(content)
}

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

// ReceiptsCoreClient is a consumer-defined interface for the SendReceipt gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type ReceiptsCoreClient interface {
	SendReceipt(ctx context.Context, req *pb.SendReceiptRequest) (*pb.SendReceiptResponse, error)
}

// ReceiptsHandler handles POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}.
type ReceiptsHandler struct {
	coreClient ReceiptsCoreClient
	serverName string
}

// ReceiptsConfig holds dependencies for NewReceiptsHandler.
type ReceiptsConfig struct {
	CoreClient ReceiptsCoreClient
	ServerName string
}

// NewReceiptsHandler constructs a ReceiptsHandler from the provided config.
func NewReceiptsHandler(cfg ReceiptsConfig) *ReceiptsHandler {
	return &ReceiptsHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// PostReceipt handles POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}.
//
// Flow:
//  1. Extract roomId, receiptType, eventId from path via r.PathValue.
//  2. Validate receiptType: only "m.read" and "m.read.private" are supported → 400 M_INVALID_PARAM.
//  3. Extract authenticated user_id from JWT context.
//  4. Build gRPC metadata and call CoreService.SendReceipt.
//  5. Map gRPC errors: PermissionDenied → 403 M_FORBIDDEN; NotFound → 404 M_NOT_FOUND; default → 500 M_UNKNOWN.
//  6. Return 200 {} on success.
func (h *ReceiptsHandler) PostReceipt(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	receiptType := r.PathValue("receiptType")
	eventID := r.PathValue("eventId")

	// Step 2: Validate receiptType — only m.read and m.read.private are supported in MVP.
	if receiptType != "m.read" && receiptType != "m.read.private" {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "Only m.read and m.read.private receipts are supported")
		return
	}

	// Step 3: Extract authenticated user_id from JWT context.
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

	// Step 4: Call gRPC Core.
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
	_, err := h.coreClient.SendReceipt(grpcCtx, &pb.SendReceiptRequest{
		RoomId:      roomID,
		UserId:      userID,
		ReceiptType: receiptType,
		EventId:     eventID,
	})
	if err != nil {
		// Step 5: Map gRPC errors.
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Step 6: Return 200 {}.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

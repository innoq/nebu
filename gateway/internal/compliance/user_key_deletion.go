package compliance

// user_key_deletion.go — Story 5.7: DSGVO User Key Deletion Handler
//
// Route: DELETE /api/v1/admin/users/{userId}/keys
// Auth: jwtMiddleware (existing) + instance_admin role gate
// Delegates the atomic key deletion to Elixir Core via gRPC DeleteUserKeys.
// Emits "user_keys_deleted" audit on success (never-raise, 500ms timeout).

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// UserKeyDeletionHandler handles DELETE /api/v1/admin/users/{userId}/keys.
// CoreClient is the gRPC stub used for both the deletion RPC and audit emission.
// DB is intentionally not included here: the Go handler does NOT query users directly;
// all user-existence checking and the atomic key deletion is delegated to Elixir Core.
type UserKeyDeletionHandler struct {
	CoreClient pb.CoreServiceClient
}

// deleteKeysBody is the JSON payload for DELETE /api/v1/admin/users/{userId}/keys.
type deleteKeysBody struct {
	Reason string `json:"reason"`
}

// DeleteUserKeys handles DELETE /api/v1/admin/users/{userId}/keys.
//
// Handler flow (AC1):
//  1. requireJSON — 415 on wrong Content-Type
//  2. Role gate — 403 M_FORBIDDEN if not instance_admin
//  3. Path param userId validation — 400 if empty or > 255 chars
//  4. JSON body decode (DisallowUnknownFields) — 400 M_BAD_JSON on parse error
//  5. reason validation: required (400) + minimum 10 chars (400)
//  6. gRPC CoreClient.DeleteUserKeys — maps gRPC codes to HTTP status
//  7. Audit emission (never-raise, 500ms timeout) — AC1
//  8. 200 with {"user_id":..., "status":"keys_deleted", "keys_deleted_at":<ISO8601>}
func (h *UserKeyDeletionHandler) DeleteUserKeys(w http.ResponseWriter, r *http.Request) {
	// Step 1: Content-Type check
	if !requireJSON(w, r) {
		return
	}

	// Step 2: Role gate — must be instance_admin
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "instance_admin" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Instance admin role required")
		return
	}

	// Step 3: Path param userId validation — defence-in-depth (AC1)
	userID := r.PathValue("userId")
	if userID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId is required")
		return
	}
	if len(userID) > 255 {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId must not exceed 255 characters")
		return
	}

	// Step 4: JSON body decode with strict field enforcement
	var req deleteKeysBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	// Step 5: reason validation
	if req.Reason == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "reason is required")
		return
	}
	if len(req.Reason) < 10 {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "reason must be at least 10 characters")
		return
	}

	// Step 6: gRPC call — Elixir Core handles atomic Ecto.Multi + failure-invariant audit
	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	resp, err := h.CoreClient.DeleteUserKeys(r.Context(), &pb.DeleteUserKeysRequest{
		AdminUserId:  callerSub,
		TargetUserId: userID,
		Reason:       req.Reason,
	})
	if err != nil {
		st, _ := grpcstatus.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "User not found")
		case codes.AlreadyExists:
			writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "User deletion already in progress or completed")
		default:
			writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Step 7: Audit emission — never-raise, 500ms timeout (AC1).
	// Note: Elixir Core already emits "user_keys_deleted" internally for its own audit trail.
	// The Go gateway emits an additional audit record on the gateway layer (analog story 5.3–5.6).
	auditCtx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"user_keys_deleted", "user", userID,
		map[string]any{"reason": req.Reason},
		"success", "")

	// Step 8: Convert keys_deleted_at Unix ms → ISO8601, build 200 response (AC6).
	keysDeletedAt := time.UnixMilli(resp.KeysDeletedAt).UTC().Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id":         userID,
		"status":          resp.Status,
		"keys_deleted_at": keysDeletedAt,
	})
}

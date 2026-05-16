package compliance

// gdpr_delete.go — Story 14.4: GDPR Right to Erasure — Orchestrating Handler
//
// Route: DELETE /api/v1/admin/users/{userId}
// Auth:  jwtWithStatusCheck (existing) + instance_admin role gate + complianceRL rate limit
//
// This handler orchestrates the full GDPR Article 17 deletion flow by chaining
// the already-implemented building blocks:
//
//  1. Role gate — 403 M_FORBIDDEN if not instance_admin
//  2. Path param userId validation — 400 if empty or > 255 chars
//  3. Self-delete guard — 403 M_FORBIDDEN if callerSub == userId (four-eyes required)
//  4. gRPC DeactivateUser — sets is_active=false + destroys all sessions
//     → 404 on NOT_FOUND; 409 if already deactivated (ALREADY_EXISTS)
//  5. gRPC DeleteUserKeys — nulls private_key for signing + encryption keys (best-effort)
//     → logs warn on failure but continues (keys may already be nulled from prior deletion)
//  6. Anonymize profiles + users — sets profiles.displayname='Deleted User', avatar_url=NULL,
//     users.anonymized_at=<now_ms> (identical to AnonymizeUser handler logic)
//  7. Emit "gdpr_deletion" audit event (never-raise, 500ms timeout)
//  8. 200 {"user_id": userId, "status": "gdpr_deleted"}
//
// Error handling:
//   - Step 4 failure → 404 or 409 or 500 (stops pipeline)
//   - Step 5 failure → log warn and continue (best-effort)
//   - Step 6 failure → 500 (PII erasure MUST NOT be silently skipped)
//   - Step 7 failure → never-raise (audit failure does not affect 200 response)

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// GdprDeleteHandler handles DELETE /api/v1/admin/users/{userId}.
// It orchestrates deactivation, key deletion, anonymization, and audit emission.
type GdprDeleteHandler struct {
	DB          *sql.DB
	CoreClient  pb.CoreServiceClient
	StoragePath string
	FileRemover FileRemover
}

// fileRemover returns the configured FileRemover or the production os-based one.
func (h *GdprDeleteHandler) fileRemover() FileRemover {
	if h.FileRemover != nil {
		return h.FileRemover
	}
	return osFileRemover{}
}

// DeleteUser handles DELETE /api/v1/admin/users/{userId}.
func (h *GdprDeleteHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	// Step 1: Role gate — must be instance_admin
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "instance_admin" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Instance admin role required")
		return
	}

	// Step 2: Path param userId validation
	userID := r.PathValue("userId")
	if userID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId is required")
		return
	}
	if len(userID) > 255 {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId must not exceed 255 characters")
		return
	}

	// Step 3: Self-delete guard — four-eyes approval required
	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)
	if userID == callerSub {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "self-deletion requires four-eyes approval from a second admin")
		return
	}

	// Step 4: gRPC DeactivateUser — sets is_active=false and destroys all sessions
	// The deactivation is the primary "stop" signal: once this succeeds, the user
	// can no longer log in or use existing sessions.
	_, err := h.CoreClient.DeactivateUser(r.Context(), &pb.DeactivateUserRequest{
		UserId: userID,
	})
	if err != nil {
		st, _ := grpcstatus.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "User not found")
			return
		case codes.AlreadyExists:
			// Already deactivated — this is acceptable for a GDPR deletion
			// (idempotent operation). Log and continue with the remaining steps.
			slog.Warn("gdpr_delete: user already deactivated, continuing with remaining steps",
				"user_id", userID)
		default:
			slog.Error("gdpr_delete: DeactivateUser gRPC failed", "user_id", userID, "err", err)
			writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return
		}
	}

	// Step 5: gRPC DeleteUserKeys — nulls private signing and encryption keys (best-effort)
	// A failure here is non-fatal: the user is already deactivated (step 4).
	// Keys may also already be null if a prior deletion was partially completed.
	const gdprDeletionReason = "GDPR Article 17 Right to Erasure — automated deletion"
	_, keysErr := h.CoreClient.DeleteUserKeys(r.Context(), &pb.DeleteUserKeysRequest{
		AdminUserId:  callerSub,
		TargetUserId: userID,
		Reason:       gdprDeletionReason,
	})
	if keysErr != nil {
		// Best-effort: log but do not fail the GDPR deletion
		slog.Warn("gdpr_delete: DeleteUserKeys failed (continuing with anonymization)",
			"user_id", userID, "err", keysErr)
	}

	// Step 6: Anonymize user — profiles + users table (PII erasure)
	// This step MUST succeed; if it fails the handler returns 500.
	// Identical logic to user_anonymization.go (AnonymizeUser).
	if err := h.anonymizeUser(r.Context(), w, userID); err != nil {
		// anonymizeUser wrote the error response; just return.
		return
	}

	// Step 7: Emit "gdpr_deletion" audit event — never-raise, 500ms timeout
	// The gdpr_deletion action must be in Compliance.AuditWriter @known_actions.
	// keysDeletedStatus captures whether key material erasure was confirmed or best-effort-failed.
	keysDeletedStatus := "confirmed"
	if keysErr != nil {
		keysDeletedStatus = "failed_best_effort"
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), auditTimeout)
	defer auditCancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"gdpr_deletion", "user", userID,
		map[string]any{"keys_deleted": keysDeletedStatus},
		"success", "")

	// Step 8: 200 OK
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id": userID,
		"status":  "gdpr_deleted",
	})
}

// anonymizeUser performs the PII anonymization steps (profiles + users + optional avatar removal).
// This is the same logic as AnonymizeUser in user_anonymization.go, extracted for reuse.
// On success, returns nil. On failure, writes the error response and returns a non-nil error.
func (h *GdprDeleteHandler) anonymizeUser(ctx context.Context, w http.ResponseWriter, userID string) error {
	// Pre-flight: get current avatar_url from profiles to handle mxc:// cleanup.
	var avatarURL sql.NullString
	err := h.DB.QueryRowContext(ctx,
		`SELECT avatar_url FROM profiles WHERE user_id = $1`,
		userID,
	).Scan(&avatarURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No profile row — user may never have logged in (DB-seeded only).
			// Create a placeholder profile row so the anonymization is recorded.
			// Fall through to the UPDATE which will be a no-op if the row doesn't exist.
			avatarURL = sql.NullString{Valid: false}
		} else {
			slog.Error("gdpr_delete: pre-flight profile lookup failed", "user_id", userID, "err", err)
			writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return err
		}
	}

	// Transaction: UPDATE profiles + UPDATE users (identical to AnonymizeUser).
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("gdpr_delete: BeginTx failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return err
	}

	// UPDATE profiles: displayname='Deleted User', avatar_url=NULL
	_, err = tx.ExecContext(ctx,
		`UPDATE profiles SET displayname = 'Deleted User', avatar_url = NULL WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		_ = tx.Rollback()
		slog.Error("gdpr_delete: UPDATE profiles failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return err
	}

	// UPDATE users: anonymized_at=<now_ms>
	anonymizedAt := time.Now().UnixMilli()
	_, err = tx.ExecContext(ctx,
		`UPDATE users SET anonymized_at = $1 WHERE user_id = $2`,
		anonymizedAt, userID,
	)
	if err != nil {
		_ = tx.Rollback()
		slog.Error("gdpr_delete: UPDATE users.anonymized_at failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return err
	}

	// Soft-delete mxc:// avatar in media_files (inside TX, best-effort).
	currentAvatarURL := ""
	if avatarURL.Valid {
		currentAvatarURL = avatarURL.String
	}
	var mxcServerName, mxcMediaID string
	var hasMxc bool
	if mxcServerName, mxcMediaID, hasMxc = parseMxcURI(currentAvatarURL); hasMxc {
		_, dbErr := tx.ExecContext(ctx,
			`UPDATE media_files SET deleted = true WHERE media_id = $1`,
			mxcMediaID,
		)
		if dbErr != nil {
			slog.Warn("gdpr_delete: UPDATE media_files failed (continuing)",
				"user_id", userID, "media_id", mxcMediaID, "err", dbErr)
		}
	}

	// Commit the transaction.
	if err = tx.Commit(); err != nil {
		_ = tx.Rollback()
		slog.Error("gdpr_delete: TX commit failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return err
	}

	// Remove disk avatar file AFTER commit (best-effort; file error does not roll back DB).
	if hasMxc {
		storagePath := h.StoragePath
		if storagePath == "" {
			storagePath = os.Getenv("NEBU_MEDIA_STORAGE_PATH")
		}
		if storagePath == "" {
			slog.Warn("gdpr_delete: NEBU_MEDIA_STORAGE_PATH not configured — skipping disk file removal",
				"user_id", userID, "media_id", mxcMediaID)
		} else {
			diskPath := filepath.Join(storagePath, mxcServerName, mxcMediaID)
			if removeErr := h.fileRemover().Remove(diskPath); removeErr != nil {
				slog.Warn("gdpr_delete: failed to remove avatar file from disk",
					"user_id", userID, "media_id", mxcMediaID, "path", diskPath, "err", removeErr)
			}
		}
	}

	return nil
}


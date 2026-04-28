package compliance

// user_anonymization.go — Story 5.8: Operational PII Anonymization Handler
//
// Route: POST /api/v1/admin/users/{userId}/anonymize
// Auth: jwtMiddleware (existing) + instance_admin role gate
// Anonymizes a user account by:
//   - Overwriting profiles.displayname → 'Deleted User', avatar_url → NULL
//   - Setting users.anonymized_at → current Unix-ms timestamp
//   - Soft-deleting any mxc:// avatar in media_files (deleted=true) and removing the disk file
//
// Emits "user_anonymized" audit on success (never-raise, 500ms timeout).
// No gRPC round-trip to Elixir Core — this is a pure DB operation.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// FileRemover abstracts os.Remove for testability.
// The default production implementation delegates to os.Remove.
type FileRemover interface {
	Remove(path string) error
}

// osFileRemover is the production FileRemover backed by os.Remove.
type osFileRemover struct{}

func (osFileRemover) Remove(path string) error {
	return os.Remove(path)
}

// AnonymizationHandler handles POST /api/v1/admin/users/{userId}/anonymize.
// DB is the *sql.DB handle for PostgreSQL (complianceDB in main.go).
// CoreClient is the gRPC stub used for audit emission only.
// StoragePath is the NEBU_MEDIA_STORAGE_PATH root for media file disk removal.
// FileRemover abstracts os.Remove for testability (defaults to osFileRemover when nil).
type AnonymizationHandler struct {
	DB          *sql.DB
	CoreClient  pb.CoreServiceClient
	StoragePath string
	FileRemover FileRemover
}

// fileRemover returns the configured FileRemover or the default os-based one.
func (h *AnonymizationHandler) fileRemover() FileRemover {
	if h.FileRemover != nil {
		return h.FileRemover
	}
	return osFileRemover{}
}

// AnonymizeUser handles POST /api/v1/admin/users/{userId}/anonymize.
//
// Handler flow (in order):
//  1. Role gate — 403 M_FORBIDDEN if not instance_admin
//  2. Path param userId validation — 400 if empty or > 255 chars
//  3. Pre-flight SELECT avatar_url FROM profiles WHERE user_id=$1
//     → 404 M_NOT_FOUND if no row (user does not exist)
//  4. UPDATE profiles SET displayname='Deleted User', avatar_url=NULL WHERE user_id=$1
//  5. UPDATE users SET anonymized_at=$1 WHERE user_id=$2 (Unix-ms timestamp)
//  6. If avatar_url is mxc://:
//     a. Parse serverName + mediaID from URI
//     b. UPDATE media_files SET deleted=true WHERE media_id=$1
//     c. Remove disk file at storagePath/serverName/mediaID (log warn on error, continue)
//  7. Audit "user_anonymized" (never-raise, 500ms timeout)
//  8. 200 {"user_id": userId, "status": "anonymized"}
func (h *AnonymizationHandler) AnonymizeUser(w http.ResponseWriter, r *http.Request) {
	// Step 1: Role gate — must be instance_admin
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "instance_admin" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Instance admin role required")
		return
	}

	// Step 2: Path param userId validation — defence-in-depth (AC1)
	userID := r.PathValue("userId")
	if userID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId is required")
		return
	}
	if len(userID) > 255 {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "userId must not exceed 255 characters")
		return
	}

	// Step 3: Pre-flight SELECT avatar_url FROM profiles — user existence + mxc check (AC3)
	var avatarURL sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT avatar_url FROM profiles WHERE user_id = $1`,
		userID,
	).Scan(&avatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "User not found")
		return
	}
	if err != nil {
		slog.Error("anonymize: pre-flight profile lookup failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 4: UPDATE profiles SET displayname='Deleted User', avatar_url=NULL (AC2)
	_, err = h.DB.ExecContext(r.Context(),
		`UPDATE profiles SET displayname = 'Deleted User', avatar_url = NULL WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		slog.Error("anonymize: UPDATE profiles failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 5: UPDATE users SET anonymized_at=<Unix-ms> (AC2)
	anonymizedAt := time.Now().UnixMilli()
	_, err = h.DB.ExecContext(r.Context(),
		`UPDATE users SET anonymized_at = $1 WHERE user_id = $2`,
		anonymizedAt, userID,
	)
	if err != nil {
		slog.Error("anonymize: UPDATE users failed", "user_id", userID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 6: Avatar mxc:// cleanup — soft-delete in DB + remove disk file (AC3)
	currentAvatarURL := ""
	if avatarURL.Valid {
		currentAvatarURL = avatarURL.String
	}
	if serverName, mediaID, ok := parseMxcURI(currentAvatarURL); ok {
		// 6a. Soft-delete in media_files
		_, dbErr := h.DB.ExecContext(r.Context(),
			`UPDATE media_files SET deleted = true WHERE media_id = $1`,
			mediaID,
		)
		if dbErr != nil {
			// Log but do not abort — anonymization is still considered successful
			slog.Warn("anonymize: UPDATE media_files failed",
				"user_id", userID, "media_id", mediaID, "err", dbErr)
		}

		// 6b. Remove disk file. Skip when StoragePath is unset to avoid attempting
		// os.Remove against a path resolved relative to the gateway's cwd
		// (NEBU_MEDIA_STORAGE_PATH may legitimately be unconfigured in
		// dev/test environments where media storage lives elsewhere).
		if h.StoragePath == "" {
			slog.Warn("anonymize: NEBU_MEDIA_STORAGE_PATH not configured — skipping disk file removal",
				"user_id", userID, "media_id", mediaID)
		} else {
			diskPath := filepath.Join(h.StoragePath, serverName, mediaID)
			if removeErr := h.fileRemover().Remove(diskPath); removeErr != nil {
				// AC3: "log error but do NOT abort"
				slog.Warn("anonymize: failed to remove avatar file from disk",
					"user_id", userID, "media_id", mediaID, "path", diskPath, "err", removeErr)
			}
		}
	}

	// Step 7: Audit emission — never-raise, 500ms timeout (AC5)
	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)
	auditCtx, auditCancel := context.WithTimeout(context.Background(), auditTimeout)
	defer auditCancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"user_anonymized", "user", userID,
		map[string]any{},
		"success", "")

	// Step 8: 200 OK response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id": userID,
		"status":  "anonymized",
	})
}

// parseMxcURI parses a Matrix mxc:// URI into its serverName and mediaID components.
// Returns (serverName, mediaID, true) on success, ("", "", false) on any parse failure.
//
// Valid format: mxc://<serverName>/<mediaId>
// Both serverName and mediaId must be non-empty strings AND must NOT contain path
// traversal sequences. Avatar URLs are user-controlled (set via PUT /profile/avatar_url
// which currently only validates the "mxc://" prefix), and the parsed components are
// joined into a filesystem path for os.Remove. Without this guard, a malicious user
// could store an avatar_url like "mxc://../etc/passwd/x" and turn admin-triggered
// anonymization into an arbitrary-file-delete primitive.
//
// Defensive checks:
//   - reject "." or ".." segments outright
//   - reject any "/" inside a segment (only one separator is permitted)
//   - reject NUL bytes and forward/backslash separators inside a segment
func parseMxcURI(uri string) (serverName, mediaID string, ok bool) {
	if !strings.HasPrefix(uri, "mxc://") {
		return "", "", false
	}
	rest := strings.TrimPrefix(uri, "mxc://")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if !isSafePathSegment(parts[0]) || !isSafePathSegment(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// isSafePathSegment returns true if s is safe to use as a single path component
// when joined with filepath.Join. It rejects path traversal markers and any
// embedded path separators or NUL bytes.
func isSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, "/\\\x00") {
		return false
	}
	return true
}


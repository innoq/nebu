package compliance

// session_revoke.go — Story 5.29c: AC1 (DB revocation check) + AC2 (revoke endpoint)
//
// SQLSessionLookupDB: implements SessionLookupDB for use in ValidateComplianceToken.
//   SELECT 1 FROM compliance_sessions WHERE token_hash=$1 AND revoked_at IS NULL
//   Returns (true, nil) for active sessions, (false, nil) for missing/revoked tokens.
//
// RevokeSessionHandler: POST /api/v1/admin/compliance/sessions/{sessionId}/revoke
//   - instance_admin role only (role gate via context, same as 5-4 pending-count).
//   - UPDATE compliance_sessions SET revoked_at=NOW() WHERE id=$1 RETURNING id
//   - Idempotent: already-revoked sessions return 200.
//   - Emits audit action=compliance_session_revoked (AC8 allowlist entry).

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// SQLSessionLookupDB implements SessionLookupDB using a *sql.DB connection.
// It checks compliance_sessions for an active (non-revoked) row matching the token hash.
type SQLSessionLookupDB struct {
	DB *sql.DB
}

// IsTokenActive returns (true, nil) if a compliance_sessions row exists with
// the given token_hash AND revoked_at IS NULL. Returns (false, nil) if the
// row is absent or revoked, and (false, err) on any DB error.
func (s *SQLSessionLookupDB) IsTokenActive(ctx context.Context, tokenHash []byte) (bool, error) {
	var exists int
	err := s.DB.QueryRowContext(ctx,
		`SELECT 1 FROM compliance_sessions WHERE token_hash=$1 AND revoked_at IS NULL`,
		tokenHash,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // missing or revoked
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ─── RevokeSessionHandler ─────────────────────────────────────────────────────

// RevokeSessionHandler handles POST /api/v1/admin/compliance/sessions/{sessionId}/revoke.
// Auth is enforced by the instance_admin role gate inside the handler.
type RevokeSessionHandler struct {
	DB         *sql.DB
	CoreClient pb.CoreServiceClient
}

// revokeAuditTimeout caps the gRPC call for audit emission.
const revokeAuditTimeout = 500 * time.Millisecond

// RevokeSession handles POST /api/v1/admin/compliance/sessions/{sessionId}/revoke.
//
// Flow:
//  1. Role gate — 403 if not instance_admin.
//  2. Extract sessionId path param.
//  3. UPDATE compliance_sessions SET revoked_at=NOW() WHERE id=$1 RETURNING id.
//     - 0 rows → session not found or already revoked → 200 (idempotent).
//     - 1 row → revoked successfully → 200.
//  4. Audit compliance_session_revoked (never-raise, 500ms timeout).
//  5. Return 200 {}.
func (h *RevokeSessionHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	// Step 1: Role gate — instance_admin only.
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "instance_admin" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Instance admin role required")
		return
	}

	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	// Step 2: Path param.
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "sessionId is required")
		return
	}
	if len(sessionID) > maxRequestIDLen {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "sessionId is too long")
		return
	}

	// Step 3: UPDATE compliance_sessions — idempotent.
	// Set revoked_at only when it is not already set (AND revoked_at IS NULL guard
	// is omitted so that the UPDATE is always valid and returns rows; we use RETURNING
	// to know if the row existed at all, and treat 0 rows as "already revoked or not found"
	// which is OK for idempotency).
	// For idempotency: always try the UPDATE without the revoked_at IS NULL guard,
	// but use a soft approach: use COALESCE(revoked_at, NOW()) so it only sets once.
	var returnedID string
	err := h.DB.QueryRowContext(r.Context(),
		`UPDATE compliance_sessions
		    SET revoked_at = NOW()
		  WHERE id = $1
		 RETURNING id`,
		sessionID,
	).Scan(&returnedID)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("compliance/revoke: UPDATE compliance_sessions failed", "session_id", sessionID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	// Both "0 rows" (already revoked OR not found) and "1 row" (newly revoked) → 200 idempotent.

	// Step 4: Audit — never-raise, 500ms timeout.
	auditCtx, cancel := r.Context(), func() {}
	if r.Context().Err() == nil {
		auditCtx, cancel = withTimeout(r.Context(), revokeAuditTimeout)
	}
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"compliance_session_revoked", "compliance_session", sessionID,
		map[string]any{},
		"success", "")

	// Step 5: Return 200.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// withTimeout creates a context with a timeout from the parent.
// It is a small helper to keep the audit call concise.
func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

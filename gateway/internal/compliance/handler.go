package compliance

// handler.go — Story 5.3: Compliance Access Request API
//                Story 5.4: Four-Eyes Approval API + Admin Dashboard Pending Badge
//                Story 5.5: Compliance Session Handler (SessionHandler + PostSession)
//
// Route namespace: /api/v1/compliance/* — separate from /_matrix/client/v3/ (Matrix CS API)
// and /admin/ (admin web UI). Same HTTP port (:8008), distinct path prefix.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	matrixvalidate "github.com/nebu/nebu/internal/matrix"
	"github.com/nebu/nebu/internal/middleware"
)

// auditTimeout caps the gRPC call for audit log emission so a hanging Core does
// not block the compliance request response path (never-raise policy).
const auditTimeout = 500 * time.Millisecond

// AccessRequestHandler handles POST /api/v1/compliance/access-requests.
// DB is the *sql.DB handle for PostgreSQL; CoreClient is the gRPC stub used for audit emission.
type AccessRequestHandler struct {
	DB         *sql.DB
	CoreClient pb.CoreServiceClient
}

// createAccessRequestBody is the JSON payload for POST /api/v1/compliance/access-requests.
type createAccessRequestBody struct {
	RoomID         string `json:"room_id"`
	TimeRangeStart string `json:"time_range_start"`
	TimeRangeEnd   string `json:"time_range_end"`
	Justification  string `json:"justification"`
}

// PostAccessRequest handles POST /api/v1/compliance/access-requests.
//
// Handler flow (in order):
//  1. requireJSON — 415 on wrong Content-Type
//  2. Role gate — 403 M_FORBIDDEN if not compliance_officer
//  3. JSON decode with DisallowUnknownFields — 400 M_BAD_JSON on parse error
//  4. Field validation (room_id, timestamps, justification)
//  5. Room existence check — 404 M_NOT_FOUND
//  6. DB INSERT ... RETURNING id
//  7. Audit emission (never-raise, 500ms timeout)
//  8. 201 Created with {"request_id":"<uuid>","status":"pending"}
func (h *AccessRequestHandler) PostAccessRequest(w http.ResponseWriter, r *http.Request) {
	// Step 1: Content-Type check
	if !requireJSON(w, r) {
		return
	}

	// Step 2: Role gate — must be compliance_officer
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "compliance_officer" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
		return
	}

	// Step 3: JSON decode with strict field enforcement
	var req createAccessRequestBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	// Step 4: Field validation (in AC order)

	// room_id: required + valid Matrix room ID format
	if req.RoomID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "room_id is required")
		return
	}
	if err := matrixvalidate.ValidateMatrixRoomID(req.RoomID); err != nil {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "room_id is not a valid Matrix room ID")
		return
	}

	// time_range_start: required + RFC 3339
	if req.TimeRangeStart == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_start is required")
		return
	}
	start, err := time.Parse(time.RFC3339, req.TimeRangeStart)
	if err != nil {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_start is not a valid ISO 8601 timestamp")
		return
	}

	// time_range_end: required + RFC 3339
	if req.TimeRangeEnd == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_end is required")
		return
	}
	end, err := time.Parse(time.RFC3339, req.TimeRangeEnd)
	if err != nil {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_end is not a valid ISO 8601 timestamp")
		return
	}

	// time_range_end must be strictly after time_range_start
	if !end.After(start) {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_end must be after time_range_start")
		return
	}

	// justification: required + minimum 20 characters
	if req.Justification == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "justification is required")
		return
	}
	if len(req.Justification) < 20 {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "justification must be at least 20 characters")
		return
	}

	// Step 5: Room existence check (rooms table, room_id TEXT PRIMARY KEY — migration 000009)
	var exists int
	err = h.DB.QueryRowContext(r.Context(), `SELECT 1 FROM rooms WHERE room_id = $1`, req.RoomID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		return
	}
	if err != nil {
		slog.Error("compliance: room existence check failed", "room_id", req.RoomID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 6: DB INSERT — id, status, created_at are DB-generated defaults
	requesterSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	var requestID string
	err = h.DB.QueryRowContext(r.Context(),
		`INSERT INTO compliance_requests
		   (requester_user_id, room_id, time_range_start, time_range_end, justification)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		requesterSub, req.RoomID, start, end, req.Justification,
	).Scan(&requestID)
	if err != nil {
		slog.Error("compliance: insert compliance_requests failed", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 7: Audit log emission — never-raise, 500ms timeout so a hanging Core
	// does not block the compliance request response (Story 5.3, AC5).
	auditCtx, cancel := context.WithTimeout(r.Context(), auditTimeout)
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, requesterSub,
		"compliance_access_requested", "room", req.RoomID,
		map[string]any{"justification_length": len(req.Justification)},
		"success", "")

	// Step 8: 201 Created
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"request_id": requestID,
		"status":     "pending",
	})
}

// ─── Story 5.4: GET Access-Requests list (AC1) ───────────────────────────────

// accessRequestItem is the JSON shape of one pending access request returned by GetAccessRequests.
type accessRequestItem struct {
	RequestID       string `json:"request_id"`
	RequesterUserID string `json:"requester_user_id"`
	RoomID          string `json:"room_id"`
	TimeRangeStart  string `json:"time_range_start"`
	TimeRangeEnd    string `json:"time_range_end"`
	Justification   string `json:"justification"`
	CreatedAt       string `json:"created_at"`
}

// GetAccessRequests handles GET /api/v1/compliance/access-requests?status=pending.
//
// Role gate: compliance_officer only.
// Returns rows where status='pending' AND requester_user_id != callerSub (self-exclusion at DB level).
// MVP: no pagination (documented risk).
func (h *AccessRequestHandler) GetAccessRequests(w http.ResponseWriter, r *http.Request) {
	// Role gate
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "compliance_officer" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
		return
	}

	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, requester_user_id, room_id,
		        time_range_start, time_range_end, justification, created_at
		   FROM compliance_requests
		  WHERE status = 'pending'
		    AND requester_user_id != $1
		  ORDER BY created_at DESC`,
		callerSub,
	)
	if err != nil {
		slog.Error("compliance: list access-requests query failed", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	defer rows.Close()

	items := make([]accessRequestItem, 0)
	for rows.Next() {
		var item accessRequestItem
		var trs, tre, cat interface{}
		if err := rows.Scan(
			&item.RequestID,
			&item.RequesterUserID,
			&item.RoomID,
			&trs, &tre,
			&item.Justification,
			&cat,
		); err != nil {
			slog.Error("compliance: scan access-request row failed", "err", err)
			writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return
		}
		// Normalise time fields: real DB returns time.Time; mock returns string.
		item.TimeRangeStart = formatTimeField(trs)
		item.TimeRangeEnd = formatTimeField(tre)
		item.CreatedAt = formatTimeField(cat)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("compliance: rows.Err on access-requests list", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": items,
		"meta": map[string]int{"total": len(items)},
	})
}

// formatTimeField converts a DB scan value (time.Time or string) to RFC 3339 string.
func formatTimeField(v interface{}) string {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

// ─── Story 5.4: POST Approve / Reject (AC2, AC3) ─────────────────────────────

// approveRejectBody is the optional JSON body for approve/reject.
type approveRejectBody struct {
	Note string `json:"note"`
}

// PostApprove handles POST /api/v1/compliance/access-requests/{requestId}/approve.
func (h *AccessRequestHandler) PostApprove(w http.ResponseWriter, r *http.Request) {
	h.postDecision(w, r, "approved", "compliance_access_approved")
}

// PostReject handles POST /api/v1/compliance/access-requests/{requestId}/reject.
func (h *AccessRequestHandler) PostReject(w http.ResponseWriter, r *http.Request) {
	h.postDecision(w, r, "rejected", "compliance_access_rejected")
}

// maxRequestIDLen caps the path-param length defensively. UUIDs are 36 chars;
// 256 leaves head-room for any future identifier scheme without enabling
// pathological inputs (Story 5.4 review).
const maxRequestIDLen = 256

// postDecision is the shared implementation for approve and reject.
// newStatus is "approved" or "rejected"; auditAction is the audit event action name.
func (h *AccessRequestHandler) postDecision(
	w http.ResponseWriter, r *http.Request,
	newStatus, auditAction string,
) {
	// Step 1: Content-Type check (AC2/AC3 — requireJSON gate, 415 on mismatch).
	if !requireJSON(w, r) {
		return
	}

	// Step 2: Role gate
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "compliance_officer" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
		return
	}

	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	// Step 3: Path param + length cap (defence-in-depth against pathological inputs)
	requestID := r.PathValue("requestId")
	if requestID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "requestId is required")
		return
	}
	if len(requestID) > maxRequestIDLen {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "requestId is too long")
		return
	}

	// Step 4: Optional JSON body — note field is optional.
	// requireJSON guarantees Content-Type is application/json; body itself may
	// be empty ({} or zero bytes). DisallowUnknownFields enforces strict shape.
	var body approveRejectBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		// Allow truly empty body (io.EOF) — note field is optional
		if !errors.Is(err, io.EOF) {
			writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
			return
		}
	}
	note := body.Note

	// Pre-flight: fetch requester_user_id to enforce self-approval/reject guard.
	var requesterUserID string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT requester_user_id FROM compliance_requests WHERE id = $1`,
		requestID,
	).Scan(&requesterUserID)
	if errors.Is(err, sql.ErrNoRows) {
		writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Request not found")
		return
	}
	if err != nil {
		slog.Error("compliance: pre-flight requester check failed", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Self-approval / self-rejection guard
	if requesterUserID == callerSub {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Self-approval is not permitted")
		return
	}

	// Atomic status transition: UPDATE WHERE status='pending' RETURNING id
	var returnedID string
	err = h.DB.QueryRowContext(r.Context(),
		`UPDATE compliance_requests
		    SET status = $3, approver_user_id = $2, approved_at = NOW()
		  WHERE id = $1 AND status = 'pending'
		 RETURNING id`,
		requestID, callerSub, newStatus,
	).Scan(&returnedID)
	if errors.Is(err, sql.ErrNoRows) {
		writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "Request is not pending")
		return
	}
	if err != nil {
		slog.Error("compliance: update compliance_requests failed", "new_status", newStatus, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Audit — never-raise, 500ms timeout
	auditCtx, cancel := context.WithTimeout(r.Context(), auditTimeout)
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		auditAction, "compliance_request", requestID,
		map[string]any{"note": note},
		"success", "")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"request_id": returnedID,
		"status":     newStatus,
	})
}

// ─── Story 5.4: PendingCountHandler (AC4) ────────────────────────────────────

// PendingCountHandler handles GET /admin/api/compliance/pending-count.
// Auth is enforced externally by the sessionGuard middleware in main.go.
type PendingCountHandler struct {
	DB *sql.DB
}

// Handler returns the number of pending compliance access requests.
// Route: GET /admin/api/compliance/pending-count
func (h *PendingCountHandler) Handler(w http.ResponseWriter, r *http.Request) {
	var count int
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM compliance_requests WHERE status = 'pending'`,
	).Scan(&count)
	if err != nil {
		slog.Error("compliance: pending-count query failed", "err", err)
		// Return 0 on error rather than failing — non-blocking by design
		count = 0
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]int{"pending_count": count})
}

// writeComplianceError writes a JSON error response compatible with the Matrix error format.
func writeComplianceError(w http.ResponseWriter, status int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"errcode": errcode, "error": message})
}

// requireJSON checks that the request Content-Type is application/json.
// Returns true when the check passes. On mismatch writes 415 M_UNSUPPORTED_MEDIA_TYPE
// and returns false — the caller must return immediately.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		return true
	}
	writeComplianceError(w, http.StatusUnsupportedMediaType, "M_UNSUPPORTED_MEDIA_TYPE",
		"Content-Type must be application/json")
	return false
}

// ─── Story 5.5: SessionHandler ────────────────────────────────────────────────

// SessionHandler handles POST /api/v1/compliance/access-requests/{requestId}/session.
// It issues a 24-hour Ed25519-signed JWT for an approved compliance access request.
//
// SigningKey and PublicKey are loaded at gateway startup from server_config
// (compliance_signing_key_priv / compliance_signing_key_pub) — persisted Ed25519
// keypair separate from the ephemeral :nebu_signing_key in Elixir core.
type SessionHandler struct {
	DB         *sql.DB
	CoreClient pb.CoreServiceClient
	SigningKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// PostSession handles POST /api/v1/compliance/access-requests/{requestId}/session.
//
// Handler flow (in order):
//  1. Role gate — 403 if not compliance_officer.
//  2. Path-param length cap — 400 if requestId > maxRequestIDLen bytes.
//  3. Pre-flight SELECT requester_user_id, status, room_id, time_range_start, time_range_end
//     FROM compliance_requests WHERE id = $1.
//     → 404 M_NOT_FOUND on 0 rows.
//     → 403 M_FORBIDDEN if status != 'approved'.
//     → 403 M_FORBIDDEN if requester_user_id != callerSub.
//  4. Duplicate session check: SELECT 1 FROM compliance_sessions WHERE request_id=$1 AND revoked_at IS NULL.
//     → 409 M_CONFLICT if row found.
//  5. Issue EdDSA JWT via IssueComplianceToken (exp = now+86400, sub = callerSub).
//  6. INSERT INTO compliance_sessions (request_id, token_hash, expires_at) RETURNING id, expires_at.
//  7. Audit compliance_session_issued (never-raise, 500ms timeout).
//  8. Return 201 {"session_token": "<jwt>", "expires_at": "<RFC3339>"}.
func (h *SessionHandler) PostSession(w http.ResponseWriter, r *http.Request) {
	// Step 1: Role gate — must be compliance_officer.
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "compliance_officer" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
		return
	}

	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	// Step 2: Path-param + length cap (defence-in-depth).
	requestID := r.PathValue("requestId")
	if requestID == "" {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "requestId is required")
		return
	}
	if len(requestID) > maxRequestIDLen {
		writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "requestId is too long")
		return
	}

	// Step 3: Pre-flight SELECT — fetch all required fields in one query.
	// Scanning 5 columns: requester_user_id, status, room_id, time_range_start, time_range_end.
	// A DB error here returns 500 M_UNKNOWN rather than silently producing a JWT with empty claims.
	var requesterUserID, status, roomID, timeRangeStart, timeRangeEnd string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT requester_user_id, status, room_id, time_range_start::text, time_range_end::text
		   FROM compliance_requests WHERE id = $1`,
		requestID,
	).Scan(&requesterUserID, &status, &roomID, &timeRangeStart, &timeRangeEnd)
	if errors.Is(err, sql.ErrNoRows) {
		writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Request not found")
		return
	}
	if err != nil {
		slog.Error("compliance/session: pre-flight query failed", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Status check before identity check (order per AC3/AC2 spec).
	if status != "approved" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Request must be in approved status")
		return
	}

	// Caller must be the original requester (no delegated issuance).
	if requesterUserID != callerSub {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Only the original requester can issue a session")
		return
	}

	// Step 4: Duplicate active session check.
	var exists int
	dupErr := h.DB.QueryRowContext(r.Context(),
		`SELECT 1 FROM compliance_sessions WHERE request_id = $1 AND revoked_at IS NULL`,
		requestID,
	).Scan(&exists)
	if dupErr == nil { // row found → conflict
		writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "An active session already exists for this request")
		return
	}
	if !errors.Is(dupErr, sql.ErrNoRows) {
		slog.Error("compliance/session: duplicate session check failed", "err", dupErr)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 5: Build claims and issue JWT.
	now := time.Now()
	claims := ComplianceClaims{
		Sub:                 callerSub,
		ComplianceRequestID: requestID,
		RoomID:              roomID,
		TimeRangeStart:      timeRangeStart,
		TimeRangeEnd:        timeRangeEnd,
		Iat:                 now.Unix(),
		Exp:                 now.Add(86400 * time.Second).Unix(),
		Iss:                 JWTIssuer,   // AC4: must be set explicitly (Story 5.29c)
		Aud:                 JWTAudience, // AC4: must be set explicitly (Story 5.29c)
	}

	tokenStr, err := IssueComplianceToken(h.SigningKey, claims)
	if err != nil {
		slog.Error("compliance/session: IssueComplianceToken failed", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 6: Compute SHA-256 token hash and INSERT.
	hash := sha256.Sum256([]byte(tokenStr))

	var sessionID string
	var expiresAt interface{}
	err = h.DB.QueryRowContext(r.Context(),
		`INSERT INTO compliance_sessions (request_id, token_hash, expires_at)
		 VALUES ($1, $2, NOW() + INTERVAL '86400 seconds')
		 RETURNING id, expires_at`,
		requestID, hash[:],
	).Scan(&sessionID, &expiresAt)
	if err != nil {
		// Unique constraint violation → duplicate session (belt-and-suspenders with step 4).
		slog.Error("compliance/session: INSERT compliance_sessions failed", "err", err)
		writeComplianceError(w, http.StatusConflict, "M_CONFLICT", "An active session already exists for this request")
		return
	}

	// Normalise expiresAt to RFC 3339 string.
	expiresAtStr := formatTimeField(expiresAt)

	// Step 7: Audit — never-raise, 500ms timeout.
	auditCtx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"compliance_session_issued", "compliance_request", requestID,
		map[string]any{"expires_at": expiresAtStr},
		"success", "")

	// Step 8: Return 201.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"session_token": tokenStr,
		"expires_at":    expiresAtStr,
	})
}

// ─── Story 5.6: ExportHandler ────────────────────────────────────────────────

// ExportHandler handles GET /api/v1/compliance/export.
// DB is the complianceDB handle; CoreClient for audit; SigningKey/PublicKey for export doc signing.
//
// Room-ID scope comes exclusively from the validated X-Compliance-Token claims — there is
// no URL room_id parameter. A tampered token (modified room_id claim) fails
// ValidateComplianceToken signature check → 401 before any room_id is used.
// Therefore the AC10 room_id-scope guard is implicitly satisfied by AC3 (Story 5.6 scope decision).
//
// Export document is signed over struct-marshalled JSON (deterministic field order via map).
// Full Matrix Canonical JSON for export documents is deferred per Story 5.6 scope decision.
type ExportHandler struct {
	DB         *sql.DB
	CoreClient pb.CoreServiceClient
	SigningKey  ed25519.PrivateKey
	PublicKey   ed25519.PublicKey
}

// generateExportUUID returns a random UUID v4 string using crypto/rand.
func generateExportUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// exportEvent represents one event entry in the compliance export.
type exportEvent struct {
	EventID        string          `json:"event_id"`
	RoomID         string          `json:"room_id"`
	Sender         string          `json:"sender"`
	Type           string          `json:"type"`
	Content        json.RawMessage `json:"content"`
	OriginServerTS int64           `json:"origin_server_ts"`
	Signatures     json.RawMessage `json:"signatures,omitempty"`
}

// GetExport handles GET /api/v1/compliance/export.
//
// Handler flow (in order):
//  1. Role gate — 403 M_FORBIDDEN if not compliance_officer.
//  2. Extract X-Compliance-Token header — 401 M_UNKNOWN_TOKEN if absent.
//  3. Validate token via ValidateComplianceToken — 401 M_UNKNOWN_TOKEN on any failure.
//  4. Parse time range from claims (RFC 3339 → epoch milliseconds).
//  5. Pre-flight SELECT requester_user_id, approver_user_id FROM compliance_requests.
//     → 500 M_UNKNOWN on 0 rows (token valid but request deleted — data integrity issue) or DB error.
//  6. Fetch m.room.message events (strict scope: room_id + event_type + origin_server_ts BETWEEN).
//  7. Build export document map (alphabetically keyed for deterministic signing).
//  8. Sign document bytes with Ed25519 private key → base64. Inject server_signature.
//  9. Set Content-Disposition header. Emit audit (never-raise, 500ms timeout). Return 200.
func (h *ExportHandler) GetExport(w http.ResponseWriter, r *http.Request) {
	// Step 1: Role gate — must be compliance_officer.
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	if systemRole != "compliance_officer" {
		writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
		return
	}
	callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

	// Step 2: Extract X-Compliance-Token header.
	tokenStr := r.Header.Get("X-Compliance-Token")
	if tokenStr == "" {
		writeComplianceError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Compliance token required")
		return
	}

	// Step 3: Validate token (reuse ValidateComplianceToken from jwt.go).
	// Pass h.DB as the SessionLookupDB — it implements IsTokenActive via SQLSessionLookupDB (AC1, Story 5.29c).
	sessionDB := &SQLSessionLookupDB{DB: h.DB}
	claims, err := ValidateComplianceToken(r.Context(), tokenStr, h.PublicKey, callerSub, sessionDB)
	if err != nil {
		writeComplianceError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid or expired compliance token")
		return
	}

	// Step 4: Parse time range from claims (RFC 3339 → epoch milliseconds).
	// A malformed claim must NOT silently degrade to a zero-time export — that would
	// produce a 200 with empty events and no audit trail. Treat as 500 M_UNKNOWN
	// (token validated structurally but its claims are corrupt) and log loudly.
	startTime, err := time.Parse(time.RFC3339, claims.TimeRangeStart)
	if err != nil {
		slog.Error("compliance/export: malformed time_range_start claim",
			"request_id", claims.ComplianceRequestID, "value", claims.TimeRangeStart, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	endTime, err := time.Parse(time.RFC3339, claims.TimeRangeEnd)
	if err != nil {
		slog.Error("compliance/export: malformed time_range_end claim",
			"request_id", claims.ComplianceRequestID, "value", claims.TimeRangeEnd, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	startMs := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	// Step 5: Pre-flight — fetch requester + approver from compliance_requests.
	// approver_user_id is nullable (JSONB without NOT NULL) — use sql.NullString.
	var requesterUserID string
	var approverNull sql.NullString
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT requester_user_id, approver_user_id FROM compliance_requests WHERE id = $1`,
		claims.ComplianceRequestID,
	).Scan(&requesterUserID, &approverNull)
	if err != nil {
		// sql.ErrNoRows: token was valid but request deleted (data integrity issue).
		// Any DB error: 500.
		slog.Error("compliance/export: pre-flight compliance_requests query failed",
			"request_id", claims.ComplianceRequestID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	approverUserID := ""
	if approverNull.Valid {
		approverUserID = approverNull.String
	}

	// Step 6: Fetch m.room.message events (strict scope: all scope from token claims).
	// origin_server_ts is BIGINT (epoch ms) — convert RFC 3339 claims to ms above.
	//
	// LIMIT 10000 is a MVP DoS-guard sane default: a single export request loads all
	// events into memory ([]exportEvent) and re-marshals them; without a cap a wide
	// time range over a busy room could produce >>100 MB responses and OOM the gateway.
	// Streaming/chunked export is tracked as FB-56-01 (Story 5.29 follow-up).
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT event_id, room_id, sender, event_type, content, origin_server_ts, signatures
		   FROM events
		  WHERE room_id = $1
		    AND event_type = 'm.room.message'
		    AND origin_server_ts BETWEEN $2 AND $3
		  ORDER BY origin_server_ts ASC
		  LIMIT 10000`,
		claims.RoomID, startMs, endMs,
	)
	if err != nil {
		slog.Error("compliance/export: events query failed", "room_id", claims.RoomID, "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	defer rows.Close()

	events := make([]exportEvent, 0)
	for rows.Next() {
		var ev exportEvent
		var signaturesRaw []byte
		var contentRaw []byte
		if err := rows.Scan(
			&ev.EventID,
			&ev.RoomID,
			&ev.Sender,
			&ev.Type,
			&contentRaw,
			&ev.OriginServerTS,
			&signaturesRaw,
		); err != nil {
			slog.Error("compliance/export: scan event row failed", "err", err)
			writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return
		}
		ev.Content = json.RawMessage(contentRaw)
		if signaturesRaw != nil {
			ev.Signatures = json.RawMessage(signaturesRaw)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		slog.Error("compliance/export: rows.Err on events", "err", err)
		writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Step 7: Build export document as a map (Go map marshaling is alphabetically ordered —
	// this ensures deterministic serialization for signing and matches test reconstruction).
	// The signed bytes must be the same as: unmarshal response → delete server_signature → remarshal.
	eventsJSON := make([]json.RawMessage, len(events))
	for i, ev := range events {
		b, _ := json.Marshal(ev)
		eventsJSON[i] = json.RawMessage(b)
	}

	exportID := generateExportUUID()
	generatedAt := time.Now().UTC().Format(time.RFC3339)

	// Build the document map for signing (no server_signature yet).
	// json.Marshal on a map produces alphabetically sorted keys.
	docMap := map[string]json.RawMessage{}
	approverJSON, _ := json.Marshal(approverUserID)
	docMap["approver"] = approverJSON
	compReqIDJSON, _ := json.Marshal(claims.ComplianceRequestID)
	docMap["compliance_request_id"] = compReqIDJSON
	eventsArrJSON, _ := json.Marshal(eventsJSON)
	docMap["events"] = eventsArrJSON
	exportIDJSON, _ := json.Marshal(exportID)
	docMap["export_id"] = exportIDJSON
	generatedAtJSON, _ := json.Marshal(generatedAt)
	docMap["generated_at"] = generatedAtJSON
	requesterJSON, _ := json.Marshal(requesterUserID)
	docMap["requester"] = requesterJSON
	roomIDJSON, _ := json.Marshal(claims.RoomID)
	docMap["room_id"] = roomIDJSON
	timeEndJSON, _ := json.Marshal(claims.TimeRangeEnd)
	docMap["time_range_end"] = timeEndJSON
	timeStartJSON, _ := json.Marshal(claims.TimeRangeStart)
	docMap["time_range_start"] = timeStartJSON

	// Step 8: Marshal doc WITHOUT server_signature → sign → base64-encode.
	// Export document is signed over map-marshalled JSON (alphabetically sorted keys).
	// Full Matrix Canonical JSON for export documents is deferred per Story 5.6 scope decision.
	docBytes, _ := json.Marshal(docMap)
	sig := ed25519.Sign(h.SigningKey, docBytes)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Inject server_signature into the map and marshal the full response.
	sigB64JSON, _ := json.Marshal(sigB64)
	docMap["server_signature"] = sigB64JSON
	responseBytes, _ := json.Marshal(docMap)

	// Step 9: Set response headers, emit audit (never-raise), write 200.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="compliance-export-%s.json"`, claims.ComplianceRequestID))

	// Audit — never-raise, 500ms timeout (AC8, AC9).
	auditCtx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
		"compliance_export_downloaded", "compliance_request", claims.ComplianceRequestID,
		map[string]any{"event_count": len(events)},
		"success", "")

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBytes)
}

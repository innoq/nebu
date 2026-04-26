package compliance

// handler.go — Story 5.3: Compliance Access Request API
//
// POST /api/v1/compliance/access-requests
//
// Route namespace: /api/v1/compliance/* — separate from /_matrix/client/v3/ (Matrix CS API)
// and /admin/ (admin web UI). Same HTTP port (:8008), distinct path prefix.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

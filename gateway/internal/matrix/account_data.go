package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nebu/nebu/internal/middleware"
)

// ErrAccountDataNotFound is returned by AccountDataDB.GetAccountData when no row exists.
var ErrAccountDataNotFound = errors.New("account data not found")

// AccountDataDB is the consumer-defined interface for reading and writing account data.
// Defined here (by the consumer/handler) per Go interface convention (ADR-009).
// roomID is empty string ("") for global account data.
type AccountDataDB interface {
	// GetAccountData retrieves account data for the given (userID, roomID, eventType) triple.
	// roomID is empty for global account data.
	// Returns ErrAccountDataNotFound when no row exists.
	GetAccountData(ctx context.Context, userID, roomID, eventType string) (json.RawMessage, error)
	// PutAccountData upserts account data for the (userID, roomID, eventType) triple.
	// roomID is empty for global account data.
	PutAccountData(ctx context.Context, userID, roomID, eventType string, content json.RawMessage) error
}

// GlobalAccountDataRow represents a single global account data event (room_id = '').
// Used as the element type for GlobalAccountDataDB.ListGlobalAccountData results.
type GlobalAccountDataRow struct {
	EventType string
	Content   json.RawMessage
}

// GlobalAccountDataDB is the consumer-defined interface for listing all global
// account data for a user. Defined separately from AccountDataDB to keep
// interfaces minimal (ADR-009 / Go interface convention).
type GlobalAccountDataDB interface {
	// ListGlobalAccountData returns all global account data rows (room_id = '')
	// for the given userID. Returns an empty slice (not nil) when no rows exist.
	ListGlobalAccountData(ctx context.Context, userID string) ([]GlobalAccountDataRow, error)
}

// AccountDataHandler handles the four Matrix account data endpoints:
//
//	GET  /_matrix/client/v3/user/{userId}/account_data/{type}
//	PUT  /_matrix/client/v3/user/{userId}/account_data/{type}
//	GET  /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}
//	PUT  /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}
type AccountDataHandler struct {
	serverName string
	db         AccountDataDB
}

// AccountDataConfig holds dependencies for NewAccountDataHandler.
type AccountDataConfig struct {
	ServerName string
	DB         AccountDataDB
}

// NewAccountDataHandler constructs an AccountDataHandler from the provided config.
func NewAccountDataHandler(cfg AccountDataConfig) *AccountDataHandler {
	return &AccountDataHandler{
		serverName: cfg.ServerName,
		db:         cfg.DB,
	}
}

// authedUserID builds the Matrix user ID (@sub:serverName) from the JWT context.
func (h *AccountDataHandler) authedUserID(r *http.Request) string {
	sub, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	return sub
}

// checkOwnership validates that the userId path param matches the authenticated user.
// Returns false and writes 403 M_FORBIDDEN if the check fails.
func (h *AccountDataHandler) checkOwnership(w http.ResponseWriter, r *http.Request) bool {
	pathUserID := r.PathValue("userId")
	authed := h.authedUserID(r)
	if pathUserID != authed {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN",
			fmt.Sprintf("You can only access account data for your own user ID (got %q, expected %q)", pathUserID, authed))
		return false
	}
	return true
}

// GetGlobalAccountData handles GET /_matrix/client/v3/user/{userId}/account_data/{type}.
//
// Flow:
//  1. Check userId path param == authenticated user (AC3).
//  2. Call db.GetAccountData(ctx, userId, "", eventType).
//  3. If not found → 404 M_NOT_FOUND.
//  4. Return 200 with the stored JSON content.
func (h *AccountDataHandler) GetGlobalAccountData(w http.ResponseWriter, r *http.Request) {
	if !h.checkOwnership(w, r) {
		return
	}
	userID := r.PathValue("userId")
	eventType := r.PathValue("type")

	content, err := h.db.GetAccountData(r.Context(), userID, "", eventType)
	if err != nil {
		if errors.Is(err, ErrAccountDataNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Account data not found")
			return
		}
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(content)
	_, _ = w.Write([]byte("\n"))
}

// PutGlobalAccountData handles PUT /_matrix/client/v3/user/{userId}/account_data/{type}.
//
// Flow:
//  1. Check userId path param == authenticated user (AC3).
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input.
//  3. Upsert via db.PutAccountData (INSERT … ON CONFLICT DO UPDATE).
//  4. Return 200 {}.
func (h *AccountDataHandler) PutGlobalAccountData(w http.ResponseWriter, r *http.Request) {
	if !h.checkOwnership(w, r) {
		return
	}
	userID := r.PathValue("userId")
	eventType := r.PathValue("type")

	var content json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	if err := h.db.PutAccountData(r.Context(), userID, "", eventType, content); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

// GetRoomAccountData handles GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}.
//
// Flow:
//  1. Check userId path param == authenticated user (AC3).
//  2. Call db.GetAccountData(ctx, userId, roomId, eventType).
//  3. If not found → 404 M_NOT_FOUND.
//  4. Return 200 with the stored JSON content.
func (h *AccountDataHandler) GetRoomAccountData(w http.ResponseWriter, r *http.Request) {
	if !h.checkOwnership(w, r) {
		return
	}
	userID := r.PathValue("userId")
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("type")

	content, err := h.db.GetAccountData(r.Context(), userID, roomID, eventType)
	if err != nil {
		if errors.Is(err, ErrAccountDataNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room account data not found")
			return
		}
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(content)
	_, _ = w.Write([]byte("\n"))
}

// PutRoomAccountData handles PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}.
//
// Flow:
//  1. Check userId path param == authenticated user (AC3).
//  2. Decode JSON body — 400 M_BAD_JSON on malformed input.
//  3. Upsert via db.PutAccountData (INSERT … ON CONFLICT DO UPDATE, AC6 last write wins).
//  4. Return 200 {}.
func (h *AccountDataHandler) PutRoomAccountData(w http.ResponseWriter, r *http.Request) {
	if !h.checkOwnership(w, r) {
		return
	}
	userID := r.PathValue("userId")
	roomID := r.PathValue("roomId")
	eventType := r.PathValue("type")

	var content json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	if err := h.db.PutAccountData(r.Context(), userID, roomID, eventType, content); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

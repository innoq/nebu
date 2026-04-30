package matrix

// ─── Story 7-29: Notifications API — GET /_matrix/client/v3/notifications ──
//
// Implements the Matrix notification history endpoint:
//
//	GET /_matrix/client/v3/notifications
//
// Query params:
//   - from:  opaque cursor (base64url-encoded notification row id); omit for newest page.
//   - limit: max items per page; default 50, max 200; values > 200 → 400 M_INVALID_PARAM.
//   - only:  if "highlight", filter to notifications whose actions include "highlight".
//
// Response: {"next_token": "...", "notifications": [...]}
//
// Pagination: cursor wraps the BIGSERIAL notification id (newest-first, id DESC).
// next_token is absent (or empty string) when no further pages exist.
//
// Storage: reads directly from the notifications table (migration 000031).
// The Event Dispatcher (Elixir) inserts rows; the gateway reads them here.
//
// JWT required — jwtMiddleware enforces this before the handler is reached.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/nebu/nebu/internal/middleware"
)

const (
	// defaultNotificationsLimit is the default page size for GET /notifications (AC4).
	defaultNotificationsLimit = 50
	// maxNotificationsLimit is the maximum page size; values above this yield 400 (AC4).
	maxNotificationsLimit = 200
)

// NotificationItem represents a single notification in the Matrix response.
// See Matrix spec CS API §14.8.
type NotificationItem struct {
	// Actions is the push rule actions array, e.g. ["notify"] or ["notify","highlight"].
	Actions []json.RawMessage `json:"actions"`
	// Event is the full event JSON that triggered the notification.
	Event json.RawMessage `json:"event"`
	// ProfileTag is the profile tag of the push rule that triggered this notification.
	// Empty string when no tag matches (most common case in MVP).
	ProfileTag string `json:"profile_tag"`
	// Read indicates whether the notification has been read.
	Read bool `json:"read"`
	// RoomID is the Matrix room ID where the event was sent.
	RoomID string `json:"room_id"`
	// TS is the Unix millisecond timestamp of the event.
	TS int64 `json:"ts"`
}

// NotificationsDB is the consumer-defined interface for reading notification rows.
// Defined here (by the consumer/handler) per Go interface convention (ADR-009).
type NotificationsDB interface {
	// GetNotifications returns a page of notification rows for the given user.
	//
	//   userID:       Matrix user ID ("@sub:server").
	//   fromID:       if > 0, return rows with id < fromID (cursor-based, newest-first).
	//                 if 0, start from the most recent row.
	//   limit:        max rows to return (1–200).
	//   onlyHighlight: when true, filter to rows whose actions JSON contains "highlight".
	//
	// Returns the rows in descending id order (newest-first).
	// nextID is the id of the last row returned (used to build next_token); 0 when no more pages.
	GetNotifications(ctx context.Context, userID string, fromID int64, limit int, onlyHighlight bool) ([]NotificationRow, int64, error)
}

// NotificationRow is a raw row from the notifications table.
type NotificationRow struct {
	ID         int64
	RoomID     string
	ActionsRaw json.RawMessage // stored as JSONB — e.g. ["notify"] or ["notify","highlight"]
	EventRaw   json.RawMessage // full Matrix event JSON
	Read       bool
	TS         int64 // Unix milliseconds (created_at converted to ms)
}

// NotificationsHandler handles GET /_matrix/client/v3/notifications.
type NotificationsHandler struct {
	db NotificationsDB
}

// NotificationsConfig holds dependencies for NewNotificationsHandler.
type NotificationsConfig struct {
	DB NotificationsDB
}

// NewNotificationsHandler constructs a NotificationsHandler from the provided config.
func NewNotificationsHandler(cfg NotificationsConfig) *NotificationsHandler {
	return &NotificationsHandler{db: cfg.DB}
}

// GetNotifications handles GET /_matrix/client/v3/notifications.
//
// Flow:
//  1. Extract authenticated user_id from JWT context.
//  2. Parse and validate query params: from, limit, only.
//  3. Call db.GetNotifications with cursor + limit + highlight filter.
//  4. Shape rows into Matrix NotificationItem objects.
//  5. Encode next_token from the last row id (base64url).
//  6. Return 200 {"next_token":"...","notifications":[...]}.
func (h *NotificationsHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	// ── Parse query params ───────────────────────────────────────────────────────

	q := r.URL.Query()

	// limit: default 50, max 200.
	limit := defaultNotificationsLimit
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM",
				"limit must be a positive integer")
			return
		}
		if v > maxNotificationsLimit {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM",
				fmt.Sprintf("limit must not exceed %d", maxNotificationsLimit))
			return
		}
		limit = v
	}

	// from: opaque cursor — base64url-encoded BIGSERIAL id.
	var fromID int64 // 0 = start from newest
	if raw := q.Get("from"); raw != "" {
		decoded, err := decodeCursor(raw)
		if err != nil {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM",
				"from: invalid cursor token")
			return
		}
		fromID = decoded
	}

	// only: if "highlight", restrict to highlight notifications (AC3).
	onlyHighlight := q.Get("only") == "highlight"

	// ── Query the DB ─────────────────────────────────────────────────────────────

	rows, nextID, err := h.db.GetNotifications(r.Context(), userID, fromID, limit, onlyHighlight)
	if err != nil {
		slog.Error("GetNotifications DB query failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// ── Shape the response ────────────────────────────────────────────────────────

	items := make([]NotificationItem, 0, len(rows))
	for _, row := range rows {
		// Decode the actions JSONB into []json.RawMessage.
		var actions []json.RawMessage
		if err := json.Unmarshal(row.ActionsRaw, &actions); err != nil {
			// Corrupt stored data — treat as ["notify"] rather than crashing.
			actions = []json.RawMessage{json.RawMessage(`"notify"`)}
		}

		items = append(items, NotificationItem{
			Actions:    actions,
			Event:      row.EventRaw,
			ProfileTag: "",
			Read:       row.Read,
			RoomID:     row.RoomID,
			TS:         row.TS,
		})
	}

	// next_token: encode the next cursor; absent when no more pages (nextID == 0).
	nextToken := ""
	if nextID > 0 {
		nextToken = encodeCursor(nextID)
	}

	resp := struct {
		NextToken     string             `json:"next_token,omitempty"`
		Notifications []NotificationItem `json:"notifications"`
	}{
		NextToken:     nextToken,
		Notifications: items,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── Cursor helpers ────────────────────────────────────────────────────────────

// encodeCursor encodes a BIGSERIAL id as a base64url string.
// The id is formatted as a decimal string before encoding so the cursor is opaque
// to clients but trivially decoded by the gateway.
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

// decodeCursor decodes a base64url cursor back to a BIGSERIAL id.
// Returns an error if the token is not valid base64url or not a positive integer.
func decodeCursor(token string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("base64url decode failed: %w", err)
	}
	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("cursor must be a positive integer, got %q", string(b))
	}
	return id, nil
}

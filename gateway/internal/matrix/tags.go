package matrix

// ─── Story 7-25: Tags API — GET/PUT/DELETE /user/{userId}/rooms/{roomId}/tags ──
//
// Implements three Matrix tag endpoints:
//
//	GET    /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags
//	PUT    /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}
//	DELETE /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}
//
// Tags are stored as a single "m.tag" room account data entry with content:
//
//	{"tags": {"m.favourite": {"order": 0.5}, "u.work": {}}}
//
// Storage: AccountDataDB (reuses the interface from account_data.go /
// the room_account_data table introduced in migration 000029).
//
// Authorization: userId in path must match the authenticated user's JWT subject (AC6).
// Tag validation: tag must be non-empty and ≤ 100 characters (AC4).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nebu/nebu/internal/middleware"
)

const (
	// mTagEventType is the Matrix account data event type used for room tags.
	mTagEventType = "m.tag"
	// maxTagLength is the maximum allowed length of a tag name (AC4).
	maxTagLength = 100
)

// TagsHandler handles GET/PUT/DELETE for room tags.
type TagsHandler struct {
	serverName string
	db         AccountDataDB // reuses AccountDataDB from account_data.go
}

// TagsConfig holds dependencies for NewTagsHandler.
type TagsConfig struct {
	ServerName string
	DB         AccountDataDB
}

// NewTagsHandler constructs a TagsHandler from the provided config.
func NewTagsHandler(cfg TagsConfig) *TagsHandler {
	return &TagsHandler{
		serverName: cfg.ServerName,
		db:         cfg.DB,
	}
}

// validateTag returns an error if the tag name is empty or exceeds maxTagLength (AC4).
func validateTag(tag string) error {
	if tag == "" {
		return errors.New("tag name must not be empty")
	}
	if len(tag) > maxTagLength {
		return fmt.Errorf("tag name exceeds maximum length of %d characters", maxTagLength)
	}
	return nil
}

// checkTagsOwnership validates the userId path param against the authenticated user.
// Returns (userID, true) on success; writes 403 M_FORBIDDEN and returns ("", false) on mismatch.
func (h *TagsHandler) checkTagsOwnership(w http.ResponseWriter, r *http.Request) (string, bool) {
	authedUserID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	pathUserID := r.PathValue("userId")
	if authedUserID == "" || pathUserID == "" || authedUserID != pathUserID {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN",
			"userId in path does not match the authenticated user")
		return "", false
	}
	return authedUserID, true
}

// readTagsContent loads the current m.tag content for (userID, roomID).
// Returns an empty map when no entry exists yet (AC1: never 404 for GET /tags).
// Returns nil and writes an error response if the DB call fails for any other reason.
func (h *TagsHandler) readTagsContent(w http.ResponseWriter, r *http.Request, userID, roomID string) (map[string]json.RawMessage, bool) {
	raw, err := h.db.GetAccountData(r.Context(), userID, roomID, mTagEventType)
	if err != nil {
		if errors.Is(err, ErrAccountDataNotFound) {
			// No tags yet — return an empty map (AC1: always 200, never 404).
			return map[string]json.RawMessage{}, true
		}
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return nil, false
	}

	// Parse existing m.tag content: {"tags": {...}}.
	var tagContent struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(raw, &tagContent); err != nil {
		// Corrupt stored data — treat as empty rather than crashing.
		return map[string]json.RawMessage{}, true
	}
	if tagContent.Tags == nil {
		tagContent.Tags = map[string]json.RawMessage{}
	}
	return tagContent.Tags, true
}

// writeTagsContent serialises tags into the m.tag event format and upserts it.
// Returns false and writes an error response if serialisation or the DB call fails.
func (h *TagsHandler) writeTagsContent(w http.ResponseWriter, r *http.Request, userID, roomID string, tags map[string]json.RawMessage) bool {
	content := map[string]interface{}{
		"tags": tags,
	}
	raw, err := json.Marshal(content)
	if err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return false
	}
	if err := h.db.PutAccountData(r.Context(), userID, roomID, mTagEventType, json.RawMessage(raw)); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return false
	}
	return true
}

// ─── GET /user/{userId}/rooms/{roomId}/tags ───────────────────────────────────

// GetTags handles GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags.
//
// Flow:
//  1. Validate userId == authenticated user (AC6).
//  2. Load m.tag content from account data; return {"tags":{}} if absent (AC1).
//  3. Return 200 {"tags": {...}}.
func (h *TagsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.checkTagsOwnership(w, r)
	if !ok {
		return
	}
	roomID := r.PathValue("roomId")

	tags, ok := h.readTagsContent(w, r, userID, roomID)
	if !ok {
		return
	}

	// Always return an object, never null (AC1).
	if tags == nil {
		tags = map[string]json.RawMessage{}
	}

	resp := map[string]interface{}{"tags": tags}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ─── PUT /user/{userId}/rooms/{roomId}/tags/{tag} ─────────────────────────────

// PutTag handles PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}.
//
// Flow:
//  1. Validate userId == authenticated user (AC6).
//  2. Validate tag name (AC4).
//  3. Decode optional body {"order": 0.5} — 400 M_BAD_JSON on malformed JSON.
//  4. Load existing m.tag content; add/replace the tag entry.
//  5. Upsert updated m.tag content.
//  6. Return 200 {}.
func (h *TagsHandler) PutTag(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.checkTagsOwnership(w, r)
	if !ok {
		return
	}
	roomID := r.PathValue("roomId")
	tag := r.PathValue("tag")

	if err := validateTag(tag); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", err.Error())
		return
	}

	// Decode the tag body (e.g. {"order": 0.5}) as raw JSON so we can store it as-is.
	// An empty body {} is valid (tag with no order property).
	var tagBody json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&tagBody); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	tags, ok := h.readTagsContent(w, r, userID, roomID)
	if !ok {
		return
	}

	tags[tag] = tagBody

	if !h.writeTagsContent(w, r, userID, roomID, tags) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

// ─── DELETE /user/{userId}/rooms/{roomId}/tags/{tag} ─────────────────────────

// DeleteTag handles DELETE /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}.
//
// Flow:
//  1. Validate userId == authenticated user (AC6).
//  2. Validate tag name (AC4).
//  3. Load existing m.tag content; remove the tag key if present (noop if absent — AC3).
//  4. Upsert updated m.tag content.
//  5. Return 200 {} (AC3: idempotent).
func (h *TagsHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.checkTagsOwnership(w, r)
	if !ok {
		return
	}
	roomID := r.PathValue("roomId")
	tag := r.PathValue("tag")

	if err := validateTag(tag); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", err.Error())
		return
	}

	tags, ok := h.readTagsContent(w, r, userID, roomID)
	if !ok {
		return
	}

	// Remove the tag (noop if not present — idempotent, AC3).
	delete(tags, tag)

	if !h.writeTagsContent(w, r, userID, roomID, tags) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

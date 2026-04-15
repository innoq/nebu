package matrix

import (
	"encoding/json"
	"net/http"

	"github.com/nebu/nebu/internal/middleware"
)

// FilterHandler handles GET /_matrix/client/v3/user/{userId}/filter/{filterId}.
//
// MVP: filter storage is stateless. POST /filter returns filter_id "0".
// GET /filter/{filterId} returns a passthrough filter definition for any ID,
// so that Element Web's sync loop does not crash on reconnect.
type FilterHandler struct {
	serverName string
}

// FilterConfig holds dependencies for NewFilterHandler.
type FilterConfig struct {
	ServerName string
}

// NewFilterHandler constructs a FilterHandler from the provided config.
func NewFilterHandler(cfg FilterConfig) *FilterHandler {
	return &FilterHandler{serverName: cfg.ServerName}
}

// passthroughFilter is the static filter definition returned for any filter_id.
// It represents an unfiltered sync — equivalent to not using a filter at all.
// Fields follow the Matrix Client-Server spec §11.19.1.
var passthroughFilter = map[string]any{
	"event_fields": []string{},
	"event_format": "client",
	"presence": map[string]any{
		"not_types": []string{},
	},
	"account_data": map[string]any{
		"not_types": []string{},
	},
	"room": map[string]any{
		"timeline": map[string]any{
			"limit": 50,
		},
	},
}

// GetFilter handles GET /_matrix/client/v3/user/{userId}/filter/{filterId}.
//
// Flow:
//  1. Extract userId from URL path via r.PathValue.
//  2. Verify that the authenticated user_id matches the path userId → 403 M_FORBIDDEN.
//  3. Return 200 with the static passthrough filter definition (any filterId accepted).
func (h *FilterHandler) GetFilter(w http.ResponseWriter, r *http.Request) {
	pathUserID := r.PathValue("userId")

	// Step 2: Ownership check — a user may only read their own filters.
	// ContextKeyUserID holds the full Matrix ID (@localpart:server), already formatted
	// by JWTMiddleware. PathValue is URL-decoded by Go 1.22+ mux, so compare directly.
	authUserID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if pathUserID != authUserID {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Cannot read filters for another user")
		return
	}

	// Step 3: Return the static passthrough filter definition.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(passthroughFilter)
}

package matrix

import (
	"encoding/json"
	"net/http"
)

// ReadMarkersHandler handles POST /_matrix/client/v3/rooms/{roomId}/read_markers.
//
// MVP: accept and acknowledge the fully-read marker without persisting it.
// This stops the Element Web retry loop that produces "Error sending fully_read"
// log spam when the endpoint is missing (404).
type ReadMarkersHandler struct {
	serverName string
}

// ReadMarkersConfig holds dependencies for NewReadMarkersHandler.
type ReadMarkersConfig struct {
	ServerName string
}

// NewReadMarkersHandler constructs a ReadMarkersHandler from the provided config.
func NewReadMarkersHandler(cfg ReadMarkersConfig) *ReadMarkersHandler {
	return &ReadMarkersHandler{serverName: cfg.ServerName}
}

// PostReadMarkers handles POST /_matrix/client/v3/rooms/{roomId}/read_markers.
//
// Flow:
//  1. Decode JSON body — 400 M_BAD_JSON on malformed input.
//  2. Return 200 {} (acknowledged, not persisted in MVP).
func (h *ReadMarkersHandler) PostReadMarkers(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}\n"))
}

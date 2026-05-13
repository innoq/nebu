package config

import (
	"encoding/json"
	"net/http"
)

// HandlerConfig contains configuration for the config Handler.
type HandlerConfig struct {
	MaxBytes int64
}

// Handler handles GET /_matrix/media/v3/config and
// GET /_matrix/client/v1/media/config (via auth middleware).
// It returns {"m.upload.size": <MaxBytes>} with no auth logic —
// auth is applied via middleware at the routing level.
type Handler struct {
	maxBytes int64
}

// NewHandler creates a new config Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{maxBytes: cfg.MaxBytes}
}

// ServeHTTP implements http.Handler.
// Always returns 200 JSON {"m.upload.size": h.maxBytes}.
func (h *Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"m.upload.size": h.maxBytes,
	})
}

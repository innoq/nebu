package registry

import (
	"encoding/json"
	"net"
	"net/http"
)

type handler struct {
	reg *Registry
}

// NewHandler returns an http.Handler that serves the node registry endpoints:
//   - POST /internal/nodes/register — registers the caller node
//   - GET  /internal/nodes          — lists all registered nodes
func NewHandler(reg *Registry) http.Handler {
	h := &handler{reg: reg}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/nodes/register", h.register)
	mux.HandleFunc("GET /internal/nodes", h.list)
	return mux
}

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	h.reg.Register(addr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	entries := h.reg.List()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(entries)
}

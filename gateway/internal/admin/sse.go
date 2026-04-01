package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// MetricsReader is the consumer-defined interface for fetching live metrics from Core.
type MetricsReader interface {
	GetMetrics(ctx context.Context) (msgPerSec float64, activeSessions int, roomCount int, err error)
}

// SSEMetricsHandler handles the GET /admin/sse/metrics Server-Sent Events endpoint.
type SSEMetricsHandler struct {
	core MetricsReader
}

// NewSSEMetricsHandler creates an SSEMetricsHandler backed by the given MetricsReader.
func NewSSEMetricsHandler(core MetricsReader) *SSEMetricsHandler {
	return &SSEMetricsHandler{core: core}
}

// Handler implements the SSE endpoint. It writes an initial metrics event immediately,
// then sends updated metrics every 5 seconds and a keep-alive ping every 30 seconds.
// The handler exits cleanly when the client disconnects (r.Context().Done()).
// On gRPC error: sends event: error and keeps the connection open.
func (h *SSEMetricsHandler) Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial metrics immediately on connect
	h.sendMetrics(r.Context(), w, flusher)

	ticker := time.NewTicker(5 * time.Second)
	pingTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			h.sendMetrics(r.Context(), w, flusher)
		case <-pingTicker.C:
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n") //nolint:errcheck
			flusher.Flush()
		}
	}
}

// sendMetrics fetches metrics from core and writes a single SSE event to w.
// On error it sends event: error and keeps the connection alive.
func (h *SSEMetricsHandler) sendMetrics(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	msgPS, sessions, rooms, err := h.core.GetMetrics(ctx)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: {\"error\":\"core unavailable\"}\n\n") //nolint:errcheck
		flusher.Flush()
		return
	}
	payload := fmt.Sprintf(`{"msg_per_sec":%.1f,"active_sessions":%d,"room_count":%d}`, msgPS, sessions, rooms)
	fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", payload) //nolint:errcheck
	flusher.Flush()
}

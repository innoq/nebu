package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// flusherRecorder wraps httptest.ResponseRecorder to implement http.Flusher.
// httptest.ResponseRecorder does NOT implement http.Flusher, so SSE handler tests
// require this wrapper to avoid the "SSE not supported" 500 response.
type flusherRecorder struct {
	*httptest.ResponseRecorder
}

// Flush is a no-op for tests — writes are buffered in ResponseRecorder anyway.
func (f *flusherRecorder) Flush() {}

// fakeMetricsReader is a test double for MetricsReader.
type fakeMetricsReader struct {
	msgPerSec      float64
	activeSessions int
	roomCount      int
	err            error
}

func (f *fakeMetricsReader) GetMetrics(_ context.Context) (float64, int, int, error) {
	return f.msgPerSec, f.activeSessions, f.roomCount, f.err
}

// TestSSEMetricsHandler_ContentType verifies the handler sets text/event-stream.
func TestSSEMetricsHandler_ContentType(t *testing.T) {
	core := &fakeMetricsReader{msgPerSec: 0, activeSessions: 0, roomCount: 0, err: nil}
	h := NewSSEMetricsHandler(core)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	r := httptest.NewRequest(http.MethodGet, "/admin/sse/metrics", nil).WithContext(ctx)
	rec := &flusherRecorder{httptest.NewRecorder()}
	h.Handler(rec, r)

	got := rec.Header().Get("Content-Type")
	if got != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", got)
	}
}

// TestSSEMetricsHandler_InitialEvent verifies the initial event: metrics payload.
func TestSSEMetricsHandler_InitialEvent(t *testing.T) {
	core := &fakeMetricsReader{msgPerSec: 1.5, activeSessions: 3, roomCount: 7}
	h := NewSSEMetricsHandler(core)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	r := httptest.NewRequest(http.MethodGet, "/admin/sse/metrics", nil).WithContext(ctx)
	rec := &flusherRecorder{httptest.NewRecorder()}
	h.Handler(rec, r)

	body := rec.Body.String()
	if !strings.Contains(body, "event: metrics") {
		t.Errorf("expected 'event: metrics' in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"msg_per_sec"`) {
		t.Errorf("expected 'msg_per_sec' key in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"active_sessions"`) {
		t.Errorf("expected 'active_sessions' key in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"room_count"`) {
		t.Errorf("expected 'room_count' key in body, got:\n%s", body)
	}
}

// TestSSEMetricsHandler_GRPCError verifies that a gRPC error produces event: error.
func TestSSEMetricsHandler_GRPCError(t *testing.T) {
	core := &fakeMetricsReader{err: context.DeadlineExceeded}
	h := NewSSEMetricsHandler(core)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	r := httptest.NewRequest(http.MethodGet, "/admin/sse/metrics", nil).WithContext(ctx)
	rec := &flusherRecorder{httptest.NewRecorder()}
	h.Handler(rec, r)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected 'event: error' in body, got:\n%s", body)
	}
	if !strings.Contains(body, "core unavailable") {
		t.Errorf("expected 'core unavailable' in body, got:\n%s", body)
	}
}

// TestSSEMetricsHandler_NoCacheHeaders verifies Cache-Control: no-cache header.
func TestSSEMetricsHandler_NoCacheHeaders(t *testing.T) {
	core := &fakeMetricsReader{}
	h := NewSSEMetricsHandler(core)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	r := httptest.NewRequest(http.MethodGet, "/admin/sse/metrics", nil).WithContext(ctx)
	rec := &flusherRecorder{httptest.NewRecorder()}
	h.Handler(rec, r)

	got := rec.Header().Get("Cache-Control")
	if got != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", got)
	}
}

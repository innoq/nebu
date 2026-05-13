package config_test

// ─── Story 12.16 ATDD Tests — Config Handler unit tests ──────────────────────
//
// These tests will FAIL TO COMPILE until:
//   1. media/internal/config/handler.go defines Handler, NewHandler, HandlerConfig
//   2. Handler implements http.Handler (ServeHTTP)
//
// Test strategy:
//   - No auth logic in this package — auth is applied via middleware at routing level.
//   - Handler.ServeHTTP always returns 200 JSON {"m.upload.size": <MaxBytes>}.
//   - Tests use httptest.NewRecorder() + httptest.NewRequest() (no mux needed).
//
// Failing reason before implementation:
//   Package "github.com/nebu/nebu/media/internal/config" does not exist.
//   Handler, NewHandler, HandlerConfig are undefined.
//
// Spec compliance (Matrix CS API v1.18 §get_matrixclientv1mediaconfig):
//   - Response: 200 JSON {"m.upload.size": <bytes>}
//   - Both /_matrix/media/v3/config (unauthenticated, deprecated) and
//     /_matrix/client/v1/media/config (authenticated via middleware) use this same handler.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/media/internal/config"
)

// ─── configResp mirrors the 200 response JSON ─────────────────────────────────

type configResp struct {
	MaxUploadSize int64 `json:"m.upload.size"`
}

// ─── AT-1: GET /media/config — returns max upload size ───────────────────────
//
// AC-1, AC-2 — Handler returns 200 JSON {"m.upload.size": maxBytes}.
// Auth is handled at routing level; the handler itself needs no auth logic.
//
// RED: fails until config.Handler is implemented.

func TestConfigHandler_ReturnsMaxBytes(t *testing.T) {
	const wantMaxBytes = int64(52428800) // 50 MiB

	h := config.NewHandler(config.HandlerConfig{MaxBytes: wantMaxBytes})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("[AT-1] Content-Type must not be empty")
	}

	var resp configResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AT-1] failed to decode response body: %v", err)
	}

	if resp.MaxUploadSize != wantMaxBytes {
		t.Errorf("[AT-1] m.upload.size: expected %d, got %d", wantMaxBytes, resp.MaxUploadSize)
	}
}

// ─── AT-1b: Handler with a different MaxBytes value ───────────────────────────
//
// AC-1: The handler must use its configured MaxBytes value, not a constant.
// Parameterisation test to confirm the field is actually wired.
//
// RED: fails until config.Handler uses HandlerConfig.MaxBytes.

func TestConfigHandler_UsesConfiguredMaxBytes(t *testing.T) {
	const wantMaxBytes = int64(104857600) // 100 MiB

	h := config.NewHandler(config.HandlerConfig{MaxBytes: wantMaxBytes})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/media/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-1b] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp configResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AT-1b] failed to decode response body: %v", err)
	}

	if resp.MaxUploadSize != wantMaxBytes {
		t.Errorf("[AT-1b] m.upload.size: expected %d, got %d", wantMaxBytes, resp.MaxUploadSize)
	}
}

// ─── AT-1c: JSON key must be "m.upload.size" (exact Matrix spec key) ──────────
//
// AC-1, AC-2 — The JSON response key is "m.upload.size" (with a dot), not
// "m_upload_size" or "upload_size". This test decodes via a raw map to verify
// the exact key name, independent of struct tag correctness.
//
// RED: fails until config.Handler serialises with the correct key.

func TestConfigHandler_JSONKeyIsExact(t *testing.T) {
	h := config.NewHandler(config.HandlerConfig{MaxBytes: 52428800})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AT-1c] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("[AT-1c] failed to decode response body: %v", err)
	}

	val, ok := raw["m.upload.size"]
	if !ok {
		t.Fatalf("[AT-1c] response JSON must have key \"m.upload.size\", got keys: %v", keysOf(raw))
	}

	// JSON numbers decode as float64 via interface{}.
	switch v := val.(type) {
	case float64:
		if int64(v) != 52428800 {
			t.Errorf("[AT-1c] m.upload.size: expected 52428800, got %v", v)
		}
	default:
		t.Errorf("[AT-1c] m.upload.size: unexpected type %T (value: %v)", val, val)
	}
}

// ─── AT-1d: Handler is idempotent across multiple calls ───────────────────────
//
// The handler must produce the same response on every call (no side effects,
// no state mutation). Called twice; both must return the same MaxBytes.
//
// RED: fails until config.Handler is implemented.

func TestConfigHandler_IdempotentAcrossMultipleCalls(t *testing.T) {
	const maxBytes = int64(52428800)
	h := config.NewHandler(config.HandlerConfig{MaxBytes: maxBytes})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/config", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("[AT-1d] call %d: expected 200, got %d; body: %s", i+1, w.Code, w.Body.String())
		}

		var resp configResp
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("[AT-1d] call %d: failed to decode response: %v", i+1, err)
		}
		if resp.MaxUploadSize != maxBytes {
			t.Errorf("[AT-1d] call %d: expected %d, got %d", i+1, maxBytes, resp.MaxUploadSize)
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// keysOf returns the keys of a map[string]interface{} as a slice (for error messages).
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

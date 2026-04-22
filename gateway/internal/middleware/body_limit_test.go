package middleware_test

// Story 5.20 — Request Body Size Limits + HTTP Server Timeouts
//
// Acceptance Criteria covered:
//   AC 3 — BodyLimitMiddleware(max int64) wraps r.Body with http.MaxBytesReader
//   AC 4 — Exceeding the limit returns 413 M_TOO_LARGE (Matrix spec errcode)
//   AC 7 — Unit tests: table-driven body-limit scenarios
//
// RED PHASE: BodyLimitMiddleware is not yet defined.
// These tests must fail with a compilation error until body_limit.go is created.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/middleware"
)

// MiB is one mebibyte in bytes, matching the constant expected in body_limit.go.
const MiB = 1 << 20 // 1 048 576 bytes

// echoHandler is a minimal http.Handler that responds 200 OK and echoes the
// Content-Type back. It drains the request body so that MaxBytesReader can
// report the error before the handler writes a response.
var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 32*1024)
	for {
		_, err := r.Body.Read(buf)
		if err != nil {
			break
		}
	}
	w.WriteHeader(http.StatusOK)
})

// ---------------------------------------------------------------------------
// TestBodyLimit_RejectsOversizedJSON — table-driven, AC 3 + AC 4
// ---------------------------------------------------------------------------

// TestBodyLimit_RejectsOversizedJSON verifies that BodyLimitMiddleware(1*MiB)
// returns 413 M_TOO_LARGE when the request body exceeds the configured maximum.
// The response body must be valid JSON with errcode "M_TOO_LARGE".
func TestBodyLimit_RejectsOversizedJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		bodySize   int
		wantStatus int
		wantErrCode string
	}{
		{
			name:        "2 MiB body exceeds 1 MiB limit",
			bodySize:    2 * MiB,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantErrCode: "M_TOO_LARGE",
		},
		{
			name:        "10 MiB body far exceeds 1 MiB limit",
			bodySize:    10 * MiB,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantErrCode: "M_TOO_LARGE",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := middleware.BodyLimitMiddleware(1 * MiB)(echoHandler)

			body := bytes.Repeat([]byte("x"), tc.bodySize)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", rr.Code, tc.wantStatus)
			}

			// Response must be valid JSON with the Matrix errcode field.
			contentType := rr.Header().Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("Content-Type: got %q, want application/json", contentType)
			}

			var resp map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response JSON: %v", err)
			}
			if resp["errcode"] != tc.wantErrCode {
				t.Errorf("errcode: got %q, want %q", resp["errcode"], tc.wantErrCode)
			}
			if resp["error"] == "" {
				t.Error("error field must not be empty")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBodyLimit_AcceptsWithinLimit — happy path, AC 3
// ---------------------------------------------------------------------------

// TestBodyLimit_AcceptsWithinLimit verifies that a body well within the limit
// passes through to the inner handler and receives 200 OK.
func TestBodyLimit_AcceptsWithinLimit(t *testing.T) {
	t.Parallel()

	handler := middleware.BodyLimitMiddleware(1 * MiB)(echoHandler)

	body := bytes.Repeat([]byte("a"), 100*1024) // 100 KiB
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// TestBodyLimit_ExactLimit — boundary value, AC 3
// ---------------------------------------------------------------------------

// TestBodyLimit_ExactLimit verifies that a body of exactly the configured limit
// is accepted (boundary inclusive on the allowed side).
func TestBodyLimit_ExactLimit(t *testing.T) {
	t.Parallel()

	const limit int64 = 1 * MiB
	handler := middleware.BodyLimitMiddleware(limit)(echoHandler)

	body := bytes.Repeat([]byte("b"), int(limit)) // exactly 1 MiB
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 for exact-limit body", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// TestBodyLimit_ExactLimitPlusOne — boundary value, AC 3 + AC 4
// ---------------------------------------------------------------------------

// TestBodyLimit_ExactLimitPlusOne verifies that a body of exactly limit+1 bytes
// is rejected with 413 (boundary exclusive on the forbidden side).
func TestBodyLimit_ExactLimitPlusOne(t *testing.T) {
	t.Parallel()

	const limit int64 = 1 * MiB
	handler := middleware.BodyLimitMiddleware(limit)(echoHandler)

	body := bytes.Repeat([]byte("c"), int(limit)+1) // 1 MiB + 1 byte
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want 413 for limit+1 body", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
	if resp["errcode"] != "M_TOO_LARGE" {
		t.Errorf("errcode: got %q, want M_TOO_LARGE", resp["errcode"])
	}
}

// ---------------------------------------------------------------------------
// SlowLoris / TCP-timeout tests — integration only
// ---------------------------------------------------------------------------

// TestBodyLimit_SlowLorisReadHeaderTimeout would verify that a client writing
// partial HTTP headers and then stalling is disconnected within ReadHeaderTimeout.
// This requires a real TCP listener (net.Listen + http.Server) and is too
// heavyweight for the unit-test binary.  The scenario is covered by the
// integration test suite (make test-integration).
func TestBodyLimit_SlowLorisReadHeaderTimeout(t *testing.T) {
	t.Skip("integration test — requires real TCP listener; covered by make test-integration")
	// If you are adding this as an integration test, the scenario is:
	//   1. Start an http.Server with ReadHeaderTimeout: 1*time.Second.
	//   2. Dial TCP, write "GET / HTTP/1.1\r\nHost: local" (incomplete headers).
	//   3. Sleep 2 seconds without completing the request.
	//   4. Assert the server closes the connection (Read returns io.EOF / net.OpError).
}

// TestBodyLimit_SlowLorisReadTimeout would verify that a client that sends
// headers quickly but then stalls mid-body is disconnected within ReadTimeout.
// Same reason for skipping — integration only.
func TestBodyLimit_SlowLorisReadTimeout(t *testing.T) {
	t.Skip("integration test — requires real TCP listener; covered by make test-integration")
}

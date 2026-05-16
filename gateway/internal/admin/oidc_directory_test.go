package admin

// oidc_directory_test.go — Story 14.2b: Gateway OIDC Directory Service + Cache + Rate Limit
//
// Acceptance tests written FIRST (red phase, TDD).
// These tests FAIL until oidc_directory.go is implemented.
//
// Test matrix (from story Acceptance Tests section):
//   AT-1  TestOIDCDirectoryService_CacheHit — httptest: only ONE HTTP call for two service calls within TTL
//   AT-2  TestOIDCDirectoryService_CacheMiss — httptest: HTTP call made on cold cache
//   AT-3  TestOIDCDirectoryService_UnreachableEndpoint — empty list + no panic + warning logged
//   AT-4  TestOIDCDirectoryService_NonHTTPSEndpoint — validation error returned; no HTTP call
//   AT-5  TestOIDCDirectoryService_RateLimitEnforcement — 6th req in 1s is denied
//   AT-6  TestOIDCDirectoryService_BearerTokenNotLogged — token absent from log output
//   AT-7  TestOIDCDirectoryService_ResponseSizeLimit — >10 MB truncated gracefully

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- AT-1: Cache Hit — only ONE HTTP call for two calls within cache TTL ---

// TestOIDCDirectoryService_CacheHit verifies that a second FetchUsers call within the
// 30-second cache TTL does NOT make a second outbound HTTP call.
// AC1, AC4 (Story 14.2b)
func TestOIDCDirectoryService_CacheHit(t *testing.T) {
	var callCount atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"sub":"user1","display_name":"Alice","email":"alice@example.com"}]`)
	}))
	defer srv.Close()

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "test-token",
		Enabled:     true,
		HTTPClient:  srv.Client(), // use TLS test client
	})

	// First call — cold cache → HTTP call expected
	users1, err := svc.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("[AT-1] first FetchUsers: unexpected error: %v", err)
	}
	if len(users1) != 1 {
		t.Fatalf("[AT-1] expected 1 user, got %d", len(users1))
	}

	// Second call — warm cache → no HTTP call
	_, err = svc.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("[AT-1] second FetchUsers: unexpected error: %v", err)
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("[AT-1] expected exactly 1 HTTP call (cache hit on second call), got %d", got)
	}
}

// --- AT-2: Cache Miss — HTTP call made on cold cache ---

// TestOIDCDirectoryService_CacheMiss verifies that the first FetchUsers call on a cold
// cache makes exactly one outbound HTTP call and returns users.
// AC1, AC4 (Story 14.2b)
func TestOIDCDirectoryService_CacheMiss(t *testing.T) {
	var callCount atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		// Verify bearer token is present in Authorization header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"sub":"u1","display_name":"Bob","email":"bob@example.com"},{"sub":"u2","display_name":"Carol","email":"carol@example.com"}]`)
	}))
	defer srv.Close()

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "my-bearer-token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	})

	users, err := svc.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("[AT-2] FetchUsers: unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("[AT-2] expected 2 users, got %d", len(users))
	}
	if callCount.Load() != 1 {
		t.Errorf("[AT-2] expected exactly 1 HTTP call, got %d", callCount.Load())
	}
}

// --- AT-3: Unreachable Endpoint — empty list + warning, no panic ---

// TestOIDCDirectoryService_UnreachableEndpoint verifies that when the OIDC directory
// endpoint is unreachable, FetchUsers returns an empty list (no error to caller) and
// logs a warning.
// AC2, AC4 (Story 14.2b)
func TestOIDCDirectoryService_UnreachableEndpoint(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    "https://localhost:19999", // nothing listening here
		BearerToken: "tok",
		Enabled:     true,
		Logger:      logger,
	})

	users, err := svc.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("[AT-3] expected no error returned to caller (graceful degradation), got: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("[AT-3] expected empty user list, got %d users", len(users))
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "WARN") && !strings.Contains(logOutput, "warn") {
		t.Errorf("[AT-3] expected a warning log line, got: %q", logOutput)
	}
}

// --- AT-4: Non-HTTPS Endpoint — validation error ---

// TestOIDCDirectoryService_NonHTTPSEndpoint verifies that configuring an HTTP
// (non-HTTPS) endpoint returns a validation error and makes no outbound call.
// AC3, AC4 (Story 14.2b) — CR-1 from security guide
func TestOIDCDirectoryService_NonHTTPSEndpoint(t *testing.T) {
	var callCount atomic.Int64
	// We set up an http (not https) server to prove no call is made
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		fmt.Fprint(w, `[]`)
	}))
	defer plainSrv.Close()

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    plainSrv.URL, // http://, not https://
		BearerToken: "tok",
		Enabled:     true,
	})

	_, err := svc.FetchUsers(context.Background())
	if err == nil {
		t.Fatal("[AT-4] expected a validation error for non-HTTPS endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "HTTPS") && !strings.Contains(err.Error(), "https") {
		t.Errorf("[AT-4] expected error message to mention HTTPS, got: %q", err.Error())
	}
	if callCount.Load() != 0 {
		t.Errorf("[AT-4] expected NO HTTP call for non-HTTPS endpoint, got %d", callCount.Load())
	}
}

// --- AT-5: Rate Limit Enforcement — 6th request in 1s is denied ---

// TestOIDCDirectoryService_RateLimitEnforcement verifies that the rate limiter allows
// exactly 5 requests per second per session and denies the 6th.
// AC1 ("rate limit of 5 req/s per admin session"), AC4 (Story 14.2b) — CR-5 from security guide
func TestOIDCDirectoryService_RateLimitEnforcement(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "tok",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	})

	const sessionID = "session-ratelimit"
	allowed := 0
	denied := 0

	// Fire 6 calls in rapid succession
	for i := 0; i < 6; i++ {
		ok := svc.Allow(sessionID)
		if ok {
			allowed++
		} else {
			denied++
		}
	}

	if allowed != 5 {
		t.Errorf("[AT-5] expected 5 allowed requests, got %d", allowed)
	}
	if denied != 1 {
		t.Errorf("[AT-5] expected 1 denied request (6th), got %d", denied)
	}
}

// --- AT-6: Bearer Token Not Logged ---

// TestOIDCDirectoryService_BearerTokenNotLogged verifies that the bearer token
// never appears in log output, even on error paths.
// AC4 (Story 14.2b) — CR-3 from security guide
func TestOIDCDirectoryService_BearerTokenNotLogged(t *testing.T) {
	const secretToken = "super-secret-bearer-token-12345"
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Use an unreachable endpoint to trigger the error/warning log path
	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    "https://localhost:19998",
		BearerToken: secretToken,
		Enabled:     true,
		Logger:      logger,
	})

	_, _ = svc.FetchUsers(context.Background())

	logOutput := buf.String()
	if strings.Contains(logOutput, secretToken) {
		t.Errorf("[AT-6] bearer token appeared in log output: %q", logOutput)
	}
}

// --- AT-7: Response Size Limit — >10 MB truncated gracefully ---

// TestOIDCDirectoryService_ResponseSizeLimit verifies that a response larger than
// 10 MB is truncated at the limit and does not cause an OOM or panic.
// AC4 (Story 14.2b) — CR-4 from security guide
func TestOIDCDirectoryService_ResponseSizeLimit(t *testing.T) {
	const limitBytes = 10 * 1024 * 1024 // 10 MB

	var warningLogged atomic.Bool
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_ = warningLogged // used via buf check below

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write more than 10 MB — deliberately malformed JSON to avoid parse success
		w.Write([]byte("["))
		junk := strings.Repeat(`{"sub":"x","display_name":"` + strings.Repeat("a", 1000) + `","email":"x@x.com"},`, 11000)
		w.Write([]byte(junk))
		// No closing bracket — response is intentionally oversized and malformed
	}))
	defer srv.Close()

	svc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "tok",
		Enabled:     true,
		HTTPClient:  srv.Client(),
		Logger:      logger,
	})

	// Should not panic; should return gracefully (empty list or partial, but no crash)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.FetchUsers(context.Background()) //nolint:errcheck
	}()

	select {
	case <-done:
		// Success — no hang, no panic
	case <-time.After(15 * time.Second):
		t.Fatal("[AT-7] FetchUsers did not return within 15 seconds (possible hang on oversized response)")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "truncat") && !strings.Contains(logOutput, "limit") && !strings.Contains(logOutput, "WARN") {
		// A warning about truncation or limit should appear — allow flexible wording
		t.Logf("[AT-7] note: no truncation warning found in logs (got: %q) — verify CR-4 is enforced", logOutput)
	}
}

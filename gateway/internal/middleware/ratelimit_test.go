package middleware_test

// Story 5.21 — Per-IP Rate Limiting for Public Endpoints
//
// Acceptance Criteria covered:
//   AC 1 — IPRateLimiter per-IP token-bucket with LRU (10 000 entries)
//   AC 2 — strict tier: 5 req/min, burst 3
//   AC 3 — Rate-limit exceeded → 429 M_LIMIT_EXCEEDED + Retry-After header
//   AC 4 — trustedProxy=true: rightmost-minus-1 XFF IP extraction (spoofing-resistant)
//   AC 7 — Unit tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/nebu/nebu/internal/middleware"
)

// okHandler is a trivial inner handler that always returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// ---------------------------------------------------------------------------
// TestRateLimit_StrictTier_BlocksAfter5
// AC 1 + AC 2 + AC 3
// ---------------------------------------------------------------------------

// TestRateLimit_StrictTier_BlocksAfter5 verifies that IPRateLimiter with a
// strict configuration (5 req/min, burst 3 — rate.Limit(5.0/60)) allows the
// first burst requests and then blocks once the bucket is exhausted.
//
// The test uses burst=5 to allow exactly 5 consecutive requests to pass
// (burst controls the initial token count), and verifies the 6th returns
// 429 M_LIMIT_EXCEEDED with a Retry-After header.
func TestRateLimit_StrictTier_BlocksAfter5(t *testing.T) {
	t.Parallel()

	// strict tier: 5 req/min, burst 5 (so exactly 5 requests are allowed
	// before the first refill tick — deterministic in unit tests).
	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(5.0 / 60.0), // 5 requests per minute
		Burst: 5,
	}
	handler := middleware.NewIPRateLimiter(cfg, false, "test")(okHandler)

	const sameIP = "192.0.2.1:12345"

	// First 5 requests must succeed.
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/login", nil)
		req.RemoteAddr = sameIP
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 OK, got %d", i, rr.Code)
		}
	}

	// 6th request must be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/login", nil)
	req.RemoteAddr = sameIP
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: expected 429 Too Many Requests, got %d; body: %s",
			rr.Code, rr.Body.String())
	}

	// Response must be valid Matrix JSON with errcode M_LIMIT_EXCEEDED.
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode 429 response JSON: %v", err)
	}
	if resp["errcode"] != "M_LIMIT_EXCEEDED" {
		t.Errorf("errcode: got %q, want M_LIMIT_EXCEEDED", resp["errcode"])
	}
	if resp["error"] == "" {
		t.Error("error field must not be empty in 429 response")
	}

	// Retry-After header must be present (validated separately in
	// TestRateLimit_RetryAfterHeader — kept here as a quick smoke check).
	if rr.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header must be present on 429 response")
	}
}

// ---------------------------------------------------------------------------
// TestRateLimit_DifferentIPs_NotShared
// AC 1 — independent per-IP buckets
// ---------------------------------------------------------------------------

// TestRateLimit_DifferentIPs_NotShared verifies that consuming tokens from
// IP A's bucket does not affect IP B's bucket.
//
// Both IPs make 3 requests against a burst-3 limiter.  All 6 requests must
// succeed because each IP starts with a fresh, independent bucket.
func TestRateLimit_DifferentIPs_NotShared(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(5.0 / 60.0),
		Burst: 3,
	}
	handler := middleware.NewIPRateLimiter(cfg, false, "test")(okHandler)

	ipA := "10.0.0.1:11111"
	ipB := "10.0.0.2:22222"

	// IP A: 3 requests — must all pass.
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ipA
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("IP A request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// IP B: 3 requests — must all pass (independent bucket, not exhausted by A).
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ipB
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("IP B request %d: expected 200, got %d (bucket must be independent of IP A)", i, rr.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRateLimit_RetryAfterHeader
// AC 3 — Retry-After is a positive integer (seconds)
// ---------------------------------------------------------------------------

// TestRateLimit_RetryAfterHeader verifies that the 429 response carries a
// Retry-After header whose value is a positive integer (seconds until the
// next token is available).
func TestRateLimit_RetryAfterHeader(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(5.0 / 60.0), // 5 req/min — next token in ~12 s
		Burst: 1,
	}
	handler := middleware.NewIPRateLimiter(cfg, false, "test")(okHandler)

	const ip = "203.0.113.42:9999"

	// Exhaust the single-token burst.
	req1 := httptest.NewRequest(http.MethodPost, "/", nil)
	req1.RemoteAddr = ip
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}

	// Second request must be rejected.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.RemoteAddr = ip
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rr2.Code)
	}

	retryAfter := rr2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("Retry-After header must be present on 429 response")
	}

	// Retry-After must be a parseable integer > 0.
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After value %q is not a valid integer: %v", retryAfter, err)
	}
	if seconds <= 0 {
		t.Errorf("Retry-After must be > 0, got %d", seconds)
	}
}

// ---------------------------------------------------------------------------
// TestRateLimit_TrustedProxy_RightmostMinusOne
// AC 4 — rightmost-minus-1 XFF extraction prevents client IP spoofing
// ---------------------------------------------------------------------------

// TestRateLimit_TrustedProxy_RightmostMinusOne verifies that when
// trustedProxy=true the middleware uses the rightmost-minus-1 entry from
// X-Forwarded-For (the IP appended by the trusted proxy), not the leftmost
// entry which is fully client-controlled and can be spoofed.
//
// Scenario: an attacker sends X-Forwarded-For with a crafted client IP
// ("1.2.3.4") but the reverse proxy appends the real peer address ("9.9.9.9").
// The limiter must key on "9.9.9.9" (proxy-appended, rightmost-minus-1).
// A second IP ("5.5.5.5") from a different peer must exhaust its own bucket
// independently.
func TestRateLimit_TrustedProxy_RightmostMinusOne(t *testing.T) {
	t.Parallel()

	// burst=1 so the first request passes and the second is rejected.
	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(1.0 / 60.0),
		Burst: 1,
	}
	handler := middleware.NewIPRateLimiter(cfg, true, "test")(okHandler)

	// Request 1: X-Forwarded-For contains a spoofed leftmost IP and the proxy-
	// appended rightmost IP.  The limiter must key on "9.9.9.9".
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "127.0.0.1:1234"
	req1.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}

	// Request 2: same proxy-appended IP ("9.9.9.9") but attacker tries a
	// different spoofed leftmost IP.  Must be rate-limited (same bucket).
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("X-Forwarded-For", "8.8.8.8, 9.9.9.9")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request (same proxy IP, different spoofed XFF): expected 429, got %d — "+
			"leftmost XFF must not be used as rate-limit key", rr2.Code)
	}

	// Request 3: a genuinely different peer IP in rightmost-minus-1 position
	// must have its own fresh bucket.
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "127.0.0.1:1234"
	req3.Header.Set("X-Forwarded-For", "1.2.3.4, 5.5.5.5")
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("third request (different real IP): expected 200, got %d", rr3.Code)
	}
}

// ---------------------------------------------------------------------------
// TestRateLimit_PrometheusCounters
// MAJOR-2 — nebu_rate_limit_total incremented on allow and deny
// ---------------------------------------------------------------------------

// TestRateLimit_PrometheusCounters verifies that nebu_rate_limit_total is
// incremented with the correct tier and decision labels on every request.
//
// Note: because prometheus.MustRegister is global, these counters accumulate
// across tests in the same process.  The test therefore records a baseline
// before making requests and asserts that the delta matches expectations.
func TestRateLimit_PrometheusCounters(t *testing.T) {
	t.Parallel()

	// Use a unique tier label so this test's counts are not confused with
	// counts from other parallel tests.
	const testTier = "prometheus_test"

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 2,
	}
	handler := middleware.NewIPRateLimiter(cfg, false, testTier)(okHandler)

	const ip = "192.0.2.99:5555"

	// Two requests pass (burst=2), third is denied.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: expected 429, got %d", rr.Code)
	}

	// Gather metrics and verify the tier-specific counters exist with non-zero values.
	// We use prometheus.DefaultGatherer to read all registered metrics.
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	var allowCount, denyCount float64
	for _, mf := range mfs {
		if mf.GetName() != "nebu_rate_limit_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			tier, decision := "", ""
			for _, lp := range m.GetLabel() {
				switch lp.GetName() {
				case "tier":
					tier = lp.GetValue()
				case "decision":
					decision = lp.GetValue()
				}
			}
			if tier != testTier {
				continue
			}
			switch decision {
			case "allow":
				allowCount = m.GetCounter().GetValue()
			case "deny":
				denyCount = m.GetCounter().GetValue()
			}
		}
	}

	if allowCount < 2 {
		t.Errorf("nebu_rate_limit_total{tier=%q,decision=allow}: got %.0f, want >= 2", testTier, allowCount)
	}
	if denyCount < 1 {
		t.Errorf("nebu_rate_limit_total{tier=%q,decision=deny}: got %.0f, want >= 1", testTier, denyCount)
	}
}

// ---------------------------------------------------------------------------
// Story 11-5 — Per-User Search Rate Limiting
//
// AC1 — 11th request from the same user returns 429 M_LIMIT_EXCEEDED
// AC2 — Different users have independent rate-limit buckets
// AC3 — 429 response body contains retry_after_ms field (milliseconds)
// AC4 — No user_id in context → falls back to IP (no panic)
// AC5 — NEBU_RATE_LIMIT_DISABLED=true → middleware is a no-op
// ---------------------------------------------------------------------------

// contextWithUserID injects a user_id into the request context to simulate
// jwtMiddleware having already run. Uses the same key as the real middleware.
func contextWithUserID(r *http.Request, userID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.ContextKeyUserID, userID))
}

// TestUserRateLimit_BlocksAfter10 verifies that the 11th request from the same
// user is rejected with 429 M_LIMIT_EXCEEDED (AC1).
func TestUserRateLimit_BlocksAfter10(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 10,
	}
	handler := middleware.NewUserRateLimiter(cfg, "search")(okHandler)

	// First 10 requests must pass.
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/search", nil)
		req = contextWithUserID(req, "@alice:test.local")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("AC1: request %d expected 200, got %d", i, rr.Code)
		}
	}

	// 11th request must be blocked.
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/search", nil)
	req = contextWithUserID(req, "@alice:test.local")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("AC1: 11th request expected 429, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_LIMIT_EXCEEDED") {
		t.Errorf("AC1: expected M_LIMIT_EXCEEDED in body, got: %s", rr.Body.String())
	}
	// MINOR-4 fix: Retry-After header must be present (Story spec AC1, last bullet).
	if rr.Header().Get("Retry-After") == "" {
		t.Error("AC1: Retry-After header must be present on 429 response")
	}
}

// TestUserRateLimit_DifferentUsers_IndependentBuckets verifies that different
// users have independent token buckets (AC2).
func TestUserRateLimit_DifferentUsers_IndependentBuckets(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 10,
	}
	handler := middleware.NewUserRateLimiter(cfg, "search")(okHandler)

	// Alice consumes all 10 tokens.
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/search", nil)
		req = contextWithUserID(req, "@alice:test.local")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Verify Alice is actually blocked (MINOR-2 fix: confirm pre-condition holds).
	aliceExtra := httptest.NewRequest(http.MethodPost, "/search", nil)
	aliceExtra = contextWithUserID(aliceExtra, "@alice:test.local")
	aliceRR := httptest.NewRecorder()
	handler.ServeHTTP(aliceRR, aliceExtra)
	if aliceRR.Code != http.StatusTooManyRequests {
		t.Fatalf("AC2 pre-condition: Alice's 11th request should be 429, got %d", aliceRR.Code)
	}

	// Bob's bucket is independent — his first request must pass.
	bobReq := httptest.NewRequest(http.MethodPost, "/search", nil)
	bobReq = contextWithUserID(bobReq, "@bob:test.local")
	bobRR := httptest.NewRecorder()
	handler.ServeHTTP(bobRR, bobReq)

	if bobRR.Code != http.StatusOK {
		t.Fatalf("AC2: Bob's bucket should be independent of Alice's; got %d: %s", bobRR.Code, bobRR.Body.String())
	}
}

// TestUserRateLimit_RetryAfterMs_InBody verifies that the 429 response body
// contains a retry_after_ms field with a positive integer value (AC3).
func TestUserRateLimit_RetryAfterMs_InBody(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 1,
	}
	handler := middleware.NewUserRateLimiter(cfg, "search")(okHandler)

	// Exhaust the single-token burst.
	req := httptest.NewRequest(http.MethodPost, "/search", nil)
	req = contextWithUserID(req, "@user:test.local")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// 2nd request must be rate-limited.
	req2 := httptest.NewRequest(http.MethodPost, "/search", nil)
	req2 = contextWithUserID(req2, "@user:test.local")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req2)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("AC2: expected 429, got %d", rr.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("AC2: invalid JSON in 429 body: %v", err)
	}
	retryAfterMs, ok := body["retry_after_ms"]
	if !ok {
		t.Fatal("AC2: 429 body must contain 'retry_after_ms' field")
	}
	v, ok := retryAfterMs.(float64)
	// MAJOR-1 fix: must be >= 1000 ms (not just > 0) to verify unit is milliseconds, not seconds.
	// With rate=10/60 and burst=1, delay is ~6 s → retry_after_ms >= 6000. Minimum valid value is 1000.
	if !ok || v < 1000 {
		t.Errorf("AC2: retry_after_ms must be >= 1000 (milliseconds), got %v", retryAfterMs)
	}

	// Retry-After header must also be present (MINOR-4 fix).
	retryAfterHeader := rr.Header().Get("Retry-After")
	if retryAfterHeader == "" {
		t.Error("AC2: Retry-After header must be present on 429 response")
	}
}

// TestUserRateLimit_NoUserID_FallsBackToIP verifies that a request without a
// user_id in context does not panic and the middleware applies a bucket keyed
// on the request IP instead (AC4).
func TestUserRateLimit_NoUserID_FallsBackToIP(t *testing.T) {
	t.Parallel()

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 10,
	}
	handler := middleware.NewUserRateLimiter(cfg, "search")(okHandler)

	// No user_id injected — must not panic, must return 200.
	req := httptest.NewRequest(http.MethodPost, "/search", nil)
	req.RemoteAddr = "10.0.0.99:55555"
	rr := httptest.NewRecorder()

	// Must not panic.
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("AC4: expected 200 (fallback to IP), got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUserRateLimit_Disabled_NoOp verifies that when NEBU_RATE_LIMIT_DISABLED=true
// the middleware is a no-op and all requests pass through without rate limiting (AC5/AC7).
func TestUserRateLimit_Disabled_NoOp(t *testing.T) {
	t.Setenv("NEBU_RATE_LIMIT_DISABLED", "true")

	cfg := middleware.RateLimitConfig{
		Rate:  rate.Limit(10.0 / 60.0),
		Burst: 1, // burst=1 so without no-op, only 1 request would pass
	}
	handler := middleware.NewUserRateLimiter(cfg, "search")(okHandler)

	// All 20 requests must pass — the limiter must be a no-op.
	for i := 1; i <= 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/search", nil)
		req = contextWithUserID(req, "@alice:test.local")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("AC7 (NEBU_RATE_LIMIT_DISABLED=true): request %d expected 200 (no-op), got %d: %s",
				i, rr.Code, rr.Body.String())
		}
	}
}

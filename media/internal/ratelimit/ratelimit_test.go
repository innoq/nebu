package ratelimit_test

// Story 12.10 — Per-IP Rate Limiting on Media Gateway
// Story 12.11 — Media Gateway Rate Limit + Audit Trail SEC Fixes (XFF gating)
//
// Acceptance Tests (RED phase — written BEFORE implementation):
//
//   AT-12-10-1 — Upload rate limit blocks after burst
//   AT-12-10-2 — Download/thumbnail rate limit blocks after burst
//   AT-12-10-3 — Per-IP isolation: two IPs have independent buckets
//   AT-12-10-4 — 429 response format: correct JSON body + Retry-After header
//   AT-12-10-5 — NEBU_RATE_LIMIT_DISABLED=true → no-op middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/nebu/nebu/media/internal/ratelimit"
	"golang.org/x/time/rate"
)

// okHandler is a trivial inner handler that always returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// ---------------------------------------------------------------------------
// AT-12-10-1 — Upload rate limit blocks the (burst+1)th request
//
// AC-1: Given IPRateLimiter with upload config (burst=10 for test clarity),
//       when 11 requests from the same IP arrive back-to-back,
//       then the first 10 return 200 and the 11th returns 429.
// ---------------------------------------------------------------------------

func TestUploadRateLimit_BlocksAfterBurst(t *testing.T) {
	t.Parallel()

	// burst=10 for test clarity: exactly 10 consecutive requests must pass.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(10), // 10 req/s
		Burst: 10,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	const ip = "192.0.2.1:12345"

	// First 10 requests must succeed.
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-10-1] request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 11th request must be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req.RemoteAddr = ip
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-10-1] 11th request: expected 429, got %d; body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AT-12-10-2 — Download/thumbnail rate limit blocks the (burst+1)th request
//
// AC-2: Given IPRateLimiter with download config (burst=100 for test),
//       when 101 requests from the same IP arrive back-to-back,
//       then the first 100 return 200 and the 101st returns 429.
// ---------------------------------------------------------------------------

func TestDownloadRateLimit_BlocksAfterBurst(t *testing.T) {
	t.Parallel()

	// burst=100: exactly 100 consecutive requests must pass.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(100), // 100 req/s
		Burst: 100,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	const ip = "198.51.100.5:8080"

	// First 100 requests must succeed.
	for i := 1; i <= 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download/example.com/abc123", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-10-2] request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 101st request must be rate-limited.
	req := httptest.NewRequest(http.MethodGet, "/_matrix/media/v3/download/example.com/abc123", nil)
	req.RemoteAddr = ip
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-10-2] 101st request: expected 429, got %d; body: %s",
			rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AT-12-10-3 — Per-IP isolation: two IPs do not share token buckets
//
// AC-3: Given burst=5, when IP A makes 5 requests AND IP B makes 5 requests,
//       then all 10 requests return 200 (independent buckets).
// ---------------------------------------------------------------------------

func TestRateLimit_DifferentIPs_IndependentBuckets(t *testing.T) {
	t.Parallel()

	cfg := ratelimit.Config{
		Rate:  rate.Limit(5),
		Burst: 5,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	ipA := "10.0.0.1:11111"
	ipB := "10.0.0.2:22222"

	// IP A: 5 requests — must all pass.
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
		req.RemoteAddr = ipA
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-10-3] IP A request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// IP B: 5 requests — must all pass (independent bucket, not shared with A).
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
		req.RemoteAddr = ipB
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-10-3] IP B request %d: expected 200, got %d (bucket must be independent of IP A)", i, rr.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// AT-12-10-4 — 429 response format: JSON body + Retry-After header
//
// AC-1/AC-2: When rate limit is exceeded, the response MUST have:
//   - HTTP 429
//   - Content-Type: application/json
//   - Retry-After header with integer >= 1
//   - JSON body: {"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}
// ---------------------------------------------------------------------------

func TestRateLimit_429ResponseFormat(t *testing.T) {
	t.Parallel()

	// burst=1 so the first request passes and the second is rejected immediately.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(1), // 1 req/s — next token in ~1s
		Burst: 1,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	const ip = "203.0.113.42:9999"

	// Exhaust the single-token burst.
	req1 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req1.RemoteAddr = ip
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("[AT-12-10-4] first request: expected 200, got %d", rr1.Code)
	}

	// Second request must be rejected.
	req2 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req2.RemoteAddr = ip
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-10-4] second request: expected 429, got %d", rr2.Code)
	}

	// Content-Type must be application/json.
	ct := rr2.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("[AT-12-10-4] Content-Type: got %q, want application/json", ct)
	}

	// Retry-After header must be a positive integer.
	retryAfter := rr2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("[AT-12-10-4] Retry-After header must be present on 429 response")
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("[AT-12-10-4] Retry-After value %q is not a valid integer: %v", retryAfter, err)
	}
	if seconds < 1 {
		t.Errorf("[AT-12-10-4] Retry-After must be >= 1, got %d", seconds)
	}

	// JSON body must match Matrix spec format.
	var resp map[string]string
	if err := json.NewDecoder(rr2.Body).Decode(&resp); err != nil {
		t.Fatalf("[AT-12-10-4] decode 429 response JSON: %v", err)
	}
	if resp["errcode"] != "M_LIMIT_EXCEEDED" {
		t.Errorf("[AT-12-10-4] errcode: got %q, want M_LIMIT_EXCEEDED", resp["errcode"])
	}
	if resp["error"] != "Too many requests" {
		t.Errorf("[AT-12-10-4] error: got %q, want \"Too many requests\"", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// AT-12-10-4b — X-Forwarded-For extraction: last entry used as rate-limit key
//
// When X-Forwarded-For has 2+ entries, the last entry (proxy-appended) is the
// rate-limit key. Two requests with different XFF rightmost IPs have separate buckets.
// ---------------------------------------------------------------------------

func TestRateLimit_XForwardedFor_Extraction(t *testing.T) {
	t.Parallel()

	// burst=1 so we can distinguish IPs easily.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(1),
		Burst: 1,
	}
	// trustedProxy=true: XFF extraction is active.
	handler := ratelimit.NewIPRateLimiter(cfg, true)(okHandler)

	// Request from proxy: X-Forwarded-For: spoofed-client, real-client
	// The limiter must key on "9.9.9.9" (last / proxy-appended entry).
	req1 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req1.RemoteAddr = "127.0.0.1:1234"
	req1.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("[AT-12-10-4b] first request (9.9.9.9): expected 200, got %d", rr1.Code)
	}

	// Second request: same rightmost IP → same bucket → must be blocked.
	req2 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("X-Forwarded-For", "8.8.8.8, 9.9.9.9")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-10-4b] second request (same proxy IP 9.9.9.9): expected 429, got %d — "+
			"leftmost XFF must not be the rate-limit key", rr2.Code)
	}

	// Third request: different rightmost IP → independent bucket → must pass.
	req3 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req3.RemoteAddr = "127.0.0.1:1234"
	req3.Header.Set("X-Forwarded-For", "1.2.3.4, 5.5.5.5")
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("[AT-12-10-4b] third request (different proxy IP 5.5.5.5): expected 200, got %d", rr3.Code)
	}
}

// ---------------------------------------------------------------------------
// AT-12-10-5 — NEBU_RATE_LIMIT_DISABLED=true makes middleware a no-op
//
// AC: When NEBU_RATE_LIMIT_DISABLED=true, all requests pass regardless of limits.
// ---------------------------------------------------------------------------

func TestRateLimit_Disabled_NoOp(t *testing.T) {
	t.Setenv("NEBU_RATE_LIMIT_DISABLED", "true")

	// burst=1 — without no-op, only 1 request would pass.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(1),
		Burst: 1,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	const ip = "10.10.10.10:5000"

	// All 20 requests must pass — the limiter must be a no-op.
	for i := 1; i <= 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
		req.RemoteAddr = ip
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-10-5] (NEBU_RATE_LIMIT_DISABLED=true) request %d expected 200 (no-op), got %d: %s",
				i, rr.Code, rr.Body.String())
		}
	}
}

// ---------------------------------------------------------------------------
// Story 12.11 — SEC Fix F-2: XFF gating behind trustedProxy flag
// ---------------------------------------------------------------------------

// AT-12-11-1 — Rate limiter ignores XFF when trustedProxy=false (default)
//
// AC-F2-1: Given NewIPRateLimiter(cfg, false), when a request has
//          X-Forwarded-For: 1.2.3.4 and RemoteAddr: 5.6.7.8:1234,
//          then rate limiting keys on 5.6.7.8 (RemoteAddr), NOT on 1.2.3.4.
//
// RED: This test will compile and fail until NewIPRateLimiter accepts a second
//      bool argument and extractClientIP ignores XFF when trustedProxy=false.
func TestRateLimit_TrustedProxyFalse_IgnoresXFF(t *testing.T) {
	t.Parallel()

	// burst=1 so we can easily exhaust one IP's bucket.
	cfg := ratelimit.Config{
		Rate:  rate.Limit(1),
		Burst: 1,
	}
	// trustedProxy=false: XFF must be ignored.
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	// First request: RemoteAddr=5.6.7.8, XFF=1.2.3.4.
	// Must succeed and consume RemoteAddr bucket for 5.6.7.8.
	req1 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req1.RemoteAddr = "5.6.7.8:1234"
	req1.Header.Set("X-Forwarded-For", "1.2.3.4")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("[AT-12-11-1] first request: expected 200, got %d", rr1.Code)
	}

	// Second request: same RemoteAddr=5.6.7.8, different XFF=9.9.9.9.
	// If the limiter used XFF as the key, this would get a fresh bucket (pass).
	// Correct behavior: it must use RemoteAddr → bucket exhausted → 429.
	req2 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req2.RemoteAddr = "5.6.7.8:1234"
	req2.Header.Set("X-Forwarded-For", "9.9.9.9")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-11-1] second request with different XFF but same RemoteAddr: "+
			"expected 429 (RemoteAddr bucket exhausted), got %d — "+
			"XFF must be ignored when trustedProxy=false", rr2.Code)
	}
}

// AT-12-11-2 — Rate limiter uses XFF rightmost entry when trustedProxy=true
//
// AC-F2-2: Given NewIPRateLimiter(cfg, true), when request has
//          X-Forwarded-For: 1.2.3.4, 10.0.0.1,
//          then rate limiting keys on 10.0.0.1 (the rightmost / proxy-appended entry).
func TestRateLimit_TrustedProxyTrue_UsesXFF(t *testing.T) {
	t.Parallel()

	cfg := ratelimit.Config{
		Rate:  rate.Limit(1),
		Burst: 1,
	}
	// trustedProxy=true: must use XFF rightmost entry.
	handler := ratelimit.NewIPRateLimiter(cfg, true)(okHandler)

	// First request: XFF: 1.2.3.4, 10.0.0.1 → key = 10.0.0.1
	req1 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req1.RemoteAddr = "10.0.0.1:5000"
	req1.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("[AT-12-11-2] first request: expected 200, got %d", rr1.Code)
	}

	// Second request: same XFF rightmost entry (10.0.0.1) → same bucket → 429.
	req2 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req2.RemoteAddr = "10.0.0.2:5000" // different RemoteAddr, but same XFF rightmost
	req2.Header.Set("X-Forwarded-For", "8.8.8.8, 10.0.0.1")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-11-2] second request: same XFF rightmost (10.0.0.1) → "+
			"expected 429 (XFF bucket exhausted), got %d", rr2.Code)
	}
}

// AT-12-11-3 — Bypass protection: 11 requests from same RemoteAddr with different XFF
//              are all counted against RemoteAddr when trustedProxy=false.
//
// AC-F2-3: Given trustedProxy=false, burst=10, attacker sends 11 requests from
//          RemoteAddr=5.6.7.8 each with a different X-Forwarded-For,
//          then all 11 are counted against 5.6.7.8 and the 11th is rate-limited.
func TestRateLimit_TrustedProxyFalse_BypassNotPossible(t *testing.T) {
	t.Parallel()

	cfg := ratelimit.Config{
		Rate:  rate.Limit(10),
		Burst: 10,
	}
	handler := ratelimit.NewIPRateLimiter(cfg, false)(okHandler)

	const remoteAddr = "5.6.7.8:4321"

	// Requests 1-10: same RemoteAddr, different XFF each time — must all pass.
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i)) // different spoofed IP each time
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("[AT-12-11-3] request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// Request 11: same RemoteAddr, yet another different XFF → must be 429.
	req11 := httptest.NewRequest(http.MethodPost, "/_matrix/media/v3/upload", nil)
	req11.RemoteAddr = remoteAddr
	req11.Header.Set("X-Forwarded-For", "10.0.0.99") // attacker tries a fresh spoofed IP
	rr11 := httptest.NewRecorder()
	handler.ServeHTTP(rr11, req11)
	if rr11.Code != http.StatusTooManyRequests {
		t.Fatalf("[AT-12-11-3] 11th request with different XFF from same RemoteAddr: "+
			"expected 429 (RemoteAddr bucket exhausted), got %d — "+
			"XFF spoofing bypass must not work when trustedProxy=false", rr11.Code)
	}
}

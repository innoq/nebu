package ratelimit

// Story 12.10 — Per-IP Rate Limiting on Media Gateway
//
// Acceptance Criteria implemented:
//   AC-1 — upload tier: 10 req/s per IP (burst 5), 429 M_LIMIT_EXCEEDED on exhaustion
//   AC-2 — download/thumbnail tier: 100 req/s per IP (burst 20), same 429 response
//   AC-3 — per-IP token buckets (sync.Map keyed by IP), not shared across clients
//   AC-4 — token bucket refills over time (golang.org/x/time/rate token bucket)
//
// IP extraction:
//   X-Forwarded-For last entry (proxy-appended) used when header has 2+ values.
//   Falls back to RemoteAddr (port stripped) when header absent or single-entry.
//   This matches the rightmost-minus-1 strategy from gateway/internal/middleware/ratelimit.go.
//
// Memory management:
//   Background cleanup goroutine removes entries not seen for >5 minutes to prevent
//   unbounded sync.Map growth. Runs every 1 minute.
//
// Dev/test escape hatch:
//   NEBU_RATE_LIMIT_DISABLED=true makes NewIPRateLimiter return a no-op wrapper.

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Config holds the token-bucket parameters for one rate-limit tier.
type Config struct {
	// Rate is the steady-state refill speed in tokens per second.
	Rate rate.Limit

	// Burst is the maximum number of tokens that can accumulate.
	// It equals the maximum number of back-to-back requests allowed.
	Burst int
}

// ipEntry holds the per-IP rate limiter and the last-seen timestamp.
// mu protects lastSeen updates from concurrent writers.
type ipEntry struct {
	limiter  *rate.Limiter
	mu       sync.Mutex
	lastSeen time.Time
}

// IPRateLimiter is a per-IP token-bucket middleware for the media gateway.
type IPRateLimiter struct {
	limiters sync.Map // map[string]*ipEntry
	rate     rate.Limit
	burst    int
}

// newIPRateLimiterCore creates the core struct and starts the cleanup goroutine.
// Exported only for testing; callers should use NewIPRateLimiter.
func newCore(cfg Config) *IPRateLimiter {
	rl := &IPRateLimiter{
		rate:  cfg.Rate,
		burst: cfg.Burst,
	}
	// Background cleanup: remove stale entries every minute.
	go rl.cleanupLoop(1*time.Minute, 5*time.Minute)
	return rl
}

// cleanupLoop runs forever, calling cleanupOnce at the given interval.
// Stale entries are those not seen for longer than maxAge.
// The goroutine is intentionally not stoppable — it runs for the process lifetime.
func (rl *IPRateLimiter) cleanupLoop(interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		rl.cleanupOnce(maxAge)
	}
}

// cleanupOnce deletes all entries not seen for longer than maxAge.
func (rl *IPRateLimiter) cleanupOnce(maxAge time.Duration) {
	threshold := time.Now().Add(-maxAge)
	rl.limiters.Range(func(key, val any) bool {
		entry := val.(*ipEntry)
		entry.mu.Lock()
		stale := entry.lastSeen.Before(threshold)
		entry.mu.Unlock()
		if stale {
			rl.limiters.Delete(key)
		}
		return true
	})
}

// getOrCreate returns the rate.Limiter for the given IP, creating one if needed.
// LoadOrStore prevents a TOCTOU race where two goroutines simultaneously create
// separate limiters for the same new IP.
func (rl *IPRateLimiter) getOrCreate(ip string) *rate.Limiter {
	entry := &ipEntry{
		limiter:  rate.NewLimiter(rl.rate, rl.burst),
		lastSeen: time.Now(),
	}
	actual, _ := rl.limiters.LoadOrStore(ip, entry)
	e := actual.(*ipEntry)
	// Update lastSeen on every access.
	e.mu.Lock()
	e.lastSeen = time.Now()
	e.mu.Unlock()
	return e.limiter
}

// NewIPRateLimiter returns a per-IP token-bucket middleware function.
//
// The returned function wraps an http.Handler and enforces the rate limit.
// On exhaustion it returns HTTP 429 with:
//
//	{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}
//	Retry-After: <seconds until next token available>
//
// Dev/test escape hatch: NEBU_RATE_LIMIT_DISABLED=true returns a no-op wrapper.
func NewIPRateLimiter(cfg Config) func(http.Handler) http.Handler {
	if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
		return func(next http.Handler) http.Handler { return next }
	}

	rl := newCore(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			lim := rl.getOrCreate(ip)

			reservation := lim.Reserve()
			if !reservation.OK() {
				// Burst is 0 — should not happen with valid Config.
				writeTooManyRequests(w, 1)
				return
			}

			delay := reservation.Delay()
			if delay > 0 {
				// Token not immediately available — cancel and return 429.
				reservation.Cancel()
				retryAfter := int(math.Ceil(delay.Seconds()))
				if retryAfter < 1 {
					retryAfter = 1
				}
				writeTooManyRequests(w, retryAfter)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP returns the client IP address for rate-limiting purposes.
//
// Strategy (rightmost-minus-1, spoofing-resistant):
//   - If X-Forwarded-For contains 2+ comma-separated entries, the last entry
//     (proxy-appended) is returned. The leftmost entries are client-controlled
//     and MUST NOT be trusted.
//   - If the header contains only one entry or is absent, RemoteAddr is used
//     (port stripped).
//
// IMPORTANT: the reverse proxy MUST strip any X-Forwarded-For header that
// arrives from untrusted external clients before forwarding the request.
func extractClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) >= 2 {
			// Rightmost entry is proxy-appended — use it as the client IP.
			return strings.TrimSpace(ips[len(ips)-1])
		}
		// Single entry: could be client-supplied — fall through to RemoteAddr.
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeTooManyRequests writes a 429 Matrix error response with a Retry-After header.
//
// Response format (Matrix CS API §rate-limiting):
//
//	HTTP 429
//	Content-Type: application/json
//	Retry-After: <retryAfterSeconds>
//	{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}
func writeTooManyRequests(w http.ResponseWriter, retryAfterSeconds int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"errcode": "M_LIMIT_EXCEEDED",
		"error":   "Too many requests",
	})
}

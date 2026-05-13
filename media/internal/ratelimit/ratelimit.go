package ratelimit

// Story 12.10 — Per-IP Rate Limiting on Media Gateway
// Story 12.11 — SEC Fix F-2: XFF trusted-proxy gate
// Story 12.14 — Full Graceful Shutdown: ctx-aware cleanupLoop
//
// Acceptance Criteria implemented:
//   AC-1 — upload tier: 10 req/s per IP (burst 5), 429 M_LIMIT_EXCEEDED on exhaustion
//   AC-2 — download/thumbnail tier: 100 req/s per IP (burst 20), same 429 response
//   AC-3 — per-IP token buckets (sync.Map keyed by IP), not shared across clients
//   AC-4 — token bucket refills over time (golang.org/x/time/rate token bucket)
//
// IP extraction (Story 12.11 — AC-F2-1..3):
//   trustedProxy=false (default): always use RemoteAddr, ignore X-Forwarded-For.
//     An attacker cannot bypass rate limiting by sending different XFF headers.
//   trustedProxy=true: use rightmost entry from X-Forwarded-For when header has
//     2+ entries (proxy-appended entry), falling back to RemoteAddr otherwise.
//     This matches the pattern in gateway/internal/middleware/ratelimit.go.
//
// Memory management:
//   Background cleanup goroutine removes entries not seen for >5 minutes to prevent
//   unbounded sync.Map growth. Runs every 1 minute.
//   Story 12.14: cleanupLoop now accepts ctx context.Context so the goroutine
//   exits cleanly when SIGTERM cancels the context (no goroutine leak on shutdown).
//
// Dev/test escape hatch:
//   NEBU_RATE_LIMIT_DISABLED=true makes NewIPRateLimiter return a no-op wrapper.

import (
	"context"
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
	limiters     sync.Map // map[string]*ipEntry
	rate         rate.Limit
	burst        int
	trustedProxy bool // Story 12.11: gate XFF extraction behind this flag
}

// newCore creates the core struct and starts the cleanup goroutine.
// Story 12.14 — ctx is threaded through so cleanupLoop exits on SIGTERM (AC-3).
func newCore(ctx context.Context, cfg Config, trustedProxy bool) *IPRateLimiter {
	rl := &IPRateLimiter{
		rate:         cfg.Rate,
		burst:        cfg.Burst,
		trustedProxy: trustedProxy,
	}
	// Background cleanup: remove stale entries every minute.
	// Story 12.14: ctx cancellation (SIGTERM) stops the goroutine cleanly.
	go rl.cleanupLoop(ctx, 1*time.Minute, 5*time.Minute)
	return rl
}

// cleanupLoop runs until ctx is cancelled, calling cleanupOnce at the given interval.
// Stale entries are those not seen for longer than maxAge.
// Story 12.14: ctx cancellation exits the goroutine cleanly (no goroutine leak on shutdown).
func (rl *IPRateLimiter) cleanupLoop(ctx context.Context, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanupOnce(maxAge)
		case <-ctx.Done():
			return
		}
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

// clientIP returns the client IP address for rate-limiting purposes.
//
// Story 12.11 — trustedProxy gate (AC-F2-1..3):
//
//   - trustedProxy=false (default, secure): always use RemoteAddr (port stripped).
//     X-Forwarded-For is completely ignored. An attacker cannot bypass rate limiting
//     by sending different XFF header values — all requests from the same TCP peer
//     are counted against the same RemoteAddr bucket.
//
//   - trustedProxy=true: if X-Forwarded-For contains 2+ comma-separated entries,
//     the rightmost entry (proxy-appended) is used as the rate-limit key.
//     Falls back to RemoteAddr when the header has 0 or 1 entry.
//     IMPORTANT: the reverse proxy MUST strip any X-Forwarded-For header received
//     from external clients before forwarding the request to the media gateway.
func (rl *IPRateLimiter) clientIP(r *http.Request) string {
	if rl.trustedProxy {
		xff := r.Header.Get("X-Forwarded-For")
		if xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) >= 2 {
				// Rightmost entry is proxy-appended — use it as the client IP.
				return strings.TrimSpace(ips[len(ips)-1])
			}
			// Single entry: could be client-supplied — fall through to RemoteAddr.
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewIPRateLimiter returns a per-IP token-bucket middleware function.
//
// The ctx parameter is used to stop the background cleanup goroutine when
// SIGTERM cancels the context (Story 12.14, AC-3). Pass the main signal context.
//
// The trustedProxy parameter controls X-Forwarded-For handling (Story 12.11):
//   - false (default): XFF is ignored; rate limiting always keys on RemoteAddr.
//     Use this unless the media gateway sits behind a trusted reverse proxy.
//   - true: XFF rightmost entry is used when present; falls back to RemoteAddr.
//     Only use when the gateway is behind a trusted proxy that controls XFF.
//
// On exhaustion the middleware returns HTTP 429 with:
//
//	{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests"}
//	Retry-After: <seconds until next token available>
//
// Dev/test escape hatch: NEBU_RATE_LIMIT_DISABLED=true returns a no-op wrapper.
func NewIPRateLimiter(ctx context.Context, cfg Config, trustedProxy bool) func(http.Handler) http.Handler {
	if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
		return func(next http.Handler) http.Handler { return next }
	}

	rl := newCore(ctx, cfg, trustedProxy)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := rl.clientIP(r)
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

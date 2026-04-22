package middleware

// Story 5.21 — Per-IP Rate Limiting for Public Endpoints
//
// Acceptance Criteria implemented:
//   AC 1 — per-IP token-bucket with LRU eviction (capacity 10 000)
//   AC 2 — configurable rate + burst via RateLimitConfig
//   AC 3 — 429 M_LIMIT_EXCEEDED + Retry-After header on exhaustion
//   AC 4 — trustedProxy=true: extract IP from X-Forwarded-For (rightmost-minus-1)
//   AC 6 — NEBU_RATE_LIMIT_DISABLED=true → no-op middleware (dev/test)
//
// Security note — X-Forwarded-For trust model (trustedProxy=true):
//   The reverse proxy MUST strip any X-Forwarded-For header that arrives from
//   external clients before forwarding the request.  Only the proxy-appended IP
//   (the rightmost entry) can be trusted; the leftmost entries are client-controlled
//   and MUST NOT be used for rate limiting (spoofing risk).
//   When trustedProxy=true, extractClientIP uses the rightmost-minus-1 strategy:
//   the IP appended by the directly-connected trusted proxy, which represents the
//   real client IP as seen by the proxy.  If the header contains only one entry
//   (no prior proxies), RemoteAddr is used instead.

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

// RateLimitConfig holds the token-bucket parameters for one rate-limit tier.
type RateLimitConfig struct {
	// Rate is the steady-state refill speed (tokens per second).
	// Example: rate.Limit(5.0/60) = 5 requests per minute.
	Rate rate.Limit

	// Burst is the maximum number of tokens that can accumulate.
	// It also equals the maximum burst of back-to-back requests allowed.
	Burst int
}

// rateLimitTotal counts rate-limit decisions by tier ("strict", "medium", "loose", …)
// and decision ("allow" or "deny").
var rateLimitTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nebu_rate_limit_total",
		Help: "Total rate limit decisions by tier and result",
	},
	[]string{"tier", "decision"},
)

func init() {
	prometheus.MustRegister(rateLimitTotal)
}

// lruCapacity is the maximum number of per-IP limiter entries kept in memory.
// LRU eviction automatically removes the least-recently-seen IPs when full.
const lruCapacity = 10_000

// extractClientIP returns the IP address of the originating client.
//
// When trustedProxy=false the port-stripped RemoteAddr is returned directly.
//
// When trustedProxy=true the X-Forwarded-For header is consulted:
//
//	X-Forwarded-For: <client>, <proxy1>, <proxy2>
//
// The rightmost-minus-1 strategy is applied: the entry appended by the
// directly-connected trusted proxy is taken (ips[len-1]), which reflects
// the real client IP as observed by the proxy.  The leftmost entries are
// forwarded verbatim from the client and MUST NOT be trusted.
//
// IMPORTANT: the reverse proxy MUST strip any X-Forwarded-For header that
// arrives from external clients.  Only ips added by the proxy itself are safe.
//
// If the header contains only a single entry (no prior proxies), RemoteAddr
// is used as fallback (the single hop is the proxy itself, not a client IP we
// want to rate-limit individually).
func extractClientIP(r *http.Request, trustedProxy bool) string {
	if trustedProxy {
		xff := r.Header.Get("X-Forwarded-For")
		if ips := strings.Split(xff, ","); len(ips) >= 2 {
			// rightmost-minus-1: the IP the trusted proxy appended for the client.
			return strings.TrimSpace(ips[len(ips)-1])
		}
		// Only one entry (or header absent) — fall through to RemoteAddr.
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewIPRateLimiter returns a per-IP token-bucket middleware.
//
// The tier parameter names the rate-limit tier (e.g. "strict", "medium",
// "loose") and is used as a Prometheus label so that allow/deny counts are
// observable per tier.
//
// IP extraction:
//   - trustedProxy=false: use r.RemoteAddr (host:port, port stripped)
//   - trustedProxy=true:  rightmost-minus-1 from X-Forwarded-For (see
//     extractClientIP for full semantics), falling back to RemoteAddr.
//
// 429 response format (Matrix CS API):
//
//	{"errcode":"M_LIMIT_EXCEEDED","error":"Rate limit exceeded"}
//	Retry-After: <seconds until next token>
//
// Dev/test escape hatch: set NEBU_RATE_LIMIT_DISABLED=true to make the
// middleware a no-op without touching the caller code.
func NewIPRateLimiter(cfg RateLimitConfig, trustedProxy bool, tier string) func(http.Handler) http.Handler {
	// No-op mode for development / integration tests.
	if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
		return func(next http.Handler) http.Handler { return next }
	}

	// lru.New never returns an error for a positive size.
	cache, _ := lru.New(lruCapacity)

	// mu guards the Get+Add sequence to prevent a TOCTOU race where two
	// concurrent requests for the same new IP each create separate limiters,
	// allowing one request to bypass the shared bucket.
	var mu sync.Mutex

	getLimiter := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		if val, ok := cache.Get(ip); ok {
			return val.(*rate.Limiter)
		}
		lim := rate.NewLimiter(cfg.Rate, cfg.Burst)
		cache.Add(ip, lim)
		return lim
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r, trustedProxy)
			lim := getLimiter(ip)

			reservation := lim.Reserve()
			if !reservation.OK() {
				// Burst is 0 or rate is 0 — should not happen with valid config.
				rateLimitTotal.WithLabelValues(tier, "deny").Inc()
				writeTooManyRequests(w, 1)
				return
			}

			delay := reservation.Delay()
			if delay > 0 {
				// Token not immediately available — cancel the reservation and
				// return 429 with Retry-After set to the wait duration.
				reservation.Cancel()
				retryAfter := int(math.Ceil(delay.Seconds()))
				if retryAfter < 1 {
					retryAfter = 1
				}
				rateLimitTotal.WithLabelValues(tier, "deny").Inc()
				writeTooManyRequests(w, retryAfter)
				return
			}

			rateLimitTotal.WithLabelValues(tier, "allow").Inc()
			next.ServeHTTP(w, r)
		})
	}
}

// writeTooManyRequests writes a 429 Matrix error response with a Retry-After header.
func writeTooManyRequests(w http.ResponseWriter, retryAfterSeconds int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"errcode": "M_LIMIT_EXCEEDED",
		"error":   "Rate limit exceeded",
	})
}

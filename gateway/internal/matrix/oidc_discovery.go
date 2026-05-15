package matrix

// ─── Story 13-7: MSC2965 OIDC Discovery Endpoints ────────────────────────────
//
// AuthIssuerHandler returns {"issuer":"<cfg.OIDCIssuer>"} — unauthenticated.
// AuthMetadataHandler proxies /.well-known/openid-configuration from the OIDC
// provider and caches the response for 5 minutes (TTL cache, thread-safe).
//
// Both handlers are registered on the unstable MSC2965 path and the stable v1
// path:
//   GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer
//   GET /_matrix/client/v1/auth_issuer
//   GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata
//   GET /_matrix/client/v1/auth_metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/nebu/nebu/internal/config"
)

// metadataCache is a simple TTL cache for the OIDC discovery document.
// Reads are served under RLock; writes (cache miss) acquire the full lock.
type metadataCache struct {
	mu        sync.RWMutex
	body      []byte
	expiresAt time.Time
	ttl       time.Duration
}

// get returns the cached body if it is still within its TTL, otherwise false.
func (c *metadataCache) get() ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.body) > 0 && time.Now().Before(c.expiresAt) {
		return c.body, true
	}
	return nil, false
}

// set stores body in the cache and advances the expiry by the configured TTL.
func (c *metadataCache) set(body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body = body
	c.expiresAt = time.Now().Add(c.ttl)
}

// AuthIssuerHandler returns an http.HandlerFunc that responds to
// GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer (and the v1
// stable path). The response is always 200 with {"issuer":"<cfg.OIDCIssuer>"}.
// No authentication is required (MSC2965 §3.1).
func AuthIssuerHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"issuer": cfg.OIDCIssuer}) //nolint:errcheck
	}
}

// AuthMetadataHandler returns an http.HandlerFunc that proxies the OIDC
// provider's /.well-known/openid-configuration document. The document is
// cached for 5 minutes to avoid hammering the OIDC provider on every request.
//
// If the provider is unreachable or returns a 5xx response the handler replies
// with 503 and {"errcode":"M_UNAVAILABLE","error":"..."} — compliant with the
// Matrix error format so that clients degrade gracefully instead of crashing.
//
// httpClient is injectable for unit tests (pass a mock httptest.Server client).
// Production callers should pass http.DefaultClient or a client with sensible
// timeouts.
func AuthMetadataHandler(cfg *config.Config, httpClient *http.Client) http.HandlerFunc {
	cache := &metadataCache{ttl: 5 * time.Minute}
	return func(w http.ResponseWriter, r *http.Request) {
		// Serve from cache when available.
		if body, ok := cache.get(); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body) //nolint:errcheck
			return
		}

		// Cache miss: fetch from the OIDC provider.
		// Pass the request context so that a disconnecting client cancels the upstream fetch.
		discoveryURL := fmt.Sprintf("%s/.well-known/openid-configuration", cfg.OIDCIssuer)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, discoveryURL, nil)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"errcode": "M_UNAVAILABLE",
				"error":   "OIDC provider is currently unavailable",
			})
			return
		}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode >= http.StatusInternalServerError {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"errcode": "M_UNAVAILABLE",
				"error":   "OIDC provider is currently unavailable",
			})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"errcode": "M_UNAVAILABLE",
				"error":   "OIDC provider is currently unavailable",
			})
			return
		}

		cache.set(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body) //nolint:errcheck
	}
}

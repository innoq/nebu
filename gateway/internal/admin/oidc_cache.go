package admin

import (
	"context"
	"sync"
	"time"
)

// oidcProvider is the interface for an OIDC provider as used by admin/auth.go.
// The production implementation is *oidc.Provider; tests inject a fake.
type oidcProvider interface{}

// cacheEntry holds a cached provider or a cached error.
type cacheEntry struct {
	provider  oidcProvider
	err       error
	cachedAt  time.Time
	isNeg     bool // true if this is a negative (error) cache entry
}

// oidcProviderCache caches OIDC provider instances keyed by issuer URL.
// Positive entries are held for ttl; negative (error) entries for negativeTTL.
type oidcProviderCache struct {
	mu          sync.Mutex
	entries     map[string]*cacheEntry
	newFn       func(ctx context.Context, issuer string) (oidcProvider, error)
	ttl         time.Duration
	negativeTTL time.Duration
}

// newOIDCProviderCache returns a new cache backed by fn for provider construction.
// Default TTL is 10 minutes; default negative TTL is 30 seconds.
func newOIDCProviderCache(fn func(ctx context.Context, issuer string) (oidcProvider, error)) *oidcProviderCache {
	return &oidcProviderCache{
		entries:     make(map[string]*cacheEntry),
		newFn:       fn,
		ttl:         10 * time.Minute,
		negativeTTL: 30 * time.Second,
	}
}

// load returns a cached oidcProvider for the given issuer, constructing one if necessary.
// Negative cache: if the last construction attempt failed within negativeTTL, the cached
// error is returned without calling newFn again.
//
// The mutex is released before calling newFn (network I/O) so that cache hits for other
// issuers are not blocked during slow OIDC discovery.
func (c *oidcProviderCache) load(ctx context.Context, issuer string) (oidcProvider, error) {
	c.mu.Lock()

	now := time.Now()

	if entry, ok := c.entries[issuer]; ok {
		if entry.isNeg {
			if now.Sub(entry.cachedAt) < c.negativeTTL {
				c.mu.Unlock()
				return nil, entry.err
			}
			// Negative TTL expired — fall through to retry.
			delete(c.entries, issuer)
		} else {
			if now.Sub(entry.cachedAt) < c.ttl {
				c.mu.Unlock()
				return entry.provider, nil
			}
			// Positive TTL expired — fall through to refresh.
			delete(c.entries, issuer)
		}
	}

	c.mu.Unlock()

	// Call newFn without holding the lock — OIDC discovery is a network call.
	// Concurrent calls for the same issuer may both fetch; the last writer wins.
	provider, err := c.newFn(ctx, issuer)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.entries[issuer] = &cacheEntry{err: err, cachedAt: now, isNeg: true}
		return nil, err
	}

	c.entries[issuer] = &cacheEntry{provider: provider, cachedAt: now}
	return provider, nil
}

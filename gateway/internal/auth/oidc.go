package auth

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/validate"
)

// Provider wraps go-oidc discovery with startup-failure tolerance and
// hourly JWKS refresh for key rotation support.
type Provider struct {
	mu     sync.RWMutex
	inner  *oidc.Provider
	issuer string
}

// NewProvider performs OIDC discovery. If the provider is unreachable, it
// logs a warning and returns a Provider with nil Inner() — the gateway starts
// normally but token validation will fail until the next background refresh.
func NewProvider(ctx context.Context, issuer string) *Provider {
	p := &Provider{issuer: issuer}
	if err := validate.IssuerURL(issuer); err != nil {
		log.Printf("OIDC provider: invalid issuer URL — token validation will fail: %v", err)
		go p.refresh()
		return p
	}
	if err := p.discover(ctx); err != nil {
		log.Printf("OIDC provider unreachable — token validation will fail until resolved: %v", err)
	}
	go p.refresh()
	return p
}

// Inner returns the underlying *oidc.Provider for use by the JWT validation
// middleware (Story 2.4). Returns nil if discovery has not yet succeeded.
func (p *Provider) Inner() *oidc.Provider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.inner
}

func (p *Provider) discover(ctx context.Context) error {
	provider, err := oidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.inner = provider
	p.mu.Unlock()
	return nil
}

func (p *Provider) refresh() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := p.discover(ctx); err != nil {
			log.Printf("OIDC provider unreachable — token validation will fail until resolved: %v", err)
		}
		cancel()
	}
}

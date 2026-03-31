package middleware

import "time"

// TokenStore tracks explicitly invalidated tokens (logout denylist).
// Production implementation: db.PostgresTokenStore (shared across instances, survives restarts).
// Test implementation: Denylist (in-memory).
type TokenStore interface {
	// Invalidate marks rawToken as logged out until expiresAt.
	Invalidate(rawToken string, expiresAt time.Time) error
	// IsInvalidated returns true if rawToken was explicitly invalidated and has not yet expired.
	IsInvalidated(rawToken string) bool
}

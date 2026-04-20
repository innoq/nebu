package admin

import (
	"context"
	"time"
)

// AdminSession represents one row in the admin_sessions table.
type AdminSession struct {
	SID       string
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time // nil means not revoked
}

// AdminSessionStore is the injectable interface for admin session persistence.
// Production implementation: *PostgresAdminSessionStore in gateway/internal/db/.
// Test implementation: *fakeAdminSessionStore in session_revocation_test.go.
type AdminSessionStore interface {
	// Create inserts a new session row and returns the generated SID.
	Create(ctx context.Context, userID string, expiresAt time.Time) (sid string, err error)
	// Get fetches a session by SID. Returns (nil, nil) when the row does not exist.
	Get(ctx context.Context, sid string) (*AdminSession, error)
	// Revoke sets revoked_at = NOW() for the given SID.
	Revoke(ctx context.Context, sid string) error
}

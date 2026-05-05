package admin

import (
	"context"
	"time"
)

// AdminSession represents one row in the admin_sessions table.
type AdminSession struct {
	SID                   string
	UserID                string
	ExpiresAt             time.Time
	RevokedAt             *time.Time // nil means not revoked
	EncryptedRefreshToken string     // AES-256-GCM encrypted refresh token; empty string means no refresh token stored
}

// AdminSessionStore is the injectable interface for admin session persistence.
// Production implementation: *PostgresAdminSessionStore in gateway/internal/db/.
// Test implementation: *fakeAdminSessionStore in session_revocation_test.go.
type AdminSessionStore interface {
	// Create inserts a new session row and returns the generated SID.
	// refreshToken is the AES-256-GCM encrypted refresh token; pass "" if none was issued.
	Create(ctx context.Context, userID string, expiresAt time.Time, refreshToken string) (sid string, err error)
	// Get fetches a session by SID. Returns (nil, nil) when the row does not exist.
	Get(ctx context.Context, sid string) (*AdminSession, error)
	// Revoke sets revoked_at = NOW() for the given SID.
	Revoke(ctx context.Context, sid string) error
	// UpdateExpiry updates the expires_at and encrypted refresh token for the given SID.
	// Used by the silent refresh middleware to slide the session expiry after a successful token refresh.
	UpdateExpiry(ctx context.Context, sid string, expiresAt time.Time, encryptedRefreshToken string) error
}

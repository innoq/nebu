package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/nebu/nebu/internal/admin"
)

// PostgresAdminSessionStore implements admin.AdminSessionStore using PostgreSQL.
type PostgresAdminSessionStore struct {
	db *sql.DB
}

// NewPostgresAdminSessionStore creates a new PostgresAdminSessionStore.
func NewPostgresAdminSessionStore(db *sql.DB) *PostgresAdminSessionStore {
	return &PostgresAdminSessionStore{db: db}
}

// generateSID generates a cryptographically random 32-byte SID encoded as base64url (no padding).
func generateSID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating SID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create inserts a new session row into admin_sessions and returns the generated SID.
func (s *PostgresAdminSessionStore) Create(ctx context.Context, userID string, expiresAt time.Time) (string, error) {
	sid, err := generateSID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO admin_sessions (sid, user_id, created_at, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		sid, userID, time.Now(), expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("inserting admin session: %w", err)
	}
	return sid, nil
}

// Get fetches a session by SID. Returns (nil, nil) when the row does not exist.
func (s *PostgresAdminSessionStore) Get(ctx context.Context, sid string) (*admin.AdminSession, error) {
	var sess admin.AdminSession
	var revokedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT sid, user_id, expires_at, revoked_at
		 FROM admin_sessions WHERE sid = $1`,
		sid,
	).Scan(&sess.SID, &sess.UserID, &sess.ExpiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying admin session: %w", err)
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		sess.RevokedAt = &t
	}
	return &sess, nil
}

// Revoke sets revoked_at = NOW() for the given SID.
func (s *PostgresAdminSessionStore) Revoke(ctx context.Context, sid string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admin_sessions SET revoked_at = $1 WHERE sid = $2`,
		time.Now(), sid,
	)
	if err != nil {
		return fmt.Errorf("revoking admin session: %w", err)
	}
	return nil
}

// CleanupExpired deletes rows where expires_at < NOW() - INTERVAL '7 days'.
// Called periodically by a background goroutine (once per hour).
func (s *PostgresAdminSessionStore) CleanupExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE expires_at < NOW() - INTERVAL '7 days'`,
	)
	if err != nil {
		return fmt.Errorf("cleaning up expired admin sessions: %w", err)
	}
	return nil
}

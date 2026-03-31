package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"time"
)

// PostgresTokenStore is the production TokenStore implementation.
// Invalidated tokens are persisted in the invalidated_tokens table,
// surviving gateway restarts and shared across all gateway instances.
type PostgresTokenStore struct {
	db *sql.DB
}

func NewPostgresTokenStore(db *sql.DB) *PostgresTokenStore {
	return &PostgresTokenStore{db: db}
}

// Invalidate inserts the token hash into invalidated_tokens.
// Uses ON CONFLICT DO NOTHING — safe to call multiple times for the same token.
func (s *PostgresTokenStore) Invalidate(rawToken string, expiresAt time.Time) error {
	hash := pgTokenHash(rawToken)
	expiresAtMs := expiresAt.UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO invalidated_tokens (token_hash, expires_at) VALUES ($1, $2) ON CONFLICT (token_hash) DO NOTHING`,
		hash, expiresAtMs,
	)
	if err != nil {
		slog.Error("failed to invalidate token", "err", err)
	}
	return err
}

// IsInvalidated returns true if the token exists in invalidated_tokens and has not expired.
// Expired rows are cleaned up lazily.
func (s *PostgresTokenStore) IsInvalidated(rawToken string) bool {
	hash := pgTokenHash(rawToken)
	nowMs := time.Now().UnixMilli()

	var expiresAt int64
	err := s.db.QueryRow(
		`SELECT expires_at FROM invalidated_tokens WHERE token_hash = $1`,
		hash,
	).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		slog.Error("failed to check invalidated token", "err", err)
		return false
	}

	if nowMs > expiresAt {
		// Lazy cleanup of expired entry
		_, _ = s.db.Exec(`DELETE FROM invalidated_tokens WHERE token_hash = $1`, hash)
		return false
	}
	return true
}

func pgTokenHash(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}

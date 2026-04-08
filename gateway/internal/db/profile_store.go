package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresProfileDB implements matrix.ProfileDB using a PostgreSQL connection.
// Used for direct profile reads in GET /profile/{userId} (no gRPC round-trip needed).
type PostgresProfileDB struct {
	db *sql.DB
}

// NewPostgresProfileDB constructs a PostgresProfileDB backed by the given *sql.DB.
func NewPostgresProfileDB(db *sql.DB) *PostgresProfileDB {
	return &PostgresProfileDB{db: db}
}

// GetProfile retrieves the public-facing profile for userID from the profiles table.
// Returns matrix.ErrProfileNotFound when no row exists.
func (p *PostgresProfileDB) GetProfile(ctx context.Context, userID string) (*matrix.ProfileData, error) {
	row := p.db.QueryRowContext(ctx,
		"SELECT displayname, avatar_url FROM profiles WHERE user_id = $1", userID)
	var displayname, avatarURL sql.NullString
	if err := row.Scan(&displayname, &avatarURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, matrix.ErrProfileNotFound
		}
		return nil, err
	}
	return &matrix.ProfileData{
		DisplayName: displayname.String,
		AvatarURL:   avatarURL.String,
	}, nil
}

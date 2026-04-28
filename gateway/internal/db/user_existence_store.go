package db

import (
	"context"
	"database/sql"
	"errors"
)

// PostgresUserExistenceChecker implements matrix.UserExistenceChecker using a PostgreSQL connection.
// It checks whether a Matrix user ID exists in the users table.
type PostgresUserExistenceChecker struct {
	db *sql.DB
}

// NewPostgresUserExistenceChecker constructs a PostgresUserExistenceChecker backed by the given *sql.DB.
func NewPostgresUserExistenceChecker(db *sql.DB) *PostgresUserExistenceChecker {
	return &PostgresUserExistenceChecker{db: db}
}

// UserExists returns true if a row for the given userID exists in the users table.
// Returns false (not an error) when the user is not found.
func (c *PostgresUserExistenceChecker) UserExists(ctx context.Context, userID string) (bool, error) {
	var exists int
	err := c.db.QueryRowContext(ctx,
		`SELECT 1 FROM users WHERE user_id = $1`,
		userID,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

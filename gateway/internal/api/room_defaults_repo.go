//go:build go1.22

// Package api provides the RoomDefaultsRepository interface and its PostgreSQL
// implementation for the server-wide room defaults Admin API (Story 6.8).
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RoomDefaultsRepository abstracts database access for server-wide room default configuration.
// The interface is defined here so that unit tests can provide a mock implementation
// without a real PostgreSQL connection.
//
// UpsertRoomDefaults updates the single row in the room_defaults table (id=1).
// GetRoomDefaults reads the current defaults from the room_defaults table (id=1).
type RoomDefaultsRepository interface {
	UpsertRoomDefaults(ctx context.Context, maxMembers int, visibility string) error
	GetRoomDefaults(ctx context.Context) (int, string, error)
}

// dbRoomDefaultsRepo is the real PostgreSQL implementation of RoomDefaultsRepository.
type dbRoomDefaultsRepo struct {
	db *sql.DB
}

// NewRoomDefaultsRepo constructs a new DB-backed RoomDefaultsRepository.
func NewRoomDefaultsRepo(db *sql.DB) RoomDefaultsRepository {
	return &dbRoomDefaultsRepo{db: db}
}

// UpsertRoomDefaults updates the single room_defaults row (id=1) with new values.
// The room_defaults table is seeded with id=1 in migration 000037, so UPDATE always
// finds the row. Returns an error if the DB operation fails.
func (r *dbRoomDefaultsRepo) UpsertRoomDefaults(ctx context.Context, maxMembers int, visibility string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE room_defaults SET default_max_members = $1, default_visibility = $2, set_at = $3 WHERE id = 1`,
		maxMembers, visibility, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("UpsertRoomDefaults: %w", err)
	}
	return nil
}

// GetRoomDefaults reads the current server-wide room defaults from the room_defaults table.
// Returns (0, "private", nil) when the table is empty (safe default for uninitialized state).
func (r *dbRoomDefaultsRepo) GetRoomDefaults(ctx context.Context) (int, string, error) {
	var maxMembers int
	var visibility string
	err := r.db.QueryRowContext(ctx,
		`SELECT default_max_members, default_visibility FROM room_defaults WHERE id = 1`).
		Scan(&maxMembers, &visibility)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "private", nil // safe default if table is empty
	}
	if err != nil {
		return 0, "", fmt.Errorf("GetRoomDefaults: %w", err)
	}
	return maxMembers, visibility, nil
}

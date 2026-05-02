package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresAccountDataDB implements matrix.AccountDataDB using a PostgreSQL connection.
// Handles both global account data (roomID = "") and per-room account data (roomID != "").
//
// The underlying table is room_account_data (migration 000029):
//
//	PRIMARY KEY (user_id, room_id, event_type)
//
// Global account data uses room_id = '' (empty string) to distinguish it from per-room data.
// Upsert semantics are provided by INSERT … ON CONFLICT DO UPDATE SET (AC6: last write wins).
type PostgresAccountDataDB struct {
	db *sql.DB
}

// NewPostgresAccountDataDB constructs a PostgresAccountDataDB backed by the given *sql.DB.
func NewPostgresAccountDataDB(db *sql.DB) *PostgresAccountDataDB {
	return &PostgresAccountDataDB{db: db}
}

// GetAccountData retrieves account data for (userID, roomID, eventType).
// roomID is empty string for global account data.
// Returns matrix.ErrAccountDataNotFound when no row exists.
func (p *PostgresAccountDataDB) GetAccountData(ctx context.Context, userID, roomID, eventType string) (json.RawMessage, error) {
	var content []byte
	err := withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`SELECT content FROM room_account_data
			 WHERE user_id = $1 AND room_id = $2 AND event_type = $3`,
			userID, roomID, eventType)
		return row.Scan(&content)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, matrix.ErrAccountDataNotFound
		}
		return nil, err
	}
	return json.RawMessage(content), nil
}

// PutAccountData upserts account data for (userID, roomID, eventType).
// roomID is empty string for global account data.
// Uses INSERT … ON CONFLICT DO UPDATE to implement last-write-wins upsert semantics (AC6).
func (p *PostgresAccountDataDB) PutAccountData(ctx context.Context, userID, roomID, eventType string, content json.RawMessage) error {
	return withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO room_account_data (user_id, room_id, event_type, content, updated_at)
			 VALUES ($1, $2, $3, $4, NOW())
			 ON CONFLICT (user_id, room_id, event_type)
			 DO UPDATE SET content = EXCLUDED.content, updated_at = NOW()`,
			userID, roomID, eventType, []byte(content))
		return err
	})
}

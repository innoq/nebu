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

// ListGlobalAccountData returns all global account data rows for userID
// (room_id = '' in room_account_data). Returns an empty slice on no rows.
//
// Story 9-24: implements matrix.GlobalAccountDataDB interface.
// Uses withUserDB to set set_config('app.user_id', userID, true) inside a
// transaction — required to satisfy the RLS policy on room_account_data
// (migration 000033 uses current_setting('app.user_id', true) for row filtering).
// Without this GUC the RLS policy silently returns zero rows.
func (p *PostgresAccountDataDB) ListGlobalAccountData(ctx context.Context, userID string) ([]matrix.GlobalAccountDataRow, error) {
	var result []matrix.GlobalAccountDataRow
	err := withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT event_type, content FROM room_account_data
			 WHERE user_id = $1 AND room_id = ''`,
			userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r matrix.GlobalAccountDataRow
			var content []byte
			if scanErr := rows.Scan(&r.EventType, &content); scanErr != nil {
				continue
			}
			r.Content = json.RawMessage(content)
			result = append(result, r)
		}
		return rows.Err()
	})
	if err != nil {
		return []matrix.GlobalAccountDataRow{}, err
	}
	if result == nil {
		result = []matrix.GlobalAccountDataRow{}
	}
	return result, nil
}

// Compile-time interface satisfaction check: *PostgresAccountDataDB must implement GlobalAccountDataDB.
var _ matrix.GlobalAccountDataDB = (*PostgresAccountDataDB)(nil)

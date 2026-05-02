package db

// ─── Story 7-30: Pushers API — PostgreSQL store ───────────────────────────────
//
// PostgresPushersDB implements matrix.PushersDB using a *sql.DB connection.
// Table: pushers (migration 000032).
//
// All queries run inside withUserDB to set SET LOCAL app.user_id = $1 for RLS (migration 000033).

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresPushersDB implements matrix.PushersDB backed by PostgreSQL.
type PostgresPushersDB struct {
	db *sql.DB
}

// NewPostgresPushersDB constructs a PostgresPushersDB from the given *sql.DB.
func NewPostgresPushersDB(db *sql.DB) *PostgresPushersDB {
	return &PostgresPushersDB{db: db}
}

// GetPushers returns all pushers registered for the given user.
func (p *PostgresPushersDB) GetPushers(ctx context.Context, userID string) ([]matrix.PusherRow, error) {
	var result []matrix.PusherRow
	err := withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT user_id, pushkey, kind, app_id, app_display_name, device_display_name, lang, data
			   FROM pushers
			  WHERE user_id = $1
			  ORDER BY id ASC`,
			userID,
		)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row matrix.PusherRow
			var data []byte
			if err := rows.Scan(
				&row.UserID,
				&row.Pushkey,
				&row.Kind,
				&row.AppID,
				&row.AppDisplayName,
				&row.DeviceDisplayName,
				&row.Lang,
				&data,
			); err != nil {
				return err
			}
			if data != nil {
				row.Data = json.RawMessage(data)
			} else {
				row.Data = json.RawMessage(`{}`)
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SetPusher creates or updates a pusher (upsert by userID+appID+pushkey).
func (p *PostgresPushersDB) SetPusher(ctx context.Context, row matrix.PusherRow) error {
	data := row.Data
	if data == nil {
		data = json.RawMessage(`{}`)
	}
	return withUserDB(ctx, p.db, row.UserID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO pushers
			       (user_id, pushkey, kind, app_id, app_display_name, device_display_name, lang, data)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (user_id, app_id, pushkey) DO UPDATE
			   SET kind                = EXCLUDED.kind,
			       app_display_name    = EXCLUDED.app_display_name,
			       device_display_name = EXCLUDED.device_display_name,
			       lang                = EXCLUDED.lang,
			       data                = EXCLUDED.data`,
			row.UserID, row.Pushkey, row.Kind, row.AppID,
			row.AppDisplayName, row.DeviceDisplayName, row.Lang, []byte(data),
		)
		return err
	})
}

// DeletePusher removes the pusher identified by (userID, appID, pushkey).
// No error is returned if the pusher does not exist (silent no-op).
func (p *PostgresPushersDB) DeletePusher(ctx context.Context, userID, appID, pushkey string) error {
	return withUserDB(ctx, p.db, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM pushers
			  WHERE user_id = $1 AND app_id = $2 AND pushkey = $3`,
			userID, appID, pushkey,
		)
		return err
	})
}

// Ensure PostgresPushersDB satisfies the matrix.PushersDB interface at compile time.
var _ interface {
	GetPushers(ctx context.Context, userID string) ([]matrix.PusherRow, error)
	SetPusher(ctx context.Context, p matrix.PusherRow) error
	DeletePusher(ctx context.Context, userID, appID, pushkey string) error
} = (*PostgresPushersDB)(nil)

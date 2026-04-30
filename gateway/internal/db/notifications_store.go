package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresNotificationsDB implements matrix.NotificationsDB using a PostgreSQL connection.
// Reads from the notifications table introduced in migration 000031.
//
// Cursor-based pagination: rows are returned newest-first (ORDER BY id DESC).
// The BIGSERIAL id is the cursor; when fromID > 0 only rows with id < fromID are returned.
// nextID is the id of the last returned row; 0 when no further rows exist beyond the page.
type PostgresNotificationsDB struct {
	db *sql.DB
}

// NewPostgresNotificationsDB constructs a PostgresNotificationsDB backed by the given *sql.DB.
func NewPostgresNotificationsDB(db *sql.DB) *PostgresNotificationsDB {
	return &PostgresNotificationsDB{db: db}
}

// GetNotifications implements matrix.NotificationsDB.
//
// Query: SELECT id, room_id, event_json, actions, read, created_at
//
//	FROM notifications
//	WHERE user_id = $1
//	  AND ($2 = 0 OR id < $2)              -- cursor filter (fromID)
//	  AND ($3 = false OR actions @> '["highlight"]')  -- highlight filter
//	ORDER BY id DESC
//	LIMIT $4 + 1                            -- fetch one extra to detect more pages
//
// If limit+1 rows are returned, the last row is dropped and nextID = last-kept row id.
// Otherwise nextID = 0 (no more pages).
func (p *PostgresNotificationsDB) GetNotifications(
	ctx context.Context,
	userID string,
	fromID int64,
	limit int,
	onlyHighlight bool,
) ([]matrix.NotificationRow, int64, error) {
	// Fetch limit+1 to detect if there is a next page.
	fetchLimit := limit + 1

	rows, err := p.db.QueryContext(ctx,
		`SELECT id, room_id, event_json, actions, read,
		        EXTRACT(EPOCH FROM created_at)::BIGINT * 1000 AS ts_ms
		   FROM notifications
		  WHERE user_id = $1
		    AND ($2 = 0 OR id < $2)
		    AND ($3 = false OR actions @> '["highlight"]'::jsonb)
		  ORDER BY id DESC
		  LIMIT $4`,
		userID, fromID, onlyHighlight, fetchLimit,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []matrix.NotificationRow
	for rows.Next() {
		var row matrix.NotificationRow
		var actionsBytes, eventBytes []byte
		if err := rows.Scan(
			&row.ID,
			&row.RoomID,
			&eventBytes,
			&actionsBytes,
			&row.Read,
			&row.TS,
		); err != nil {
			return nil, 0, err
		}
		row.ActionsRaw = json.RawMessage(actionsBytes)
		row.EventRaw = json.RawMessage(eventBytes)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Detect next page.
	var nextID int64
	if len(result) == fetchLimit {
		// Drop the extra sentinel row; nextID = id of last kept row.
		result = result[:limit]
		nextID = result[len(result)-1].ID
	}

	return result, nextID, nil
}

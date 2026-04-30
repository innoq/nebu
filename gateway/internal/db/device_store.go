package db

// ─── Story 7-26: Device Store ─────────────────────────────────────────────────
//
// PostgresDeviceStore implements the matrix.DevicesDB interface via direct
// PostgreSQL access to the sessions table.
//
// All queries include a user_id filter to enforce ownership and prevent IDOR.
// Migration 000030 adds device_display_name TEXT (nullable) to sessions.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nebu/nebu/internal/matrix"
)

// PostgresDeviceStore provides device/session operations for the Matrix
// device management handlers.
type PostgresDeviceStore struct {
	db *sql.DB
}

// NewPostgresDeviceStore constructs a PostgresDeviceStore.
func NewPostgresDeviceStore(db *sql.DB) *PostgresDeviceStore {
	return &PostgresDeviceStore{db: db}
}

// ListDevices returns all devices for userID from the sessions table (AC1).
func (s *PostgresDeviceStore) ListDevices(ctx context.Context, userID string) ([]matrix.Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, device_display_name, last_active_at
		 FROM sessions
		 WHERE user_id = $1
		 ORDER BY last_active_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []matrix.Device
	for rows.Next() {
		var d matrix.Device
		var displayName sql.NullString
		if err := rows.Scan(&d.DeviceID, &displayName, &d.LastSeenTS); err != nil {
			return nil, fmt.Errorf("scan device row: %w", err)
		}
		if displayName.Valid {
			d.DisplayName = &displayName.String
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device rows: %w", err)
	}
	return devices, nil
}

// GetDevice returns the device with deviceID for userID (AC2).
// Returns matrix.ErrDeviceNotFound if not found or not owned by userID.
func (s *PostgresDeviceStore) GetDevice(ctx context.Context, userID, deviceID string) (*matrix.Device, error) {
	var d matrix.Device
	var displayName sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT device_id, device_display_name, last_active_at
		 FROM sessions
		 WHERE user_id = $1 AND device_id = $2`,
		userID, deviceID,
	).Scan(&d.DeviceID, &displayName, &d.LastSeenTS)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, matrix.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	if displayName.Valid {
		d.DisplayName = &displayName.String
	}
	return &d, nil
}

// UpdateDeviceDisplayName updates device_display_name for (userID, deviceID) (AC3).
// Returns matrix.ErrDeviceNotFound if the row doesn't exist or isn't owned by userID.
func (s *PostgresDeviceStore) UpdateDeviceDisplayName(ctx context.Context, userID, deviceID string, displayName *string) error {
	var result sql.Result
	var err error

	if displayName == nil {
		result, err = s.db.ExecContext(ctx,
			`UPDATE sessions SET device_display_name = NULL
			 WHERE user_id = $1 AND device_id = $2`,
			userID, deviceID,
		)
	} else {
		result, err = s.db.ExecContext(ctx,
			`UPDATE sessions SET device_display_name = $3
			 WHERE user_id = $1 AND device_id = $2`,
			userID, deviceID, *displayName,
		)
	}

	if err != nil {
		return fmt.Errorf("update device display name: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return matrix.ErrDeviceNotFound
	}
	return nil
}

// DeleteDevice removes the session row for (userID, deviceID) (AC4).
// Returns matrix.ErrDeviceNotFound if not found or not owned by userID.
func (s *PostgresDeviceStore) DeleteDevice(ctx context.Context, userID, deviceID string) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND device_id = $2`,
		userID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected after delete: %w", err)
	}
	if n == 0 {
		return matrix.ErrDeviceNotFound
	}
	return nil
}

// DeleteDevices atomically removes multiple session rows for userID (AC5).
// Device IDs not owned by userID are silently ignored.
func (s *PostgresDeviceStore) DeleteDevices(ctx context.Context, userID string, deviceIDs []string) error {
	if len(deviceIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete_devices transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, deviceID := range deviceIDs {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM sessions WHERE user_id = $1 AND device_id = $2`,
			userID, deviceID,
		)
		if err != nil {
			return fmt.Errorf("delete device %s in bulk: %w", deviceID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete_devices transaction: %w", err)
	}
	return nil
}

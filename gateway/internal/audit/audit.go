// Package audit provides audit logging utilities for Story 5.1.
package audit

import (
	"context"
	"database/sql"
	"errors"
)

// ErrInvalidRetentionDays is returned when retentionDays is not a positive integer.
// A zero or negative value would purge all or future-dated rows — refused up front
// so a corrupted server_config value cannot silently destroy the audit history.
var ErrInvalidRetentionDays = errors.New("audit: retentionDays must be a positive integer")

// RunCleanup deletes audit_log rows older than retentionDays days.
// It calls the SECURITY DEFINER function audit_log_purge, which runs with the
// privileges of its owner (the migration role) and is the only sanctioned path
// for removing audit records. The app role retains only INSERT/SELECT on the
// table — purging is gated by EXECUTE on the function (granted in the migration).
//
// The caller passes a regular *sql.DB connection (the same one used for INSERTs);
// SECURITY DEFINER handles the privilege elevation inside PostgreSQL. The function
// itself validates retentionDays >= 1 as a second line of defense.
//
// Returns the number of rows deleted, or ErrInvalidRetentionDays if the input is
// not a positive integer.
func RunCleanup(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
	if retentionDays < 1 {
		return 0, ErrInvalidRetentionDays
	}
	// AC7 (Story 5.29c): cap at 36500 days (~100 years) to prevent make_interval overflow in SQL.
	if retentionDays > 36500 {
		return 0, ErrInvalidRetentionDays
	}
	var deleted int64
	err := db.QueryRowContext(ctx,
		"SELECT audit_log_purge($1)", retentionDays,
	).Scan(&deleted)
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

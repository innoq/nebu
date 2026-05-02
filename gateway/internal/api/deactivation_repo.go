//go:build go1.22

// Package api contains the DeactivationRepository interface and implementation
// for Story 6.5: User Deactivation + Reactivation.
package api

import (
	"context"
	"database/sql"
	"errors"
)

// ErrUserNotFound is returned by DeactivationRepository when the target user does not exist.
var ErrUserNotFound = errors.New("user not found")

// DeactivationRepository provides DB operations for user deactivation/reactivation.
// All methods are called after the caller has verified pre-conditions.
type DeactivationRepository interface {
	// GetUserStatus returns (is_active, deletion_status, anonymized_at) for user.
	// Returns (false, "", 0, ErrUserNotFound) if user does not exist.
	GetUserStatus(ctx context.Context, userID string) (isActive bool, deletionStatus string, anonymizedAt int64, err error)

	// DeactivateUser sets is_active=false, deactivated_at=nowMs, deactivation_reason=reason.
	// Caller must verify user is currently active before calling.
	DeactivateUser(ctx context.Context, userID, reason string, nowMs int64) error

	// ReactivateUser sets is_active=true, deactivated_at=NULL, deactivation_reason=NULL.
	// Caller must verify user is in deactivated state before calling.
	ReactivateUser(ctx context.Context, userID string) error
}

// dbDeactivationRepo is the production SQL-backed implementation of DeactivationRepository.
type dbDeactivationRepo struct {
	db *sql.DB
}

// NewDeactivationRepo creates a production DeactivationRepository backed by db.
func NewDeactivationRepo(db *sql.DB) DeactivationRepository {
	return &dbDeactivationRepo{db: db}
}

// GetUserStatus returns the current deactivation state for a user.
// Returns ErrUserNotFound if the user does not exist.
func (r *dbDeactivationRepo) GetUserStatus(ctx context.Context, userID string) (bool, string, int64, error) {
	const q = `
		SELECT is_active,
		       COALESCE(deletion_status, ''),
		       COALESCE(anonymized_at, 0)
		FROM users
		WHERE user_id = $1`

	var isActive bool
	var deletionStatus string
	var anonymizedAt int64

	err := r.db.QueryRowContext(ctx, q, userID).Scan(&isActive, &deletionStatus, &anonymizedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", 0, ErrUserNotFound
	}
	if err != nil {
		return false, "", 0, err
	}
	return isActive, deletionStatus, anonymizedAt, nil
}

// DeactivateUser sets is_active=false and records the deactivation timestamp and reason.
func (r *dbDeactivationRepo) DeactivateUser(ctx context.Context, userID, reason string, nowMs int64) error {
	const q = `
		UPDATE users
		SET is_active = false,
		    deactivated_at = $2,
		    deactivation_reason = $3
		WHERE user_id = $1`

	_, err := r.db.ExecContext(ctx, q, userID, nowMs, reason)
	return err
}

// ReactivateUser clears the deactivation state and restores is_active=true.
func (r *dbDeactivationRepo) ReactivateUser(ctx context.Context, userID string) error {
	const q = `
		UPDATE users
		SET is_active = true,
		    deactivated_at = NULL,
		    deactivation_reason = NULL
		WHERE user_id = $1`

	_, err := r.db.ExecContext(ctx, q, userID)
	return err
}

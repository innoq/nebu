//go:build go1.22

// Package api provides the RoleOverrideRepository interface and its PostgreSQL
// implementation for the User Role Assignment API (Story 6.6).
package api

import (
	"context"
	"database/sql"
	"errors"
)

// ErrRoleOverrideNotFound is returned by RevokeRoleOverride when the target
// (user_id, role) row does not exist in role_overrides.
var ErrRoleOverrideNotFound = errors.New("role override not found")

// RoleOverrideChecker is a narrower interface used by RequireRole middleware.
// It answers a single question: does this user have the named role in role_overrides?
// The DB-backed implementation caches results in RequireRole's per-instance sync.Map
// (60s TTL) to avoid per-request DB hits.
//
// Implement as a subset of RoleOverrideRepository so the same *dbRoleOverrideRepo
// satisfies both interfaces with no extra wrapper.
type RoleOverrideChecker interface {
	HasRoleOverride(ctx context.Context, userID, role string) (bool, error)
}

// RoleOverrideRepository abstracts all DB operations for role_overrides.
// The interface is defined here so unit tests can provide a mock without a
// real PostgreSQL connection.
type RoleOverrideRepository interface {
	// GrantRoleOverride upserts (user_id, role, granted_by) into role_overrides.
	// ON CONFLICT (user_id, role): updates granted_by and granted_at.
	// Returns nil on success.
	GrantRoleOverride(ctx context.Context, userID, role, grantedBy string) error

	// RevokeRoleOverride deletes the (user_id, role) row from role_overrides.
	// Returns ErrRoleOverrideNotFound if no row exists for (userID, role).
	RevokeRoleOverride(ctx context.Context, userID, role string) error

	// GetRoleOverrides returns all roles explicitly granted to userID.
	// Returns an empty slice (not nil) if no overrides exist.
	GetRoleOverrides(ctx context.Context, userID string) ([]string, error)

	// GetAllRoleOverridesForUsers batch-loads overrides for a set of user IDs.
	// Returns a map from userID → []string of granted roles.
	// Users with no overrides are omitted from the map (not present with nil/empty slice).
	GetAllRoleOverridesForUsers(ctx context.Context, userIDs []string) (map[string][]string, error)

	// UserExists returns true if the users table contains the given user_id.
	// Used by AssignAdminUserRole to reject grant/revoke for ghost users.
	UserExists(ctx context.Context, userID string) (bool, error)

	// HasRoleOverride satisfies RoleOverrideChecker (embedded in repository).
	HasRoleOverride(ctx context.Context, userID, role string) (bool, error)
}

// Compile-time check: *dbRoleOverrideRepo must satisfy both interfaces.
var _ RoleOverrideRepository = (*dbRoleOverrideRepo)(nil)
var _ RoleOverrideChecker = (*dbRoleOverrideRepo)(nil)

// dbRoleOverrideRepo is the production PostgreSQL-backed implementation.
type dbRoleOverrideRepo struct {
	db *sql.DB
}

// NewRoleOverrideRepo constructs a production RoleOverrideRepository.
func NewRoleOverrideRepo(db *sql.DB) RoleOverrideRepository {
	return &dbRoleOverrideRepo{db: db}
}

// GrantRoleOverride upserts into role_overrides.
func (r *dbRoleOverrideRepo) GrantRoleOverride(ctx context.Context, userID, role, grantedBy string) error {
	const q = `
		INSERT INTO role_overrides (user_id, role, granted_by, granted_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, role) DO UPDATE
		    SET granted_by = EXCLUDED.granted_by,
		        granted_at = NOW()`
	_, err := r.db.ExecContext(ctx, q, userID, role, grantedBy)
	return err
}

// RevokeRoleOverride deletes the override row and returns ErrRoleOverrideNotFound
// if the row did not exist.
func (r *dbRoleOverrideRepo) RevokeRoleOverride(ctx context.Context, userID, role string) error {
	const q = `DELETE FROM role_overrides WHERE user_id = $1 AND role = $2`
	res, err := r.db.ExecContext(ctx, q, userID, role)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRoleOverrideNotFound
	}
	return nil
}

// GetRoleOverrides returns the list of roles granted to userID via role_overrides.
func (r *dbRoleOverrideRepo) GetRoleOverrides(ctx context.Context, userID string) ([]string, error) {
	const q = `SELECT role FROM role_overrides WHERE user_id = $1 ORDER BY role`
	rows, err := r.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []string{}
	}
	return roles, nil
}

// GetAllRoleOverridesForUsers batch-loads overrides for a slice of user IDs.
// Uses ANY($1) to avoid N+1 queries.
func (r *dbRoleOverrideRepo) GetAllRoleOverridesForUsers(ctx context.Context, userIDs []string) (map[string][]string, error) {
	if len(userIDs) == 0 {
		return map[string][]string{}, nil
	}
	const q = `SELECT user_id, role FROM role_overrides WHERE user_id = ANY($1) ORDER BY user_id, role`
	rows, err := r.db.QueryContext(ctx, q, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var uid, role string
		if err := rows.Scan(&uid, &role); err != nil {
			return nil, err
		}
		result[uid] = append(result[uid], role)
	}
	return result, rows.Err()
}

// UserExists returns true if users table contains the given user_id.
func (r *dbRoleOverrideRepo) UserExists(ctx context.Context, userID string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM users WHERE user_id = $1)`
	var exists bool
	err := r.db.QueryRowContext(ctx, q, userID).Scan(&exists)
	return exists, err
}

// HasRoleOverride returns true if a role_overrides row exists for (userID, role).
// Used by the RequireRole middleware checker.
func (r *dbRoleOverrideRepo) HasRoleOverride(ctx context.Context, userID, role string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM role_overrides WHERE user_id = $1 AND role = $2)`
	var exists bool
	err := r.db.QueryRowContext(ctx, q, userID, role).Scan(&exists)
	return exists, err
}

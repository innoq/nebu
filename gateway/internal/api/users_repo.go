//go:build go1.22

// Package api provides the UserRepository interface and its PostgreSQL implementation
// for the Admin User List + Get API (Story 6.4).
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AdminUser is the JSON-serialisable representation of a user for the Admin API.
// Fields match the AC#1 user object: user_id, display_name, email_masked, roles,
// status, created_at (ISO 8601), last_seen_at (ISO 8601 | null).
//
// NOTE: email_masked is always "" in MVP — email decryption requires the user's
// X25519 private key, which is unavailable in the Admin context (and irreversibly
// gone after key deletion). Email masking will be wired in a future story.
type AdminUser struct {
	UserID      string   `json:"user_id"`
	DisplayName string   `json:"display_name"`
	EmailMasked string   `json:"email_masked"`
	Roles       []string `json:"roles"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	LastSeenAt  *string  `json:"last_seen_at"`
}

// AdminUserDetail extends AdminUser with room_count for the single-user GET endpoint (AC#2).
type AdminUserDetail struct {
	AdminUser
	RoomCount int `json:"room_count"`
}

// UserRepository abstracts database access for Admin user queries.
// The interface is defined here so that unit tests can provide a mock implementation
// without a real PostgreSQL connection.
//
// ListUsers signature: (ctx, afterID, afterCreatedAt, limit, search) →
//
//	(users, total, nextCursor, error)
//
// GetUser returns (nil, nil) when the user does not exist.
type UserRepository interface {
	ListUsers(ctx context.Context, afterID, afterCreatedAt string, limit int, search string) ([]AdminUser, int, string, error)
	GetUser(ctx context.Context, userID string) (*AdminUserDetail, error)
}

// userRepo is the real PostgreSQL implementation of UserRepository.
type userRepo struct {
	db    *sql.DB
	roles RoleOverrideRepository // optional; nil disables role_overrides merge (Story 6.6)
}

// NewUserRepo constructs a new DB-backed UserRepository.
// Pass rolesRepo (non-nil) to merge role_overrides into every user's Roles field.
// Pass nil to disable role override merging (tests, or callers that have not yet wired the table).
func NewUserRepo(db *sql.DB) UserRepository {
	return &userRepo{db: db}
}

// NewUserRepoWithRoles constructs a UserRepository that also merges role_overrides (Story 6.6).
func NewUserRepoWithRoles(db *sql.DB, roles RoleOverrideRepository) UserRepository {
	return &userRepo{db: db, roles: roles}
}

// deriveStatus maps the three lifecycle columns to the canonical status string.
//
//	"anonymized"   — anonymized_at IS NOT NULL (irreversible; takes priority)
//	"keys_deleted" — deletion_status = 'keys_deleted'
//	"deactivated"  — is_active = false
//	"active"       — default
func deriveStatus(isActive bool, deletionStatus sql.NullString, anonymizedAt sql.NullInt64) string {
	if anonymizedAt.Valid {
		return "anonymized"
	}
	if deletionStatus.Valid && deletionStatus.String == "keys_deleted" {
		return "keys_deleted"
	}
	if !isActive {
		return "deactivated"
	}
	return "active"
}

// maskEmail returns "a***@example.com" format.
// If the email is empty or has no "@", it returns "***".
// NOTE: For MVP, email decryption is out of scope; this helper exists for future use.
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	return string(email[0]) + "***" + email[at:]
}

// epochMsToISO8601 converts a Unix epoch millisecond timestamp to an ISO 8601 string.
// Returns "" for zero or negative values.
func epochMsToISO8601(epochMs int64) string {
	if epochMs <= 0 {
		return ""
	}
	sec := epochMs / 1000
	ns := (epochMs % 1000) * int64(time.Millisecond)
	return time.Unix(sec, ns).UTC().Format(time.RFC3339)
}

// ListUsers queries the users + profiles tables with optional cursor pagination and search.
// For MVP, email_masked is always "" (encrypted, no decryption key available in Admin context).
// Story 6.6: if r.roles is non-nil, batch-loads role_overrides for all returned users and
// merges them into each user's Roles field (deduped, system_role always present).
func (r *userRepo) ListUsers(ctx context.Context, afterID, afterCreatedAt string, limit int, search string) ([]AdminUser, int, string, error) {
	// Build argument list and WHERE clauses progressively.
	// Argument index tracks the $N placeholder position in the SQL.
	args := []any{}
	searchClause := ""
	cursorClause := ""
	n := 1

	if search != "" {
		searchClause = fmt.Sprintf(` AND (p.displayname ILIKE '%%' || $%d || '%%')`, n)
		args = append(args, search)
		n++
	}

	// Count query uses the same search filter but not the cursor (cursor is for keyset pagination).
	countArgs := make([]any, len(args))
	copy(countArgs, args)

	if afterID != "" && afterCreatedAt != "" {
		// afterCreatedAt from the cursor is an ISO 8601 timestamp string.
		// The created_at column is epoch ms (BIGINT). We parse the ISO 8601 string back
		// to epoch ms for the keyset comparison.
		afterCreatedAtMs, parseErr := parseISO8601ToEpochMs(afterCreatedAt)
		if parseErr != nil {
			return nil, 0, "", fmt.Errorf("cursor: invalid after_created_at: %w", parseErr)
		}
		cursorClause = fmt.Sprintf(` AND (u.created_at, u.user_id) < ($%d, $%d)`, n, n+1)
		args = append(args, afterCreatedAtMs, afterID)
		n += 2
	}

	// Count total matching rows (without cursor — cursor is pagination-relative, not filter-relative).
	countSQL := `SELECT COUNT(*) FROM users u LEFT JOIN profiles p ON u.user_id = p.user_id WHERE 1=1` + searchClause
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, "", fmt.Errorf("ListUsers count: %w", err)
	}

	// Main list query with keyset pagination.
	limitPlaceholder := fmt.Sprintf(`$%d`, n)
	listSQL := `
		SELECT u.user_id, COALESCE(p.displayname, ''), u.system_role,
		       u.is_active, u.deletion_status, u.anonymized_at,
		       u.created_at, u.last_seen_at
		FROM users u
		LEFT JOIN profiles p ON u.user_id = p.user_id
		WHERE 1=1` + searchClause + cursorClause + `
		ORDER BY u.created_at DESC, u.user_id DESC
		LIMIT ` + limitPlaceholder

	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, "", fmt.Errorf("ListUsers query: %w", err)
	}
	defer rows.Close()

	var users []AdminUser
	var lastCreatedAt int64
	var lastUserID string

	for rows.Next() {
		var (
			uid            string
			displayName    string
			systemRole     string
			isActive       bool
			deletionStatus sql.NullString
			anonymizedAt   sql.NullInt64
			createdAt      int64
			lastSeenAt     sql.NullInt64
		)
		if err := rows.Scan(&uid, &displayName, &systemRole,
			&isActive, &deletionStatus, &anonymizedAt,
			&createdAt, &lastSeenAt); err != nil {
			return nil, 0, "", fmt.Errorf("ListUsers scan: %w", err)
		}

		status := deriveStatus(isActive, deletionStatus, anonymizedAt)

		// MVP: email_masked is always "" — no decryption key available in Admin context.
		emailMasked := ""

		u := AdminUser{
			UserID:      uid,
			DisplayName: displayName,
			EmailMasked: emailMasked,
			Roles:       []string{systemRole},
			Status:      status,
			CreatedAt:   epochMsToISO8601(createdAt),
		}
		if lastSeenAt.Valid {
			s := epochMsToISO8601(lastSeenAt.Int64)
			u.LastSeenAt = &s
		}

		users = append(users, u)
		lastCreatedAt = createdAt
		lastUserID = uid
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("ListUsers rows: %w", err)
	}

	// Story 6.6: merge role_overrides for all returned users (batch lookup).
	if r.roles != nil && len(users) > 0 {
		userIDs := make([]string, len(users))
		for i, u := range users {
			userIDs[i] = u.UserID
		}
		overrideMap, ovErr := r.roles.GetAllRoleOverridesForUsers(ctx, userIDs)
		if ovErr == nil {
			for i := range users {
				if extras, ok := overrideMap[users[i].UserID]; ok {
					users[i].Roles = mergeRoles(users[i].Roles, extras)
				}
			}
		}
		// On DB error: fail-open — return users with system_role only, no additional overrides.
	}

	// Encode next cursor from the last returned row (only if the page was full —
	// a partial page means we have reached the end of the result set).
	var nextCursor string
	if len(users) == limit && len(users) > 0 {
		nextCursor = EncodeCursor(lastUserID, epochMsToISO8601(lastCreatedAt))
	}

	return users, total, nextCursor, nil
}

// mergeRoles returns a deduped slice with all roles from base plus extras.
// The base slice is never modified.
func mergeRoles(base, extras []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extras))
	merged := make([]string, 0, len(base)+len(extras))
	for _, r := range base {
		if _, dup := seen[r]; !dup {
			seen[r] = struct{}{}
			merged = append(merged, r)
		}
	}
	for _, r := range extras {
		if _, dup := seen[r]; !dup {
			seen[r] = struct{}{}
			merged = append(merged, r)
		}
	}
	return merged
}

// GetUser fetches a single user with room_count.
// Returns (nil, nil) if the user does not exist.
// Story 6.6: if r.roles is non-nil, merges role_overrides into the returned user's Roles field.
func (r *userRepo) GetUser(ctx context.Context, userID string) (*AdminUserDetail, error) {
	const q = `
		SELECT u.user_id, COALESCE(p.displayname, ''), u.system_role,
		       u.is_active, u.deletion_status, u.anonymized_at,
		       u.created_at, u.last_seen_at,
		       COUNT(rm.room_id) FILTER (WHERE rm.left_at IS NULL) AS room_count
		FROM users u
		LEFT JOIN profiles p ON u.user_id = p.user_id
		LEFT JOIN room_members rm ON rm.user_id = u.user_id
		WHERE u.user_id = $1
		GROUP BY u.user_id, p.displayname, u.system_role, u.is_active,
		         u.deletion_status, u.anonymized_at, u.created_at, u.last_seen_at`

	var (
		uid            string
		displayName    string
		systemRole     string
		isActive       bool
		deletionStatus sql.NullString
		anonymizedAt   sql.NullInt64
		createdAt      int64
		lastSeenAt     sql.NullInt64
		roomCount      int
	)

	err := r.db.QueryRowContext(ctx, q, userID).Scan(
		&uid, &displayName, &systemRole,
		&isActive, &deletionStatus, &anonymizedAt,
		&createdAt, &lastSeenAt, &roomCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetUser query: %w", err)
	}

	status := deriveStatus(isActive, deletionStatus, anonymizedAt)

	// MVP: email_masked is always "" (see ListUsers comment).
	emailMasked := ""

	u := AdminUser{
		UserID:      uid,
		DisplayName: displayName,
		EmailMasked: emailMasked,
		Roles:       []string{systemRole},
		Status:      status,
		CreatedAt:   epochMsToISO8601(createdAt),
	}
	if lastSeenAt.Valid {
		s := epochMsToISO8601(lastSeenAt.Int64)
		u.LastSeenAt = &s
	}

	// Story 6.6: merge role_overrides for this user.
	if r.roles != nil {
		if overrides, ovErr := r.roles.GetRoleOverrides(ctx, uid); ovErr == nil {
			u.Roles = mergeRoles(u.Roles, overrides)
		}
		// On DB error: fail-open — return user with system_role only.
	}

	return &AdminUserDetail{AdminUser: u, RoomCount: roomCount}, nil
}

// parseISO8601ToEpochMs parses an ISO 8601 timestamp and returns Unix epoch milliseconds.
func parseISO8601ToEpochMs(iso string) (int64, error) {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

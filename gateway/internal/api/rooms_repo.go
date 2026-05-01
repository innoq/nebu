//go:build go1.22

// Package api provides the RoomRepository interface and its PostgreSQL implementation
// for the Admin Room List + Get API (Story 6.7).
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AdminRoom is the JSON-serialisable representation of a room for the Admin API (Story 6.7).
// Fields that are not yet stored in the DB are returned as defaults with TODO comments.
type AdminRoom struct {
	RoomID         string `json:"room_id"`
	Name           string `json:"name"`
	Topic          string `json:"topic"`          // "" — not in DB yet (Story 6.8)
	CanonicalAlias string `json:"canonical_alias"` // "" — not in DB yet
	Visibility     string `json:"visibility"`      // "public" | "private"
	IsPublic       bool   `json:"is_public"`       // derived: visibility == "public"
	MemberCount    int    `json:"member_count"`
	Status         string `json:"status"`          // "active" | "archived"
	CreatedAt      string `json:"created_at"`      // ISO 8601 (uses epochMsToISO8601)
	CreatorUserID  string `json:"creator_user_id"` // "" — not in DB yet
	AdminNote      string `json:"admin_note"`      // "" — not in DB yet
}

// AdminRoomDetail extends AdminRoom with detail-only fields.
// TODO(story-6.8): MaxMembers will be populated after rooms.max_members column is added.
type AdminRoomDetail struct {
	AdminRoom
	MaxMembers      int    `json:"max_members"`       // 0 until Story 6.8 adds the column
	MessageCount    int    `json:"message_count"`
	PowerLevelsJSON string `json:"power_levels_json"` // raw JSON string from DB
}

// RoomRepository abstracts database access for Admin room queries.
// The interface is consumer-defined (same pattern as UserRepository in users_repo.go).
//
// ListRooms signature: (ctx, afterID, afterCreatedAt, limit, search, statusFilter) →
//
//	(rooms, total, nextCursor, error)
//
// GetRoom returns (nil, nil) when the room does not exist.
type RoomRepository interface {
	ListRooms(ctx context.Context, afterID, afterCreatedAt string, limit int, search, statusFilter string) ([]AdminRoom, int, string, error)
	GetRoom(ctx context.Context, roomID string) (*AdminRoomDetail, error)
}

// dbRoomRepo is the real PostgreSQL implementation of RoomRepository.
type dbRoomRepo struct {
	db *sql.DB
}

// NewRoomRepo constructs a new DB-backed RoomRepository.
func NewRoomRepo(db *sql.DB) RoomRepository {
	return &dbRoomRepo{db: db}
}

// ListRooms queries the rooms table with optional cursor pagination, search, and status filter.
//
// member_count is derived via a correlated sub-select (left_at IS NULL = still member).
// created_at is stored as epoch ms (BIGINT), converted to ISO 8601 via epochMsToISO8601.
// Cursor pagination uses (created_at DESC, room_id) keyset — identical to users pattern.
func (r *dbRoomRepo) ListRooms(ctx context.Context, afterID, afterCreatedAt string, limit int, search, statusFilter string) ([]AdminRoom, int, string, error) {
	// Build argument list and WHERE clauses progressively.
	args := []any{}
	n := 1

	var searchClause, statusClause, cursorClause string

	if search != "" {
		searchClause = fmt.Sprintf(` AND r.name ILIKE '%%' || $%d || '%%'`, n)
		args = append(args, search)
		n++
	}

	switch statusFilter {
	case "active":
		statusClause = ` AND r.archived_at IS NULL`
	case "archived":
		statusClause = ` AND r.archived_at IS NOT NULL`
	}

	// Count query: same filters but no cursor (cursor is pagination-relative, not filter-relative).
	countArgs := make([]any, len(args))
	copy(countArgs, args)

	if afterID != "" && afterCreatedAt != "" {
		afterCreatedAtMs, parseErr := parseISO8601ToEpochMs(afterCreatedAt)
		if parseErr != nil {
			return nil, 0, "", fmt.Errorf("cursor: invalid after_created_at: %w", parseErr)
		}
		cursorClause = fmt.Sprintf(` AND (r.created_at, r.room_id) < ($%d, $%d)`, n, n+1)
		args = append(args, afterCreatedAtMs, afterID)
		n += 2
	}

	// Count total matching rows (without cursor).
	countSQL := `SELECT COUNT(*) FROM rooms r WHERE 1=1` + searchClause + statusClause
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, "", fmt.Errorf("ListRooms count: %w", err)
	}

	// Main list query with keyset pagination and correlated sub-select for member_count.
	limitPlaceholder := fmt.Sprintf(`$%d`, n)
	listSQL := `
		SELECT r.room_id,
		       COALESCE(r.name, ''),
		       r.visibility,
		       r.created_at,
		       r.archived_at,
		       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.room_id AND rm.left_at IS NULL) AS member_count
		FROM rooms r
		WHERE 1=1` + searchClause + statusClause + cursorClause + `
		ORDER BY r.created_at DESC, r.room_id DESC
		LIMIT ` + limitPlaceholder

	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, "", fmt.Errorf("ListRooms query: %w", err)
	}
	defer rows.Close()

	var rooms []AdminRoom
	var lastCreatedAt int64
	var lastRoomID string

	for rows.Next() {
		var (
			roomID      string
			name        string
			visibility  string
			createdAt   int64
			archivedAt  sql.NullInt64
			memberCount int
		)
		if err := rows.Scan(&roomID, &name, &visibility, &createdAt, &archivedAt, &memberCount); err != nil {
			return nil, 0, "", fmt.Errorf("ListRooms scan: %w", err)
		}

		status := "active"
		if archivedAt.Valid {
			status = "archived"
		}

		rooms = append(rooms, AdminRoom{
			RoomID:         roomID,
			Name:           name,
			Topic:          "",   // TODO(story-6.8): not in DB yet
			CanonicalAlias: "",   // TODO(story-6.8): not in DB yet
			Visibility:     visibility,
			IsPublic:       visibility == "public",
			MemberCount:    memberCount,
			Status:         status,
			CreatedAt:      epochMsToISO8601(createdAt),
			CreatorUserID:  "",   // TODO(story-6.8): not in DB yet
			AdminNote:      "",   // TODO(story-6.8): not in DB yet
		})
		lastCreatedAt = createdAt
		lastRoomID = roomID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("ListRooms rows: %w", err)
	}

	// Encode next cursor only for a full page (partial page = end of result set).
	var nextCursor string
	if len(rooms) == limit && len(rooms) > 0 {
		nextCursor = EncodeCursor(lastRoomID, epochMsToISO8601(lastCreatedAt))
	}

	return rooms, total, nextCursor, nil
}

// GetRoom fetches a single room with message_count and power_levels_json.
// Returns (nil, nil) if the room does not exist (sql.ErrNoRows).
func (r *dbRoomRepo) GetRoom(ctx context.Context, roomID string) (*AdminRoomDetail, error) {
	const q = `
		SELECT r.room_id,
		       COALESCE(r.name, ''),
		       r.visibility,
		       r.created_at,
		       r.archived_at,
		       r.power_levels_json,
		       (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.room_id AND rm.left_at IS NULL) AS member_count,
		       (SELECT COUNT(*) FROM events e WHERE e.room_id = r.room_id) AS message_count
		FROM rooms r
		WHERE r.room_id = $1`

	var (
		rid             string
		name            string
		visibility      string
		createdAt       int64
		archivedAt      sql.NullInt64
		powerLevelsJSON string
		memberCount     int
		messageCount    int
	)

	err := r.db.QueryRowContext(ctx, q, roomID).Scan(
		&rid, &name, &visibility, &createdAt, &archivedAt, &powerLevelsJSON, &memberCount, &messageCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRoom query: %w", err)
	}

	status := "active"
	if archivedAt.Valid {
		status = "archived"
	}

	room := AdminRoom{
		RoomID:         rid,
		Name:           name,
		Topic:          "",   // TODO(story-6.8): not in DB yet
		CanonicalAlias: "",   // TODO(story-6.8): not in DB yet
		Visibility:     visibility,
		IsPublic:       visibility == "public",
		MemberCount:    memberCount,
		Status:         status,
		CreatedAt:      epochMsToISO8601(createdAt),
		CreatorUserID:  "",   // TODO(story-6.8): not in DB yet
		AdminNote:      "",   // TODO(story-6.8): not in DB yet
	}

	return &AdminRoomDetail{
		AdminRoom:       room,
		MaxMembers:      0, // TODO(story-6.8): rooms.max_members column not yet added
		MessageCount:    messageCount,
		PowerLevelsJSON: powerLevelsJSON,
	}, nil
}

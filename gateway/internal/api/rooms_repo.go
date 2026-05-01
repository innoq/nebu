//go:build go1.22

// Package api provides the RoomRepository interface and its PostgreSQL implementation
// for the Admin Room List + Get API (Story 6.7) and Room Settings Update API (Story 6.8).
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AdminRoom is the list-view representation of a room for the Admin API.
// Fields match AC#1 room object: room_id, name, topic, visibility, member_count,
// status, created_at (ISO 8601), creator_user_id.
type AdminRoom struct {
	RoomID        string `json:"room_id"`
	Name          string `json:"name"`
	Topic         string `json:"topic"`
	Visibility    string `json:"visibility"`
	MemberCount   int    `json:"member_count"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"` // ISO 8601 from epoch ms
	CreatorUserID string `json:"creator_user_id"`
}

// AdminRoomDetail extends AdminRoom for the single-room GET endpoint (AC#2).
type AdminRoomDetail struct {
	AdminRoom
	MaxMembers      int    `json:"max_members"`       // 0 = no limit
	MessageCount    int    `json:"message_count"`
	PowerLevelsJSON string `json:"power_levels_json"`
}

// RoomPatch holds the optional fields for a PATCH /admin/rooms/{roomId} request.
// Only non-nil fields are applied to the rooms table.
type RoomPatch struct {
	MaxMembers *int
	Visibility *string
	Name       *string
	Topic      *string
}

// RoomRepository abstracts database access for Admin room queries.
// The interface is defined here so that unit tests can provide a mock implementation
// without a real PostgreSQL connection.
//
// ListRooms signature: (ctx, afterID, afterCreatedAt, limit, search, status) →
//
//	(rooms, total, nextCursor, error)
//
// GetRoom returns (nil, nil) when the room does not exist.
// UpdateRoom applies the non-nil fields in patch and returns the updated room.
// Returns (nil, nil) if the room does not exist.
type RoomRepository interface {
	ListRooms(ctx context.Context, afterID, afterCreatedAt string, limit int, search, status string) ([]AdminRoom, int, string, error)
	GetRoom(ctx context.Context, roomID string) (*AdminRoomDetail, error)
	UpdateRoom(ctx context.Context, roomID string, patch RoomPatch) (*AdminRoomDetail, error)
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
// Ordering: (created_at DESC, room_id DESC) — newest rooms first, tie-broken by room_id.
// member_count: count from room_members where left_at IS NULL (active members only).
func (r *dbRoomRepo) ListRooms(ctx context.Context, afterID, afterCreatedAt string, limit int, search, status string) ([]AdminRoom, int, string, error) {
	args := []any{}
	searchClause := ""
	statusClause := ""
	cursorClause := ""
	n := 1

	if search != "" {
		searchClause = fmt.Sprintf(` AND r.name ILIKE '%%' || $%d || '%%'`, n)
		args = append(args, search)
		n++
	}

	if status != "" {
		statusClause = fmt.Sprintf(` AND r.status = $%d`, n)
		args = append(args, status)
		n++
	}

	// Count query uses the same search/status filters but no cursor.
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

	// Main list query with keyset pagination.
	limitPlaceholder := fmt.Sprintf(`$%d`, n)
	listSQL := `
		SELECT r.room_id,
		       COALESCE(r.name, ''),
		       COALESCE(r.topic, ''),
		       r.visibility,
		       COUNT(rm.user_id) FILTER (WHERE rm.left_at IS NULL) AS member_count,
		       r.status,
		       r.created_at,
		       COALESCE(r.creator_user_id, '')
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.room_id
		WHERE 1=1` + searchClause + statusClause + cursorClause + `
		GROUP BY r.room_id, r.name, r.topic, r.visibility, r.status, r.created_at, r.creator_user_id
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
			roomID        string
			name          string
			topic         string
			visibility    string
			memberCount   int
			roomStatus    string
			createdAt     int64
			creatorUserID string
		)
		if err := rows.Scan(&roomID, &name, &topic, &visibility, &memberCount, &roomStatus, &createdAt, &creatorUserID); err != nil {
			return nil, 0, "", fmt.Errorf("ListRooms scan: %w", err)
		}

		rm := AdminRoom{
			RoomID:        roomID,
			Name:          name,
			Topic:         topic,
			Visibility:    visibility,
			MemberCount:   memberCount,
			Status:        roomStatus,
			CreatedAt:     epochMsToISO8601(createdAt),
			CreatorUserID: creatorUserID,
		}
		rooms = append(rooms, rm)
		lastCreatedAt = createdAt
		lastRoomID = roomID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("ListRooms rows: %w", err)
	}

	// Encode next cursor from the last returned row (only if the page was full).
	var nextCursor string
	if len(rooms) == limit && len(rooms) > 0 {
		nextCursor = EncodeCursor(lastRoomID, epochMsToISO8601(lastCreatedAt))
	}

	return rooms, total, nextCursor, nil
}

// GetRoom fetches a single room with member_count, message_count, and power_levels_json.
// Returns (nil, nil) if the room does not exist.
func (r *dbRoomRepo) GetRoom(ctx context.Context, roomID string) (*AdminRoomDetail, error) {
	const q = `
		SELECT r.room_id,
		       COALESCE(r.name, ''),
		       COALESCE(r.topic, ''),
		       r.visibility,
		       COUNT(rm.user_id) FILTER (WHERE rm.left_at IS NULL) AS member_count,
		       r.status,
		       r.created_at,
		       COALESCE(r.creator_user_id, ''),
		       r.max_members,
		       COUNT(e.event_id) AS message_count,
		       r.power_levels_json
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.room_id
		LEFT JOIN events e ON e.room_id = r.room_id
		WHERE r.room_id = $1
		GROUP BY r.room_id, r.name, r.topic, r.visibility, r.status,
		         r.created_at, r.creator_user_id, r.max_members, r.power_levels_json`

	var (
		rid           string
		name          string
		topic         string
		visibility    string
		memberCount   int
		roomStatus    string
		createdAt     int64
		creatorUserID string
		maxMembers    int
		messageCount  int
		powerLevels   string
	)

	err := r.db.QueryRowContext(ctx, q, roomID).Scan(
		&rid, &name, &topic, &visibility, &memberCount, &roomStatus,
		&createdAt, &creatorUserID, &maxMembers, &messageCount, &powerLevels,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRoom query: %w", err)
	}

	return &AdminRoomDetail{
		AdminRoom: AdminRoom{
			RoomID:        rid,
			Name:          name,
			Topic:         topic,
			Visibility:    visibility,
			MemberCount:   memberCount,
			Status:        roomStatus,
			CreatedAt:     epochMsToISO8601(createdAt),
			CreatorUserID: creatorUserID,
		},
		MaxMembers:      maxMembers,
		MessageCount:    messageCount,
		PowerLevelsJSON: powerLevels,
	}, nil
}

// UpdateRoom applies only the non-nil fields in patch to the rooms table, then
// returns the full updated AdminRoomDetail by calling GetRoom.
//
// Returns (nil, nil) when the room does not exist (UPDATE affects 0 rows).
// When patch has no non-nil fields (empty patch), skips UPDATE and falls through
// to GetRoom — the result is the current room state, or (nil, nil) if not found.
func (r *dbRoomRepo) UpdateRoom(ctx context.Context, roomID string, patch RoomPatch) (*AdminRoomDetail, error) {
	setClauses := []string{}
	args := []any{roomID} // $1 = room_id for WHERE
	n := 2

	if patch.MaxMembers != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_members = $%d", n))
		args = append(args, *patch.MaxMembers)
		n++
	}
	if patch.Visibility != nil {
		setClauses = append(setClauses, fmt.Sprintf("visibility = $%d", n))
		args = append(args, *patch.Visibility)
		n++
	}
	if patch.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", n))
		args = append(args, *patch.Name)
		n++
	}
	if patch.Topic != nil {
		setClauses = append(setClauses, fmt.Sprintf("topic = $%d", n))
		args = append(args, *patch.Topic)
		n++
	}

	if len(setClauses) > 0 {
		q := fmt.Sprintf("UPDATE rooms SET %s WHERE room_id = $1", strings.Join(setClauses, ", "))
		result, err := r.db.ExecContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("UpdateRoom: %w", err)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return nil, nil // room not found
		}
	}

	// Fetch updated state (also handles the no-op case and not-found when no SET clauses).
	return r.GetRoom(ctx, roomID)
}

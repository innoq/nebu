//go:build go1.22

// Package api provides the MetricsRepository interface and its PostgreSQL
// implementation for the Admin Metrics API (Story 6.10).
package api

import (
	"context"
	"database/sql"
	"fmt"
)

// MetricsRepository abstracts database access for Admin instance metrics.
// The interface is defined here so that unit tests can provide a mock implementation
// without a real PostgreSQL connection.
//
// GetMetricsCounts returns DB-derived counts:
//   - RoomCount:         active rooms (status = 'active')
//   - ArchivedRoomCount: archived rooms (status = 'archived')
//   - RegisteredUsers:   active users (is_active = true)
//   - DeactivatedUsers:  deactivated users (is_active = false)
//
// active_sessions and msg_per_sec come from gRPC GetMetrics, not from the DB.
type MetricsRepository interface {
	GetMetricsCounts(ctx context.Context) (*MetricsCounts, error)
}

// MetricsCounts holds the DB-derived metric counts for the Admin Metrics API.
type MetricsCounts struct {
	RoomCount         int
	ArchivedRoomCount int
	RegisteredUsers   int
	DeactivatedUsers  int
}

// dbMetricsRepo is the real PostgreSQL implementation of MetricsRepository.
type dbMetricsRepo struct {
	db *sql.DB
}

// NewMetricsRepo constructs a new DB-backed MetricsRepository.
func NewMetricsRepo(db *sql.DB) MetricsRepository {
	return &dbMetricsRepo{db: db}
}

// GetMetricsCounts queries room and user counts from PostgreSQL in two queries.
// Uses aggregate FILTER to compute active and archived room counts in a single pass.
func (r *dbMetricsRepo) GetMetricsCounts(ctx context.Context) (*MetricsCounts, error) {
	counts := &MetricsCounts{}

	// Room counts: active and archived in one pass.
	err := r.db.QueryRowContext(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE status = 'active')   AS room_count,
		   COUNT(*) FILTER (WHERE status = 'archived') AS archived_room_count
		 FROM rooms`).
		Scan(&counts.RoomCount, &counts.ArchivedRoomCount)
	if err != nil {
		return nil, fmt.Errorf("GetMetricsCounts: rooms query: %w", err)
	}

	// User counts: registered (active) and deactivated in one pass.
	err = r.db.QueryRowContext(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE is_active = true)  AS registered_users,
		   COUNT(*) FILTER (WHERE is_active = false) AS deactivated_users
		 FROM users`).
		Scan(&counts.RegisteredUsers, &counts.DeactivatedUsers)
	if err != nil {
		return nil, fmt.Errorf("GetMetricsCounts: users query: %w", err)
	}

	return counts, nil
}

// Compile-time check: dbMetricsRepo satisfies MetricsRepository.
var _ MetricsRepository = (*dbMetricsRepo)(nil)

// compiletimeCheck: dbServerConfigRepo satisfies ServerConfigRepository.
var _ ServerConfigRepository = (*dbServerConfigRepo)(nil)

package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
)

// BootstrapStatusChecker checks whether the instance is in bootstrap mode.
type BootstrapStatusChecker interface {
	IsBootstrapActive(ctx context.Context) (bool, error)
}

type bootstrapResponse struct {
	BootstrapActive bool `json:"bootstrap_active"`
}

// BootstrapHandler serves GET /admin/bootstrap.
type BootstrapHandler struct {
	checker BootstrapStatusChecker
}

// NewBootstrapHandler creates a BootstrapHandler with the given status checker.
func NewBootstrapHandler(checker BootstrapStatusChecker) *BootstrapHandler {
	return &BootstrapHandler{checker: checker}
}

// Handler responds with the current bootstrap status as JSON.
func (h *BootstrapHandler) Handler(w http.ResponseWriter, r *http.Request) {
	active, err := h.checker.IsBootstrapActive(r.Context())
	if err != nil {
		slog.Error("bootstrap status check failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !active {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bootstrapResponse{BootstrapActive: true})
}

// PostgresBootstrapChecker checks bootstrap status against PostgreSQL.
type PostgresBootstrapChecker struct {
	db *sql.DB
}

// NewPostgresBootstrapChecker creates a checker backed by the given DB connection.
func NewPostgresBootstrapChecker(db *sql.DB) *PostgresBootstrapChecker {
	return &PostgresBootstrapChecker{db: db}
}

// IsBootstrapActive returns true when the instance is in bootstrap mode:
//   - bootstrap_completed exists → false
//   - bootstrap_active exists (no bootstrap_completed) → true
//   - neither exists and no users → true (pre-first-login)
//   - neither exists and users exist → false
func (c *PostgresBootstrapChecker) IsBootstrapActive(ctx context.Context) (bool, error) {
	rows, err := c.db.QueryContext(ctx,
		"SELECT key, value FROM server_config WHERE key IN ('bootstrap_active', 'bootstrap_completed')")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var hasActive, hasCompleted bool
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return false, err
		}
		switch key {
		case "bootstrap_active":
			hasActive = true
		case "bootstrap_completed":
			hasCompleted = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	if hasCompleted {
		return false, nil
	}
	if hasActive {
		return true, nil
	}

	// Neither flag exists — check if any users exist
	var usersExist bool
	err = c.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users)").Scan(&usersExist)
	if err != nil {
		return false, err
	}

	return !usersExist, nil
}

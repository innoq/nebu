package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// InitServerConfig ensures server_name is present in server_config after migrations.
//
// Behavior:
//   - If server_name already exists in DB: returns DB value (ignores serverName param)
//   - If server_name is absent and serverName != "": INSERTs and logs "Server name set: <value>"
//   - If server_name is absent and serverName == "": returns ("", nil) — no error
//
// Call after RunMigrations, before starting the HTTP listener.
func InitServerConfig(dbURL, serverName string) (string, error) {
	database, err := sql.Open("pgx", dbURL)
	if err != nil {
		return "", fmt.Errorf("opening db for server config: %w", err)
	}
	defer database.Close()

	// Check if server_name already set
	var existing string
	err = database.QueryRow("SELECT value FROM server_config WHERE key = 'server_name'").Scan(&existing)
	if err == nil {
		// Row exists: use DB value, ignore env var
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("querying server_config: %w", err)
	}

	// No row: insert from env var if provided
	if serverName == "" {
		return "", nil
	}

	nowMs := time.Now().UnixMilli()
	_, err = database.Exec(
		"INSERT INTO server_config (key, value, set_at) VALUES ('server_name', $1, $2)",
		serverName, nowMs,
	)
	if err != nil {
		return "", fmt.Errorf("inserting server_name: %w", err)
	}

	slog.Info("Server name set: " + serverName)
	return serverName, nil
}

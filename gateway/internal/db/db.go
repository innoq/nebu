package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/nebu/nebu/migrations"
)

// pgx5URL converts a standard postgres:// URL to the pgx5:// scheme required
// by the golang-migrate pgx/v5 database driver.
func pgx5URL(dbURL string) string {
	if strings.HasPrefix(dbURL, "postgres://") {
		return "pgx5://" + dbURL[len("postgres://"):]
	}
	if strings.HasPrefix(dbURL, "postgresql://") {
		return "pgx5://" + dbURL[len("postgresql://"):]
	}
	return dbURL
}

// RunMigrations applies all pending migrations synchronously.
// Returns nil if migrations succeed or there are no pending migrations.
// Call before starting the HTTP listener.
//
// Story 5.29a — AC3: migrations run as nebu_migrate (table owner), not nebu_app
// (runtime role). If migrateURL is empty, falls back to dbURL for backward
// compatibility in test environments that pre-date the role split.
func RunMigrations(dbURL string, migrateURL ...string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("creating migration source: %w", err)
	}

	// Use the dedicated migration URL if provided (nebu_migrate role).
	// Fall back to the runtime URL for backward compatibility.
	effectiveURL := dbURL
	if len(migrateURL) > 0 && migrateURL[0] != "" {
		effectiveURL = migrateURL[0]
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, pgx5URL(effectiveURL))
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// WaitAndRunMigrations retries RunMigrations every 2s until ctx is done or success.
// Use this at gateway startup so the container survives a slow postgres boot in
// environments where start ordering is not guaranteed (e.g. GitLab CI services:).
// Returns immediately on ErrDirty — a dirty schema cannot self-heal by retrying.
func WaitAndRunMigrations(ctx context.Context, dbURL string, migrateURL ...string) error {
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("waiting for database: %w", ctx.Err())
		}
		err := RunMigrations(dbURL, migrateURL...)
		if err == nil {
			return nil
		}
		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			return fmt.Errorf("database schema is dirty (version %d) — run 'migrate force <version>' to recover: %w", dirtyErr.Version, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for database: %w (last migration error: %v)", ctx.Err(), err)
		case <-time.After(2 * time.Second):
		}
	}
}

// CheckDB opens a connection and pings the database.
// Used by the /ready endpoint (Story 1.11) to verify DB availability.
func CheckDB(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening connection: %w", err)
	}
	defer db.Close()
	return db.Ping()
}

// GetMigrationVersion returns the highest applied migration version.
// Returns (0, nil) if no migrations have been applied yet — caller treats 0 as DOWN.
// Used by the /ready endpoint to verify migration state.
func GetMigrationVersion(dbURL string) (int64, error) {
	database, err := sql.Open("pgx", dbURL)
	if err != nil {
		return 0, fmt.Errorf("opening db for migration version: %w", err)
	}
	defer database.Close()

	var version int64
	err = database.QueryRow(
		"SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1",
	).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("querying schema_migrations: %w", err)
	}
	return version, nil
}

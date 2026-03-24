package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
func RunMigrations(dbURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("creating migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, pgx5URL(dbURL))
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
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

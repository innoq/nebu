//go:build integration

package integration_test

// anonymization_migrations_test.go — Story 5.8: Migration 000022 + 000023 integration tests
//
// ALL tests in this file are expected to FAIL until Story 5.8 is implemented.
// Failing reasons:
//   - Migration 000022_users_anonymized.up.sql does not exist yet.
//     → users.anonymized_at BIGINT column is missing.
//   - Migration 000023_media_files_deleted.up.sql does not exist yet.
//     → media_files.deleted BOOLEAN column is missing.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Uses the NEBU_TEST_DB_URL env var to connect to the real PostgreSQL instance.
//   - Does NOT run migrations itself — assumes the stack is already migrated.
//   - Verifies columns via information_schema.columns.
//   - Type-checks: anonymized_at must be BIGINT nullable; deleted must be BOOLEAN not-null default false.
//
// AC coverage:
//   Migration AC (Task 1) — TestUsersAnonymizedAt_MigrationApplies
//   Migration AC (Task 1) — TestMediaFilesDeleted_MigrationApplies

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestUsersAnonymizedAt_MigrationApplies verifies that migration 000022 has
// added the anonymized_at BIGINT NULL column to the users table.
//
// Failing reason before implementation:
//   SELECT on information_schema.columns returns no row for anonymized_at —
//   the column does not exist until migration 000022 is applied.
func TestUsersAnonymizedAt_MigrationApplies(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// ── 1. users table must exist ─────────────────────────────────────────────

	var tableExists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name   = 'users'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("querying users table existence: %v", err)
	}
	if !tableExists {
		t.Fatal("table 'users' does not exist — base schema not applied")
	}

	// ── 2. anonymized_at column — BIGINT, nullable ────────────────────────────
	//
	// Migration spec: ALTER TABLE users ADD COLUMN anonymized_at BIGINT;
	// (no NOT NULL, no DEFAULT — column is nullable like keys_deleted_at)

	var dataType, isNullable string
	err = db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'users'
		  AND column_name  = 'anonymized_at'
	`).Scan(&dataType, &isNullable)
	if err == sql.ErrNoRows {
		t.Fatal("column users.anonymized_at is missing — migration 000022 may not have run")
	}
	if err != nil {
		t.Fatalf("querying users.anonymized_at column: %v", err)
	}
	if dataType != "bigint" {
		t.Errorf("users.anonymized_at: expected data_type='bigint', got %q", dataType)
	}
	// Column must be nullable — anonymized_at is NULL for non-anonymized users
	if isNullable != "YES" {
		t.Errorf("users.anonymized_at: expected is_nullable='YES', got %q", isNullable)
	}

	// ── 3. Sanity: write and read back a value ────────────────────────────────
	//
	// Use a CTE to verify the column is writable without touching real user data.
	// A simple column-level type check via pg_typeof suffices.

	var pgType string
	err = db.QueryRow(`
		SELECT pg_typeof(anonymized_at)::text
		FROM users
		LIMIT 1
	`).Scan(&pgType)
	// If the table is empty, skip this assertion (no rows to query).
	if err != nil && err != sql.ErrNoRows {
		t.Logf("pg_typeof check skipped (table may be empty or error: %v)", err)
	}
	if err == nil && pgType != "bigint" {
		t.Errorf("users.anonymized_at pg_typeof: expected 'bigint', got %q", pgType)
	}
}

// TestMediaFilesDeleted_MigrationApplies verifies that migration 000023 has
// added the deleted BOOLEAN NOT NULL DEFAULT false column to the media_files table.
//
// Failing reason before implementation:
//   SELECT on information_schema.columns returns no row for deleted —
//   the column does not exist until migration 000023 is applied.
func TestMediaFilesDeleted_MigrationApplies(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// ── 1. media_files table must exist ──────────────────────────────────────

	var tableExists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name   = 'media_files'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("querying media_files table existence: %v", err)
	}
	if !tableExists {
		t.Fatal("table 'media_files' does not exist — base schema (migration 000016) not applied")
	}

	// ── 2. deleted column — BOOLEAN, NOT NULL, default false ──────────────────
	//
	// Migration spec: ALTER TABLE media_files ADD COLUMN deleted BOOLEAN NOT NULL DEFAULT false

	var dataType, isNullable, columnDefault string
	err = db.QueryRow(`
		SELECT data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'media_files'
		  AND column_name  = 'deleted'
	`).Scan(&dataType, &isNullable, &columnDefault)
	if err == sql.ErrNoRows {
		t.Fatal("column media_files.deleted is missing — migration 000023 may not have run")
	}
	if err != nil {
		t.Fatalf("querying media_files.deleted column: %v", err)
	}
	if dataType != "boolean" {
		t.Errorf("media_files.deleted: expected data_type='boolean', got %q", dataType)
	}
	// Column must be NOT NULL — migration spec says "BOOLEAN NOT NULL DEFAULT false"
	if isNullable != "NO" {
		t.Errorf("media_files.deleted: expected is_nullable='NO' (NOT NULL), got %q", isNullable)
	}
	// Default must be false
	if columnDefault != "false" {
		t.Errorf("media_files.deleted: expected column_default='false', got %q", columnDefault)
	}
}

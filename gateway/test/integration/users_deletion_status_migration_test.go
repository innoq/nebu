//go:build integration

package integration_test

// users_deletion_status_migration_test.go — Story 5.7: Migration 000021 integration test
//
// ALL tests in this file are expected to FAIL until Story 5.7 is implemented.
// Failing reason: migration 000021_users_deletion_status.up.sql does not exist yet.
// Once the migration file exists and runs, the columns deletion_status and
// keys_deleted_at are verified on the users table.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Uses the NEBU_TEST_DB_URL env var to connect to the real PostgreSQL instance.
//   - Does NOT run migrations itself — assumes the stack is already migrated.
//   - Verifies users.deletion_status TEXT DEFAULT 'active' exists (NOTE: story spec
//     says default 'active' but story AC says no default and uses NULL check; the
//     migration spec in Task 1 omits a DEFAULT so the column is nullable).
//   - Verifies users.keys_deleted_at BIGINT NULL exists (consistent with existing
//     deleted_at BIGINT pattern in user_keys).
//   - Type-checks via information_schema.columns.
//
// AC coverage:
//   Migration AC (Task 1) — TestUsersDeletionStatus_MigrationApplies

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestUsersDeletionStatus_MigrationApplies verifies that migration 000021 has
// added the deletion_status TEXT and keys_deleted_at BIGINT columns to the users
// table with the correct types and nullability.
//
// Failing reason before implementation:
//   SELECT on information_schema.columns returns no row for deletion_status or
//   keys_deleted_at — the columns do not exist until the migration is applied.
func TestUsersDeletionStatus_MigrationApplies(t *testing.T) {
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

	// ── 2. deletion_status column — TEXT, nullable, constraint exists ─────────

	var dsDataType, dsIsNullable string
	err = db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'users'
		  AND column_name  = 'deletion_status'
	`).Scan(&dsDataType, &dsIsNullable)
	if err == sql.ErrNoRows {
		t.Fatal("column users.deletion_status is missing — migration 000021 may not have run")
	}
	if err != nil {
		t.Fatalf("querying users.deletion_status column: %v", err)
	}
	if dsDataType != "text" {
		t.Errorf("users.deletion_status: expected data_type='text', got %q", dsDataType)
	}
	// Column is nullable (no DEFAULT in migration spec; NULL means 'active' for existing rows)
	if dsIsNullable != "YES" {
		t.Errorf("users.deletion_status: expected is_nullable='YES', got %q", dsIsNullable)
	}

	// ── 3. keys_deleted_at column — BIGINT, nullable ──────────────────────────

	var kdDataType, kdIsNullable string
	err = db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'users'
		  AND column_name  = 'keys_deleted_at'
	`).Scan(&kdDataType, &kdIsNullable)
	if err == sql.ErrNoRows {
		t.Fatal("column users.keys_deleted_at is missing — migration 000021 may not have run")
	}
	if err != nil {
		t.Fatalf("querying users.keys_deleted_at column: %v", err)
	}
	if kdDataType != "bigint" {
		t.Errorf("users.keys_deleted_at: expected data_type='bigint', got %q", kdDataType)
	}
	if kdIsNullable != "YES" {
		t.Errorf("users.keys_deleted_at: expected is_nullable='YES', got %q", kdIsNullable)
	}

	// ── 4. deletion_status CHECK constraint exists ────────────────────────────
	//
	// Constraint name is not fixed by spec but must contain the allowed values.

	var constraintDef string
	err = db.QueryRow(`
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'users'::regclass
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) LIKE '%deletion_in_progress%'
		  AND pg_get_constraintdef(oid) LIKE '%keys_deleted%'
	`).Scan(&constraintDef)
	if err == sql.ErrNoRows {
		t.Error("CHECK constraint on users.deletion_status is missing — migration 000021 must add CHECK (deletion_status IN ('deletion_in_progress', 'keys_deleted'))")
	} else if err != nil {
		t.Errorf("querying CHECK constraint on users.deletion_status: %v", err)
	}
}

//go:build integration

package audit_test

// Shared helpers for the audit-log integration tests.
// Extracted from retention_test.go (code-review MINOR-2, 2026-04-23).
//
// Build tag: integration — only compiled when `-tags=integration` is passed.

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openPrivilegedDB opens a DB connection using the privileged migration user
// (NEBU_TEST_MIGRATION_DB_URL) or falls back to NEBU_TEST_DB_URL.
// The privileged user must be the table owner so it can bypass RLS for cleanup.
func openPrivilegedDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if dbURL == "" {
		dbURL = os.Getenv("NEBU_TEST_DB_URL")
	}
	if dbURL == "" {
		t.Skip("NEBU_TEST_DB_URL not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// openAppRoleDB opens a connection as the application role (nebu user) to test RLS.
// NEBU_TEST_DB_URL must point to a connection authenticated as the nebu role.
func openAppRoleDB(t *testing.T) *sql.DB {
	t.Helper()
	dbURL := os.Getenv("NEBU_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("NEBU_TEST_DB_URL not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

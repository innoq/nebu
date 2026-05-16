//go:build integration

package migrations_test

// migrations_048_integration_test.go — Story 14-2a: OIDC Directory Config
// Integration tests for migration 000048 (require a live PostgreSQL via NEBU_TEST_DB_URL).
//
// Covers:
//   TestMigration048_OidcDirectoryRowsSeeded:
//     After migration 000048, server_config must contain rows for both
//     'oidc_directory_enabled' (value='false') and 'oidc_directory_endpoint' (value='').
//   TestMigration048_OidcDirectoryKeysAreMutable:
//     After migration 000048, nebu_app can UPDATE both new keys (they are in the
//     expanded config_update_mutable RLS policy).
//
// Run:  go test -tags=integration ./gateway/migrations/...

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nebu/nebu/migrations"
)

// testDB048URL returns the PostgreSQL connection string for integration tests.
func testDB048URL() string {
	return testDB046URL() // reuse helper from migrations_046_integration_test.go
}

// pgx5URL048 converts postgres:// or postgresql:// to pgx5:// for golang-migrate.
func pgx5URL048(dbURL string) string {
	return pgx5URL046(dbURL) // reuse helper from migrations_046_integration_test.go
}

// newMigrate048 constructs a golang-migrate instance pointed at the embedded FS.
func newMigrate048(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("creating migration source from embedded FS: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, pgx5URL048(dbURL))
	if err != nil {
		t.Fatalf("connecting to database for migrations: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// AT-15 — oidc_directory_enabled and oidc_directory_endpoint rows seeded [AC1]
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration048_OidcDirectoryRowsSeeded verifies that after migration 000048,
// the server_config table contains both new rows with correct default values.
//
// Failing reason before implementation:
//   migration 000048 does not exist → rows are absent → SELECT returns 0 rows.
func TestMigration048_OidcDirectoryRowsSeeded(t *testing.T) {
	dbURL := testDB048URL()
	m := newMigrate048(t, dbURL)
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open superuser db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT key, value FROM server_config WHERE key IN ('oidc_directory_enabled', 'oidc_directory_endpoint') ORDER BY key`,
	)
	if err != nil {
		t.Fatalf("[AT-15] query server_config: %v", err)
	}
	defer rows.Close()

	found := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("[AT-15] scan row: %v", err)
		}
		found[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("[AT-15] rows.Err: %v", err)
	}

	if v, ok := found["oidc_directory_enabled"]; !ok {
		t.Error("[AT-15] expected row 'oidc_directory_enabled' in server_config, not found")
	} else if v != "false" {
		t.Errorf("[AT-15] expected oidc_directory_enabled='false', got %q", v)
	}

	if _, ok := found["oidc_directory_endpoint"]; !ok {
		t.Error("[AT-15] expected row 'oidc_directory_endpoint' in server_config, not found")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AT-16 — oidc_directory keys are mutable via nebu_app RLS policy [AC1]
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration048_OidcDirectoryKeysAreMutable verifies that after migration 000048,
// nebu_app can UPDATE both new server_config keys (they are in the expanded mutable allowlist).
//
// Failing reason before implementation:
//   migration 000048 does not exist → the old config_update_mutable policy (from 000046)
//   does not include the new keys → UPDATE is rejected by RLS (0 rows affected or error).
func TestMigration048_OidcDirectoryKeysAreMutable(t *testing.T) {
	dbURL := testDB048URL()
	m := newMigrate048(t, dbURL)
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	// Ensure rows exist as superuser (ON CONFLICT DO NOTHING means they may already be present).
	superDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open superuser db: %v", err)
	}
	defer superDB.Close()
	_, _ = superDB.Exec(`INSERT INTO server_config (key, value, set_at) VALUES ('oidc_directory_enabled', 'false', 0) ON CONFLICT (key) DO NOTHING`)
	_, _ = superDB.Exec(`INSERT INTO server_config (key, value, set_at) VALUES ('oidc_directory_endpoint', '', 0) ON CONFLICT (key) DO NOTHING`)

	// Connect as nebu_app.
	appURL := strings.Replace(dbURL, "nebu:nebu_dev_password", "nebu_app:nebu_app_dev_pw", 1)
	appDB, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatalf("open db as nebu_app: %v", err)
	}
	defer appDB.Close()

	// UPDATE oidc_directory_enabled — must succeed.
	result, err := appDB.Exec(`UPDATE server_config SET value = 'true' WHERE key = 'oidc_directory_enabled'`)
	if err != nil {
		t.Fatalf("[AT-16] UPDATE oidc_directory_enabled as nebu_app: expected success, got error: %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("[AT-16] expected 1 row updated for oidc_directory_enabled, got %d", rowsAffected)
	}

	// UPDATE oidc_directory_endpoint — must succeed.
	result, err = appDB.Exec(`UPDATE server_config SET value = 'https://idp.example.com/admin/users' WHERE key = 'oidc_directory_endpoint'`)
	if err != nil {
		t.Fatalf("[AT-16] UPDATE oidc_directory_endpoint as nebu_app: expected success, got error: %v", err)
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("[AT-16] expected 1 row updated for oidc_directory_endpoint, got %d", rowsAffected)
	}

	// Restore default values.
	_, _ = superDB.Exec(`UPDATE server_config SET value = 'false' WHERE key = 'oidc_directory_enabled'`)
	_, _ = superDB.Exec(`UPDATE server_config SET value = '' WHERE key = 'oidc_directory_endpoint'`)
}

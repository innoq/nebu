//go:build integration

package migrations_test

// migrations_046_integration_test.go — Story 12.7: SEC Gate 2 Fixes
// Integration tests for migration 000046 (require a live PostgreSQL via NEBU_TEST_DB_URL).
//
// RED PHASE — all tests here MUST FAIL until migration 000046 is applied.
//
// Failing reasons before implementation:
//   TestMigration046_ImmutableKeyUpdateBlocked:
//     - config_update_all policy with USING(true) allows UPDATE on server_name
//     - After 000046, config_update_mutable restricts UPDATE to mutable keys only
//   TestMigration046_MutableKeyUpdateSucceeds:
//     - oidc_user_id_claim is in the mutable allowlist → UPDATE must succeed
//
// Run:  go test -tags=integration ./gateway/migrations/...

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nebu/nebu/migrations"
)

// testDB046URL returns the PostgreSQL connection string for integration tests.
func testDB046URL() string {
	u := os.Getenv("NEBU_TEST_DB_URL")
	if u == "" {
		u = "postgresql://nebu:nebu_dev_password@postgres:5432/nebu"
	}
	return u
}

// pgx5URL046 converts postgres:// or postgresql:// to pgx5:// for golang-migrate.
func pgx5URL046(dbURL string) string {
	if strings.HasPrefix(dbURL, "postgres://") {
		return "pgx5://" + dbURL[len("postgres://"):]
	}
	if strings.HasPrefix(dbURL, "postgresql://") {
		return "pgx5://" + dbURL[len("postgresql://"):]
	}
	return dbURL
}

// newMigrate046 constructs a golang-migrate instance pointed at the embedded FS.
func newMigrate046(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("creating migration source from embedded FS: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, pgx5URL046(dbURL))
	if err != nil {
		t.Fatalf("connecting to database for migrations: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// AT-13 — Immutable key (server_name) UPDATE rejected by RLS [AC5-1]
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration046_ImmutableKeyUpdateBlocked verifies that after migration 000046,
// attempting to UPDATE server_config WHERE key='server_name' as nebu_app raises
// an RLS violation (no rows updated or error).
//
// Failing reason before implementation:
//   migration 000046 does not exist → either config_update_all allows the update
//   (succeeds when it should fail) or the migration fails to apply.

func TestMigration046_ImmutableKeyUpdateBlocked(t *testing.T) {
	dbURL := testDB046URL()
	m := newMigrate046(t, dbURL)
	defer m.Close()

	// Run all migrations up to 000046.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	// Connect as nebu_app to verify RLS.
	appURL := strings.Replace(dbURL, "nebu:nebu_dev_password", "nebu_app:nebu_app_dev_pw", 1)
	db, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatalf("open db as nebu_app: %v", err)
	}
	defer db.Close()

	// Ensure server_name exists in the table (may have been set during bootstrap).
	// First, ensure the row exists by inserting if absent (as a test pre-condition).
	// We use nebu (superuser) for the pre-condition setup, not nebu_app.
	superDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open superuser db: %v", err)
	}
	defer superDB.Close()

	// Ensure server_name row exists.
	_, _ = superDB.Exec(`INSERT INTO server_config (key, value) VALUES ('server_name', 'test.local') ON CONFLICT (key) DO NOTHING`)

	// As nebu_app, attempt to UPDATE server_name — should be blocked by RLS after migration 000046.
	result, err := db.Exec(`UPDATE server_config SET value = 'evil.attacker.com' WHERE key = 'server_name'`)
	if err != nil {
		// RLS can surface as an error — this is also acceptable.
		// Accept any error as the test passing.
		t.Logf("[AT-13] UPDATE server_name returned error (RLS blocked): %v", err)
		return
	}

	// If no error, check rows affected — RLS should result in 0 rows updated.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("[AT-13] RowsAffected: %v", err)
	}
	if rowsAffected != 0 {
		t.Errorf("[AT-13] UPDATE server_name as nebu_app: expected 0 rows affected (RLS blocked), got %d", rowsAffected)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AT-14 — Mutable key (oidc_user_id_claim) UPDATE succeeds [AC5-2]
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration046_MutableKeyUpdateSucceeds verifies that after migration 000046,
// updating oidc_user_id_claim as nebu_app succeeds (it's in the mutable allowlist).
//
// Failing reason before implementation:
//   migration 000046 does not exist → config_update_all allows UPDATE but with wrong scope,
//   or after applying a restrictive policy that also blocks oidc_user_id_claim, the update fails.

func TestMigration046_MutableKeyUpdateSucceeds(t *testing.T) {
	dbURL := testDB046URL()
	m := newMigrate046(t, dbURL)
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	// Ensure oidc_user_id_claim row exists as superuser.
	superDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("open superuser db: %v", err)
	}
	defer superDB.Close()
	_, _ = superDB.Exec(`INSERT INTO server_config (key, value) VALUES ('oidc_user_id_claim', 'name') ON CONFLICT (key) DO NOTHING`)

	// Connect as nebu_app.
	appURL := strings.Replace(dbURL, "nebu:nebu_dev_password", "nebu_app:nebu_app_dev_pw", 1)
	db, err := sql.Open("pgx", appURL)
	if err != nil {
		t.Fatalf("open db as nebu_app: %v", err)
	}
	defer db.Close()

	// As nebu_app, UPDATE oidc_user_id_claim — must succeed (mutable key).
	result, err := db.Exec(`UPDATE server_config SET value = 'email' WHERE key = 'oidc_user_id_claim'`)
	if err != nil {
		t.Fatalf("[AT-14] UPDATE oidc_user_id_claim as nebu_app: expected success, got error: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("[AT-14] RowsAffected: %v", err)
	}
	if rowsAffected != 1 {
		t.Errorf("[AT-14] expected 1 row updated, got %d", rowsAffected)
	}

	// Restore original value.
	_, _ = superDB.Exec(fmt.Sprintf(`UPDATE server_config SET value = 'name' WHERE key = 'oidc_user_id_claim'`))
}

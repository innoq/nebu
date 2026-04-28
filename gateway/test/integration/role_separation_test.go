//go:build integration

package integration_test

// Story 5.29a — Block A: Non-superuser DB role separation (FB-51-01)
//
// These tests verify that the two distinct DB roles (nebu_migrate, nebu_app) are
// correctly provisioned and that nebu_app is a non-superuser with no BYPASSRLS.
//
// ALL tests FAIL until:
//   - nebu_app role is provisioned (not superuser, no BYPASSRLS)
//   - nebu_migrate role is provisioned
//   - nebu_app has no CREATE TABLE privilege
//   - audit_log FORCE RLS policy is in effect for nebu_app (DeleteDenied)
//   - audit_log_purge SECURITY DEFINER function grants EXECUTE to nebu_app
//
// Failing reasons (before implementation):
//   - nebu_app does not exist in pg_roles → Tests 1, 2, 4, 5, 6 fail
//   - nebu_migrate does not exist in pg_roles → Test 3 fails
//
// Build tag: integration — run with:
//   go test -tags=integration ./test/integration/... -v -run TestAppRole
//
// Environment variables:
//   NEBU_TEST_DB_URL            — nebu_app connection (non-superuser, subject to RLS)
//   NEBU_TEST_MIGRATION_DB_URL  — nebu_migrate connection (table owner / privileged)

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openRoleSeparationAppDB opens a connection as nebu_app.
// Uses NEBU_TEST_DB_URL (must point to a nebu_app DSN after Story 5.29a).
func openRoleSeparationAppDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("NEBU_TEST_DB_URL")
	if dsn == "" {
		t.Skip("NEBU_TEST_DB_URL not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open (app role): %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping (app role): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// openRoleSeparationMigrateDB opens a connection as nebu_migrate.
// Uses NEBU_TEST_MIGRATION_DB_URL (must point to a nebu_migrate DSN after Story 5.29a).
func openRoleSeparationMigrateDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if dsn == "" {
		t.Skip("NEBU_TEST_MIGRATION_DB_URL not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open (migrate role): %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping (migrate role): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestAppRole_IsNotSuperuser — AC1
//
// Given: nebu_app role provisioned at DB-init
// When:  SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname='nebu_app'
// Then:  both fields return false
//
// FAILS until nebu_app is provisioned as a non-superuser with no BYPASSRLS.
func TestAppRole_IsNotSuperuser(t *testing.T) {
	// Use MIGRATION_DB_URL to query pg_roles (needs connect privilege to the db)
	dsn := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if dsn == "" {
		dsn = os.Getenv("NEBU_TEST_DB_URL")
	}
	if dsn == "" {
		t.Skip("no DB URL set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}

	ctx := context.Background()
	var rolSuper, rolBypassRLS bool
	err = db.QueryRowContext(ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'nebu_app'`,
	).Scan(&rolSuper, &rolBypassRLS)

	if err == sql.ErrNoRows {
		t.Fatal("AC1 FAIL: nebu_app role does not exist in pg_roles — " +
			"provision it via DB-init script (e.g. dev/postgres/init/01-roles.sql)")
	}
	if err != nil {
		t.Fatalf("AC1 FAIL: querying pg_roles for nebu_app: %v", err)
	}
	if rolSuper {
		t.Error("AC1 FAIL: nebu_app has rolsuper=true — must be a non-superuser role")
	}
	if rolBypassRLS {
		t.Error("AC1 FAIL: nebu_app has rolbypassrls=true — must NOT have BYPASSRLS")
	}
	if !rolSuper && !rolBypassRLS {
		t.Log("AC1 PASS: nebu_app is not superuser and has no BYPASSRLS")
	}
}

// TestAppRole_CannotCreateTable — AC1/AC2
//
// Given: connected as nebu_app (non-superuser, no CREATE privilege)
// When:  CREATE TABLE _rls_privtest_x (id INT) is issued
// Then:  error code 42501 (insufficient_privilege)
//
// FAILS until nebu_app is provisioned without CREATEDB/CREATE privileges.
func TestAppRole_CannotCreateTable(t *testing.T) {
	appDB := openRoleSeparationAppDB(t)
	ctx := context.Background()

	_, err := appDB.ExecContext(ctx,
		`CREATE TABLE _rls_privtest_5_29a (id INT)`)

	if err == nil {
		// Cleanup if the table was accidentally created.
		migrateDB := openRoleSeparationMigrateDB(t)
		_, _ = migrateDB.ExecContext(ctx, `DROP TABLE IF EXISTS _rls_privtest_5_29a`)
		t.Fatal("AC1 FAIL: CREATE TABLE succeeded as nebu_app — " +
			"nebu_app must not have CREATE privilege on the public schema. " +
			"Revoke CREATE from nebu_app in the DB-init script.")
	}

	// Expect SQLSTATE 42501 (insufficient_privilege).
	errMsg := err.Error()
	if !strings.Contains(errMsg, "42501") && !strings.Contains(strings.ToLower(errMsg), "permission denied") &&
		!strings.Contains(strings.ToLower(errMsg), "insufficient_privilege") {
		t.Errorf("AC1 FAIL: unexpected error (expected 42501/permission denied), got: %v", err)
	} else {
		t.Logf("AC1 PASS: CREATE TABLE correctly denied for nebu_app: %v", err)
	}
}

// TestMigrateRole_Exists — AC1
//
// Given: DB-init script has run
// When:  SELECT 1 FROM pg_roles WHERE rolname='nebu_migrate'
// Then:  exactly one row is returned
//
// FAILS until nebu_migrate is provisioned in the DB-init step.
func TestMigrateRole_Exists(t *testing.T) {
	dsn := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if dsn == "" {
		dsn = os.Getenv("NEBU_TEST_DB_URL")
	}
	if dsn == "" {
		t.Skip("no DB URL set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}

	ctx := context.Background()
	var count int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_roles WHERE rolname = 'nebu_migrate'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("AC1 FAIL: querying pg_roles for nebu_migrate: %v", err)
	}
	if count == 0 {
		t.Fatal("AC1 FAIL: nebu_migrate role does not exist in pg_roles — " +
			"provision it via DB-init script (e.g. dev/postgres/init/01-roles.sql)")
	}
	t.Logf("AC1 PASS: nebu_migrate role exists (count=%d)", count)
}

// TestAppRole_CanReadAuditLog — AC4
//
// Given: nebu_app has SELECT privilege on audit_log (via GRANT in migrations)
// When:  SELECT COUNT(*) FROM audit_log as nebu_app
// Then:  succeeds with no error
//
// FAILS if nebu_app lacks SELECT privilege on audit_log (missing GRANT in migration).
func TestAppRole_CanReadAuditLog(t *testing.T) {
	appDB := openRoleSeparationAppDB(t)
	ctx := context.Background()

	var count int
	err := appDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log`,
	).Scan(&count)

	if err != nil {
		t.Fatalf("AC4 FAIL: SELECT from audit_log failed as nebu_app: %v — "+
			"nebu_app must have SELECT privilege. "+
			"Add GRANT SELECT ON audit_log TO nebu_app in a migration.", err)
	}
	t.Logf("AC4 PASS: SELECT from audit_log succeeded as nebu_app (count=%d)", count)
}

// TestAppRole_CannotDeleteAuditLog — AC1/AC5
//
// Given: at least one row in audit_log (seeded via nebu_migrate)
// When:  DELETE FROM audit_log as nebu_app
// Then:  error 42501 (insufficient_privilege) or RLS policy violation
//
// This test verifies that nebu_app is genuinely bound by FORCE RLS,
// not merely the table owner who could otherwise bypass it.
//
// FAILS if:
//   - nebu_app is the table owner (would bypass FORCE RLS)
//   - FORCE ROW LEVEL SECURITY is not set
//   - a DELETE policy mistakenly exists
func TestAppRole_CannotDeleteAuditLog(t *testing.T) {
	// Seed a row via privileged connection for the DELETE attempt.
	migrateDB := openRoleSeparationMigrateDB(t)
	ctx := context.Background()

	var seedID int64
	err := migrateDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ($1, 'sys-role-sep-test', 'role_sep_delete_test', 'success')
		 RETURNING id`,
		time.Now(),
	).Scan(&seedID)
	if err != nil {
		t.Fatalf("AC5 setup: INSERT via nebu_migrate failed: %v — "+
			"ensure nebu_migrate has INSERT privilege on audit_log", err)
	}
	t.Cleanup(func() {
		_, _ = migrateDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
	})
	t.Logf("seeded row id=%d for role separation DELETE test", seedID)

	// Attempt DELETE as nebu_app.
	appDB := openRoleSeparationAppDB(t)
	_, err = appDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE id = $1", seedID)

	if err == nil {
		t.Fatal("AC5 FAIL: DELETE FROM audit_log succeeded as nebu_app — " +
			"nebu_app must be bound by FORCE RLS (not the table owner). " +
			"Transfer audit_log ownership to nebu_migrate and keep FORCE ROW LEVEL SECURITY.")
	}

	errMsg := strings.ToLower(err.Error())
	isExpectedError := strings.Contains(errMsg, "42501") ||
		strings.Contains(errMsg, "row-level security") ||
		strings.Contains(errMsg, "row level security") ||
		strings.Contains(errMsg, "permission denied") ||
		strings.Contains(errMsg, "insufficient_privilege")
	if !isExpectedError {
		t.Errorf("AC5 FAIL: unexpected error (expected RLS/permission denied), got: %v", err)
	} else {
		t.Logf("AC5 PASS: DELETE correctly denied for nebu_app (FORCE RLS active): %v", err)
	}
}

// TestAuditLogPurge_AppRoleCanCallSecurityDefiner — AC4/AC6
//
// Given: nebu_app has EXECUTE privilege on audit_log_purge() (SECURITY DEFINER function)
// When:  SELECT audit_log_purge(30) is called as nebu_app
// Then:  succeeds (SECURITY DEFINER elevates to function owner who can bypass RLS)
//
// Proves that the controlled purge pathway works for nebu_app even though direct
// DELETE is denied. If this test FAILS, check:
//   - GRANT EXECUTE ON FUNCTION audit_log_purge(int) TO nebu_app in migration 000018
//   - Function is declared SECURITY DEFINER
//
// FAILS until nebu_app is provisioned and has EXECUTE on audit_log_purge.
func TestAuditLogPurge_AppRoleCanCallSecurityDefiner(t *testing.T) {
	migrateDB := openRoleSeparationMigrateDB(t)
	ctx := context.Background()

	// Seed a row dated 3001 days ago (beyond any reasonable retention).
	var seedID int64
	oldTS := time.Now().Add(-3001 * 24 * time.Hour).UTC()
	err := migrateDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ($1, 'sys-role-sep-purge', 'role_sep_purge_test', 'success')
		 RETURNING id`,
		oldTS,
	).Scan(&seedID)
	if err != nil {
		t.Fatalf("AC6 setup: privileged INSERT failed: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup in case the purge didn't remove it.
		_, _ = migrateDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
	})

	appDB := openRoleSeparationAppDB(t)

	// Step 1: Confirm direct DELETE is still denied (baseline for this test).
	if _, err := appDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE id = $1", seedID); err == nil {
		t.Fatal("baseline FAIL: direct DELETE succeeded as nebu_app — " +
			"FORCE RLS is not active; the SECURITY DEFINER elevation test is meaningless")
	}

	// Step 2: Call audit_log_purge as nebu_app.
	// SECURITY DEFINER must elevate execution to the function owner (nebu_migrate),
	// allowing the internal DELETE to bypass RLS.
	var deleted int64
	err = appDB.QueryRowContext(ctx,
		"SELECT audit_log_purge($1)", 30,
	).Scan(&deleted)
	if err != nil {
		t.Fatalf("AC6 FAIL: audit_log_purge(30) call failed as nebu_app: %v — "+
			"check that EXECUTE on audit_log_purge is granted to nebu_app "+
			"and that the function is declared SECURITY DEFINER", err)
	}
	if deleted < 1 {
		t.Errorf("AC6 FAIL: audit_log_purge returned deleted=%d, want >= 1 — "+
			"the expired row was not deleted (SECURITY DEFINER elevation may be missing or "+
			"function owner cannot bypass RLS)", deleted)
	}

	// Step 3: Verify the row is gone.
	var remaining int
	if err := appDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", seedID,
	).Scan(&remaining); err != nil {
		t.Fatalf("AC6 verify: SELECT after purge failed: %v", err)
	}
	if remaining != 0 {
		t.Errorf("AC6 FAIL: seeded row still present after audit_log_purge (count=%d) — "+
			"SECURITY DEFINER elevation not effective", remaining)
	} else {
		t.Logf("AC6 PASS: nebu_app called audit_log_purge via SECURITY DEFINER, deleted=%d", deleted)
	}
}

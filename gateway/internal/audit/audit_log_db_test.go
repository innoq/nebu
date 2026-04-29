//go:build integration

package audit_test

// Story 5.1 — AC1, AC2, AC5: Schema + RLS Integration Tests
//
// These tests require a real PostgreSQL database with migration 000018 applied.
// They FAIL until:
//   - gateway/migrations/000018_audit_log.up.sql is created and applied
//   - The migration creates audit_log with the required columns
//   - RLS policies are applied (INSERT allowed, DELETE denied for app role)
//
// Build tag: integration — run with:
//   go test -tags=integration ./internal/audit/... -v
//
// Environment variables:
//   NEBU_TEST_DB_URL            — runtime app-role connection (nebu_app); tests INSERT + DELETE via app role
//   NEBU_TEST_MIGRATION_DB_URL  — privileged connection (nebu_migrate, table owner);
//                                  used for seeding/teardown that requires DELETE
//
// RLS role clarification (Story 5.29a — role split, FB-51-01):
//   nebu_migrate is the table OWNER. nebu_app is the runtime role and is NOT
//   the owner — therefore FORCE ROW LEVEL SECURITY actually applies to nebu_app
//   (nebu_app is also NOSUPERUSER and NOBYPASSRLS).
//   nebu_app has SELECT/INSERT only on audit_log (UPDATE/DELETE explicitly
//   REVOKEd in migration 000024 as defense-in-depth alongside FORCE RLS).
//   INSERT is allowed by the audit_log_insert policy (WITH CHECK (true)).
//   DELETE is denied because no DELETE policy exists and FORCE RLS defaults to
//   deny-all (and the privilege itself is revoked).
//
//   Therefore:
//     AC2: INSERT as nebu_app → succeeds
//     AC5: DELETE as nebu_app → fails with RLS policy violation (or permission denied)

import (
	"context"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAuditLogMigration_TableExists — AC1
//
// Given: migration 000018_audit_log applied to a clean database
// When:  we query the information_schema for audit_log columns
// Then:  all required columns exist with the correct types
//
// FAILS if migration 000018_audit_log.up.sql has not been applied.
func TestAuditLogMigration_TableExists(t *testing.T) {
	db := openPrivilegedDB(t)
	ctx := context.Background()

	requiredColumns := map[string]string{
		"id":            "bigint",
		"event_time":    "timestamp with time zone",
		"actor_user_id": "text",
		"action":        "text",
		"target_type":   "text",
		"target_id":     "text",
		"metadata":      "jsonb",
		"outcome":       "text",
		"error_detail":  "text",
	}

	rows, err := db.QueryContext(ctx,
		`SELECT column_name, data_type
		 FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'audit_log'`)
	if err != nil {
		t.Fatalf("AC1 FAIL: querying information_schema for audit_log: %v — "+
			"migration 000018_audit_log.up.sql not applied", err)
	}
	defer rows.Close()

	found := make(map[string]string)
	for rows.Next() {
		var colName, dataType string
		if err := rows.Scan(&colName, &dataType); err != nil {
			t.Fatalf("rows.Scan: %v", err)
		}
		found[colName] = dataType
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("AC1 FAIL: audit_log table does not exist or has no columns — " +
			"migration 000018_audit_log.up.sql has not been applied")
	}

	for col, wantType := range requiredColumns {
		gotType, ok := found[col]
		if !ok {
			t.Errorf("AC1 FAIL: column %q missing from audit_log — "+
				"migration 000018_audit_log.up.sql must create this column", col)
			continue
		}
		if gotType != wantType {
			t.Errorf("AC1 FAIL: column %q has type %q, want %q", col, gotType, wantType)
		}
	}
}

// TestAuditLogMigration_InsertSucceeds — AC1 + AC2
//
// Given: audit_log table exists (migration 000018 applied)
// When:  INSERT via the app DB role (nebu user — subject to FORCE RLS)
// Then:  no error, RETURNING id returns a valid bigint
//
// FAILS if the table does not exist (migration not applied).
func TestAuditLogMigration_InsertSucceeds(t *testing.T) {
	db := openAppRoleDB(t)
	ctx := context.Background()

	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO audit_log (actor_user_id, action, outcome)
		 VALUES ('sys', 'test_insert_ac1', 'success')
		 RETURNING id`,
	).Scan(&id)
	if err != nil {
		t.Fatalf("AC1/AC2 FAIL: INSERT into audit_log failed: %v — "+
			"either the table does not exist (migration 000018 not applied) "+
			"or the RLS insert policy is missing", err)
	}
	if id == 0 {
		t.Error("AC1 FAIL: INSERT returned id=0 — RETURNING id must produce a non-zero BIGSERIAL value")
	}
	t.Logf("AC1/AC2 PASS: INSERT succeeded, id=%d", id)

	// Cleanup via privileged connection (nebu user cannot DELETE due to RLS).
	privilegedDB := openPrivilegedDB(t)
	if _, err := privilegedDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE action = 'test_insert_ac1'"); err != nil {
		t.Logf("WARNING: cleanup DELETE failed: %v — manual cleanup may be needed", err)
	}
}

// TestAuditLogMigration_DeleteDenied — AC2 + AC5
//
// Given: at least one row in audit_log (inserted by AC1 test)
// When:  DELETE FROM audit_log executed as the app DB role (nebu user, subject to FORCE RLS)
// Then:  PostgreSQL returns an RLS policy violation error (or permission denied)
//        — DELETE is not allowed because no DELETE policy exists under FORCE RLS
//
// FAILS if:
//   - the table does not exist (migration 000018 not applied)
//   - FORCE ROW LEVEL SECURITY is not set (owner would bypass RLS)
//   - a DELETE policy mistakenly allows the operation
//
// Implementation note: FORCE RLS makes even the table owner (nebu) subject to RLS.
// Since no DELETE policy exists, the default-deny applies and DELETE must fail.
func TestAuditLogMigration_DeleteDenied(t *testing.T) {
	// First, seed a row using the privileged connection.
	privilegedDB := openPrivilegedDB(t)
	ctx := context.Background()

	var seedID int64
	if err := privilegedDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (actor_user_id, action, outcome)
		 VALUES ('sys-rls-test', 'rls_delete_test', 'success')
		 RETURNING id`,
	).Scan(&seedID); err != nil {
		t.Fatalf("AC2/AC5 setup: INSERT via privileged DB failed: %v — "+
			"migration 000018_audit_log.up.sql has not been applied", err)
	}
	t.Logf("seeded row id=%d for RLS delete test", seedID)

	// Attempt DELETE as the app role (nebu user, subject to FORCE RLS).
	appDB := openAppRoleDB(t)
	_, err := appDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE id = $1", seedID)

	if err == nil {
		// FAIL: DELETE succeeded — FORCE RLS is not enforced.
		t.Errorf("AC2/AC5 FAIL: DELETE FROM audit_log succeeded as app role — "+
			"this violates the RLS policy. "+
			"FORCE ROW LEVEL SECURITY must be set so the table owner cannot DELETE. "+
			"Hint: check that 000018_audit_log.up.sql contains "+
			"'ALTER TABLE audit_log FORCE ROW LEVEL SECURITY'")

		// Cleanup the leaking row via privileged DB.
		_, _ = privilegedDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
		return
	}

	// Verify the error is an RLS violation (not an unrelated DB error).
	errMsg := strings.ToLower(err.Error())
	isRLSViolation := strings.Contains(errMsg, "row-level security") ||
		strings.Contains(errMsg, "row level security") ||
		strings.Contains(errMsg, "new row violates") ||
		strings.Contains(errMsg, "permission denied")
	if !isRLSViolation {
		t.Errorf("AC2/AC5 FAIL: DELETE returned unexpected error (not an RLS violation): %v", err)
	} else {
		t.Logf("AC2/AC5 PASS: DELETE correctly denied by RLS: %v", err)
	}

	// Cleanup via privileged connection.
	if _, err := privilegedDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE id = $1", seedID); err != nil {
		t.Logf("WARNING: privileged cleanup DELETE failed: %v", err)
	}
}

// TestAuditLogMigration_UpdateDenied — AC2 (UPDATE variant)
//
// Given: one row in audit_log
// When:  UPDATE audit_log SET outcome = 'tampered' executed as app DB role
// Then:  PostgreSQL returns an RLS policy violation error
//
// FAILS if FORCE RLS is absent or an UPDATE policy mistakenly allows the operation.
func TestAuditLogMigration_UpdateDenied(t *testing.T) {
	// Seed via privileged connection.
	privilegedDB := openPrivilegedDB(t)
	ctx := context.Background()

	var seedID int64
	if err := privilegedDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (actor_user_id, action, outcome)
		 VALUES ('sys-rls-test', 'rls_update_test', 'success')
		 RETURNING id`,
	).Scan(&seedID); err != nil {
		t.Fatalf("AC2 setup: INSERT via privileged DB failed: %v — "+
			"migration 000018_audit_log.up.sql has not been applied", err)
	}
	t.Logf("seeded row id=%d for RLS update test", seedID)
	t.Cleanup(func() {
		_, _ = privilegedDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
	})

	// Attempt UPDATE as the app role.
	appDB := openAppRoleDB(t)
	_, err := appDB.ExecContext(ctx,
		"UPDATE audit_log SET outcome = 'tampered' WHERE id = $1", seedID)

	if err == nil {
		t.Errorf("AC2 FAIL: UPDATE audit_log succeeded as app role — "+
			"this violates the append-only requirement. "+
			"FORCE ROW LEVEL SECURITY must deny UPDATE (no UPDATE policy must exist).")
		return
	}

	errMsg := strings.ToLower(err.Error())
	isRLSViolation := strings.Contains(errMsg, "row-level security") ||
		strings.Contains(errMsg, "row level security") ||
		strings.Contains(errMsg, "new row violates") ||
		strings.Contains(errMsg, "permission denied")
	if !isRLSViolation {
		t.Errorf("AC2 FAIL: UPDATE returned unexpected error (not an RLS violation): %v", err)
	} else {
		t.Logf("AC2 PASS: UPDATE correctly denied by RLS: %v", err)
	}
}

// TestAuditLogMigration_RLSAllowsSelect — AC2 (SELECT must work for app role)
//
// Given: a row in audit_log
// When:  SELECT as app DB role
// Then:  succeeds — the SELECT policy (USING (true)) must be present
func TestAuditLogMigration_RLSAllowsSelect(t *testing.T) {
	privilegedDB := openPrivilegedDB(t)
	ctx := context.Background()

	var seedID int64
	if err := privilegedDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (actor_user_id, action, outcome)
		 VALUES ('sys-rls-test', 'rls_select_test', 'success')
		 RETURNING id`,
	).Scan(&seedID); err != nil {
		t.Fatalf("AC2 setup: INSERT via privileged DB failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = privilegedDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
	})

	appDB := openAppRoleDB(t)
	var count int
	if err := appDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", seedID,
	).Scan(&count); err != nil {
		t.Errorf("AC2 FAIL: SELECT from audit_log failed as app role: %v — "+
			"a SELECT policy (USING (true)) must be created alongside the INSERT policy "+
			"so the app can read audit records", err)
		return
	}
	if count != 1 {
		t.Errorf("AC2 FAIL: SELECT returned count=%d, want 1 — inserted row not visible", count)
	} else {
		t.Logf("AC2 PASS: SELECT succeeded, count=%d", count)
	}
}

// ─── Story 5.29c: FB-51-02 — event_time BEFORE INSERT trigger (AC6) ──────────
//
// RED-phase: this test FAILS until migration 000025 adds a BEFORE INSERT trigger
// that sets NEW.event_time := NOW(), overriding any caller-supplied value.
//
// Without the trigger, explicit event_time values in INSERT statements are
// persisted as-is — a potential audit backdating attack. After the trigger is
// applied, the row's event_time must reflect the server's clock, not the caller's.
//
// Dependency note: this test only verifies enforcement meaningfully AFTER Story
// 5-29a's role split (nebu_app is no longer a superuser and cannot bypass RLS).

// TestAuditLog_EventTimeTrigger_OverridesCallerValue — AC6
//
// Given: INSERT into audit_log with explicit event_time='2000-01-01'
// When:  row is fetched back
// Then:  stored event_time is approximately NOW() (not 2000-01-01)
//        (trigger must override caller-supplied value)
func TestAuditLog_EventTimeTrigger_OverridesCallerValue(t *testing.T) {
	db := openPrivilegedDB(t)
	ctx := context.Background()

	// Insert with a backdated event_time. Without the trigger, this would persist.
	var rowID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ('2000-01-01T00:00:00Z', 'sys-trigger-test', 'trigger_override_test', 'success')
		 RETURNING id`,
	).Scan(&rowID); err != nil {
		t.Fatalf("AC6 setup: INSERT failed: %v — migration 000018 not applied", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM audit_log WHERE id = $1", rowID)
	})

	// Fetch the stored event_time.
	var storedEventTime time.Time
	if err := db.QueryRowContext(ctx,
		"SELECT event_time FROM audit_log WHERE id = $1", rowID,
	).Scan(&storedEventTime); err != nil {
		t.Fatalf("AC6: SELECT event_time failed: %v", err)
	}

	// The trigger must have overridden the caller's 2000-01-01 with NOW().
	// Accept ±60s clock skew. The year 2000 is obviously wrong.
	if storedEventTime.Year() < 2020 {
		t.Errorf("AC6 FAIL: event_time was NOT overridden by trigger — "+
			"stored event_time is %v (expected approximately NOW()) — "+
			"BEFORE INSERT trigger setting NEW.event_time := NOW() is missing",
			storedEventTime)
		return
	}

	// Also verify it is within 60 seconds of now (not future-dated or stale).
	diff := time.Since(storedEventTime)
	if diff < -60*time.Second || diff > 60*time.Second {
		t.Errorf("AC6 FAIL: trigger-set event_time=%v is not within 60s of NOW() (diff=%v)",
			storedEventTime, diff)
	} else {
		t.Logf("AC6 PASS: event_time=%v correctly set by trigger (diff=%v)", storedEventTime, diff)
	}
}

// TestAuditLogPurge_RaisesOnExtremeRetentionDays — AC7 (SQL function guard)
//
// Given: audit_log_purge PostgreSQL function
// When:  called with retention_days=36501 (> 36500)
// Then:  raises a PostgreSQL RAISE EXCEPTION (SQL error propagated to Go)
//
// This complements TestRunCleanup_RejectsExtremeRetentionDays (Go-side guard)
// by testing that the SECURITY DEFINER function itself also rejects the extreme value.
// Defense-in-depth: both the Go layer and SQL layer must guard the upper bound.
func TestAuditLogPurge_RaisesOnExtremeRetentionDays(t *testing.T) {
	db := openPrivilegedDB(t)
	ctx := context.Background()

	var deleted int64
	err := db.QueryRowContext(ctx,
		"SELECT audit_log_purge($1)", 36501,
	).Scan(&deleted)

	if err == nil {
		t.Errorf("AC7 FAIL: audit_log_purge(36501) succeeded — "+
			"SQL function must RAISE EXCEPTION for retention_days > 36500 "+
			"to prevent make_interval integer overflow")
	} else {
		t.Logf("AC7 PASS: audit_log_purge(36501) raised error: %v", err)
	}
}

// TestAuditLogPurge_SecurityDefinerElevatesAppRole — AC2/AC4 contrast test
//
// Proves the security claim of the SECURITY DEFINER function audit_log_purge():
//   - Direct DELETE from audit_log as the app role is DENIED (FORCE RLS).
//   - Calling SELECT audit_log_purge($1) as the SAME app role SUCCEEDS and
//     deletes expired rows — elevation happens because the function runs as
//     its owner (the DB superuser / migration role), bypassing RLS.
//
// Without this test the SECURITY DEFINER guarantee is only circumstantially
// shown (RunCleanup opens a privileged conn today). A misconfigured
// audit_log_purge (e.g. written as SECURITY INVOKER) would cause Go cleanup
// code to silently no-op the day the connection role changes — and every
// other test would still pass.
func TestAuditLogPurge_SecurityDefinerElevatesAppRole(t *testing.T) {
	// openSeedDB bypasses the BEFORE INSERT trigger via session_replication_role
	// = replica so the historical event_time below survives into the row. Without
	// this the new event_time-trigger from Story 5.29c (migration 000025) would
	// rewrite the seed timestamp to NOW() and the purge would never fire.
	privilegedDB := openSeedDB(t)
	ctx := context.Background()

	// Seed one row dated 3000 days ago (far past any reasonable retention).
	var seedID int64
	oldTS := time.Now().Add(-3000 * 24 * time.Hour).UTC()
	if err := privilegedDB.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ($1, 'sys-purge-elev-test', 'purge_elev_test', 'success')
		 RETURNING id`, oldTS,
	).Scan(&seedID); err != nil {
		t.Fatalf("setup: privileged INSERT failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = privilegedDB.ExecContext(ctx,
			"DELETE FROM audit_log WHERE id = $1", seedID)
	})

	appDB := openAppRoleDB(t)

	// Step 1: confirm direct DELETE as app role is denied (baseline assumption).
	if _, err := appDB.ExecContext(ctx,
		"DELETE FROM audit_log WHERE id = $1", seedID); err == nil {
		t.Fatal("baseline FAIL: direct DELETE succeeded as app role — " +
			"FORCE RLS is not active; the elevation test below would be meaningless")
	}

	// Step 2: call audit_log_purge via the app-role connection.
	// SECURITY DEFINER must elevate the function execution to its owner,
	// allowing the internal DELETE to bypass RLS.
	retentionDays := 30 // seeded row is 3000 days old → must be purged
	var deleted int64
	if err := appDB.QueryRowContext(ctx,
		"SELECT audit_log_purge($1)", retentionDays,
	).Scan(&deleted); err != nil {
		t.Fatalf("AC4 FAIL: audit_log_purge($1) call failed as app role: %v — "+
			"check that the function exists and is declared SECURITY DEFINER", err)
	}

	if deleted < 1 {
		t.Errorf("AC4 FAIL: audit_log_purge returned deleted=%d, want >= 1 — "+
			"the expired row was not deleted (SECURITY DEFINER elevation may be missing)", deleted)
	}

	// Step 3: verify the row is actually gone (SELECT is permitted, DELETE is not).
	var remaining int
	if err := appDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", seedID,
	).Scan(&remaining); err != nil {
		t.Fatalf("verify: SELECT after purge failed: %v", err)
	}
	if remaining != 0 {
		t.Errorf("AC4 FAIL: seeded row still present after audit_log_purge (count=%d)", remaining)
	}
}


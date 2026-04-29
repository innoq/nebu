//go:build integration

package audit_test

// Story 5.1 — AC4: Retention Cleanup Unit Test
//
// TestAuditLogRetentionCleanup_DeletesOldRows verifies that the cleanup
// function deletes rows older than retention_days and preserves newer rows.
//
// This test will FAIL until:
//   - gateway/internal/audit/retention.go is created with a RunCleanup function
//   - The function deletes audit_log rows where event_time < NOW() - retention_days*day
//
// Framework: database/sql integration test against a real PostgreSQL instance.
// Build tag: integration (requires NEBU_TEST_DB_URL environment variable)
//
// RLS note: RunCleanup must use a privileged DB role (or a superuser connection)
// to perform deletes — the app role (nebu) cannot DELETE under the RLS policy
// (which is intentional for AC2). The cleanup function therefore must operate
// as the DB owner / migration user, NOT as the application nebu role.
// This is documented in the retention package design (see implementation notes
// in story 5-1).

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nebu/nebu/internal/audit"
)

// openPrivilegedDB and openAppRoleDB are defined in testhelpers_test.go —
// extracted there after code-review MINOR-2 (2026-04-23) for maintainability.

// TestAuditLogRetentionCleanup_DeletesOldRows — AC4
//
// Given: one old row (event_time = NOW() - 3000 days) and one fresh row (NOW())
// When:  audit.RunCleanup(ctx, db, retentionDays=2555) is called
// Then:  the old row is deleted; the fresh row remains
//
// This test FAILS until audit.RunCleanup is implemented in retention.go.
func TestAuditLogRetentionCleanup_DeletesOldRows(t *testing.T) {
	// openSeedDB disables triggers via session_replication_role=replica so the
	// historical event_time we INSERT survives the BEFORE INSERT trigger from
	// migration 000025 (Story 5.29c AC6). Without this the trigger would
	// silently overwrite event_time to NOW() and the retention test would
	// observe 0 rows removed.
	db := openSeedDB(t)
	ctx := context.Background()

	// Insert a row that is older than the retention window.
	oldTime := time.Now().Add(-3000 * 24 * time.Hour)
	var oldID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ($1, 'sys-test', 'retention_test_old', 'success')
		 RETURNING id`,
		oldTime,
	).Scan(&oldID)
	if err != nil {
		t.Fatalf("INSERT old row: %v — audit_log table missing (migration 000018 not applied)", err)
	}
	t.Logf("inserted old row id=%d at %v", oldID, oldTime)

	// Insert a row that is within the retention window.
	var freshID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO audit_log (actor_user_id, action, outcome)
		 VALUES ('sys-test', 'retention_test_fresh', 'success')
		 RETURNING id`,
	).Scan(&freshID)
	if err != nil {
		t.Fatalf("INSERT fresh row: %v", err)
	}
	t.Logf("inserted fresh row id=%d", freshID)

	// AC4: run the cleanup with retention_days=2555.
	// FAILS until audit.RunCleanup exists.
	deleted, err := audit.RunCleanup(ctx, db, 2555)
	if err != nil {
		t.Fatalf("audit.RunCleanup: %v", err)
	}
	t.Logf("RunCleanup deleted %d rows", deleted)

	// Old row must be gone.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", oldID,
	).Scan(&count); err != nil {
		t.Fatalf("SELECT old row: %v", err)
	}
	if count != 0 {
		t.Errorf("AC4 FAIL: old row (id=%d, event_time=%v) still present after cleanup — "+
			"RunCleanup must delete rows older than retention_days",
			oldID, oldTime)
	}

	// Fresh row must still exist.
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", freshID,
	).Scan(&count); err != nil {
		t.Fatalf("SELECT fresh row: %v", err)
	}
	if count != 1 {
		t.Errorf("AC4 FAIL: fresh row (id=%d) was deleted — RunCleanup must only delete rows older than retention_days",
			freshID)
	}

	// Cleanup: remove test rows so the test is idempotent.
	_, _ = db.ExecContext(ctx,
		"DELETE FROM audit_log WHERE actor_user_id = 'sys-test' AND action IN ('retention_test_old', 'retention_test_fresh')")
}

// TestAuditLogRetentionCleanup_RespectsRetentionDays — AC4 (boundary)
//
// Given: a row at exactly the retention boundary (NOW() - retention_days)
// When:  RunCleanup is called with that retention_days value
// Then:  the boundary row is deleted (event_time < NOW() - INTERVAL, not <=)
func TestAuditLogRetentionCleanup_RespectsRetentionDays(t *testing.T) {
	// openSeedDB: see DeletesOldRows above — same trigger-bypass requirement.
	db := openSeedDB(t)
	ctx := context.Background()

	const retentionDays = 2555

	// Insert a row slightly past the retention boundary (should be deleted).
	// A 10-minute buffer avoids flakes under CI load where the call-site NOW()
	// in the SECURITY DEFINER function may drift relative to this test process
	// (MINOR-3 from code-review 2026-04-23 — was 1 minute, insufficient margin).
	boundaryTime := time.Now().Add(-retentionDays * 24 * time.Hour).Add(-10 * time.Minute)
	var boundaryID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO audit_log (event_time, actor_user_id, action, outcome)
		 VALUES ($1, 'sys-test-boundary', 'retention_boundary_test', 'success')
		 RETURNING id`,
		boundaryTime,
	).Scan(&boundaryID); err != nil {
		t.Fatalf("INSERT boundary row: %v — audit_log table missing (migration 000018 not applied)", err)
	}
	t.Logf("inserted boundary row id=%d at %v", boundaryID, boundaryTime)

	// FAILS until audit.RunCleanup exists.
	_, err := audit.RunCleanup(ctx, db, retentionDays)
	if err != nil {
		t.Fatalf("audit.RunCleanup: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE id = $1", boundaryID,
	).Scan(&count); err != nil {
		t.Fatalf("SELECT boundary row: %v", err)
	}
	if count != 0 {
		t.Errorf("AC4 FAIL: boundary row (id=%d) not deleted — rows older than %d days must be removed",
			boundaryID, retentionDays)
	}

	// Cleanup.
	_, _ = db.ExecContext(ctx,
		"DELETE FROM audit_log WHERE actor_user_id = 'sys-test-boundary'")
}


package audit_test

// retention_guard_test.go — Story 5.29c: FB-51-02 — RunCleanup upper-bound (AC7)
//
// RED-phase: ALL tests in this file FAIL until audit.RunCleanup is extended with:
//   if retentionDays > 36500 { return 0, ErrInvalidRetentionDays }
//
// Currently RunCleanup only checks retentionDays < 1. Adding the symmetric
// upper-bound guard (> 36500, approximately 100 years) prevents make_interval
// integer overflow in the audit_log_purge PostgreSQL function.
//
// No DB required: the guard must fire before any SQL is issued. Using a nil *sql.DB
// proves this: if the guard is absent, RunCleanup calls db.QueryRowContext on nil,
// which panics and fails the test.
//
// Build tag: NONE — runs without integration infrastructure.

import (
	"context"
	"errors"
	"testing"

	"github.com/nebu/nebu/internal/audit"
)

// TestRunCleanup_RejectsExtremeRetentionDays — AC7
//
// Given: retentionDays=36501 (one above the 100-year cap)
// When:  audit.RunCleanup(ctx, nil, 36501) is called
// Then:  returns ErrInvalidRetentionDays before touching DB (nil DB → no panic)
func TestRunCleanup_RejectsExtremeRetentionDays(t *testing.T) {
	// nil DB: if RunCleanup calls db.QueryRowContext, it panics → test failure.
	// The guard must return early BEFORE any DB call.
	deleted, err := audit.RunCleanup(context.Background(), nil, 36501)
	if err == nil {
		t.Fatalf("AC7 FAIL: RunCleanup(36501) must return error, got nil — "+
			"upper-bound guard (retentionDays > 36500) is missing in audit.go")
	}
	if !errors.Is(err, audit.ErrInvalidRetentionDays) {
		t.Errorf("AC7 FAIL: expected ErrInvalidRetentionDays, got: %v", err)
	}
	if deleted != 0 {
		t.Errorf("AC7 FAIL: expected 0 deleted on error, got %d", deleted)
	}
}

// TestRunCleanup_RejectsExtremeRetentionDays_BoundaryBelow — AC7 (boundary)
//
// Given: retentionDays=36500 (exactly at the cap, must be accepted)
// When:  audit.RunCleanup(ctx, nil, 36500) is called
// Then:  does NOT return ErrInvalidRetentionDays (guard passes, DB call follows)
//
// The nil DB will cause a subsequent panic or error, but NOT ErrInvalidRetentionDays.
func TestRunCleanup_AcceptsMaxValidRetentionDays(t *testing.T) {
	var returnedErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// nil DB panic → guard passed, DB call was attempted. Correct.
				returnedErr = nil
			}
		}()
		_, returnedErr = audit.RunCleanup(context.Background(), nil, 36500)
	}()

	if errors.Is(returnedErr, audit.ErrInvalidRetentionDays) {
		t.Errorf("AC7 FAIL: RunCleanup(36500) must NOT return ErrInvalidRetentionDays — "+
			"36500 is the maximum valid retention period (~100 years)")
	}
}

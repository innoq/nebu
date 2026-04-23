package audit

// Story 5.1 — unit tests for input validation in RunCleanup.
//
// These tests run in the default build (no integration tag) and verify the
// pre-DB guard that prevents a zero or negative retentionDays from ever
// reaching the SECURITY DEFINER function. A corrupted server_config value
// would otherwise destroy the audit history silently.

import (
	"context"
	"errors"
	"testing"
)

func TestRunCleanup_RejectsZero(t *testing.T) {
	_, err := RunCleanup(context.Background(), nil, 0)
	if !errors.Is(err, ErrInvalidRetentionDays) {
		t.Fatalf("RunCleanup(0) returned %v, want ErrInvalidRetentionDays — "+
			"must refuse zero to avoid deleting every row", err)
	}
}

func TestRunCleanup_RejectsNegative(t *testing.T) {
	_, err := RunCleanup(context.Background(), nil, -1)
	if !errors.Is(err, ErrInvalidRetentionDays) {
		t.Fatalf("RunCleanup(-1) returned %v, want ErrInvalidRetentionDays — "+
			"negative days would subtract a negative interval (future-dated filter) "+
			"and effectively purge every row including not-yet-written ones", err)
	}
}

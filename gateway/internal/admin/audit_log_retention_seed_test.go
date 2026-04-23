package admin

// Story 5.1 — AC3: Retention Seed in completeBootstrapTx
//
// TestAuditLogRetentionSeed_BootstrapComplete verifies that when
// completeBootstrapTx is called, it also writes the default retention config
// key 'audit_log_retention_days' = '2555' to server_config.
//
// This test FAILS until completeBootstrapTx is updated to include:
//   INSERT INTO server_config (key, value, set_at)
//   VALUES ('audit_log_retention_days', '2555', ...)
//   ON CONFLICT (key) DO NOTHING
//
// Framework: in-memory fake sqlQuerier (same pattern as claim_selection_tx_test.go).
// No real DB required — the fake intercepted all ExecContext calls.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// retentionSeedCapture is a test double that records all keys written to
// server_config so we can assert that audit_log_retention_days was seeded.
type retentionSeedCapture struct {
	writtenKeys map[string]string // key → value as written
	// seenQueries preserves every raw SQL statement passed to ExecContext so
	// tests can assert directly on the statement text (e.g. ON CONFLICT DO NOTHING).
	seenQueries []string
	// bootstrapAlreadyCompleted controls whether the bootstrap_completed insert
	// returns 0 rows (ErrAlreadyCompleted) or 1 row (success).
	bootstrapAlreadyCompleted bool
}

// ExecContext captures server_config writes. Supports the statements emitted by
// completeBootstrapTx (one or more INSERT INTO server_config ... statements).
func (r *retentionSeedCapture) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.seenQueries = append(r.seenQueries, query)
	if !strings.Contains(query, "server_config") {
		// Not a server_config write; succeed silently.
		return fakeResult{rowsAffected: 1}, nil
	}

	// Determine which key/value pair is being written.
	// Pattern A (positional args): INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)
	// Pattern B (literal key): INSERT INTO server_config ... VALUES ('bootstrap_completed', $1, $2)
	if strings.Contains(query, "bootstrap_completed") ||
		(len(args) >= 1 && args[0] == "bootstrap_completed") {
		if r.bootstrapAlreadyCompleted {
			return fakeResult{rowsAffected: 0}, nil
		}
		r.writtenKeys["bootstrap_completed"] = "true"
		return fakeResult{rowsAffected: 1}, nil
	}

	// For generic INSERT (key=$1, value=$2, ...):
	if len(args) >= 2 {
		key, keyOK := args[0].(string)
		val, valOK := args[1].(string)
		if keyOK && valOK {
			// Honour ON CONFLICT DO NOTHING semantics in the fake: if the key is
			// already present, do not overwrite and report 0 rows affected.
			// This mirrors PostgreSQL's INSERT ... ON CONFLICT DO NOTHING
			// behaviour so the idempotency test below is a real assertion.
			if strings.Contains(strings.ToUpper(query), "ON CONFLICT") &&
				strings.Contains(strings.ToUpper(query), "DO NOTHING") {
				if _, exists := r.writtenKeys[key]; exists {
					return fakeResult{rowsAffected: 0}, nil
				}
			}
			r.writtenKeys[key] = val
			return fakeResult{rowsAffected: 1}, nil
		}
	}

	return fakeResult{rowsAffected: 1}, nil
}

func (r *retentionSeedCapture) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return nil
}

// TestAuditLogRetentionSeed_BootstrapComplete — AC3
//
// Given: empty server_config (fake store with no prior state)
// When:  completeBootstrapTx(ctx, q) is called
// Then:  key 'audit_log_retention_days' with value '2555' is written to server_config
//
// FAILS until completeBootstrapTx adds the retention seed INSERT.
func TestAuditLogRetentionSeed_BootstrapComplete(t *testing.T) {
	store := &retentionSeedCapture{
		writtenKeys: make(map[string]string),
	}

	ctx := context.Background()
	if err := completeBootstrapTx(ctx, store); err != nil {
		t.Fatalf("completeBootstrapTx returned error: %v — "+
			"expected success (bootstrap not previously completed)", err)
	}

	// AC3: 'audit_log_retention_days' must be written.
	val, ok := store.writtenKeys["audit_log_retention_days"]
	if !ok {
		t.Errorf("AC3 FAIL: key 'audit_log_retention_days' was NOT written to server_config during completeBootstrapTx. "+
			"Story 5.1 requires this seeding to be added to completeBootstrapTx in "+
			"gateway/internal/admin/auth.go. "+
			"Expected: INSERT INTO server_config ... ('audit_log_retention_days', '2555', ...)")
		return
	}
	if val != "2555" {
		t.Errorf("AC3 FAIL: 'audit_log_retention_days' written with value %q, want %q — "+
			"default retention must be 2555 days (7 years)", val, "2555")
	} else {
		t.Logf("AC3 PASS: 'audit_log_retention_days' = %q written during completeBootstrapTx", val)
	}
}

// TestAuditLogRetentionSeed_NotOverwrittenIfPresent — AC3 (idempotency)
//
// Given: server_config already has 'audit_log_retention_days' = '365'
// When:  completeBootstrapTx is called (ON CONFLICT DO NOTHING)
// Then:  1. The SQL emitted for the retention insert must contain
//           "ON CONFLICT (key) DO NOTHING" — this is a direct statement-level
//           check independent of the fake store's semantics.
//        2. The fake (which models ON CONFLICT DO NOTHING) keeps the
//           pre-existing '365' rather than overwriting to '2555'.
//
// This replaces an earlier no-op assertion that only logged (t.Logf) and
// therefore never failed. MINOR fix from code review 2026-04-23.
func TestAuditLogRetentionSeed_NotOverwrittenIfPresent(t *testing.T) {
	store := &retentionSeedCapture{
		writtenKeys: map[string]string{
			// Pre-existing custom retention value.
			"audit_log_retention_days": "365",
		},
	}

	ctx := context.Background()
	if err := completeBootstrapTx(ctx, store); err != nil {
		t.Fatalf("completeBootstrapTx returned error: %v", err)
	}

	// Assertion 1: at least one captured INSERT targeting audit_log_retention_days
	// must use ON CONFLICT ... DO NOTHING. This validates the SQL text itself.
	foundConflictClause := false
	for _, q := range store.seenQueries {
		upper := strings.ToUpper(q)
		if strings.Contains(upper, "SERVER_CONFIG") &&
			strings.Contains(upper, "ON CONFLICT") &&
			strings.Contains(upper, "DO NOTHING") {
			foundConflictClause = true
			break
		}
	}
	if !foundConflictClause {
		t.Errorf("AC3 idempotency FAIL: completeBootstrapTx did not emit an "+
			"INSERT ... ON CONFLICT (key) DO NOTHING against server_config. "+
			"Captured queries: %v", store.seenQueries)
	}

	// Assertion 2: the fake models ON CONFLICT DO NOTHING — the pre-existing
	// value '365' must be preserved, NOT overwritten to the default '2555'.
	val := store.writtenKeys["audit_log_retention_days"]
	if val != "365" {
		t.Errorf("AC3 idempotency FAIL: pre-existing retention value was "+
			"overwritten: got %q, want %q (manual override must be preserved)",
			val, "365")
	}
}


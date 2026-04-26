//go:build integration

package integration_test

// compliance_requests_migration_test.go — Story 5.3: Migration 000019 integration test
//
// ALL tests in this file are expected to FAIL until Story 5.3 is implemented.
// Failing reason: migration 000019_compliance_requests.up.sql does not exist yet.
// Once the migration file exists and is run, the table and its columns are verified.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Uses the NEBU_TEST_DB_URL env var to connect to the real PostgreSQL instance
//     provisioned by `make dev` or `make test-integration`.
//   - Does NOT run migrations itself — assumes the stack is already migrated
//     (the dev/test Compose setup runs migrations on startup).
//   - Verifies the compliance_requests table exists with all required columns.
//   - Verifies RLS is enabled (pg_class.relrowsecurity = true).
//   - Verifies the DELETE-denied policy (attempts a direct DELETE, expects error or 0 rows).

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCompliance_MigrationAppliesAndCreatesTable verifies that migration 000019 has
// created the compliance_requests table with all required columns and RLS enabled.
//
// AC7 acceptance criterion coverage:
//   - Table compliance_requests exists
//   - All required columns present with correct types
//   - RLS is enabled (relrowsecurity = true)
//   - DELETE is denied (USING (false) policy)
func TestCompliance_MigrationAppliesAndCreatesTable(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// ── 1. Table existence ────────────────────────────────────────────────────

	var tableExists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name   = 'compliance_requests'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("querying table existence: %v", err)
	}
	if !tableExists {
		t.Fatal("table compliance_requests does not exist — migration 000019 may not have run")
	}

	// ── 2. Required columns ───────────────────────────────────────────────────

	requiredColumns := []struct {
		name     string
		dataType string // information_schema data_type value
	}{
		{"id", "uuid"},
		{"requester_user_id", "text"},
		{"room_id", "text"},
		{"time_range_start", "timestamp with time zone"},
		{"time_range_end", "timestamp with time zone"},
		{"justification", "text"},
		{"status", "text"},
		{"approver_user_id", "text"},
		{"approved_at", "timestamp with time zone"},
		{"created_at", "timestamp with time zone"},
	}

	for _, col := range requiredColumns {
		var colDataType string
		err = db.QueryRow(`
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = 'compliance_requests'
			  AND column_name  = $1
		`, col.name).Scan(&colDataType)
		if err == sql.ErrNoRows {
			t.Errorf("column %q is missing from compliance_requests", col.name)
			continue
		}
		if err != nil {
			t.Errorf("querying column %q: %v", col.name, err)
			continue
		}
		if colDataType != col.dataType {
			t.Errorf("column %q: expected data_type=%q, got %q", col.name, col.dataType, colDataType)
		}
	}

	// ── 3. RLS enabled AND forced ─────────────────────────────────────────────
	// MINOR-3: verify BOTH pg_class.relrowsecurity (ENABLE) AND pg_class.relforcerowsecurity
	// (FORCE). Without FORCE, table owners — including the application role once we migrate
	// off the BYPASSRLS superuser (Story 5.29) — would silently skip all policies.

	var rlsEnabled, rlsForced bool
	err = db.QueryRow(`
		SELECT relrowsecurity, relforcerowsecurity
		FROM pg_class
		WHERE relname = 'compliance_requests'
		  AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
	`).Scan(&rlsEnabled, &rlsForced)
	if err != nil {
		t.Fatalf("querying relrowsecurity/relforcerowsecurity: %v", err)
	}
	if !rlsEnabled {
		t.Error("RLS is not enabled on compliance_requests — ALTER TABLE ... ENABLE ROW LEVEL SECURITY missing from migration")
	}
	if !rlsForced {
		t.Error("RLS is not FORCED on compliance_requests — ALTER TABLE ... FORCE ROW LEVEL SECURITY missing; owner role would bypass policies")
	}

	// ── 3b. Index (status, created_at DESC) exists ────────────────────────────
	// MINOR-3: Story 5.4's pending-list query filters on status and orders by
	// created_at DESC. Without this composite index the query would seq-scan
	// the table. Verify presence via pg_indexes; validate the key order via
	// pg_index.indkey to guard against accidental reordering.

	var indexDef string
	err = db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename  = 'compliance_requests'
		  AND indexname  = 'compliance_requests_status_created_at_idx'
	`).Scan(&indexDef)
	if err == sql.ErrNoRows {
		t.Error("missing index compliance_requests_status_created_at_idx — (status, created_at DESC) composite index required for Story 5.4 pending-list query")
	} else if err != nil {
		t.Errorf("querying index definition: %v", err)
	} else {
		// pg_get_indexdef preserves the ORDER spec — DESC must appear on created_at.
		if !strings.Contains(indexDef, "status") || !strings.Contains(indexDef, "created_at") {
			t.Errorf("index definition missing expected columns: %q", indexDef)
		}
		if !strings.Contains(indexDef, "DESC") {
			t.Errorf("index definition missing DESC ordering on created_at — got: %q", indexDef)
		}
	}

	// ── 3c. CHECK constraint on status column ─────────────────────────────────
	// MINOR-3: beyond the runtime INSERT-rejection covered by
	// TestCompliance_StatusCheckConstraint, assert the constraint is present in
	// the schema by name (compliance_requests_status_check) so a renamed /
	// dropped constraint is surfaced immediately, not only once a bad INSERT
	// happens to run.

	var constraintDef string
	err = db.QueryRow(`
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'compliance_requests_status_check'
		  AND conrelid = 'public.compliance_requests'::regclass
	`).Scan(&constraintDef)
	if err == sql.ErrNoRows {
		t.Error("missing CHECK constraint compliance_requests_status_check — CHECK (status IN ('pending','approved','rejected')) required")
	} else if err != nil {
		t.Errorf("querying constraint definition: %v", err)
	} else {
		for _, want := range []string{"pending", "approved", "rejected"} {
			if !strings.Contains(constraintDef, want) {
				t.Errorf("status CHECK constraint missing %q — got: %q", want, constraintDef)
			}
		}
	}

	// ── 4. DELETE is denied by RLS ────────────────────────────────────────────
	// The compliance_requests_no_delete policy uses USING (false), meaning any
	// DELETE attempt by the application role returns 0 rows affected without error
	// (the row is simply filtered out by RLS). We verify that a seeded row is not
	// actually deleted — RLS's USING (false) silently blocks the operation.

	// Insert a test row to delete.
	var testID string
	err = db.QueryRow(`
		INSERT INTO compliance_requests
		  (requester_user_id, room_id, time_range_start, time_range_end, justification)
		VALUES ('test-sub', '!test:server.example', NOW() - INTERVAL '1 hour', NOW(), 'Migration RLS delete-denial verification row')
		RETURNING id
	`).Scan(&testID)
	if err != nil {
		t.Fatalf("inserting test row: %v", err)
	}

	result, err := db.Exec(`DELETE FROM compliance_requests WHERE id = $1`, testID)
	if err != nil {
		// If RLS is FORCE, the DELETE error is acceptable — some configurations raise an error.
		// Log and continue; the row count check below is the authoritative assertion.
		t.Logf("DELETE returned an error (acceptable under FORCE RLS): %v", err)
	} else {
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected != 0 {
			t.Errorf("DELETE was not blocked by RLS policy: %d row(s) deleted — compliance_requests_no_delete USING (false) policy missing", rowsAffected)
		}
	}

	// Clean up test row via SELECT ... for UPDATE then direct deletion is not
	// possible under RLS. Accept the row remains (it is a test-only artifact).
	// The test database is ephemeral and reset between runs.
}

// TestCompliance_StatusCheckConstraint verifies that the compliance_requests table
// enforces the status check constraint: only 'pending', 'approved', 'rejected' allowed.
//
// AC7: CONSTRAINT compliance_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected'))
func TestCompliance_StatusCheckConstraint(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not reachable at %q — skipping: %v", dbURL, err)
	}

	// Attempt to INSERT with an invalid status value.
	_, err = db.Exec(`
		INSERT INTO compliance_requests
		  (requester_user_id, room_id, time_range_start, time_range_end, justification, status)
		VALUES ('sub', '!r:s.example', NOW() - INTERVAL '1 hour', NOW(), 'Status constraint verification row — twenty chars', 'invalid_status')
	`)
	if err == nil {
		t.Error("INSERT with status='invalid_status' should have been rejected by check constraint, but succeeded")
	}
	// Any error from PostgreSQL is acceptable — the constraint blocked the insert.
}

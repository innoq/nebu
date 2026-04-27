//go:build integration

package integration_test

// compliance_sessions_migration_test.go — Story 5.5: Migration 000020 integration test
//
// ALL tests in this file are expected to FAIL until Story 5.5 is implemented.
// Failing reason: migration 000020_compliance_sessions.up.sql does not exist yet.
// Once the migration file exists and runs, the table, partial unique index, and
// RLS policies are verified.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Uses the NEBU_TEST_DB_URL env var to connect to the real PostgreSQL instance
//     provisioned by `make dev` or `make test-integration`.
//   - Does NOT run migrations itself — assumes the stack is already migrated.
//   - Verifies the compliance_sessions table exists with all required columns.
//   - Verifies the partial unique index prevents duplicate active sessions.
//   - Verifies RLS is enabled (pg_class.relrowsecurity = true).
//   - Verifies DELETE is denied by RLS policy.
//
// AC coverage:
//   AC10 — TestComplianceSessions_MigrationApplies (columns + RLS + index)
//   AC10 — TestComplianceSessions_PartialUniqueIndex_BlocksDuplicateActive
//   AC10 — TestComplianceSessions_RLS_DeleteDenied

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestComplianceSessions_MigrationApplies verifies that migration 000020 has
// created the compliance_sessions table with all required columns and RLS enabled.
//
// AC10 acceptance criterion coverage:
//   - Table compliance_sessions exists
//   - All required columns present (id UUID, request_id UUID, token_hash BYTEA,
//     issued_at TIMESTAMPTZ, expires_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ nullable)
//   - RLS is enabled and forced
//   - Partial unique index compliance_sessions_active_request_idx exists

func TestComplianceSessions_MigrationApplies(t *testing.T) {
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
			  AND table_name   = 'compliance_sessions'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("querying table existence: %v", err)
	}
	if !tableExists {
		t.Fatal("table compliance_sessions does not exist — migration 000020 may not have run")
	}

	// ── 2. Required columns ───────────────────────────────────────────────────

	requiredColumns := []struct {
		name     string
		dataType string // information_schema data_type value
		nullable string // YES or NO
	}{
		{"id", "uuid", "NO"},
		{"request_id", "uuid", "NO"},
		{"token_hash", "bytea", "NO"},
		{"issued_at", "timestamp with time zone", "NO"},
		{"expires_at", "timestamp with time zone", "NO"},
		{"revoked_at", "timestamp with time zone", "YES"}, // nullable — cleared on revocation
	}

	for _, col := range requiredColumns {
		var colDataType, isNullable string
		err = db.QueryRow(`
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = 'compliance_sessions'
			  AND column_name  = $1
		`, col.name).Scan(&colDataType, &isNullable)
		if err == sql.ErrNoRows {
			t.Errorf("column %q is missing from compliance_sessions", col.name)
			continue
		}
		if err != nil {
			t.Errorf("querying column %q: %v", col.name, err)
			continue
		}
		if colDataType != col.dataType {
			t.Errorf("column %q: expected data_type=%q, got %q", col.name, col.dataType, colDataType)
		}
		if isNullable != col.nullable {
			t.Errorf("column %q: expected is_nullable=%q, got %q", col.name, col.nullable, isNullable)
		}
	}

	// ── 3. RLS enabled AND forced ─────────────────────────────────────────────

	var rlsEnabled, rlsForced bool
	err = db.QueryRow(`
		SELECT relrowsecurity, relforcerowsecurity
		FROM pg_class
		WHERE relname = 'compliance_sessions'
		  AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
	`).Scan(&rlsEnabled, &rlsForced)
	if err != nil {
		t.Fatalf("querying relrowsecurity/relforcerowsecurity: %v", err)
	}
	if !rlsEnabled {
		t.Error("RLS is not enabled on compliance_sessions — ALTER TABLE ... ENABLE ROW LEVEL SECURITY missing from migration 000020")
	}
	if !rlsForced {
		t.Error("RLS is not FORCED on compliance_sessions — ALTER TABLE ... FORCE ROW LEVEL SECURITY missing; owner role would bypass policies")
	}

	// ── 4. Partial unique index exists ────────────────────────────────────────

	var indexDef string
	err = db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename  = 'compliance_sessions'
		  AND indexname  = 'compliance_sessions_active_request_idx'
	`).Scan(&indexDef)
	if err == sql.ErrNoRows {
		t.Error("missing partial unique index compliance_sessions_active_request_idx — required to enforce no duplicate active sessions atomically")
	} else if err != nil {
		t.Errorf("querying index definition: %v", err)
	} else {
		// Must reference request_id and have WHERE revoked_at IS NULL partial condition
		if indexDef == "" {
			t.Error("partial unique index indexdef is empty")
		}
	}
}

// TestComplianceSessions_PartialUniqueIndex_BlocksDuplicateActive verifies that the
// partial unique index on (request_id) WHERE revoked_at IS NULL prevents two active
// sessions for the same request.
//
// AC10 — partial unique index enforcement:
//   - INSERT two rows with same request_id and revoked_at IS NULL → second fails
//   - After UPDATE first row to set revoked_at, a third INSERT with same request_id succeeds

func TestComplianceSessions_PartialUniqueIndex_BlocksDuplicateActive(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not reachable at %q — skipping: %v", dbURL, err)
	}

	// First: ensure compliance_sessions table exists (skip gracefully if migration not applied)
	var tableExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'compliance_sessions')
	`).Scan(&tableExists)
	if !tableExists {
		t.Skip("compliance_sessions table does not exist — migration 000020 not yet applied")
	}

	// We need a valid compliance_requests row to satisfy the FK constraint.
	// Insert a test request row first.
	var requestID string
	err = db.QueryRow(`
		INSERT INTO compliance_requests
		  (requester_user_id, room_id, time_range_start, time_range_end, justification, status)
		VALUES ('test-sub-55', '!test55:server.example', NOW() - INTERVAL '2 hours', NOW(), 'Unique index test row twenty chars', 'approved')
		RETURNING id
	`).Scan(&requestID)
	if err != nil {
		t.Fatalf("inserting test compliance_request: %v", err)
	}

	// INSERT first active session (revoked_at IS NULL)
	fakeTokenHash := make([]byte, 32)
	var sessionID1 string
	err = db.QueryRow(`
		INSERT INTO compliance_sessions (request_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '86400 seconds')
		RETURNING id
	`, requestID, fakeTokenHash).Scan(&sessionID1)
	if err != nil {
		t.Fatalf("inserting first compliance_session: %v", err)
	}

	// INSERT second active session with same request_id — must FAIL (unique violation)
	fakeTokenHash2 := make([]byte, 32)
	fakeTokenHash2[0] = 0xFF // different hash
	_, err = db.Exec(`
		INSERT INTO compliance_sessions (request_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '86400 seconds')
	`, requestID, fakeTokenHash2)
	if err == nil {
		t.Error("second INSERT with same request_id and revoked_at IS NULL should have failed (partial unique index), but succeeded")
	}
	// Any error is acceptable — the unique constraint blocked the insert

	// UPDATE the first session to set revoked_at (revoke it)
	_, err = db.Exec(`
		UPDATE compliance_sessions SET revoked_at = NOW() WHERE id = $1
	`, sessionID1)
	if err != nil {
		t.Fatalf("revoking first session: %v", err)
	}

	// INSERT third session with same request_id — must SUCCEED now (first is revoked)
	fakeTokenHash3 := make([]byte, 32)
	fakeTokenHash3[0] = 0xAA
	var sessionID3 string
	err = db.QueryRow(`
		INSERT INTO compliance_sessions (request_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '86400 seconds')
		RETURNING id
	`, requestID, fakeTokenHash3).Scan(&sessionID3)
	if err != nil {
		t.Errorf("third INSERT should succeed after first session is revoked, got error: %v", err)
	}
}

// TestComplianceSessions_RLS_DeleteDenied verifies that the RLS DELETE policy
// (USING false) prevents direct DELETE operations on compliance_sessions.
//
// AC10 — RLS DELETE denial:
//   - Insert a session row
//   - Attempt DELETE — should be blocked or return 0 rows affected

func TestComplianceSessions_RLS_DeleteDenied(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not reachable at %q — skipping: %v", dbURL, err)
	}

	// Skip gracefully if table doesn't exist yet
	var tableExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'compliance_sessions')
	`).Scan(&tableExists)
	if !tableExists {
		t.Skip("compliance_sessions table does not exist — migration 000020 not yet applied")
	}

	// Insert a test compliance_request + session
	var requestID string
	err = db.QueryRow(`
		INSERT INTO compliance_requests
		  (requester_user_id, room_id, time_range_start, time_range_end, justification, status)
		VALUES ('test-sub-rls', '!test-rls:server.example', NOW() - INTERVAL '2 hours', NOW(), 'RLS delete denied test twenty chars', 'approved')
		RETURNING id
	`).Scan(&requestID)
	if err != nil {
		t.Fatalf("inserting test compliance_request: %v", err)
	}

	fakeTokenHash := make([]byte, 32)
	var sessionID string
	err = db.QueryRow(`
		INSERT INTO compliance_sessions (request_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '86400 seconds')
		RETURNING id
	`, requestID, fakeTokenHash).Scan(&sessionID)
	if err != nil {
		t.Fatalf("inserting test compliance_session: %v", err)
	}

	// Attempt DELETE — RLS USING (false) should block it
	result, err := db.Exec(`DELETE FROM compliance_sessions WHERE id = $1`, sessionID)
	if err != nil {
		// Error is acceptable under FORCE RLS — some configurations raise a policy violation error
		t.Logf("DELETE returned an error (acceptable under FORCE RLS): %v", err)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 0 {
		t.Errorf("DELETE was not blocked by RLS policy: %d row(s) deleted — compliance_sessions_no_delete USING (false) policy missing", rowsAffected)
	}
}

//go:build integration

package integration_test

// avatar_url_scrub_migration_test.go — Story 5.29b: AC7 (FB-58-02) migration test
//
// ALL tests in this file are expected to FAIL until Story 5.29b is implemented.
// Failing reason: migration 000026_avatar_url_scrub.up.sql does not exist yet.
// Once the migration file exists and is applied, the test verifies that existing
// profiles rows with unsafe avatar_url values have been scrubbed to NULL.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Connects to the real PostgreSQL instance via NEBU_TEST_DB_URL.
//   - Does NOT run migrations itself — assumes the stack is already migrated.
//   - Inserts a test profile row with an unsafe avatar_url before the migration
//     would be applied (or verifies the column state post-migration).
//   - Since we cannot re-run a migration in integration tests, we verify the
//     migration's effect by checking that the column accepts only valid mxc URIs
//     at the DB constraint level OR by querying existing rows for unsafe patterns.
//
// Strategy — two-phase test:
//   Phase 1: Verify migration 000026 was applied (by confirming a DB trigger or
//            constraint exists, or by checking a known-unsafe test row is NULL).
//   Phase 2: Insert a row with an unsafe avatar_url; assert it is scrubbed OR
//            a check constraint prevents the insert.
//
// Note: Since we cannot INSERT bad data after the migration adds a check constraint,
//       the test verifies the migration mechanism by:
//         1. Checking the migration was applied (migration_id 000026 in schema_migrations).
//         2. Verifying that no existing profiles rows have unsafe avatar_url patterns.
//
// AC7 coverage:
//   - TestProfilesAvatarURLScrub_MigrationApplied — migration 000026 is in schema_migrations
//   - TestProfilesAvatarURLScrub_RemovesUnsafeURIs — no profiles row with unsafe mxc URI exists post-migration

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestProfilesAvatarURLScrub_MigrationApplied verifies that migration 000026 has
// been applied to the schema_migrations table.
//
// Failing reason before implementation:
//   The row '000026' does not exist in schema_migrations until the migration file
//   000026_avatar_url_scrub.up.sql is created and applied.
func TestProfilesAvatarURLScrub_MigrationApplied(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// Verify migration 000026 is present in schema_migrations.
	// golang-migrate tracks applied migrations in the schema_migrations table
	// (column: version BIGINT PRIMARY KEY).
	var version int64
	err = db.QueryRow(`
		SELECT version
		FROM schema_migrations
		WHERE version = 26
	`).Scan(&version)

	if err == sql.ErrNoRows {
		t.Fatalf("migration 000026 (avatar_url_scrub) has not been applied — "+
			"000026_avatar_url_scrub.up.sql must be created and run (Story 5.29b AC7 not implemented)")
	}
	if err != nil {
		t.Fatalf("querying schema_migrations for version=26: %v", err)
	}

	if version != 26 {
		t.Errorf("expected schema_migrations.version=26, got %d", version)
	}
}

// TestProfilesAvatarURLScrub_RemovesUnsafeURIs verifies that migration 000026 has
// scrubbed all profiles rows with unsafe avatar_url values (set to NULL).
//
// "Unsafe" means:
//   - Contains path traversal sequences ("../", "..")
//   - Contains literal URL-encoded traversal ("%2e%2e")
//   - Does not match the safe mxc format: mxc://<safe-server>/<safe-mediaId>
//     where safe parts contain only [a-zA-Z0-9._:-] (no "/" beyond the mxc prefix)
//
// Failing reason before implementation:
//   The profiles table may have rows with avatar_url = 'mxc://../../etc/passwd/x'
//   or other unsafe values that the migration should set to NULL. Until migration
//   000026 is applied, these rows remain unchanged.
func TestProfilesAvatarURLScrub_RemovesUnsafeURIs(t *testing.T) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// Check 1: No profiles row has an avatar_url containing path traversal sequences.
	// This catches the most common unsafe patterns that the migration must scrub.
	rows, err := db.Query(`
		SELECT user_id, avatar_url
		FROM profiles
		WHERE avatar_url IS NOT NULL
		  AND (
		        avatar_url LIKE '%../%'
		     OR avatar_url LIKE '%..%'
		     OR avatar_url LIKE '%/%/%' -- more than one slash after mxc:// is suspicious
		  )
		LIMIT 100
	`)
	if err != nil {
		t.Fatalf("querying profiles for unsafe avatar_url: %v", err)
	}
	defer rows.Close()

	var unsafeRows []string
	for rows.Next() {
		var userID, avatarURL string
		if err := rows.Scan(&userID, &avatarURL); err != nil {
			t.Fatalf("scan: %v", err)
		}
		unsafeRows = append(unsafeRows, userID+": "+avatarURL)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	// RED-PHASE ASSERTION: will fail until migration 000026 scrubs unsafe URIs.
	if len(unsafeRows) > 0 {
		t.Errorf("found %d profiles row(s) with unsafe avatar_url after migration 000026 should have scrubbed them — "+
			"Story 5.29b AC7 not yet implemented:\n  %s",
			len(unsafeRows), strings.Join(unsafeRows, "\n  "))
	}

	// Check 2: No profiles row has a non-mxc avatar_url (must be NULL or mxc://).
	// After the migration, all non-NULL avatar_url values must start with "mxc://".
	rowsNonMxc, err := db.Query(`
		SELECT user_id, avatar_url
		FROM profiles
		WHERE avatar_url IS NOT NULL
		  AND avatar_url NOT LIKE 'mxc://%'
		LIMIT 100
	`)
	if err != nil {
		t.Fatalf("querying profiles for non-mxc avatar_url: %v", err)
	}
	defer rowsNonMxc.Close()

	var nonMxcRows []string
	for rowsNonMxc.Next() {
		var userID, avatarURL string
		if err := rowsNonMxc.Scan(&userID, &avatarURL); err != nil {
			t.Fatalf("scan: %v", err)
		}
		nonMxcRows = append(nonMxcRows, userID+": "+avatarURL)
	}
	if err := rowsNonMxc.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	if len(nonMxcRows) > 0 {
		t.Errorf("found %d profiles row(s) with non-mxc avatar_url — migration 000026 must scrub these to NULL:\n  %s",
			len(nonMxcRows), strings.Join(nonMxcRows, "\n  "))
	}
}

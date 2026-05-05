package migrations_test

// Story 9.14: Admin UI — OIDC Token Refresh — Migration 000039 test
//
// RED PHASE — this test MUST fail until migration 000039 files are created.
//
// AC1: Migration 000039_admin_sessions_refresh_token.up.sql adds a refresh_token TEXT
//      (nullable) column to admin_sessions. Down migration drops the column.
//
// What causes this test to fail right now:
//   - 000039_admin_sessions_refresh_token.up.sql does not exist
//   - 000039_admin_sessions_refresh_token.down.sql does not exist
//
// This test only verifies that both migration files are present in the embedded FS.
// Full up/down execution against a live DB is covered by the integration test suite.

import (
	"testing"

	"github.com/nebu/nebu/migrations"
)

// TestMigration039UpDown verifies that both migration 000039 files exist in the
// embedded FS and are non-empty.
//
// AC1 coverage:
//   - 000039_admin_sessions_refresh_token.up.sql present and non-empty
//   - 000039_admin_sessions_refresh_token.down.sql present and non-empty
func TestMigration039UpDown(t *testing.T) {
	files := []string{
		"000039_admin_sessions_refresh_token.up.sql",
		"000039_admin_sessions_refresh_token.down.sql",
	}

	for _, name := range files {
		f, err := migrations.FS.Open(name)
		if err != nil {
			t.Errorf("AC1 FAIL: embedded FS missing required migration file %q — "+
				"create gateway/migrations/%s", name, name)
			continue
		}

		stat, err := f.Stat()
		_ = f.Close()
		if err != nil {
			t.Errorf("cannot stat %q: %v", name, err)
			continue
		}

		if stat.Size() == 0 {
			t.Errorf("AC1 FAIL: migration file %q is empty — it must contain a valid SQL statement", name)
		}
	}
}

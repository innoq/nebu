package migrations_test

// migrations_042_test.go — Story 11.1: DB Migration + Search Index — Migration 000042
//
// RED PHASE — all integration tests in this file MUST FAIL until migration 000042
// files are created and applied.
//
// Failing reasons (before implementation):
//   - 000043_search_vector.up.sql does not exist yet (AC1 FS test fails)
//   - search_vector column does not exist in events table (AC1 schema test fails)
//   - events_search_vector_gin_idx index does not exist (AC1 index test fails)
//   - events_search_vector_trigger trigger does not exist (AC2 trigger test fails)
//   - search_vector is not populated on INSERT (AC2 trigger test fails)
//   - search_vector is NULL on pre-existing m.room.message rows (AC3 backfill test fails)
//   - search_vector column / index / trigger still present after down migration (AC4 test fails)
//
// Test breakdown:
//   TestMigration042_FilesExist               — AC1 (unit, no build tag, no DB)
//   TestMigration042_ColumnAndIndexExist      — AC1 (integration, requires NEBU_TEST_DB_URL)
//   TestMigration042_TriggerPopulatesSearchVector — AC2 (integration)
//   TestMigration042_BackfillPopulatesExistingRows — AC3 (integration, applies migration step-by-step)
//   TestMigration042_DownMigration            — AC4 (integration)
//
// Build tag for integration tests: //go:build integration
// Run unit test:        go test ./gateway/migrations/...
// Run integration tests: go test -tags=integration ./gateway/migrations/...

import (
	"testing"

	"github.com/nebu/nebu/migrations"
)

// ─────────────────────────────────────────────────────────────────────────────
// AC1 — FS presence test (unit, always runs, no DB required)
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration042_FilesExist verifies that both migration 000042 files exist in
// the embedded FS and are non-empty.
//
// AC1 coverage:
//   - 000043_search_vector.up.sql present and non-empty
//   - 000043_search_vector.down.sql present and non-empty
//
// RED PHASE: fails immediately because the SQL files do not exist yet.
func TestMigration042_FilesExist(t *testing.T) {
	files := []string{
		"000043_search_vector.up.sql",
		"000043_search_vector.down.sql",
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
			t.Errorf("AC1 FAIL: migration file %q is empty — it must contain valid SQL", name)
		}
	}
}

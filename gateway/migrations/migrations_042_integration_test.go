//go:build integration

package migrations_test

// migrations_042_integration_test.go — Story 11.1: DB Migration + Search Index
// Integration tests for migration 000042 (require a live PostgreSQL via NEBU_TEST_DB_URL).
//
// RED PHASE — all tests here MUST FAIL until migration 000042 is applied.
//
// Failing reasons before implementation:
//   TestMigration042_ColumnAndIndexExist:
//     - events.search_vector column does not exist → information_schema query returns no row
//     - events_search_vector_gin_idx index does not exist → pg_indexes query returns no row
//   TestMigration042_TriggerPopulatesSearchVector:
//     - INSERT into events succeeds but search_vector is NULL (trigger not installed)
//     - tsquery match fails because search_vector is NULL
//   TestMigration042_BackfillPopulatesExistingRows:
//     - Pre-seeded m.room.message rows have search_vector = NULL after migration (backfill not run)
//   TestMigration042_DownMigration:
//     - After down migration, search_vector column still exists (DROP not run)
//     - After down migration, GIN index still exists
//     - After down migration, trigger still exists
//     - After down migration, trigger function still exists
//
// Run:  go test -tags=integration ./gateway/migrations/...

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nebu/nebu/migrations"
)

// testDB042URL returns the PostgreSQL connection string for integration tests.
// Falls back to the default Docker Compose connection string.
func testDB042URL() string {
	u := os.Getenv("NEBU_TEST_DB_URL")
	if u == "" {
		u = "postgresql://nebu:nebu_dev_password@postgres:5432/nebu"
	}
	return u
}

// pgx5URL042 converts postgres:// or postgresql:// to pgx5:// for golang-migrate.
func pgx5URL042(dbURL string) string {
	if strings.HasPrefix(dbURL, "postgres://") {
		return "pgx5://" + dbURL[len("postgres://"):]
	}
	if strings.HasPrefix(dbURL, "postgresql://") {
		return "pgx5://" + dbURL[len("postgresql://"):]
	}
	return dbURL
}

// newMigrate042 constructs a golang-migrate instance pointed at the embedded FS.
func newMigrate042(t *testing.T, dbURL string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		t.Fatalf("creating migration source from embedded FS: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, pgx5URL042(dbURL))
	if err != nil {
		t.Fatalf("connecting to database for migrations: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// AC1 — Column + GIN index (up migration)
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration042_ColumnAndIndexExist verifies that after migration 000042 is applied:
//   - events.search_vector tsvector column exists
//   - events_search_vector_gin_idx GIN index exists
//
// RED PHASE: fails because 000043_search_vector.up.sql has not been created and applied.
func TestMigration042_ColumnAndIndexExist(t *testing.T) {
	dbURL := testDB042URL()
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// ── 1. events table must exist (base schema sanity check) ─────────────────

	var tableExists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name   = 'events'
		)
	`).Scan(&tableExists); err != nil {
		t.Fatalf("querying events table existence: %v", err)
	}
	if !tableExists {
		t.Fatal("table 'events' does not exist — base schema not applied")
	}

	// ── 2. search_vector column — tsvector, nullable ──────────────────────────
	//
	// AC1: "column search_vector tsvector is added to the events table"
	// Failing reason: migration 000042 not yet applied → column missing.

	var colDataType string
	err = db.QueryRow(`
		SELECT udt_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'events'
		  AND column_name  = 'search_vector'
	`).Scan(&colDataType)
	if err == sql.ErrNoRows {
		t.Fatal("AC1 FAIL: column events.search_vector is missing — " +
			"run migration 000043_search_vector.up.sql (Story 11.1 AC1 not implemented)")
	}
	if err != nil {
		t.Fatalf("querying events.search_vector column: %v", err)
	}
	// information_schema.columns.udt_name reports 'tsvector' for the tsvector type
	if colDataType != "tsvector" {
		t.Errorf("AC1 FAIL: events.search_vector udt_name: expected 'tsvector', got %q", colDataType)
	}

	// ── 3. GIN index exists ───────────────────────────────────────────────────
	//
	// AC1: "GIN index created via CREATE INDEX CONCURRENTLY"
	// AC1 test spec: indexname = 'events_search_vector_gin_idx'
	// Failing reason: migration 000042 not yet applied → index missing.

	var indexName string
	err = db.QueryRow(`
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename  = 'events'
		  AND indexname  = 'events_search_vector_gin_idx'
	`).Scan(&indexName)
	if err == sql.ErrNoRows {
		t.Fatal("AC1 FAIL: GIN index events_search_vector_gin_idx is missing — " +
			"migration 000043_search_vector.up.sql must CREATE INDEX ... USING GIN (search_vector)")
	}
	if err != nil {
		t.Fatalf("querying pg_indexes for events_search_vector_gin_idx: %v", err)
	}

	// Verify it is actually a GIN index
	var indexDef string
	if err := db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename  = 'events'
		  AND indexname  = 'events_search_vector_gin_idx'
	`).Scan(&indexDef); err != nil {
		t.Fatalf("querying indexdef for events_search_vector_gin_idx: %v", err)
	}
	if !strings.Contains(strings.ToUpper(indexDef), "USING GIN") {
		t.Errorf("AC1 FAIL: index events_search_vector_gin_idx is not a GIN index — indexdef: %q", indexDef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC2 — Automatic population via trigger
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration042_TriggerPopulatesSearchVector verifies that after migration 000042:
//   - Inserting an m.room.message event with body content populates search_vector
//   - The populated search_vector matches a tsquery for a word in the body
//
// RED PHASE: fails because the trigger does not exist yet — search_vector is NULL after INSERT.
//
// AC2 acceptance criterion:
//   Given the index exists,
//   When a new message event is inserted,
//   Then search_vector is populated automatically via a trigger using pg_catalog.simple
//   and source data from content->>'body' (JSONB extraction).
func TestMigration042_TriggerPopulatesSearchVector(t *testing.T) {
	dbURL := testDB042URL()
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot connect to PostgreSQL at %q: %v", dbURL, err)
	}

	// Skip gracefully if migration 000042 was not applied (search_vector column absent).
	// The ColumnAndIndexExist test already catches this with a harder failure.
	var colExists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = 'events'
			  AND column_name  = 'search_vector'
		)
	`).Scan(&colExists); err != nil {
		t.Fatalf("checking search_vector column existence: %v", err)
	}
	if !colExists {
		t.Skip("events.search_vector column does not exist — migration 000042 not applied; " +
			"this test is expected to fail until Story 11.1 is implemented")
	}

	// We need a valid room to satisfy the FK constraint on events.room_id.
	// Use a unique test room alias to avoid conflicts with other tests.
	testRoomID := "!test-trigger-042:test.nebu.local"
	_, _ = db.Exec(`DELETE FROM events WHERE room_id = $1`, testRoomID)
	_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, testRoomID)

	_, err = db.Exec(`
		INSERT INTO rooms (room_id, visibility, created_at)
		VALUES ($1, 'private', 1000)
	`, testRoomID)
	if err != nil {
		t.Fatalf("inserting test room %q: %v", testRoomID, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM events WHERE room_id = $1`, testRoomID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, testRoomID)
	})

	// Insert a message event. The trigger should populate search_vector automatically.
	testEventID := "$test-trigger-042-ev1"
	_, err = db.Exec(`
		INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		VALUES ($1, $2, '@test-trigger-042:test.nebu.local', 'm.room.message',
		        '{"msgtype":"m.text","body":"hello world"}', 1000)
	`, testEventID, testRoomID)
	if err != nil {
		t.Fatalf("inserting test event %q: %v", testEventID, err)
	}

	// ── 1. search_vector must be non-null ─────────────────────────────────────
	//
	// AC2 failing reason: trigger not installed → search_vector is NULL after INSERT.

	var searchVector sql.NullString
	if err := db.QueryRow(`
		SELECT search_vector::text FROM events WHERE event_id = $1
	`, testEventID).Scan(&searchVector); err != nil {
		t.Fatalf("querying search_vector for event %q: %v", testEventID, err)
	}
	if !searchVector.Valid || searchVector.String == "" {
		t.Fatalf("AC2 FAIL: events.search_vector is NULL after INSERT — "+
			"trigger events_search_vector_trigger was not fired (Story 11.1 AC2 not implemented). "+
			"Expected: non-null tsvector containing 'hello'. Got: NULL")
	}

	// ── 2. search_vector matches 'hello' via tsquery ──────────────────────────
	//
	// AC2 spec: search_vector @@ to_tsquery('pg_catalog.simple', 'hello') must be true.

	var matches bool
	if err := db.QueryRow(`
		SELECT search_vector @@ to_tsquery('pg_catalog.simple', 'hello')
		FROM events
		WHERE event_id = $1
	`, testEventID).Scan(&matches); err != nil {
		t.Fatalf("tsquery match check for event %q: %v", testEventID, err)
	}
	if !matches {
		t.Errorf("AC2 FAIL: search_vector does not match to_tsquery('pg_catalog.simple', 'hello') "+
			"for event %q — trigger must extract body from content->>'body' using "+
			"to_tsvector('pg_catalog.simple', ...). Got search_vector=%q",
			testEventID, searchVector.String)
	}

	// ── 3. MINOR-2: non-message event (state event) gets search_vector IS NOT NULL ──
	//
	// Trigger must fire for all event types, including state events with no body.
	// COALESCE(content->>'body', '') ensures an empty tsvector (not NULL) for non-message events.

	stateEventID := "$test-trigger-042-state"
	_, err = db.Exec(`
		INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		VALUES ($1, $2, '@test-trigger-042:test.nebu.local', 'm.room.name',
		        '{"name":"Test Room"}', 1001)
	`, stateEventID, testRoomID)
	if err != nil {
		t.Fatalf("inserting non-message test event %q: %v", stateEventID, err)
	}

	var stateSearchVector sql.NullString
	if err := db.QueryRow(`
		SELECT search_vector::text FROM events WHERE event_id = $1
	`, stateEventID).Scan(&stateSearchVector); err != nil {
		t.Fatalf("querying search_vector for non-message event %q: %v", stateEventID, err)
	}
	if !stateSearchVector.Valid {
		t.Errorf("MINOR-2 FAIL: events.search_vector is NULL for non-message event %q after INSERT — "+
			"trigger must set search_vector = to_tsvector('pg_catalog.simple', '') for events with no body "+
			"(empty tsvector is acceptable, NULL is not)", stateEventID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC3 — Backfill of existing rows
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration042_BackfillPopulatesExistingRows verifies that existing m.room.message
// events have search_vector populated after migration 000042 runs its backfill UPDATE.
//
// Strategy:
//   1. Apply all migrations up to version 41 (one step before 000042).
//   2. Insert seed m.room.message events (search_vector column does not exist yet).
//   3. Apply migration 000042 (adds column, trigger, index, runs backfill).
//   4. Assert seed events have non-null search_vector matching seeded keywords.
//
// RED PHASE: fails because migration 000042 does not exist yet — Step 1 may succeed
// but Step 3 will fail (no migration file) and search_vector will be NULL.
//
// NOTE: This test requires a migration URL with DDL privileges (nebu_migrate role or
// equivalent). It uses NEBU_TEST_MIGRATION_DB_URL if set, otherwise falls back to
// NEBU_TEST_DB_URL. The test runs migrations itself and is NOT safe to run in parallel
// with other migration tests against the same database.
func TestMigration042_BackfillPopulatesExistingRows(t *testing.T) {
	// Use privileged migration URL if available, fall back to app URL.
	migrateDBURL := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if migrateDBURL == "" {
		migrateDBURL = testDB042URL()
	}

	db, err := sql.Open("pgx", migrateDBURL)
	if err != nil {
		t.Fatalf("sql.Open (migrate URL): %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not reachable at migration URL — skipping backfill test: %v", err)
	}

	// ── Step 1: Roll back to version 41 ──────────────────────────────────────
	//
	// We use golang-migrate Steps(-1) to go from current version (≥42) down to 41,
	// or Migrate(41) to force version 41. If the current version is already < 42,
	// we can skip the rollback and just apply forward.
	//
	// IMPORTANT: This test mutates the DB schema. Run in an isolated test DB only.

	m := newMigrate042(t, migrateDBURL)
	defer m.Close()

	currentVersion, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		t.Fatalf("getting current migration version: %v", err)
	}
	if dirty {
		t.Fatalf("migration state is dirty (version=%d) — cannot run backfill test safely", currentVersion)
	}

	// If we are at version ≥ 42, roll back to 41 first.
	if currentVersion >= 42 {
		if err := m.Migrate(41); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("rolling back to version 41: %v", err)
		}
		t.Cleanup(func() {
			// Re-apply 000042 on cleanup so the DB is left in a migrated state.
			m2 := newMigrate042(t, migrateDBURL)
			defer m2.Close()
			if upErr := m2.Up(); upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
				t.Logf("cleanup re-apply 000042 failed (non-fatal): %v", upErr)
			}
		})
	}

	// ── Step 2: Verify search_vector column does NOT exist yet ───────────────

	var colExistsBefore bool
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = 'events'
			  AND column_name  = 'search_vector'
		)
	`).Scan(&colExistsBefore)
	if colExistsBefore {
		t.Log("NOTE: search_vector column already exists at version 41 — " +
			"the backfill test may not exercise the pre-migration state correctly")
	}

	// ── Step 3: Insert seed m.room.message events ────────────────────────────

	seedRoomID := "!test-backfill-042:test.nebu.local"
	_, _ = db.Exec(`DELETE FROM events WHERE room_id = $1`, seedRoomID)
	_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, seedRoomID)

	_, err = db.Exec(`
		INSERT INTO rooms (room_id, visibility, created_at)
		VALUES ($1, 'private', 2000)
	`, seedRoomID)
	if err != nil {
		t.Fatalf("inserting seed room: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM events WHERE room_id = $1`, seedRoomID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, seedRoomID)
	})

	type seedEvent struct {
		eventID string
		body    string
		keyword string // a word from the body to verify with tsquery
	}
	seeds := []seedEvent{
		{"$backfill-042-ev1", "nebuchadnezzar matrix ship", "nebuchadnezzar"},
		{"$backfill-042-ev2", "morpheus red pill blue pill", "morpheus"},
	}

	for _, s := range seeds {
		content := fmt.Sprintf(`{"msgtype":"m.text","body":%q}`, s.body)
		_, err = db.Exec(`
			INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
			VALUES ($1, $2, '@test-backfill-042:test.nebu.local', 'm.room.message', $3::jsonb, 2000)
		`, s.eventID, seedRoomID, content)
		if err != nil {
			t.Fatalf("inserting seed event %q: %v", s.eventID, err)
		}
	}

	// Also insert a non-message event (state event) — backfill must skip it or
	// give it an empty tsvector; it must not be left NULL.
	_, err = db.Exec(`
		INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		VALUES ('$backfill-042-state', $1, '@test-backfill-042:test.nebu.local',
		        'm.room.name', '{"name":"Test Room"}', 1500)
	`, seedRoomID)
	if err != nil {
		t.Fatalf("inserting seed state event: %v", err)
	}

	// ── Step 4: Apply migration 000042 ────────────────────────────────────────
	//
	// RED PHASE: this fails because 000043_search_vector.up.sql does not exist yet.

	if err := m.Migrate(42); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("AC3 FAIL: applying migration 000042: %v — "+
			"000043_search_vector.up.sql does not exist yet (Story 11.1 AC3 not implemented)", err)
	}

	// ── Step 5: Assert backfill populated search_vector on seed events ────────
	//
	// AC3: "all existing events with event_type = 'm.room.message' have non-null search_vector"

	for _, s := range seeds {
		var sv sql.NullString
		if err := db.QueryRow(`
			SELECT search_vector::text FROM events WHERE event_id = $1
		`, s.eventID).Scan(&sv); err != nil {
			t.Errorf("querying search_vector for seed event %q: %v", s.eventID, err)
			continue
		}
		if !sv.Valid {
			t.Errorf("AC3 FAIL: seed event %q has NULL search_vector after backfill — "+
				"migration 000042 backfill UPDATE missed this row", s.eventID)
			continue
		}

		// AC3 spec: search_vector @@ to_tsquery('pg_catalog.simple', keyword) is true
		var matches bool
		if err := db.QueryRow(`
			SELECT search_vector @@ to_tsquery('pg_catalog.simple', $1)
			FROM events WHERE event_id = $2
		`, s.keyword, s.eventID).Scan(&matches); err != nil {
			t.Errorf("tsquery match for seed event %q keyword %q: %v", s.eventID, s.keyword, err)
			continue
		}
		if !matches {
			t.Errorf("AC3 FAIL: seed event %q search_vector does not match keyword %q — "+
				"backfill used wrong text configuration or wrong source column. "+
				"Got search_vector=%q", s.eventID, s.keyword, sv.String)
		}
	}

	// ── Step 6: MINOR-1: state event must have search_vector IS NOT NULL after backfill ──
	//
	// The backfill UPDATE runs for all events (not just m.room.message).
	// State events with no body get an empty tsvector — not NULL.

	var stateSV sql.NullString
	if err := db.QueryRow(`
		SELECT search_vector::text FROM events WHERE event_id = '$backfill-042-state'
	`).Scan(&stateSV); err != nil {
		t.Errorf("MINOR-1: querying search_vector for seed state event: %v", err)
	} else if !stateSV.Valid {
		t.Error("MINOR-1 FAIL: seed state event '$backfill-042-state' has NULL search_vector after backfill — " +
			"migration 000042 backfill must set search_vector = to_tsvector('pg_catalog.simple', '') " +
			"for non-message events (empty tsvector acceptable, NULL is not)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC4 — Down migration (clean rollback)
// ─────────────────────────────────────────────────────────────────────────────

// TestMigration042_DownMigration verifies that rolling back migration 000042 removes:
//   - events.search_vector column
//   - events_search_vector_gin_idx GIN index
//   - events_search_vector_trigger trigger
//   - events_search_vector_update() trigger function
//
// RED PHASE: fails because 000043_search_vector.down.sql does not exist yet, OR
// because the down migration does not DROP all objects cleanly.
//
// AC4 acceptance criterion:
//   Given migration 000043_search_vector.down.sql runs,
//   When it completes,
//   Then the search_vector column, GIN index, trigger, and trigger function are gone.
func TestMigration042_DownMigration(t *testing.T) {
	migrateDBURL := os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if migrateDBURL == "" {
		migrateDBURL = testDB042URL()
	}

	db, err := sql.Open("pgx", migrateDBURL)
	if err != nil {
		t.Fatalf("sql.Open (migrate URL): %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not reachable — skipping down migration test: %v", err)
	}

	m := newMigrate042(t, migrateDBURL)
	defer m.Close()

	// Ensure migration 000042 is applied first.
	if applyErr := m.Migrate(42); applyErr != nil && !errors.Is(applyErr, migrate.ErrNoChange) {
		t.Fatalf("applying migration 000042 before down-migration test: %v — "+
			"000043_search_vector.up.sql does not exist yet (Story 11.1 AC4 not implemented)", applyErr)
	}
	// Always re-apply up migration on cleanup so the DB is left in a forward state.
	t.Cleanup(func() {
		m2 := newMigrate042(t, migrateDBURL)
		defer m2.Close()
		if upErr := m2.Up(); upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
			t.Logf("cleanup re-apply after down migration test: %v (non-fatal)", upErr)
		}
	})

	// Apply down migration: roll back from version 42 to version 41.
	if downErr := m.Migrate(41); downErr != nil && !errors.Is(downErr, migrate.ErrNoChange) {
		t.Fatalf("AC4 FAIL: running down migration 000042: %v — "+
			"000043_search_vector.down.sql does not exist yet (Story 11.1 AC4 not implemented)", downErr)
	}

	// ── 1. search_vector column must be gone ─────────────────────────────────

	var colExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = 'events'
			  AND column_name  = 'search_vector'
		)
	`).Scan(&colExists)
	if colExists {
		t.Error("AC4 FAIL: events.search_vector column still exists after down migration — " +
			"000043_search_vector.down.sql must DROP COLUMN search_vector")
	}

	// ── 2. GIN index must be gone ─────────────────────────────────────────────

	var indexExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'events'
			  AND indexname  = 'events_search_vector_gin_idx'
		)
	`).Scan(&indexExists)
	if indexExists {
		t.Error("AC4 FAIL: GIN index events_search_vector_gin_idx still exists after down migration — " +
			"000043_search_vector.down.sql must DROP INDEX events_search_vector_gin_idx")
	}

	// ── 3. Trigger must be gone ───────────────────────────────────────────────

	var triggerExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.triggers
			WHERE trigger_schema = 'public'
			  AND event_object_table = 'events'
			  AND trigger_name = 'events_search_vector_trigger'
		)
	`).Scan(&triggerExists)
	if triggerExists {
		t.Error("AC4 FAIL: trigger events_search_vector_trigger still exists after down migration — " +
			"000043_search_vector.down.sql must DROP TRIGGER events_search_vector_trigger ON events")
	}

	// ── 4. Trigger function must be gone ─────────────────────────────────────

	var funcExists bool
	_ = db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_proc
			WHERE proname = 'events_search_vector_update'
			  AND pronamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
		)
	`).Scan(&funcExists)
	if funcExists {
		t.Error("AC4 FAIL: trigger function events_search_vector_update() still exists after down migration — " +
			"000043_search_vector.down.sql must DROP FUNCTION events_search_vector_update()")
	}
}

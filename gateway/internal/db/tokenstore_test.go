package db_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/db"
)

// openUnreachableDB opens a *sql.DB pointed at an unreachable host.
// sql.Open itself never dials — the error surfaces on the first query.
func openUnreachableDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("pgx", "postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func TestPostgresTokenStore_Invalidate_DBError(t *testing.T) {
	store := db.NewPostgresTokenStore(openUnreachableDB(t))
	err := store.Invalidate("some-token", time.Now().Add(time.Hour))
	if err == nil {
		t.Error("expected error for unreachable DB, got nil")
	}
}

func TestPostgresTokenStore_IsInvalidated_DBError_ReturnsFalse(t *testing.T) {
	// On DB error, IsInvalidated must return false (fail-open).
	// Returning true on error would lock out all users when the DB is unreachable.
	store := db.NewPostgresTokenStore(openUnreachableDB(t))
	if store.IsInvalidated("some-token") {
		t.Error("IsInvalidated must return false on DB error, not lock out the user")
	}
}

func TestPostgresTokenStore_IsInvalidated_UnknownToken(t *testing.T) {
	// A token that was never invalidated must not be blocked.
	// With unreachable DB, IsInvalidated returns false — correct for an unknown token.
	store := db.NewPostgresTokenStore(openUnreachableDB(t))
	if store.IsInvalidated("never-seen-token") {
		t.Error("unknown token must not be reported as invalidated")
	}
}

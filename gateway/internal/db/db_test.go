package db_test

import (
	"testing"

	"github.com/nebu/nebu/internal/db"
)

func TestRunMigrations_ReturnsErrorOnUnreachableDB(t *testing.T) {
	// Given an unreachable database URL, RunMigrations must return a non-nil error.
	// This validates AC #5: gateway fails fast with an error on unreachable DB.
	err := db.RunMigrations("postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable")

	if err == nil {
		t.Fatal("RunMigrations: expected error for unreachable DB, got nil")
	}
}

func TestRunMigrations_ErrorContainsDiagnosticInfo(t *testing.T) {
	// The error returned by RunMigrations must contain diagnostic information
	// so that main.go can form the AC #5 log message: "database connection failed: <error>"
	err := db.RunMigrations("postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	msg := err.Error()
	if msg == "" {
		t.Error("error message must not be empty")
	}
}

func TestCheckDB_ReturnsErrorOnUnreachableDB(t *testing.T) {
	// Given an unreachable database URL, CheckDB must return a non-nil error.
	// CheckDB is used by Story 1.11 /ready endpoint to verify DB availability.
	err := db.CheckDB("postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable")

	if err == nil {
		t.Fatal("CheckDB: expected error for unreachable DB, got nil")
	}
}

func TestRunMigrations_RejectsEmptyURL(t *testing.T) {
	// An empty URL must fail — ensures callers validate NEBU_DB_URL before calling RunMigrations.
	err := db.RunMigrations("")

	if err == nil {
		t.Fatal("RunMigrations: expected error for empty URL, got nil")
	}
}

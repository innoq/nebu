package db_test

import (
	"testing"

	"github.com/nebu/nebu/internal/db"
)

func TestInitServerConfig_ReturnsErrorOnUnreachableDB(t *testing.T) {
	// Given an unreachable database URL, InitServerConfig must return a non-nil error.
	// Validates AC #5: gateway fails fast on DB errors during startup.
	_, err := db.InitServerConfig("postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable", "chat.example.com")

	if err == nil {
		t.Fatal("InitServerConfig: expected error for unreachable DB, got nil")
	}
}

func TestInitServerConfig_EmptyServerNameReturnsErrorOnUnreachableDB(t *testing.T) {
	// Given an unreachable DB and empty serverName, InitServerConfig still returns
	// an error at the connection attempt (before checking serverName).
	_, err := db.InitServerConfig("postgres://nebu:wrong@localhost:9999/nebu?sslmode=disable", "")

	if err == nil {
		t.Fatal("InitServerConfig: expected error for unreachable DB even with empty serverName, got nil")
	}
}

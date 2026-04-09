package storage

// ─── Story 4-19: local filesystem storage unit tests ─────────────────────────
//
// These tests are written FIRST (ATDD gate), before any implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-19 is implemented.
//
// Test strategy:
//   - Tests call Store(basePath, subDir, name, data) — which does not exist yet.
//   - Compilation will fail: "undefined: Store".
//   - Once fs.go is written with the correct signature, tests must pass.
//   - Tests use t.TempDir() for isolation — no persistent side effects.
//
// Function under test (from story spec):
//   func Store(basePath, subDir, name string, data []byte) (string, error)

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Test 1: Write then Read returns same bytes ────────────────────────────────
//
// AC #4.5 — Store writes encrypted bytes to <basePath>/<subDir>/<name>.
// Reading back those bytes must return exactly what was written.

func TestLocalStorage_WriteRead(t *testing.T) {
	base := t.TempDir()
	subDir := "test.local"
	name := "abc123def456"
	data := []byte("encrypted media payload for nebu test")

	fullPath, err := Store(base, subDir, name, data)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	// Verify the returned path is well-formed.
	expectedPath := filepath.Join(base, subDir, name)
	if fullPath != expectedPath {
		t.Errorf("Store returned path %q, expected %q", fullPath, expectedPath)
	}

	// Read back and compare bytes.
	got, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("ReadFile after Store returned error: %v", err)
	}

	if string(got) != string(data) {
		t.Errorf("bytes read back differ from bytes written\nwant: %q\ngot:  %q", data, got)
	}
}

// ─── Test 2: Store in non-existent subdirectory creates directory ─────────────
//
// AC #4.5 — "create the <server_name> subdirectory if it does not exist".
// Store must call os.MkdirAll — not fail if the directory is absent.

func TestLocalStorage_WriteDoesNotExistDir_Creates(t *testing.T) {
	base := t.TempDir()
	// Use a subDir that definitely does not exist yet.
	subDir := "new-server.example.com"
	name := "media-id-789"
	data := []byte{0x01, 0x02, 0x03, 0xAB, 0xCD}

	// Confirm the subDir does not exist before the call.
	subDirPath := filepath.Join(base, subDir)
	if _, err := os.Stat(subDirPath); !os.IsNotExist(err) {
		t.Fatalf("pre-condition failed: subDir %q already exists or stat error: %v", subDirPath, err)
	}

	fullPath, err := Store(base, subDir, name, data)
	if err != nil {
		t.Fatalf("Store returned error when subDir did not exist: %v", err)
	}

	// The file must exist at the returned path.
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Errorf("file %q was not created by Store", fullPath)
	}

	// The directory must have been created.
	if _, err := os.Stat(subDirPath); os.IsNotExist(err) {
		t.Errorf("subdirectory %q was not created by Store", subDirPath)
	}
}

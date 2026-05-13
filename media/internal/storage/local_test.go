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
//
// ─── Story 12.2: Storer interface + LocalStorer unit tests (ATDD) ────────────
//
// These tests are written FIRST (ATDD gate), before LocalStorer is implemented.
// ALL tests in the 12.2 section are expected to FAIL until local.go is written.
//
// Test strategy:
//   - AT-1: Compile-time check that *LocalStorer satisfies Storer interface
//   - AT-2: Put then Get round-trip returns identical bytes
//   - AT-3: Get on non-existent key returns an error
//   - AT-4: Delete removes the file; subsequent Get returns an error

import (
	"bytes"
	"context"
	"io"
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

// ─── Story 12.2 ATDD Tests ────────────────────────────────────────────────────

// AT-1: Compile-time check that *LocalStorer satisfies the Storer interface.
// This will fail to compile until storage.go defines Storer and local.go defines LocalStorer.
var _ Storer = &LocalStorer{}

// AT-2: Put then Get round-trip returns identical bytes.
//
// AC2 (Story 12.2) — LocalStorer implements Storer; Put followed by Get returns
// the same bytes. Key format is "<serverName>/<mediaID>".

func TestLocalStorer_Put_Get_RoundTrip(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	s := &LocalStorer{BasePath: base}

	key := "test.local/media-round-trip-001"
	data := []byte("nebu round-trip test payload — AES-256-GCM ciphertext placeholder")

	// Put
	if err := s.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Put returned unexpected error: %v", err)
	}

	// Get
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll after Get returned error: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, data)
	}
}

// AT-3: Get on non-existent key returns a non-nil error.
//
// AC2 (Story 12.2) — LocalStorer.Get must return an error for unknown keys.

func TestLocalStorer_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	s := &LocalStorer{BasePath: t.TempDir()}

	_, err := s.Get(ctx, "nonexistent/key-that-was-never-stored")
	if err == nil {
		t.Fatal("expected non-nil error for missing key, got nil")
	}
}

// AT-4: Delete removes the stored file; subsequent Get returns an error.
//
// AC2 (Story 12.2) — LocalStorer.Delete removes the file from disk.

func TestLocalStorer_Delete(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	s := &LocalStorer{BasePath: base}

	key := "test.local/delete-me-001"
	data := []byte("data to be deleted")

	// Store the file first.
	if err := s.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	// Confirm it exists.
	if _, err := s.Get(ctx, key); err != nil {
		t.Fatalf("Get before Delete returned error: %v", err)
	}

	// Delete.
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	// File must be gone from disk.
	subDir, name := splitStorageKey(key)
	filePath := filepath.Join(base, subDir, name)
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("file %q still exists after Delete (stat error: %v)", filePath, err)
	}

	// Get must now return an error.
	if _, err := s.Get(ctx, key); err == nil {
		t.Fatal("expected error from Get after Delete, got nil")
	}
}

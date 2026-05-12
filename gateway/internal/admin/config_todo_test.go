package admin

// TestNoTODOEpic6InConfigGo asserts that gateway/internal/admin/config.go contains
// zero occurrences of the literal string "TODO(epic-6)".
//
// AC3 — Story 9.4
//
// RED PHASE: This test FAILS until Story 9.4 is fully implemented because
// config.go currently contains exactly 2 occurrences of "TODO(epic-6)":
//   - UpdateConfigHandler header (~line 37): stub mutation comment
//   - UpdateConfigHandler body  (~line 62): inline comment before stub mutations
//
// The test passes only when both markers have been removed (replaced with
// real gRPC calls to UpdateServerConfig as documented in Story 9.4 Tasks 2–3).
import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNoTODOEpic6InConfigGo(t *testing.T) {
	// Resolve the path to config.go relative to this test file.
	// runtime.Caller(0) returns the absolute path of this source file at compile time,
	// which is always gateway/internal/admin/config_todo_test.go — so the target file
	// is in the same directory.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot determine test file path")
	}
	dir := filepath.Dir(testFile)
	configGoPath := filepath.Join(dir, "config.go")

	content, err := os.ReadFile(configGoPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", configGoPath, err)
	}

	marker := []byte("TODO(epic-6)")
	if bytes.Contains(content, marker) {
		// Count occurrences for a helpful failure message.
		count := bytes.Count(content, marker)
		t.Errorf(
			"%s still contains %d occurrence(s) of %q — "+
				"remove all TODO(epic-6) markers before marking Story 9.4 done",
			configGoPath, count, string(marker),
		)
	}
}

func TestNoTODOEpic6InRoleMappingGo(t *testing.T) {
	// Resolve the path to role_mapping.go relative to this test file.
	// runtime.Caller(0) returns the absolute path of this source file at compile time,
	// which is always gateway/internal/admin/config_todo_test.go — so the target file
	// is in the same directory.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot determine test file path")
	}
	dir := filepath.Dir(testFile)
	roleMappingGoPath := filepath.Join(dir, "role_mapping.go")

	content, err := os.ReadFile(roleMappingGoPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", roleMappingGoPath, err)
	}

	marker := []byte("TODO(epic-6)")
	if bytes.Contains(content, marker) {
		// Count occurrences for a helpful failure message.
		count := bytes.Count(content, marker)
		t.Errorf(
			"%s still contains %d occurrence(s) of %q — "+
				"remove all TODO(epic-6) markers before marking Story 9.4 done",
			roleMappingGoPath, count, string(marker),
		)
	}
}

package admin

// TestNoTODOEpic6InUsersGo asserts that gateway/internal/admin/users.go contains
// zero occurrences of the literal string "TODO(epic-6)".
//
// AC5 — Story 9.2
//
// RED PHASE: This test FAILS until Story 9.2 is fully implemented because
// users.go currently contains exactly 4 occurrences of "TODO(epic-6)":
//   - UpdateRoleHandler      (~line 159): stub mutation of stubUsers[i].Role
//   - DeactivateUserHandler  (~line 183): stub mutation of stubUsers[i].Status
//   - ReactivateUserHandler  (~line 198): stub mutation of stubUsers[i].Status
//   - UpdateDisplayNameHandler (~line 212): stub mutation of stubUsers[i].DisplayName
//
// The test passes only when all four markers have been removed (replaced with
// real gRPC calls or — in the case of UpdateDisplayNameHandler — with an explicit
// "// NOTE: deferred to follow-up story" comment as documented in Dev Notes).
import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNoTODOEpic6InUsersGo(t *testing.T) {
	// Resolve the path to users.go relative to this test file.
	// runtime.Caller(0) returns the absolute path of this source file at compile time,
	// which is always gateway/internal/admin/users_todo_test.go — so the target file
	// is in the same directory.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot determine test file path")
	}
	dir := filepath.Dir(testFile)
	usersGoPath := filepath.Join(dir, "users.go")

	content, err := os.ReadFile(usersGoPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", usersGoPath, err)
	}

	marker := []byte("TODO(epic-6)")
	if bytes.Contains(content, marker) {
		// Count occurrences for a helpful failure message.
		count := bytes.Count(content, marker)
		t.Errorf(
			"%s still contains %d occurrence(s) of %q — "+
				"remove all TODO(epic-6) markers before marking Story 9.2 done",
			usersGoPath, count, string(marker),
		)
	}
}

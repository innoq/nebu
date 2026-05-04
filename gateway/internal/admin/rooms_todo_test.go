package admin

// TestNoTODOEpic6InRoomsGo asserts that gateway/internal/admin/rooms.go contains
// zero occurrences of the literal string "TODO(epic-6)".
//
// AC4 — Story 9.3
//
// RED PHASE: This test FAILS until Story 9.3 is fully implemented because
// rooms.go currently contains exactly 3 occurrences of "TODO(epic-6)":
//   - UpdateRoomNameHandler (~line 148): stub mutation of stubRooms[i].Name
//   - ArchiveRoomHandler    (~line 178): stub mutation of stubRooms[i].Status → "archived"
//   - UnarchiveRoomHandler  (~line 199): stub mutation of stubRooms[i].Status → "active"
//
// The test passes only when all three markers have been removed (replaced with
// real gRPC calls to ArchiveRoom, UnarchiveRoom, and UpdateRoomSettings as
// documented in Story 9.3 Tasks 4–6).
//
// Note: there is also a non-epic-6 "TODO:" on line ~125 (rune-aware initials helper).
// That marker does NOT contain the substring "TODO(epic-6)" and is therefore
// intentionally excluded from this assertion.
import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNoTODOEpic6InRoomsGo(t *testing.T) {
	// Resolve the path to rooms.go relative to this test file.
	// runtime.Caller(0) returns the absolute path of this source file at compile time,
	// which is always gateway/internal/admin/rooms_todo_test.go — so the target file
	// is in the same directory.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot determine test file path")
	}
	dir := filepath.Dir(testFile)
	roomsGoPath := filepath.Join(dir, "rooms.go")

	content, err := os.ReadFile(roomsGoPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", roomsGoPath, err)
	}

	marker := []byte("TODO(epic-6)")
	if bytes.Contains(content, marker) {
		// Count occurrences for a helpful failure message.
		count := bytes.Count(content, marker)
		t.Errorf(
			"%s still contains %d occurrence(s) of %q — "+
				"remove all TODO(epic-6) markers before marking Story 9.3 done",
			roomsGoPath, count, string(marker),
		)
	}
}

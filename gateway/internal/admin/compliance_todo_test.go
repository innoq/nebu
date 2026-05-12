package admin

// TestNoTODOEpic6InComplianceHandlerGo asserts that
// gateway/internal/admin/compliance_handler.go contains zero occurrences of
// the literal string "TODO(epic-6)".
//
// AC3 — Story 9.5
//
// RED PHASE: This test FAILS until Story 9.5 is fully implemented because
// compliance_handler.go currently contains exactly 2 occurrences of "TODO(epic-6)":
//   - ApproveHandler (~line 52): stub mutation cr.Status = "approved"
//   - RejectHandler  (~line 69): stub mutation cr.Status = "rejected"
//
// The test passes only when both markers have been removed (replaced with
// real DB/API calls via ComplianceApprovalClient).
import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNoTODOEpic6InComplianceHandlerGo(t *testing.T) {
	// Resolve the path to compliance_handler.go relative to this test file.
	// runtime.Caller(0) returns the absolute path of this source file at compile
	// time, which is always gateway/internal/admin/compliance_todo_test.go — so
	// the target file is in the same directory.
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot determine test file path")
	}
	dir := filepath.Dir(testFile)
	handlerPath := filepath.Join(dir, "compliance_handler.go")

	content, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", handlerPath, err)
	}

	marker := []byte("TODO(epic-6)")
	if bytes.Contains(content, marker) {
		// Count occurrences for a helpful failure message.
		count := bytes.Count(content, marker)
		t.Errorf(
			"%s still contains %d occurrence(s) of %q — "+
				"remove all TODO(epic-6) markers before marking Story 9.5 done",
			handlerPath, count, string(marker),
		)
	}
}

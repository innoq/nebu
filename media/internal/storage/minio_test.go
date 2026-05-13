package storage_test

// ─── Story 12.4 ATDD Tests — MinIOStorer.Get error classification ─────────────
//
// These tests will FAIL until:
//   1. storage.ErrNotFound and storage.ErrStorageUnavailable sentinels are defined
//      in storage/storage.go
//   2. classifyMinIOError (or equivalent logic) is added to storage/minio.go
//   3. MinIOStorer.Get calls obj.Stat() after GetObject and classifies errors via
//      classifyMinIOError
//
// Test strategy:
//   - classifyMinIOError is tested directly (package-level function) by constructing
//     a minio.ErrorResponse and wrapping it as an error.
//   - No real MinIO connection is needed.
//   - errors.Is() checks verify the sentinel wrapping.

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/nebu/nebu/media/internal/storage"
)

// ─── AT-1: classifyMinIOError with NoSuchKey code → ErrNotFound ───────────────
//
// Story 12.4 AC3 — MinIOStorer.Get must return ErrNotFound when MinIO reports
// NoSuchKey. classifyMinIOError is a helper exported (or testable) from the
// storage package.
//
// Failing reason before implementation:
//   storage.ErrNotFound is not defined; classifyMinIOError does not exist.

func TestClassifyMinIOError_NoSuchKey_ReturnsErrNotFound(t *testing.T) {
	// Construct a minio.ErrorResponse with Code "NoSuchKey" (equivalent to S3 404).
	rawErr := minio.ErrorResponse{
		Code:       "NoSuchKey",
		Message:    "The specified key does not exist.",
		StatusCode: http.StatusNotFound,
	}
	// Wrap as an error the same way the MinIO SDK returns it.
	wrapped := fmt.Errorf("minio SDK error: %w", rawErr)

	// classifyMinIOError must return an error that wraps ErrNotFound.
	classified := storage.ClassifyMinIOError(wrapped)

	if !errors.Is(classified, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound for NoSuchKey error, got: %v (type %T)", classified, classified)
	}
	if errors.Is(classified, storage.ErrStorageUnavailable) {
		t.Errorf("NoSuchKey must NOT map to ErrStorageUnavailable")
	}
}

// ─── AT-2: classifyMinIOError with network error → ErrStorageUnavailable ──────
//
// Story 12.4 AC4 — When MinIO is unreachable (no MinIO ErrorResponse Code),
// classifyMinIOError must return an error wrapping ErrStorageUnavailable.
//
// Failing reason before implementation:
//   storage.ErrStorageUnavailable is not defined; classifyMinIOError does not exist.

func TestClassifyMinIOError_NetworkError_ReturnsErrStorageUnavailable(t *testing.T) {
	// A plain network error — not a minio.ErrorResponse.
	networkErr := fmt.Errorf("dial tcp: connection refused")

	classified := storage.ClassifyMinIOError(networkErr)

	if !errors.Is(classified, storage.ErrStorageUnavailable) {
		t.Errorf("expected ErrStorageUnavailable for network error, got: %v (type %T)", classified, classified)
	}
	if errors.Is(classified, storage.ErrNotFound) {
		t.Errorf("network error must NOT map to ErrNotFound")
	}
}

// ─── AT-3: errors.Is(ErrNotFound, ErrNotFound) == true ────────────────────────
//
// Story 12.4 AC3 — Sentinel errors must be usable with errors.Is.
//
// Failing reason before implementation:
//   storage.ErrNotFound is not defined.

func TestSentinel_ErrNotFound_IsItself(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", storage.ErrNotFound)
	if !errors.Is(wrapped, storage.ErrNotFound) {
		t.Error("errors.Is must return true for wrapped ErrNotFound")
	}
}

// ─── AT-4: errors.Is(ErrStorageUnavailable, ErrStorageUnavailable) == true ────
//
// Story 12.4 AC4 — Sentinel errors must be usable with errors.Is.
//
// Failing reason before implementation:
//   storage.ErrStorageUnavailable is not defined.

func TestSentinel_ErrStorageUnavailable_IsItself(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", storage.ErrStorageUnavailable)
	if !errors.Is(wrapped, storage.ErrStorageUnavailable) {
		t.Error("errors.Is must return true for wrapped ErrStorageUnavailable")
	}
}

// ─── AT-5: classifyMinIOError with StatusCode 404 → ErrNotFound ───────────────
//
// Story 12.4 AC3 — MinIO may also return StatusCode 404 without Code "NoSuchKey"
// in some edge cases. classifyMinIOError must handle both.
//
// Failing reason before implementation:
//   classifyMinIOError does not exist.

func TestClassifyMinIOError_StatusCode404_ReturnsErrNotFound(t *testing.T) {
	rawErr := minio.ErrorResponse{
		Code:       "", // empty code — only status code is set
		Message:    "Not Found",
		StatusCode: http.StatusNotFound,
	}
	wrapped := fmt.Errorf("minio SDK error: %w", rawErr)

	classified := storage.ClassifyMinIOError(wrapped)

	if !errors.Is(classified, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound for StatusCode 404 error, got: %v (type %T)", classified, classified)
	}
}

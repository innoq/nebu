package admin

// bootstrap_scim.go — Story 14-3c: SCIM 2.0 User Fetch + Progress Tracking
//
// This file extends BootstrapHandler (bootstrap.go) with:
//   1. SCIMFetcher interface — SCIM 2.0 counterpart to OIDCDirectoryFetcher.
//   2. WithSCIMFetcher — wires a SCIMClient into BootstrapHandler.
//   3. Package-level import progress singletons — importInProgress (atomic.Bool)
//      and importProgress (*importProgressState with atomic.Int32 counters).
//   4. importStatusHandler — serves GET /api/v1/admin/bootstrap/import-status as JSON.
//   5. resetImportState — test-helper to reset singletons between unit tests.
//
// Security requirements (from _bmad-output/implementation-artifacts/security-guide-scim-2026-05-16.md):
//   HR-3: singleton lock → HTTP 409 when a second import is triggered (handled in bootstrap.go step 4).
//   CR-3: import-status endpoint is registered behind sessionGuard in main.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// SCIMFetcher is the interface used by BootstrapHandler to access SCIM 2.0 user data.
// It mirrors OIDCDirectoryFetcher so Bootstrap Step 4 can use either protocol.
// Satisfied by *SCIMClient (from scim_client.go).
// Defined here so tests can inject fakes without a real SCIM server.
type SCIMFetcher interface {
	IsEnabled() bool
	FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
}

// importProgressState holds the live import counters updated atomically by the import goroutine.
// Using atomic.Int32 avoids mutex contention between the import worker and polling handlers.
type importProgressState struct {
	imported atomic.Int32
	total    atomic.Int32
	failed   atomic.Int32
	done     atomic.Bool
}

// Package-level singletons for import progress tracking (HR-3).
// importInProgress acts as a mutex: CAS from false → true before starting an import;
// the import goroutine sets it back to false when done.
// importProgress holds the live counters readable by importStatusHandler.
var (
	importInProgress atomic.Bool
	importProgress   = &importProgressState{}
)

// resetImportState resets all import progress singleton state.
// Used in unit tests for isolation between test cases.
// Defined in the production file (not a _test.go file) so it is always compiled
// and accessible within the package — avoids duplicate-symbol build errors.
func resetImportState() {
	importInProgress.Store(false)
	importProgress.imported.Store(0)
	importProgress.total.Store(0)
	importProgress.failed.Store(0)
	importProgress.done.Store(false)
}

// importStatusResponse is the JSON shape returned by importStatusHandler.
// CR-3: this response never includes scim_bearer_token or any secret material.
type importStatusResponse struct {
	Imported int32 `json:"imported"`
	Total    int32 `json:"total"`
	Failed   int32 `json:"failed"`
	Done     bool  `json:"done"`
}

// ImportStatusHandler handles GET /api/v1/admin/bootstrap/import-status.
// Returns the current import progress as JSON.
// Registered behind sessionGuard in main.go (CR-3).
//
// Response shape:
//
//	{ "imported": 5, "total": 20, "failed": 1, "done": false }
func ImportStatusHandler(w http.ResponseWriter, r *http.Request) {
	importStatusHandler(w, r)
}

// importStatusHandler is the internal implementation of ImportStatusHandler.
func importStatusHandler(w http.ResponseWriter, r *http.Request) {
	resp := importStatusResponse{
		Imported: importProgress.imported.Load(),
		Total:    importProgress.total.Load(),
		Failed:   importProgress.failed.Load(),
		Done:     importProgress.done.Load(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// WithSCIMFetcher wires a SCIM 2.0 fetcher into BootstrapHandler.
// When both SCIM and OIDC fetchers are configured, SCIM takes priority (AC1).
// Call after WithImportServices. Returns the handler for fluent chaining.
func (h *BootstrapHandler) WithSCIMFetcher(f SCIMFetcher) *BootstrapHandler {
	h.scimFetcher = f
	return h
}

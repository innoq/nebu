package admin

// bootstrap_scim_test.go — Story 14-3c: SCIM 2.0 User Fetch + Progress Tracking
//
// AT-3: Progress endpoint returns correct live counts during and after import
// AT-5: Concurrent import attempt returns HTTP 409
//
// Design notes:
//   - importInProgress (atomic.Bool) and importProgress (*importProgressState) are package-level
//     singletons defined in bootstrap_scim.go.
//   - importProgressState has fields: imported, total, failed (all atomic.Int32) + done (atomic.Bool).
//   - The import-status handler is registered at GET /api/v1/admin/bootstrap/import-status.
//     For unit testing, we call the handler directly without going through the mux.
//   - The singleton importInProgress guard returns HTTP 409 when a second import is triggered.
//   - resetImportState is defined in bootstrap_scim.go (MINOR-1 fix).
//   - All tests use the admin package-internal functions/types directly (same package).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// ---------------------------------------------------------------------------
// AT-3: Progress endpoint returns correct live counts
// ---------------------------------------------------------------------------

// TestImportStatusHandler_ReturnsCounts verifies that the import-status handler
// returns the current importProgress counts as JSON.
func TestImportStatusHandler_ReturnsCounts(t *testing.T) {
	// Reset singleton state for test isolation
	resetImportState()

	// Simulate an in-progress import by setting the progress state directly
	importProgress.total.Store(20)
	importProgress.imported.Store(5)
	importProgress.failed.Store(1)

	req := httptest.NewRequest("GET", "/api/v1/admin/bootstrap/import-status", nil)
	rr := httptest.NewRecorder()
	importStatusHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	ct := rr.Header().Get("Content-Type")
	if ct == "" || !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var got importStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if got.Imported != 5 {
		t.Errorf("imported: expected 5, got %d", got.Imported)
	}
	if got.Total != 20 {
		t.Errorf("total: expected 20, got %d", got.Total)
	}
	if got.Failed != 1 {
		t.Errorf("failed: expected 1, got %d", got.Failed)
	}
}

// TestImportStatusHandler_ZeroBeforeImport verifies that the handler returns zeros
// when no import has been triggered yet.
func TestImportStatusHandler_ZeroBeforeImport(t *testing.T) {
	resetImportState()

	req := httptest.NewRequest("GET", "/api/v1/admin/bootstrap/import-status", nil)
	rr := httptest.NewRecorder()
	importStatusHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rr.Code)
	}

	var got importStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if got.Imported != 0 || got.Total != 0 || got.Failed != 0 {
		t.Errorf("expected all zeros before import, got imported=%d total=%d failed=%d",
			got.Imported, got.Total, got.Failed)
	}
}

// TestImportStatusHandler_FinalCountsAfterCompletion verifies that the handler
// returns stable final counts after the import goroutine finishes.
func TestImportStatusHandler_FinalCountsAfterCompletion(t *testing.T) {
	resetImportState()

	// Simulate completed import
	importProgress.total.Store(10)
	importProgress.imported.Store(9)
	importProgress.failed.Store(1)
	importProgress.done.Store(true)
	importInProgress.Store(false)

	req := httptest.NewRequest("GET", "/api/v1/admin/bootstrap/import-status", nil)
	rr := httptest.NewRecorder()
	importStatusHandler(rr, req)

	var got importStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if got.Imported != 9 {
		t.Errorf("expected imported=9 after completion, got %d", got.Imported)
	}
	if got.Total != 10 {
		t.Errorf("expected total=10 after completion, got %d", got.Total)
	}
	if !got.Done {
		t.Errorf("expected done=true after completion")
	}
}

// ---------------------------------------------------------------------------
// AT-5: Concurrent import attempt returns HTTP 409
// ---------------------------------------------------------------------------

// TestBootstrapStep4_ConcurrentImportReturns409 verifies that a second POST
// action=import while an import is already in progress returns HTTP 409 Conflict.
// This tests HR-3 from the security guide: singleton import lock.
func TestBootstrapStep4_ConcurrentImportReturns409(t *testing.T) {
	resetImportState()

	// Simulate an import already in progress
	importInProgress.Store(true)

	scimFetcher := &fakeSCIMFetcher{
		enabled: true,
		users: []OIDCDirectoryUser{
			{Sub: "alice@corp.com", DisplayName: "Alice Smith", Email: "alice@corp.com"},
		},
	}
	bulkClient := &fakeBulkImportClient{
		resp: &pb.BulkImportUsersResponse{Imported: 1},
	}
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  &fakeBootstrapPersister{},
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret"),
	}
	h.WithImportServices(nil, bulkClient, "example.com")
	h.WithSCIMFetcher(scimFetcher)

	form := url.Values{}
	form.Set("step", "4")
	form.Set("action", "import")
	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.StepHandler(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected HTTP 409 Conflict for concurrent import, got %d (body: %s)",
			rr.Code, rr.Body.String())
	}
}

// TestBootstrapStep4_SCIMFetcherPreferredOverOIDC verifies that when both SCIM and OIDC
// fetchers are configured, SCIM is preferred (AC1).
func TestBootstrapStep4_SCIMFetcherPreferredOverOIDC(t *testing.T) {
	resetImportState()

	scimUsers := []OIDCDirectoryUser{
		{Sub: "scim-alice@corp.com", DisplayName: "SCIM Alice", Email: "scim-alice@corp.com"},
	}
	oidcUsers := []OIDCDirectoryUser{
		{Sub: "oidc-alice@idp.com", DisplayName: "OIDC Alice", Email: "oidc-alice@idp.com"},
	}

	scimFetcher := &fakeSCIMFetcher{enabled: true, users: scimUsers}
	oidcFetcher := &fakeOIDCFetcherForBootstrap{enabled: true, users: oidcUsers}

	var capturedRequest *pb.BulkImportUsersRequest
	bulkClient := &capturingBulkImportClient{
		capturedReq: &capturedRequest,
	}

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  &fakeBootstrapPersister{},
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret"),
	}
	h.WithImportServices(oidcFetcher, bulkClient, "example.com")
	h.WithSCIMFetcher(scimFetcher)

	form := url.Values{}
	form.Set("step", "4")
	form.Set("action", "import")
	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for SCIM import, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	// BulkImportUsers must have been called with SCIM users, not OIDC users
	if capturedRequest == nil {
		t.Fatal("BulkImportUsers must be called when action=import")
	}
	if len(capturedRequest.Users) == 0 {
		t.Fatal("BulkImportUsers was called with no users")
	}
	// The Matrix user ID is derived from sanitizeOIDCSub(scim-alice@corp.com) + ":example.com".
	// Check that the localpart comes from the SCIM user's sub.
	firstUserID := capturedRequest.Users[0].UserId
	if !strings.Contains(firstUserID, "scim-alice") {
		t.Errorf("expected SCIM users to be used (UserID contains 'scim-alice'), got UserID=%q",
			firstUserID)
	}
}

// ---------------------------------------------------------------------------
// Test doubles for Story 14-3c
// ---------------------------------------------------------------------------

// fakeSCIMFetcher is a test double for SCIMFetcher (defined in bootstrap_scim.go).
type fakeSCIMFetcher struct {
	enabled  bool
	users    []OIDCDirectoryUser
	fetchErr error
}

func (f *fakeSCIMFetcher) IsEnabled() bool { return f.enabled }
func (f *fakeSCIMFetcher) FetchUsers(_ context.Context) ([]OIDCDirectoryUser, error) {
	return f.users, f.fetchErr
}

// capturingBulkImportClient captures the pb.BulkImportUsersRequest passed to BulkImportUsers.
// MINOR-2 fix: uses real *pb.BulkImportUsersRequest instead of interface{}.
// Used by TestBootstrapStep4_SCIMFetcherPreferredOverOIDC to verify SCIM preference.
type capturingBulkImportClient struct {
	capturedReq **pb.BulkImportUsersRequest
}

func (c *capturingBulkImportClient) BulkImportUsers(
	_ context.Context,
	req *pb.BulkImportUsersRequest,
	_ ...grpc.CallOption,
) (*pb.BulkImportUsersResponse, error) {
	*c.capturedReq = req
	return &pb.BulkImportUsersResponse{
		Imported: int32(len(req.GetUsers())),
	}, nil
}

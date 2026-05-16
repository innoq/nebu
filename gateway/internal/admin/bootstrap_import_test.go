package admin

// bootstrap_import_test.go — Story 14-3b: Bootstrap Wizard Step 4 UI — Preview + Import
//
// AT-1: Step 4 rendered with "User Import" heading and enabled import button
// AT-2: Preview fetch returns user table (display name, email, matrix user id)
// AT-3: Import API calls BulkImportUsers gRPC and returns counts
// AT-4: Disabled button when OIDC directory is disabled
//
// RED PHASE: These tests are written before implementation exists.
// All tests will FAIL until Step 4 is implemented in bootstrap.go.
//
// Design notes:
//   - BulkImportClient is a new interface to be defined in bootstrap.go.
//   - OIDCDirectoryFetcher is a new interface to be defined in bootstrap.go,
//     abstracting IsEnabled()/FetchUsers() for bootstrap handler testability.
//     (UsersHandler uses *OIDCDirectoryService directly; BootstrapHandler uses
//     the interface so this test can inject fakes without a real HTTP server.)
//   - BootstrapHandler gains fields: oidcFetcher OIDCDirectoryFetcher, core BulkImportClient, serverName string.
//   - WithImportServices(fetcher OIDCDirectoryFetcher, core BulkImportClient, serverName string) is the
//     fluent setter.
//   - GET /admin/bootstrap?step=4 renders step 4 (Handler method).
//   - POST /admin/bootstrap step=4 action=preview → renders preview table.
//   - POST /admin/bootstrap step=4 action=import → calls BulkImportUsers, renders result.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeBulkImportClient is a test double for BulkImportClient (Story 14-3b).
// BulkImportClient interface to be defined in bootstrap.go:
//   type BulkImportClient interface {
//       BulkImportUsers(ctx context.Context, req *pb.BulkImportUsersRequest, opts ...grpc.CallOption) (*pb.BulkImportUsersResponse, error)
//   }
type fakeBulkImportClient struct {
	resp        *pb.BulkImportUsersResponse
	err         error
	lastRequest *pb.BulkImportUsersRequest // tracks the last call for assertion
}

func (f *fakeBulkImportClient) BulkImportUsers(_ context.Context, req *pb.BulkImportUsersRequest, _ ...grpc.CallOption) (*pb.BulkImportUsersResponse, error) {
	f.lastRequest = req
	return f.resp, f.err
}

// fakeOIDCFetcherForBootstrap is a test double for OIDCDirectoryFetcher.
// OIDCDirectoryFetcher interface to be defined in bootstrap.go:
//   type OIDCDirectoryFetcher interface {
//       IsEnabled() bool
//       FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
//   }
type fakeOIDCFetcherForBootstrap struct {
	enabled  bool
	users    []OIDCDirectoryUser
	fetchErr error
}

func (f *fakeOIDCFetcherForBootstrap) IsEnabled() bool { return f.enabled }
func (f *fakeOIDCFetcherForBootstrap) FetchUsers(_ context.Context) ([]OIDCDirectoryUser, error) {
	return f.users, f.fetchErr
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// newBootstrapHandlerForImportTest creates a BootstrapHandler configured for Step 4 tests.
// It uses WithImportServices (to be implemented in bootstrap.go) to wire the fakes.
func newBootstrapHandlerForImportTest(
	t *testing.T,
	oidcFetcher OIDCDirectoryFetcher,
	bulkClient BulkImportClient,
) *BootstrapHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  &fakeBootstrapPersister{},
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret-for-encryption"),
	}
	return h.WithImportServices(oidcFetcher, bulkClient, "example.com")
}

// ---------------------------------------------------------------------------
// AT-1: GET /admin/bootstrap?step=4 renders "User Import" heading and "Import from OIDC" button
// ---------------------------------------------------------------------------
func TestBootstrapStep4_RendersUserImportStep(t *testing.T) {
	fetcher := &fakeOIDCFetcherForBootstrap{enabled: true}
	h := newBootstrapHandlerForImportTest(t, fetcher, nil)

	req := httptest.NewRequest("GET", "/admin/bootstrap?step=4", nil)
	rr := httptest.NewRecorder()
	h.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for step 4 GET, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "User Import") {
		t.Error("step 4 must contain 'User Import' heading or label")
	}
	if !strings.Contains(body, "Import from OIDC") {
		t.Error("step 4 must contain 'Import from OIDC' button")
	}
	// Progress indicator must show 4 steps; "Import" is the 4th step label
	if !strings.Contains(body, "Import") {
		t.Error("step progress indicator must show 'Import' as the 4th step")
	}
}

// ---------------------------------------------------------------------------
// AT-2: POST step=4 action=preview returns table with display_name, email, matrix_user_id
// ---------------------------------------------------------------------------
func TestBootstrapStep4_PreviewFetchReturnsTable(t *testing.T) {
	oidcUsers := []OIDCDirectoryUser{
		{Sub: "alice@example.com", DisplayName: "Alice Smith", Email: "alice@example.com"},
		{Sub: "bob@example.com", DisplayName: "Bob Jones", Email: "bob@example.com"},
		{Sub: "carol@example.com", DisplayName: "Carol White", Email: "carol@example.com"},
	}
	fetcher := &fakeOIDCFetcherForBootstrap{enabled: true, users: oidcUsers}
	h := newBootstrapHandlerForImportTest(t, fetcher, nil)

	form := url.Values{}
	form.Set("step", "4")
	form.Set("action", "preview")
	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for preview action, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	// Preview must show each user's display name
	if !strings.Contains(body, "Alice Smith") {
		t.Error("preview table must contain display name 'Alice Smith'")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Error("preview table must contain email 'alice@example.com'")
	}
	if !strings.Contains(body, "Bob Jones") {
		t.Error("preview table must contain display name 'Bob Jones'")
	}
	if !strings.Contains(body, "Carol White") {
		t.Error("preview table must contain display name 'Carol White'")
	}
	// Matrix User ID must contain the server name
	if !strings.Contains(body, "@") || !strings.Contains(body, "example.com") {
		t.Error("preview table must show computed Matrix User ID containing '@' and server name 'example.com'")
	}
}

// ---------------------------------------------------------------------------
// AT-3: POST step=4 action=import calls BulkImportUsers and shows counts
// ---------------------------------------------------------------------------
func TestBootstrapStep4_ImportCallsBulkImportAndShowsCounts(t *testing.T) {
	oidcUsers := []OIDCDirectoryUser{
		{Sub: "alice@example.com", DisplayName: "Alice Smith", Email: "alice@example.com"},
		{Sub: "bob@example.com", DisplayName: "Bob Jones", Email: "bob@example.com"},
	}
	fetcher := &fakeOIDCFetcherForBootstrap{enabled: true, users: oidcUsers}
	bulkClient := &fakeBulkImportClient{
		resp: &pb.BulkImportUsersResponse{
			Imported: 2,
			Skipped:  0,
			Failed:   0,
		},
	}
	h := newBootstrapHandlerForImportTest(t, fetcher, bulkClient)

	form := url.Values{}
	form.Set("step", "4")
	form.Set("action", "import")
	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for import action, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	// BulkImportUsers must have been called
	if bulkClient.lastRequest == nil {
		t.Fatal("BulkImportUsers gRPC must be called when action=import")
	}
	if len(bulkClient.lastRequest.Users) != 2 {
		t.Errorf("expected 2 users in BulkImportUsers request, got %d", len(bulkClient.lastRequest.Users))
	}
	// Verify system_role is "user" for all bulk-imported users
	for i, u := range bulkClient.lastRequest.Users {
		if u.SystemRole != "user" {
			t.Errorf("user[%d].SystemRole must be 'user' for bulk import, got %q", i, u.SystemRole)
		}
	}

	body := rr.Body.String()
	// Result must display the imported count
	if !strings.Contains(body, "2") {
		t.Error("import result page must display imported count (2)")
	}
}

// ---------------------------------------------------------------------------
// AT-4: Step 4 with OIDC dir disabled shows disabled button and explanatory message
// ---------------------------------------------------------------------------
func TestBootstrapStep4_DisabledButtonWhenOIDCDirDisabled(t *testing.T) {
	fetcher := &fakeOIDCFetcherForBootstrap{enabled: false}
	h := newBootstrapHandlerForImportTest(t, fetcher, nil)

	req := httptest.NewRequest("GET", "/admin/bootstrap?step=4", nil)
	rr := httptest.NewRecorder()
	h.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for step 4 GET, got %d", rr.Code)
	}
	body := rr.Body.String()
	// "Import from OIDC" button must be rendered with disabled attribute
	if !strings.Contains(body, "disabled") {
		t.Error("'Import from OIDC' button must be disabled when OIDC directory is not enabled")
	}
	// Explanatory message must be present
	if !strings.Contains(body, "Provider does not support user listing") {
		t.Error("step 4 must show 'Provider does not support user listing' when OIDC dir is disabled")
	}
}

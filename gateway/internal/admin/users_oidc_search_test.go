package admin

// users_oidc_search_test.go — Story 14.2c: Admin UI User Search OIDC Integration
//
// Acceptance tests written FIRST (red phase, TDD).
// These tests FAIL until the OIDC directory merge logic is implemented in users.go.
//
// Test matrix (from story Acceptance Tests section):
//   AT-1  TestUsersSearch_OIDCMerge_NotYetLoggedIn  — OIDC-only user appears with "Not yet logged in" badge
//   AT-2  TestUsersSearch_OIDCDisabled_NoOIDCCalls  — disabled: no OIDC calls, no badge
//   AT-3  TestUsersSearch_OIDCUnavailable_WarningBanner — unreachable OIDC → DB-only + warning
//   AT-4  TestUsersSearch_DeduplicatesByNebusMatrixUserID — duplicate sub → only Nebu DB entry shown
//
// Each test uses httptest to call UsersHandler.ListHandler directly.
// OIDCDirectoryService is injected via WithOIDCDirectory (to be added in implementation).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// --- Test helpers ---

// fakeOIDCServer creates a test HTTPS server serving the given user list JSON.
// Returns the server (caller must defer server.Close()) and its TLS client.
func fakeOIDCServer(t *testing.T, usersJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, usersJSON)
	}))
	return srv
}

// fakeOIDCServerUnreachable returns a URL that will always fail to connect.
// The URL is formed by immediately closing a TLS server so the port is no longer listening.
func fakeOIDCServerUnreachable(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately — port no longer listening
	return url
}

// mockAdminCore implements AdminUsersClient for unit tests.
// It returns a fixed list of users and records calls.
type mockAdminCore struct {
	users []*pb.AdminUserProto
}

func (m *mockAdminCore) ListAdminUsers(_ context.Context, req *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error) {
	return &pb.ListAdminUsersResponse{Users: m.users}, nil
}

func (m *mockAdminCore) GetAdminUser(_ context.Context, req *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error) {
	for _, u := range m.users {
		if u.GetUserId() == req.GetUserId() {
			return &pb.GetAdminUserResponse{User: u}, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockAdminCore) DeactivateUser(_ context.Context, _ *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error) {
	return &pb.DeactivateUserResponse{}, nil
}

func (m *mockAdminCore) ReactivateUser(_ context.Context, _ *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error) {
	return &pb.ReactivateUserResponse{}, nil
}

func (m *mockAdminCore) UpdateUserRole(_ context.Context, _ *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error) {
	return &pb.UpdateUserRoleResponse{}, nil
}

// --- AT-1: OIDC-only user appears with "Not yet logged in" badge ---

// TestUsersSearch_OIDCMerge_NotYetLoggedIn verifies that when OIDC directory is enabled,
// users present in OIDC but absent from Nebu DB appear in search results with a
// "Not yet logged in" badge and a computed Matrix User ID preview.
// AC1 (Story 14.2c)
func TestUsersSearch_OIDCMerge_NotYetLoggedIn(t *testing.T) {
	// OIDC directory server returns Frank OIDC — not in Nebu DB
	srv := fakeOIDCServer(t, `[{"sub":"frank.oidc","display_name":"Frank OIDC","email":"frank@idp.example.com"}]`)
	defer srv.Close()

	oidcSvc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "test-token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	// Nebu DB has Alice Müller — Frank OIDC is NOT in DB
	core := &mockAdminCore{users: []*pb.AdminUserProto{
		{UserId: "usr-001", DisplayName: "Alice Müller", EmailMasked: "a***@example.com", SystemRole: "instance_admin", IsActive: true},
	}}

	h := NewUsersHandler(tmpl, core)
	h = h.WithOIDCDirectory(oidcSvc, "example.com")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=frank", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	if !strings.Contains(body, "Frank OIDC") {
		t.Error("expected 'Frank OIDC' to appear in search results (OIDC-only user)")
	}
	if !strings.Contains(body, "Not yet logged in") {
		t.Error("expected 'Not yet logged in' badge for OIDC-only user")
	}
	if !strings.Contains(body, "@frank.oidc:") {
		t.Errorf("expected computed Matrix User ID preview '@frank.oidc:example.com' in body, got:\n%s", body)
	}
}

// --- AT-2: OIDC disabled — no OIDC calls, no badge ---

// TestUsersSearch_OIDCDisabled_NoOIDCCalls verifies that when OIDC directory is disabled,
// the users list shows only Nebu DB users and makes zero outbound OIDC calls.
// AC2 (Story 14.2c)
func TestUsersSearch_OIDCDisabled_NoOIDCCalls(t *testing.T) {
	var oidcCallCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oidcCallCount++
		t.Errorf("OIDC server called unexpectedly (call #%d) — OIDC is disabled", oidcCallCount)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	oidcSvc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "test-token",
		Enabled:     false, // DISABLED
		HTTPClient:  srv.Client(),
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	core := &mockAdminCore{users: []*pb.AdminUserProto{
		{UserId: "usr-001", DisplayName: "Alice Müller", EmailMasked: "a***@example.com", SystemRole: "instance_admin", IsActive: true},
	}}

	h := NewUsersHandler(tmpl, core)
	h = h.WithOIDCDirectory(oidcSvc, "example.com")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=frank", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()

	if strings.Contains(body, "Not yet logged in") {
		t.Error("expected NO 'Not yet logged in' badge when OIDC is disabled")
	}
	if oidcCallCount > 0 {
		t.Errorf("expected zero OIDC HTTP calls when OIDC disabled, got %d", oidcCallCount)
	}
}

// --- AT-3: OIDC provider unavailable → DB-only + warning banner ---

// TestUsersSearch_OIDCUnavailable_WarningBanner verifies that when the OIDC provider is
// unreachable, the handler returns Nebu DB results only and renders a non-blocking
// warning banner containing "OIDC directory temporarily unavailable".
// AC3 (Story 14.2c)
func TestUsersSearch_OIDCUnavailable_WarningBanner(t *testing.T) {
	unreachableURL := fakeOIDCServerUnreachable(t)

	oidcSvc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    unreachableURL,
		BearerToken: "test-token",
		Enabled:     true,
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	core := &mockAdminCore{users: []*pb.AdminUserProto{
		{UserId: "usr-001", DisplayName: "Alice Müller", EmailMasked: "a***@example.com", SystemRole: "instance_admin", IsActive: true},
	}}

	h := NewUsersHandler(tmpl, core)
	h = h.WithOIDCDirectory(oidcSvc, "example.com")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=alice", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected Nebu DB user 'Alice Müller' to appear when OIDC is unavailable")
	}
	if !strings.Contains(body, "OIDC directory temporarily unavailable") {
		t.Error("expected non-blocking warning 'OIDC directory temporarily unavailable' in response body")
	}
}

// --- AT-4: Deduplication — Nebu DB entry wins over OIDC entry ---

// TestUsersSearch_DeduplicatesByNebusMatrixUserID verifies that when the OIDC directory
// returns a user whose sub matches the Matrix localpart of an existing Nebu DB user
// (matched via UserId since AdminUserProto does not expose a separate matrix_user_id field),
// only one row is rendered (the Nebu DB entry, without the OIDC-only badge).
// AC1 (deduplication note), Story 14.2c
//
// Deduplication strategy: UsersHandler builds a set of known Nebu user IDs.
// sanitize(oidcUser.Sub) is compared against each Nebu user's computed localpart
// (derived from UserId or DisplayName). If a match is found, the OIDC entry is dropped.
// For the unit test: the Nebu DB user "usr-alice-mueller" has DisplayName "Alice Müller";
// the OIDC entry with sub="usr-alice-mueller" matches by sub == UserId → dedup fires.
func TestUsersSearch_DeduplicatesByNebusMatrixUserID(t *testing.T) {
	// OIDC returns a user whose Sub is the same as a Nebu DB UserId
	// (representing the case where sub maps to the same identity)
	srv := fakeOIDCServer(t, `[{"sub":"usr-alice-mueller","display_name":"Alice Müller OIDC","email":"alice@idp.example.com"}]`)
	defer srv.Close()

	oidcSvc := NewOIDCDirectoryService(OIDCDirectoryConfig{
		Endpoint:    srv.URL,
		BearerToken: "test-token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	// Nebu DB has Alice Müller — UserId "usr-alice-mueller" matches the OIDC sub exactly
	core := &mockAdminCore{users: []*pb.AdminUserProto{
		{
			UserId:      "usr-alice-mueller",
			DisplayName: "Alice Müller",
			EmailMasked: "a***@example.com",
			SystemRole:  "instance_admin",
			IsActive:    true,
		},
	}}

	h := NewUsersHandler(tmpl, core)
	h = h.WithOIDCDirectory(oidcSvc, "example.com")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()

	// Nebu DB name wins — NOT the OIDC display name variant
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected Nebu DB entry 'Alice Müller' to appear")
	}
	// OIDC-only display name variant must NOT appear (dedup: Nebu entry wins)
	if strings.Contains(body, "Alice Müller OIDC") {
		t.Error("OIDC display name variant 'Alice Müller OIDC' must NOT appear — Nebu DB entry wins")
	}
	// No "Not yet logged in" badge — Alice is already in Nebu DB
	if strings.Contains(body, "Not yet logged in") {
		t.Error("expected NO 'Not yet logged in' badge for user who exists in both OIDC and Nebu DB")
	}
}

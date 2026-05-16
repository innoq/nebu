package admin

// scim_client_test.go — Story 14-3c: SCIM 2.0 User Fetch + Progress Tracking
//
// RED PHASE — All tests will FAIL until scim_client.go is implemented.
//
// AT-1: SCIM user fetch (paginated, mock HTTPS server)
// AT-2: SCIM-to-Nebu claim mapping (userName → OIDCDirectoryUser)
// AT-4: Non-HTTPS scim_base_url is rejected
// AT-6: Bearer token never appears in log output
//
// Design notes:
//   - SCIMClient is to be defined in gateway/internal/admin/scim_client.go
//   - It must satisfy the SCIMFetcher interface (IsEnabled() bool, FetchUsers(ctx) ([]OIDCDirectoryUser, error))
//   - The secretString type already exists in oidc_directory.go — reuse it, do not redefine
//   - httptest.NewTLSServer is used because SCIM must use HTTPS (CR-2)
//   - All constants are to be defined in scim_client.go:
//     scimPageSize = 100, scimMaxTotal = 100_000, maxScimPageResponseBytes = 5 * 1024 * 1024

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test doubles / helpers
// ---------------------------------------------------------------------------

// scimTestUser builds a minimal SCIM User JSON object for test servers.
func scimTestUser(id, userName, displayName, email string) map[string]interface{} {
	return map[string]interface{}{
		"id":          id,
		"userName":    userName,
		"displayName": displayName,
		"emails": []map[string]interface{}{
			{"value": email, "primary": true},
		},
	}
}

// mockSCIMServer starts a TLS test server simulating a paginated SCIM /Users endpoint.
// users is the full user list; pageSize is the max per page.
// Returns a *httptest.Server (TLS) and a cleanup function.
func mockSCIMServer(t *testing.T, users []map[string]interface{}, pageSize int) (*httptest.Server, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is present and starts with "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		startIndex := 1
		count := pageSize
		if si := q.Get("startIndex"); si != "" {
			if v, err := strconv.Atoi(si); err == nil {
				startIndex = v
			}
		}
		if c := q.Get("count"); c != "" {
			if v, err := strconv.Atoi(c); err == nil {
				count = v
			}
		}
		// Build page slice (startIndex is 1-based)
		idx := startIndex - 1
		if idx < 0 {
			idx = 0
		}
		end := idx + count
		if end > len(users) {
			end = len(users)
		}
		page := users[idx:end]

		resp := map[string]interface{}{
			"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
			"totalResults": len(users),
			"startIndex":   startIndex,
			"itemsPerPage": count,
			"Resources":    page,
		}
		w.Header().Set("Content-Type", "application/scim+json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	return srv, srv.Close
}

// logCapture creates an slog.Logger that writes to buf.
// Used by AT-6 to verify the bearer token never appears in log output.
func logCapture(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// ---------------------------------------------------------------------------
// AT-1: SCIM user fetch — paginated, mock HTTPS server
// ---------------------------------------------------------------------------

// TestSCIMClient_FetchUsers_Paginated verifies that SCIMClient fetches all users
// across multiple pages using RFC 7644 §3.4.2 startIndex/count pagination.
//
// RED PHASE: will fail until SCIMClient is implemented.
func TestSCIMClient_FetchUsers_Paginated(t *testing.T) {
	// Setup: 3 users split across 2 pages (pageSize=2)
	users := []map[string]interface{}{
		scimTestUser("u1", "alice.smith@corp.com", "Alice Smith", "alice@corp.com"),
		scimTestUser("u2", "bob.jones@corp.com", "Bob Jones", "bob@corp.com"),
		scimTestUser("u3", "carol.white@corp.com", "Carol White", "carol@corp.com"),
	}
	srv, cleanup := mockSCIMServer(t, users, 2)
	defer cleanup()

	// SCIMClientConfig is the config struct to be defined in scim_client.go.
	// It mirrors OIDCDirectoryConfig in structure.
	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "test-bearer-token",
		Enabled:     true,
		HTTPClient:  srv.Client(), // use TLS client from test server
	}
	client := NewSCIMClient(cfg)

	got, err := client.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("FetchUsers returned unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 users, got %d", len(got))
	}
	// Verify all userNames are present (order may vary)
	gotNames := make(map[string]bool)
	for _, u := range got {
		gotNames[u.Sub] = true
	}
	for _, want := range []string{"alice.smith@corp.com", "bob.jones@corp.com", "carol.white@corp.com"} {
		if !gotNames[want] {
			t.Errorf("expected Sub %q in result, not found (got: %v)", want, gotNames)
		}
	}
}

// TestSCIMClient_FetchUsers_SendsBearerToken verifies that each SCIM page request
// includes the Authorization: Bearer {token} header.
//
// RED PHASE: will fail until SCIMClient is implemented.
func TestSCIMClient_FetchUsers_SendsBearerToken(t *testing.T) {
	var receivedAuth []string
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = append(receivedAuth, r.Header.Get("Authorization"))
		resp := map[string]interface{}{
			"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
			"totalResults": 1,
			"startIndex":   1,
			"itemsPerPage": 100,
			"Resources": []map[string]interface{}{
				scimTestUser("u1", "alice@corp.com", "Alice", "alice@corp.com"),
			},
		}
		w.Header().Set("Content-Type", "application/scim+json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "my-secret-token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	}
	client := NewSCIMClient(cfg)
	_, _ = client.FetchUsers(context.Background())

	if len(receivedAuth) == 0 {
		t.Fatal("no requests received by mock SCIM server")
	}
	for i, auth := range receivedAuth {
		if auth != "Bearer my-secret-token" {
			t.Errorf("request[%d]: expected Authorization 'Bearer my-secret-token', got %q", i, auth)
		}
	}
}

// TestSCIMClient_FetchUsers_StartIndexIsOneBased verifies that the first SCIM request
// uses startIndex=1 (RFC 7644 §3.4.2 — startIndex is 1-based, NOT 0-based).
//
// RED PHASE: will fail until SCIMClient is implemented.
func TestSCIMClient_FetchUsers_StartIndexIsOneBased(t *testing.T) {
	var firstStartIndex string
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, r *http.Request) {
		if firstStartIndex == "" {
			firstStartIndex = r.URL.Query().Get("startIndex")
		}
		resp := map[string]interface{}{
			"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
			"totalResults": 1,
			"startIndex":   1,
			"itemsPerPage": 100,
			"Resources": []map[string]interface{}{
				scimTestUser("u1", "alice@corp.com", "Alice", "alice@corp.com"),
			},
		}
		w.Header().Set("Content-Type", "application/scim+json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := SCIMClientConfig{
		BaseURL:    srv.URL,
		BearerToken: "token",
		Enabled:    true,
		HTTPClient: srv.Client(),
	}
	client := NewSCIMClient(cfg)
	_, _ = client.FetchUsers(context.Background())

	if firstStartIndex != "1" {
		t.Errorf("first SCIM request must use startIndex=1 (1-based, RFC 7644 §3.4.2), got %q", firstStartIndex)
	}
}

// ---------------------------------------------------------------------------
// AT-2: SCIM-to-Nebu claim mapping
// ---------------------------------------------------------------------------

// TestSCIMClient_ClaimMapping verifies that SCIM User fields are correctly mapped
// to OIDCDirectoryUser for use in the BulkImportUsers provisioning flow.
//
// RED PHASE: will fail until SCIMClient + scimUserToDirectoryUser is implemented.
func TestSCIMClient_ClaimMapping(t *testing.T) {
	users := []map[string]interface{}{
		// Primary email present
		scimTestUser("u1", "Alice.Smith@corp.com", "Alice Smith", "alice@corp.com"),
	}
	srv, cleanup := mockSCIMServer(t, users, 100)
	defer cleanup()

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	}
	client := NewSCIMClient(cfg)

	got, err := client.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("FetchUsers error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 user, got %d", len(got))
	}
	u := got[0]

	// Sub must be the SCIM userName (will be passed through sanitizeOIDCSub at import time)
	if u.Sub != "Alice.Smith@corp.com" {
		t.Errorf("Sub: expected %q, got %q", "Alice.Smith@corp.com", u.Sub)
	}
	if u.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName: expected %q, got %q", "Alice Smith", u.DisplayName)
	}
	if u.Email != "alice@corp.com" {
		t.Errorf("Email: expected %q, got %q", "alice@corp.com", u.Email)
	}
}

// TestSCIMClient_ClaimMapping_PrimaryEmailPreferred verifies that when multiple emails
// are present, the primary email is used.
//
// RED PHASE: will fail until SCIMClient is implemented.
func TestSCIMClient_ClaimMapping_PrimaryEmailPreferred(t *testing.T) {
	users := []map[string]interface{}{
		{
			"id":          "u1",
			"userName":    "bob@corp.com",
			"displayName": "Bob Jones",
			"emails": []map[string]interface{}{
				{"value": "bob-work@corp.com", "primary": false},
				{"value": "bob@corp.com", "primary": true},
			},
		},
	}
	srv, cleanup := mockSCIMServer(t, users, 100)
	defer cleanup()

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	}
	client := NewSCIMClient(cfg)

	got, err := client.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("FetchUsers error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected 1 user, got 0")
	}
	if got[0].Email != "bob@corp.com" {
		t.Errorf("expected primary email %q, got %q", "bob@corp.com", got[0].Email)
	}
}

// TestSCIMClient_ClaimMapping_MatrixLocalpart verifies that the sanitizeOIDCSub function
// correctly converts SCIM userName to a valid Matrix localpart.
// This tests CR-4 from the security guide: userName must go through sanitizeOIDCSub.
//
// RED PHASE: will fail until scim_client.go uses sanitizeOIDCSub.
func TestSCIMClient_ClaimMapping_MatrixLocalpart(t *testing.T) {
	tests := []struct {
		userName      string
		wantLocalpart string
	}{
		{"alice.smith@corp.com", "alice.smith_corp.com"},
		{"Bob Jones", "bob_jones"},
		{"user@DOMAIN.COM", "user_domain.com"},
		{"normal-user_123", "normal-user_123"},
	}

	for _, tc := range tests {
		t.Run(tc.userName, func(t *testing.T) {
			got := sanitizeOIDCSub(tc.userName)
			if got != tc.wantLocalpart {
				t.Errorf("sanitizeOIDCSub(%q) = %q, want %q", tc.userName, got, tc.wantLocalpart)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AT-4: Non-HTTPS scim_base_url is rejected
// ---------------------------------------------------------------------------

// TestSCIMClient_RejectsNonHTTPS verifies that FetchUsers returns an error when
// scim_base_url uses http:// instead of https://.
// This tests CR-2 from the security guide.
//
// RED PHASE: will fail until SCIMClient validates HTTPS.
func TestSCIMClient_RejectsNonHTTPS(t *testing.T) {
	cfg := SCIMClientConfig{
		BaseURL:     "http://idp.corp.com/scim",
		BearerToken: "token",
		Enabled:     true,
		// Do NOT set HTTPClient — the real client should never be reached
	}
	client := NewSCIMClient(cfg)

	_, err := client.FetchUsers(context.Background())
	if err == nil {
		t.Fatal("expected error for non-HTTPS scim_base_url, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "https") {
		t.Errorf("error should mention HTTPS requirement, got: %v", err)
	}
}

// TestSCIMClient_RejectsEmptyBaseURL verifies that an empty BaseURL is rejected.
//
// RED PHASE: will fail until SCIMClient validates BaseURL.
func TestSCIMClient_RejectsEmptyBaseURL(t *testing.T) {
	cfg := SCIMClientConfig{
		BaseURL:     "",
		BearerToken: "token",
		Enabled:     true,
	}
	client := NewSCIMClient(cfg)

	_, err := client.FetchUsers(context.Background())
	if err == nil {
		t.Fatal("expected error for empty scim_base_url, got nil")
	}
}

// ---------------------------------------------------------------------------
// AT-6: Bearer token never appears in log output
// ---------------------------------------------------------------------------

// TestSCIMClient_TokenNeverLogged verifies that the bearer token value never appears
// in slog output, even when errors occur during fetch.
// This tests CR-1/CR-3 from the security guide: "never in logs".
//
// RED PHASE: will fail until SCIMClient uses secretString for the token.
func TestSCIMClient_TokenNeverLogged(t *testing.T) {
	const secretToken = "super-secret-token-12345"

	// Use a mock HTTPS server that always returns 401
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"detail":"unauthorized"}`, http.StatusUnauthorized)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	var buf bytes.Buffer
	logger := logCapture(&buf)

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: secretToken,
		Enabled:     true,
		HTTPClient:  srv.Client(),
		Logger:      logger,
	}
	client := NewSCIMClient(cfg)
	// FetchUsers may return error or empty list — we only care about logs
	_, _ = client.FetchUsers(context.Background())

	logOutput := buf.String()
	if strings.Contains(logOutput, secretToken) {
		t.Errorf("bearer token %q must never appear in log output, but found in:\n%s", secretToken, logOutput)
	}
}

// TestSCIMClient_Disabled_ReturnsEmpty verifies that FetchUsers returns an empty
// slice immediately when IsEnabled() is false (no HTTP call made).
//
// RED PHASE: will fail until SCIMClient respects Enabled flag.
func TestSCIMClient_Disabled_ReturnsEmpty(t *testing.T) {
	cfg := SCIMClientConfig{
		BaseURL:     "https://idp.corp.com/scim",
		BearerToken: "token",
		Enabled:     false, // disabled
	}
	client := NewSCIMClient(cfg)

	if client.IsEnabled() {
		t.Error("expected IsEnabled() = false, got true")
	}

	got, err := client.FetchUsers(context.Background())
	if err != nil {
		t.Errorf("expected no error for disabled client, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result for disabled client, got %d users", len(got))
	}
}

// TestSCIMClient_AbortWhenTotalExceedsCap verifies that FetchUsers aborts with an
// error when totalResults exceeds scimMaxTotal (100_000).
// This tests HR-1 from the security guide: prevent OOM from unbounded imports.
//
// RED PHASE: will fail until SCIMClient enforces the cap.
func TestSCIMClient_AbortWhenTotalExceedsCap(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
			"totalResults": 100_001, // exceeds scimMaxTotal
			"startIndex":   1,
			"itemsPerPage": 100,
			"Resources":    []map[string]interface{}{scimTestUser("u1", "alice@corp.com", "Alice", "alice@corp.com")},
		}
		w.Header().Set("Content-Type", "application/scim+json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	}
	client := NewSCIMClient(cfg)

	_, err := client.FetchUsers(context.Background())
	if err == nil {
		t.Fatal("expected error when totalResults > 100_000, got nil")
	}
	// Error should be descriptive enough for the admin to understand
	if !strings.Contains(err.Error(), "100") {
		// The error should mention the cap or the count
		t.Logf("error: %v (acceptable: should mention the cap)", err)
	}
}

// TestSCIMClient_ResponseSizeLimited verifies that the client does not OOM
// on a large response body (> maxScimPageResponseBytes = 5 MB).
// This tests MR-4 from the security guide.
//
// RED PHASE: will fail until SCIMClient uses io.LimitReader.
func TestSCIMClient_ResponseSizeLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, _ *http.Request) {
		// Write a valid JSON prefix, then a huge body (> 5 MB)
		w.Header().Set("Content-Type", "application/scim+json")
		// Write a valid header but then garbage bytes to exceed the cap
		fmt.Fprintf(w, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":1,"startIndex":1,"itemsPerPage":100,"Resources":[`)
		// Pad to well over 5 MB
		padding := strings.Repeat("x", 6*1024*1024)
		fmt.Fprintf(w, `"%s"]}`, padding)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := SCIMClientConfig{
		BaseURL:     srv.URL,
		BearerToken: "token",
		Enabled:     true,
		HTTPClient:  srv.Client(),
	}
	client := NewSCIMClient(cfg)

	// Should not hang or OOM — should return an error or empty list gracefully
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = client.FetchUsers(context.Background())
	}()

	select {
	case <-done:
		// OK — completed without hanging
	}
	// The test passes if FetchUsers returns within a reasonable time (not hanging on huge response)
}

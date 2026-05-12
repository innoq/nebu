//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.4:
// User List + Get API.
//
// RED PHASE — all tests fail until implementation is complete.
// The types AdminUser, UserRepository, and handlers ListAdminUsers / GetAdminUser
// do not exist yet; this file will not compile until:
//   - UserRepository interface is defined in users_repo.go
//   - AdminServer gains DB, CoreClient, and Users fields (server.go)
//   - ListAdminUsers and GetAdminUser are implemented (server.go)
//   - GET /api/v1/admin/users/{userId} is registered (router.go)
//   - make gen-api has regenerated api_gen.go with new request/response types
//
// Covered Acceptance Criteria:
//   - AC#1  GET /api/v1/admin/users — pagination, email_masked, status, search, cursor validation
//   - AC#2  GET /api/v1/admin/users/{userId} — single user with room_count, 404 on missing
//   - AC#3  Both routes registered, {userId} route is new
//   - AC#5  AdminServer implements real handlers via UserRepository
//   - AC#6  Unit tests: all 8 acceptance test cases
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/api"
	"github.com/nebu/nebu/internal/middleware"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// mockUserRepository implements api.UserRepository for unit tests.
// All fields are populated per-test; zero-value fields produce empty results.
type mockUserRepository struct {
	users      []api.AdminUser
	total      int
	nextCursor string
	listErr    error

	detail    *api.AdminUserDetail
	detailErr error
}

func (m *mockUserRepository) ListUsers(
	_ context.Context,
	_, _ string,
	_ int,
	search string,
) ([]api.AdminUser, int, string, error) {
	if m.listErr != nil {
		return nil, 0, "", m.listErr
	}
	if search != "" {
		var filtered []api.AdminUser
		for _, u := range m.users {
			if strings.Contains(strings.ToLower(u.DisplayName), strings.ToLower(search)) {
				filtered = append(filtered, u)
			}
		}
		return filtered, len(filtered), "", nil
	}
	return m.users, m.total, m.nextCursor, nil
}

func (m *mockUserRepository) GetUser(
	_ context.Context,
	userID string,
) (*api.AdminUserDetail, error) {
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	return m.detail, nil
}

// mockCoreClient captures the most recent WriteAuditLog call for assertion.
// It satisfies pb.CoreServiceClient (only WriteAuditLog is used in production
// code paths exercised by these tests; all other methods panic if called).
type mockCoreClient struct {
	pb.CoreServiceClient // embed to satisfy the interface; only WriteAuditLog is overridden

	auditCalled  bool
	lastAction   string
	lastTarget   string
	lastTargetID string
}

func (m *mockCoreClient) WriteAuditLog(
	_ context.Context,
	req *pb.WriteAuditLogRequest,
	_ ...grpc.CallOption,
) (*pb.WriteAuditLogResponse, error) {
	m.auditCalled = true
	m.lastAction = req.GetAction()
	m.lastTarget = req.GetTargetType()
	m.lastTargetID = req.GetTargetId()
	return &pb.WriteAuditLogResponse{}, nil
}

// Story 11.3: SearchMessages stub.
func (m *mockCoreClient) SearchMessages(_ context.Context, _ *pb.SearchMessagesRequest, _ ...grpc.CallOption) (*pb.SearchMessagesResponse, error) {
	return &pb.SearchMessagesResponse{}, nil
}

// Story 11-8: GetEvent stub — not called in users handler tests.
func (m *mockCoreClient) GetEvent(_ context.Context, _ *pb.GetEventRequest, _ ...grpc.CallOption) (*pb.GetEventResponse, error) {
	panic("unexpected call: GetEvent")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// noopJWTMiddlewareForUsers injects an instance_admin role and a test actor
// user ID into the request context, simulating the real JWT middleware.
func noopJWTMiddlewareForUsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "instance_admin")
		ctx = context.WithValue(ctx, middleware.ContextKeyUserID, "@admin:example.com")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// buildAdminServer constructs an AdminServer wired with the provided repository
// and (optionally) a mock CoreClient. Pass nil for coreClient to skip audit assertions.
func buildAdminServer(repo api.UserRepository, coreClient pb.CoreServiceClient) *api.AdminServer {
	return &api.AdminServer{
		Users:      repo,
		CoreClient: coreClient,
	}
}

// makeAdminUser constructs an AdminUser for use in test fixtures.
func makeAdminUser(userID, displayName, status string) api.AdminUser {
	now := time.Now().UTC().Format(time.RFC3339)
	return api.AdminUser{
		UserID:      userID,
		DisplayName: displayName,
		EmailMasked: "",
		Roles:       []string{"user"},
		Status:      status,
		CreatedAt:   now,
		LastSeenAt:  nil,
	}
}

// ── AC#1: List endpoint ───────────────────────────────────────────────────────

// TestListAdminUsers_PaginatedResults covers AC#1 + AC#6 (test 1) [P0]:
// When the repository returns 3 users and limit=2, the response must contain
// 2 users in "data", a non-empty "meta.next_cursor", and "meta.total"=3.
func TestListAdminUsers_PaginatedResults(t *testing.T) {
	repo := &mockUserRepository{
		users: []api.AdminUser{
			makeAdminUser("@alice:example.com", "Alice", "active"),
			makeAdminUser("@bob:example.com", "Bob", "active"),
		},
		total:      3,
		nextCursor: api.EncodeCursor("@bob:example.com", time.Now().UTC().Format(time.RFC3339)),
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminUser `json:"data"`
		Meta struct {
			Total      int    `json:"total"`
			NextCursor string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if len(resp.Data) != 2 {
		t.Errorf("[AC#1] expected 2 users in data, got %d", len(resp.Data))
	}
	if resp.Meta.Total != 3 {
		t.Errorf("[AC#1] expected meta.total=3, got %d", resp.Meta.Total)
	}
	if resp.Meta.NextCursor == "" {
		t.Error("[AC#1] expected non-empty meta.next_cursor")
	}
}

// TestListAdminUsers_EmailMasked covers AC#1 + AC#6 (test 2) [P0]:
// The response must never contain the raw email address. email_masked
// must follow the "a***@example.com" format (or "" for MVP with encrypted emails).
// For MVP, email_masked is always "" because decryption is out of scope (see Dev Notes).
func TestListAdminUsers_EmailMasked(t *testing.T) {
	user := makeAdminUser("@alice:example.com", "Alice", "active")
	// EmailMasked is "" in MVP (encrypted, cannot decrypt without X25519 key).
	// The field must be present in the JSON output as an empty string — not omitted.
	user.EmailMasked = ""

	repo := &mockUserRepository{
		users: []api.AdminUser{user},
		total: 1,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d", w.Code)
	}

	body := w.Body.String()

	// email_masked key must be present in the JSON output (empty string, not omitted).
	if !strings.Contains(body, `"email_masked"`) {
		t.Errorf("[AC#1] expected email_masked key in response; got: %s", body)
	}
	// Raw email must never appear.
	if strings.Contains(body, "alice@") {
		t.Errorf("[AC#1] raw email must never appear in response; got: %s", body)
	}
}

// TestListAdminUsers_SearchFiltersByDisplayName covers AC#1 + AC#6 (test 3) [P1]:
// When search=ali is provided, only users whose display_name contains "ali"
// (case-insensitive) must be returned. "Bob" must be absent.
func TestListAdminUsers_SearchFiltersByDisplayName(t *testing.T) {
	repo := &mockUserRepository{
		users: []api.AdminUser{
			makeAdminUser("@alice:example.com", "Alice", "active"),
			makeAdminUser("@bob:example.com", "Bob", "active"),
		},
		total: 2,
	}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?search=ali", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminUser `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Errorf("[AC#1] expected 1 user (Alice) in search results, got %d", len(resp.Data))
	}
	if len(resp.Data) > 0 && resp.Data[0].DisplayName != "Alice" {
		t.Errorf("[AC#1] expected DisplayName=Alice in result, got %q", resp.Data[0].DisplayName)
	}
	for _, u := range resp.Data {
		if u.DisplayName == "Bob" {
			t.Error("[AC#1] Bob must not appear in search results for 'ali'")
		}
	}
}

// TestListAdminUsers_InvalidCursor_Returns400 covers AC#1 + AC#6 (test 4) [P0]:
// An invalid cursor must produce 400 M_BAD_REQUEST.
func TestListAdminUsers_InvalidCursor_Returns400(t *testing.T) {
	repo := &mockUserRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?cursor=not-valid-base64!!", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for invalid cursor, got %d", w.Code)
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected error code M_BAD_REQUEST, got %q", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "cursor") && !strings.Contains(strings.ToLower(resp.Error.Message), "invalid") {
		t.Errorf("[AC#1] expected error message to mention 'cursor' or 'invalid', got %q", resp.Error.Message)
	}
}

// TestListAdminUsers_LimitZero_Returns400 covers AC#1 + AC#6 (test 5) [P0]:
// limit=0 is out of the valid range [1,100] and must produce 400 M_BAD_REQUEST.
func TestListAdminUsers_LimitZero_Returns400(t *testing.T) {
	repo := &mockUserRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=0", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for limit=0, got %d", w.Code)
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected error code M_BAD_REQUEST for limit=0, got %q", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "limit") {
		t.Errorf("[AC#1] expected error message to mention 'limit', got %q", resp.Error.Message)
	}
}

// TestListAdminUsers_LimitAbove100_Returns400 covers AC#1 [P0]:
// limit=101 exceeds the maximum and must produce 400 M_BAD_REQUEST.
func TestListAdminUsers_LimitAbove100_Returns400(t *testing.T) {
	repo := &mockUserRepository{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=101", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("[AC#1] expected status 400 for limit=101, got %d", w.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_BAD_REQUEST" {
		t.Errorf("[AC#1] expected M_BAD_REQUEST for limit=101, got %q", resp.Error.Code)
	}
}

// ── AC#1: Status derivation ───────────────────────────────────────────────────

// TestListAdminUsers_StatusFields covers AC#1 [P0]:
// All four status values must be correctly serialised by the list endpoint.
func TestListAdminUsers_StatusFields(t *testing.T) {
	users := []api.AdminUser{
		makeAdminUser("@active:example.com", "Active User", "active"),
		makeAdminUser("@deactivated:example.com", "Deactivated User", "deactivated"),
		makeAdminUser("@keysdeleted:example.com", "Keys Deleted User", "keys_deleted"),
		makeAdminUser("@anon:example.com", "Anonymized User", "anonymized"),
	}
	repo := &mockUserRepository{users: users, total: 4}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []api.AdminUser `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#1] response is not valid JSON: %v", err)
	}

	wantStatuses := map[string]string{
		"@active:example.com":      "active",
		"@deactivated:example.com": "deactivated",
		"@keysdeleted:example.com": "keys_deleted",
		"@anon:example.com":        "anonymized",
	}
	for _, u := range resp.Data {
		want, ok := wantStatuses[u.UserID]
		if !ok {
			continue
		}
		if u.Status != want {
			t.Errorf("[AC#1] user %s: expected status %q, got %q", u.UserID, want, u.Status)
		}
	}
}

// ── AC#2: Get single user endpoint ───────────────────────────────────────────

// TestGetAdminUser_KnownUser_Returns200WithRoomCount covers AC#2 + AC#6 (test 6) [P0]:
// GET /api/v1/admin/users/{userId} for a known user must return 200 with the
// user's details including room_count derived from active room memberships.
func TestGetAdminUser_KnownUser_Returns200WithRoomCount(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	detail := &api.AdminUserDetail{
		AdminUser: api.AdminUser{
			UserID:      "@alice:example.com",
			DisplayName: "Alice",
			EmailMasked: "",
			Roles:       []string{"user"},
			Status:      "active",
			CreatedAt:   now,
			LastSeenAt:  nil,
		},
		RoomCount: 3,
	}
	repo := &mockUserRepository{detail: detail}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/@alice:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data api.AdminUserDetail `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}

	if resp.Data.UserID != "@alice:example.com" {
		t.Errorf("[AC#2] expected user_id @alice:example.com, got %q", resp.Data.UserID)
	}
	if resp.Data.RoomCount != 3 {
		t.Errorf("[AC#2] expected room_count=3, got %d", resp.Data.RoomCount)
	}
}

// TestGetAdminUser_UnknownUser_Returns404 covers AC#2 + AC#6 (test 7) [P0]:
// GET /api/v1/admin/users/{userId} for a user not in the repository must return
// 404 with error code M_NOT_FOUND.
func TestGetAdminUser_UnknownUser_Returns404(t *testing.T) {
	// detail=nil signals "not found" to the handler.
	repo := &mockUserRepository{detail: nil}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/@doesnotexist:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("[AC#2] expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("[AC#2] response is not valid JSON: %v", err)
	}
	if resp.Error.Code != "M_NOT_FOUND" {
		t.Errorf("[AC#2] expected error code M_NOT_FOUND, got %q", resp.Error.Code)
	}
	if resp.Error.Message == "" {
		t.Error("[AC#2] expected non-empty error message for 404")
	}
}

// ── AC#3: Route registration ──────────────────────────────────────────────────

// TestGetAdminUser_RouteRegistered covers AC#3 [P0]:
// GET /api/v1/admin/users/{userId} must be registered. A request without a role
// must be rejected (401 or 403), not 404 — 404 means the route is missing.
func TestGetAdminUser_RouteRegistered(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(&mockUserRepository{}, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/@some:example.com", nil)
	// No role set — use a middleware that injects no role to check for 401/403, not 404.
	noAuthMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	mux2 := http.NewServeMux()
	api.RegisterAdminRoutes(mux2, buildAdminServer(&mockUserRepository{}, nil), noAuthMW, nil)
	w := httptest.NewRecorder()
	mux2.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("[AC#3] GET /api/v1/admin/users/{userId} is not registered — got 404")
	}
}

// ── AC#1/AC#2: Audit log emission ────────────────────────────────────────────

// TestListAdminUsers_AuditLogEmitted covers AC#1 + AC#6 (test 8) [P0]:
// On a successful list request, audit.LogEvent must be called with
// action="admin_user_viewed" and target_type="user". target_id must be "".
func TestListAdminUsers_AuditLogEmitted(t *testing.T) {
	repo := &mockUserRepository{
		users: []api.AdminUser{makeAdminUser("@alice:example.com", "Alice", "active")},
		total: 1,
	}
	mockClient := &mockCoreClient{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, mockClient), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !mockClient.auditCalled {
		t.Error("[AC#1] expected audit.LogEvent to be called on list request")
	}
	if mockClient.lastAction != "admin_user_viewed" {
		t.Errorf("[AC#1] expected audit action 'admin_user_viewed', got %q", mockClient.lastAction)
	}
	if mockClient.lastTarget != "user" {
		t.Errorf("[AC#1] expected audit target_type 'user', got %q", mockClient.lastTarget)
	}
	if mockClient.lastTargetID != "" {
		t.Errorf("[AC#1] expected empty target_id for list audit, got %q", mockClient.lastTargetID)
	}
}

// TestGetAdminUser_AuditLogEmitted covers AC#2 [P1]:
// On a successful single-user request, audit.LogEvent must be called with
// action="admin_user_viewed", target_type="user", and target_id=userId.
func TestGetAdminUser_AuditLogEmitted(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	detail := &api.AdminUserDetail{
		AdminUser: api.AdminUser{
			UserID:      "@alice:example.com",
			DisplayName: "Alice",
			EmailMasked: "",
			Roles:       []string{"user"},
			Status:      "active",
			CreatedAt:   now,
			LastSeenAt:  nil,
		},
		RoomCount: 0,
	}
	repo := &mockUserRepository{detail: detail}
	mockClient := &mockCoreClient{}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, mockClient), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/@alice:example.com", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#2] expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if !mockClient.auditCalled {
		t.Error("[AC#2] expected audit.LogEvent to be called on get-user request")
	}
	if mockClient.lastAction != "admin_user_viewed" {
		t.Errorf("[AC#2] expected audit action 'admin_user_viewed', got %q", mockClient.lastAction)
	}
	if mockClient.lastTargetID != "@alice:example.com" {
		t.Errorf("[AC#2] expected audit target_id '@alice:example.com', got %q", mockClient.lastTargetID)
	}
}

// ── AC#1: AdminUser field presence ───────────────────────────────────────────

// TestListAdminUsers_UserObjectFields covers AC#1 [P0]:
// Each user object must contain all mandatory fields: user_id, display_name,
// email_masked, roles, status, created_at. last_seen_at may be null.
func TestListAdminUsers_UserObjectFields(t *testing.T) {
	user := makeAdminUser("@alice:example.com", "Alice", "active")
	repo := &mockUserRepository{users: []api.AdminUser{user}, total: 1}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("[AC#1] expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	requiredFields := []string{
		`"user_id"`,
		`"display_name"`,
		`"email_masked"`,
		`"roles"`,
		`"status"`,
		`"created_at"`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(body, field) {
			t.Errorf("[AC#1] expected field %s in user object; body: %s", field, body)
		}
	}
}

// ── AC#1: Default limit ───────────────────────────────────────────────────────

// TestListAdminUsers_DefaultLimit covers AC#1 [P1]:
// When no limit is specified, the handler must use the default limit of 20.
// We verify by checking that the repository was called (status 200) and no
// validation error was returned.
func TestListAdminUsers_DefaultLimit_NoError(t *testing.T) {
	repo := &mockUserRepository{users: []api.AdminUser{}, total: 0}

	mux := http.NewServeMux()
	api.RegisterAdminRoutes(mux, buildAdminServer(repo, nil), noopJWTMiddlewareForUsers, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("[AC#1] expected 200 for default limit (no limit param), got %d; body: %s", w.Code, w.Body.String())
	}
}

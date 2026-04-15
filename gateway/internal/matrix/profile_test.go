package matrix

// ─── Story 4-18: GET/PUT /_matrix/client/v3/profile/{userId}[/displayname|/avatar_url] ─
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-18 is implemented.
//
// Test strategy:
//   - mockProfileCoreClient implements ProfileCoreClient (consumer-defined interface,
//     Go convention) — defined here alongside the tests.
//   - mockProfileDB implements ProfileDB interface for direct DB reads (GET profile).
//   - buildProfileHandler wires ProfileHandler on a mux without JWTMiddleware (GET
//     is a public endpoint — no auth required).
//   - buildAuthedProfileHandler wires JWTMiddleware → ProfileHandler for PUT endpoints.
//   - A capturedReq field lets tests inspect the gRPC UpdateProfileRequest forwarded
//     by the handler (user_id, displayname, avatar_url).
//   - GET /profile/{userId} — no Authorization header needed (public endpoint).
//   - PUT displayname/avatar_url — JWT required; path userId must match JWT sub.
//   - userId-mismatch test: path userId differs from JWT sub → 403 BEFORE Core call.
//   - Unauthenticated PUT test: omit Authorization header → 401 from JWTMiddleware.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockProfileCoreClient implements ProfileCoreClient (defined in profile.go).
// capturedReq records the last UpdateProfileRequest forwarded so tests can assert
// the handler built the correct gRPC payload.

type mockProfileCoreClient struct {
	resp        *pb.UpdateProfileResponse
	err         error
	capturedReq *pb.UpdateProfileRequest
}

func (m *mockProfileCoreClient) UpdateProfile(_ context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Mock ProfileDB ───────────────────────────────────────────────────────────

// mockProfileDB implements ProfileDB (defined in profile.go).
// When found is true, GetProfile returns a populated ProfileData.
// When found is false, GetProfile returns nil, ErrProfileNotFound (sentinel).

type mockProfileDB struct {
	found       bool
	displayName string
	avatarURL   string
}

func (m *mockProfileDB) GetProfile(_ context.Context, _ string) (*ProfileData, error) {
	if !m.found {
		return nil, ErrProfileNotFound
	}
	return &ProfileData{
		DisplayName: m.displayName,
		AvatarURL:   m.avatarURL,
	}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildProfileHandler wires a ProfileHandler on a mux WITHOUT JWTMiddleware.
// Used for GET /profile/{userId} (public endpoint).
//
// Returns the http.Handler ready for httptest.
func buildProfileHandler(t *testing.T, coreMock *mockProfileCoreClient, dbMock *mockProfileDB) http.Handler {
	t.Helper()

	handler := NewProfileHandler(ProfileConfig{
		CoreClient: coreMock,
		ServerName: "test.local",
		DB:         dbMock,
	})

	mux := http.NewServeMux()
	// GET is unauthenticated — no jwtMiddleware wrapper (per AC1 and story main.go note).
	mux.HandleFunc("GET /profile/{userId}", handler.GetProfile)

	return mux
}

// buildAuthedProfileHandler wires JWTMiddleware → ProfileHandler for PUT endpoints.
//
// Returns the http.Handler, the OIDC test server, and a makeToken closure.
// JWT sub is always "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
func buildAuthedProfileHandler(t *testing.T, coreMock *mockProfileCoreClient, dbMock *mockProfileDB) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewProfileHandler(ProfileConfig{
		CoreClient: coreMock,
		ServerName: "test.local",
		DB:         dbMock,
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("PUT /profile/{userId}/displayname",
		jwtMiddleware(http.HandlerFunc(handler.PutDisplayname)))
	mux.Handle("PUT /profile/{userId}/avatar_url",
		jwtMiddleware(http.HandlerFunc(handler.PutAvatarURL)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: GET /profile/{userId} — happy path → 200 ────────────────────────
//
// AC #1 — GET without Authorization header returns 200 with displayname + avatar_url.
// No JWT required (public endpoint per Matrix spec).

func TestGetProfile_HappyPath(t *testing.T) {
	dbMock := &mockProfileDB{
		found:       true,
		displayName: "Alice",
		avatarURL:   "mxc://test.local/abc123",
	}
	coreMock := &mockProfileCoreClient{}
	mux := buildProfileHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(http.MethodGet, "/profile/@alice:test.local", nil)
	// Deliberately no Authorization header — GET profile is public.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp["displayname"] != "Alice" {
		t.Errorf("expected displayname=Alice, got %v", resp["displayname"])
	}
	if resp["avatar_url"] != "mxc://test.local/abc123" {
		t.Errorf("expected avatar_url=mxc://test.local/abc123, got %v", resp["avatar_url"])
	}
}

// ─── Test 2: GET /profile/{userId} — no JWT required → 200 ──────────────────
//
// AC #1 — Explicitly verifies that the absence of an Authorization header does
// NOT result in 401. GET /profile is a public endpoint; JWTMiddleware must not
// wrap this route.

func TestGetProfile_NoJWT_Allowed(t *testing.T) {
	dbMock := &mockProfileDB{
		found:       true,
		displayName: "Bob",
		avatarURL:   "mxc://test.local/bob",
	}
	coreMock := &mockProfileCoreClient{}
	mux := buildProfileHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(http.MethodGet, "/profile/@bob:test.local", nil)
	// No Authorization header at all.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must be 200, NOT 401 — public endpoint.
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("GET /profile must be public (no auth), but got 401")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 3: PUT /profile/{userId}/displayname — happy path → 200 ────────────
//
// AC #2 — Authenticated user sets displayname on their own profile.
// Mock receives UpdateProfileRequest{user_id, displayname: "Alice Nebu", avatar_url: ""}.

func TestPutDisplayname_HappyPath(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"displayname": "Alice Nebu"}`
	// Path userId must match JWT sub "test-sub-123" → "@test-sub-123:test.local".
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", resp)
	}

	// Assert the gRPC request was built correctly.
	if coreMock.capturedReq == nil {
		t.Fatal("expected UpdateProfile to be called, but capturedReq is nil")
	}
	if coreMock.capturedReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected user_id %q, got %q", "@test-sub-123:test.local", coreMock.capturedReq.UserId)
	}
	if coreMock.capturedReq.Displayname != "Alice Nebu" {
		t.Errorf("expected displayname=%q, got %q", "Alice Nebu", coreMock.capturedReq.Displayname)
	}
	// avatar_url must be empty string (not updating it).
	if coreMock.capturedReq.AvatarUrl != "" {
		t.Errorf("expected avatar_url empty, got %q", coreMock.capturedReq.AvatarUrl)
	}
}

// ─── Test 4: PUT /profile/{userId}/displayname — userId mismatch → 403 ───────
//
// AC #2 — Path userId differs from JWT sub → 403 M_FORBIDDEN.
// Core must NOT be called (mismatch check must happen before gRPC call).

func TestPutDisplayname_UserIdMismatch(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"displayname": "Alice"}`
	// JWT sub is "test-sub-123" → "@test-sub-123:test.local", but path has @bob:test.local.
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@bob:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on userId mismatch, but capturedReq is set")
	}
}

// ─── Test 5: PUT /profile/{userId}/displayname — unauthenticated → 401 ───────
//
// AC #2 — JWTMiddleware must reject PUT requests missing Authorization header.
// Core must NOT be called.

func TestPutDisplayname_Unauthenticated(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, _ := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"displayname": "Alice"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on unauthenticated request, but capturedReq is set")
	}
}

// ─── Test 6: PUT /profile/{userId}/avatar_url — happy path → 200 ─────────────
//
// AC #3 — Authenticated user sets avatar_url with a valid mxc:// URI.
// Mock receives UpdateProfileRequest{user_id, displayname: "", avatar_url: "mxc://..."}.

func TestPutAvatarUrl_HappyPath(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"avatar_url": "mxc://test.local/img1"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/avatar_url",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", resp)
	}

	// Assert the gRPC request was built correctly.
	if coreMock.capturedReq == nil {
		t.Fatal("expected UpdateProfile to be called, but capturedReq is nil")
	}
	if coreMock.capturedReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected user_id %q, got %q", "@test-sub-123:test.local", coreMock.capturedReq.UserId)
	}
	if coreMock.capturedReq.AvatarUrl != "mxc://test.local/img1" {
		t.Errorf("expected avatar_url=%q, got %q", "mxc://test.local/img1", coreMock.capturedReq.AvatarUrl)
	}
	// displayname must be empty string (not updating it).
	if coreMock.capturedReq.Displayname != "" {
		t.Errorf("expected displayname empty, got %q", coreMock.capturedReq.Displayname)
	}
}

// ─── Test 7: GET /profile/{userId} — not found → 404 ─────────────────────────
//
// MAJOR-1 — When the DB returns ErrProfileNotFound the handler must return
// 404 with errcode M_NOT_FOUND. Core (gRPC) is never involved in GET profile
// (it is a direct DB read), but GetProfile on the mock IS called (it is the DB
// lookup that returns not found).

func TestGetProfile_NotFound(t *testing.T) {
	dbMock := &mockProfileDB{
		found: false, // GetProfile returns nil, ErrProfileNotFound
	}
	coreMock := &mockProfileCoreClient{}
	mux := buildProfileHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(http.MethodGet, "/profile/@unknown:test.local", nil)
	// Deliberately no Authorization header — GET profile is public.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %s", errResp.ErrCode)
	}

	// gRPC UpdateProfile must NOT have been called — GET profile never calls Core.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on GET profile, but capturedReq is set")
	}
}

// ─── Test 8: PUT /profile/{userId}/displayname — empty displayname → 400 ─────
//
// MAJOR-2 — displayname must be 1–128 chars. Empty string must be rejected
// with 400 M_INVALID_PARAM before the gRPC call is made.

func TestPutDisplayname_EmptyDisplayname(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"displayname": ""}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called — validation happens before gRPC call.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on empty displayname, but capturedReq is set")
	}
}

// ─── Test 9: PUT /profile/{userId}/displayname — displayname too long → 400 ──
//
// MAJOR-3 — displayname must be 1–128 chars. A 129-character string must be
// rejected with 400 M_INVALID_PARAM before the gRPC call is made.

func TestPutDisplayname_TooLong(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	// 129 'a' characters — one over the 128-char limit.
	tooLong := strings.Repeat("a", 129)
	body := `{"displayname": "` + tooLong + `"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called — validation happens before gRPC call.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on too-long displayname, but capturedReq is set")
	}
}

// ─── Test 10: PUT /profile/{userId}/avatar_url — non-mxc URL → 400 ───────────
//
// MAJOR-4 — avatar_url must begin with mxc://. An https:// URL must be
// rejected with 400 M_INVALID_PARAM before the gRPC call is made.

func TestPutAvatarUrl_NonMxcUrl(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"avatar_url": "https://cdn.example.com/img.jpg"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/avatar_url",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called — validation happens before gRPC call.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on non-mxc avatar_url, but capturedReq is set")
	}
}

// ─── Test 11: PUT /profile/{userId}/avatar_url — unauthenticated → 401 ───────
//
// MINOR-2 — JWTMiddleware must reject PUT /avatar_url requests that omit the
// Authorization header. Core must NOT be called.

func TestPutAvatarUrl_Unauthenticated(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, _ := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"avatar_url": "mxc://test.local/img1"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/avatar_url",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on unauthenticated request, but capturedReq is set")
	}
}

// ─── Test 12: PUT /profile/{userId}/avatar_url — userId mismatch → 403 ───────
//
// AC #3 — Path userId differs from JWT sub → 403 M_FORBIDDEN.
// Core must NOT be called.

func TestPutAvatarUrl_UserIdMismatch(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	body := `{"avatar_url": "mxc://test.local/img1"}`
	// JWT sub is "test-sub-123" → "@test-sub-123:test.local", but path has @bob:test.local.
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@bob:test.local/avatar_url",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called.
	if coreMock.capturedReq != nil {
		t.Error("expected UpdateProfile NOT to be called on userId mismatch, but capturedReq is set")
	}
}

// ─── Test 13: PUT /profile/{userId}/displayname — multi-byte unicode 128 chars → 200 ─
//
// Edge case: displayname with 128 multi-byte CJK characters (each 3 bytes in UTF-8).
// len() returns 384 bytes but utf8.RuneCountInString returns 128 runes.
// Must be accepted (AC says "1-128 chars", not bytes).

func TestPutDisplayname_MultiByteUnicode128Chars(t *testing.T) {
	coreMock := &mockProfileCoreClient{
		resp: &pb.UpdateProfileResponse{},
	}
	mux, _, makeToken := buildAuthedProfileHandler(t, coreMock, &mockProfileDB{})

	// 128 CJK characters — each is 3 bytes in UTF-8 (total 384 bytes).
	displayname := strings.Repeat("\u4e16", 128) // 世 repeated 128 times
	body := `{"displayname": "` + displayname + `"}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/profile/@test-sub-123:test.local/displayname",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for 128-char multi-byte displayname, got %d; body: %s", w.Code, w.Body.String())
	}
}

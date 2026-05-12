package matrix

// ─── Story 7-20: GET /_matrix/client/v3/rooms/{roomId}/joined_members ────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until members.go contains
// GetJoinedMembersHandler.
//
// Acceptance Criteria covered:
//   AC1 — GET /joined_members → 200 with "joined" map containing joined user IDs
//   AC2 — Each value has display_name and avatar_url (null if no profile)
//   AC3 — Non-member → 403 M_FORBIDDEN (gRPC PermissionDenied)
//   AC4 — Unknown room → 404 M_NOT_FOUND (gRPC NotFound)
//   AC5 — No pagination — all joined members returned in single response
//   AC6 — JWT required — jwtMiddleware enforces auth before handler
//   AC7 — Missing profile → null display_name and avatar_url (no 404 raised)
//
// Test strategy:
//   - mockGetJoinedMembersCoreClient implements GetJoinedMembersCoreClient
//     (consumer-defined interface, Go convention) — will be declared in members.go.
//   - mockJoinedMembersProfileDB implements ProfileDB (reuses the interface
//     defined in profile.go) — returns ErrProfileNotFound for unknown users.
//   - buildAuthedJoinedMembersHandler wires JWTMiddleware → GetJoinedMembersHandler
//     so the full auth → handler pipeline is exercised at httptest level.
//   - A capturedReq field lets tests inspect the gRPC GetRoomStateRequest.
//
// NOTE: GetJoinedMembersCoreClient, GetJoinedMembersHandler, GetJoinedMembersConfig,
// NewGetJoinedMembersHandler, GetJoinedMembers are declared in
// gateway/internal/matrix/members.go — which does NOT contain them yet.
// Every test in this file MUST fail with a compilation error until members.go
// is updated.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockGetJoinedMembersCoreClient implements GetJoinedMembersCoreClient
// (will be defined in members.go). capturedReq records the last
// GetRoomStateRequest forwarded so tests can assert the handler built the
// correct gRPC payload.

type mockGetJoinedMembersCoreClient struct {
	resp        *pb.GetRoomStateResponse
	err         error
	capturedReq *pb.GetRoomStateRequest
}

func (m *mockGetJoinedMembersCoreClient) GetRoomState(_ context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Mock ProfileDB ───────────────────────────────────────────────────────────

// mockJoinedMembersProfileDB implements ProfileDB (defined in profile.go).
// profiles maps user IDs to their profile data. Missing entries return
// ErrProfileNotFound, exercising the "null fields" path (AC2, AC7).

type mockJoinedMembersProfileDB struct {
	profiles map[string]*ProfileData
}

func (m *mockJoinedMembersProfileDB) GetProfile(_ context.Context, userID string) (*ProfileData, error) {
	if p, ok := m.profiles[userID]; ok {
		return p, nil
	}
	return nil, ErrProfileNotFound
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedJoinedMembersHandler wires JWTMiddleware → GetJoinedMembersHandler
// and registers it on a mux with the correct GET pattern so PathValue resolves.
//
// JWT sub is always "test-sub-123", authenticated user_id = "@test-sub-123:test.local".
func buildAuthedJoinedMembersHandler(
	t *testing.T,
	coreMock *mockGetJoinedMembersCoreClient,
	dbMock *mockJoinedMembersProfileDB,
) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetJoinedMembersHandler(GetJoinedMembersConfig{
		CoreClient: coreMock,
		DB:         dbMock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")(
		http.HandlerFunc(handler.GetJoinedMembers),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /rooms/{roomId}/joined_members", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 1: Happy path — room with two members and known profiles ─────────────
//
// AC1: returns 200 with "joined" map containing both member IDs.
// AC2: each entry has display_name and avatar_url from the profile DB.
// AC5: all members returned in a single response (no pagination keys).

func TestGetJoinedMembers_HappyPath(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{
		resp: &pb.GetRoomStateResponse{
			Members: []string{"@alice:test.local", "@bob:test.local"},
		},
	}
	dbMock := &mockJoinedMembersProfileDB{
		profiles: map[string]*ProfileData{
			"@alice:test.local": {DisplayName: "Alice", AvatarURL: "mxc://test.local/alice-avatar"},
			"@bob:test.local":   {DisplayName: "Bob", AvatarURL: ""},
		},
	}

	mux, makeToken := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/joined_members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Joined map[string]map[string]any `json:"joined"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	if body.Joined == nil {
		t.Fatal("expected non-null 'joined' object in response")
	}
	if len(body.Joined) != 2 {
		t.Fatalf("expected 2 entries in joined map, got %d", len(body.Joined))
	}

	// Verify @alice:test.local entry.
	alice, ok := body.Joined["@alice:test.local"]
	if !ok {
		t.Error("@alice:test.local missing from joined map")
	} else if alice["display_name"] != "Alice" {
		t.Errorf("@alice:test.local: expected display_name Alice, got %v", alice["display_name"])
	}

	// Verify @bob:test.local entry.
	if _, ok := body.Joined["@bob:test.local"]; !ok {
		t.Error("@bob:test.local missing from joined map")
	}

	// Verify no pagination keys are present (AC5).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("raw unmarshal failed: %v", err)
	}
	for _, unwanted := range []string{"next_batch", "start", "end", "chunk"} {
		if _, found := raw[unwanted]; found {
			t.Errorf("unexpected pagination key %q found in response", unwanted)
		}
	}

	// Verify handler forwarded correct room_id to Core.
	if coreMock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if coreMock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id !room1:test.local, got %s", coreMock.capturedReq.RoomId)
	}
}

// ─── Test 2: Profile not found → fields omitted for that member ──────────────
//
// AC2 + AC7: when ProfileDB returns ErrProfileNotFound, the member must still
// appear in the "joined" map. The handler's documented choice is "omit" — both
// display_name and avatar_url MUST be absent from the per-user object (not
// present-but-null). No 404 should be raised — missing profile is not an error.

func TestGetJoinedMembers_ProfileNull_WhenNoProfile(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{
		resp: &pb.GetRoomStateResponse{
			Members: []string{"@new:test.local"},
		},
	}
	// Empty profiles map → ErrProfileNotFound for every lookup.
	dbMock := &mockJoinedMembersProfileDB{
		profiles: map[string]*ProfileData{},
	}

	mux, makeToken := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/joined_members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Parse body as raw map so we can distinguish "field absent" from "field present-but-null".
	var body struct {
		Joined map[string]map[string]json.RawMessage `json:"joined"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	newUser, ok := body.Joined["@new:test.local"]
	if !ok {
		t.Fatal("@new:test.local missing from joined map despite being a member")
	}

	// Per the handler's documented "omit" convention (joinedMemberProfile uses
	// omitempty on *string fields), display_name and avatar_url MUST be absent
	// from the JSON object when no profile exists — not present-but-null.
	for _, field := range []string{"display_name", "avatar_url"} {
		if raw, exists := newUser[field]; exists {
			t.Errorf("@new:test.local %s: expected field to be OMITTED (no profile), but it was present with value %s", field, raw)
		}
	}
}

// ─── Test 3: Room not found → 404 M_NOT_FOUND ────────────────────────────────
//
// AC4: gRPC NotFound → handler returns 404 with errcode M_NOT_FOUND.

func TestGetJoinedMembers_RoomNotFound(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}
	dbMock := &mockJoinedMembersProfileDB{profiles: map[string]*ProfileData{}}

	mux, makeToken := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!doesnotexist%3Atest.local/joined_members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %v", body["errcode"])
	}
}

// ─── Test 4: Not a member → 403 M_FORBIDDEN ──────────────────────────────────
//
// AC3: gRPC PermissionDenied → handler returns 403 with errcode M_FORBIDDEN.

func TestGetJoinedMembers_Forbidden_NonMember(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}
	dbMock := &mockJoinedMembersProfileDB{profiles: map[string]*ProfileData{}}

	mux, makeToken := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!private%3Atest.local/joined_members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %v", body["errcode"])
	}
}

// ─── Test 5: Unauthenticated → 401 ───────────────────────────────────────────
//
// AC6: jwtMiddleware enforces auth — request without Bearer token gets 401
// before the handler is called.

func TestGetJoinedMembers_Unauthenticated(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{}
	dbMock := &mockJoinedMembersProfileDB{profiles: map[string]*ProfileData{}}

	mux, _ := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/joined_members",
		nil,
	)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called (AC6).
	if coreMock.capturedReq != nil {
		t.Error("Core.GetRoomState must not be called for unauthenticated requests")
	}
}

// ─── Test 6: Core unavailable → 503 M_UNAVAILABLE ────────────────────────────

func TestGetJoinedMembers_CoreUnavailable(t *testing.T) {
	coreMock := &mockGetJoinedMembersCoreClient{
		err: status.Error(codes.Unavailable, "core is down"),
	}
	dbMock := &mockJoinedMembersProfileDB{profiles: map[string]*ProfileData{}}

	mux, makeToken := buildAuthedJoinedMembersHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/joined_members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %v", body["errcode"])
	}
}

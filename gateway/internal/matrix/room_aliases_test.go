package matrix

// ─── Story 7-23: GET /_matrix/client/v3/rooms/{roomId}/aliases ───────────────
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until rooms.go contains GetRoomAliasesHandler.
//
// Acceptance Criteria covered:
//   AC1 — GET /aliases → 200 with {"aliases":[]} for a current room member
//   AC2 — Non-member → 403 M_FORBIDDEN (gRPC PermissionDenied)
//   AC3 — Unknown room → 404 M_NOT_FOUND (gRPC NotFound)
//   AC4 — JWT required — jwtMiddleware enforces auth before handler
//   AC5 — Response always contains "aliases" key, even when the array is empty
//   AC6 — Handler is extensible: aliases field populated from gRPC in a future story
//
// Design decisions:
//   - GetRoomAliasesCoreClient is a new consumer-defined interface (Go convention, ADR-009)
//     declared here alongside the tests; it will live in rooms.go when implemented.
//   - mockGetRoomAliasesCoreClient records capturedReq so tests assert correct gRPC payload.
//   - buildAuthedGetRoomAliasesHandler registers the route on a ServeMux so PathValue("roomId")
//     resolves correctly.
//
// NOTE: GetRoomAliasesCoreClient, GetRoomAliasesHandler, GetRoomAliasesConfig,
// NewGetRoomAliasesHandler, and GetRoomAliases are declared in
// gateway/internal/matrix/rooms.go — which does NOT contain them yet.
// Every test in this file MUST fail with a compilation error until rooms.go
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

// mockGetRoomAliasesCoreClient implements GetRoomAliasesCoreClient
// (will be defined in rooms.go). capturedReq records the last
// GetRoomStateRequest forwarded so tests can assert the handler built the
// correct gRPC payload.
type mockGetRoomAliasesCoreClient struct {
	resp        *pb.GetRoomStateResponse
	err         error
	capturedReq *pb.GetRoomStateRequest
}

func (m *mockGetRoomAliasesCoreClient) GetRoomState(_ context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedGetRoomAliasesHandler wires JWTMiddleware → GetRoomAliasesHandler
// and registers it on a mux with the correct GET pattern so PathValue resolves.
//
// The handler types (GetRoomAliasesHandler, GetRoomAliasesConfig,
// NewGetRoomAliasesHandler) are expected to be declared in rooms.go.
// This file will not compile until they exist.
func buildAuthedGetRoomAliasesHandler(t *testing.T, mock *mockGetRoomAliasesCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetRoomAliasesHandler(GetRoomAliasesConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetRoomAliases),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/aliases", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── AC1 + AC5: GET /aliases → 200 with {"aliases":[]} ───────────────────────
//
// Mock returns a successful GetRoomStateResponse (membership verified).
// Handler must return 200 with a JSON object containing an "aliases" key
// whose value is an empty array (never null, never omitted — AC5).

func TestGetRoomAliases_HappyPath_EmptyArray(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		resp: &pb.GetRoomStateResponse{
			Members: []string{"@alice:test.local"},
		},
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be a JSON object with an "aliases" key.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	rawAliases, ok := body["aliases"]
	if !ok {
		t.Fatalf("expected 'aliases' key in response, got: %s", w.Body.String())
	}

	// AC5: must be an array (not null, not missing).
	var aliases []string
	if err := json.Unmarshal(rawAliases, &aliases); err != nil {
		t.Fatalf("aliases field is not a JSON array: %v; raw: %s", err, rawAliases)
	}

	// AC1: MVP returns empty array — no aliases stored yet.
	if len(aliases) != 0 {
		t.Errorf("expected empty aliases array, got %v", aliases)
	}

	// Verify the handler forwarded the correct room_id to Core.
	if mock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id !room1:test.local, got %s", mock.capturedReq.RoomId)
	}
}

// ─── AC5 edge: aliases field must never be null ───────────────────────────────
//
// When Core returns a response with no member data, the handler still MUST
// produce {"aliases":[]} — not {"aliases":null} or {}.

func TestGetRoomAliases_AliasesFieldNeverNull(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		resp: &pb.GetRoomStateResponse{},
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Raw body check: "aliases":null is not acceptable.
	body := w.Body.String()
	if body == "" {
		t.Fatal("response body is empty")
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, body)
	}

	rawAliases, ok := parsed["aliases"]
	if !ok {
		t.Fatalf("'aliases' key missing from response; body: %s", body)
	}
	if string(rawAliases) == "null" {
		t.Errorf("'aliases' must not be null; body: %s", body)
	}

	// Must be a parseable empty array.
	var aliases []string
	if err := json.Unmarshal(rawAliases, &aliases); err != nil {
		t.Fatalf("aliases is not a JSON array: %v; raw: %s", err, rawAliases)
	}
	if len(aliases) != 0 {
		t.Errorf("expected empty array, got %v", aliases)
	}
}

// ─── AC2: Non-member → 403 M_FORBIDDEN ───────────────────────────────────────
//
// Core returns codes.PermissionDenied — handler must map to HTTP 403 M_FORBIDDEN.

func TestGetRoomAliases_Forbidden_NonMember(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!private:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", errResp["errcode"])
	}
}

// ─── AC3: Unknown room → 404 M_NOT_FOUND ─────────────────────────────────────
//
// Core returns codes.NotFound — handler must map to HTTP 404 M_NOT_FOUND.

func TestGetRoomAliases_NotFound_UnknownRoom(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!doesnotexist:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp["errcode"])
	}
}

// ─── AC4: No JWT → 401 ───────────────────────────────────────────────────────
//
// jwtMiddleware must reject requests without a valid Authorization header
// before the handler is reached. Core must NOT be called.

func TestGetRoomAliases_Unauthenticated(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{}

	mux, _ := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/aliases",
		nil,
	)
	// Deliberately no Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must NOT have been called — middleware must have short-circuited.
	if mock.capturedReq != nil {
		t.Error("Core.GetRoomState must not be called for unauthenticated requests")
	}
}

// ─── Error mapping: gRPC Unavailable → 503 M_UNAVAILABLE ─────────────────────

func TestGetRoomAliases_ServiceUnavailable(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		err: status.Error(codes.Unavailable, "core unavailable"),
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %q", errResp["errcode"])
	}
}

// ─── Error mapping: gRPC default → 500 M_UNKNOWN ─────────────────────────────

func TestGetRoomAliases_InternalServerError(t *testing.T) {
	mock := &mockGetRoomAliasesCoreClient{
		err: status.Error(codes.Internal, "internal error"),
	}

	mux, makeToken := buildAuthedGetRoomAliasesHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/aliases",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp["errcode"])
	}
}

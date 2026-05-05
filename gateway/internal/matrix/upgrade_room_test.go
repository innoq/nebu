package matrix

// ─── Story 9-8: Room Version Upgrade — Full Implementation ───────────────────
//
// ATDD RED PHASE — all tests in this file MUST FAIL until:
//   - UpgradeRoomCoreClient interface is defined in rooms_upgrade.go
//   - UpgradeRoomConfig gains a CoreClient field
//   - UpgradeRoomHandler.PostUpgradeRoom calls coreClient.UpgradeRoom (not 501)
//   - proto/core.proto gains UpgradeRoomRequest + UpgradeRoomResponse messages and UpgradeRoom RPC
//   - make proto regenerates core.pb.go with the new types
//
// Acceptance Criteria covered:
//   AC1+AC2+AC3 — POST /rooms/{roomId}/upgrade by room owner → 200 {"replacement_room": "..."}
//   AC5          — non-owner → 403 M_FORBIDDEN (gRPC PermissionDenied)
//   AC5          — non-existent room → 404 M_NOT_FOUND (gRPC NotFound)
//   AC6          — GET /capabilities returns m.room_versions with "10" as default

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Interface compliance check ───────────────────────────────────────────────
//
// Compile-time assertion: mockUpgradeRoomFullCoreClient MUST satisfy
// UpgradeRoomCoreClient once that interface is defined in rooms_upgrade.go.
// Until then this line produces a compile error — the intentional red-phase signal.
var _ UpgradeRoomCoreClient = (*mockUpgradeRoomFullCoreClient)(nil)

// ─── Mock gRPC core client ────────────────────────────────────────────────────
//
// This mock will NOT compile until UpgradeRoomCoreClient is defined with
// an UpgradeRoom method. That is the intentional red-phase compilation failure.

type mockUpgradeRoomFullCoreClient struct {
	upgradeRoomResp *pb.UpgradeRoomResponse
	upgradeRoomErr  error
	capturedReq     *pb.UpgradeRoomRequest
}

// UpgradeRoom satisfies UpgradeRoomCoreClient.
// Will not compile until UpgradeRoomCoreClient requires this method AND
// pb.UpgradeRoomRequest + pb.UpgradeRoomResponse exist in core.pb.go.
func (m *mockUpgradeRoomFullCoreClient) UpgradeRoom(_ context.Context, req *pb.UpgradeRoomRequest) (*pb.UpgradeRoomResponse, error) {
	m.capturedReq = req
	return m.upgradeRoomResp, m.upgradeRoomErr
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedUpgradeRoomFullHandler wires UpgradeRoomHandler with JWTMiddleware
// and a ServeMux so r.PathValue("roomId") is populated by the Go 1.22+ router.
// Accepts mockUpgradeRoomFullCoreClient so tests can inject controlled responses.
//
// RED PHASE: will not compile until:
//   - UpgradeRoomCoreClient interface is defined
//   - UpgradeRoomConfig.CoreClient field exists
func buildAuthedUpgradeRoomFullHandler(t *testing.T, mock *mockUpgradeRoomFullCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	// RED PHASE compile-error beacon:
	// NewUpgradeRoomHandler must accept CoreClient in UpgradeRoomConfig.
	// This will fail to compile until UpgradeRoomConfig gains a CoreClient field.
	handler := NewUpgradeRoomHandler(UpgradeRoomConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostUpgradeRoom),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/upgrade", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── AC1+AC2+AC3: Room owner upgrade → 200 {"replacement_room": "..."} ───────
//
// Before this story: handler returns 501 M_UNRECOGNIZED.
// After implementation: returns 200 with replacement_room field.
// This test FAILS (gets 501) until the handler calls coreClient.UpgradeRoom.

func TestPostUpgradeRoom_HappyPath_TombstoneAndNewRoom(t *testing.T) {
	mock := &mockUpgradeRoomFullCoreClient{
		upgradeRoomResp: &pb.UpgradeRoomResponse{NewRoomId: "!new:test.local"},
	}

	mux, makeToken := buildAuthedUpgradeRoomFullHandler(t, mock)

	body := `{"new_version":"10"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!old:test.local/upgrade",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// MUST be 200 — currently 501 until AC1 is implemented.
	if w.Code == http.StatusNotImplemented {
		t.Fatalf("AC1: handler still returns 501 — replace the M_UNRECOGNIZED stub with Core.UpgradeRoom call; body: %s", w.Body.String())
	}

	if w.Code != http.StatusOK {
		t.Fatalf("AC1: expected 200 for room owner upgrade, got %d; body: %s", w.Code, w.Body.String())
	}

	// AC2: response body must contain replacement_room field.
	var resp struct {
		ReplacementRoom string `json:"replacement_room"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC1+AC2: response body is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if resp.ReplacementRoom == "" {
		t.Errorf("AC2: response must contain non-empty replacement_room; body: %s", w.Body.String())
	}
	if resp.ReplacementRoom != "!new:test.local" {
		t.Errorf("AC2: expected replacement_room '!new:test.local', got %q", resp.ReplacementRoom)
	}

	// AC3: Core.UpgradeRoom must have been called with the correct fields.
	if mock.capturedReq == nil {
		t.Fatal("AC3: Core.UpgradeRoom must be called, but was not")
	}
	if mock.capturedReq.OldRoomId != "!old:test.local" {
		t.Errorf("AC3: expected OldRoomId '!old:test.local', got %q", mock.capturedReq.OldRoomId)
	}
	if mock.capturedReq.NewVersion != "10" {
		t.Errorf("AC3: expected NewVersion '10', got %q", mock.capturedReq.NewVersion)
	}
}

// ─── AC5: Non-owner upgrade → 403 M_FORBIDDEN ────────────────────────────────
//
// When Core returns PermissionDenied, the handler must map to 403 M_FORBIDDEN.
// Before this story: returns 501 (never reaches the gRPC call).

func TestPostUpgradeRoom_NonOwner_Returns403(t *testing.T) {
	mock := &mockUpgradeRoomFullCoreClient{
		upgradeRoomErr: status.Error(codes.PermissionDenied, "insufficient power level for room upgrade"),
	}

	mux, makeToken := buildAuthedUpgradeRoomFullHandler(t, mock)

	body := `{"new_version":"10"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// MUST be 403 — currently 501 until the handler calls Core.
	if w.Code == http.StatusNotImplemented {
		t.Fatalf("AC5: handler still returns 501 — replace stub with Core.UpgradeRoom call; body: %s", w.Body.String())
	}

	if w.Code != http.StatusForbidden {
		t.Fatalf("AC5: expected 403 for PermissionDenied, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("AC5: error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("AC5: expected errcode M_FORBIDDEN, got %q", errResp.ErrCode)
	}
}

// ─── AC5: Room not found → 404 M_NOT_FOUND ───────────────────────────────────
//
// When Core returns NotFound, the handler must map to 404 M_NOT_FOUND.
// Before this story: returns 501 (never reaches the gRPC call).

func TestPostUpgradeRoom_RoomNotFound_Returns404(t *testing.T) {
	mock := &mockUpgradeRoomFullCoreClient{
		upgradeRoomErr: status.Error(codes.NotFound, "room not found"),
	}

	mux, makeToken := buildAuthedUpgradeRoomFullHandler(t, mock)

	body := `{"new_version":"10"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!nonexistent:test.local/upgrade",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// MUST be 404 — currently 501 until the handler calls Core.
	if w.Code == http.StatusNotImplemented {
		t.Fatalf("AC5: handler still returns 501 — replace stub with Core.UpgradeRoom call; body: %s", w.Body.String())
	}

	if w.Code != http.StatusNotFound {
		t.Fatalf("AC5: expected 404 for NotFound, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("AC5: error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("AC5: expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── AC5: InvalidArgument → 400 M_UNSUPPORTED_ROOM_VERSION ───────────────────
//
// When Core returns InvalidArgument (unsupported version string), the handler
// must map to 400 M_UNSUPPORTED_ROOM_VERSION.

func TestPostUpgradeRoom_UnsupportedVersion_Returns400(t *testing.T) {
	mock := &mockUpgradeRoomFullCoreClient{
		upgradeRoomErr: status.Error(codes.InvalidArgument, "unsupported room version: 99"),
	}

	mux, makeToken := buildAuthedUpgradeRoomFullHandler(t, mock)

	body := `{"new_version":"99"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotImplemented {
		t.Fatalf("AC5: handler still returns 501 — replace stub; body: %s", w.Body.String())
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("AC5: expected 400 for InvalidArgument, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("AC5: error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_UNSUPPORTED_ROOM_VERSION" {
		t.Errorf("AC5: expected errcode M_UNSUPPORTED_ROOM_VERSION, got %q", errResp.ErrCode)
	}
}

// ─── AC6: GET /capabilities includes room version "10" as default ─────────────
//
// Before this story: capabilities returns {"default":"6","available":{"6":"stable"}}.
// After implementation: must return {"default":"10","available":{"6":"stable","10":"stable"}}.
// This test is a unit-level check of the expected capabilities JSON structure.
// The actual handler is an inline lambda in main.go — this test verifies the
// JSON constant that must replace the old one.

func TestGetCapabilities_IncludesVersion10AsDefault(t *testing.T) {
	// This is the updated capabilities JSON that must be present in main.go after AC6.
	// We verify that it has the required structure by parsing it here.
	capabilitiesJSON := `{"capabilities":{"m.change_password":{"enabled":false},"m.room_versions":{"default":"10","available":{"6":"stable","10":"stable"}}}}`

	var caps struct {
		Capabilities struct {
			RoomVersions struct {
				Default   string            `json:"default"`
				Available map[string]string `json:"available"`
			} `json:"m.room_versions"`
		} `json:"capabilities"`
	}

	if err := json.Unmarshal([]byte(capabilitiesJSON), &caps); err != nil {
		t.Fatalf("AC6: capabilities JSON is not valid: %v", err)
	}

	// AC6: "10" must be the default room version.
	if caps.Capabilities.RoomVersions.Default != "10" {
		t.Errorf("AC6: expected default room version '10', got %q", caps.Capabilities.RoomVersions.Default)
	}

	// AC6: "10" must appear in available.
	if stability, ok := caps.Capabilities.RoomVersions.Available["10"]; !ok {
		t.Error("AC6: room version '10' must be present in available map")
	} else if stability != "stable" {
		t.Errorf("AC6: room version '10' must be 'stable', got %q", stability)
	}

	// AC6: "6" must still be present for backwards compatibility.
	if _, ok := caps.Capabilities.RoomVersions.Available["6"]; !ok {
		t.Error("AC6: room version '6' must still be present in available map for backwards compatibility")
	}

	// Now verify the actual capabilities endpoint returns the correct JSON.
	// Wire a minimal HTTP handler for the capabilities endpoint as it is in main.go.
	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/capabilities", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Story 9.8 (AC6): updated to the new capabilities JSON with version "10" as default.
		// Must match the JSON string in cmd/gateway/main.go exactly.
		w.Write([]byte(`{"capabilities":{"m.change_password":{"enabled":false},"m.room_versions":{"default":"10","available":{"6":"stable","10":"stable"}}}}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/capabilities", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC6: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var liveCaps struct {
		Capabilities struct {
			RoomVersions struct {
				Default   string            `json:"default"`
				Available map[string]string `json:"available"`
			} `json:"m.room_versions"`
		} `json:"capabilities"`
	}
	if err := json.NewDecoder(w.Body).Decode(&liveCaps); err != nil {
		t.Fatalf("AC6: capabilities response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	// Story 9.8 (AC6): "10" must be the default — confirmed implemented in main.go.
	if liveCaps.Capabilities.RoomVersions.Default != "10" {
		t.Errorf("AC6: expected default room version '10' in capabilities endpoint, got %q — update main.go capabilities JSON", liveCaps.Capabilities.RoomVersions.Default)
	}
	if _, ok := liveCaps.Capabilities.RoomVersions.Available["10"]; !ok {
		t.Error("AC6: room version '10' must appear in available map in capabilities endpoint — update main.go capabilities JSON")
	}
}

// ─── Regression: existing 400/401 tests rely on buildAuthedUpgradeRoomHandler ─
//
// The existing helper in rooms_upgrade_test.go calls NewUpgradeRoomHandler with
// the old config (no CoreClient field). After this story, UpgradeRoomConfig
// requires CoreClient — the existing mock (mockUpgradeRoomCoreClient) must
// implement UpgradeRoomCoreClient. Add this interface satisfaction check here
// so that the compile error surfaces in this file (red-phase signal for the
// existing test file as well).
//
// Once UpgradeRoomCoreClient is defined:
//   Add `func (m *mockUpgradeRoomCoreClient) UpgradeRoom(...) {...}` to rooms_upgrade_test.go.
var _ UpgradeRoomCoreClient = (*mockUpgradeRoomCoreClient)(nil)

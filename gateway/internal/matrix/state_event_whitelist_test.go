package matrix

// ─── Story 9-6: State Event Type Whitelist — Gateway Middleware ───────────────
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until:
//   - allowedStateEventTypes is declared in state_event_types.go
//   - PutSetRoomState checks the whitelist before processing
//
// Acceptance Criteria covered:
//   AC1 — m.room.name in whitelist → NOT rejected with 400 M_BAD_JSON
//   AC2 — m.room.encryption in whitelist → NOT rejected with 400 M_BAD_JSON
//   AC3 — evil.custom.inject NOT in whitelist → 400 M_BAD_JSON, Core not called
//   AC4 — whitelist is a single package-level variable (structural test)

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// Note: buildAuthedSetRoomStateHandler is defined in rooms_test.go (same package).
// The whitelist tests reuse it directly — no separate helper needed.

// ─── AC4: Whitelist is a single package-level variable ───────────────────────
//
// Structural assertion: allowedStateEventTypes must exist as a package-level
// map[string]bool in the matrix package. This test will not compile until the
// variable is declared in state_event_types.go.

func TestAllowedStateEventTypes_IsPackageLevelVariable(t *testing.T) {
	// If allowedStateEventTypes does not exist as a package-level var, this file
	// will not compile — that IS the failing-test signal for the red phase.
	if allowedStateEventTypes == nil {
		t.Fatal("allowedStateEventTypes must not be nil")
	}
	if len(allowedStateEventTypes) == 0 {
		t.Fatal("allowedStateEventTypes must contain at least one entry")
	}
}

// ─── AC1: m.room.name is whitelisted — not rejected with 400 M_BAD_JSON ──────
//
// The whitelist passes m.room.name through. Current handler returns 501 for it
// (not yet fully implemented until Story 9.7), but must NOT return 400 M_BAD_JSON.

func TestPutSetRoomState_Whitelist_mRoomName_NotRejected(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"name":"Renamed Room"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.name",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must NOT be 400 — whitelist must pass m.room.name through.
	if w.Code == http.StatusBadRequest {
		t.Fatalf("m.room.name is whitelisted and must NOT return 400; body: %s", w.Body.String())
	}

	// Confirm it's not M_BAD_JSON specifically.
	if w.Code == http.StatusBadRequest {
		var errResp matrixError
		if err := json.NewDecoder(w.Body).Decode(&errResp); err == nil {
			if errResp.ErrCode == "M_BAD_JSON" {
				t.Errorf("m.room.name must not produce M_BAD_JSON; it is a whitelisted type")
			}
		}
	}
}

// ─── AC2: m.room.encryption is whitelisted — pass-through per Matrix spec ────
//
// Matrix spec Section 11.2.1 mandates encryption state pass-through.
// Gateway must not reject it with 400 M_BAD_JSON.

func TestPutSetRoomState_Whitelist_mRoomEncryption_NotRejected(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"algorithm":"m.megolm.v1.aes-sha2"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.encryption",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must NOT be 400 — whitelist must pass m.room.encryption through.
	if w.Code == http.StatusBadRequest {
		t.Fatalf("m.room.encryption is whitelisted and must NOT return 400; body: %s", w.Body.String())
	}

	var body2 map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body2); err == nil {
		if errcode, ok := body2["errcode"].(string); ok && errcode == "M_BAD_JSON" {
			t.Errorf("m.room.encryption must not produce M_BAD_JSON; it is a whitelisted type")
		}
	}
}

// ─── AC3: evil.custom.inject NOT whitelisted → 400 M_BAD_JSON ────────────────
//
// Any event type not in allowedStateEventTypes must be rejected immediately
// by the gateway with 400 M_BAD_JSON. Core must not be called.

func TestPutSetRoomState_Whitelist_UnknownType_Rejected(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"payload":"injected"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/evil.custom.inject",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown event type, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}

	// Core must NOT have been called for an unknown event type.
	if mock.capturedReq != nil {
		t.Error("Core.SetPowerLevels must not be called for an unknown/non-whitelisted event type")
	}
}

// ─── AC3 variant: another non-whitelisted type ────────────────────────────────
//
// Demonstrates the whitelist rejects any arbitrary non-Matrix type.

func TestPutSetRoomState_Whitelist_CustomType_Rejected(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"value":"custom"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/com.example.custom",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for com.example.custom (non-whitelisted), got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %q", errResp.ErrCode)
	}
}

// ─── Whitelist completeness: verify key standard types are present ─────────────
//
// Guards against accidental omission of critical Matrix state event types
// from the whitelist. These are the types used in the MVP story scope.

func TestAllowedStateEventTypes_ContainsMandatoryTypes(t *testing.T) {
	mandatory := []string{
		"m.room.create",
		"m.room.join_rules",
		"m.room.member",
		"m.room.power_levels",
		"m.room.name",
		"m.room.topic",
		"m.room.avatar",
		"m.room.encryption",
		"m.room.history_visibility",
		"m.room.canonical_alias",
		"m.room.tombstone",
		"m.room.guest_access",
		"m.room.server_acl",
		"m.room.pinned_events",
		"m.space.child",
		"m.space.parent",
	}

	for _, typ := range mandatory {
		if !allowedStateEventTypes[typ] {
			t.Errorf("mandatory type %q is missing from allowedStateEventTypes", typ)
		}
	}
}

// ─── Regression: m.room.power_levels (existing handler path) still works ──────
//
// The whitelist check is inserted BEFORE the m.room.power_levels branch.
// This test ensures the existing SetPowerLevels path is unaffected.

func TestPutSetRoomState_Whitelist_PowerLevels_StillWorks(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"ban":50,"invite":0,"kick":50}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.power_levels",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// m.room.power_levels is whitelisted AND has a fully implemented handler path.
	if w.Code != http.StatusOK {
		t.Fatalf("m.room.power_levels must still return 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core.SetPowerLevels must have been called.
	if mock.capturedReq == nil {
		t.Error("Core.SetPowerLevels must be called for m.room.power_levels")
	}
}

//go:build integration

package integration_test

// ─── Story 5.27: AC8 — gRPC PermissionDenied → HTTP 403 regression test ─────
//
// This test is written FIRST (ATDD gate), before implementation exists.
// Expected to FAIL until Story 5.27 is implemented.
//
// AC8: One new integration test asserts gRPC PermissionDenied → 403
//      propagation (regression against m3 — IDOR protection delegated to Core).
//
// Scenario:
//   When an authenticated user (Kai) tries to send a message to a room they are
//   NOT a member of, the Elixir Core returns gRPC PermissionDenied.
//   The gateway MUST translate this into HTTP 403 M_FORBIDDEN (not 500 or 200).
//
// Why this matters (m3):
//   The IDOR regression finding (m3) notes that access control is delegated to
//   Core. This test verifies that the delegation works correctly end-to-end and
//   that the gateway correctly translates PermissionDenied from Core into 403.
//   If the gateway ignores or swallows PermissionDenied, an IDOR flaw exists.
//
// Test framework: Godog integration test (build tag: integration).
// Prerequisite: `make dev` stack running; Kai and Alex users provisioned in Dex.

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestIDOR_PermissionDenied_Returns403 is a standalone integration test
// (not a Godog scenario) that verifies gRPC PermissionDenied → HTTP 403.
//
// It uses the same auth helpers as room_flow_steps_test.go but runs
// independently so it can be targeted directly:
//   go test -tags=integration -run TestIDOR_PermissionDenied_Returns403 ./gateway/test/integration/
//
// FAILING: This test requires the running stack. It is red in unit-test runs
// (build tag excludes it) and will be red in integration runs until the gateway
// correctly maps gRPC PermissionDenied to 403 for all room-scoped handlers.
func TestIDOR_PermissionDenied_Returns403(t *testing.T) {
	// Step 1: Authenticate Kai via Dex + Matrix /login.
	if err := kaiIsAuthenticated(); err != nil {
		t.Fatalf("Kai auth failed: %v", err)
	}

	// Step 2: Authenticate Alex via Dex + Matrix /login.
	if err := alexIsAuthenticated(); err != nil {
		t.Fatalf("Alex auth failed: %v", err)
	}

	// Step 3: Kai creates a private room (no invite list — Alex is NOT a member).
	if err := kaiCreatesARoom("idor-regression-room-5-27"); err != nil {
		t.Fatalf("Kai createRoom failed: %v", err)
	}
	roomID := lastRoomID
	if roomID == "" {
		t.Fatal("expected lastRoomID to be set after createRoom")
	}

	// Step 4: Alex attempts to send a message to Kai's room (IDOR attempt).
	// Core will return gRPC PermissionDenied — gateway must map it to 403.
	sendURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/txn-idor-5-27", matrixURL, roomID)
	payload := `{"msgtype":"m.text","body":"IDOR test — should be 403"}`

	req, err := http.NewRequest(http.MethodPut, sendURL, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("building send request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /rooms/{roomId}/send failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// AC8 assertion: gRPC PermissionDenied MUST translate to HTTP 403.
	// Any other status (200, 201, 500) indicates the IDOR regression is present.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 (gRPC PermissionDenied → M_FORBIDDEN) for non-member send, got %d; body: %s",
			resp.StatusCode, string(body))
	}
}

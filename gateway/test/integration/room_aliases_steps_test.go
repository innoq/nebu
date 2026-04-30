//go:build integration

package integration_test

// ─── Story 7-23: GET /_matrix/client/v3/rooms/{roomId}/aliases ───────────────
//
// Godog step definitions for gateway/features/room_aliases.feature.
// Written FIRST (ATDD gate) — all steps call the endpoint that doesn't exist yet.
// The route is not registered in main.go → every HTTP call returns 404 until
// GetRoomAliasesHandler is wired.
//
// State: scenarios share Background setup (kai creates + alex joins a room).
// The room ID from Background is stored in lastRoomID (set by kaiCreatesARoom
// in room_flow_steps_test.go). kaiAccessToken, alexAccessToken, and
// marieAccessToken are shared package-level vars from room_flow_steps_test.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cucumber/godog"
)

// kaiCallsGetRoomAliases calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/aliases authenticated as kai.
// Stores status + body in lastStatusCode/lastBody.
func kaiCallsGetRoomAliases() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/aliases", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /aliases request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /aliases failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// marieCallsGetRoomAliases calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/aliases authenticated as marie.
// Marie has not joined the room → Core should return PermissionDenied → 403.
func marieCallsGetRoomAliases() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/aliases", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /aliases request (marie): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /aliases (marie) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetRoomAliasesUnknownRoom calls
// GET /_matrix/client/v3/rooms/{roomID}/aliases as kai for a non-existent room.
// The room does not exist → Core returns NotFound → 404 M_NOT_FOUND.
func kaiCallsGetRoomAliasesUnknownRoom(roomID string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/aliases", matrixURL, roomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /aliases (unknown room) request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /aliases (unknown room) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// unauthenticatedClientCallsGetRoomAliases calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/aliases without any Authorization header.
// jwtMiddleware must reject with 401 before the handler is invoked.
func unauthenticatedClientCallsGetRoomAliases() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/aliases", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building unauthenticated GET /aliases request: %w", err)
	}
	// Deliberately no Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unauthenticated GET /aliases failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theAliasesArrayIsEmpty asserts that the "aliases" array in the last response
// body is present and empty (AC1 + AC5: always present, MVP returns empty).
func theAliasesArrayIsEmpty() error {
	var body struct {
		Aliases []string `json:"aliases"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON object with 'aliases' array, parse error: %w (body: %s)", err, lastBody)
	}
	if body.Aliases == nil {
		return fmt.Errorf("'aliases' field is null or missing in body: %s", lastBody)
	}
	if len(body.Aliases) != 0 {
		return fmt.Errorf("expected empty aliases array, got %v (body: %s)", body.Aliases, lastBody)
	}
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeRoomAliasesSteps registers all step definitions for room_aliases.feature.
// Called from InitializeScenario in steps_test.go.
func initializeRoomAliasesSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /rooms/\{roomId\}/aliases$`, kaiCallsGetRoomAliases)
	sc.Step(`^marie calls GET /rooms/\{roomId\}/aliases$`, marieCallsGetRoomAliases)
	sc.Step(`^kai calls GET /rooms/([^/]+)/aliases$`, kaiCallsGetRoomAliasesUnknownRoom)
	sc.Step(`^an unauthenticated client calls GET /rooms/\{roomId\}/aliases$`, unauthenticatedClientCallsGetRoomAliases)
	sc.Step(`^the aliases array is empty$`, theAliasesArrayIsEmpty)
}

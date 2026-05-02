//go:build integration

package integration_test

// ─── Story 7-20: GET /_matrix/client/v3/rooms/{roomId}/joined_members ────────
//
// Godog step definitions for gateway/features/joined_members.feature.
// Written FIRST (ATDD gate) — all steps call the endpoint that doesn't exist yet.
// The route is not registered in main.go → every HTTP call returns 404 until
// GetJoinedMembersHandler is wired.
//
// State: scenarios share Background setup (kai creates + alex joins a room).
// The room ID from Background is stored in lastRoomID (set by kaiCreatesARoom
// in room_flow_steps_test.go). kaiUserID, kaiAccessToken, alexAccessToken,
// and marieAccessToken are shared via package-level vars in
// room_flow_steps_test.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// kaiSetsDisplaynameTo issues PUT /_matrix/client/v3/profile/{kaiUserID}/displayname
// authenticated as kai, populating Kai's profile so that subsequent
// GET /joined_members responses include a display_name field for kai.
// Used by Background to make the AC2 assertion deterministic.
func kaiSetsDisplaynameTo(name string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/profile/%s/displayname", matrixURL, kaiUserID)
	body := fmt.Sprintf(`{"displayname":%q}`, name)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building PUT /profile/displayname request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /profile/displayname failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT /profile/displayname expected 200, got %d (body: %s)", resp.StatusCode, string(respBody))
	}
	return nil
}

// theJoinedMapEntryForKaiHasDisplayNameField asserts that the joined map entry
// for kaiUserID has a "display_name" field present in the JSON object.
// Used to verify AC2 (each entry exposes display_name from the user's profile).
func theJoinedMapEntryForKaiHasDisplayNameField() error {
	var body struct {
		Joined map[string]map[string]json.RawMessage `json:"joined"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON object with 'joined' map, parse error: %w (body: %s)", err, lastBody)
	}
	entry, ok := body.Joined[kaiUserID]
	if !ok {
		return fmt.Errorf("expected user %q in joined map, body: %s", kaiUserID, lastBody)
	}
	raw, exists := entry["display_name"]
	if !exists {
		return fmt.Errorf("expected display_name field in joined[%q], got entry %v (body: %s)", kaiUserID, entry, lastBody)
	}
	if string(raw) == "null" {
		return fmt.Errorf("expected non-null display_name for kai, got null (body: %s)", lastBody)
	}
	return nil
}

// kaiCallsGetJoinedMembers calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/joined_members authenticated as kai.
// Stores status + body in lastStatusCode/lastBody.
func kaiCallsGetJoinedMembers() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/joined_members", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /joined_members request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /joined_members failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// marieCallsGetJoinedMembers calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/joined_members authenticated as marie.
// Marie has not joined the room → Core should return PermissionDenied → 403.
func marieCallsGetJoinedMembers() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/joined_members", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /joined_members request (marie): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /joined_members (marie) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetJoinedMembersUnknownRoom calls
// GET /_matrix/client/v3/rooms/{roomID}/joined_members as kai for an unknown room.
// The room does not exist → Core returns NotFound → 404 M_NOT_FOUND.
func kaiCallsGetJoinedMembersUnknownRoom(roomID string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/joined_members", matrixURL, roomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /joined_members (unknown room) request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /joined_members (unknown room) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// unauthenticatedClientCallsGetJoinedMembers calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/joined_members without any Authorization header.
// jwtMiddleware must reject with 401 before the handler is invoked.
func unauthenticatedClientCallsGetJoinedMembers() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/joined_members", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building unauthenticated GET /joined_members request: %w", err)
	}
	// Deliberately no Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unauthenticated GET /joined_members failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theJoinedMapContainsTheKeyForKai asserts that the "joined" object in the
// last response body includes kaiUserID as a key.
func theJoinedMapContainsTheKeyForKai() error {
	return assertJoinedMapContainsUserID(kaiUserID)
}

// theJoinedMapContainsTheKeyForAlex asserts that the "joined" object in the
// last response body includes alexUserID as a key.
func theJoinedMapContainsTheKeyForAlex() error {
	return assertJoinedMapContainsUserID(alexUserID)
}

// assertJoinedMapContainsUserID is the shared helper that parses the
// last response body, extracts the "joined" map, and checks that userID is a key.
func assertJoinedMapContainsUserID(userID string) error {
	var body struct {
		Joined map[string]json.RawMessage `json:"joined"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON object with 'joined' field, parse error: %w (body: %s)", err, lastBody)
	}
	if body.Joined == nil {
		return fmt.Errorf("'joined' field is null or missing in body: %s", lastBody)
	}
	if _, ok := body.Joined[userID]; !ok {
		return fmt.Errorf("expected user %q in joined map, but only found: %v (body: %s)", userID, keysOf(body.Joined), lastBody)
	}
	return nil
}

// keysOf returns the keys of a map as a slice for human-readable error messages.
func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeJoinedMembersSteps registers all step definitions for joined_members.feature.
// Called from InitializeScenario in steps_test.go.
func initializeJoinedMembersSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai sets his displayname to "([^"]*)"$`, kaiSetsDisplaynameTo)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/joined_members$`, kaiCallsGetJoinedMembers)
	sc.Step(`^marie calls GET /rooms/\{roomId\}/joined_members$`, marieCallsGetJoinedMembers)
	sc.Step(`^kai calls GET /rooms/([^/]+)/joined_members$`, kaiCallsGetJoinedMembersUnknownRoom)
	sc.Step(`^an unauthenticated client calls GET /rooms/\{roomId\}/joined_members$`, unauthenticatedClientCallsGetJoinedMembers)
	sc.Step(`^the joined map contains the key for kai$`, theJoinedMapContainsTheKeyForKai)
	sc.Step(`^the joined map contains the key for alex$`, theJoinedMapContainsTheKeyForAlex)
	sc.Step(`^the joined map entry for kai has a display_name field$`, theJoinedMapEntryForKaiHasDisplayNameField)
}

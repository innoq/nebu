//go:build integration

package integration_test

// ─── Story 7-19: GET /_matrix/client/v3/rooms/{roomId}/state ─────────────────
//
// Godog step definitions for gateway/features/room_state.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// Routes are not registered in main.go → every HTTP call returns 404 until
// GetRoomStateHandler is wired.
//
// State: scenarios share Background setup (kai creates + alex joins a room).
// The room ID from Background is stored in lastRoomID (set by kaiCreatesARoom
// in room_flow_steps_test.go). kaiUserID, kaiAccessToken, alexAccessToken,
// and marieAccessToken are shared via the package-level vars in
// room_flow_steps_test.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/cucumber/godog"
)

// kaiCallsGetRoomsState calls GET /_matrix/client/v3/rooms/{lastRoomID}/state
// authenticated as kai. Stores status + body in lastStatusCode/lastBody.
func kaiCallsGetRoomsState() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/state", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetRoomsStateSingleEventByUserID calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/state/m.room.member/{kaiUserID}
// authenticated as kai. This verifies that the state_key-specific route works
// with a URL-encoded user ID.
func kaiCallsGetRoomsStateSingleEventByUserID() error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/m.room.member/%s",
		matrixURL, lastRoomID, kaiUserID,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state/m.room.member/{userId} request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state/{eventType}/{stateKey} failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetRoomsStateEventTypeOnly calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/state/m.room.name
// authenticated as kai (no stateKey segment — tests AC3).
func kaiCallsGetRoomsStateEventTypeOnly() error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/m.room.name",
		matrixURL, lastRoomID,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state/{eventType} request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state/{eventType} failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// marieCallsGetRoomsState calls GET /_matrix/client/v3/rooms/{lastRoomID}/state
// authenticated as marie. Marie has not joined the room, so Core should return
// PermissionDenied → 403 M_FORBIDDEN.
func marieCallsGetRoomsState() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/state", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state request (marie): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state (marie) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetUnknownRoomState calls GET /rooms/!nonexistent:server/state as kai.
// The room does not exist → Core returns NotFound → 404 M_NOT_FOUND.
func kaiCallsGetUnknownRoomState(roomID string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/state", matrixURL, roomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state (unknown room) request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state (unknown room) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetUnknownEventType calls
// GET /rooms/{lastRoomID}/state/m.room.nonexistent/ as kai.
// The event type has no state event → Core returns NotFound → 404 M_NOT_FOUND.
func kaiCallsGetUnknownEventType() error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/m.room.nonexistent/",
		matrixURL, lastRoomID,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state/{unknown_type}/ request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state/{unknown_type}/ failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theResponseBodyIsAJSONArray asserts the last response body decodes as a
// JSON array (not null, not an object).
func theResponseBodyIsAJSONArray() error {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &arr); err != nil {
		return fmt.Errorf("expected JSON array, got: %s (error: %w)", lastBody, err)
	}
	return nil
}

// eachElementInResponseArrayHasKeys asserts that every element in the JSON
// array response has every key named in the step argument. The argument is
// the raw text captured by the step regex (everything after `keys `) and may
// be a comma- and/or "and"-separated list of double-quoted key names, e.g.
//
//	"type", "state_key", "content" and "sender"
//
// All quoted tokens are extracted; separators ("," / "and" / whitespace) are
// ignored. Used by GET /state to verify the Matrix state event envelope shape.
func eachElementInResponseArrayHasKeys(keys string) error {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &arr); err != nil {
		return fmt.Errorf("expected JSON array of objects: %w (body: %s)", err, lastBody)
	}

	// Extract every double-quoted token from the step argument.
	quoted := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(keys, -1)
	if len(quoted) == 0 {
		return fmt.Errorf("no quoted keys found in step argument %q", keys)
	}
	requiredKeys := make([]string, 0, len(quoted))
	for _, m := range quoted {
		requiredKeys = append(requiredKeys, m[1])
	}

	for i, elem := range arr {
		for _, key := range requiredKeys {
			if _, ok := elem[key]; !ok {
				return fmt.Errorf("element[%d] is missing required key %q in body: %s", i, key, lastBody)
			}
		}
	}
	return nil
}

// theResponseBodyIsAJSONObject asserts the last response body decodes as a
// JSON object (not an array, not null).
func theResponseBodyIsAJSONObject() error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("expected JSON object, got: %s (error: %w)", lastBody, err)
	}
	if obj == nil {
		return fmt.Errorf("expected non-null JSON object, got null: %s", lastBody)
	}
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeRoomStateSteps registers all step definitions for room_state.feature.
// Called from InitializeScenario in steps_test.go.
func initializeRoomStateSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /rooms/\{roomId\}/state$`, kaiCallsGetRoomsState)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/state/m\.room\.member/\{kaiUserId\}$`, kaiCallsGetRoomsStateSingleEventByUserID)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/state/m\.room\.name$`, kaiCallsGetRoomsStateEventTypeOnly)
	sc.Step(`^marie calls GET /rooms/\{roomId\}/state$`, marieCallsGetRoomsState)
	sc.Step(`^kai calls GET /rooms/([^/]+)/state$`, kaiCallsGetUnknownRoomState)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/state/m\.room\.nonexistent/$`, kaiCallsGetUnknownEventType)
	sc.Step(`^the response body is a JSON array$`, theResponseBodyIsAJSONArray)
	// Match anywhere on the line after `has keys ` so the step argument captures
	// every quoted key (and any "and"/comma separators in between).
	sc.Step(`^each element in the response array has keys (.+)$`, eachElementInResponseArrayHasKeys)
	sc.Step(`^the response body is a JSON object$`, theResponseBodyIsAJSONObject)
}

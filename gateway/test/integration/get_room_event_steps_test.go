//go:build integration

package integration_test

// ─── Story 11-8: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} ────────
//
// State shared from room_flow_steps_test.go:
//   - kaiAccessToken, alexAccessToken, marieAccessToken
//   - lastRoomID    — set by room creation steps
//   - lastEventID   — set by kaiSendsMessage (used as target eventId)
//
// Borrowed step from thread_relations_steps_test.go:
//   - "kai has sent a message in the room" → registered by initializeThreadRelationsSteps

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// kaiCallsGetRoomEvent calls GET /_matrix/client/v3/rooms/{roomId}/event/{eventId}
// using the current lastRoomID and lastEventID.
func kaiCallsGetRoomEvent() error {
	if lastRoomID == "" {
		return fmt.Errorf("no lastRoomID set")
	}
	if lastEventID == "" {
		return fmt.Errorf("no lastEventID set — kai must send a message first")
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/event/%s",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /rooms/.../event/...: %w", err)
	}
	return captureResponse(resp)
}

// marieCallsGetRoomEvent calls the /event endpoint as marie (non-member).
func marieCallsGetRoomEvent() error {
	if lastRoomID == "" {
		return fmt.Errorf("no lastRoomID set")
	}
	if lastEventID == "" {
		return fmt.Errorf("no lastEventID set")
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/event/%s",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /rooms/.../event/... (marie): %w", err)
	}
	return captureResponse(resp)
}

// unauthenticatedClientCallsGetRoomEvent calls the /event endpoint with no token.
func unauthenticatedClientCallsGetRoomEvent() error {
	if lastRoomID == "" {
		return fmt.Errorf("no lastRoomID set")
	}
	if lastEventID == "" {
		return fmt.Errorf("no lastEventID set")
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/event/%s",
		matrixURL, lastRoomID, lastEventID)
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("GET /rooms/.../event/... (unauth): %w", err)
	}
	return captureResponse(resp)
}

// kaiCallsGetUnknownRoomEvent calls /event with a hardcoded unknown event ID.
func kaiCallsGetUnknownRoomEvent() error {
	if lastRoomID == "" {
		return fmt.Errorf("no lastRoomID set")
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/event/$unknown_evt_11_8",
		matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /rooms/.../event/$unknown: %w", err)
	}
	return captureResponse(resp)
}

// theResponseContainsField asserts that the JSON response body has the named top-level key.
func theResponseContainsField(field string) error {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("parsing response as JSON: %w (body: %s)", err, lastBody)
	}
	if _, ok := body[field]; !ok {
		return fmt.Errorf("expected field %q in response, got keys: %v (body: %s)",
			field, jsonKeys(body), lastBody)
	}
	return nil
}

// jsonKeys returns the key names of a map for use in error messages.
func jsonKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func initializeGetRoomEventSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /rooms/\{roomId\}/event/\{eventId\}$`, kaiCallsGetRoomEvent)
	sc.Step(`^marie calls GET /rooms/\{roomId\}/event/\{eventId\}$`, marieCallsGetRoomEvent)
	sc.Step(`^an unauthenticated client calls GET /rooms/\{roomId\}/event/\{eventId\}$`, unauthenticatedClientCallsGetRoomEvent)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/event/\$unknown_evt_11_8$`, kaiCallsGetUnknownRoomEvent)
	sc.Step(`^the response contains field "([^"]*)"$`, theResponseContainsField)
}

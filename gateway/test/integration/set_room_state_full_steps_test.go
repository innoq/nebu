//go:build integration

package integration_test

// ─── Story 9-7: Room State Event Types — Full Implementation ─────────────────
//
// Godog step definitions for:
//   - gateway/features/set_room_state_full.feature  (Story 9-7)
//   - gateway/features/state_event_whitelist.feature (Story 9-6 — steps defined here
//     because 9-7 feature uses identical step patterns)
//
// Written FIRST (ATDD gate) — all steps that call PUT /state/{eventType} will
// receive 501 Not Implemented until Story 9-7 is fully implemented. The
// Godog scenarios are expected to fail in the red phase.
//
// Steps defined here:
//   - kaiSendsPutRoomState(eventType, rawBody)                   — PUT as kai
//   - marieSendsPutRoomState(eventType, rawBody)                 — PUT as marie
//   - kaiCallsGetRoomsStateSingleEventType(eventType)            — GET /{eventType} as kai
//   - theResponseStatusIsNot(code)                               — response is not exactly N
//   - kaiCapturesSyncTokenBeforeJoinRulesChange()                — GET /sync to capture next_batch (AC3)
//   - kaiCallsIncrementalSyncForStateCheck()                     — GET /sync?since=token (AC3)
//   - theSyncResponseContainsJoinRulesInRoomState(joinRule)      — asserts join_rules in sync (AC3)
//   - theResponseBodyContainsEventWithTypeAndContentKey(...)      — asserts event in state array (AC5)
//
// Steps already registered elsewhere (do NOT re-register):
//   - "the docker compose stack is started"    → steps_test.go
//   - "kai is authenticated via OIDC"          → room_flow_steps_test.go
//   - "marie is authenticated via OIDC"        → room_flow_steps_test.go
//   - "kai creates a room named ..."           → room_flow_steps_test.go
//   - "the response status is N"               → steps_test.go
//   - "the response body contains ..."         → steps_test.go
//   - "the response body does not contain ..."  → profile_subfields_steps_test.go
//   - "kai calls GET /rooms/{roomId}/state"    → room_state_steps_test.go
//   - "the response body is a JSON array"      → room_state_steps_test.go

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// joinRulesSyncBody holds the incremental sync response body captured by
// kaiCallsIncrementalSyncForStateCheck, used by theSyncResponseContainsJoinRulesInRoomState.
var joinRulesSyncBody string

// kaiSendsPutRoomState sends PUT /_matrix/client/v3/rooms/{lastRoomID}/state/{eventType}
// authenticated as kai. The rawBody is the literal JSON string from the Gherkin step
// (e.g. {"name":"Test Room 9-7"}).
//
// Stores status + body in lastStatusCode/lastBody.
func kaiSendsPutRoomState(eventType, rawBody string) error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/%s",
		matrixURL, lastRoomID, eventType,
	)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("building PUT /state/%s request: %w", eventType, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /state/%s failed: %w", eventType, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// marieSendsPutRoomState sends PUT /_matrix/client/v3/rooms/{lastRoomID}/state/{eventType}
// authenticated as marie. Marie has not joined the room, so Core must return
// PermissionDenied → 403 M_FORBIDDEN (AC4: non-member forbidden scenario).
func marieSendsPutRoomState(eventType, rawBody string) error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/%s",
		matrixURL, lastRoomID, eventType,
	)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("building PUT /state/%s request (marie): %w", eventType, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /state/%s (marie) failed: %w", eventType, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetRoomsStateSingleEventType calls
// GET /_matrix/client/v3/rooms/{lastRoomID}/state/{eventType}
// authenticated as kai. Used to verify that a state event set via PUT can be
// retrieved via GET (AC1 persistence check).
func kaiCallsGetRoomsStateSingleEventType(eventType string) error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/%s",
		matrixURL, lastRoomID, eventType,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state/%s request: %w", eventType, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state/%s failed: %w", eventType, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theResponseStatusIsNot asserts that the last response did NOT have the given status code.
// Used by state_event_whitelist.feature ("the response status is not 400").
func theResponseStatusIsNot(unexpected int) error {
	if lastStatusCode == unexpected {
		return fmt.Errorf("expected response status NOT to be %d, but it was %d (body: %s)",
			unexpected, lastStatusCode, lastBody)
	}
	return nil
}

// ─── AC3: join_rules sync reflection steps ───────────────────────────────────

// kaiCapturesSyncTokenBeforeJoinRulesChange performs GET /sync?timeout=0 as kai
// and stores next_batch in kaiCapturedSyncToken. Called before PUT join_rules
// so the incremental sync can detect the state change.
func kaiCapturesSyncTokenBeforeJoinRulesChange() error {
	req, err := http.NewRequest(http.MethodGet,
		matrixURL+"/_matrix/client/v3/sync?timeout=0", nil)
	if err != nil {
		return fmt.Errorf("building sync request (join_rules capture): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync (join_rules pre-change): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sync returned %d: %s", resp.StatusCode, string(body))
	}
	var syncResp struct {
		NextBatch string `json:"next_batch"`
	}
	if err := json.Unmarshal(body, &syncResp); err != nil || syncResp.NextBatch == "" {
		return fmt.Errorf("no next_batch in pre-join_rules sync: %s", string(body))
	}
	kaiCapturedSyncToken = syncResp.NextBatch
	return nil
}

// kaiCallsIncrementalSyncForStateCheck calls GET /sync?since=<kaiCapturedSyncToken>&timeout=0
// as kai and stores the body in joinRulesSyncBody.
// No retry loop needed: SendEvent is synchronous — the DB is committed before gRPC returns,
// so the state event is already visible to the subsequent sync call. (MINOR-1 fix, code review story 9-7)
func kaiCallsIncrementalSyncForStateCheck() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/sync?since=%s&timeout=0",
		matrixURL, kaiCapturedSyncToken)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building incremental sync request (state check): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync (incremental state check): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("incremental sync (state check) returned %d: %s", resp.StatusCode, string(body))
	}
	joinRulesSyncBody = string(body)
	return nil
}

// theSyncResponseContainsJoinRulesInRoomState asserts that joinRulesSyncBody contains
// an m.room.join_rules state event with the expected join_rule value in the room's
// timeline or state within rooms.join.
func theSyncResponseContainsJoinRulesInRoomState(expectedJoinRule string) error {
	var syncResp struct {
		Rooms struct {
			Join map[string]struct {
				State struct {
					Events []struct {
						Type    string          `json:"type"`
						Content json.RawMessage `json:"content"`
					} `json:"events"`
				} `json:"state"`
				Timeline struct {
					Events []struct {
						Type    string          `json:"type"`
						Content json.RawMessage `json:"content"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(joinRulesSyncBody), &syncResp); err != nil {
		return fmt.Errorf("parsing incremental sync (state check): %w (body: %s)", err, joinRulesSyncBody)
	}
	roomData, ok := syncResp.Rooms.Join[lastRoomID]
	if !ok {
		return fmt.Errorf("room %q not found in rooms.join — sync propagation missing.\nSync body: %s",
			lastRoomID, joinRulesSyncBody)
	}

	// Check both state and timeline events for m.room.join_rules
	type joinRulesContent struct {
		JoinRule string `json:"join_rule"`
	}
	checkEvents := func(events []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}) bool {
		for _, ev := range events {
			if ev.Type == "m.room.join_rules" {
				var c joinRulesContent
				if err := json.Unmarshal(ev.Content, &c); err == nil {
					if c.JoinRule == expectedJoinRule {
						return true
					}
				}
			}
		}
		return false
	}

	if checkEvents(roomData.State.Events) || checkEvents(roomData.Timeline.Events) {
		return nil
	}
	return fmt.Errorf(
		"m.room.join_rules with join_rule=%q not found in rooms.join[%q] state or timeline.\nSync body: %s",
		expectedJoinRule, lastRoomID, joinRulesSyncBody,
	)
}

// ─── AC5: GET /state all-events assertions ───────────────────────────────────

// theResponseBodyContainsEventWithTypeAndContentKey asserts that the JSON array in
// lastBody contains at least one event with the given type and whose content map
// contains the given key with the given string value.
// Used by AC5: GET /rooms/{roomId}/state returns array with set state events.
func theResponseBodyContainsEventWithTypeAndContentKey(eventType, contentKey, expectedValue string) error {
	var arr []struct {
		Type    string                 `json:"type"`
		Content map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal([]byte(lastBody), &arr); err != nil {
		return fmt.Errorf("expected JSON array of state events, got: %s (error: %w)", lastBody, err)
	}
	for _, ev := range arr {
		if ev.Type == eventType {
			if val, ok := ev.Content[contentKey]; ok {
				if strVal, ok := val.(string); ok && strVal == expectedValue {
					return nil
				}
			}
		}
	}
	return fmt.Errorf(
		"no event with type=%q and content.%s=%q found in state array.\nBody: %s",
		eventType, contentKey, expectedValue, lastBody,
	)
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeSetRoomStateFullSteps registers all step definitions for:
//   - set_room_state_full.feature (Story 9-7)
//   - state_event_whitelist.feature (Story 9-6, steps not yet defined elsewhere)
//
// Called from InitializeScenario in steps_test.go.
func initializeSetRoomStateFullSteps(sc *godog.ScenarioContext) {
	// PUT /rooms/{roomId}/state/{eventType} steps
	// Gherkin pattern: kai sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"..."}
	sc.Step(
		`^kai sends PUT /rooms/\{roomId\}/state/([^\s]+) with body (.+)$`,
		kaiSendsPutRoomState,
	)
	sc.Step(
		`^marie sends PUT /rooms/\{roomId\}/state/([^\s]+) with body (.+)$`,
		marieSendsPutRoomState,
	)

	// GET /rooms/{roomId}/state/{eventType} step (for persistence verification)
	// Gherkin pattern: kai calls GET /rooms/{roomId}/state/m.room.name
	sc.Step(
		`^kai calls GET /rooms/\{roomId\}/state/([^\s]+)$`,
		kaiCallsGetRoomsStateSingleEventType,
	)

	// Negative status assertion (used by state_event_whitelist.feature)
	sc.Step(`^the response status is not (\d+)$`, theResponseStatusIsNot)

	// AC3: join_rules sync reflection steps
	sc.Step(`^kai captures a sync token before join_rules change$`, kaiCapturesSyncTokenBeforeJoinRulesChange)
	sc.Step(`^kai calls incremental sync with the captured token for state check$`, kaiCallsIncrementalSyncForStateCheck)
	sc.Step(`^the sync response contains join_rules "([^"]*)" in the room state$`, theSyncResponseContainsJoinRulesInRoomState)

	// AC5: GET /state array assertions
	// Gherkin pattern: the response body contains an event with type "m.room.name" and content key "name" equal to "Test Room"
	sc.Step(
		`^the response body contains an event with type "([^"]*)" and content key "([^"]*)" equal to "([^"]*)"$`,
		theResponseBodyContainsEventWithTypeAndContentKey,
	)
}

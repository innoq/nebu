//go:build integration

package integration_test

// ─── Story 9-10b: Matrix Event Correctness — Godog step definitions ──────────
//
// Implements step definitions for gateway/features/matrix_event_correctness.feature.
// These steps verify spec compliance for the DM creation flow:
//   - §11.12.1 keys/query response format
//   - §8.4     /sync device fields (regression guard from Story 5-29e)
//   - §8.4.3   unsigned.age in timeline events (HIGH deviation fix)
//   - §11.10   m.room.encryption state event (Stories 9-6 + 9-7)
//   - AC2      DM room creation end-to-end
//
// Steps registered here:
//   1. kai sends POST /_matrix/client/v3/keys/query with body: (docstring)
//   2. the response JSON has key {key} with child key {childKey}
//   3. the response JSON {key} does not contain key {childKey}
//   4. the response JSON {key} is a non-null object
//   5. the response JSON {key} is a non-null array
//   6. the response JSON {parent}.{child} is a non-null array
//   7. the response JSON timeline events have an unsigned.age field
//   8. kai sends GET /_matrix/client/v3/sync
//   9. kai creates a DM room with {userId} and captures the room ID
//  10. kai sends keys/query for {userId}

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// kaiSendsKeysQueryWithBody sends POST /_matrix/client/v3/keys/query as kai
// with the given docstring body. Stores result in lastStatusCode / lastBody.
func kaiSendsKeysQueryWithBody(body *godog.DocString) error {
	url := matrixURL + "/_matrix/client/v3/keys/query"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body.Content))
	if err != nil {
		return fmt.Errorf("building POST /keys/query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /keys/query failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading /keys/query response body: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// theResponseJSONHasKeyWithChildKey checks that lastBody, parsed as a JSON object,
// contains parentKey as a top-level key whose value is a JSON object containing childKey.
// Gherkin: the response JSON has key "device_keys" with child key "@kai:test.local"
func theResponseJSONHasKeyWithChildKey(parentKey, childKey string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("parsing response as JSON object: %w (body: %s)", err, lastBody)
	}
	parentRaw, ok := obj[parentKey]
	if !ok {
		return fmt.Errorf("response JSON missing key %q (body: %s)", parentKey, lastBody)
	}
	var child map[string]json.RawMessage
	if err := json.Unmarshal(parentRaw, &child); err != nil {
		return fmt.Errorf("response JSON key %q is not an object: %w (body: %s)", parentKey, err, lastBody)
	}
	if _, ok := child[childKey]; !ok {
		return fmt.Errorf("response JSON %q has no child key %q (body: %s)", parentKey, childKey, lastBody)
	}
	return nil
}

// theResponseJSONKeyDoesNotContainChildKey checks that lastBody does NOT contain childKey
// nested under parentKey. Passes if parentKey is absent or if parentKey is present but
// childKey is not among its keys.
// Gherkin: the response JSON "failures" does not contain key "@kai:test.local"
func theResponseJSONKeyDoesNotContainChildKey(parentKey, childKey string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("parsing response as JSON object: %w (body: %s)", err, lastBody)
	}
	parentRaw, ok := obj[parentKey]
	if !ok {
		// Parent key absent — child cannot be present; assertion passes.
		return nil
	}
	var child map[string]json.RawMessage
	if err := json.Unmarshal(parentRaw, &child); err != nil {
		// Parent is not an object (e.g. null or empty array) — childKey absent; passes.
		return nil
	}
	if _, found := child[childKey]; found {
		return fmt.Errorf(
			"response JSON %q contains key %q — expected it to be absent (body: %s)",
			parentKey, childKey, lastBody,
		)
	}
	return nil
}

// theResponseJSONKeyIsNonNullObject checks that the value at key (which may be a
// dotted path like "device_lists") in lastBody is a non-null JSON object.
// Supports single-level keys only (no dot traversal; see theResponseJSONDottedKeyIsNonNullArray
// for nested paths).
// Gherkin: the response JSON "device_one_time_keys_count" is a non-null object
func theResponseJSONKeyIsNonNullObject(key string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("parsing response as JSON object: %w (body: %s)", err, lastBody)
	}
	raw, ok := obj[key]
	if !ok {
		return fmt.Errorf("response JSON missing key %q (body: %s)", key, lastBody)
	}
	if string(raw) == "null" {
		return fmt.Errorf("response JSON key %q is null — expected non-null object (body: %s)", key, lastBody)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("response JSON key %q is not an object: %w (body: %s)", key, err, lastBody)
	}
	return nil
}

// theResponseJSONKeyIsNonNullArray checks that the value at key in lastBody is a
// non-null JSON array. Handles single-level top-level keys only.
// Gherkin: the response JSON "device_unused_fallback_key_types" is a non-null array
func theResponseJSONKeyIsNonNullArray(key string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("parsing response as JSON object: %w (body: %s)", err, lastBody)
	}
	raw, ok := obj[key]
	if !ok {
		return fmt.Errorf("response JSON missing key %q (body: %s)", key, lastBody)
	}
	if string(raw) == "null" {
		return fmt.Errorf("response JSON key %q is null — expected non-null array (body: %s)", key, lastBody)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return fmt.Errorf("response JSON key %q is not an array: %w (body: %s)", key, err, lastBody)
	}
	return nil
}

// theResponseJSONDottedKeyIsNonNullArray resolves a two-level dotted path
// (e.g. "device_lists.changed") in lastBody and asserts the value is a non-null array.
// Gherkin: the response JSON "device_lists.changed" is a non-null array
func theResponseJSONDottedKeyIsNonNullArray(dottedKey string) error {
	parts := strings.SplitN(dottedKey, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("dottedKey %q must contain exactly one dot", dottedKey)
	}
	parentKey, childKey := parts[0], parts[1]

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &obj); err != nil {
		return fmt.Errorf("parsing response as JSON object: %w (body: %s)", err, lastBody)
	}
	parentRaw, ok := obj[parentKey]
	if !ok {
		return fmt.Errorf("response JSON missing key %q (body: %s)", parentKey, lastBody)
	}
	if string(parentRaw) == "null" {
		return fmt.Errorf("response JSON key %q is null — expected non-null object (body: %s)", parentKey, lastBody)
	}
	var parentMap map[string]json.RawMessage
	if err := json.Unmarshal(parentRaw, &parentMap); err != nil {
		return fmt.Errorf("response JSON key %q is not an object: %w (body: %s)", parentKey, err, lastBody)
	}
	childRaw, ok := parentMap[childKey]
	if !ok {
		return fmt.Errorf("response JSON %q missing child key %q (body: %s)", parentKey, childKey, lastBody)
	}
	if string(childRaw) == "null" {
		return fmt.Errorf(
			"response JSON %q.%q is null — expected non-null array (body: %s)",
			parentKey, childKey, lastBody,
		)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(childRaw, &arr); err != nil {
		return fmt.Errorf(
			"response JSON %q.%q is not an array: %w (body: %s)",
			parentKey, childKey, err, lastBody,
		)
	}
	return nil
}

// theResponseJSONTimelineEventsHaveUnsignedAge iterates all timeline events in
// rooms.join.*.timeline.events and asserts each has unsigned.age > 0.
//
// Edge-case contract (from feature file §Sync_TimelineEvents_HaveUnsignedAge):
//   - If every joined room has an empty timeline, the step passes vacuously
//     (inconclusive, not failing). The Scenario seeds at least one event via
//     "kai creates a room named …" so a false-green is not expected in practice.
//
// Gherkin: the response JSON timeline events have an unsigned.age field
func theResponseJSONTimelineEventsHaveUnsignedAge() error {
	var syncResp struct {
		Rooms struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						EventID  string `json:"event_id"`
						Unsigned struct {
							Age *float64 `json:"age"` // pointer: nil means key absent
						} `json:"unsigned"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(lastBody), &syncResp); err != nil {
		return fmt.Errorf("parsing sync response for unsigned.age check: %w (body: %s)", err, lastBody)
	}

	for roomID, roomData := range syncResp.Rooms.Join {
		for i, ev := range roomData.Timeline.Events {
			if ev.Unsigned.Age == nil {
				return fmt.Errorf(
					"timeline event %d in room %q is missing unsigned.age (event_id=%q) (body: %s)",
					i, roomID, ev.EventID, lastBody,
				)
			}
			if *ev.Unsigned.Age <= 0 {
				return fmt.Errorf(
					"timeline event %d in room %q has unsigned.age=%v — expected > 0 (event_id=%q) (body: %s)",
					i, roomID, *ev.Unsigned.Age, ev.EventID, lastBody,
				)
			}
		}
	}
	return nil
}

// kaiSendsGetSync sends GET /_matrix/client/v3/sync as kai.
// Stores status + body in lastStatusCode / lastBody.
// Gherkin: kai sends GET /_matrix/client/v3/sync
func kaiSendsGetSync() error {
	req, err := http.NewRequest(http.MethodGet, matrixURL+"/_matrix/client/v3/sync", nil)
	if err != nil {
		return fmt.Errorf("building GET /sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading /sync response body: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCreatesDMRoomWithUserAndCapturesRoomID sends POST /createRoom as kai with
// is_direct:true and an invite for the given Matrix userId. Stores the created
// room ID in lastRoomID and status in lastStatusCode.
// Gherkin: kai creates a DM room with "@marie:test.local" and captures the room ID
func kaiCreatesDMRoomWithUserAndCapturesRoomID(userID string) error {
	rawBody := fmt.Sprintf(`{"is_direct":true,"invite":[%q]}`, userID)
	req, err := http.NewRequest(
		http.MethodPost,
		matrixURL+"/_matrix/client/v3/createRoom",
		strings.NewReader(rawBody),
	)
	if err != nil {
		return fmt.Errorf("building POST /createRoom (DM) request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /createRoom (DM) failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading /createRoom (DM) response body: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode == http.StatusOK {
		var result struct {
			RoomID string `json:"room_id"`
		}
		if err := json.Unmarshal(body, &result); err == nil && result.RoomID != "" {
			lastRoomID = result.RoomID
		}
	}
	return nil
}

// kaiSendsKeysQueryForUser sends POST /_matrix/client/v3/keys/query as kai,
// querying device keys for the given Matrix userId.
// Stores status + body in lastStatusCode / lastBody.
// Gherkin: kai sends keys/query for "@marie:test.local"
func kaiSendsKeysQueryForUser(userID string) error {
	rawBody := fmt.Sprintf(`{"device_keys":{%q:[]}}`, userID)
	req, err := http.NewRequest(
		http.MethodPost,
		matrixURL+"/_matrix/client/v3/keys/query",
		strings.NewReader(rawBody),
	)
	if err != nil {
		return fmt.Errorf("building POST /keys/query (for user) request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /keys/query (for user) failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading /keys/query (for user) response body: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeMatrixEventCorrectnessSteps registers all step definitions for
// gateway/features/matrix_event_correctness.feature (Story 9-10b).
// Called from InitializeScenario in steps_test.go.
func initializeMatrixEventCorrectnessSteps(sc *godog.ScenarioContext) {
	// Step 1: keys/query with docstring body
	sc.Step(
		`^kai sends POST /_matrix/client/v3/keys/query with body:$`,
		kaiSendsKeysQueryWithBody,
	)

	// Step 2: nested JSON presence check
	// Gherkin: the response JSON has key "device_keys" with child key "@kai:test.local"
	sc.Step(
		`^the response JSON has key "([^"]*)" with child key "([^"]*)"$`,
		theResponseJSONHasKeyWithChildKey,
	)

	// Step 3: nested JSON absence check
	// Gherkin: the response JSON "failures" does not contain key "@kai:test.local"
	sc.Step(
		`^the response JSON "([^"]*)" does not contain key "([^"]*)"$`,
		theResponseJSONKeyDoesNotContainChildKey,
	)

	// Step 4: non-null object assertion (top-level key)
	// Gherkin: the response JSON "device_one_time_keys_count" is a non-null object
	sc.Step(
		`^the response JSON "([^"]*)" is a non-null object$`,
		theResponseJSONKeyIsNonNullObject,
	)

	// Step 5: non-null array assertion (top-level key)
	// Gherkin: the response JSON "device_unused_fallback_key_types" is a non-null array
	// Note: dotted keys like "device_lists.changed" are handled by step 6 below.
	// This regex matches only keys that do NOT contain a dot.
	sc.Step(
		`^the response JSON "([^".]*)" is a non-null array$`,
		theResponseJSONKeyIsNonNullArray,
	)

	// Step 6: non-null array assertion (two-level dotted path)
	// Gherkin: the response JSON "device_lists.changed" is a non-null array
	sc.Step(
		`^the response JSON "([^"]*\.[^"]*)" is a non-null array$`,
		theResponseJSONDottedKeyIsNonNullArray,
	)

	// Step 7: timeline events unsigned.age audit
	// Gherkin: the response JSON timeline events have an unsigned.age field
	sc.Step(
		`^the response JSON timeline events have an unsigned\.age field$`,
		theResponseJSONTimelineEventsHaveUnsignedAge,
	)

	// Step 8: simple GET /sync as kai
	// Gherkin: kai sends GET /_matrix/client/v3/sync
	sc.Step(
		`^kai sends GET /_matrix/client/v3/sync$`,
		kaiSendsGetSync,
	)

	// Step 9: DM room creation
	// Gherkin: kai creates a DM room with "@marie:test.local" and captures the room ID
	sc.Step(
		`^kai creates a DM room with "([^"]*)" and captures the room ID$`,
		kaiCreatesDMRoomWithUserAndCapturesRoomID,
	)

	// Step 10: targeted keys/query for a specific user
	// Gherkin: kai sends keys/query for "@marie:test.local"
	sc.Step(
		`^kai sends keys/query for "([^"]*)"$`,
		kaiSendsKeysQueryForUser,
	)
}

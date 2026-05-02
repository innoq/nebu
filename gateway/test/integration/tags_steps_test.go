//go:build integration

package integration_test

// ─── Story 7-25: Tags API — GET/PUT/DELETE /user/{userId}/rooms/{roomId}/tags ──
//
// Godog step definitions for gateway/features/tags.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// The routes are not registered in main.go → every HTTP call returns 404 until
// GetTags, PutTag, DeleteTag handlers are wired.
//
// State: scenarios share Background setup (kai creates a room).
// The room ID from Background is stored in lastRoomID (set by kaiCreatesARoom
// in room_flow_steps_test.go). kaiAccessToken, alexAccessToken are shared
// package-level vars from room_flow_steps_test.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiCallsGetTags calls
// GET /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/tags authenticated as kai.
func kaiCallsGetTags() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/tags",
		matrixURL, kaiUserID, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /tags request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /tags failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsPutTagWithBody calls
// PUT /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/tags/{tag} with the given body.
func kaiCallsPutTagWithBody(tag, body string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/tags/%s",
		matrixURL, kaiUserID, lastRoomID, tag)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("building PUT /tags/%s request: %w", tag, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /tags/%s failed: %w", tag, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// kaiCallsDeleteTag calls
// DELETE /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/tags/{tag} authenticated as kai.
func kaiCallsDeleteTag(tag string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/tags/%s",
		matrixURL, kaiUserID, lastRoomID, tag)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("building DELETE /tags/%s request: %w", tag, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /tags/%s failed: %w", tag, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiHasSetTagOnRoom is a Given step that pre-sets a tag via PUT.
func kaiHasSetTagOnRoom(tag string) error {
	return kaiCallsPutTagWithBody(tag, `{"order":0.5}`)
}

// alexCallsPutTagForKai calls
// PUT /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/tags/{tag}
// authenticated as alex (userId mismatch — should get 403).
func alexCallsPutTagForKaiWithBody(body string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/tags/m.favourite",
		matrixURL, kaiUserID, lastRoomID)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("building PUT /tags/m.favourite request (alex for kai): %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /tags (alex for kai) failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// unauthenticatedClientCallsGetTags calls GET /tags without any Authorization header.
func unauthenticatedClientCallsGetTags() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/tags",
		matrixURL, kaiUserID, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building unauthenticated GET /tags request: %w", err)
	}
	// Deliberately no Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unauthenticated GET /tags failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theTagsObjectIsEmpty asserts that the "tags" field in the response is a JSON object
// with no keys (AC1: always present, empty when no tags set).
func theTagsObjectIsEmpty() error {
	var body struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON object with 'tags' field, parse error: %w (body: %s)", err, lastBody)
	}
	if body.Tags == nil {
		return fmt.Errorf("'tags' field is null or missing in body: %s", lastBody)
	}
	if len(body.Tags) != 0 {
		return fmt.Errorf("expected empty tags object, got %v (body: %s)", body.Tags, lastBody)
	}
	return nil
}

// theTagsObjectContains asserts that the "tags" field contains the given tag key.
func theTagsObjectContains(tag string) error {
	var body struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON object with 'tags' field, parse error: %w (body: %s)", err, lastBody)
	}
	if _, ok := body.Tags[tag]; !ok {
		return fmt.Errorf("expected tag %q in tags object, got tags: %v (body: %s)", tag, body.Tags, lastBody)
	}
	return nil
}

// theResponseBodyIs asserts the trimmed response body equals the expected string.
func theResponseBodyIs(expected string) error {
	trimmed := strings.TrimSpace(lastBody)
	// Normalize: compare without trailing newline / whitespace.
	if trimmed != expected {
		return fmt.Errorf("expected body %q, got %q", expected, trimmed)
	}
	return nil
}

// kaiPutsTagForRoom calls
// PUT /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/tags/{tag}
// with the given JSON body, authenticated as kai.
// Used in the Story 7-36 sync propagation scenario.
func kaiPutsTagForRoom(tag, body string) error {
	return kaiCallsPutTagWithBody(tag, body)
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeTagsSteps registers all step definitions for tags.feature.
// Called from InitializeScenario in steps_test.go.
func initializeTagsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /user/\{userId\}/rooms/\{roomId\}/tags$`, kaiCallsGetTags)
	sc.Step(`^kai calls PUT /user/\{userId\}/rooms/\{roomId\}/tags/([^ ]+) with body (.+)$`, kaiCallsPutTagWithBody)
	sc.Step(`^kai calls DELETE /user/\{userId\}/rooms/\{roomId\}/tags/(\S+)$`, kaiCallsDeleteTag)
	sc.Step(`^kai has set tag "([^"]+)" on the room$`, kaiHasSetTagOnRoom)
	sc.Step(`^alex calls PUT /user/kai-userId/rooms/\{roomId\}/tags/m\.favourite with body (.+)$`, alexCallsPutTagForKaiWithBody)
	sc.Step(`^an unauthenticated client calls GET /user/\{userId\}/rooms/\{roomId\}/tags$`, unauthenticatedClientCallsGetTags)
	sc.Step(`^the tags object is empty$`, theTagsObjectIsEmpty)
	sc.Step(`^the tags object contains "([^"]+)"$`, theTagsObjectContains)
	sc.Step(`^the response body is "([^"]*)"$`, theResponseBodyIs)
	// Story 7-36: tag sync propagation steps
	sc.Step(`^kai puts tag "([^"]*)" with body (.+?) for the created room$`, kaiPutsTagForRoom)
}

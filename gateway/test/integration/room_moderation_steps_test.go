//go:build integration

package integration_test

// ─── Story 7-22: POST /kick, /ban, /unban, /forget ────────────────────────────
//
// Godog step definitions for gateway/features/room_moderation.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// The routes are not registered in main.go → every HTTP call returns 404 until
// the ModerationHandler is wired.
//
// State: scenarios share Background setup (kai creates a room; alex and marie join).
// The room ID from Background is stored in lastRoomID (set by kaiCreatesARoom
// in room_flow_steps_test.go). kaiAccessToken, alexAccessToken, marieAccessToken,
// and kaiUserID, alexUserID, marieUserID are shared package-level vars from
// room_flow_steps_test.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiKicksAlex calls POST /_matrix/client/v3/rooms/{lastRoomID}/kick
// authenticated as kai, targeting alexUserID.
func kaiKicksAlex() error {
	return postModerationAction(lastRoomID, "kick", kaiAccessToken,
		fmt.Sprintf(`{"user_id":%q}`, alexUserID))
}

// alexKicksMarie calls POST /_matrix/client/v3/rooms/{lastRoomID}/kick
// authenticated as alex (power level 0), targeting marieUserID.
// alex lacks the kick power level → Core should return PermissionDenied → 403.
func alexKicksMarie() error {
	return postModerationAction(lastRoomID, "kick", alexAccessToken,
		fmt.Sprintf(`{"user_id":%q}`, marieUserID))
}

// kaiBansAlex calls POST /_matrix/client/v3/rooms/{lastRoomID}/ban
// authenticated as kai, targeting alexUserID.
func kaiBansAlex() error {
	return postModerationAction(lastRoomID, "ban", kaiAccessToken,
		fmt.Sprintf(`{"user_id":%q}`, alexUserID))
}

// kaiHasBannedAlex is a Background-level precondition step: it bans alex
// as kai so that subsequent unban tests start from the correct state.
// Sets up the banned state without asserting on lastStatusCode.
func kaiHasBannedAlex() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/ban", matrixURL, lastRoomID)
	body := fmt.Sprintf(`{"user_id":%q}`, alexUserID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building ban precondition request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ban precondition POST failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	// Allow 200 (success) only; treat anything else as a test setup failure.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ban precondition: expected 200, got %d (body: %s)", resp.StatusCode, string(respBody))
	}
	return nil
}

// kaiUnbansAlex calls POST /_matrix/client/v3/rooms/{lastRoomID}/unban
// authenticated as kai, targeting alexUserID.
func kaiUnbansAlex() error {
	return postModerationAction(lastRoomID, "unban", kaiAccessToken,
		fmt.Sprintf(`{"user_id":%q}`, alexUserID))
}

// alexHasLeftTheModerationTestRoom calls POST /leave as alex on lastRoomID.
// This is the precondition for the Forget_Success_AfterLeave scenario.
func alexHasLeftTheModerationTestRoom() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/leave", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building leave request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /leave failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leave precondition: expected 200, got %d (body: %s)", resp.StatusCode, string(respBody))
	}
	return nil
}

// alexForgetsTheModerationTestRoom calls POST /_matrix/client/v3/rooms/{lastRoomID}/forget
// authenticated as alex.
func alexForgetsTheModerationTestRoom() error {
	return postModerationAction(lastRoomID, "forget", alexAccessToken, "{}")
}

// kaiForgetsTheModerationTestRoom calls POST /_matrix/client/v3/rooms/{lastRoomID}/forget
// authenticated as kai (who is still joined → should return 403).
func kaiForgetsTheModerationTestRoom() error {
	return postModerationAction(lastRoomID, "forget", kaiAccessToken, "{}")
}

// kaiPostsKickWithoutUserID calls POST /kick with a body missing user_id.
// Verifies AC5 (400 M_BAD_JSON for missing required field).
func kaiPostsKickWithoutUserID() error {
	return postModerationAction(lastRoomID, "kick", kaiAccessToken, `{"reason":"no user id here"}`)
}

// kaiBansAlexFromRoom calls POST /ban on the specified roomID (allows passing
// an unknown room ID for the AC6 not-found scenario).
func kaiBansAlexFromRoom(roomID string) error {
	return postModerationAction(roomID, "ban", kaiAccessToken,
		fmt.Sprintf(`{"user_id":%q}`, alexUserID))
}

// unauthenticatedClientKicksAlex calls POST /kick without Authorization header.
// jwtMiddleware must reject with 401 before the handler is reached.
func unauthenticatedClientKicksAlex() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/kick", matrixURL, lastRoomID)
	body := fmt.Sprintf(`{"user_id":%q}`, alexUserID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building unauthenticated kick request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unauthenticated POST /kick failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theResponseBodyIsEmptyObject asserts the response body is exactly "{}".
// Matrix spec: all four moderation endpoints return {} on success.
func theResponseBodyIsEmptyObject(expected string) error {
	// Normalise: trim whitespace and newlines.
	trimmed := strings.TrimSpace(lastBody)
	// Parse as JSON to be tolerant of formatting differences.
	var got interface{}
	if err := json.Unmarshal([]byte(trimmed), &got); err != nil {
		return fmt.Errorf("expected JSON body %q but body is not valid JSON: %s", expected, lastBody)
	}
	// Re-marshal expected to normalise it.
	var want interface{}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		return fmt.Errorf("test bug: expected value %q is not valid JSON", expected)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		return fmt.Errorf("expected body %s, got %s", wantJSON, gotJSON)
	}
	return nil
}

// ─── Shared HTTP helper ───────────────────────────────────────────────────────

// postModerationAction issues a POST to /_matrix/client/v3/rooms/{roomID}/{action}
// with the given accessToken and JSON body. Stores the result in lastStatusCode/lastBody.
func postModerationAction(roomID, action, accessToken, body string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/%s", matrixURL, roomID, action)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building POST /%s request: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /%s failed: %w", action, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeModerationSteps registers all step definitions for room_moderation.feature.
// Called from InitializeScenario in steps_test.go.
func initializeModerationSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai kicks alex from the room$`, kaiKicksAlex)
	sc.Step(`^alex kicks marie from the room$`, alexKicksMarie)
	sc.Step(`^kai bans alex from the room$`, kaiBansAlex)
	sc.Step(`^kai has banned alex from the room$`, kaiHasBannedAlex)
	sc.Step(`^kai unbans alex from the room$`, kaiUnbansAlex)
	sc.Step(`^alex has left the moderation test room$`, alexHasLeftTheModerationTestRoom)
	sc.Step(`^alex forgets the moderation test room$`, alexForgetsTheModerationTestRoom)
	sc.Step(`^kai forgets the moderation test room$`, kaiForgetsTheModerationTestRoom)
	sc.Step(`^kai posts kick without user_id$`, kaiPostsKickWithoutUserID)
	sc.Step(`^kai bans alex from room "([^"]+)"$`, kaiBansAlexFromRoom)
	sc.Step(`^an unauthenticated client kicks alex from the room$`, unauthenticatedClientKicksAlex)
	sc.Step(`^the response body is "([^"]*)"$`, theResponseBodyIsEmptyObject)
}

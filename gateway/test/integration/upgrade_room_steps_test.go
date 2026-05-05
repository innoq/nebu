//go:build integration

package integration_test

// ─── Story 9-8: Room Version Upgrade — Full Implementation ───────────────────
//
// Godog step definitions for:
//   - gateway/features/upgrade_room.feature (Story 9-8)
//
// Written FIRST (ATDD gate) — all steps that call POST /upgrade will receive
// 501 Not Implemented until Story 9-8 is fully implemented. The Godog scenarios
// are expected to fail in the red phase.
//
// Steps defined here:
//   - kaiPostsUpgradeForRoomWithNewVersion(roomName, newVersion)    — POST as kai
//   - mariePostsUpgradeForRoomWithNewVersion(roomName, newVersion)  — POST as marie
//   - kaiCallsGetRoomStateForNewRoom(eventType)                     — GET /state/{type} on newRoomID
//   - iCallGETCapabilities                                          — GET /capabilities
//
// Steps already registered elsewhere (do NOT re-register):
//   - "the docker compose stack is started"    → steps_test.go
//   - "kai is authenticated via OIDC"          → room_flow_steps_test.go
//   - "marie is authenticated via OIDC"        → room_flow_steps_test.go
//   - "kai creates a room named ..."           → room_flow_steps_test.go
//   - "the response status is N"               → steps_test.go
//   - "the response body contains ..."         → steps_test.go
//   - "I call GET /... on the gateway"         → steps_test.go (note: different pattern)

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// alexUpgradeInviteSyncBody holds the sync response body captured by
// alexCallsGetSyncAndSeesNewRoomInInvite, used to assert rooms.invite membership.
var alexUpgradeInviteSyncBody string

// lastNewRoomID holds the replacement_room value returned by a successful upgrade.
// Populated by kaiPostsUpgradeForRoomWithNewVersion when the upgrade succeeds.
// Used by kaiCallsGetRoomStateForNewRoom to GET state on the newly created room.
var lastNewRoomID string

// kaiPostsUpgradeForRoomWithNewVersion sends POST /_matrix/client/v3/rooms/{lastRoomID}/upgrade
// authenticated as kai with the given new_version.
//
// Stores status + body in lastStatusCode/lastBody.
// If the response is 200 and contains replacement_room, stores it in lastNewRoomID
// for subsequent steps that operate on the new room.
//
// RED PHASE: currently returns 501 M_UNRECOGNIZED — all callers that assert 200 will fail.
func kaiPostsUpgradeForRoomWithNewVersion(_, newVersion string) error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/upgrade",
		matrixURL, lastRoomID,
	)
	payload := fmt.Sprintf(`{"new_version":%q}`, newVersion)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building POST /upgrade request (kai): %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /upgrade (kai) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	// If the response is 200, extract replacement_room for subsequent steps.
	if resp.StatusCode == http.StatusOK {
		var upgradeResp struct {
			ReplacementRoom string `json:"replacement_room"`
		}
		if err := json.Unmarshal(body, &upgradeResp); err == nil && upgradeResp.ReplacementRoom != "" {
			lastNewRoomID = upgradeResp.ReplacementRoom
		}
	}

	return nil
}

// mariePostsUpgradeForRoomWithNewVersion sends POST /_matrix/client/v3/rooms/{lastRoomID}/upgrade
// authenticated as marie with the given new_version.
//
// Marie is not the room owner (not a member), so Core should return PermissionDenied → 403.
//
// Stores status + body in lastStatusCode/lastBody.
// RED PHASE: currently returns 501 (handler never reaches the gRPC call).
func mariePostsUpgradeForRoomWithNewVersion(_, newVersion string) error {
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/upgrade",
		matrixURL, lastRoomID,
	)
	payload := fmt.Sprintf(`{"new_version":%q}`, newVersion)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building POST /upgrade request (marie): %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /upgrade (marie) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetRoomStateForNewRoom sends
// GET /_matrix/client/v3/rooms/{lastNewRoomID}/state/{eventType}
// authenticated as kai.
//
// Used to verify that the new room's m.room.create event contains a predecessor field.
// RED PHASE: currently fails because no new room is created (upgrade returns 501).
func kaiCallsGetRoomStateForNewRoom(eventType string) error {
	if lastNewRoomID == "" {
		return fmt.Errorf("lastNewRoomID is empty — upgrade did not return a replacement_room (current upgrade response: %s)", lastBody)
	}
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/state/%s",
		matrixURL, lastNewRoomID, eventType,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /state/%s request on new room: %w", eventType, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /state/%s on new room failed: %w", eventType, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// iCallGETCapabilities calls GET /_matrix/client/v3/capabilities on the gateway
// (unauthenticated — capabilities is a public endpoint).
//
// Stores status + body in lastStatusCode/lastBody.
// RED PHASE: currently returns {"default":"6"} — will fail the assertion for "10".
func iCallGETCapabilities() error {
	url := matrixURL + "/_matrix/client/v3/capabilities"
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET /capabilities failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// alexCallsGetSyncAndSeesNewRoomInInvite calls GET /sync?timeout=0 as alex and
// asserts that lastNewRoomID appears in rooms.invite of the response.
//
// AC4 (Story 9-8): After kai upgrades the room, all previous members (here alex)
// should receive an invitation to the new room. This step polls sync up to 3 times
// with a short delay to allow the invite to propagate.
//
// RED PHASE: currently fails because upgrade returns 501 and no invites are sent.
func alexCallsGetSyncAndSeesNewRoomInInvite() error {
	if lastNewRoomID == "" {
		return fmt.Errorf("lastNewRoomID is empty — upgrade did not return a replacement_room (current upgrade response: %s)", lastBody)
	}

	syncURL := fmt.Sprintf("%s/_matrix/client/v3/sync?timeout=0", matrixURL)

	var body []byte
	var statusCode int
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest(http.MethodGet, syncURL, nil)
		if err != nil {
			return fmt.Errorf("building GET /sync request (alex): %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+alexAccessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("GET /sync (alex) failed: %w", err)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode
		if statusCode == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("alex sync returned %d: %s", statusCode, string(body))
	}
	alexUpgradeInviteSyncBody = string(body)

	var syncResp struct {
		Rooms struct {
			Invite map[string]json.RawMessage `json:"invite"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return fmt.Errorf("parsing alex sync response: %w (body: %s)", err, string(body))
	}
	if _, ok := syncResp.Rooms.Invite[lastNewRoomID]; !ok {
		return fmt.Errorf("new room %q not found in alex's rooms.invite — invite was not sent after upgrade.\nSync body: %s",
			lastNewRoomID, alexUpgradeInviteSyncBody)
	}
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeUpgradeRoomSteps registers all step definitions for upgrade_room.feature.
// Called from InitializeScenario in steps_test.go.
func initializeUpgradeRoomSteps(sc *godog.ScenarioContext) {
	// POST /rooms/{roomId}/upgrade steps
	// Gherkin pattern: kai posts upgrade for room "upgrade-test-room" with new_version "10"
	sc.Step(
		`^kai posts upgrade for room "([^"]*)" with new_version "([^"]*)"$`,
		kaiPostsUpgradeForRoomWithNewVersion,
	)
	// Gherkin pattern: marie posts upgrade for room "upgrade-test-room" with new_version "10"
	sc.Step(
		`^marie posts upgrade for room "([^"]*)" with new_version "([^"]*)"$`,
		mariePostsUpgradeForRoomWithNewVersion,
	)

	// GET /rooms/{newRoomId}/state/{eventType} step (for predecessor verification)
	// Gherkin pattern: kai calls GET /rooms/{newRoomId}/state/m.room.create
	sc.Step(
		`^kai calls GET /rooms/\{newRoomId\}/state/([^\s]+)$`,
		kaiCallsGetRoomStateForNewRoom,
	)

	// GET /capabilities step (for AC6 verification)
	// Gherkin pattern: I call GET /_matrix/client/v3/capabilities
	sc.Step(
		`^I call GET /_matrix/client/v3/capabilities$`,
		iCallGETCapabilities,
	)

	// AC4: alex calls GET /sync and sees the new room in rooms.invite
	// Gherkin pattern: alex calls GET /sync and sees the new room in rooms.invite
	sc.Step(
		`^alex calls GET /sync and sees the new room in rooms\.invite$`,
		alexCallsGetSyncAndSeesNewRoomInInvite,
	)
}
